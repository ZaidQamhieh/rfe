#!/bin/bash
# Live E2E cases REQ-21..REQ-41 except REQ-34, 37 and 39 (cases-iso.sh), on one fresh instance.
# Ported from the 2026-10-26 pass-4 harness; fixture setup is tools/e2e/lib.sh.
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
MKARCH=$E2E_DIR/mkarch.py
note() { echo "# $*"; }
fin() { # n failures evidence
  if [ -z "$2" ]; then echo "REQ $1 PASS $3"; else echo "REQ $1 FAIL[product?] ${2}| $3"; fi
}
na() { echo "REQ $1 N-A $2"; }
call() { # tok method path [curl args]
  local tok=$1 m=$2 p=$3; shift 3
  CODE=$(curl -sk -X "$m" -H "Authorization: Bearer $tok" -D $W/hdr -o $W/body -w '%{http_code}' "$B$p" "$@")
  BODY=$(cat $W/body)
}
jc() { call "$1" "$2" "$3" -H 'Content-Type: application/json' --data "$4"; }
ecode() { echo "$BODY" | jq -r '.error.code // .code // empty' 2>/dev/null; }
short() { echo "$BODY" | tr '\n' ' ' | cut -c1-${1:-160}; }
sq() { e2e_sq "$1"; }
sha() { sha256sum "$1" | cut -d' ' -f1; }
rnd() { head -c "$2" /dev/urandom > "$1"; }


# transfer helpers
openS() { # tok path size sha chunk [overwrite]  -> CODE BODY ID
  local ow=${6:-false}
  jc "$1" POST /transfers "{\"path\":\"$2\",\"size\":$3,\"sha256\":\"$4\",\"chunkSize\":$5,\"overwrite\":$ow}"
  ID=$(echo "$BODY" | jq -r '.id // empty' 2>/dev/null)
}
putc() { # tok id n file [hashoverride]
  local h=${5:-$(sha "$4")}
  CODE=$(curl -sk -X PUT -H "Authorization: Bearer $1" -H "X-Chunk-Sha256: $h" -H 'Content-Type: application/octet-stream' --data-binary @"$4" -D $W/hdr -o $W/body -w '%{http_code}' "$B/transfers/$2/chunks/$3")
  BODY=$(cat $W/body)
}
received() { call "$1" GET /transfers/$2; echo "$BODY" | jq -c '.receivedChunks'; }
chunkfile() { dd if="$1" bs=$3 skip=$2 count=1 status=none > $W/cf.$2; echo $W/cf.$2; }
uploadall() { # tok id file chunk n
  local i f
  for ((i=0;i<$5;i++)); do f=$(chunkfile "$3" $i $4); putc "$1" "$2" $i "$f"; [ "$CODE" = 204 ] || { echo "chunk $i -> $CODE"; return 1; }; done
}
listrows() { call $T GET /transfers/list; echo "$BODY" | jq 'if type=="array" then length else ((.transfers // .items // .sessions // []) | length) end' 2>/dev/null; }

fail_all() { local n; for n in 21 22 23 24 25 26 27 28 29 30 31 32 33 35 36 38 40 41; do echo "REQ $n FAIL[fixture] $1"; done; exit 1; }
e2e_start b root || fail_all "instance b did not start"
pairdev() { e2e_device "$1" || fail_all "pairing $1 failed"; }
grant() { e2e_grant_all "$1"; }
pairdev tb; TB=$DEV_TOK IDB=$DEV_ID                 # never granted
pairdev d2; T2=$DEV_TOK ID2=$DEV_ID; grant $ID2
pairdev d3; T3=$DEV_TOK ID3=$DEV_ID; grant $ID3
pairdev d4; T4=$DEV_TOK ID4=$DEV_ID; grant $ID4
pairdev d5; T5=$DEV_TOK ID5=$DEV_ID; grant $ID5
note "devices: owner=$IDO tb=$IDB d2=$ID2 d3=$ID3 d4=$ID4 d5=$ID5"
call $T GET "/fs?path=$R"; note "owner list: $CODE"
call $T2 GET "/fs?path=$R"; note "d2 list: $CODE"

############ 21 compress keep-both
r21() {
  local f="" d=$R/c21
  mkdir -p $d; echo hello > $d/a.txt; echo ORIGZIP > $d/dest.zip
  jc $T POST /fs/compress "{\"sources\":[\"$d/a.txt\"],\"dest\":\"$d/dest.zip\"}"
  local p; p=$(echo "$BODY" | jq -r .path)
  [ "$CODE" = 201 ] || f+="code=$CODE "
  [ "$p" = "$d/dest (1).zip" ] || f+="entry.path=$p "
  [ -f "$d/dest (1).zip" ] || f+="no (1) file "
  [ "$(cat $d/dest.zip)" = ORIGZIP ] || f+="dest.zip changed "
  jc $T POST /fs/compress "{\"sources\":[\"$d/a.txt\"],\"dest\":\"$d/dest.zip\"}"
  p2=$(echo "$BODY" | jq -r .path)
  [ "$p2" = "$d/dest (2).zip" ] || f+="second path=$p2 "
  fin 21 "$f" "code=$CODE entry.path=$p, 2nd=$p2, dest.zip=$(cat $d/dest.zip)"
}


############ 22 compress jail/atomic
r22() {
  local f="" d=$R/c22
  mkdir -p $d; echo in > $d/in.txt; echo out > $O/secret.txt
  jc $T POST /fs/compress "{\"sources\":[\"$d/in.txt\",\"$O/secret.txt\"],\"dest\":\"$d/d1.zip\"}"
  local c1=$CODE e1; e1=$(ecode)
  [ "$c1" = 403 ] || f+="outside-source code=$c1 "
  [ -e $d/d1.zip ] && f+="d1.zip created "
  ls -A $d | rg -q 'rfe-tmp' && f+="tmp left (outside case) "
  jc $T POST /fs/compress "{\"sources\":[\"$d/in.txt\"],\"dest\":\"$O/d3.zip\"}"
  local c3=$CODE
  [ "$c3" = 403 ] || f+="outside-dest code=$c3 "
  [ -e $O/d3.zip ] && f+="outside dest created "
  echo secret > $d/unread.txt; chmod 000 $d/unread.txt
  jc $T POST /fs/compress "{\"sources\":[\"$d/in.txt\",\"$d/unread.txt\"],\"dest\":\"$d/dest.zip\"}"
  local c2=$CODE b2; b2=$(short 100)
  chmod 644 $d/unread.txt
  case $c2 in 2*) f+="unreadable source accepted code=$c2 ";; esac
  [ -e $d/dest.zip ] && f+="dest.zip exists "
  ls -A $d | rg -q 'rfe-tmp' && f+="tmp file left: $(ls -A $d | rg rfe-tmp | tr '\n' ' ') "
  fin 22 "$f" "outside-src=$c1/$e1 outside-dest=$c3 unreadable=$c2 $b2; dir=$(ls -A $d | tr '\n' ',')"
}


############ 23 zip-slip and absolute names (R23 as decided at the 2026-10-26 closure: refuse, roll back)
r23() {
  local f="" d=$R/x23 ev="" name i=0
  mkdir -p $d
  python3 -I $MKARCH zipnames $d/slip.zip "ok.txt=ok" "../evil.txt=EVIL"
  jc $T POST /fs/extract "{\"archive\":\"$d/slip.zip\",\"destDir\":\"$d/out-slip\"}"
  [ "$CODE" = 403 ] && [ "$(ecode)" = FORBIDDEN ] || f+="slip=$CODE/$(ecode) "
  ev+="slip=$CODE/$(ecode) "
  for name in "$O/abs23.txt" '/abs23-root.txt' 'C:\abs23\win.txt' 'C:/abs23/win.txt' '\\srv\share\abs23.txt' '//srv/share/abs23.txt'; do
    i=$((i+1)); mkdir -p $d/out$i; echo mine > $d/out$i/ok.txt
    python3 -I $MKARCH zipnames $d/abs$i.zip "new.txt=new" "ok.txt=theirs" "$name=ABS"
    jc $T POST /fs/extract "$(jq -nc --arg a $d/abs$i.zip --arg o $d/out$i '{archive:$a,destDir:$o}')"
    local c=$CODE e; e=$(ecode)
    [ "$c" = 403 ] && [ "$e" = FORBIDDEN ] || f+="abs[$name]=$c/$e "
    [ "$(ls -A $d/out$i | tr '\n' ',')" = "ok.txt," ] || f+="abs[$name] out$i holds $(ls -A $d/out$i | tr '\n' ',') (want only the pre-existing ok.txt) "
    [ "$(cat $d/out$i/ok.txt)" = mine ] || f+="abs[$name] pre-existing ok.txt changed "
    ev+="abs[$name]=$c/$e "
  done
  for p in $d/evil.txt $R/evil.txt $O/abs23.txt /abs23-root.txt; do [ -e "$p" ] && f+="exists:$p "; done
  python3 -I $MKARCH zipnames $d/rel.zip "./dot.txt=dot" "x/../inside.txt=in"
  jc $T POST /fs/extract "{\"archive\":\"$d/rel.zip\",\"destDir\":\"$d/out-rel\"}"
  [ "$CODE" = 200 ] && [ "$(cat $d/out-rel/dot.txt 2>/dev/null)" = dot ] && [ "$(cat $d/out-rel/inside.txt 2>/dev/null)" = in ] || f+="relative names=$CODE $(short 100) "
  ev+="relative=$CODE"
  fin 23 "$f" "$ev"
}

############ 24 symlink write-through / special entries
r24() {
  local f="" d=$R/x24
  mkdir -p $d/dest $O/o24; ln -s $O/o24 $d/dest/l
  python3 -I $MKARCH zipnames $d/thru.zip "l/x=THRU" "ok.txt=ok"
  jc $T POST /fs/extract "{\"archive\":\"$d/thru.zip\",\"destDir\":\"$d/dest\"}"
  local c1=$CODE s1; s1=$(short 100)
  [ -e $O/o24/x ] && f+="O/x written "
  case $c1 in 2*) note "24: write-through zip got $c1 (skip semantic?)";; esac
  python3 -I $MKARCH zipsym $d/sym.zip "$O/o24"
  jc $T POST /fs/extract "{\"archive\":\"$d/sym.zip\",\"destDir\":\"$d/zs\"}"; local c2=$CODE
  python3 -I $MKARCH tarsym $d/sym.tar.gz "$O/o24"
  jc $T POST /fs/extract "{\"archive\":\"$d/sym.tar.gz\",\"destDir\":\"$d/ts\"}"; local c3=$CODE
  local n; n=$(find $d/zs $d/ts -type l 2>/dev/null | wc -l)
  [ "$n" = 0 ] || f+="links created: $(find $d/zs $d/ts -type l | tr '\n' ' ') "
  [ -e $d/ts/hard ] && [ "$(stat -c %h $d/ts/ok.txt)" != 1 ] && f+="hardlink created "
  fin 24 "$f" "thru=$c1 $s1 O/x absent=$([ -e $O/o24/x ] && echo no || echo yes); zipsym=$c2 tarsym=$c3 links=$n; zs=$(ls -A $d/zs 2>&1|tr '\n' ',') ts=$(ls -A $d/ts 2>&1|tr '\n' ',')"
}


############ 25 extract never replaces
r25() {
  local f="" d=$R/x25
  mkdir -p $d; printf A > $d/f.txt
  python3 -I $MKARCH zipnames $d/a.zip "f.txt=B"
  jc $T POST /fs/extract "{\"archive\":\"$d/a.zip\",\"destDir\":\"$d\"}"
  [ "$CODE" = 200 ] || f+="code=$CODE "
  [ "$(cat $d/f.txt)" = A ] || f+="f.txt changed "
  [ "$(cat "$d/f (1).txt" 2>/dev/null)" = B ] || f+="no 'f (1).txt'=B "
  fin 25 "$f" "code=$CODE f.txt=$(cat $d/f.txt) f(1).txt=$(cat "$d/f (1).txt" 2>&1)"
}


############ 26 extract limits
r26() {
  local f="" d=$R/x26
  mkdir -p $d
  python3 -I $MKARCH many $d/many.zip 100001
  jc $T POST /fs/extract "{\"archive\":\"$d/many.zip\",\"destDir\":\"$d/m\"}"
  local c1=$CODE s1; s1=$(short 120)
  case $c1 in 2*) f+="100001 entries accepted ";; esac
  local n1; n1=$(find $d/m -type f 2>/dev/null | wc -l)
  python3 -I $MKARCH zipnames $d/ok.zip "a=1"   # sanity: small ok
  python3 -I $MKARCH big $d/big.zip $((2*1024*1024*1024 + 1048576))
  jc $T POST /fs/extract "{\"archive\":\"$d/big.zip\",\"destDir\":\"$d/b\"}"
  local c2=$CODE s2; s2=$(short 120)
  case $c2 in 2*) f+="2GiB+ accepted ";; esac
  local n2; n2=$(find $d/b -type f 2>/dev/null | wc -l)
  [ "$n2" = 0 ] || f+="files left after big: $(find $d/b -type f -printf '%p:%s ' | head -c 200) "
  [ "$n1" = 0 ] || f+="files left after many: $n1 "
  rm -f $d/big.zip
  fin 26 "$f" "many=$c1 $s1 files=$n1 | big=$c2 $s2 files=$n2 (500 status for breach = known policy item)"
}


############ 27 chunk length validation
r27() {
  local f="" d=$R/t27 cs=4096 size=$((3*4096+100))
  mkdir -p $d; rnd $W/full27 $size
  openS $T $d/f.bin $size $(sha $W/full27) $cs
  local tc; tc=$(echo "$BODY" | jq -r .totalChunks)
  [ "$CODE" = 201 ] || [ "$CODE" = 200 ] || f+="open=$CODE "
  head -c 100 $W/full27 > $W/s100; head -c 4097 $W/full27 > $W/l4097; head -c 4096 $W/full27 > $W/e4096
  putc $T $ID 0 $W/s100; local a=$CODE ac; ac=$(ecode)
  [ "$a" = 400 ] || f+="short=$a/$ac "
  putc $T $ID 0 $W/l4097; local b=$CODE bc; bc=$(ecode)
  [ "$b" = 413 ] || f+="long=$b/$bc "
  putc $T $ID 4 $W/s100; local c=$CODE cc; cc=$(ecode)
  [ "$c" = 400 ] || f+="idx=total:$c/$cc "
  putc $T $ID 3 $W/e4096; local e=$CODE ec; ec=$(ecode)
  case $e in 4*) ;; *) f+="last-too-long=$e ";; esac
  putc $T $ID 3 $W/s100; local g=$CODE; # last chunk 100 bytes is the right length? remainder = 100
  local rc; rc=$(received $T $ID)
  [ "$g" = 204 ] || f+="valid last chunk=$g "
  [ "$rc" = "[3]" ] || f+="received=$rc (expected [3]) "
  putc $T $ID -1 $W/s100; local h=$CODE
  case $h in 4*) ;; *) f+="idx -1=$h ";; esac
  fin 27 "$f" "totalChunks=$tc short=$a/$ac long=$b/$bc idx4=$c/$cc lastLong=$e/$ec idx-1=$h valid-last=$g received=$rc"
}


############ 28 chunk hash
r28() {
  local f="" d=$R/t28 cs=4096 size=8192
  mkdir -p $d; rnd $W/full28 $size
  openS $T $d/f.bin $size $(sha $W/full28) $cs
  local c0; c0=$(chunkfile $W/full28 0 $cs)
  putc $T $ID 0 $c0 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  local a=$CODE ac; ac=$(ecode)
  [ "$a" = 409 ] && [ "$ac" = CHUNK_HASH_MISMATCH ] || f+="bad hash=$a/$ac "
  local r1; r1=$(received $T $ID)
  [ "$r1" = "[]" ] || [ "$r1" = null ] || f+="stored after mismatch=$r1 "
  putc $T $ID 0 $c0; local b=$CODE
  [ "$b" = 204 ] || f+="good chunk=$b "
  local tp; tp=$(sq "select temp_path from transfers where id='$ID'")
  local h1 m1; h1=$(sha $tp); m1=$(stat -c %.9Y $tp)
  sleep 1.1
  putc $T $ID 0 $c0; local c=$CODE
  local h2 m2; h2=$(sha $tp); m2=$(stat -c %.9Y $tp)
  [ "$c" = 204 ] || f+="resend=$c "
  [ "$h1" = "$h2" ] || f+="temp bytes changed "
  [ "$m1" = "$m2" ] || f+="temp mtime changed ($m1 -> $m2: file rewritten) "
  local r2; r2=$(received $T $ID)
  fin 28 "$f" "bad=$a/$ac received-after-bad=$r1 good=$b resend=$c temp-sha-same=$([ "$h1" = "$h2" ] && echo y || echo n) mtime $m1/$m2 received=$r2"
}


############ 29 incomplete
r29() {
  local f="" d=$R/t29 cs=4096 size=12288
  mkdir -p $d; rnd $W/full29 $size
  openS $T $d/f.bin $size $(sha $W/full29) $cs
  local c0; c0=$(chunkfile $W/full29 0 $cs); putc $T $ID 0 $c0
  call $T POST /transfers/$ID/complete; local a=$CODE ac; ac=$(ecode)
  [ "$a" = 409 ] && [ "$ac" = UPLOAD_INCOMPLETE ] || f+="complete=$a/$ac "
  call $T GET /transfers/$ID; local st; st=$(echo "$BODY" | jq -r .status)
  [ "$st" = open ] || f+="status=$st "
  uploadall $T $ID $W/full29 $cs 3 >/dev/null || f+="upload rest failed "
  call $T POST /transfers/$ID/complete; local b=$CODE
  [ "$b" = 200 ] || f+="final complete=$b $(short 100) "
  [ "$(sha $d/f.bin 2>/dev/null)" = "$(sha $W/full29)" ] || f+="published bytes differ "
  fin 29 "$f" "incomplete=$a/$ac status=$st final=$b"
}


############ 30 whole-file hash
r30() {
  local f="" d=$R/t30 cs=4096 size=8192
  mkdir -p $d; rnd $W/x30 $size; rnd $W/y30 $size
  openS $T $d/bad.bin $size $(sha $W/x30) $cs
  uploadall $T $ID $W/y30 $cs 2 >/dev/null || f+="upload y failed "
  call $T POST /transfers/$ID/complete; local a=$CODE ac vf; ac=$(ecode); vf=$(echo "$BODY" | jq -r '.verified // "absent"')
  [ "$a" = 422 ] && [ "$ac" = HASH_MISMATCH ] || f+="mismatch complete=$a/$ac "
  [ "$vf" = true ] && f+="verified=true on failure "
  [ -e $d/bad.bin ] && f+="dest published "
  call $T GET /transfers/$ID; local st; st=$(echo "$BODY" | jq -r .status)
  [ "$st" = failed ] || f+="status=$st "
  openS $T $d/good.bin $size $(sha $W/x30) $cs
  uploadall $T $ID $W/x30 $cs 2 >/dev/null || f+="upload x failed "
  call $T POST /transfers/$ID/complete; local b=$CODE v2 s2; v2=$(echo "$BODY" | jq -r .verified); s2=$(echo "$BODY" | jq -r .sha256)
  [ "$b" = 200 ] && [ "$v2" = true ] && [ "$s2" = "$(sha $W/x30)" ] || f+="good complete=$b verified=$v2 sha=$s2 "
  fin 30 "$f" "mismatch=$a/$ac verified=$vf dest-absent=$([ -e $d/bad.bin ] && echo n || echo y) status=$st | good=$b verified=$v2 sha256-ok=$([ "$s2" = "$(sha $W/x30)" ] && echo y || echo n)"
}


############ 31 overwrite semantics
r31() {
  local f="" d=$R/t31 cs=4096 size=8192
  mkdir -p $d; rnd $W/x31 $size; rnd $W/y31 $size
  openS $T $d/a.bin $size $(sha $W/x31) $cs false
  uploadall $T $ID $W/x31 $cs 2 >/dev/null
  printf KEEP > $d/a.bin
  call $T POST /transfers/$ID/complete; local a=$CODE ac; ac=$(ecode)
  [ "$a" = 409 ] && [ "$ac" = CONFLICT ] || f+="appeared-file complete=$a/$ac "
  [ "$(cat $d/a.bin)" = KEEP ] || f+="a.bin changed "
  printf OLD > $d/b.bin
  openS $T $d/b.bin $size $(sha $W/x31) $cs true
  [ "$CODE" = 201 ] || [ "$CODE" = 200 ] || f+="ow open=$CODE "
  uploadall $T $ID $W/y31 $cs 2 >/dev/null
  call $T POST /transfers/$ID/complete; local b=$CODE bc; bc=$(ecode)
  [ "$b" = 422 ] || f+="ow bad-hash complete=$b/$bc "
  [ "$(cat $d/b.bin)" = OLD ] || f+="b.bin changed on failing hash "
  openS $T $d/b.bin $size $(sha $W/x31) $cs true
  uploadall $T $ID $W/x31 $cs 2 >/dev/null
  call $T POST /transfers/$ID/complete; local c=$CODE
  [ "$c" = 200 ] && [ "$(sha $d/b.bin)" = "$(sha $W/x31)" ] || f+="ow good complete=$c replaced=$([ "$(sha $d/b.bin)" = "$(sha $W/x31)" ] && echo y || echo n) "
  fin 31 "$f" "ow=false+appeared=$a/$ac keep=$(cat $d/a.bin) | ow=true bad hash=$b/$bc kept OLD | ow=true good=$c"
}


############ 32 complete re-checks
r32() {
  local f="" d=$R/t32 cs=4096 size=4096
  mkdir -p $d/j1 $d/other; rnd $W/x32 $size
  local sh; sh=$(sha $W/x32)
  # (a) jail
  openS $T2 $d/j1/a.bin $size $sh $cs; local oa=$CODE
  uploadall $T2 $ID $W/x32 $cs 1 >/dev/null
  sq "update devices set jail_root='$d/other' where id='$ID2'"
  call $T2 POST /transfers/$ID/complete; local a=$CODE ac; ac=$(ecode)
  sq "update devices set jail_root='' where id='$ID2'"
  case $a in 403) ;; *) f+="jail complete=$a/$ac ";; esac
  [ -e $d/j1/a.bin ] && f+="jail: file published "
  # (b) read-only
  openS $T2 $d/j1/b.bin $size $sh $cs
  uploadall $T2 $ID $W/x32 $cs 1 >/dev/null
  sq "update devices set read_only=1 where id='$ID2'"
  call $T2 POST /transfers/$ID/complete; local b=$CODE bc; bc=$(ecode)
  sq "update devices set read_only=0 where id='$ID2'"
  [ "$b" = 403 ] && [ "$bc" = READ_ONLY ] || f+="ro complete=$b/$bc "
  [ -e $d/j1/b.bin ] && f+="ro: file published "
  # (c) modify revoked with overwrite=true
  openS $T2 $d/j1/c.bin $size $sh $cs true
  uploadall $T2 $ID $W/x32 $cs 1 >/dev/null
  sq "update devices set can_modify=0 where id='$ID2'"
  call $T2 POST /transfers/$ID/complete; local c=$CODE cc; cc=$(ecode)
  sq "update devices set can_modify=1 where id='$ID2'"
  [ "$c" = 403 ] && [ "$cc" = CAPABILITY_DENIED ] || f+="modify complete=$c/$cc "
  [ -e $d/j1/c.bin ] && f+="modify: file published "
  fin 32 "$f" "jail=$a/$ac ro=$b/$bc modify=$c/$cc (opens $oa); none published"
}


############ 33 open validation
r33() {
  local f="" d=$R/t33 sh
  mkdir -p $d; sh=$(printf x | sha256sum | cut -d' ' -f1); printf x > $d/exists.bin
  local rows0; rows0=$(listrows)
  local out=""
  tc() { # label expected body
    jc $T POST /transfers "$3"; out+="$1=$CODE "
    [ "$CODE" = "$2" ] || f+="$1 got $CODE want $2 ($(short 80)) "
  }
  tc big64 413 "{\"path\":\"$d/a\",\"size\":68719476737,\"sha256\":\"$sh\",\"chunkSize\":4096}"
  tc negsize 400 "{\"path\":\"$d/a\",\"size\":-1,\"sha256\":\"$sh\",\"chunkSize\":4096}"
  tc chunk32M+1 400 "{\"path\":\"$d/a\",\"size\":10,\"sha256\":\"$sh\",\"chunkSize\":33554433}"
  tc chunk0 400 "{\"path\":\"$d/a\",\"size\":10,\"sha256\":\"$sh\",\"chunkSize\":0}"
  tc chunkneg 400 "{\"path\":\"$d/a\",\"size\":10,\"sha256\":\"$sh\",\"chunkSize\":-5}"
  tc nopath 400 "{\"size\":10,\"sha256\":\"$sh\",\"chunkSize\":4096}"
  tc nosha 400 "{\"path\":\"$d/a\",\"size\":10,\"chunkSize\":4096}"
  tc exists 409 "{\"path\":\"$d/exists.bin\",\"size\":1,\"sha256\":\"$sh\",\"chunkSize\":4096,\"overwrite\":false}"
  local rows1; rows1=$(listrows)
  [ "$rows0" = "$rows1" ] || f+="rows $rows0 -> $rows1 "
  fin 33 "$f" "$out rows $rows0->$rows1"
}


############ 35 session visibility
r35() {
  local f="" d=$R/t35 cs=4096 size=8192
  mkdir -p $d; rnd $W/x35 $size
  local sh; sh=$(sha $W/x35)
  openS $T2 $d/a.bin $size $sh $cs; local A=$ID
  local fake=00000000-0000-4000-8000-000000000000
  call $T3 GET /transfers/$fake; local ug=$BODY
  local cb; cb=$(chunkfile $W/x35 0 $cs)
  putc $T3 $fake 0 $cb; local up=$BODY
  call $T3 POST /transfers/$fake/complete; local uc=$BODY
  call $T3 DELETE /transfers/$fake; local ud=$BODY
  call $T3 GET /transfers/$A; local g=$CODE gb=$BODY
  putc $T3 $A 0 $cb; local p=$CODE pb=$BODY
  call $T3 POST /transfers/$A/complete; local c=$CODE cbd=$BODY
  call $T3 DELETE /transfers/$A; local dl=$CODE db=$BODY
  [ "$g $p $c $dl" = "404 404 404 404" ] || f+="B codes get/put/complete/del=$g $p $c $dl "
  [ "$gb" = "$ug" ] && [ "$pb" = "$up" ] && [ "$cbd" = "$uc" ] && [ "$db" = "$ud" ] || f+="B bodies differ from unknown-id bodies ($gb vs $ug) "
  call $T GET /transfers/$A; local ag=$CODE
  [ "$ag" = 200 ] || f+="admin get=$ag "
  call $T2 GET /transfers/$A; [ "$CODE" = 200 ] || f+="owner get=$CODE "
  # A completes, then deletes completed session -> file stays
  uploadall $T2 $A $W/x35 $cs 2 >/dev/null
  call $T2 POST /transfers/$A/complete; local cc=$CODE
  call $T2 DELETE /transfers/$A; local dc=$CODE
  [ "$(sha $d/a.bin 2>/dev/null)" = "$sh" ] || f+="published file gone after DELETE of completed (complete=$cc del=$dc) "
  # DELETE during active chunk upload
  local cs2=1048576
  rnd $W/x35b $cs2
  openS $T2 $d/b.bin $cs2 $(sha $W/x35b) $cs2; local SB=$ID
  local tp; tp=$(sq "select temp_path from transfers where id='$SB'")
  curl -sk -X PUT -H "Authorization: Bearer $T2" -H "X-Chunk-Sha256: $(sha $W/x35b)" -H 'Content-Type: application/octet-stream' --limit-rate 150k --data-binary @$W/x35b -o /dev/null -w '%{http_code}' "$B/transfers/$SB/chunks/0" > $W/slow.code 2>&1 &
  local sp=$!
  sleep 2
  call $T2 DELETE /transfers/$SB; local d1=$CODE d1c; d1c=$(ecode)
  [ "$d1" = 409 ] && [ "$d1c" = TRANSFER_ACTIVE ] || f+="delete during chunk=$d1/$d1c "
  [ -e "$tp" ] || f+="temp removed during active delete "
  wait $sp; local sc; sc=$(cat $W/slow.code)
  [ "$sc" = 204 ] || f+="slow chunk=$sc "
  call $T2 GET /transfers/$SB; [ "$CODE" = 200 ] || f+="session gone after refused delete=$CODE "
  call $T2 DELETE /transfers/$SB; local d2=$CODE
  [ -e "$tp" ] && f+="temp not removed by normal delete "
  [ -e $d/b.bin ] && f+="b.bin published "
  fin 35 "$f" "B get/put/complete/del=$g/$p/$c/$dl same-as-unknown=$([ "$gb" = "$ug" ] && echo y || echo n) admin-get=$ag completed-del=$dc file-kept=$([ -f $d/a.bin ] && echo y || echo n) active-del=$d1/$d1c slow=$sc normal-del=$d2 temp-gone=$([ -e "$tp" ] && echo n || echo y)"
}


############ 36 stale sweep + clock restart
r36() {
  local f="" d=$R/t36 cs=4096 size=4096
  mkdir -p $d; rnd $W/x36 $size; local sh; sh=$(sha $W/x36)
  local now; now=$(date +%s)
  openS $T2 $d/s1.bin $size $sh $cs; local S1=$ID
  openS $T2 $d/s2.bin $size $sh $cs; local S2=$ID
  local tp2; tp2=$(sq "select temp_path from transfers where id='$S2'")
  sq "update transfers set updated_at=$((now-7*86400+3600)) where id='$S1'"   # 6d23h old
  sq "update transfers set updated_at=$((now-7*86400-10)) where id='$S2'"      # 7d + 10s
  openS $T3 $d/s3.bin $size $sh $cs   # admission runs the same cleanup
  local r1 r2; r1=$(sq "select count(*) from transfers where id='$S1'"); r2=$(sq "select count(*) from transfers where id='$S2'")
  [ "$r1" = 1 ] || f+="row before boundary removed "
  [ "$r2" = 0 ] || f+="stale row kept "
  [ -e "$tp2" ] && f+="stale temp kept "
  # owner status restarts clock
  local u0; u0=$(sq "select updated_at from transfers where id='$S1'")
  call $T2 GET /transfers/$S1
  local u1; u1=$(sq "select updated_at from transfers where id='$S1'")
  [ $((u1-now)) -ge -5 ] || f+="status did not refresh clock ($u0->$u1) "
  # chunk activity restarts clock
  sq "update transfers set updated_at=$((now-7*86400+3600)) where id='$S1'"
  local c0; c0=$(chunkfile $W/x36 0 $cs); putc $T2 $S1 0 $c0
  local u2; u2=$(sq "select updated_at from transfers where id='$S1'")
  [ $((u2-now)) -ge -5 ] || f+="chunk did not refresh clock ($u2) "
  # non-owner (non-admin) status must not refresh
  sq "update transfers set updated_at=$((now-7*86400+3600)) where id='$S1'"
  call $T4 GET /transfers/$S1
  local u3; u3=$(sq "select updated_at from transfers where id='$S1'")
  [ "$u3" = $((now-7*86400+3600)) ] || note "36: non-owner status changed clock ($u3)"
  # sweep via restart too
  sq "update transfers set updated_at=$((now-7*86400-10)) where id='$S1'"
  local tp1; tp1=$(sq "select temp_path from transfers where id='$S1'")
  e2e_restart
  local r3; r3=$(sq "select count(*) from transfers where id='$S1'")
  [ "$r3" = 0 ] || f+="startup sweep kept stale row "
  [ -e "$tp1" ] && f+="startup sweep kept temp "
  fin 36 "$f" "6d23h kept=$r1 7d+10s removed=$([ "$r2" = 0 ] && echo y || echo n) (sweep triggered via admission+startup; hourly ticker not waited) status-refresh $u0->$u1 chunk-refresh->$u2 startup-sweep-removed=$([ "$r3" = 0 ] && echo y || echo n)"
}


############ 38 capability + owner roots
r38() {
  local f="" d=$R/t38
  mkdir -p $d; echo x > $d/f.txt
  jc $TB POST /fs/folder "{\"path\":\"$d/nofold\"}"; local a=$CODE ac; ac=$(ecode)
  [ "$a" = 403 ] && [ "$ac" = CAPABILITY_DENIED ] || f+="browse-only folder=$a/$ac "
  [ -e $d/nofold ] && f+="folder created by denied device "
  call $TB GET "/content?path=$d/f.txt"; local b=$CODE bc; bc=$(ecode)
  [ "$b" = 403 ] && [ "$bc" = CAPABILITY_DENIED ] || f+="browse-only download=$b/$bc "
  call $TB DELETE "/fs?path=$d/f.txt&permanent=true"; local c=$CODE cc; cc=$(ecode)
  case $c in 403) ;; 2*) [ -e $d/f.txt ] && f+="delete returned $c but file kept (batch item?) $(short 100) " || f+="denied device deleted file ";; esac
  [ -e $d/f.txt ] || f+="f.txt gone "
  sq "update devices set can_modify=0 where id='$IDO'"
  jc $T POST /fs/folder "{\"path\":\"$d/ownerfold\"}"; local e=$CODE
  jc $T POST /fs/folder "{\"path\":\"$O/outfold\"}"; local g=$CODE gc; gc=$(ecode)
  sq "update devices set can_modify=1 where id='$IDO'"
  [ "$e" = 201 ] && [ -d $d/ownerfold ] || f+="owner w/o modify in-root=$e "
  [ "$g" = 403 ] && [ "$gc" = FORBIDDEN ] || f+="owner outside root=$g/$gc "
  [ -e $O/outfold ] && f+="outside folder created "
  fin 38 "$f" "browse-only folder=$a/$ac download=$b/$bc delete=$c/$cc | owner(no modify) in-root=$e outside=$g/$gc"
}


############ 40 GET /content
r40() {
  local f="" d=$R/t40
  mkdir -p $d; seq 1 20000 > $d/n.txt   # compressible text
  local sz; sz=$(stat -c %s $d/n.txt)
  curl -sk -H "Authorization: Bearer $T" -H 'Accept-Encoding: gzip' -H 'Range: bytes=10-99' -D $W/h40 -o $W/r40 -w '%{http_code}' "$B/content?path=$d/n.txt" > $W/c40
  local a; a=$(cat $W/c40)
  [ "$a" = 206 ] || f+="range=$a "
  [ "$(cat $W/r40 | wc -c)" = 90 ] && cmp -s <(tail -c +11 $d/n.txt | head -c 90) $W/r40 || f+="range bytes wrong "
  rg -qi '^content-encoding: *gzip' $W/h40 && f+="gzip with Range "
  rg -qi '^content-range: bytes 10-99/' $W/h40 || f+="no/incorrect Content-Range: $(rg -i content-range $W/h40 | tr -d '\r') "
  curl -sk -H "Authorization: Bearer $T" -H "Range: bytes=$((sz+100))-$((sz+200))" -o /dev/null -w '%{http_code}' "$B/content?path=$d/n.txt" > $W/c40b
  local b; b=$(cat $W/c40b)
  [ "$b" = 416 ] || f+="unsat range=$b "
  curl -sk -H "Authorization: Bearer $T" -D $W/h40c -o $W/r40c -w '%{http_code}' "$B/content?path=$d/n.txt" > $W/c40c
  local c; c=$(cat $W/c40c)
  [ "$c" = 200 ] || f+="full=$c "
  rg -qi '^content-disposition: *attachment' $W/h40c || f+="no Content-Disposition attachment "
  rg -qi '^x-content-type-options: *nosniff' $W/h40c || f+="no nosniff "
  rg -qi '^content-security-policy:.*sandbox' $W/h40c || f+="no CSP sandbox "
  rg -qi '^cache-control:.*no-store' $W/h40c || f+="no Cache-Control no-store "
  curl -sk -H "Authorization: Bearer $T" -H 'Accept-Encoding: gzip' -D $W/h40d -o /dev/null -w '%{http_code}' "$B/content?path=$d/n.txt" > $W/c40d
  fin 40 "$f" "range=$a (90 bytes, content-range ok) unsat=$b full=$c headers: $(rg -i '^(content-disposition|x-content-type-options|content-security-policy|cache-control|content-encoding)' $W/h40c | tr -d '\r' | tr '\n' ';' | cut -c1-260) | gzip-no-range enc=$(rg -i '^content-encoding' $W/h40d | tr -d '\r')"
}


############ 41 PUT /content
r41() {
  local f="" d=$R/t41
  mkdir -p $d; printf orig > $d/f.txt; chmod 640 $d/f.txt
  head -c $((5*1024*1024+1)) /dev/zero > $W/big41
  call $T PUT "/content?path=$d/f.txt" -H 'Content-Type: application/octet-stream' --data-binary @$W/big41
  local a=$CODE ac; ac=$(ecode)
  [ "$a" = 413 ] && [ "$ac" = PAYLOAD_TOO_LARGE ] || f+="5MiB+1=$a/$ac "
  [ "$(cat $d/f.txt)" = orig ] || f+="file changed after 413 "
  printf new > $W/new41
  call $T PUT "/content?path=$d/f.txt&baseModified=2001-01-01T00:00:00Z" -H 'Content-Type: application/octet-stream' --data-binary @$W/new41
  local b=$CODE bc; bc=$(ecode)
  [ "$b" = 409 ] || f+="stale=$b/$bc "
  [ "$(cat $d/f.txt)" = orig ] || f+="file changed after 409 "
  local mt; mt=$(date -u -d @$(stat -c %Y $d/f.txt) +%Y-%m-%dT%H:%M:%SZ)
  local ino0; ino0=$(stat -c %i $d/f.txt)
  call $T PUT "/content?path=$d/f.txt&baseModified=$mt" -H 'Content-Type: application/octet-stream' --data-binary @$W/new41
  local c=$CODE
  [ "$c" = 200 ] || f+="good write=$c $(short 100) "
  [ "$(cat $d/f.txt)" = new ] || f+="content not replaced "
  [ "$(stat -c %a $d/f.txt)" = 640 ] || f+="mode now $(stat -c %a $d/f.txt) "
  ls -A $d | rg -q 'tmp' && f+="temp left: $(ls -A $d | tr '\n' ' ') "
  # exactly 5 MiB allowed
  head -c $((5*1024*1024)) /dev/zero > $W/ok41
  call $T PUT "/content?path=$d/f.txt" -H 'Content-Type: application/octet-stream' --data-binary @$W/ok41
  local e=$CODE
  [ "$e" = 200 ] || f+="exactly 5MiB=$e "
  fin 41 "$f" "5MiB+1=$a/$ac stale=$b/$bc matching=$c mode=$(stat -c %a $d/f.txt) inode $ino0->$(stat -c %i $d/f.txt) 5MiB=$e"
}


for n in 21 22 23 24 25 26 27 28 29 30 31 32 33 35 36 38 40 41; do e2e_case $n r$n; done
e2e_stop
