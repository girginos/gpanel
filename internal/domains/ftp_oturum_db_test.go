package domains

// ftp_oturum_db_test.go — FTP parola değişiminde açık oturum düşürme (handler katmanı):
// doğru hesaba çağrı, başarısız parola değişiminde çağrı YOK, düşürme hatasında parola
// değişimi YİNE başarılı + denetim ok=0. Gerçek pure-ftpd yok (ftpOturumDusur dikişi).

import (
	"errors"
	"strings"
	"testing"

	"girginospanel/internal/apianahtar"
)

func ftpOturumSahte(t *testing.T, n int, hata error) *[]string {
	t.Helper()
	var cagri []string
	e := ftpOturumDusur
	t.Cleanup(func() { ftpOturumDusur = e })
	ftpOturumDusur = func(k string) (int, error) { cagri = append(cagri, k); return n, hata }
	return &cagri
}

func TestFTPOturumDus_ParolaSonrasiDogruHesap(t *testing.T) {
	db, h, _ := ap_kur(t)
	cagri := ftpOturumSahte(t, 2, nil)
	ftp := ap_anahtarEkle(t, db, 2, apianahtar.RolBayi, "ftp.parola:yaz")

	if y := ap_istek(t, h, ftp.yetki(), "/domains/20/ftp-accounts/c_bayi2/password", map[string]string{"password": apGuclu}); y.kod != 200 {
		t.Fatalf("parola: %d %s", y.kod, y.ham)
	}
	if len(*cagri) != 1 || (*cagri)[0] != "c_bayi2" {
		t.Fatalf("yalnız c_bayi2 düşürülmeli: %v", *cagri)
	}
	var detay string
	var okv int
	if err := db.QueryRow(`SELECT COALESCE(detail,''), ok FROM audit_log WHERE action='ftp.oturum_dusur'`).Scan(&detay, &okv); err != nil {
		t.Fatalf("denetim yok: %v", err)
	}
	if okv != 1 || !strings.Contains(detay, "c_bayi2") || !strings.Contains(detay, "sonlandirilan=2") {
		t.Fatalf("denetim: ok=%d detay=%q", okv, detay)
	}

	// Ana FTP ucu (ftp/password) da AYNI noktadan, ana hesaba.
	*cagri = nil
	if y := ap_istek(t, h, ftp.yetki(), "/domains/20/ftp/password", map[string]string{"password": apGuclu + "9"}); y.kod != 200 {
		t.Fatalf("ana uç: %d %s", y.kod, y.ham)
	}
	if len(*cagri) != 1 || (*cagri)[0] != "c_bayi2" {
		t.Fatalf("ana uç düşürme: %v", *cagri)
	}
}

func TestFTPOturumDus_BasarisizParolaDegisimindeDusurmeYok(t *testing.T) {
	db, h, _ := ap_kur(t)
	cagri := ftpOturumSahte(t, 1, nil)
	ftp := ap_anahtarEkle(t, db, 2, apianahtar.RolBayi, "ftp.parola:yaz")
	// Başkasının hesabı (IDOR): 404; hiçbir oturum düşürülmez.
	if y := ap_istek(t, h, ftp.yetki(), "/domains/20/ftp-accounts/c_kurban/password", map[string]string{"password": apGuclu}); y.kod != 404 {
		t.Fatalf("başka hesap 404 olmalı: %d %s", y.kod, y.ham)
	}
	// Zayıf parola: 400; düşürme yok.
	if y := ap_istek(t, h, ftp.yetki(), "/domains/20/ftp-accounts/c_bayi2/password", map[string]string{"password": "1"}); y.kod != 400 {
		t.Fatalf("zayıf parola 400 olmalı: %d %s", y.kod, y.ham)
	}
	if len(*cagri) != 0 {
		t.Fatalf("başarısız değişimde oturum düşürülmemeli: %v", *cagri)
	}
}

func TestFTPOturumDus_DusurmeHatasiParolayiBozmaz(t *testing.T) {
	db, h, _ := ap_kur(t)
	cagri := ftpOturumSahte(t, 0, errors.New("pure-ftpwho: yok"))
	ftp := ap_anahtarEkle(t, db, 2, apianahtar.RolBayi, "ftp.parola:yaz")
	eski, _ := ap_ftpHash(t, db, "c_bayi2")

	if y := ap_istek(t, h, ftp.yetki(), "/domains/20/ftp-accounts/c_bayi2/password", map[string]string{"password": apGuclu}); y.kod != 200 {
		t.Fatalf("düşürme hatası parola değişimini bozmamalı: %d %s", y.kod, y.ham)
	}
	if yeni, _ := ap_ftpHash(t, db, "c_bayi2"); yeni == eski {
		t.Fatal("parola değişmeli")
	}
	if len(*cagri) != 1 {
		t.Fatalf("düşürme denenmeliydi: %v", *cagri)
	}
	var detay string
	var okv int
	if err := db.QueryRow(`SELECT COALESCE(detail,''), ok FROM audit_log WHERE action='ftp.oturum_dusur'`).Scan(&detay, &okv); err != nil {
		t.Fatalf("hata denetime yazılmalı: %v", err)
	}
	if okv != 0 || !strings.Contains(detay, "hata=") {
		t.Fatalf("denetim ok=0 + hata bekleniyordu: ok=%d detay=%q", okv, detay)
	}
	// Parola ana işlem kaydı hâlâ başarılı.
	var ana int
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='ftp.parola_api' AND ok=1`).Scan(&ana)
	if ana != 1 {
		t.Fatalf("ana denetim kaydı başarılı olmalı: %d", ana)
	}
}
