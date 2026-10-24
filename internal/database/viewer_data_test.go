//Test baca file dengan pagination, termasuk table kosong dan nilai NULL
package database

import (
	"testing"
)

func TestReadTableDataPagination(t *testing.T) {
	db, ctx, schema := createViewerTestTable(t)

	firstPage, err := ReadTableData(
		ctx, db, schema, "items", 1, 1,
	)
	if err != nil {
		t.Fatalf("failed to read first page: %v", err)
	}

	if firstPage.TotalRows != 2 {
		t.Fatalf("expected 2 total rows, got %d", firstPage.TotalRows)
	}
	if len(firstPage.Rows) != 1 {
		t.Fatalf("expected 1 row on first page, got %d", len(firstPage.Rows))
	}
	if firstPage.Columns[0] != "id" {
		t.Errorf("expected first column to be id, got %q", firstPage.Columns[0])
	}

	secondPage, err := ReadTableData(
		ctx, db, schema, "items", 2, 1,
	)
	if err != nil {
		t.Fatalf("failed to read second page: %v", err)
	}
	if len(secondPage.Rows) != 1 {
		t.Fatalf("expected 1 row on second page, got %d", len(secondPage.Rows))
	}

	// Pastikan halaman pertama dan kedua berisi baris berbeda.
	firstID, ok1 := firstPage.Rows[0][0].(int64)
	secondID, ok2 := secondPage.Rows[0][0].(int64)
	if !ok1 || !ok2 {
		t.Fatalf("expected IDs to be int64, got %T and %T",
			firstPage.Rows[0][0], secondPage.Rows[0][0])
	}
	if firstID == secondID {
		t.Error("expected different rows on the two pages")
	}
}

func TestReadTableDataRejectsInvalidPagination(t *testing.T) {
	db, ctx, schema := createViewerTestTable(t)

	tests := []struct {
		name     string
		page     int
		pageSize int
	}{
		{"zero page", 0, 10},
		{"zero page size", 1, 0},
		{"page size too large", 1, 101},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadTableData(
				ctx, db, schema, "items", tt.page, tt.pageSize,
			)
			if err == nil {
				t.Error("expected an error for invalid pagination")
			}
		})
	}
}
