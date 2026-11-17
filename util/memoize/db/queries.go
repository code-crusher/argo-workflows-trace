package db

import (
	"context"
	"errors"
	"time"

	"github.com/upper/db/v4"

	"github.com/argoproj/argo-workflows/v4/util/sqldb"
)

// Entry is a single entry in the memoization cache table.
type Entry struct {
	Cache             string    `db:"cache"`
	CacheKey          string    `db:"cache_key"`
	NodeID            string    `db:"node_id"`
	Outputs           []byte    `db:"outputs"`
	CreationTimestamp time.Time `db:"creation_timestamp"`
	LastHitTimestamp  time.Time `db:"last_hit_timestamp"`
}

// Queries implements the database operations for the memoization cache.
type Queries struct {
	sessionProxy *sqldb.SessionProxy
	config       Config
}

// NewQueries creates a Queries instance for the given session proxy and config.
func NewQueries(sessionProxy *sqldb.SessionProxy, config Config) *Queries {
	return &Queries{sessionProxy: sessionProxy, config: config}
}

// Get returns the entry for the given key, or nil if there is none.
func (q *Queries) Get(ctx context.Context, key string) (*Entry, error) {
	var entry *Entry
	err := q.sessionProxy.With(ctx, func(session db.Session) error {
		res := Entry{}
		err := session.SQL().Select("cache", "cache_key", "node_id", "outputs", "creation_timestamp", "last_hit_timestamp").
			From(q.config.TableName).
			Where(db.Cond{"cache": q.config.CacheName, "cache_key": key}).
			Limit(1).
			One(&res)
		if err != nil {
			if errors.Is(err, db.ErrNoMoreRows) {
				return nil
			}
			return err
		}
		entry = &res
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// SetLastHit updates the last hit timestamp of an entry.
func (q *Queries) SetLastHit(ctx context.Context, key string, at time.Time) error {
	return q.sessionProxy.With(ctx, func(session db.Session) error {
		_, err := session.SQL().Update(q.config.TableName).
			Set("last_hit_timestamp", at).
			Where(db.Cond{"cache": q.config.CacheName, "cache_key": key}).
			Exec()
		return err
	})
}

// Upsert inserts or updates the entry for the given key.
func (q *Queries) Upsert(ctx context.Context, key, nodeID string, outputs []byte, now time.Time) error {
	return q.sessionProxy.With(ctx, func(session db.Session) error {
		if q.sessionProxy.DBType() == sqldb.Postgres {
			_, err := session.SQL().ExecContext(ctx,
				"insert into "+q.config.TableName+" (cache, cache_key, node_id, outputs, creation_timestamp, last_hit_timestamp) values (?, ?, ?, ?, ?, ?) "+
					"on conflict (cache, cache_key) do update set node_id = excluded.node_id, outputs = excluded.outputs, creation_timestamp = excluded.creation_timestamp, last_hit_timestamp = excluded.last_hit_timestamp",
				q.config.CacheName, key, nodeID, outputs, now, now)
			return err
		}
		_, err := session.SQL().ExecContext(ctx,
			"insert into "+q.config.TableName+" (cache, cache_key, node_id, outputs, creation_timestamp, last_hit_timestamp) values (?, ?, ?, ?, ?, ?) "+
				"on duplicate key update node_id = values(node_id), outputs = values(outputs), creation_timestamp = values(creation_timestamp), last_hit_timestamp = values(last_hit_timestamp)",
			q.config.CacheName, key, nodeID, outputs, now, now)
		return err
	})
}

// DeleteOlderThan deletes entries across all caches in the table whose last hit
// timestamp is before notHitSince, or whose creation timestamp is before createdBefore.
// A zero time disables the corresponding condition. It returns the number of entries deleted.
// Note this intentionally ignores the configured cache name.
func (q *Queries) DeleteOlderThan(ctx context.Context, notHitSince, createdBefore time.Time) (int64, error) {
	var deleted int64
	err := q.sessionProxy.With(ctx, func(session db.Session) error {
		query := "delete from " + q.config.TableName + " where 1 = 0"
		args := []interface{}{}
		if !notHitSince.IsZero() {
			query = "delete from " + q.config.TableName + " where last_hit_timestamp < ?"
			args = append(args, notHitSince)
		}
		if !createdBefore.IsZero() {
			if len(args) == 0 {
				query = "delete from " + q.config.TableName + " where creation_timestamp < ?"
				args = append(args, createdBefore)
			} else {
				query += " or creation_timestamp < ?"
				args = append(args, createdBefore)
			}
		}
		res, err := session.SQL().ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		deleted, err = res.RowsAffected()
		return err
	})
	return deleted, err
}
