// Tes input tidak valid, nilai tidak ada, database nil
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestValidateIdentifier(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "empty", wantErr: true},
		{name: "maximum length", value: strings.Repeat("a", maxIdentifierBytes)},
		{name: "too long", value: strings.Repeat("a", maxIdentifierBytes+1), wantErr: true},
		{name: "NUL", value: "table\x00name", wantErr: true},
		{name: "invalid UTF-8", value: string([]byte{0xff}), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIdentifier("table", tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateIdentifier() error = %v, wantErr %t", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("expected ErrInvalidArgument, got %v", err)
			}
		})
	}
}

func TestViewerValidationErrorsAreSentinels(t *testing.T) {
	db := &sql.DB{}
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "ListTables schema",
			call: func() error {
				_, err := ListTables(ctx, db, "")
				return err
			},
		},
		{
			name: "ListColumns schema",
			call: func() error {
				_, err := ListColumns(ctx, db, "", "items")
				return err
			},
		},
		{
			name: "ListColumns table",
			call: func() error {
				_, err := ListColumns(ctx, db, "public", "")
				return err
			},
		},
		{
			name: "ListConstraints identifiers",
			call: func() error {
				_, err := ListConstraints(ctx, db, "", "items")
				return err
			},
		},
		{
			name: "ListIndexes identifiers",
			call: func() error {
				_, err := ListIndexes(ctx, db, "public", "")
				return err
			},
		},
		{
			name: "ReadTableData identifiers",
			call: func() error {
				_, err := ReadTableData(ctx, db, "", "items", 1, 20)
				return err
			},
		},
		{
			name: "ReadTableData page",
			call: func() error {
				_, err := ReadTableData(ctx, db, "public", "items", 0, 20)
				return err
			},
		},
		{
			name: "ReadTableData page size",
			call: func() error {
				_, err := ReadTableData(ctx, db, "public", "items", 1, 101)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("expected ErrInvalidArgument, got %v", err)
			}
		})
	}
}

func TestViewerRejectsInvalidIdentifiers(t *testing.T) {
	db := &sql.DB{}
	ctx := context.Background()
	tooLong := strings.Repeat("a", maxIdentifierBytes+1)
	invalidUTF8 := string([]byte{0xff})
	tests := []struct {
		name string
		call func(schema, table string) error
	}{
		{
			name: "ListTables",
			call: func(schema, _ string) error {
				_, err := ListTables(ctx, db, schema)
				return err
			},
		},
		{
			name: "ListColumns",
			call: func(schema, table string) error {
				_, err := ListColumns(ctx, db, schema, table)
				return err
			},
		},
		{
			name: "ListConstraints",
			call: func(schema, table string) error {
				_, err := ListConstraints(ctx, db, schema, table)
				return err
			},
		},
		{
			name: "ListIndexes",
			call: func(schema, table string) error {
				_, err := ListIndexes(ctx, db, schema, table)
				return err
			},
		},
		{
			name: "ReadTableData",
			call: func(schema, table string) error {
				_, err := ReadTableData(ctx, db, schema, table, 1, 20)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/overlong schema", func(t *testing.T) {
			if err := tt.call(tooLong, "items"); !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("expected ErrInvalidArgument, got %v", err)
			}
		})
		t.Run(tt.name+"/invalid UTF-8 schema", func(t *testing.T) {
			if err := tt.call(invalidUTF8, "items"); !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("expected ErrInvalidArgument, got %v", err)
			}
		})
		if tt.name != "ListTables" {
			t.Run(tt.name+"/NUL table", func(t *testing.T) {
				if err := tt.call("public", "items\x00"); !errors.Is(err, ErrInvalidArgument) {
					t.Errorf("expected ErrInvalidArgument, got %v", err)
				}
			})
			t.Run(tt.name+"/invalid UTF-8 table", func(t *testing.T) {
				if err := tt.call("public", invalidUTF8); !errors.Is(err, ErrInvalidArgument) {
					t.Errorf("expected ErrInvalidArgument, got %v", err)
				}
			})
		}
	}
}

func TestWrapQueryError(t *testing.T) {
	tests := []struct {
		code string
		want error
	}{
		{"42501", ErrTargetPermissionDenied},
		{"42P01", ErrTableNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			err := wrapQueryError("test query", &pgconn.PgError{Code: tt.code})
			if !errors.Is(err, tt.want) {
				t.Errorf("expected %v, got %v", tt.want, err)
			}
		})
	}
}

func TestViewerRejectsNilDB(t *testing.T) {
	_, ctx, schema := createViewerTestTable(t)

	if _, err := ListSchemas(ctx, nil); err == nil {
		t.Error("ListSchemas should reject nil DB")
	}
	if _, err := ListTables(ctx, nil, schema); err == nil {
		t.Error("ListTables should reject nil DB")
	}
	if _, err := ListColumns(ctx, nil, schema, "items"); err == nil {
		t.Error("ListColumns should reject nil DB")
	}
	if _, err := ListConstraints(ctx, nil, schema, "items"); err == nil {
		t.Error("ListConstraints should reject nil DB")
	}
	if _, err := ListIndexes(ctx, nil, schema, "items"); err == nil {
		t.Error("ListIndexes should reject nil DB")
	}
	if _, err := ReadTableData(
		ctx, nil, schema, "items", 1, 20,
	); err == nil {
		t.Error("ReadTableData should reject nil DB")
	}
}

func TestViewerRejectsEmptyIdentifiers(t *testing.T) {
	db, ctx, schema := createViewerTestTable(t)

	if _, err := ListTables(ctx, db, ""); err == nil {
		t.Error("ListTables should reject empty schema")
	} else if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("ListTables empty schema: expected ErrInvalidArgument, got %v", err)
	}
	if _, err := ListColumns(ctx, db, "", "items"); err == nil {
		t.Error("ListColumns should reject empty schema")
	} else if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("ListColumns empty schema: expected ErrInvalidArgument, got %v", err)
	}
	if _, err := ListColumns(ctx, db, schema, ""); err == nil {
		t.Error("ListColumns should reject empty table")
	} else if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("ListColumns empty table: expected ErrInvalidArgument, got %v", err)
	}
	if _, err := ListConstraints(ctx, db, "", "items"); err == nil {
		t.Error("ListConstraints should reject empty schema")
	} else if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("ListConstraints empty schema: expected ErrInvalidArgument, got %v", err)
	}
	if _, err := ListIndexes(ctx, db, schema, ""); err == nil {
		t.Error("ListIndexes should reject empty table")
	} else if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("ListIndexes empty table: expected ErrInvalidArgument, got %v", err)
	}
	if _, err := ReadTableData(
		ctx, db, "", "items", 1, 20,
	); err == nil {
		t.Error("ReadTableData should reject empty schema")
	} else if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("ReadTableData empty schema: expected ErrInvalidArgument, got %v", err)
	}
}

func TestReadTableDataRejectsMissingTable(t *testing.T) {
	db, ctx, schema := createViewerTestTable(t)

	_, err := ReadTableData(
		ctx, db, schema, "table_yang_tidak_ada", 1, 20,
	)
	if !errors.Is(err, ErrTableNotFound) {
		t.Fatalf("expected ErrTableNotFound, got %v", err)
	}
}

func TestReadTableDataUsesReadOnlyTransaction(t *testing.T) {
	db, ctx, schema := createViewerTestTable(t)

	qualifiedSchema := quoteIdentifier(schema)
	functionName := qualifiedSchema + "." + quoteIdentifier("write_attempt")
	itemsTable := qualifiedSchema + "." + quoteIdentifier("items")
	function := fmt.Sprintf(
		`CREATE FUNCTION %s() RETURNS integer LANGUAGE plpgsql VOLATILE AS $function$
		BEGIN
			UPDATE %s SET nama = nama WHERE id = 1;
			RETURN 1;
		END
		$function$`,
		functionName,
		itemsTable,
	)
	if _, err := db.ExecContext(ctx, function); err != nil {
		t.Fatalf("failed to create write-attempt function: %v", err)
	}

	view := qualifiedSchema + "." + quoteIdentifier("write_attempt_view")
	if _, err := db.ExecContext(
		ctx,
		"CREATE VIEW "+view+" AS SELECT "+functionName+"() AS value",
	); err != nil {
		t.Fatalf("failed to create write-attempt view: %v", err)
	}

	_, err := ReadTableData(ctx, db, schema, "write_attempt_view", 1, 20)
	if err == nil {
		t.Fatal("expected the write inside the view to be rejected")
	}

	if !errors.Is(err, ErrWriteRejected) {
		t.Fatalf("expected ErrWriteRejected, got %v", err)
	}
}

func TestReadTableDataBeyondLastPage(t *testing.T) {
	db, ctx, schema := createViewerTestTable(t)

	result, err := ReadTableData(
		ctx, db, schema, "items", 999, 20,
	)
	if err != nil {
		t.Fatalf("expected an empty page, got error: %v", err)
	}
	if len(result.Rows) != 0 {
		t.Errorf("expected no rows on page 999, got %d", len(result.Rows))
	}
	if result.TotalRows != 2 {
		t.Errorf("expected total rows 2, got %d", result.TotalRows)
	}

	result, err = ReadTableData(
		ctx, db, schema, "items", 999999999, 20,
	)
	if err != nil {
		t.Fatalf("expected a very large page number to return an empty page, got error: %v", err)
	}
	if len(result.Rows) != 0 {
		t.Errorf("expected no rows on a very large page, got %d", len(result.Rows))
	}
	if result.TotalRows != 2 {
		t.Errorf("expected total rows 2 for a very large page, got %d", result.TotalRows)
	}
}

func TestReadTableDataPageSizeBoundaries(t *testing.T) {
	db, ctx, schema := createViewerTestTable(t)

	for _, pageSize := range []int{1, 100} {
		t.Run(fmt.Sprintf("page_size_%d", pageSize), func(t *testing.T) {
			_, err := ReadTableData(
				ctx, db, schema, "items", 1, pageSize,
			)
			if err != nil {
				t.Errorf("page size %d should be accepted: %v", pageSize, err)
			}
		})
	}
}
