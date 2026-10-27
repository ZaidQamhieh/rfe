package mdns

import (
	"testing"
)

func TestStartStop(t *testing.T) {
	svc, err := Start(18765, "1.5.0")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	svc.Stop()
}
