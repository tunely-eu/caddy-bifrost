# syntax=docker/dockerfile:1.7

ARG CADDY_VERSION=2.11.2

FROM caddy:${CADDY_VERSION}-builder AS builder

ARG CADDY_VERSION=2.11.2
ARG CADDY_BIFROST_WITH=github.com/tunely-eu/caddy-bifrost=.

WORKDIR /src
COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    xcaddy build "v${CADDY_VERSION}" --with "${CADDY_BIFROST_WITH}"

FROM caddy:${CADDY_VERSION}

COPY --from=builder /src/caddy /usr/bin/caddy
