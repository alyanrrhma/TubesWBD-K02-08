package web

import (
	"errors"
	"net/http"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/access"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/migration"
)

type schemaDiffPageData struct {
	DB       *DatabaseView
	Schemas  []string
	Selected string
	Desired  string
	Result   *migration.SchemaDiffResult
	Failure  string
	Example  string
}

const desiredSchemaExample = `{
  "schema": "public",
  "tables": [
    {
      "name": "customers",
      "columns": [
        {"name": "id", "type": "bigint", "nullable": false},
        {"name": "email", "type": "text", "nullable": false},
        {"name": "phone", "type": "text", "nullable": true}
      ]
    }
  ]
}`

func (s *Server) schemaDiffPage(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "databaseID")
	if !ok {
		return
	}
	db, err := s.Viewer.Database(r.Context(), currentUser(r).ID, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	data := schemaDiffPageData{DB: db, Example: desiredSchemaExample}
	if db.Level >= access.ViewEdit {
		schemas, err := s.Viewer.Schemas(r.Context(), currentUser(r).ID, id)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		data.Schemas = schemas
		data.Selected = pickSchema(schemas, r.URL.Query().Get("schema"))
		if data.Selected != "" {
			data.Desired, err = s.Migrations.CurrentSchema(r.Context(), currentUser(r).ID, id, data.Selected)
			if err != nil {
				s.fail(w, r, err)
				return
			}
		}
	}
	s.render(w, r, http.StatusOK, "schema_diff", "Schema diff · "+db.Name, "dashboard", data)
}

func (s *Server) schemaDiffSubmit(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "databaseID")
	if !ok {
		return
	}
	userID := currentUser(r).ID
	db, err := s.Viewer.Database(r.Context(), userID, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	desired := r.PostFormValue("desired_schema")
	result, err := s.Migrations.DiffSchema(r.Context(), userID, id, desired)
	if err != nil {
		switch {
		case errors.Is(err, access.ErrForbidden):
			s.fail(w, r, err)
		case errors.Is(err, migration.ErrDesiredSchemaTooLarge):
			s.errorPage(w, r, http.StatusRequestEntityTooLarge, "Schema terlalu besar", "Definisi schema maksimal 256 KiB.")
		case errors.Is(err, migration.ErrInvalidDesiredSchema):
			s.render(w, r, http.StatusUnprocessableEntity, "schema_diff", "Schema diff · "+db.Name, "dashboard", schemaDiffPageData{DB: db, Desired: desired, Failure: err.Error(), Example: desiredSchemaExample})
		default:
			s.fail(w, r, err)
		}
		return
	}
	s.render(w, r, http.StatusOK, "schema_diff", "Schema diff · "+db.Name, "dashboard", schemaDiffPageData{DB: db, Desired: desired, Result: &result, Example: desiredSchemaExample})
}
