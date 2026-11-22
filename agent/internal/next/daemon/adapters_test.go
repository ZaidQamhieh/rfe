package daemon

import "testing"

// The API finds DropDevice by type assertion, so the adapter it is given must have it.
func TestTransferAdapterExposesDropDevice(t *testing.T) {
	var a any = &transferAdapter{}
	if _, ok := a.(interface{ DropDevice(deviceID string) }); !ok {
		t.Fatal("transferAdapter lacks DropDevice; revoking a device would not clear its uploads")
	}
}
