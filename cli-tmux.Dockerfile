# Assumes a local copy of gemini-cli-sandbox
ARG BASE_IMAGE=gemini-cli-sandbox

FROM ${BASE_IMAGE} AS builder
USER root

# Install build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
  wget \
  make \
  gcc \
  libssl-dev \
  libcurl4-gnutls-dev \
  libexpat1-dev \
  libghc-zlib-dev \
  gettext \
  ca-certificates \
  && apt-get clean \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /opt

# Download and compile Git
# later version required to support git worktree relative paths
RUN wget https://www.kernel.org/pub/software/scm/git/git-2.52.0.tar.gz && \
    tar -xvf git-2.52.0.tar.gz && \
    rm git-2.52.0.tar.gz

WORKDIR /opt/git-2.52.0/
# Compile to /opt/git to keep it isolated for copying
RUN make prefix=/opt/git all && \
    make prefix=/opt/git install


FROM ${BASE_IMAGE}
USER root

# Install runtime dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
  tmux \
  curl \
  wget \
  libssl-dev \
  libcurl4-gnutls-dev \
  libexpat1-dev \
  libghc-zlib-dev \
  gettext \
  ca-certificates \
  && apt-get clean \
  && rm -rf /var/lib/apt/lists/*

# Copy git from builder
COPY --from=builder /opt/git /usr/local/

# Create symlink as in original
RUN ln -s /usr/local/bin/git /usr/bin/git

# Install Go
ARG GO_VERSION=1.25.4
RUN ARCH=$(dpkg --print-architecture) && \
    case "${ARCH}" in \
      amd64) GO_ARCH='linux-amd64' ;; \
      arm64) GO_ARCH='linux-arm64' ;; \
      *) echo "Unsupported architecture: ${ARCH}"; exit 1 ;; \
    esac && \
    curl -L "https://go.dev/dl/go${GO_VERSION}.${GO_ARCH}.tar.gz" -o go.tar.gz && \
    tar -C /usr/local -xzf go.tar.gz && \
    rm go.tar.gz

ENV PATH=/usr/local/go/bin:$PATH

USER node
WORKDIR /workspace
