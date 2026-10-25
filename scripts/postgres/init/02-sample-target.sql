-- Data contoh untuk database target dbmp_target (dibuat oleh 01-create-target.sql).
-- Hanya berjalan saat volume Postgres pertama kali dibuat (docker compose down -v untuk mengulang).
-- Daftarkan lewat halaman admin "Koneksi": host localhost (atau "postgres" dari dalam Docker),
-- port 5433 (atau 5432 dari dalam Docker), database dbmp_target, user dbmp.
\connect dbmp_target

CREATE TABLE customers (
    id         SERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    email      TEXT UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE orders (
    id          SERIAL PRIMARY KEY,
    customer_id INTEGER       NOT NULL REFERENCES customers (id),
    total       NUMERIC(12,2) NOT NULL CHECK (total >= 0),
    status      TEXT          NOT NULL DEFAULT 'pending'
);
CREATE INDEX orders_customer_id_idx ON orders (customer_id);

CREATE SCHEMA audit;
CREATE TABLE audit.audit_log (
    id     BIGSERIAL PRIMARY KEY,
    actor  TEXT NOT NULL,
    action TEXT NOT NULL,
    at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO customers (name, email)
SELECT 'Pelanggan ' || g, CASE WHEN g % 9 = 0 THEN NULL ELSE 'user' || g || '@example.com' END
FROM generate_series(1, 120) AS g;

INSERT INTO orders (customer_id, total, status)
SELECT (g % 120) + 1, (g * 13 % 900) + 25, (ARRAY['pending','paid','shipped'])[g % 3 + 1]
FROM generate_series(1, 300) AS g;

INSERT INTO audit.audit_log (actor, action)
SELECT 'system', (ARRAY['LOGIN','UPDATE','EXPORT'])[g % 3 + 1] FROM generate_series(1, 40) AS g;