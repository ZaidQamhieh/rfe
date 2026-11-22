package pairing

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/skip2/go-qrcode"

	oldpairing "github.com/ZaidQamhieh/rfe/agent/internal/pairing"
)

// pngSignature is the eight bytes every PNG file starts with.
var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// TestQRPayloadJSONMatchesOldAgent checks the text a QR code carries is the
// same JSON the old agent embeds, for a payload with and without a Tailscale
// address.
func TestQRPayloadJSONMatchesOldAgent(t *testing.T) {
	for _, ts := range []string{"", "100.64.0.9:8765"} {
		got := QRPayload{Address: srcA, TailscaleAddress: ts, CertFingerprint: testFingerprint, PairingCode: "ABCD2345"}
		want := oldpairing.QRPayload{Address: srcA, TailscaleAddress: ts, CertFingerprint: testFingerprint, PairingCode: "ABCD2345"}
		if got.JSON() != want.JSON() {
			t.Errorf("tailscale %q: JSON = %s, want %s", ts, got.JSON(), want.JSON())
		}
	}
}

// TestQRPNGIsAScannableImageOfThePayload checks the output is a PNG of the
// documented size whose pixels are the QR code of the payload's JSON: it equals
// the old agent's encoding of the same text.
func TestQRPNGIsAScannableImageOfThePayload(t *testing.T) {
	p := QRPayload{Address: srcA, CertFingerprint: testFingerprint, PairingCode: "ABCD2345"}
	data, err := QRPNG(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, pngSignature) {
		t.Fatalf("not a PNG: starts % x", data[:min(len(data), len(pngSignature))])
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("PNG header does not decode: %v", err)
	}
	if cfg.Width != QRSizePixels || cfg.Height != QRSizePixels {
		t.Errorf("size = %dx%d, want %dx%d", cfg.Width, cfg.Height, QRSizePixels, QRSizePixels)
	}
	old := oldpairing.QRPayload{Address: srcA, CertFingerprint: testFingerprint, PairingCode: "ABCD2345"}
	want, err := qrcode.Encode(old.JSON(), qrcode.Medium, QRSizePixels)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, want) {
		t.Error("PNG differs from the old agent's QR of the same payload")
	}
	other, err := QRPNG(QRPayload{Address: srcA, CertFingerprint: testFingerprint, PairingCode: "ZZZZ9999"})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(data, other) {
		t.Error("a different payload gave the same image")
	}
}
