# Build Caches

Build tools keep a writable cache in a home directory, such as Gradle's `GRADLE_USER_HOME`.
Sharing that directory between pods looks like a cheap way to warm the cache once and reuse it everywhere.
For tools that lock files inside the cache, sharing a writable directory across pods does not work, and the tool fails at startup.

## Why a shared writable cache fails across pods

Gradle coordinates access to `GRADLE_USER_HOME/caches` with file locks.
A process that finds a cache locked reads the owner's PID, lock ID and listening port from the lock file, then sends a UDP datagram to that port to ask the owner to release the lock.
The owner releases the lock when the datagram arrives, immediately if no cache operation is in progress, or when the current operation finishes.

Gradle assumes the owner runs on the same host, where its port is reachable.
Every pod has its own network namespace, so the datagram goes to the contending pod's own loopback interface, where nothing listens.
The owner never receives the request, so it keeps the lock while it uses the cache, and the contending build fails when the lock timeout expires.
Gradle 7.4 uses a default lock timeout of 60 seconds.

The storage backend does not matter.
The same timeout happens with a `hostPath` on a single node and with a ReadWriteMany PVC, because the contending pod cannot reach the owning process.
Pods with `hostNetwork: true` on the same node share the node's network namespace, so they can reach each other, but the timeout still happens across nodes.

The failure looks like this:

```text
Gradle could not start your build.
> Could not create service of type FileAccessTimeJournal using GradleUserHomeScopeServices.createFileAccessTimeJournal().
   > Timeout waiting to lock journal cache (/opt/workflow-data/gradlew/caches/journal-1). It is currently in use by another Gradle instance.
     Owner PID: 41
     Our PID: 41
     Owner Operation:
     Our operation:
     Lock file: /opt/workflow-data/gradlew/caches/journal-1/journal-1.lock
```

### Owner PID and Our PID

`Owner PID` and `Our PID` can be the same number, which makes the error look like a self-deadlock.
Each pod has its own PID namespace, so the Gradle process in every pod has a low PID such as `41`.
The two PIDs belong to different processes in different pods.
Gradle reports the owner's PID from the lock file, and the values only match because the namespaces are separate.

## Sharing a cache safely

You can share a build cache between pods in one of these ways:

| Layout | Concurrency |
|--------|-------------|
| A private writable home per pod, plus a shared read-only dependency cache | Any number of pods |
| One shared writable home, protected by a mutex | One build at a time |

### Private home with a shared read-only cache

Give each pod its own writable home on an `emptyDir` volume, and take dependencies from a shared cache that every pod mounts read-only.
Gradle reads the shared cache without locking it, so concurrent builds do not contend.

Gradle reads the shared cache from the directory in `GRADLE_RO_DEP_CACHE`, which must contain a `modules-2` directory.
Populate that directory with a build that uses the same Gradle version, and copy only the `modules-2` directory, without the `*.lock` and `gc.properties` files.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: gradle-cache-
spec:
  entrypoint: build
  volumes:
    - name: gradle-deps
      persistentVolumeClaim:
        claimName: gradle-deps
        readOnly: true
  templates:
    - name: build
      container:
        image: gradle:7.4-jdk17
        command: [sh, -c]
        args: ["gradle --no-daemon build"]
        env:
          - name: GRADLE_USER_HOME
            value: /home/gradle/.gradle
          - name: GRADLE_RO_DEP_CACHE
            value: /opt/gradle-deps
        volumeMounts:
          - name: gradle-home
            mountPath: /home/gradle/.gradle
          - name: gradle-deps
            mountPath: /opt/gradle-deps
            readOnly: true
      volumes:
        - name: gradle-home
          emptyDir: { }
```

A `hostPath` volume works in place of the PVC, as long as you seed the path on every node that runs the pod.
The pod's own writes go to the `emptyDir`, so they are lost when the pod is deleted.

### Serializing access to a shared writable cache

If you must share one writable home, allow only one build to use it at a time.
A [mutex](synchronization.md#template-level-synchronization) on the build template does this, including across Workflows:

```yaml
templates:
  - name: build
    synchronization:
      mutexes:
        - name: gradle-user-home
    container:
      image: gradle:7.4-jdk17
      # ...
```

The builds wait for the mutex instead of failing.
Use a [semaphore](synchronization.md#template-level-synchronization) instead of a mutex only if the tool tolerates concurrent writers.
A local mutex only covers Workflows that the controller running it reconciles, so use a [database-backed mutex](synchronization.md#multiple-controller-locks) if more than one controller runs Workflows at the same time.

## Other build tools

The same reasoning applies to any tool that takes locks inside a directory you share.
Prefer a private writable home per pod, and share only the parts of the cache that the tool reads without locking.
