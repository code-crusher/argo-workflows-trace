package unstructured

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	wfv1 "github.com/argoproj/argo-workflows/v4/pkg/apis/workflow/v1alpha1"
	wfversioned "github.com/argoproj/argo-workflows/v4/pkg/client/clientset/versioned"
	wffake "github.com/argoproj/argo-workflows/v4/pkg/client/clientset/versioned/fake"
)

var podsGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}

func newTestPod(name, namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
		},
	}
}

func TestMultiNamespaceListerWatcherLists(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(),
		newTestPod("pod-a", "ns-1"),
		newTestPod("pod-b", "ns-2"),
		newTestPod("pod-c", "ns-3"), // outside the allowlist
	)
	lw := NewMultiNamespaceListerWatcher(client, podsGVR, []string{"ns-1", "ns-2"})

	list, err := cache.ToListerWatcherWithContext(lw).ListWithContext(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	listMeta, ok := list.(*unstructured.UnstructuredList)
	require.True(t, ok)
	names := make([]string, 0, len(listMeta.Items))
	for _, item := range listMeta.Items {
		names = append(names, item.GetName())
	}
	// items from both namespaces, none from outside the allowlist, no continue token
	assert.ElementsMatch(t, []string{"pod-a", "pod-b"}, names)
	assert.Empty(t, listMeta.GetContinue())
	assert.Empty(t, listMeta.GetResourceVersion())
}

func TestMultiNamespaceListerWatcherSingleNamespace(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(),
		newTestPod("pod-a", "ns-1"),
	)
	// a single namespace gets a plain ListWatch, not a multiplexer
	lw := NewMultiNamespaceListerWatcher(client, podsGVR, []string{"ns-1"})
	assert.IsType(t, &cache.ListWatch{}, lw)
}

func TestMultiNamespaceListerWatcherWatch(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	lw := NewMultiNamespaceListerWatcher(client, podsGVR, []string{"ns-1", "ns-2"})

	w, err := cache.ToListerWatcherWithContext(lw).WatchWithContext(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	defer w.Stop()

	// an event in either namespace arrives on the multiplexed channel
	_, err = client.Resource(podsGVR).Namespace("ns-2").Create(context.Background(),
		newTestPod("pod-b", "ns-2"), metav1.CreateOptions{})
	require.NoError(t, err)

	select {
	case event, ok := <-w.ResultChan():
		require.True(t, ok)
		assert.Equal(t, watch.Added, event.Type)
		pod, ok := event.Object.(*unstructured.Unstructured)
		require.True(t, ok)
		assert.Equal(t, "pod-b", pod.GetName())
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for watch event")
	}
}

func TestMultiNamespaceListerWatcherWatchStopsAll(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	lw := NewMultiNamespaceListerWatcher(client, podsGVR, []string{"ns-1", "ns-2"})

	w, err := cache.ToListerWatcherWithContext(lw).WatchWithContext(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	w.Stop()

	// the multiplexer closes its channel once stopped, and callers must not panic
	select {
	case _, ok := <-w.ResultChan():
		assert.False(t, ok)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for watch channel to close")
	}
	// stopping twice is safe
	w.Stop()
}

func TestCombineListsTyped(t *testing.T) {
	// typed lists (not just unstructured) must combine, and the combined list must be
	// writable through reflection back into a typed list
	ns1 := &corev1.PodList{Items: []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "ns-1"}}}}
	ns2 := &corev1.PodList{Items: []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "pod-b", Namespace: "ns-2"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "pod-c", Namespace: "ns-2"}},
	}}
	combined, err := combineLists([]runtime.Object{ns1, ns2})
	require.NoError(t, err)
	podList, ok := combined.(*corev1.PodList)
	require.True(t, ok)
	names := []string{}
	for _, pod := range podList.Items {
		names = append(names, pod.Name)
	}
	assert.ElementsMatch(t, []string{"pod-a", "pod-b", "pod-c"}, names)
}

func TestMultiNamespaceInformerSync(t *testing.T) {
	// end-to-end: an informer over a two-namespace allowlist syncs items from both
	// namespaces and receives events from both
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(),
		newTestPod("pod-a", "ns-1"),
		newTestPod("pod-b", "ns-2"),
	)
	lw := NewMultiNamespaceListerWatcher(client, podsGVR, []string{"ns-1", "ns-2"})
	informer := cache.NewSharedIndexInformer(lw, &unstructured.Unstructured{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	stop := make(chan struct{})
	defer close(stop)
	go informer.Run(stop)

	require.True(t, cache.WaitForCacheSync(stop, informer.HasSynced))

	indexer := informer.GetIndexer()
	assert.Len(t, indexer.ListKeys(), 2)

	_, err := client.Resource(podsGVR).Namespace("ns-2").Create(context.Background(),
		newTestPod("pod-c", "ns-2"), metav1.CreateOptions{})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return len(indexer.ListKeys()) == 3
	}, 3*time.Second, 10*time.Millisecond)
}

// The remaining tests exercise the typed-client flavour used by the workflow informer.

func newTypedWorkflow(name, namespace string) *wfv1.Workflow {
	return &wfv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
}

func TestMultiNamespaceListerWatcherFuncTyped(t *testing.T) {
	var client wfversioned.Interface = wffake.NewClientset(
		newTypedWorkflow("wf-a", "ns-1"),
		newTypedWorkflow("wf-b", "ns-2"),
	)
	lw := NewMultiNamespaceListerWatcherFunc([]string{"ns-1", "ns-2"},
		func(ctx context.Context, ns string, options metav1.ListOptions) (runtime.Object, error) {
			return client.ArgoprojV1alpha1().Workflows(ns).List(ctx, options)
		},
		func(ctx context.Context, ns string, options metav1.ListOptions) (watch.Interface, error) {
			return client.ArgoprojV1alpha1().Workflows(ns).Watch(ctx, options)
		},
	)
	list, err := cache.ToListerWatcherWithContext(lw).ListWithContext(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	wfList, ok := list.(*wfv1.WorkflowList)
	require.True(t, ok)
	names := []string{}
	for _, wf := range wfList.Items {
		names = append(names, wf.Name)
	}
	assert.ElementsMatch(t, []string{"wf-a", "wf-b"}, names)
}

func TestMultiNamespaceListerWatcherFuncSingleNamespace(t *testing.T) {
	// a single namespace gets a plain ListWatch; zero namespaces list all namespaces
	client := fake.NewSimpleClientset()
	lw := NewMultiNamespaceListerWatcherFunc([]string{"ns-1"},
		func(ctx context.Context, ns string, options metav1.ListOptions) (runtime.Object, error) {
			return client.CoreV1().Pods(ns).List(ctx, options)
		},
		func(ctx context.Context, ns string, options metav1.ListOptions) (watch.Interface, error) {
			return client.CoreV1().Pods(ns).Watch(ctx, options)
		},
	)
	assert.IsType(t, &cache.ListWatch{}, lw)
}

func TestMultiNamespaceListerWatcherPaginates(t *testing.T) {
	// each namespace's list paginates internally: page two of a namespace must not be
	// dropped when the fan-out merges lists
	listCalls := map[string]int{}
	mu := sync.Mutex{}
	page := 0
	lw := NewMultiNamespaceListerWatcherFunc([]string{"ns-1", "ns-2"},
		func(ctx context.Context, ns string, options metav1.ListOptions) (runtime.Object, error) {
			mu.Lock()
			defer mu.Unlock()
			listCalls[ns]++
			// first call for each namespace returns one item plus a continue token
			if options.Continue == "" {
				return &corev1.PodList{
					ListMeta: metav1.ListMeta{Continue: "next-page"},
					Items:    []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "pod-1-" + ns, Namespace: ns}}},
				}, nil
			}
			// the continued call returns the last item, no continue token
			page++
			return &corev1.PodList{
				Items: []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "pod-2-" + ns, Namespace: ns}}},
			}, nil
		},
		func(ctx context.Context, ns string, options metav1.ListOptions) (watch.Interface, error) {
			return watch.NewEmptyWatch(), nil
		},
	)
	list, err := cache.ToListerWatcherWithContext(lw).ListWithContext(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	podList, ok := list.(*corev1.PodList)
	require.True(t, ok)
	names := []string{}
	for _, pod := range podList.Items {
		names = append(names, pod.Name)
	}
	assert.ElementsMatch(t, []string{"pod-1-ns-1", "pod-2-ns-1", "pod-1-ns-2", "pod-2-ns-2"}, names)
	assert.Equal(t, 2, listCalls["ns-1"])
	assert.Equal(t, 2, listCalls["ns-2"])
	assert.Empty(t, podList.GetContinue())
}
