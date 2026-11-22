package fsops

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
)

// mediaHeadCap is the most bytes read from the start of a file when looking
// for image dimensions, EXIF data or Matroska headers. MP4 files are read by
// seeking to individual boxes instead, so a moov box at the end is found
// without reading the media data in front of it.
const mediaHeadCap = 1 << 20

// MediaInfo holds what could be read from a photo or video header. A field is
// zero (and left out of the JSON) when the file does not say.
type MediaInfo struct {
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	TakenAt    string `json:"takenAt,omitempty"` // RFC 3339, from EXIF DateTimeOriginal
	DurationMs int64  `json:"durationMs,omitempty"`
}

// MediaInfo opens path through the same jail checks as every other read and
// returns the header facts of the file there. A folder or a file of an unknown
// type gives an empty MediaInfo; a missing path gives ErrNotFound.
func (o *Ops) MediaInfo(path string) (MediaInfo, error) {
	f, err := o.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return MediaInfo{}, ErrNotFound
		}
		return MediaInfo{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return MediaInfo{}, err
	}
	if st.IsDir() {
		return MediaInfo{}, nil
	}
	return ProbeMedia(f, st.Size()), nil
}

// ProbeMedia identifies the file by its leading bytes (JPEG, PNG, GIF, WebP,
// MP4/MOV, WebM/Matroska) and reads dimensions, capture date and duration from
// its headers. It never decodes pixels, reads a bounded number of bytes and
// returns an empty MediaInfo for anything it does not recognise or understand.
func ProbeMedia(r io.ReaderAt, size int64) MediaInfo {
	head := readMediaHead(r, size)
	switch {
	case bytes.HasPrefix(head, []byte{0xFF, 0xD8, 0xFF}):
		return probeJPEG(head)
	case bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n")):
		// The IHDR chunk follows the 8-byte signature: length, "IHDR", width, height.
		if len(head) >= 24 && string(head[12:16]) == "IHDR" {
			return dims(int(binary.BigEndian.Uint32(head[16:])), int(binary.BigEndian.Uint32(head[20:])))
		}
	case bytes.HasPrefix(head, []byte("GIF87a")), bytes.HasPrefix(head, []byte("GIF89a")):
		if len(head) >= 10 {
			return dims(int(binary.LittleEndian.Uint16(head[6:])), int(binary.LittleEndian.Uint16(head[8:])))
		}
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		return probeWebP(head)
	case len(head) >= 8 && string(head[4:8]) == "ftyp":
		return probeMP4(r, size)
	case bytes.HasPrefix(head, []byte{0x1A, 0x45, 0xDF, 0xA3}):
		return probeMatroska(head)
	}
	return MediaInfo{}
}

// readMediaHead returns up to mediaHeadCap bytes from the start of the file.
func readMediaHead(r io.ReaderAt, size int64) []byte {
	n := min(size, mediaHeadCap)
	if n <= 0 {
		return nil
	}
	buf := make([]byte, n)
	got, _ := r.ReadAt(buf, 0)
	return buf[:got]
}

// dims returns a MediaInfo with the given size, or an empty one if either side
// is not positive or implausibly large (a corrupt header).
func dims(w, h int) MediaInfo {
	const maxSide = 1 << 20
	if w <= 0 || h <= 0 || w > maxSide || h > maxSide {
		return MediaInfo{}
	}
	return MediaInfo{Width: w, Height: h}
}

// probeWebP reads the canvas size from the first chunk of a RIFF/WebP file,
// which is a lossy "VP8 ", lossless "VP8L" or extended "VP8X" chunk.
func probeWebP(head []byte) MediaInfo {
	if len(head) < 20 {
		return MediaInfo{}
	}
	data := head[20:] // chunk payload; head[12:16] is the chunk name
	switch string(head[12:16]) {
	case "VP8X":
		if len(data) >= 10 {
			w := int(data[4]) | int(data[5])<<8 | int(data[6])<<16
			h := int(data[7]) | int(data[8])<<8 | int(data[9])<<16
			return dims(w+1, h+1)
		}
	case "VP8L":
		if len(data) >= 5 && data[0] == 0x2F {
			bits := binary.LittleEndian.Uint32(data[1:])
			return dims(int(bits&0x3FFF)+1, int(bits>>14&0x3FFF)+1)
		}
	case "VP8 ":
		if len(data) >= 10 && data[3] == 0x9D && data[4] == 0x01 && data[5] == 0x2A {
			return dims(int(binary.LittleEndian.Uint16(data[6:])&0x3FFF), int(binary.LittleEndian.Uint16(data[8:])&0x3FFF))
		}
	}
	return MediaInfo{}
}
