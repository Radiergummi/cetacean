package timeline

import (
	"database/sql"
	"net/url"
	"testing"
)

// openRaw opens the database as another program would, with the locking the
// store uses, for a test to change it behind the store's back.
func openRaw(t *testing.T, path string) *sql.DB {
	t.Helper()

	query := url.Values{
		"vfs":     {ownerOnlyVFS},
		"_pragma": {"locking_mode(EXCLUSIVE)", "journal_mode(WAL)"},
	}

	db, err := sql.Open("sqlite3", "file:"+path+"?"+query.Encode())
	if err != nil {
		t.Fatal(err)
	}

	db.SetMaxOpenConns(1)

	return db
}

var OpenRaw = openRaw
