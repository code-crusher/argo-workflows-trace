package db

import (
	"context"

	"github.com/upper/db/v4"

	"github.com/argoproj/argo-workflows/v4/util/sqldb"
)

const (
	versionTable = "memoization_schema_history"
)

// MigrateChanges returns the list of schema changes for the memoization cache table.
func MigrateChanges(config *Config, dbType sqldb.DBType) []sqldb.Change {
	return []sqldb.Change{
		sqldb.ByType(dbType, sqldb.TypedChanges{
			sqldb.Postgres: sqldb.AnsiSQLChange(`create table if not exists ` + config.TableName + ` (
    cache varchar(255) not null,
    cache_key varchar(255) not null,
    node_id varchar(255) not null,
    outputs text,
    creation_timestamp timestamp,
    last_hit_timestamp timestamp,
    primary key (cache, cache_key)
)`),
			sqldb.MySQL: sqldb.AnsiSQLChange(`create table if not exists ` + config.TableName + ` (
    cache varchar(255) not null,
    cache_key varchar(255) not null,
    node_id varchar(255) not null,
    outputs mediumtext,
    creation_timestamp timestamp,
    last_hit_timestamp timestamp,
    primary key (cache, cache_key)
)`),
		}),
		sqldb.AnsiSQLChange(`create index imemcache_lasthit on ` + config.TableName + ` (last_hit_timestamp)`),
	}
}

func migrate(ctx context.Context, session db.Session, dbType sqldb.DBType, config *Config) error {
	return sqldb.Migrate(ctx, session, dbType, versionTable, MigrateChanges(config, dbType))
}
