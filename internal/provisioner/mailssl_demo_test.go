package provisioner

// GERÇEK sunucuda çalıştırılan mail SSL testi.
//
// 🔴 Normal `go test ./...` koşusunda ATLANIR: Let's Encrypt'ten GERÇEK
// sertifika ister ve nginx yapılandırmasını değiştirir.
//
//	go test -c -o /tmp/sslt ./internal/provisioner
//	GOSP_GERCEK_KURULUM=1 GOSP_TEST_DOMAIN=ornek.net /tmp/sslt \
//	    -test.run TestMailSertifikaGercek -test.v

import (
	"os"
	"strings"
	"testing"
)

func TestMailSertifikaGercek(t *testing.T) {
	if os.Getenv("GOSP_GERCEK_KURULUM") != "1" {
		t.Skip("GOSP_GERCEK_KURULUM=1 verilmedi")
	}
	alanAdi := os.Getenv("GOSP_TEST_DOMAIN")
	if alanAdi == "" {
		t.Skip("GOSP_TEST_DOMAIN verilmedi")
	}
	cert, key, kapsam, atlanan, err := MailSertifikaAl(alanAdi, os.Getenv("GOSP_TEST_SK"))
	t.Logf("kapsam (SAN'a giren) : %v", kapsam)
	t.Logf("atlanan (SAN'dan çıkarılan): %v", atlanan)
	if err != nil {
		t.Fatalf("MailSertifikaAl: %v", err)
	}
	t.Logf("cert=%s key=%s", cert, key)
	if !dosyaVarPv(cert) || !dosyaVarPv(key) {
		t.Fatalf("sertifika dosyaları yok")
	}
	if err := WebmailVhostDomainYaz(alanAdi, cert, key); err != nil {
		t.Fatalf("WebmailVhostDomainYaz: %v", err)
	}
	t.Logf("webmail vhost'u GERÇEK sertifikaya geçirildi")
	if err := MailSertifikaGonder(alanAdi, cert, key); err != nil {
		t.Errorf("MailSertifikaGonder (mail yığınına kurulum): %v", err)
	} else {
		t.Logf("sertifika mail eklentisine (IMAP/SMTP) gönderildi")
	}
	if len(kapsam) < 2 {
		t.Logf("UYARI: SAN'da 2 ad beklenirken %d var — atlananlar: %s",
			len(kapsam), strings.Join(atlanan, "; "))
	}
}
