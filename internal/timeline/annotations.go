package timeline

import (
	"context"
	"database/sql"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
)

func (s *SQLite) Annotations(ctx context.Context) ([]cache.Annotation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, version, at, until, text,
			created_at, updated_at,
			actor_subject, actor_name, actor_provider, actor_client, actor_transport,
			editor_subject, editor_name, editor_provider, editor_client, editor_transport
		FROM annotations ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // rows.Err is checked

	var (
		result []cache.Annotation
		byID   = map[uint64]int{}
	)

	for rows.Next() {
		var (
			a              cache.Annotation
			at, created    int64
			until, updated sql.NullInt64
			actor, editor  actorColumns
		)

		if err := rows.Scan(
			&a.ID, &a.Version, &at, &until, &a.Text, &created, &updated,
			&actor.subject, &actor.name, &actor.provider, &actor.client, &actor.transport,
			&editor.subject, &editor.name, &editor.provider, &editor.client, &editor.transport,
		); err != nil {
			return nil, err
		}

		a.Timestamp = time.Unix(0, at)
		a.Until = unixTime(until)
		a.Created = time.Unix(0, created)
		a.Updated = unixTime(updated)
		a.Actor = actor.actor()
		a.Editor = editor.actor()

		byID[a.ID] = len(result)
		result = append(result, a)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// rowid keeps the order each list was written in.
	err = eachRow(ctx, s.db, `SELECT annotation_id, tag FROM annotation_tags
		ORDER BY annotation_id, rowid`, func(rows *sql.Rows) error {
		var (
			id  uint64
			tag string
		)

		if err := rows.Scan(&id, &tag); err != nil {
			return err
		}

		if i, ok := byID[id]; ok {
			result[i].Tags = append(result[i].Tags, tag)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	err = eachRow(ctx, s.db, `SELECT annotation_id, type, resource_id, name, service, stack
		FROM annotation_resources ORDER BY annotation_id, rowid`, func(rows *sql.Rows) error {
		var (
			id                   uint64
			typ, resourceID      string
			name, service, stack sql.NullString
		)

		if err := rows.Scan(&id, &typ, &resourceID, &name, &service, &stack); err != nil {
			return err
		}

		if i, ok := byID[id]; ok {
			result[i].Resources = append(result[i].Resources, cache.AnnotationResource{
				Type:    cache.EventType(typ),
				ID:      resourceID,
				Name:    name.String,
				Service: service.String,
				Stack:   stack.String,
			})
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (s *SQLite) PutAnnotation(ctx context.Context, a cache.Annotation) error {
	actor, editor := columnsOf(a.Actor), columnsOf(a.Editor)

	return inTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO annotations (
				id, version, at, until, text, created_at, updated_at,
				actor_subject, actor_name, actor_provider, actor_client, actor_transport,
				editor_subject, editor_name, editor_provider, editor_client, editor_transport
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (id) DO UPDATE SET
				version = excluded.version, at = excluded.at, until = excluded.until,
				text = excluded.text, created_at = excluded.created_at,
				updated_at = excluded.updated_at,
				actor_subject = excluded.actor_subject, actor_name = excluded.actor_name,
				actor_provider = excluded.actor_provider, actor_client = excluded.actor_client,
				actor_transport = excluded.actor_transport,
				editor_subject = excluded.editor_subject, editor_name = excluded.editor_name,
				editor_provider = excluded.editor_provider,
				editor_client = excluded.editor_client,
				editor_transport = excluded.editor_transport`,
			a.ID, a.Version, a.Timestamp.UnixNano(), nullTime(a.Until), a.Text,
			a.Created.UnixNano(), nullTime(a.Updated),
			actor.subject, actor.name, actor.provider, actor.client, actor.transport,
			editor.subject, editor.name, editor.provider, editor.client, editor.transport,
		); err != nil {
			return err
		}

		if err := deleteLists(ctx, tx, a.ID); err != nil {
			return err
		}

		for _, tag := range a.Tags {
			if _, err := tx.ExecContext(
				ctx,
				`INSERT INTO annotation_tags (annotation_id, tag) VALUES (?, ?)`,
				a.ID,
				tag,
			); err != nil {
				return err
			}
		}

		for _, r := range a.Resources {
			if _, err := tx.ExecContext(ctx, `INSERT INTO annotation_resources (
					annotation_id, type, resource_id, name, service, stack
				) VALUES (?, ?, ?, ?, ?, ?)`,
				a.ID, string(r.Type), r.ID, r.Name, r.Service, r.Stack,
			); err != nil {
				return err
			}
		}

		return nil
	})
}

func (s *SQLite) DeleteAnnotation(ctx context.Context, id uint64) error {
	return inTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM annotations WHERE id = ?`, id); err != nil {
			return err
		}

		return deleteLists(ctx, tx, id)
	})
}

func deleteLists(ctx context.Context, tx *sql.Tx, id uint64) error {
	for _, stmt := range []string{
		`DELETE FROM annotation_tags WHERE annotation_id = ?`,
		`DELETE FROM annotation_resources WHERE annotation_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, id); err != nil {
			return err
		}
	}

	return nil
}

// actorColumns is an actor as its five columns; a NULL subject is no actor.
type actorColumns struct {
	subject, name, provider, client, transport sql.NullString
}

func columnsOf(a *cache.Actor) actorColumns {
	if a == nil {
		return actorColumns{}
	}

	return actorColumns{
		subject:   sql.NullString{String: a.Subject, Valid: true},
		name:      sql.NullString{String: a.Name, Valid: true},
		provider:  sql.NullString{String: a.Provider, Valid: true},
		client:    sql.NullString{String: a.Client, Valid: true},
		transport: sql.NullString{String: string(a.Transport), Valid: true},
	}
}

func (c actorColumns) actor() *cache.Actor {
	if !c.subject.Valid {
		return nil
	}

	return &cache.Actor{
		Subject:   c.subject.String,
		Name:      c.name.String,
		Provider:  c.provider.String,
		Client:    c.client.String,
		Transport: cache.Transport(c.transport.String),
	}
}

func nullTime(t time.Time) sql.NullInt64 {
	if t.IsZero() {
		return sql.NullInt64{}
	}

	return sql.NullInt64{Int64: t.UnixNano(), Valid: true}
}

func unixTime(n sql.NullInt64) time.Time {
	if !n.Valid {
		return time.Time{}
	}

	return time.Unix(0, n.Int64)
}

func eachRow(ctx context.Context, db *sql.DB, query string, fn func(*sql.Rows) error) error {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close() //nolint:errcheck // rows.Err is checked

	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}

	return rows.Err()
}
