// Package auth — аутентификация и сессии PANEL (FR-07).
//
// Сессии живут в памяти (рестарт panel = повторный вход — осознанное решение v1,
// панель не хранит state, критичный для кластера, NFR-6).
// Пароли — bcrypt cost 12 (3.7.2). Rate-limit на login — 5 попыток / 15 мин с IP (3.7.3).
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/xakus/DSMS/panel/internal/store"
)

// BcryptCost — стоимость bcrypt из 3.7.2.
const BcryptCost = 12

// SessionCookie — имя сессионной cookie.
const SessionCookie = "dsms_session"

// Ограничения rate-limit для /login (3.7.3).
const (
	loginMaxAttempts = 5
	loginWindow      = 15 * time.Minute
)

// Session — активная сессия пользователя.
// UserID (а не имя) — задел под RBAC (3.7.8).
type Session struct {
	Token     string    // случайный идентификатор в cookie
	UserID    int64     // владелец сессии
	Username  string    // для отображения и audit-деталей
	Role      string    // в v1 всегда 'admin'
	CSRF      string    // токен для мутирующих запросов (3.7.6)
	ExpiresAt time.Time // абсолютный TTL (24 ч)
}

// Manager — in-memory менеджер сессий + rate-limit login-попыток.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session   // token → session
	attempts map[string][]time.Time // ip → времена неудачных попыток login
	store    *store.Store
	ttl      time.Duration
}

// NewManager создаёт менеджер сессий с заданным TTL.
func NewManager(st *store.Store, ttl time.Duration) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		attempts: make(map[string][]time.Time),
		store:    st,
		ttl:      ttl,
	}
}

// CheckPassword сравнивает пароль с bcrypt-хэшем.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// HashPassword хэширует пароль с cost 12.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	return string(b), err
}

// Allowed проверяет rate-limit для IP: false — лимит попыток исчерпан.
func (m *Manager) Allowed(ip string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	fresh := m.attempts[ip][:0]
	for _, t := range m.attempts[ip] {
		if now.Sub(t) < loginWindow {
			fresh = append(fresh, t)
		}
	}
	m.attempts[ip] = fresh
	return len(fresh) < loginMaxAttempts
}

// RecordFailure фиксирует неудачную попытку входа с IP.
func (m *Manager) RecordFailure(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attempts[ip] = append(m.attempts[ip], time.Now())
}

// Create создаёт сессию для пользователя и возвращает её.
func (m *Manager) Create(u *store.User) (*Session, error) {
	token, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	csrf, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	s := &Session{
		Token:     token,
		UserID:    u.ID,
		Username:  u.Username,
		Role:      u.Role,
		CSRF:      csrf,
		ExpiresAt: time.Now().Add(m.ttl),
	}
	m.mu.Lock()
	m.sessions[token] = s
	m.mu.Unlock()
	return s, nil
}

// Get возвращает живую сессию по токену; nil — нет или истекла.
func (m *Manager) Get(token string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[token]
	if !ok {
		return nil
	}
	if time.Now().After(s.ExpiresAt) {
		delete(m.sessions, token)
		return nil
	}
	return s
}

// Destroy удаляет сессию (logout).
func (m *Manager) Destroy(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, token)
}

// SetCookie ставит сессионную cookie с флагами из 3.7.2:
// HttpOnly, Secure, SameSite=Strict. secure=false — только локальная
// разработка без TLS (DSMS_COOKIE_INSECURE=1).
func SetCookie(w http.ResponseWriter, s *Session, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    s.Token,
		Path:     "/",
		Expires:  s.ExpiresAt,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// ClearCookie сбрасывает сессионную cookie.
func ClearCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// randomHex возвращает криптослучайную hex-строку длиной 2*n символов.
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
