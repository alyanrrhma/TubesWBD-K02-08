package web

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/access"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/admin"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/database"
)

// AccessChecker dipenuhi access.Service
type AccessChecker interface {
	EffectiveAccess(ctx context.Context, userID, databaseID int64) (access.Level, error)
	Require(ctx context.Context, userID, databaseID int64, needed access.Level) error
}

// ConnectionSource memberi metadata koneksi (nama, host, status aktif). Dipenuhi *admin.Service
type ConnectionSource interface {
	ListConnections(ctx context.Context) ([]admin.Connection, error)
	GetConnection(ctx context.Context, id int64) (*admin.Connection, error)
}

// TargetRegistry dipenuhi *database.Registry 
type TargetRegistry interface {
	Get(databaseID int64) (*sql.DB, error)
}

// Urutan setiap operasi: cek akses efektif -> ambil koneksi target dari Registry -> baca.
type DatabaseViewer struct {
	Access   AccessChecker
	Conns    ConnectionSource
	Registry TargetRegistry
}

const maxCellRunes = 500

func (v DatabaseViewer) ListDatabases(ctx context.Context, userID int64) ([]DatabaseView, error) {
	conns, err := v.Conns.ListConnections(ctx)
	if err != nil {
		return nil, err
	}
	var out []DatabaseView
	for _, c := range conns {
		level, err := v.Access.EffectiveAccess(ctx, userID, c.ID)
		if err != nil {
			return nil, err
		}
		if level != access.NoAccess {
			out = append(out, toView(c, level))
		}
	}
	return out, nil
}

func toView(c admin.Connection, level access.Level) DatabaseView {
	return DatabaseView{ID: c.ID, Name: c.Name, Host: c.Host, Port: c.Port, Database: c.Database, Level: level, Active: c.Active}
}

// authorize memastikan pengguna punya akses ke database dan mengembalikan metadatanya
func (v DatabaseViewer) authorize(ctx context.Context, userID, databaseID int64) (*DatabaseView, error) {
	level, err := v.Access.EffectiveAccess(ctx, userID, databaseID)
	if err != nil {
		return nil, err
	}
	if level == access.NoAccess {
		return nil, access.ErrForbidden
	}
	c, err := v.Conns.GetConnection(ctx, databaseID)
	if err != nil {
		return nil, err
	}
	dv := toView(*c, level)
	return &dv, nil
}

func (v DatabaseViewer) Database(ctx context.Context, userID, databaseID int64) (*DatabaseView, error) {
	return v.authorize(ctx, userID, databaseID)
}

// target mengotorisasi lalu mengembalikan koneksi target aktif
func (v DatabaseViewer) target(ctx context.Context, userID, databaseID int64) (*sql.DB, error) {
	if _, err := v.authorize(ctx, userID, databaseID); err != nil {
		return nil, err
	}
	return v.Registry.Get(databaseID)
}

func (v DatabaseViewer) Schemas(ctx context.Context, userID, databaseID int64) ([]string, error) {
	db, err := v.target(ctx, userID, databaseID)
	if err != nil {
		return nil, err
	}
	schemas, err := database.ListSchemas(ctx, db)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(schemas))
	for i, s := range schemas {
		out[i] = s.Name
	}
	return out, nil
}

func (v DatabaseViewer) Tables(ctx context.Context, userID, databaseID int64, schema string) ([]database.Table, error) {
	db, err := v.target(ctx, userID, databaseID)
	if err != nil {
		return nil, err
	}
	return database.ListTables(ctx, db, schema)
}

func (v DatabaseViewer) TableDetail(ctx context.Context, userID, databaseID int64, schema, table string) (*TableDetailView, error) {
	db, err := v.target(ctx, userID, databaseID)
	if err != nil {
		return nil, err
	}
	cols, err := database.ListColumns(ctx, db, schema, table)
	if err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("%w: %q.%q", database.ErrTableNotFound, schema, table)
	}
	cons, err := database.ListConstraints(ctx, db, schema, table)
	if err != nil {
		return nil, err
	}
	idx, err := database.ListIndexes(ctx, db, schema, table)
	if err != nil {
		return nil, err
	}
	return &TableDetailView{Columns: cols, Constraints: cons, Indexes: idx}, nil
}

func (v DatabaseViewer) TableData(ctx context.Context, userID, databaseID int64, schema, table string, page, pageSize int) (*TableDataView, error) {
	db, err := v.target(ctx, userID, databaseID)
	if err != nil {
		return nil, err
	}
	d, err := database.ReadTableData(ctx, db, schema, table, page, pageSize)
	if err != nil {
		return nil, err
	}
	out := &TableDataView{Columns: d.Columns, Page: d.Page, PageSize: d.PageSize, Total: d.TotalRows}
	for _, row := range d.Rows {
		cells := make([]CellView, len(row))
		for i, val := range row {
			cells[i] = formatCell(val)
		}
		out.Rows = append(out.Rows, cells)
	}
	return out, nil
}

// formatCell mengubah nilai hasil scan ke teks tampilan. NULL ditandai terpisah agar dapat dibedakan dari string kosong atau teks "NULL"
func formatCell(val any) CellView {
	var s string
	switch x := val.(type) {
	case nil:
		return CellView{Null: true}
	case string:
		s = x
	case []byte:
		if utf8.Valid(x) {
			s = string(x)
		} else {
			s = `\x` + hex.EncodeToString(x)
		}
	case time.Time:
		s = x.Format("2006-01-02 15:04:05.999999999 -07:00")
	default:
		s = fmt.Sprint(x)
	}
	if r := []rune(s); len(r) > maxCellRunes {
		s = string(r[:maxCellRunes]) + "…"
	}
	return CellView{Text: strings.ToValidUTF8(s, "\uFFFD")}
}