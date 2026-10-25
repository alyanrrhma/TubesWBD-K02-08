package migration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

const maxDesiredSchemaBytes = 256 << 10

var (
	ErrInvalidDesiredSchema  = errors.New("migration: invalid desired schema")
	ErrDesiredSchemaTooLarge = errors.New("migration: desired schema definition is too large")
	typePattern              = regexp.MustCompile(`(?i)^(smallint|integer|bigint|text|boolean|date|timestamp( with(out)? time zone)?|time( with(out)? time zone)?|uuid|json|jsonb|bytea|real|double precision|numeric(\([0-9]{1,3}(,[0-9]{1,3})?\))?|decimal(\([0-9]{1,3}(,[0-9]{1,3})?\))?|varchar\([0-9]{1,6}\)|character varying\([0-9]{1,6}\)|char\([0-9]{1,6}\)|character\([0-9]{1,6}\))$`)
)

// SchemaDefinition describes the table and column portion of a PostgreSQL schema.
// Defaults, constraints, indexes, and generated columns are intentionally outside
// this first diff format and are left untouched by generated SQL.
type SchemaDefinition struct {
	Schema string        `json:"schema"`
	Tables []SchemaTable `json:"tables"`
}

type SchemaTable struct {
	Name    string         `json:"name"`
	Columns []SchemaColumn `json:"columns"`
}

type SchemaColumn struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

type SchemaChange struct {
	Kind        string
	Object      string
	Description string
	Destructive bool
}

type SchemaDiffResult struct {
	Changes []SchemaChange
	SQL     string
}

// DiffSchemas compares table and column definitions and builds a reviewable SQL
// candidate. Identifiers are quoted and PostgreSQL types are allowlisted.
func DiffSchemas(current, desired SchemaDefinition) (SchemaDiffResult, error) {
	if err := validateSchema(current, false); err != nil {
		return SchemaDiffResult{}, fmt.Errorf("current schema: %w", err)
	}
	if err := validateSchema(desired, true); err != nil {
		return SchemaDiffResult{}, fmt.Errorf("desired schema: %w", err)
	}
	if current.Schema != desired.Schema {
		return SchemaDiffResult{}, fmt.Errorf("%w: schema name cannot be changed", ErrInvalidDesiredSchema)
	}

	currentTables := make(map[string]SchemaTable, len(current.Tables))
	desiredTables := make(map[string]SchemaTable, len(desired.Tables))
	for _, table := range current.Tables {
		currentTables[table.Name] = table
	}
	for _, table := range desired.Tables {
		desiredTables[table.Name] = table
	}

	var result SchemaDiffResult
	var statements []string
	quote := func(identifier string) string { return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"` }
	tableRef := func(table string) string { return quote(desired.Schema) + "." + quote(table) }

	for _, tableName := range sortedKeys(desiredTables) {
		want := desiredTables[tableName]
		have, exists := currentTables[tableName]
		if !exists {
			cols := make([]string, 0, len(want.Columns))
			for _, col := range want.Columns {
				cols = append(cols, "  "+quote(col.Name)+" "+strings.TrimSpace(col.Type)+nullableSQL(col.Nullable))
			}
			statements = append(statements, "CREATE TABLE "+tableRef(tableName)+" (\n"+strings.Join(cols, ",\n")+"\n);")
			result.Changes = append(result.Changes, SchemaChange{Kind: "CREATE TABLE", Object: tableName, Description: "Membuat tabel beserta kolom yang didefinisikan."})
			continue
		}
		haveColumns := make(map[string]SchemaColumn, len(have.Columns))
		wantColumns := make(map[string]SchemaColumn, len(want.Columns))
		for _, col := range have.Columns {
			haveColumns[col.Name] = col
		}
		for _, col := range want.Columns {
			wantColumns[col.Name] = col
		}

		for _, colName := range sortedKeys(wantColumns) {
			wantCol := wantColumns[colName]
			haveCol, columnExists := haveColumns[colName]
			if !columnExists {
				statements = append(statements, "ALTER TABLE "+tableRef(tableName)+" ADD COLUMN "+quote(colName)+" "+strings.TrimSpace(wantCol.Type)+nullableSQL(wantCol.Nullable)+";")
				description := "Menambahkan kolom bertipe " + wantCol.Type + "."
				if !wantCol.Nullable {
					description += " Jika tabel sudah berisi data, penambahan kolom NOT NULL tanpa default dapat gagal."
				}
				result.Changes = append(result.Changes, SchemaChange{Kind: "ADD COLUMN", Object: tableName + "." + colName, Description: description})
				continue
			}
			if normalizeType(haveCol.Type) != normalizeType(wantCol.Type) {
				stmt := "ALTER TABLE " + tableRef(tableName) + " ALTER COLUMN " + quote(colName) + " TYPE " + strings.TrimSpace(wantCol.Type) + " USING " + quote(colName) + "::" + strings.TrimSpace(wantCol.Type) + ";"
				statements = append(statements, stmt)
				result.Changes = append(result.Changes, SchemaChange{Kind: "ALTER TYPE", Object: tableName + "." + colName, Description: "Mengubah tipe dari " + haveCol.Type + " menjadi " + wantCol.Type + "; konversi data dapat gagal."})
			}
			if haveCol.Nullable != wantCol.Nullable {
				if wantCol.Nullable {
					statements = append(statements, "ALTER TABLE "+tableRef(tableName)+" ALTER COLUMN "+quote(colName)+" DROP NOT NULL;")
					result.Changes = append(result.Changes, SchemaChange{Kind: "ALLOW NULL", Object: tableName + "." + colName, Description: "Mengizinkan nilai NULL."})
				} else {
					statements = append(statements, "ALTER TABLE "+tableRef(tableName)+" ALTER COLUMN "+quote(colName)+" SET NOT NULL;")
					result.Changes = append(result.Changes, SchemaChange{Kind: "SET NOT NULL", Object: tableName + "." + colName, Description: "Mewajibkan nilai; gagal bila data saat ini mengandung NULL."})
				}
			}
		}
		for _, colName := range sortedKeys(haveColumns) {
			if _, exists := wantColumns[colName]; exists {
				continue
			}
			statements = append(statements, "ALTER TABLE "+tableRef(tableName)+" DROP COLUMN "+quote(colName)+";")
			result.Changes = append(result.Changes, SchemaChange{Kind: "DROP COLUMN", Object: tableName + "." + colName, Description: "Menghapus kolom dan datanya.", Destructive: true})
		}
	}
	for _, tableName := range sortedKeys(currentTables) {
		if _, exists := desiredTables[tableName]; exists {
			continue
		}
		statements = append(statements, "DROP TABLE "+tableRef(tableName)+";")
		result.Changes = append(result.Changes, SchemaChange{Kind: "DROP TABLE", Object: tableName, Description: "Menghapus tabel beserta seluruh datanya.", Destructive: true})
	}
	if len(statements) == 0 {
		result.Changes = append(result.Changes, SchemaChange{Kind: "NO CHANGE", Description: "Schema tabel dan kolom sudah sesuai."})
	}
	result.SQL = strings.Join(statements, "\n")
	return result, nil
}

func (s *Service) DiffSchema(ctx context.Context, userID, databaseID int64, desiredJSON string) (SchemaDiffResult, error) {
	if userID <= 0 || databaseID <= 0 {
		return SchemaDiffResult{}, ErrInvalidArgument
	}
	ok, err := s.authz.CanEdit(ctx, userID, databaseID)
	if err != nil {
		return SchemaDiffResult{}, fmt.Errorf("check edit access: %w", err)
	}
	if !ok {
		return SchemaDiffResult{}, ErrForbidden
	}
	if len(desiredJSON) > maxDesiredSchemaBytes {
		return SchemaDiffResult{}, ErrDesiredSchemaTooLarge
	}
	var desired SchemaDefinition
	decoder := json.NewDecoder(strings.NewReader(desiredJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&desired); err != nil {
		return SchemaDiffResult{}, fmt.Errorf("%w: %v", ErrInvalidDesiredSchema, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return SchemaDiffResult{}, fmt.Errorf("%w: expected one JSON object", ErrInvalidDesiredSchema)
	}
	if err := validateSchema(desired, true); err != nil {
		return SchemaDiffResult{}, err
	}
	target, err := s.targets.Get(databaseID)
	if err != nil {
		return SchemaDiffResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	current, err := loadSchema(ctx, target, desired.Schema)
	if err != nil {
		return SchemaDiffResult{}, fmt.Errorf("read current schema: %w", err)
	}
	return DiffSchemas(current, desired)
}

func (s *Service) CurrentSchema(ctx context.Context, userID, databaseID int64, schema string) (string, error) {
	if userID <= 0 || databaseID <= 0 {
		return "", ErrInvalidArgument
	}
	ok, err := s.authz.CanEdit(ctx, userID, databaseID)
	if err != nil {
		return "", fmt.Errorf("check edit access: %w", err)
	}
	if !ok {
		return "", ErrForbidden
	}
	if !validIdentifier(schema) {
		return "", fmt.Errorf("%w: invalid schema name", ErrInvalidDesiredSchema)
	}
	target, err := s.targets.Get(databaseID)
	if err != nil {
		return "", err
	}
	opCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	definition, err := loadSchema(opCtx, target, schema)
	if err != nil {
		return "", fmt.Errorf("read current schema: %w", err)
	}
	encoded, err := json.MarshalIndent(definition, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode current schema: %w", err)
	}
	return string(encoded), nil
}

func loadSchema(ctx context.Context, db *sql.DB, schema string) (SchemaDefinition, error) {
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname = $1)`, schema).Scan(&exists); err != nil {
		return SchemaDefinition{}, err
	}
	if !exists {
		return SchemaDefinition{}, fmt.Errorf("%w: schema %q does not exist", ErrInvalidDesiredSchema, schema)
	}
	result := SchemaDefinition{Schema: schema, Tables: []SchemaTable{}}
	rows, err := db.QueryContext(ctx, `
		SELECT c.relname, a.attname, format_type(a.atttypid, a.atttypmod), NOT a.attnotnull
		FROM pg_catalog.pg_class c
	JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
	LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
		WHERE n.nspname = $1 AND c.relkind IN ('r', 'p')
		ORDER BY c.relname, a.attnum`, schema)
	if err != nil {
		return SchemaDefinition{}, err
	}
	defer rows.Close()
	byName := make(map[string]int)
	for rows.Next() {
		var tableName string
		var columnName, dataType sql.NullString
		var nullable sql.NullBool
		if err := rows.Scan(&tableName, &columnName, &dataType, &nullable); err != nil {
			return SchemaDefinition{}, err
		}
		i, exists := byName[tableName]
		if !exists {
			i = len(result.Tables)
			byName[tableName] = i
			result.Tables = append(result.Tables, SchemaTable{Name: tableName, Columns: []SchemaColumn{}})
		}
		if columnName.Valid {
			result.Tables[i].Columns = append(result.Tables[i].Columns, SchemaColumn{Name: columnName.String, Type: dataType.String, Nullable: nullable.Bool})
		}
	}
	if err := rows.Err(); err != nil {
		return SchemaDefinition{}, err
	}
	return result, nil
}

func validateSchema(schema SchemaDefinition, validateTypes bool) error {
	if !validIdentifier(schema.Schema) {
		return fmt.Errorf("%w: schema name is missing or invalid", ErrInvalidDesiredSchema)
	}
	tables := map[string]bool{}
	for _, table := range schema.Tables {
		if !validIdentifier(table.Name) || tables[table.Name] {
			return fmt.Errorf("%w: duplicate or invalid table name %q", ErrInvalidDesiredSchema, table.Name)
		}
		tables[table.Name] = true
		if len(table.Columns) == 0 {
			return fmt.Errorf("%w: table %q needs at least one column", ErrInvalidDesiredSchema, table.Name)
		}
		columns := map[string]bool{}
		for _, col := range table.Columns {
			if !validIdentifier(col.Name) || columns[col.Name] {
				return fmt.Errorf("%w: duplicate or invalid column name %q", ErrInvalidDesiredSchema, col.Name)
			}
			columns[col.Name] = true
			if validateTypes && !typePattern.MatchString(strings.TrimSpace(col.Type)) {
				return fmt.Errorf("%w: unsupported or invalid PostgreSQL type %q", ErrInvalidDesiredSchema, col.Type)
			}
		}
	}
	return nil
}

func validIdentifier(value string) bool {
	return value != "" && len(value) <= 63 && !strings.ContainsRune(value, 0)
}

func nullableSQL(nullable bool) string {
	if nullable {
		return ""
	}
	return " NOT NULL"
}

func normalizeType(value string) string {
	value = strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
	value = strings.ReplaceAll(value, "character varying", "varchar")
	value = strings.ReplaceAll(value, "timestamp without time zone", "timestamp")
	value = strings.ReplaceAll(value, "time without time zone", "time")
	return value
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
