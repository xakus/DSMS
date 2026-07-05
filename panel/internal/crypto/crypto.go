// Package crypto — шифрование секретов панели (FR-13, 3.7.9).
//
// AES-256-GCM: пароли реестров хранятся в SQLite только в зашифрованном
// виде. Ключ приходит из Docker secret / env DSMS_ENCRYPTION_KEY
// (hex, 64 символа = 32 байта) и никогда не пишется в БД.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
)

// Box — симметричное шифрование с фиксированным ключом процесса.
type Box struct {
	aead cipher.AEAD
}

// NewBox создаёт Box из hex-ключа (32 байта после декодирования).
func NewBox(hexKey string) (*Box, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("encryption key must be 64 hex chars (32 bytes)")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal шифрует plaintext; nonce кладётся в начало результата.
func (b *Box) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Open расшифровывает данные, созданные Seal.
func (b *Box) Open(data []byte) ([]byte, error) {
	ns := b.aead.NonceSize()
	if len(data) < ns {
		return nil, fmt.Errorf("ciphertext too short")
	}
	return b.aead.Open(nil, data[:ns], data[ns:], nil)
}
