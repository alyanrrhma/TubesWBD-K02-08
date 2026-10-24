//Tes input tidak valid, nilai tidak ada, database nil
package database

import (
	"testing"
	"fmt"
)

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
	}
	if _, err := ListColumns(ctx, db, "", "items"); err == nil {
		t.Error("ListColumns should reject empty schema")
	}
	if _, err := ListColumns(ctx, db, schema, ""); err == nil {
		t.Error("ListColumns should reject empty table")
	}
	if _, err := ListConstraints(ctx, db, "", "items"); err == nil {
		t.Error("ListConstraints should reject empty schema")
	}
	if _, err := ListIndexes(ctx, db, schema, ""); err == nil {
		t.Error("ListIndexes should reject empty table")
	}
	if _, err := ReadTableData(
		ctx, db, "", "items", 1, 20,
	); err == nil {
		t.Error("ReadTableData should reject empty schema")
	}
}

func TestReadTableDataRejectsMissingTable(t *testing.T) {
	db, ctx, schema := createViewerTestTable(t)

	_, err := ReadTableData(
		ctx, db, schema, "table_yang_tidak_ada", 1, 20,
	)
	if err == nil {
		t.Fatal("expected an error for a missing table")
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
