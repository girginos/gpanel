package portyonetim

// Backend + dis port degistirme — multi-verify #2 sonrasi revize:
//
// CRITICAL FIX'LER:
//   #1 External reachability — UI'da guclu uyari + firewall not-verified
//      dokumantasyon. Kod tarafinda public IP curl'lansa da NAT/loopback
//      firewall katmanini bypass edebilir; UI kullaniciyi FW konusunda uyariyor.
//   #2 Rollback verify — geriYukle sonrasi curl-verify + alarm log.
//   #3 Backend port degisikligi DETACH — panel restart'inda verify sureci
//      olmesin diye harici bash helper systemd-run ile fire-and-forget.
//      DisPort ise panel restart ETMEZ, ayni surecte kalir (sorunsuz).
//
// IMPORTANT FIX'LER:
//   #5 Backup rotate — son 5 tut, kalanlari sil.
//   #6 Cross-package concurrency — backend degisikligi ipyonetim.YazKilitTryLock()
//      alir (baska IP islemi sirasinda restart olmasin).
//   #7 nginx proxy_pass regex — whitespace-agnostic.
//   #8 nginx listen regex — [IP:] prefix optional.
//   #9 curlVerify insecure bool parametresi (URL " -k" hack yerine).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"girginospanel/internal/ipyonetim"
)

const (
	// Backend degisikligi icin harici bash helper. Panel restart olsa da
	// helper ayakta kalir; rollback + curl-verify sonuna kadar surer.
	BackendHelperYolu = "/usr/local/sbin/girginospanel-port-swap.sh"
	YedekMaxSayi      = 5 // her dosya icin son N yedek tutulur
)

// BackendPortDegistir — asagida validasyon; sonra DETACH helper cagirir.
// DisPortDegistiSonrasi — main.go set eder: dış port değişikliği başarılı
// olduğunda hostuyg subdomain vhost'larını güncellemek için.
var DisPortDegistiSonrasi func(eskiPort, yeniPort int) error

func BackendPortDegistir(ctx context.Context, db *sql.DB, yeniPort int, aktorUID *int64) (*Is, error) {
	if yasak, aciklama := PortYasakMi(yeniPort); yasak {
		return nil, fmt.Errorf("port %d yasak: %s", yeniPort, aciklama)
	}
	eski := BackendPortOku()
	if yeniPort == eski {
		return nil, errors.New("ayni port — degisiklik yok")
	}
	if PortMesgul(yeniPort, eski) {
		return nil, fmt.Errorf("port %d baska bir servis tarafindan kullaniliyor", yeniPort)
	}
	if !TipKilitle("backend") {
		return nil, errors.New("baska bir port degisikligi devam ediyor")
	}
	// Cross-package lock — IP admin isleri sirasinda restart olmasin
	if !ipyonetim.YazKilitTryLock() {
		TipSerbest("backend")
		return nil, errors.New("IP yonetimi islemi devam ediyor — port degisikligi bekletildi")
	}
	is := isBaslat("backend", eski, yeniPort)
	go backendYurutDetach(context.Background(), db, is, eski, yeniPort, aktorUID)
	return is, nil
}

// DisPortDegistir — nginx :listen degisikligi, panel restart YOK.
func DisPortDegistir(ctx context.Context, db *sql.DB, yeniPort int, aktorUID *int64) (*Is, error) {
	if yasak, aciklama := PortYasakMi(yeniPort); yasak {
		return nil, fmt.Errorf("port %d yasak: %s", yeniPort, aciklama)
	}
	eski := DisPortOku()
	if yeniPort == eski {
		return nil, errors.New("ayni port — degisiklik yok")
	}
	if PortMesgul(yeniPort, eski) {
		return nil, fmt.Errorf("port %d baska bir servis tarafindan kullaniliyor", yeniPort)
	}
	if !TipKilitle("dis") {
		return nil, errors.New("baska bir port degisikligi devam ediyor")
	}
	is := isBaslat("dis", eski, yeniPort)
	go disYurut(context.Background(), db, is, eski, yeniPort, aktorUID)
	return is, nil
}

/* ============================================================
   Backend yurutme — DETACH bash helper systemd-run ile
============================================================ */

func backendYurutDetach(ctx context.Context, db *sql.DB, is *Is, eski, yeni int, aktor *int64) {
	defer TipSerbest("backend")
	defer ipyonetim.YazKilitUnlock()
	defer gecmisYaz(db, is, aktor)

	// Helper var mi? Yoksa hata (kurulum eksik)
	if _, err := os.Stat(BackendHelperYolu); err != nil {
		bitir(is, "helper mevcut", fmt.Errorf("bash helper yok (%s) — 'sudo /opt/girginospanel/bin/install-port-helper' calistirin", BackendHelperYolu), false)
		return
	}
	isAdim(is, "helper hazir", true, BackendHelperYolu)

	// Log dosyasi — helper sonucunu buraya yazar; biz burdan okuruz.
	logDosya := fmt.Sprintf("/var/log/girginospanel/port-swap.%d.log", time.Now().Unix())
	_ = os.MkdirAll(filepath.Dir(logDosya), 0755)

	// systemd-run ile detach — panel restart olsa bile helper ayakta kalir.
	unit := fmt.Sprintf("portswap-%d", time.Now().Unix())
	cmd := exec.Command("systemd-run",
		"--unit="+unit, "--collect",
		"--property=StandardOutput=append:"+logDosya,
		"--property=StandardError=append:"+logDosya,
		BackendHelperYolu, "backend", itoa(eski), itoa(yeni), logDosya)
	if out, err := cmd.CombinedOutput(); err != nil {
		bitir(is, "systemd-run", fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err), false)
		return
	}
	isAdim(is, "helper detach basladi", true, "unit="+unit)

	// Helper'in log dosyasini poll et — SONUC: satiri gorene kadar.
	// Toplam maksimum 40 saniye (helper icinde de 30s timeout var).
	basladi := time.Now()
	for time.Since(basladi) < 40*time.Second {
		time.Sleep(1500 * time.Millisecond)
		b, err := os.ReadFile(logDosya)
		if err != nil {
			continue
		}
		// Helper log formati:
		//   ADIM: <etiket> <ok> <bilgi>
		//   SONUC: OK|ROLLBACK|FAIL <hata>
		// Duplicate onlemek icin her poll'da helper adimlarini sifirla,
		// panel-tarafi ilk 2 adim (helper hazir, detach basladi) korunur.
		isTrimAdimlar(is, 2)
		for _, l := range strings.Split(string(b), "\n") {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "ADIM:") {
				parts := strings.SplitN(strings.TrimPrefix(l, "ADIM:"), "|", 3)
				if len(parts) >= 2 {
					etiket := strings.TrimSpace(parts[0])
					ok := strings.TrimSpace(parts[1]) == "1"
					bilgi := ""
					if len(parts) == 3 {
						bilgi = strings.TrimSpace(parts[2])
					}
					isAdim(is, etiket, ok, bilgi)
				}
			}
			if strings.HasPrefix(l, "SONUC:") {
				sonuc := strings.TrimSpace(strings.TrimPrefix(l, "SONUC:"))
				parts := strings.SplitN(sonuc, " ", 2)
				kod := parts[0]
				hata := ""
				if len(parts) > 1 {
					hata = parts[1]
				}
				switch kod {
				case "OK":
					isBitir(is, true, "", false)
				case "ROLLBACK":
					isBitir(is, false, hata, true)
				default:
					isBitir(is, false, hata, false)
				}
				// backup rotate
				rotateYedekler()
				return
			}
		}
	}
	// timeout
	isBitir(is, false, "helper 40s icinde sonuc yazmadi (log: "+logDosya+")", false)
}

/* ============================================================
   Dis port yurutme — panel restart YOK, ayni surecte guvenli
============================================================ */

func disYurut(ctx context.Context, db *sql.DB, is *Is, eski, yeni int, aktor *int64) {
	defer TipSerbest("dis")
	defer gecmisYaz(db, is, aktor)
	defer rotateYedekler()

	ts := time.Now().Unix()
	nginxYedek := fmt.Sprintf("%s.yedek.port.%d", VhostYolu, ts)
	if err := dosyaKopyala(VhostYolu, nginxYedek); err != nil {
		bitir(is, "nginx yedegi", err, false)
		return
	}
	isAdim(is, "nginx yedegi alindi", true, nginxYedek)

	if err := nginxListenDegistir(VhostYolu, eski, yeni); err != nil {
		bitir(is, "nginx listen guncelle", err, false)
		return
	}
	isAdim(is, "nginx listen guncellendi", true, fmt.Sprintf("-> %d (v4+v6)", yeni))

	if out, err := exec.CommandContext(ctx, "nginx", "-t").CombinedOutput(); err != nil {
		_ = dosyaKopyala(nginxYedek, VhostYolu)
		bitir(is, "nginx -t", fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err), true)
		return
	}
	isAdim(is, "nginx -t", true, "syntax OK")

	// Best-effort: catchall (bilinmeyen-host) vhost'unu da senkronla.
	// Dosya yoksa veya güncelleme başarısızsa akış devam eder — kritik değil.
	if _, statErr := os.Stat(VhostCatchallYolu); statErr == nil {
		catchallYedek := fmt.Sprintf("%s.yedek.port.%d", VhostCatchallYolu, ts)
		if kopErr := dosyaKopyala(VhostCatchallYolu, catchallYedek); kopErr == nil {
			if chErr := nginxListenDegistir(VhostCatchallYolu, eski, yeni); chErr != nil {
				_ = dosyaKopyala(catchallYedek, VhostCatchallYolu)
				isAdim(is, "catchall vhost sync", false, chErr.Error())
			} else if out, tErr := exec.CommandContext(ctx, "nginx", "-t").CombinedOutput(); tErr != nil {
				_ = dosyaKopyala(catchallYedek, VhostCatchallYolu)
				isAdim(is, "catchall vhost sync", false, strings.TrimSpace(string(out)))
			} else {
				isAdim(is, "catchall vhost sync", true, fmt.Sprintf("-> %d", yeni))
			}
		}
	}

	if err := FwPortAc(yeni); err != nil {
		isAdim(is, "firewall yeni port aç", false, err.Error())
	} else {
		isAdim(is, "firewall yeni port aç", true, fmt.Sprintf(":%d/tcp", yeni))
	}

	if out, err := exec.CommandContext(ctx, "nginx", "-s", "reload").CombinedOutput(); err != nil {
		_ = dosyaKopyala(nginxYedek, VhostYolu)
		if out2, err2 := exec.Command("nginx", "-s", "reload").CombinedOutput(); err2 != nil {
			log.Printf("portyonetim.disYurut: rollback nginx reload FAIL: %s (%v)",
				strings.TrimSpace(string(out2)), err2)
		}
		bitir(is, "nginx reload", fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err), true)
		return
	}
	isAdim(is, "nginx reload", true, "")

	time.Sleep(3 * time.Second)
	if !curlVerify(fmt.Sprintf("https://127.0.0.1:%d/healthz", yeni), true, 5, 1200*time.Millisecond) {
		_ = dosyaKopyala(nginxYedek, VhostYolu)
		if out2, err2 := exec.Command("nginx", "-s", "reload").CombinedOutput(); err2 != nil {
			log.Printf("portyonetim.disYurut: rollback nginx reload FAIL: %s (%v)",
				strings.TrimSpace(string(out2)), err2)
		}
		_ = FwPortKapat(yeni) // rollback: yeni FW kuralını temizle
		// ROLLBACK VERIFY (multi-verify #2 fix)
		time.Sleep(2 * time.Second)
		if !curlVerify(fmt.Sprintf("https://127.0.0.1:%d/healthz", eski), true, 5, 1200*time.Millisecond) {
			log.Printf("portyonetim.disYurut: 🔴 ROLLBACK VERIFY FAIL — panel eski portta da yaniti yok!")
			bitir(is, "yeni port healthz", fmt.Errorf("yeni port %d yaniti yok VE rollback sonrasi eski port %d de yanit vermiyor — manuel inceleme", yeni, eski), true)
			return
		}
		bitir(is, "yeni port healthz", errors.New("yeni port :"+itoa(yeni)+" yanit vermiyor — geri alindi (eski port dogrulandi)"), true)
		return
	}
	isAdim(is, "yeni port healthz", true, fmt.Sprintf("https://127.0.0.1:%d/healthz", yeni))

	// Faz 3.3 hook: hostuyg subdomain vhost port sync
	if DisPortDegistiSonrasi != nil {
		if err := DisPortDegistiSonrasi(eski, yeni); err != nil {
			isAdim(is, "hostuyg subdomain sync", false, err.Error())
		} else {
			isAdim(is, "hostuyg subdomain sync", true, "")
		}
	}

	// FW: eski portu kapat (idempotent)
	if err := FwPortKapat(eski); err != nil {
		isAdim(is, "firewall eski port kapat", false, err.Error())
	} else {
		isAdim(is, "firewall eski port kapat", true, fmt.Sprintf(":%d/tcp", eski))
	}

	bitir(is, "", nil, false)
}

/* ============================================================ */

func bitir(is *Is, adim string, hata error, rollback bool) {
	if hata != nil {
		if adim != "" {
			isAdim(is, adim, false, hata.Error())
		}
		isBitir(is, false, hata.Error(), rollback)
		return
	}
	isBitir(is, true, "", false)
}

func dosyaKopyala(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0644)
}

// rotateYedekler — env + nginx yedeklerini son YedekMaxSayi tut, kalanlari sil.
// Multi-verify #5 fix.
func rotateYedekler() {
	rotateOne := func(dizin, prefix string) {
		entries, err := os.ReadDir(dizin)
		if err != nil {
			return
		}
		var eslesenler []string
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), prefix+".yedek.port.") {
				eslesenler = append(eslesenler, filepath.Join(dizin, e.Name()))
			}
		}
		sort.Strings(eslesenler) // eskiden yeniye (ts sirasi)
		if len(eslesenler) <= YedekMaxSayi {
			return
		}
		silinecek := len(eslesenler) - YedekMaxSayi
		for i := 0; i < silinecek; i++ {
			_ = os.Remove(eslesenler[i])
		}
	}
	rotateOne(filepath.Dir(EnvYolu), filepath.Base(EnvYolu))
	rotateOne(filepath.Dir(VhostYolu), filepath.Base(VhostYolu))
}

var (
	// Multi-verify #7 fix — whitespace-agnostic proxy_pass regex
	reProxyPassPort = regexp.MustCompile(`(proxy_pass\s+http://127\.0\.0\.1:)(\d+)(\s*;)`)
	// Multi-verify #8 fix — optional [IP:] prefix
	reListenV4 = regexp.MustCompile(`(?m)^(\s*listen\s+(?:\d+\.\d+\.\d+\.\d+:)?)(\d+)(\s+ssl\b)`)
	reListenV6 = regexp.MustCompile(`(?m)^(\s*listen\s+(?:\[[0-9a-fA-F:]+\]|\[::\]):)(\d+)(\s+ssl\b)`)
)

// nginxListenDegistir — v4 + v6 listen satirlarini yeni port'a degistir.
func nginxListenDegistir(yol string, eski, yeni int) error {
	b, err := os.ReadFile(yol)
	if err != nil {
		return err
	}
	s := string(b)
	degistir := func(re *regexp.Regexp) {
		s = re.ReplaceAllStringFunc(s, func(m string) string {
			p := re.FindStringSubmatch(m)
			if len(p) >= 4 && p[2] == itoa(eski) {
				return p[1] + itoa(yeni) + p[3]
			}
			return m
		})
	}
	degistir(reListenV4)
	degistir(reListenV6)
	if s == string(b) {
		return fmt.Errorf("listen %d ssl satiri vhost'ta bulunamadi", eski)
	}
	return atomicYaz(yol, []byte(s), 0644)
}

func atomicYaz(yol string, b []byte, mode os.FileMode) error {
	tmp := yol + ".tmp"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	return os.Rename(tmp, yol)
}

// curlVerify — multi-verify #9 fix: insecure bool parametresi.
func curlVerify(url string, insecure bool, deneme int, ara time.Duration) bool {
	for i := 0; i < deneme; i++ {
		args := []string{"-s", "-o", "/dev/null", "-w", "%{http_code}", "-m", "5"}
		if insecure {
			args = append(args, "-k")
		}
		args = append(args, url)
		out, _ := exec.Command("curl", args...).Output()
		code := strings.TrimSpace(string(out))
		if code == "200" || code == "204" {
			return true
		}
		time.Sleep(ara)
	}
	return false
}

func itoa(i int) string { return fmt.Sprintf("%d", i) }

/* ============================================================
   DB history — cp_port_gecmisi
============================================================ */

func gecmisYaz(db *sql.DB, is *Is, aktor *int64) {
	ctx, iptal := context.WithTimeout(context.Background(), 5*time.Second)
	defer iptal()
	_, _ = db.ExecContext(ctx,
		`INSERT INTO cp_port_gecmisi
			(tip, eski_port, yeni_port, basarili, rollback, son_hata, aktor_uid)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		is.Tip, is.EskiPort, is.YeniPort, is.Basarili, is.Rollback, is.SonHata, aktor)
}
