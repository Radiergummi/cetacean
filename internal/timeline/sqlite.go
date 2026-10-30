package timeline

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/driver" // registers "sqlite3"
	"github.com/ncruces/go-sqlite3/vfs"

	"github.com/radiergummi/cetacean/internal/cache"
)

//go:embed migrations/*.sql
var migrations embed.FS

// kindChange is the only kind this version records, restores and retains;
// rows of kinds a newer binary added are left alone.
const kindChange = "change"

// ErrHeld is OpenSQLite's error when another process holds the database.
var ErrHeld = errors.New("the timeline is held by another process")

// SQLite is the Store on a local SQLite database, owned by one process.
type SQLite struct {
	db *sql.DB

	// coverage is this process's coverage row, zero until OpenCoverage.
	coverage int64
}

var _ Store = (*SQLite)(nil)

// OpenSQLite opens and migrates the database at path. It fails when another
// process holds it: EXCLUSIVE locking keeps no shared-memory file, so a data
// directory on NFS works, at the price of a second process being refused.
func OpenSQLite(ctx context.Context, path string) (*SQLite, error) {
	query := url.Values{
		"vfs":     {ownerOnlyVFS},
		"_txlock": {"immediate"},
		"_pragma": {"locking_mode(EXCLUSIVE)", "journal_mode(WAL)", "synchronous(FULL)"},
	}

	db, err := sql.Open("sqlite3", "file:"+(&url.URL{Path: path}).EscapedPath()+"?"+query.Encode())
	if err != nil {
		return nil, err
	}

	// The lock belongs to the connection, so there must be exactly one.
	db.SetMaxOpenConns(1)

	if err := migrate(ctx, db); err != nil {
		_ = db.Close()

		if errors.Is(err, sqlite3.BUSY) {
			return nil, fmt.Errorf("timeline %s: %w: %w", path, ErrHeld, err)
		}

		return nil, fmt.Errorf("timeline %s: %w", path, err)
	}

	return &SQLite{db: db}, nil
}

// migrate applies every embedded migration not yet recorded, each in its own
// transaction. Its first transaction is a write, which takes the lock the
// process then holds. A version this binary does not know is left alone.
func migrate(ctx context.Context, db *sql.DB) error {
	applied := map[int64]bool{}

	err := inTx(ctx, db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL
		)`); err != nil {
			return err
		}

		rows, err := tx.QueryContext(ctx, `SELECT version FROM schema_migrations`)
		if err != nil {
			return err
		}
		defer rows.Close() //nolint:errcheck // rows.Err is checked

		for rows.Next() {
			var version int64
			if err := rows.Scan(&version); err != nil {
				return err
			}

			applied[version] = true
		}

		return rows.Err()
	})
	if err != nil {
		return err
	}

	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}

	slices.Sort(files)

	for _, file := range files {
		number, _, _ := strings.Cut(path.Base(file), "_")

		version, err := strconv.ParseInt(number, 10, 64)
		if err != nil {
			return fmt.Errorf("migration %s: %w", file, err)
		}

		if applied[version] {
			continue
		}

		body, err := migrations.ReadFile(file)
		if err != nil {
			return err
		}

		err = inTx(ctx, db, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, string(body)); err != nil {
				return err
			}

			_, err := tx.ExecContext(
				ctx,
				`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
				version,
				time.Now().UnixNano(),
			)

			return err
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", file, err)
		}
	}

	return nil
}

func (s *SQLite) Restore(ctx context.Context, limit int) ([]cache.HistoryEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, at, type, action, resource_id, name, summary,
			service, stack, service_id,
			actor_subject, actor_name, actor_provider, actor_client, actor_transport
		FROM timeline WHERE kind = ? ORDER BY id DESC LIMIT ?`, kindChange, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // rows.Err is checked

	var entries []cache.HistoryEntry

	for rows.Next() {
		var (
			e                                               cache.HistoryEntry
			at                                              int64
			typ, action, resourceID, name, summary          sql.NullString
			service, stack, serviceID                       sql.NullString
			subject, actorName, provider, client, transport sql.NullString
		)

		if err := rows.Scan(
			&e.ID, &at, &typ, &action, &resourceID, &name, &summary,
			&service, &stack, &serviceID,
			&subject, &actorName, &provider, &client, &transport,
		); err != nil {
			return nil, err
		}

		e.Timestamp = time.Unix(0, at)
		e.Type = cache.EventType(typ.String)
		e.Action = action.String
		e.ResourceID = resourceID.String
		e.Name = name.String
		e.Summary = summary.String
		e.Service = service.String
		e.Stack = stack.String
		e.ServiceID = serviceID.String

		if subject.Valid {
			e.Actor = &cache.Actor{
				Subject:   subject.String,
				Name:      actorName.String,
				Provider:  provider.String,
				Client:    client.String,
				Transport: cache.Transport(transport.String),
			}
		}

		entries = append(entries, e)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	slices.Reverse(entries)

	return entries, nil
}

func (s *SQLite) OpenCoverage(
	ctx context.Context,
	at time.Time,
) (previous time.Time, ok bool, err error) {
	err = inTx(ctx, s.db, func(tx *sql.Tx) error {
		var seenAt int64

		err := tx.QueryRowContext(
			ctx,
			`SELECT seen_at FROM coverage ORDER BY id DESC LIMIT 1`,
		).Scan(&seenAt)

		switch {
		case err == nil:
			previous, ok = time.Unix(0, seenAt), true
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}

		return tx.QueryRowContext(
			ctx,
			`INSERT INTO coverage (id, started_at, seen_at)
				SELECT coalesce(max(id), 0) + 1, ?, ? FROM coverage
				RETURNING id`,
			at.UnixNano(),
			at.UnixNano(),
		).Scan(&s.coverage)
	})
	if err != nil {
		s.coverage = 0
		return time.Time{}, false, err
	}

	return previous, ok, nil
}

func (s *SQLite) Commit(ctx context.Context, batch Batch) error {
	return inTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := insert(ctx, tx, batch.Entries); err != nil {
			return err
		}

		if s.coverage != 0 && !batch.SeenAt.IsZero() {
			if _, err := tx.ExecContext(
				ctx,
				`UPDATE coverage SET seen_at = ? WHERE id = ?`,
				batch.SeenAt.UnixNano(),
				s.coverage,
			); err != nil {
				return err
			}
		}

		if batch.Keep <= 0 {
			return nil
		}

		// With fewer than Keep entries the subquery is NULL and deletes nothing.
		if _, err := tx.ExecContext(
			ctx,
			`DELETE FROM timeline WHERE kind = ?1 AND id < (
				SELECT id FROM timeline WHERE kind = ?1 ORDER BY id DESC LIMIT 1 OFFSET ?2
			)`,
			kindChange,
			batch.Keep-1,
		); err != nil {
			return err
		}

		// The newest row stays: it is where the next gap starts.
		_, err := tx.ExecContext(ctx, `DELETE FROM coverage
			WHERE seen_at < (SELECT min(at) FROM timeline WHERE kind = ?)
				AND id < (SELECT max(id) FROM coverage)`, kindChange)

		return err
	})
}

func insert(ctx context.Context, tx *sql.Tx, entries []cache.HistoryEntry) error {
	if len(entries) == 0 {
		return nil
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO timeline (
			id, kind, at, type, action, resource_id, name, summary,
			service, stack, service_id,
			actor_subject, actor_name, actor_provider, actor_client, actor_transport
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO NOTHING`)
	if err != nil {
		return err
	}
	defer stmt.Close() //nolint:errcheck // closed with the transaction regardless

	for _, e := range entries {
		var subject, name, provider, client, transport sql.NullString
		if e.Actor != nil {
			subject = sql.NullString{String: e.Actor.Subject, Valid: true}
			name = sql.NullString{String: e.Actor.Name, Valid: true}
			provider = sql.NullString{String: e.Actor.Provider, Valid: true}
			client = sql.NullString{String: e.Actor.Client, Valid: true}
			transport = sql.NullString{String: string(e.Actor.Transport), Valid: true}
		}

		if _, err := stmt.ExecContext(
			ctx,
			e.ID, kindChange, e.Timestamp.UnixNano(),
			string(e.Type), e.Action, e.ResourceID, e.Name, e.Summary,
			e.Service, e.Stack, e.ServiceID,
			subject, name, provider, client, transport,
		); err != nil {
			return err
		}
	}

	return nil
}

const ownerOnlyVFS = "cetacean-owner-only"

func init() {
	vfs.Register(ownerOnlyVFS, ownerOnly{vfs.Find("os").(vfs.VFSFilename)})
}

// ownerOnly creates the database and its WAL 0600 before SQLite opens them,
// which would otherwise create them readable by everyone.
type ownerOnly struct{ vfs.VFSFilename }

func (o ownerOnly) OpenFilename(
	name *vfs.Filename,
	flags vfs.OpenFlag,
) (vfs.File, vfs.OpenFlag, error) {
	if name != nil && flags&vfs.OPEN_CREATE != 0 && flags&vfs.OPEN_EXCLUSIVE == 0 {
		f, err := os.OpenFile(name.String(), os.O_RDWR|os.O_CREATE, 0600)
		if err != nil {
			return nil, flags, err
		}

		_ = f.Close()
	}

	return o.VFSFilename.OpenFilename(name, flags)
}

func (s *SQLite) Close() error {
	return s.db.Close()
}

func inTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // a no-op after Commit

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit()
}
