package httpapi

import (
	"context"
	"database/sql"
	"net/http"
	"qiantie/backend/internal/screenplay"
)

type assistantAttachmentAccess struct {
	Scope         screenplay.AssistantReferenceScope
	ReadGuard     screenplay.AssistantAttachmentGuard
	VerifyPrepare func(context.Context) error
	CommitPrepare screenplay.AssistantAttachmentGuard
	CanPrepare    bool
}

func assistantAttachmentRequestAccess(auth BridgeAuth, r *http.Request, svc screenplay.AuthoringService, owner, effective, session string, requireStrong bool) (out assistantAttachmentAccess, err error) {
	project := r.PathValue("id")
	bridge, ok := BridgeIdentityFromContext(r.Context())
	if !ok || bridge.Username != effective {
		return out, screenplay.ErrAccessDenied
	}
	if session == "" {
		return out, screenplay.ErrInvalidArgument
	}
	resolved, e := svc.Resolve(r.Context(), effective, project, "read")
	if e != nil {
		return out, e
	}
	if resolved != owner {
		return out, screenplay.ErrAccessDenied
	}
	if e = checkReferenceSession(r.Context(), svc, owner, project, effective, session); e != nil {
		return out, e
	}
	actual := ""
	_, switched := switchedBridgeIdentity(r)
	strong := bearerToken(r) != "" || switched
	if strong {
		authority := newSessionAuthority(auth, r, effective)
		if e = authority.verify(r.Context()); e != nil {
			return out, e
		}
		identity, valid := AccountCenterIdentityFromContext(r.Context())
		if !valid || identity.Subject.Username != effective || identity.Actor.Username == "" {
			return out, screenplay.ErrAccessDenied
		}
		actual = identity.Actor.Username
		// The SQL guard revalidates the original frozen identity in this tx.
		// Do not open autocommit auth reads while project/session locks are held.
		guarded := screenplay.AssistantAttachmentGuard(authority.commit)
		out.ReadGuard = guarded
		out.VerifyPrepare = authority.verify
		out.CommitPrepare = guarded
	} else {
		if requireStrong {
			return out, screenplay.ErrAccessDenied
		}
		// HMAC may carry account-center profile data; it is not original-login proof.
		out.ReadGuard = func(ctx context.Context, _ *sql.Tx) error {
			identity, ok := BridgeIdentityFromContext(ctx)
			if !ok || identity.Username != effective {
				return screenplay.ErrAccessDenied
			}
			return ctx.Err()
		}
	}
	out.Scope, e = screenplay.NewAssistantReferenceScope(owner, project, session, effective, actual)
	if e != nil {
		return out, e
	}
	if strong {
		if _, e := svc.Resolve(r.Context(), effective, project, "edit"); e == nil {
			out.CanPrepare = true
		} else if _, e = svc.Resolve(r.Context(), effective, project, "generate"); e == nil {
			out.CanPrepare = true
		}
	}
	if requireStrong && !out.CanPrepare {
		return out, screenplay.ErrAccessDenied
	}
	return out, nil
}
