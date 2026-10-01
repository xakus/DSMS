// Тесты аутентификации и сессий (FR-07).
//
// Проверяется поведение, от которого зависит доступ к docker.sock кластера:
// пароли не сравниваются как строки, сессии протухают, токены не повторяются,
// перебор логина упирается в лимит, cookie несёт защитные флаги.
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xakus/DSMS/panel/internal/store"
)

func testUser() *store.User {
	return &store.User{ID: 7, Username: "murad", Role: "admin"}
}

// TestPasswordHashing: хэш не равен паролю, проверка чувствительна к регистру
// и не пропускает пустую строку вместо пароля.
func TestPasswordHashing(t *testing.T) {
	hash, err := HashPassword("correct-horse")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "correct-horse" {
		t.Fatal("пароль сохранён в открытом виде")
	}
	if !CheckPassword(hash, "correct-horse") {
		t.Fatal("верный пароль не принят")
	}
	for _, wrong := range []string{"", "Correct-horse", "correct-horse ", "wrong"} {
		if CheckPassword(hash, wrong) {
			t.Fatalf("принят неверный пароль %q", wrong)
		}
	}
	// Мусор вместо хэша не должен пропускать вход.
	if CheckPassword("not-a-bcrypt-hash", "anything") {
		t.Fatal("некорректный хэш пропустил вход")
	}
}

// TestHashIsSalted: одинаковые пароли дают разные хэши.
func TestHashIsSalted(t *testing.T) {
	a, _ := HashPassword("same")
	b, _ := HashPassword("same")
	if a == b {
		t.Fatal("хэши одинаковы — соль не используется")
	}
}

// TestSessionLifecycle: создание → получение → удаление; токен и CSRF
// уникальны и не совпадают друг с другом.
func TestSessionLifecycle(t *testing.T) {
	m := NewManager(nil, time.Hour)

	s1, err := m.Create(testUser())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if s1.Token == "" || s1.CSRF == "" || s1.Token == s1.CSRF {
		t.Fatalf("токены сессии некорректны: %+v", s1)
	}
	if s1.UserID != 7 || s1.Username != "murad" || s1.Role != "admin" {
		t.Fatalf("сессия не сохранила пользователя: %+v", s1)
	}

	if got := m.Get(s1.Token); got == nil || got.UserID != 7 {
		t.Fatalf("сессия не найдена по токену: %+v", got)
	}
	if m.Get("подобранный-токен") != nil {
		t.Fatal("найдена сессия по несуществующему токену")
	}

	s2, _ := m.Create(testUser())
	if s2.Token == s1.Token || s2.CSRF == s1.CSRF {
		t.Fatal("токены двух сессий совпали")
	}

	m.Destroy(s1.Token)
	if m.Get(s1.Token) != nil {
		t.Fatal("сессия жива после logout")
	}
	if m.Get(s2.Token) == nil {
		t.Fatal("logout убил чужую сессию")
	}
}

// TestSessionExpires: истёкшая сессия не проходит (TTL из 3.7.2).
func TestSessionExpires(t *testing.T) {
	m := NewManager(nil, -time.Second) // уже протухла в момент создания
	s, err := m.Create(testUser())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if m.Get(s.Token) != nil {
		t.Fatal("истёкшая сессия принята")
	}
	// Повторный запрос тоже не должен её воскресить.
	if m.Get(s.Token) != nil {
		t.Fatal("истёкшая сессия принята со второй попытки")
	}
}

// TestLoginRateLimit: 5 неудач с одного IP закрывают вход, другой IP не задет.
func TestLoginRateLimit(t *testing.T) {
	m := NewManager(nil, time.Hour)
	const ip = "10.0.0.5:5555"

	for i := 0; i < loginMaxAttempts; i++ {
		if !m.Allowed(ip) {
			t.Fatalf("вход закрыт раньше времени, попытка %d", i+1)
		}
		m.RecordFailure(ip)
	}
	if m.Allowed(ip) {
		t.Fatalf("после %d неудач вход должен быть закрыт", loginMaxAttempts)
	}
	if !m.Allowed("10.0.0.6:5555") {
		t.Fatal("лимит одного IP закрыл вход другому")
	}
}

// TestRateLimitWindowExpires: старые попытки выпадают из окна и вход
// открывается снова — иначе одна серия ошибок блокировала бы IP навсегда.
func TestRateLimitWindowExpires(t *testing.T) {
	m := NewManager(nil, time.Hour)
	const ip = "10.0.0.7:5555"

	old := time.Now().Add(-loginWindow - time.Minute)
	m.mu.Lock()
	for i := 0; i < loginMaxAttempts; i++ {
		m.attempts[ip] = append(m.attempts[ip], old)
	}
	m.mu.Unlock()

	if !m.Allowed(ip) {
		t.Fatal("попытки старше окна должны забываться")
	}
}

// TestFailureDelayGrows: backoff растёт с числом неудач и упирается в потолок.
func TestFailureDelayGrows(t *testing.T) {
	m := NewManager(nil, time.Hour)
	const ip = "10.0.0.8:5555"

	first := m.FailureDelay(ip)
	m.RecordFailure(ip)
	second := m.FailureDelay(ip)
	if second <= first {
		t.Fatalf("задержка не растёт: %v → %v", first, second)
	}
	for i := 0; i < 20; i++ {
		m.RecordFailure(ip)
	}
	if d := m.FailureDelay(ip); d != 3*time.Second {
		t.Fatalf("потолок задержки должен быть 3с, получено %v", d)
	}
}

// TestCookieFlags: HttpOnly + SameSite=Strict всегда, Secure — по настройке
// (в dev по HTTP браузер Secure-cookie не примет).
func TestCookieFlags(t *testing.T) {
	m := NewManager(nil, time.Hour)
	s, _ := m.Create(testUser())

	for _, secure := range []bool{true, false} {
		rec := httptest.NewRecorder()
		SetCookie(rec, s, secure)

		cookies := rec.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("ожидалась одна cookie, получено %d", len(cookies))
		}
		c := cookies[0]
		if c.Name != SessionCookie || c.Value != s.Token {
			t.Fatalf("cookie неверна: %+v", c)
		}
		if !c.HttpOnly {
			t.Fatal("cookie без HttpOnly — доступна из JS")
		}
		if c.SameSite != http.SameSiteStrictMode {
			t.Fatalf("SameSite должен быть Strict, получено %v", c.SameSite)
		}
		if c.Secure != secure {
			t.Fatalf("Secure=%v при secure=%v", c.Secure, secure)
		}
	}
}

// TestClearCookie: logout гасит cookie (MaxAge < 0), сохраняя защитные флаги.
func TestClearCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	ClearCookie(rec, true)

	c := rec.Result().Cookies()[0]
	if c.Value != "" || c.MaxAge >= 0 {
		t.Fatalf("cookie не сброшена: %+v", c)
	}
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
		t.Fatalf("сброшенная cookie потеряла флаги: %+v", c)
	}
}
