package provisioner

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

func uretCert(t *testing.T, cn string, sans []string, bas, bit time.Time) (string, string) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn},
		DNSNames: sans, NotBefore: bas, NotAfter: bit,
	}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	cpem := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	kb, _ := x509.MarshalECPrivateKey(key)
	kpem := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})
	return string(cpem), string(kpem)
}

func TestSertifikaDogrula(t *testing.T) {
	now := time.Now()
	// (1) Gecerli — alan adini kapsar, sureli
	c, k := uretCert(t, "girgin.net.tr", []string{"girgin.net.tr", "www.girgin.net.tr"}, now.Add(-time.Hour), now.Add(24*time.Hour))
	if _, err := sertifikaDogrula(c, k, "girgin.net.tr"); err != nil {
		t.Errorf("gecerli cert reddedildi: %v", err)
	}
	// (2) Suresi gecmis → RED
	c2, k2 := uretCert(t, "girgin.net.tr", []string{"girgin.net.tr"}, now.Add(-48*time.Hour), now.Add(-24*time.Hour))
	if _, err := sertifikaDogrula(c2, k2, "girgin.net.tr"); err == nil {
		t.Error("suresi gecmis cert KABUL edildi")
	}
	// (3) Yanlis alan adi → RED
	c3, k3 := uretCert(t, "baska.com", []string{"baska.com"}, now.Add(-time.Hour), now.Add(24*time.Hour))
	if _, err := sertifikaDogrula(c3, k3, "girgin.net.tr"); err == nil {
		t.Error("alan-adini-kapsamayan cert KABUL edildi")
	}
	// (4) Key <-> cert eslesmiyor → RED
	_, kx := uretCert(t, "girgin.net.tr", []string{"girgin.net.tr"}, now.Add(-time.Hour), now.Add(24*time.Hour))
	if _, err := sertifikaDogrula(c, kx, "girgin.net.tr"); err == nil {
		t.Error("eslesmeyen key KABUL edildi")
	}
}
