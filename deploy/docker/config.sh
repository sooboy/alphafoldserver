#!/bin/bash
# Sourced by the container entrypoint and tested without launching a browser.
build_platform_args() {
  PLATFORM_ARGS=(serve -data "${AF_DATA:-/data}" -listen "${AF_LISTEN:-127.0.0.1:8788}"
    -public-url "${AF_PUBLIC_URL:-http://127.0.0.1:8788}"
    -browser "${AF_BROWSER:-/usr/bin/chromium}" -max-active-browsers "${AF_MAX_BROWSERS:-3}"
    -browser-idle "${AF_BROWSER_IDLE:-2m}")
  [[ -z "${AF_TLS_CERT:-}" ]] || PLATFORM_ARGS+=(-tls-cert "$AF_TLS_CERT")
  [[ -z "${AF_TLS_KEY:-}" ]] || PLATFORM_ARGS+=(-tls-key "$AF_TLS_KEY")
  PLATFORM_ARGS+=("$@")
}
validate_desktop_port() {
  [[ "$1" =~ ^[1-9][0-9]{3,4}$ ]] && ((10#$1 >= 1024 && 10#$1 <= 65535))
}
init_desktop_password() {
  local file=$1 password
  [[ ! -L "$file" ]] || { echo 'Desktop password file must not be a symlink.' >&2; return 2; }
  if [[ ! -e "$file" ]]; then
    [[ -z "${AF_VNC_PASSWORD_FILE:-}" ]] || { echo 'Supplied VNC password file does not exist.' >&2; return 2; }
    (umask 077; set -o noclobber; openssl rand -hex 4 > "$file") || return 2
  fi
  password=$(cat "$file") || return 2
  [[ "$password" =~ ^[A-Za-z0-9]{8}$ ]] || { echo 'VNC password file must contain exactly 8 ASCII letters/digits.' >&2; return 2; }
  # Never print the password or put it on a process command line.
}
