# TubesWBD-K02-08
# TubesWBD-K02-08 — Database Management Platform (DBMP)

> Tugas Besar **IF3110 Pengembangan Aplikasi Web 2026 — Milestone 1** (kelompok K02-08).
> Fondasi Database Management Platform: pengaturan akses, penjelajahan database, dan pencatatan perubahan schema secara terkontrol.

Aplikasi web untuk mengelola dan menjelajahi banyak database **PostgreSQL** dari satu tempat. Platform Admin mendaftarkan koneksi database target, mengelompokkan pengguna ke dalam *group*, lalu memberi group tersebut akses **View Only** atau **View + Edit** ke database tertentu. Pengguna biasa dapat menjelajahi schema, tabel, struktur, dan isi data, serta (bila punya akses *View + Edit*) menjalankan **migration SQL** yang tercatat riwayatnya.

Dibangun dengan Go (standard library `net/http`), server-side rendering memakai `html/template`, dan driver `pgx`. Seluruh template dan file statis ditanam ke dalam binary lewat `go:embed`.

---

## Daftar Isi

- [Fitur](#fitur)
- [Alur Minimum Sistem](#alur-minimum-sistem)
- [Keputusan Desain](#keputusan-desain)
- [Pemenuhan Spesifikasi Milestone 1](#pemenuhan-spesifikasi-milestone-1)
- [Teknologi](#teknologi)
- [Arsitektur](#arsitektur)
- [Struktur Direktori](#struktur-direktori)
- [Model Akses](#model-akses)
- [Keamanan](#keamanan)
- [Skema Database Platform](#skema-database-platform)
- [Daftar Route](#daftar-route)
- [Konfigurasi](#konfigurasi)
- [Menjalankan Aplikasi](#menjalankan-aplikasi)
- [Menjalankan dengan Docker](#menjalankan-dengan-docker)
- [Pengujian](#pengujian)
- [Catatan Perilaku](#catatan-perilaku)

---

## Fitur

**Autentikasi & sesi**
- Login berbasis username dan password; password di-hash dengan PBKDF2-HMAC-SHA256.
- Sesi server-side di PostgreSQL dengan *idle timeout*, *absolute timeout*, rotasi token berkala, dan *grace period*.
- Proteksi CSRF pada semua request yang mengubah state, ditambah pemeriksaan header `Origin`.
- Login baru mencabut sesi lama; penghapusan pengguna mencabut seluruh sesinya.

**Panel Platform Admin**
- **Pengguna**: tambah dan hapus pengguna, termasuk menjadikannya Platform Admin. Platform Admin terakhir tidak dapat dihapus, dan admin tidak dapat menghapus akunnya sendiri.
- **Group**: buat dan hapus group, kelola anggota, dan atur level akses group ke setiap database.
- **Koneksi**: daftarkan database target (host, port, nama database, username, password, mode SSL), uji koneksi, dan hapus koneksi. Password disimpan terenkripsi (AES-256-GCM) dan tidak pernah ditampilkan kembali.

**Database viewer**
- Dashboard berisi daftar database yang dapat diakses pengguna beserta level aksesnya.
- Menjelajahi schema dan tabel pada database target.
- Halaman struktur tabel: kolom, constraint, dan index.
- Halaman data tabel dengan pagination (50 baris per halaman), penanda `NULL`, dan pembacaan lewat transaksi *read-only*.

**Migration**
- Pengguna dengan akses *View + Edit* dapat menjalankan SQL pada database target dalam satu transaksi: *commit* bila berhasil, *rollback* bila gagal.
- Setiap eksekusi (berhasil maupun gagal) tercatat sebagai versi berurutan per database: pelaksana, waktu, status, SQL, dan pesan error.
- Pengguna dengan akses *View Only* dapat melihat riwayat dan detail migration, tetapi tidak dapat menjalankannya.

---

## Alur Minimum Sistem

Spesifikasi menetapkan alur minimum: *Login → Access Control → Select Database → View Schema/Data → Create Migration → Execute → Commit/Rollback → Update Migration History*. Pemetaannya pada aplikasi:

| Tahap | Implementasi |
| --- | --- |
| Login | `POST /login`: verifikasi password, cabut sesi lama, buat sesi baru dan cookie `dbmp_session` |
| Access Control | `requireAuth` (sesi), `requireAdmin` (area admin), `csrfProtect` (request pengubah state), dan `access.Service` (level akses efektif per database) |
| Select Database | `/dashboard` hanya menampilkan database yang punya akses efektif ≥ View Only |
| View Schema/Data | `/databases/{id}` (schema dan tabel), `.../tables/{schema}/{table}` (kolom, constraint, index), `.../data` (isi tabel) |
| Create Migration | Editor SQL di `/databases/{id}/migrations`, hanya tampil untuk akses View + Edit |
| Execute | `POST /databases/{id}/migrations` (otorisasi View + Edit diperiksa di backend sebelum SQL divalidasi) |
| Commit/Rollback | `applyInTx`: `COMMIT` bila seluruh SQL berhasil, `ROLLBACK` bila gagal |
| Update Migration History | Baris baru pada `migration_history` (status `SUCCESS` atau `FAILED`) dengan nomor versi berikutnya |

---

## Keputusan Desain

Spesifikasi meminta tim menentukan dan mendokumentasikan beberapa hal. Keputusan yang dipakai:

**Akses efektif bila pengguna berada di beberapa group.**
Untuk satu database, akses efektif adalah **level tertinggi** dari semua group pengguna yang punya akses ke database itu (`VIEW_EDIT` mengalahkan `VIEW_ONLY`). Group tanpa entri akses ke database tersebut tidak memengaruhi hasil. Implementasi: `access.Service.EffectiveAccess`.

**Platform Admin bukan level akses database.**
Flag Platform Admin hanya membuka halaman administrasi (`/admin/*`). Akses ke sebuah database tetap hanya datang dari keanggotaan group, termasuk untuk akun admin.

**Parameter sesi** (dapat diubah lewat environment, lihat [Konfigurasi](#konfigurasi)):

| Parameter | Nilai default | Arti |
| --- | --- | --- |
| Idle timeout | 30 menit | Sesi berakhir bila tidak dipakai selama itu |
| Absolute expiration | 8 jam | Umur maksimum sesi sejak login; **tidak diperpanjang** oleh aktivitas maupun rotasi |
| Rotation interval | 15 menit | Nilai cookie `dbmp_session` diganti secara berkala (nama cookie tetap) |
| Grace period | 30 detik | Identifier lama masih diterima sebentar setelah rotasi untuk menangani response yang tidak sampai atau request paralel |

Konfigurasi dinyatakan tidak valid bila grace period tidak lebih kecil dari rotation interval.

**Penghitungan umur cookie.** Cookie sesi selalu memakai waktu kedaluwarsa absolut yang sama, sehingga rotasi hanya mengganti nilai token.

**Hashing password.** PBKDF2-HMAC-SHA256, 600.000 iterasi, salt acak 16 byte.

**Penyimpanan sesi.** Sesi disimpan di PostgreSQL (hanya hash SHA-256 token); client hanya memegang identifier acak di cookie `HttpOnly`, `SameSite=Lax`, dan `Secure` pada production.

---

## Pemenuhan Spesifikasi Milestone 1

### Ketentuan umum

| Ketentuan | Status | Keterangan |
| --- | :---: | --- |
| Backend menggunakan Go | ✔ | Go 1.25, `net/http` |
| Frontend bukan SPA (Go template) | ✔ | `html/template` dengan server-side rendering |
| Database target PostgreSQL | ✔ | Driver `pgx/v5` |
| Unit test dan integration test | ✔ | Lihat [Pengujian](#pengujian) |

### Authentication, Authorization, dan Session

| Kebutuhan | Implementasi | Lokasi |
| --- | --- | --- |
| Login dengan username dan password | Form login, verifikasi hash | `internal/web/auth_pages.go`, `internal/auth/login.go` |
| Password disimpan dengan hashing yang sesuai | PBKDF2-HMAC-SHA256 + salt acak | `internal/auth/password.go` |
| Password dan session identifier tidak muncul di URL, response, maupun log | Session identifier hanya lewat cookie `HttpOnly`; log request hanya memuat method, path, status, dan durasi; password koneksi tidak pernah dirender kembali | `internal/auth/session.go`, `internal/web/middleware.go`, `internal/web/render.go` |
| Setiap tindakan diperiksa di backend | Level akses diperiksa di service (`DatabaseViewer`, `migration.Service`) pada setiap pemanggilan, bukan hanya disembunyikan di UI | `internal/web/viewer_service.go`, `internal/migration/` |
| Pengaturan hak akses dipisah dari identifikasi pengguna | Paket khusus untuk level akses dan perhitungan akses efektif | `internal/access/` |
| Session identifier opaque, acak, tersimpan di backend | Token acak 32 byte; yang disimpan hanya hash-nya | `internal/auth/session.go`, `internal/auth/postgres.go` |
| Cookie `HttpOnly`, `SameSite`, `Secure` (HTTPS/production) | `HttpOnly`, `SameSite=Lax`, `Secure` mengikuti `SESSION_COOKIE_SECURE` (wajib `true` di production) | `internal/auth/session.go`, `internal/config/config.go` |
| Idle timeout dan absolute expiration | Diperiksa setiap request terlindungi | `Manager.Validate` |
| Rotasi identifier: saat login, saat privilege naik, dan berkala | Login selalu membuat identifier baru dan mencabut sesi lama; rotasi berkala di `Validate`; `RotatePrivilege` tersedia dan diuji (lihat catatan di bawah) | `internal/auth/session.go` |
| Identifier lama berlaku selama grace period | Hash token sebelumnya disimpan dengan batas waktu grace | `internal/auth/session.go`, `internal/auth/postgres.go` |
| Rotasi tidak memperpanjang absolute expiration | `expires_at` tidak diubah saat rotasi | `Manager.Validate` |
| Logout dan pencabutan akses menonaktifkan sesi | Logout mencabut satu sesi; penghapusan pengguna mencabut seluruh sesinya; perubahan akses group berlaku pada request berikutnya karena akses dihitung ulang setiap request | `Manager.Logout`, `Manager.RevokeUser`, `internal/admin/users.go` |
| Sesi melewati absolute expiration mengharuskan login ulang | Sesi dicabut dan pengguna diarahkan ke `/login` | `Manager.Validate`, `requireAuth` |
| Perlindungan CSRF pada request pengubah state | Token CSRF per sesi dan pemeriksaan `Origin` untuk semua method selain GET/HEAD/OPTIONS | `internal/web/middleware.go`, `internal/auth/middleware.go` |

> **Catatan privilege elevation.** Peran admin ditetapkan saat akun dibuat dan dibawa ke sesi saat login; belum ada fitur yang menaikkan privilege pada sesi yang sedang berjalan. Karena itu `RotatePrivilege` belum dipanggil dari alur web, tetapi sudah disediakan dan diuji (`TestRegressionPrivilegeRotation`) untuk dipakai bila fitur seperti itu ditambahkan.

### Platform Admin, User, Group, dan Akses Database

| Kebutuhan | Implementasi |
| --- | --- |
| Platform Admin mengelola user, group, koneksi, dan pemberian akses | Menu **Pengguna**, **Group**, dan **Koneksi** (`/admin/*`), hanya untuk Platform Admin |
| User dapat menjadi anggota satu atau lebih group | Tabel `user_groups` (relasi many-to-many) |
| Akses database diberikan lewat group, bukan langsung ke user | Tabel `group_database_access` |
| Level View Only dan View + Edit per database | Kolom `access_level` (`VIEW_ONLY`/`VIEW_EDIT`) dengan `CHECK` constraint |
| Satu group dapat punya level berbeda pada tiap database | Kunci utama `(group_id, database_id)` |
| Cara menghitung akses efektif didokumentasikan | Lihat [Keputusan Desain](#keputusan-desain) |

### Database Management

| Kebutuhan | Implementasi | Lokasi |
| --- | --- | --- |
| Pengelolaan koneksi ke satu atau lebih database PostgreSQL oleh Platform Admin | Tambah, tes, dan hapus koneksi; password terenkripsi AES-256-GCM; pool aktif disimpan di `Registry` | `internal/admin/connections.go`, `internal/database/registry.go` |
| Database viewer: schema, table, column, constraint, index, dan data | Halaman schema/tabel, struktur tabel, dan data berpagination | `internal/database/viewer.go`, `internal/web/viewer_pages.go` |
| Penanganan kegagalan koneksi dan akses tidak sah | Error dipetakan ke halaman aman: `403` akses ditolak, `503` koneksi belum aktif, `502` koneksi/izin database target gagal, `404` data tidak ditemukan; detail internal hanya masuk log | `internal/web/helpers.go` |

### Migration

| Kebutuhan | Implementasi |
| --- | --- |
| Migration query viewer/editor | Editor SQL pada halaman Migration; SQL yang gagal dipertahankan di editor agar dapat diperbaiki |
| Buat dan jalankan migration hanya oleh View + Edit | Otorisasi `CanEdit` diperiksa di backend sebelum validasi SQL; View Only menerima `403` bila memanggil endpoint langsung |
| View Only hanya melihat riwayat dan detail | Halaman riwayat dan detail terbuka untuk View Only; editor tidak ditampilkan |
| Riwayat migration linear per database | Nomor versi berurutan per database (`MAX(version) + 1`), unik pada `(database_id, version)`, dan dijalankan satu per satu per database |
| Pencatatan versi, SQL, pelaksana, waktu, dan status | Tabel `migration_history` |
| Transaction: commit bila berhasil, rollback bila gagal | `applyInTx` pada database target |
| Tampilan riwayat perubahan | Daftar riwayat berpagination dan halaman detail per versi |

### Fitur bonus

Schema diff menghasilkan kandidat SQL yang dapat ditinjau sebelum dijalankan. Analisis query menyediakan EXPLAIN estimasi dan EXPLAIN ANALYZE dengan waktu serta row aktual; mode ANALYZE menjalankan statement dalam transaksi yang selalu di-rollback, dengan batas waktu 15 detik. Efek non-transaksional seperti sequence atau efek eksternal dari fungsi mungkin tetap terjadi. Dukungan DBMS selain PostgreSQL belum diimplementasikan.

---

## Teknologi

| Komponen | Keterangan |
| --- | --- |
| Bahasa | Go 1.25 |
| HTTP | `net/http` (`ServeMux` dengan pola method dan wildcard) |
| Template | `html/template`, `embed` |
| Driver database | [`jackc/pgx/v5`](https://github.com/jackc/pgx) (via `database/sql`) |
| Konfigurasi | Environment variable, `.env` (loader bawaan; `joho/godotenv` untuk test) |
| Database | PostgreSQL 16 (Docker Compose) |
| Frontend | HTML + CSS + sedikit JavaScript tanpa framework dan tanpa inline script/style |

---

## Arsitektur

Aplikasi memakai dua jenis database PostgreSQL:

1. **Platform DB** (`DATABASE_URL`): database internal aplikasi. Menyimpan akun, group, sesi, konfigurasi koneksi (dengan password terenkripsi), hak akses, dan riwayat migration.
2. **Database target**: database yang dikelola pengguna. Koneksinya didaftarkan Platform Admin dan disimpan sebagai pool aktif di `database.Registry`.

```
Browser
   │  HTTP
   ▼
internal/web  ── middleware: recoverer → logging → security headers → requireAuth → (requireAdmin) → csrfProtect
   │
   ├── internal/auth       sesi, CSRF, password
   ├── internal/access     level akses efektif (Platform DB)
   ├── internal/admin      pengguna, group, koneksi, enkripsi password koneksi
   ├── internal/database   Registry koneksi target + query metadata/data (read-only)
   └── internal/migration  eksekusi SQL + riwayat migration
              │
              ▼
   Platform DB (internal)          Database target (dikelola)
   internal/platformdb             via database.Registry
```

Alur startup (`cmd/server/main.go`):

1. Memuat konfigurasi (`.env` lalu environment proses) dan memvalidasinya.
2. Membuka Platform DB dan menjalankan migrasi skema (`schema.sql` + `migrations/*.sql`).
3. Menyiapkan session manager, access service, registry, dan admin service.
4. Membuat Platform Admin pertama bila `BOOTSTRAP_ADMIN_*` diisi dan belum ada admin sama sekali.
5. Mengaktifkan semua koneksi target tersimpan (`ConnectAll`). Kegagalan satu koneksi hanya dicatat sebagai peringatan dan tidak menghentikan aplikasi.
6. Menjalankan *purge* sesi kadaluarsa di latar belakang (tiap 30 menit) dan HTTP server dengan *graceful shutdown* (`SIGINT`/`SIGTERM`).

---

## Struktur Direktori

```
.
├── cmd/server/                 Entry point (main.go) dan job pembersihan sesi (maintenance.go)
├── internal/
│   ├── access/                 Level akses (VIEW_ONLY/VIEW_EDIT), akses efektif, middleware akses
│   ├── admin/                  Service admin: users, groups, connections, secretbox (AES-256-GCM)
│   ├── auth/                   Password (PBKDF2), session manager, store (Postgres/memory), CSRF
│   ├── config/                 Pembacaan dan validasi konfigurasi, loader .env
│   ├── database/               Registry koneksi target, query schema/tabel/kolom/constraint/index/data
│   ├── migration/              Eksekusi migration, riwayat, otorisasi
│   ├── platformdb/             Koneksi Platform DB, schema.sql, migrations/, purge sesi
│   └── web/                    Router, middleware, handler halaman, adapter service, render template
├── web/
│   ├── assets.go               go:embed untuk templates dan static
│   ├── static/                 app.css, app.js
│   └── templates/              layout.html dan pages/*.html
├── scripts/postgres/init/      SQL inisialisasi container Postgres (database target contoh dan database test)
├── tests/integration/          Tes end-to-end admin → viewer → migration lewat HTTP
├── docker-compose.yml          PostgreSQL 16 untuk pengembangan
├── Dockerfile                  Build multi-stage (Go → Alpine)
└── .env.example                Contoh konfigurasi lokal
```

---

## Model Akses

- Hak akses diberikan **per group per database** dengan dua level: `VIEW_ONLY` dan `VIEW_EDIT`.
- Seorang pengguna dapat berada di banyak group. Bila beberapa group memberi level berbeda pada database yang sama, **level tertinggi yang berlaku**.
- Pengguna tanpa group yang punya akses tidak melihat database apa pun, dan akses langsung lewat URL ditolak dengan `403`.
- Status **Platform Admin** hanya membuka halaman administrasi (`/admin/*`). Untuk menjelajahi atau memigrasi sebuah database, akun admin pun perlu menjadi anggota group yang memiliki akses ke database tersebut.
- Pemeriksaan akses dilakukan di sisi backend pada setiap pemanggilan service (viewer dan migration), sehingga pencabutan akses langsung berlaku.

| Aksi | View Only | View + Edit |
| --- | :---: | :---: |
| Melihat database di dashboard | ✔ | ✔ |
| Menjelajahi schema, tabel, struktur, dan data | ✔ | ✔ |
| Melihat riwayat dan detail migration | ✔ | ✔ |
| Menjalankan migration | ✘ | ✔ |

---

## Keamanan

| Area | Implementasi |
| --- | --- |
| Password pengguna | PBKDF2-HMAC-SHA256, 600.000 iterasi, salt acak 16 byte, panjang 8–1024 byte, verifikasi *constant-time* |
| Timing login | Hash dummy dipakai saat username tidak ditemukan agar waktu respons seragam |
| Sesi | Token acak 32 byte; hanya hash SHA-256 yang disimpan di database. Cookie `dbmp_session` bersifat `HttpOnly`, `SameSite=Lax`, dan `Secure` di production |
| Siklus sesi | Idle 30 menit, absolut 8 jam, rotasi token tiap 15 menit, grace period 30 detik (semua dapat dikonfigurasi). Rotasi memakai *compare-and-swap* |
| CSRF | Token per sesi (hash disimpan di database) dikirim lewat field `csrf_token` atau header `X-CSRF-Token`, ditambah pemeriksaan `Origin` |
| Open redirect | Parameter `next` hanya menerima path lokal |
| Password koneksi target | Dienkripsi AES-256-GCM dengan `APP_SECRET_KEY` (32 byte) dan *additional authenticated data*; tidak pernah ditampilkan |
| Pesan error | Error database/koneksi tidak diteruskan mentah ke UI atau riwayat (tanpa DSN, host, atau kredensial) |
| Viewer | Identifier SQL di-*quote*; data dibaca dalam transaksi `REPEATABLE READ` + `READ ONLY` |
| Migration | Otorisasi dicek sebelum validasi input; batas ukuran 1 MiB; `statement_timeout` 30 detik dan `lock_timeout` 5 detik per transaksi |
| Header HTTP | `Content-Security-Policy` ketat (tanpa inline script/style), `X-Content-Type-Options`, `X-Frame-Options: DENY`, `Referrer-Policy`, dan `Strict-Transport-Security` saat cookie secure aktif |
| Output | Seluruh output template di-escape oleh `html/template` |
| Halaman dinamis | `Cache-Control: no-store` |

---

## Skema Database Platform

Skema dibuat otomatis saat aplikasi start (idempotent, dilindungi *advisory lock* agar dua instance tidak memigrasi bersamaan).

| Tabel | Isi |
| --- | --- |
| `users` | Akun: `username` (unik), `password_hash`, `is_platform_admin`, `created_at` |
| `app_groups` | Group pengguna (`name` unik) |
| `user_groups` | Relasi pengguna ↔ group |
| `database_connections` | Koneksi database target: `name`, `host`, `port`, `database_name`, `username`, `ssl_mode`, `password_encrypted` |
| `group_database_access` | Level akses group ke database: `VIEW_ONLY` atau `VIEW_EDIT` |
| `migration_history` | Riwayat migration per database: `version`, `sql_text`, `executed_by`, `executed_at`, `status` (`SUCCESS`/`FAILED`), `error_message` |
| `sessions` | Sesi: hash token saat ini dan sebelumnya, hash CSRF, waktu, status cabut |
| `platform_schema_migrations` | Pencatat file di `internal/platformdb/migrations/` yang sudah diterapkan |

Migrasi tambahan ditaruh sebagai file `.sql` di `internal/platformdb/migrations/` (diurutkan berdasarkan nama) dan diterapkan sekali saja. Saat ini terdapat `0001_connection_material.sql` yang menambahkan kolom detail koneksi pada `database_connections`.

---

## Daftar Route

Semua route di bawah `Terlindungi` memerlukan sesi; method selain `GET` juga memerlukan `Origin` yang sesuai dan token CSRF. Route `Admin` hanya untuk Platform Admin.

| Method | Path | Akses | Fungsi |
| --- | --- | --- | --- |
| GET | `/healthz` | Publik | Health check (memeriksa koneksi Platform DB) |
| GET | `/static/...` | Publik | File statis |
| GET, POST | `/login` | Publik | Form dan proses login |
| GET | `/` | Terlindungi | Redirect ke `/dashboard` |
| POST | `/logout` | Terlindungi | Keluar dan cabut sesi |
| GET | `/dashboard` | Terlindungi | Daftar database yang dapat diakses |
| GET | `/databases/{databaseID}` | Terlindungi | Schema dan daftar tabel |
| GET | `/databases/{databaseID}/tables/{schema}/{table}` | Terlindungi | Struktur tabel (kolom, constraint, index) |
| GET | `/databases/{databaseID}/tables/{schema}/{table}/data` | Terlindungi | Data tabel dengan pagination (`?page=`) |
| GET | `/databases/{databaseID}/migrations` | Terlindungi | Riwayat migration (`?page=`) dan form migration baru |
| GET | `/databases/{databaseID}/migrations/{version}` | Terlindungi | Detail satu migration |
| POST | `/databases/{databaseID}/migrations` | Terlindungi (View + Edit) | Menjalankan migration |
| GET | `/admin` | Admin | Redirect ke `/admin/users` |
| GET, POST | `/admin/users` | Admin | Daftar dan tambah pengguna |
| POST | `/admin/users/{id}/delete` | Admin | Hapus pengguna |
| GET, POST | `/admin/groups` | Admin | Daftar dan buat group |
| GET | `/admin/groups/{id}` | Admin | Detail group (anggota dan akses database) |
| POST | `/admin/groups/{id}/delete` | Admin | Hapus group |
| POST | `/admin/groups/{id}/members` | Admin | Tambah anggota |
| POST | `/admin/groups/{id}/members/{userID}/remove` | Admin | Keluarkan anggota |
| POST | `/admin/groups/{id}/access` | Admin | Beri atau ubah akses group ke database |
| POST | `/admin/groups/{id}/access/{dbID}/revoke` | Admin | Cabut akses group ke database |
| GET, POST | `/admin/connections` | Admin | Daftar dan tambah koneksi database |
| POST | `/admin/connections/{id}/test` | Admin | Tes (dan aktifkan) koneksi |
| POST | `/admin/connections/{id}/delete` | Admin | Hapus koneksi |

---

## Konfigurasi

Konfigurasi dibaca dari environment proses. Bila ada file `.env` di direktori kerja, isinya dimuat lebih dulu (variabel yang sudah ada di environment tidak ditimpa).

| Variabel | Wajib | Default | Keterangan |
| --- | :---: | --- | --- |
| `DATABASE_URL` | Ya | — | DSN Platform DB |
| `APP_ENV` | Tidak | `development` | `development` atau `production` |
| `APP_ADDR` | Tidak | `:8080` | Alamat listen HTTP server |
| `APP_SECRET_KEY` | Production | kunci dev bawaan (hanya `development`) | Kunci 32 byte dalam format base64 atau hex untuk mengenkripsi password koneksi target |
| `BOOTSTRAP_ADMIN_USERNAME` | Tidak | — | Username Platform Admin pertama (harus diisi berpasangan dengan password) |
| `BOOTSTRAP_ADMIN_PASSWORD` | Tidak | — | Password Platform Admin pertama (8–1024 karakter) |
| `SESSION_COOKIE_SECURE` | Tidak | `true` di production, `false` di development | Menandai cookie sebagai `Secure`; wajib `true` di production |
| `SESSION_IDLE_TIMEOUT` | Tidak | `30m` | Batas tidak aktif |
| `SESSION_ABSOLUTE_TIMEOUT` | Tidak | `8h` | Umur maksimum sesi |
| `SESSION_ROTATION_INTERVAL` | Tidak | `15m` | Interval rotasi token sesi |
| `SESSION_GRACE_PERIOD` | Tidak | `30s` | Masa berlaku token lama setelah rotasi (harus lebih kecil dari interval rotasi) |

Variabel khusus pengujian (tidak dibaca oleh aplikasi saat berjalan):

| Variabel | Dipakai oleh |
| --- | --- |
| `TARGET_DATABASE_URL` | Tes paket `database` dan `admin` |
| `TEST_PLATFORM_DATABASE_URL` | Tes paket `migration` dan `tests/integration` |
| `TEST_TARGET_DATABASE_URL` | Tes paket `migration` dan `tests/integration` |

Durasi memakai format Go (misalnya `30m`, `8h`). Konfigurasi yang tidak valid menghentikan aplikasi dengan daftar kesalahan yang jelas.

Menghasilkan `APP_SECRET_KEY` untuk production:

```bash
openssl rand -base64 32
```

> Kunci pengembangan bawaan diketahui oleh siapa pun yang membaca repositori. Jangan dipakai di luar mesin lokal. Mengganti `APP_SECRET_KEY` setelah ada koneksi tersimpan membuat password koneksi lama tidak dapat didekripsi.

---

## Menjalankan Aplikasi

### Prasyarat

- Go 1.25 atau lebih baru
- Docker dan Docker Compose (untuk PostgreSQL pengembangan)

### Langkah (WSL/Linux)

1. **Pastikan Docker Engine, Docker Compose, dan Go tersedia di WSL.** Jalankan perintah berikut dari terminal distro WSL, bukan PowerShell. Jika `.env` belum ada, buat dari contoh; jangan menimpa `.env` yang sudah ada.

   ```bash
   test -f .env || cp .env.example .env
   ```

   Periksa bahwa `DATABASE_URL` menggunakan port `5433`.

2. **Mulai PostgreSQL dan server dari WSL.** Script ini menunggu PostgreSQL siap sebelum menjalankan aplikasi. Hentikan server dengan `Ctrl+C`.

   ```bash
   ./scripts/dev-wsl.sh
   ```

   PostgreSQL hanya dipublikasikan ke loopback (`127.0.0.1:5433`); server web tersedia di `http://localhost:8080`. Untuk menghentikan PostgreSQL setelah keluar dari server, jalankan `docker compose down` dari WSL. `docker compose ps` dan `docker compose logs postgres` membantu memeriksa statusnya.

   Container membuat database berikut saat volume pertama kali dibuat:

   | Database | Fungsi |
   | --- | --- |
   | `dbmp_internal` | Platform DB |
   | `dbmp_target` | Database target contoh (tabel `customers`, `orders`, dan schema `audit` dengan tabel `audit_log`) |
   | `dbmp_platform_test`, `dbmp_target_test` | Database khusus pengujian |

3. **Masuk ke aplikasi.** Buka <http://localhost:8080> dan gunakan akun dari `BOOTSTRAP_ADMIN_USERNAME` / `BOOTSTRAP_ADMIN_PASSWORD`. Akun ini hanya dibuat bila belum ada Platform Admin sama sekali.

4. **Daftarkan database target contoh** lewat menu **Koneksi** (`/admin/connections`):

   | Field | Nilai |
   | --- | --- |
   | Host | `localhost` |
   | Port | `5433` |
   | Nama database | `dbmp_target` |
   | Username | `dbmp` |
   | Password | `dbmp_dev_password` |
   | Mode SSL | `disable` |

   Status **Aktif** berarti aplikasi berhasil terhubung ke target tersebut. Koneksi target disimpan oleh aplikasi; database `dbmp_target` yang baru dibuat Docker tidak otomatis terdaftar. Jika statusnya tidak aktif setelah server dimulai ulang, periksa host `localhost`, port `5433`, kredensial di atas, lalu gunakan aksi **Tes koneksi** di halaman Koneksi.

5. **Atur akses.** Buat group di menu **Group**, tambahkan anggota, lalu beri group akses *View Only* atau *View + Edit* ke database tersebut. Pengguna yang menjadi anggota group kemudian dapat melihat database di dashboard.

Untuk mengulang inisialisasi database contoh dari awal (menghapus seluruh data):

```bash
docker compose down -v && docker compose up -d
```

### Build binary

```bash
go build -o bin/server ./cmd/server
./bin/server
```

---

## Menjalankan dengan Docker

`docker-compose.yml` hanya menjalankan PostgreSQL. Aplikasi dapat dibangun menjadi image sendiri:

```bash
docker build -t dbmp .
```

Image berisi satu binary statis, berjalan sebagai user non-root (`uid 10001`), dan membuka port `8080`. File `.env` tidak ikut masuk ke image (`.dockerignore`), jadi berikan konfigurasi saat menjalankan container:

```bash
docker run --rm -p 8080:8080 --env-file .env dbmp
```

`DATABASE_URL` harus dapat dijangkau dari dalam container. Alamat `localhost:5433` pada `.env.example` menunjuk ke container itu sendiri, jadi ganti host-nya dengan alamat yang dapat dijangkau (misalnya `host.docker.internal`, atau nama service bila aplikasi dijalankan dalam jaringan Compose yang sama dengan PostgreSQL). Hal yang sama berlaku untuk host pada koneksi database target yang didaftarkan lewat UI.

Untuk production: set `APP_ENV=production`, isi `APP_SECRET_KEY`, dan layani aplikasi di belakang HTTPS (cookie `Secure` wajib aktif di production).

---

## Pengujian

```bash
go test ./...
```

Spesifikasi mewajibkan automated testing untuk fitur backend, mencakup alur berhasil, kegagalan, dan penolakan akses. Tes dikelompokkan menjadi dua jenis.

### Unit test (tanpa database)

Menguji bagian kecil secara terisolasi dengan fake, stub, atau store in-memory. Selalu berjalan tanpa konfigurasi tambahan.

| Paket | Yang diuji (contoh) |
| --- | --- |
| `internal/config` | Default development, syarat production, nilai tidak valid, pasangan akun bootstrap |
| `internal/access` | Akses efektif, penolakan akses langsung, ID tidak valid, error repository, middleware |
| `internal/auth` | Hashing password, siklus sesi, idle dan absolute timeout, rotasi token dan grace period, rotasi saat privilege naik, pencabutan sesi, CSRF, login/logout handler, *compare-and-swap* store |
| `internal/admin` | Enkripsi password koneksi (`secretBox`), escaping DSN, validasi input koneksi |
| `internal/database` | Penolakan DSN kosong dan database tidak terjangkau, password tidak bocor pada error, pemetaan error PostgreSQL |
| `internal/migration` | Penolakan sebelum menyentuh database, riwayat terlarang/tidak valid, pesan error tidak membocorkan detail |
| `internal/web` | Handler dengan fake service: redirect tanpa sesi, login gagal/berhasil, origin asing ditolak, `next` bukan open redirect, CSRF, timeout dan rotasi sesi, pembatasan halaman admin, akses langsung ke database terlarang, View Only tidak dapat menjalankan migration, eksekusi migration berhasil/gagal/kosong, pagination, escaping output, error tidak membocorkan detail, security header dan health check |

### Integration test (database PostgreSQL nyata)

Menguji interaksi komponen nyata. Tes dilewati (*skip*) otomatis bila variabel environment yang dibutuhkan belum diisi.

| Cakupan | Yang diuji (contoh) | Variabel yang dibutuhkan |
| --- | --- | --- |
| `internal/platformdb` | Koneksi Platform DB, migrasi skema idempotent, purge sesi | `DATABASE_URL` |
| `internal/admin` | Aturan pengguna dan admin terakhir, keanggotaan group dan akses efektif, siklus hidup koneksi dengan Registry | `DATABASE_URL`, `TARGET_DATABASE_URL` |
| `internal/database` | Registry koneksi, daftar schema/tabel/kolom/constraint/index, pagination data, transaksi read-only | `TARGET_DATABASE_URL` |
| `internal/migration` | Eksekusi berhasil, rollback total saat gagal, versi linear di bawah konkurensi, View Only hanya membaca riwayat, matriks akses nyata | `TEST_PLATFORM_DATABASE_URL`, `TEST_TARGET_DATABASE_URL` |
| `tests/integration` | Alur end-to-end lewat HTTP (lihat di bawah), dan sesi pengguna yang sudah dihapus ditolak | `TEST_PLATFORM_DATABASE_URL`, `TEST_TARGET_DATABASE_URL` |

Pastikan PostgreSQL dari `docker compose up -d` sudah berjalan. Variabel dapat diisi lewat `.env` (dimuat otomatis oleh `TestMain`) atau environment shell.

> **Peringatan:** tes `migration` menjalankan `DROP SCHEMA public CASCADE` pada database yang ditunjuk `TEST_PLATFORM_DATABASE_URL` dan `TEST_TARGET_DATABASE_URL`. Karena itu nama database wajib berakhiran `_test` (dicek oleh tes) dan jangan diarahkan ke database yang berisi data penting. Nilai default pada `.env.example` (`dbmp_platform_test`, `dbmp_target_test`) sudah aman.

Skenario end-to-end (`TestEndToEndAdminToViewer`) mengikuti alur minimum spesifikasi:

1. Platform Admin mendaftarkan koneksi, membuat group, menambah anggota, dan memberi akses *View Only*.
2. Pengguna *View Only* melihat database di dashboard, menjelajahi tabel, struktur, dan data (pagination, `NULL`, escaping HTML), tetapi `403` saat menjalankan migration dan tabel target tidak berubah.
3. Pengguna tanpa group tidak melihat database apa pun dan ditolak (`403`) saat membuka URL langsung; non-admin ditolak di halaman admin.
4. Akses naik menjadi *View + Edit* lewat group kedua (level tertinggi berlaku); migration berhasil dijalankan dan riwayat serta detail SQL-nya tampil.
5. Koneksi dihapus: akses hilang. Logout mencabut sesi.

---

