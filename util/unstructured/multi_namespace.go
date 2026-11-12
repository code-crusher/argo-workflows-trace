package unstructured

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
)

// multiNamespaceListWatch is a ListerWatcher that fans a list or watch out to each of a
// static allowlist of namespaces. It is only needed for informers whose client does not
// support client-go's WithNamespace option (dynamic and generated informers take a single
// namespace), because Kubernetes has no way to list or watch several named namespaces in
// one call.
//
// The per-namespace lists are combined into one list. Each list call paginates within its
// own namespace, so there is no continue token spanning namespaces; all items are
// gathered up front. Resource versions differ per namespace, so the combined list carries
// no ResourceVersion and the informer relies on the watches (one per namespace) for
// change detection after the initial list.
type multiNamespaceListWatch struct {
	list   func(ctx context.Context, namespace string, options metav1.ListOptions) (runtime.Object, error)
	watch  func(ctx context.Context, namespace string, options metav1.ListOptions) (watch.Interface, error)
	scopes []string
}

// listPerPage drains every page within one namespace: a continue token is only valid
// within its own list call, so pagination cannot span the fan-out.
func (lw *multiNamespaceListWatch) listPerPage(ctx context.Context, namespace string, options metav1.ListOptions) (runtime.Object, error) {
	list, err := lw.list(ctx, namespace, options)
	if err != nil {
		return nil, err
	}
	listMeta, ok := list.(metav1.ListInterface)
	if !ok {
		return list, nil
	}
	for listMeta.GetContinue() != "" {
		next := options
		next.Continue = listMeta.GetContinue()
		// a continued list is a new query; resource version from the first page does not apply
		next.ResourceVersion = ""
		next.ResourceVersionMatch = ""
		nextList, err := lw.list(ctx, namespace, next)
		if err != nil {
			return nil, err
		}
		nextMeta, ok := nextList.(metav1.ListInterface)
		if !ok {
			return nil, fmt.Errorf("cannot paginate list of type %T", nextList)
		}
		if err := appendItems(list, nextList); err != nil {
			return nil, err
		}
		listMeta = nextMeta
	}
	listMeta.SetContinue("")
	return list, nil
}

func (lw *multiNamespaceListWatch) ListWithContext(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
	lists := make([]runtime.Object, 0, len(lw.scopes))
	for _, ns := range lw.scopes {
		list, err := lw.listPerPage(ctx, ns, options)
		if err != nil {
			return nil, err
		}
		lists = append(lists, list)
	}
	return combineLists(lists)
}

func (lw *multiNamespaceListWatch) WatchWithContext(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
	// The informer starts the watch once, after the initial list. Errors opening any
	// namespace's watch stop the whole thing: the informer treats a stopped watch as
	// watch-expired and re-lists, so a failing namespace surfaces as a re-list rather
	// than as silently missing events.
	var watches []watch.Interface
	for _, ns := range lw.scopes {
		w, err := lw.watch(ctx, ns, options)
		if err != nil {
			for _, started := range watches {
				started.Stop()
			}
			return nil, err
		}
		watches = append(watches, w)
	}
	return newMultiWatch(watches...), nil
}

// The legacy non-context methods satisfy cache.ListerWatcher; SharedIndexInformer prefers
// the context variants, but reflectors started without a context fall back to these.
func (lw *multiNamespaceListWatch) List(options metav1.ListOptions) (runtime.Object, error) {
	return lw.ListWithContext(context.Background(), options)
}

func (lw *multiNamespaceListWatch) Watch(options metav1.ListOptions) (watch.Interface, error) {
	return lw.WatchWithContext(context.Background(), options)
}

// IsWatchListSemanticsUnSupported reports that this ListerWatcher cannot serve the
// WatchList (streaming) protocol: SendInitialEvents and its terminating bookmark are
// defined per single watch, and a fan-out over several namespaces multiplexes streams
// that each would need their own bookmark. Reflectors detect this method (as with
// cache.ToListerWatcherWithWatchListSemantics) and use the regular list-then-watch
// path instead.
func (lw *multiNamespaceListWatch) IsWatchListSemanticsUnSupported() bool {
	return true
}

// appendItems appends the Items of src onto dst, in place, via reflection on the Items
// field, so any list type with an Items slice works (typed, unstructured, PartialObjectMetadata).
func appendItems(dst, src runtime.Object) error {
	dstItems := reflect.Indirect(reflect.ValueOf(dst)).FieldByName("Items")
	srcItems := reflect.Indirect(reflect.ValueOf(src)).FieldByName("Items")
	if !dstItems.IsValid() || !srcItems.IsValid() || dstItems.Kind() != reflect.Slice || srcItems.Kind() != reflect.Slice {
		return fmt.Errorf("cannot append items from %T to %T: no Items slice", src, dst)
	}
	for i := 0; i < srcItems.Len(); i++ {
		dstItems.Set(reflect.Append(dstItems, srcItems.Index(i)))
	}
	return nil
}

// combineLists merges the per-namespace lists into one list of the same type as the
// first input. Items are appended via appendItems, and pagination and resource-version
// metadata are cleared: the combined list spans namespaces with no common continue
// token or resource version.
func combineLists(lists []runtime.Object) (runtime.Object, error) {
	if len(lists) == 0 {
		return nil, fmt.Errorf("no lists to combine")
	}
	combined := lists[0].DeepCopyObject()
	for _, list := range lists[1:] {
		if err := appendItems(combined, list); err != nil {
			return nil, err
		}
	}
	if listMeta, ok := combined.(metav1.ListInterface); ok {
		listMeta.SetContinue("")
		listMeta.SetResourceVersion("")
	}
	return combined, nil
}

// multiWatch multiplexes several watches into one watch.Interface. When every source has
// ended (each namespace's watch expires on its own server-side timeout) the result channel
// closes, which the informer interprets as watch expiry and handles by re-listing and
// re-watching. Stopping the multiplexed watch stops every source and also closes the
// result channel, per the watch.Interface contract.
type multiWatch struct {
	result  chan watch.Event
	sources []watch.Interface
	wg      sync.WaitGroup
	stop    chan struct{}
	stopMux sync.Mutex
	once    sync.Once
}

func newMultiWatch(sources ...watch.Interface) *multiWatch {
	m := &multiWatch{result: make(chan watch.Event), sources: sources, stop: make(chan struct{})}
	for _, w := range sources {
		m.wg.Add(1)
		go m.forward(w)
	}
	go func() {
		m.wg.Wait()
		// all sources ended on their own: close the result channel so the informer
		// notices the expiry
		m.closeResult()
	}()
	return m
}

// closeResult closes the result channel exactly once, and only when no forward goroutine
// can be sending: both callers first wait on m.wg, so every forwarder has returned.
func (m *multiWatch) closeResult() {
	m.once.Do(func() { close(m.result) })
}

func (m *multiWatch) forward(w watch.Interface) {
	defer m.wg.Done()
	for event := range w.ResultChan() {
		select {
		case m.result <- event:
		case <-m.stop:
			return
		}
	}
}

func (m *multiWatch) ResultChan() <-chan watch.Event {
	return m.result
}

func (m *multiWatch) Stop() {
	m.stopMux.Lock()
	select {
	case <-m.stop:
		m.stopMux.Unlock()
		// a previous Stop already closed the result channel after waiting on m.wg
		return
	default:
		close(m.stop)
	}
	m.stopMux.Unlock()
	// closing m.stop wakes every forwarder parked on a send; stopping the sources ends
	// the rest. Only once no forwarder can remain is it safe to close the result
	// channel (a send on a closed channel panics).
	for _, w := range m.sources {
		w.Stop()
	}
	m.wg.Wait()
	m.closeResult()
}

// NewMultiNamespaceListerWatcher returns a ListerWatcher that lists and watches a dynamic
// resource across a static allowlist of namespaces. An empty or single-element namespaces
// slice lists and watches all namespaces, or that one namespace, respectively.
func NewMultiNamespaceListerWatcher(client dynamic.Interface, resource schema.GroupVersionResource, namespaces []string) cache.ListerWatcher {
	if len(namespaces) <= 1 {
		ns := ""
		if len(namespaces) == 1 {
			ns = namespaces[0]
		}
		return newListWatch(resource, client, ns, nil, nil)
	}
	return newMultiNamespaceListerWatcherFunc(namespaces, func(ctx context.Context, ns string, options metav1.ListOptions) (runtime.Object, error) {
		return client.Resource(resource).Namespace(ns).List(ctx, options)
	}, func(ctx context.Context, ns string, options metav1.ListOptions) (watch.Interface, error) {
		return client.Resource(resource).Namespace(ns).Watch(ctx, options)
	})
}

// NewMultiNamespaceListerWatcherFunc is like NewMultiNamespaceListerWatcher but takes
// list/watch funcs, so it works with any client (typed, metadata, dynamic).
func NewMultiNamespaceListerWatcherFunc(namespaces []string, list func(ctx context.Context, namespace string, options metav1.ListOptions) (runtime.Object, error), watchFn func(ctx context.Context, namespace string, options metav1.ListOptions) (watch.Interface, error)) cache.ListerWatcher {
	if len(namespaces) <= 1 {
		ns := ""
		if len(namespaces) == 1 {
			ns = namespaces[0]
		}
		return &cache.ListWatch{
			// the legacy List/Watch methods fall back to these context variants
			ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
				return list(ctx, ns, options)
			},
			WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
				return watchFn(ctx, ns, options)
			},
		}
	}
	return newMultiNamespaceListerWatcherFunc(namespaces, list, watchFn)
}

func newMultiNamespaceListerWatcherFunc(namespaces []string, list func(ctx context.Context, namespace string, options metav1.ListOptions) (runtime.Object, error), watchFn func(ctx context.Context, namespace string, options metav1.ListOptions) (watch.Interface, error)) cache.ListerWatcher {
	return &multiNamespaceListWatch{list: list, watch: watchFn, scopes: namespaces}
}
