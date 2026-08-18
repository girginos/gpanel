package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"girginospanel/internal/accounts"
	"girginospanel/internal/antivirus"
	"girginospanel/internal/auth"
	"girginospanel/internal/backups"
	"girginospanel/internal/composer"
	"girginospanel/internal/config"
	"girginospanel/internal/cron"
	"girginospanel/internal/db"
	"girginospanel/internal/denetim"
	"girginospanel/internal/dns"
	"girginospanel/internal/domains"
	"girginospanel/internal/domainyasak"
	"girginospanel/internal/eklenti"
	"girginospanel/internal/files"
	"girginospanel/internal/git"
	githubpkg "girginospanel/internal/github"
	"girginospanel/internal/gizli"
	"girginospanel/internal/guvenlikduvari"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/hostuyg"
	"girginospanel/internal/httpx"
	"girginospanel/internal/ipyonetim"
	"girginospanel/internal/istatistik"
	"girginospanel/internal/kaynak"
	"girginospanel/internal/kaynaklimit"
	"girginospanel/internal/laravel"
	"girginospanel/internal/lisans"
	"girginospanel/internal/logs"
	"girginospanel/internal/middleware"
	"girginospanel/internal/monitor"
	"girginospanel/internal/musteri"
	"girginospanel/internal/nginxset"
	"girginospanel/internal/optimize"
	"girginospanel/internal/oturumbosta"
	"girginospanel/internal/paketler"
	"girginospanel/internal/panelhost"
	"girginospanel/internal/performans"
	"girginospanel/internal/php"
	"girginospanel/internal/phpext"
	"girginospanel/internal/phpsurum"
	"girginospanel/internal/plans"
	"girginospanel/internal/pma"
	"girginospanel/internal/portyonetim"
	"girginospanel/internal/provisioner"
	"girginospanel/internal/redis"
	"girginospanel/internal/reseller"
	"girginospanel/internal/sifrekoruma"
	"girginospanel/internal/sitekopya"
	"girginospanel/internal/sshaccess"
	"girginospanel/internal/subdomain"
	"girginospanel/internal/system"
	"girginospanel/internal/tasima"
	"girginospanel/internal/users"
	"girginospanel/internal/uygulama"
	"girginospanel/internal/waf"
	"girginospanel/internal/websec"
	"girginospanel/internal/wordpress"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

const version = "0.3.0-f3"

// 🔴 Host Uygulamalari GECICI olarak kapali (kullanici talebi 2026-08-17).
// Paket ve sayfalar duruyor; geri acmak icin bunu true yapmak + frontend'de
// menu/rota satirlarini geri koymak yeterli. Kapaliyken hicbir /hostuyg
// rotasi kaydedilmez ve arka plan gorevleri (cron, orphan temizleyici)
// hic baslatilmaz.
var hostUygulamalariAcik = false

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	d, err := db.Open(cfg.DBDsn)
	if err != nil {
		// Reboot/MariaDB gecikmesinde ANINDA log.Fatalf ile olmek yerine bekle+tekrar dene.
		// StartLimitBurst tuzagini onler; systemd Restart=always ile panel DB gelene kadar ayakta kalir.
		basla := time.Now()
		for err != nil {
			if time.Since(basla) >= 5*time.Minute {
				log.Fatalf("db: 5dk boyunca baglanilamadi (systemd yeniden baslatacak): %v", err)
			}
			log.Printf("db: baglanilamadi, 3sn sonra tekrar denenecek: %v", err)
			time.Sleep(3 * time.Second)
			d, err = db.Open(cfg.DBDsn)
		}
	}
	defer d.Close()

	// migrations
	runMigrations(d)
	gizli.ParolalariSifrele(d)          // mevcut duz-metin DB parolalarini at-rest sifrele (idempotent)
	gizli.SaglikKontrol(d)              // anahtar gercekten cozuyor mu (sessiz bozulma uyarisi)
	gizli.PMATokenSupur(d, time.Minute) // suresi gecmis pma token satirlari birikmesin

	provisioner.Init(d) // askıya-alma tutarlılığı için provisioner'a DB handle'ı ver
	middleware.Init(d)  // musteri-scope askiya-alma kontrolu icin DB handle

	ipv4 := detectIPv4()
	log.Printf("server ipv4: %s", ipv4)

	if err := domains.SeedIfEmpty(context.Background(), d, ipv4); err != nil {
		log.Printf("seed warn: %v", err)
	}
	if err := plans.SeedIfEmpty(context.Background(), d); err != nil {
		log.Printf("plans seed warn: %v", err)
	}
	// SeedSync: dolu kurulumlarda (ör. 177) mevcut planlara DOKUNMADAN eksik standart
	// tier'ları idempotent ekler.
	if err := plans.SeedSync(context.Background(), d); err != nil {
		log.Printf("plans seed-sync warn: %v", err)
	}
	if err := dns.SeedTemplateIfEmpty(context.Background(), d); err != nil {
		log.Printf("dns template seed warn: %v", err)
	}
	// Startup heal: mevcut tüm zone'lara güncel include şablonunu (AXFR-kilit + varsa DNSSEC)
	// checkconf-gate'li uygula. Böylece kural yalnız sonraki DNS düzenlemesinde değil,
	// açılışta da eski zone'lara işler. named yoksa/erişilemezse yalnız uyarı loglanır.
	if err := dns.HealZoneIncludes(context.Background(), d); err != nil {
		log.Printf("dns zone-include heal warn: %v", err)
	}
	// Batch5A: mevcut planlı domain'leri per-tenant FPM'e (Seçenek A) ARKA PLANDA + GÜVENLE
	// (baseline/post self-check + auto-rollback) migrate et. Panel her restart'ında
	// (girginospanel-update) otomatik döner → mevcut-müşteri cutover'ı plan-driven tamamlanır.
	// Boot'u bloklamaz (bg goroutine, kendi context'i).
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		kaynaklimit.HealTenantFPM(ctx, d)
	}()
	// Batch5A: MySQL Governor yavaş-sorgu watchdog (plan db_max_query_seconds>0 olan tenant
	// DB-kullanıcılarının eşik-aşan sorgularını KILL eder). Panel ömrü boyunca bg çalışır.
	go kaynaklimit.SlowQueryWatchdog(context.Background(), d)
	// Disk kotası (XFS user quota — CloudLinux paritesi): fs'te kota AKTİF ise TÜM tenant'lara
	// efektif kotayı (domain override > plan > varsayılan) idempotent uygula; noquota ise
	// (tek seferlik reboot bekliyor) sessizce atla. Boot'u bloklamaz (bg goroutine).
	go kaynaklimit.HealKotaOnStartup(context.Background(), d)

	musteriH := &musteri.Handlers{DB: d, Secret: cfg.JWTSecret}
	authH := &auth.Handlers{DB: d, Secret: cfg.JWTSecret, LifetimeSec: cfg.JWTLifetime}
	usersH := &users.Handlers{DB: d}
	domainsH := &domains.Handlers{DB: d, IPv4: ipv4}
	domains.KokDB = d // sahip kolonunda kok adini cozmek icin (onbellekli)
	// Yasaklı domain (phishing koruması): cache init + provisioner
	// ValidateDomain'a bağla. Init fail-open (DB blip'inde panel açılsın).
	if err := domainyasak.Init(d); err != nil {
		log.Printf("domainyasak: init hatası (fail-open devam): %v", err)
	}
	provisioner.SetYasakliKontrolu(domainyasak.Yasakli)
	oturumbostaH := &oturumbosta.Handler{DB: d}
	websecH := &websec.Handler{DB: d}
	panelhost.DB = d
	panelhost.IsRestartTemizle() // yarım kalan işleri temizle
	panelhostH := &panelhost.Handler{}
	ipYonetimH := &ipyonetim.Handler{
		DB: d,
		Aktor: func(r *http.Request) (int64, bool) {
			uid, _ := middleware.Aktor(r)
			return uid, uid > 0
		},
	}
	// Panel açılışta IP persist unit'ini güncelle (idempotent)
	if hostUygulamalariAcik {
		hostuyg.DB = d
		hostuyg.PanelListenPortGetir = portyonetim.DisPortOku
		portyonetim.DisPortDegistiSonrasi = hostuyg.SubdomainVhostlariPortGuncelle
		hostuyg.SubdomainCronKontrol() // hostuyg subdomain cert auto-renew
		hostuyg.IsRestartTemizle()
		hostuyg.OrphanTemizle(d) // I1: disk artıkları temizle
	}
	uygulamaH := &uygulama.Handler{
		DB: d,
		// 🔴 Bunlar bagli DEGILDI (nil) -> DBGerek=true olan Joomla/Drupal/
		// PrestaShop/MediaWiki/Nextcloud/Matomo kurulumlari SESSIZCE DB'siz
		// tamamlaniyor, db_adi='' kaydediliyordu: sihirbaz elle DB istiyor,
		// silme de hicbir DB'yi drop etmiyordu.
		MySQLOlustur: func(domainID int64, dbAd, dbUser, dbSifre string) error {
			return hesaplar.MySQLCreateDB(d, domainID, dbAd, dbUser, dbSifre)
		},
		MySQLDrop: func(_ int64, dbAd, dbUser string) error {
			return hesaplar.MySQLDropDB(d, dbAd, dbUser)
		},
		DomainAl: func(r *http.Request) (uygulama.Domain, error) {
			idStr := chi.URLParam(r, "id")
			var id int64
			fmt.Sscanf(idStr, "%d", &id)
			if id <= 0 {
				return uygulama.Domain{}, fmt.Errorf("geçersiz domain id")
			}
			var alanAdi, sk string
			err := d.QueryRowContext(r.Context(), "SELECT alan_adi, sistem_kullanici FROM domains WHERE id=?", id).Scan(&alanAdi, &sk)
			if err != nil {
				return uygulama.Domain{}, err
			}
			return uygulama.Domain{
				ID: id, AlanAdi: alanAdi, SK: sk,
				DocRoot: "/home/" + sk + "/public_html",
			}, nil
		},
	}
	hostuygH := &hostuyg.Handler{
		DB: d,
		Aktor: func(r *http.Request) (int64, bool) {
			uid, _ := middleware.Aktor(r)
			return uid, uid > 0
		},
	}
	optimizeH := &optimize.Handler{
		DB: d,
		Aktor: func(r *http.Request) (int64, bool) {
			uid, _ := middleware.Aktor(r)
			return uid, uid > 0
		},
	}
	portYonetimH := &portyonetim.Handler{
		DB: d,
		Aktor: func(r *http.Request) (int64, bool) {
			uid, _ := middleware.Aktor(r)
			return uid, uid > 0
		},
	}
	go func() { _ = ipyonetim.PersistYaz(context.Background(), d) }()
	go panelhost.TemizleLoop() // async iş kayıtları temizleyici
	// Website Security Monitor — 6 saatlik arka plan tarama.
	go websec.Dongusu(context.Background(), d)
	domainyasakH := &domainyasak.Handler{
		DB: d,
		Aktor: func(r *http.Request) (int64, bool) {
			uid, _ := middleware.Aktor(r)
			return uid, uid > 0
		},
	}
	filesH := &files.Handlers{DB: d}
	cronH := &cron.Handlers{DB: d}
	logsH := &logs.Handlers{DB: d}
	tasimaH := &tasima.Handlers{DB: d}
	tasimaH.AcilistaTemizle()
	plansH := &plans.Handlers{DB: d}
	dnsH := &dns.Handlers{DB: d}
	accountsH := &accounts.Handlers{DB: d}
	backupsH := &backups.Handlers{DB: d}
	backups.StartScheduler(d)
	gitH := &git.Handlers{DB: d}
	githubH := &githubpkg.Handlers{DB: d, WebhookBase: "https://" + ipv4 + ":8443"}
	pmaH := &pma.Handlers{DB: d}
	phpH := &php.Handlers{DB: d}
	kaynakH := &kaynak.Handlers{DB: d}
	monitorH := &monitor.Handlers{DB: d}
	eklentiH := &eklenti.Handlers{DB: d}
	go eklentiH.SaglikDongusu(context.Background())
	// Lisansli eklenti pazaryeri + periyodik lisans nabzi.
	// Nabiz AG HATASINDA FAIL-OPEN'dir (bkz. internal/lisans/nabiz.go).
	lisansH := &lisans.Handlers{DB: d, Surum: version}
	go lisansH.NabizDongusu(context.Background())
	// Kurulu eklenti ikilisinin imzali paket basligiyla tutarliligi.
	// 🔴 Nabizdan AYRI: kurcalama agdan bagimsiz bir olaydir, lisans
	// sunucusuna ulasilamasa bile goruilmelidir.
	go lisans.ButunlukIzleyici(d)
	nginxsetH := &nginxset.Handlers{DB: d}
	sshH := &sshaccess.Handlers{DB: d, IPv4: ipv4}
	statH := &istatistik.Handlers{DB: d}
	perfH := &performans.Handlers{DB: d}
	compH := &composer.Handlers{DB: d}
	laravelH := &laravel.Handlers{DB: d}
	korumaH := &sifrekoruma.Handlers{DB: d}
	avH := &antivirus.Handlers{DB: d}
	kopyaH := &sitekopya.Handlers{DB: d}
	wpH := &wordpress.Handlers{DB: d}
	fwH := &guvenlikduvari.Handlers{DB: d}
	wafH := &waf.Handlers{DB: d}
	redisH := &redis.Handlers{DB: d}
	subH := &subdomain.Handlers{DB: d, IPv4: ipv4}
	resellerH := &reseller.Handlers{DB: d}
	denetimH := &denetim.Handlers{DB: d}
	sshaccess.EnsureInfra()
	provisioner.HealNginxLogPerms() // nginx log dizinini kiraciya kapat (cross-tenant log okuma)
	redis.HealScanAcl()             // Valkey SCAN/RANDOMKEY komsu key-ismi sizintisini kapat
	phpExtH := &phpext.Handlers{DB: d}
	paketlerH := &paketler.Handlers{DB: d}
	phpSurumH := &phpsurum.Handlers{DB: d}
	// 🔴 PERF: PHP kurulabilirlik keşfini (dnf) arka-plana al — /php/versions gibi
	// TumSurumler() çağıran endpoint'ler istek path'inde dnf'e bloklamasın (Domains listesi).
	phpsurum.StartAvailabilitySweeper()

	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	// NOT: chimw.RealIP KULLANILMIYOR — spoof-edilebilir X-Forwarded-For / X-Real-IP /
	// True-Client-IP basliklarini guvenilir-vekil kontrolu OLMADAN r.RemoteAddr'a yazip
	// giris hiz-sinirini atlatilabilir kilardi. Gercek istemci IP'si httpx.ClientIP ile
	// alinir (yalniz loopback/nginx peer'dan gelen X-Real-IP'ye guvenir).
	r.Use(chimw.Recoverer)
	r.Use(chimw.Timeout(300 * time.Second))

	r.Post("/api/v1/git-webhook/{secret}", gitH.Webhook)
	r.Post("/api/v1/internal/pma-redeem", pmaH.Bozdur)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"durum": "ayakta",
			"surum": version,
			"zaman": time.Now().UTC().Format(time.RFC3339),
		})
	})

	// eklenti frontend bundle: nginx yalnizca /api/ proxyler + <script src> JWT tasiyamaz => auth disi
	r.Get("/api/v1/eklenti-bundle/{ad}/app.js", eklentiH.Bundle)

	r.Route("/api/v1", func(r chi.Router) {
		// Kaba-kuvvet koruması: giriş uçları IP başına hız-sınırlı (bkz. middleware.GirisLimiti)
		r.With(middleware.GirisLimiti).Post("/auth/login", authH.Login)
		r.With(middleware.GirisLimiti).Post("/musteri/login", musteriH.Login)

		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(cfg.JWTSecret))
			r.Get("/me", usersH.Me)
			r.With(middleware.AdminVeyaReseller).Put("/me", authH.ProfilGuncelle)
			r.With(middleware.AdminOnly).Get("/dashboard-duzen", authH.DashboardDuzenGetir)
			r.With(middleware.AdminOnly).Put("/dashboard-duzen", authH.DashboardDuzenKaydet)
			r.With(middleware.AdminVeyaReseller).Post("/me/parola", authH.ParolaDegistir)
			r.With(middleware.AdminVeyaReseller).Get("/me/2fa/setup", authH.TwoFASetup)
			r.With(middleware.AdminVeyaReseller).Post("/me/2fa/enable", authH.TwoFAEnable)
			r.With(middleware.AdminVeyaReseller).Post("/me/2fa/disable", authH.TwoFADisable)
			r.With(middleware.AdminVeyaReseller).Get("/domains", domainsH.List)
			r.With(middleware.AdminVeyaReseller).Get("/subdomains", subH.TumListe)
			r.With(middleware.AdminVeyaReseller).Get("/reseller/ozet", resellerH.Ozet)
			r.With(middleware.AdminVeyaReseller).Get("/denetim", denetimH.Liste)
			r.With(middleware.AdminVeyaReseller).Get("/nav-sayaclar", domainsH.NavSayac)
			r.With(middleware.AdminOnly).Get("/resellers", resellerH.Liste)
			r.With(middleware.AdminOnly).Post("/resellers", resellerH.Olustur)
			r.With(middleware.AdminOnly).Put("/resellers/{id}", resellerH.Guncelle)
			r.With(middleware.AdminOnly).Delete("/resellers/{id}", resellerH.Sil)
			r.With(middleware.AdminOnly).Get("/reseller-plans", resellerH.PaketListe)
			r.With(middleware.AdminOnly).Get("/reseller-plans/{id}", resellerH.PaketDetay)
			r.With(middleware.AdminOnly).Post("/reseller-plans", resellerH.PaketOlustur)
			r.With(middleware.AdminOnly).Put("/reseller-plans/{id}", resellerH.PaketGuncelle)
			r.With(middleware.AdminOnly).Delete("/reseller-plans/{id}", resellerH.PaketSil)
			// Yasaklı alan adları (phishing koruması, admin only).
			r.With(middleware.AdminOnly).Get("/banned-domains", domainyasakH.List)
			r.With(middleware.AdminOnly).Post("/banned-domains", domainyasakH.Create)
			r.With(middleware.AdminOnly).Post("/banned-domains/bulk", domainyasakH.BulkCreate)
			r.With(middleware.AdminOnly).Post("/banned-domains/bulk-delete", domainyasakH.BulkDelete)
			r.With(middleware.AdminOnly).Delete("/banned-domains/{domain}", domainyasakH.Delete)
			// Oturum boşta süresi
			r.Get("/settings/session-idle", oturumbostaH.Get) // herkes okur
			r.With(middleware.AdminOnly).Put("/settings/session-idle", oturumbostaH.Put)
			// Website Security Monitor
			r.With(middleware.AdminOnly).Get("/websec/status", websecH.Status)
			r.With(middleware.AdminOnly).Get("/websec/findings", websecH.Findings)
			r.With(middleware.AdminOnly).Get("/websec/apps", websecH.Apps)
			r.With(middleware.AdminOnly).Post("/websec/rescan", websecH.Rescan)
			r.With(middleware.AdminOnly).Post("/websec/rescan-many", websecH.RescanMany)
			// Panel Hostname & SSL (admin only)
			r.With(middleware.AdminOnly).Get("/panel-host", panelhostH.Durum)
			r.With(middleware.AdminOnly).Post("/panel-host/dns", panelhostH.DNS)
			r.With(middleware.AdminOnly).Post("/panel-host/apply", panelhostH.Ayarla)
			r.With(middleware.AdminOnly).Post("/panel-host/ssl", panelhostH.SslKur)
			r.With(middleware.AdminOnly).Get("/panel-host/is", panelhostH.IsDurum)
			r.With(middleware.AdminOnly).Get("/panel-host/gecmis", panelhostH.Gecmis)
			// IP Yönetimi (admin only)
			r.With(middleware.AdminOnly).Get("/ipler", ipYonetimH.Liste)
			r.With(middleware.AdminOnly).Post("/ipler", ipYonetimH.Ekle)
			r.With(middleware.AdminOnly).Delete("/ipler/{ip}", ipYonetimH.Sil)
			r.With(middleware.AdminOnly).Put("/domains/{id}/ipv4", ipYonetimH.DomainIPGuncelle)
			// Port Değiştirme (Feature 6)
			r.With(middleware.AdminOnly).Get("/portlar", portYonetimH.Durum)
			r.With(middleware.AdminOnly).Get("/portlar/is", portYonetimH.IsDurum)
			r.With(middleware.AdminOnly).Get("/portlar/yasakli", portYonetimH.YasakliListele)
			r.With(middleware.AdminOnly).Get("/portlar/gecmis", portYonetimH.Gecmis)
			r.With(middleware.AdminOnly).Post("/portlar/backend", portYonetimH.BackendDegistir)
			r.With(middleware.AdminOnly).Post("/portlar/dis", portYonetimH.DisDegistir)
			// Host uygulamaları — GEÇİCİ KAPALI (hostUygulamalariAcik)
			if hostUygulamalariAcik {
				r.With(middleware.AdminOnly).Get("/hostuyg/katalog", hostuygH.Katalog)
				r.With(middleware.AdminOnly).Get("/hostuyg", hostuygH.Liste)
				r.With(middleware.AdminOnly).Get("/hostuyg/is/{is_id}", hostuygH.IsDurum)
				r.With(middleware.AdminOnly).Get("/hostuyg/tumu/metrik", hostuygH.MetriklerTumu)
				r.With(middleware.AdminOnly).Get("/hostuyg/{id}/metrik", hostuygH.Metrik)
				r.With(middleware.AdminOnly).Post("/hostuyg/{id}/yedek", hostuygH.YedekAl)
				r.With(middleware.AdminOnly).Get("/hostuyg/{id}/yedekler", hostuygH.YedekListe)
				r.With(middleware.AdminOnly).Post("/hostuyg/{id}/yedek/geriyukle", hostuygH.YedekGeriYukle)
				r.With(middleware.AdminOnly).Delete("/hostuyg/yedek/{yedek_id}", hostuygH.YedekSil)
				r.With(middleware.AdminOnly).Get("/hostuyg/{id}", hostuygH.Detay)
				r.With(middleware.AdminOnly).Get("/hostuyg/{id}/log", hostuygH.Log)
				r.With(middleware.AdminOnly).Post("/hostuyg", hostuygH.Kur)
				r.With(middleware.AdminOnly).Post("/hostuyg/{id}/baslat", hostuygH.Baslat)
				r.With(middleware.AdminOnly).Post("/hostuyg/{id}/restart", hostuygH.Restart)
				r.With(middleware.AdminOnly).Post("/hostuyg/{id}/eylem", hostuygH.Eylem)
				r.With(middleware.AdminOnly).Post("/hostuyg/{id}/durdur", hostuygH.Durdur)
				r.With(middleware.AdminOnly).Delete("/hostuyg/{id}", hostuygH.Kaldir)
			}
			// Tenant-level uygulama installer (Faz 1 yeniden)
			r.With(middleware.MusteriScope).Get("/domains/{id}/uygulamalar", uygulamaH.Liste)
			r.With(middleware.MusteriScope).Post("/domains/{id}/uygulamalar", uygulamaH.Kur)
			r.With(middleware.MusteriScope).Delete("/domains/{id}/uygulamalar/{kayit_id}", uygulamaH.Sil)
			r.Get("/uygulamalar/katalog", uygulamaH.Katalog)
			r.With(middleware.MusteriScope).Get("/domains/{id}", domainsH.Get)
			r.With(middleware.AdminOnly).Get("/system/usage", system.Handler)
			r.With(middleware.AdminOnly).Get("/optimize/analiz", optimizeH.Analiz)
			r.With(middleware.AdminOnly).Post("/optimize/uygula", optimizeH.Uygula)
			r.With(middleware.AdminOnly).Get("/optimize/yedekler", optimizeH.Yedekler)
			r.With(middleware.AdminOnly).Post("/optimize/rollback", optimizeH.Rollback)
			r.With(middleware.AdminOnly).Get("/system/servisler", system.ServisDurumlar)
			r.With(middleware.AdminOnly).Post("/system/servis-islem", system.ServisIslem)
			r.With(middleware.AdminOnly).Get("/system/guncelleme", system.GuncellemeDurum)
			r.With(middleware.AdminOnly).Post("/system/guncelleme/baslat", system.GuncellemeBaslat)
			r.With(middleware.AdminOnly).Get("/system/guncelleme/log", system.GuncellemeLog)
			r.With(middleware.AdminOnly).Get("/system/optimize", system.OptimizeDurum)
			r.With(middleware.AdminOnly).Post("/system/optimize/baslat", system.OptimizeBaslat)
			r.With(middleware.AdminOnly).Get("/system/optimize/log", system.OptimizeLog)
			// Site tasima (cPanel / Plesk / DirectAdmin) — yalniz yonetici.
			r.With(middleware.AdminOnly).Post("/system/tasima/test", tasimaH.Test)
			r.With(middleware.AdminOnly).Post("/system/tasima/kesif", tasimaH.Kesif)
			r.With(middleware.AdminOnly).Post("/system/tasima/baslat", tasimaH.Baslat)
			r.With(middleware.AdminOnly).Get("/system/tasima", tasimaH.Durum)
			r.With(middleware.AdminOnly).Get("/system/tasima/{id}", tasimaH.Detay)
			r.With(middleware.AdminOnly).Get("/system/tasima/{id}/log", tasimaH.Log)
			r.With(middleware.AdminOnly).Post("/system/tasima/{id}/iptal", tasimaH.Iptal)
			r.With(middleware.AdminOnly).Get("/system/cve", system.CveDurum)
			r.With(middleware.AdminOnly).Get("/system/surum-kontrol", system.SurumKontrolDurum)
			r.With(middleware.AdminOnly).Post("/system/surum-kontrol/yenile", system.SurumKontrolYenile)
			r.With(middleware.AdminOnly).Post("/system/cve/guncelle", system.CveGuncelle)
			r.With(middleware.AdminOnly).Get("/system/cve/log", system.CveLog)
			r.With(middleware.AdminOnly).Get("/system/kernelcare", system.KernelcareDurumHandler)
			r.With(middleware.AdminOnly).Post("/system/kernelcare/yamala", system.KernelcareYamala)
			lisansH.Routes(r)
			eklentiH.Routes(r)
			r.With(middleware.AdminOnly).Get("/system/processes", monitor.Processes)
			r.With(middleware.AdminOnly).Get("/system/load-history", monitorH.YukGecmisi)
			r.With(middleware.AdminOnly).Get("/admin/system/loglar", monitorH.SunucuLog)
			r.With(middleware.MusteriScope).Get("/domains/{id}/health", monitorH.Health)

			// Yazma + müşteri-scope route'ları — per-route AdminOnly/MusteriScope ile yetkilendirilir
			r.Group(func(r chi.Router) {
				r.With(middleware.AdminVeyaReseller).Post("/domains", domainsH.Create)
				r.With(middleware.SahipYonetim).Delete("/domains/{id}", domainsH.Delete)
				r.With(middleware.AdminOnly).Post("/domains/toplu/sahip", domainsH.TopluSahip)
				r.With(middleware.AdminVeyaReseller).Post("/domains/toplu/durum", domainsH.TopluDurum)
				r.With(middleware.MusteriScope).Put("/domains/{id}/php", domainsH.SetPHP)
				r.With(middleware.MusteriScope).Get("/domains/{id}/ssh", sshH.Goster)
				r.With(middleware.AdminOnly).Put("/domains/{id}/ssh", sshH.Ayarla)
				r.With(middleware.AdminOnly).Put("/domains/{id}/ssh/anahtar", sshH.AnahtarKaydet)
				r.With(middleware.MusteriScope).Get("/domains/{id}/istatistik", statH.Goster)
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain/{sid}/istatistik", statH.Goster)
				r.With(middleware.MusteriScope).Get("/domains/{id}/performans", perfH.Goster)
				r.With(middleware.MusteriScope).Get("/domains/{id}/composer", compH.Durum)
				r.With(middleware.MusteriScope).Post("/domains/{id}/composer", compH.Calistir)
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain/{sid}/composer", compH.Durum)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain/{sid}/composer", compH.Calistir)
				r.With(middleware.MusteriScope).Get("/domains/{id}/laravel", laravelH.Durum)
				r.With(middleware.MusteriScope).Post("/domains/{id}/laravel/kur", laravelH.Kur)
				r.With(middleware.MusteriScope).Get("/domains/{id}/laravel/kur/durum", laravelH.KurDurum)
				r.With(middleware.MusteriScope).Post("/domains/{id}/laravel/artisan", laravelH.Artisan)
				r.With(middleware.MusteriScope).Post("/domains/{id}/laravel/composer", laravelH.Composer)
				r.With(middleware.MusteriScope).Get("/domains/{id}/laravel/node", laravelH.NodeSurumler)
				r.With(middleware.MusteriScope).Post("/domains/{id}/laravel/npm", laravelH.Npm)
				r.With(middleware.MusteriScope).Get("/domains/{id}/laravel/env", laravelH.EnvOku)
				r.With(middleware.MusteriScope).Put("/domains/{id}/laravel/env", laravelH.EnvYaz)
				r.With(middleware.MusteriScope).Post("/domains/{id}/laravel/bakim", laravelH.Bakim)
				r.With(middleware.MusteriScope).Post("/domains/{id}/laravel/deploy", laravelH.Deploy)
				r.With(middleware.MusteriScope).Get("/domains/{id}/laravel/deploy/durum", laravelH.DeployDurum)
				r.With(middleware.MusteriScope).Post("/domains/{id}/laravel/schedule", laravelH.Schedule)
				r.With(middleware.MusteriScope).Post("/domains/{id}/laravel/queue", laravelH.Queue)
				r.With(middleware.MusteriScope).Get("/domains/{id}/laravel/queue/durum", laravelH.QueueDurum)
				r.With(middleware.MusteriScope).Get("/domains/{id}/laravel/app-adaylar", laravelH.AppAdaylar)
				r.With(middleware.MusteriScope).Put("/domains/{id}/laravel/app-root", laravelH.SetAppRoot)
				r.With(middleware.MusteriScope).Get("/domains/{id}/redis", redisH.Durum)
				r.With(middleware.MusteriScope).Post("/domains/{id}/redis", redisH.Ac)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/redis", redisH.Kapat)
				r.With(middleware.MusteriScope).Get("/domains/{id}/koruma", korumaH.Liste)
				r.With(middleware.MusteriScope).Post("/domains/{id}/koruma", korumaH.Ekle)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/koruma/{kid}", korumaH.Sil)
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain/{sid}/koruma", korumaH.Liste)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain/{sid}/koruma", korumaH.Ekle)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/subdomain/{sid}/koruma/{kid}", korumaH.Sil)
				r.With(middleware.MusteriScope).Get("/domains/{id}/antivirus", avH.Durum)
				r.With(middleware.MusteriScope).Post("/domains/{id}/antivirus/tara", avH.Tara)
				r.With(middleware.MusteriScope).Get("/domains/{id}/antivirus/tara/{sid}", avH.TaraDurum)
				r.With(middleware.MusteriScope).Post("/domains/{id}/antivirus/karantina", avH.Karantina)
				r.With(middleware.MusteriScope).Post("/domains/{id}/antivirus/imza-guncelle", avH.ImzaGuncelle)
				r.With(middleware.MusteriScope).Get("/domains/{id}/kopya", kopyaH.Liste)
				r.With(middleware.MusteriScope).Post("/domains/{id}/kopya", kopyaH.Olustur)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/kopya/{ad}", kopyaH.Sil)
				r.With(middleware.MusteriScope).Get("/domains/{id}/wordpress", wpH.Liste)
				r.With(middleware.MusteriScope).Post("/domains/{id}/wordpress", wpH.Kur)
				r.With(middleware.MusteriScope).Post("/domains/{id}/wordpress/guncelle", wpH.Guncelle)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/wordpress", wpH.Sil)
				// WordPress Toolkit — eklenti/tema/kullanıcı yönetimi + onarım + araçlar
				r.With(middleware.MusteriScope).Get("/domains/{id}/wordpress/durum", wpH.Durum)
				r.With(middleware.MusteriScope).Get("/domains/{id}/wordpress/eklentiler", wpH.Eklentiler)
				r.With(middleware.MusteriScope).Post("/domains/{id}/wordpress/eklenti", wpH.EklentiIslem)
				r.With(middleware.MusteriScope).Get("/domains/{id}/wordpress/temalar", wpH.Temalar)
				r.With(middleware.MusteriScope).Post("/domains/{id}/wordpress/tema", wpH.TemaIslem)
				r.With(middleware.MusteriScope).Get("/domains/{id}/wordpress/kullanicilar", wpH.Kullanicilar)
				r.With(middleware.MusteriScope).Post("/domains/{id}/wordpress/kullanici-parola", wpH.KullaniciParola)
				r.With(middleware.MusteriScope).Post("/domains/{id}/wordpress/onar", wpH.Onar)
				r.With(middleware.MusteriScope).Post("/domains/{id}/wordpress/arac", wpH.AracIslem)
				r.With(middleware.AdminOnly).Get("/wordpress/tumu", wpH.TumListe)
				r.With(middleware.AdminOnly).Get("/firewall", fwH.Liste)
				r.With(middleware.AdminOnly).Post("/firewall", fwH.Ekle)
				r.With(middleware.AdminOnly).Post("/firewall/sablon", fwH.Sablon)
				r.With(middleware.AdminOnly).Delete("/firewall/{id}", fwH.Sil)
				r.With(middleware.AdminOnly).Post("/firewall/{id}/durum", fwH.Durum)
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain", subH.Liste)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain", subH.Olustur)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/subdomain/{sid}", subH.Sil)
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain/{sid}", subH.Detay)
				r.With(middleware.MusteriScope).Put("/domains/{id}/subdomain/{sid}/php", subH.PHPDegistir)
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain/{sid}/php-settings", subH.PHPAyarGet)
				r.With(middleware.MusteriScope).Put("/domains/{id}/subdomain/{sid}/php-settings", subH.PHPAyarPut)
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain/{sid}/web-sunucu", subH.WebGet)
				r.With(middleware.MusteriScope).Put("/domains/{id}/subdomain/{sid}/web-sunucu", subH.WebPut)
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain/{sid}/ssl", subH.SSLDurum)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain/{sid}/ssl", subH.SSLKur)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/subdomain/{sid}/ssl", subH.SSLKaldir)
				// Subdomain WordPress (docroot-kapsamli, wpKapsam ile alt-alana hapis)
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain/{sid}/wordpress", wpH.Liste)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain/{sid}/wordpress", wpH.Kur)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain/{sid}/wordpress/guncelle", wpH.Guncelle)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/subdomain/{sid}/wordpress", wpH.Sil)
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain/{sid}/wordpress/durum", wpH.Durum)
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain/{sid}/wordpress/eklentiler", wpH.Eklentiler)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain/{sid}/wordpress/eklenti", wpH.EklentiIslem)
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain/{sid}/wordpress/temalar", wpH.Temalar)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain/{sid}/wordpress/tema", wpH.TemaIslem)
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain/{sid}/wordpress/kullanicilar", wpH.Kullanicilar)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain/{sid}/wordpress/kullanici-parola", wpH.KullaniciParola)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain/{sid}/wordpress/onar", wpH.Onar)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain/{sid}/wordpress/arac", wpH.AracIslem)
				r.With(middleware.MusteriScope).Get("/domains/{id}/web-backend", domainsH.GetWebBackend)
				r.With(middleware.MusteriScope).Put("/domains/{id}/web-backend", domainsH.SetWebBackend)
				r.With(middleware.MusteriScope).Get("/domains/{id}/web-root", domainsH.GetWebRoot)
				r.With(middleware.MusteriScope).Put("/domains/{id}/web-root", domainsH.SetWebRoot)
				r.With(middleware.MusteriScope).Put("/domains/{id}/ftp/password", domainsH.SetFTPPassword)
				r.With(middleware.MusteriScope).Get("/domains/{id}/databases", domainsH.ListDatabases)
				r.With(middleware.MusteriScope).Post("/domains/{id}/databases", domainsH.CreateDatabase)
				r.With(middleware.DBSahipligi("dbid")).Delete("/databases/{dbid}", domainsH.DeleteDatabase)
				r.With(middleware.DBSahipligi("dbid")).Put("/databases/{dbid}/password", domainsH.SetDatabasePassword)
				r.With(middleware.MusteriScope).Get("/domains/{id}/files", filesH.List)
				r.With(middleware.MusteriScope).Get("/domains/{id}/files/oku", filesH.Read)
				r.With(middleware.MusteriScope).Get("/domains/{id}/files/indir", filesH.Download)
				r.With(middleware.MusteriScope).Post("/domains/{id}/files/mkdir", filesH.Mkdir)
				r.With(middleware.MusteriScope).Post("/domains/{id}/files/upload", filesH.Upload)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/files", filesH.Delete)
				r.With(middleware.MusteriScope).Post("/domains/{id}/files/yaz", filesH.Yaz)
				r.With(middleware.MusteriScope).Post("/domains/{id}/files/rename", filesH.Rename)
				r.With(middleware.MusteriScope).Post("/domains/{id}/files/chmod", filesH.Chmod)
				r.With(middleware.MusteriScope).Post("/domains/{id}/files/extract", filesH.Extract)
				r.With(middleware.MusteriScope).Post("/domains/{id}/files/copy", filesH.Copy)
				r.With(middleware.MusteriScope).Post("/domains/{id}/files/move", filesH.Move)
				r.With(middleware.MusteriScope).Post("/domains/{id}/files/archive", filesH.Archive)
				r.With(middleware.MusteriScope).Post("/domains/{id}/files/yeni-dosya", filesH.YeniDosya)
				r.With(middleware.MusteriScope).Get("/domains/{id}/files/boyut", filesH.BoyutHesapla)
				r.With(middleware.MusteriScope).Get("/domains/{id}/files/ara", filesH.Ara)
				// Subdomain dosya yoneticisi (ayni handler, /home/sk hapsi; UI subdomain docroot ta baslar)
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain/{sid}/files", filesH.List)
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain/{sid}/files/oku", filesH.Read)
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain/{sid}/files/indir", filesH.Download)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain/{sid}/files/mkdir", filesH.Mkdir)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain/{sid}/files/upload", filesH.Upload)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/subdomain/{sid}/files", filesH.Delete)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain/{sid}/files/yaz", filesH.Yaz)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain/{sid}/files/rename", filesH.Rename)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain/{sid}/files/chmod", filesH.Chmod)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain/{sid}/files/extract", filesH.Extract)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain/{sid}/files/copy", filesH.Copy)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain/{sid}/files/move", filesH.Move)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain/{sid}/files/archive", filesH.Archive)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain/{sid}/files/yeni-dosya", filesH.YeniDosya)
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain/{sid}/files/boyut", filesH.BoyutHesapla)
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain/{sid}/files/ara", filesH.Ara)
				r.With(middleware.MusteriScope).Get("/domains/{id}/ssl", domainsH.SSLDurum)
				r.With(middleware.MusteriScope).Post("/domains/{id}/ssl/issue", domainsH.SSLIssue)
				r.With(middleware.MusteriScope).Get("/domains/{id}/ssl/ilerleme", domainsH.SSLIlerleme)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/ssl", domainsH.SSLDisable)
				r.With(middleware.MusteriScope).Get("/domains/{id}/cron", cronH.List)
				r.With(middleware.MusteriScope).Post("/domains/{id}/cron", cronH.Create)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/cron/{idx}", cronH.Delete)
				r.With(middleware.MusteriScope).Get("/domains/{id}/logs", logsH.List)
				r.With(middleware.MusteriScope).Get("/domains/{id}/logs/oku", logsH.Read)
				r.With(middleware.MusteriScope).Get("/domains/{id}/logs/canli", logsH.Tail)
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain/{sid}/logs", logsH.List)
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain/{sid}/logs/oku", logsH.Read)
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain/{sid}/logs/canli", logsH.Tail)
				r.With(middleware.MusteriScope).Post("/domains/{id}/disk-hesapla", domainsH.DiskHesapla)
				r.With(middleware.AdminVeyaReseller).Get("/plans", plansH.List)
				r.With(middleware.AdminVeyaReseller).Get("/plans/{id}", plansH.Get)
				r.With(middleware.AdminVeyaReseller).Post("/plans", plansH.Create)
				r.With(middleware.AdminVeyaReseller).Put("/plans/{id}", plansH.Update)
				r.With(middleware.AdminVeyaReseller).Delete("/plans/{id}", plansH.Delete)
				r.With(middleware.AdminVeyaReseller).Get("/plans/{id}/domains", plansH.DomainlerAra)
				r.With(middleware.SahipYonetim).Put("/domains/{id}/plan", domainsH.SetPlan)
				r.With(middleware.SahipYonetim).Post("/domains/{id}/ozel-plan", domainsH.OzelPlan)
				r.With(middleware.MusteriScope).Get("/domains/{id}/dns", dnsH.List)
				r.With(middleware.MusteriScope).Post("/domains/{id}/dns", dnsH.Create)
				r.With(middleware.MusteriScope).Put("/domains/{id}/dns/{rid}", dnsH.Update)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/dns/{rid}", dnsH.Delete)
				r.With(middleware.MusteriScope).Post("/domains/{id}/dns/sablon", dnsH.ApplyTemplate)
				r.With(middleware.MusteriScope).Post("/domains/{id}/dns/toplu-sil", dnsH.TopluSil)
				r.With(middleware.MusteriScope).Post("/domains/{id}/dns/toplu-durum", dnsH.TopluDurum)
				r.With(middleware.MusteriScope).Get("/domains/{id}/dns/soa", dnsH.GetSOA)
				r.With(middleware.MusteriScope).Put("/domains/{id}/dns/soa", dnsH.PutSOA)
				r.With(middleware.MusteriScope).Get("/domains/{id}/dns/dnssec", dnsH.GetDNSSEC)
				r.With(middleware.MusteriScope).Post("/domains/{id}/dns/dnssec", dnsH.PostDNSSEC)
				r.With(middleware.MusteriScope).Get("/domains/{id}/dns/disa-aktar", dnsH.DisaAktar) // BIND zone export
				r.With(middleware.MusteriScope).Post("/domains/{id}/dns/ice-aktar", dnsH.IceAktar)  // BIND zone import
				// Merkezi DNS şablonu (admin) — domain eklerken + "Şablonu Uygula" bunu okur
				r.With(middleware.AdminVeyaReseller).Get("/dns-template", dnsH.GetTemplate)
				r.With(middleware.AdminVeyaReseller).Put("/dns-template", dnsH.PutTemplate)
				// Domain askıya al / geri al (suspend)
				r.With(middleware.SahipYonetim).Post("/domains/{id}/askiya-al", domainsH.AskiyaAl)
				r.With(middleware.SahipYonetim).Post("/domains/{id}/askidan-al", domainsH.AskidanAl)
				// Aylık trafik toplayıcıyı elle tetikle (test/anlık güncelleme)
				r.With(middleware.AdminOnly).Post("/admin/trafik/tick", func(w http.ResponseWriter, req *http.Request) {
					n := istatistik.AggregateAll(d)
					httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "islenen_domain": n})
				})
				r.With(middleware.AdminOnly).Get("/customers", accountsH.ListCustomers)
				r.With(middleware.AdminOnly).Post("/customers", accountsH.CreateCustomer)
				r.With(middleware.AdminOnly).Put("/customers/{id}", accountsH.UpdateCustomer)
				r.With(middleware.AdminOnly).Delete("/customers/{id}", accountsH.DeleteCustomer)
				r.With(middleware.MusteriScope).Get("/domains/{id}/backups", backupsH.List)
				r.With(middleware.MusteriScope).Post("/domains/{id}/backups", backupsH.Create)
				r.With(middleware.MusteriScope).Get("/domains/{id}/backups/{bid}/indir", backupsH.Download)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/backups/{bid}", backupsH.Delete)
				r.With(middleware.MusteriScope).Post("/domains/{id}/backups/{bid}/geriyukle", backupsH.Restore)
				r.With(middleware.MusteriScope).Get("/domains/{id}/backups/{bid}/icerik", backupsH.Icerik)
				r.With(middleware.MusteriScope).Get("/domains/{id}/backup-schedule", backupsH.GetSchedule)
				r.With(middleware.MusteriScope).Put("/domains/{id}/backup-schedule", backupsH.SetSchedule)
				r.With(middleware.AdminOnly).Post("/admin/backups/tick", backupsH.TickNow)
				r.With(middleware.AdminOnly).Get("/admin/backups/ozet", backupsH.Ozet)
				r.With(middleware.AdminOnly).Post("/admin/backups/jobs", backupsH.JobYedekBaslat)
				r.With(middleware.AdminOnly).Get("/admin/backups/jobs", backupsH.JobListe)
				r.With(middleware.AdminOnly).Get("/admin/backups/jobs/{jid}", backupsH.JobDetay)
				r.With(middleware.AdminOnly).Post("/admin/backups/restore", backupsH.JobGeriBaslat)
				r.With(middleware.MusteriScope).Get("/domains/{id}/backup-destination", backupsH.GetDestination)
				r.With(middleware.MusteriScope).Put("/domains/{id}/backup-destination", backupsH.PutDestination)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/backup-destination", backupsH.DeleteDestination)
				r.With(middleware.MusteriScope).Post("/domains/{id}/backup-destination/test", backupsH.TestDestination)
				r.With(middleware.MusteriScope).Get("/domains/{id}/git", gitH.Get)
				r.With(middleware.MusteriScope).Post("/domains/{id}/git", gitH.Bagla)
				r.With(middleware.MusteriScope).Post("/domains/{id}/git/klonla", gitH.Klonla)
				r.With(middleware.MusteriScope).Post("/domains/{id}/git/pull", gitH.Pull)
				r.With(middleware.MusteriScope).Get("/domains/{id}/github", githubH.Get)
				r.With(middleware.MusteriScope).Post("/domains/{id}/github/connect", githubH.Connect)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/github", githubH.Disconnect)
				r.With(middleware.MusteriScope).Get("/domains/{id}/github/repos", githubH.ListRepos)
				r.With(middleware.MusteriScope).Get("/domains/{id}/github/branches", githubH.ListBranches)
				r.With(middleware.MusteriScope).Post("/domains/{id}/github/use", githubH.Use)
				r.With(middleware.DBSahipligiMusteri("dbId")).Post("/databases/{dbId}/pma-token", pmaH.TokenIste)
				r.Get("/php/versions", phpH.Versions)
				r.With(middleware.MusteriScope).Get("/domains/{id}/php-settings", phpH.GetAyarlar)
				r.With(middleware.MusteriScope).Put("/domains/{id}/php-settings", phpH.PutAyarlar)
				r.With(middleware.MusteriScope).Get("/domains/{id}/php/debug-log", phpH.GetDebugLog)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/php/debug-log", phpH.ClearDebugLog)
				r.With(middleware.MusteriScope).Get("/domains/{id}/kaynak", kaynakH.Goster)
				r.With(middleware.MusteriScope).Get("/domains/{id}/nginx-settings", nginxsetH.Goster)
				r.With(middleware.MusteriScope).Put("/domains/{id}/nginx-settings", nginxsetH.Kaydet)
				r.With(middleware.MusteriScope).Get("/domains/{id}/waf", wafH.Goster)
				r.With(middleware.MusteriScope).Put("/domains/{id}/waf", wafH.Kaydet)
				r.With(middleware.AdminOnly).Get("/php-extensions", phpExtH.List)
				r.With(middleware.AdminOnly).Put("/php-extensions/toggle", phpExtH.Toggle)
				r.With(middleware.AdminOnly).Post("/php-extensions/pecl-install", phpExtH.PECLKur)
				r.With(middleware.AdminOnly).Post("/php-extensions/pecl-uninstall", phpExtH.PECLSil)
				r.With(middleware.AdminOnly).Post("/php-extensions/ioncube-kur", phpExtH.IonCubeKur)
				r.With(middleware.AdminOnly).Post("/php-extensions/ioncube-kaldir", phpExtH.IonCubeKaldir)
				r.With(middleware.AdminOnly).Get("/paketler", paketlerH.Ara)
				r.With(middleware.AdminOnly).Get("/paketler/kurulu", paketlerH.Kurulu)
				r.With(middleware.AdminOnly).Get("/paketler/bilgi", paketlerH.Bilgi)
				r.With(middleware.AdminOnly).Get("/paketler/durum", paketlerH.Durum)
				r.With(middleware.AdminOnly).Post("/paketler/kur", paketlerH.Kur)
				r.With(middleware.AdminOnly).Post("/paketler/kaldir", paketlerH.Kaldir)
				r.With(middleware.AdminOnly).Post("/paketler/guncelle", paketlerH.Guncelle)
				r.With(middleware.AdminOnly).Get("/php-surumler", phpSurumH.Liste)
				r.With(middleware.AdminOnly).Post("/php-surumler/kur", phpSurumH.Kur)
				r.With(middleware.AdminOnly).Post("/php-surumler/kaldir", phpSurumH.Kaldir)
				r.With(middleware.AdminOnly).Get("/php-surumler/durum", phpSurumH.OpDurum)
				r.With(middleware.AdminOnly).Get("/php-surumler/log", phpSurumH.OpLog)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/git", gitH.Sil)
			})
		})
	})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Minute, // buyuk upload icin genis ust sinir (slowloris ust siniri kalir)
		WriteTimeout:      30 * time.Minute, // buyuk upload/download: yanit yazma deadline-i erken gecmesin
		IdleTimeout:       120 * time.Second,
	}

	monitor.StartYukSampler(d, 60*time.Second)         // dashboard yük geçmişi örnekleyici
	istatistik.StartTrafikAggregator(d, 5*time.Minute) // per-domain aylık trafik toplayıcı
	guvenlikduvari.FirewalldDevral()                   // AlmaLinux varsayılan firewalld'yi devral (çakışma önleme)
	if err := guvenlikduvari.Reapply(d); err != nil {
		log.Printf("firewall reapply warn: %v", err)
	}

	// Sürüm kontrolü + güvenlik duyuru kanalı. PANEL_SURUM_KONTROL=0 ile kapalı;
	// kapalıyken hiç ağ isteği atılmaz (bkz. internal/system/surumkontrol.go).
	system.SurumBaslat(version)

	// Anasayfa Servisler widget'i, mail eklentisi servislerini gostermek icin
	// cp_eklentiler kapisini okur (bkz. internal/system/usage.go).
	system.DBAyarla(d)

	go func() {
		log.Printf("girginospanel %s — %s üzerinde dinleniyor (env=%s)", version, cfg.ListenAddr, cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Printf("kapatılıyor...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

// gocDefteriKur — `schema_migrations` defterini olusturur ve uygulanmis
// goclerin (dosya adi -> sha256) haritasini dondurur.
//
// 🔴 NEDEN: defter YOKKEN her acilista 59 .sql dosyasinin TAMAMI yeniden
// kosuyordu. Bu, her goc dosyasinin sonsuza dek idempotent kalmasina bel
// bagliyor (tek bir "IF NOT EXISTS" unutmak her yeniden baslatmada hata
// uretir) ve acilisi gereksiz uzatiyor. Ayrica "0011" gibi mukerrer numara
// sorunlarini gorunmez kiliyordu.
//
// Ikinci donen deger false ise defter KURULAMADI; cagiran taraf eski
// davranisa (her seyi yeniden kos) duser -- defter kurulamadi diye panel
// acilmamazlik etmez.
func gocDefteriKur(d *sql.DB) (map[string]string, bool) {
	if _, err := d.Exec("CREATE TABLE IF NOT EXISTS schema_migrations (\n" +
		"  dosya     VARCHAR(190) NOT NULL PRIMARY KEY,\n" +
		"  sha256    CHAR(64)     NOT NULL,\n" +
		"  uygulandi TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP\n" +
		") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4"); err != nil {
		log.Printf("🔴 schema_migrations olusturulamadi: %v — goc defteri DEVRE DISI, "+
			"tum gocler eskisi gibi her acilista yeniden kosacak", err)
		return nil, false
	}
	rows, err := d.Query("SELECT dosya, sha256 FROM schema_migrations")
	if err != nil {
		log.Printf("🔴 schema_migrations okunamadi: %v — goc defteri DEVRE DISI", err)
		return nil, false
	}
	defer rows.Close()
	uygulanan := make(map[string]string)
	for rows.Next() {
		var dosya, ozet string
		if err := rows.Scan(&dosya, &ozet); err != nil {
			log.Printf("🔴 schema_migrations satiri okunamadi: %v — goc defteri DEVRE DISI", err)
			return nil, false
		}
		uygulanan[dosya] = ozet
	}
	if err := rows.Err(); err != nil {
		log.Printf("🔴 schema_migrations taramasi yarim kaldi: %v — goc defteri DEVRE DISI", err)
		return nil, false
	}
	return uygulanan, true
}

func runMigrations(d *sql.DB) {
	// Gercek (zararsiz olmayan) goc hatalarinin sayisi.
	gercekHata := 0
	dir := "/opt/girginospanel/src/migrations"
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("migrations dir okunamadı: %v", err)
		return
	}

	// Defter. Bos defterle acilan MEVCUT bir kurulumda ilk tur bugunku
	// davranisin AYNISIDIR (her sey kosar, hepsi idempotent) -- fark sonraki
	// aciliSlarda ortaya cikar. Yani gecis turu davranis degistirmez.
	uygulanan, defterVar := gocDefteriKur(d)
	kosuldu, atlandi := 0, 0

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		body, err := os.ReadFile(dir + "/" + e.Name())
		if err != nil {
			continue
		}
		ozet := fmt.Sprintf("%x", sha256.Sum256(body))
		if defterVar {
			if eski, ok := uygulanan[e.Name()]; ok {
				// 🔴 Uygulanmis bir goc dosyasi SONRADAN degistirilmis. Otomatik
				// yeniden kosmuyoruz: yikici bir duzenleme (DROP/UPDATE) sessizce
				// canliya uygulanmis olurdu. Bunun yerine ACIKCA bagiriyoruz.
				if eski != ozet {
					log.Printf("🔴 GOC DEGISMIS (%s): defterdeki ozet tutmuyor — YENIDEN KOSULMADI. "+
						"Uygulanmis goc dosyasi duzenlenmemelidir; degisiklik icin YENI numarali dosya ekleyin. "+
						"Bilerek yeniden kosturmak icin: DELETE FROM schema_migrations WHERE dosya='%s';",
						e.Name(), e.Name())
				}
				atlandi++
				continue
			}
		}
		kosuldu++
		// Bu dosyaya ozel hata sayaci: deftere YALNIZCA temiz gecen dosya yazilir.
		// Yarim uygulanmis bir goc "uygulandi" diye isaretlenirse bir daha ASLA
		// denenmez ve sema kalici olarak eksik kalirdi.
		dosyaHata := 0
		log.Printf("migration: %s", e.Name())
		// Önce yorum satırlarını çıkar
		var cleaned []string
		for _, line := range strings.Split(string(body), "\n") {
			t := strings.TrimSpace(line)
			if t == "" || strings.HasPrefix(t, "--") {
				continue
			}
			cleaned = append(cleaned, line)
		}
		sqlBody := strings.Join(cleaned, "\n")
		for _, stmt := range strings.Split(sqlBody, ";") {
			s := strings.TrimSpace(stmt)
			if s == "" {
				continue
			}
			if _, err := d.Exec(s); err != nil {
				// Zararsiz olanlar: goc takibi olmadigi icin her acilista tum
				// dosyalar yeniden calisir; "zaten var" hatalari BEKLENIR.
				msg := strings.ToLower(err.Error())
				zararsiz := strings.Contains(msg, "duplicate column") ||
					strings.Contains(msg, "duplicate key") ||
					strings.Contains(msg, "already exists") ||
					strings.Contains(msg, "duplicate entry")
				if zararsiz {
					continue
				}
				// 🔴 GERCEK hata: gurultuden AYRILIR ve sayilir.
				dosyaHata++
				gercekHata++
				log.Printf("  🔴 GOC HATASI (%s): %v", e.Name(), err)
			}
		}
		// Deftere yaz — SADECE tek bir gercek hata bile almadiysa.
		if defterVar && dosyaHata == 0 {
			if _, err := d.Exec(
				"INSERT INTO schema_migrations (dosya, sha256) VALUES (?, ?) "+
					"ON DUPLICATE KEY UPDATE sha256=VALUES(sha256), uygulandi=CURRENT_TIMESTAMP",
				e.Name(), ozet); err != nil {
				// Deftere yazamamak veri kaybi degil: bir sonraki aciliSta bu
				// dosya yine kosar (bugunku davranis). Yalnizca gorunur olsun.
				log.Printf("  ! goc defterine yazilamadi (%s): %v — sonraki aciliSta yeniden kosacak", e.Name(), err)
			}
		}
	}
	if defterVar {
		log.Printf("goc defteri: %d kosuldu / %d atlandi (zaten uygulanmis) / toplam %d",
			kosuldu, atlandi, kosuldu+atlandi)
	}
	if gercekHata > 0 {
		log.Printf("🔴 %d goc ifadesi BASARISIZ oldu (yukariya bakin). "+
			"Sema eksik kalmis olabilir.", gercekHata)
	}
	// 🔴 "Goc calisti" YETMEZ -- kodun bagimli oldugu kolonlar GERCEKTEN var mi?
	semaDogrula(d)
}

// semaDogrula — kodun okudugu kritik kolonlarin varligini information_schema'dan
// DOGRULAR. Goc dosyasinin calismis olmasi kolonun olustugunu KANITLAMAZ:
// dosya okunmus ama ifade dusmus olabilir (ve eskiden bu sessizce geciyordu).
// Eksik kolon panelde "alan hic gorunmuyor" seklinde ortaya cikar; sebebini
// burada ACIKCA yazariz.
func semaDogrula(d *sql.DB) {
	beklenen := []struct{ tablo, kolon string }{
		{"service_plans", "max_email"},
		{"service_plans", "saatlik_mail_limiti"},
		{"service_plans", "mail_kutu_kota_mb"},
		{"cp_ayarlar", "anahtar"},
		{"cp_eklentiler", "ad"},
	}
	var eksik []string
	for _, b := range beklenen {
		var n int
		err := d.QueryRow(`SELECT COUNT(*) FROM information_schema.COLUMNS
		    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`,
			b.tablo, b.kolon).Scan(&n)
		if err != nil {
			log.Printf("sema dogrulama sorgusu basarisiz (%s.%s): %v", b.tablo, b.kolon, err)
			continue
		}
		if n == 0 {
			eksik = append(eksik, b.tablo+"."+b.kolon)
		}
	}
	if len(eksik) > 0 {
		log.Printf("🔴 SEMA EKSIK — su kolonlar YOK: %s", strings.Join(eksik, ", "))
		log.Printf("🔴 Ilgili paneldeki alanlar GORUNMEYECEK. Goc dosyalari " +
			"/opt/girginospanel/src/migrations altinda; hedef tabloyu ve " +
			"yukaridaki GOC HATASI satirlarini kontrol edin.")
		return
	}
	log.Printf("sema dogrulama: kritik kolonlarin tamami mevcut (%d kontrol)", len(beklenen))
}

func detectIPv4() string {
	if v := strings.TrimSpace(os.Getenv("PANEL_PUBLIC_IPV4")); v != "" {
		return v
	}
	// non-loopback ilk IPv4 (sade fallback)
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip := ipnet.IP.To4(); ip != nil {
				return ip.String()
			}
		}
	}
	return ""
}
