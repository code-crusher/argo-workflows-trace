package informer

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/dynamic/dynamiclister"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-workflows/v4/pkg/apis/workflow"
	extwfv1 "github.com/argoproj/argo-workflows/v4/pkg/client/informers/externalversions/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v4/pkg/client/listers/workflow/v1alpha1"
	informerutil "github.com/argoproj/argo-workflows/v4/util/informer"
	"github.com/argoproj/argo-workflows/v4/util/namespaces"
	unstructuredutil "github.com/argoproj/argo-workflows/v4/util/unstructured"
)

type tolerantWorkflowTemplateInformer struct {
	delegate informers.GenericInformer
}

// NewTolerantWorkflowTemplateInformer is a drop-in replacement for `extwfv1.WorkflowTemplateInformer` that ignores malformed resources.
// The namespace is the managed-namespace scope: "" for all namespaces, a single namespace,
// or a comma-separated allowlist of namespaces.
func NewTolerantWorkflowTemplateInformer(dynamicInterface dynamic.Interface, defaultResync time.Duration, namespace string) extwfv1.WorkflowTemplateInformer {
	tweakListOptions := func(options *metav1.ListOptions) {
		// `ResourceVersion=0` does not honor the `limit` in API calls, which results in making significant List calls
		// without `limit`. For details, see https://github.com/argoproj/argo-workflows/pull/11343
		// Check if ResourceVersion is "0" and reset it to empty string to ensure proper pagination behavior
		if options.ResourceVersion == "0" {
			options.ResourceVersion = ""
		}
	}
	gvr := schema.GroupVersionResource{Group: workflow.Group, Version: workflow.Version, Resource: workflow.WorkflowTemplatePlural}
	var delegate informers.GenericInformer
	if namespaces.IsMultiple(namespace) {
		delegate = NewMultiNamespaceDynamicInformer(dynamicInterface, gvr, namespaces.Parse(namespace), defaultResync, tweakListOptions)
	} else {
		delegate = dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicInterface, defaultResync, namespace, tweakListOptions).ForResource(gvr)
	}
	//nolint:errcheck // the error only happens if the informer was already started, and it hasn't been
	delegate.Informer().SetTransform(informerutil.StripManagedFields)
	return &tolerantWorkflowTemplateInformer{delegate: delegate}
}

// NewMultiNamespaceDynamicInformer builds a GenericInformer for a dynamic resource across
// several namespaces, in the same shape dynamicinformer.NewFilteredDynamicInformer builds
// it for one namespace.
func NewMultiNamespaceDynamicInformer(dynamicInterface dynamic.Interface, gvr schema.GroupVersionResource, namespaces []string, resyncPeriod time.Duration, tweakListOptions func(options *metav1.ListOptions)) informers.GenericInformer {
	lw := unstructuredutil.NewMultiNamespaceListerWatcher(dynamicInterface, gvr, namespaces)
	lwctx := cache.ToListerWatcherWithContext(lw)
	list := func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
		if tweakListOptions != nil {
			tweakListOptions(&options)
		}
		return lwctx.ListWithContext(ctx, options)
	}
	watchFn := func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
		if tweakListOptions != nil {
			tweakListOptions(&options)
		}
		return lwctx.WatchWithContext(ctx, options)
	}
	informer := cache.NewSharedIndexInformerWithOptions(
		// the multi-namespace ListerWatcher is passed as-is: it reports itself as not
		// supporting WatchList semantics, which a per-namespace fan-out cannot honor,
		// so the reflector uses the regular list-then-watch path
		&multiNamespaceDynamicListerWatcher{lwctx, list, watchFn},
		&unstructured.Unstructured{},
		cache.SharedIndexInformerOptions{
			ResyncPeriod:      resyncPeriod,
			Indexers:          cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
			ObjectDescription: gvr.String(),
		},
	)
	return &multiNamespaceDynamicInformer{
		informer: informer,
		gvr:      gvr,
	}
}

// multiNamespaceDynamicListerWatcher adapts the context-typed multi-namespace calls to
// both the legacy and context ListerWatcher interfaces, and disables WatchList semantics
// for the reflector.
type multiNamespaceDynamicListerWatcher struct {
	lw      cache.ListerWatcherWithContext
	list    func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error)
	watchFn func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error)
}

func (l *multiNamespaceDynamicListerWatcher) ListWithContext(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
	return l.list(ctx, options)
}

func (l *multiNamespaceDynamicListerWatcher) WatchWithContext(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
	return l.watchFn(ctx, options)
}

func (l *multiNamespaceDynamicListerWatcher) List(options metav1.ListOptions) (runtime.Object, error) {
	return l.list(context.Background(), options)
}

func (l *multiNamespaceDynamicListerWatcher) Watch(options metav1.ListOptions) (watch.Interface, error) {
	return l.watchFn(context.Background(), options)
}

func (l *multiNamespaceDynamicListerWatcher) IsWatchListSemanticsUnSupported() bool {
	return true
}

type multiNamespaceDynamicInformer struct {
	informer cache.SharedIndexInformer
	gvr      schema.GroupVersionResource
}

func (d *multiNamespaceDynamicInformer) Informer() cache.SharedIndexInformer {
	return d.informer
}

func (d *multiNamespaceDynamicInformer) Lister() cache.GenericLister {
	return dynamiclister.NewRuntimeObjectShim(dynamiclister.New(d.informer.GetIndexer(), d.gvr))
}

func (t *tolerantWorkflowTemplateInformer) Informer() cache.SharedIndexInformer {
	return t.delegate.Informer()
}

func (t *tolerantWorkflowTemplateInformer) Lister() v1alpha1.WorkflowTemplateLister {
	return &tolerantWorkflowTemplateLister{delegate: t.delegate.Lister()}
}
