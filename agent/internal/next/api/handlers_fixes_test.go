package api_test

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/pairing"
)

// Fixed values of the bandwidth, archive and revoked-device tests.
const (
	// throttleRate paces a one-thousand-byte body over half a second.
	throttleRate = 2000
	// throttleFloor is a little under that half second, so a loaded machine
	// cannot make a correct limiter look too fast.
	throttleFloor = 450 * time.Millisecond
	// unlimitedCeiling is far above what an unthrottled in-memory request takes
	// and below the paced time.
	unlimitedCeiling = throttleFloor

	throttledBody = 1000

	tarName       = "mixed.tar"
	tarNoMtime    = "no-mtime.txt"
	tarWithMtime  = "has-mtime.txt"
	tarModifiedAt = "2021-03-04T05:06:07Z"
	jsonModified  = "modified"
)

// setBandwidth stores the live upload and download limits (0 = unlimited).
func (f *fakeSettings) setBandwidth(up, down int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.s.MaxUploadBytesPerSec, f.s.MaxDownloadBytesPerSec = up, down
}

func TestContentDownloadFollowsTheLiveBandwidthSetting(t *testing.T) {
	c := newContentRig(t, nil)
	data := bytes.Repeat([]byte("x"), throttledBody)
	p := c.put("big.bin", data)
	fetch := func() (time.Duration, result) {
		start := time.Now()
		res := c.do(http.MethodGet, "/content"+q("path", p), c.admin, nil)
		return time.Since(start), res
	}

	// Unlimited: fast.
	if d, res := fetch(); res.Code != http.StatusOK || d >= unlimitedCeiling {
		t.Fatalf("unlimited download = %d in %v", res.Code, d)
	}
	// An upload limit alone must not slow a download.
	c.set.setBandwidth(throttleRate, 0)
	if d, res := fetch(); res.Code != http.StatusOK || d >= unlimitedCeiling {
		t.Fatalf("download with only an upload limit = %d in %v", res.Code, d)
	}
	// The setting is read on the next request, no restart.
	c.set.setBandwidth(0, throttleRate)
	d, res := fetch()
	if res.Code != http.StatusOK || !bytes.Equal(res.Body.Bytes(), data) {
		t.Fatalf("limited download = %d, %d bytes", res.Code, res.Body.Len())
	}
	if d < throttleFloor {
		t.Fatalf("a %d-byte body at %d B/s took %v, want at least %v", throttledBody, throttleRate, d, throttleFloor)
	}
	// Lifting the limit takes effect again.
	c.set.setBandwidth(0, 0)
	if d, res := fetch(); res.Code != http.StatusOK || d >= unlimitedCeiling {
		t.Fatalf("download after lifting the limit = %d in %v", res.Code, d)
	}
}

func TestContentRangeStillWorksWhenThrottled(t *testing.T) {
	c := newContentRig(t, nil)
	p := c.put("note.txt", []byte(contentText))
	c.set.setBandwidth(0, 1<<20)
	res := c.do(http.MethodGet, "/content"+q("path", p), c.admin, nil, withHeader(rangeHeader, rangeRequest))
	if res.Code != http.StatusPartialContent || res.Body.String() != rangeBody || res.Header().Get(headerRange) != rangeResponse {
		t.Fatalf("throttled range = %d %q %q", res.Code, res.Body.String(), res.Header().Get(headerRange))
	}
}

func TestChunkUploadFollowsTheLiveBandwidthSetting(t *testing.T) {
	x := newXferRig(t, nil)
	data := xferPayload(xferSize)
	s := x.open(x.admin, "paced.bin", data, false)
	send := func(n int) (time.Duration, result) {
		start := time.Now()
		res := x.put(x.admin, s.ID, data, n)
		return time.Since(start), res
	}

	// A download limit alone must not slow an upload.
	x.rig.set.setBandwidth(0, throttleRate)
	if d, res := send(0); res.Code != http.StatusNoContent || d >= unlimitedCeiling {
		t.Fatalf("upload with only a download limit = %d in %v", res.Code, d)
	}
	x.rig.set.setBandwidth(throttleRate, 0)
	d, res := send(1)
	if res.Code != http.StatusNoContent {
		t.Fatalf("limited chunk = %d %s", res.Code, res.Body.String())
	}
	if d < throttleFloor {
		t.Fatalf("a %d-byte chunk at %d B/s took %v, want at least %v", xferChunkSize, throttleRate, d, throttleFloor)
	}
	// The paced bytes still arrive intact: the file completes with its hash.
	x.rig.set.setBandwidth(0, 0)
	if res := x.put(x.admin, s.ID, data, 2); res.Code != http.StatusNoContent {
		t.Fatalf("last chunk = %d %s", res.Code, res.Body.String())
	}
	if res := x.do(http.MethodPost, "/transfers/"+s.ID+"/complete", x.admin, nil); res.Code != http.StatusOK {
		t.Fatalf("complete = %d %s", res.Code, res.Body.String())
	}
	x.wantFile("paced.bin", data)
}

func TestArchiveEntryWithoutAModifiedTimeOmitsIt(t *testing.T) {
	c := newContentRig(t, nil)
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	mtime, err := time.Parse(time.RFC3339, tarModifiedAt)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []struct {
		name string
		mod  time.Time
	}{{tarNoMtime, time.Time{}}, {tarWithMtime, mtime}} {
		if err := tw.WriteHeader(&tar.Header{Name: e.name, Mode: 0o644, Size: 0, ModTime: e.mod, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	p := c.put(tarName, buf.Bytes())
	res := c.do(http.MethodGet, "/fs/archive"+q("path", p), c.admin, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("archive = %d %s", res.Code, res.Body.String())
	}
	var body struct {
		Entries []map[string]json.RawMessage `json:"entries"`
	}
	res.json(t, &body)
	if len(body.Entries) != 2 {
		t.Fatalf("entries = %s", res.Body.String())
	}
	for _, e := range body.Entries {
		var name string
		if err := json.Unmarshal(e["path"], &name); err != nil {
			t.Fatal(err)
		}
		_, has := e[jsonModified]
		switch name {
		case tarNoMtime:
			if has {
				t.Errorf("an entry with no time carries modified = %s", e[jsonModified])
			}
		case tarWithMtime:
			var got time.Time
			if !has || json.Unmarshal(e[jsonModified], &got) != nil || !got.Equal(mtime) {
				t.Errorf("an entry with a time lost it: %s", e[jsonModified])
			}
		default:
			t.Errorf("unexpected entry %q", name)
		}
		for _, k := range []string{"path", "size", "isDir"} {
			if _, ok := e[k]; !ok {
				t.Errorf("entry %q lacks %q: wire shape changed", name, k)
			}
		}
	}
}

func TestPatchOfARevokedDeviceIsRefusedAndChangesNothing(t *testing.T) {
	r := newRig(t, rigOptions{})
	adminTok, _ := r.registerAdmin(newPhone("admin-1", 1))
	_, id := r.pairDevice(newPhone("phone-1", 2))
	if err := r.svc.Revoke(pairing.PC, id); err != nil {
		t.Fatal(err)
	}
	before, err := r.db.DeviceByID(id)
	if err != nil || before == nil || !before.Revoked {
		t.Fatalf("setup: %+v %v", before, err)
	}
	for name, body := range map[string]map[string]any{
		"readOnly": {"readOnly": true},
		"files":    {"download": true, "modify": true},
		"apps":     {"viewApps": true},
		"all":      {"readOnly": true, "download": true, "viewApps": true, "launchApps": true},
		"empty":    {},
	} {
		res := r.do(http.MethodPatch, "/devices/"+id, adminTok, body)
		res.wantError(t, http.StatusConflict, api.CodeConflict)
		after, err := r.db.DeviceByID(id)
		if err != nil || after == nil || *after != *before {
			t.Fatalf("%s: a refused patch changed the device: before %+v after %+v (%v)", name, before, after, err)
		}
	}
	// An unknown device is still 404, not 409.
	r.do(http.MethodPatch, "/devices/nope", adminTok, map[string]any{"readOnly": true}).wantError(t, http.StatusNotFound, api.CodeNotFound)
}
