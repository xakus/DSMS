// Вспомогательный cookie-jar для тестов.
package api

import (
	"net/http"

	"net/http/cookiejar"
)

// newJar создаёт стандартный in-memory cookie-jar.
func newJar() (http.CookieJar, error) {
	return cookiejar.New(nil)
}
