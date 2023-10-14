# Build the manager binary
FROM golang:1.20 as builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# Copy files generated through `go generate`
COPY cmd/operator/*.go-generate.txt .

# Copy the go source
COPY cmd/operator/main.go main.go
COPY api/ api/
COPY pkg/ pkg/

# Build
# the GOARCH has not a default value to allow the binary be built according to
# the host where the command was called. For example, if we call make
# docker-build in a local env which has the Apple Silicon M1 SO the docker
# BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be
# linux/amd64. Therefore, by leaving it empty we can ensure that the container
# and binary shipped on it will have the same platform.
#
# Note, we include cache mounts to speed up incremental builds. The directories
# we mount are the cache directories present in the container. These can be
# found via `go env | grep CACHE`.
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -v -o manager main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]

LABEL org.opencontainers.image.source=https://github.com/vertica/vertica-kubernetes/tree/main/docker-operator \
      org.opencontainers.image.title='VerticaDB Operator' \
      org.opencontainers.image.description='Deploys the VerticaDB operator.  The operator manages a Vertica Eon Mode database in Kubernetes, and automates administative tasks.' \
      org.opencontainers.image.url=https://github.com/vertica/vertica-kubernetes/ \
      org.opencontainers.image.documentation=https://www.vertica.com/docs/latest/HTML/Content/Authoring/Containers/ContainerizedVertica.htm
