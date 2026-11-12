# Managed Namespace

You can install Argo in either namespace scoped or cluster scoped configurations.
The main difference is whether you install Roles or ClusterRoles, respectively.

In namespace scoped configuration, you must run both the Workflow Controller and Argo Server using `--namespaced`.
If you want to run workflows in a separate namespace, add `--managed-namespace` as well.
(In cluster scoped configuration, _don't_ include `--namespaced` or `--managed-namespace`.)

For example:

```yaml
      - args:
        - --configmap
        - workflow-controller-configmap
        - --executor-image
        - quay.io/argoproj/argoexec:v4.1.5
        - --namespaced
        - --managed-namespace
        - default
```

## Managing multiple namespaces

`--managed-namespace` also accepts a comma-separated allowlist of namespaces, so one
installation can run Workflows in several namespaces without cluster-wide access:

```yaml
      - args:
        - --configmap
        - workflow-controller-configmap
        - --executor-image
        - quay.io/argoproj/argoexec:v4.1.5
        - --namespaced
        - --managed-namespace
        - ns-1,ns-2,ns-3
```

Pass the same allowlist to both the Workflow Controller and the Argo Server.
The controller creates one informer per namespace and reconciles Workflows in all of them.
The server watches the same namespaces and keeps its per-request access control.
Compared with a cluster-scoped install, this stays on namespace Roles (one per managed namespace) instead of a ClusterRole, at the cost of one list/watch connection per namespace.

Please note that both cluster scoped and namespace scoped configurations require "admin" roles to install because Argo's Custom Resource Definitions (CRDs) must be created (CRDs are cluster scoped objects).

!!! Info "Example Use Case"
    You can use a managed namespace install if you want some users or services to run Workflows without granting them privileges in the namespace where Argo Workflows is installed.
    For example, if you only run CI/CD Workflows that are maintained by the same team that manages the Argo Workflows installation, you may want a namespace install.
    But if all the Workflows are run by a separate data science team, you may want to give them a "data-science-workflows" namespace and use a managed namespace install of Argo Workflows in another namespace.
