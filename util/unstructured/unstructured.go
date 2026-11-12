package unstructured

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers/internalinterfaces"
	"k8s.io/client-go/tools/cache"
)

const workflowPaginationLimit = 500

// newListWatch returns a ListWatch for a single namespace (or all namespaces for ""),
// applying the given tweaks to list and watch options.
func newListWatch(resource schema.GroupVersionResource, client dynamic.Interface, namespace string, tweakListRequestListOptions internalinterfaces.TweakListOptionsFunc, tweakWatchRequestListOptions internalinterfaces.TweakListOptionsFunc) *cache.ListWatch {
	return &cache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
			if tweakListRequestListOptions != nil {
				tweakListRequestListOptions(&options)
			}
			return client.Resource(resource).Namespace(namespace).List(ctx, options)
		},
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			if tweakWatchRequestListOptions != nil {
				tweakWatchRequestListOptions(&options)
			}
			return client.Resource(resource).Namespace(namespace).Watch(ctx, options)
		},
	}
}

// NewFilteredUnstructuredInformer constructs a new informer for Unstructured type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredUnstructuredInformer(ctx context.Context, resource schema.GroupVersionResource, client dynamic.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListRequestListOptions internalinterfaces.TweakListOptionsFunc, tweakWatchRequestListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return NewFilteredUnstructuredInformerFromListerWatcher(ctx, newListWatch(resource, client, namespace, tweakListRequestListOptions, tweakWatchRequestListOptions), client, resyncPeriod, indexers, tweakListRequestListOptions, tweakWatchRequestListOptions)
}

// NewFilteredUnstructuredInformerFromListerWatcher constructs a new informer for Unstructured
// type from the given ListerWatcher, allowing callers to supply their own list/watch
// strategy (e.g. spanning several namespaces) while keeping the same pagination and tweak
// behaviour as NewFilteredUnstructuredInformer. The client decides watch-list support
// (fake clients report it unsupported, see watchlist.DoesClientNotSupportWatchListSemantics).
func NewFilteredUnstructuredInformerFromListerWatcher(ctx context.Context, lw cache.ListerWatcher, client any, resyncPeriod time.Duration, indexers cache.Indexers, tweakListRequestListOptions internalinterfaces.TweakListOptionsFunc, tweakWatchRequestListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	lwctx := cache.ToListerWatcherWithContext(lw)
	return cache.NewSharedIndexInformer(
		cache.ToListWatcherWithWatchListSemantics(&cache.ListWatch{
			ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
				if tweakListRequestListOptions != nil {
					tweakListRequestListOptions(&options)
				}
				var allWorkflows []unstructured.Unstructured
				continueTok := ""
				options.Limit = workflowPaginationLimit
				for {
					options.Continue = continueTok
					if options.Continue != "" {
						options.ResourceVersion = ""
						options.ResourceVersionMatch = ""
					}
					unList, err := lwctx.ListWithContext(ctx, options)
					if err != nil {
						return nil, err
					}
					unstructList, ok := unList.(*unstructured.UnstructuredList)
					if !ok {
						return nil, fmt.Errorf("unexpected list type %T", unList)
					}
					allWorkflows = append(allWorkflows, unstructList.Items...)

					if unstructList.GetContinue() == "" {
						break
					}
					continueTok = unstructList.GetContinue()
				}
				return &unstructured.UnstructuredList{
					Object: map[string]any{
						"apiVersion": "v1",
						"kind":       "List",
					},
					Items: allWorkflows,
				}, nil
			},
			WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
				if tweakWatchRequestListOptions != nil {
					tweakWatchRequestListOptions(&options)
				}
				return lwctx.WatchWithContext(ctx, options)
			},
		}, client),
		&unstructured.Unstructured{},
		resyncPeriod,
		indexers,
	)
}
