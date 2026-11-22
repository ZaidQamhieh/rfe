package fsops

import (
	"encoding/binary"
	"io"
	"math"
)

// Limits that keep a hostile file from making the box walk long.
const (
	mp4MaxBoxes     = 64 // top-level boxes visited, and children per moov
	mp4MaxTrakBoxes = 16 // children of one trak visited
	mp4MaxPayload   = 128
)

// mp4Atom is one ISO base media box: absolute file offsets of its payload and end.
type mp4Atom struct {
	typ          string
	payload, end int64
}

// readMP4Box parses the box header at off, which must lie inside [off, limit).
// A size of 1 means a 64-bit size follows; 0 means the box runs to limit. A
// box that claims more than is left is clamped to limit.
func readMP4Box(r io.ReaderAt, off, limit int64) (mp4Atom, bool) {
	var h [16]byte
	if off < 0 || off+8 > limit {
		return mp4Atom{}, false
	}
	if n, _ := r.ReadAt(h[:8], off); n < 8 {
		return mp4Atom{}, false
	}
	size := int64(binary.BigEndian.Uint32(h[:4]))
	hdr := int64(8)
	switch size {
	case 1:
		if off+16 > limit {
			return mp4Atom{}, false
		}
		if n, _ := r.ReadAt(h[8:16], off+8); n < 8 {
			return mp4Atom{}, false
		}
		size = int64(binary.BigEndian.Uint64(h[8:]))
		hdr = 16
	case 0:
		size = limit - off
	}
	if size < hdr {
		return mp4Atom{}, false
	}
	end := limit
	if size <= limit-off {
		end = off + size
	}
	return mp4Atom{typ: string(h[4:8]), payload: off + hdr, end: end}, true
}

// payloadHead reads up to max bytes of the box payload.
func (b mp4Atom) payloadHead(r io.ReaderAt, max int64) []byte {
	n := min(b.end-b.payload, max)
	if n <= 0 {
		return nil
	}
	buf := make([]byte, n)
	got, _ := r.ReadAt(buf, b.payload)
	return buf[:got]
}

// probeMP4 finds the moov box by hopping over the top-level boxes (so a moov
// after a large mdat costs only a few small reads), then reads the movie
// duration from mvhd and the display size from the first track with one.
func probeMP4(r io.ReaderAt, size int64) MediaInfo {
	off := int64(0)
	for i := 0; i < mp4MaxBoxes; i++ {
		b, ok := readMP4Box(r, off, size)
		if !ok {
			break
		}
		if b.typ == "moov" {
			return probeMoov(r, b)
		}
		off = b.end
	}
	return MediaInfo{}
}

// probeMoov reads mvhd (duration) and each trak's tkhd (size) from a moov box.
func probeMoov(r io.ReaderAt, moov mp4Atom) MediaInfo {
	var info MediaInfo
	off := moov.payload
	for i := 0; i < mp4MaxBoxes; i++ {
		b, ok := readMP4Box(r, off, moov.end)
		if !ok {
			break
		}
		switch b.typ {
		case "mvhd":
			info.DurationMs = mvhdDurationMs(b.payloadHead(r, mp4MaxPayload))
		case "trak":
			if info.Width == 0 {
				if w, h := trakSize(r, b); w > 0 && h > 0 {
					info.Width, info.Height = w, h
				}
			}
		}
		off = b.end
	}
	return info
}

// mvhdDurationMs converts the movie header's duration and timescale to
// milliseconds. Version 0 uses 32-bit fields and version 1 64-bit ones; an
// all-ones duration means unknown.
func mvhdDurationMs(p []byte) int64 {
	var scale uint32
	var dur uint64
	switch {
	case len(p) >= 20 && p[0] == 0:
		scale = binary.BigEndian.Uint32(p[12:])
		d := binary.BigEndian.Uint32(p[16:])
		if d == math.MaxUint32 {
			return 0
		}
		dur = uint64(d)
	case len(p) >= 32 && p[0] == 1:
		scale = binary.BigEndian.Uint32(p[20:])
		dur = binary.BigEndian.Uint64(p[24:])
		if dur == math.MaxUint64 {
			return 0
		}
	default:
		return 0
	}
	if scale == 0 || dur/uint64(scale) > math.MaxInt64/1000-1 {
		return 0
	}
	whole := dur / uint64(scale)
	rest := dur % uint64(scale)
	return int64(whole*1000 + rest*1000/uint64(scale))
}

// trakSize returns the display size from a trak's tkhd box (16.16 fixed-point
// at the end of the header), swapped when the track's matrix rotates it by 90
// or 270 degrees, as phones record portrait video. Audio tracks give 0, 0.
func trakSize(r io.ReaderAt, trak mp4Atom) (int, int) {
	off := trak.payload
	for i := 0; i < mp4MaxTrakBoxes; i++ {
		b, ok := readMP4Box(r, off, trak.end)
		if !ok {
			return 0, 0
		}
		if b.typ == "tkhd" {
			p := b.payloadHead(r, mp4MaxPayload)
			if len(p) == 0 {
				return 0, 0
			}
			shift := 0 // version 1 stores 64-bit times: every later field moves 12 bytes
			if p[0] == 1 {
				shift = 12
			}
			if len(p) < 84+shift {
				return 0, 0
			}
			a := binary.BigEndian.Uint32(p[40+shift:])
			d := binary.BigEndian.Uint32(p[56+shift:])
			w := int(binary.BigEndian.Uint32(p[76+shift:]) >> 16)
			h := int(binary.BigEndian.Uint32(p[80+shift:]) >> 16)
			if a == 0 && d == 0 {
				w, h = h, w
			}
			return w, h
		}
		off = b.end
	}
	return 0, 0
}

// EBML element IDs used for WebM / Matroska (IDs keep their length marker bits).
const (
	ebmlSegment     = 0x18538067
	ebmlInfo        = 0x1549A966
	ebmlTracks      = 0x1654AE6B
	ebmlTrackEntry  = 0xAE
	ebmlVideo       = 0xE0
	ebmlTimecodeSc  = 0x2AD7B1
	ebmlDuration    = 0x4489
	ebmlPixelWidth  = 0xB0
	ebmlPixelHeight = 0xBA
	ebmlCluster     = 0x1F43B675
	ebmlMaxDepth    = 6
	// Matroska's default tick is 1,000,000 ns.
	ebmlDefaultScale = 1_000_000
)

// ebmlState collects the values found while walking a Matroska header.
type ebmlState struct {
	scale         uint64
	ticks         float64
	width, height int
}

// probeMatroska reads duration and the first video track's pixel size from the
// Segment's Info and Tracks elements within the first mediaHeadCap bytes.
func probeMatroska(head []byte) MediaInfo {
	s := ebmlState{scale: ebmlDefaultScale}
	s.walk(head, 0)
	info := dims(s.width, s.height)
	ms := s.ticks * float64(s.scale) / 1e6
	if ms > 0 && ms < float64(math.MaxInt64/2) {
		info.DurationMs = int64(math.Round(ms))
	}
	return info
}

// walk visits the sibling elements in buf. It descends into the few master
// elements that lead to the wanted values, skips the rest by size, and stops
// at the first Cluster (the media data) or at any element that is cut off.
func (s *ebmlState) walk(buf []byte, depth int) {
	for len(buf) > 0 {
		id, idLen := ebmlID(buf)
		if idLen == 0 {
			return
		}
		size, sizeLen, unknown := ebmlSize(buf[idLen:])
		if sizeLen == 0 {
			return
		}
		body := buf[idLen+sizeLen:]
		whole := !unknown && size <= uint64(len(body))
		elem := body
		if whole {
			elem = body[:size]
		}
		switch id {
		case ebmlCluster:
			return
		case ebmlSegment, ebmlInfo, ebmlTracks, ebmlTrackEntry, ebmlVideo:
			if depth < ebmlMaxDepth {
				s.walk(elem, depth+1)
			}
		case ebmlTimecodeSc:
			if v := ebmlUint(elem); v > 0 {
				s.scale = v
			}
		case ebmlDuration:
			switch len(elem) {
			case 4:
				s.ticks = float64(math.Float32frombits(binary.BigEndian.Uint32(elem)))
			case 8:
				s.ticks = math.Float64frombits(binary.BigEndian.Uint64(elem))
			}
		case ebmlPixelWidth:
			if s.width == 0 {
				s.width = int(min(ebmlUint(elem), 1<<20))
			}
		case ebmlPixelHeight:
			if s.height == 0 {
				s.height = int(min(ebmlUint(elem), 1<<20))
			}
		}
		if !whole {
			return
		}
		buf = body[size:]
	}
}

// ebmlID reads an element ID (1-4 bytes, length given by the leading zero
// bits) and returns it with its byte length, or 0 when invalid or cut off.
func ebmlID(b []byte) (uint32, int) {
	if len(b) == 0 || b[0] == 0 {
		return 0, 0
	}
	n := 1
	for mask := byte(0x80); b[0]&mask == 0; mask >>= 1 {
		n++
	}
	if n > 4 || len(b) < n {
		return 0, 0
	}
	var id uint32
	for _, c := range b[:n] {
		id = id<<8 | uint32(c)
	}
	return id, n
}

// ebmlSize reads an element size (1-8 bytes, marker bit removed). unknown is
// true for the all-ones "size not known" value. A length of 0 means invalid.
func ebmlSize(b []byte) (size uint64, n int, unknown bool) {
	if len(b) == 0 || b[0] == 0 {
		return 0, 0, false
	}
	n = 1
	mask := byte(0x80)
	for b[0]&mask == 0 {
		n++
		mask >>= 1
	}
	if len(b) < n {
		return 0, 0, false
	}
	size = uint64(b[0] & (mask - 1))
	allOnes := b[0]&(mask-1) == mask-1
	for _, c := range b[1:n] {
		size = size<<8 | uint64(c)
		allOnes = allOnes && c == 0xFF
	}
	return size, n, allOnes
}

// ebmlUint reads a big-endian unsigned integer of up to 8 bytes.
func ebmlUint(b []byte) uint64 {
	if len(b) == 0 || len(b) > 8 {
		return 0
	}
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v
}
