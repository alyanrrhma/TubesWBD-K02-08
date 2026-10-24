// Test ListTables dan ListColumns
package database

import (
	"testing"
)

func TestListTables(t *testing.T) {
	db, ctx, schema := createViewerTestTable(t)

	tables, err := ListTables(ctx, db, schema)
	if err != nil {
		t.Fatalf("failed to list tables: %v", err)
	}

	found := false
	for _, table := range tables {
		if table.Name == "items" {
			found = true
			break
		}
	}

	if !found {
		t.Fatal("expected table items to exist")
	}
}

func TestListColumns(t *testing.T) {
	db, ctx, schema := createViewerTestTable(t)

	columns, err := ListColumns(ctx, db, schema, "items")
	if err != nil {
		t.Fatalf("failed to list columns: %v", err)
	}

	expected := []string{"id", "nama", "jurusan"}
	if len(columns) != len(expected) {
		t.Fatalf("expected %d columns, got %d", len(expected), len(columns))
	}

	for i, name := range expected {
		if columns[i].Name != name {
			t.Errorf("column %d: expected %q, got %q", i, name, columns[i].Name)
		}
	}

	if columns[0].IsNullable {
		// Kolom id adalah PRIMARY KEY sehingga tidak boleh NULL.
		t.Error("expected id column to be NOT NULL")
	}
}
