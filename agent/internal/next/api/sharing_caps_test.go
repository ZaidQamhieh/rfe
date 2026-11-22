package api

import "testing"

func TestWithSharingPolicyClearsShareOnlyWhileSharingIsOff(t *testing.T) {
	all := FileCapsJSON{true, true, true, true, true, true}
	off := withSharingPolicy(all, false)
	if off.Share || !off.Browse || !off.Download || !off.Upload || !off.Modify || !off.Delete {
		t.Fatalf("sharing off must clear only share: %+v", off)
	}
	if on := withSharingPolicy(all, true); on != all {
		t.Fatalf("sharing on must change nothing: %+v", on)
	}
}
