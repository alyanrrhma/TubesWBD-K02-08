package admin

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// secretBox mengenkripsi password koneksi target dengan AES-256-GCM.
// Format: nonce (12 byte) || ciphertext+tag.
type secretBox struct{ aead cipher.AEAD }

var secretAAD = []byte("dbmp/database-connection/v1")

func newSecretBox(key []byte) (*secretBox, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("admin: kunci enkripsi harus 32 byte, dapat %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &secretBox{aead: aead}, nil
}

func (b *secretBox) seal(plain []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, plain, secretAAD), nil
}

func (b *secretBox) open(data []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(data) < n+b.aead.Overhead() {
		return nil, errors.New("admin: data terenkripsi tidak valid")
	}
	plain, err := b.aead.Open(nil, data[:n], data[n:], secretAAD)
	if err != nil {
		return nil, errors.New("admin: gagal mendekripsi (kunci salah atau data rusak)")
	}
	return plain, nil
}