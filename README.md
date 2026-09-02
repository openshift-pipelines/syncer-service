# Kueue Workload Controller

A Knative-based Kubernetes controller that reconciles [Kueue](https://kueue.sigs.k8s.io/) Workload CRDs that are owned by Tekton PipelineRuns.

## Overview

This controller watches Kueue Workload resources that are owned by Tekton PipelineRuns and automatically syncs required secrets from the hub cluster to spoke clusters. It enables seamless multi-cluster pipeline execution by ensuring PipelineRuns have the necessary authentication secrets available on their target clusters.

## Features

- **Automatic Secret Syncing**: Syncs Git authentication secrets from hub to spoke clusters
- **Multi-Cluster Support**: Works with Kueue's MultiKueue for distributed workload execution
- **Selective Processing**: Only handles Workloads owned by Tekton PipelineRuns
- **Production Ready**: Includes RBAC, deployment manifests, and Docker support

## Prerequisites

- Go 1.22 or later
- Kubernetes cluster with:
  - [Kueue](https://kueue.sigs.k8s.io/) installed
  - [Tekton Pipelines](https://tekton.dev/) installed
- kubectl configured to access your cluster
- Docker for building images

## Installation

### 1. Build and Vendor Dependencies

```bash
make tidy    # Run go mod tidy
make vendor  # Run go mod vendor
```

### 2. Build Binary Locally

```bash
make build
```

### 3. Build Docker Image

```bash
# Build for local architecture
make docker-build

# Or build and push multi-arch image
make docker-buildx IMG=your-registry/workload-controller:latest
```

### 4. Deploy to Kubernetes

```bash
# Apply RBAC and deployment
make deploy

# Check status
make status

# View logs
make logs
```

## Development

### Local Development

```bash
# Format code
make fmt

# Run linters
make vet

# Run tests
make test

# Run controller locally (requires kubeconfig)
make run
```

### Quick Development Cycle

```bash
# Build, dockerize, and deploy in one command
make quick-deploy
```

## Configuration

### Environment Variables

The controller reads these environment variables (set in `config/deployment.yaml`):

- `SYSTEM_NAMESPACE`: Namespace where the controller runs
- `CONFIG_LOGGING_NAME`: ConfigMap name for logging configuration
- `CONFIG_OBSERVABILITY_NAME`: ConfigMap name for observability configuration
- `METRICS_DOMAIN`: Domain for metrics reporting

### RBAC Permissions

The controller requires access to:

- Kueue Workloads (read and watch)
- Tekton PipelineRuns (read and watch)
- Secrets (read access on the hub; spoke writes use spoke-cluster credentials)
- MultiKueueClusters (read for cluster connection details)
- ConfigMaps and Leases (for controller configuration and leader election)

## How It Works

When a PipelineRun is scheduled to run on a spoke cluster via Kueue MultiKueue:

1. The controller detects the Workload resource associated with the PipelineRun
2. Retrieves the Git authentication secret specified in the PipelineRun's annotations
3. Syncs the secret from the hub cluster to the target spoke cluster
4. Ensures the secret has proper ownership for lifecycle management

The PipelineRun can then access the authentication secret on the spoke cluster to clone repositories and execute pipeline tasks.

## Makefile Targets

```
help          - Display available targets
fmt           - Run go fmt
vet           - Run go vet
test          - Run tests
build         - Build binary
run           - Run locally
tidy          - Run go mod tidy
vendor        - Run go mod vendor
docker-build  - Build docker image
docker-push   - Push docker image
docker-buildx - Build multi-arch image
deploy        - Deploy to cluster
undeploy      - Remove from cluster
logs          - Show controller logs
status        - Show controller status
clean         - Clean build artifacts
all           - Full workflow: vendor, build, push, deploy
quick-deploy  - Quick local development cycle
```

## Troubleshooting

### Check Controller Status

```bash
kubectl get deployment workload-controller -n syncer-service
kubectl get pods -n syncer-service -l app=workload-controller
```

### View Logs

```bash
kubectl logs -n syncer-service -l app=workload-controller -f
```

### Common Issues

1. **Secrets not syncing**: Ensure PipelineRun has the `pipelinesascode.tekton.dev/git-auth-secret` annotation
2. **Controller not starting**: Verify RBAC permissions and that Kueue and Tekton are installed
3. **Image pull errors**: Ensure the controller image is pushed and accessible from your cluster

## References

- [Kueue Documentation](https://kueue.sigs.k8s.io/)
- [Kueue MultiKueue](https://kueue.sigs.k8s.io/docs/concepts/multikueue/)
- [Tekton Pipelines](https://tekton.dev/)

## License

See LICENSE file for details.
