// Пароль администратора из переменной ADMIN_PASSWORD_HASH (ТЗ 3.7.1).
//
// Зачем: единственный администратор может забыть пароль, а кнопки «забыл
// пароль» в панели нет и быть не может — доступ к панели равен доступу к
// docker.sock. Восстановление — только у того, кто управляет сервисом панели
// в Swarm: он задаёт переменную с bcrypt-хэшем нового пароля и перезапускает
// панель. Сам пароль в переменные и журналы не попадает — только хэш.
package auth

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/xakus/DSMS/panel/internal/store"
)

// Что сделала ApplyAdminHash (для записи в журнал при старте).
const (
	AdminHashSkipped   = "skipped"   // переменная не задана
	AdminHashCreated   = "created"   // пользователей не было — создан admin
	AdminHashUpdated   = "updated"   // пароль администратора заменён
	AdminHashUnchanged = "unchanged" // пароль уже такой
)

const (
	// DefaultAdminUsername — имя администратора, если панель создаёт его
	// сама из ADMIN_PASSWORD_HASH (при первой настройке имя задаёт человек).
	DefaultAdminUsername = "admin"
	// roleAdmin — роль администратора в таблице users.
	roleAdmin = "admin"
	// minHashCost — минимальная стоимость bcrypt в ADMIN_PASSWORD_HASH.
	// Меньше — хэш перебирается слишком быстро; панель сама считает с 12.
	minHashCost = 10
	// MinPasswordLen — минимальная длина пароля, символов (как на setup-экране).
	MinPasswordLen = 8
)

// ErrBadAdminHash — в ADMIN_PASSWORD_HASH не bcrypt-хэш или он слишком слабый.
var ErrBadAdminHash = errors.New("ADMIN_PASSWORD_HASH must be a bcrypt hash with cost 10 or higher")

// adminStore — что нужно от хранилища, чтобы задать пароль администратора.
type adminStore interface {
	CountUsers() (int, error)
	CreateUser(username, passwordHash, role string) (int64, error)
	FirstAdmin() (*store.User, error)
	UpdatePassword(userID int64, passwordHash string) error
}

// ApplyAdminHash задаёт пароль администратора из bcrypt-хэша hash.
//
// Пока переменная задана, она главнее пароля из настроек: хэш применяется
// при каждом старте панели. После восстановления доступа переменную убирают.
//
// @param st   хранилище пользователей
// @param hash значение ADMIN_PASSWORD_HASH; пустое — ничего не делать
// @return что сделано (константы AdminHash*) и имя учётной записи
// Ошибка: ErrBadAdminHash — значение не bcrypt-хэш (сам хэш в ошибку не
// попадает); ошибки хранилища — как есть.
func ApplyAdminHash(st adminStore, hash string) (action, username string, err error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return AdminHashSkipped, "", nil
	}
	if cost, cerr := bcrypt.Cost([]byte(hash)); cerr != nil || cost < minHashCost {
		return "", "", ErrBadAdminHash
	}

	n, err := st.CountUsers()
	if err != nil {
		return "", "", err
	}
	if n == 0 {
		if _, err := st.CreateUser(DefaultAdminUsername, hash, roleAdmin); err != nil {
			return "", "", err
		}
		return AdminHashCreated, DefaultAdminUsername, nil
	}

	admin, err := st.FirstAdmin()
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", errors.New("no admin user to set the password for")
	}
	if err != nil {
		return "", "", err
	}
	if admin.PasswordHash == hash {
		return AdminHashUnchanged, admin.Username, nil
	}
	if err := st.UpdatePassword(admin.ID, hash); err != nil {
		return "", "", err
	}
	return AdminHashUpdated, admin.Username, nil
}

// HashFromReader читает пароль (первую строку) и возвращает его bcrypt-хэш
// для ADMIN_PASSWORD_HASH. Пароль читается из потока, а не из аргументов
// командной строки: аргументы видны в списке процессов и в истории shell.
//
// Ошибка — если пароль короче MinPasswordLen символов.
func HashFromReader(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	password := strings.TrimRight(line, "\r\n")
	if len([]rune(password)) < MinPasswordLen {
		return "", fmt.Errorf("password must be at least %d characters", MinPasswordLen)
	}
	return HashPassword(password)
}
