package cache

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-workflows/v4/util/namespaces"
	unstructuredutil "github.com/argoproj/argo-workflows/v4/util/unstructured"
)

type ResourceCache struct {
	cache  Interface
	client kubernetes.Interface
	v1.ServiceAccountLister
	saInformer cache.SharedIndexInformer
}

func NewResourceCacheWithTimeout(client kubernetes.Interface, namespace string, timeout time.Duration) *ResourceCache {
	// namespace is the managed-namespace scope: "" for all namespaces, one namespace, or
	// a comma-separated allowlist
	saInformer := newServiceAccountInformer(client, namespace)
	c := &ResourceCache{
		cache:                NewLRUTtlCache(timeout, 2000),
		client:               client,
		ServiceAccountLister: v1.NewServiceAccountLister(saInformer.GetIndexer()),
		saInformer:           saInformer,
	}
	return c
}

// newServiceAccountInformer watches service accounts in the given managed-namespace
// scope. A single namespace (or all namespaces) uses the shared informer factory; an
// allowlist of namespaces needs one lister-watcher fanning out per namespace, which the
// factory cannot express.
func newServiceAccountInformer(client kubernetes.Interface, namespace string) cache.SharedIndexInformer {
	if namespaces.IsMultiple(namespace) {
		lw := unstructuredutil.NewMultiNamespaceListerWatcherFunc(namespaces.Parse(namespace),
			func(ctx context.Context, ns string, options metav1.ListOptions) (runtime.Object, error) {
				return client.CoreV1().ServiceAccounts(ns).List(ctx, options)
			},
			func(ctx context.Context, ns string, options metav1.ListOptions) (watch.Interface, error) {
				return client.CoreV1().ServiceAccounts(ns).Watch(ctx, options)
			},
		)
		return cache.NewSharedIndexInformer(lw, &corev1.ServiceAccount{}, time.Minute*20, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	}
	informerFactory := informers.NewSharedInformerFactoryWithOptions(client, time.Minute*20, informers.WithNamespace(namespace))
	return informerFactory.Core().V1().ServiceAccounts().Informer()
}

func NewResourceCache(client kubernetes.Interface, namespace string) *ResourceCache {
	return NewResourceCacheWithTimeout(client, namespace, time.Minute*1)
}

func (c *ResourceCache) Run(stopCh <-chan struct{}) {
	go c.saInformer.Run(stopCh)
	cache.WaitForCacheSync(stopCh, c.saInformer.HasSynced)
}

func (c *ResourceCache) GetSecret(ctx context.Context, namespace string, secretName string) (*corev1.Secret, error) {
	cacheKey := c.getSecretCacheKey(namespace, secretName)
	if secret, ok := c.cache.Get(cacheKey); ok {
		if secret, ok := secret.(*corev1.Secret); ok {
			return secret, nil
		}
	}

	secret, err := c.getSecretFromServer(ctx, namespace, secretName)
	if err != nil {
		return nil, err
	}

	c.cache.Add(cacheKey, secret)
	return secret, nil
}

func (c *ResourceCache) getSecretFromServer(ctx context.Context, namespace string, secretName string) (*corev1.Secret, error) {
	return c.client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
}

func (c *ResourceCache) getSecretCacheKey(namespace string, secretName string) string {
	return namespace + ":secret:" + secretName
}
