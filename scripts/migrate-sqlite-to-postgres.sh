#!/bin/sh
set -eu

log() {
	printf '%s\n' "clirelay sqlite migration: $*" >&2
}

fail() {
	log "$*"
	exit 1
}

is_false() {
	case "$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')" in
		false|0|no|off) return 0 ;;
		*) return 1 ;;
	esac
}

find_binary() {
	if [ -n "${CLIRELAY_BIN:-}" ]; then
		printf '%s\n' "$CLIRELAY_BIN"
		return
	fi
	for candidate in ./CLIProxyAPI /CLIProxyAPI/CLIProxyAPI ./cli-proxy-api ./clirelay2; do
		if [ -x "$candidate" ]; then
			printf '%s\n' "$candidate"
			return
		fi
	done
	fail "set CLIRELAY_BIN to the CliRelay binary path"
}

find_sqlite() {
	if [ -n "${1:-}" ]; then
		printf '%s\n' "$1"
		return
	fi
	if [ -n "${CLIRELAY_SQLITE_PATH:-}" ]; then
		printf '%s\n' "$CLIRELAY_SQLITE_PATH"
		return
	fi
	for candidate in \
		/CLIProxyAPI/data/usage.db \
		/CLIProxyAPI/usage.db \
		/CLIProxyAPI/logs/usage.db \
		./data/usage.db \
		./usage.db \
		./logs/usage.db
	do
		if [ -f "$candidate" ]; then
			printf '%s\n' "$candidate"
			return
		fi
	done
	return 0
}

if is_false "${CLIRELAY_SQLITE_AUTO_MIGRATE:-true}"; then
	log "disabled by CLIRELAY_SQLITE_AUTO_MIGRATE"
	exit 0
fi

sqlite_path="$(find_sqlite "${1:-}")"
if [ -z "$sqlite_path" ]; then
	log "no legacy usage.db found; starting with PostgreSQL runtime data"
	exit 0
fi
[ -f "$sqlite_path" ] || fail "sqlite file not found: $sqlite_path"
[ -n "${CLIRELAY_POSTGRES_DSN:-}" ] || fail "CLIRELAY_POSTGRES_DSN is required when legacy SQLite exists"

bin="$(find_binary)"
apply_import="${CLIRELAY_SQLITE_AUTO_IMPORT:-true}"

log "legacy SQLite found at $sqlite_path"
log "running read-only SQLite inventory"
"$bin" -sqlite-dry-run "$sqlite_path"

log "running PostgreSQL import dry-run"
"$bin" -sqlite-import "$sqlite_path"

if is_false "$apply_import"; then
	log "apply disabled by CLIRELAY_SQLITE_AUTO_IMPORT; dry-run complete"
	exit 0
fi

log "applying SQLite import into PostgreSQL"
"$bin" -sqlite-import "$sqlite_path" -sqlite-import-dry-run=false
log "migration complete; SQLite file was left in place"
