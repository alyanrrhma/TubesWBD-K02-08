// Test ListSchemas, ListConstaints, ListIndexes
package database

import (
	"testing"
)

func TestListSchemas(t *testing.T) {
	db, ctx := openTestDB(t)

	schemas, err := ListSchemas(ctx, db)
	if err != nil {
		t.Fatalf("ListSchemas failed: %v", err)
	}

	for _, schema := range schemas {
		if schema.Name == "public" {
			return
		}
	}
	t.Fatal("expected schema public to exist")
}

func TestListConstraints(t *testing.T) {
    db, ctx, schema := createViewerTestTable(t)

    constraints, err := ListConstraints(ctx, db, schema, "items")
    if err != nil {
        t.Fatalf("ListConstraints failed: %v", err)
    }

    for _, c := range constraints {
        if c.Type == "PRIMARY KEY" &&
            c.Definition == "PRIMARY KEY (id)" {
            return
        }
    }

    t.Fatal("expected primary key on items.id")
}

func TestListIndexes(t *testing.T) {
	db, ctx, schema := createViewerTestTable(t)

	indexes, err := ListIndexes(ctx, db, schema, "items")
	if err != nil {
		t.Fatalf("ListIndexes failed: %v", err)
	}

	for _, idx := range indexes {
		if idx.IsPrimary {
			return
		}
	}
	t.Fatal("expected primary-key index on items")
}