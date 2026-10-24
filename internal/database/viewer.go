package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"github.com/jackc/pgx/v5/pgconn"
)

const queryTimeout = 15 * time.Second

// wrapQueryError menerjemahkan error PostgreSQL yang penting menjadi sentinel.
func wrapQueryError(op string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "42501": // insufficient_privilege
			return fmt.Errorf("%w: %s", ErrTargetPermissionDenied, op)
		case "42P01": // undefined_table
			return fmt.Errorf("%w: %s", ErrTableNotFound, op)
		}
	}
	return fmt.Errorf("%s: %w", op, err)
}

type Table struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
}
type Column struct {
	Name       string `json:"name"`
	DataType   string `json:"data_type"`
	IsNullable bool   `json:"is_nullable"`
	Position   int    `json:"position"`
}
type Schema struct {
	Name string `json:"name"`
}
type Constraint struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Definition string `json:"definition"`
}
type Index struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
	IsUnique   bool   `json:"is_unique"`
	IsPrimary  bool   `json:"is_primary"`
}
type TableData struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	TotalRows int      `json:"total_rows"`
	Page      int      `json:"page"`
	PageSize  int      `json:"page_size"`
}

// ListTables mengambil daftar tabel biasa dalam sebuah schema.
func ListTables(ctx context.Context, db *sql.DB, schema string) ([]Table, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}
	if schema == "" {
		return nil, fmt.Errorf("%w: schema must not be empty", ErrInvalidArgument)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT table_schema, table_name
		FROM information_schema.tables
		WHERE table_schema = $1
		  AND table_type = 'BASE TABLE'
		ORDER BY table_name
	`, schema)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()

	tables := make([]Table, 0)

	for rows.Next() {
		var table Table
		if err := rows.Scan(&table.Schema, &table.Name); err != nil {
			return nil, fmt.Errorf("scan table: %w", err)
		}
		tables = append(tables, table)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tables: %w", err)
	}

	return tables, nil
}

// ListColumns mengambil daftar kolom dari tabel tertentu.
func ListColumns(ctx context.Context, db *sql.DB, schema string, table string) ([]Column, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}
	if schema == "" || table == "" {
		return nil, fmt.Errorf("%w: schema and table must not be empty", ErrInvalidArgument)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT
			column_name,
			data_type,
			is_nullable,
			ordinal_position
		FROM information_schema.columns
		WHERE table_schema = $1
		  AND table_name = $2
		ORDER BY ordinal_position
	`, schema, table)
	if err != nil {
		return nil, fmt.Errorf("list columns: %w", err)
	}
	defer rows.Close()

	columns := make([]Column, 0)

	for rows.Next() {
		var column Column
		var nullable string

		if err := rows.Scan(
			&column.Name,
			&column.DataType,
			&nullable,
			&column.Position,
		); err != nil {
			return nil, fmt.Errorf("scan column: %w", err)
		}

		column.IsNullable = nullable == "YES"
		columns = append(columns, column)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate columns: %w", err)
	}

	return columns, nil
}

// ListSchemas mengambil daftar schema yang dapat diakses
// pada database target.
func ListSchemas(ctx context.Context, db *sql.DB) ([]Schema, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	rows, err := db.QueryContext(ctx, `
		SELECT schema_name
		FROM information_schema.schemata
		WHERE schema_name NOT IN (
			'information_schema',
			'pg_catalog'
		)
		AND schema_name NOT LIKE 'pg_toast%%'
		AND schema_name NOT LIKE 'pg_temp_%%'
		ORDER BY schema_name
	`)
	if err != nil {
		return nil, fmt.Errorf("list schemas: %w", err)
	}
	defer rows.Close()

	schemas := make([]Schema, 0)

	for rows.Next() {
		var schema Schema
		if err := rows.Scan(&schema.Name); err != nil {
			return nil, fmt.Errorf("scan schema: %w", err)
		}
		schemas = append(schemas, schema)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schemas: %w", err)
	}

	return schemas, nil
}

// ListConstraints mengambil constraint dari tabel tertentu.
func ListConstraints(ctx context.Context, db *sql.DB, schema string, table string) ([]Constraint, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}
	if schema == "" || table == "" {
		return nil, fmt.Errorf("%w: schema and table must not be empty", ErrInvalidArgument)
	}

	const query = `
        SELECT
            c.conname,
            CASE c.contype
                WHEN 'p' THEN 'PRIMARY KEY'
                WHEN 'u' THEN 'UNIQUE'
                WHEN 'f' THEN 'FOREIGN KEY'
                WHEN 'c' THEN 'CHECK'
                WHEN 'x' THEN 'EXCLUDE'
                ELSE 'OTHER'
            END AS constraint_type,
            pg_get_constraintdef(c.oid, true) AS definition
        FROM pg_catalog.pg_constraint c
        JOIN pg_catalog.pg_class t
            ON t.oid = c.conrelid
        JOIN pg_catalog.pg_namespace n
            ON n.oid = t.relnamespace
        WHERE n.nspname = $1
          AND t.relname = $2
        ORDER BY c.conname
    `

	rows, err := db.QueryContext(ctx, query, schema, table)
	if err != nil {
		return nil, fmt.Errorf("list table constraints: %w", err)
	}
	defer rows.Close()

	constraints := make([]Constraint, 0)

	for rows.Next() {
		var c Constraint
		if err := rows.Scan(&c.Name, &c.Type, &c.Definition); err != nil {
			return nil, fmt.Errorf("scan table constraint: %w", err)
		}
		constraints = append(constraints, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate table constraints: %w", err)
	}

	return constraints, nil
}

// ListIndexes mengambil index dari tabel tertentu.
func ListIndexes(ctx context.Context, db *sql.DB, schema string, table string) ([]Index, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}
	if schema == "" || table == "" {
		return nil, fmt.Errorf("%w: schema and table must not be empty", ErrInvalidArgument)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT
			idx.relname,
			pg_get_indexdef(i.indexrelid),
			i.indisunique,
			i.indisprimary
		FROM pg_catalog.pg_index i
		JOIN pg_catalog.pg_class tbl
			ON tbl.oid = i.indrelid
		JOIN pg_catalog.pg_namespace ns
			ON ns.oid = tbl.relnamespace
		JOIN pg_catalog.pg_class idx
			ON idx.oid = i.indexrelid
		WHERE ns.nspname = $1
			AND tbl.relname = $2
		ORDER BY idx.relname
	`, schema, table)
	if err != nil {
		return nil, fmt.Errorf("list indexes: %w", err)
	}
	defer rows.Close()

	indexes := make([]Index, 0)

	for rows.Next() {
		var idx Index
		if err := rows.Scan(
			&idx.Name,
			&idx.Definition,
			&idx.IsUnique,
			&idx.IsPrimary,
		); err != nil {
			return nil, fmt.Errorf("scan index: %w", err)
		}
		indexes = append(indexes, idx)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate indexes: %w", err)
	}

	return indexes, nil
}

// ReadTableData membaca isi tabel dengan pagination. Page dimulai dari 1.
func ReadTableData(ctx context.Context, db *sql.DB, schema string, table string, page int, pageSize int) (TableData, error) {
	if db == nil {
		return TableData{}, fmt.Errorf("database connection is nil")
	}
	if schema == "" || table == "" {
		return TableData{}, fmt.Errorf("%w: schema and table must not be empty", ErrInvalidArgument)
	}
	if page < 1 {
		return TableData{}, fmt.Errorf("%w: page must be at least 1", ErrInvalidArgument)
	}
	if pageSize < 1 || pageSize > 100 {
		return TableData{}, fmt.Errorf("%w: page size must be between 1 and 100", ErrInvalidArgument)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	// Memastikan tabel tersedia dan memperoleh daftar kolomnya.
	metadata, err := ListColumns(ctx, db, schema, table)
	if err != nil {
		return TableData{}, err
	}
	if len(metadata) == 0 {
		return TableData{}, fmt.Errorf("%w: %q.%q", ErrTableNotFound, schema, table)
	}

	result := TableData{
		Columns:  make([]string, 0, len(metadata)),
		Rows:     make([][]any, 0),
		Page:     page,
		PageSize: pageSize,
	}

	for _, column := range metadata {
		result.Columns = append(result.Columns, column.Name)
	}

	qualifiedTable := quoteIdentifier(schema) + "." + quoteIdentifier(table)

	tx, err := db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return TableData{}, wrapQueryError("begin read-only transaction", err)
	}
	defer tx.Rollback()

	// Menghitung total baris untuk informasi pagination.
	countQuery := "SELECT COUNT(*) FROM " + qualifiedTable
	if err := tx.QueryRowContext(ctx, countQuery).Scan(&result.TotalRows); err != nil {
		return TableData{}, wrapQueryError("count table rows", err)
	}

	// Mengambil kolom primary key untuk menentukan urutan baris.
	primaryKeys, err := primaryKeyColumns(ctx, tx, schema, table)
	if err != nil {
		return TableData{}, err
	}

	query := "SELECT * FROM " + qualifiedTable

	if len(primaryKeys) > 0 {
		// Jika ada primary key, gunakan sebagai urutan baris.
		orderColumns := make([]string, 0, len(primaryKeys))

		for _, name := range primaryKeys {
			orderColumns = append(orderColumns, quoteIdentifier(name))
		}

		query += " ORDER BY " + strings.Join(orderColumns, ", ")
	} else {
		// Jika tidak ada primary key, ctid menjadi urutan sementara.
		// ctid bukan identitas permanen dan bisa berubah.
		query += " ORDER BY ctid"
	}

	offset := (page - 1) * pageSize
	query += " LIMIT $1 OFFSET $2"

	rows, err := tx.QueryContext(ctx, query, pageSize, offset)
	if err != nil {
		return TableData{}, wrapQueryError("read table data", err)
	}
	defer rows.Close()

	for rows.Next() {
		values := make([]any, len(result.Columns))
		destinations := make([]any, len(values))

		for i := range values {
			destinations[i] = &values[i]
		}

		if err := rows.Scan(destinations...); err != nil {
			return TableData{}, fmt.Errorf("scan table row: %w", err)
		}

		result.Rows = append(result.Rows, values)
	}

	if err := rows.Err(); err != nil {
		return TableData{}, wrapQueryError("iterate table data", err)
	}

	return result, nil
}

// quoteIdentifier mengamankan nama schema, tabel, dan kolom yang digunakan sebagai identifier SQL PostgreSQL.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// Menggunakan katalog PostgreSQL untuk mendapatkan kolom primary key, termasuk jika primary key terdiri dari beberapa kolom.
func primaryKeyColumns(ctx context.Context, tx *sql.Tx, schema string, table string) ([]string, error) {
	const query = `
        SELECT a.attname
        FROM pg_catalog.pg_index i
        JOIN pg_catalog.pg_class t
            ON t.oid = i.indrelid
        JOIN pg_catalog.pg_namespace n
            ON n.oid = t.relnamespace
        JOIN LATERAL
            unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord)
            ON true
        JOIN pg_catalog.pg_attribute a
            ON a.attrelid = t.oid
           AND a.attnum = k.attnum
        WHERE i.indisprimary
          AND n.nspname = $1
          AND t.relname = $2
        ORDER BY k.ord
    `

	rows, err := tx.QueryContext(ctx, query, schema, table)
	if err != nil {
		return nil, wrapQueryError("find primary key columns", err)
	}
	defer rows.Close()

	columns := make([]string, 0)

	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return nil, fmt.Errorf("scan primary key column: %w", err)
		}
		columns = append(columns, column)
	}

	if err := rows.Err(); err != nil {
		return nil, wrapQueryError("find primary key columns", err)
	}

	return columns, nil
}