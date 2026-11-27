package yzcmusage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"qiantie/backend/internal/safehttp"
)

type identityKey struct{}
type identity struct{ Owner, Feature, TeamID, ActorUsername string }

// WithAttribution snapshots the authoritative subject, team and actor before
// executing a paid request. Legacy credential selection cannot replace them.
func WithAttribution(ctx context.Context, owner, teamID, actorUsername string) context.Context {
	previous, _ := ctx.Value(identityKey{}).(identity)
	previous.Owner, previous.TeamID, previous.ActorUsername = owner, teamID, actorUsername
	return context.WithValue(ctx, identityKey{}, previous)
}

// AttributionForOwner reads the accepted attribution only for the same subject.
func AttributionForOwner(ctx context.Context, owner string) (teamID, actorUsername string) {
	i, _ := ctx.Value(identityKey{}).(identity)
	if i.Owner == owner {
		return i.TeamID, i.ActorUsername
	}
	return "", ""
}

func WithIdentity(ctx context.Context, owner, feature string) context.Context {
	previous, _ := ctx.Value(identityKey{}).(identity)
	if owner != "" && previous.Owner == "" {
		previous.Owner = owner
	}
	if feature != "" {
		previous.Feature = feature
	}
	return context.WithValue(ctx, identityKey{}, previous)
}
func Fingerprint(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

type Tracker struct {
	ReceiptDir string
	// DBOnly is explicit and exclusive to installations without local receipt claims.
	DBOnly             bool
	Store              Store
	Client             Client
	ResolveCredential  func(context.Context, string, string) (string, error)
	wake               chan struct{}
	once               sync.Once
	credentialMu       sync.Mutex
	credentials        map[string]cachedCredential
	receiptBindingMu   sync.Mutex
	receiptBindingSync func(*os.File) error // Used to inject disk failures during binding tests.
	receiptBound       *receiptBinding
	receiptDBOnlyBound bool
	receiptScanMu      sync.Mutex
	receiptScan        *os.File
	receiptScanInfo    os.FileInfo
	receiptScanNext    os.DirEntry
	receiptProbeMu     sync.Mutex
	receiptProbe       *receiptProbe
	receiptProbeErr    error
	receiptProbeRetry  time.Time
}

type cachedCredential struct {
	key   string
	until time.Time
}

func (t *Tracker) remember(key string) {
	t.credentialMu.Lock()
	defer t.credentialMu.Unlock()
	if t.credentials == nil {
		t.credentials = map[string]cachedCredential{}
	}
	hash := Fingerprint(key)
	if len(t.credentials) >= 256 {
		for name, value := range t.credentials {
			if time.Now().After(value.until) {
				delete(t.credentials, name)
			}
		}
		if len(t.credentials) >= 256 {
			for name := range t.credentials {
				delete(t.credentials, name)
				break
			}
		}
	}
	t.credentials[hash] = cachedCredential{key: key, until: time.Now().Add(15 * time.Minute)}
}
func (t *Tracker) credential(hash string) string {
	t.credentialMu.Lock()
	defer t.credentialMu.Unlock()
	if value, ok := t.credentials[hash]; ok && time.Now().Before(value.until) {
		return value.key
	}
	delete(t.credentials, hash)
	return ""
}

func (t *Tracker) Run(ctx context.Context) {
	t.once.Do(func() {
		t.wake = make(chan struct{}, 1)
		go func() {
			defer t.closeReceiptScan()
			t.flushReceipts(ctx)
			timer := time.NewTicker(20 * time.Second)
			defer timer.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
				case <-t.wake:
				}
				t.flushReceipts(ctx)
				t.reconcile(ctx)
			}
		}()
	})
}
func (t *Tracker) reconcile(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	pending, err := t.Store.Pending(ctx)
	if err != nil {
		log.Print("YZCM usage: pending lookup failed")
		return
	}
	if len(pending) == 0 {
		return
	}
	rates, err := t.Client.Rates(ctx)
	if err != nil {
		return
	}
	groups := map[string][]Pending{}
	for _, p := range pending {
		groups[p.CredentialHash] = append(groups[p.CredentialHash], p)
	}
	for _, g := range groups {
		key := t.credential(g[0].CredentialHash)
		if key == "" && t.ResolveCredential != nil {
			owners := map[string]bool{}
			for _, pending := range g {
				if owners[pending.Owner] {
					continue
				}
				owners[pending.Owner] = true
				if resolved, resolveErr := t.ResolveCredential(ctx, pending.Owner, pending.CredentialHash); resolveErr == nil && resolved != "" {
					key = resolved
					t.remember(key)
					break
				}
			}
		}
		if key == "" {
			continue
		}
		for start := 0; start < len(g); start += 50 {
			end := start + 50
			if end > len(g) {
				end = len(g)
			}
			requests := []Request{}
			for _, p := range g[start:end] {
				requests = append(requests, p.Request)
			}
			logs, err := t.Client.Logs(ctx, key, requests)
			if err != nil {
				continue
			}
			owners := map[string]bool{}
			for _, request := range requests {
				owners[request.Owner] = true
			}
			for owner := range owners {
				entries, err := Match(owner, requests, logs, rates)
				if err != nil {
					continue
				}
				for _, entry := range entries {
					if err := t.Store.Settle(ctx, entry); err != nil {
						log.Print("YZCM usage: settlement persistence failed")
					}
				}
			}
		}
	}
}

type trackedTransport struct {
	session bool
	base    http.RoundTripper
	t       *Tracker
}

func (t *Tracker) Wrap(base http.RoundTripper) http.RoundTripper {
	return &trackedTransport{base: base, t: t}
}
func (t *trackedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	i, _ := r.Context().Value(identityKey{}).(identity)
	a, bound := r.Context().Value(sessionAttemptKey{}).(sessionAttempt)
	if t.session || bound {
		if !t.session || !bound || t.t == nil || !a.matches(r, i) {
			return nil, errors.Join(ErrSessionNotSent, ErrSessionGuard)
		}
	}
	path := strings.TrimRight(r.URL.Path, "/")
	paid := r.Method == http.MethodPost && (path == "/v1/chat/completions" || path == "/v1/responses" || path == "/v1/embeddings" || path == "/v1/rerank" || path == "/v1/completions" || path == "/v1/images/generations" || path == "/v1/images/edits" || path == "/v1/audio/speech" || path == "/v1/audio/transcriptions")
	if r.URL.Hostname() != "newapi.yzcm.site" || !paid || i.Owner == "" {
		return t.base.RoundTrip(r)
	}
	key := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if key == "" {
		if t.session {
			return nil, errors.Join(ErrSessionNotSent, ErrSessionGuard)
		}
		return t.base.RoundTrip(r)
	}
	if t.session {
		if err := t.t.CheckSessionStorage(r.Context()); err != nil {
			return nil, errors.Join(ErrSessionNotSent, err)
		}
	}
	binding, err := t.t.preflightReceipt(r.Context())
	if err != nil {
		if t.session {
			return nil, errors.Join(ErrSessionNotSent, err)
		}
		return nil, errors.Join(safehttp.ErrRequestNotSent, err)
	}
	at := time.Now().UTC()
	if t.session {
		if err := a.claim(r.Context()); err != nil {
			return nil, errors.Join(ErrSessionNotSent, err)
		}
	}
	response, err := t.base.RoundTrip(r)
	if err != nil {
		if t.session {
			return nil, errors.Join(ErrSessionReceiptUnknown, err)
		}
		return response, err
	}
	id := strings.TrimSpace(response.Header.Get("X-Oneapi-Request-Id"))
	if id == "" || len(id) > 64 || t.session && !validSessionProviderID(response.Header, id) {
		if t.session {
			response.Body.Close()
			return nil, ErrSessionReceiptUnknown
		}
		return response, nil
	}
	feature := i.Feature
	if feature == "" {
		feature = "model"
	}
	if strings.Contains(path, "/images/") {
		feature = "image"
	}
	if strings.Contains(path, "/audio/") {
		feature = "audio"
	}
	t.t.remember(key)
	captured := receipt{Request: Request{ID: id, Owner: i.Owner, TeamID: i.TeamID, ActorUsername: i.ActorUsername, Feature: feature}, CredentialHash: Fingerprint(key), Status: response.StatusCode, At: at}
	if t.session {
		captured.AttemptID = a.id
		persist, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 6*time.Second)
		defer cancel()
		if err := t.t.recordBoundReceipt(persist, binding, captured); err != nil {
			response.Body.Close()
			if errors.Is(err, ErrSessionReceiptOnDisk) {
				return nil, ErrSessionReceiptOnDisk
			}
			return nil, errors.Join(ErrSessionReceiptUnknown, err)
		}
		return response, nil
	}
	if err := t.t.recordBoundReceipt(context.Background(), binding, captured); err != nil {
		logReceiptError(id, err)
	}
	// A bounded worker, rather than a goroutine per response, performs lookups.
	return response, nil
}
