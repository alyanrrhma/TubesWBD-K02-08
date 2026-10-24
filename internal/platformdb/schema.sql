-- Platform database schema owned collaboratively with P3.
CREATE TABLE IF NOT EXISTS users (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 username TEXT NOT NULL UNIQUE,
 password_hash TEXT NOT NULL,
 is_platform_admin BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS app_groups (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 name TEXT NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS user_groups (
 user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 group_id BIGINT NOT NULL REFERENCES app_groups(id) ON DELETE CASCADE,
 PRIMARY KEY (user_id,group_id)
);
CREATE TABLE IF NOT EXISTS database_connections (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 name TEXT NOT NULL UNIQUE,
 -- P2/P3 will add encrypted connection material using a separate, reviewed migration.
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS migration_history (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    database_id BIGINT NOT NULL
        REFERENCES database_connections(id) ON DELETE CASCADE,
    version BIGINT NOT NULL CHECK (version > 0),
    sql_text TEXT NOT NULL,
    executed_by BIGINT NOT NULL
        REFERENCES users(id) ON DELETE RESTRICT,
    executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    status TEXT NOT NULL
        CHECK (status IN ('SUCCESS', 'FAILED')),
    error_message TEXT,
    CONSTRAINT migration_history_database_version_unique
        UNIQUE (database_id, version),
    CONSTRAINT migration_history_error_consistency
        CHECK (
            (status = 'SUCCESS' AND error_message IS NULL)
            OR
            (status = 'FAILED')
        )
);
CREATE TABLE IF NOT EXISTS group_database_access (
 group_id BIGINT NOT NULL REFERENCES app_groups(id) ON DELETE CASCADE,
 database_id BIGINT NOT NULL REFERENCES database_connections(id) ON DELETE CASCADE,
 access_level TEXT NOT NULL CHECK (access_level IN ('VIEW_ONLY','VIEW_EDIT')),
 PRIMARY KEY(group_id,database_id)
);
CREATE TABLE IF NOT EXISTS sessions (
 family_id TEXT PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 is_admin BOOLEAN NOT NULL DEFAULT FALSE,
 token_hash BYTEA NOT NULL UNIQUE,
 old_token_hash BYTEA,
 old_expires_at TIMESTAMPTZ,
 csrf_hash BYTEA NOT NULL,
 created_at TIMESTAMPTZ NOT NULL,
 last_seen TIMESTAMPTZ NOT NULL,
 rotated_at TIMESTAMPTZ NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL,
 revoked BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX IF NOT EXISTS sessions_user_id_idx ON sessions(user_id);
CREATE INDEX IF NOT EXISTS sessions_old_token_hash_idx ON sessions(old_token_hash) WHERE old_token_hash IS NOT NULL;
CREATE INDEX IF NOT EXISTS migration_history_database_executed_at_idx
    ON migration_history (database_id, executed_at DESC);
CREATE INDEX IF NOT EXISTS migration_history_executed_by_idx
    ON migration_history (executed_by);
