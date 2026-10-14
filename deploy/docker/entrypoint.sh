#!/bin/bash
set -Eeuo pipefail
umask 077
source "$(dirname "${BASH_SOURCE[0]}")/config.sh"

# Docker adapter only: the Go executable's native configuration remains intact.
if [ "$#" -eq 0 ]; then set -- serve; fi
if [[ "$1" == -* ]]; then set -- serve "$@"; fi
if [[ "$1" != serve ]]; then exec /usr/local/bin/afplatform "$@"; fi
for arg in "$@"; do
  if [[ "$arg" == -h || "$arg" == --help ]]; then exec /usr/local/bin/afplatform "$@"; fi
done
[[ "$(id -u)" != 0 ]] || { echo 'Run the image as its non-root user (10001:10001).' >&2; exit 2; }
shift
AF_DATA=${AF_DATA:-/data}
AF_LISTEN=${AF_LISTEN:-127.0.0.1:8788}
AF_PUBLIC_URL=${AF_PUBLIC_URL:-http://127.0.0.1:8788}
AF_DESKTOP=${AF_DESKTOP:-1}
[[ "$AF_DESKTOP" == 0 || "$AF_DESKTOP" == 1 ]] || { echo 'AF_DESKTOP must be 0 or 1.' >&2; exit 2; }
build_platform_args "$@" # Explicit flags are last and take precedence in Go's parser.
mkdir -p "$AF_DATA" "$HOME" "${XDG_CONFIG_HOME:-$HOME/.config}" "${XDG_CACHE_HOME:-$HOME/.cache}"
[[ -w "$AF_DATA" ]] || { echo 'Data directory is not writable by UID 10001.' >&2; exit 2; }

pids=()
app_pid=''
runtime_dir=$(mktemp -d /tmp/afplatform-runtime.XXXXXXXX)
export XDG_RUNTIME_DIR=$runtime_dir
cleanup() {
  local status=$? pid attempts
  trap - EXIT TERM INT
  # Give Go time to stop its workers and close account browsers before the desktop.
  if [[ -n "$app_pid" ]] && kill -0 "$app_pid" 2>/dev/null; then
    kill -TERM "$app_pid" 2>/dev/null || true
    for ((attempts=0; attempts<250; attempts++)); do
      kill -0 "$app_pid" 2>/dev/null || break
      sleep 0.1
    done
  fi
  for pid in "${pids[@]}"; do kill -TERM "$pid" 2>/dev/null || true; done
  sleep 1
  for pid in "${pids[@]}"; do
    if kill -0 "$pid" 2>/dev/null; then kill -KILL "$pid" 2>/dev/null || true; fi
    wait "$pid" 2>/dev/null || true
  done
  # Only this freshly-created disposable directory is removed; never the data volume.
  [[ "$runtime_dir" == /tmp/afplatform-runtime.* ]] && rm -rf -- "$runtime_dir"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 0' TERM INT
start() { "$@" & pids+=("$!"); }
export DBUS_SESSION_BUS_ADDRESS="unix:path=$runtime_dir/bus"
start dbus-daemon --session --nofork --address="$DBUS_SESSION_BUS_ADDRESS"
for ((i=0; i<100; i++)); do [[ -S "$runtime_dir/bus" ]] && break; sleep 0.1; done
[[ -S "$runtime_dir/bus" ]] || { echo 'Session D-Bus failed to start.' >&2; exit 1; }

if [[ "$AF_DESKTOP" == 1 ]]; then
  AF_DESKTOP_PORT=${AF_DESKTOP_PORT:-6080}
  AF_VNC_PORT=${AF_VNC_PORT:-5900}
  for port in "$AF_DESKTOP_PORT" "$AF_VNC_PORT"; do
    validate_desktop_port "$port" || { echo 'Desktop ports must be 1024..65535.' >&2; exit 2; }
  done
  [[ "$AF_DESKTOP_PORT" != "$AF_VNC_PORT" ]] || { echo 'Desktop ports must differ.' >&2; exit 2; }
  password_file=${AF_VNC_PASSWORD_FILE:-$AF_DATA/.desktop-password}
  # Classic VNC authentication uses only 8 characters. SSH is the outer protection.
  init_desktop_password "$password_file"
  export DISPLAY=:99 XAUTHORITY="$runtime_dir/Xauthority"
  touch "$XAUTHORITY"
  xauth -f "$XAUTHORITY" add "$DISPLAY" . "$(openssl rand -hex 16)"
  start Xvfb "$DISPLAY" -screen 0 1440x900x24 -nolisten tcp -auth "$XAUTHORITY"
  ready=0
  for ((i=0; i<100; i++)); do
    if xdpyinfo -display "$DISPLAY" >/dev/null 2>&1; then ready=1; break; fi
    sleep 0.1
  done
  [[ "$ready" == 1 ]] || { echo 'Virtual desktop failed to start.' >&2; exit 1; }
  start openbox --sm-disable
  start x11vnc -display "$DISPLAY" -auth "$XAUTHORITY" \
    -listen 127.0.0.1 -rfbport "$AF_VNC_PORT" -no6 \
    -passwdfile "$password_file" -forever -shared -noxdamage -nolookup -quiet
  start websockify --web=/usr/share/novnc/ --heartbeat=30 \
    "127.0.0.1:$AF_DESKTOP_PORT" "127.0.0.1:$AF_VNC_PORT"
  echo "Administrator desktop: loopback port $AF_DESKTOP_PORT, use an SSH tunnel. Password file: $password_file (not printed)."
else
  unset DISPLAY WAYLAND_DISPLAY
fi

/usr/local/bin/afplatform "${PLATFORM_ARGS[@]}" &
app_pid=$!
pids+=("$app_pid")
set +e
wait -n -p ended_pid "${pids[@]}"
status=$?
set -e
if [[ "${ended_pid:-}" != "$app_pid" ]]; then
  echo 'A required desktop/session process exited; stopping the platform.' >&2
  exit 1
fi
exit "$status"
