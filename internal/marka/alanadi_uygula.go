package marka

// alanadi_uygula.go — bayi özel panel adresi: TEK "UYGULA" eylemi (göç 0170).
//
// Eski akış bayiden iki ayrı elle adım istiyordu (ekle → DNS'e A+TXT gir → Doğrula
// → bekle). Uygula tek eylemdir ve İDEMPOTENTTİR (tekrar basılabilir):
//
//	dogrulama  alan adı biçimi/yasaklı/ad alanı (AdAlaniKanca) + bölge sahipliği
//	dns_kontrol  bölge bu panelde mi, A kaydı var mı
//	dns_eklendi / dns_bekleniyor  kayıt eklendi ya da dış DNS'te bekleniyor
//	ssl  durum ssl_bekliyor: provisioner/panelmarka.go acme döngüsü (HEMEN dürtülür)
//	aktif
//
// SAHİPLİK (🔴): TXT şartı HARİCİ DNS için aynen korunur. Yerel kısayol (TXT
// yok, panel tek A kaydı ekler) YALNIZ şu hâlde: ad, bu panelde barındırılan bir
// bölgenin (domains / ek_alan_adlari) İÇİNDE; o hosting hesabı BU bayinin
// (domains.reseller_id = bayi), müşteriye ait değil (customer_id boş), askıda/pasif
// değil; VE kamu DNS'inde bölgenin NS'leri bu sunucuyu gösteriyor (yani alan adını
// gerçekten bu sunucuda yönetiyor). NS başka yerdeyse kısayol yoktur: bayiye A + TXT
// dış DNS talimatı verilir. Başka bayinin/kökün bölgesi → 409 (IDOR), kayıt eklenmez.
//
// DNS yazma yetkisi: yalnız tek A kaydı (ad → panel IP'si); AAAA/CNAME/TXT eklenmez,
// mevcut kayıt değiştirilmez/silinmez, aynı ad için başka içerikli A/AAAA/CNAME/NS
// varsa DOKUNULMAZ ve net hata verilir.
//
// 🔴 DNS aşamasında hata durumu 'hata' DEĞİL 'dns_bekliyor' + adim'dir: 'hata' acme
// döngüsünce yeniden denenir ve TXT'siz ad sertifika alabilirdi.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/dnsutil"
	"girginospanel/internal/httpx"
)

// Adım adları (cp_marka_alanadi.adim).
const (
	adimDogrulama  = "dogrulama"
	adimDNSKontrol = "dns_kontrol"
	adimDNSEklendi = "dns_eklendi"
	adimDNSBekle   = "dns_bekleniyor"
	adimDNSCakisma = "dns_cakisma"
	adimDNSRed     = "dns_red"
	adimDNSZaman   = "dns_zamanasimi"
	adimSSL        = "ssl"
)

// Bayiye gösterilen sade mesajlar (sabit küme: ham DNS çıktısı/başka bayi bilgisi yok).
const (
	msjDogrulama   = "Alan adı doğrulanıyor."
	msjDNSKontrol  = "DNS kayıtları kontrol ediliyor."
	msjDNSEklendi  = "DNS kaydı eklendi; yayılması bekleniyor."
	msjDNSBekle    = "DNS kaydı henüz görünmüyor; sayfa kendiliğinden kontrol ediyor."
	msjNSHarici    = "Alan adınızın ad sunucuları bu sunucuyu göstermiyor; DNS bu sunucuda değil. Aşağıdaki kayıtları DNS’inizi yönettiğiniz yerde ekleyin."
	msjHariciA     = "A kaydı henüz bu sunucunun IP adresine işaret etmiyor."
	msjHariciTXT   = "Doğrulama TXT kaydı bulunamadı ya da değeri eşleşmiyor."
	msjHariciIkisi = "A kaydı ve doğrulama TXT kaydı henüz görünmüyor."
	msjZamanasimi  = "DNS kayıtları 30 dakika içinde görünmedi. Kayıtları kontrol edip yeniden kontrol edin."
	msjSSL         = "DNS doğrulandı; SSL sertifikası alınıyor, birkaç dakika sürebilir."
	msjYazilamadi  = "DNS kaydı eklenemedi; yeniden denenecek."
	msjOkunamadi   = "DNS durumu okunamadı; yeniden denenecek."
	msjRedGenel    = "Bu adres kullanılamıyor."
	msjAskida      = "Bu adresin bağlı olduğu hosting hesabı askıda ya da pasif."
	msjIPYok       = "Sunucunun IP adresi bilinmiyor; yöneticinize başvurun."
)

const (
	yoklamaSure   = 30 * time.Minute
	uygulaSaatlik = 3
	dogrulaBekle  = 5 * time.Second
)

// Test/bağlama kancaları.
var (
	simdi        = time.Now
	arkaCalistir = func(f func()) { go f() }
	// ZoneYazKanca — bölgeyi DB'den yeniden yazar (main.go: dns.WriteZone / dns.WriteEkZone).
	ZoneYazKanca func(ctx context.Context, ek bool, id int64) error
	// nsBuSunucuya — bölge kökünün KAMU NS'leri bu sunucuya mı çözülüyor.
	nsBuSunucuya = kamuNSBuSunucuya
)

// kamuNSBuSunucuya — kökün NS adlarını kamu çözücülerden alır, her biri sunucu
// IP'lerinden birine çözülüyorsa true.
func kamuNSBuSunucuya(ctx context.Context, kok string, ipler []string) bool {
	set := map[string]bool{}
	for _, ip := range ipler {
		if ip != "" {
			set[ip] = true
		}
	}
	if len(set) == 0 {
		return false
	}
	var nsler []*net.NS
	for _, sunucu := range []string{"1.1.1.1:53", "8.8.8.8:53", "9.9.9.9:53"} {
		sunucu := sunucu
		res := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 4 * time.Second}
			return d.DialContext(ctx, network, sunucu)
		}}
		c, iptal := context.WithTimeout(ctx, 6*time.Second)
		v, err := res.LookupNS(c, kok)
		iptal()
		if err == nil && len(v) > 0 {
			nsler = v
			break
		}
	}
	for _, n := range nsler {
		for _, ip := range dnsutil.Cozumle(strings.TrimSuffix(strings.ToLower(n.Host), ".")) {
			if set[ip] {
				return true
			}
		}
	}
	return false
}

// ── hız sınırı: bayi başına saatte en çok 3 Uygula ─────────────────────────

var uygulaSayac = struct {
	sync.Mutex
	m map[int64][]time.Time
}{m: map[int64][]time.Time{}}

func uygulaIzin(rid int64) bool {
	uygulaSayac.Lock()
	defer uygulaSayac.Unlock()
	simd := simdi()
	var tut []time.Time
	for _, t := range uygulaSayac.m[rid] {
		if simd.Sub(t) < time.Hour {
			tut = append(tut, t)
		}
	}
	if len(tut) >= uygulaSaatlik {
		uygulaSayac.m[rid] = tut
		return false
	}
	uygulaSayac.m[rid] = append(tut, simd)
	return true
}

func uygulaSayacSifirla() {
	uygulaSayac.Lock()
	uygulaSayac.m = map[int64][]time.Time{}
	uygulaSayac.Unlock()
}

// ── bayi başına tek-uçuş kilidi + "şimdi kontrol et" bekleme süresi ────────

var ilerleDurum = struct {
	sync.Mutex
	calisiyor  map[int64]bool
	sonDogrula map[int64]time.Time
}{calisiyor: map[int64]bool{}, sonDogrula: map[int64]time.Time{}}

func ilerleKilitle(rid int64) bool {
	ilerleDurum.Lock()
	defer ilerleDurum.Unlock()
	if ilerleDurum.calisiyor[rid] {
		return false
	}
	ilerleDurum.calisiyor[rid] = true
	return true
}

func ilerleAc(rid int64) {
	ilerleDurum.Lock()
	delete(ilerleDurum.calisiyor, rid)
	ilerleDurum.Unlock()
}

// ── zaman yardımcıları (UTC dizgesi; DB saat diliminden bağımsız) ───────────

const zamanBicim = "2006-01-02 15:04:05"

func zamanYaz(t time.Time) string { return t.UTC().Format(zamanBicim) }

func zamanOku(s sql.NullString) time.Time {
	if !s.Valid || s.String == "" {
		return time.Time{}
	}
	t, err := time.ParseInLocation(zamanBicim, s.String, time.UTC)
	if err != nil {
		return time.Time{}
	}
	return t
}

// ── yerel bölge ─────────────────────────────────────────────────────────────

type yerelBolge struct {
	Var     bool
	Ek      bool
	ID      int64 // domains.id ya da ek_alan_adlari.id (kayıt tablosunun sahibi)
	Kok     string
	Rel     string // bölge içi göreli ad ("panel", "a.b")
	Sahip   int64
	Askida  bool
	Musteri bool
}

// Red — bayiye ASLA adı/sahibi söylenmeyen red gerekçesi; "" = sorun yok.
func (y yerelBolge) Red(rid int64) string {
	if !y.Var {
		return ""
	}
	if y.Rel == "" || y.Sahip != rid {
		return msjRedGenel
	}
	if y.Askida {
		return msjAskida
	}
	return ""
}

// Uygun — yerel sahiplik kısayolunun ön koşulu (NS denetimi ayrıca yapılır).
func (y yerelBolge) Uygun(rid int64) bool {
	return y.Var && y.Red(rid) == "" && !y.Musteri
}

// 🔴 domains.alan_adi utf8mb4_unicode_ci, ek_alan_adlari.alan_adi utf8mb4_general_ci: UNION'da kolasyon açıkça
// eşitlenmezse MariaDB 1271 "Illegal mix of collations" verir ve "Özel adres doğrulanamadı" hatası çıkar.
func yerelBolgeBul(ctx context.Context, db *sql.DB, host string, rid int64) (yerelBolge, error) {
	sorgu := func(ek bool) string {
		s := `SELECT t, id, kok, sahip, askida, durum, cust FROM (
			SELECT 0 AS t, d.id AS id, d.alan_adi AS kok, COALESCE(d.reseller_id,0) AS sahip, d.askida AS askida, d.durum AS durum, COALESCE(d.customer_id,0) AS cust FROM domains d`
		if ek {
			s += ` UNION ALL
			SELECT 1, e.id, e.alan_adi COLLATE utf8mb4_unicode_ci, COALESCE(d.reseller_id,0), d.askida, d.durum, COALESCE(d.customer_id,0)
			FROM ek_alan_adlari e JOIN domains d ON d.id=e.domain_id`
		}
		return s + `) z WHERE z.kok = ? OR (CHAR_LENGTH(z.kok) < CHAR_LENGTH(?) AND RIGHT(?, CHAR_LENGTH(z.kok)+1) = CONCAT('.', z.kok))
			ORDER BY CHAR_LENGTH(z.kok) DESC, z.t ASC LIMIT 1`
	}
	oku := func(ek bool) (yerelBolge, error) {
		var y yerelBolge
		var t, askida int
		var durum string
		var cust int64
		err := db.QueryRowContext(ctx, sorgu(ek), host, host, host).Scan(&t, &y.ID, &y.Kok, &y.Sahip, &askida, &durum, &cust)
		if errors.Is(err, sql.ErrNoRows) {
			return yerelBolge{}, nil
		}
		if err != nil {
			return yerelBolge{}, err
		}
		y.Var, y.Ek = true, t == 1
		y.Askida = askida != 0 || durum != "aktif"
		y.Musteri = cust > 0
		if host != y.Kok {
			y.Rel = strings.TrimSuffix(host, "."+y.Kok)
		}
		return y, nil
	}
	y, err := oku(true)
	if err != nil && (strings.Contains(err.Error(), "1146") || strings.Contains(strings.ToLower(err.Error()), "doesn't exist")) {
		return oku(false) // ek_alan_adlari yok (0160 koşmamış)
	}
	return y, err
}

type dnsCakisma struct{ Tip, Deger string }

func (c dnsCakisma) Mesaj() string {
	return fmt.Sprintf("Bu ad için mevcut kayıt var: %s → %s; kaldırın ya da başka bir ad seçin.", c.Tip, c.Deger)
}

func temizDeger(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r != 0x7f {
			b.WriteRune(r)
		}
		if b.Len() >= 100 {
			break
		}
	}
	return b.String()
}

func normKayitAd(ad, kok string) string {
	ad = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(ad)), ".")
	return strings.TrimSuffix(ad, "."+kok)
}

// aKaydiGarantile — hostname için tek A kaydı: zaten doğruysa dokunmaz; çakışan
// kayıt varsa dokunmaz + (*dnsCakisma); yoksa ekler ve bölgeyi yeniden yazar.
func (h *Handlers) aKaydiGarantile(ctx context.Context, y yerelBolge, ip string, ipler []string) (eklendi bool, cak *dnsCakisma, err error) {
	tablo, sutun := "dns_records", "domain_id"
	if y.Ek {
		tablo, sutun = "ek_dns_kayitlari", "ek_alan_id"
	}
	rows, err := h.DB.QueryContext(ctx, `SELECT ad, tip, deger FROM `+tablo+` WHERE `+sutun+`=? AND aktif=1`, y.ID)
	if err != nil {
		return false, nil, err
	}
	dogru := false
	var bulunan *dnsCakisma
	for rows.Next() {
		var ad, tip, deger string
		if err := rows.Scan(&ad, &tip, &deger); err != nil {
			rows.Close()
			return false, nil, err
		}
		if normKayitAd(ad, y.Kok) != y.Rel {
			continue
		}
		tip = strings.ToUpper(strings.TrimSpace(tip))
		switch tip {
		case "A":
			ok := false
			for _, i := range ipler {
				if strings.TrimSpace(deger) == i {
					ok = true
				}
			}
			if ok {
				dogru = true
			} else if bulunan == nil {
				bulunan = &dnsCakisma{Tip: "A", Deger: temizDeger(deger)}
			}
		case "AAAA", "CNAME", "NS":
			if bulunan == nil {
				bulunan = &dnsCakisma{Tip: tip, Deger: temizDeger(deger)}
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, nil, err
	}
	if bulunan != nil {
		return false, bulunan, nil
	}
	if dogru {
		return false, nil, nil
	}
	if ZoneYazKanca == nil {
		return false, nil, errors.New("bölge yazıcı bağlı değil")
	}
	res, err := h.DB.ExecContext(ctx, `INSERT INTO `+tablo+` (`+sutun+`, ad, tip, deger, ttl, oncelik, aktif) VALUES (?, ?, 'A', ?, 600, 0, 1)`, y.ID, y.Rel, ip)
	if err != nil {
		return false, nil, err
	}
	kid, _ := res.LastInsertId()
	if err := ZoneYazKanca(ctx, y.Ek, y.ID); err != nil {
		// Bölge yazılamadı: eklediğimiz TEK satırı geri al (başka kayda dokunma).
		_, _ = h.DB.ExecContext(ctx, `DELETE FROM `+tablo+` WHERE id=? AND `+sutun+`=?`, kid, y.ID)
		_ = ZoneYazKanca(ctx, y.Ek, y.ID)
		return false, nil, err
	}
	return true, nil, nil
}

// ── satır modeli ────────────────────────────────────────────────────────────

type alanSatir struct {
	Ad, Durum, Jeton, Adim, Kaynak string
	RID                            int64
	Bitis                          time.Time
}

func (h *Handlers) satirOku(ctx context.Context, rid int64) (*alanSatir, error) {
	var a alanSatir
	var b sql.NullString
	err := h.DB.QueryRowContext(ctx, `SELECT alan_adi, durum, dogrulama, adim, dns_kaynak, DATE_FORMAT(yoklama_bitis,'%Y-%m-%d %H:%i:%s') FROM cp_marka_alanadi WHERE reseller_id=?`, rid).
		Scan(&a.Ad, &a.Durum, &a.Jeton, &a.Adim, &a.Kaynak, &b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.RID = rid
	a.Bitis = zamanOku(b)
	return &a, nil
}

func bekleyenAdim(adim string) bool {
	switch adim {
	case adimDogrulama, adimDNSKontrol, adimDNSEklendi, adimDNSBekle:
		return true
	}
	return false
}

// isleniyor — Uygula tekrar basıldığında "zaten işleniyor" mu?
func (a *alanSatir) isleniyor() bool {
	if a.Durum == "ssl_bekliyor" {
		return true
	}
	return a.Durum == "dns_bekliyor" && bekleyenAdim(a.Adim) && a.Bitis.After(simdi())
}

func (h *Handlers) adimYaz(ctx context.Context, a *alanSatir, adim, mesaj, hata string, durdur bool) {
	q := `UPDATE cp_marka_alanadi SET adim=?, adim_mesaj=?, son_hata_genel=?, adim_zaman=?`
	if durdur {
		q += `, yoklama_bitis=NULL`
	}
	q += ` WHERE alan_adi=? AND reseller_id=? AND dogrulama=? AND durum='dns_bekliyor'`
	if _, err := h.DB.ExecContext(ctx, q, adim, mesaj, hata, zamanYaz(simdi()), a.Ad, a.RID, a.Jeton); err != nil {
		log.Printf("marka: adım yazılamadı (rid=%d): %v", a.RID, err)
	}
	a.Adim = adim
}

func (h *Handlers) kaynakYaz(ctx context.Context, a *alanSatir, kaynak string) {
	if a.Kaynak == kaynak {
		return
	}
	if _, err := h.DB.ExecContext(ctx, `UPDATE cp_marka_alanadi SET dns_kaynak=? WHERE alan_adi=? AND reseller_id=? AND dogrulama=?`, kaynak, a.Ad, a.RID, a.Jeton); err == nil {
		a.Kaynak = kaynak
	}
}

func (h *Handlers) sunucuIP() string {
	for _, ip := range h.SunucuIPleri {
		if ip != "" {
			return ip
		}
	}
	return ""
}

func (h *Handlers) markaIzinli(ctx context.Context, rid int64) bool {
	var izin, etkinIzin int
	err := h.DB.QueryRowContext(ctx, `SELECT COALESCE(alanadi_izni,0), 1 FROM cp_marka WHERE reseller_id=?`, rid).Scan(&izin, &etkinIzin)
	return err == nil && izin == 1
}

// ilerle — durum makinesinin bir turu (idempotent; bayi başına tek uçuş).
func (h *Handlers) ilerle(ctx context.Context, rid int64) {
	if !ilerleKilitle(rid) {
		return
	}
	defer ilerleAc(rid)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("🔴 marka uygula: tur panikledi (rid=%d): %v", rid, r)
		}
	}()
	a, err := h.satirOku(ctx, rid)
	if err != nil || a == nil || a.Durum != "dns_bekliyor" || !bekleyenAdim(a.Adim) {
		return
	}
	if !a.Bitis.After(simdi()) {
		h.adimYaz(ctx, a, adimDNSZaman, msjZamanasimi, msjZamanasimi, true)
		return
	}
	if !h.markaIzinli(ctx, rid) {
		return // izin kapandı: satır dokunulmadan kalır, host çözümü zaten düşer
	}
	ipler := h.SunucuIPleri
	ip := h.sunucuIP()
	if ip == "" {
		h.adimYaz(ctx, a, adimDNSRed, msjIPYok, msjIPYok, true)
		return
	}
	onceki := a.Adim
	h.adimYaz(ctx, a, adimDNSKontrol, msjDNSKontrol, "", false)
	// Ad alanı her turda yeniden: araya başka hesap aynı adı/alt adı açmış olabilir.
	if AdAlaniKanca != nil {
		if err := AdAlaniKanca(ctx, a.Ad, rid); err != nil {
			h.adimYaz(ctx, a, adimDNSRed, msjRedGenel, msjRedGenel, true)
			return
		}
	}
	y, err := yerelBolgeBul(ctx, h.DB, a.Ad, rid)
	if err != nil {
		log.Printf("marka uygula: bölge okunamadı (rid=%d): %v", rid, err)
		h.adimYaz(ctx, a, adimDNSBekle, msjOkunamadi, "", false)
		return
	}
	if red := y.Red(rid); red != "" {
		h.adimYaz(ctx, a, adimDNSRed, red, red, true)
		return
	}

	eklendi, yerelKaynak := false, false
	if y.Uygun(rid) {
		var cak *dnsCakisma
		eklendi, cak, err = h.aKaydiGarantile(ctx, y, ip, ipler)
		switch {
		case cak != nil:
			h.kaynakYaz(ctx, a, "panel")
			h.adimYaz(ctx, a, adimDNSCakisma, cak.Mesaj(), cak.Mesaj(), true)
			return
		case err != nil:
			log.Printf("marka uygula: A kaydı yazılamadı (rid=%d): %v", rid, err)
			h.adimYaz(ctx, a, adimDNSBekle, msjYazilamadi, "", false)
			return
		}
		if eklendi {
			httpx.DenetimSistem(h.DB, rid, "bayi", "marka.alanadi_dns_eklendi", a.Ad, "A kaydı eklendi", rid, true)
		}
		yerelKaynak = nsBuSunucuya(ctx, y.Kok, ipler)
	}

	if yerelKaynak {
		h.kaynakYaz(ctx, a, "panel")
		if aBuSunucuya(a.Ad, ipler) {
			h.sslyeGec(ctx, a)
			return
		}
		if eklendi || onceki == adimDNSEklendi {
			h.adimYaz(ctx, a, adimDNSEklendi, msjDNSEklendi, "", false)
		} else {
			h.adimYaz(ctx, a, adimDNSBekle, msjDNSBekle, "", false)
		}
		return
	}

	// HARİCİ DNS: A + TXT şartı aynen.
	h.kaynakYaz(ctx, a, "harici")
	aOk := aBuSunucuya(a.Ad, ipler)
	v, terr := txtCoz(ctx, DogrulamaOnek+a.Ad)
	tOk := terr == nil && txtIcerir(v, a.Jeton)
	if aOk && tOk {
		h.sslyeGec(ctx, a)
		return
	}
	mesaj := msjHariciIkisi
	switch {
	case y.Uygun(rid):
		mesaj = msjNSHarici
	case aOk:
		mesaj = msjHariciTXT
	case tOk:
		mesaj = msjHariciA
	}
	h.adimYaz(ctx, a, adimDNSBekle, mesaj, "", false)
}

func (h *Handlers) sslyeGec(ctx context.Context, a *alanSatir) {
	res, err := h.DB.ExecContext(ctx, `UPDATE cp_marka_alanadi SET durum='ssl_bekliyor', adim=?, adim_mesaj=?, son_hata_genel='', adim_zaman=?, yoklama_bitis=NULL
		WHERE alan_adi=? AND reseller_id=? AND dogrulama=? AND durum='dns_bekliyor'`, adimSSL, msjSSL, zamanYaz(simdi()), a.Ad, a.RID, a.Jeton)
	if err != nil {
		log.Printf("marka uygula: ssl_bekliyor yazılamadı (rid=%d): %v", a.RID, err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		hostTemizle()
		alanDegisti(a.Ad)
	}
}

// UygulaDongusu — işlenen adresleri 30 sn aralıkla ilerletir (yeniden başlatmadan sonra da sürer).
func (h *Handlers) UygulaDongusu(ctx context.Context, aralik time.Duration) {
	go func() {
		t := time.NewTimer(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			h.dongusuTek(ctx)
			t.Reset(aralik)
		}
	}()
}

func (h *Handlers) dongusuTek(ctx context.Context) {
	rows, err := h.DB.QueryContext(ctx, `SELECT reseller_id FROM cp_marka_alanadi WHERE durum='dns_bekliyor' AND yoklama_bitis IS NOT NULL
		AND adim IN ('dogrulama','dns_kontrol','dns_eklendi','dns_bekleniyor')`)
	if err != nil {
		return
	}
	var ridler []int64
	for rows.Next() {
		var rid int64
		if rows.Scan(&rid) == nil {
			ridler = append(ridler, rid)
		}
	}
	rows.Close()
	for _, rid := range ridler {
		h.ilerle(ctx, rid)
	}
}

// ── ad doğrulama ────────────────────────────────────────────────────────────

var yasakliSon = []string{".localhost", ".local", ".internal", ".localdomain", ".arpa", ".invalid", ".example"}

func adYasakli(ad string) bool {
	for _, s := range yasakliSon {
		if strings.HasSuffix(ad, s) {
			return true
		}
	}
	return false
}

// ── uçlar ───────────────────────────────────────────────────────────────────

// AlanUygula — POST /marka/alan-adi/uygula {"alan_adi": "..."} (gövde boşsa mevcut ad sürer).
func (h *Handlers) AlanUygula(w http.ResponseWriter, r *http.Request) {
	k, ok := h.alanBayi(w, r)
	if !ok {
		return
	}
	var g struct {
		AlanAdi string `json:"alan_adi"`
	}
	if r.ContentLength != 0 {
		if err := govdeCoz(r, &g); err != nil {
			yazHata(w, http.StatusBadRequest, "geçersiz istek gövdesi")
			return
		}
	}
	ctx := r.Context()
	mevcut, err := h.satirOku(ctx, k.RID)
	if err != nil {
		yazHata(w, http.StatusInternalServerError, "Özel adres okunamadı.")
		return
	}
	ad := HostNormalize(g.AlanAdi)
	if strings.TrimSpace(g.AlanAdi) == "" && mevcut != nil {
		ad = mevcut.Ad
	}
	if ad == "" || strings.Count(ad, ".") < 1 || adYasakli(ad) {
		yazHata(w, http.StatusBadRequest, "Geçerli bir alan adı girin (ör. panel.ornek.com).")
		return
	}
	// İdempotent kısa yollar (hız sınırına SAYILMAZ, çift iş yok).
	if mevcut != nil && mevcut.Ad == ad {
		if mevcut.Durum == "aktif" || mevcut.isleniyor() {
			h.yazDurum(w, r, k.RID, http.StatusOK, mevcut.Durum == "aktif", mevcut.isleniyor())
			return
		}
	}
	if mevcut != nil && mevcut.Ad != ad && mevcut.Durum == "aktif" {
		yazHata(w, http.StatusConflict, "Zaten bir özel adresiniz var; yenisini eklemek için önce onu kaldırın.")
		return
	}
	// Ad alanı + bölge sahipliği (kayıt yazmadan ÖNCE; red = 409, kimin olduğu söylenmez).
	if AdAlaniKanca != nil {
		if err := AdAlaniKanca(ctx, ad, k.RID); err != nil {
			yazHata(w, http.StatusConflict, "Bu adres kullanılamıyor: "+err.Error())
			return
		}
	}
	y, err := yerelBolgeBul(ctx, h.DB, ad, k.RID)
	if err != nil {
		yazHata(w, http.StatusInternalServerError, "Özel adres doğrulanamadı.")
		return
	}
	if red := y.Red(k.RID); red != "" {
		h.denetim(r, "marka.alanadi_uygula", ad, "reddedildi", k.RID, false)
		yazHata(w, http.StatusConflict, red)
		return
	}
	if !uygulaIzin(k.RID) {
		w.Header().Set("Retry-After", "3600")
		yazHata(w, http.StatusTooManyRequests, "Saatte en çok 3 kez uygulayabilirsiniz; bir süre sonra tekrar deneyin.")
		return
	}
	bitis := zamanYaz(simdi().Add(yoklamaSure))
	su := zamanYaz(simdi())
	switch {
	case mevcut != nil && mevcut.Ad == ad:
		// Aynı ad sürer: jeton korunur (dış DNS'e girilmiş TXT geçerli kalır).
		_, err = h.DB.ExecContext(ctx, `UPDATE cp_marka_alanadi SET durum='dns_bekliyor', adim=?, adim_mesaj=?, son_hata_genel='', son_hata_ayrinti=NULL,
			dns_kaynak='', yoklama_bitis=?, adim_zaman=? WHERE reseller_id=? AND alan_adi=?`, adimDogrulama, msjDogrulama, bitis, su, k.RID, ad)
	default:
		if mevcut != nil { // bekleyen/hatalı eski adres yenisiyle değişir
			if _, err = h.DB.ExecContext(ctx, `DELETE FROM cp_marka_alanadi WHERE reseller_id=? AND durum<>'aktif'`, k.RID); err == nil {
				hostTemizle()
				alanDegisti("")
			}
		}
		if err == nil {
			jb := make([]byte, 16)
			if _, rerr := rand.Read(jb); rerr != nil {
				yazHata(w, http.StatusInternalServerError, "Özel adres eklenemedi.")
				return
			}
			_, err = h.DB.ExecContext(ctx, `INSERT INTO cp_marka_alanadi (alan_adi, reseller_id, dogrulama, durum, adim, adim_mesaj, yoklama_bitis, adim_zaman)
				VALUES (?, ?, ?, 'dns_bekliyor', ?, ?, ?, ?)`, ad, k.RID, hex.EncodeToString(jb), adimDogrulama, msjDogrulama, bitis, su)
		}
	}
	if err != nil {
		// PK (başka bayi aynı adı almış) / UNIQUE(reseller_id): kimin olduğu SÖYLENMEZ.
		h.denetim(r, "marka.alanadi_uygula", ad, "reddedildi", k.RID, false)
		yazHata(w, http.StatusConflict, "Bu adres kullanılamıyor.")
		return
	}
	h.denetim(r, "marka.alanadi_uygula", ad, "", k.RID, true)
	rid := k.RID
	arkaCalistir(func() { h.ilerle(context.Background(), rid) })
	h.yazDurum(w, r, k.RID, http.StatusAccepted, false, false)
}

// yazDurum — güncel kayıt + sunucu IP'leri.
func (h *Handlers) yazDurum(w http.ResponseWriter, r *http.Request, rid int64, kod int, zatenAktif, zatenIsleniyor bool) {
	a, err := h.alanGorunum(r.Context(), rid)
	if err != nil {
		yazHata(w, http.StatusInternalServerError, "Özel adres okunamadı.")
		return
	}
	httpx.WriteJSON(w, kod, map[string]any{"kayit": a, "sunucu_ip": h.SunucuIPleri,
		"zaten_aktif": zatenAktif, "zaten_isleniyor": zatenIsleniyor && !zatenAktif})
}

// alanGorunum — alanOku + adım listesi + dış DNS talimatı (bayiye gösterilen biçim).
func (h *Handlers) alanGorunum(ctx context.Context, rid int64) (*alanKaydi, error) {
	a, err := alanOku(ctx, h.DB, rid)
	if err != nil || a == nil {
		return a, err
	}
	a.Adimlar = adimlariTuret(a.Durum, a.adim)
	a.Adim = &adimBilgi{Ad: a.adim, Mesaj: a.adimMesaj, Guncelleme: a.adimZaman}
	if a.Adim.Ad == "" {
		a.Adim.Ad = eskiAdim(a.Durum)
	}
	a.Isleniyor = (&alanSatir{Durum: a.Durum, Adim: a.adim, Bitis: a.bitis}).isleniyor()
	if a.Durum == "dns_bekliyor" && (a.DNSKaynak == "harici" || a.adim == "") {
		for _, ip := range h.SunucuIPleri {
			if ip != "" {
				a.DNSKayitlari = append(a.DNSKayitlari, dnsTalimat{Tur: "A", Ad: a.AlanAdi, Deger: ip})
			}
		}
		a.DNSKayitlari = append(a.DNSKayitlari, dnsTalimat{Tur: "TXT", Ad: a.TXTAd, Deger: a.TXTDeger})
	}
	return a, nil
}

func eskiAdim(durum string) string {
	switch durum {
	case "aktif":
		return "aktif"
	case "ssl_bekliyor", "hata":
		return adimSSL
	}
	return adimDNSBekle
}

type adimBilgi struct {
	Ad         string `json:"ad"`
	Mesaj      string `json:"mesaj"`
	Guncelleme string `json:"guncelleme"`
}

type adimSatir struct {
	Ad    string `json:"ad"`    // dogrulama | dns | ssl | aktif
	Durum string `json:"durum"` // tamam | suruyor | bekliyor | hata
}

type dnsTalimat struct {
	Tur   string `json:"tur"`
	Ad    string `json:"ad"`
	Deger string `json:"deger"`
}

// adimlariTuret — ekranın 4 adımlı listesi: durum + adim'dan türetilir.
func adimlariTuret(durum, adim string) []adimSatir {
	d := func(a, b, c, e string) []adimSatir {
		return []adimSatir{{"dogrulama", a}, {"dns", b}, {"ssl", c}, {"aktif", e}}
	}
	switch durum {
	case "aktif":
		return d("tamam", "tamam", "tamam", "tamam")
	case "ssl_bekliyor":
		return d("tamam", "tamam", "suruyor", "bekliyor")
	case "hata":
		return d("tamam", "tamam", "hata", "bekliyor")
	case "kapali":
		return d("bekliyor", "bekliyor", "bekliyor", "bekliyor")
	}
	switch adim {
	case adimDogrulama:
		return d("suruyor", "bekliyor", "bekliyor", "bekliyor")
	case adimDNSKontrol, adimDNSEklendi:
		return d("tamam", "suruyor", "bekliyor", "bekliyor")
	case adimDNSCakisma, adimDNSRed, adimDNSZaman:
		return d("tamam", "hata", "bekliyor", "bekliyor")
	}
	return d("tamam", "bekliyor", "bekliyor", "bekliyor")
}

// alanSimdiKontrol — dogrula ucunun yeni (adim'li) satırlar için yolu: "Şimdi kontrol et".
// Yoklamayı yeniden kurar ve bir tur senkron ilerletir; LE hız sınırına SAYILMAZ
// (sertifika denemeleri provisioner geri çekilmesiyle ayrıca sınırlı).
func (h *Handlers) alanSimdiKontrol(w http.ResponseWriter, r *http.Request, rid int64, a *alanSatir) {
	ctx := r.Context()
	ilerleDurum.Lock()
	son := ilerleDurum.sonDogrula[rid]
	soguk := simdi().Sub(son) >= dogrulaBekle
	if soguk {
		ilerleDurum.sonDogrula[rid] = simdi()
	}
	ilerleDurum.Unlock()
	if soguk {
		switch {
		case a.Durum == "dns_bekliyor" && a.Adim != "":
			adim := a.Adim
			if !bekleyenAdim(adim) {
				adim = adimDNSKontrol
			}
			_, _ = h.DB.ExecContext(ctx, `UPDATE cp_marka_alanadi SET adim=?, yoklama_bitis=?, son_hata_genel='' WHERE reseller_id=? AND alan_adi=? AND durum='dns_bekliyor'`,
				adim, zamanYaz(simdi().Add(yoklamaSure)), rid, a.Ad)
			h.ilerle(ctx, rid)
		case a.Durum == "hata":
			alanDegisti(a.Ad) // SSL hatası: acme geri çekilmesi (15 dk'dan eskiyse) sıfırlanır + döngü dürtülür
		}
	}
	h.yazDurum(w, r, rid, http.StatusOK, false, false)
}
