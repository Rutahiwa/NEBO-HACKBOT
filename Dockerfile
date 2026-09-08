# syntax=docker/dockerfile:1.4

# ========================================
# Stage 1: Frontend Application Build
# ========================================
FROM node:24.17.0-slim AS frontend-compiler

# Production build configuration
ENV NODE_ENV=production
ENV VITE_BUILD_MEMORY_LIMIT=4096
ENV NODE_OPTIONS="--max-old-space-size=4096"
ENV PNPM_HOME="/usr/local/share/pnpm"
ENV PATH="$PNPM_HOME/bin:$PNPM_HOME:$PATH"

WORKDIR /app/ui

# Install build essentials and enable pnpm via corepack
RUN apt-get update && apt-get install -y \
    ca-certificates \
    tzdata \
    gcc \
    g++ \
    make \
    git \
    && corepack enable

# GraphQL schema for code generation
COPY ./backend/pkg/graph/schema.graphqls ../backend/pkg/graph/

# Application source code
COPY frontend/ .

# Install dependencies
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile

# Generate license report for frontend dependencies
RUN pnpm add -g license-checker && \
    mkdir -p /licenses/frontend && \
    license-checker --production --json > /licenses/frontend/licenses.json && \
    license-checker --production --csv > /licenses/frontend/licenses.csv

# Build frontend with optimizations and parallel processing
RUN pnpm run build -- \
    --mode production \
    --minify esbuild \
    --outDir dist \
    --emptyOutDir \
    --sourcemap false \
    --target es2020

# ========================================
# Stage 2: Backend Services Compilation
# ========================================
FROM golang:1.26-bookworm AS api-builder

# Version injection arguments
ARG PACKAGE_VER=develop
ARG PACKAGE_REV=

# Static binary compilation settings
ENV CGO_ENABLED=0
ENV GO111MODULE=on

# Install compilation toolchain and dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    tzdata \
    gcc \
    g++ \
    make \
    git \
    musl-dev

WORKDIR /app/backend

COPY backend/ .

# Fetch Go module dependencies (cached for faster rebuilds)
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download && go mod verify

# Install go-licenses tool for license extraction
RUN --mount=type=cache,target=/go/pkg/mod \
    go install github.com/google/go-licenses@latest

# Generate license reports for backend dependencies
RUN mkdir -p /licenses/backend && \
    go list -m all > /licenses/backend/dependencies.txt && \
    GOROOT=$(go env GOROOT) GOTOOLCHAIN=auto go-licenses csv ./cmd/pentagi > /licenses/backend/licenses.csv 2>/dev/null || true

# Compile main application binary with embedded version metadata
RUN go build -trimpath \
    -ldflags "\
        -X pentagi/pkg/version.PackageName=nebo-hackbot \
        -X pentagi/pkg/version.PackageVer=${PACKAGE_VER} \
        -X pentagi/pkg/version.PackageRev=${PACKAGE_REV}" \
    -o /nebo-hackbot ./cmd/pentagi

# Build ctester utility
RUN go build -trimpath \
    -ldflags "\
        -X pentagi/pkg/version.PackageName=ctester \
        -X pentagi/pkg/version.PackageVer=${PACKAGE_VER} \
        -X pentagi/pkg/version.PackageRev=${PACKAGE_REV}" \
    -o /ctester ./cmd/ctester

# Build ftester utility
RUN go build -trimpath \
    -ldflags "\
        -X pentagi/pkg/version.PackageName=ftester \
        -X pentagi/pkg/version.PackageVer=${PACKAGE_VER} \
        -X pentagi/pkg/version.PackageRev=${PACKAGE_REV}" \
    -o /ftester ./cmd/ftester

# Build etester utility
RUN go build -trimpath \
    -ldflags "\
        -X pentagi/pkg/version.PackageName=etester \
        -X pentagi/pkg/version.PackageVer=${PACKAGE_VER} \
        -X pentagi/pkg/version.PackageRev=${PACKAGE_REV}" \
    -o /etester ./cmd/etester

# ========================================
# Stage 3: Production Runtime Environment
# ========================================
FROM alpine:3.23.5

# Establish non-privileged execution context with docker socket access
RUN addgroup -g 998 docker && \
    addgroup -S nebo-hackbot && \
    adduser -S nebo-hackbot -G nebo-hackbot && \
    addgroup nebo-hackbot docker

# Install required packages
RUN apk --no-cache add ca-certificates openssl openssh-keygen shadow

ADD scripts/entrypoint.sh /opt/nebo-hackbot/bin/

RUN sed -i 's/\r//' /opt/nebo-hackbot/bin/entrypoint.sh && \
    chmod +x /opt/nebo-hackbot/bin/entrypoint.sh

RUN mkdir -p \
    /root/.ollama \
    /opt/nebo-hackbot/bin \
    /opt/nebo-hackbot/ssl \
    /opt/nebo-hackbot/fe \
    /opt/nebo-hackbot/logs \
    /opt/nebo-hackbot/data \
    /opt/nebo-hackbot/conf && \
    chmod 777 /root/.ollama

COPY --from=api-builder /nebo-hackbot /opt/nebo-hackbot/bin/nebo-hackbot
COPY --from=api-builder /ctester /opt/nebo-hackbot/bin/ctester
COPY --from=api-builder /ftester /opt/nebo-hackbot/bin/ftester
COPY --from=api-builder /etester /opt/nebo-hackbot/bin/etester
COPY --from=frontend-compiler /app/ui/dist /opt/nebo-hackbot/fe
COPY --from=api-builder /licenses/backend /opt/nebo-hackbot/licenses/backend
COPY --from=frontend-compiler /licenses/frontend /opt/nebo-hackbot/licenses/frontend

# Copy provider configuration files
COPY examples/configs/atlas.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/azure-openai.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/bedrock-glm-flash.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/custom-openai.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/deepinfra.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/deepseek.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/hcnsec.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/moonshot.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/novita.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/nvidia-glm-5.1.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/ollama-cloud.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/ollama-llama318b-instruct.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/ollama-llama318b.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/ollama-qwen332b-fp16-tc.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/ollama-qwq32b-fp16-tc.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/opencode.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/openrouter.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/orcarouter.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/vllm-mixed.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/vllm-qwen3.5-27b-fp8-no-think.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/vllm-qwen3.5-27b-fp8.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/vllm-qwen3.6-27b-fp8-no-think.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/vllm-qwen3.6-27b-fp8.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/vllm-qwen3.6-35b-a3b-fp8-no-think.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/vllm-qwen3.6-35b-a3b-fp8.provider.yml /opt/nebo-hackbot/conf/
COPY examples/configs/vllm-qwen332b-fp16.provider.yml /opt/nebo-hackbot/conf/

COPY LICENSE /opt/nebo-hackbot/LICENSE
COPY NOTICE /opt/nebo-hackbot/NOTICE
COPY EULA.md /opt/nebo-hackbot/EULA
COPY EULA.md /opt/nebo-hackbot/fe/EULA.md

RUN chown -R nebo-hackbot:nebo-hackbot /opt/nebo-hackbot

WORKDIR /opt/nebo-hackbot

USER nebo-hackbot

ENTRYPOINT ["/opt/nebo-hackbot/bin/entrypoint.sh", "/opt/nebo-hackbot/bin/nebo-hackbot"]

# Image Metadata
LABEL org.opencontainers.image.source="https://github.com/vxcontrol/pentagi"
LABEL org.opencontainers.image.description="Fully autonomous AI Agents system capable of performing complex penetration testing tasks"
LABEL org.opencontainers.image.authors="NEBO-HACKBOT Development Team"
LABEL org.opencontainers.image.licenses="MIT License"
