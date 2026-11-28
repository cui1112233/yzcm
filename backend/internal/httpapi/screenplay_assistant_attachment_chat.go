package httpapi

import (
	"context"
	"database/sql"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/screenplay"
	"reflect"
	"sync"
)

// Freeze selection materials for this file-bearing chat only. Later catalog
// reads verify equality; they can reject but never replace the chosen materials.
func freezeAssistantAttachmentResolver(resolve func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error)) func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
	var mu sync.Mutex
	var frozen *batchfactoryv11.OpenAICompatibleProvider
	var frozenOwner, frozenSelection string
	return func(ctx context.Context, owner, selection string) (batchfactoryv11.DirectorProvider, error) {
		mu.Lock()
		defer mu.Unlock()
		if resolve == nil {
			return nil, screenplay.ErrAPIKeyMissing
		}
		if frozen != nil && (owner != frozenOwner || selection != frozenSelection) {
			return nil, screenplay.ErrAccessDenied
		}
		provider, e := resolve(ctx, owner, selection)
		if e != nil {
			return nil, e
		}
		p, ok := provider.(*batchfactoryv11.OpenAICompatibleProvider)
		if !ok || p == nil {
			return nil, screenplay.ErrInvalidState
		}
		if frozen == nil {
			copy := *p
			if p.Metadata.SupportsTemperature != nil {
				v := *p.Metadata.SupportsTemperature
				copy.Metadata.SupportsTemperature = &v
			}
			if p.Metadata.SupportsMaxTokens != nil {
				v := *p.Metadata.SupportsMaxTokens
				copy.Metadata.SupportsMaxTokens = &v
			}
			frozen = &copy
			frozenOwner = owner
			frozenSelection = selection
		} else if p.Endpoint != frozen.Endpoint || p.APIKey != frozen.APIKey || p.Model != frozen.Model || p.UsageOwner != frozen.UsageOwner || p.Client != frozen.Client || !reflect.DeepEqual(p.Metadata, frozen.Metadata) {
			return nil, screenplay.ErrAccessDenied
		}
		return frozen, nil
	}
}
func freezeAssistantAttachmentSessionGenerator(g sessionChannelGenerator) (sessionChannelGenerator, error) {
	if g.channel == nil || g.channel.catalog == nil || g.repo == nil || g.verify == nil || g.commitGuard == nil {
		return g, errSessionReceiptUnavailable
	}
	original := g.channel.catalog
	frozen := freezeAssistantAttachmentResolver(func(ctx context.Context, owner, selection string) (batchfactoryv11.DirectorProvider, error) {
		return original(ctx, owner, selection)
	})
	copy := *g.channel
	copy.catalog = func(ctx context.Context, owner, selection string) (*batchfactoryv11.OpenAICompatibleProvider, error) {
		provider, e := frozen(ctx, owner, selection)
		if e != nil {
			return nil, e
		}
		p, ok := provider.(*batchfactoryv11.OpenAICompatibleProvider)
		if !ok {
			return nil, errSessionReceiptUnavailable
		}
		return p, nil
	}
	g.channel = &copy
	return g, nil
}
func checkAssistantAttachmentSessionBudget(ctx context.Context, g sessionChannelGenerator, request screenplay.TextRequest) error {
	p, e := g.provider(ctx, request.Model)
	if e != nil {
		return e
	}
	checked := request
	checked.Model = p.Model
	return checkReferenceRequestBudget(checked)
}
func assistantReferenceModelHistory(messages []screenplay.AssistantMessage) []screenplay.AssistantMessage {
	out := append([]screenplay.AssistantMessage(nil), messages...)
	for i := range out {
		out[i].References = nil
		out[i].ReferenceOperationID = ""
	}
	return out
}
func hasAssistantAttachmentSelectors(selectors []screenplay.AssistantReferenceSelector) bool {
	for _, s := range selectors {
		if s.Kind == "attachment_text" {
			return true
		}
	}
	return false
}
func checkAssistantAttachmentChatScope(ctx context.Context, scope screenplay.AssistantReferenceScope, owner, project, effective, session string) error {
	if scope.ValidatePrepare() != nil {
		return screenplay.ErrAccessDenied
	}
	identity, ok := AccountCenterIdentityFromContext(ctx)
	if !ok || identity.Subject.Username != effective {
		return screenplay.ErrAccessDenied
	}
	bridge, ok := BridgeIdentityFromContext(ctx)
	if !ok || bridge.Username != effective {
		return screenplay.ErrAccessDenied
	}
	expected, e := screenplay.NewAssistantReferenceScope(owner, project, session, effective, identity.Actor.Username)
	if e != nil || expected != scope {
		return screenplay.ErrAccessDenied
	}
	return nil
}

// This is the post-provider save path. The unchanged original login/paid-channel
// guard runs first in the same SQL transaction as source checks and publication.
func finishAssistantAttachmentSession(ctx context.Context, repo *screenplay.MySQLAuthoringRepository, g sessionChannelGenerator, access assistantAttachmentAccess, req screenplay.AssistantSessionRequest, history *screenplay.AssistantSessionHistory, operationID, message string, refs []screenplay.AssistantReferenceSnapshot) error {
	if repo == nil || history == nil || g.channel == nil || g.repo != repo || g.subject != req.Actor || g.commitGuard == nil {
		return screenplay.ErrAccessDenied
	}
	if e := checkAssistantAttachmentChatScope(ctx, access.Scope, req.Owner, req.Project, req.Actor, req.SessionID); e != nil {
		return e
	}
	commit := screenplay.AssistantAttachmentMessageCommit{OperationID: operationID, UserContent: message, PreviousMessages: req.History.Messages, CandidateMessages: history.Messages, References: refs}
	return repo.FinishAssistantRequestGuarded(ctx, req, history, func(ctx context.Context, tx *sql.Tx) error {
		if e := g.commitGuard(ctx, tx); e != nil {
			return e
		}
		return screenplay.CheckAssistantReferencesScopedInTx(ctx, tx, access.Scope, commit)
	})
}
