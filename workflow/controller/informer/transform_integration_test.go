package informer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/tools/cache"

	workflow "github.com/argoproj/argo-workflows/v4/pkg/apis/workflow"
	wfv1 "github.com/argoproj/argo-workflows/v4/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v4/pkg/client/clientset/versioned/scheme"
)

// lastApplied mimics kubectl.kubernetes.io/last-applied-configuration as client-side
// `kubectl apply` (and GitOps tooling like Argo CD) writes it: a full copy of the spec.
const lastApplied = `{"apiVersion":"argoproj.io/v1alpha1","kind":"WorkflowTemplate","metadata":{"name":"my-tmpl","namespace":"my-ns"},"spec":{"entrypoint":"main","templates":[{"name":"main","container":{"image":"docker/whalesay"}}]}}`

// templateWithLastApplied builds an unstructured template carrying the
// last-applied-configuration annotation, as an Argo CD-managed cluster would serve it.
// kind selects WorkflowTemplate or ClusterWorkflowTemplate.
func templateWithLastApplied(t *testing.T, kind, name, namespace string) *unstructured.Unstructured {
	t.Helper()
	meta := metav1.ObjectMeta{
		Name:        name,
		Namespace:   namespace,
		Annotations: map[string]string{corev1.LastAppliedConfigAnnotation: lastApplied},
		ManagedFields: []metav1.ManagedFieldsEntry{
			{Manager: "argocd", Operation: metav1.ManagedFieldsOperationUpdate},
		},
	}
	spec := wfv1.WorkflowSpec{
		Entrypoint: "main",
		Templates: []wfv1.Template{
			{Name: "main", Container: &corev1.Container{Image: "docker/whalesay"}},
		},
	}
	var obj any
	switch kind {
	case workflow.WorkflowTemplateKind:
		obj = &wfv1.WorkflowTemplate{TypeMeta: typeMeta(kind), ObjectMeta: meta, Spec: spec}
	case workflow.ClusterWorkflowTemplateKind:
		obj = &wfv1.ClusterWorkflowTemplate{TypeMeta: typeMeta(kind), ObjectMeta: meta, Spec: spec}
	default:
		t.Fatalf("unknown kind %q", kind)
	}
	data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	require.NoError(t, err)
	return &unstructured.Unstructured{Object: data}
}

func typeMeta(kind string) metav1.TypeMeta {
	return metav1.TypeMeta{APIVersion: wfv1.SchemeGroupVersion.String(), Kind: kind}
}

func TestWorkflowTemplateInformerStripsLastAppliedConfiguration(t *testing.T) {
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme, templateWithLastApplied(t, workflow.WorkflowTemplateKind, "my-tmpl", "my-ns"))

	informer := NewTolerantWorkflowTemplateInformer(dynamicClient, time.Minute, "my-ns")
	stopCh := make(chan struct{})
	defer close(stopCh)
	go informer.Informer().Run(stopCh)
	require.True(t, cache.WaitForCacheSync(t.Context().Done(), informer.Informer().HasSynced))

	tmpl, err := informer.Lister().WorkflowTemplates("my-ns").Get("my-tmpl")
	require.NoError(t, err)
	assert.NotContains(t, tmpl.Annotations, corev1.LastAppliedConfigAnnotation)
	assert.Nil(t, tmpl.ManagedFields)
	assert.Equal(t, "docker/whalesay", tmpl.Spec.Templates[0].Container.Image)
}

func TestClusterWorkflowTemplateInformerStripsLastAppliedConfiguration(t *testing.T) {
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme, templateWithLastApplied(t, workflow.ClusterWorkflowTemplateKind, "my-tmpl", ""))

	informer := NewTolerantClusterWorkflowTemplateInformer(dynamicClient, time.Minute)
	stopCh := make(chan struct{})
	defer close(stopCh)
	go informer.Informer().Run(stopCh)
	require.True(t, cache.WaitForCacheSync(t.Context().Done(), informer.Informer().HasSynced))

	tmpl, err := informer.Lister().Get("my-tmpl")
	require.NoError(t, err)
	assert.NotContains(t, tmpl.Annotations, corev1.LastAppliedConfigAnnotation)
	assert.Nil(t, tmpl.ManagedFields)
	assert.Equal(t, "docker/whalesay", tmpl.Spec.Templates[0].Container.Image)
}
