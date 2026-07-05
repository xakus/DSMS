// Middleware API: сессии и CSRF (FR-07).
package api

import (
	"context"
	"net/http"

	"github.com/xakus/DSMS/panel/internal/auth"
)

// ctxKey — приватный тип ключей контекста запроса.
type ctxKey int

// sessionKey — ключ, под которым сессия лежит в контексте запроса.
const sessionKey ctxKey = iota

// requireSession пускает дальше только запросы с живой сессионной cookie.
func (h *handlers) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(auth.SessionCookie)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		s := h.Sessions.Get(c.Value)
		if s == nil {
			writeErr(w, http.StatusUnauthorized, "session expired")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey, s)))
	})
}

// requireCSRF проверяет заголовок X-CSRF-Token на всех мутирующих методах (3.7.6).
// GET/HEAD/OPTIONS проходят свободно.
func (h *handlers) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		s := sessionFrom(r)
		if s == nil || r.Header.Get("X-CSRF-Token") != s.CSRF {
			writeErr(w, http.StatusForbidden, "csrf token mismatch")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sessionFrom достаёт сессию из контекста запроса (после requireSession).
func sessionFrom(r *http.Request) *auth.Session {
	s, _ := r.Context().Value(sessionKey).(*auth.Session)
	return s
}
