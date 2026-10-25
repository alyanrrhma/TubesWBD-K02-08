-- Detail koneksi database target (dipakai halaman admin "Koneksi" dan Registry P2).
-- schema.sql sengaja hanya memuat id/name/created_at dan menyerahkan kolom ini
-- ke migration terpisah ("P2/P3 will add encrypted connection material").
--
-- Kolom dibuat NULL-able agar baris lama (jika ada) tidak gagal; baris tanpa
-- detail tampil sebagai "belum lengkap" di UI dan tidak dihubungkan.
-- password_encrypted: AES-256-GCM (nonce || ciphertext) dengan kunci APP_SECRET_KEY.

ALTER TABLE database_connections
    ADD COLUMN IF NOT EXISTS host               TEXT,
    ADD COLUMN IF NOT EXISTS port               INTEGER CHECK (port BETWEEN 1 AND 65535),
    ADD COLUMN IF NOT EXISTS database_name      TEXT,
    ADD COLUMN IF NOT EXISTS username           TEXT,
    ADD COLUMN IF NOT EXISTS ssl_mode           TEXT NOT NULL DEFAULT 'prefer' CHECK (ssl_mode IN ('disable', 'prefer', 'require')),
    ADD COLUMN IF NOT EXISTS password_encrypted BYTEA;