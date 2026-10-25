package config

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/auth"
)

// devSecretKey hanya dipakai bila APP_ENV=development dan APP_SECRET_KEY kosong.
// Jangan dipakai di luar mesin lokal: siapa pun yang membaca repo mengetahuinya.
const devSecretKey = "ZGJtcC1kZXYtb25seS1rZXktbm90LWZvci1wcm9kISE="

type Config struct {
	Env  string // development | production
	Addr string

	// DatabaseURL adalah DSN platform DB (nama variabel sama dengan yang dipakai test P1/P2)
	DatabaseURL string

	// SecretKey (32 byte) mengenkripsi password koneksi database target di platform DB
	SecretKey []byte
	// UsingDevSecretKey true bila SecretKey berasal dari kunci pengembangan bawaan
	UsingDevSecretKey bool

	// Session dipetakan langsung ke auth.Config milik P1
	Session auth.Config

	// Akun Platform Admin pertama; dibuat saat start bila belum ada admin sama sekali
	BootstrapAdminUsername string
	BootstrapAdminPassword string
}

func (c *Config) IsProduction() bool { return c.Env == "production" }

// Load memuat .env (bila ada) lalu membaca environment proses
func Load() (*Config, error) {
	if err := LoadDotEnv(".env"); err != nil {
		return nil, err
	}
	return FromLookup(os.LookupEnv)
}

// FromLookup memudahkan pengujian tanpa menyentuh environment proses
func FromLookup(lookup func(string) (string, bool)) (*Config, error) {
	get := func(key, def string) string {
		if v, ok := lookup(key); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		return def
	}
	var errs []string
	dur := func(key string, def time.Duration) time.Duration {
		d, err := time.ParseDuration(get(key, def.String()))
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Sprintf("%s harus berupa durasi positif (mis. 30m)", key))
			return def
		}
		return d
	}

	cfg := &Config{
		Env:                    get("APP_ENV", "development"),
		Addr:                   get("APP_ADDR", ":8080"),
		DatabaseURL:            get("DATABASE_URL", ""),
		BootstrapAdminUsername: get("BOOTSTRAP_ADMIN_USERNAME", ""),
		BootstrapAdminPassword: get("BOOTSTRAP_ADMIN_PASSWORD", ""),
	}
	if cfg.Env != "development" && cfg.Env != "production" {
		errs = append(errs, "APP_ENV harus development atau production")
	}

	def := auth.DefaultConfig()
	secure := cfg.Env == "production"
	if v := get("SESSION_COOKIE_SECURE", ""); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, "SESSION_COOKIE_SECURE harus true atau false")
		}
		secure = b
	}
	cfg.Session = auth.Config{
		IdleTimeout:      dur("SESSION_IDLE_TIMEOUT", def.IdleTimeout),
		AbsoluteTimeout:  dur("SESSION_ABSOLUTE_TIMEOUT", def.AbsoluteTimeout),
		RotationInterval: dur("SESSION_ROTATION_INTERVAL", def.RotationInterval),
		GracePeriod:      dur("SESSION_GRACE_PERIOD", def.GracePeriod),
		SecureCookies:    secure,
	}
	if err := cfg.Session.Validate(); err != nil {
		errs = append(errs, "konfigurasi session tidak valid: semua durasi harus positif dan SESSION_GRACE_PERIOD harus lebih kecil dari SESSION_ROTATION_INTERVAL")
	}

	switch raw := get("APP_SECRET_KEY", ""); {
	case raw != "":
		key, err := decodeKey(raw)
		if err != nil {
			errs = append(errs, "APP_SECRET_KEY harus 32 byte dalam format base64 atau hex")
		}
		cfg.SecretKey = key
	case cfg.Env == "development":
		cfg.SecretKey, _ = decodeKey(devSecretKey)
		cfg.UsingDevSecretKey = true
	default:
		errs = append(errs, "APP_SECRET_KEY wajib diisi (32 byte, base64 atau hex)")
	}

	if cfg.IsProduction() {
		if cfg.DatabaseURL == "" {
			errs = append(errs, "DATABASE_URL wajib diisi di production")
		}
		if !cfg.Session.SecureCookies {
			errs = append(errs, "SESSION_COOKIE_SECURE wajib true di production")
		}
	}
	if (cfg.BootstrapAdminUsername == "") != (cfg.BootstrapAdminPassword == "") {
		errs = append(errs, "BOOTSTRAP_ADMIN_USERNAME dan BOOTSTRAP_ADMIN_PASSWORD harus diisi berpasangan")
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("konfigurasi tidak valid:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return cfg, nil
}

func decodeKey(s string) ([]byte, error) {
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	return nil, fmt.Errorf("kunci tidak valid")
}

// LoadDotEnv memuat file KEY=VALUE. Variabel yang sudah ada di environment tidak ditimpa. File yang tidak ada bukan error.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
		if _, exists := os.LookupEnv(k); !exists {
			os.Setenv(k, v)
		}
	}
	return sc.Err()
}