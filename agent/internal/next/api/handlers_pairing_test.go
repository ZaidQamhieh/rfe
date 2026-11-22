package api_test

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"net/http"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/pairing"
)

// TestPairingGenerateReturnsAQRPNGOfThePayload checks /pairing/generate sends
// qrPngBase64, a PNG whose pixels are the QR code of the qrPayload it returns
// (the same for an ordinary and a guest code).
func TestPairingGenerateReturnsAQRPNGOfThePayload(t *testing.T) {
	r := newRig(t, rigOptions{validateJail: func(root string) (string, error) { return root, nil }})
	adminTok, _ := r.registerAdmin(newPhone("admin-1", 1))

	for name, body := range map[string]any{
		"ordinary": nil,
		"guest":    map[string]any{"guest": true, "jailRoot": "/ok"},
	} {
		t.Run(name, func(t *testing.T) {
			res := r.do("POST", "/pairing/generate", adminTok, body)
			if res.Code != http.StatusOK {
				t.Fatalf("generate = %d %s", res.Code, res.Body.String())
			}
			var out struct {
				QRPayload   pairing.QRPayload `json:"qrPayload"`
				QRPngBase64 string            `json:"qrPngBase64"`
			}
			res.json(t, &out)
			if out.QRPngBase64 == "" {
				t.Fatalf("no qrPngBase64 in %s", res.Body.String())
			}
			data, err := base64.StdEncoding.DecodeString(out.QRPngBase64)
			if err != nil {
				t.Fatalf("qrPngBase64 is not base64: %v", err)
			}
			cfg, err := png.DecodeConfig(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("qrPngBase64 is not a PNG: %v", err)
			}
			if cfg.Width != pairing.QRSizePixels || cfg.Height != pairing.QRSizePixels {
				t.Errorf("size = %dx%d, want %d square", cfg.Width, cfg.Height, pairing.QRSizePixels)
			}
			want, err := pairing.QRPNG(out.QRPayload)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, want) {
				t.Error("the PNG is not the QR code of the returned qrPayload")
			}
		})
	}
}
