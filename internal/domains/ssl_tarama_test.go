package domains

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sslt_sertifika(t *testing.T, yol string, bitis time.Time) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sab := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "ornek.test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: bitis}
	der, err := x509.CreateCertificate(rand.Reader, sab, sab, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yol, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSertifikaYoluGuvenli(t *testing.T) {
	d := t.TempDir()
	c := filepath.Join(d, "a.crt")
	sslt_sertifika(t, c, time.Now().Add(5*24*time.Hour))
	if !sertifikaYoluGuvenli(c) {
		t.Fatal("geçici dizindeki normal sertifika okunabilmeli")
	}
	if sertifikaYoluGuvenli("goreli/a.crt") || sertifikaYoluGuvenli("") {
		t.Error("göreli/boş yol reddedilmeli")
	}
	if sertifikaYoluGuvenli(d) {
		t.Error("dizin reddedilmeli")
	}
	// /home altına çıkan sembolik bağlantı reddedilir (hedef var olmasa da: EvalSymlinks düşer).
	b := filepath.Join(d, "bag.crt")
	if err := os.Symlink("/home/c_kiraci/sahte.crt", b); err != nil {
		t.Fatal(err)
	}
	if sertifikaYoluGuvenli(b) {
		t.Error("/home'a çıkan bağlantı reddedilmeli")
	}
	_, bitis, _ := sertifikaGercek(c, "", time.Time{})
	if kalan := time.Until(bitis); kalan < 4*24*time.Hour || kalan > 6*24*time.Hour {
		t.Errorf("NotAfter okunamadı: %v", bitis)
	}
}
