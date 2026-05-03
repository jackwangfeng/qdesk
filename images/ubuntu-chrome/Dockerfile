# syntax=docker/dockerfile:1.7

# ---- Builder stage: compile qdesk-agentd ----
FROM rust:1.85-slim-bookworm AS builder
WORKDIR /src

# Use Aliyun mirror for apt + USTC mirror for crates.io (CN-friendly, no proxy needed).
# Override at build time with --build-arg APT_MIRROR=deb.debian.org if you're outside CN.
ARG APT_MIRROR=mirrors.aliyun.com
ARG CARGO_REGISTRY_MIRROR=https://mirrors.ustc.edu.cn/crates.io-index
RUN sed -i "s|deb.debian.org|${APT_MIRROR}|g; s|security.debian.org|${APT_MIRROR}|g" \
    /etc/apt/sources.list.d/debian.sources

RUN apt-get update && apt-get install -y --no-install-recommends \
    pkg-config libssl-dev \
    && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /usr/local/cargo \
    && printf '[source.crates-io]\nreplace-with = "ustc"\n\n[source.ustc]\nregistry = "%s"\n' \
       "${CARGO_REGISTRY_MIRROR}" > /usr/local/cargo/config.toml

COPY Cargo.toml Cargo.lock ./
COPY crates ./crates

RUN cargo build --release -p qdesk-agentd

# ---- Runtime stage ----
# NOTE: Plan called for ubuntu:24.04, but on 24.04 `chromium-browser` is a
# snap redirect that fails in apt-get install. Switched to debian:bookworm-slim
# which ships native `chromium` and has the same Debian package ecosystem
# as the rust:slim-bookworm builder.
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

COPY --from=builder /src/target/release/qdesk-agentd /usr/local/bin/qdesk-agentd
COPY images/ubuntu-chrome/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 7878
ENV DISPLAY_NUM=99 RES=1920x1080x24

ENTRYPOINT ["/entrypoint.sh"]
