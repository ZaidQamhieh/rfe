package sidecar

import (
	"os"
	"testing"
	"time"
)

// Audit F-71/F-97: a sidecar that stops reading stdin must not block send forever.
func TestSendTimesOutWhenSidecarStopsReading(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	old := sendTimeout
	sendTimeout = 200 * time.Millisecond
	defer func() { sendTimeout = old }()

	c := &Client{stdin: w}
	done := make(chan error, 1)
	go func() { done <- c.send([]byte(`{"id":1}`), make([]byte, 1<<20)) }() // far above the pipe buffer
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("send to a pipe nobody reads returned nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("send blocked past its write deadline")
	}
}
