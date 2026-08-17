package hostuyg

// Kurulum boru hattı — Faz 3.1 TAM implementasyon.
//
// Sıra:
//   1. Kurulum dizini oluştur
//   2. Sistem user yarat
//   3. DB kayıt (durum=kurulmakta)
//   4. Port ayır
//   5. Binary indir + SHA doğrula + çıkar
//   6. Config render + yaz
//   7. Dizin sahipliğini app user'a devret
//   8. Systemd unit yaz + daemon-reload
//   9. Firewall port aç (recipe web değilse — nginx proxy varsa portları
//      dışardan açma, sadece 127.0.0.1'de dinlensin)
//   10. Nginx reverse proxy (varsa)
//   11. Enable + start
//   12. Sağlık kontrolü
//   13. Başarılı → durum=aktif
//   Fail → rollback (tersine tüm adımlar)

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"
)

const KurulumKok = "/opt/gpanel-apps"

// C3 fix: process-lokal per-(kod,ornek_ad) kilit. İki paralel Kur aynı isme
// çakıştıysa ikincisi hemen reddedilir → 1. install'ın user/dizin'i rollback
// tarafından silinmez.
var (
	kurulumKilit sync.Mutex
	kurulumAktif = map[string]bool{}
)

func kurulumKilitleAl(kod, ornekAd string) bool {
	k := kod + "|" + ornekAd
	kurulumKilit.Lock()
	defer kurulumKilit.Unlock()
	if kurulumAktif[k] {
		return false
	}
	kurulumAktif[k] = true
	return true
}
func kurulumKilitSerbest(kod, ornekAd string) {
	kurulumKilit.Lock()
	defer kurulumKilit.Unlock()
	delete(kurulumAktif, kod+"|"+ornekAd)
}

// PanelHostVarsayilan — nginx proxy render'ında {panelhost} yerine konur.
// main.go set edebilir (panelhost.DurumOku().Hostname).
var PanelHostVarsayilan = "localhost"

// KurAsync — kurulum işini başlat + iş kaydını döner. Handler ve test için
// public giriş noktası. C1 fix: OrnekAdDogrula burada zorunlu.
func KurAsync(db *sql.DB, tarif *Tarif, ornekAd string, aktor *int64) (*Is, error) {
	if ornekAd == "" {
		ornekAd = tarif.Kod
	}
	// N2 fix: kod+ornek_ad birlikte 32 char sınırını aşarsa truncation
	// collision. OrnekAdVeKodDogrula her ikisini kontrol eder.
	if err := OrnekAdVeKodDogrula(tarif.Kod, ornekAd); err != nil {
		return nil, err
	}
	// Aynı SubPathOn'a başka aktif kurulum varsa red (nginx çakışma önle)
	if tarif.NginxProxy != nil && tarif.NginxProxy.SubPathOn != "" && db != nil {
		var mev int
		_ = db.QueryRow(`SELECT COUNT(*) FROM cp_host_uygulamalar WHERE kod=?`, tarif.Kod).Scan(&mev)
		if mev > 0 {
			return nil, fmt.Errorf("bu recipe zaten kurulu (aynı %s subpath'i çakışır) — önce mevcut instance'ı kaldır",
				tarif.NginxProxy.SubPathOn)
		}
	}
	if !kurulumKilitleAl(tarif.Kod, ornekAd) {
		return nil, errors.New("aynı ornek_ad için zaten bir kurulum çalışıyor")
	}
	is := IsBaslat("kur", tarif.Kod, 0)
	go func() {
		defer kurulumKilitSerbest(tarif.Kod, ornekAd)
		kurulumYurut(db, is, tarif, ornekAd, aktor)
	}()
	return is, nil
}

// KaldirAsync — kaldırma işi.
func KaldirAsync(db *sql.DB, k *UygulamaKayit, aktor *int64) *Is {
	is := IsBaslat("kaldir", k.Kod, k.ID)
	go kaldirYurut(db, is, k, aktor)
	return is
}

func kurulumYurut(db *sql.DB, is *Is, tarif *Tarif, ornekAd string, aktor *int64) {
	// I7 fix: rollback closure defer'in ÜSTÜNDE tanımlı olacak — recover
	// içinden çağır ki panic durumunda user/dir/DB/nginx yarım kalmasın.
	// Rollback closure aşağıda oluşturulduğu için: bir ptr tut.
	var rollbackRef func(string)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("hostuyg.kurulumYurut PANIC: %v\n%s", r, debug.Stack())
			if rollbackRef != nil {
				rollbackRef(fmt.Sprintf("panik: %v", r))
			}
			is.Bitir(fmt.Sprintf("panik: %v", r))
		}
	}()

	sistemKul := KullaniciAdi(tarif.Kod, ornekAd)
	kurulumYolu := filepath.Join(KurulumKok, tarif.Kod+"-"+ornekAd)
	unitAd := "gpanel-app-" + tarif.Kod
	if ornekAd != tarif.Kod {
		unitAd = "gpanel-app-" + tarif.Kod + "-" + ornekAd
	}

	// -------- Rollback helper closure --------
	// Kurulum ilerledikçe bu bayraklar açılır; fail edilirse rollback bunlara
	// göre tersine gider.
	var (
		uygID        int64
		portlar      map[string]int
		userYapildi  bool
		unitYapildi  bool
		nginxYapildi bool
		servisAcik   bool
	)
	// Important-2 fix: rollbackRef fn ptr'yi closure tanımından HEMEN sonra
	// ata; aradaki `bitirHata := ...` satırı sırasında panic olsa bile
	// rollback erişilebilir olsun.
	rollback := func(reason string) {
		is.AdimEkle("ROLLBACK: "+reason, false)
		defer func() { _ = recover() }() // rollback'in kendisi panic'lerse yut
		if servisAcik {
			ctx, iptal := context.WithTimeout(context.Background(), 10*time.Second)
			_ = UnitStopDisable(ctx, unitAd)
			iptal()
			is.AdimEkle("  ← servis durduruldu", true)
		}
		if nginxYapildi {
			// Rollback: SubdomainAd DB'ye zaten INSERT edildi (adım 4 DB kayıt)
			// → cert var olabilir → subdomain adı geçir ki cert de silinsin.
			sd := ""
			if tarif.NginxProxy != nil && tarif.NginxProxy.Subdomain {
				sd = SubdomainAdi(tarif.NginxProxy.SubdomainOn, PanelHostVarsayilan)
			}
			_ = NginxProxyKaldir(tarif.Kod, sd)
			is.AdimEkle("  ← nginx proxy kaldırıldı", true)
		}
		if unitYapildi {
			_ = UnitSil(unitAd)
			_ = EnvDosyaSil(unitAd) // C2: secret env dosyası da temizle
			is.AdimEkle("  ← systemd unit + env dosyası silindi", true)
		}
		// Doğru protokolü tarif üzerinden eşleştir (hem-tcp-hem-udp yerine).
		for _, pt := range tarif.Portlar {
			if p, ok := portlar[pt.Ad]; ok {
				_ = FirewallPortKapat(p, pt.Protokol)
			}
		}
		if userYapildi {
			_ = KullaniciSil(sistemKul)
			is.AdimEkle("  ← sistem user silindi", true)
		}
		_ = os.RemoveAll(kurulumYolu)
		if uygID > 0 {
			_ = UygulamaSil(context.Background(), db, uygID)
			is.AdimEkle("  ← DB kayıt + port CASCADE silindi", true)
		}
	}
	rollbackRef = rollback // Important-2 fix: rollback tanımından hemen sonra
	bitirHata := func(step string, err error) {
		is.AdimEkle(step+": "+err.Error(), false)
		rollback(err.Error())
		is.Bitir(step + ": " + err.Error())
	}

	// -------- 1) Recipe validasyonu (ayrıca) --------
	if err := tarif.Dogrula(); err != nil {
		is.AdimEkle("recipe validation: "+err.Error(), false)
		is.Bitir("recipe geçersiz")
		return
	}
	is.AdimEkle("recipe validation OK", true)

	// -------- 2) Kurulum dizini --------
	// C3 fix: dizin zaten varsa hata — başka install'ı ezmeyi engelle.
	if _, err := os.Stat(kurulumYolu); err == nil {
		bitirHata("dizin", fmt.Errorf("%s zaten var (başka bir install kalıntısı olabilir)", kurulumYolu))
		return
	}
	if err := os.MkdirAll(kurulumYolu, 0755); err != nil {
		bitirHata("dizin", err)
		return
	}
	is.AdimEkle("kurulum dizini: "+kurulumYolu, true)

	// -------- 3) Sistem user --------
	// C3 fix: user zaten varsa hata — idempotent değil.
	if KullaniciVarMi(sistemKul) {
		bitirHata("user", fmt.Errorf("user %s zaten var (başka bir install kalıntısı olabilir)", sistemKul))
		return
	}
	if err := KullaniciYarat(sistemKul, kurulumYolu); err != nil {
		bitirHata("user", err)
		return
	}
	userYapildi = true
	is.AdimEkle("sistem user: "+sistemKul, true)

	// -------- 4) DB kayıt --------
	kayit := &UygulamaKayit{
		Kod: tarif.Kod, OrnekAd: ornekAd, Surum: tarif.Surum,
		KurulumYolu: kurulumYolu, SistemKullanici: sistemKul,
		SystemdUnit: unitAd, Durum: "kurulmakta", MetaJSON: "{}",
	}
	// Faz 3.4: subdomain adı persist et (kaldirma sırasında cert cleanup için)
	if tarif.NginxProxy != nil && tarif.NginxProxy.Subdomain {
		kayit.SubdomainAd = SubdomainAdi(tarif.NginxProxy.SubdomainOn, PanelHostVarsayilan)
	}
	ctx, iptal := context.WithTimeout(context.Background(), 5*time.Second)
	uygIDLocal, err := UygulamaYaz(ctx, db, kayit, aktor)
	iptal()
	if err != nil {
		bitirHata("DB", err)
		return
	}
	uygID = uygIDLocal
	is.UygulamaID = uygID
	is.AdimEkle(fmt.Sprintf("DB kayıt (id=%d)", uygID), true)

	// -------- 5) Port ayır --------
	ctx, iptal = context.WithTimeout(context.Background(), 10*time.Second)
	portlar, err = PortAyir(ctx, db, uygID, tarif.Portlar)
	iptal()
	if err != nil {
		bitirHata("port", err)
		return
	}
	is.AdimEkle(fmt.Sprintf("portlar: %v", portlar), true)

	// -------- 6) Binary indir + SHA doğrula --------
	is.AdimEkle("indiriliyor: "+tarif.IndirmeURL, true)
	indirilenYol, err := Indir(tarif.IndirmeURL, tarif.SHA256)
	if err != nil {
		bitirHata("indirme", err)
		return
	}
	defer os.Remove(indirilenYol)
	is.AdimEkle("SHA256 doğrulandı", true)

	// -------- 7) Çıkar --------
	if err := Cikart(indirilenYol, kurulumYolu, tarif.IcerikTuru, tarif.BinaryYol); err != nil {
		bitirHata("çıkarma", err)
		return
	}
	is.AdimEkle("içerik açıldı: "+tarif.IcerikTuru, true)

	// -------- 8) Secret üret + config render --------
	secretler := map[string]string{
		"secret16": secretUret(16),
		"secret32": secretUret(32),
		"secret64": secretUret(64),
	}
	if err := ConfigYaz(tarif, kurulumYolu, sistemKul, PanelHostVarsayilan, portlar, secretler); err != nil {
		bitirHata("config", err)
		return
	}
	if len(tarif.ConfigDosyalar) > 0 {
		is.AdimEkle(fmt.Sprintf("%d config dosyası yazıldı", len(tarif.ConfigDosyalar)), true)
	}

	// -------- 9) Dizin sahipliği app user'a --------
	if err := DizinSahipDegistir(kurulumYolu, sistemKul); err != nil {
		bitirHata("chown", err)
		return
	}
	is.AdimEkle("dizin sahipliği "+sistemKul, true)

	// -------- 10) Systemd unit + EnvironmentFile 0400 (C2) --------
	unitIcerik, envMap := UnitRender(tarif, unitAd, sistemKul, kurulumYolu, portlar, secretler, PanelHostVarsayilan)
	if len(envMap) > 0 {
		if err := EnvDosyaYaz(unitAd, envMap); err != nil {
			bitirHata("env dosya", err)
			return
		}
		is.AdimEkle(fmt.Sprintf("secret env dosyası 0400: %s", EnvDosyaYolu(unitAd)), true)
	}
	if err := UnitYaz(unitAd, unitIcerik); err != nil {
		_ = EnvDosyaSil(unitAd)
		bitirHata("unit yaz", err)
		return
	}
	unitYapildi = true
	is.AdimEkle("systemd unit: "+unitAd+".service", true)

	// -------- 11) Firewall (nginx proxy YOKSA port'lar dışardan açık;
	//        varsa 127.0.0.1'de kalır, dışardan erişim nginx'ten) --------
	// Firewall port aç: NginxProxy YOKSA tüm portlar; VARSA yalnız DisAcik=true
	// olanlar (Syncthing P2P sync, MC voice vb.).
	for _, pt := range tarif.Portlar {
		acilmali := tarif.NginxProxy == nil || pt.DisAcik
		if !acilmali {
			continue
		}
		port, ok := portlar[pt.Ad]
		if !ok {
			continue
		}
		if err := FirewallPortAc(port, pt.Protokol); err != nil {
			is.AdimEkle("firewall "+pt.Ad+" uyarı: "+err.Error(), false)
		} else {
			is.AdimEkle(fmt.Sprintf("firewall açıldı %d/%s (%s)", port, pt.Protokol, pt.Ad), true)
			_, _ = db.Exec(`UPDATE cp_host_uyg_portlari SET firewall_acik=1
				WHERE uygulama_id=? AND port=? AND protokol=?`, uygID, port, pt.Protokol)
		}
	}
	if tarif.NginxProxy != nil {
		is.AdimEkle("nginx reverse proxy modu (yalnız DisAcik port'lar firewall'da)", true)
	}

	// -------- 12) Nginx proxy (web app'ler için) --------
	if tarif.NginxProxy != nil {
		// Ana web port'unu al (ilk tcp port veya "web" adlı)
		webPort := portlar["web"]
		if webPort == 0 {
			for _, pt := range tarif.Portlar {
				if pt.Protokol == "tcp" {
					webPort = portlar[pt.Ad]
					break
				}
			}
		}
		if webPort == 0 {
			bitirHata("nginx proxy", fmt.Errorf("web portu bulunamadı"))
			return
		}
		if err := NginxProxyKur(tarif.Kod, tarif, webPort); err != nil {
			bitirHata("nginx", err)
			return
		}
		nginxYapildi = true
		is.AdimEkle("nginx reverse proxy: "+tarif.NginxProxy.SubPathOn, true)
	}

	// -------- 13) Enable + start --------
	ctx, iptal = context.WithTimeout(context.Background(), 15*time.Second)
	err = UnitEnableStart(ctx, unitAd)
	iptal()
	if err != nil {
		bitirHata("start", err)
		return
	}
	servisAcik = true
	is.AdimEkle("systemctl enable + start OK", true)

	// -------- 14) Sağlık kontrolü --------
	ok, mesaj := SaglikBekle(tarif, portlar)
	if !ok {
		bitirHata("sağlık", errors.New(mesaj))
		return
	}
	is.AdimEkle("sağlık: "+mesaj, true)

	// -------- 14b) Recipe post-install kancası (best-effort) --------
	PostInstallHook(db, uygID, tarif.Kod, unitAd)

	// -------- 15) Başarı --------
	_ = UygulamaDurumGuncelle(context.Background(), db, uygID, "aktif", "")
	is.AdimEkle("kurulum tamamlandı — durum=aktif", true)
	is.Bitir("")
}

// I4 fix: kaldirYurut hataları artık takip ediliyor. Kritik hata → DB kaydı
// SİLİNMEZ, durum='kaldirma_hata' bırakılır → UI'da "manuel temizle" opsiyonu.
func kaldirYurut(db *sql.DB, is *Is, k *UygulamaKayit, aktor *int64) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("hostuyg.kaldirYurut PANIC: %v\n%s", r, debug.Stack())
			is.Bitir(fmt.Sprintf("panik: %v", r))
		}
	}()
	_ = UygulamaDurumGuncelle(context.Background(), db, k.ID, "kaldirilmakta", "")

	var hatalar []string
	edinHata := func(step string, err error) {
		if err == nil {
			return
		}
		hatalar = append(hatalar, step+": "+err.Error())
		is.AdimEkle(step+" HATA: "+err.Error(), false)
	}

	// 1) Unit stop + disable
	ctx, iptal := context.WithTimeout(context.Background(), 15*time.Second)
	_ = UnitStopDisable(ctx, k.SystemdUnit)
	iptal()
	is.AdimEkle("unit durduruldu + disable", true)

	// 2) Nginx proxy kaldır
	if err := NginxProxyKaldir(k.Kod, k.SubdomainAd); err != nil {
		edinHata("nginx kaldır", err)
	} else {
		is.AdimEkle("nginx proxy kaldırıldı (varsa)", true)
	}

	// 3) Firewall port kapat
	for _, p := range k.Portlar {
		if p.FirewallAcik {
			if err := FirewallPortKapat(p.Port, p.Protokol); err != nil {
				edinHata(fmt.Sprintf("firewall %d/%s", p.Port, p.Protokol), err)
			}
		}
	}
	is.AdimEkle(fmt.Sprintf("firewall %d port işlemi", len(k.Portlar)), true)

	// 4) Env dosya (C2) + unit sil
	_ = EnvDosyaSil(k.SystemdUnit)
	if err := UnitSil(k.SystemdUnit); err != nil {
		edinHata("unit sil", err)
	} else {
		is.AdimEkle("unit + env dosyası silindi", true)
	}

	// 5) User sil (home dahil)
	if err := KullaniciSil(k.SistemKullanici); err != nil {
		edinHata("user sil", err)
	} else {
		is.AdimEkle("sistem user silindi: "+k.SistemKullanici, true)
	}

	// 6) Kurulum dizini
	if err := os.RemoveAll(k.KurulumYolu); err != nil {
		edinHata("dizin sil", err)
	} else {
		is.AdimEkle("kurulum dizini temizlendi", true)
	}

	// Kritik hata varsa DB'yi silme — orphan olarak işaretle
	if len(hatalar) > 0 {
		_ = UygulamaDurumGuncelle(context.Background(), db, k.ID, "kaldirma_hata",
			"kaldırma sırasında "+fmt.Sprintf("%d", len(hatalar))+" hata — manuel temizle")
		is.AdimEkle("DB kaydı KORUNDU (orphan işaretli, manuel temizle gerekli)", false)
		is.Bitir("kaldırma kısmi başarısız: " + hatalar[0])
		return
	}

	// 7) DB kayıt sil (portlar CASCADE)
	if err := UygulamaSil(context.Background(), db, k.ID); err != nil {
		is.AdimEkle("DB sil hata: "+err.Error(), false)
		is.Bitir("db hata")
		return
	}
	is.AdimEkle("DB kayıt silindi (port CASCADE)", true)
	MetrikCacheSil(k.SystemdUnit, k.KurulumYolu) // MV8 cleanup
	is.Bitir("")
}

// I5 fix: rand.Read hatasını yutmak öngörülebilir secret üretir. Fail = panic.
func secretUret(uzunluk int) string {
	b := make([]byte, uzunluk/2)
	if _, err := rand.Read(b); err != nil {
		panic("hostuyg.secretUret: crypto/rand fail: " + err.Error())
	}
	return hex.EncodeToString(b)
}
