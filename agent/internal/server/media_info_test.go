package server

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// mediaInfoGet calls the handler for one path and returns the recorder.
func mediaInfoGet(ops *fsops.Ops, path string) *httptest.ResponseRecorder {
	target := "/v1/fs/media-info"
	if path != "" {
		target += "?path=" + url.QueryEscape(path)
	}
	rr := httptest.NewRecorder()
	mediaInfoHandler(ops)(rr, httptest.NewRequest(http.MethodGet, target, nil))
	return rr
}

// writeTestJPEG writes a small w x h JPEG with an EXIF capture date.
func writeTestJPEG(t *testing.T, path string, w, h int) {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	date := []byte("2022:07:08 09:10:11\x00")
	var tiff []byte
	tiff = append(tiff, 'I', 'I', 42, 0, 8, 0, 0, 0) // header, IFD0 at 8
	tiff = append(tiff, 1, 0)                        // one IFD0 entry
	tiff = append(tiff, 0x69, 0x87, 4, 0, 1, 0, 0, 0, 26, 0, 0, 0)
	tiff = append(tiff, 0, 0, 0, 0) // next IFD
	tiff = append(tiff, 1, 0)       // ExifIFD at 26: one entry
	tiff = append(tiff, 0x03, 0x90, 2, 0, byte(len(date)), 0, 0, 0, 44, 0, 0, 0)
	tiff = append(tiff, 0, 0, 0, 0)
	tiff = append(tiff, date...)
	app1 := append([]byte("Exif\x00\x00"), tiff...)
	seg := binary.BigEndian.AppendUint16([]byte{0xFF, 0xE1}, uint16(len(app1)+2))
	out := append(append(append([]byte{}, raw[:2]...), seg...), app1...)
	out = append(out, raw[2:]...)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMediaInfoHandler_JPEG(t *testing.T) {
	ops, root := newFsFixture(t)
	p := filepath.Join(root, "p.jpg")
	writeTestJPEG(t, p, 12, 8)
	rr := mediaInfoGet(ops, p)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["width"] != float64(12) || got["height"] != float64(8) || got["takenAt"] != "2022-07-08T09:10:11Z" {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
	if _, has := got["durationMs"]; has {
		t.Fatalf("durationMs must be omitted for a photo: %s", rr.Body.String())
	}
}

func TestMediaInfoHandler_MP4(t *testing.T) {
	ops, root := newFsFixture(t)
	box := func(typ string, body ...[]byte) []byte {
		b := bytes.Join(body, nil)
		return append(append(binary.BigEndian.AppendUint32(nil, uint32(8+len(b))), typ...), b...)
	}
	mv := []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	mv = binary.BigEndian.AppendUint32(mv, 600)
	mv = binary.BigEndian.AppendUint32(mv, 600*3) // 3 s
	mv = append(mv, make([]byte, 80)...)
	file := bytes.Join([][]byte{box("ftyp", []byte("isom\x00\x00\x00\x00")), box("mdat", make([]byte, 10)), box("moov", box("mvhd", mv))}, nil)
	p := filepath.Join(root, "v.mp4")
	if err := os.WriteFile(p, file, 0o644); err != nil {
		t.Fatal(err)
	}
	rr := mediaInfoGet(ops, p)
	if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != `{"durationMs":3000}` {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
}

func TestMediaInfoHandler_UnsupportedAndDirectoryGiveEmptyObject(t *testing.T) {
	ops, root := newFsFixture(t)
	for _, p := range []string{filepath.Join(root, "a.txt"), root} {
		rr := mediaInfoGet(ops, p)
		if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != `{}` {
			t.Fatalf("%s: got %d %s", p, rr.Code, rr.Body.String())
		}
	}
}

func TestMediaInfoHandler_Errors(t *testing.T) {
	ops, root := newFsFixture(t)
	if rr := mediaInfoGet(ops, ""); rr.Code != http.StatusBadRequest {
		t.Fatalf("missing path: expected 400, got %d", rr.Code)
	}
	if rr := mediaInfoGet(ops, filepath.Join(root, "nope.jpg")); rr.Code != http.StatusNotFound {
		t.Fatalf("missing file: expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
	outside := t.TempDir()
	writeTestJPEG(t, filepath.Join(outside, "o.jpg"), 5, 5)
	if rr := mediaInfoGet(ops, filepath.Join(outside, "o.jpg")); rr.Code != http.StatusForbidden {
		t.Fatalf("outside the root: expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestMediaInfoHandler_SymlinkEscapeRefused(t *testing.T) {
	ops, root := newFsFixture(t)
	outside := t.TempDir()
	writeTestJPEG(t, filepath.Join(outside, "o.jpg"), 5, 5)
	if err := os.Symlink(filepath.Join(outside, "o.jpg"), filepath.Join(root, "link.jpg")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	rr := mediaInfoGet(ops, filepath.Join(root, "link.jpg"))
	if rr.Code == http.StatusOK && strings.Contains(rr.Body.String(), "width") {
		t.Fatalf("symlink escaped the root: %s", rr.Body.String())
	}
}
