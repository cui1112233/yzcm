package httpapi

import "net/http"

func RegisterAccountPurgeCompat(root *http.ServeMux, auth BridgeAuth) {
	admin := accountCenterAdmin{auth: auth}
	root.Handle("POST /api/account-recovery/purge/{username}", auth.AccountCenterMiddleware(admin.accountChange("purge")))
}
