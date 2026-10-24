package database

import "errors"
// Kontrak error package database:
//   ErrInvalidArgument        argumen tidak valid (ID <= 0, DSN kosong, page di luar batas)
//   ErrNotRegistered          koneksi belum terdaftar di Registry
//   ErrAlreadyRegistered      koneksi dengan ID tersebut sudah terdaftar
//   ErrConnectFailed          gagal konek/ping ke database target (termasuk timeout)
//   ErrTargetPermissionDenied user database target ditolak oleh PostgreSQL target
//   ErrTableNotFound          schema/tabel tidak ada atau tidak terlihat
var (
	ErrInvalidArgument        = errors.New("invalid argument")
	ErrNotRegistered          = errors.New("database connection is not registered")
	ErrAlreadyRegistered      = errors.New("database connection is already registered")
	ErrConnectFailed          = errors.New("cannot connect to target database")
	ErrTargetPermissionDenied = errors.New("target database denied access")
	ErrTableNotFound          = errors.New("table not found")
)