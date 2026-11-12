Description: The `--managed-namespace` flag of the workflow-controller and the Argo Server accepts a comma-separated allowlist of namespaces, so one installation can watch several namespaces without cluster-wide RBAC
Authors: [Claude](https://claude.com/claude-code)
Component: General
Issues: 16980

Pass `--managed-namespace=ns-1,ns-2,ns-3` (with `--namespaced`) to both the workflow-controller and the Argo Server to watch exactly those namespaces.
The controller creates one informer per namespace and reconciles Workflows in all of them; the server watches the same namespaces and keeps its per-request access control.
Compared with a cluster-scoped install this stays on namespace Roles (one per managed namespace) instead of a ClusterRole, at the cost of one list/watch connection per namespace.
See [Managed Namespace](managed-namespace.md).
