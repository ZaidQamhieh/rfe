package fsops

import (
	"errors"
	"testing"
)

func TestRenamedEntryReportsTheNewNameWhenTheLookupFails(t *testing.T) {
	dst := &securePath{full: "/data/new name.txt"}
	e := renamedEntry(dst, nil, errors.New("gone"))
	if e.Name != "new name.txt" || e.Path != "/data/new name.txt" {
		t.Fatalf("got %+v", e)
	}
}
