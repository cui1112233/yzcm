package yzcmusage

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"path/filepath"
	"qiantie/backend/internal/safehttp"
	"strings"
	"time"
)

var ErrSessionReceiptUnknown = errors.New("本轮模型请求的回执未确认，等待核账")
var ErrSessionReceiptOnDisk = errors.New("本轮模型回执已保存，等待写入数据库")
var ErrSessionNotSent = safehttp.ErrRequestNotSent
var ErrSessionGuard = errors.New("会话计费通道或发送权限无效")

const SessionChatEndpoint = "https://newapi.yzcm.site/v1/chat/completions"

type sessionAttemptKey struct{}
type sessionAttempt struct {
	id, subject, team, actor, keyHash string
	claim                             func(context.Context) error
}

func WithSessionAttempt(ctx context.Context, id, subject, team, actor, keyHash string, claim func(context.Context) error) context.Context {
	return context.WithValue(ctx, sessionAttemptKey{}, sessionAttempt{id, subject, team, actor, keyHash, claim})
}

// This client owns a fresh HTTP/1 transport, with neither proxies nor reused
// connections. It accepts only one fixed POST path. Callers cannot inject a
// provider-specific client below the durable claim.
func (t *Tracker) NewSessionClient() *http.Client {
	transport := safehttp.NewSinglePostTransport()
	return &http.Client{Transport: &trackedTransport{base: transport, t: t, session: true}, Timeout: 5 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (a sessionAttempt) matches(r *http.Request, i identity) bool {
	if a.claim == nil || a.id == "" || a.subject == "" || a.actor == "" {
		return false
	}
	if i.Owner != a.subject || i.TeamID != a.team || i.ActorUsername != a.actor {
		return false
	}
	if r.URL == nil || r.URL.String() != SessionChatEndpoint || r.Method != http.MethodPost || r.Host != "" && r.Host != r.URL.Host {
		return false
	}
	key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && key != "" && key == strings.TrimSpace(key) && a.keyHash == Fingerprint(key)
}

func validSessionProviderID(h http.Header, id string) bool {
	if len(h.Values("X-Oneapi-Request-Id")) != 1 || len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// CheckSessionStorage is read-only. Startup installs the binding; a chat must
// never create, adopt, upgrade or repair it to get permission to send.
func (t *Tracker) CheckSessionStorage(ctx context.Context) error {
	t.receiptBindingMu.Lock()
	bound, dbOnly := t.receiptBound, t.receiptDBOnlyBound
	t.receiptBindingMu.Unlock()
	if bound == nil && !dbOnly {
		return ErrReceiptPersistenceUnavailable
	}
	row, found, err := t.Store.loadReceiptBinding(ctx)
	if err != nil {
		return errors.Join(ErrReceiptPersistenceUnavailable, err)
	}
	if !found || row.State != receiptBindingActive {
		return ErrReceiptPersistenceUnavailable
	}
	if dbOnly {
		_, err := hex.DecodeString(row.StoreID)
		if !t.DBOnly || t.ReceiptDir != "" || bound != nil || len(row.StoreID) != 32 || err != nil || row.PathDigest != Fingerprint("yzcm-receipt-db-only") || row.Origin != receiptBindingDBOnly || row.FilesystemIdentity != "" || row.SyncParentLevels != 0 {
			return ErrReceiptPersistenceUnavailable
		}
		return nil
	}
	abs, err := filepath.Abs(t.ReceiptDir)
	if err != nil {
		return ErrReceiptPersistenceUnavailable
	}
	if t.DBOnly || bound.id != row.StoreID || bound.pathDigest != row.PathDigest || bound.pathDigest != receiptPathDigest(abs) {
		return ErrReceiptPersistenceUnavailable
	}
	switch row.Origin {
	case receiptBindingNew, receiptBindingUpgrade, receiptBindingAdopt:
	default:
		return ErrReceiptPersistenceUnavailable
	}
	if !strings.HasPrefix(row.FilesystemIdentity, receiptIdentityV2Prefix) || row.FilesystemIdentity != bound.objects.encode() {
		return ErrReceiptPersistenceUnavailable
	}
	return bound.verifyVisible()
}
