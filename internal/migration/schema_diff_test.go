package migration

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDiffSchemasGeneratesReviewableDDL(t *testing.T) {
	current := SchemaDefinition{Schema: "public", Tables: []SchemaTable{
		{Name: "customers", Columns: []SchemaColumn{
			{Name: "id", Type: "bigint", Nullable: false},
			{Name: "legacy", Type: "text", Nullable: true},
			{Name: "state", Type: "varchar(12)", Nullable: true},
		}},
		{Name: "old_table", Columns: []SchemaColumn{{Name: "id", Type: "integer", Nullable: false}}},
	}}
	desired := SchemaDefinition{Schema: "public", Tables: []SchemaTable{
		{Name: "customers", Columns: []SchemaColumn{
			{Name: "id", Type: "bigint", Nullable: false},
			{Name: "state", Type: "text", Nullable: false},
			{Name: "phone", Type: "text", Nullable: true},
		}},
		{Name: "new_table", Columns: []SchemaColumn{{Name: "name", Type: "text", Nullable: true}}},
	}}

	result, err := DiffSchemas(current, desired)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`CREATE TABLE "public"."new_table"`,
		`ALTER TABLE "public"."customers" ADD COLUMN "phone" text;`,
		`ALTER TABLE "public"."customers" ALTER COLUMN "state" TYPE text USING "state"::text;`,
		`ALTER TABLE "public"."customers" ALTER COLUMN "state" SET NOT NULL;`,
		`ALTER TABLE "public"."customers" DROP COLUMN "legacy";`,
		`DROP TABLE "public"."old_table";`,
	} {
		if !strings.Contains(result.SQL, expected) {
			t.Errorf("candidate SQL missing %q:\n%s", expected, result.SQL)
		}
	}
	destructive := 0
	for _, change := range result.Changes {
		if change.Destructive {
			destructive++
		}
	}
	if destructive != 2 {
		t.Fatalf("want two destructive changes flagged, got %d", destructive)
	}
}

func TestDiffSchemasQuotesIdentifiersAndRejectsUnsafeTypes(t *testing.T) {
	current := SchemaDefinition{Schema: `s"afe`, Tables: []SchemaTable{}}
	desired := SchemaDefinition{Schema: `s"afe`, Tables: []SchemaTable{{Name: `t"able`, Columns: []SchemaColumn{{Name: `c"ol`, Type: "text", Nullable: true}}}}}
	result, err := DiffSchemas(current, desired)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.SQL, `"s""afe"."t""able"`) || !strings.Contains(result.SQL, `"c""ol"`) {
		t.Fatalf("identifiers were not quoted: %s", result.SQL)
	}
	desired.Tables[0].Columns[0].Type = "text); DROP TABLE customers; --"
	if _, err := DiffSchemas(current, desired); !errors.Is(err, ErrInvalidDesiredSchema) {
		t.Fatalf("unsafe type should be rejected, got %v", err)
	}
}

func TestDiffSchemaValidatesBeforeTargetLookup(t *testing.T) {
	service := NewService(nil, fakeTargets{}, fakeAuthz{edit: true})
	if _, err := service.DiffSchema(context.Background(), 1, 2, `{not json`); !errors.Is(err, ErrInvalidDesiredSchema) {
		t.Fatalf("invalid JSON should fail before opening target, got %v", err)
	}
	viewOnly := NewService(nil, fakeTargets{}, fakeAuthz{view: true})
	if _, err := viewOnly.DiffSchema(context.Background(), 1, 2, `{}`); !errors.Is(err, ErrForbidden) {
		t.Fatalf("view-only user must be rejected before schema validation, got %v", err)
	}
}
