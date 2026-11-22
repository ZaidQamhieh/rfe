package fsops

import (
	"bytes"
	"encoding/binary"
	"time"
)

// probeJPEG walks the marker segments at the start of a JPEG (already in
// memory, at most mediaHeadCap bytes), taking the pixel size from the frame
// header (SOFn) and the capture date and rotation from an EXIF APP1 segment.
// A photo stored rotated by 90 degrees (EXIF orientation 5-8) reports its
// size as it is displayed, so width and height are swapped.
func probeJPEG(buf []byte) MediaInfo {
	var info MediaInfo
	var exif exifData
	pos := 2 // after the SOI marker
	for pos+1 < len(buf) {
		if buf[pos] != 0xFF {
			break
		}
		m := buf[pos+1]
		if m == 0xFF { // fill byte before a marker
			pos++
			continue
		}
		pos += 2
		if m == 0x00 {
			break
		}
		if m == 0x01 || (m >= 0xD0 && m <= 0xD8) { // markers without a length
			continue
		}
		if m == 0xD9 || m == 0xDA { // EOI or start of scan: no frame header ahead
			break
		}
		if pos+2 > len(buf) {
			break
		}
		segLen := int(binary.BigEndian.Uint16(buf[pos:]))
		if segLen < 2 || pos+segLen > len(buf) {
			break
		}
		seg := buf[pos+2 : pos+segLen]
		pos += segLen
		switch {
		case m == 0xE1 && !exif.found && bytes.HasPrefix(seg, []byte("Exif\x00\x00")):
			exif = parseExif(seg[6:])
		case isJPEGFrameMarker(m):
			if len(seg) >= 5 {
				info = dims(int(binary.BigEndian.Uint16(seg[3:])), int(binary.BigEndian.Uint16(seg[1:])))
			}
			return finishJPEG(info, exif)
		}
	}
	return finishJPEG(info, exif)
}

// isJPEGFrameMarker reports whether m is a start-of-frame marker (SOF0-SOF15,
// which exclude DHT 0xC4, JPG 0xC8 and DAC 0xCC).
func isJPEGFrameMarker(m byte) bool {
	return m >= 0xC0 && m <= 0xCF && m != 0xC4 && m != 0xC8 && m != 0xCC
}

// finishJPEG merges the frame size with the EXIF facts.
func finishJPEG(info MediaInfo, exif exifData) MediaInfo {
	if exif.orientation >= 5 && exif.orientation <= 8 {
		info.Width, info.Height = info.Height, info.Width
	}
	if !exif.taken.IsZero() {
		info.TakenAt = exif.taken.Format(time.RFC3339)
	}
	return info
}

// exifData is what parseExif extracts from an EXIF block.
type exifData struct {
	found       bool
	orientation int
	taken       time.Time // zero when absent or unparsable
}

// EXIF tags used here.
const (
	exifTagOrientation = 0x0112
	exifTagExifIFD     = 0x8769
	exifTagDateOrig    = 0x9003
	exifTagOffsetOrig  = 0x9011
	exifTypeASCII      = 2
	exifDateLayout     = "2006:01:02 15:04:05"
)

// parseExif reads a TIFF structure (the part of an EXIF APP1 segment after
// "Exif\0\0"): IFD0 for the orientation and a pointer to the Exif IFD, which
// holds DateTimeOriginal and, on newer cameras, its UTC offset. Every offset
// is checked against the block's length, so corrupt data just gives less.
func parseExif(tiff []byte) exifData {
	out := exifData{found: true}
	if len(tiff) < 8 {
		return out
	}
	var bo binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return out
	}
	if bo.Uint16(tiff[2:]) != 42 {
		return out
	}
	var exifIFD uint32
	eachIFDEntry(tiff, bo, bo.Uint32(tiff[4:]), func(tag, typ uint16, count uint32, val []byte) {
		switch tag {
		case exifTagOrientation:
			out.orientation = int(bo.Uint16(val))
		case exifTagExifIFD:
			exifIFD = bo.Uint32(val)
		}
	})
	var date, offset string
	eachIFDEntry(tiff, bo, exifIFD, func(tag, typ uint16, count uint32, val []byte) {
		if typ != exifTypeASCII {
			return
		}
		switch tag {
		case exifTagDateOrig:
			date = exifString(tiff, bo, count, val)
		case exifTagOffsetOrig:
			offset = exifString(tiff, bo, count, val)
		}
	})
	if date != "" {
		out.taken = parseExifTime(date, offset)
	}
	return out
}

// eachIFDEntry calls fn for every 12-byte entry of the image file directory at
// off, stopping at the end of the block.
func eachIFDEntry(tiff []byte, bo binary.ByteOrder, off uint32, fn func(tag, typ uint16, count uint32, val []byte)) {
	if off == 0 || uint64(off)+2 > uint64(len(tiff)) {
		return
	}
	n := int(bo.Uint16(tiff[off:]))
	for i := 0; i < n; i++ {
		e := uint64(off) + 2 + uint64(i)*12
		if e+12 > uint64(len(tiff)) {
			return
		}
		fn(bo.Uint16(tiff[e:]), bo.Uint16(tiff[e+2:]), bo.Uint32(tiff[e+4:]), tiff[e+8:e+12])
	}
}

// exifString returns an ASCII entry's text, which sits in the 4-byte value
// field when it fits there and otherwise at the offset the field holds.
func exifString(tiff []byte, bo binary.ByteOrder, count uint32, val []byte) string {
	var s []byte
	if count <= 4 {
		s = val[:count]
	} else {
		off := uint64(bo.Uint32(val))
		if off+uint64(count) > uint64(len(tiff)) {
			return ""
		}
		s = tiff[off : off+uint64(count)]
	}
	if i := bytes.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	return string(s)
}

// parseExifTime parses "YYYY:MM:DD HH:MM:SS" with the camera's UTC offset
// ("+02:00") when it recorded one. Without an offset the wall-clock time is
// kept and labelled UTC, since EXIF stores no zone. It returns the zero time
// for an unparsable or all-zero date.
func parseExifTime(date, offset string) time.Time {
	if offset != "" {
		if t, err := time.Parse(exifDateLayout+"-07:00", date+offset); err == nil {
			return t
		}
	}
	t, err := time.Parse(exifDateLayout, date)
	if err != nil {
		return time.Time{}
	}
	return t
}
