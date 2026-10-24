package web

import (
	"net/http"
	"strconv"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/database"
)

const dataPageSize = 50

type databasePageData struct {
	DB       *DatabaseView
	Schemas  []string
	Selected string
	Tables   []database.Table
}

func (s *Server) databasePage(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "databaseID")
	if !ok {
		return
	}
	ctx, uid := r.Context(), currentUser(r).ID

	db, err := s.Viewer.Database(ctx, uid, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	schemas, err := s.Viewer.Schemas(ctx, uid, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	selected := pickSchema(schemas, r.URL.Query().Get("schema"))
	var tables []database.Table
	if selected != "" {
		if tables, err = s.Viewer.Tables(ctx, uid, id, selected); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	s.render(w, r, http.StatusOK, "database", db.Name, "dashboard",
		databasePageData{DB: db, Schemas: schemas, Selected: selected, Tables: tables})
}

// pickSchema memakai pilihan pengguna bila valid, lalu "public", lalu yang pertama.
func pickSchema(schemas []string, want string) string {
	for _, s := range schemas {
		if s == want {
			return s
		}
	}
	for _, s := range schemas {
		if s == "public" {
			return s
		}
	}
	if len(schemas) > 0 {
		return schemas[0]
	}
	return ""
}

type tablePageData struct {
	DB     *DatabaseView
	Schema string
	Table  string
	Tab    string // "structure" | "data"

	Detail *TableDetailView

	Data     *TableDataView
	Pages    int
	PrevPage int
	NextPage int
	From, To int
}

func (s *Server) tableStructure(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "databaseID")
	if !ok {
		return
	}
	ctx, uid := r.Context(), currentUser(r).ID
	schema, table := r.PathValue("schema"), r.PathValue("table")

	db, err := s.Viewer.Database(ctx, uid, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	detail, err := s.Viewer.TableDetail(ctx, uid, id, schema, table)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "table", schema+"."+table, "dashboard",
		tablePageData{DB: db, Schema: schema, Table: table, Tab: "structure", Detail: detail})
}

func (s *Server) tableData(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "databaseID")
	if !ok {
		return
	}
	ctx, uid := r.Context(), currentUser(r).ID
	schema, table := r.PathValue("schema"), r.PathValue("table")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	db, err := s.Viewer.Database(ctx, uid, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	data, err := s.Viewer.TableData(ctx, uid, id, schema, table, page, dataPageSize)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	pages := (data.Total + dataPageSize - 1) / dataPageSize
	if pages < 1 {
		pages = 1
	}
	if page > pages { // halaman di luar jangkauan -> halaman terakhir
		http.Redirect(w, r, r.URL.Path+"?page="+strconv.Itoa(pages), http.StatusSeeOther)
		return
	}
	pd := tablePageData{DB: db, Schema: schema, Table: table, Tab: "data", Data: data, Pages: pages}
	if page > 1 {
		pd.PrevPage = page - 1
	}
	if page < pages {
		pd.NextPage = page + 1
	}
	if len(data.Rows) > 0 {
		pd.From = (page-1)*dataPageSize + 1
		pd.To = pd.From + len(data.Rows) - 1
	}
	s.render(w, r, http.StatusOK, "table", schema+"."+table, "dashboard", pd)
}