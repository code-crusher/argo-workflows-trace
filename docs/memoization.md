# Step Level Memoization

## Introduction

Workflows often have outputs that are expensive to compute.
Memoization reduces cost and workflow execution time by recording the result of previously run steps:
it stores the outputs of a template into a specified cache with a variable key.

All steps can be memoized, whether or not they have outputs.

## Cache Method

Currently, the cached data is stored in config-maps.
This allows you to easily manipulate cache entries manually through `kubectl` and the Kubernetes API without having to go through Argo.
All cache config-maps must have the label `workflows.argoproj.io/configmap-type: Cache` to be used as a cache. This prevents accidental access to other important config-maps in the system

## Using Memoization

Memoization is set at the template level. You must specify a `key`, which can be static strings but more often depend on inputs.
You must also specify a name for the `config-map` cache.
Optionally you can set a `maxAge` in seconds or hours (e.g. `180s`, `24h`) to define how long should it be considered valid. If an entry is older than the `maxAge`, it will be ignored.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
   generateName: memoized-workflow-
spec:
   entrypoint: print-message
   templates:
      - name: print-message
        memoize:
           key: "{{inputs.parameters.message}}"
           maxAge: "10s"
           cache:
              configMap:
                 name: print-message-cache
```

[Find a simple example for memoization here](https://github.com/argoproj/argo-workflows/blob/main/examples/memoize-simple.yaml).

!!! Note
    In order to use memoization it is necessary to add the verbs `create` and `update` to the `configmaps` resource for the appropriate (cluster) roles. In the case of a cluster install the `argo-cluster-role` cluster role should be updated, whilst for a namespace install the `argo-role` role should be updated.

## FAQ

1. If you see errors like `error creating cache entry: ConfigMap \"reuse-task\" is invalid: []: Too long: must have at most 1048576 characters`,
   this is due to [the 1MB limit placed on the size of `ConfigMap`](https://github.com/kubernetes/kubernetes/issues/19781).
   Here are a couple of ways that might help resolve this:
    * Delete the existing `ConfigMap` cache or switch to use a different cache.
    * Reduce the size of the output parameters for the nodes that are being memoized.
    * Split your cache into different memoization keys and cache names so that each cache entry is small.

## Database cache

By default the cache is stored in ConfigMaps.
For large-scale use, you can instead store memoization entries in a PostgreSQL or MySQL database.
The database backend removes the 1MB ConfigMap size limit and avoids creating many ConfigMap objects.
ConfigMaps remain the default; the database backend is opt-in.

The cache still uses the `configMap` name from the template's `memoize` block to identify which cache an entry belongs to.
With the database backend configured, that name becomes a partition key in the cache table instead of a ConfigMap name.
No changes to your templates are needed.

To configure the database, set the `memoization` section in the `workflow-controller-configmap` ConfigMap.
The database can be PostgreSQL, MySQL, or MariaDB.
See [the `MemoizationConfig` section](workflow-controller-configmap.md#memoizationconfig) for all options.

The `workflow-controller-configmap` ConfigMap can carry each section as its own key, so you can add a `memoization` key next to the existing keys:

```yaml title="workflow-controller-configmap.yaml"
apiVersion: v1
kind: ConfigMap
metadata:
  name: workflow-controller-configmap
data:
  memoization: |
    postgresql:
      host: postgres
      port: 5432
      database: argo
      userNameSecret:
        name: argo-postgres-config
        key: username
      passwordSecret:
        name: argo-postgres-config
        key: password
    cacheTTL: 720h
```

The controller creates and migrates the `memoization_cache` table automatically at startup.
Set `skipMigration: true` if you manage the schema yourself, and `tableName` to use a different table name.
For the list of SQL statements applied during migration, see [Database Migrations](database-migrations.md).

`cacheTTL` sets the maximum age of a cache entry.
Entries older than `cacheTTL` are ignored when the controller reads the cache, and are deleted when garbage collection runs.
If `cacheTTL` is not set, entries do not expire based on age.
In both cases, entries that have not been hit for the duration configured by the `CACHE_GC_AFTER_NOT_HIT_DURATION` environment variable (default 30 seconds) are deleted when garbage collection runs, and garbage collection itself runs every `CACHE_GC_PERIOD`.
Set `CACHE_GC_PERIOD` to a non-zero duration to enable garbage collection; it is disabled by default.

If the controller cannot connect to the database or the migration fails, memoization falls back to the ConfigMap cache and the error is logged.

You can delete entries manually with SQL, for example:

```sql
DELETE FROM memoization_cache WHERE cache = 'print-message-cache';
```
