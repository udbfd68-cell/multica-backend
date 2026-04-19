package middleware

import (
	"net/http"
)

// DefaultUserID is the hardcoded user UUID used when auth is disabled.
// It is set at startup by EnsureDefaultUser.
var DefaultUserID = ""

// DefaultUserEmail is the email for the default user.
const DefaultUserEmail = "admin@aurion.studio"

// NoAuth is a middleware that injects a default user into every request,
// bypassing all authentication. Used when auth is fully disabled.
func NoAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set("X-User-ID", DefaultUserID)
			r.Header.Set("X-User-Email", DefaultUserEmail)
			next.ServeHTTP(w, r)
		})
	}
}

// NoAuthDaemon is a pass-through middleware for daemon routes when auth is disabled.
func NoAuthDaemon() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set("X-User-ID", DefaultUserID)
			r.Header.Set("X-User-Email", DefaultUserEmail)
			next.ServeHTTP(w, r)
		})
	}
}
