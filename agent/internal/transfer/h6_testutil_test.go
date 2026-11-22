package transfer

import (
	"io"
	"os"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// h6Upload opens a session for data (4-byte chunks), writes the chunks in the order given and returns the session's temp path.
func h6Upload(t *testing.T, tm *Manager, id, target string, data []byte, overwrite bool, order []int) string {
	t.Helper()
	const chunk = 4
	if _, err := tm.OpenSession(id, target, int64(len(data)), chunk, sha256hex(data), overwrite, "dev"); err != nil {
		t.Fatalf("open %s: %v", id, err)
	}
	for _, n := range order {
		end := (n + 1) * chunk
		if end > len(data) {
			end = len(data)
		}
		part := data[n*chunk : end]
		if err := tm.WriteChunk(id, n, part, sha256hex(part)); err != nil {
			t.Fatalf("chunk %d of %s: %v", n, id, err)
		}
	}
	return tm.tempPathFor(id)
}

// h6InOrder is 0..n-1 for the chunk count of data.
func h6InOrder(data []byte) []int {
	var order []int
	for n := 0; n*4 < len(data); n++ {
		order = append(order, n)
	}
	return order
}

// h6Complete completes id the way the HTTP handler does: publishing through ops with the session's publish options.
func h6Complete(tm *Manager, ops *fsops.Ops, id string) error {
	_, _, err := tm.CompleteWithPublisher(id, func(path string, source io.Reader, overwrite bool) (os.FileInfo, error) {
		info, err := ops.PublishReaderWith(path, source, overwrite, tm.PublishOptions(id))
		if err == fsops.ErrConflict {
			return nil, ErrDestinationExists
		}
		return info, err
	})
	return err
}

// h6Ops returns the Ops variants a publish runs under: jailed to root, and without a jail.
func h6Ops(root string) map[string]*fsops.Ops {
	return map[string]*fsops.Ops{"jailed": fsops.New([]string{root}, false), "unjailed": fsops.New(nil, false)}
}

func h6Read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// h6NoPublishTemps fails if dir holds a hidden publish temp.
func h6NoPublishTemps(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if fsops.IsPublishTemp(e.Name()) {
			t.Errorf("publish temp %s left in %s", e.Name(), dir)
		}
	}
}
