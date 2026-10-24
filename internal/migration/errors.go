package migration

import "errors"

// Kontrak error package migration:
//	ErrInvalidArgument    ID/page/pageSize tidak valid
//	ErrEmptySQL           SQL migration kosong
//	ErrSQLTooLarge        SQL melebihi batas ukuran
//	ErrInvalidSQL         SQL mengandung karakter yang tidak diizinkan (mis. NUL)
//	ErrForbidden          user tidak punya hak (View untuk riwayat, View+Edit untuk eksekusi)
//	ErrMigrationNotFound  versi migration tidak ada pada database tersebut
//	ErrHistoryNotRecorded eksekusi di target sudah selesai, tetapi riwayat gagal disimpan

// Catatan: migration yang gagal dieksekusi dikembalikan sebagai Migration dengan Status == StatusFailed dan ErrorMessage terisi.
var (
	ErrInvalidArgument    = errors.New("migration: invalid argument")
	ErrEmptySQL           = errors.New("migration: sql is empty")
	ErrSQLTooLarge        = errors.New("migration: sql is too large")
	ErrInvalidSQL         = errors.New("migration: sql contains invalid characters")
	ErrForbidden          = errors.New("migration: access denied")
	ErrMigrationNotFound  = errors.New("migration: not found")
	ErrHistoryNotRecorded = errors.New("migration: executed but history was not recorded")
)