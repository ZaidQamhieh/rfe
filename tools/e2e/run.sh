#!/bin/bash
export LC_ALL=C
# Live E2E of the 41 cases in cases.md against a fresh build of ../../agent.
#   tools/e2e/run.sh              build the agent from this checkout, self-check, run all cases
#   E2E_AGENT=/path/agent run.sh  test that binary instead
#   E2E_KEEP=1                    keep each instance's files (default: logs only)
# Needs bash, curl, jq, rg, sqlite3, python3, go. Everything runs on 127.0.0.1 under a new
# /tmp/rfe-e2e-run.* dir; XDG_DATA_HOME points inside it, so no real trash or home file is touched.
set -u
HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO=$(cd "$HERE/../.." && pwd)
E2E_RUN=$(mktemp -d /tmp/rfe-e2e-run.XXXXXX)
export E2E_RUN
mkdir -p "$E2E_RUN/bin"
echo "run dir: $E2E_RUN"

if [ -z "${E2E_AGENT:-}" ]; then
	(cd "$REPO/agent" && go build -o "$E2E_RUN/bin/agent" ./cmd/agent) || { echo "agent build failed (env)"; exit 3; }
	E2E_AGENT=$E2E_RUN/bin/agent
fi
(cd "$HERE/client" && go build -o "$E2E_RUN/bin/client" .) || { echo "client build failed (env)"; exit 3; }
export E2E_AGENT E2E_CLIENT=$E2E_RUN/bin/client
{
	echo "agent sha256: $(sha256sum <"$E2E_AGENT" | cut -d' ' -f1)"
	echo "source: $(git -C "$REPO" rev-parse --short HEAD) + diff sha256 $(git -C "$REPO" diff HEAD | sha256sum | cut -d' ' -f1)"
	echo "started: $(date -Is)"
} | tee "$E2E_RUN/build.txt"

if [ "${E2E_SKIP_SELFCHECK:-0}" != 1 ]; then
	bash "$HERE/selfcheck.sh" | tee "$E2E_RUN/selfcheck.log"
	[ "${PIPESTATUS[0]}" = 0 ] || { echo "fixture self-check failed: case results would not be trustworthy"; exit 4; }
fi

for part in a b iso; do
	bash "$HERE/cases-$part.sh" >"$E2E_RUN/cases-$part.log" 2>&1
	echo "cases-$part: exit $?"
done

# One verdict per case: a missing case is NOT RUN, a duplicate is a harness fault.
cat "$E2E_RUN"/cases-*.log | rg '^REQ [0-9]+ ' >"$E2E_RUN/verdicts.txt"
{
	echo "| Case | Status | Evidence |"
	echo "|---|---|---|"
	for n in $(seq 1 41); do
		lines=$(rg "^REQ $n " "$E2E_RUN/verdicts.txt")
		count=$(printf '%s' "$lines" | rg -c '^REQ' || true)
		if [ -z "$lines" ]; then
			echo "| REQ-$n | NOT RUN | no verdict printed |"
		elif [ "${count:-0}" -gt 1 ]; then
			echo "| REQ-$n | FAIL[harness] | $count verdicts printed |"
		else
			st=$(printf '%s' "$lines" | awk '{print $3}' | tr -d ';')
			ev=$(printf '%s' "$lines" | cut -d' ' -f4- | tr '|' '/' | cut -c1-400)
			echo "| REQ-$n | $st | $ev |"
		fi
	done
} >"$E2E_RUN/matrix.md"
rg --no-filename '^HARNESS' "$E2E_RUN"/cases-*.log >"$E2E_RUN/harness-notes.txt" || true

left=$(pgrep -f -- "-data $E2E_RUN/" || true)
[ -z "$left" ] || echo "WARNING: agent processes from this run still alive: $left"
echo "== matrix ($E2E_RUN/matrix.md)"
cat "$E2E_RUN/matrix.md"
echo "== harness notes"; cat "$E2E_RUN/harness-notes.txt"
echo "== counts"
awk -F' [|] ' 'NR>2 {split($2,a,"["); c[a[1]]++} END {for (k in c) printf "%s %d\n", k, c[k]}' "$E2E_RUN/matrix.md" | sort
! rg -q '\| (FAIL|NOT RUN)' "$E2E_RUN/matrix.md"
