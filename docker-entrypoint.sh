#!/bin/sh
set -eu

case "${1:-}" in
  ./CLIProxyAPI|CLIProxyAPI|/CLIProxyAPI/CLIProxyAPI)
    auth_path="${AUTH_PATH:-/CLIProxyAPI/auths}"
    mkdir -p /CLIProxyAPI/data /CLIProxyAPI/logs "$auth_path"
    case "$auth_path" in
      /root/*) chmod 755 /root ;;
    esac
    chown -R clirelay:clirelay /CLIProxyAPI/data /CLIProxyAPI/logs "$auth_path"
    if [ -e /CLIProxyAPI/config.yaml ]; then
      chown clirelay:clirelay /CLIProxyAPI/config.yaml 2>/dev/null || true
    fi
    su-exec clirelay:clirelay /usr/local/bin/migrate-sqlite-to-postgres.sh
    exec su-exec clirelay:clirelay "$@"
    ;;
  *)
    exec "$@"
    ;;
esac
