# syntax=docker/dockerfile:1

# ---- build ---------------------------------------------------------------
FROM golang:1.26-alpine AS build

WORKDIR /src

# Copy the module files first so the dependency layer is cached until go.mod
# or go.sum changes.
COPY go.mod go.sum ./

# Overridable for networks that cannot reach proxy.golang.org, for example
# behind a corporate mirror.
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}

RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
        -trimpath \
        -ldflags "-s -w -X main.version=${VERSION}" \
        -o /out/api ./cmd/api

# ---- runtime -------------------------------------------------------------
FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
    && adduser -D -u 10001 app \
    && mkdir -p /app \
    && chown app:app /app

USER app
WORKDIR /app

COPY --from=build /out/api /usr/local/bin/api

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=15s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

# The binary handles SIGTERM and drains in-flight requests.
ENTRYPOINT ["/usr/local/bin/api"]
