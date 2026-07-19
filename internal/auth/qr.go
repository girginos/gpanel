package auth

import (
	"encoding/base64"

	qrcode "github.com/skip2/go-qrcode"
)

// TOTPQRDataURI: otpauth:// URI'sinden 256px QR PNG üretir ve base64 data-URI
// ("data:image/png;base64,...") olarak döndürür. Pure-Go (skip2/go-qrcode), cgo YOK.
// Not: URI secret içerir; bu fonksiyon secret'ı LOGLAMAZ.
func TOTPQRDataURI(uri string) (string, error) {
	png, err := qrcode.Encode(uri, qrcode.Medium, 256)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}
