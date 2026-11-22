package fsops

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// probeBytes runs ProbeMedia over an in-memory file.
func probeBytes(b []byte) MediaInfo {
	return ProbeMedia(bytes.NewReader(b), int64(len(b)))
}

// ---- JPEG + EXIF ----

// exifOpts selects which tags buildExif writes.
type exifOpts struct {
	bigEndian   bool
	orientation uint16 // 0 = omit
	taken       string // "" = omit DateTimeOriginal
	offset      string // "" = omit OffsetTimeOriginal
}

// buildExif returns an APP1 payload ("Exif\0\0" + TIFF) with IFD0 -> ExifIFD.
func buildExif(o exifOpts) []byte {
	var bo binary.ByteOrder = binary.LittleEndian
	hdr := []byte("II")
	if o.bigEndian {
		bo = binary.BigEndian
		hdr = []byte("MM")
	}
	u16 := func(v uint16) []byte { b := make([]byte, 2); bo.PutUint16(b, v); return b }
	u32 := func(v uint32) []byte { b := make([]byte, 4); bo.PutUint32(b, v); return b }
	entry := func(tag, typ uint16, count uint32, val []byte) []byte {
		e := append(u16(tag), u16(typ)...)
		e = append(e, u32(count)...)
		v := make([]byte, 4)
		copy(v, val)
		return append(e, v...)
	}

	nIFD0 := 1 // ExifIFD pointer
	if o.orientation != 0 {
		nIFD0++
	}
	ifd0Size := 2 + nIFD0*12 + 4
	exifIFDOff := 8 + ifd0Size
	nExif := 0
	if o.taken != "" {
		nExif++
	}
	if o.offset != "" {
		nExif++
	}
	exifIFDSize := 2 + nExif*12 + 4
	dataOff := exifIFDOff + exifIFDSize

	tiff := append(append([]byte{}, hdr...), u16(42)...)
	tiff = append(tiff, u32(8)...)
	tiff = append(tiff, u16(uint16(nIFD0))...)
	if o.orientation != 0 {
		tiff = append(tiff, entry(0x0112, 3, 1, u16(o.orientation))...)
	}
	tiff = append(tiff, entry(0x8769, 4, 1, u32(uint32(exifIFDOff)))...)
	tiff = append(tiff, u32(0)...)

	var data []byte
	tiff = append(tiff, u16(uint16(nExif))...)
	if o.taken != "" {
		s := append([]byte(o.taken), 0)
		tiff = append(tiff, entry(0x9003, 2, uint32(len(s)), u32(uint32(dataOff+len(data))))...)
		data = append(data, s...)
	}
	if o.offset != "" {
		s := append([]byte(o.offset), 0)
		tiff = append(tiff, entry(0x9011, 2, uint32(len(s)), u32(uint32(dataOff+len(data))))...)
		data = append(data, s...)
	}
	tiff = append(tiff, u32(0)...)
	tiff = append(tiff, data...)
	return append([]byte("Exif\x00\x00"), tiff...)
}

// jpegWithExif encodes a w x h JPEG and inserts an APP1 segment after SOI.
func jpegWithExif(t *testing.T, w, h int, app1 []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	if app1 == nil {
		return raw
	}
	seg := []byte{0xFF, 0xE1, byte((len(app1) + 2) >> 8), byte(len(app1) + 2)}
	seg = append(seg, app1...)
	out := append([]byte{}, raw[:2]...)
	out = append(out, seg...)
	return append(out, raw[2:]...)
}

func TestProbeJPEGDimensionsAndTakenAt(t *testing.T) {
	for _, bigEndian := range []bool{false, true} {
		data := jpegWithExif(t, 7, 5, buildExif(exifOpts{bigEndian: bigEndian, taken: "2021:03:04 05:06:07", offset: "+02:00"}))
		got := probeBytes(data)
		if got.Width != 7 || got.Height != 5 {
			t.Fatalf("bigEndian=%v: dims %dx%d, want 7x5", bigEndian, got.Width, got.Height)
		}
		if got.TakenAt != "2021-03-04T05:06:07+02:00" {
			t.Fatalf("bigEndian=%v: takenAt %q", bigEndian, got.TakenAt)
		}
		if got.DurationMs != 0 {
			t.Fatalf("durationMs %d on a photo", got.DurationMs)
		}
	}
}

func TestProbeJPEGWithoutOffsetAssumesUTC(t *testing.T) {
	got := probeBytes(jpegWithExif(t, 3, 2, buildExif(exifOpts{taken: "2020:12:31 23:59:58"})))
	if got.TakenAt != "2020-12-31T23:59:58Z" {
		t.Fatalf("takenAt %q", got.TakenAt)
	}
}

func TestProbeJPEGOrientationSwapsDimensions(t *testing.T) {
	got := probeBytes(jpegWithExif(t, 8, 3, buildExif(exifOpts{orientation: 6})))
	if got.Width != 3 || got.Height != 8 {
		t.Fatalf("orientation 6: %dx%d, want 3x8", got.Width, got.Height)
	}
	got = probeBytes(jpegWithExif(t, 8, 3, buildExif(exifOpts{orientation: 1})))
	if got.Width != 8 || got.Height != 3 {
		t.Fatalf("orientation 1: %dx%d, want 8x3", got.Width, got.Height)
	}
	if got.TakenAt != "" {
		t.Fatalf("takenAt %q without a date tag", got.TakenAt)
	}
}

func TestProbeJPEGNoExifOrBadDate(t *testing.T) {
	got := probeBytes(jpegWithExif(t, 4, 9, nil))
	if got.Width != 4 || got.Height != 9 || got.TakenAt != "" {
		t.Fatalf("plain jpeg: %+v", got)
	}
	got = probeBytes(jpegWithExif(t, 4, 9, buildExif(exifOpts{taken: "0000:00:00 00:00:00"})))
	if got.Width != 4 || got.TakenAt != "" {
		t.Fatalf("zeroed date: %+v", got)
	}
}

// ---- PNG, GIF, WebP ----

func TestProbePNGAndGIF(t *testing.T) {
	var p bytes.Buffer
	if err := png.Encode(&p, image.NewRGBA(image.Rect(0, 0, 13, 6))); err != nil {
		t.Fatal(err)
	}
	if got := probeBytes(p.Bytes()); got.Width != 13 || got.Height != 6 {
		t.Fatalf("png: %+v", got)
	}
	var g bytes.Buffer
	pal := image.NewPaletted(image.Rect(0, 0, 4, 11), color.Palette{color.Black, color.White})
	if err := gif.Encode(&g, pal, nil); err != nil {
		t.Fatal(err)
	}
	if got := probeBytes(g.Bytes()); got.Width != 4 || got.Height != 11 {
		t.Fatalf("gif: %+v", got)
	}
}

func riffWebP(chunk string, data []byte) []byte {
	body := append([]byte("WEBP"), []byte(chunk)...)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(data)))
	body = append(body, data...)
	out := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(len(body)))...)
	return append(out, body...)
}

func TestProbeWebPVariants(t *testing.T) {
	// VP8X: canvas width-1 and height-1 as 24-bit little-endian.
	vp8x := []byte{0, 0, 0, 0, 0x2F, 0x01, 0x00, 0x8F, 0x00, 0x00} // 304 x 144
	if got := probeBytes(riffWebP("VP8X", vp8x)); got.Width != 304 || got.Height != 144 {
		t.Fatalf("VP8X: %+v", got)
	}
	// VP8L: signature 0x2F then (w-1) | (h-1)<<14 in 32 bits.
	vp8l := append([]byte{0x2F}, binary.LittleEndian.AppendUint32(nil, uint32(99)|uint32(49)<<14)...)
	if got := probeBytes(riffWebP("VP8L", vp8l)); got.Width != 100 || got.Height != 50 {
		t.Fatalf("VP8L: %+v", got)
	}
	// VP8: 3-byte frame tag, start code 9D 01 2A, 14-bit width and height.
	vp8 := []byte{0x10, 0x00, 0x00, 0x9D, 0x01, 0x2A, 0x40, 0x01, 0xF0, 0x00} // 320 x 240
	if got := probeBytes(riffWebP("VP8 ", vp8)); got.Width != 320 || got.Height != 240 {
		t.Fatalf("VP8: %+v", got)
	}
}

// ---- MP4 / MOV ----

func mp4Box(typ string, payload ...[]byte) []byte {
	body := bytes.Join(payload, nil)
	out := binary.BigEndian.AppendUint32(nil, uint32(8+len(body)))
	out = append(out, typ...)
	return append(out, body...)
}

func mvhd(version byte, timescale uint32, duration uint64) []byte {
	if version == 1 {
		p := []byte{1, 0, 0, 0}
		p = append(p, make([]byte, 16)...) // creation + modification
		p = binary.BigEndian.AppendUint32(p, timescale)
		p = binary.BigEndian.AppendUint64(p, duration)
		return mp4Box("mvhd", p, make([]byte, 80))
	}
	p := []byte{0, 0, 0, 0}
	p = append(p, make([]byte, 8)...)
	p = binary.BigEndian.AppendUint32(p, timescale)
	p = binary.BigEndian.AppendUint32(p, uint32(duration))
	return mp4Box("mvhd", p, make([]byte, 80))
}

// tkhd builds a v0 or v1 track header; rotate writes a 90 degree matrix.
func tkhd(version byte, w, h uint32, rotate bool) []byte {
	var p []byte
	if version == 1 {
		p = append([]byte{1, 0, 0, 3}, make([]byte, 32)...) // times, track id, reserved, duration
	} else {
		p = append([]byte{0, 0, 0, 3}, make([]byte, 20)...)
	}
	p = append(p, make([]byte, 16)...) // reserved, layer, group, volume, reserved
	one := uint32(0x10000)
	m := [9]uint32{one, 0, 0, 0, one, 0, 0, 0, 0x40000000}
	if rotate {
		m = [9]uint32{0, one, 0, 0xFFFF0000, 0, 0, 0, 0, 0x40000000}
	}
	for _, v := range m {
		p = binary.BigEndian.AppendUint32(p, v)
	}
	p = binary.BigEndian.AppendUint32(p, w<<16)
	p = binary.BigEndian.AppendUint32(p, h<<16)
	return mp4Box("tkhd", p)
}

func TestProbeMP4MoovAtEnd(t *testing.T) {
	audio := mp4Box("trak", tkhd(0, 0, 0, false))
	video := mp4Box("trak", tkhd(0, 1920, 1080, false))
	moov := mp4Box("moov", mvhd(0, 1000, 5500), audio, video)
	// 64-bit "largesize" mdat in front of moov, as long recordings use.
	mdatBody := make([]byte, 5000)
	mdat := binary.BigEndian.AppendUint32(nil, 1)
	mdat = append(mdat, "mdat"...)
	mdat = binary.BigEndian.AppendUint64(mdat, uint64(16+len(mdatBody)))
	mdat = append(mdat, mdatBody...)
	file := bytes.Join([][]byte{mp4Box("ftyp", []byte("isom\x00\x00\x02\x00")), mdat, moov}, nil)
	got := probeBytes(file)
	if got.DurationMs != 5500 || got.Width != 1920 || got.Height != 1080 {
		t.Fatalf("moov at end: %+v", got)
	}
}

func TestProbeMP4MoovFirstV1AndRotation(t *testing.T) {
	video := mp4Box("trak", tkhd(1, 1920, 1080, true))
	moov := mp4Box("moov", mvhd(1, 90000, 90000*61+45000), video) // 61.5 s
	file := bytes.Join([][]byte{mp4Box("ftyp", []byte("qt  \x00\x00\x00\x00")), moov, mp4Box("mdat", make([]byte, 64))}, nil)
	got := probeBytes(file)
	if got.DurationMs != 61500 {
		t.Fatalf("duration %d, want 61500", got.DurationMs)
	}
	if got.Width != 1080 || got.Height != 1920 {
		t.Fatalf("rotated video %dx%d, want 1080x1920", got.Width, got.Height)
	}
}

func TestProbeMP4UnknownDurationIgnored(t *testing.T) {
	moov := mp4Box("moov", mvhd(0, 1000, math.MaxUint32))
	got := probeBytes(bytes.Join([][]byte{mp4Box("ftyp", []byte("isom\x00\x00\x00\x00")), moov}, nil))
	if got.DurationMs != 0 {
		t.Fatalf("all-ones duration reported as %d", got.DurationMs)
	}
}

// ---- WebM / Matroska ----

func ebml(id []byte, payload ...[]byte) []byte {
	body := bytes.Join(payload, nil)
	out := append([]byte{}, id...)
	if len(body) < 127 {
		return append(append(out, 0x80|byte(len(body))), body...)
	}
	out = append(out, 0x40|byte(len(body)>>8), byte(len(body)))
	return append(out, body...)
}

func TestProbeWebM(t *testing.T) {
	dur := binary.BigEndian.AppendUint64(nil, math.Float64bits(12345.5))
	info := ebml([]byte{0x15, 0x49, 0xA9, 0x66},
		ebml([]byte{0x2A, 0xD7, 0xB1}, []byte{0x0F, 0x42, 0x40}), // 1,000,000 ns per tick
		ebml([]byte{0x44, 0x89}, dur))
	video := ebml([]byte{0xE0},
		ebml([]byte{0xB0}, []byte{0x02, 0x80}), // 640
		ebml([]byte{0xBA}, []byte{0x01, 0xE0})) // 480
	tracks := ebml([]byte{0x16, 0x54, 0xAE, 0x6B},
		ebml([]byte{0xAE}, ebml([]byte{0x83}, []byte{1}), video))
	cluster := ebml([]byte{0x1F, 0x43, 0xB6, 0x75}, []byte{0xE7, 0x81, 0x00})
	seg := ebml([]byte{0x18, 0x53, 0x80, 0x67}, info, tracks, cluster)
	file := append(ebml([]byte{0x1A, 0x45, 0xDF, 0xA3}, ebml([]byte{0x42, 0x82}, []byte("webm"))), seg...)
	got := probeBytes(file)
	if got.DurationMs != 12346 {
		t.Fatalf("duration %d, want 12346 (12345.5 ms rounded)", got.DurationMs)
	}
	if got.Width != 640 || got.Height != 480 {
		t.Fatalf("dims %dx%d, want 640x480", got.Width, got.Height)
	}
}

// ---- unsupported and hostile input ----

func TestProbeUnsupportedAndEmpty(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty": nil,
		"text":  []byte("hello world, this is not media"),
		"pdf":   []byte("%PDF-1.7 ......"),
	} {
		if got := probeBytes(data); got != (MediaInfo{}) {
			t.Fatalf("%s: %+v, want empty", name, got)
		}
	}
}

// Every truncation of every fixture, and garbage after a valid magic, must
// return without panicking.
func TestProbeTruncatedAndGarbageDoNotPanic(t *testing.T) {
	jp := jpegWithExif(t, 6, 6, buildExif(exifOpts{taken: "2021:03:04 05:06:07", offset: "+02:00", orientation: 6}))
	mp := bytes.Join([][]byte{mp4Box("ftyp", []byte("isom\x00\x00\x00\x00")), mp4Box("moov", mvhd(1, 1000, 9000), mp4Box("trak", tkhd(1, 10, 20, true)))}, nil)
	wb := riffWebP("VP8X", []byte{0, 0, 0, 0, 1, 0, 0, 1, 0, 0})
	for _, data := range [][]byte{jp, mp, wb} {
		for n := 0; n <= len(data); n++ {
			probeBytes(data[:n])
		}
	}
	junk := bytes.Repeat([]byte{0xFF}, 4096)
	for _, magic := range [][]byte{{0xFF, 0xD8, 0xFF}, {0x1A, 0x45, 0xDF, 0xA3}, []byte("\x00\x00\x00\x18ftyp"), []byte("RIFF\x00\x00\x00\x00WEBP")} {
		probeBytes(append(append([]byte{}, magic...), junk...))
		probeBytes(append(append([]byte{}, magic...), make([]byte, 4096)...))
	}
}

// ---- Ops.MediaInfo ----

func TestOpsMediaInfoStaysInsideRoot(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	ops := New([]string{root}, false)
	good := jpegWithExif(t, 5, 4, nil)
	if err := os.WriteFile(filepath.Join(root, "in.jpg"), good, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "out.jpg"), good, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := ops.MediaInfo(filepath.Join(root, "in.jpg"))
	if err != nil || info.Width != 5 || info.Height != 4 {
		t.Fatalf("inside: %+v, %v", info, err)
	}
	if _, err := ops.MediaInfo(filepath.Join(outside, "out.jpg")); err == nil {
		t.Fatal("a file outside the root was probed")
	}
	if err := os.Symlink(filepath.Join(outside, "out.jpg"), filepath.Join(root, "link.jpg")); err == nil {
		if got, err := ops.MediaInfo(filepath.Join(root, "link.jpg")); err == nil && got != (MediaInfo{}) {
			t.Fatalf("symlink escape probed: %+v", got)
		}
	}
	if _, err := ops.MediaInfo(filepath.Join(root, "missing.jpg")); err == nil {
		t.Fatal("missing file did not fail")
	}
}
