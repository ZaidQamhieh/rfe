#!/bin/bash
# Live E2E cases that need their own configuration: REQ-11, 34, 37, 39.
# Each runs on a new instance whose configuration is set at first start (never by
# restart flags), prints the precondition it depends on, and has a control that
# shows the refusal comes from the rule under test. These are the four cases that
# gave false results in pass 4.
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

call() { # tok method path [json]
	local t=$1 m=$2 p=$3 out
	if [ $# -ge 4 ]; then
		out=$(curl -sk -m 20 -X "$m" -H "Authorization: Bearer $t" -H 'Content-Type: application/json' --data "$4" -w '\n%{http_code}' "$B$p")
	else
		out=$(curl -sk -m 20 -X "$m" -H "Authorization: Bearer $t" -w '\n%{http_code}' "$B$p")
	fi
	CODE=${out##*$'\n'} BODY=${out%$'\n'*}
}
ec() { printf '%s' "$BODY" | jq -r '(.error.code // .code // "")' 2>/dev/null; }
items() { printf '%s' "$BODY" | jq -r '[.results[]? | (.error.code // "ok")] | join(",")' 2>/dev/null; }
open_s() { # tok path size sha chunk [overwrite]
	call "$1" POST /transfers "{\"path\":\"$2\",\"size\":$3,\"sha256\":\"$4\",\"chunkSize\":$5,\"overwrite\":${6:-false}}"
	ID=$(printf '%s' "$BODY" | jq -r '.id // empty' 2>/dev/null)
}
put0() { # tok id file
	local h
	h=$(sha256sum <"$3" | cut -d' ' -f1)
	CODE=$(curl -sk -m 20 -o "$W/body" -w '%{http_code}' -X PUT -H "Authorization: Bearer $1" -H "X-Chunk-Sha256: $h" -H 'Content-Type: application/octet-stream' --data-binary @"$3" "$B/transfers/$2/chunks/0")
	BODY=$(cat "$W/body")
}
# want DESC ACTUAL EXPECTED: record a check; F collects failures, EV the evidence.
want() { if [ "$2" = "$3" ]; then EV+="$1=$2 "; else F+="$1: got '$2' want '$3'; "; fi; }
# pre DESC ACTUAL EXPECTED: a precondition; a miss makes the case a fixture failure, not a product result.
pre() { if [ "$2" = "$3" ]; then EV+="[pre $1=$2] "; else PRE+="$1: got '$2' want '$3'; "; fi; }
verdict() { # n
	if [ -n "$PRE" ]; then echo "REQ $1 FAIL[fixture] precondition not met: $PRE| $EV"
	elif [ -z "$F" ]; then echo "REQ $1 PASS $EV"
	else echo "REQ $1 FAIL[product?] $F| $EV"; fi
}
SH0=$(printf '' | sha256sum | cut -d' ' -f1)
G=$((1 << 30))

# ---------------------------------------------------------------- 11
# The data dir must be refused even though the configured root contains it.
r11() {
	F= EV= PRE=
	e2e_start r11 instance || { echo "REQ 11 FAIL[fixture] instance did not start"; return; }
	local D=$S/data
	pre "roots contain the data dir" "$(printf '%s' "$E2E_EFFECTIVE" | jq -r --arg s "$S" '[.roots[]? | select(. == $s)] | length')" 1
	echo beside >"$W/b"; echo df >"$D/file11"; mkdir -p "$S/data-sibling"; echo sib >"$S/data-sibling/f"
	# Controls: paths beside the data dir, including a name that shares its prefix, are reachable.
	call "$T" GET "/fs?path=$W"; pre "control list beside" "$CODE" 200
	call "$T" GET "/content?path=$S/data-sibling/f"; pre "control prefix sibling" "$CODE" 200
	call "$T" GET "/fs?path=$D"; want "ListDir data" "$CODE/$(ec)" 403/FORBIDDEN
	call "$T" GET "/fs/meta?path=$D/file11"; want "Meta data/file" "$CODE/$(ec)" 403/FORBIDDEN
	call "$T" GET "/content?path=$D/agent.db"; want "Open agent.db" "$CODE/$(ec)" 403/FORBIDDEN
	call "$T" DELETE "/fs?permanent=true" "{\"paths\":[\"$D/file11\"]}"; want "Delete data/file" "$CODE[$(items)]" "200[FORBIDDEN]"
	want "data/file11 kept" "$([ -f "$D/file11" ] && echo y)" y
	call "$T" POST /fs/copy "{\"sources\":[\"$W/b\"],\"destDir\":\"$D\"}"; want "Copy into data" "$(items)" FORBIDDEN
	want "nothing copied in" "$([ -e "$D/b" ] && echo y || echo n)" n
	call "$T" POST /fs/copy "{\"sources\":[\"$D/file11\"],\"destDir\":\"$W\"}"; want "Copy out of data" "$(items)" FORBIDDEN
	want "nothing copied out" "$([ -e "$W/file11" ] && echo y || echo n)" n
	verdict 11
	e2e_stop
}

# ---------------------------------------------------------------- 34
# Admission limits; each sub-check starts from zero open sessions and is followed
# by a control showing the refused request is admitted once the limit is gone.
r34() {
	F= EV= PRE=
	e2e_start r34 root || { echo "REQ 34 FAIL[fixture] instance did not start"; return; }
	local d=$R/t34 i tk toks=() n
	mkdir -p "$d"
	for n in 2 3 4 5; do
		e2e_device "d$n" || { echo "REQ 34 FAIL[fixture] pairing d$n"; e2e_stop; return; }
		e2e_grant_all "$DEV_ID"; toks+=("$DEV_TOK")
	done
	pre "open sessions at start" "$(e2e_open_sessions)" 0
	for i in 1 2 3 4; do open_s "${toks[0]}" "$d/s$i" 10 "$SH0" 4096; pre "device session $i" "$CODE" 201; done
	open_s "${toks[0]}" "$d/s5" 10 "$SH0" 4096; want "device 5th session" "$CODE/$(ec)" 429/RESOURCE_LIMIT
	e2e_reap_sessions; pre "reaped" "$(e2e_open_sessions)" 0
	open_s "${toks[0]}" "$d/s5" 10 "$SH0" 4096; want "control 5th path after reap" "$CODE" 201
	e2e_reap_sessions

	open_s "${toks[0]}" "$d/big1" $((40 * G)) "$SH0" 33554432; pre "device 40GiB" "$CODE" 201
	open_s "${toks[0]}" "$d/big2" $((30 * G)) "$SH0" 33554432; want "device 70GiB (>64)" "$CODE/$(ec)" 429/RESOURCE_LIMIT
	e2e_reap_sessions
	open_s "${toks[0]}" "$d/big2" $((30 * G)) "$SH0" 33554432; want "control 30GiB alone" "$CODE" 201
	e2e_reap_sessions

	open_s "${toks[0]}" "$d/h1" $((60 * G)) "$SH0" 33554432; pre "host d2 60GiB" "$CODE" 201
	open_s "${toks[1]}" "$d/h2" $((60 * G)) "$SH0" 33554432; pre "host d3 60GiB" "$CODE" 201
	open_s "${toks[2]}" "$d/h3" $((10 * G)) "$SH0" 33554432; want "host 130GiB (>128)" "$CODE/$(ec)" 429/RESOURCE_LIMIT
	e2e_reap_sessions

	for tk in "${toks[@]}"; do for i in 1 2 3 4; do open_s "$tk" "$d/n$i-${#tk}-$RANDOM" 10 "$SH0" 4096; [ "$CODE" = 201 ] || PRE+="host fill $CODE; "; done; done
	pre "host sessions open" "$(e2e_open_sessions)" 16
	open_s "$T" "$d/n17" 10 "$SH0" 4096; want "host 17th session" "$CODE/$(ec)" 429/RESOURCE_LIMIT
	e2e_reap_sessions
	verdict 34
	e2e_stop
}

# ---------------------------------------------------------------- 37
# overwrite=true needs modify at open, each chunk and complete. The control
# (modify kept) must complete, so a 403 cannot come from anything but the grant.
r37() {
	F= EV= PRE=
	e2e_start r37 root || { echo "REQ 37 FAIL[fixture] instance did not start"; return; }
	local d=$R/t37 sh A I2 I3
	mkdir -p "$d"
	e2e_device d2 || { echo "REQ 37 FAIL[fixture] pairing d2"; e2e_stop; return; }
	local T2=$DEV_TOK ID2=$DEV_ID
	e2e_grant_all "$ID2"
	head -c 4096 /dev/urandom >"$W/x"; sh=$(sha256sum <"$W/x" | cut -d' ' -f1)
	pre "open sessions at start" "$(e2e_open_sessions)" 0
	e2e_sq "update devices set can_modify=0 where id='$ID2'"
	open_s "$T2" "$d/o.bin" 4096 "$sh" 4096 true; want "open without modify" "$CODE/$(ec)" 403/CAPABILITY_DENIED
	e2e_sq "update devices set can_modify=1 where id='$ID2'"
	open_s "$T2" "$d/a.bin" 4096 "$sh" 4096 true; A=$ID; pre "open with modify" "$CODE" 201
	e2e_sq "update devices set can_modify=0 where id='$ID2'"
	put0 "$T2" "$A" "$W/x"; want "chunk after revoke" "$CODE/$(printf '%s' "$BODY" | jq -r '.code // .error.code')" 403/CAPABILITY_DENIED
	call "$T2" POST "/transfers/$A/complete"; want "complete after revoke" "$CODE/$(ec)" 403/CAPABILITY_DENIED
	e2e_sq "update devices set can_modify=1 where id='$ID2'"
	open_s "$T2" "$d/b.bin" 4096 "$sh" 4096 true; I2=$ID; pre "open2 with modify" "$CODE" 201
	put0 "$T2" "$I2" "$W/x"; pre "chunk with modify" "$CODE" 204
	e2e_sq "update devices set can_modify=0 where id='$ID2'"
	call "$T2" POST "/transfers/$I2/complete"; want "complete after stored chunk, revoked" "$CODE/$(ec)" 403/CAPABILITY_DENIED
	want "b.bin not published" "$([ -e "$d/b.bin" ] && echo y || echo n)" n
	e2e_sq "update devices set can_modify=1 where id='$ID2'"
	open_s "$T2" "$d/c.bin" 4096 "$sh" 4096 true; I3=$ID; put0 "$T2" "$I3" "$W/x"
	call "$T2" POST "/transfers/$I3/complete"; want "control complete with modify" "$CODE" 200
	verdict 37
	e2e_reap_sessions
	e2e_stop
}

# ---------------------------------------------------------------- 39
ro_checks() { # label token dir
	local l=$1 t=$2 d=$3
	call "$t" PATCH /fs/rename "{\"src\":\"$d/a.txt\",\"dst\":\"$d/a2.txt\"}"; want "$l rename" "$CODE/$(ec)" 403/READ_ONLY
	call "$t" POST /fs/copy "{\"sources\":[\"$d/b.txt\"],\"destDir\":\"$d/dst\"}"; want "$l copy items" "$(items)" READ_ONLY
	call "$t" DELETE /fs "{\"paths\":[\"$d/c.txt\"]}"; want "$l trash items" "$(items)" READ_ONLY
	call "$t" DELETE "/fs?permanent=true" "{\"paths\":[\"$d/d.txt\"]}"; want "$l delete items" "$(items)" READ_ONLY
	open_s "$t" "$d/up.bin" 10 "$SH0" 4096; want "$l transfers" "$CODE/$(ec)" 403/READ_ONLY
	want "$l disk unchanged" "$(LC_ALL=C ls "$d" | tr '\n' ' ')" "a.txt b.txt c.txt d.txt dst "
	want "$l dst empty" "$(ls -A "$d/dst" | wc -l)" 0
}
r39() {
	F= EV= PRE=
	local d
	# Per-device read-only, with the owner (not read-only) as the control on the same instance.
	if e2e_start r39dev root; then
		d=$R/t39; mkdir -p "$d/dst"; for x in a b c d; do echo $x >"$d/$x.txt"; done
		e2e_device d2 && e2e_grant_all "$DEV_ID" && e2e_sq "update devices set read_only=1 where id='$DEV_ID'"
		pre "device d2 read_only" "$(e2e_sq "select read_only from devices where id='$DEV_ID'")" 1
		ro_checks device "$DEV_TOK" "$d"
		mkdir -p "$R/ctl"; echo x >"$R/ctl/a.txt"
		call "$T" PATCH /fs/rename "{\"src\":\"$R/ctl/a.txt\",\"dst\":\"$R/ctl/a2.txt\"}"; pre "control owner rename" "$CODE" 200
		e2e_stop
	else PRE+="instance r39dev did not start; "; fi
	# Global read-only, set at first start of a new instance.
	if e2e_start r39glob root -read-only; then
		pre "effective readOnly" "$(printf '%s' "$E2E_EFFECTIVE" | jq -r '.readOnly')" true
		d=$R/t39; mkdir -p "$d/dst"; for x in a b c d; do echo $x >"$d/$x.txt"; done
		ro_checks global "$T" "$d"
		e2e_stop
	else PRE+="instance r39glob did not start; "; fi
	verdict 39
}

for n in ${E2E_ISO_CASES:-11 34 37 39}; do "r$n"; done
