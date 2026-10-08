package tasima

import (
	"context"
	"database/sql"
	"fmt"
	"girginospanel/internal/backups"
	"girginospanel/internal/efektiflimit"
	stdlog "log"
	"path/filepath"
	"strings"
	"time"

	"girginospanel/internal/domains"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/kaynaklimit"
	bayikilit "girginospanel/internal/kilit"
	"girginospanel/internal/plans"
	"girginospanel/internal/provisioner"
)

// kotaDenetle — bayi hosting kotası (domains.resellerKotaKontrol; testler değiştirir).
var kotaDenetle = domains.TasimaKotaKontrol

// diskHavuzuDenetle — bayi fiili disk havuzu (testler değiştirir).
var diskHavuzuDenetle func(h *Handlers, ctx context.Context, rid, domainID, bayt int64) error

func init() { diskHavuzuDenetle = (*Handlers).bayiDiskHavuzu }

// saglama / saglamaGeriAl / limitUygula: sistem hesabı + vhost kurulumu ve plan
// limitleri (systemd slice, kiracı FPM) — testler değiştirir; ret ve geri alma
// yollarının useradd/nginx/systemd yan etkisine HİÇ ulaşmadığını kanıtlamak için.
var (
	saglama       = provisioner.Provision
	saglamaGeriAl = provisioner.Deprovision
	limitUygula   = kaynaklimit.UygulaHepsi
)

// AktarSonuc — bir hesabin tasima ciktisi.
type AktarSonuc struct {
	DomainID    int64
	DosyaBayt   int64
	DBSayisi    int
	DNSSayisi   int
	PostaSayisi int
	Uyarilar    []string
}

// HesapAktar — TEK bir hesabi/domaini uctan uca tasir.
//
// Sira: hedef kontrolu → saglama → domains kaydi → FTP → dosyalar → veritabani
// (+ yapilandirma yeniden yazma) → DNS → SSL.
//
// 🔴 Veri guvenligi kurallari:
//   - Hedefte var olan bir domaine yazarken (ustune) rsync SILME YAPMAZ; mevcut
//     dosyalar korunur, kaynaktakiler uzerine yazilir.
//   - Veritabani parolasi domain basina BIR KEZ uretilir; mevcut DB kullanicisi
//     varsa parolasi DEGISTIRILMEZ (canli siteyi olduruyordu).
//   - Veritabani adimi basarisiz olursa kalem BASARILI sayilmaz.
//
// 🔴 KAPSAMLI HATA: dönen HER hata kapsama göre süzülür. Kök ham hatayı görür
// (mevcut davranış birebir); bayiye komşu kiracının adı / sistem yolu / araç
// çıktısı sızmaz (bkz. hataMetni). Bayi hatasının HAM hâli sunucu günlüğüne
// yazılır: bayiye dönen genel metin bunu söyler, kök arızayı oradan ayıklar.
//
// Süzme bu ince sarmalayıcıdadır, iç fonksiyonda adlandırılmış dönüşle DEĞİL:
// `return nil, err` adlandırılmış sonucu ertelenmiş fonksiyonlardan ÖNCE nil'ler;
// geri alma defer'i (sonuc.DomainID) nil işaretçiye düşer, yeni açılan hesap
// (domain satırı, sistem kullanıcısı, vhost) geri alınmadan öksüz kalırdı.
func (h *Handlers) HesapAktar(ctx context.Context, k *Kaynak, hs Hesap, ay Ayarlar, kapsam Kapsam, log func(string, ...any)) (*AktarSonuc, error) {
	sonuc, err := h.hesapAktarIc(ctx, k, hs, ay, kapsam, log)
	if err != nil {
		if kapsam.Bayi() {
			stdlog.Printf("tasima: kalem hatası (bayi=%d alan=%q): %s",
				kapsam.RID, kisalt(hs.AlanAdi, 253), kisalt(temizHata(err.Error()), 1000))
		}
		return nil, hataMetni(kapsam.Yonetici(), err)
	}
	return sonuc, nil
}

// hesapAktarIc — HesapAktar'ın gövdesi; hatayı HAM döndürür (süzme çağıranda).
func (h *Handlers) hesapAktarIc(ctx context.Context, k *Kaynak, hs Hesap, ay Ayarlar, kapsam Kapsam, log func(string, ...any)) (*AktarSonuc, error) {
	ctx, iptal := context.WithTimeout(ctx, hesapSuresi(k))
	defer iptal()

	sonuc := &AktarSonuc{}
	alanAdi := strings.ToLower(strings.TrimSpace(hs.AlanAdi))
	if !reAlanAdi.MatchString(alanAdi) || !strings.Contains(alanAdi, ".") {
		return nil, fmt.Errorf("gecersiz alan adi")
	}

	// --- 1. Hedef kontrolu -------------------------------------------------
	var mevcutID, mevcutRID int64
	var mevcutSK, mevcutKok string
	err := h.DB.QueryRowContext(ctx,
		`SELECT id, sistem_kullanici, COALESCE(web_root,''), reseller_id FROM domains WHERE alan_adi=?`,
		alanAdi).Scan(&mevcutID, &mevcutSK, &mevcutKok, &mevcutRID)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("hedef kontrolu: %w", err)
	}
	yeniOlusturuldu := false
	var sk, webRoot string
	php := ay.HedefPHP
	if php == "" {
		php = hs.PHPSurum
	}
	if php == "" {
		php = "8.3"
	}

	if mevcutID > 0 {
		// 🔴 BAYİ ÜZERİNE YAZMA — SAHİP EŞİTLİĞİ: bir bayi yalnız KENDİ hesabındaki
		// bir domainin üzerine yazabilir. Aksi halde komşunun (kökün ya da başka
		// bayinin) sitesine dosya/DB yazardı. Kök (Yonetici) için sınır yok.
		if kapsam.Bayi() && mevcutRID != kapsam.RID {
			return nil, fmt.Errorf("hedef alan adı bu hesaba ait değil")
		}
		if !ay.Ustune {
			return nil, fmt.Errorf("hedefte '%s' zaten var (uzerine yazma kapali)", alanAdi)
		}
		sonuc.DomainID, sk = mevcutID, mevcutSK
		// 🔴 KİRACI EVİNE KİLİT: sonraki adımlar (yapılandırma yeniden yazımı ve
		// okuyucular) kiracıyı web kökü DİZGESİNDEN türetir. web_root/sistem_kullanici
		// (DB bozulması) başka kiracının evini, evin kendisini ya da ev dışını
		// gösteriyorsa o evde çalışırlardı → hiçbir adımdan önce reddet (kök dahil).
		ki, err := kiraciAl(sk)
		if err != nil {
			return nil, fmt.Errorf("hedef sistem kullanicisi gecersiz")
		}
		// 🔴 Belge koku alt klasor olabilir (or. Laravel .../public_html/public).
		// public_html'e yazarsak icerik HIC YAYINLANMAZ.
		webRoot = mevcutKok
		if webRoot == "" {
			webRoot = filepath.Join(ki.Ev, "public_html")
		}
		if _, err := ki.Goreli(webRoot); err != nil {
			return nil, fmt.Errorf("hedef web kok dizini kiracinin ev dizininde degil")
		}
		log("hedefte mevcut domain bulundu, uzerine yaziliyor (id=%d, kok=%s)", mevcutID, webRoot)
	} else {
		// 🔴 SAHİP SEÇİMİ KAPSAMLA BELİRLENİR:
		//   - Bayi işi (RID>0): site YALNIZ bayinin KENDİ hesabına açılır. Gövdeden
		//     gelen ResellerID/CustomerID ve kaynaktan gelen SahipEsle YOK SAYILIR
		//     (aksi halde bayi siteyi başka kiracıya atardı). CustomerID=0.
		//   - Kök işi (RID=0): sahiplik taşıma açıksa domainin KENDİ sahibini
		//     (SahipEsle[hs.SahipLogin]) kullan; yoksa Ayarlar varsayılanına düş.
		var resellerID, customerID int64
		if kapsam.Bayi() {
			resellerID, customerID = kapsam.RID, 0
		} else {
			resellerID, customerID = ay.ResellerID, ay.CustomerID
			if hs.SahipLogin != "" && ay.SahipEsle != nil {
				if hedef, ok := ay.SahipEsle[hs.SahipLogin]; ok {
					resellerID, customerID = hedef.ResellerID, hedef.CustomerID
				}
			}
		}
		// 🔴 PLAN KAPSAMI (bayi): gövdeden gelen PlanID domains.Create ile AYNI kuralla
		// (plans.BaglanabilirMi) denetlenir — bayi başka bayinin planını ya da bir
		// hostinge ÖZEL planı (kökünkü dahil) yeni siteye bağlayamaz; bağlasaydı limitler
		// (kaynaklimit) o plandan uygulanırdı. Sağlamadan ÖNCE (yan etkisiz).
		if kapsam.Bayi() && ay.PlanID != 0 {
			if err := plans.BaglanabilirMi(ctx, h.DB, kapsam.RID, ay.PlanID, 0); err != nil {
				return nil, fmt.Errorf("plan: %w", err)
			}
		}
		// 🔴 Ad-alanı (GL-02): domains.Create ile AYNI kural — oluşacak domainin
		// sahibine (resellerID) göre başka kiracının alt/üst adı, alt alan adı, panel
		// adları reddedilir. Sağlamadan ÖNCE (yan etkisiz); INSERT'te kilitle yeniden.
		if err := domains.AdAlaniDenetle(ctx, h.DB, alanAdi, resellerID); err != nil {
			return nil, fmt.Errorf("ad alanı: %w", err)
		}
		// 🔴 PLANSIZ BAYİ SİTESİ YOK: plansız hosting kaynak limitsiz açılır
		// (kaynaklimit: plansız = sınırsız slice). Baslat planı çözer (istenen ya da
		// varsayılan katalog); burası savunma derinliği. Sağlamadan ÖNCE.
		if kapsam.Bayi() && ay.PlanID <= 0 {
			return nil, errBayiPlanGerekli
		}
		// 🔴 BAYİ KOTASI + TOCTOU: kota okuması ile domains INSERT'i arasında bayi
		// kilidi TUTULUR (domains.Create ile aynı kilit/kural: max_domain, fiili disk,
		// taahhüt, askı). Kilit INSERT'ten hemen sonra bırakılır — rsync/dump boyunca
		// TUTULMAZ (askı/plan/silme işlemleri aynı kilidi bekler). Kota dolu ya da
		// bayi askıdaysa kalem 'atlandi' olur (kotaHatasi).
		kotaBirak := func() {}
		if kapsam.Bayi() {
			bk := bayikilit.Bayi(kapsam.RID)
			bk.Lock()
			birakildi := false
			kotaBirak = func() {
				if !birakildi {
					birakildi = true
					bk.Unlock()
				}
			}
			defer kotaBirak()
			pid := ay.PlanID
			if err := kotaDenetle(ctx, h.DB, kapsam.RID, &pid); err != nil {
				return nil, &kotaHatasi{mesaj: err.Error()}
			}
		}
		istenen := php
		php = kuruluPHPSec(php)
		if php != istenen {
			log("uyari: kaynak PHP %s hedefte kurulu degil → %s kullanilacak", istenen, php)
			sonuc.Uyarilar = append(sonuc.Uyarilar,
				fmt.Sprintf("PHP %s yok → %s", istenen, php))
		}
		log("sistem hesabi olusturuluyor (php %s)…", php)
		pr, err := saglama(alanAdi, php)
		if err != nil {
			return nil, fmt.Errorf("saglama: %w", err)
		}
		sk = pr.SistemKullanici
		webRoot = pr.WebRoot
		yeniOlusturuldu = true

		dbUser, dbAdi := sk+"_db", sk+"_main"
		// 🔴 durum='pasif': surec yarida olurse (panel restart) yarim domain
		// "aktif" gorunmesin. Basarida 'aktif'e cevrilir.
		// Sahiplik hedefi geçerli mi (bayi=reseller users satırı, müşteri=customers).
		if err := h.sahiplikDogrula(ctx, resellerID, customerID); err != nil {
			_ = saglamaGeriAl(alanAdi, sk)
			return nil, err
		}
		var res sql.Result
		if err := domains.AdAlaniEkle(ctx, h.DB, alanAdi, resellerID, func() error {
			var e error
			res, e = h.DB.ExecContext(ctx,
				`INSERT INTO domains (alan_adi, sistem_kullanici, php_surum, ipv4, ftp_host,
				   db_host, db_user, db_adi, web_root, durum, plan_id, web_backend, reseller_id, customer_id)
				 VALUES (?,?,?,?,?,'localhost',?,?,?, 'pasif', NULLIF(?,0), 'php-fpm', ?, NULLIF(?,0))`,
				alanAdi, sk, php, h.sunucuIP(), h.sunucuIP(), dbUser, dbAdi, pr.WebRoot, ay.PlanID, resellerID, customerID)
			return e
		}); err != nil {
			_ = saglamaGeriAl(alanAdi, sk)
			return nil, fmt.Errorf("domain kaydi: %w", err)
		}
		sonuc.DomainID, _ = res.LastInsertId()
		// A2: bu kimlik daha önce silinmiş bir hostinge aitse onun kalıntı yedek satırlarını yetim işaretle.
		backups.YetimKalintilariniAyir(ctx, h.DB, sonuc.DomainID)
		kotaBirak() // satır yazıldı: sonraki kota okumaları bu siteyi sayar

		limCtx, limIptal := context.WithTimeout(context.Background(), 5*time.Minute)
		if err := limitUygula(limCtx, h.DB, sonuc.DomainID); err != nil {
			log("uyari: kaynak limitleri uygulanamadi: %v", err)
			sonuc.Uyarilar = append(sonuc.Uyarilar, "Limitler uygulanamadı")
		}
		limIptal()

		if uid, gid, err := uidGid(sk); err == nil {
			if err := hesaplar.FTPCreate(h.DB, sonuc.DomainID, sk, hesaplar.RandomParola(16), uid, gid); err != nil {
				log("uyari: FTP hesabi olusturulamadi: %v", err)
			}
		}
	}

	basarili := false
	defer func() {
		if basarili {
			return
		}
		if yeniOlusturuldu {
			log("hata olustu — olusturulan hesap geri aliniyor…")
			if _, err := h.DB.Exec(`DELETE FROM domains WHERE id=?`, sonuc.DomainID); err != nil {
				log("UYARI: geri alma sirasinda domain kaydi silinemedi (id=%d): %v", sonuc.DomainID, err)
			}
			_ = saglamaGeriAl(alanAdi, sk)
		}
	}()

	// --- 2. Dosyalar -------------------------------------------------------
	var dosyaHata error
	if k.arsiv != nil {
		dosyaHata = h.arsivDosyalariAktar(ctx, k, hs, ay, kapsam, sk, webRoot, alanAdi, sonuc, log)
	} else {
		dosyaHata = h.dosyalariAktar(ctx, k, hs, ay, sk, webRoot, alanAdi, sonuc, log)
	}
	if dosyaHata != nil {
		return nil, dosyaHata
	}
	// 🔴 BAYİ DİSK HAVUZU (fiili): taşınan dosyalar bayinin toplam disk hakkını
	// aşıyorsa yeni açılan site geri alınır (kalem 'atlandi').
	if kapsam.Bayi() && yeniOlusturuldu {
		if err := diskHavuzuDenetle(h, ctx, kapsam.RID, sonuc.DomainID, sonuc.DosyaBayt); err != nil {
			return nil, err
		}
	}

	// --- 3. Veritabanlari --------------------------------------------------
	// 🔴 YEDEK KEŞIF: Kesif DB listesini boş bıraktıysa (ek/alt alan adı — DB'ler
	// yalnız ana alana atanır — ya da Plesk DB sorgusu sessizce boş döndü), KOPYALANAN
	// yapılandırmadan (wp-config.php/.env/…) gerçek DB adını çıkar. Bu ad kaynakta da
	// aynıdır → dökümü oradan alınır. Böylece "SQL hiç taşınmadı, kalem yeşil" biter.
	if ay.Veritabani && len(hs.DBler) == 0 && ay.Dosyalar {
		if bulunan := configtenDBBul(webRoot); len(bulunan) > 0 {
			log("kesif DB bulmadi; yapilandirmadan %d DB adi cikarildi: %v", len(bulunan), bulunan)
			hs.DBler = bulunan
		}
	}
	// 🔴 BAYİ PLAN DB LİMİTİ: kaynaktaki DB sayısı planın max_db'sini aşıyorsa site
	// açılmaz (geri alınır) — yarım DB'li site yayına alınmaz, limit aşılmaz.
	if kapsam.Bayi() && ay.Veritabani && len(hs.DBler) > 0 {
		if err := h.bayiDBKotasi(ctx, sonuc.DomainID, len(hs.DBler)); err != nil {
			return nil, err
		}
	}
	if ay.Veritabani && len(hs.DBler) > 0 {
		esleme, dbPw, dbHata := h.veritabanlariniAktar(ctx, k, hs, sk, webRoot, kapsam, sonuc, log)
		if dbHata != nil {
			// 🔴 DB adimi basarisizsa kalem BASARILI sayilmaz — sessiz basari
			// mustericinin sitesini bos veritabaniyla yayina alir.
			return nil, dbHata
		}
		if n := h.configYenidenYaz(webRoot, esleme, hs.KaynakHesap, dbPw, log); n > 0 {
			log("%d yapilandirma dosyasi guncellendi (DB baglantisi)", n)
		}
	} else if ay.Veritabani {
		// 🔴 SESSİZ GEÇME: DB istendi ama hiç DB bulunamadı → kullanıcı "SQL taşındı"
		// sanmasın. Görünür uyarı bırak (kalem "Not" sütunu + log).
		log("uyari: bu site icin kaynakta veritabani bulunamadi; SQL tasinmadi")
		sonuc.Uyarilar = append(sonuc.Uyarilar, "Veritabanı yok — taşınmadı")
	}

	// --- 4. DNS ------------------------------------------------------------
	if ay.DNS {
		n, err := h.dnsAktar(ctx, k, sonuc.DomainID, alanAdi, log)
		if err != nil {
			log("uyari: DNS aktarilamadi (varsayilan sablon kullanildi): %v", err)
			sonuc.Uyarilar = append(sonuc.Uyarilar, "DNS: varsayılan şablon")
		}
		sonuc.DNSSayisi = n
	}

	// --- 5. SSL ------------------------------------------------------------
	if ay.SSL {
		// 🔴 ÖNCE kaynak sertifikasını KOPYALA (sslAktar): kaynağın çalışan cert'i
		// varsa aynen taşınır → DNS geçişini beklemeden HTTPS hazır. Kaynak cert
		// yok/geçersizse Let's Encrypt'e düşülür (regresyon yok — eski davranış).
		log("SSL: kaynak sertifikasi okunuyor…")
		if cp, kp, gercek, err := h.sslAktar(ctx, k, hs, alanAdi, sk, php); err == nil && cp != "" {
			_, _ = h.DB.ExecContext(ctx,
				`UPDATE domains SET ssl_aktif=1, ssl_kaynak='imported', cert_path=?, key_path=? WHERE id=?`,
				cp, kp, sonuc.DomainID)
			if gercek {
				log("SSL: kaynak sertifikasi (gercek CA) kopyalandi ve kuruldu")
			} else {
				log("SSL: kaynak sertifikasi (self-signed) kopyalandi")
				sonuc.Uyarilar = append(sonuc.Uyarilar, "SSL: self-signed (DNS sonrası yenilenir)")
			}
		} else {
			if err != nil {
				log("SSL: kaynak sertifikasi alinamadi (%s) — Let's Encrypt deneniyor", kisalt(temizHata(err.Error(), k.Parola), 120))
			} else {
				log("SSL: kaynakta kullanilabilir sertifika yok — Let's Encrypt deneniyor")
			}
			crt, key, gercek := provisioner.OtoSSLDene(alanAdi, sk, kuruluPHPSec(php), "php-fpm")
			if crt != "" {
				kaynakAd := "self-signed"
				if gercek {
					kaynakAd = "letsencrypt"
				}
				_, _ = h.DB.ExecContext(ctx,
					`UPDATE domains SET ssl_aktif=1, ssl_kaynak=?, cert_path=?, key_path=? WHERE id=?`,
					kaynakAd, crt, key, sonuc.DomainID)
				log("SSL: %s", kaynakAd)
				if !gercek {
					sonuc.Uyarilar = append(sonuc.Uyarilar,
						"SSL: self-signed (DNS sonrası yenilenir)")
				}
			} else {
				sonuc.Uyarilar = append(sonuc.Uyarilar, "SSL yok — taşınmadı")
			}
		}
	}

	// --- 6. Posta (kutular + mail verisi) ----------------------------------
	if ay.Posta {
		log("Posta: kaynak kutulari kesfediliyor…")
		hsPosta := hs
		hsPosta.AlanAdi = alanAdi
		n, uyar, err := h.mailAktar(ctx, k, hsPosta, log)
		sonuc.Uyarilar = append(sonuc.Uyarilar, uyar...)
		if err != nil {
			log("Posta: taşınamadı (%s)", kisalt(temizHata(err.Error(), k.Parola), 120))
			sonuc.Uyarilar = append(sonuc.Uyarilar, "Posta taşınamadı")
		} else {
			sonuc.PostaSayisi = n
			if n > 0 {
				log("Posta: %d kutu taşındı", n)
			}
		}
	}

	// --- 7. Cron (yalnız arşiv kaynakları) ---------------------------------
	if ay.Cron && k.arsiv != nil {
		h.arsivCronAktar(k, alanAdi, sk, sonuc, log)
	}

	if yeniOlusturuldu {
		if _, err := h.DB.ExecContext(ctx, `UPDATE domains SET durum='aktif' WHERE id=?`, sonuc.DomainID); err != nil {
			sonuc.Uyarilar = append(sonuc.Uyarilar, "domain 'aktif' işaretlenemedi (elle etkinleştirin): "+err.Error())
		}
	}
	basarili = true
	return sonuc, nil
}

// sslAktar — KAYNAK'tan domainin çalışan cert+key'ini okuyup DOĞRULAYIP kurar.
// Başarılı: (certPath, keyPath, gercekCA, nil). Kaynakta cert yoksa: ("","",false,nil)
// → çağıran Let's Encrypt'e düşer. Doğrulama/kurulum hatası: err döner → yine LE.
func (h *Handlers) sslAktar(ctx context.Context, k *Kaynak, hs Hesap, alanAdi, sk, php string) (string, string, bool, error) {
	// Arşivde kullanılabilir sertifika taşınmaz (yeniden düzenlenir): doğrudan Let's Encrypt yolu.
	if k.arsiv != nil {
		return "", "", false, nil
	}
	// Kaynağa özel depo önce (cPanel apache_tls, DA kullanıcı dizini — Apache
	// tabanlı kaynaklarda nginx vhost'u yoktur); boşsa ortak nginx taraması.
	var certPEM, keyPEM string
	if s, ok := surucuBul(k.Tip); ok && s.sertifikaOku != nil {
		hsC := hs
		hsC.AlanAdi = alanAdi
		if c, kk, err := s.sertifikaOku(ctx, k, hsC); err == nil {
			certPEM, keyPEM = c, kk
		}
	}
	if certPEM == "" || keyPEM == "" {
		var err error
		certPEM, keyPEM, err = kaynakCertOku(ctx, k, alanAdi)
		if err != nil {
			return "", "", false, err
		}
	}
	if certPEM == "" || keyPEM == "" {
		return "", "", false, nil
	}
	return provisioner.SertifikaKur(alanAdi, sk, kuruluPHPSec(php), "php-fpm", certPEM, keyPEM)
}

// kaynakCertOku — kaynak (Plesk) nginx vhost'undan ssl_certificate + ssl_certificate_key
// YOLLARINI bulup içeriklerini okur (sürümden bağımsız — hangi cert servis ediliyorsa o).
// Özel anahtar sırdır: yalnız doğrulama+kuruluma beslenir, loglanmaz.
func kaynakCertOku(ctx context.Context, k *Kaynak, alanAdi string) (string, string, error) {
	if !reAlanAdi.MatchString(alanAdi) {
		return "", "", fmt.Errorf("gecersiz alan adi")
	}
	uzak := "d=" + shQuote(alanAdi) + "\n" + `# 🔴 KAYNAK PANEL FARKETMEZ (Plesk / GirginOSPanel / cPanel / duz nginx):
# server_name TOKEN'larinda $d TAM eslesme + ssl_certificate tanimli ilk vhost'u bul.
# Eskiden yalniz Plesk yollari taraniyordu → GirginOSPanel/cPanel kaynagindan
# tasimada cert BULUNAMIYOR, SSL kayboluyordu (self-signed). POSIX sh uyumlu.
conf=""
for c in /etc/nginx/plesk.conf.d/vhosts/*.conf /etc/nginx/conf.d/*.conf $(grep -rlsE "server_name[^;]*${d}" /etc/nginx/ 2>/dev/null); do
  [ -f "$c" ] || continue
  case "$c" in *_default*|*_panel*|*default443*|*default80*|*catch*) continue;; esac
  grep -qE "^[[:space:]]*ssl_certificate[[:space:]]" "$c" 2>/dev/null || continue
  sn=$(grep -hE "^[[:space:]]*server_name[[:space:]]" "$c" 2>/dev/null | head -1 | sed -E 's/.*server_name[[:space:]]+([^;]+);.*/\1/')
  ok=0
  for t in $sn; do [ "$t" = "$d" ] && ok=1; done
  [ "$ok" = 1 ] || continue
  conf="$c"; break
done
if [ -n "$conf" ]; then
  cert=$(grep -hE "^[[:space:]]*ssl_certificate[[:space:]]" "$conf" 2>/dev/null | head -1 | sed -E 's/.*ssl_certificate[[:space:]]+([^;]+);.*/\1/' | tr -d " \t")
  key=$(grep -hE "^[[:space:]]*ssl_certificate_key[[:space:]]" "$conf" 2>/dev/null | head -1 | sed -E 's/.*ssl_certificate_key[[:space:]]+([^;]+);.*/\1/' | tr -d " \t")
  if [ -n "$cert" ] && [ -n "$key" ] && [ -f "$cert" ] && [ -f "$key" ]; then
    printf '###GOSPCERT###\n'; cat "$cert"; printf '\n###GOSPKEY###\n'; cat "$key"; printf '\n###GOSPEND###\n'
  fi
fi`
	out, err := k.Calistir(ctx, uzak)
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(kesitAl(out, "###GOSPCERT###", "###GOSPKEY###")),
		strings.TrimSpace(kesitAl(out, "###GOSPKEY###", "###GOSPEND###")), nil
}

// kesitAl — s içinde bas ile son işaretleri ARASINDAKİ metni döndürür.
func kesitAl(s, bas, son string) string {
	i := strings.Index(s, bas)
	if i < 0 {
		return ""
	}
	i += len(bas)
	j := strings.Index(s[i:], son)
	if j < 0 {
		return ""
	}
	return s[i : i+j]
}

// bayiDiskHavuzu — bayinin öteki sitelerinin fiili kullanımı + taşınan bayt
// max_disk_mb'yi aşıyor mu? 0 = sınırsız. Metin yalnız bayinin kendi sayıları.
func (h *Handlers) bayiDiskHavuzu(ctx context.Context, rid, domainID, bayt int64) error {
	var maxMB int64
	if err := h.DB.QueryRowContext(ctx,
		`SELECT max_disk_mb FROM users WHERE id=? AND role='reseller'`, rid).Scan(&maxMB); err != nil {
		return &kotaHatasi{mesaj: "bayi kaydı bulunamadı"}
	}
	if maxMB <= 0 {
		return nil
	}
	var digerKB int64
	if err := h.DB.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(boyut_kb),0) FROM domains WHERE reseller_id=? AND id<>?`, rid, domainID).Scan(&digerKB); err != nil {
		return &kotaHatasi{mesaj: "disk kullanımı okunamadı"}
	}
	tasinanMB := (bayt + (1<<20 - 1)) >> 20
	if digerKB/1024+tasinanMB > maxMB {
		return &kotaHatasi{mesaj: fmt.Sprintf("disk kotanız yetersiz: taşınan site %d MB, mevcut kullanım %d MB, limit %d MB — site geri alındı",
			tasinanMB, digerKB/1024, maxMB)}
	}
	return nil
}

// bayiDBKotasi — domainin planı (max_db, 0 = sınırsız) adet yeni DB'yi kaldırır mı?
func (h *Handlers) bayiDBKotasi(ctx context.Context, domainID int64, adet int) error {
	var maks int
	if err := h.DB.QueryRowContext(ctx,
		`SELECT `+efektiflimit.IfadeVarsayilan("max_db", 0)+` FROM domains d`+efektiflimit.Join+`WHERE d.id=?`,
		domainID).Scan(&maks); err != nil {
		return &kotaHatasi{mesaj: "plan limitleri okunamadı"}
	}
	if maks <= 0 {
		return nil
	}
	var mevcut int
	_ = h.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM db_accounts WHERE domain_id=?`, domainID).Scan(&mevcut)
	if mevcut+adet > maks {
		return &kotaHatasi{mesaj: fmt.Sprintf("planın veritabanı limiti yetersiz: kaynakta %d veritabanı, plan en çok %d — site geri alındı", adet, maks)}
	}
	return nil
}

// sahiplikDogrula — taşınan sitenin atanacağı bayi/müşteri gerçekten var mı?
// Geçersiz hedef sessizce ana hesaba düşmesin — hata döndür.
func (h *Handlers) sahiplikDogrula(ctx context.Context, resellerID, customerID int64) error {
	if resellerID > 0 {
		var n int
		if err := h.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM users WHERE id=? AND role='reseller'`, resellerID).Scan(&n); err != nil || n == 0 {
			return fmt.Errorf("geçersiz bayi (reseller_id=%d)", resellerID)
		}
	}
	if customerID > 0 {
		var n int
		if err := h.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM customers WHERE id=?`, customerID).Scan(&n); err != nil || n == 0 {
			return fmt.Errorf("geçersiz müşteri (customer_id=%d)", customerID)
		}
	}
	return nil
}
