// Тесты шифрования секретов (FR-13, 3.7.9).
//
// Цена ошибки здесь максимальная: этим ключом зашифрованы пароли реестров
// и env managed-стеков. Тихая порча = невосстановимая потеря, тихое
// «расшифровалось не тем ключом» = утечка. Поэтому проверяем не только
// happy path, но и все способы получить мусор на вход.
package crypto

import (
	"bytes"
	"strings"
	"testing"
)

// testKey — 32 байта в hex (64 символа).
const testKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func newBox(t *testing.T, key string) *Box {
	t.Helper()
	b, err := NewBox(key)
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}
	return b
}

// TestSealOpenRoundtrip: что зашифровали — то и расшифровали.
func TestSealOpenRoundtrip(t *testing.T) {
	b := newBox(t, testKey)

	cases := map[string][]byte{
		"обычный пароль":    []byte("s3cret-p@ssw0rd"),
		"пустое значение":   {},
		"юникод":            []byte("пароль-с-кириллицей-🔐"),
		"многострочный env": []byte("JWT_SECRET=abc\nPOSTGRES_PASSWORD=def\n"),
		"бинарные данные":   {0x00, 0xff, 0x10, 0x00, 0x7f},
	}
	for name, plain := range cases {
		t.Run(name, func(t *testing.T) {
			sealed, err := b.Seal(plain)
			if err != nil {
				t.Fatalf("Seal: %v", err)
			}
			// Шифртекст не должен содержать открытый текст.
			if len(plain) > 0 && bytes.Contains(sealed, plain) {
				t.Fatal("открытый текст виден в шифртексте")
			}
			opened, err := b.Open(sealed)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if !bytes.Equal(opened, plain) {
				t.Fatalf("данные исказились: %q != %q", opened, plain)
			}
		})
	}
}

// TestSealIsRandomized: два шифрования одного значения дают разный результат
// (иначе по БД видно, у каких реестров одинаковые пароли).
func TestSealIsRandomized(t *testing.T) {
	b := newBox(t, testKey)
	plain := []byte("same-password")

	first, err := b.Seal(plain)
	if err != nil {
		t.Fatalf("Seal #1: %v", err)
	}
	second, err := b.Seal(plain)
	if err != nil {
		t.Fatalf("Seal #2: %v", err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("одинаковый шифртекст при повторном шифровании — nonce не случаен")
	}
}

// TestOpenRejectsTampering: порча любого байта ломает расшифровку (GCM
// аутентифицирует данные — подмена значения в БД не пройдёт незаметно).
func TestOpenRejectsTampering(t *testing.T) {
	b := newBox(t, testKey)
	sealed, err := b.Seal([]byte("secret"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	for _, pos := range []int{0, len(sealed) / 2, len(sealed) - 1} {
		damaged := bytes.Clone(sealed)
		damaged[pos] ^= 0xff
		if _, err := b.Open(damaged); err == nil {
			t.Fatalf("порча байта %d осталась незамеченной", pos)
		}
	}
}

// TestOpenRejectsShortInput: обрезанные данные не должны ронять панель паникой.
func TestOpenRejectsShortInput(t *testing.T) {
	b := newBox(t, testKey)
	for _, data := range [][]byte{nil, {}, {0x01}, make([]byte, 11)} {
		if _, err := b.Open(data); err == nil {
			t.Fatalf("короткий вход (%d байт) принят как валидный", len(data))
		}
	}
}

// TestOpenRejectsWrongKey: другой ключ не расшифровывает — иначе смена
// DSMS_ENCRYPTION_KEY молча отдавала бы мусор вместо пароля.
func TestOpenRejectsWrongKey(t *testing.T) {
	sealed, err := newBox(t, testKey).Seal([]byte("secret"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	other := newBox(t, strings.Repeat("ab", 32))
	if _, err := other.Open(sealed); err == nil {
		t.Fatal("данные расшифровались чужим ключом")
	}
}

// TestNewBoxValidatesKey: некорректный ключ — ошибка при старте, а не при
// первой попытке сохранить пароль.
func TestNewBoxValidatesKey(t *testing.T) {
	bad := map[string]string{
		"пустой":         "",
		"короткий":       "abcd",
		"не hex":         strings.Repeat("zz", 32),
		"31 байт":        strings.Repeat("ab", 31),
		"33 байта":       strings.Repeat("ab", 33),
		"нечётная длина": strings.Repeat("a", 63),
	}
	for name, key := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := NewBox(key); err == nil {
				t.Fatalf("ключ %q принят", key)
			}
		})
	}
	if _, err := NewBox(strings.ToUpper(testKey)); err != nil {
		t.Fatalf("hex в верхнем регистре должен приниматься: %v", err)
	}
}
