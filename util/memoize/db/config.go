// Package db provides a database-backed store for workflow step memoization
// cache entries, as an alternative to the default config map based cache.
package db

import (
	"context"
	"time"

	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo-workflows/v4/config"
	"github.com/argoproj/argo-workflows/v4/util/logging"
	"github.com/argoproj/argo-workflows/v4/util/sqldb"
)

const (
	defaultTableName = "memoization_cache"
)

// Config holds database configuration for the memoization cache
type Config struct {
	TableName     string
	CacheName     string
	SkipMigration bool
	// CacheTTL is the maximum age of a cache entry. Entries older than this are
	// never served and are removed by garbage collection. Zero means no age-based expiry.
	CacheTTL time.Duration
}

// Info bundles the database connection and configuration for the memoization cache
type Info struct {
	Config       Config
	SessionProxy *sqldb.SessionProxy
}

// ConfigFromConfig creates a Config from the controller configuration.
// name is the memoization cache name (the name of the config map the template refers to).
func ConfigFromConfig(cfg *config.MemoizationConfig, name string) Config {
	if cfg == nil {
		return Config{}
	}
	return Config{
		TableName:     defaultTable(cfg.TableName, defaultTableName),
		CacheName:     name,
		SkipMigration: cfg.SkipMigration,
		CacheTTL:      time.Duration(cfg.CacheTTL),
	}
}

func defaultTable(tableName, defaultName string) string {
	if tableName == "" {
		return defaultName
	}
	return tableName
}

// Migrate runs the schema migration unless it is disabled in the configuration.
// If the migration fails, the session proxy is dropped and memoization lookups
// will fail, surfacing the problem instead of silently serving stale data.
func (d *Info) Migrate(ctx context.Context) {
	if d.SessionProxy == nil {
		return
	}
	logger := logging.RequireLoggerFromContext(ctx)
	logger.Info(ctx, "Setting up memoization cache database")
	if d.Config.SkipMigration {
		logger.Info(ctx, "Memoization cache database migration skipped")
		return
	}
	err := migrate(ctx, d.SessionProxy.Session(ctx), d.SessionProxy.DBType(), &d.Config)
	if err != nil {
		logger.WithError(err).Error(ctx, "cannot initialize memoization cache database, disabling database backed memoization")
		d.SessionProxy = nil
		return
	}
	logger.Info(ctx, "Memoization cache database migration complete")
}

// SessionProxyFromConfig creates a session proxy for the memoization cache database.
// It returns nil if the configuration is nil or the connection cannot be established.
func SessionProxyFromConfig(ctx context.Context, kubectlConfig kubernetes.Interface, namespace string, cfg *config.MemoizationConfig) *sqldb.SessionProxy {
	if cfg == nil {
		return nil
	}
	sessionProxy, err := sqldb.NewSessionProxy(ctx, sqldb.SessionProxyConfig{
		KubectlConfig: kubectlConfig,
		Namespace:     namespace,
		DBConfig:      cfg.DBConfig,
	})
	if err != nil {
		log := logging.RequireLoggerFromContext(ctx)
		log.WithError(err).Error(ctx, "was unable to create memoization cache database connection")
		return nil
	}
	return sessionProxy
}
