package controller

import (
	"cmp"
	"context"
	"fmt"
	"sync"
	"time"

	apierr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"sigs.k8s.io/yaml"

	argoerrors "github.com/argoproj/argo-workflows/v4/errors"
	wfv1 "github.com/argoproj/argo-workflows/v4/pkg/apis/workflow/v1alpha1"
	errorsutil "github.com/argoproj/argo-workflows/v4/util/errors"
	"github.com/argoproj/argo-workflows/v4/util/intstr"
	"github.com/argoproj/argo-workflows/v4/util/logging"
)

// resourceWaitRequeue is how long a wait node waits between two checks of its
// target. The controller only observes the target when the workflow is
// reconciled, so this is also how long a deletion can go unnoticed.
const resourceWaitRequeue = 1 * time.Minute

// restMapperOnce guards lazy initialization of restMapper.
var restMapperOnce sync.Once

// restMapper maps kinds to resources for the wait action. It is built lazily
// from the controller's rest config so a controller that never runs a wait
// template pays no discovery cost.
var restMapper meta.RESTMapper

// waitTarget is the Kubernetes object a wait action watches, resolved once from
// the template's manifest.
type waitTarget struct {
	gvr       schema.GroupVersionResource
	namespace string
	name      string
}

// executeWaitResource runs the pod-free `action: wait` resource template in the
// workflow controller: no pod is created, the node is marked Running and the
// workflow requeued until the wait condition holds. The wait state is the node
// itself, persisted in the workflow status, so it survives controller restarts.
//
// A wait never blocks a reconciliation. Each operation checks the target once
// and, if the condition is not yet met, requeues the workflow.
func (woc *wfOperationCtx) executeWaitResource(ctx context.Context, nodeName string, templateScope string, tmpl *wfv1.Template, orgTmpl wfv1.TemplateReferenceHolder, opts *executeTemplateOpts) (*wfv1.NodeStatus, error) {
	node, err := woc.wf.GetNodeByName(nodeName)
	if err != nil {
		_, node = woc.initializeExecutableNode(ctx, nodeName, wfv1.NodeTypeWait, templateScope, tmpl, orgTmpl, opts.boundaryID, wfv1.NodeRunning, opts.nodeFlag, true)
	}

	target, err := resolveWaitTarget(tmpl.Resource.Manifest, woc.wf.Namespace)
	if err != nil {
		return node, err
	}

	// activeDeadlineSeconds bounds the wait: without a pod, whose
	// activeDeadlineSeconds the kubelet would enforce, the controller fails
	// the node itself once the deadline has passed.
	if deadline := woc.waitDeadline(node, tmpl); deadline != nil && time.Now().UTC().After(*deadline) {
		return woc.markNodePhase(ctx, nodeName, wfv1.NodeFailed, fmt.Sprintf("resource wait exceeded its %s deadline", tmpl.ActiveDeadlineSeconds)), nil
	}

	deleted, err := woc.isWaitTargetDeleted(ctx, target)
	if err != nil {
		// Transient API errors (apiserver briefly unavailable, a call that
		// timed out) are requeued rather than failed, like any other
		// controller-side API call.
		if errorsutil.IsTransientErr(ctx, err) {
			woc.log.WithError(err).WithField("nodeName", nodeName).Warn(ctx, "transient error waiting for resource deletion, requeueing")
			woc.requeue()
			return node, nil
		}
		return node, err
	}

	if !deleted {
		woc.log.WithFields(logging.Fields{
			"nodeName": nodeName,
			"waitFor":  tmpl.Resource.WaitFor,
			"resource": target.describe(),
		}).Info(ctx, "resource wait condition not yet met, requeueing")

		woc.requeueAfter(min(woc.waitRequeueDelay(node, tmpl), resourceWaitRequeue))
		return node, nil
	}

	woc.log.WithFields(logging.Fields{
		"nodeName": nodeName,
		"waitFor":  tmpl.Resource.WaitFor,
		"resource": target.describe(),
	}).Info(ctx, "resource wait condition met, completing node")

	return woc.markNodePhase(ctx, nodeName, wfv1.NodeSucceeded), nil
}

// isWaitTargetDeleted reports whether the target resource is fully deleted. A
// resource that never existed counts as deleted.
func (woc *wfOperationCtx) isWaitTargetDeleted(ctx context.Context, target *waitTarget) (bool, error) {
	_, err := woc.controller.dynamicInterface.Resource(target.gvr).Namespace(target.namespace).Get(ctx, target.name, metav1.GetOptions{})
	if apierr.IsNotFound(err) {
		return true, nil
	}
	return false, err
}

// waitRequeueDelay returns how long to wait before checking the target again:
// the wait's activeDeadlineSeconds when it has one, so the node fails as soon
// as its deadline passes, otherwise the default requeue.
func (woc *wfOperationCtx) waitRequeueDelay(node *wfv1.NodeStatus, tmpl *wfv1.Template) time.Duration {
	if deadline := woc.waitDeadline(node, tmpl); deadline != nil {
		return time.Until(*deadline)
	}
	return resourceWaitRequeue
}

// waitDeadline returns the absolute deadline of a wait node from the template's
// activeDeadlineSeconds, measured from the node's start time, or nil when the
// wait has no deadline.
func (woc *wfOperationCtx) waitDeadline(node *wfv1.NodeStatus, tmpl *wfv1.Template) *time.Time {
	seconds, err := intstr.Int(tmpl.ActiveDeadlineSeconds)
	if err != nil || seconds == nil {
		return nil
	}
	deadline := node.StartedAt.Add(time.Duration(*seconds) * time.Second)
	return &deadline
}

// resolveWaitTarget resolves the manifest of a wait action to the object it
// names. Unlike the executor's kubectl-based actions, the controller does not
// accept flags, so the manifest itself must identify the target. wfNamespace is
// the namespace used for namespaced resources whose manifest does not set one.
func resolveWaitTarget(manifest, wfNamespace string) (*waitTarget, error) {
	obj := unstructured.Unstructured{}
	// Unmarshal into a plain map first: unstructured.Unstructured's own
	// unmarshalling rejects a manifest without a kind before we can report it.
	var content map[string]any
	if err := yaml.Unmarshal([]byte(manifest), &content); err != nil {
		return nil, argoerrors.Errorf(argoerrors.CodeBadRequest, "failed to parse resource manifest: %v", err)
	}
	obj.SetUnstructuredContent(content)
	name := obj.GetName()
	gvk := obj.GroupVersionKind()
	if name == "" || gvk.Kind == "" {
		return nil, argoerrors.New(argoerrors.CodeBadRequest, "kind and name are both required but at least one of them is missing from the manifest")
	}

	gvr, namespaced := gvrForKind(gvk)
	namespace := ""
	if namespaced {
		namespace = cmp.Or(obj.GetNamespace(), wfNamespace)
	}
	return &waitTarget{gvr: gvr, namespace: namespace, name: name}, nil
}

// gvrForKind maps a kind to its resource, preferring the cluster's discovery
// data (which knows the real plural and scope) and falling back to the same
// plural-name guess the executor uses to build the self link it polls.
func gvrForKind(gvk schema.GroupVersionKind) (schema.GroupVersionResource, bool) {
	if mapper := getRestMapper(); mapper != nil {
		if mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version); err == nil {
			return mapping.Resource, mapping.Scope.Name() == meta.RESTScopeNameNamespace
		}
	}
	plural, _ := meta.UnsafeGuessKindToResource(gvk)
	return plural, true
}

// getRestMapper lazily builds the discovery-based REST mapper, once per
// process. A discovery failure leaves restMapper nil: gvrForKind then falls back
// to the plural-name guess, which is enough for most kinds.
func getRestMapper() meta.RESTMapper {
	restMapperOnce.Do(func() {
		if controllerRestConfig == nil {
			return
		}
		discoveryClient, err := discovery.NewDiscoveryClientForConfig(controllerRestConfig)
		if err != nil {
			return
		}
		cached := memory.NewMemCacheClient(discoveryClient)
		restMapper = restmapper.NewDeferredDiscoveryRESTMapper(cached)
	})
	return restMapper
}

// controllerRestConfig is the config getRestMapper builds its discovery client
// from; tests can swap it. It is set once, by NewWorkflowController.
var controllerRestConfig *rest.Config

func (t *waitTarget) describe() string {
	return fmt.Sprintf("%s/%s, namespace: %q", t.gvr.Resource, t.name, t.namespace)
}
