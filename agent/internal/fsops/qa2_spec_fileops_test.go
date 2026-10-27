package fsops

// Spec tests (QA2) for file operations: delete, rename, move, copy, device jail,
// global read-only and the error-code enum. Expectations come from the
// requirements (FR-*, QR-*, UC-*); the code is only used to reach them.

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// qa2specLoadEnum reads the code enum of the Error schema from the committed
// contract, ../../../protocol/openapi.yaml relative to this package dir. It fails
// the test when the file cannot be read or no enum is found under the Error schema.
func qa2specLoadEnum(t *testing.T) map[string]bool {
	t.Helper()
	const path = "../../../protocol/openapi.yaml"
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read contract %s: %v", path, err)
	}
	item := regexp.MustCompile(`^\s+- ([A-Z][A-Z_]*)\s*$`)
	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		if line != "    Error:" {
			continue
		}
		enum := map[string]bool{}
		inCode, inEnum := false, false
		for _, l := range lines[i+1:] {
			if strings.TrimSpace(l) != "" && !strings.HasPrefix(l, "      ") {
				break // left the Error schema block
			}
			switch {
			case l == "        code:":
				inCode = true
			case inCode && l == "          enum:":
				inEnum = true
			case inEnum:
				m := item.FindStringSubmatch(l)
				if m == nil {
					inEnum = false
					break
				}
				enum[m[1]] = true
			}
		}
		if len(enum) > 0 {
			return enum
		}
	}
	t.Fatalf("no code enum under the Error schema in %s", path)
	return nil
}

// qa2specWrite creates the parent directories of path and writes content to it.
func qa2specWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// qa2specRead returns the content of path.
func qa2specRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// qa2specExists reports whether path exists, without following a final symlink.
func qa2specExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// qa2specCode returns the error code of a batch result, or "" when it has none.
func qa2specCode(r BatchResult) string {
	if r.Error == nil {
		return ""
	}
	return r.Error.Code
}

// qa2specDirContents maps each entry name in dir to that file's content.
func qa2specDirContents(t *testing.T, dir string) map[string]string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir %s: %v", dir, err)
	}
	out := make(map[string]string, len(ents))
	for _, e := range ents {
		out[e.Name()] = qa2specRead(t, filepath.Join(dir, e.Name()))
	}
	return out
}

// qa2specHasOther reports whether m holds content under a name other than skip.
func qa2specHasOther(m map[string]string, skip, content string) bool {
	for name, got := range m {
		if name != skip && got == content {
			return true
		}
	}
	return false
}

// TestQA2SpecFR5_DeleteOutsideJailForbidden checks that a device jail refuses
// a delete outside it and still removes a path inside it.
func TestQA2SpecFR5_DeleteOutsideJailForbidden(t *testing.T) {
	// FR-5: "Given a path outside the device's jail, when DELETE /fs?path=<that path>, then the result carries code FORBIDDEN and the path is unchanged."
	// Boundary: a path inside the device's jail is removed. The grant check is server-side and not tested here.
	// catches: the device jail does not restrict Delete, or a refused entry is removed anyway.
	root := t.TempDir()
	dev := filepath.Join(root, "dev")
	inside := filepath.Join(dev, "inside.txt")
	outside := filepath.Join(root, "other", "outside.txt")
	qa2specWrite(t, inside, "in")
	qa2specWrite(t, outside, "out")
	ops := New([]string{root}, false).Jailed(dev)

	res := ops.Delete([]string{outside})
	if len(res) != 1 || res[0].OK || qa2specCode(res[0]) != "FORBIDDEN" {
		t.Errorf("Delete outside device jail: %+v, want one result with code FORBIDDEN", res)
	}
	if got := qa2specRead(t, outside); got != "out" {
		t.Errorf("outside file changed: %q, want %q", got, "out")
	}

	res = ops.Delete([]string{inside})
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("Delete inside device jail: %+v, want one ok result", res)
	}
	if qa2specExists(inside) {
		t.Errorf("inside file %s still present after delete", inside)
	}

	// Global roots: a path outside every configured root is refused the same way.
	global := New([]string{root}, false)
	foreign := filepath.Join(t.TempDir(), "foreign.txt")
	qa2specWrite(t, foreign, "keep")
	res = global.Delete([]string{foreign})
	if len(res) != 1 || qa2specCode(res[0]) != "FORBIDDEN" {
		t.Errorf("Delete outside global roots: %+v, want code FORBIDDEN", res)
	}
	if got := qa2specRead(t, foreign); got != "keep" {
		t.Errorf("foreign file changed: %q", got)
	}
}

// TestQA2SpecFR10_RenameReturnsRenamedEntry checks an in-place rename.
func TestQA2SpecFR10_RenameReturnsRenamedEntry(t *testing.T) {
	// FR-10: "Given file D/a.txt, when PATCH /fs/rename with src=D/a.txt and dst=D/b.txt, then D/a.txt is absent, D/b.txt holds the same bytes, and the response is 200 with the Entry for D/b.txt."
	// catches: the source is left in place, the bytes change, or the returned Entry names another path.
	ops, root := setupJail(t)
	src := filepath.Join(root, "D", "a.txt")
	dst := filepath.Join(root, "D", "b.txt")
	qa2specWrite(t, src, "payload")

	e, err := ops.Rename(src, dst)
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if e == nil || e.Path != dst || e.Name != "b.txt" {
		t.Errorf("Rename returned %+v, want the Entry for %s", e, dst)
	}
	if qa2specExists(src) {
		t.Errorf("source %s still present after rename", src)
	}
	if got := qa2specRead(t, dst); got != "payload" {
		t.Errorf("destination bytes = %q, want %q", got, "payload")
	}
}

// TestQA2SpecFR11_RenameRefusesExistingDifferentEntry checks the conflict case.
func TestQA2SpecFR11_RenameRefusesExistingDifferentEntry(t *testing.T) {
	// FR-11: "Given files D/a.txt and D/b.txt with different bytes, when PATCH /fs/rename with src=D/a.txt and dst=D/b.txt, then the response is 409 CONFLICT and both files keep their bytes."
	// The 409 mapping is server-side; the package returns ErrConflict.
	// catches: the existing destination is replaced, or the source is moved anyway.
	ops, root := setupJail(t)
	a := filepath.Join(root, "D", "a.txt")
	b := filepath.Join(root, "D", "b.txt")
	qa2specWrite(t, a, "AAA")
	qa2specWrite(t, b, "BBB")

	if _, err := ops.Rename(a, b); !errors.Is(err, ErrConflict) {
		t.Errorf("Rename onto existing entry: err = %v, want ErrConflict", err)
	}
	if got := qa2specRead(t, a); got != "AAA" {
		t.Errorf("source bytes = %q, want %q", got, "AAA")
	}
	if got := qa2specRead(t, b); got != "BBB" {
		t.Errorf("destination bytes = %q, want %q", got, "BBB")
	}
}

// TestQA2SpecUCRename_MissingSourceNotFound checks the UC-RENAME alternate flow.
func TestQA2SpecUCRename_MissingSourceNotFound(t *testing.T) {
	// UC-RENAME alt: "src missing -> 404 PATH_NOT_FOUND". The package returns ErrNotFound; the 404 mapping is server-side.
	// catches: a missing source is reported as success or creates the destination.
	ops, root := setupJail(t)
	src := filepath.Join(root, "missing.txt")
	dst := filepath.Join(root, "new.txt")

	if _, err := ops.Rename(src, dst); !errors.Is(err, ErrNotFound) {
		t.Errorf("Rename of missing source: err = %v, want ErrNotFound", err)
	}
	if qa2specExists(dst) {
		t.Errorf("destination %s created for a missing source", dst)
	}
}

// TestQA2SpecFR12_MoveRelocatesSource checks that a move removes the source and places it in destDir.
func TestQA2SpecFR12_MoveRelocatesSource(t *testing.T) {
	// FR-12: "Given file D1/a.txt and folder D2 without a.txt, when POST /fs/move with sources=[D1/a.txt] and destDir=D2, then D1/a.txt is absent, D2/a.txt holds the bytes, and the BatchResult for the source has ok=true."
	// catches: the source is copied and left in place, or it lands outside destDir.
	ops, root := setupJail(t)
	src := filepath.Join(root, "D1", "a.txt")
	dir := filepath.Join(root, "D2")
	qa2specWrite(t, src, "moved bytes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	res := ops.Move([]string{src}, dir, false, false)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("Move: %+v, want one ok result", res)
	}
	if qa2specExists(src) {
		t.Errorf("source %s still present after move", src)
	}
	if got := qa2specRead(t, filepath.Join(dir, "a.txt")); got != "moved bytes" {
		t.Errorf("D2/a.txt = %q, want %q", got, "moved bytes")
	}
}

// TestQA2SpecFR14_MoveDuplicateAutoRenamesAndKeepsExisting checks the duplicate collision rule on move.
func TestQA2SpecFR14_MoveDuplicateAutoRenamesAndKeepsExisting(t *testing.T) {
	// FR-14: "Given D2/a.txt exists, when POST /fs/move with sources=[D1/a.txt], destDir=D2 and duplicate=true, then D2/a.txt is unchanged and the moved item has a new name in D2."
	// catches: the existing D2/a.txt is overwritten, or the moved item keeps the colliding name.
	ops, root := setupJail(t)
	src := filepath.Join(root, "D1", "a.txt")
	dir := filepath.Join(root, "D2")
	qa2specWrite(t, src, "payload")
	qa2specWrite(t, filepath.Join(dir, "a.txt"), "old")

	res := ops.Move([]string{src}, dir, true, false)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("Move duplicate=true: %+v, want one ok result", res)
	}
	if qa2specExists(src) {
		t.Errorf("source %s still present after move", src)
	}
	contents := qa2specDirContents(t, dir)
	if contents["a.txt"] != "old" {
		t.Errorf("existing D2/a.txt = %q, want %q", contents["a.txt"], "old")
	}
	if len(contents) != 2 || !qa2specHasOther(contents, "a.txt", "payload") {
		t.Errorf("D2 = %v, want the existing a.txt plus one new entry holding %q", contents, "payload")
	}
}

// TestQA2SpecFR14_CopyDuplicateAutoRenamesAndKeepsExisting checks the duplicate collision rule on copy.
func TestQA2SpecFR14_CopyDuplicateAutoRenamesAndKeepsExisting(t *testing.T) {
	// FR-14: "Move and copy must apply the collision rule per source: with duplicate=true the item must be auto-renamed and the existing destination entry left unchanged."
	// catches: copy overwrites the existing destination, or the source is removed.
	ops, root := setupJail(t)
	src := filepath.Join(root, "D1", "a.txt")
	dir := filepath.Join(root, "D2")
	qa2specWrite(t, src, "payload")
	qa2specWrite(t, filepath.Join(dir, "a.txt"), "old")

	res := ops.Copy([]string{src}, dir, true, false)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("Copy duplicate=true: %+v, want one ok result", res)
	}
	if got := qa2specRead(t, src); got != "payload" {
		t.Errorf("source after copy = %q, want %q (copy keeps the source)", got, "payload")
	}
	contents := qa2specDirContents(t, dir)
	if contents["a.txt"] != "old" {
		t.Errorf("existing D2/a.txt = %q, want %q", contents["a.txt"], "old")
	}
	if len(contents) != 2 || !qa2specHasOther(contents, "a.txt", "payload") {
		t.Errorf("D2 = %v, want the existing a.txt plus one new entry holding %q", contents, "payload")
	}
}

// TestQA2SpecFR14_MoveConflictLeavesSource checks the default collision rule on move.
func TestQA2SpecFR14_MoveConflictLeavesSource(t *testing.T) {
	// FR-14: "Given D2/a.txt exists, when POST /fs/move with sources=[D1/a.txt], destDir=D2, duplicate=false and overwrite=false, then the BatchResult for that source has ok=false and error code CONFLICT, and D1/a.txt is unchanged."
	// catches: the colliding source is moved or replaces the destination; the result is ok=true.
	ops, root := setupJail(t)
	src := filepath.Join(root, "D1", "a.txt")
	dir := filepath.Join(root, "D2")
	qa2specWrite(t, src, "payload")
	qa2specWrite(t, filepath.Join(dir, "a.txt"), "old")

	res := ops.Move([]string{src}, dir, false, false)
	if len(res) != 1 || res[0].OK || qa2specCode(res[0]) != "CONFLICT" {
		t.Errorf("Move without duplicate or overwrite: %+v, want ok=false with code CONFLICT", res)
	}
	if got := qa2specRead(t, src); got != "payload" {
		t.Errorf("source = %q, want unchanged %q", got, "payload")
	}
	if got := qa2specRead(t, filepath.Join(dir, "a.txt")); got != "old" {
		t.Errorf("destination = %q, want unchanged %q", got, "old")
	}
}

// TestQA2SpecFR14_CopyConflictLeavesSource checks the default collision rule on copy.
func TestQA2SpecFR14_CopyConflictLeavesSource(t *testing.T) {
	// FR-14: "Move and copy must apply the collision rule per source: ... with duplicate=false and overwrite=false a colliding source must fail with a CONFLICT entry and stay in place."
	// catches: copy replaces the destination, or reports ok=true.
	ops, root := setupJail(t)
	src := filepath.Join(root, "D1", "a.txt")
	dir := filepath.Join(root, "D2")
	qa2specWrite(t, src, "payload")
	qa2specWrite(t, filepath.Join(dir, "a.txt"), "old")

	res := ops.Copy([]string{src}, dir, false, false)
	if len(res) != 1 || res[0].OK || qa2specCode(res[0]) != "CONFLICT" {
		t.Errorf("Copy without duplicate or overwrite: %+v, want ok=false with code CONFLICT", res)
	}
	if got := qa2specRead(t, src); got != "payload" {
		t.Errorf("source = %q, want unchanged %q", got, "payload")
	}
	if got := qa2specRead(t, filepath.Join(dir, "a.txt")); got != "old" {
		t.Errorf("destination = %q, want unchanged %q", got, "old")
	}
}

// TestQA2SpecFR14_DuplicateWinsOverOverwrite checks the precedence stated in the contract text cited by FR-14.
func TestQA2SpecFR14_DuplicateWinsOverOverwrite(t *testing.T) {
	// FR-14 (Source: openapi-HEAD.yaml:2357-2366, TransferOpRequest): "if duplicate=true, auto-rename wins and overwrite is ignored."
	// catches: overwrite=true replaces the existing destination even though duplicate=true.
	ops, root := setupJail(t)
	src := filepath.Join(root, "D1", "a.txt")
	dir := filepath.Join(root, "D2")
	qa2specWrite(t, src, "payload")
	qa2specWrite(t, filepath.Join(dir, "a.txt"), "old")

	res := ops.Move([]string{src}, dir, true, true)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("Move duplicate=true overwrite=true: %+v, want one ok result", res)
	}
	contents := qa2specDirContents(t, dir)
	if contents["a.txt"] != "old" {
		t.Errorf("existing D2/a.txt = %q, want %q (overwrite must be ignored)", contents["a.txt"], "old")
	}
	if len(contents) != 2 || !qa2specHasOther(contents, "a.txt", "payload") {
		t.Errorf("D2 = %v, want the existing a.txt plus one new entry holding %q", contents, "payload")
	}
}

// TestQA2SpecFR15_MoveOverwriteReplacesDestination checks overwrite=true on move.
func TestQA2SpecFR15_MoveOverwriteReplacesDestination(t *testing.T) {
	// FR-15: "Given D2/a.txt exists, when POST /fs/move with sources=[D1/a.txt], destDir=D2, overwrite=true and duplicate=false, then D2/a.txt holds the source's bytes, D1/a.txt is absent, and the BatchResult has ok=true."
	// What happens to the replaced entry is open question 4 and is not asserted.
	// catches: the destination keeps its old bytes, or the source stays behind.
	ops, root := setupJail(t)
	src := filepath.Join(root, "D1", "a.txt")
	dir := filepath.Join(root, "D2")
	qa2specWrite(t, src, "payload")
	qa2specWrite(t, filepath.Join(dir, "a.txt"), "old")

	res := ops.Move([]string{src}, dir, false, true)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("Move overwrite=true: %+v, want one ok result", res)
	}
	if got := qa2specRead(t, filepath.Join(dir, "a.txt")); got != "payload" {
		t.Errorf("D2/a.txt = %q, want the source bytes %q", got, "payload")
	}
	if qa2specExists(src) {
		t.Errorf("source %s still present after move", src)
	}
}

// TestQA2SpecFR15_CopyOverwriteReplacesDestination checks overwrite=true on copy.
func TestQA2SpecFR15_CopyOverwriteReplacesDestination(t *testing.T) {
	// FR-15 (copy form, Source: openapi-HEAD.yaml:2362-2365): "with overwrite=true and duplicate=false ... replace an existing destination entry with the source."
	// catches: copy keeps the old destination bytes, or removes the source.
	ops, root := setupJail(t)
	src := filepath.Join(root, "D1", "a.txt")
	dir := filepath.Join(root, "D2")
	qa2specWrite(t, src, "payload")
	qa2specWrite(t, filepath.Join(dir, "a.txt"), "old")

	res := ops.Copy([]string{src}, dir, false, true)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("Copy overwrite=true: %+v, want one ok result", res)
	}
	if got := qa2specRead(t, filepath.Join(dir, "a.txt")); got != "payload" {
		t.Errorf("D2/a.txt = %q, want the source bytes %q", got, "payload")
	}
	if got := qa2specRead(t, src); got != "payload" {
		t.Errorf("source after copy = %q, want %q", got, "payload")
	}
}

// TestQA2SpecQR4_ReadOnlyAgentRefusesMutations checks read-only refusals at package level.
func TestQA2SpecQR4_ReadOnlyAgentRefusesMutations(t *testing.T) {
	// QR-4: "Given a read-only device, when ... PATCH /fs/rename is sent, then the response is 403 with code READ_ONLY." Package form: global read-only.
	// The per-item form on DELETE is open question 18, so DELETE asserts only that the item is refused and kept.
	// The PUT /transfers chunk route is server-side and not tested here.
	// catches: a mutating operation runs on a read-only agent.
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "b.txt")
	dir := filepath.Join(root, "D2")
	qa2specWrite(t, src, "keep")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ro := New([]string{root}, true)

	if _, err := ro.Rename(src, dst); !errors.Is(err, ErrReadOnly) {
		t.Errorf("Rename on read-only agent: err = %v, want ErrReadOnly", err)
	}
	if !qa2specExists(src) || qa2specExists(dst) {
		t.Errorf("rename on read-only agent changed the tree")
	}

	res := ro.Move([]string{src}, dir, false, false)
	if len(res) != 1 || qa2specCode(res[0]) != "READ_ONLY" {
		t.Errorf("Move on read-only agent: %+v, want code READ_ONLY", res)
	}
	res = ro.Copy([]string{src}, dir, false, false)
	if len(res) != 1 || qa2specCode(res[0]) != "READ_ONLY" {
		t.Errorf("Copy on read-only agent: %+v, want code READ_ONLY", res)
	}
	res = ro.Delete([]string{src})
	if len(res) != 1 || res[0].OK {
		t.Errorf("Delete on read-only agent: %+v, want ok=false", res)
	}
	if got := qa2specRead(t, src); got != "keep" {
		t.Errorf("source after refused operations = %q, want %q", got, "keep")
	}
}

// TestQA2SpecFR35_GlobalPolicyStillAppliesToOwner checks the global policy that FR-35 keeps in force.
func TestQA2SpecFR35_GlobalPolicyStillAppliesToOwner(t *testing.T) {
	// FR-35: "given the same device and a path outside the jail, then the result is FORBIDDEN" and "global read-only, configured roots and the jail still apply."
	// The grant bypass for owner devices is server-side and not tested here.
	// catches: a read-only agent deletes an in-root file, or a path outside the roots is deleted.
	root := t.TempDir()
	inside := filepath.Join(root, "in.txt")
	qa2specWrite(t, inside, "keep")
	outside := filepath.Join(t.TempDir(), "out.txt")
	qa2specWrite(t, outside, "keep")

	ro := New([]string{root}, true)
	res := ro.Delete([]string{inside})
	if len(res) != 1 || res[0].OK || !qa2specExists(inside) {
		t.Errorf("Delete inside root on read-only agent: %+v, want refused and file kept", res)
	}

	rw := New([]string{root}, false)
	res = rw.Delete([]string{outside})
	if len(res) != 1 || qa2specCode(res[0]) != "FORBIDDEN" {
		t.Errorf("Delete outside roots: %+v, want code FORBIDDEN", res)
	}
	if got := qa2specRead(t, outside); got != "keep" {
		t.Errorf("outside file changed: %q", got)
	}
}

// TestQA2SpecQR5_EmittedCodesAreInContractEnum scans this package's sources for emitted error codes.
func TestQA2SpecQR5_EmittedCodesAreInContractEnum(t *testing.T) {
	// QR-5: "The error code field must take a value listed in the contract's code enum for every code the agent returns on file and transfer routes."
	// Method: the codes are literals passed to apiErr(...) or set as Code: "...". The package sources are read (structure check, not a runtime check),
	// and only this package's non-test files. The server handlers are not scanned here.
	// catches: a code that is not in the enum, such as MOVE_FAILED, COPY_FAILED, TRASH_FAILED or RESTORE_FAILED.
	re := regexp.MustCompile(`(?:apiErr\(|Code:\s*)"([A-Z][A-Z_]*)"`)
	enum := qa2specLoadEnum(t)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	seen := 0
	reported := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			seen++
			key := f + ": " + m[1]
			if !enum[m[1]] && !reported[key] {
				reported[key] = true
				t.Errorf("code emitted but not in the contract enum: %s", key)
			}
		}
	}
	if seen == 0 {
		t.Fatal("the scan found no emitted codes; the check is not running")
	}
}
