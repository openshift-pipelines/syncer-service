# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /workspace

# Copy go mod files
COPY go.mod go.sum ./

# Copy source code
COPY . .

# Build the binary
RUN go build -o bin/workload-controller ./cmd/controller

# Final stage
FROM gcr.io/distroless/static:nonroot

WORKDIR /

# Copy the binary from builder stage
COPY --from=builder /workspace/bin/workload-controller .

# Use nonroot user
USER 65532:65532

ENTRYPOINT ["/workload-controller"]

LABEL \
      com.redhat.component="openshift-syncer-service-rhel9-container" \
      cpe="cpe:/a:redhat:openshift_pipelines:0.1::el9" \
      description="Red Hat OpenShift Pipelines syncer-service syncer-service" \
      io.k8s.description="Red Hat OpenShift Pipelines syncer-service syncer-service" \
      io.k8s.display-name="Red Hat OpenShift Pipelines syncer-service syncer-service" \
      io.openshift.tags="tekton,openshift,syncer-service,syncer-service" \
      maintainer="pipelines-extcomm@redhat.com" \
      name="openshift-pipelines/syncer-service-rhel9" \
      summary="Red Hat OpenShift Pipelines syncer-service syncer-service" \
      version="v0.1.1"