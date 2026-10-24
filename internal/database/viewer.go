package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

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
	Name                string `json:"name"`
	Type                string `json:"type"`
	Definition string `json:"definition"`
}
type Index struct {
	Name      string `json:"name"`
	Definition string `json:"definition"`
	IsUnique  bool   `json:"is_unique"`
	IsPrimary bool   `json:"is_primary"`
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
		return nil, fmt.Errorf("schema must not be empty")
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
		return nil, fmt.Errorf("schema and table must not be empty")
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
        return nil, fmt.Errorf("schema and table must not be empty")
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
		return nil, fmt.Errorf("schema and table must not be empty")
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
		return TableData{}, fmt.Errorf("schema and table must not be empty")
	}
	if page < 1 {
		return TableData{}, fmt.Errorf("page must be at least 1")
	}
	if pageSize < 1 || pageSize > 100 {
		return TableData{}, fmt.Errorf("page size must be between 1 and 100")
	}

	// Memastikan tabel tersedia dan memperoleh daftar kolomnya.
	metadata, err := ListColumns(ctx, db, schema, table)
	if err != nil {
		return TableData{}, err
	}
	if len(metadata) == 0 {
		return TableData{}, fmt.Errorf(
			"table %q.%q does not exist or has no visible columns",
			schema, table,
		)
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

	// Menghitung total baris untuk informasi pagination.
	countQuery := "SELECT COUNT(*) FROM " + qualifiedTable
	if err := db.QueryRowContext(ctx, countQuery).Scan(&result.TotalRows); err != nil {
		return TableData{}, fmt.Errorf("count table rows: %w", err)
	}

	// Mengambil kolom primary key agar urutan halaman stabil
	// untuk tabel yang memiliki primary key.
	pkRows, err := db.QueryContext(ctx, `
		SELECT kcu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
			ON tc.constraint_catalog = kcu.constraint_catalog
			AND tc.constraint_schema = kcu.constraint_schema
			AND tc.constraint_name = kcu.constraint_name
			AND tc.table_schema = kcu.table_schema
			AND tc.table_name = kcu.table_name
		WHERE tc.constraint_type = 'PRIMARY KEY'
			AND tc.table_schema = $1
			AND tc.table_name = $2
		ORDER BY kcu.ordinal_position
	`, schema, table)
	if err != nil {
		return TableData{}, fmt.Errorf("find primary key: %w", err)
	}

	var primaryKeys []string
	for pkRows.Next() {
		var name string
		if err := pkRows.Scan(&name); err != nil {
			pkRows.Close()
			return TableData{}, fmt.Errorf("scan primary key: %w", err)
		}
		primaryKeys = append(primaryKeys, name)
	}
	if err := pkRows.Err(); err != nil {
		pkRows.Close()
		return TableData{}, fmt.Errorf("iterate primary keys: %w", err)
	}
	if err := pkRows.Close(); err != nil {
		return TableData{}, fmt.Errorf("close primary key rows: %w", err)
	}

	query := "SELECT * FROM " + qualifiedTable
	if len(primaryKeys) > 0 {
		query += " ORDER BY "
		for i, name := range primaryKeys {
			if i > 0 {
				query += ", "
			}
			query += quoteIdentifier(name)
		}
	}

	offset := (page - 1) * pageSize
	query += " LIMIT $1 OFFSET $2"

	rows, err := db.QueryContext(ctx, query, pageSize, offset)
	if err != nil {
		return TableData{}, fmt.Errorf("read table data: %w", err)
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
		return TableData{}, fmt.Errorf("iterate table data: %w", err)
	}

	return result, nil
}

// quoteIdentifier mengamankan nama schema, tabel, dan kolom yang digunakan sebagai identifier SQL PostgreSQL.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}