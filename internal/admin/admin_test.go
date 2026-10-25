package admin

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/access"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/auth"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/database"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/platformdb"
	"github.com/jackc/pgx/v5/pgconn"
)

var testKey = bytes.Repeat([]byte{7}, 32)

func TestSecretBox(t *testing.T) {
	box, err := newSecretBox(testKey)
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := box.seal([]byte("s3cr3t"))
	if bytes.Contains(enc, []byte("s3cr3t")) {
		t.Fatal("ciphertext tidak boleh memuat plaintext")
	}
	if got, err := box.open(enc); err != nil || string(got) != "s3cr3t" {
		t.Fatalf("round trip gagal: %q %v", got, err)
	}
	enc[len(enc)-1] ^= 1
	if _, err := box.open(enc); err == nil {
		t.Fatal("data yang diubah harus ditolak")
	}
	other, _ := newSecretBox(bytes.Repeat([]byte{9}, 32))
	good, _ := box.seal([]byte("x"))
	if _, err := other.open(good); err == nil {
		t.Fatal("kunci berbeda harus ditolak")
	}
	if _, err := newSecretBox([]byte("pendek")); err == nil {
		t.Fatal("kunci selain 32 byte harus ditolak")
	}
}

func TestBuildDSNEscapesSpecialCharacters(t *testing.T) {
	dsn := BuildDSN("db.example", 5432, "my db", "us@er", "p@ss:w/rd?#", "require")
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pw, _ := u.User.Password()
	if u.Host != "db.example:5432" || u.Path != "/my db" || u.User.Username() != "us@er" || pw != "p@ss:w/rd?#" || u.Query().Get("sslmode") != "require" {
		t.Fatalf("DSN tidak ter-escape dengan benar: %s", dsn)
	}
}

func TestConnectionInputValidation(t *testing.T) {
	ok := ConnectionInput{Name: "x", Host: "h", Port: 5432, Database: "d", Username: "u", Password: "p"}
	if err := ok.normalize(); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(*ConnectionInput){
		"nama kosong":   func(c *ConnectionInput) { c.Name = " " },
		"host aneh":     func(c *ConnectionInput) { c.Host = "a b/c" },
		"port 0":        func(c *ConnectionInput) { c.Port = 0 },
		"port besar":    func(c *ConnectionInput) { c.Port = 70000 },
		"password":      func(c *ConnectionInput) { c.Password = "" },
		"ssl tak valid": func(c *ConnectionInput) { c.SSLMode = "verify-none" },
	} {
		in := ConnectionInput{Name: "x", Host: "h", Port: 5432, Database: "d", Username: "u", Password: "p"}
		mut(&in)
		var ie *InputError
		if err := in.normalize(); !errors.As(err, &ie) {
			t.Errorf("%s: harus InputError, dapat %v", name, err)
		}
	}
}

func TestUserDeleteErrorMapsOnlyMigrationHistoryConstraint(t *testing.T) {
	err := &pgconn.PgError{
		Code:           pgForeignKeyViolation,
		ConstraintName: migrationHistoryUserConstraint,
	}
	if !errors.Is(userDeleteError(err), ErrUserHasHistory) {
		t.Fatal("FK riwayat migration harus dikenali sebagai ErrUserHasHistory")
	}

	otherConstraint := &pgconn.PgError{
		Code:           pgForeignKeyViolation,
		ConstraintName: "some_other_user_fkey",
	}
	if got := userDeleteError(otherConstraint); got != otherConstraint {
		t.Fatal("FK lain harus tetap menjadi error aslinya")
	}

	otherViolation := &pgconn.PgError{
		Code:           pgUniqueViolation,
		ConstraintName: migrationHistoryUserConstraint,
	}
	if got := userDeleteError(otherViolation); got != otherViolation {
		t.Fatal("pelanggaran selain FK harus tetap menjadi error aslinya")
	}
}

// butuh PostgreSQL (DATABASE_URL)
func openPlatform(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	db, err := platformdb.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := platformdb.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	return db, ctx
}

func uniq(prefix string) string {
	return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano()%1_000_000_000)
}

func TestUsersAndAdminRules(t *testing.T) {
	db, ctx := openPlatform(t)
	svc, _ := New(db, nil, nil, testKey)

	name := uniq("u")
	u, err := svc.CreateUser(ctx, name, "password-1234", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id=$1`, u.ID) })

	var hash string
	db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id=$1`, u.ID).Scan(&hash)
	if hash == "password-1234" || !auth.VerifyPassword(hash, "password-1234") {
		t.Fatal("password harus tersimpan sebagai hash yang dapat diverifikasi P1")
	}

	var ie *InputError
	if _, err := svc.CreateUser(ctx, name, "password-1234", false); !errors.As(err, &ie) || !strings.Contains(ie.Message, "sudah dipakai") {
		t.Fatalf("username duplikat harus InputError, dapat %v", err)
	}
	if _, err := svc.CreateUser(ctx, "a b", "password-1234", false); !errors.As(err, &ie) {
		t.Fatal("username tidak valid harus ditolak")
	}
	if _, err := svc.CreateUser(ctx, uniq("u"), "pendek", false); !errors.As(err, &ie) {
		t.Fatal("password pendek harus ditolak")
	}
	if err := svc.DeleteUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetUser(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("user harus hilang setelah dihapus")
	}
}

func TestLastAdminCannotBeDeleted(t *testing.T) {
	db, ctx := openPlatform(t)
	svc, _ := New(db, nil, nil, testKey)

	// Jalur positif: dengan dua admin, salah satunya boleh dihapus.
	a1, err := svc.CreateUser(ctx, uniq("adm"), "password-1234", true)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := svc.CreateUser(ctx, uniq("adm"), "password-1234", true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, a1.ID, a2.ID) })
	if err := svc.DeleteUser(ctx, a1.ID); err != nil {
		t.Fatalf("admin boleh dihapus jika masih ada admin lain: %v", err)
	}

	// Jalur negatif hanya dapat diuji pada database tanpa admin lain.
	if err := svc.DeleteUser(ctx, a2.ID); err == nil {
		var others int
		db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE is_platform_admin`).Scan(&others)
		if others == 0 {
			t.Fatal("admin terakhir tidak boleh terhapus")
		}
	}
}

func TestGroupMembershipAndEffectiveAccessWithP1(t *testing.T) {
	db, ctx := openPlatform(t)
	svc, _ := New(db, nil, nil, testKey)

	u, err := svc.CreateUser(ctx, uniq("m"), "password-1234", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id=$1`, u.ID) })
	g1, _ := svc.CreateGroup(ctx, uniq("g1-"))
	g2, _ := svc.CreateGroup(ctx, uniq("g2-"))
	t.Cleanup(func() { db.Exec(`DELETE FROM app_groups WHERE id IN ($1,$2)`, g1.ID, g2.ID) })

	var dbID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO database_connections (name) VALUES ($1) RETURNING id`, uniq("conn")).Scan(&dbID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM database_connections WHERE id=$1`, dbID) })

	acc := access.Service{Repo: access.PostgresRepository{DB: db}}
	level := func() access.Level { l, _ := acc.EffectiveAccess(ctx, u.ID, dbID); return l }

	if level() != access.NoAccess {
		t.Fatal("tanpa group harus tanpa akses")
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(svc.AddMember(ctx, g1.ID, u.ID))
	must(svc.AddMember(ctx, g1.ID, u.ID))
	must(svc.AddMember(ctx, g2.ID, u.ID))
	must(svc.GrantAccess(ctx, g1.ID, dbID, access.ViewOnly))
	if level() != access.ViewOnly {
		t.Fatal("harus View Only")
	}
	must(svc.GrantAccess(ctx, g2.ID, dbID, access.ViewEdit))
	if level() != access.ViewEdit {
		t.Fatal("View + Edit dari group lain harus menang")
	}
	must(svc.GrantAccess(ctx, g2.ID, dbID, access.ViewOnly)) // upsert menurunkan level
	if level() != access.ViewOnly {
		t.Fatal("upsert harus mengubah level")
	}
	must(svc.RevokeAccess(ctx, g1.ID, dbID))
	must(svc.RemoveMember(ctx, g2.ID, u.ID))
	if level() != access.NoAccess {
		t.Fatalf("setelah akses dicabut dan keanggotaan dihapus harus tanpa akses, dapat %v", level())
	}

	members, err := svc.ListMembers(ctx, g1.ID)
	if err != nil || len(members) != 1 || members[0].ID != u.ID {
		t.Fatalf("ListMembers: %v %v", members, err)
	}
	if err := svc.GrantAccess(ctx, g1.ID, dbID, access.NoAccess); err == nil {
		t.Fatal("level NoAccess harus ditolak")
	}
	if err := svc.GrantAccess(ctx, g1.ID, 999999999, access.ViewOnly); !errors.Is(err, ErrNotFound) {
		t.Fatalf("database tidak ada harus ErrNotFound, dapat %v", err)
	}
	if err := svc.AddMember(ctx, g1.ID, 999999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("user tidak ada harus ErrNotFound, dapat %v", err)
	}
}

func TestConnectionLifecycleWithRegistry(t *testing.T) {
	db, ctx := openPlatform(t)
	target := os.Getenv("TARGET_DATABASE_URL")
	if target == "" {
		t.Skip("TARGET_DATABASE_URL not set")
	}
	tu, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(tu.Port())
	pw, _ := tu.User.Password()
	in := ConnectionInput{
		Name: uniq("target"), Host: tu.Hostname(), Port: port,
		Database: strings.TrimPrefix(tu.Path, "/"), Username: tu.User.Username(), Password: pw, SSLMode: "disable",
	}

	reg := database.NewRegistry()
	t.Cleanup(func() { reg.CloseAll() })
	svc, _ := New(db, reg, nil, testKey)

	c, err := svc.CreateConnection(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM database_connections WHERE id=$1`, c.ID) })
	if !c.Active || !c.Complete {
		t.Fatalf("koneksi baru harus aktif dan lengkap: %+v", c)
	}
	var raw []byte
	db.QueryRowContext(ctx, `SELECT password_encrypted FROM database_connections WHERE id=$1`, c.ID).Scan(&raw)
	if len(raw) == 0 || bytes.Contains(raw, []byte(pw)) {
		t.Fatal("password harus tersimpan terenkripsi")
	}
	if _, err := reg.Get(c.ID); err != nil {
		t.Fatalf("Registry harus berisi koneksi: %v", err)
	}
	if err := svc.TestConnection(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	updatedName := in.Name + "-updated"
	updateInput := in
	updateInput.Name, updateInput.Password = updatedName, ""
	updated, err := svc.UpdateConnection(ctx, c.ID, updateInput)
	if err != nil || updated.Name != updatedName || !updated.Active {
		t.Fatalf("koneksi harus dapat diperbarui sambil mempertahankan password: %#v %v", updated, err)
	}
	badUpdate := updateInput
	badUpdate.Name, badUpdate.Port = in.Name+"-failed", 1
	if _, err := svc.UpdateConnection(ctx, c.ID, badUpdate); !errors.As(err, new(*InputError)) {
		t.Fatalf("update dengan target tidak terjangkau harus ditolak: %v", err)
	}
	still, err := svc.GetConnection(ctx, c.ID)
	if err != nil || still.Name != updatedName {
		t.Fatalf("data lama harus tetap ada setelah koneksi gagal: %#v %v", still, err)
	}

	var ie *InputError
	if _, err := svc.CreateConnection(ctx, updateInput); !errors.As(err, &ie) {
		t.Fatalf("nama duplikat harus InputError, dapat %v", err)
	}

	// Registry baru (simulasi restart): ConnectAll harus mengaktifkan kembali dengan password terdekripsi.
	reg2 := database.NewRegistry()
	t.Cleanup(func() { reg2.CloseAll() })
	svc2, _ := New(db, reg2, nil, testKey)
	n, failures, err := svc2.ConnectAll(ctx)
	if err != nil || n < 1 || len(failures) != 0 {
		t.Fatalf("ConnectAll: n=%d failures=%v err=%v", n, failures, err)
	}
	if _, err := reg2.Get(c.ID); err != nil {
		t.Fatal("koneksi harus aktif setelah ConnectAll")
	}

	// Kunci yang salah tidak boleh membocorkan apa pun dan hanya menggagalkan koneksi itu.
	wrongRegistry := database.NewRegistry()
	t.Cleanup(func() { wrongRegistry.CloseAll() })
	wrong, _ := New(db, wrongRegistry, nil, bytes.Repeat([]byte{1}, 32))
	if err := wrong.TestConnection(ctx, c.ID); err == nil || strings.Contains(err.Error(), pw) {
		t.Fatalf("kunci salah harus gagal tanpa membocorkan password: %v", err)
	}
	// Memasukkan password baru secara eksplisit dapat memperbaiki ciphertext dari kunci lama.
	repairInput := in
	repairInput.Name = updatedName
	if repaired, err := wrong.UpdateConnection(ctx, c.ID, repairInput); err != nil || !repaired.Active {
		t.Fatalf("password baru harus dapat mengenkripsi ulang koneksi: %#v %v", repaired, err)
	}
	if err := wrong.TestConnection(ctx, c.ID); err != nil {
		t.Fatalf("koneksi harus aktif setelah password dimasukkan ulang: %v", err)
	}

	if err := svc.DeleteConnection(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Get(c.ID); !errors.Is(err, database.ErrNotRegistered) {
		t.Fatal("koneksi harus keluar dari Registry setelah dihapus")
	}

	// Target yang tidak dapat dihubungi tidak boleh tersimpan.
	bad := in
	bad.Name, bad.Port = uniq("bad"), 1
	if _, err := svc.CreateConnection(ctx, bad); !errors.As(err, &ie) {
		t.Fatalf("target mati harus InputError, dapat %v", err)
	}
	var n2 int
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM database_connections WHERE name=$1`, bad.Name).Scan(&n2)
	if n2 != 0 {
		t.Fatal("koneksi gagal tidak boleh tersimpan")
	}
}
