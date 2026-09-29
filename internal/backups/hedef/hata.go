package hedef

// hata.go — uzak hedef hatalarının KATEGORİLİ temsili.
//
// 🔴 NEDEN: bugün lftp/curl çıktısı olduğu gibi son_hata'ya, bildirime ve test
// yanıtına yazılıyor (ör. 400 karakterlik çıktı, host/dizin şeması). Hedef
// hataları artık tek tiptir: Error() YALNIZ sabit bir işlem adı ve kategori
// döndürür ("hedef yukle: kimlik"); ham metin maskelenip Ayrinti()'de kalır ve
// yalnız sunucu günlüğüne yazılır. Unwrap BİLEREK yok: errors.Unwrap ile ham
// hataya (ve onun Error() metnine) ulaşılamasın. errors.Is ise çalışır
// (context.Canceled, ErrLftpYok gibi nöbetçiler için).

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"io/fs"
	"net"
	"regexp"
	"strings"

	"girginospanel/internal/agkapi"
)

// Kategori — kullanıcıya gösterilebilen hata sınıfı.
type Kategori string

const (
	KategoriKimlik     Kategori = "kimlik"
	KategoriErisim     Kategori = "erisim"
	KategoriBulunamadi Kategori = "bulunamadi"
	KategoriAg         Kategori = "ag"
	KategoriTLS        Kategori = "tls"
	KategoriKota       Kategori = "kota"
	KategoriOran       Kategori = "oran"
	KategoriBilinmeyen Kategori = "bilinmeyen"
)

var kategoriAciklama = map[Kategori]string{
	KategoriKimlik:     "kimlik doğrulanamadı (kullanıcı adı, parola, erişim anahtarı ya da yetki jetonu)",
	KategoriErisim:     "erişim reddedildi (izin yok ya da adres engelli)",
	KategoriBulunamadi: "uzak dosya ya da dizin bulunamadı",
	KategoriAg:         "ağ hatası (bağlantı kurulamadı, zaman aşımı ya da ad çözülemedi)",
	KategoriTLS:        "TLS sertifikası ya da sunucu anahtarı doğrulanamadı",
	KategoriKota:       "uzak depoda yer ya da kota yetersiz",
	KategoriOran:       "sağlayıcının istek sınırına takıldı; daha sonra yeniden denenecek",
	KategoriBilinmeyen: "bilinmeyen hata (ayrıntı sunucu günlüğünde)",
}

// Kategoriler — tüm kategoriler (sabit sıra).
func Kategoriler() []Kategori {
	return []Kategori{KategoriKimlik, KategoriErisim, KategoriBulunamadi, KategoriAg,
		KategoriTLS, KategoriKota, KategoriOran, KategoriBilinmeyen}
}

// Gecerli — tanımlı bir kategori mi?
func (k Kategori) Gecerli() bool { _, ok := kategoriAciklama[k]; return ok }

// Aciklama — kullanıcıya gösterilecek Türkçe açıklama.
func (k Kategori) Aciklama() string {
	if a, ok := kategoriAciklama[k]; ok {
		return a
	}
	return kategoriAciklama[KategoriBilinmeyen]
}

// islemRe — Error()'a girebilen işlem adı: yalnız küçük harf sabit sözcük
// (çağıran yanlışlıkla ham metin verirse Error()'a sızmasın).
var islemRe = regexp.MustCompile(`^[a-z][a-z_]{0,23}$`)

// Hata — kategorili uzak hedef hatası.
type Hata struct {
	Kategori Kategori
	Islem    string // "yukle", "indir", "listele", "sil", "test" ...
	ayrinti  string // maskelenmiş ham metin — YALNIZ günlük için
	neden    error  // errors.Is için; dışarı açılmaz
}

// YeniHata — kategorisi bilinen hata. neden'in metni sirlar ile maskelenir.
func YeniHata(k Kategori, islem string, neden error, sirlar ...string) *Hata {
	if !k.Gecerli() {
		k = KategoriBilinmeyen
	}
	if !islemRe.MatchString(islem) {
		islem = ""
	}
	return &Hata{Kategori: k, Islem: islem, ayrinti: MaskeleHata(neden, sirlar...), neden: neden}
}

// Error — YALNIZ işlem + kategori; ham metin ASLA.
func (h *Hata) Error() string {
	if h.Islem != "" {
		return "hedef " + h.Islem + ": " + string(h.Kategori)
	}
	return "hedef: " + string(h.Kategori)
}

// Ayrinti — maskelenmiş ham metin (sunucu günlüğü için; kullanıcıya gösterme).
func (h *Hata) Ayrinti() string { return h.ayrinti }

// Is — errors.Is'i iç nedene iletir (Unwrap olmadan).
func (h *Hata) Is(hedef error) bool {
	return h.neden != nil && errors.Is(h.neden, hedef)
}

// Sarmala — herhangi bir hatayı *Hata'ya çevirir (nil → nil). err zaten yalın
// bir *Hata ise aynen döner; *Hata'yı SARAN bir hata ise (fmt.Errorf("...%w"))
// dış metin sızmasın diye yeni *Hata kurulur (kategori korunur, ayrıntı dış
// metnin maskelenmiş hali olur). Diğer hatalar Sinifla ile sınıflanır.
func Sarmala(islem string, err error, sirlar ...string) error {
	if err == nil {
		return nil
	}
	if h, ok := err.(*Hata); ok {
		if h.Islem == "" && islemRe.MatchString(islem) {
			c := *h
			c.Islem = islem
			return &c
		}
		return h
	}
	var ic *Hata
	if errors.As(err, &ic) {
		if !islemRe.MatchString(islem) {
			islem = ic.Islem
		}
		return YeniHata(ic.Kategori, islem, err, sirlar...)
	}
	return YeniHata(Sinifla(err), islem, err, sirlar...)
}

// KategoriAl — hatanın kategorisi (nil → "").
func KategoriAl(err error) Kategori {
	if err == nil {
		return ""
	}
	var h *Hata
	if errors.As(err, &h) {
		return h.Kategori
	}
	return Sinifla(err)
}

// HTTPKategori — HTTP durum kodundan kategori (REST tabanlı hedefler için).
func HTTPKategori(kod int) Kategori {
	switch {
	case kod == 401:
		return KategoriKimlik
	case kod == 403:
		return KategoriErisim
	case kod == 404 || kod == 410:
		return KategoriBulunamadi
	case kod == 429:
		return KategoriOran
	case kod == 507 || kod == 413:
		return KategoriKota
	case kod == 408 || kod >= 500:
		return KategoriAg
	}
	return KategoriBilinmeyen
}

// metinKurallari — tür bilgisi taşımayan hatalar (alt süreç çıktısı, sağlayıcı
// hata kodları) için küçük harf alt dizge kuralları. SIRA ÖNEMLİ: ilk eşleşen
// kazanır ("550 failed to change directory" bulunamadı, "access failed:
// permission denied" erişim).
var metinKurallari = []struct {
	k        Kategori
	dizgeler []string
}{
	{KategoriTLS, []string{"x509", "tls:", "certificate", "handshake", "host key", "remote host identification"}},
	{KategoriKimlik, []string{"login failed", "login incorrect", "authentication failed", "auth fail",
		"unauthorized", "invalid_grant", "invalid_client", "invalidaccesskeyid", "signaturedoesnotmatch",
		"permission denied (", "logon failure", "logon_failure", "expired_access_token",
		"invalid_access_token", "wrong password", "bad password", "login denied", "curl: (67)"}},
	{KategoriKota, []string{"quota", "no space left", "insufficient_space", "insufficient storage",
		"exceeded storage allocation", "entitytoolarge", "disk full"}},
	{KategoriOran, []string{"rate limit", "ratelimit", "too many requests", "too_many_requests",
		"too_many_write_operations", "slowdown", "slow down", "throttl", "too many connections"}},
	{KategoriBulunamadi, []string{"no such file", "no such key", "nosuchkey", "nosuchbucket", "not found",
		"does not exist", "object_name_not_found", "failed to change directory", "path/not_found",
		"failed to open file"}},
	{KategoriErisim, []string{"permission denied", "access denied", "accessdenied", "forbidden",
		"access failed", "operation not permitted", "access_denied", "insufficient_permissions", "ssrf-guard"}},
	{KategoriAg, []string{"connection refused", "connection reset", "timed out", "timeout", "no route to host",
		"network is unreachable", "name or service not known", "no such host", "could not resolve",
		"temporary failure in name resolution", "broken pipe", "unexpected eof", ": eof", "cannot connect",
		"connection closed", "server misbehaving", "dial tcp"}},
}

// Sinifla — hatayı kategoriye ayırır: önce tür bilgisi (errors.Is/As), sonra
// metin kuralları; hiçbiri tutmazsa KategoriBilinmeyen.
func Sinifla(err error) Kategori {
	if err == nil {
		return ""
	}
	var h *Hata
	if errors.As(err, &h) {
		return h.Kategori
	}
	if errors.Is(err, agkapi.ErrReddedildi) {
		return KategoriErisim
	}
	if errors.Is(err, context.Canceled) {
		return KategoriBilinmeyen
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.ErrUnexpectedEOF) {
		return KategoriAg
	}
	if errors.Is(err, fs.ErrNotExist) {
		return KategoriBulunamadi
	}
	if errors.Is(err, fs.ErrPermission) {
		return KategoriErisim
	}
	var (
		bilinmeyenKok x509.UnknownAuthorityError
		adUyusmaz     x509.HostnameError
		gecersizSert  x509.CertificateInvalidError
		dogrulama     *tls.CertificateVerificationError
		kayitBasligi  tls.RecordHeaderError
	)
	if errors.As(err, &bilinmeyenKok) || errors.As(err, &adUyusmaz) || errors.As(err, &gecersizSert) ||
		errors.As(err, &dogrulama) || errors.As(err, &kayitBasligi) {
		return KategoriTLS
	}
	// Metin kuralları genel ağ türlerinden ÖNCE: sağlayıcı istemcileri kimlik/kota
	// hatalarını da net.Error'u uygulayan tiplerle sarabiliyor; özgül metin,
	// "bir ağ hatası oldu" bilgisinden daha doğru kategori verir.
	m := strings.ToLower(err.Error())
	for _, kural := range metinKurallari {
		for _, d := range kural.dizgeler {
			if strings.Contains(m, d) {
				return kural.k
			}
		}
	}
	var (
		dnsHata *net.DNSError
		opHata  *net.OpError
		agHata  net.Error
	)
	if errors.As(err, &dnsHata) || errors.As(err, &opHata) || errors.As(err, &agHata) {
		return KategoriAg
	}
	return KategoriBilinmeyen
}
