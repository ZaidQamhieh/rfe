# RFE live E2E case manifest

Canonical list of the 41 live E2E cases run by `tools/e2e/run.sh`. Case n is requirement n of the 2026-10-26 bug-hunt requirements list (pass 1);
the case ID is `REQ-n`. One case per requirement, so the counts are the same: 41 cases, of which case 1 is a meta requirement (always N-A).

Requirements 19 and 23 were changed at the 2026-10-26 closure (owner decision); the old text is kept under each.

Statuses: PASS, FAIL (with a class: product, harness, fixture, env, spec), N-A (with a reason), NOT RUN.

## Symlink and entry semantics

### REQ-1
The fs methods (Delete, MoveToTrash, RestoreFromTrash, Rename, Copy, Move, Chmod, Compress, Extract, Meta, ListDir, Open) and the transfer routes in this area must behave as the requirements in this list state, with path traversal, symlink escape, filesystem boundaries, destructive operations, transfer integrity and permission checks covered first.

Accept: Open question 1 decides what counts as expected behaviour beyond the stated sources; until it is answered, each item is pass/fail against its own cited source only.

### REQ-2
DELETE /fs must act on a symbolic link itself, permanently or into trash, leaving its target unchanged, including when the target lies outside the configured roots.

Accept: link L -> T, with T inside and then outside the roots. DELETE /fs?path=L&permanent=true and the trash variant each return results[0].ok=true, Lstat(L) fails, and T keeps its bytes and mode. Fail if T is removed or changed, or if the call fails because T is outside the roots.

### REQ-3
PATCH /fs/rename must rename a symbolic link itself, and its 200 response must describe the link (isSymlink true, symlinkTarget set to the original target string).

Accept: L -> T renamed to L2. Response has isSymlink=true and symlinkTarget=T, Readlink(L2)=T, T unchanged.

### REQ-4
POST /fs/move must move a symbolic link among the sources as the link itself, leave its target where it is, and accept a destDir that is itself a link to a folder inside the roots.

Accept: sources=[L] moved into D gives a link with the same target string under D; T unchanged. Repeat with destDir a link to a folder inside the roots; the move succeeds.

### REQ-5
POST /fs/copy must copy a symbolic-link source as the content of its target under the link's own name, and must recreate links inside a copied folder as links with the same target string, not dereferenced.

Accept: (a) L -> file with bytes B; copy to D gives D/L as a regular file with bytes B. (b) Folder F containing X -> T; copy F to D gives a symlink at D/F/X with Readlink == T, and no bytes of T written under D/F.

### REQ-6
A symbolic link whose target lies outside the configured roots must not yield the target's bytes through copy or through GET /content.

Accept: L (inside) -> O/secret. POST /fs/copy with source L returns a per-item error and writes no secret bytes under destDir. GET /content?path=L returns 403 FORBIDDEN and no bytes.

## Jail and traversal

### REQ-7
A relative path passed to any fs route must be refused with 403 FORBIDDEN and must leave the filesystem unchanged.

Accept: GET /fs?path=relative/dir and POST /fs/folder with path=relative/x each return 403 FORBIDDEN, and nothing is created relative to the process working directory.

### REQ-8
A path whose cleaned or symlink-resolved location lies outside the configured roots or the device's jail must be refused with 403 FORBIDDEN, and nothing at that location may be read, created, modified, renamed or deleted.

Accept: root R, outside O with O/x. GET /fs?path=O, PUT /content?path=O/x, DELETE of O/x, and POST /fs/compress with dest O/a.zip each return 403, and O/x is unchanged.

### REQ-9
Dot-dot components must be removed before the jail decision: a path whose cleaned form stays inside a root is accepted, and one whose cleaned form leaves the root is refused.

Accept: R/sub/../plain.txt is accepted. R/sub/../../O/x returns 403 and O/x is untouched.

### REQ-10
A symbolic link in an intermediate component must not be followed when its target lies outside the root: a request naming R/link/x must be refused and must not create or read anything under the link target.

Accept: R/link -> O. POST /fs/folder with path=R/link/new returns 403, O/new is not created, and GET /fs?path=R/link/... returns 403.

### REQ-11
Each Ops method must refuse paths at or under the agent's protected data directory, whatever the configured roots are.

Accept: after ProtectDir(D), ListDir, Meta, Open and Delete on D or D/file, and Copy into or out of D, all return ErrForbidden (403). A path beside D still works.

### REQ-12
Opening, downloading or checksumming a FIFO, socket or device must return NOT_A_FILE (422) without reading its content.

Accept: mkfifo R/p; GET /content?path=R/p returns 422 NOT_A_FILE and does not block. (Current code returns 500; see Conflicts.)

## Destructive operations

### REQ-13
DELETE /fs without permanent=true must move each path to the trash, and permanent=true must remove the path irreversibly and recursively, including directory contents.

Accept: default delete makes GET /trash list the item with its original path. permanent=true on a folder with children leaves neither the folder nor the children, and no trash entry.

### REQ-14
Copy or move with overwrite=true and duplicate=false must replace an existing destination. With overwrite=false and duplicate=false, an existing destination must yield a per-item CONFLICT (ok=false, error.code=CONFLICT) and leave the destination unchanged.

Accept: destination F exists. overwrite=false returns ok=false with CONFLICT, and F's bytes are unchanged. overwrite=true leaves F holding the source's bytes.

### REQ-15
duplicate=true must create a non-colliding name on collision and must never replace an existing destination, even when overwrite=true is also sent.

Accept: F exists. Copy with duplicate=true and overwrite=true creates "F (1)", and F is unchanged.

### REQ-16
A failed overwrite must restore the original destination, and an overwrite must never delete a destination that contains the source.

Accept: (a) Inject a failure in the copy during overwrite (via the test seams in agent/internal/fsops/trash.go:387-392, or a fixture that forces the failure). The original destination's bytes are intact afterwards. (b) Move folder D into D/sub with overwrite=true returns an error, and D is intact.

### REQ-17
Rename must never replace an existing file or folder, must return 409 CONFLICT, and must leave both entries unchanged. Renaming a folder into its own subtree must return 409 CONFLICT with nothing created.

Accept: rename onto an existing file returns 409 CONFLICT, and both files are intact. Rename of D to D/sub/x returns 409 and creates no D/sub.

### REQ-18
Restore from trash must never replace an occupied original path: it must use a keep-both name, and the BatchResult path must be the name actually used.

Accept: occupy the original path, then POST /trash/restore. The result path is "name (1)" with the trashed content, and the original is intact.

### REQ-19
Trash ids must be single opaque names. POST /trash/restore is a batch: for a well-formed request it answers 200 with one result per id, and an id containing a separator, a volume prefix (as the host OS defines it), or equal to "." or ".." gets ok=false with code BAD_REQUEST and its own id as path, never a success; the valid ids in the same batch are still processed. A malformed request (body not JSON, ids missing or empty) is a request-level 400. DELETE /trash refuses any invalid id with a request-level 400 and deletes nothing. Neither route may read or change any path outside the trash store.

Accept: POST /trash/restore with ids ["../x"], [".."], ["."], ["a/b"], ["/abs"]: 200, results[0] ok=false, error.code BAD_REQUEST. A mixed batch [valid, "../x"] restores the valid id and reports the invalid one as BAD_REQUEST. {} and {"ids":[]} return 400. DELETE /trash with each invalid id returns 400. Files outside the store, and the store's own entries, are unchanged.

Old (pass 1): an invalid id to /trash/restore and to DELETE /trash returns 400.

### REQ-20
DELETE /trash must empty the trash for a request with no body or with a body that names ids. A body that is present but malformed, is {}, is null, or has an empty ids list must return 400 and delete nothing. On a jailed device, listing and emptying must cover only items whose recorded original path lies inside its jail.

Accept: body {} returns 400 and removes nothing. With device jail J, GET /trash lists only items whose original path is in J, and DELETE /trash without ids removes only those.

## Archive

### REQ-21
Compress must never overwrite an existing file: an existing dest must become a "(n)" sibling, and the 201 Entry must report the actual path.

Accept: dest.zip exists. Compress creates "dest (1).zip", dest.zip's bytes are unchanged, and Entry.path is the actual path.

### REQ-22
Compress must check each source and the destination against the jail before writing any byte, must write the archive to a temp file and rename it into place only on success, and must leave neither a partial archive nor a temp file after any failure.

Accept: one source outside the roots returns 403, and no dest is created. An unreadable source yields an error, and neither dest.zip nor a ".dest.zip.rfe-tmp-*" file remains.

### REQ-23
Extract must not write any byte outside destDir. An archive entry whose name is absolute in any form (leading "/" or "\", UNC, a drive prefix such as "C:\x", "C:/x" or "C:x", that is any name starting with one letter and ":"), or whose relative name climbs out of destDir, must be refused with 403 FORBIDDEN; the whole extraction fails and rolls back the files and folders it created, and content already in destDir is unchanged. Relative names that stay inside destDir still extract.

Accept: A zip with "../evil.txt", and zips with "/abs", "C:\x" and "\\srv\share\x" entries (each after a valid entry), return 403 FORBIDDEN; no file appears outside destDir or under destDir (the valid entry is rolled back), and a pre-existing file in destDir keeps its bytes. A zip with "./a" and "x/../b" extracts both.

Old (pass 1): an absolute entry name must produce no file outside destDir and a non-2xx response; abort or skip left open.

### REQ-24
Extract must not write through a symbolic link that already exists in destDir or in an entry's parent path, and must not create symlink, hard-link or device entries from an archive.

Accept: destDir/l -> O. A zip entry "l/x" is refused and O/x is absent. A zip or tar with a symlink entry creates no link.

### REQ-25
Extract must never replace an existing file. A name already in use must receive a "(n)" sibling, and the original bytes must stay intact.

Accept: f.txt exists with bytes A. Extracting entry f.txt with bytes B creates "f (1).txt" with B, and f.txt still holds A.

### REQ-26
Extract must refuse an archive with more than 100,000 entries or more than 2 GiB of expanded bytes, and must not leave a partially written file for the entry that exceeded the budget.

Accept: an archive with 100,001 entries is refused. An archive that expands past 2 GiB is refused. No truncated file remains for the over-limit entry.

## Transfers

### REQ-27
A chunk body must have exactly the length its index requires (chunkSize for each chunk except the last, the remainder for the last). A shorter or longer body must be refused and not stored. An index outside 0..totalChunks-1 must be refused.

Accept: a short non-last chunk returns 400 BAD_REQUEST and n is absent from receivedChunks. A body of chunkSize+1 bytes returns 413 PAYLOAD_TOO_LARGE. n = totalChunks returns 400.

### REQ-28
A chunk whose X-Chunk-Sha256 does not match its body must be refused with 409 CHUNK_HASH_MISMATCH and not stored. A resend of an already received chunk with a matching hash must return 204 and must not rewrite the stored bytes.

Accept: a wrong hash returns 409, and n is absent from receivedChunks. Resending a matching chunk returns 204, and the file bytes are unchanged.

### REQ-29
POST /transfers/{id}/complete must return 409 UPLOAD_INCOMPLETE while any chunk is missing, and the session must stay open for the missing chunks.

Accept: complete after 1 of 3 chunks returns 409 UPLOAD_INCOMPLETE. Status shows status "open". After the missing chunks are uploaded, complete returns 200.

### REQ-30
Complete must verify the whole-file SHA-256 against the sha256 supplied at session open before anything is published. On a mismatch it must return 422 HASH_MISMATCH and publish nothing. Only a successful complete may return verified=true.

Accept: upload bytes that differ from the declared sha256 while each chunk hash matches. Complete returns 422, the destination is absent, and status is "failed" (behaviour from agent/internal/transfer/transfer.go:666; not in the contract). A successful complete returns the Entry fields plus sha256 and verified=true.

### REQ-31
Complete with overwrite=false must never replace a file that appeared during the upload, and must return 409 CONFLICT leaving that file untouched. Complete with overwrite=true must replace the destination only after the hash and permission checks pass.

Accept: a target created during upload makes complete return 409 CONFLICT, with the target's bytes unchanged. With overwrite=true and a failing hash, the target is unchanged.

### REQ-32
Complete must re-check the target against the calling device's current jail, read-only state, and (for overwrite=true) modify grant. A failed check must publish nothing.

Accept: open a session, then change the device's jail, read-only flag or modify grant, then complete. The response is 403 (FORBIDDEN, READ_ONLY or CAPABILITY_DENIED), and no file exists at the target.

### REQ-33
POST /transfers must refuse with the stated status: 413 PAYLOAD_TOO_LARGE for a declared size above 64 GiB, 400 BAD_REQUEST for a negative size, a chunkSize above 32 MiB, a chunkSize of 0 or less, or a missing path or sha256, and 409 CONFLICT for an existing destination when overwrite=false. A refused request must create no session.

Accept: each case returns its stated status, and GET /transfers shows no new row.

### REQ-34
Session admission must return 429 RESOURCE_LIMIT when a device exceeds 4 open sessions or 64 GiB reserved, or when the host exceeds 16 sessions or 128 GiB reserved.

Accept: a 5th open session from one device returns 429. Two devices whose reservations together pass 128 GiB return 429 for the request that crosses the limit.

### REQ-35
A transfer session must be visible and mutable only to the device that opened it or to an admin device. Any other device must receive 404 NOT_FOUND identical to an unknown id on GET, chunk PUT, complete and DELETE. DELETE must remove the temp file but never a file already published at the destination, and must return 409 TRANSFER_ACTIVE and remove nothing while an operation holds the session.

Accept: device B uses A's session id on all four routes and gets the same 404 body as an unknown id. A's DELETE of a completed session leaves the published file. DELETE during an active chunk upload returns 409 and removes nothing.

### REQ-36
An open session with no chunk or owner status activity for 7 days must be removed by the sweep, and an owner status or resume request must restart that 7-day clock.

Accept: call the cleanup routine with a clock 7 days and 1 second ahead (agent/internal/transfer/transfer.go:345-349). The row and temp file are removed. A status call before the boundary keeps the row.

## Permissions and content

### REQ-37
An upload session with overwrite=true must require the modify grant when opened, for each chunk, and at completion. Removing modify between requests must make the next such request 403 CAPABILITY_DENIED.

Accept: revoke modify after open. The next chunk PUT returns 403 CAPABILITY_DENIED, and complete also returns 403.

### REQ-38
A paired device lacking the capability a route needs must receive 403 CAPABILITY_DENIED, and the route must change nothing. An owner device must bypass per-device file grants but remain subject to the configured roots, global and per-device read-only, and its jail.

Accept: a device without modify gets 403 CAPABILITY_DENIED from POST /fs/folder and no folder is created. An owner device without modify creates a folder inside the root and gets 403 for a path outside it.

### REQ-39
Global or per-device read-only must refuse every filesystem write and transfer write with no change on disk. Single-item calls return 403 READ_ONLY, and batch calls return per-item READ_ONLY results.

Accept: on a read-only device, rename returns 403 READ_ONLY. Copy, delete and trash return 200 or 202 with per-item READ_ONLY and no change. POST /transfers returns 403 READ_ONLY.

### REQ-40
GET /content must serve file bytes with HTTP Range semantics (206 for satisfiable ranges, 416 for unsatisfiable). A request that carries a Range header must never receive Content-Encoding gzip. Responses must carry Content-Disposition attachment, X-Content-Type-Options nosniff, a sandbox Content-Security-Policy and Cache-Control no-store.

Accept: a Range request returns 206 with the correct bytes and no Content-Encoding. An out-of-range request returns 416. A 200 response carries the four headers.

### REQ-41
PUT /content must refuse bodies over 5 MiB with 413 PAYLOAD_TOO_LARGE, must replace the file atomically while keeping its mode, and must refuse with 409 when baseModified does not equal the file's mtime truncated to the second. Both refusals leave the file unchanged.

Accept: a 5 MiB + 1 byte body returns 413 with the file unchanged. A mismatched baseModified returns 409 with the file unchanged. A successful write keeps the file's mode.
