package safehttp

import (
	"errors"
	"net/http"
	"time"
)

// ErrRequestNotSent is only for a refusal before a provider transport was
// entered. A timeout once transmission has started has an unknown outcome.
var ErrRequestNotSent = errors.New("请求尚未发送")

// NewSinglePostTransport gives a caller owning admission and receipt tracking
// the standard dial guard without installing the global managed decorator a
// second time. Its caller owns the endpoint, method and redirect policy.
func NewSinglePostTransport() http.RoundTripper {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	return newTransport(Options{Transport: &http.Transport{Protocols: protocols, DisableKeepAlives: true, TLSHandshakeTimeout: 10 * time.Second}})
}
