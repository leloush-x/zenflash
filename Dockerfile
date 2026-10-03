# The dashboard is prebuilt and committed at webui/dist (rebuild with
# `cd webui && npm ci && npm run build` after UI changes and commit it),
# so the runtime image needs no Node toolchain.
FROM --platform=$BUILDPLATFORM golang:1.25-trixie AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 \
    GOOS="${TARGETOS:-linux}" \
    GOARCH="${TARGETARCH:-amd64}" \
    go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/zenflash-llm ./cmd/zenflash-llm

FROM ubuntu:24.04

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata wget \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -r zenflash-llm \
    && useradd -r -g zenflash-llm -d /var/lib/zenflash-llm zenflash-llm \
    && mkdir -p /app /var/lib/zenflash-llm \
    && chown -R zenflash-llm:zenflash-llm /app /var/lib/zenflash-llm

COPY --from=builder /out/zenflash-llm /usr/local/bin/zenflash-llm
COPY --chmod=0755 docker-entrypoint.sh /usr/local/bin/docker-entrypoint
COPY --chown=zenflash-llm:zenflash-llm config.example.json /app/config.example.json

# LISTEN_ADDRESS / WEBUI_LISTEN_ADDRESS are intentionally unset: config.json is
# authoritative. Export them to override the configured listen addresses.
ENV CONFIG_PATH=/var/lib/zenflash-llm/config.json \
    CONFIG_SEED_PATH= \
    LISTEN_ADDRESS= \
    WEBUI_LISTEN_ADDRESS= \
    STATE_DIR=/var/lib/zenflash-llm

WORKDIR /app

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/docker-entrypoint"]
