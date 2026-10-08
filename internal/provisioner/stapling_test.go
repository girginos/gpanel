package provisioner

import (
	"strings"
	"testing"
)

// Eski global TLS politikası (stapling on) göçte kapatılır: nginx -t'de OCSP URL'i olmayan
// (Let's Encrypt 2025 sonrası, kendinden imzalı) her sertifika için uyarı basılmasın.
func TestStaplingKapat(t *testing.T) {
	eski := `ssl_session_tickets off;
# OCSP stapling: el sikismayi hizlandirir, istemcinin CA'ya gitmesini onler.
ssl_stapling on;
ssl_stapling_verify on;
resolver 1.1.1.1 8.8.8.8 valid=300s ipv6=off;
resolver_timeout 5s;
`
	yeni, degisti := staplingKapat(eski)
	if !degisti {
		t.Fatal("eski politika değişmedi olarak döndü")
	}
	if strings.Contains(yeni, "ssl_stapling on;") || strings.Contains(yeni, "ssl_stapling_verify") {
		t.Errorf("stapling hâlâ açık:\n%s", yeni)
	}
	if !strings.Contains(yeni, "ssl_stapling off;") {
		t.Errorf("ssl_stapling off yok:\n%s", yeni)
	}
	// Diğer satırlara dokunulmadı.
	for _, s := range []string{"ssl_session_tickets off;", "resolver 1.1.1.1 8.8.8.8 valid=300s ipv6=off;", "resolver_timeout 5s;"} {
		if !strings.Contains(yeni, s) {
			t.Errorf("satır kayboldu: %s", s)
		}
	}
	// İdempotent: ikinci kez değişiklik yok.
	if _, tekrar := staplingKapat(yeni); tekrar {
		t.Error("ikinci çalıştırmada yine değişti (idempotent değil)")
	}
}

// Elle düzenlenmiş biçimler de yakalanır (fazla boşluk, satır sonu yorumu); yorum satırı ve
// vhost'a özgü satırlar korunur.
func TestStaplingKapat_BicimVaryantlari(t *testing.T) {
	girdi := "ssl_stapling   on ;  # elle\n  ssl_stapling_verify  on;\n# ssl_stapling on;\n"
	yeni, degisti := staplingKapat(girdi)
	if !degisti {
		t.Fatal("varyantlar yakalanmadı")
	}
	if strings.Contains(yeni, "ssl_stapling_verify") {
		t.Errorf("verify kalmış:\n%s", yeni)
	}
	if !strings.Contains(yeni, "\nssl_stapling off;") && !strings.HasPrefix(yeni, "# OCSP") {
		t.Errorf("off satırı yok:\n%s", yeni)
	}
	if !strings.Contains(yeni, "# ssl_stapling on;") {
		t.Errorf("yorum satırı değişmemeliydi:\n%s", yeni)
	}
}
