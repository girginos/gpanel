package subdomain

// ssl_hedef.go — hesap düzeyinde SSL hedef listesi (ana + ek + alt alan adları).
//
// GET /domains/{id}/ssl/hedefler: tek istekte hesabın TÜM alan adlarının SSL durumu.
// SALT-OKUNUR: kurulum/kaldırma mevcut üç uçta kalır (domains/{id}/ssl*, subdomain/{sid}/ssl,
// ek-alan-adlari/{eid}/ssl); arayüz hedefin türüne göre ilgili uca çağrı yapar.
//
// Güvenlik: hedefler yalnız {id} hesabının DB kayıtlarından gelir (başka hesabın ek/alt
// kimliği listede olamaz). Sertifika dosyası yalnız beklenen kök altında, sembolik bağ
// izlemeden (her yol bileşeni Lstat, O_NOFOLLOW) ve boyut sınırıyla okunur; yalnız tür,
// bitiş ve SAN kapsamı çıkarılır — ham içerik, anahtar ve dosya yolu yanıta GİRMEZ.
// DNS hazırlığı yalnız DB'deki alan adları için, kamu çözümleyiciye, zaman aşımlı,
// önbellekli (60 sn) ve eşzamanlılığı sınırlı sorulur (kullanıcı girdisi dış istek üretmez).

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"girginospanel/internal/dnsutil"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"
	"girginospanel/internal/provisioner"
)

const (
	sslYaklasanGun  = 14
	sslSertMaxBayt  = 256 << 10
	sslAcmeKayitMax = 64 << 10
	// acme.sh planlı yenileme zamanından bu kadar gün sonra hâlâ yenilenmediyse "gecikti".
	sslYenilemeTolGun = 1
)

// Test dikişleri.
var (
	anaSertifikaKoku = "/etc/pki/girginospanel" // provisioner.certSystemBaseDir ile aynı
	acmeKayitKoku    = provisioner.AcmeConfigHome
	sslSimdi         = time.Now
	dnsCozucu        = dnsutil.Cozumle
	dnsBekle         = 800 * time.Millisecond
	dnsTTL           = 60 * time.Second
)

// SSLHedef — listedeki bir alan adı.
type SSLHedef struct {
	Tur              string   `json:"tur"` // ana | ek | alt
	AlanAdi          string   `json:"alan_adi"`
	EkAlanID         *int64   `json:"ek_alan_id,omitempty"`
	AltAlanID        *int64   `json:"alt_alan_id,omitempty"`
	SSLAktif         bool     `json:"ssl_aktif"`
	SertifikaTuru    *string  `json:"tur_sertifika"` // letsencrypt | oz-imza | bilinmiyor | null (SSL yok)
	Bitis            string   `json:"bitis,omitempty"`
	KalanGun         *int     `json:"kalan_gun"`
	Durum            string   `json:"durum"` // aktif | yok | yaklasiyor | doldu | okunamadi
	OtomatikYenileme bool     `json:"otomatik_yenileme"`
	SonrakiYenileme  string   `json:"sonraki_yenileme,omitempty"`
	YenilemeGecikti  bool     `json:"yenileme_gecikti"`
	Kapsam           []string `json:"kapsam"` // apex | www
	DNSHazir         *bool    `json:"dns_hazir"`
	Askida           bool     `json:"askida"`
	Demo             bool     `json:"demo"`
}

// ── güvenli sertifika okuma ────────────────────────────────────────────────

var errSertYol = errors.New("sertifika yolu güvenli değil")

// sertDosyaOku — yol, kok altında olmalı; kok'tan sonraki HER bileşen sembolik bağ
// olmamalı; dosya düzgün (regular) ve max bayt içinde olmalı; O_NOFOLLOW ile açılır.
func sertDosyaOku(kok, yol string, max int64) ([]byte, error) {
	kok, yol = filepath.Clean(kok), filepath.Clean(yol)
	sep := string(filepath.Separator)
	if !strings.HasPrefix(yol, kok+sep) {
		return nil, errSertYol
	}
	cur := kok
	for _, bilesen := range strings.Split(strings.TrimPrefix(yol, kok+sep), sep) {
		if bilesen == "" || bilesen == "." || bilesen == ".." {
			return nil, errSertYol
		}
		cur = filepath.Join(cur, bilesen)
		fi, err := os.Lstat(cur)
		if err != nil {
			return nil, err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return nil, errSertYol
		}
		if cur != yol && !fi.IsDir() {
			return nil, errSertYol
		}
	}
	f, err := os.OpenFile(yol, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > max {
		return nil, errSertYol
	}
	return io.ReadAll(io.LimitReader(f, max+1))
}

// dosyaDuzgun — anahtar gibi OKUNMAYACAK dosyanın varlığı (Lstat; sembolik bağ sayılmaz).
func dosyaDuzgun(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode().IsRegular()
}

type sertOzet struct {
	Bitis time.Time
	Tur   string // letsencrypt | oz-imza | bilinmiyor
	SAN   []string
}

// sertAyristir — PEM'deki ilk CERTIFICATE (yaprak) özetlenir. Bozuk/boş → genel hata.
func sertAyristir(b []byte) (sertOzet, error) {
	for {
		var blk *pem.Block
		blk, b = pem.Decode(b)
		if blk == nil {
			return sertOzet{}, errors.New("sertifika ayrıştırılamadı")
		}
		if blk.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(blk.Bytes)
		if err != nil {
			return sertOzet{}, errors.New("sertifika ayrıştırılamadı")
		}
		o := sertOzet{Bitis: c.NotAfter.UTC(), Tur: "bilinmiyor", SAN: c.DNSNames}
		switch {
		case bytes.Equal(c.RawIssuer, c.RawSubject):
			o.Tur = "oz-imza"
		case icerir(c.Issuer.Organization, "Let's Encrypt") || strings.HasPrefix(c.Issuer.CommonName, "Let's Encrypt"):
			o.Tur = "letsencrypt"
		}
		return o, nil
	}
}

func icerir(s []string, v string) bool {
	for _, x := range s {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}

type sertOnbGiris struct {
	mod  time.Time
	boyt int64
	ozet sertOzet
}

var (
	sertOnbMu sync.Mutex
	sertOnb   = map[string]sertOnbGiris{}
)

// sertOzetle — kok altındaki sertifikayı (mtime+boyut önbellekli) özetler.
func sertOzetle(kok, yol string) (sertOzet, error) {
	fi, err := os.Lstat(yol)
	if err != nil {
		return sertOzet{}, err
	}
	sertOnbMu.Lock()
	g, ok := sertOnb[yol]
	sertOnbMu.Unlock()
	if ok && g.mod.Equal(fi.ModTime()) && g.boyt == fi.Size() {
		return g.ozet, nil
	}
	b, err := sertDosyaOku(kok, yol, sslSertMaxBayt)
	if err != nil {
		return sertOzet{}, err
	}
	o, err := sertAyristir(b)
	if err != nil {
		return sertOzet{}, err
	}
	sertOnbMu.Lock()
	if len(sertOnb) > 4096 {
		sertOnb = map[string]sertOnbGiris{}
	}
	sertOnb[yol] = sertOnbGiris{mod: fi.ModTime(), boyt: fi.Size(), ozet: o}
	sertOnbMu.Unlock()
	return o, nil
}

// sslDurumHesapla — bitişe göre durum + kalan gün (taban; geçmişte negatif).
func sslDurumHesapla(bitis, simdi time.Time) (string, int) {
	kalan := int(math.Floor(bitis.Sub(simdi).Hours() / 24))
	switch {
	case !bitis.After(simdi):
		return "doldu", kalan
	case kalan <= sslYaklasanGun:
		return "yaklasiyor", kalan
	}
	return "aktif", kalan
}

// acmeYenileme — acme.sh yenileme kaydı var mı + planlı yenileme zamanı.
func acmeYenileme(ad string) (bool, time.Time) {
	kok := acmeKayitKoku()
	for _, d := range []string{ad + "_ecc", ad} {
		b, err := sertDosyaOku(kok, filepath.Join(kok, d, ad+".conf"), sslAcmeKayitMax)
		if err != nil {
			continue
		}
		for _, s := range strings.Split(string(b), "\n") {
			s = strings.TrimSpace(s)
			if strings.HasPrefix(s, "Le_NextRenewTime=") {
				v := strings.Trim(strings.TrimPrefix(s, "Le_NextRenewTime="), `'"`)
				if u, e := strconv.ParseInt(v, 10, 64); e == nil && u > 0 {
					return true, time.Unix(u, 0).UTC()
				}
			}
		}
		return true, time.Time{}
	}
	return false, time.Time{}
}

// hedefDoldur — sertifika dosyasından türetilen alanları doldurur.
func hedefDoldur(t *SSLHedef, kok, crt string, aktif bool, varsayilanKapsam []string, simdi time.Time) {
	t.Kapsam = varsayilanKapsam
	t.Durum = "yok"
	if !aktif {
		return
	}
	t.SSLAktif = true
	o, err := sertOzetle(kok, crt)
	if err != nil {
		bil := "bilinmiyor"
		t.SertifikaTuru = &bil
		t.Durum = "okunamadi"
		return
	}
	tur := o.Tur
	t.SertifikaTuru = &tur
	t.Bitis = o.Bitis.Format("2006-01-02")
	d, k := sslDurumHesapla(o.Bitis, simdi)
	t.Durum, t.KalanGun = d, &k
	// Kapsam gerçek SAN'dan (apex = alan adının kendisi).
	var kap []string
	ad := strings.ToLower(t.AlanAdi)
	apexVar, wwwVar := false, false
	for _, s := range o.SAN {
		switch strings.ToLower(s) {
		case ad:
			apexVar = true
		case "www." + ad:
			wwwVar = true
		}
	}
	if apexVar {
		kap = append(kap, "apex")
	}
	if wwwVar {
		kap = append(kap, "www")
	}
	if kap != nil {
		t.Kapsam = kap
	}
	if tur == "letsencrypt" {
		if v, sonraki := acmeYenileme(t.AlanAdi); v {
			t.OtomatikYenileme = true
			if !sonraki.IsZero() {
				t.SonrakiYenileme = sonraki.Format("2006-01-02")
				t.YenilemeGecikti = simdi.After(sonraki.Add(sslYenilemeTolGun * 24 * time.Hour))
			}
		}
	}
}

// ── DNS hazırlığı (önbellekli, zaman aşımlı, eşzamanlılığı sınırlı) ─────────

type dnsGiris struct {
	v bool
	t time.Time
}

var (
	dnsMu     sync.Mutex
	dnsOnb    = map[string]dnsGiris{}
	dnsUcusta = map[string]bool{}
	dnsEsz    = make(chan struct{}, 4)
)

// dnsAra — tek ad: önbellek tazeyse hemen (değer, true); değilse kapasite varsa arka
// planda çözer ve bekleme kanalı döner; kapasite/uçuş varsa (false, false, nil).
func dnsAra(host string, ips []string) (bool, bool, <-chan bool) {
	anahtar := host + "|" + strings.Join(ips, ",")
	dnsMu.Lock()
	if g, ok := dnsOnb[anahtar]; ok && sslSimdi().Sub(g.t) < dnsTTL {
		dnsMu.Unlock()
		return g.v, true, nil
	}
	if dnsUcusta[anahtar] {
		dnsMu.Unlock()
		return false, false, nil
	}
	select {
	case dnsEsz <- struct{}{}:
	default:
		dnsMu.Unlock()
		return false, false, nil
	}
	dnsUcusta[anahtar] = true
	dnsMu.Unlock()
	ch := make(chan bool, 1)
	coz, saat := dnsCozucu, sslSimdi // dikişler çağıran goroutine'de okunur
	go func() {
		ok := false
		for _, a := range coz(host) {
			for _, ip := range ips {
				if ip != "" && a == ip {
					ok = true
				}
			}
		}
		dnsMu.Lock()
		if len(dnsOnb) > 4096 {
			dnsOnb = map[string]dnsGiris{}
		}
		dnsOnb[anahtar] = dnsGiris{v: ok, t: saat()}
		delete(dnsUcusta, anahtar)
		dnsMu.Unlock()
		<-dnsEsz
		ch <- ok
	}()
	return false, false, ch
}

// dnsKontrol — tüm adlar bu sunucu IP'lerinden birine çözülüyor mu? false = en az biri
// değil; nil = henüz bilinmiyor (sonraki istekte önbellekten gelir).
func dnsKontrol(adlar, ips []string) *bool {
	var bek []<-chan bool
	bilinmeyen, tum := false, true
	for _, a := range adlar {
		v, bil, ch := dnsAra(a, ips)
		switch {
		case bil:
			tum = tum && v
		case ch != nil:
			bek = append(bek, ch)
		default:
			bilinmeyen = true
		}
	}
	son := time.After(dnsBekle)
	for _, ch := range bek {
		select {
		case v := <-ch:
			tum = tum && v
		case <-son:
			bilinmeyen = true
		}
	}
	if !tum {
		f := false
		return &f
	}
	if bilinmeyen {
		return nil
	}
	t := true
	return &t
}

// GET /domains/{id}/ssl/hedefler
func (h *Handlers) SSLHedefler(w http.ResponseWriter, r *http.Request) {
	id, sk, alanAdi, _, demo, ok := h.parent(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	var anaAktif int
	var certYol, ipv4 string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(ssl_aktif,0), COALESCE(cert_path,''), COALESCE(ipv4,'') FROM domains WHERE id=?`, id).
		Scan(&anaAktif, &certYol, &ipv4); err != nil {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	askida, _ := hesaplar.HesapAskida(r.Context(), h.DB, sk)
	simdi := sslSimdi()
	ips := []string{h.IPv4}
	if ipv4 != "" {
		ips = []string{ipv4, h.IPv4}
	}

	type kayit struct {
		id int64
		ad string
	}
	oku := func(sorgu string) ([]kayit, error) {
		rows, err := h.DB.QueryContext(r.Context(), sorgu, id)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var l []kayit
		for rows.Next() {
			var k kayit
			if rows.Scan(&k.id, &k.ad) == nil {
				l = append(l, k)
			}
		}
		return l, rows.Err()
	}
	ekler, err1 := oku(`SELECT id, alan_adi FROM ek_alan_adlari WHERE domain_id=? ORDER BY alan_adi`)
	altlar, err2 := oku(`SELECT id, tam_ad FROM subdomanlar WHERE domain_id=? ORDER BY tam_ad`)
	if err1 != nil || err2 != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "listelenemedi")
		return
	}

	out := []SSLHedef{}
	// Ana alan adı: yol DB'den gelir ama yalnız sistem kökü altında okunur (aksi halde
	// kanonik yol); kök dışı/sembolik yol okunmaz.
	ana := SSLHedef{Tur: "ana", AlanAdi: alanAdi, Askida: askida, Demo: demo}
	crt := filepath.Join(anaSertifikaKoku, alanAdi, alanAdi+".crt")
	if c := filepath.Clean(certYol); certYol != "" && strings.HasPrefix(c, anaSertifikaKoku+"/") {
		crt = c
	}
	hedefDoldur(&ana, anaSertifikaKoku, crt, anaAktif == 1, []string{"apex", "www"}, simdi)
	out = append(out, ana)

	ekle := func(tur string, k kayit, kapsam []string) {
		kimlik := k.id
		t := SSLHedef{Tur: tur, AlanAdi: k.ad, Askida: askida, Demo: demo, Kapsam: kapsam, Durum: "yok"}
		if tur == "ek" {
			t.EkAlanID = &kimlik
		} else {
			t.AltAlanID = &kimlik
		}
		if ekAdGuvenli(k.ad) && reEkSK.MatchString(sk) {
			c, key := certYolu(sk, k.ad)
			hedefDoldur(&t, evKoku, c, dosyaDuzgun(c) && dosyaDuzgun(key), kapsam, simdi)
		}
		out = append(out, t)
	}
	for _, k := range ekler {
		ekle("ek", k, []string{"apex", "www"})
	}
	for _, k := range altlar {
		ekle("alt", k, []string{"apex"})
	}

	// DNS hazırlığı yalnız Let's Encrypt ile ilgilenebilecek (SSL'siz / öz-imza) satırlar için.
	for i := range out {
		t := &out[i]
		if t.SSLAktif && t.SertifikaTuru != nil && *t.SertifikaTuru == "letsencrypt" {
			continue
		}
		adlar := []string{t.AlanAdi}
		if t.Tur != "alt" {
			adlar = append(adlar, "www."+t.AlanAdi)
		}
		t.DNSHazir = dnsKontrol(adlar, ips)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"hedefler": out})
}
