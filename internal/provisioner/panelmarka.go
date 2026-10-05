package provisioner

// panelmarka.go — bayi özel panel alan adı (D2 whitelabel) G/Ç katmanı:
// acme.sh ile sertifika + nginx (SNI haritası, include, :80 mini vhost) + ölçüm.
// Saf üreticiler internal/marka/alanadi_nginx.go'da (birim testli).
//
// DOĞRULUK KAYNAĞI DB'dir (cp_marka_alanadi + cp_marka.alanadi_izni); dosyalar her
// döngüde ondan TÜRETİLİR (PanelMarkaBaslat: açılışta + aralıkla + PanelMarkaDurt).
//
//	tut   = izin AÇIK ve durum ∈ {ssl_bekliyor, hata, aktif}  → :80 vhost, sertifika, acme kaydı
//	aday  = tut ∩ (ssl_bekliyor | hata | sertifikası geçersiz aktif)  → sertifika iste
//	etkin = tut ∩ geçerli sertifika ∩ (aktif | bu döngüde hazırlanan)  → SNI + include
//
// Akış (tek döngü, panelMarkaMu altında):
//  1. :80 vhost'u tut kümesine uyumla (HTTP-01 için ÖNCE canlı olmalı).
//  2. Her aday: geri çekilme → DNS A kaydı bu sunucuya mı (ŞART) → acme.sh --issue
//     (webroot) → --install-cert (anahtar ÖNCEDEN 0640 root:nginx açılır; acme.sh
//     `cat >` ile yazar, izin korunur) → sertifika doğrula (SAN, anahtar eşi, süre).
//  3. etkin kümeye göre SNI + include + _panel.conf (değişkenli ↔ statik) + :80 →
//     kapiNginxUygula: `nginx -t` (taban kontrolü dahil) geçmezse HEPSİ geri alınır.
//  4. ÖLÇ: reload'dan sonra 127.0.0.1:<genel port>'a gerçek TLS el sıkışması —
//     varsayılan (SNI'sız) el sıkışma önce çalışıp sonra çalışmıyorsa (anahtar
//     izni, SELinux, bozuk kopya) değişiklik GERİ ALINIR: panel kimseye kilitlenmez.
//     Her bayi adı kendi sertifikasını sunmalı; sunmuyorsa yalnız o ad 'hata'.
//  5. DB: aktif + ssl_bitis | hata + genel/ayrıntı gerekçe; host önbelleği temizlenir.
//  6. Temizlik (yalnız nginx onlardan arındıktan sonra): tut dışındaki adların
//     sertifika dosyaları + acme.sh yenileme kayıtları (yalnız Le_RealFullChainPath'i
//     BayiSertDizin altında olanlar — panelin kendi kaydına ASLA dokunulmaz).
//
// 🔴 Yenileme: acme.sh'nin panel config-home'u (/opt/girginospanel/acme) için
// kurulu cron'u (panelhost --install-cronjob); install-cert kaydı reloadcmd taşır.
// Değişkenli sertifika her el sıkışmada okunduğundan reload olmasa da yenisi geçer.
// 🔴 Hız sınırı: LE ad başına saatte 5 başarısız doğrulamaya izin verir; ad başına
// üstel geri çekilme (10 dk → 24 sa) + döngü başına en çok pmDonguAzami istek.
// 🔴 LE STAGING: /etc/girginospanel/marka-acme-staging varsa --server letsencrypt_test.

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/dnsutil"
	"girginospanel/internal/kapi"
	"girginospanel/internal/marka"
)

// PanelMarkaSunucuIPleri — özel adın A kaydının işaret etmesi gereken adresler
// (main bağlar; marka.Handlers.SunucuIPleri ile aynı). Boşsa yerel arayüzler.
var PanelMarkaSunucuIPleri []string

// Dikişler: testler geçici dizin + sahte acme/nginx/TLS ile koşar.
var (
	pmSertDizin     = marka.BayiSertDizin
	pmVarsCrt       = marka.VarsayilanCrt
	pmVarsKey       = marka.VarsayilanKey
	pmIncYolu       = marka.MarkaAdlariInc
	pmSNIYolu       = marka.MarkaSNIConf
	pmPanelCrt      = marka.PanelCrt
	pmPanelKey      = marka.PanelKey
	pmWebroot       = marka.PanelAcmeWebroot
	pmAcmeHome      = "/opt/girginospanel/acme" // panelhost.AcmeConfigHome
	pmStagingIsaret = "/etc/girginospanel/marka-acme-staging"
	pmAcmeKomut     = func(ctx context.Context, arg ...string) ([]byte, error) {
		return exec.CommandContext(ctx, "/root/.acme.sh/acme.sh", arg...).CombinedOutput()
	}
	pmAcmeVar    = func() bool { _, err := os.Stat("/root/.acme.sh/acme.sh"); return err == nil }
	pmBuSunucuya = func(ad string, ipler []string) bool { return dnsutil.BuSunucuya(ad, ipler...) }
	pmTLSOlc     = pmTLSYaprak
	// pmNginxGrup: anahtar/dizin grubunu nginx yapar (worker değişkenli sertifikayı okur).
	pmNginxGrup  = func(yol string) { _ = exec.Command("chgrp", "nginx", yol).Run() }
	pmRestorecon = func(yol string) { _, _ = exec.Command("restorecon", "-R", yol).CombinedOutput() }
	pmSimdi      = time.Now
	pmOlcBekle   = 700 * time.Millisecond
)

const (
	pmDonguAzami = 3 // döngü başına en çok sertifika isteği
	pmGeriIlk    = 10 * time.Minute
	pmGeriTavan  = 24 * time.Hour

	pmMsjDNS     = "A kaydı bu sunucunun IP adresine işaret etmiyor. DNS'i düzeltin; otomatik olarak yeniden denenecek."
	pmMsjAcme    = "Sertifika alınamadı (Let's Encrypt doğrulaması başarısız). Otomatik olarak yeniden denenecek."
	pmMsjSert    = "Alınan sertifika doğrulanamadı. Otomatik olarak yeniden denenecek."
	pmMsjNginx   = "Sunucu yapılandırması uygulanamadı; sunucu yöneticisi panel günlüğünden ayrıntıyı görebilir."
	pmMsjSunum   = "Sertifika kuruldu ama sunucu onu henüz sunmuyor. Otomatik olarak yeniden denenecek."
	pmMsjAdKotu  = "Bu adres kullanılamıyor."
	pmMsjAcmeYok = "Sunucuda sertifika aracı (acme.sh) kurulu değil; sunucu yöneticinize başvurun."
)

var (
	panelMarkaMu sync.Mutex
	pmDurtKanal  = make(chan struct{}, 1)

	pmGeriMu sync.Mutex
	pmGeri   = map[string]pmGeriKayit{}
)

type pmGeriKayit struct {
	n            int
	son, sonraki time.Time
}

// pmSatir — cp_marka_alanadi satırı (+ kökün izni).
type pmSatir struct {
	ad, durum, jeton string
	rid              int64
	izin             bool
}

// pmSonuc — bir döngünün özeti (testler + günlük).
type pmSonuc struct {
	Etkin    []string          // nginx'te etkin adlar
	Hatalar  map[string]string // ad → genel gerekçe
	Uygulama bool              // nginx değişikliği uygulandı mı (değişiklik yoksa true)
	GeriAlma bool              // ölçüm başarısız → geri alındı
}

// PanelMarkaDurt — döngüyü hemen çalıştırır (bloklamaz). ad verilirse ve son denemesi
// 15 dk'dan eskiyse o adın geri çekilmesi sıfırlanır (bayi DNS'i düzeltip yeniden
// doğruladı); daha sık dürtmek LE hız sınırını zorlayamaz.
func PanelMarkaDurt(ad string) {
	if ad != "" {
		pmGeriMu.Lock()
		if g, ok := pmGeri[ad]; ok && pmSimdi().Sub(g.son) > 15*time.Minute {
			delete(pmGeri, ad)
		}
		pmGeriMu.Unlock()
	}
	select {
	case pmDurtKanal <- struct{}{}:
	default:
	}
}

// PanelMarkaBaslat — açılışta bir kez, sonra her aralıkta ya da dürtüldüğünde uzlaştırır.
func PanelMarkaBaslat(ctx context.Context, aralik time.Duration) {
	go func() {
		t := time.NewTimer(30 * time.Second) // açılış: diğer Heal'ler nginx'i otursun
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			case <-pmDurtKanal:
				if !t.Stop() {
					select {
					case <-t.C:
					default:
					}
				}
			}
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("🔴 panelmarka: döngü panikledi: %v", r)
					}
				}()
				if pkgDB != nil {
					PanelMarkaUzlastir(ctx)
				}
			}()
			t.Reset(aralik)
		}
	}()
}

// PanelMarkaUzlastir — tek döngü (bkz. dosya başı).
func PanelMarkaUzlastir(ctx context.Context) pmSonuc {
	panelMarkaMu.Lock()
	defer panelMarkaMu.Unlock()
	return pmDongu(ctx, pkgDB)
}

func pmDongu(ctx context.Context, db *sql.DB) pmSonuc {
	sonuc := pmSonuc{Hatalar: map[string]string{}, Uygulama: true}
	satirlar, err := pmSatirlarOku(ctx, db)
	if err != nil {
		log.Printf("panelmarka: satırlar okunamadı: %v", err)
		sonuc.Uygulama = false
		return sonuc
	}
	panelAdlari := pmPanelAdlari()
	var tut []pmSatir
	tutAd := map[string]bool{}
	for _, s := range satirlar {
		if !marka.HostnameGecerli(s.ad) {
			log.Printf("🔴 panelmarka: geçersiz ad DB'de (rid=%d) — yok sayıldı", s.rid)
			continue
		}
		if panelAdlari[s.ad] {
			// Panelin kendi adı sonradan bu ada çekilmiş: bayiye ASLA verilmez.
			if s.izin && s.durum != "hata" {
				pmDurumYaz(ctx, db, s, "hata", pmMsjAdKotu, "ad panelin kendi server_name'inde", nil)
			}
			sonuc.Hatalar[s.ad] = pmMsjAdKotu
			continue
		}
		if s.izin && (s.durum == "ssl_bekliyor" || s.durum == "hata" || s.durum == "aktif") {
			tut = append(tut, s)
			tutAd[s.ad] = true
		}
	}
	pmGeriBuda(tutAd)

	// 1) :80 vhost'u tut kümesine uyumla (sertifika isteğinden ÖNCE canlı olmalı).
	// SNI/include ŞİMDİKİ etkin kümede kalır: henüz sertifikası olmayan ad yazılmaz.
	simdikiEtkin := map[string]bool{}
	for _, s := range tut {
		if s.durum == "aktif" {
			if _, ok := pmSertGecerli(s.ad); ok {
				simdikiEtkin[s.ad] = true
			}
		}
	}
	on80, _, _ := pmNginxUyumla(sirala(tutAd), sirala(simdikiEtkin), false)

	// 2) Sertifika adayları.
	hazir := map[string]time.Time{}
	istek := 0
	for _, s := range tut {
		if bitis, ok := pmSertGecerli(s.ad); ok {
			// Geçerli sertifika zaten var (yeniden etkinleştirme, nginx hatası sonrası):
			// LE'ye GİTME. Aktif olmayan ad geri çekilmedeyse bekler.
			if s.durum == "aktif" || pmGeriIzinli(s.ad) {
				hazir[s.ad] = bitis
			}
			continue
		}
		if !pmGeriIzinli(s.ad) || istek >= pmDonguAzami {
			continue
		}
		if !on80 {
			pmHata(ctx, db, s, pmMsjNginx, ":80 doğrulama vhost'u uygulanamadı", &sonuc)
			continue
		}
		istek++
		if bitis, genel, ayrinti := pmSertifikaAl(ctx, s.ad); genel != "" {
			pmHata(ctx, db, s, genel, ayrinti, &sonuc)
		} else {
			hazir[s.ad] = bitis
		}
	}

	// 3) Etkin kümeyi uygula + ölç.
	etkin := map[string]bool{}
	for ad := range hazir {
		etkin[ad] = true
	}
	ok, geriAlindi, sunmayan := pmNginxUyumla(sirala(tutAd), sirala(etkin), true)
	sonuc.Uygulama, sonuc.GeriAlma = ok, geriAlindi
	for _, s := range tut {
		bitis, h := hazir[s.ad]
		if !h {
			continue
		}
		switch {
		case !ok:
			if s.durum != "aktif" { // aktif ad eski (dokunulmamış) yapılandırmada sunulmaya devam eder
				pmHata(ctx, db, s, pmMsjNginx, "nginx -t / reload / TLS ölçümü başarısız — değişiklik geri alındı", &sonuc)
			}
		case sunmayan[s.ad]:
			pmHata(ctx, db, s, pmMsjSunum, "TLS el sıkışmasında ad kendi sertifikasını sunmadı", &sonuc)
		default:
			sonuc.Etkin = append(sonuc.Etkin, s.ad)
			pmGeriSifirla(s.ad)
			b := bitis
			if s.durum != "aktif" {
				pmDurumYaz(ctx, db, s, "aktif", "", "", &b)
			} else {
				pmBitisYaz(ctx, db, s, b)
			}
		}
	}
	sort.Strings(sonuc.Etkin)
	marka.HostOnbellekTemizle()

	// 4) Temizlik — yalnız nginx tut dışındaki adlardan arındıysa (uygulama başarılı).
	if ok {
		pmTemizle(tutAd)
	}
	return sonuc
}

func sirala(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func pmSatirlarOku(ctx context.Context, db *sql.DB) ([]pmSatir, error) {
	if db == nil {
		return nil, errors.New("DB yok")
	}
	rows, err := db.QueryContext(ctx, `SELECT a.alan_adi, a.reseller_id, a.durum, a.dogrulama, COALESCE(m.alanadi_izni,0)
		FROM cp_marka_alanadi a LEFT JOIN cp_marka m ON m.reseller_id=a.reseller_id ORDER BY a.alan_adi`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pmSatir
	for rows.Next() {
		var s pmSatir
		var izin int
		if err := rows.Scan(&s.ad, &s.rid, &s.durum, &s.jeton, &izin); err != nil {
			return nil, err
		}
		s.izin = izin == 1
		out = append(out, s)
	}
	return out, rows.Err()
}

// pmDurumYaz — satırı YALNIZ okunduğu hâldeyse günceller (jeton + durum koşulu):
// bayi bu arada adı silip aynı adı yeniden eklediyse ya da yeniden doğruladıysa
// döngünün bayat kararı onu ezmez; sonraki döngü taze satırla karar verir.
func pmDurumYaz(ctx context.Context, db *sql.DB, s pmSatir, durum, genel, ayrinti string, bitis *time.Time) {
	var b any
	if bitis != nil {
		b = bitis.UTC().Format("2006-01-02 15:04:05")
	}
	var ay any
	if ayrinti != "" {
		ay = kirpPM(ayrinti, 4000)
	}
	if _, err := db.ExecContext(ctx, `UPDATE cp_marka_alanadi SET durum=?, son_hata_genel=?, son_hata_ayrinti=?, ssl_bitis=COALESCE(?, ssl_bitis)
		WHERE alan_adi=? AND reseller_id=? AND dogrulama=? AND durum=?`,
		durum, genel, ay, b, s.ad, s.rid, s.jeton, s.durum); err != nil {
		log.Printf("panelmarka: durum yazılamadı (%s): %v", s.ad, err)
	}
}

func pmBitisYaz(ctx context.Context, db *sql.DB, s pmSatir, bitis time.Time) {
	_, _ = db.ExecContext(ctx, `UPDATE cp_marka_alanadi SET ssl_bitis=? WHERE alan_adi=? AND reseller_id=? AND dogrulama=? AND durum='aktif'
		AND (ssl_bitis IS NULL OR ssl_bitis<>?)`, bitis.UTC().Format("2006-01-02 15:04:05"), s.ad, s.rid, s.jeton, bitis.UTC().Format("2006-01-02 15:04:05"))
}

func pmHata(ctx context.Context, db *sql.DB, s pmSatir, genel, ayrinti string, sonuc *pmSonuc) {
	pmGeriBasarisiz(s.ad)
	log.Printf("panelmarka: %s (rid=%d): %s — %s", s.ad, s.rid, genel, kirpPM(ayrinti, 300))
	pmDurumYaz(ctx, db, s, "hata", genel, ayrinti, nil)
	sonuc.Hatalar[s.ad] = genel
}

// ── geri çekilme ────────────────────────────────────────────────────────────

func pmGeriIzinli(ad string) bool {
	pmGeriMu.Lock()
	defer pmGeriMu.Unlock()
	g, ok := pmGeri[ad]
	return !ok || !pmSimdi().Before(g.sonraki)
}

func pmGeriBasarisiz(ad string) {
	pmGeriMu.Lock()
	defer pmGeriMu.Unlock()
	g := pmGeri[ad]
	g.n++
	bek := pmGeriIlk
	for i := 1; i < g.n && bek < pmGeriTavan; i++ {
		bek *= 3
	}
	if bek > pmGeriTavan {
		bek = pmGeriTavan
	}
	g.son = pmSimdi()
	g.sonraki = g.son.Add(bek)
	pmGeri[ad] = g
}

func pmGeriSifirla(ad string) {
	pmGeriMu.Lock()
	delete(pmGeri, ad)
	pmGeriMu.Unlock()
}

func pmGeriBuda(tut map[string]bool) {
	pmGeriMu.Lock()
	for ad := range pmGeri {
		if !tut[ad] {
			delete(pmGeri, ad)
		}
	}
	pmGeriMu.Unlock()
}

// ── sertifika ───────────────────────────────────────────────────────────────

func pmCrt(ad string) string { return filepath.Join(pmSertDizin, ad+".crt") }
func pmKey(ad string) string { return filepath.Join(pmSertDizin, ad+".key") }

// pmSertGecerli — adın kurulu sertifikası kullanılabilir mi: PEM, anahtar eşi, SAN
// adı kapsıyor, süresi dolmamış. (bitiş, ok)
func pmSertGecerli(ad string) (time.Time, bool) {
	if !marka.HostnameGecerli(ad) {
		return time.Time{}, false
	}
	return pmCiftGecerli(pmCrt(ad), pmKey(ad), ad)
}

func pmCiftGecerli(crtYol, keyYol, ad string) (time.Time, bool) {
	crt, err1 := os.ReadFile(crtYol)
	key, err2 := os.ReadFile(keyYol)
	if err1 != nil || err2 != nil || len(crt) == 0 || len(key) == 0 {
		return time.Time{}, false
	}
	cift, err := tls.X509KeyPair(crt, key)
	if err != nil || len(cift.Certificate) == 0 {
		return time.Time{}, false
	}
	yaprak, err := x509.ParseCertificate(cift.Certificate[0])
	if err != nil || !pmSimdi().Before(yaprak.NotAfter) {
		return time.Time{}, false
	}
	if ad != "" && yaprak.VerifyHostname(ad) != nil {
		return time.Time{}, false
	}
	return yaprak.NotAfter, true
}

// pmYaprakDER — PEM dosyasındaki ilk sertifika (fullchain'in yaprağı).
func pmYaprakDER(yol string) []byte {
	b, err := os.ReadFile(yol)
	if err != nil {
		return nil
	}
	blok, _ := pem.Decode(b)
	if blok == nil || blok.Type != "CERTIFICATE" {
		return nil
	}
	return blok.Bytes
}

// pmSertifikaAl — DNS ön koşulu + acme.sh issue/install + doğrulama.
// Başarı: (bitiş, "", ""); hata: (_, genel, ayrıntı).
func pmSertifikaAl(ctx context.Context, ad string) (time.Time, string, string) {
	if !pmAcmeVar() {
		return time.Time{}, pmMsjAcmeYok, "acme.sh yok"
	}
	// 🔴 DNS ŞART: LE'ye gitmeden önce A kaydı bu sunucuya (kamu çözücü). Aksi hâlde
	// başarısız doğrulama LE hız sınırını yer.
	if !pmBuSunucuya(ad, PanelMarkaSunucuIPleri) {
		return time.Time{}, pmMsjDNS, "A kaydı sunucu IP'lerine çözülmüyor"
	}
	if err := pmDizinHazirla(); err != nil {
		return time.Time{}, pmMsjSert, "dizin: " + err.Error()
	}
	// 🔴 vhost reload'u asenkron: yolu LE'ye gitmeden önce ölç (bkz. panelmarka_acme_hazir.go).
	if err := pmAcmeYoluBekle(ad); err != nil {
		return time.Time{}, pmMsjNginx, "challenge yolu yerel nginx'te canlı değil: " + err.Error()
	}
	c, iptal := context.WithTimeout(ctx, 4*time.Minute)
	defer iptal()
	sunucu := "letsencrypt"
	if _, err := os.Stat(pmStagingIsaret); err == nil {
		sunucu = "letsencrypt_test"
	}
	out, err := pmAcmeKomut(c, "--issue", "-d", ad, "--webroot", pmWebroot, "--server", sunucu,
		"--config-home", pmAcmeHome, "--keylength", "ec-256")
	if err != nil && !acmeZatenGecerli(string(out)) {
		return time.Time{}, pmMsjAcme, "acme.sh --issue: " + sonSatirlarPM(string(out), 15)
	}
	// Anahtar dosyası acme.sh yazmadan ÖNCE 0640 root:nginx açılır: acme.sh var olan
	// dosyaya `cat >` ile yazar (izin korunur); yoksa 0600 root açar → worker okuyamaz.
	crt, key := pmCrt(ad), pmKey(ad)
	if err := pmDosyaAc(key, 0o640, true); err != nil {
		return time.Time{}, pmMsjSert, "anahtar dosyası: " + err.Error()
	}
	if err := pmDosyaAc(crt, 0o644, false); err != nil {
		return time.Time{}, pmMsjSert, "sertifika dosyası: " + err.Error()
	}
	out, err = pmAcmeKomut(c, "--install-cert", "-d", ad, "--ecc", "--config-home", pmAcmeHome,
		"--key-file", key, "--fullchain-file", crt, "--reloadcmd", "systemctl reload nginx")
	if err != nil {
		return time.Time{}, pmMsjSert, "acme.sh --install-cert: " + sonSatirlarPM(string(out), 10)
	}
	_ = os.Chmod(key, 0o640)
	pmNginxGrup(key)
	_ = os.Chmod(crt, 0o644)
	pmRestorecon(pmSertDizin)
	bitis, ok := pmSertGecerli(ad)
	if !ok {
		return time.Time{}, pmMsjSert, "kurulan sertifika okunamadı / anahtar eşleşmiyor / SAN adı kapsamıyor"
	}
	return bitis, "", ""
}

func acmeZatenGecerli(out string) bool {
	low := strings.ToLower(out)
	return strings.Contains(low, "domains not changed") || strings.Contains(low, "skipping") ||
		strings.Contains(low, "next renewal time is")
}

// pmDizinHazirla — BayiSertDizin 0750 root:nginx (kiracılar listeleyemez; worker girer).
func pmDizinHazirla() error {
	if err := os.MkdirAll(pmSertDizin, 0o750); err != nil {
		return err
	}
	_ = os.Chmod(pmSertDizin, 0o750)
	pmNginxGrup(pmSertDizin)
	return nil
}

// pmDosyaAc — dosya yoksa verilen izinle (boş) açar; varsa iznini düzeltir. Sembolik
// bağı İZLEMEZ (dizin root:nginx 0750; yine de savunma).
func pmDosyaAc(yol string, mod os.FileMode, nginxGrup bool) error {
	if fi, err := os.Lstat(yol); err == nil {
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("%s düzenli dosya değil", filepath.Base(yol))
		}
	} else {
		f, err := os.OpenFile(yol, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mod)
		if err != nil {
			return err
		}
		_ = f.Close()
	}
	if err := os.Chmod(yol, mod); err != nil {
		return err
	}
	if nginxGrup {
		pmNginxGrup(yol)
	}
	return nil
}

// pmAtomikYaz — geçici dosya + izin + rename (el sıkışma yarım dosya okumasın).
func pmAtomikYaz(yol string, veri []byte, mod os.FileMode, nginxGrup bool) error {
	gec := yol + ".gosp-yeni"
	_ = os.Remove(gec)
	f, err := os.OpenFile(gec, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mod)
	if err != nil {
		return err
	}
	if _, err := f.Write(veri); err != nil {
		_ = f.Close()
		_ = os.Remove(gec)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(gec)
		return err
	}
	_ = os.Chmod(gec, mod)
	if nginxGrup {
		pmNginxGrup(gec)
	}
	return os.Rename(gec, yol)
}

// pmVarsayilanEsitle — panel sertifikasının worker'ın okuyabildiği kopyası
// (değişkenli kipte map'in default'u). Panel çifti geçersizse (yarım yenileme)
// DOKUNMAZ; kopya hiç yoksa hata (değişkenli kip açılamaz). (değişti mi, hata)
func pmVarsayilanEsitle() (bool, error) {
	if _, ok := pmCiftGecerli(pmPanelCrt, pmPanelKey, ""); !ok {
		if _, ok2 := pmCiftGecerli(pmVarsCrt, pmVarsKey, ""); ok2 {
			return false, nil // eski kopya geçerli: sonraki döngü eşitler
		}
		return false, errors.New("panel sertifika/anahtar çifti okunamadı ya da eşleşmiyor")
	}
	if err := pmDizinHazirla(); err != nil {
		return false, err
	}
	crt, _ := os.ReadFile(pmPanelCrt)
	key, _ := os.ReadFile(pmPanelKey)
	degisti := false
	if eski, err := os.ReadFile(pmVarsKey); err != nil || !bytes.Equal(eski, key) {
		if err := pmAtomikYaz(pmVarsKey, key, 0o640, true); err != nil {
			return false, err
		}
		degisti = true
	} else {
		_ = os.Chmod(pmVarsKey, 0o640)
		pmNginxGrup(pmVarsKey)
	}
	if eski, err := os.ReadFile(pmVarsCrt); err != nil || !bytes.Equal(eski, crt) {
		if err := pmAtomikYaz(pmVarsCrt, crt, 0o644, false); err != nil {
			return false, err
		}
		degisti = true
	}
	if degisti {
		pmRestorecon(pmSertDizin)
	}
	return degisti, nil
}

// ── nginx ───────────────────────────────────────────────────────────────────

// pmPanelAdlari — _panel.conf'un kendi server_name adları (include HARİÇ).
func pmPanelAdlari() map[string]bool {
	out := map[string]bool{}
	b, err := os.ReadFile(kapiVhostYolu)
	if err != nil {
		return out
	}
	for _, ln := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, "server_name ") {
			continue
		}
		for _, f := range strings.Fields(strings.TrimSuffix(strings.TrimPrefix(t, "server_name "), ";")) {
			out[strings.ToLower(strings.TrimSuffix(f, ";"))] = true
		}
	}
	return out
}

func pmGenelPort(conf string) int {
	a, err := kapi.Oku(kapiAyarYolu)
	if err != nil {
		a = nil
	}
	if p := kapi.GenelPanelPortu(conf, a); p > 0 {
		return p
	}
	return 8443
}

func pmDosya(yol string, icerik string, ozel bool) nginxDosya {
	eski, err := os.ReadFile(yol)
	if err != nil {
		eski = nil
	}
	if icerik == "" {
		return nginxDosya{yol: yol, eski: eski, sil: true, ozel: ozel}
	}
	return nginxDosya{yol: yol, eski: eski, yeni: []byte(icerik), ozel: ozel}
}

// pmNginxUyumla — :80 (acme80) + etkin küme dosyalarını üretir, kapiNginxUygula ile
// atomik uygular ve (olc ise) TLS ile ölçer. Varsayılan el sıkışma bozulursa GERİ ALIR.
// Dönüş: (uygulandı ya da değişiklik yok, geri alındı mı, kendi sertifikasını sunmayan adlar).
func pmNginxUyumla(acme80, etkin []string, olc bool) (bool, bool, map[string]bool) {
	sunmayan := map[string]bool{}
	kapiMu.Lock()
	defer kapiMu.Unlock()
	origB, err := os.ReadFile(kapiVhostYolu)
	if err != nil {
		return len(acme80) == 0 && len(etkin) == 0, false, sunmayan // panel bu host'ta kurulu değil
	}
	orig := string(origB)
	port := pmGenelPort(orig)

	acmeIcerik, err := marka.AcmeVhost80(acme80, port)
	if err != nil {
		log.Printf("🔴 panelmarka: :80 vhost üretilemedi: %v", err)
		return false, false, sunmayan
	}
	dosyalar := []nginxDosya{pmDosya(kapiMarkaAcmeYolu, acmeIcerik, true)}
	yeniConf := orig
	varsDegisti := false
	if len(etkin) > 0 {
		d, err := pmVarsayilanEsitle()
		if err != nil {
			log.Printf("🔴 panelmarka: varsayılan sertifika kopyası hazırlanamadı: %v — etkinleştirme yapılmadı", err)
			return false, false, sunmayan
		}
		varsDegisti = d
		sni, err1 := marka.SNIHaritasi(etkin)
		inc, err2 := marka.AdlarInclude(etkin)
		uy, _, err3 := marka.PanelConfUyarla(orig)
		if err := errors.Join(err1, err2, err3); err != nil {
			log.Printf("🔴 panelmarka: nginx içeriği üretilemedi: %v", err)
			return false, false, sunmayan
		}
		if err := os.MkdirAll(filepath.Dir(pmIncYolu), 0o755); err != nil {
			log.Printf("🔴 panelmarka: %s: %v", filepath.Dir(pmIncYolu), err)
			return false, false, sunmayan
		}
		yeniConf = uy
		dosyalar = append(dosyalar, pmDosya(pmSNIYolu, sni, true), pmDosya(pmIncYolu, inc, true))
	} else {
		if g, d := marka.PanelConfGeriAl(orig); d {
			yeniConf = g
		}
		dosyalar = append(dosyalar, pmDosya(pmSNIYolu, "", true), pmDosya(pmIncYolu, "", true))
	}
	dosyalar = append(dosyalar, nginxDosya{yol: kapiVhostYolu, eski: origB, yeni: []byte(yeniConf)})

	degisecek := varsDegisti
	for _, d := range dosyalar {
		if d.degisir() {
			degisecek = true
		}
	}
	// Ölçüm tabanı: değişiklikten ÖNCE varsayılan el sıkışma çalışıyor muydu? Çalışmıyorsa
	// (nginx durmuş, port başka adreste) bozulmayı biz yapmış sayılmayız → ölçüm atlanır
	// (ad başına ölçüm de: erişilemeyen nginx'te her adı 'hata' saymak yanlış olurdu).
	if !degisecek {
		if olc && len(etkin) > 0 && pmTLSDene(port, "", 1) == nil {
			return true, false, pmSunumOlc(true, port, etkin, sunmayan)
		}
		return true, false, sunmayan
	}
	oncesi := pmTLSDene(port, "", 1) == nil
	// Varsayılan kopyanın yenilenmesi tek başına reload istemez: değişkenli sertifika
	// her el sıkışmada diskten okunur.
	tamam, _ := kapiNginxUygula(dosyalar, nil, false)
	if !tamam {
		return false, false, sunmayan
	}
	kapiVhostIzin()
	if oncesi {
		if err := pmTLSDene(port, "", 6); err != nil {
			// 🔴 KİLİTLENME KORUMASI: panel kimseye açılmıyor → hepsini geri al.
			ters := make([]nginxDosya, 0, len(dosyalar))
			for _, d := range dosyalar {
				if !d.degisir() {
					continue
				}
				yeni, _ := os.ReadFile(d.yol)
				if d.sil {
					yeni = nil
				}
				r := nginxDosya{yol: d.yol, eski: yeni, yeni: d.eski, ozel: d.ozel, onarim: true}
				if d.eski == nil {
					r.sil = true
				}
				ters = append(ters, r)
			}
			geri, _ := kapiNginxUygula(ters, nil, true)
			log.Printf("🔴 panelmarka: değişiklikten sonra varsayılan TLS el sıkışması başarısız (%v) — geri alındı (geri alma tamam=%v)", err, geri)
			return false, true, sunmayan
		}
	}
	return true, false, pmSunumOlc(olc && oncesi, port, etkin, sunmayan)
}

// pmSunumOlc — her etkin ad kendi sertifikasını sunuyor mu (reload asenkron: dene).
func pmSunumOlc(olc bool, port int, etkin []string, sunmayan map[string]bool) map[string]bool {
	if !olc {
		return sunmayan
	}
	for _, ad := range etkin {
		bekle := pmYaprakDER(pmCrt(ad))
		var son []byte
		for i := 0; i < 6; i++ {
			if y, err := pmTLSOlc(port, ad); err == nil {
				son = y
				if bytes.Equal(y, bekle) {
					break
				}
			}
			time.Sleep(pmOlcBekle)
		}
		if len(bekle) == 0 || !bytes.Equal(son, bekle) {
			sunmayan[ad] = true
		}
	}
	return sunmayan
}

func pmTLSDene(port int, sni string, deneme int) error {
	var err error
	for i := 0; i < deneme; i++ {
		if _, err = pmTLSOlc(port, sni); err == nil {
			return nil
		}
		if i+1 < deneme {
			time.Sleep(pmOlcBekle)
		}
	}
	return err
}

// pmTLSYaprak — 127.0.0.1:port'a TLS el sıkışması (sni "" → SNI gönderilmez);
// sunulan yaprak sertifikanın DER'i. Doğrulama YOK (ölçüm: hangi sertifikanın
// sunulduğu ham bayt karşılaştırmasıyla denetlenir; staging/kendinden imzalı da ölçülür).
func pmTLSYaprak(port int, sni string) ([]byte, error) {
	d := &net.Dialer{Timeout: 4 * time.Second}
	c, err := tls.DialWithDialer(d, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		&tls.Config{ServerName: sni, InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}) // #nosec G402 -- ölçüm, güven kararı değil
	if err != nil {
		return nil, err
	}
	defer c.Close()
	st := c.ConnectionState()
	if len(st.PeerCertificates) == 0 {
		return nil, errors.New("sertifika sunulmadı")
	}
	return st.PeerCertificates[0].Raw, nil
}

// ── temizlik ────────────────────────────────────────────────────────────────

// pmTemizle — tut dışındaki adların sertifika dosyaları ve acme.sh kayıtları.
// Etkin ad kalmadıysa varsayılan kopya da silinir (statik kipe dönüldü).
func pmTemizle(tut map[string]bool) {
	if girdiler, err := os.ReadDir(pmSertDizin); err == nil {
		etkinVar := false
		if b, err := os.ReadFile(kapiVhostYolu); err == nil && marka.PanelConfDegiskenli(string(b)) {
			etkinVar = true
		}
		for _, g := range girdiler {
			ad := g.Name()
			if !g.Type().IsRegular() {
				continue
			}
			taban := strings.TrimSuffix(strings.TrimSuffix(ad, ".crt"), ".key")
			if taban == ad {
				continue
			}
			if taban == "_varsayilan" {
				if !etkinVar {
					_ = os.Remove(filepath.Join(pmSertDizin, ad))
				}
				continue
			}
			if marka.HostnameGecerli(taban) && !tut[taban] {
				_ = os.Remove(filepath.Join(pmSertDizin, ad))
			}
		}
	}
	pmAcmeKayitTemizle(tut)
}

// pmAcmeKayitTemizle — acme.sh config-home'da, sertifikası BayiSertDizin'e kurulan
// (Le_RealFullChainPath) ve tut dışında kalan adların yenileme kayıtlarını siler.
// Panelin kendi kaydı (panel.crt'ye kurulu) bu ölçütle ASLA eşleşmez.
func pmAcmeKayitTemizle(tut map[string]bool) {
	girdiler, err := os.ReadDir(pmAcmeHome)
	if err != nil {
		return
	}
	onek := filepath.Clean(pmSertDizin) + "/"
	for _, g := range girdiler {
		if !g.IsDir() {
			continue
		}
		ad := strings.TrimSuffix(g.Name(), "_ecc")
		if !marka.HostnameGecerli(ad) || tut[ad] {
			continue
		}
		dizin := filepath.Join(pmAcmeHome, g.Name())
		b, err := os.ReadFile(filepath.Join(dizin, ad+".conf"))
		if err != nil {
			continue
		}
		yol := acmeConfDeger(string(b), "Le_RealFullChainPath")
		if !strings.HasPrefix(filepath.Clean(yol), onek) || filepath.Clean(yol) != filepath.Join(pmSertDizin, ad+".crt") {
			continue
		}
		c, iptal := context.WithTimeout(context.Background(), time.Minute)
		_, _ = pmAcmeKomut(c, "--remove", "-d", ad, "--ecc", "--config-home", pmAcmeHome)
		iptal()
		if err := os.RemoveAll(dizin); err != nil {
			log.Printf("panelmarka: acme kaydı silinemedi (%s): %v", ad, err)
		} else {
			log.Printf("panelmarka: %s acme yenileme kaydı kaldırıldı", ad)
		}
	}
}

// acmeConfDeger — acme.sh alan conf'undaki KEY='değer' satırı.
func acmeConfDeger(conf, anahtar string) string {
	for _, ln := range strings.Split(conf, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(ln), anahtar+"="); ok {
			return strings.Trim(v, `'"`)
		}
	}
	return ""
}

func kirpPM(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func sonSatirlarPM(s string, n int) string {
	satirlar := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(satirlar) > n {
		satirlar = satirlar[len(satirlar)-n:]
	}
	return strings.Join(satirlar, "\n")
}
