package sidecar

import (
	"bytes"
	"errors"
	"testing"
)

type recordingWriter struct {
	writes [][]byte
	failAt int // 1-based write number that fails; 0 never
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.writes = append(w.writes, append([]byte(nil), p...))
	if w.failAt != 0 && len(w.writes) == w.failAt {
		return 0, errors.New("boom")
	}
	return len(p), nil
}

func TestWriteFrameSplitsOnlyLargeBodiesAndKeepsWireFormat(t *testing.T) {
	for _, tc := range []struct {
		size       int
		wantWrites int
	}{{0, 1}, {64 << 10, 1}, {64<<10 + 1, 2}, {1 << 20, 2}} {
		payload := bytes.Repeat([]byte{'x'}, tc.size)
		rw := &recordingWriter{}
		if err := writeFrame(rw, kindBin, payload); err != nil {
			t.Fatal(err)
		}
		if len(rw.writes) != tc.wantWrites {
			t.Fatalf("size %d: %d writes, want %d", tc.size, len(rw.writes), tc.wantWrites)
		}
		var wire bytes.Buffer
		for _, w := range rw.writes {
			wire.Write(w)
		}
		f, err := readFrame(&wire)
		if err != nil || f.kind != kindBin || !bytes.Equal(f.payload, payload) {
			t.Fatalf("size %d: round trip failed (err %v)", tc.size, err)
		}
	}
	// A failing second write of a large frame must surface.
	rw := &recordingWriter{failAt: 2}
	if err := writeFrame(rw, kindBin, make([]byte, 1<<20)); err == nil {
		t.Fatal("payload write error was swallowed")
	}
}
