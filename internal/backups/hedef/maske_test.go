package hedef

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMaskele_BilinenSirVeBicimleri(t *testing.T) {
	sir := `p@ss "w0rd"\ ğ/+&=`
	metinler := []string{
		"open -u \"yedek\",\"" + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(sir) + "\" sftp://h",
		"GET /x?pw=" + url.QueryEscape(sir),
		"path /" + url.PathEscape(sir),
		`{"parola_alani":"p@ss \"w0rd\"\\ ğ/+&="}`,
		"düz: " + sir + " sonu",
	}
	for _, m := range metinler {
		got := Maskele(m, sir)
		if strings.Contains(got, "w0rd") {
			t.Errorf("sır sızdı:\n girdi: %s\n çıktı: %s", m, got)
		}
		if !strings.Contains(got, MaskeIsareti) {
			t.Errorf("maske işareti yok: %s", got)
		}
	}
	// Boş/boşluk sır metni bozmamalı.
	if got := Maskele("a b c", "", "  "); got != "a b c" {
		t.Errorf("boş sır metni değiştirdi: %q", got)
	}
	// Bir sır diğerinin öneki: uzun olan tamamen maskelenir.
	if got := Maskele("x=abcdef", "abc", "abcdef"); strings.Contains(got, "def") {
		t.Errorf("uzun sır önce maskelenmeliydi: %q", got)
	}
}

// Sahte sır parçaları çalışma anında birleştirilir: kaynakta sır biçimli bir
// dizge durmasın (gitleaks bu test verisini gerçek sır sanıp commit'i durdurur).
func sahte(parcalar ...string) string { return strings.Join(parcalar, "") }

func TestMaskele_Desenler(t *testing.T) {
	var (
		yenileme  = sahte("1/", "/", "0gSAHTEsahteSAHTEsahte")
		istemci   = sahte("GOCSPX", "-", "sahteistemci")
		anahtarID = sahte("AKIA", "SAHTESAHTESAHTE0")
		ozelBas   = sahte("-----BEGIN ", "OPENSSH ", "PRIVATE ", "KEY-----")
		ozelSon   = sahte("-----END ", "OPENSSH ", "PRIVATE ", "KEY-----")
		pkcs8Bas  = sahte("-----BEGIN ", "PRIVATE ", "KEY-----")
		ageAnahtr = sahte("AGE-SECRET-", "KEY-1", strings.Repeat("Q", 40))
	)
	durumlar := []struct{ girdi, yasak string }{
		{"dial sftp://kullanici:Gizli123@depo.ornek:22 failed", "Gizli123"},
		{"Authorization: AWS4-HMAC-SHA256 Credential=" + anahtarID + "/20260929/us-east-1/s3/aws4_request, Signature=deadbeef", "deadbeef"},
		{"Authorization: Basic dXNlcjpwYXNz", "dXNlcjpwYXNz"},
		{"istek başlığı Bearer sahte.jeton.degeri reddedildi", "sahte.jeton"},
		{`POST /token {"refresh_token":"` + yenileme + `","client_secret":"` + istemci + `"}`, "sahteistemci"},
		{`POST /token {"refresh_token":"` + yenileme + `"}`, "SAHTEsahte"},
		{"https://s3.ornek/b/k?X-Amz-Signature=abcdef0123&X-Amz-Credential=" + anahtarID + "%2F2026", "abcdef0123"},
		{"access key " + anahtarID + " geçersiz", anahtarID},
		{"GET /oauth-donus?code=4/0SahteKod&state=xyz", "0SahteKod"},
		{"password=hunter2 user=x", "hunter2"},
		{"şifre: çokgizli123 yanlış", "çokgizli123"},
		{"anahtar " + ageAnahtr + " sızdı", "QQQQQQQQQQ"},
		{"jeton " + sahte("ya29", ".", "SahteErisim-_") + " geçersiz", "SahteErisim"},
		{"jeton " + sahte("sl", ".", "SAHTEkisaomurlujetondegeri") + " geçersiz", "SAHTEkisaomurlu"},
		{ozelBas + "\nc2FodGUtYW5haHRhcg\n" + ozelSon, "c2FodGUtYW5haHRhcg"},
		{pkcs8Bas + "\nc2FodGUta2VzaWs (kesik)", "c2FodGUta2VzaWs"},
	}
	for _, d := range durumlar {
		if got := Maskele(d.girdi); strings.Contains(got, d.yasak) {
			t.Errorf("desen maskelemedi:\n girdi: %s\n çıktı: %s", d.girdi, got)
		}
	}
	// Tanı değeri olan zararsız metin korunur.
	zararsiz := "put: Access failed: 550 Failed to change directory. (/yedek/2026-09-24)"
	if got := Maskele(zararsiz); got != zararsiz {
		t.Errorf("zararsız metin değişti: %q", got)
	}
}

func TestMaskele_KontrolKarakteriVeKesme(t *testing.T) {
	got := Maskele("satir1\r\nsatir2\tsekme\x00nul\x1b[31m")
	if strings.ContainsAny(got, "\r\n\t\x00\x1b") {
		t.Errorf("kontrol karakteri kaldı: %q", got)
	}
	uzun := strings.Repeat("ğ", AzamiAyrinti+100)
	got = Maskele(uzun)
	if utf8.RuneCountInString(got) != AzamiAyrinti+1 || !strings.HasSuffix(got, "…") || !utf8.ValidString(got) {
		t.Errorf("kesme yanlış: %d rune", utf8.RuneCountInString(got))
	}
	// Maskeleme kesmeden ÖNCE: sınırda duran sır kısmen de sızmaz.
	sir := "SINIRDAKI-SIR-0123456789"
	metin := strings.Repeat("x", AzamiAyrinti-10) + sir
	if got := Maskele(metin, sir); strings.Contains(got, "SINIRDAK") {
		t.Errorf("sınırdaki sır kısmen sızdı: %q", got[len(got)-30:])
	}
	if MaskeleHata(nil) != "" {
		t.Error("nil hata boş dönmeli")
	}
	if got := MaskeleHata(errors.New("parola=abc"), "abc"); strings.Contains(got, "abc") {
		t.Errorf("MaskeleHata: %q", got)
	}
}
