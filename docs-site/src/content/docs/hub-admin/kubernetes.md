---
title: Running Scion on Kubernetes
---

Scion supports running agents as Pods in a Kubernetes cluster. This enables remote execution, resource management, and scaling beyond a single machine.

## Prerequisites

- A running Kubernetes cluster (GKE, EKS, AKS, or self-managed).
- `kubectl` configured with access to the target cluster.
- Scion agent images available to the cluster (pushed to a container registry accessible by the cluster).
- RBAC permissions as described in the [Required Permissions](#required-permissions) section.

Use `scion doctor` to verify prerequisites before starting agents.

## Configuration

Configure the Kubernetes runtime in your global `~/.scion/settings.yaml`:

```yaml
runtimes:
  k8s:
    type: kubernetes
    context: my-cluster-context    # kubectl context (optional, defaults to current)
    namespace: scion-agents        # target namespace (default: "default")
    gke: false                     # enable GKE-specific features
    list_all_namespaces: false     # list agents across all namespaces

profiles:
  default:
    runtime: k8s
```

### Agent-Level Kubernetes Configuration

Per-agent or per-template Kubernetes settings in `~/.scion/settings.yaml`:

```yaml
kubernetes:
  namespace: custom-namespace          # override runtime namespace
  context: alternate-context           # override runtime context
  serviceAccountName: agent-sa         # Workload Identity / IRSA
  runtimeClassName: gvisor             # sandboxed runtime (gVisor, Kata, etc.)
  imagePullPolicy: IfNotPresent        # Always, IfNotPresent, or Never
  nodeSelector:
    pool: agents
    accelerator: gpu
  tolerations:
    - key: dedicated
      operator: Equal
      value: agents
      effect: NoSchedule
  resources:
    requests:
      nvidia.com/gpu: "1"
    limits:
      nvidia.com/gpu: "1"
```

### Resource Configuration

Standard compute resources use the common `resources` field:

```yaml
resources:
  requests:
    cpu: "500m"
    memory: "1Gi"
  limits:
    cpu: "2"
    memory: "4Gi"
  disk: "20Gi"    # maps to ephemeral-storage (both requests and limits)
```

Extended resources (GPUs, custom devices) use `kubernetes.resources`.

### GKE Workload Identity

When running in Google Kubernetes Engine (GKE), Scion natively supports Workload Identity for secure access to GCP APIs (like Vertex AI or Cloud Storage) without passing long-lived service account keys.

1. Enable the `gke: true` flag in your runtime configuration.
2. Ensure your cluster is configured with Workload Identity.
3. Bind a Kubernetes Service Account to a Google Service Account.
4. Set the `serviceAccountName` in the agent's Kubernetes configuration to match the bound KSA.

This provides the agent container with an ambient identity, which the underlying harness (e.g., Gemini or Claude via Vertex) can automatically resolve using Application Default Credentials (ADC).

## Support Matrix

### Volume Types

| Volume Type | Status | Notes |
|---|---|---|
| EmptyDir (workspace) | Supported | Default workspace volume, always created |
| GCS FUSE CSI | Supported | Requires `gcsfuse.csi.storage.gke.io` CSI driver; GKE only |
| Local/bind-mount | Not supported | Logged as warning, skipped. Use tar sync instead |
| PersistentVolumeClaim | Not supported | Future enhancement |

### Secret Modes

| Mode | Status | Prerequisites |
|---|---|---|
| Native K8s Secret | Supported (default) | Secret create/delete RBAC |
| GKE Secret Store CSI | Supported | `gke: true`, Secrets Store CSI Driver + GCP provider, SecretProviderClass CRD |
| ResolvedAuth files | Supported | Injected via K8s Secret volumes (not hostPath) |

Secrets are composable: `ResolvedAuth` and `ResolvedSecrets` are applied independently (not mutually exclusive).

### Sync Modes

| Mode | Status | Notes |
|---|---|---|
| Tar snapshot | Supported | Default. Full workspace snapshot via `pods/exec` streaming |
| GCS volume sync | Supported | For GCS-mounted volumes via `gcloud storage rsync` |

Tar sync includes retry with exponential backoff (1s, 2s, 4s — up to 3 retries) for transient errors (connection resets, broken pipes, timeouts).

### Pod Spec Features

| Feature | Status |
|---|---|
| Resource requests/limits | Supported |
| Extended resources (GPUs) | Supported |
| Ephemeral storage (disk) | Supported (requests + limits) |
| RuntimeClassName | Supported |
| ServiceAccountName | Supported |
| NodeSelector | Supported |
| Tolerations | Supported |
| ImagePullPolicy | Supported (Always, IfNotPresent, Never) |
| FSGroup security context | Supported (auto-set from host GID) |

### Namespace Management

| Feature | Status |
|---|---|
| Default namespace | Supported |
| Per-agent namespace | Supported (via config or labels) |
| Multi-namespace listing | Supported (`list_all_namespaces: true`) |
| Namespace/pod ID format | Supported (`namespace/podname` for all operations) |
| Namespace annotation | Supported (`scion.namespace` persisted on pod) |

## Required Permissions

The user or service account running scion needs the following RBAC permissions in the target namespace:

### Minimum RBAC

| Resource | Verbs |
|---|---|
| pods | create, get, list, delete |
| pods/exec | create |
| pods/log | get |
| secrets | create, list, delete |

### Additional for GKE Mode

| Resource | Verbs |
|---|---|
| secretproviderclasses (secrets-store.csi.x-k8s.io) | create, list, delete |

### Additional for Multi-Namespace

| Resource | Verbs |
|---|---|
| namespaces | get, list |
| pods (cluster-wide) | list |

### Example ClusterRole

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: scion-agent-manager
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["create", "get", "list", "delete"]
- apiGroups: [""]
  resources: ["pods/exec", "pods/log"]
  verbs: ["create", "get"]
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["create", "list", "delete"]
- apiGroups: [""]
  resources: ["namespaces"]
  verbs: ["get", "list"]
```

## Execution Flow

1. **Start**: `scion start` creates a Pod with the configured image, resources, and secrets.
2. **Sync**: Workspace and agent home are transferred to the Pod via tar streaming over `pods/exec`.
3. **Ready**: Pod readiness is polled with detailed error classification (image pull, scheduling, config errors).
4. **Attach**: `scion attach` connects to the tmux session inside the Pod via `pods/exec`.
5. **Sync back**: `scion sync from <agent>` retrieves workspace changes via tar streaming.
6. **Delete**: `scion rm <agent>` deletes the Pod and associated Secrets/SecretProviderClasses.

## Diagnostics

Run `scion doctor` to verify your Kubernetes runtime configuration:

```bash
scion doctor
```

This checks:
- Cluster connectivity and authentication
- Namespace existence and access
- Pod CRUD and exec permissions
- Secret management permissions
- (GKE mode) SecretProviderClass CRD availability
- (GKE mode) Secrets Store CSI driver installation
- (GKE mode) GCS FUSE CSI driver installation

Use `scion doctor --format json` for machine-readable output.

## Error Handling

The Kubernetes runtime provides structured error messages with remediation hints:

| Error | Remediation |
|---|---|
| ImagePullBackOff / ErrImagePull | Verify image name and registry access; check `imagePullPolicy` |
| InvalidImageName | Check image name format |
| CreateContainerConfigError | Check secret references and volume mounts |
| CrashLoopBackOff | Check container logs with `scion logs` |
| Unschedulable | Check node selectors, tolerations, and resource availability |
| Invalid resource values | Error includes the field name and invalid value |

## Limitations

- Workspace sync uses tar snapshots (not live filesystem). Changes require explicit `scion sync`.
- Local/bind-mount volumes are not supported on remote clusters.
- Pod networking depends on cluster CNI configuration.
- Authentication credentials must be propagated via Secrets or Workload Identity.
