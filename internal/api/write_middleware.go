package api

import "net/http"

// requireWrite is a middleware placeholder for future RBAC on write operations.
// Today it is a pass-through: the auth middleware upstream already rejects
// unauthenticated requests before handlers run. This middleware will check
// identity.Groups against allowed roles once authorization is implemented.
func requireWrite(next http.HandlerFunc) http.Handler {
	return next
}
