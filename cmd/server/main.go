package main

import (
	"context"
	"database/sql"
	"errors"
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
	"girginospanel/internal/dns"
	"girginospanel/internal/domains"
	"girginospanel/internal/eklenti"
	"girginospanel/internal/files"
	"girginospanel/internal/git"
	githubpkg "girginospanel/internal/github"
	"girginospanel/internal/guvenlikduvari"
	"girginospanel/internal/httpx"
	"girginospanel/internal/istatistik"
	"girginospanel/internal/kaynak"
	"girginospanel/internal/kaynaklimit"
	"girginospanel/internal/logs"
	"girginospanel/internal/middleware"
	"girginospanel/internal/monitor"
	"girginospanel/internal/musteri"
	"girginospanel/internal/nginxset"
	"girginospanel/internal/paketler"
	"girginospanel/internal/performans"
	"girginospanel/internal/php"
	"girginospanel/internal/phpext"
	"girginospanel/internal/phpsurum"
	"girginospanel/internal/plans"
	"girginospanel/internal/pma"
	"girginospanel/internal/provisioner"
	"girginospanel/internal/redis"
	"girginospanel/internal/sifrekoruma"
	"girginospanel/internal/sitekopya"
	"girginospanel/internal/sshaccess"
	"girginospanel/internal/subdomain"
	"girginospanel/internal/system"
	"girginospanel/internal/users"
	"girginospanel/internal/waf"
	"girginospanel/internal/wordpress"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

const version = "0.3.0-f2"

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
	filesH := &files.Handlers{DB: d}
	cronH := &cron.Handlers{DB: d}
	logsH := &logs.Handlers{DB: d}
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
	nginxsetH := &nginxset.Handlers{DB: d}
	sshH := &sshaccess.Handlers{DB: d, IPv4: ipv4}
	statH := &istatistik.Handlers{DB: d}
	perfH := &performans.Handlers{DB: d}
	compH := &composer.Handlers{DB: d}
	korumaH := &sifrekoruma.Handlers{DB: d}
	avH := &antivirus.Handlers{DB: d}
	kopyaH := &sitekopya.Handlers{DB: d}
	wpH := &wordpress.Handlers{DB: d}
	fwH := &guvenlikduvari.Handlers{DB: d}
	wafH := &waf.Handlers{DB: d}
	redisH := &redis.Handlers{DB: d}
	subH := &subdomain.Handlers{DB: d, IPv4: ipv4}
	sshaccess.EnsureInfra()
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
			r.With(middleware.AdminOnly).Put("/me", authH.ProfilGuncelle)
			r.With(middleware.AdminOnly).Get("/dashboard-duzen", authH.DashboardDuzenGetir)
			r.With(middleware.AdminOnly).Put("/dashboard-duzen", authH.DashboardDuzenKaydet)
			r.With(middleware.AdminOnly).Post("/me/parola", authH.ParolaDegistir)
			r.With(middleware.AdminOnly).Get("/me/2fa/setup", authH.TwoFASetup)
			r.With(middleware.AdminOnly).Post("/me/2fa/enable", authH.TwoFAEnable)
			r.With(middleware.AdminOnly).Post("/me/2fa/disable", authH.TwoFADisable)
			r.With(middleware.AdminOnly).Get("/domains", domainsH.List)
			r.With(middleware.MusteriScope).Get("/domains/{id}", domainsH.Get)
			r.With(middleware.AdminOnly).Get("/system/usage", system.Handler)
			r.With(middleware.AdminOnly).Get("/system/servisler", system.ServisDurumlar)
			r.With(middleware.AdminOnly).Post("/system/servis-islem", system.ServisIslem)
			r.With(middleware.AdminOnly).Get("/system/guncelleme", system.GuncellemeDurum)
			r.With(middleware.AdminOnly).Post("/system/guncelleme/baslat", system.GuncellemeBaslat)
			r.With(middleware.AdminOnly).Get("/system/guncelleme/log", system.GuncellemeLog)
			r.With(middleware.AdminOnly).Get("/system/optimize", system.OptimizeDurum)
			r.With(middleware.AdminOnly).Post("/system/optimize/baslat", system.OptimizeBaslat)
			r.With(middleware.AdminOnly).Get("/system/optimize/log", system.OptimizeLog)
			r.With(middleware.AdminOnly).Get("/system/cve", system.CveDurum)
			r.With(middleware.AdminOnly).Get("/system/surum-kontrol", system.SurumKontrolDurum)
			r.With(middleware.AdminOnly).Post("/system/surum-kontrol/yenile", system.SurumKontrolYenile)
			r.With(middleware.AdminOnly).Post("/system/cve/guncelle", system.CveGuncelle)
			r.With(middleware.AdminOnly).Get("/system/cve/log", system.CveLog)
			r.With(middleware.AdminOnly).Get("/system/kernelcare", system.KernelcareDurumHandler)
			r.With(middleware.AdminOnly).Post("/system/kernelcare/yamala", system.KernelcareYamala)
			eklentiH.Routes(r)
			r.With(middleware.AdminOnly).Get("/system/processes", monitor.Processes)
			r.With(middleware.AdminOnly).Get("/system/load-history", monitorH.YukGecmisi)
			r.With(middleware.AdminOnly).Get("/admin/system/loglar", monitorH.SunucuLog)
			r.With(middleware.MusteriScope).Get("/domains/{id}/health", monitorH.Health)

			// Yazma + müşteri-scope route'ları — per-route AdminOnly/MusteriScope ile yetkilendirilir
			r.Group(func(r chi.Router) {
				r.With(middleware.AdminOnly).Post("/domains", domainsH.Create)
				r.With(middleware.MusteriScope).Delete("/domains/{id}", domainsH.Delete)
				r.With(middleware.AdminOnly).Post("/domains/toplu/sahip", domainsH.TopluSahip)
				r.With(middleware.AdminOnly).Post("/domains/toplu/durum", domainsH.TopluDurum)
				r.With(middleware.MusteriScope).Put("/domains/{id}/php", domainsH.SetPHP)
				r.With(middleware.MusteriScope).Get("/domains/{id}/ssh", sshH.Goster)
				r.With(middleware.AdminOnly).Put("/domains/{id}/ssh", sshH.Ayarla)
				r.With(middleware.AdminOnly).Put("/domains/{id}/ssh/anahtar", sshH.AnahtarKaydet)
				r.With(middleware.MusteriScope).Get("/domains/{id}/istatistik", statH.Goster)
				r.With(middleware.MusteriScope).Get("/domains/{id}/performans", perfH.Goster)
				r.With(middleware.MusteriScope).Get("/domains/{id}/composer", compH.Durum)
				r.With(middleware.MusteriScope).Post("/domains/{id}/composer", compH.Calistir)
				r.With(middleware.MusteriScope).Get("/domains/{id}/redis", redisH.Durum)
				r.With(middleware.MusteriScope).Post("/domains/{id}/redis", redisH.Ac)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/redis", redisH.Kapat)
				r.With(middleware.MusteriScope).Get("/domains/{id}/koruma", korumaH.Liste)
				r.With(middleware.MusteriScope).Post("/domains/{id}/koruma", korumaH.Ekle)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/koruma/{kid}", korumaH.Sil)
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
				r.With(middleware.MusteriScope).Get("/domains/{id}/subdomain/{sid}/ssl", subH.SSLDurum)
				r.With(middleware.MusteriScope).Post("/domains/{id}/subdomain/{sid}/ssl", subH.SSLKur)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/subdomain/{sid}/ssl", subH.SSLKaldir)
				r.With(middleware.MusteriScope).Get("/domains/{id}/web-backend", domainsH.GetWebBackend)
				r.With(middleware.MusteriScope).Put("/domains/{id}/web-backend", domainsH.SetWebBackend)
				r.With(middleware.MusteriScope).Put("/domains/{id}/ftp/password", domainsH.SetFTPPassword)
				r.With(middleware.MusteriScope).Get("/domains/{id}/databases", domainsH.ListDatabases)
				r.With(middleware.MusteriScope).Post("/domains/{id}/databases", domainsH.CreateDatabase)
				r.With(middleware.AdminOnly).Delete("/databases/{dbid}", domainsH.DeleteDatabase)
				r.With(middleware.AdminOnly).Put("/databases/{dbid}/password", domainsH.SetDatabasePassword)
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
				r.With(middleware.MusteriScope).Get("/domains/{id}/ssl", domainsH.SSLDurum)
				r.With(middleware.MusteriScope).Post("/domains/{id}/ssl/issue", domainsH.SSLIssue)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/ssl", domainsH.SSLDisable)
				r.With(middleware.MusteriScope).Get("/domains/{id}/cron", cronH.List)
				r.With(middleware.MusteriScope).Post("/domains/{id}/cron", cronH.Create)
				r.With(middleware.MusteriScope).Delete("/domains/{id}/cron/{idx}", cronH.Delete)
				r.With(middleware.MusteriScope).Get("/domains/{id}/logs", logsH.List)
				r.With(middleware.MusteriScope).Get("/domains/{id}/logs/oku", logsH.Read)
				r.With(middleware.MusteriScope).Get("/domains/{id}/logs/canli", logsH.Tail)
				r.With(middleware.MusteriScope).Post("/domains/{id}/disk-hesapla", domainsH.DiskHesapla)
				r.With(middleware.AdminOnly).Get("/plans", plansH.List)
				r.With(middleware.AdminOnly).Get("/plans/{id}", plansH.Get)
				r.With(middleware.AdminOnly).Post("/plans", plansH.Create)
				r.With(middleware.AdminOnly).Put("/plans/{id}", plansH.Update)
				r.With(middleware.AdminOnly).Delete("/plans/{id}", plansH.Delete)
				r.With(middleware.AdminOnly).Get("/plans/{id}/domains", plansH.DomainlerAra)
				r.With(middleware.AdminOnly).Put("/domains/{id}/plan", domainsH.SetPlan)
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
				// Merkezi DNS şablonu (admin) — domain eklerken + "Şablonu Uygula" bunu okur
				r.With(middleware.AdminOnly).Get("/dns-template", dnsH.GetTemplate)
				r.With(middleware.AdminOnly).Put("/dns-template", dnsH.PutTemplate)
				// Domain askıya al / geri al (suspend)
				r.With(middleware.AdminOnly).Post("/domains/{id}/askiya-al", domainsH.AskiyaAl)
				r.With(middleware.AdminOnly).Post("/domains/{id}/askidan-al", domainsH.AskidanAl)
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
				r.With(middleware.MusteriScope).Get("/domains/{id}/backup-schedule", backupsH.GetSchedule)
				r.With(middleware.MusteriScope).Put("/domains/{id}/backup-schedule", backupsH.SetSchedule)
				r.With(middleware.AdminOnly).Post("/admin/backups/tick", backupsH.TickNow)
				r.With(middleware.AdminOnly).Get("/admin/backups/ozet", backupsH.Ozet)
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
				r.Post("/databases/{dbId}/pma-token", pmaH.TokenIste)
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

func runMigrations(d *sql.DB) {
	dir := "/opt/girginospanel/src/migrations"
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("migrations dir okunamadı: %v", err)
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		body, err := os.ReadFile(dir + "/" + e.Name())
		if err != nil {
			continue
		}
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
				log.Printf("  - hata (%s): %v", e.Name(), err)
			}
		}
	}
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
