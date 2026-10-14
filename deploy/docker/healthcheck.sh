#!/bin/bash
set -euo pipefail
# Default Linux-host-network recipe uses HTTP loopback behind optional host TLS proxy.
# If overriding command flags, also update AF_* / AF_HEALTHCHECK_URL accordingly.
origin=${AF_PUBLIC_URL:-http://127.0.0.1:8788}
host=${origin#*://}
host=${host%%/*}
listen=${AF_LISTEN:-127.0.0.1:8788}
url=${AF_HEALTHCHECK_URL:-http://127.0.0.1:${listen##*:}/api/v1/setup}
args=(--fail --silent --show-error --max-time 4 --noproxy '*' --header "Host: $host")
[[ -z "${AF_HEALTH_CA_CERT:-}" ]] || args+=(--cacert "$AF_HEALTH_CA_CERT")
exec curl "${args[@]}" "$url"
