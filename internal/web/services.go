package web

import (
	"context"
	"time"

	"github.com/alyanrrhma/TubesWBD-K02-08/internal/access"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/admin"
	"github.com/alyanrrhma/TubesWBD-K02-08/internal/database"
)

// AdminService dipenuhi *admin.Service. Interface ada agar handler dapat diuji tanpa database
type AdminService interface {
	ListUsers(ctx context.Context) ([]admin.User, error)
	GetUser(ctx context.Context, id int64) (*admin.User, error)
	CreateUser(ctx context.Context, username, password string, isAdmin bool) (*admin.User, error)
	DeleteUser(ctx context.Context, id int64) error

	ListGroups(ctx context.Context) ([]admin.Group, error)
	GetGroup(ctx context.Context, id int64) (*admin.Group, error)
	CreateGroup(ctx context.Context, name string) (*admin.Group, error)
	DeleteGroup(ctx context.Context, id int64) error
	ListMembers(ctx context.Context, groupID int64) ([]admin.User, error)
	AddMember(ctx context.Context, groupID, userID int64) error
	RemoveMember(ctx context.Context, groupID, userID int64) error
	ListGroupAccess(ctx context.Context, groupID int64) ([]admin.GroupAccess, error)
	GrantAccess(ctx context.Context, groupID, databaseID int64, level access.Level) error
	RevokeAccess(ctx context.Context, groupID, databaseID int64) error

	ListConnections(ctx context.Context) ([]admin.Connection, error)
	CreateConnection(ctx context.Context, in admin.ConnectionInput) (*admin.Connection, error)
	UpdateConnection(ctx context.Context, id int64, in admin.ConnectionInput) (*admin.Connection, error)
	DeleteConnection(ctx context.Context, id int64) error
	TestConnection(ctx context.Context, id int64) error
}

// viewer

type DatabaseView struct {
	ID       int64
	Name     string
	Host     string
	Port     int
	Database string
	Level    access.Level
	Active   bool
}

type CellView struct {
	Text string
	Null bool
}

type TableDataView struct {
	Columns  []string
	Rows     [][]CellView
	Page     int
	PageSize int
	Total    int
}

type TableDetailView struct {
	Columns     []database.Column
	Constraints []database.Constraint
	Indexes     []database.Index
}

// Viewer adalah satu-satunya pintu halaman viewer ke data target. Setiap method menerima userID dan WAJIB memeriksa akses (access.ErrForbidden bila ditolak)
// Implementasi nyata: DatabaseViewer (viewer_service.go)
type Viewer interface {
	ListDatabases(ctx context.Context, userID int64) ([]DatabaseView, error)
	Database(ctx context.Context, userID, databaseID int64) (*DatabaseView, error)
	Schemas(ctx context.Context, userID, databaseID int64) ([]string, error)
	Tables(ctx context.Context, userID, databaseID int64, schema string) ([]database.Table, error)
	TableDetail(ctx context.Context, userID, databaseID int64, schema, table string) (*TableDetailView, error)
	TableData(ctx context.Context, userID, databaseID int64, schema, table string, page, pageSize int) (*TableDataView, error)
}

// migration

const (
	MigrationSuccess = "SUCCESS"
	MigrationFailed  = "FAILED"
)

type MigrationRecord struct {
	ID           int64
	Version      int
	SQL          string
	ExecutedBy   string
	ExecutedAt   time.Time
	Status       string
	ErrorMessage string
}

type Migrations interface {
	History(ctx context.Context, userID, databaseID int64, page, pageSize int) ([]MigrationRecord, int, error)
	Detail(ctx context.Context, userID, databaseID, version int64) (*MigrationRecord, error)
	Explain(ctx context.Context, userID, databaseID int64, sql string) (string, error)
	Execute(ctx context.Context, userID, databaseID int64, sql string) (*MigrationRecord, error)
}
