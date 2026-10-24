package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/database"
)

// Connection tidak pernah membawa password.
type Connection struct {
	ID       int64
	Name     string
	Host     string
	Port     int
	Database string
	Username string
	SSLMode  string
	// Complete false untuk baris lama yang belum punya detail koneksi
	Complete bool
	// Active true bila koneksi sudah terdaftar di Registry (siap dipakai viewer)
	Active bool
}

type ConnectionInput struct {
	Name     string
	Host     string
	Port     int
	Database string
	Username string
	Password string
	SSLMode  string // disable | prefer | require (kosong = prefer)
}

var hostPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,253}$`)

const connectionColumns = `id, name, COALESCE(host,''), COALESCE(port,0), COALESCE(database_name,''), COALESCE(username,''), ssl_mode,
	(host IS NOT NULL AND port IS NOT NULL AND database_name IS NOT NULL AND username IS NOT NULL AND password_encrypted IS NOT NULL)`

func (s *Service) scanConnection(sc interface{ Scan(...any) error }) (Connection, error) {
	var c Connection
	if err := sc.Scan(&c.ID, &c.Name, &c.Host, &c.Port, &c.Database, &c.Username, &c.SSLMode, &c.Complete); err != nil {
		return c, err
	}
	if s.registry != nil {
		if _, err := s.registry.Get(c.ID); err == nil {
			c.Active = true
		}
	}
	return c, nil
}

func (s *Service) ListConnections(ctx context.Context) ([]Connection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+connectionColumns+` FROM database_connections ORDER BY lower(name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Connection
	for rows.Next() {
		c, err := s.scanConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) GetConnection(ctx context.Context, id int64) (*Connection, error) {
	c, err := s.scanConnection(s.db.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM database_connections WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// BuildDSN menyusun DSN PostgreSQL dengan escaping yang benar untuk setiap bagian.
func BuildDSN(host string, port int, dbName, user, password, sslMode string) string {
	u := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, password),
		Host:     net.JoinHostPort(host, strconv.Itoa(port)),
		Path:     "/" + dbName,
		RawQuery: "sslmode=" + url.QueryEscape(sslMode),
	}
	return u.String()
}

func (in *ConnectionInput) normalize() error {
	in.Name, in.Host = strings.TrimSpace(in.Name), strings.TrimSpace(in.Host)
	in.Database, in.Username = strings.TrimSpace(in.Database), strings.TrimSpace(in.Username)
	if in.SSLMode == "" {
		in.SSLMode = "prefer"
	}
	switch {
	case in.Name == "" || len([]rune(in.Name)) > 64:
		return inputErr("Nama koneksi wajib diisi (maksimal 64 karakter).")
	case !hostPattern.MatchString(in.Host):
		return inputErr("Host tidak valid.")
	case in.Port < 1 || in.Port > 65535:
		return inputErr("Port harus antara 1 dan 65535.")
	case in.Database == "" || len(in.Database) > 63:
		return inputErr("Nama database wajib diisi (maksimal 63 karakter).")
	case in.Username == "" || len(in.Username) > 63:
		return inputErr("Username database wajib diisi (maksimal 63 karakter).")
	case in.Password == "" || len(in.Password) > 1024:
		return inputErr("Password database wajib diisi.")
	case in.SSLMode != "disable" && in.SSLMode != "prefer" && in.SSLMode != "require":
		return inputErr("Mode SSL tidak valid.")
	}
	return nil
}

// CreateConnection menyimpan koneksi (password terenkripsi) dan mengaktifkannya di Registry. Jika target tidak dapat dihubungi, koneksi TIDAK disimpan.
func (s *Service) CreateConnection(ctx context.Context, in ConnectionInput) (*Connection, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	enc, err := s.box.seal([]byte(in.Password))
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRowContext(ctx, `INSERT INTO database_connections (name, host, port, database_name, username, ssl_mode, password_encrypted)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		in.Name, in.Host, in.Port, in.Database, in.Username, in.SSLMode, enc).Scan(&id)
	if pgCode(err) == pgUniqueViolation {
		return nil, inputErr("Nama koneksi sudah dipakai.")
	}
	if err != nil {
		return nil, err
	}

	if s.registry != nil {
		if err := s.registry.Connect(ctx, id, BuildDSN(in.Host, in.Port, in.Database, in.Username, in.Password, in.SSLMode)); err != nil {
			// Pesan error P2 sudah tidak memuat kredensial tetap tidak diteruskan mentah ke UI
			return nil, inputErr("Tidak dapat terhubung ke database target. Periksa host, port, nama database, dan kredensial.")
		}
	}
	if err := tx.Commit(); err != nil {
		if s.registry != nil {
			_ = s.registry.Close(id)
		}
		return nil, err
	}
	return s.GetConnection(ctx, id)
}

func (s *Service) DeleteConnection(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM database_connections WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if s.registry != nil {
		if err := s.registry.Close(id); err != nil && !errors.Is(err, database.ErrNotRegistered) {
			return err
		}
	}
	return nil
}

// TestConnection memeriksa koneksi. Jika belum aktif di Registry (mis. target mati saat aplikasi start), koneksi diaktifkan ketika tes berhasil.
// Error yang dikembalikan adalah database.ErrConnectFailed tanpa detail kredensial.
func (s *Service) TestConnection(ctx context.Context, id int64) error {
	dsn, err := s.dsn(ctx, id)
	if err != nil {
		return err
	}
	if s.registry != nil {
		if db, gerr := s.registry.Get(id); gerr == nil {
			pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if err := db.PingContext(pctx); err != nil {
				return fmt.Errorf("%w: ping", database.ErrConnectFailed)
			}
			return nil
		}
		return s.registry.Connect(ctx, id, dsn)
	}
	db, err := database.OpenTarget(ctx, dsn)
	if err != nil {
		return err
	}
	return db.Close()
}

func (s *Service) dsn(ctx context.Context, id int64) (string, error) {
	var host, dbName, user, ssl sql.NullString
	var port sql.NullInt64
	var enc []byte
	err := s.db.QueryRowContext(ctx, `SELECT host, port, database_name, username, ssl_mode, password_encrypted FROM database_connections WHERE id = $1`, id).
		Scan(&host, &port, &dbName, &user, &ssl, &enc)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if !host.Valid || !port.Valid || !dbName.Valid || !user.Valid || enc == nil {
		return "", inputErr("Detail koneksi belum lengkap. Hapus koneksi ini lalu tambahkan ulang.")
	}
	pw, err := s.box.open(enc)
	if err != nil {
		return "", err
	}
	return BuildDSN(host.String, int(port.Int64), dbName.String, user.String, string(pw), ssl.String), nil
}

// ConnectAll mengaktifkan semua koneksi tersimpan di Registry (dipanggil saat start).
// Kegagalan satu koneksi tidak menghentikan yang lain; hasilnya dikembalikan sebagai peta id -> error.
func (s *Service) ConnectAll(ctx context.Context) (connected int, failures map[int64]error, err error) {
	if s.registry == nil {
		return 0, nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM database_connections WHERE password_encrypted IS NOT NULL ORDER BY id`)
	if err != nil {
		return 0, nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}

	failures = map[int64]error{}
	for _, id := range ids {
		dsn, err := s.dsn(ctx, id)
		if err == nil {
			err = s.registry.Connect(ctx, id, dsn)
		}
		switch {
		case err == nil, errors.Is(err, database.ErrAlreadyRegistered):
			connected++
		default:
			failures[id] = err
		}
	}
	return connected, failures, nil
}