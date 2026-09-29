package hedef

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"strings"
	"testing"

	"girginospanel/internal/agkapi"
)

const nobetciSir = "N0betci-Sir-7f3a9c"

// Error() YALNIZ işlem + kategori döndürür; ham metin ve sır ASLA.
func TestHata_ErrorYalnizKategori(t *testing.T) {
	ham := errors.New("lftp: open -u \"yedek\",\"" + nobetciSir + "\" sftp://depo.ornek:22: Login failed: 530 Login incorrect")
	h := YeniHata(KategoriKimlik, "yukle", ham, nobetciSir)
	if got := h.Error(); got != "hedef yukle: kimlik" {
		t.Fatalf("Error() = %q", got)
	}
	if strings.Contains(h.Error(), nobetciSir) || strings.Contains(h.Error(), "depo.ornek") {
		t.Fatal("Error() ham metin sızdırdı")
	}
	if strings.Contains(h.Ayrinti(), nobetciSir) {
		t.Fatalf("Ayrinti maskelenmedi: %s", h.Ayrinti())
	}
	if !strings.Contains(h.Ayrinti(), "Login failed") {
		t.Errorf("Ayrinti tanı bilgisini kaybetti: %s", h.Ayrinti())
	}
	// İşlem adı olarak ham metin verilse bile Error()'a girmez.
	h2 := YeniHata(KategoriAg, "yukle "+nobetciSir, ham)
	if strings.Contains(h2.Error(), nobetciSir) || h2.Error() != "hedef: ag" {
		t.Errorf("geçersiz işlem adı Error()'a girdi: %q", h2.Error())
	}
	// Tanımsız kategori → bilinmeyen.
	if YeniHata("uydurma", "sil", nil).Kategori != KategoriBilinmeyen {
		t.Error("tanımsız kategori bilinmeyen'e düşmeliydi")
	}
	// Unwrap yok: errors.Unwrap ham hataya ulaşamaz.
	if errors.Unwrap(h) != nil {
		t.Error("Hata Unwrap ile ham nedeni açmamalı")
	}
}

var errNobetci = errors.New("nöbetçi")

func TestHata_IsNedeneIletir(t *testing.T) {
	h := YeniHata(KategoriBilinmeyen, "sil", fmt.Errorf("sar: %w", errNobetci))
	if !errors.Is(h, errNobetci) {
		t.Error("errors.Is iç nedene ulaşmalı")
	}
	iptal := Sarmala("yukle", fmt.Errorf("put: %w", context.Canceled))
	if !errors.Is(iptal, context.Canceled) {
		t.Error("iptal errors.Is ile tanınmalı")
	}
}

func TestSarmala(t *testing.T) {
	if Sarmala("yukle", nil) != nil {
		t.Fatal("nil → nil olmalı")
	}
	yalin := YeniHata(KategoriKota, "", errors.New("quota"))
	s := Sarmala("yukle", yalin)
	var h *Hata
	if !errors.As(s, &h) || h.Kategori != KategoriKota || h.Islem != "yukle" {
		t.Fatalf("yalın Hata işlem adı almalıydı: %v", s)
	}
	if yalin.Islem != "" {
		t.Error("Sarmala girdiyi değiştirmemeli")
	}
	// *Hata'yı saran dış metin (sır içeren) Error()'a SIZMAZ.
	dis := fmt.Errorf("yukleme %s basarisiz: %w", nobetciSir, YeniHata(KategoriKimlik, "yukle", nil))
	s2 := Sarmala("yukle", dis, nobetciSir)
	if strings.Contains(s2.Error(), nobetciSir) || KategoriAl(s2) != KategoriKimlik {
		t.Fatalf("dış metin sızdı ya da kategori kayboldu: %q", s2.Error())
	}
	if !errors.As(s2, &h) || strings.Contains(h.Ayrinti(), nobetciSir) {
		t.Fatalf("ayrıntı maskelenmedi: %q", h.Ayrinti())
	}
	// Düz hata sınıflanır ve maskelenir.
	s3 := Sarmala("test", errors.New("Login failed "+nobetciSir), nobetciSir)
	if KategoriAl(s3) != KategoriKimlik || strings.Contains(s3.Error(), nobetciSir) {
		t.Fatalf("düz hata: %q", s3.Error())
	}
}

func TestSinifla(t *testing.T) {
	opRed := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: connection refused")}
	durumlar := []struct {
		err error
		k   Kategori
	}{
		// 181'de lftp 4.9.2 ile alınmış gerçek çıktılar (hazirlik_hata_test.go).
		{errors.New("uzaktan indirme: cd: Access failed: 550 Failed to change directory. (/admin-yedek-deposu/sunucu181/2026-09-24)"), KategoriBulunamadi},
		{errors.New("uzaktan indirme: open: yedek-deposu.gecersiz-ornek.invalid: Name or service not known"), KategoriAg},
		{errors.New("lftp: Login failed: 530 Login incorrect."), KategoriKimlik},
		{errors.New("user@host: Permission denied (publickey,password)."), KategoriKimlik},
		{errors.New("curl: (67) Access denied: 530"), KategoriKimlik},
		{errors.New("get: Access failed: 550 Failed to open file. (c_x.tar.gz)"), KategoriBulunamadi},
		{errors.New("put: Access failed: 553 Could not create file. (Permission denied)"), KategoriErisim},
		{errors.New("Host key verification failed."), KategoriTLS},
		{errors.New("Fatal error: Certificate verification: Not trusted"), KategoriTLS},
		{errors.New("put: Access failed: 552 Disk quota exceeded"), KategoriKota},
		{errors.New("421 Too many connections (8) from this IP"), KategoriOran},
		{errors.New("The specified key does not exist. (NoSuchKey)"), KategoriBulunamadi},
		{errors.New("InvalidAccessKeyId: The AWS Access Key Id you provided does not exist"), KategoriKimlik},
		{errors.New("SlowDown: Please reduce your request rate"), KategoriOran},
		{errors.New(`{"error": "invalid_grant", "error_description": "Token has been expired or revoked."}`), KategoriKimlik},
		{errors.New("path/not_found/.."), KategoriBulunamadi},
		{errors.New("storageQuotaExceeded"), KategoriKota},
		{errors.New("STATUS_LOGON_FAILURE"), KategoriKimlik},
		{errors.New("tamamen yeni bir hata"), KategoriBilinmeyen},
		{fmt.Errorf("dial: %w", agkapi.ErrReddedildi), KategoriErisim},
		{context.DeadlineExceeded, KategoriAg},
		{context.Canceled, KategoriBilinmeyen},
		{fmt.Errorf("stat: %w", fs.ErrNotExist), KategoriBulunamadi},
		{fmt.Errorf("open: %w", fs.ErrPermission), KategoriErisim},
		{x509.UnknownAuthorityError{}, KategoriTLS},
		{&net.DNSError{Err: "no such host", Name: "x.invalid"}, KategoriAg},
		{opRed, KategoriAg},
		{YeniHata(KategoriOran, "yukle", nil), KategoriOran},
		{nil, ""},
	}
	for _, d := range durumlar {
		if got := Sinifla(d.err); got != d.k {
			t.Errorf("Sinifla(%v) = %q, beklenen %q", d.err, got, d.k)
		}
	}
	// Gerçek SSRF reddi (agkapi'den) erişim kategorisine düşer.
	_, err := agkapi.Yeni(agkapi.Secenek{}).DialContext(context.Background(), "tcp", "169.254.169.254:80")
	if Sinifla(err) != KategoriErisim {
		t.Errorf("ssrf reddi: %v → %q", err, Sinifla(err))
	}
}

func TestHTTPKategori(t *testing.T) {
	durumlar := map[int]Kategori{401: KategoriKimlik, 403: KategoriErisim, 404: KategoriBulunamadi, 410: KategoriBulunamadi,
		429: KategoriOran, 507: KategoriKota, 413: KategoriKota, 500: KategoriAg, 503: KategoriAg, 408: KategoriAg,
		400: KategoriBilinmeyen, 409: KategoriBilinmeyen}
	for kod, k := range durumlar {
		if got := HTTPKategori(kod); got != k {
			t.Errorf("HTTPKategori(%d) = %q, beklenen %q", kod, got, k)
		}
	}
	for _, k := range Kategoriler() {
		if !k.Gecerli() || k.Aciklama() == "" {
			t.Errorf("kategori tanımsız: %q", k)
		}
	}
	if Kategori("x").Aciklama() != KategoriBilinmeyen.Aciklama() {
		t.Error("tanımsız kategori açıklaması bilinmeyen olmalı")
	}
}
