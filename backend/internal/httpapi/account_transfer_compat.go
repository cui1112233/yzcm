package httpapi

import "net/http"

// The old transfer contract also rewrote JSON API permissions and quotas.
// It remains closed until the backing stores can update those in the same operation.
func RegisterAccountTransferCompat(root *http.ServeMux, middleware BridgeAuth) {
	admin := accountCenterAdmin{auth: middleware}
	for _, pattern := range []string{
		"GET /api/account-admin/accounts/{username}/transfer-preview",
		"POST /api/account-admin/accounts/{username}/transfer",
	} {
		root.Handle(pattern, middleware.AccountCenterMiddleware(http.HandlerFunc(admin.retired)))
	}
}
