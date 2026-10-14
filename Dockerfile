# syntax=docker/dockerfile:1
ARG GO_IMAGE=golang:1.26-trixie
ARG RUNTIME_IMAGE=debian:trixie-slim
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY examples/api/ ./examples/api/
# The committed embedded UI is used; Node/npm are not needed in this image.
RUN CGO_ENABLED=0 go test ./cmd/... ./internal/...
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/afplatform ./cmd/afplatform

FROM ${RUNTIME_IMAGE} AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates chromium chromium-sandbox curl dbus-x11 \
      fonts-liberation fonts-noto-cjk novnc openbox openssl tini \
      websockify x11-utils x11vnc xauth xvfb \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 afplatform \
    && useradd --uid 10001 --gid 10001 --home-dir /data/home --no-create-home \
       --shell /usr/sbin/nologin afplatform \
    && install -d -o 10001 -g 10001 -m 0700 /data /data/home /data/cache
COPY --from=build /out/afplatform /usr/local/bin/afplatform
COPY --chmod=0755 deploy/docker/*.sh /usr/local/lib/afplatform/
COPY docs/THIRD_PARTY_NOTICES.txt /usr/local/share/doc/afplatform/THIRD_PARTY_NOTICES.txt
COPY deploy/docker/NOTICE.txt deploy/docker/PLAYWRIGHT-LICENSE /usr/local/share/doc/afplatform/
RUN bash -n /usr/local/lib/afplatform/entrypoint.sh \
    && bash -n /usr/local/lib/afplatform/config.sh \
    && bash -n /usr/local/lib/afplatform/healthcheck.sh
ENV HOME=/data/home \
    XDG_CONFIG_HOME=/data/home/.config \
    XDG_CACHE_HOME=/data/cache \
    AF_DATA=/data \
    AF_LISTEN=127.0.0.1:8788 \
    AF_PUBLIC_URL=http://127.0.0.1:8788 \
    AF_BROWSER=/usr/bin/chromium \
    AF_MAX_BROWSERS=3 \
    AF_BROWSER_IDLE=2m \
    AF_DESKTOP=1 \
    AF_DESKTOP_PORT=6080 \
    AF_VNC_PORT=5900
USER 10001:10001
WORKDIR /data
VOLUME ["/data"]
# Linux host networking is intentional: ALL listeners stay on loopback.
# See compose.yaml; do not replace with port publication without configuring TLS.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD ["/usr/local/lib/afplatform/healthcheck.sh"]
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/lib/afplatform/entrypoint.sh"]
CMD ["serve"]
