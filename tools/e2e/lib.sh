# shellcheck shell=bash
# Fixture layer for the live E2E (tools/e2e). Sourced by the case files; run.sh sets
# E2E_RUN (a fresh run directory), E2E_AGENT and E2E_CLIENT.
#
# Rules this file enforces, each from a pass-4 false result:
# - Every instance gets a new data dir. Settings given as flags (-roots, -read-only)
#   only seed a NEW agent.db, so e2e_start refuses an existing dir, and e2e_restart
#   takes no flags: a different configuration needs a new instance.
# - Devices are found by a unique label, never by created time or rowid (which
#   picked the owner once).
# - Each case starts with no open upload session and ends with its sessions
#   removed (a leak is reported as a harness note, never hidden).
# - Only processes this file started, checked by executable path, are stopped.

: "${E2E_RUN:?E2E_RUN not set (use run.sh)}"
: "${E2E_AGENT:?E2E_AGENT not set}"
: "${E2E_CLIENT:?E2E_CLIENT not set}"
case $E2E_RUN in /tmp/*/rfe-e2e-run.*|/tmp/rfe-e2e-run.*) ;; *) echo "E2E_RUN=$E2E_RUN is not a run dir made by run.sh" >&2; exit 2 ;; esac
E2E_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
E2E_AGENT_REAL=$(readlink -f "$E2E_AGENT")
E2E_PIDS=()
E2E_OUT=${E2E_OUT:-/dev/stdout}

e2e_note() { echo "# $*"; }
# A harness or fixture problem, kept apart from product results.
e2e_harness() { echo "HARNESS $*"; }

e2e_free_port() { python3 -I -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])'; }

e2e_pid_is_ours() { [ -n "${1:-}" ] && [ "$(readlink -f "/proc/$1/exe" 2>/dev/null)" = "$E2E_AGENT_REAL" ]; }

e2e_launch() {
	(cd "$S" && XDG_DATA_HOME=$XDG exec "$E2E_AGENT" serve -addr "127.0.0.1:$PORT" -data "$S/data" -roots "$E2E_ROOTS" -name "e2e-$E2E_NAME" "${E2E_FLAGS[@]}" >>"$S/agent.log" 2>&1) &
	PID=$!
	E2E_PIDS+=("$PID")
	local i
	for i in $(seq 1 100); do
		curl -sk -m 1 "https://127.0.0.1:$PORT/v1/health" >/dev/null 2>&1 && break
		kill -0 "$PID" 2>/dev/null || break
		sleep 0.1
	done
	e2e_pid_is_ours "$PID" || { e2e_harness "agent for $E2E_NAME is not running (see $S/agent.log)"; return 1; }
	curl -sk -m 2 "https://127.0.0.1:$PORT/v1/health" >/dev/null || { e2e_harness "agent for $E2E_NAME never answered on $PORT"; return 1; }
}

# e2e_start NAME ROOTS [serve flags...]
# ROOTS is "root" ($S/root), "instance" ($S itself, so the data dir lies inside the root) or "none" (empty -roots, unrestricted).
# Sets S R O XDG W PORT B FP PID, owner token T and owner device id IDO.
e2e_start() {
	E2E_NAME=$1 E2E_ROOTSPEC=$2
	shift 2
	E2E_FLAGS=("$@")
	S=$E2E_RUN/$E2E_NAME
	if [ -e "$S" ]; then
		e2e_harness "instance dir $S exists; an agent database is never reused"
		return 1
	fi
	mkdir -p "$S/data" "$S/root" "$S/outside" "$S/xdg" "$S/w"
	R=$S/root O=$S/outside XDG=$S/xdg W=$S/w
	case $E2E_ROOTSPEC in
	root) E2E_ROOTS=$R ;;
	instance) E2E_ROOTS=$S ;;
	none) E2E_ROOTS= ;;
	*) e2e_harness "bad roots spec $E2E_ROOTSPEC"; return 1 ;;
	esac
	PORT=$(e2e_free_port)
	B=https://127.0.0.1:$PORT/v1
	e2e_launch || return 1
	FP=$(rg -o 'cert-fingerprint=(\S+)' -r '$1' "$S/agent.log" | head -1)
	[ -n "$FP" ] || { e2e_harness "no fingerprint in $S/agent.log"; return 1; }
	local code
	code=$("$E2E_AGENT" pair -data "$S/data" --owner | rg -o 'Pairing code: (\S+)' -r '$1')
	T=$("$E2E_CLIENT" "https://127.0.0.1:$PORT" "$FP" "$code" owner e2e-owner) || { e2e_harness "owner registration failed"; return 1; }
	IDO=$(e2e_device_id e2e-owner) || return 1
	e2e_note "instance $E2E_NAME pid=$PID port=$PORT roots='$E2E_ROOTS' flags='${E2E_FLAGS[*]}' owner=$IDO"
	e2e_effective
}

# e2e_restart: same instance, same flags (for device-row edits that need a reload).
e2e_restart() {
	[ $# -eq 0 ] || { e2e_harness "e2e_restart takes no flags; start a new instance for a new configuration"; return 1; }
	e2e_stop_pid "$PID"
	e2e_launch || return 1
	e2e_note "restarted $E2E_NAME pid=$PID (flags unchanged)"
}

# Print the configuration the running agent actually applies (evidence next to verdicts).
e2e_effective() {
	local s
	s=$(curl -sk -m 5 -H "Authorization: Bearer $T" "$B/settings")
	E2E_EFFECTIVE=$(printf '%s' "$s" | jq -c '{roots: (.roots // .allowedRoots // null), readOnly: (if has("readOnly") then .readOnly else null end)}' 2>/dev/null)
	e2e_note "effective $E2E_NAME: $E2E_EFFECTIVE"
}

e2e_sq() { sqlite3 "$S/data/agent.db" "$1"; }

# e2e_device_id LABEL: the id of the one device with that label (fails on 0 or more than 1).
e2e_device_id() {
	local n
	n=$(e2e_sq "select count(*) from devices where label='$1'")
	if [ "$n" != 1 ]; then
		e2e_harness "device label '$1' matches $n devices"
		return 1
	fi
	e2e_sq "select id from devices where label='$1'"
}

# e2e_device NAME: pair a plain (browse-only) device labelled e2e-dev-NAME; sets DEV_TOK and DEV_ID.
e2e_device() {
	local code label=e2e-dev-$1
	code=$("$E2E_AGENT" pair -data "$S/data" | rg -o 'Pairing code: (\S+)' -r '$1')
	DEV_TOK=$("$E2E_CLIENT" "https://127.0.0.1:$PORT" "$FP" "$code" - "$label") || { e2e_harness "pairing $label failed"; return 1; }
	DEV_ID=$(e2e_device_id "$label") || return 1
	[ "$DEV_ID" != "$IDO" ] || { e2e_harness "device $label resolved to the owner"; return 1; }
}

e2e_grant_all() { e2e_sq "update devices set can_download=1,can_upload=1,can_modify=1,can_delete=1,can_share=1 where id='$1'"; }

e2e_open_sessions() { e2e_sq "select count(*) from transfers where status='open'"; }

# e2e_case N FN: run FN for case N with a clean session table before and after.
e2e_case() {
	local n=$1 fn=$2 before after id
	before=$(e2e_open_sessions)
	if [ "$before" != 0 ]; then
		e2e_harness "REQ $n precondition: $before open sessions before the case; removing them"
		e2e_reap_sessions
	fi
	"$fn"
	after=$(e2e_open_sessions)
	if [ "$after" != 0 ]; then
		e2e_harness "REQ $n left $after open sessions; removed"
		e2e_reap_sessions
	fi
}

e2e_reap_sessions() {
	local id
	for id in $(e2e_sq "select id from transfers where status='open'"); do
		curl -sk -m 5 -o /dev/null -X DELETE -H "Authorization: Bearer $T" "$B/transfers/$id"
	done
}

e2e_stop_pid() {
	local p=$1 i
	e2e_pid_is_ours "$p" || return 0
	kill "$p"
	for i in $(seq 1 50); do kill -0 "$p" 2>/dev/null || break; sleep 0.1; done
	e2e_pid_is_ours "$p" && kill -9 "$p"
	wait "$p" 2>/dev/null
	return 0
}

# e2e_stop: stop the current instance; remove its fixture files unless E2E_KEEP=1 (agent.log stays).
e2e_stop() {
	e2e_stop_pid "${PID:-}"
	if [ "${E2E_KEEP:-0}" != 1 ] && [ -n "${S:-}" ] && [ "${S#"$E2E_RUN"/}" != "$S" ]; then
		chmod -R u+rwX "$S" 2>/dev/null
		rm -rf -- "$S/data" "$S/root" "$S/outside" "$S/xdg" "$S/w"
	fi
}

e2e_stop_all() {
	local p
	for p in "${E2E_PIDS[@]}"; do e2e_stop_pid "$p"; done
}
trap e2e_stop_all EXIT INT TERM
