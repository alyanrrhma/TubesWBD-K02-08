package access

import (
	"context"
	"database/sql"
)

type PostgresRepository struct{ DB *sql.DB }

func (p PostgresRepository) GrantsForUser(ctx context.Context, userID, databaseID int64) ([]Grant, error) {
	rows, e := p.DB.QueryContext(ctx, `SELECT g.group_id,g.database_id,g.access_level FROM group_database_access g JOIN user_groups ug ON ug.group_id=g.group_id WHERE ug.user_id=$1 AND g.database_id=$2`, userID, databaseID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Grant{}
	for rows.Next() {
		var x Grant
		var level string
		if e = rows.Scan(&x.GroupID, &x.DatabaseID, &level); e != nil {
			return nil, e
		}
		x.Level, e = ParseLevel(level)
		if e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
