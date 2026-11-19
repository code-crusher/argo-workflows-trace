package controller

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
	"sigs.k8s.io/yaml"

	wfv1 "github.com/argoproj/argo-workflows/v4/pkg/apis/workflow/v1alpha1"
	argointstr "github.com/argoproj/argo-workflows/v4/util/intstr"
	"github.com/argoproj/argo-workflows/v4/util/logging"
)

var configMapGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}

const waitCMManifest = `apiVersion: v1
kind: ConfigMap
metadata:
  name: resource-wait-cm
`

// waitWF returns a workflow whose single template waits for the deletion of
// the ConfigMap named resource-wait-cm, with activeDeadlineSeconds set when
// deadlineSeconds is not empty.
func waitWF(deadlineSeconds string) *wfv1.Workflow {
	wf := wfv1.MustUnmarshalWorkflow("@testdata/operator/resource-wait-template.yaml")
	if deadlineSeconds != "" {
		wf.Spec.Templates[0].ActiveDeadlineSeconds = argointstr.ParsePtr(deadlineSeconds)
	}
	return wf
}

// createWaitWF creates a controller and the wait workflow in it, seeding the
// ConfigMap the wait targets when withCM is set. Both live in the "default"
// namespace.
func createWaitWF(t *testing.T, ctx context.Context, deadlineSeconds string, withCM bool) (*WorkflowController, *wfv1.Workflow) {
	t.Helper()
	wf := waitWF(deadlineSeconds)
	wf.Namespace = "default"
	var objects []any
	if withCM {
		cm := &unstructured.Unstructured{}
		require.NoError(t, yaml.Unmarshal([]byte(waitCMManifest), cm))
		cm.SetNamespace(wf.Namespace)
		objects = append(objects, cm)
	}
	cancel, controller := newController(logging.TestContext(t.Context()), objects...)
	t.Cleanup(cancel)
	created, err := controller.wfclientset.ArgoprojV1alpha1().Workflows(wf.Namespace).Create(ctx, wf, metav1.CreateOptions{})
	require.NoError(t, err)
	return controller, created
}

func getWF(t *testing.T, ctx context.Context, controller *WorkflowController, wf *wfv1.Workflow) *wfv1.Workflow {
	t.Helper()
	updated, err := controller.wfclientset.ArgoprojV1alpha1().Workflows(wf.Namespace).Get(ctx, wf.Name, metav1.GetOptions{})
	require.NoError(t, err)
	return updated
}

// getWaitNode returns the workflow's wait node.
func getWaitNode(t *testing.T, wf *wfv1.Workflow) *wfv1.NodeStatus {
	t.Helper()
	node := wf.Status.Nodes.Find(func(n wfv1.NodeStatus) bool {
		return n.Type == wfv1.NodeTypeWait
	})
	require.NotNil(t, node, "no wait node found")
	return node
}

// assertNoPods asserts the wait created no pods: it runs entirely in the
// controller.
func assertNoPods(t *testing.T, ctx context.Context, controller *WorkflowController, wf *wfv1.Workflow) {
	t.Helper()
	pods, err := controller.kubeclientset.CoreV1().Pods(wf.Namespace).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, pods.Items, "a wait action must not create a pod")
}

func TestResourceWaitTemplateNoResource(t *testing.T) {
	ctx := logging.TestContext(t.Context())
	controller, wf := createWaitWF(t, ctx, "", false)

	woc := newWorkflowOperationCtx(ctx, wf, controller)
	woc.operate(ctx)

	wf = getWF(t, ctx, controller, wf)
	// The resource never existed, so the wait succeeds immediately...
	assert.Equal(t, wfv1.WorkflowSucceeded, wf.Status.Phase)
	assert.Equal(t, wfv1.NodeSucceeded, getWaitNode(t, wf).Phase)
	// ...without creating any pod.
	assertNoPods(t, ctx, controller, wf)
}

func TestResourceWaitTemplateWaitsThenSucceeds(t *testing.T) {
	ctx := logging.TestContext(t.Context())
	controller, wf := createWaitWF(t, ctx, "", true)

	newWorkflowOperationCtx(ctx, wf, controller).operate(ctx)

	wf = getWF(t, ctx, controller, wf)
	// The resource still exists: the node stays running, no pod is created.
	assert.Equal(t, wfv1.WorkflowRunning, wf.Status.Phase)
	assert.Equal(t, wfv1.NodeRunning, getWaitNode(t, wf).Phase)
	assertNoPods(t, ctx, controller, wf)

	// Delete the resource out from under the wait, as an operator would.
	require.NoError(t, controller.dynamicInterface.Resource(configMapGVR).Namespace(wf.Namespace).Delete(ctx, "resource-wait-cm", metav1.DeleteOptions{}))

	newWorkflowOperationCtx(ctx, wf, controller).operate(ctx)

	wf = getWF(t, ctx, controller, wf)
	// The wait observes the deletion and completes the workflow.
	assert.Equal(t, wfv1.WorkflowSucceeded, wf.Status.Phase)
	assert.Equal(t, wfv1.NodeSucceeded, getWaitNode(t, wf).Phase)
}

func TestResourceWaitTemplateExceedsDeadline(t *testing.T) {
	ctx := logging.TestContext(t.Context())
	controller, wf := createWaitWF(t, ctx, "1", true)

	newWorkflowOperationCtx(ctx, wf, controller).operate(ctx)
	wf = getWF(t, ctx, controller, wf)
	assert.Equal(t, wfv1.NodeRunning, getWaitNode(t, wf).Phase)

	// Reconcile only after the one-second deadline has passed.
	time.Sleep(2 * time.Second)
	newWorkflowOperationCtx(ctx, wf, controller).operate(ctx)

	wf = getWF(t, ctx, controller, wf)
	node := getWaitNode(t, wf)
	assert.Equal(t, wfv1.NodeFailed, node.Phase)
	assert.Contains(t, node.Message, "deadline")
	assert.Equal(t, wfv1.WorkflowFailed, wf.Status.Phase)
	assertNoPods(t, ctx, controller, wf)
}

func TestResourceWaitTemplateTransientError(t *testing.T) {
	ctx := logging.TestContext(t.Context())
	controller, wf := createWaitWF(t, ctx, "", true)

	newWorkflowOperationCtx(ctx, wf, controller).operate(ctx)
	wf = getWF(t, ctx, controller, wf)
	require.Equal(t, wfv1.NodeRunning, getWaitNode(t, wf).Phase)

	// Make the target's Get fail with a transient, server-side error.
	dynamicClient := controller.dynamicInterface.(*dynamicfake.FakeDynamicClient)
	dynamicClient.PrependReactor("get", "configmaps",
		func(ktesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierr.NewServiceUnavailable("apiserver is having a moment")
		})

	newWorkflowOperationCtx(ctx, wf, controller).operate(ctx)

	wf = getWF(t, ctx, controller, wf)
	// The wait rides out the transient error instead of failing.
	assert.Equal(t, wfv1.NodeRunning, getWaitNode(t, wf).Phase)
	assert.Equal(t, wfv1.WorkflowRunning, wf.Status.Phase)
}

func TestResolveWaitTarget(t *testing.T) {
	// The wait runs in the controller, so tests exercise the plural-name
	// fallback the executor also uses for the self link it polls.
	require.Nil(t, getRestMapper())
	tests := []struct {
		name        string
		manifest    string
		wfNamespace string
		wantErr     string
		wantGVR     string
		wantNS      string
		wantName    string
	}{{
		name:        "namespaced resource without namespace",
		manifest:    waitCMManifest,
		wfNamespace: "argo",
		wantGVR:     "/v1, Resource=configmaps",
		wantNS:      "argo",
		wantName:    "resource-wait-cm",
	}, {
		name: "namespaced resource with namespace",
		manifest: `apiVersion: batch/v1
kind: Job
metadata:
  name: my-job
  namespace: elsewhere
`,
		wantGVR:  "batch/v1, Resource=jobs",
		wantNS:   "elsewhere",
		wantName: "my-job",
	}, {
		name: "custom resource",
		manifest: `apiVersion: stable.example.com/v1
kind: CronTab
metadata:
  name: my-crontab
`,
		wfNamespace: "argo",
		wantGVR:     "stable.example.com/v1, Resource=crontabs",
		wantNS:      "argo",
		wantName:    "my-crontab",
	}, {
		name: "cluster-scoped resource",
		manifest: `apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: fast
`,
		wantGVR:  "storage.k8s.io/v1, Resource=storageclasses",
		wantNS:   "",
		wantName: "fast",
	}, {
		name:     "missing name",
		manifest: "apiVersion: v1\nkind: ConfigMap\n",
		wantErr:  "kind and name are both required",
	}, {
		name:     "missing kind",
		manifest: "apiVersion: v1\nmetadata:\n  name: my-cm\n",
		wantErr:  "kind and name are both required",
	}, {
		name:     "invalid yaml",
		manifest: "apiVersion: [",
		wantErr:  "failed to parse resource manifest",
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			target, err := resolveWaitTarget(tc.manifest, tc.wfNamespace)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantGVR, target.gvr.String())
			assert.Equal(t, tc.wantNS, target.namespace)
			assert.Equal(t, tc.wantName, target.name)
		})
	}
}
