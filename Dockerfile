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
