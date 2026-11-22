package pairing

import (
	"encoding/json"

	"github.com/skip2/go-qrcode"
)

// QRSizePixels is the width and height of the QR image sent to a client
// (openapi /pairing/generate: a 240px PNG).
const QRSizePixels = 240

// qrRecovery is the error-correction level of the QR code (medium, as the old
// agent used).
const qrRecovery = qrcode.Medium

// JSON returns the payload as the JSON text the QR code carries.
func (p QRPayload) JSON() string {
	b, _ := json.Marshal(p) // a struct of strings cannot fail to marshal
	return string(b)
}

// QRPNG renders the payload's JSON as a QR code and returns it as PNG bytes,
// QRSizePixels square. The caller treats a failure as non-fatal: the text code
// alone is enough to pair.
func QRPNG(p QRPayload) ([]byte, error) {
	return qrcode.Encode(p.JSON(), qrRecovery, QRSizePixels)
}
