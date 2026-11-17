Description: Store memoized step outputs in PostgreSQL or MySQL instead of ConfigMaps
Authors: [Argo Workflows Contributors](https://github.com/argoproj/argo-workflows)
Component: General
Issues: 15952

Memoization normally stores step outputs in a ConfigMap, which caps each entry at 1MB and creates one ConfigMap object per cache per key.
You can now store memoized outputs in a PostgreSQL, MySQL, or MariaDB database instead by setting a `memoization` section in the `workflow-controller-configmap`.
ConfigMaps remain the default, so nothing changes unless you set `memoization`.

```yaml
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

The controller creates and migrates the `memoization_cache` table automatically at startup, or when you change the `memoization` config, so no manual schema setup is needed.
Templates do not change: the `configMap` name in the `memoize` block becomes a partition key in the cache table instead of a ConfigMap name.
Set `cacheTTL` to keep entries for as long as you want (for example `720h` for a month); entries older than `cacheTTL` are ignored and removed by garbage collection.
Set `CACHE_GC_PERIOD` to a non-zero duration to enable garbage collection, and `CACHE_GC_AFTER_NOT_HIT_DURATION` to control how long an entry can go unhit before it is deleted.
If the controller cannot connect to the database or the migration fails, memoization falls back to the ConfigMap cache and the error is logged.
See [Memoization](https://argoproj.github.io/argo-workflows/memoization.html#database-cache) for details.
