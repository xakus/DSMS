// Middleware API: сессии и CSRF (FR-07).
package api

import (
	"context"
	"net/http"
	"strings"

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

// realIP восстанавливает адрес клиента за reverse proxy (панель всегда за ним,
// 3.7.5) — от него зависит rate-limit логина и записи audit.
//
// Берётся ПОСЛЕДНИЙ элемент X-Forwarded-For, а не первый. Первый элемент
// приходит от клиента и подделывается тривиально: с ним атакующий менял бы
// «свой IP» на каждом запросе и обходил лимит попыток входа. Последний
// дописывает наш прокси, подставляя адрес, с которого реально пришёл запрос.
// Заголовка нет — остаётся r.RemoteAddr.
func realIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
				r.RemoteAddr = ip
			}
		}
		next.ServeHTTP(w, r)
	})
}

// securityHeaders — защитные заголовки на все ответы (этап 8, FR-07).
// CSP разрешает только собственные ресурсы: SPA встроена в бинарник,
// внешних CDN нет; ws/wss нужны для live-обновлений.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; "+
				"connect-src 'self' ws: wss:; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}
