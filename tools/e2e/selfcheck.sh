#!/bin/bash
# Self-checks of the fixture layer (lib.sh), one per pass-4 false result. run.sh
# runs these first; a failure here means the case results cannot be trusted.
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
FAILS=0
ok() { echo "SELFCHECK PASS $*"; }
bad() { echo "SELFCHECK FAIL $*"; FAILS=$((FAILS + 1)); }

e2e_start sc root >/dev/null || { bad "instance sc did not start"; exit 1; }
SC_S=$S SC_PID=$PID

# 1. A data dir is never reused, and a restart cannot change the configuration.
out=$(e2e_start sc root 2>&1) && bad "e2e_start reused an existing instance dir" || { [[ $out == *"never reused"* ]] && ok "existing instance dir refused" || bad "unexpected refusal text: $out"; }
S=$SC_S PID=$SC_PID
out=$(e2e_restart -read-only 2>&1) && bad "e2e_restart accepted flags" || ok "e2e_restart refuses flags"

# 2. The premise behind rule 1: -read-only on an existing agent.db is ignored.
e2e_stop_pid "$PID"
E2E_FLAGS=(-read-only)
e2e_launch >/dev/null || bad "relaunch for seed check failed"
e2e_effective >/dev/null
ro=$(printf '%s' "$E2E_EFFECTIVE" | jq -r .readOnly)
if [ "$ro" = null ]; then
	bad "settings report no readOnly field (effective $E2E_EFFECTIVE); precondition evidence would be blind"
elif [ "$ro" = false ]; then
	ok "restart with -read-only keeps the stored readOnly=false (seed-once premise holds)"
else
	echo "SELFCHECK NOTE restart flag now applies to an existing agent.db (effective $E2E_EFFECTIVE); the seed-once rule is still safe"
fi
E2E_FLAGS=()
e2e_stop_pid "$PID"; e2e_launch >/dev/null || bad "relaunch failed"

# 3. Device lookup is by unique label and never yields the owner.
e2e_device one && ID1=$DEV_ID && e2e_device two && ID2=$DEV_ID
if [ -n "${ID1:-}" ] && [ -n "${ID2:-}" ] && [ "$ID1" != "$ID2" ] && [ "$ID1" != "$IDO" ] && [ "$ID2" != "$IDO" ]; then
	ok "devices one=$ID1 two=$ID2 owner=$IDO are distinct"
else bad "device ids one=${ID1:-} two=${ID2:-} owner=$IDO"; fi
e2e_sq "insert into devices (id,label,token_hash,created,last_seen) values ('dup-a','e2e-dev-dup','h-a',1,1),('dup-b','e2e-dev-dup','h-b',1,1)"
out=$(e2e_device_id e2e-dev-dup) && bad "ambiguous label resolved to $out" || ok "ambiguous label refused"
e2e_sq "delete from devices where id in ('dup-a','dup-b')"

# 4. A session left open by a case is reported and removed.
leaky() { curl -sk -m 5 -o /dev/null -X POST -H "Authorization: Bearer $T" -H 'Content-Type: application/json' \
	--data "{\"path\":\"$R/leak.bin\",\"size\":10,\"sha256\":\"$(printf x | sha256sum | cut -d' ' -f1)\",\"chunkSize\":4096}" "$B/transfers"; }
out=$(e2e_case 0 leaky)
if [[ $out == *"left 1 open sessions"* ]] && [ "$(e2e_open_sessions)" = 0 ]; then ok "leaked session reported and reaped"; else bad "leak check: '$out', open=$(e2e_open_sessions)"; fi

# 5. Only our own agent processes are stopped.
sleep 30 & other=$!
e2e_stop_pid "$other"
if kill -0 "$other" 2>/dev/null; then ok "foreign pid $other left alone"; kill "$other"; else bad "foreign pid $other was stopped"; fi
wait "$other" 2>/dev/null

e2e_stop
[ "$FAILS" = 0 ]
