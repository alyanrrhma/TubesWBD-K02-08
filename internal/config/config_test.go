package config

import (
	"strings"
	"testing"
	"time"
)

func lookup(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

const validKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" // 32 byte (hex)

func TestDevelopmentDefaults(t *testing.T) {
	cfg, err := FromLookup(lookup(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8080" || cfg.Session.SecureCookies || cfg.Session.IdleTimeout != 30*time.Minute {
		t.Fatalf("default development tidak sesuai: %+v", cfg)
	}
	if !cfg.UsingDevSecretKey || len(cfg.SecretKey) != 32 {
		t.Fatal("development tanpa APP_SECRET_KEY harus memakai kunci dev 32 byte")
	}
}

func TestProductionRequirements(t *testing.T) {
	_, err := FromLookup(lookup(map[string]string{"APP_ENV": "production"}))
	if err == nil {
		t.Fatal("production tanpa konfigurasi wajib harus ditolak")
	}
	for _, want := range []string{"APP_SECRET_KEY", "DATABASE_URL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error harus menyebut %s: %v", want, err)
		}
	}
	cfg, err := FromLookup(lookup(map[string]string{"APP_ENV": "production", "APP_SECRET_KEY": validKey, "DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Session.SecureCookies {
		t.Fatal("production harus memakai cookie Secure secara default")
	}
	if _, err := FromLookup(lookup(map[string]string{"APP_ENV": "production", "APP_SECRET_KEY": validKey, "DATABASE_URL": "x", "SESSION_COOKIE_SECURE": "false"})); err == nil {
		t.Fatal("production dengan cookie tidak Secure harus ditolak")
	}
}

func TestInvalidValues(t *testing.T) {
	_, err := FromLookup(lookup(map[string]string{
		"SESSION_IDLE_TIMEOUT": "bukan-durasi",
		"APP_SECRET_KEY":       "terlalu-pendek",
	}))
	if err == nil || !strings.Contains(err.Error(), "SESSION_IDLE_TIMEOUT") || !strings.Contains(err.Error(), "APP_SECRET_KEY") {
		t.Fatalf("error harus menyebut kedua variabel, dapat: %v", err)
	}
}

func TestGraceMustBeShorterThanRotation(t *testing.T) {
	_, err := FromLookup(lookup(map[string]string{"SESSION_GRACE_PERIOD": "20m"}))
	if err == nil {
		t.Fatal("grace period >= rotation interval harus ditolak (aturan auth.Config.Validate)")
	}
}

func TestBootstrapAdminPair(t *testing.T) {
	if _, err := FromLookup(lookup(map[string]string{"BOOTSTRAP_ADMIN_USERNAME": "admin"})); err == nil {
		t.Fatal("username tanpa password harus ditolak")
	}
}