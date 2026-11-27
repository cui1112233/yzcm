package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/auth"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/screenplay"
	"qiantie/backend/internal/yzcmusage"
	"strings"
	"time"
)

type ScreenplaySessionChannel struct {
	accounts auth.Store
	center   accountcenter.Store
	catalog  func(context.Context, string, string) (*batchfactoryv11.OpenAICompatibleProvider, error)
	tracker  *yzcmusage.Tracker
}

func NewScreenplaySessionChannel(accounts auth.Store, center accountcenter.Store, catalog func(context.Context, string, string) (*batchfactoryv11.OpenAICompatibleProvider, error), tracker *yzcmusage.Tracker) *ScreenplaySessionChannel {
	if accounts == nil || center == nil || catalog == nil || tracker == nil {
		return nil
	}
	return &ScreenplaySessionChannel{accounts, center, catalog, tracker}
}

type sessionUsageRepository interface {
	ReserveSessionAttempt(context.Context, screenplay.AssistantSessionRequest, string, string, string, string) (string, error)
	ClaimSessionAttempt(context.Context, screenplay.AssistantSessionRequest, string, string, string, string) error
}
type sessionChannelGenerator struct {
	channel     *ScreenplaySessionChannel
	repo        sessionUsageRepository
	subject     string
	verify      func(context.Context) error
	commitGuard func(context.Context, *sql.Tx) error
}

// Keep the original credentials only inside the authenticated request handler.
func sessionAuthorityCheck(auth BridgeAuth, original *http.Request, subject string) func(context.Context) error {
	return newSessionAuthority(auth, original, subject).verify
}

type sessionAuthorityCallbacks struct {
	verify func(context.Context) error
	commit func(context.Context, *sql.Tx) error
}

func newSessionAuthority(auth BridgeAuth, original *http.Request, subject string) sessionAuthorityCallbacks {
	var frozenVersion string
	var versionCaptured bool
	verify := func(ctx context.Context) error {
		// Shadow the cached switch so impersonation is validated from its record.
		fresh := original.WithContext(context.WithValue(ctx, impersonationContextKey{}, struct{}{}))
		// Paid sessions require the original revocable login. Do not fall back to
		// a signed bridge request or a replacement login after revocation.
		token := bearerToken(original)
		if token == "" || auth.AuthStore == nil {
			return screenplay.ErrAccessDenied
		}
		now := time.Now
		if auth.Now != nil {
			now = auth.Now
		}
		loginSession, err := auth.AuthStore.FindSession(ctx, token, now().UTC())
		if err != nil {
			return screenplay.ErrAccessDenied
		}
		loginAccount, err := auth.AuthStore.FindAccount(ctx, loginSession.Username)
		if err != nil || !loginAccount.Active || loginAccount.Username != loginSession.Username {
			return screenplay.ErrAccessDenied
		}
		login := BridgeIdentity{Username: loginAccount.Username, IsOwner: loginAccount.IsOwner}
		frozen, hasAccount := AccountCenterIdentityFromContext(original.Context())
		if !hasAccount {
			if auth.AccountCenterStore != nil || login.Username != subject {
				return screenplay.ErrAccessDenied
			}
			return nil
		}
		if auth.AccountCenterStore == nil || login.Username != frozen.Actor.Username {
			return screenplay.ErrAccessDenied
		}
		actor, err := auth.AccountCenterStore.ResolveIdentity(ctx, login.Username)
		if err != nil || !actor.Active || actor.ArchivedAt != nil || actor.Username != login.Username {
			return screenplay.ErrAccessDenied
		}
		if frozen.ImpersonationID != "" {
			active, err := (accountImpersonation{auth: auth}).active(fresh, actor)
			if err != nil || active.identity.ImpersonationID != frozen.ImpersonationID || active.identity.Subject.Username != subject || active.identity.Subject.TeamID != frozen.Subject.TeamID {
				return screenplay.ErrAccessDenied
			}
		} else if actor.Username != subject || actor.TeamID != frozen.Subject.TeamID {
			return screenplay.ErrAccessDenied
		}
		access, ok := auth.AccountCenterStore.(accountcenter.BusinessAccessReader)
		if !ok {
			return screenplay.ErrAccessDenied
		}
		allowed, err := access.BusinessAllowed(ctx, subject)
		if err != nil || !allowed {
			return screenplay.ErrAccessDenied
		}
		reader, ok := auth.AccountCenterStore.(accountcenter.SessionAuthorityReader)
		if !ok {
			return screenplay.ErrAccessDenied
		}
		version, err := reader.SessionAuthorityVersion(ctx, frozen.Actor.Username, subject, frozen.Subject.TeamID)
		if err != nil || version == "" {
			return screenplay.ErrAccessDenied
		}
		if !versionCaptured {
			frozenVersion = version
			versionCaptured = true
		} else if frozenVersion != version {
			return screenplay.ErrAccessDenied
		}
		return nil
	}
	return sessionAuthorityCallbacks{verify: verify, commit: func(ctx context.Context, tx *sql.Tx) error {
		identity, ok := AccountCenterIdentityFromContext(original.Context())
		if !ok || !versionCaptured || identity.Subject.Username != subject {
			return screenplay.ErrAccessDenied
		}
		token := bearerToken(original)
		if token == "" {
			return screenplay.ErrAccessDenied
		}
		digest := sha256.Sum256([]byte(token))
		frozen := accountcenter.SessionCommitAuthority{Identity: identity, LoginTokenHash: hex.EncodeToString(digest[:]), Version: frozenVersion}
		if identity.ImpersonationID != "" {
			cookie, err := original.Cookie(impersonationCookieName)
			if err != nil || len(cookie.Value) != 43 {
				return screenplay.ErrAccessDenied
			}
			hash := impersonationTokenHash(cookie.Value, token)
			frozen.ImpersonationTokenHash = hash[:]
		}
		if err := accountcenter.GuardSessionCommit(ctx, tx, frozen); err != nil {
			return errors.Join(screenplay.ErrAccessDenied, err)
		}
		return nil
	}}
}

// Only GenerateSessionStage has a durable reservation.
func (g sessionChannelGenerator) Generate(context.Context, screenplay.TextRequest) (screenplay.TextResponse, error) {
	return screenplay.TextResponse{}, errSessionReceiptUnavailable
}

func sessionBillingContext(ctx context.Context, subject string) (context.Context, string, string, error) {
	ident, ok := BridgeIdentityFromContext(ctx)
	if !ok || ident.Username != subject || subject == "" {
		return ctx, "", "", screenplay.ErrAccessDenied
	}
	ctx = withBridgeUsageAttribution(ctx, subject, "assistant-session")
	team, actor := yzcmusage.AttributionForOwner(ctx, subject)
	if account, ok := AccountCenterIdentityFromContext(ctx); ok {
		if account.Subject.Username != subject || actor == "" || actor != account.Actor.Username {
			return ctx, "", "", screenplay.ErrAccessDenied
		}
	} else {
		actor = subject
		team = ""
		ctx = yzcmusage.WithAttribution(ctx, subject, team, actor)
	}
	return ctx, team, actor, nil
}

func (g sessionChannelGenerator) provider(ctx context.Context, model string) (*batchfactoryv11.OpenAICompatibleProvider, error) {
	if g.channel == nil || g.repo == nil || g.verify == nil || strings.TrimSpace(model) == "" {
		return nil, errSessionReceiptUnavailable
	}
	if err := g.verify(ctx); err != nil {
		return nil, err
	}
	_, _, _, err := sessionBillingContext(ctx, g.subject)
	if err != nil {
		return nil, err
	}
	c := g.channel
	account, err := c.accounts.FindAccount(ctx, g.subject)
	if err != nil || !account.Active {
		return nil, screenplay.ErrAccessDenied
	}
	owner, err := ResolveAPIConfigOwner(ctx, c.center, g.subject)
	if err != nil {
		return nil, screenplay.ErrAccessDenied
	}
	if owner != g.subject {
		acct, err := c.accounts.FindAccount(ctx, owner)
		if err != nil || !acct.Active {
			return nil, screenplay.ErrAccessDenied
		}
	}
	p, err := c.catalog(ctx, owner, model)
	if err != nil {
		return nil, errSessionReceiptUnavailable
	}
	if p == nil || p.Client != nil || p.Endpoint != yzcmusage.SessionChatEndpoint || strings.TrimSpace(p.APIKey) == "" || strings.TrimSpace(p.Model) == "" {
		return nil, errSessionReceiptUnavailable
	}
	return &batchfactoryv11.OpenAICompatibleProvider{Endpoint: yzcmusage.SessionChatEndpoint, APIKey: p.APIKey, Model: p.Model, UsageOwner: g.subject, Client: c.tracker.NewSessionClient()}, nil
}

func (g sessionChannelGenerator) checkSessionModel(ctx context.Context, model string) error {
	_, err := g.provider(ctx, model)
	if err != nil {
		return err
	}
	if err := g.channel.tracker.CheckSessionStorage(ctx); err != nil {
		return errSessionReceiptUnavailable
	}
	return nil
}

func (g sessionChannelGenerator) GenerateSessionStage(ctx context.Context, req screenplay.AssistantSessionRequest, stage string, input screenplay.TextRequest) (screenplay.TextResponse, error) {
	if req.Actor != g.subject {
		return screenplay.TextResponse{}, screenplay.ErrAccessDenied
	}
	p, err := g.provider(ctx, input.Model)
	if err != nil {
		return screenplay.TextResponse{}, errors.Join(yzcmusage.ErrSessionNotSent, err)
	}
	ctx, team, actor, err := sessionBillingContext(ctx, g.subject)
	if err != nil {
		return screenplay.TextResponse{}, errors.Join(yzcmusage.ErrSessionNotSent, err)
	}
	hash := yzcmusage.Fingerprint(p.APIKey)
	id, err := g.repo.ReserveSessionAttempt(ctx, req, stage, team, actor, hash)
	if err != nil {
		return screenplay.TextResponse{}, errors.Join(yzcmusage.ErrSessionNotSent, err)
	}
	ctx = yzcmusage.WithSessionAttempt(ctx, id, g.subject, team, actor, hash, func(ctx context.Context) error {
		current, err := g.provider(ctx, input.Model)
		if err != nil {
			return err
		}
		if current.Model != p.Model || yzcmusage.Fingerprint(current.APIKey) != hash {
			return screenplay.ErrAccessDenied
		}
		return g.repo.ClaimSessionAttempt(ctx, req, id, team, actor, hash)
	})
	input.SingleAttempt = true
	generator := screenplayTextGenerator{owner: g.subject, resolver: func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) { return p, nil }}
	response, err := generator.Generate(ctx, input)
	if err != nil {
		return response, err
	}
	// The transport has already captured any real receipt. A revoked caller
	// must not receive or persist model content from this completed attempt.
	if err := g.checkSessionAuthority(ctx); err != nil {
		return screenplay.TextResponse{}, err
	}
	return response, nil
}

func (g sessionChannelGenerator) checkSessionAuthority(ctx context.Context) error {
	if g.verify == nil {
		return screenplay.ErrAccessDenied
	}
	return g.verify(ctx)
}
