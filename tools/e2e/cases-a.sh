#!/bin/bash
# Live E2E cases REQ-1..REQ-20 except REQ-11 (cases-iso.sh), on one fresh instance.
# Ported from the 2026-10-26 pass-4 harness; fixture setup is tools/e2e/lib.sh.
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# ---------------------------------------------------------------- helpers
call() { # METHOD PATH [json]
  local m=$1 p=$2 d=${3-} out
  if [ $# -ge 3 ]; then
    out=$(curl -sk -m 20 -X $m -H "Authorization: Bearer ${TOK:-$T}" -H 'Content-Type: application/json' --data "$d" -w '\n%{http_code}' "$B$p")
  else
    out=$(curl -sk -m 20 -X $m -H "Authorization: Bearer ${TOK:-$T}" -w '\n%{http_code}' "$B$p")
  fi
  CODE=${out##*$'\n'}; BODY=${out%$'\n'*}
}
rawput() { # PATH FILE  (octet-stream body)
  local out
  out=$(curl -sk -m 30 -X PUT -H "Authorization: Bearer ${TOK:-$T}" -H 'Content-Type: application/octet-stream' --data-binary @"$2" -w '\n%{http_code}' "$B$1")
  CODE=${out##*$'\n'}; BODY=${out%$'\n'*}
}
jqr() { printf '%s' "$BODY" | jq -r "$1" 2>/dev/null; }
ecode() { printf '%s' "$BODY" | jq -r '(.error.code // .code // .results[0].error.code // "")' 2>/dev/null; }
rok() { jqr '.results[0].ok'; }
rcode() { jqr '.results[0].error.code // ""'; }
sha() { sha256sum < "$1" | cut -d' ' -f1; }

begin() { N=$1; FAILS=""; EV=""; }
ev() { EV="$EV; $*"; }
expect() { # desc actual want
  if [ "$2" = "$3" ]; then EV="$EV; $1=$2"; else FAILS="$FAILS; $1: got '$2' want '$3' (code=$CODE body=$(printf '%s' "$BODY" | tr '\n' ' ' | cut -c1-200))"; fi
}
t() { # desc cmd...
  local d=$1; shift
  if "$@"; then EV="$EV; $d ok"; else FAILS="$FAILS; $d FAILED"; fi
}
end() {
  if [ -z "$FAILS" ]; then echo "REQ $N PASS$EV"; else echo "REQ $N FAIL[product?]${FAILS} |$EV"; fi
}
refused() { # desc : 403 either at top level or as per-item FORBIDDEN
  local d=$1
  if [ "$CODE" = 403 ]; then EV="$EV; $d=403 $(ecode)"
  elif [ "$(rok)" = false ] && [ "$(rcode)" = FORBIDDEN ]; then EV="$EV; $d=$CODE per-item FORBIDDEN"
  else FAILS="$FAILS; $d not refused: code=$CODE body=$(printf '%s' "$BODY" | tr '\n' ' ' | cut -c1-200)"; fi
}

# ---------------------------------------------------------------- setup
fail_all() { local n; for n in $(seq 1 20); do [ $n = 11 ] || echo "REQ $n FAIL[fixture] $1"; done; exit 1; }
e2e_start a root || fail_all "instance a did not start"
mkdir -p $R/jail
e2e_device d2 || fail_all "device d2 not paired"; T2=$DEV_TOK; ID2=$DEV_ID
e2e_grant_all $ID2; e2e_sq "update devices set jail_root='$R/jail' where id='$ID2'"
e2e_restart || fail_all "restart failed"
TOK=$T2 call GET "/fs?path=$R/jail"; e2e_note "d2 list jail: $CODE"
TOK=$T2 call GET "/fs?path=$R"; e2e_note "d2 list root (precondition, want 403): $CODE"
[ "$CODE" = 403 ] || fail_all "precondition: d2 jail not applied (list root=$CODE)"

# ---------------------------------------------------------------- 1
begin 1; EV="; meta requirement (brief: N-A)"; echo "REQ 1 N-A meta requirement: it sets the scope of REQ-2..41 and has no behaviour of its own to test"

# ---------------------------------------------------------------- 2
begin 2; D=$R/r2; mkdir -p $D
for kind in in out; do for mode in perm trash; do
  if [ $kind = in ]; then TG=$D/T_$mode; else TG=$O/T2_$mode; fi
  printf 'secretbytes-%s-%s' $kind $mode > $TG; chmod 640 $TG; L=$D/L_${kind}_$mode; ln -s $TG $L
  h0=$(sha $TG); m0=$(stat -c %a $TG)
  if [ $mode = perm ]; then call DELETE "/fs?path=$L&permanent=true"; else call DELETE "/fs?path=$L"; fi
  expect "$kind/$mode http" "$CODE" 200; expect "$kind/$mode ok" "$(rok)" true
  t "$kind/$mode link gone" test ! -L $L -a ! -e $L
  t "$kind/$mode target bytes" test "$(sha $TG)" = "$h0"
  expect "$kind/$mode target mode" "$(stat -c %a $TG)" "$m0"
done; done
end

# ---------------------------------------------------------------- 3
begin 3; D=$R/r3; mkdir -p $D
for kind in in out; do
  if [ $kind = in ]; then TG=$D/T3; else TG=$O/T3o; fi
  echo "t3-$kind" > $TG; ln -s $TG $D/L3_$kind; h0=$(sha $TG)
  call PATCH /fs/rename "$(jq -nc --arg s $D/L3_$kind --arg d $D/L3b_$kind '{src:$s,dst:$d}')"
  expect "$kind http" "$CODE" 200; expect "$kind isSymlink" "$(jqr .isSymlink)" true
  expect "$kind symlinkTarget" "$(jqr .symlinkTarget)" "$TG"
  expect "$kind readlink" "$(readlink $D/L3b_$kind)" "$TG"
  t "$kind old name gone" test ! -L $D/L3_$kind -a ! -e $D/L3_$kind
  t "$kind target unchanged" test "$(sha $TG)" = "$h0"
done
end

# ---------------------------------------------------------------- 4
begin 4; D=$R/r4; mkdir -p $D/D4 $D/real; echo t4 > $D/T4; ln -s $D/T4 $D/L4; ln -s $D/real $D/DL; ln -s $D/T4 $D/L4b; h0=$(sha $D/T4)
call POST /fs/move "$(jq -nc --arg s $D/L4 --arg d $D/D4 '{sources:[$s],destDir:$d}')"
expect "move http" "$CODE" 202; expect "ok" "$(rok)" true
expect "moved link target" "$(readlink $D/D4/L4)" "$D/T4"
t "src link gone" test ! -L $D/L4; t "T4 unchanged" test "$(sha $D/T4)" = "$h0"
call POST /fs/move "$(jq -nc --arg s $D/L4b --arg d $D/DL '{sources:[$s],destDir:$d}')"
expect "destDir-link http" "$CODE" 202; expect "destDir-link ok" "$(rok)" true
expect "link in real" "$(readlink $D/real/L4b)" "$D/T4"
t "src2 gone" test ! -L $D/L4b; t "T4 unchanged2" test "$(sha $D/T4)" = "$h0"
end

# ---------------------------------------------------------------- 5
begin 5; D=$R/r5; mkdir -p $D/dstA $D/dstB $D/F5; printf 'BYTES-B-5' > $D/file5; ln -s $D/file5 $D/L5
printf 'UNIQ5-TARGET-DATA' > $D/T5; ln -s $D/T5 $D/F5/X; echo plain > $D/F5/plain
call POST /fs/copy "$(jq -nc --arg s $D/L5 --arg d $D/dstA '{sources:[$s],destDir:$d}')"
expect "a http" "$CODE" 202; expect "a ok" "$(rok)" true
t "a regular file" test -f $D/dstA/L5 -a ! -L $D/dstA/L5
t "a bytes" test "$(cat $D/dstA/L5 2>/dev/null)" = "BYTES-B-5"
call POST /fs/copy "$(jq -nc --arg s $D/F5 --arg d $D/dstB '{sources:[$s],destDir:$d}')"
expect "b http" "$CODE" 202; expect "b ok" "$(rok)" true
t "b X is symlink" test -L $D/dstB/F5/X
expect "b readlink" "$(readlink $D/dstB/F5/X)" "$D/T5"
t "b no T bytes under D/F" test -z "$(rg -l UNIQ5 $D/dstB 2>/dev/null)"
t "b plain copied" test -f $D/dstB/F5/plain
end

# ---------------------------------------------------------------- 6
begin 6; D=$R/r6; mkdir -p $D/dst; printf 'TOPSECRET6' > $O/secret6; ln -s $O/secret6 $D/L6
call POST /fs/copy "$(jq -nc --arg s $D/L6 --arg d $D/dst '{sources:[$s],destDir:$d}')"
ev "copy http=$CODE ok=$(rok) code=$(rcode)"
if [ "$(rok)" = false ] || [ "$CODE" = 403 ]; then EV="$EV; copy per-item error"; else FAILS="$FAILS; copy not refused: $CODE $BODY"; fi
t "no secret under dst" test -z "$(rg -l TOPSECRET6 $D/dst 2>/dev/null)"
t "no entry named L6 in dst" test ! -e $D/dst/L6
call GET "/content?path=$D/L6"
expect "content http" "$CODE" 403; expect "content code" "$(ecode)" FORBIDDEN
t "no secret bytes in body" test -z "$(printf '%s' "$BODY" | rg TOPSECRET6)"
end

# ---------------------------------------------------------------- 7
begin 7
call GET "/fs?path=relative/dir"; expect "GET /fs rel http" "$CODE" 403; expect "GET /fs rel code" "$(ecode)" FORBIDDEN
call POST /fs/folder '{"path":"relative/x"}'; expect "POST folder rel http" "$CODE" 403; expect "code" "$(ecode)" FORBIDDEN
t "nothing under agent cwd" test ! -e $S/relative
t "nothing under root cwd" test ! -e $R/relative
end

# ---------------------------------------------------------------- 8
begin 8; echo orig8 > $O/x; mkdir -p $R/r8; echo c8 > $R/r8/f8; h0=$(sha $O/x)
call GET "/fs?path=$O"; expect "GET /fs outside" "$CODE" 403
echo changed8 > $S/put8.txt; rawput "/content?path=$O/x" $S/put8.txt; expect "PUT content outside" "$CODE" 403
call DELETE "/fs?path=$O/x"; refused "DELETE outside"
call DELETE "/fs?path=$O/x&permanent=true"; refused "DELETE permanent outside"
call POST /fs/compress "$(jq -nc --arg s $R/r8/f8 --arg d $O/a.zip '{sources:[$s],dest:$d}')"; expect "compress dest outside" "$CODE" 403
t "O/x unchanged" test "$(sha $O/x)" = "$h0"; t "no a.zip" test ! -e $O/a.zip
end

# ---------------------------------------------------------------- 9
begin 9; mkdir -p $R/sub; echo plain9 > $R/plain.txt; echo orig9 > $O/x9; h0=$(sha $O/x9)
call GET "/fs/meta?path=$R/sub/../plain.txt"; expect "inside dotdot http" "$CODE" 200
call GET "/fs/meta?path=$R/sub/../../outside/x9"; expect "escape meta" "$CODE" 403
call DELETE "/fs?path=$R/sub/../../outside/x9&permanent=true"; refused "escape delete"
call POST /fs/folder "$(jq -nc --arg p $R/sub/../../outside/new9 '{path:$p}')"; expect "escape mkdir" "$CODE" 403
t "O/x9 untouched" test "$(sha $O/x9)" = "$h0"; t "no O/new9" test ! -e $O/new9
end

# ---------------------------------------------------------------- 10
begin 10; echo orig10 > $O/x10; ln -s $O $R/link10; h0=$(sha $O/x10)
call POST /fs/folder "$(jq -nc --arg p $R/link10/new '{path:$p}')"; expect "mkdir via link" "$CODE" 403
t "O/new absent" test ! -e $O/new
call GET "/fs?path=$R/link10/"; expect "list link/" "$CODE" 403
call GET "/fs?path=$R/link10"; expect "list link" "$CODE" 403
call GET "/fs?path=$R/link10/sub"; expect "list link/sub" "$CODE" 403
call GET "/content?path=$R/link10/x10"; expect "read link/x10" "$CODE" 403
echo chg10 > $S/put10.txt; rawput "/content?path=$R/link10/x10" $S/put10.txt; expect "write link/x10" "$CODE" 403
t "O/x10 unchanged" test "$(sha $O/x10)" = "$h0"
end

# ---------------------------------------------------------------- 12
begin 12; D=$R/r12; mkdir -p $D; mkfifo $D/p
printf 'import socket,sys\ns=socket.socket(socket.AF_UNIX)\ns.bind(sys.argv[1])\n' > $S/mksock.py; python3 -I $S/mksock.py $D/s
ts=$(date +%s%N)
call GET "/content?path=$D/p"; te=$(date +%s%N)
expect "fifo content http" "$CODE" 422; expect "fifo code" "$(ecode)" NOT_A_FILE
ev "fifo took $(( (te - ts) / 1000000 ))ms"
call GET "/content?path=$D/s"; expect "socket content http" "$CODE" 422; expect "socket code" "$(ecode)" NOT_A_FILE
call GET "/fs/checksum?path=$D/p"; expect "fifo checksum http" "$CODE" 422
call GET "/fs/checksum?path=$D/s"; expect "socket checksum http" "$CODE" 422
call GET "/fs?path=$D/p"; expect "fifo list http" "$CODE" 422
ev "device nodes: not creatable unprivileged, /dev links fall outside roots (not tested)"
end

# ---------------------------------------------------------------- 13
begin 13; D=$R/r13; mkdir -p $D/dir13/sub; echo f > $D/f13; echo c1 > $D/dir13/c1; echo c2 > $D/dir13/sub/c2
call DELETE "/fs?path=$D/f13"; expect "trash delete http" "$CODE" 200; expect "ok" "$(rok)" true
t "f13 moved away" test ! -e $D/f13
call GET /trash; expect "trash list http" "$CODE" 200
expect "trash lists f13 originalPath" "$(printf '%s' "$BODY" | jq --arg p $D/f13 '[.items[]|select(.originalPath==$p)]|length')" 1
n0=$(printf '%s' "$BODY" | jq '.items|length')
call DELETE "/fs?path=$D/dir13&permanent=true"; expect "permanent http" "$CODE" 200; expect "perm ok" "$(rok)" true
t "dir13 gone" test ! -e $D/dir13; t "children gone" test ! -e $D/dir13/c1
call GET /trash; n1=$(printf '%s' "$BODY" | jq '.items|length')
expect "trash count unchanged by permanent" "$n1" "$n0"
expect "no trash entry for dir13" "$(printf '%s' "$BODY" | jq --arg p $D/dir13 '[.items[]|select(.originalPath|startswith($p))]|length')" 0
t "no dir13 in XDG trash files" test -z "$(ls $XDG/Trash/files | rg dir13)"
end

# ---------------------------------------------------------------- 14
begin 14; D=$R/r14; mkdir -p $D/dst $D/src $D/src2; printf old > $D/dst/F; printf new > $D/src/F; printf new2 > $D/src2/F
call POST /fs/copy "$(jq -nc --arg s $D/src/F --arg d $D/dst '{sources:[$s],destDir:$d,overwrite:false,duplicate:false}')"
expect "copy no-overwrite ok" "$(rok)" false; expect "copy code" "$(rcode)" CONFLICT; expect "F unchanged" "$(cat $D/dst/F)" old
call POST /fs/copy "$(jq -nc --arg s $D/src/F --arg d $D/dst '{sources:[$s],destDir:$d,overwrite:true,duplicate:false}')"
expect "copy overwrite ok" "$(rok)" true; expect "F = source" "$(cat $D/dst/F)" new
call POST /fs/move "$(jq -nc --arg s $D/src2/F --arg d $D/dst '{sources:[$s],destDir:$d,overwrite:false,duplicate:false}')"
expect "move no-overwrite ok" "$(rok)" false; expect "move code" "$(rcode)" CONFLICT; expect "F unchanged (move)" "$(cat $D/dst/F)" new
t "move source still there" test -f $D/src2/F
call POST /fs/move "$(jq -nc --arg s $D/src2/F --arg d $D/dst '{sources:[$s],destDir:$d,overwrite:true,duplicate:false}')"
expect "move overwrite ok" "$(rok)" true; expect "F = move source" "$(cat $D/dst/F)" new2; t "move source gone" test ! -e $D/src2/F
end

# ---------------------------------------------------------------- 15
begin 15; D=$R/r15; mkdir -p $D/dst $D/src; printf orig > $D/dst/F; printf srcbytes > $D/src/F; printf mvbytes > $D/src/G; printf origG > $D/dst/G
call POST /fs/copy "$(jq -nc --arg s $D/src/F --arg d $D/dst '{sources:[$s],destDir:$d,overwrite:true,duplicate:true}')"
expect "copy ok" "$(rok)" true; ev "result path (info)=$(jqr '.results[0].path')"
expect "F unchanged" "$(cat $D/dst/F)" orig; expect "F (1) bytes" "$(cat "$D/dst/F (1)" 2>/dev/null)" srcbytes
call POST /fs/move "$(jq -nc --arg s $D/src/G --arg d $D/dst '{sources:[$s],destDir:$d,overwrite:true,duplicate:true}')"
expect "move ok" "$(rok)" true; expect "G unchanged" "$(cat $D/dst/G)" origG; expect "G (1) bytes" "$(cat "$D/dst/G (1)" 2>/dev/null)" mvbytes
end

# ---------------------------------------------------------------- 16
begin 16; D=$R/r16; mkdir -p $D/src/D $D/dst/D; printf good > $D/src/D/a; printf bad > $D/src/D/b; chmod 000 $D/src/D/b; printf keep > $D/dst/D/keep.txt
ev "uid=$(id -u)"
call POST /fs/copy "$(jq -nc --arg s $D/src/D --arg d $D/dst '{sources:[$s],destDir:$d,overwrite:true}')"
expect "(a) copy fails" "$(rok)" false; ev "(a) code=$(rcode)"
expect "(a) orig bytes" "$(cat $D/dst/D/keep.txt 2>/dev/null)" keep
t "(a) no a leaked into dst/D" test ! -e $D/dst/D/a
expect "(a) dst entries" "$(ls -A $D/dst | tr '\n' ' ')" "D "
chmod 600 $D/src/D/b
# (b) move D into D/sub
mkdir -p $D/b/D/sub; echo inner > $D/b/D/f
call POST /fs/move "$(jq -nc --arg s $D/b/D --arg d $D/b/D/sub '{sources:[$s],destDir:$d,overwrite:true}')"
expect "(b) move into own sub ok" "$(rok)" false; ev "(b) code=$(rcode)"
t "(b) D intact" test "$(cat $D/b/D/f 2>/dev/null)" = inner
call POST /fs/copy "$(jq -nc --arg s $D/b/D --arg d $D/b/D/sub '{sources:[$s],destDir:$d,overwrite:true}')"
ev "(b') copy into own sub ok=$(rok) code=$(rcode)"; t "(b') D intact" test "$(cat $D/b/D/f 2>/dev/null)" = inner
# (c) destination that contains the source: source n/n, destDir parent => dest = n which contains source
mkdir -p $D/c/n/n; echo deep > $D/c/n/n/f; echo top > $D/c/n/g
call POST /fs/move "$(jq -nc --arg s $D/c/n/n --arg d $D/c '{sources:[$s],destDir:$d,overwrite:true}')"
expect "(c) move overwrite onto ancestor ok" "$(rok)" false; ev "(c) code=$(rcode)"
t "(c) n/n/f intact" test "$(cat $D/c/n/n/f 2>/dev/null)" = deep; t "(c) n/g intact" test "$(cat $D/c/n/g 2>/dev/null)" = top
call POST /fs/copy "$(jq -nc --arg s $D/c/n/n --arg d $D/c '{sources:[$s],destDir:$d,overwrite:true}')"
ev "(c') copy onto ancestor ok=$(rok) code=$(rcode)"
t "(c') n/n/f intact" test "$(cat $D/c/n/n/f 2>/dev/null)" = deep; t "(c') n/g intact" test "$(cat $D/c/n/g 2>/dev/null)" = top
end

# ---------------------------------------------------------------- 17
begin 17; D=$R/r17; mkdir -p $D/D17; echo A > $D/a; echo Bb > $D/b; echo dd > $D/D17/f
call PATCH /fs/rename "$(jq -nc --arg s $D/a --arg d $D/b '{src:$s,dst:$d}')"
expect "rename onto file http" "$CODE" 409; expect "code" "$(ecode)" CONFLICT
expect "a intact" "$(cat $D/a)" A; expect "b intact" "$(cat $D/b)" Bb
call PATCH /fs/rename "$(jq -nc --arg s $D/D17 --arg d $D/D17/sub/x '{src:$s,dst:$d}')"
expect "rename into subtree http" "$CODE" 409; expect "code" "$(ecode)" CONFLICT
t "no D17/sub" test ! -e $D/D17/sub; t "D17/f intact" test -f $D/D17/f
mkdir -p $D/E17; call PATCH /fs/rename "$(jq -nc --arg s $D/E17 --arg d $D/D17 '{src:$s,dst:$d}')"
expect "rename folder onto folder" "$CODE" 409; t "both folders intact" test -d $D/E17 -a -f $D/D17/f
end

# ---------------------------------------------------------------- 18
begin 18; D=$R/r18; mkdir -p $D; printf trashed18 > $D/f18.txt
call DELETE "/fs?path=$D/f18.txt"; expect "trash http" "$CODE" 200
printf occupant18 > $D/f18.txt
call GET /trash; ID=$(printf '%s' "$BODY" | jq -r --arg p $D/f18.txt '[.items[]|select(.originalPath==$p)][0].id')
ev "trash id=$ID"
call POST /trash/restore "$(jq -nc --arg i "$ID" '{ids:[$i]}')"
expect "restore http" "$CODE" 200; expect "restore ok" "$(rok)" true
expect "result path" "$(jqr '.results[0].path')" "$D/f18 (1).txt"
expect "restored bytes" "$(cat "$D/f18 (1).txt" 2>/dev/null)" trashed18
expect "original intact" "$(cat $D/f18.txt)" occupant18
end


# ---------------------------------------------------------------- 19 (R19 as decided at the 2026-10-26 closure: restore is a batch)
begin 19; mkdir -p $XDG/Trash/files $XDG/Trash/info $R/r19
printf sentinelX > $XDG/Trash/x; printf sentinelI > $XDG/Trash/x.trashinfo; printf sentinelO > $S/outside_sentinel
h0=$(sha $XDG/Trash/x); h1=$(sha $XDG/Trash/x.trashinfo); h2=$(sha $S/outside_sentinel)
for id in "../x" ".." "." "a/b" "/abs" "../../outside/x" "../../outside_sentinel"; do
  call POST /trash/restore "$(jq -nc --arg i "$id" '{ids:[$i]}')"
  expect "restore '$id' http" "$CODE" 200; expect "restore '$id' ok" "$(rok)" false; expect "restore '$id' item" "$(rcode)" BAD_REQUEST
  expect "restore '$id' path echo" "$(jqr '.results[0].path')" "$id"
  call DELETE /trash "$(jq -nc --arg i "$id" '{ids:[$i]}')"; expect "delete '$id'" "$CODE" 400
done
# mixed batch: one real item plus an invalid id
printf mixed19 > $R/r19/m.txt; call DELETE "/fs?path=$R/r19/m.txt"
call GET /trash; MID=$(printf '%s' "$BODY" | jq -r --arg p $R/r19/m.txt '[.items[]|select(.originalPath==$p)][0].id')
call POST /trash/restore "$(jq -nc --arg i "$MID" '{ids:[$i,"../x"]}')"
expect "mixed http" "$CODE" 200; expect "mixed results" "$(jqr '.results|length')" 2
expect "mixed valid ok" "$(jqr '.results[0].ok')" true; expect "mixed invalid" "$(jqr '.results[1].ok'),$(jqr '.results[1].error.code')" false,BAD_REQUEST
t "mixed valid restored" test "$(cat $R/r19/m.txt 2>/dev/null)" = mixed19
for b in '{}' '{"ids":[]}' '{bad'; do call POST /trash/restore "$b"; expect "restore body $b" "$CODE" 400; done
t "Trash/x intact" test "$(sha $XDG/Trash/x 2>/dev/null)" = "$h0"; t "Trash/x.trashinfo intact" test "$(sha $XDG/Trash/x.trashinfo 2>/dev/null)" = "$h1"
t "outside sentinel intact" test "$(sha $S/outside_sentinel)" = "$h2"
t "Trash/files still there" test -d $XDG/Trash/files; t "Trash/info still there" test -d $XDG/Trash/info
end
# ---------------------------------------------------------------- 20
begin 20; D=$R/r20; mkdir -p $D
call DELETE /trash; ev "pre-empty http=$CODE"
printf j1 > $R/jail/j1; printf o1 > $D/o1; printf o2 > $D/o2
call DELETE "/fs?path=$R/jail/j1"; call DELETE "/fs?path=$D/o1"
call GET /trash; ev "owner lists $(printf '%s' "$BODY" | jq -r '[.items[].originalPath]|join(",")')"
expect "owner sees 2" "$(printf '%s' "$BODY" | jq '.items|length')" 2
for b in '{}' 'null' '{"ids":[]}' '{"ids":null}' '{bad' '[]' '{"ids":"x"}'; do
  call DELETE /trash "$b"; expect "body $b" "$CODE" 400
done
call GET /trash; expect "nothing removed by bad bodies" "$(printf '%s' "$BODY" | jq '.items|length')" 2
TOK=$T2 call GET /trash; expect "jailed list http" "$CODE" 200
expect "jailed list paths" "$(printf '%s' "$BODY" | jq -r '[.items[].originalPath]|join(",")')" "$R/jail/j1"
TOK=$T2 call DELETE /trash; expect "jailed empty http" "$CODE" 200
call GET /trash; expect "owner left with only outside item" "$(printf '%s' "$BODY" | jq -r '[.items[].originalPath]|join(",")')" "$D/o1"
# ids body
call DELETE "/fs?path=$D/o2"; call GET /trash; OID=$(printf '%s' "$BODY" | jq -r --arg p $D/o2 '[.items[]|select(.originalPath==$p)][0].id')
call DELETE /trash "$(jq -nc --arg i "$OID" '{ids:[$i]}')"; expect "ids body http" "$CODE" 200
call GET /trash; expect "o2 removed, o1 kept" "$(printf '%s' "$BODY" | jq -r '[.items[].originalPath]|join(",")')" "$D/o1"
call DELETE /trash; expect "no body empties all http" "$CODE" 200
call GET /trash; expect "trash empty" "$(printf '%s' "$BODY" | jq '.items|length')" 0
end


e2e_stop
