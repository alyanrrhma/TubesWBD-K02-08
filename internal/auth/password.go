package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Passwords use PBKDF2-HMAC-SHA256, 600000 iterations, random 16-byte salt.
// PBKDF2 is implemented here with standard-library primitives so tests run offline.
// Never log a plaintext password or store it outside the hashing call.
const iterations = 600000

func pbkdf2(password, salt []byte, rounds, n int) []byte {
	out := make([]byte, 0, n)
	for block := uint32(1); len(out) < n; block++ {
		v := []byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)}
		m := hmac.New(sha256.New, password)
		m.Write(salt)
		m.Write(v)
		u := m.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < rounds; i++ {
			m = hmac.New(sha256.New, password)
			m.Write(u)
			u = m.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:n]
}
func HashPassword(password string) (string, error) {
	if len(password) < 8 || len(password) > 1024 {
		return "", errors.New("password must contain 8-1024 bytes")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := pbkdf2([]byte(password), salt, iterations, 32)
	return fmt.Sprintf("pbkdf2_sha256$%d$%s$%s", iterations, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}
func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2_sha256" {
		return false
	}
	rounds, err := strconv.Atoi(parts[1])
	if err != nil || rounds < 100000 || rounds > 1000000 {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(parts[2])
	if e != nil || len(salt) != 16 {
		return false
	}
	expected, e := base64.RawStdEncoding.DecodeString(parts[3])
	if e != nil || len(expected) != 32 {
		return false
	}
	actual := pbkdf2([]byte(password), salt, rounds, 32)
	return subtle.ConstantTimeCompare(actual, expected) == 1
}