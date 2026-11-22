package api_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/apps"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/files"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/system"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/transfer"
)

// Geometry of the test upload: 2500 bytes in chunks of 1000 is three chunks,
// the last one 500 bytes long.
const (
	xferSize      = 2500
	xferChunkSize = 1000
	xferChunks    = 3
	xferBusyWait  = 30 * time.Millisecond
)

// xferEngine adapts a real transfer.Manager and a real file service to the
// api.Transfers interface, the way the composition root does.
type xferEngine struct {
	m   *transfer.Manager
	set *rootfs.Set
}

// pub is the caller's view of the disk.
func (e *xferEngine) pub(p authz.Principal, pol authz.Policy) transfer.Publisher {
	set := e.set
	if p.ReadOnly || pol.ReadOnly {
		set = set.ReadOnly()
	}
	return files.New(set)
}

// Open starts a session.
func (e *xferEngine) Open(p authz.Principal, pol authz.Policy, req api.TransferOpen) (*store.Upload, error) {
	return e.m.Open(p, e.pub(p, pol), transfer.OpenRequest{
		ID: req.ID, Path: req.Path, Size: req.Size, ChunkSize: req.ChunkSize, SHA256: req.SHA256, Overwrite: req.Overwrite,
	})
}

// Lookup returns the session row.
func (e *xferEngine) Lookup(p authz.Principal, id string) (*store.Upload, error) {
	return e.m.Lookup(p, id)
}

// Status returns the session with its chunks.
func (e *xferEngine) Status(p authz.Principal, id string) (*store.Upload, error) {
	return e.m.Status(p, id)
}

// Delete removes a session.
func (e *xferEngine) Delete(p authz.Principal, id string) error { return e.m.Delete(p, id) }

// WriteChunk stores a chunk.
func (e *xferEngine) WriteChunk(p authz.Principal, id string, n int, data []byte, sum string) error {
	return e.m.WriteChunk(p, id, n, data, sum)
}

// Complete finishes a session.
func (e *xferEngine) Complete(p authz.Principal, pol authz.Policy, id string) (any, error) {
	return e.m.Complete(p, e.pub(p, pol), id)
}

// xferRig is the HTTP stack with the transfer routes wired to a real engine.
type xferRig struct {
	*rig
	root  string // the one allowed folder
	dir   string // the engine's temp folder
	disk  *rootfs.Set
	tune  func(*api.Config)
	admin string
	mgrs  []*transfer.Manager
}

// newXferRig builds the stack; tune may change the server Config.
func newXferRig(t *testing.T, tune func(*api.Config)) *xferRig {
	t.Helper()
	x := &xferRig{rig: newRig(t, rigOptions{}), root: t.TempDir(), dir: t.TempDir(), tune: tune}
	set, err := rootfs.New([]string{x.root}, rootfs.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Close() })
	x.disk = set
	t.Cleanup(func() {
		for _, m := range x.mgrs {
			_ = m.Close()
		}
	})
	x.start()
	x.admin, _ = x.registerAdmin(newPhone("admin-1", 1))
	return x
}

// start builds a new engine over the same store and temp folder, and a new
// handler over it: what a restart of the agent does.
func (x *xferRig) start() {
	x.t.Helper()
	mgr, err := transfer.New(transfer.Config{Dir: x.dir, Store: x.db})
	if err != nil {
		x.t.Fatal(err)
	}
	x.mgrs = append(x.mgrs, mgr)
	sys, err := system.New(system.Config{
		Version: testVersion, APIVersion: testAPIVer, Started: startedAt, Fingerprint: testFingerprint,
		Port: "8765", Settings: x.rig.set, Disk: fakeDisk{}, Network: fakeNet{}, Counts: x.db, Host: fakeHost{},
	})
	if err != nil {
		x.t.Fatal(err)
	}
	cfg := api.Config{
		Pairing: x.svc, Devices: x.db, System: sys, Policy: x.rig.set, Logger: x.logs,
		Apps:      apps.New(apps.Config{Lister: fakeLister{}, Launcher: x.launcher}),
		Transfers: &xferEngine{m: mgr, set: x.disk}, Uploads: x.db,
	}
	if x.tune != nil {
		x.tune(&cfg)
	}
	h, err := api.New(cfg)
	if err != nil {
		x.t.Fatal(err)
	}
	x.rig.h = h
}

// restart drops the engine and handler and builds fresh ones.
func (x *xferRig) restart() {
	x.t.Helper()
	if err := x.mgrs[len(x.mgrs)-1].Close(); err != nil {
		x.t.Fatal(err)
	}
	x.start()
}

// uploader pairs a device that may upload (and optionally modify).
func (x *xferRig) uploader(id string, seed byte, modify bool) (token, deviceID string) {
	x.t.Helper()
	token, deviceID = x.pairDevice(newPhone(id, seed))
	g := authz.Grants{Browse: true, Upload: true, Modify: modify}
	if err := x.svc.SetFileGrants(pairing.PC, deviceID, g); err != nil {
		x.t.Fatal(err)
	}
	return token, deviceID
}

// xferPayload is deterministic file content of n bytes.
func xferPayload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + i/251)
	}
	return b
}

// xferSum is the lower-case hex SHA-256 of b.
func xferSum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// chunkOf returns chunk n of data.
func chunkOf(data []byte, n int) []byte {
	lo := n * xferChunkSize
	hi := min(lo+xferChunkSize, len(data))
	return data[lo:hi]
}

// target is a path inside the allowed folder.
func (x *xferRig) target(name string) string { return filepath.Join(x.root, name) }

// open opens a session for data at name and returns it.
func (x *xferRig) open(tok, name string, data []byte, overwrite bool) api.UploadSessionJSON {
	x.t.Helper()
	res := x.openRaw(tok, name, data, overwrite)
	if res.Code != http.StatusCreated {
		x.t.Fatalf("open = %d %s", res.Code, res.Body.String())
	}
	var s api.UploadSessionJSON
	res.json(x.t, &s)
	return s
}

// openRaw sends POST /transfers.
func (x *xferRig) openRaw(tok, name string, data []byte, overwrite bool) result {
	return x.do(http.MethodPost, "/transfers", tok, map[string]any{
		"path": x.target(name), "size": len(data), "sha256": xferSum(data), "chunkSize": xferChunkSize, "overwrite": overwrite,
	})
}

// put sends chunk n with its real checksum.
func (x *xferRig) put(tok, id string, data []byte, n int) result {
	c := chunkOf(data, n)
	return x.putBody(tok, id, fmt.Sprint(n), c, xferSum(c))
}

// putBody sends an arbitrary chunk body and checksum header.
func (x *xferRig) putBody(tok, id, n string, body []byte, sum string) result {
	var opts []opt
	if sum != "" {
		opts = append(opts, withHeader("X-Chunk-Sha256", sum))
	}
	return x.do(http.MethodPut, "/transfers/"+id+"/chunks/"+n, tok, string(body), opts...)
}

// session fetches GET /transfers/{id}.
func (x *xferRig) session(tok, id string) api.UploadSessionJSON {
	x.t.Helper()
	res := x.do(http.MethodGet, "/transfers/"+id, tok, nil)
	if res.Code != http.StatusOK {
		x.t.Fatalf("status = %d %s", res.Code, res.Body.String())
	}
	var s api.UploadSessionJSON
	res.json(x.t, &s)
	return s
}

// wantFile checks that the file at name has exactly want.
func (x *xferRig) wantFile(name string, want []byte) {
	x.t.Helper()
	got, err := os.ReadFile(x.target(name))
	if err != nil {
		x.t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		x.t.Fatalf("file %s differs from the upload (%d vs %d bytes)", name, len(got), len(want))
	}
}

// noHostText fails if the answer shows a folder of this machine.
func (x *xferRig) noHostText(res result) {
	x.t.Helper()
	for _, dir := range []string{x.root, x.dir} {
		if strings.Contains(res.Body.String(), dir) {
			x.t.Fatalf("answer shows a host folder: %s", res.Body.String())
		}
	}
}

func TestTransferCreateAndChunksOutOfOrder(t *testing.T) {
	x := newXferRig(t, nil)
	data := xferPayload(xferSize)
	s := x.open(x.admin, "a.bin", data, false)
	if s.ID == "" || s.Status != store.UploadOpen || s.Size != xferSize || s.ChunkSize != xferChunkSize || s.TotalChunks != xferChunks {
		t.Fatalf("session = %+v", s)
	}
	if s.ReceivedChunks == nil || len(s.ReceivedChunks) != 0 {
		t.Fatalf("receivedChunks = %#v, want an empty list", s.ReceivedChunks)
	}
	for _, n := range []int{2, 0, 1} {
		if res := x.put(x.admin, s.ID, data, n); res.Code != http.StatusNoContent {
			t.Fatalf("chunk %d = %d %s", n, res.Code, res.Body.String())
		}
	}
	if got := x.session(x.admin, s.ID); fmt.Sprint(got.ReceivedChunks) != "[0 1 2]" {
		t.Fatalf("receivedChunks = %v, want [0 1 2]", got.ReceivedChunks)
	}
	res := x.do(http.MethodPost, "/transfers/"+s.ID+"/complete", x.admin, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("complete = %d %s", res.Code, res.Body.String())
	}
	var done map[string]any
	res.json(t, &done)
	if done["sha256"] != xferSum(data) || done["verified"] != true || done["size"] != float64(xferSize) {
		t.Fatalf("complete answer = %v", done)
	}
	x.wantFile("a.bin", data)
	if got := x.session(x.admin, s.ID); got.Status != store.UploadCompleted {
		t.Fatalf("status after complete = %q", got.Status)
	}
}

func TestTransferResumesAfterTheHandlerRestarts(t *testing.T) {
	x := newXferRig(t, nil)
	data := xferPayload(xferSize)
	s := x.open(x.admin, "r.bin", data, false)
	if res := x.put(x.admin, s.ID, data, 1); res.Code != http.StatusNoContent {
		t.Fatalf("chunk 1 = %d", res.Code)
	}
	x.restart()
	if got := x.session(x.admin, s.ID); fmt.Sprint(got.ReceivedChunks) != "[1]" || got.Status != store.UploadOpen {
		t.Fatalf("after restart: %+v, want chunk 1 kept and open", got)
	}
	for _, n := range []int{0, 2} {
		if res := x.put(x.admin, s.ID, data, n); res.Code != http.StatusNoContent {
			t.Fatalf("chunk %d = %d %s", n, res.Code, res.Body.String())
		}
	}
	if res := x.do(http.MethodPost, "/transfers/"+s.ID+"/complete", x.admin, nil); res.Code != http.StatusOK {
		t.Fatalf("complete = %d %s", res.Code, res.Body.String())
	}
	x.wantFile("r.bin", data)
}

func TestTransferChunkChecksums(t *testing.T) {
	x := newXferRig(t, nil)
	data := xferPayload(xferSize)
	s := x.open(x.admin, "c.bin", data, false)
	wrong := xferSum([]byte("something else"))
	x.putBody(x.admin, s.ID, "0", chunkOf(data, 0), wrong).wantError(t, http.StatusConflict, api.CodeChunkHash)
	x.putBody(x.admin, s.ID, "0", chunkOf(data, 0), "").wantError(t, http.StatusBadRequest, api.CodeBadRequest)
	x.putBody(x.admin, s.ID, "0", chunkOf(data, 0), "not-hex").wantError(t, http.StatusBadRequest, api.CodeBadRequest)
	if got := x.session(x.admin, s.ID); len(got.ReceivedChunks) != 0 {
		t.Fatalf("a refused chunk was recorded: %v", got.ReceivedChunks)
	}
	// The hash header is accepted in upper case too.
	good := chunkOf(data, 0)
	if res := x.putBody(x.admin, s.ID, "0", good, strings.ToUpper(xferSum(good))); res.Code != http.StatusNoContent {
		t.Fatalf("upper-case checksum = %d %s", res.Code, res.Body.String())
	}
}

func TestTransferChunkIndexAndLength(t *testing.T) {
	x := newXferRig(t, nil)
	data := xferPayload(xferSize)
	s := x.open(x.admin, "i.bin", data, false)
	good := chunkOf(data, 0)
	for _, n := range []string{"abc", "-1", "3", "99999999999999999999"} {
		x.putBody(x.admin, s.ID, n, good, xferSum(good)).wantError(t, http.StatusBadRequest, api.CodeBadRequest)
	}
	short := good[:len(good)-1]
	x.putBody(x.admin, s.ID, "0", short, xferSum(short)).wantError(t, http.StatusBadRequest, api.CodeBadRequest)
}

func TestTransferOversizeChunkIsRefused(t *testing.T) {
	x := newXferRig(t, nil)
	data := xferPayload(xferSize)
	s := x.open(x.admin, "o.bin", data, false)
	big := append(append([]byte{}, chunkOf(data, 0)...), 'x')
	x.putBody(x.admin, s.ID, "0", big, xferSum(big)).wantError(t, http.StatusRequestEntityTooLarge, api.CodePayloadTooLarge)
	// The last chunk is shorter than the chunk size; a longer body is still too large.
	long := append(append([]byte{}, chunkOf(data, 2)...), make([]byte, 10)...)
	x.putBody(x.admin, s.ID, "2", long, xferSum(long)).wantError(t, http.StatusRequestEntityTooLarge, api.CodePayloadTooLarge)
	// A body far beyond the chunk size is cut off, not read.
	huge := make([]byte, 8*xferChunkSize)
	x.putBody(x.admin, s.ID, "1", huge, xferSum(huge)).wantError(t, http.StatusRequestEntityTooLarge, api.CodePayloadTooLarge)
	if got := x.session(x.admin, s.ID); len(got.ReceivedChunks) != 0 {
		t.Fatalf("an oversize chunk was recorded: %v", got.ReceivedChunks)
	}
}

func TestTransferChunkRouteHasItsOwnBodyLimit(t *testing.T) {
	// The global JSON body limit is below one chunk (900 < 1000 bytes): a chunk still passes,
	// while a JSON route stays limited by it.
	x := newXferRig(t, func(c *api.Config) { c.MaxBodyBytes = 900 })
	data := xferPayload(xferSize)
	s := x.open(x.admin, "l.bin", data, false)
	if res := x.put(x.admin, s.ID, data, 0); res.Code != http.StatusNoContent {
		t.Fatalf("a chunk larger than the global limit = %d %s", res.Code, res.Body.String())
	}
	x.do(http.MethodPost, "/transfers", x.admin, map[string]any{"path": strings.Repeat("a", 2000)}).
		wantError(t, http.StatusRequestEntityTooLarge, api.CodePayloadTooLarge)
}

func TestTransferCompleteBeforeAllChunks(t *testing.T) {
	x := newXferRig(t, nil)
	data := xferPayload(xferSize)
	s := x.open(x.admin, "p.bin", data, false)
	x.put(x.admin, s.ID, data, 0)
	res := x.do(http.MethodPost, "/transfers/"+s.ID+"/complete", x.admin, nil)
	res.wantError(t, http.StatusConflict, api.CodeUploadIncomplete)
	x.noHostText(res)
	if got := x.session(x.admin, s.ID); got.Status != store.UploadOpen || fmt.Sprint(got.ReceivedChunks) != "[0]" {
		t.Fatalf("session after a refused complete = %+v", got)
	}
	if _, err := os.Stat(x.target("p.bin")); err == nil {
		t.Fatal("a file was published from an incomplete upload")
	}
	// The session finishes normally afterwards.
	x.put(x.admin, s.ID, data, 1)
	x.put(x.admin, s.ID, data, 2)
	if res := x.do(http.MethodPost, "/transfers/"+s.ID+"/complete", x.admin, nil); res.Code != http.StatusOK {
		t.Fatalf("complete = %d %s", res.Code, res.Body.String())
	}
}

func TestTransferWholeFileHashMismatch(t *testing.T) {
	x := newXferRig(t, nil)
	data := xferPayload(xferSize)
	// Declare the hash of other content: every chunk is fine, the whole is not.
	res := x.do(http.MethodPost, "/transfers", x.admin, map[string]any{
		"path": x.target("w.bin"), "size": len(data), "sha256": xferSum([]byte("other")), "chunkSize": xferChunkSize,
	})
	var s api.UploadSessionJSON
	res.json(t, &s)
	for n := range xferChunks {
		x.put(x.admin, s.ID, data, n)
	}
	x.do(http.MethodPost, "/transfers/"+s.ID+"/complete", x.admin, nil).wantError(t, http.StatusUnprocessableEntity, api.CodeHashMismatch)
	if _, err := os.Stat(x.target("w.bin")); err == nil {
		t.Fatal("a file that failed verification was published")
	}
	// The session is failed, so no more chunks are taken.
	x.put(x.admin, s.ID, data, 0).wantError(t, http.StatusConflict, api.CodeTransferNotOpen)
}

func TestTransferDuplicateChunkIsIdempotent(t *testing.T) {
	x := newXferRig(t, nil)
	data := xferPayload(xferSize)
	s := x.open(x.admin, "d.bin", data, false)
	for range 3 {
		if res := x.put(x.admin, s.ID, data, 1); res.Code != http.StatusNoContent {
			t.Fatalf("repeated chunk = %d %s", res.Code, res.Body.String())
		}
	}
	if got := x.session(x.admin, s.ID); fmt.Sprint(got.ReceivedChunks) != "[1]" {
		t.Fatalf("receivedChunks = %v, want [1]", got.ReceivedChunks)
	}
	x.put(x.admin, s.ID, data, 0)
	x.put(x.admin, s.ID, data, 2)
	if res := x.do(http.MethodPost, "/transfers/"+s.ID+"/complete", x.admin, nil); res.Code != http.StatusOK {
		t.Fatalf("complete = %d %s", res.Code, res.Body.String())
	}
	x.wantFile("d.bin", data)
}

func TestTransferCancel(t *testing.T) {
	x := newXferRig(t, nil)
	data := xferPayload(xferSize)
	s := x.open(x.admin, "x.bin", data, false)
	x.put(x.admin, s.ID, data, 0)
	if res := x.do(http.MethodDelete, "/transfers/"+s.ID, x.admin, nil); res.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", res.Code, res.Body.String())
	}
	x.do(http.MethodGet, "/transfers/"+s.ID, x.admin, nil).wantError(t, http.StatusNotFound, api.CodeNotFound)
	x.do(http.MethodDelete, "/transfers/"+s.ID, x.admin, nil).wantError(t, http.StatusNotFound, api.CodeNotFound)
	x.put(x.admin, s.ID, data, 1).wantError(t, http.StatusNotFound, api.CodeNotFound)
	left, err := filepath.Glob(filepath.Join(x.dir, "*"+transfer.TempSuffix))
	if err != nil || len(left) != 0 {
		t.Fatalf("temp files left after cancel: %v %v", left, err)
	}
	if _, err := os.Stat(x.target("x.bin")); err == nil {
		t.Fatal("a cancelled upload published a file")
	}
}

func TestTransferListScopesAndFilters(t *testing.T) {
	x := newXferRig(t, nil)
	data := xferPayload(xferSize)
	phoneTok, phoneID := x.uploader("phone-1", 2, false)
	otherTok, otherID := x.uploader("phone-2", 3, false)
	adminSess := x.open(x.admin, "admin.bin", data, false)
	mine := x.open(phoneTok, "mine.bin", data, false)
	x.open(otherTok, "other.bin", data, false)
	x.put(phoneTok, mine.ID, data, 0)
	x.put(phoneTok, mine.ID, data, 1)
	for n := range xferChunks {
		x.put(x.admin, adminSess.ID, data, n)
	}
	x.do(http.MethodPost, "/transfers/"+adminSess.ID+"/complete", x.admin, nil)

	var all api.TransferListJSON
	x.do(http.MethodGet, "/transfers/list", x.admin, nil).json(t, &all)
	if len(all.Transfers) != 3 || all.Total != 3 || all.Counts[store.UploadOpen] != 2 || all.Counts[store.UploadCompleted] != 1 {
		t.Fatalf("admin list = %+v", all)
	}
	if len(all.Devices) != 3 || all.Users == nil {
		t.Fatalf("admin devices/users = %+v / %v", all.Devices, all.Users)
	}
	var row api.TransferSummaryJSON
	for _, r := range all.Transfers {
		if r.ID == mine.ID {
			row = r
		}
	}
	if row.Name != "mine.bin" || row.DeviceID != phoneID || row.ReceivedBytes != 2*xferChunkSize || row.Status != store.UploadOpen {
		t.Fatalf("row = %+v", row)
	}
	if row.Progress < 66 || row.Progress > 67 {
		t.Fatalf("progress = %v, want about 66.7", row.Progress)
	}

	var byDevice api.TransferListJSON
	x.do(http.MethodGet, "/transfers/list?device="+otherID, x.admin, nil).json(t, &byDevice)
	if len(byDevice.Transfers) != 1 || byDevice.Transfers[0].DeviceID != otherID || byDevice.Total != 3 {
		t.Fatalf("device filter = %+v", byDevice)
	}

	// A device sees only its own sessions; its filter parameters change nothing.
	var own api.TransferListJSON
	x.do(http.MethodGet, "/transfers/list?device="+otherID+"&user=owner", phoneTok, nil).json(t, &own)
	if len(own.Transfers) != 1 || own.Transfers[0].ID != mine.ID || own.Total != 1 || len(own.Devices) != 0 || len(own.Users) != 0 {
		t.Fatalf("device list = %+v", own)
	}
	if own.Counts[store.UploadOpen] != 1 {
		t.Fatalf("device counts = %v", own.Counts)
	}
}

func TestTransferListIsCapped(t *testing.T) {
	x := newXferRig(t, nil)
	for i := range api.MaxTransferListRows + 5 {
		u := store.Upload{
			ID: fmt.Sprintf("seed-%03d", i), DeviceID: "d", TargetPath: "/x/f", TotalSize: 1, ChunkSize: 1, TotalChunks: 1,
			SHA256: xferSum(nil), Status: store.UploadOpen, TempName: "seed.tmp", UpdatedAt: int64(1000 + i),
		}
		if err := x.db.InsertUpload(u); err != nil {
			t.Fatal(err)
		}
	}
	var l api.TransferListJSON
	x.do(http.MethodGet, "/transfers/list", x.admin, nil).json(t, &l)
	if len(l.Transfers) != api.MaxTransferListRows || l.Total != api.MaxTransferListRows+5 {
		t.Fatalf("rows = %d total = %d", len(l.Transfers), l.Total)
	}
	if l.Transfers[0].UpdatedAt != int64(1000+api.MaxTransferListRows+4) {
		t.Fatalf("newest row first expected, got updatedAt %d", l.Transfers[0].UpdatedAt)
	}
}

func TestTransferSessionsBelongToTheirDevice(t *testing.T) {
	x := newXferRig(t, nil)
	data := xferPayload(xferSize)
	ownerTok, _ := x.uploader("phone-1", 2, false)
	strangerTok, _ := x.uploader("phone-2", 3, false)
	s := x.open(ownerTok, "own.bin", data, false)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/transfers/" + s.ID},
		{http.MethodDelete, "/transfers/" + s.ID},
		{http.MethodPost, "/transfers/" + s.ID + "/complete"},
	} {
		x.do(c.method, c.path, strangerTok, nil).wantError(t, http.StatusNotFound, api.CodeNotFound)
	}
	x.put(strangerTok, s.ID, data, 0).wantError(t, http.StatusNotFound, api.CodeNotFound)
	// An unknown id gets the same answer.
	x.do(http.MethodGet, "/transfers/does-not-exist", strangerTok, nil).wantError(t, http.StatusNotFound, api.CodeNotFound)
	// An admin may use any session.
	if res := x.put(x.admin, s.ID, data, 0); res.Code != http.StatusNoContent {
		t.Fatalf("admin chunk = %d %s", res.Code, res.Body.String())
	}
	if got := x.session(ownerTok, s.ID); fmt.Sprint(got.ReceivedChunks) != "[0]" {
		t.Fatalf("owner sees %v", got.ReceivedChunks)
	}
}

func TestTransferOpenAuthorization(t *testing.T) {
	x := newXferRig(t, nil)
	data := xferPayload(xferSize)
	browseTok, _ := x.pairDevice(newPhone("browse-only", 4))
	x.openRaw(browseTok, "n.bin", data, false).wantError(t, http.StatusForbidden, authz.CodeCapabilityDenied)

	uploadTok, _ := x.uploader("phone-1", 2, false)
	modifyTok, _ := x.uploader("phone-3", 5, true)
	x.openRaw(uploadTok, "ow.bin", data, true).wantError(t, http.StatusForbidden, authz.CodeCapabilityDenied)
	if err := os.WriteFile(x.target("ow.bin"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	x.openRaw(uploadTok, "ow.bin", data, false).wantError(t, http.StatusConflict, api.CodeConflict)
	s := x.open(modifyTok, "ow.bin", data, true)
	for n := range xferChunks {
		x.put(modifyTok, s.ID, data, n)
	}
	if res := x.do(http.MethodPost, "/transfers/"+s.ID+"/complete", modifyTok, nil); res.Code != http.StatusOK {
		t.Fatalf("overwrite complete = %d %s", res.Code, res.Body.String())
	}
	x.wantFile("ow.bin", data)

	// A path outside the allowed folders is refused, and the answer names no folder.
	x.allowRoots(x.root)
	outside := filepath.Join(t.TempDir(), "elsewhere.bin")
	res := x.do(http.MethodPost, "/transfers", uploadTok, map[string]any{
		"path": outside, "size": 1, "sha256": xferSum([]byte("a")), "chunkSize": 1,
	})
	res.wantError(t, http.StatusForbidden, authz.CodeForbidden)
	if strings.Contains(res.Body.String(), filepath.Dir(outside)) {
		t.Fatalf("answer shows the folder: %s", res.Body.String())
	}

	// Read-only mode refuses every mutating transfer route.
	x.rig.set.setReadOnly(true)
	x.openRaw(uploadTok, "ro.bin", data, false).wantError(t, http.StatusForbidden, authz.CodeReadOnly)
	x.put(uploadTok, s.ID, data, 0).wantError(t, http.StatusForbidden, authz.CodeReadOnly)
	x.do(http.MethodPost, "/transfers/"+s.ID+"/complete", uploadTok, nil).wantError(t, http.StatusForbidden, authz.CodeReadOnly)
}

// allowRoots restricts the agent to the given folders.
func (x *xferRig) allowRoots(roots ...string) {
	x.rig.set.mu.Lock()
	defer x.rig.set.mu.Unlock()
	x.rig.set.s.Roots = roots
}

func TestTransferOpenValidation(t *testing.T) {
	x := newXferRig(t, nil)
	good := map[string]any{"path": x.target("v.bin"), "size": 1, "sha256": xferSum([]byte("a")), "chunkSize": 1}
	with := func(k string, v any) map[string]any {
		m := map[string]any{}
		for key, val := range good {
			m[key] = val
		}
		if v == nil {
			delete(m, k)
		} else {
			m[k] = v
		}
		return m
	}
	cases := []struct {
		name   string
		body   any
		status int
		code   string
	}{
		{"no path", with("path", nil), http.StatusBadRequest, api.CodeBadRequest},
		{"no sha256", with("sha256", nil), http.StatusBadRequest, api.CodeBadRequest},
		{"no chunk size", with("chunkSize", nil), http.StatusBadRequest, api.CodeBadRequest},
		{"negative size", with("size", -1), http.StatusBadRequest, api.CodeBadRequest},
		{"chunk size over the cap", with("chunkSize", transfer.MaxChunkSize+1), http.StatusBadRequest, api.CodeBadRequest},
		{"size over the cap", with("size", transfer.MaxTransferSize+1), http.StatusRequestEntityTooLarge, api.CodePayloadTooLarge},
		{"not JSON", "{nope", http.StatusBadRequest, api.CodeBadRequest},
		{"size is not a number", with("size", "big"), http.StatusBadRequest, api.CodeBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := x.do(http.MethodPost, "/transfers", x.admin, c.body)
			res.wantError(t, c.status, c.code)
			x.noHostText(res)
		})
	}
}

func TestTransferQuotaIsAnAnswerNotACrash(t *testing.T) {
	x := newXferRig(t, nil)
	data := xferPayload(xferSize)
	tok, _ := x.uploader("phone-1", 2, false)
	for i := range transfer.MaxActivePerDevice {
		x.open(tok, fmt.Sprintf("q%d.bin", i), data, false)
	}
	x.openRaw(tok, "q-extra.bin", data, false).wantError(t, http.StatusTooManyRequests, api.CodeResourceLimit)
}

// busyRequest builds a chunk upload whose body the test controls.
func (x *xferRig) busyRequest(tok, id string, n int, body io.Reader, sum string) *http.Request {
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/v1/transfers/%s/chunks/%d", id, n), body)
	req.RemoteAddr = "10.8.0.1:4000"
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Chunk-Sha256", sum)
	return req
}

func TestTransferChunkBudgetAnswersChunkBusy(t *testing.T) {
	// The budget holds exactly one chunk; a second chunk that cannot get room in
	// time is told to retry.
	x := newXferRig(t, func(c *api.Config) { c.ChunkBufferBytes = xferChunkSize; c.ChunkWait = xferBusyWait })
	data := xferPayload(xferSize)
	s := x.open(x.admin, "b.bin", data, false)

	pr, pw := io.Pipe()
	first := chunkOf(data, 0)
	rec := httptest.NewRecorder()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		x.rig.h.ServeHTTP(rec, x.busyRequest(x.admin, s.ID, 0, pr, xferSum(first)))
	}()
	// A pipe write returns once the handler has read it, so the first chunk now
	// holds the whole budget.
	if _, err := pw.Write(first[:10]); err != nil {
		t.Fatal(err)
	}
	second := chunkOf(data, 1)
	busy := x.putBody(x.admin, s.ID, "1", second, xferSum(second))
	busy.wantError(t, http.StatusTooManyRequests, api.CodeChunkBusy)
	if got := busy.Header().Get("Retry-After"); got != "2" {
		t.Fatalf("Retry-After = %q, want 2", got)
	}
	if _, err := pw.Write(first[10:]); err != nil {
		t.Fatal(err)
	}
	_ = pw.Close()
	wg.Wait()
	if rec.Code != http.StatusNoContent {
		t.Fatalf("first chunk = %d %s", rec.Code, rec.Body.String())
	}
	// With the budget free again the retry goes through.
	if res := x.putBody(x.admin, s.ID, "1", second, xferSum(second)); res.Code != http.StatusNoContent {
		t.Fatalf("retried chunk = %d %s", res.Code, res.Body.String())
	}
}

func TestTransferChunksInParallelWithinTheBudget(t *testing.T) {
	x := newXferRig(t, nil)
	data := xferPayload(xferSize)
	s := x.open(x.admin, "par.bin", data, false)
	var wg sync.WaitGroup
	codes := make([]int, xferChunks)
	for n := range xferChunks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := chunkOf(data, n)
			req := x.busyRequest(x.admin, s.ID, n, bytes.NewReader(c), xferSum(c))
			rec := httptest.NewRecorder()
			x.rig.h.ServeHTTP(rec, req)
			codes[n] = rec.Code
		}()
	}
	wg.Wait()
	for n, c := range codes {
		if c != http.StatusNoContent {
			t.Fatalf("chunk %d = %d", n, c)
		}
	}
	if res := x.do(http.MethodPost, "/transfers/"+s.ID+"/complete", x.admin, nil); res.Code != http.StatusOK {
		t.Fatalf("complete = %d %s", res.Code, res.Body.String())
	}
	x.wantFile("par.bin", data)
}

func TestTransferRoutesAnswer501WithoutAnEngine(t *testing.T) {
	r := newRig(t, rigOptions{})
	tok, _ := r.registerAdmin(newPhone("admin-1", 1))
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/transfers"},
		{http.MethodGet, "/transfers/list"},
		{http.MethodGet, "/transfers/x"},
		{http.MethodDelete, "/transfers/x"},
		{http.MethodPut, "/transfers/x/chunks/0"},
		{http.MethodPost, "/transfers/x/complete"},
	} {
		r.do(c.method, c.path, tok, nil).wantError(t, http.StatusNotImplemented, api.CodeNotImplemented)
	}
}

func TestTransferRoutesNeedAToken(t *testing.T) {
	x := newXferRig(t, nil)
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/transfers"},
		{http.MethodGet, "/transfers/list"},
		{http.MethodGet, "/transfers/x"},
		{http.MethodDelete, "/transfers/x"},
		{http.MethodPut, "/transfers/x/chunks/0"},
		{http.MethodPost, "/transfers/x/complete"},
	} {
		x.do(c.method, c.path, "", nil).wantError(t, http.StatusUnauthorized, authz.CodeUnauthorized)
	}
}
