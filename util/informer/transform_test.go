package informer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/cache"
)

func TestStripManagedFields(t *testing.T) {
	t.Run("typed object", func(t *testing.T) {
		pod := &apiv1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:          "my-pod",
				ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "kubelet"}},
			},
		}
		out, err := StripManagedFields(pod)
		require.NoError(t, err)
		assert.Same(t, pod, out)
		assert.Nil(t, pod.ManagedFields)
		assert.Equal(t, "my-pod", pod.Name)
	})
	t.Run("unstructured object", func(t *testing.T) {
		un := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "Workflow",
			"metadata": map[string]any{
				"name":          "my-wf",
				"managedFields": []any{map[string]any{"manager": "workflow-controller"}},
			},
		}}
		out, err := StripManagedFields(un)
		require.NoError(t, err)
		assert.Same(t, un, out)
		assert.Nil(t, un.GetManagedFields())
		assert.NotContains(t, un.Object["metadata"], "managedFields")
		assert.Equal(t, "my-wf", un.GetName())
	})
	t.Run("nil managedFields is not mutated", func(t *testing.T) {
		un := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "Workflow",
			"metadata":   map[string]any{"name": "my-wf"},
		}}
		want := un.DeepCopy()
		out, err := StripManagedFields(un)
		require.NoError(t, err)
		assert.Same(t, un, out)
		assert.Equal(t, want, un)
	})
	t.Run("non-object input passes through", func(t *testing.T) {
		tombstone := cache.DeletedFinalStateUnknown{Key: "my-ns/my-pod"}
		out, err := StripManagedFields(tombstone)
		require.NoError(t, err)
		assert.Equal(t, tombstone, out)
	})
}

func TestStripLastAppliedConfiguration(t *testing.T) {
	// last-applied-configuration as written by client-side `kubectl apply` (and Argo CD).
	const lastApplied = `{"apiVersion":"argoproj.io/v1alpha1","kind":"WorkflowTemplate",` +
		`"metadata":{"name":"my-tmpl","namespace":"my-ns"},` +
		`"spec":{"entrypoint":"main","templates":[{"name":"main","container":{"image":"docker/whalesay"}}]}}`
	t.Run("typed object", func(t *testing.T) {
		pod := &apiv1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name:        "my-pod",
			Annotations: map[string]string{apiv1.LastAppliedConfigAnnotation: lastApplied, "other": "kept"},
		}}
		out, err := StripLastAppliedConfiguration(pod)
		require.NoError(t, err)
		assert.Same(t, pod, out)
		assert.NotContains(t, pod.Annotations, apiv1.LastAppliedConfigAnnotation)
		assert.Equal(t, map[string]string{"other": "kept"}, pod.Annotations)
	})
	t.Run("unstructured object", func(t *testing.T) {
		un := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "WorkflowTemplate",
			"metadata": map[string]any{
				"name":        "my-tmpl",
				"namespace":   "my-ns",
				"annotations": map[string]any{apiv1.LastAppliedConfigAnnotation: lastApplied, "other": "kept"},
			},
		}}
		out, err := StripLastAppliedConfiguration(un)
		require.NoError(t, err)
		assert.Same(t, un, out)
		assert.NotContains(t, un.GetAnnotations(), apiv1.LastAppliedConfigAnnotation)
		assert.Equal(t, map[string]string{"other": "kept"}, un.GetAnnotations())
		assert.Equal(t, "my-tmpl", un.GetName())
	})
	t.Run("shared annotation map is not mutated", func(t *testing.T) {
		// Two objects sharing one annotation map (e.g. an informer and a
		// deep-copy-derived twin): stripping one must not affect the other.
		shared := map[string]string{apiv1.LastAppliedConfigAnnotation: lastApplied}
		a := &apiv1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "a", Annotations: shared}}
		b := &apiv1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "b", Annotations: shared}}
		_, err := StripLastAppliedConfiguration(a)
		require.NoError(t, err)
		assert.NotContains(t, a.Annotations, apiv1.LastAppliedConfigAnnotation)
		assert.Contains(t, b.Annotations, apiv1.LastAppliedConfigAnnotation)
	})
	t.Run("object without the annotation is not mutated", func(t *testing.T) {
		un := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "WorkflowTemplate",
			"metadata":   map[string]any{"name": "my-tmpl", "annotations": map[string]any{"other": "kept"}},
		}}
		want := un.DeepCopy()
		out, err := StripLastAppliedConfiguration(un)
		require.NoError(t, err)
		assert.Same(t, un, out)
		assert.Equal(t, want, un)
	})
	t.Run("object without annotations is not mutated", func(t *testing.T) {
		un := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "WorkflowTemplate",
			"metadata":   map[string]any{"name": "my-tmpl"},
		}}
		want := un.DeepCopy()
		out, err := StripLastAppliedConfiguration(un)
		require.NoError(t, err)
		assert.Same(t, un, out)
		assert.Equal(t, want, un)
	})
	t.Run("non-object input passes through", func(t *testing.T) {
		tombstone := cache.DeletedFinalStateUnknown{Key: "my-ns/my-tmpl"}
		out, err := StripLastAppliedConfiguration(tombstone)
		require.NoError(t, err)
		assert.Equal(t, tombstone, out)
	})
}

func TestChainTransforms(t *testing.T) {
	t.Run("runs each transform in order", func(t *testing.T) {
		un := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "WorkflowTemplate",
			"metadata": map[string]any{
				"name":          "my-tmpl",
				"managedFields": []any{map[string]any{"manager": "workflow-controller"}},
				"annotations":   map[string]any{apiv1.LastAppliedConfigAnnotation: "{}"},
			},
		}}
		out, err := ChainTransforms(StripManagedFields, StripLastAppliedConfiguration)(un)
		require.NoError(t, err)
		assert.Same(t, un, out)
		assert.Nil(t, un.GetManagedFields())
		assert.NotContains(t, un.GetAnnotations(), apiv1.LastAppliedConfigAnnotation)
	})
	t.Run("empty chain passes through", func(t *testing.T) {
		un := &unstructured.Unstructured{Object: map[string]any{"kind": "WorkflowTemplate"}}
		out, err := ChainTransforms()(un)
		require.NoError(t, err)
		assert.Same(t, un, out)
	})
	t.Run("error aborts the chain", func(t *testing.T) {
		wantErr := assert.AnError
		out, err := ChainTransforms(func(any) (any, error) { return nil, wantErr })(unstructured.Unstructured{})
		assert.ErrorIs(t, err, wantErr)
		assert.Nil(t, out)
	})
}
