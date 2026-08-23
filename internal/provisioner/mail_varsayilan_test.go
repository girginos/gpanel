package provisioner

// mail_varsayilan_test.go — varsayılan mail sertifikası mantığının kanıtı:
//  - self-signed cert → geçersiz (değiştirilmeli / kaynak olarak reddedilir)
//  - CA-imzalı (LE) cert → geçerli
//  - süresi geçmiş → geçersiz

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

// certUret — test için: self-signed VEYA "farklı issuer" (LE taklidi) cert PEM.
func certUret(t *testing.T, subjectCN, issuerCN string, notAfter time.Time) []byte {
	t.Helper()
	anahtar, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	// issuer != subject yapmak için ayrı bir "CA" ile imzala.
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: issuerCN},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: subjectCN},
		Issuer:       pkix.Name{CommonName: issuerCN},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}
	var der []byte
	if subjectCN == issuerCN {
		// self-signed: kendi anahtarıyla, kendi kendine imza
		der, _ = x509.CreateCertificate(rand.Reader, tmpl, tmpl, &anahtar.PublicKey, anahtar)
	} else {
		// CA imzalı (issuer != subject) — LE benzeri
		der, _ = x509.CreateCertificate(rand.Reader, tmpl, caTmpl, &anahtar.PublicKey, caKey)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestCertGecerliLE(t *testing.T) {
	gelecek := time.Now().Add(90 * 24 * time.Hour)
	gecmis := time.Now().Add(-24 * time.Hour)

	self := certUret(t, "gpanel.example", "gpanel.example", gelecek) // self-signed
	le := certUret(t, "mail.example", "R3 Let's Encrypt", gelecek)    // CA imzalı
	suresi := certUret(t, "mail.example", "R3 Let's Encrypt", gecmis) // süresi geçmiş LE

	if certGecerliLE(self) {
		t.Error("self-signed cert GEÇERLİ sayıldı — kaynak olarak reddedilmeliydi")
	}
	if !certGecerliLE(le) {
		t.Error("CA-imzalı LE cert GEÇERSİZ sayıldı — kabul edilmeliydi")
	}
	if certGecerliLE(suresi) {
		t.Error("süresi geçmiş cert GEÇERLİ sayıldı")
	}
	t.Log("certGecerliLE: self-signed reddedildi, LE kabul edildi, süresi geçmiş reddedildi ✓")
}
