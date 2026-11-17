package db

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/argoproj/argo-workflows/v4/util/logging"
)

func TestMigrateChangesMatchSchema(t *testing.T) {
	changes := MigrateChanges(&Config{TableName: "memoization_cache"}, "postgres")
	assert.NotEmpty(t, changes)
}

func TestConfigFromConfigDefaults(t *testing.T) {
	cfg := ConfigFromConfig(nil, "")
	assert.Equal(t, Config{}, cfg)

	cfg = ConfigFromConfig(nil, "ignored")
	assert.Equal(t, Config{}, cfg)
}

func TestSessionProxyFromConfigNil(t *testing.T) {
	ctx := logging.TestContext(t.Context())
	assert.Nil(t, SessionProxyFromConfig(ctx, nil, "default", nil))
}

func TestMigrateNoSession(t *testing.T) {
	ctx := logging.TestContext(t.Context())
	info := &Info{}
	info.Migrate(ctx) // must not panic
	assert.Nil(t, info.SessionProxy)
}

func TestEntryJSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	row := Entry{Cache: "c", CacheKey: "k", NodeID: "node-1", Outputs: []byte(`{"parameters":[]}`), CreationTimestamp: now, LastHitTimestamp: now}
	assert.Equal(t, "c", row.Cache)
	assert.Equal(t, "k", row.CacheKey)
	assert.Equal(t, "node-1", row.NodeID)
	assert.JSONEq(t, `{"parameters":[]}`, string(row.Outputs))
	assert.Equal(t, now, row.CreationTimestamp)
}
