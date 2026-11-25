# Build stage
FROM registry.access.redhat.com/ubi9/go-toolset:1.26@sha256:0e3894cea4fc077084245e8aa2befc8a7d91a4f60fbf850f0b5b555cbe62ee26 AS builder

USER 0

WORKDIR /workspace

# Copy go mod files
COPY go.mod go.sum ./

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOTOOLCHAIN=local go build -mod=vendor -o bin/workload-controller ./cmd/controller

# Final stage
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest@sha256:08d4c70f1baf6537a0728656cf7942d561e74a35916509d205b6519861266352

WORKDIR /

# Copy the binary from builder stage
COPY --from=builder /workspace/bin/workload-controller .

# Use nonroot user
USER 65532:65532

ENTRYPOINT ["/workload-controller"]

LABEL \
    com.redhat.component="openshift-syncer-service-rhel9-container" \
    cpe="cpe:/a:redhat:openshift_pipelines:0.1::" \
    description="Red Hat OpenShift Pipelines syncer-service syncer-service" \
    distribution-scope="public" \
    io.k8s.description="Red Hat OpenShift Pipelines syncer-service syncer-service" \
    io.k8s.display-name="Red Hat OpenShift Pipelines syncer-service syncer-service" \
    io.openshift.tags="tekton,openshift,syncer-service,syncer-service" \
    maintainer="pipelines-extcomm@redhat.com" \
    name="openshift-pipelines/syncer-service-rhel9" \
    release="1" \
    summary="Red Hat OpenShift Pipelines syncer-service syncer-service" \
    url="https://github.com/openshift-pipelines/syncer-service" \
    vendor="Red Hat, Inc." \
    version="v0.1.1"
