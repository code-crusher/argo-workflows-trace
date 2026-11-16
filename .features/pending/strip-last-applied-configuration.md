Description: Strip kubectl.kubernetes.io/last-applied-configuration from template informer caches
Authors: [Alan Clucas](https://github.com/Joibel)
Component: General
Issues: 16566

The workflow-controller now removes the `kubectl.kubernetes.io/last-applied-configuration` annotation from WorkflowTemplates and ClusterWorkflowTemplates before storing them in its informer caches.
Client-side `kubectl apply` (used by Argo CD and other GitOps tooling) writes that annotation as a full copy of the object's spec — measured at ~44% of a typical WorkflowTemplate — and nothing in the controller reads it.
On GitOps-managed installs this is the dominant per-template cache overhead.
It is stripped only from informers whose objects the controller never writes back whole (the template informers); objects stored in the cluster are unaffected.
