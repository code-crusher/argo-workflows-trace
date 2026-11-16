// Package informer provides helpers shared by the informers used in the
// workflow-controller and argo-server.
package informer

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/tools/cache"
)

// StripManagedFields is a cache.TransformFunc that removes
// metadata.managedFields before objects enter an informer cache.
// Nothing reads managedFields from the cache, and the API server ignores
// a nil managedFields on non-apply updates, so this is loss-free. It can
// be 20% or more of the object size.
func StripManagedFields(i any) (any, error) {
	obj, err := meta.Accessor(i)
	// err != nil: tombstones (DeletedFinalStateUnknown) etc — pass through.
	// The nil-check avoids mutating objects that already lack managedFields
	// (kubernetes/kubernetes#124337).
	if err == nil && obj.GetManagedFields() != nil {
		obj.SetManagedFields(nil)
	}
	return i, nil
}

// StripLastAppliedConfiguration is a cache.TransformFunc that removes the
// kubectl.kubernetes.io/last-applied-configuration annotation, which
// client-side `kubectl apply` (Argo CD and other GitOps tooling) stores as a
// full copy of the object's spec. Nothing in this repo reads it, so dropping
// it from the cache is loss-free for read-only informers — but it must never
// be used where a cached object is written back whole: the annotation would be
// removed from the live object, breaking subsequent client-side-apply
// three-way merges (#16566). It can be ~44% of a WorkflowTemplate's size.
func StripLastAppliedConfiguration(i any) (any, error) {
	obj, err := meta.Accessor(i)
	// err != nil: tombstones (DeletedFinalStateUnknown) etc — pass through.
	// The nil-check avoids re-marshalling annotations that lack it.
	if err == nil && obj.GetAnnotations()[corev1.LastAppliedConfigAnnotation] != "" {
		// The annotations map may be shared (e.g. after a DeepCopy), so copy
		// it before deleting the key rather than mutating in place.
		annotations := make(map[string]string, len(obj.GetAnnotations()))
		for k, v := range obj.GetAnnotations() {
			if k != corev1.LastAppliedConfigAnnotation {
				annotations[k] = v
			}
		}
		obj.SetAnnotations(annotations)
	}
	return i, nil
}

// ChainTransforms composes cache.TransformFuncs: each runs on the previous
// one's output, in order, and the first error aborts the chain.
func ChainTransforms(transforms ...cache.TransformFunc) cache.TransformFunc {
	return func(i any) (any, error) {
		var err error
		for _, transform := range transforms {
			i, err = transform(i)
			if err != nil {
				return nil, err
			}
		}
		return i, nil
	}
}
