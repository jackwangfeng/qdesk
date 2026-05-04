# syntax=docker/dockerfile:1.7

# ---- Builder stage: compile qdesk-agentd ----
FROM golang:1.25-bookworm AS builder
WORKDIR /src

# Override at build time with --build-arg APT_MIRROR=deb.debian.org if you're outside CN.
ARG APT_MIRROR=mirrors.aliyun.com
ARG GOPROXY=https://goproxy.cn,direct

ENV CGO_ENABLED=0 \
    GOPROXY=${GOPROXY}

# Version + commit injection. Defaults to "dev" so plain `docker build`
# (without --build-arg) still works for development; release pipelines
# pass the real values.
ARG VERSION=dev
ARG COMMIT=unknown

COPY go.mod go.sum ./
RUN go mod download

COPY pkg ./pkg
COPY internal ./internal
COPY cmd ./cmd

RUN go build -trimpath \
    -ldflags="-s -w \
        -X github.com/jeffwang/qdesk/pkg/version.Version=${VERSION} \
        -X github.com/jeffwang/qdesk/pkg/version.Commit=${COMMIT}" \
    -o /out/qdesk-agentd ./cmd/qdesk-agentd

# ---- Runtime stage ----
# debian:bookworm-slim ships native `chromium` (no snap redirect).
FROM debian:bookworm-slim
ENV DEBIAN_FRONTEND=noninteractive

ARG APT_MIRROR=mirrors.aliyun.com
RUN sed -i "s|deb.debian.org|${APT_MIRROR}|g; s|security.debian.org|${APT_MIRROR}|g" \
    /etc/apt/sources.list.d/debian.sources

RUN apt-get update && apt-get install -y --no-install-recommends \
        xvfb xfwm4 x11-xserver-utils x11-utils \
        scrot xdotool \
        chromium fonts-noto-core fonts-noto-cjk \
        ca-certificates dbus-x11 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/qdesk-agentd /usr/local/bin/qdesk-agentd
COPY images/ubuntu-chrome/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 7878
ENV DISPLAY_NUM=99 RES=1920x1080x24

ENTRYPOINT ["/entrypoint.sh"]
