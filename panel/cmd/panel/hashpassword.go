// Режим «выдать bcrypt-хэш пароля» для переменной ADMIN_PASSWORD_HASH
// (восстановление доступа, ТЗ 3.7.1).
//
// Образ собран на scratch: htpasswd и shell в нём нет, поэтому хэш считает
// сам бинарник:
//
//	docker run --rm -i ghcr.io/xakus/dsms-panel:<версия> -hash-password
//
// Пароль вводится с клавиатуры (stdin), а не аргументом: аргументы видны в
// списке процессов и остаются в истории shell.
package main

import (
	"fmt"
	"os"

	"github.com/xakus/DSMS/panel/internal/auth"
)

// hashPasswordFlag — аргумент, включающий режим выдачи хэша.
const hashPasswordFlag = "-hash-password"

// isHashPasswordMode — вызван ли бинарник для выдачи хэша.
func isHashPasswordMode(args []string) bool {
	for _, a := range args[1:] {
		if a == hashPasswordFlag || a == "--hash-password" {
			return true
		}
	}
	return false
}

// runHashPassword читает пароль из stdin, печатает хэш в stdout и
// возвращает код выхода: 0 — успех, 1 — пароль не подошёл по длине.
func runHashPassword() int {
	fmt.Fprintf(os.Stderr, "Новый пароль (не короче %d символов), затем Enter: ", auth.MinPasswordLen)
	hash, err := auth.HashFromReader(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nhash-password:", err)
		return 1
	}
	fmt.Fprintln(os.Stderr)
	fmt.Println(hash)
	return 0
}
