package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	wfv1 "github.com/argoproj/argo-workflows/v4/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v4/util/logging"
	memoizedb "github.com/argoproj/argo-workflows/v4/util/memoize/db"
)

// sqlCache is a database-backed memoization cache.
type sqlCache struct {
	info    *memoizedb.Info
	queries *memoizedb.Queries
	lock    sync.Mutex
}

// NewSQLCache creates a database-backed memoization cache for the given cache name.
// The cache name mirrors the config map name used by the config map backed cache, so
// entries for different caches are kept in separate partitions of the same table.
func NewSQLCache(info *memoizedb.Info, name string) MemoizationCache {
	cfg := info.Config
	cfg.CacheName = name
	return &sqlCache{
		info:    info,
		queries: memoizedb.NewQueries(info.SessionProxy, cfg),
		lock:    sync.Mutex{},
	}
}

func (c *sqlCache) Load(ctx context.Context, key string) (*Entry, error) {
	if !cacheKeyRegex.MatchString(key) {
		return nil, fmt.Errorf("invalid cache key: %s", key)
	}
	c.lock.Lock()
	defer c.lock.Unlock()

	logger := logging.RequireLoggerFromContext(ctx)
	row, err := c.queries.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if row == nil {
		logger.WithField("key", key).Debug(ctx, "memoization cache miss: entry does not exist")
		return nil, nil
	}
	if ttl := c.info.Config.CacheTTL; ttl > 0 && time.Since(row.CreationTimestamp) > ttl {
		logger.WithField("key", key).Debug(ctx, "memoization cache miss: entry expired")
		return nil, nil
	}

	var outputs *wfv1.Outputs
	if len(row.Outputs) > 0 {
		if err := json.Unmarshal(row.Outputs, &outputs); err != nil {
			return nil, fmt.Errorf("malformed cache entry: could not unmarshal outputs; unable to parse: %w", err)
		}
	}

	// best effort: a failed last-hit timestamp update must not fail the read
	hitTime := time.Now()
	if err := c.queries.SetLastHit(ctx, key, hitTime); err != nil {
		logger.WithField("key", key).WithError(err).Debug(ctx, "failed to update memoization cache entry last hit timestamp")
	}

	return &Entry{
		NodeID:            row.NodeID,
		Outputs:           outputs,
		CreationTimestamp: metav1.Time{Time: row.CreationTimestamp},
		LastHitTimestamp:  metav1.Time{Time: hitTime},
	}, nil
}

func (c *sqlCache) Save(ctx context.Context, key string, nodeID string, value *wfv1.Outputs) error {
	if !cacheKeyRegex.MatchString(key) {
		return fmt.Errorf("invalid cache key: %s", key)
	}
	c.lock.Lock()
	defer c.lock.Unlock()

	now := time.Now()
	outputsJSON, err := json.Marshal(value)
	if err != nil {
		logger := logging.RequireLoggerFromContext(ctx)
		logger.WithField("key", key).WithError(err).Error(ctx, "unable to marshal cache entry outputs")
		return fmt.Errorf("unable to marshal cache entry outputs: %w", err)
	}
	return c.queries.Upsert(ctx, key, nodeID, outputsJSON, now)
}

// DeleteOlderThan deletes entries across all caches in the memoization table whose last hit
// is before notHitSince, or whose creation is before createdBefore. A zero time disables the
// corresponding condition. It returns the number of entries deleted.
func DeleteOlderThan(ctx context.Context, info *memoizedb.Info, notHitSince, createdBefore time.Time) (int64, error) {
	if info == nil || info.SessionProxy == nil {
		return 0, nil
	}
	queries := memoizedb.NewQueries(info.SessionProxy, info.Config)
	return queries.DeleteOlderThan(ctx, notHitSince, createdBefore)
}
