# syntax=docker/dockerfile:1
# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Scion Base Image - Adds sciontool and scion user on top of core-base
ARG BASE_IMAGE
FROM ${BASE_IMAGE} AS base

# Builder stage for sciontool compilation.
#
# `--platform=$BUILDPLATFORM` pins this stage to the build host's native
# architecture so the Go toolchain runs natively (no QEMU emulation). We then
# cross-compile to $TARGETARCH using Go's built-in cross-compiler. This is
# safe because CGO is disabled — pure-Go builds need no C cross-toolchain.
FROM --platform=$BUILDPLATFORM ${BASE_IMAGE} AS builder
USER root
WORKDIR /src
ENV PATH="/usr/local/go/bin:${PATH}"
ENV CGO_ENABLED=0
ENV GOOS=linux

# These are populated automatically by buildx for each target platform.
ARG TARGETARCH
ARG TARGETOS

# Build arguments for version info
ARG GIT_COMMIT
ARG BUILD_TIME

# Copy go.mod and go.sum first for better layer caching. The module download
# is cached via a buildx cache mount so it survives across builds and is
# shared between target architectures (since we run on $BUILDPLATFORM).
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source and build scion and sciontool
COPY cmd/ ./cmd/
COPY pkg/ ./pkg/
COPY web/*.go ./web/
# Embed the pre-built web frontend assets into the scion binary.
# The caller must have run `cd web && npm run build` (or equivalent) BEFORE
# `docker build` so that web/dist/client/ contains the vite build output.
# If that directory is missing, the go:embed directive at web/embed.go:25
# (`//go:embed all:dist/client`) will fail the compile with "pattern
# dist/client: no matching files found".
COPY web/dist/client/ ./web/dist/client/

# Build with version info injected via ldflags.
# VCS stamping is disabled since .git is not in the build context;
# commit info is passed explicitly via the GIT_COMMIT build arg.
# Both /go/pkg/mod and /root/.cache/go-build are mounted as buildx caches so
# repeat builds are fast even when source changes.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build,id=scion-gobuild-${TARGETOS}-${TARGETARCH} \
    BUILD_TIME="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}" && \
    SCION_PKG="github.com/GoogleCloudPlatform/scion/pkg/version" && \
    SCIONTOOL_PKG="github.com/GoogleCloudPlatform/scion/cmd/sciontool/commands" && \
    SCION_LDFLAGS="-s -w -X ${SCION_PKG}.BuildTime=${BUILD_TIME}" && \
    SCIONTOOL_LDFLAGS="-s -w -X ${SCIONTOOL_PKG}.BuildTime=${BUILD_TIME}" && \
    if [ -n "${GIT_COMMIT}" ]; then \
        SCION_LDFLAGS="${SCION_LDFLAGS} -X ${SCION_PKG}.Commit=${GIT_COMMIT}"; \
        SCIONTOOL_LDFLAGS="${SCIONTOOL_LDFLAGS} -X ${SCIONTOOL_PKG}.Commit=${GIT_COMMIT}"; \
    fi && \
    GOARCH=${TARGETARCH} go build -buildvcs=false -ldflags="${SCION_LDFLAGS}" -o /out/scion ./cmd/scion/ && \
    GOARCH=${TARGETARCH} go build -buildvcs=false -tags no_embed_web -ldflags="${SCIONTOOL_LDFLAGS}" -o /out/sciontool ./cmd/sciontool/

# Final stage
FROM base
USER root

# Copy scion and sciontool from builder
COPY --from=builder /out/scion /usr/local/bin/scion
COPY --from=builder /out/sciontool /usr/local/bin/sciontool

# Create dev-override bin directory at highest PATH priority.
# When SCION_DEV_BINARIES is set, this directory is bind-mounted with
# locally-built binaries, allowing rapid iteration without image rebuilds.
RUN mkdir -p /opt/scion/bin
ENV PATH="/opt/scion/bin:${PATH}"

# Create scion user with passwordless sudo
RUN useradd -m -s /bin/zsh -u 1000 scion && \
    echo "scion ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/scion && \
    chmod 0440 /etc/sudoers.d/scion

# Set ownership for npm global directory
# TODO: Consider making npm-global world read-write (chmod) instead of chown
# to scion. This would decouple it from the scion UID and avoid any ownership
# concerns when the UID is remapped at container start.
RUN chown -R scion:scion /usr/local/share/npm-global

# Note: Container starts as root to allow sciontool init to adjust
# the scion user's UID/GID to match the host user for proper file
# permissions on mounted volumes. The child process drops privileges.

# Set sciontool as the container entrypoint (PID 1)
# The -- separator ensures everything after it is treated as the child command
ENTRYPOINT ["sciontool", "init", "--"]

# Default command (can be overridden at runtime)
CMD ["bash"]
