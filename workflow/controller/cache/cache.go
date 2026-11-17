package cache

import (
	"context"
	"regexp"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	wfv1 "github.com/argoproj/argo-workflows/v4/pkg/apis/workflow/v1alpha1"
	memoizedb "github.com/argoproj/argo-workflows/v4/util/memoize/db"
)

var cacheKeyRegex = regexp.MustCompile("^[a-zA-Z0-9][-a-zA-Z0-9]*$")

type MemoizationCache interface {
	Load(ctx context.Context, key string) (*Entry, error)
	Save(ctx context.Context, key string, nodeID string, value *wfv1.Outputs) error
}

type Entry struct {
	NodeID            string        `json:"nodeID"`
	Outputs           *wfv1.Outputs `json:"outputs"`
	CreationTimestamp metav1.Time   `json:"creationTimestamp"`
	LastHitTimestamp  metav1.Time   `json:"lastHitTimestamp"`
}

func (e *Entry) Hit() bool {
	return e != nil && e.NodeID != ""
}

func (e *Entry) GetOutputs() *wfv1.Outputs {
	if e == nil {
		return nil
	}
	return e.Outputs
}

func (e *Entry) GetOutputsWithMaxAge(maxAge time.Duration) (*wfv1.Outputs, bool) {
	if e == nil {
		return nil, false
	}
	if time.Since(e.CreationTimestamp.Time) > maxAge {
		// Outputs have expired
		return nil, false
	}
	return e.Outputs, true
}

type cacheFactory struct {
	caches      map[string]MemoizationCache
	kubeclient  kubernetes.Interface
	namespace   string
	memoization *memoizedb.Info
	lock        sync.RWMutex
}

type Factory interface {
	// GetCache returns the memoization cache for the given name. If a database is
	// configured for memoization the name partitions the database table, otherwise it is
	// the name of a config map. Returns nil if no cache could be created.
	GetCache(name string) MemoizationCache
}

// NewCacheFactory creates a cache factory. If info is non-nil (and backed by a live
// database connection), memoization entries are stored in the database; otherwise the
// legacy config map cache is used.
func NewCacheFactory(ki kubernetes.Interface, ns string, info *memoizedb.Info) Factory {
	return &cacheFactory{
		caches:      make(map[string]MemoizationCache),
		kubeclient:  ki,
		namespace:   ns,
		memoization: info,
		lock:        sync.RWMutex{},
	}
}

// Returns a cache if it exists and creates it otherwise
func (cf *cacheFactory) GetCache(name string) MemoizationCache {
	cf.lock.RLock()
	c, ok := cf.caches[name]
	cf.lock.RUnlock()
	if ok {
		return c
	}

	cf.lock.Lock()
	defer cf.lock.Unlock()

	if c, ok := cf.caches[name]; ok {
		return c
	}

	if cf.memoization != nil && cf.memoization.SessionProxy != nil {
		c = NewSQLCache(cf.memoization, name)
		cf.caches[name] = c
		return c
	}
	c = NewConfigMapCache(cf.namespace, cf.kubeclient, name)
	cf.caches[name] = c
	return c
}
