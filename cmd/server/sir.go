package main

// Sır CLI'ı — config.Load()'dan ÖNCE çalışır (main.go). Kurulum/güncelleme
// betikleri ve yönetici düz kabuktan (root) çağırır:
//
//	girginospanel-server sir yenile [--hepsi | jwt db redis] [--yeniden-baslatma-yok] [--saglik-sn N]
//	girginospanel-server sir kurtar
//	girginospanel-server sir durum
//
// Sır DEĞERLERİ hiçbir zaman stdout/stderr'e ya da argv'ye çıkmaz.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"girginospanel/internal/config"
	"girginospanel/internal/sirdosya"
	"girginospanel/internal/sirrota"
)

const sirKullanim = `Usage: girginospanel-server sir <command>
  yenile [--hepsi | jwt db redis ...] [--yeniden-baslatma-yok] [--saglik-sn N]
        Rotates the selected panel secrets: DB (ALTER USER, all host entries) →
        Valkey (ACL SETUSER default + ACL SAVE) → /etc/girginospanel/env (atomic)
        → panel restart + /healthz. If the panel does not come up healthy with the
        new secrets, EVERYTHING is rolled back to the old secrets. Rotating the JWT
        secret drops all sessions.
        --hepsi                 rotate all three (jwt, db, redis)
        --yeniden-baslatma-yok  do not restart the panel (new secrets apply at the next restart)
        --saglik-sn N           how many seconds to wait for a healthy panel (default 120, min 5)
  kurtar  After an interrupted rotation, find the valid password and realign env.
  durum   Summary of the secret layout (prints no values).
Exit: 0 ok · 1 failed (old secrets still in place) · 2 usage · 3 CRITICAL (rollback incomplete)`

// sirServis — panel yeniden başlatma + sağlık (testte sahte).
type sirServis interface {
	Saglikli(ctx context.Context, sure time.Duration) error
	YenidenBaslat(ctx context.Context) error
	DeneYenidenBaslat(ctx context.Context, birimler ...string)
}

type systemdServis struct{ envYolu string }

func (s systemdServis) saglikURL() string {
	adres := "127.0.0.1:8080"
	if b, _, err := sirdosya.Oku(s.envYolu, 1<<20); err == nil {
		if v := strings.TrimSpace(config.EnvAyristir(b)["PANEL_LISTEN"]); v != "" {
			adres = v
		}
	}
	switch {
	case strings.HasPrefix(adres, ":"):
		adres = "127.0.0.1" + adres
	case strings.HasPrefix(adres, "0.0.0.0:"):
		adres = "127.0.0.1" + strings.TrimPrefix(adres, "0.0.0.0")
	}
	return "http://" + adres + "/healthz"
}

// Saglikli: panel ACTIVE + /healthz 200 art arda 2 yoklama (2 sn arayla).
func (s systemdServis) Saglikli(ctx context.Context, sure time.Duration) error {
	url := s.saglikURL()
	istemci := &http.Client{Timeout: 5 * time.Second}
	son := time.Now().Add(sure)
	ard := 0
	for time.Now().Before(son) {
		ok := exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "girginospanel").Run() == nil
		if ok {
			if r, err := istemci.Get(url); err == nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 4096))
				r.Body.Close()
				ok = r.StatusCode == http.StatusOK
			} else {
				ok = false
			}
		}
		if ok {
			ard++
			if ard >= 2 {
				return nil
			}
		} else {
			ard = 0
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("did not get ACTIVE + %s within %s", url, sure)
}

func (systemdServis) YenidenBaslat(ctx context.Context) error {
	if out, err := exec.CommandContext(ctx, "systemctl", "restart", "girginospanel").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl restart girginospanel: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// DeneYenidenBaslat: yalnız ÇALIŞAN birimleri yeniden başlatır (try-restart).
func (systemdServis) DeneYenidenBaslat(ctx context.Context, birimler ...string) {
	for _, b := range birimler {
		if out, err := exec.CommandContext(ctx, "systemctl", "try-restart", b).CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "  ! could not restart %s: %v %s\n", b, err, strings.TrimSpace(string(out)))
		}
	}
}

func sirCLI(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, sirKullanim)
		return 2
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "sir: must be run as root")
		return 2
	}
	kilit, err := sirKilit("/run/girginospanel-sir.lock")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sir: another secret operation is already running (%v)\n", err)
		return 1
	}
	defer kilit.Close()
	r := sirrota.Varsayilan()
	r.Log = func(f string, a ...any) { fmt.Printf("  ✓ "+f+"\n", a...) }
	sv := systemdServis{envYolu: r.EnvYolu}
	ctx := context.Background()
	switch args[0] {
	case "yenile":
		return sirYenileCLI(ctx, r, sv, args[1:], os.Stdout, os.Stderr)
	case "kurtar":
		if !r.BekleyenVar() {
			fmt.Println("no interrupted rotation")
			return 0
		}
		if err := r.Kurtar(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "✗ recovery: %v\n", err)
			return 1
		}
		fmt.Println("recovery done — restarting the panel")
		if err := sv.YenidenBaslat(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "✗", err)
			return 1
		}
		return 0
	case "durum":
		return sirDurum(r, os.Stdout)
	}
	fmt.Fprintln(os.Stderr, sirKullanim)
	return 2
}

func sirKilit(yol string) (*os.File, error) {
	f, err := os.OpenFile(yol, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// sirYenileCLI: argümanları çözer ve yenileme + yeniden başlatma + sağlık +
// gerekirse geri dönüş akışını yürütür.
func sirYenileCLI(ctx context.Context, r *sirrota.Rotator, sv sirServis, args []string, out, hata io.Writer) int {
	var hedefler []string
	baslat := true
	saglikSn := 120
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--hepsi":
			hedefler = append(hedefler, sirrota.Hepsi...)
		case "--yeniden-baslatma-yok":
			baslat = false
		case "--saglik-sn":
			if i+1 >= len(args) {
				fmt.Fprintln(hata, sirKullanim)
				return 2
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n < 5 {
				fmt.Fprintln(hata, "--saglik-sn must be at least 5")
				return 2
			}
			saglikSn, i = n, i+1
		case sirrota.JWT, sirrota.DB, sirrota.Redis:
			hedefler = append(hedefler, a)
		default:
			fmt.Fprintln(hata, sirKullanim)
			return 2
		}
	}
	if len(hedefler) == 0 {
		fmt.Fprintln(hata, sirKullanim)
		return 2
	}
	sure := time.Duration(saglikSn) * time.Second
	// Panel ŞU AN sağlıksızsa yenileme sonrası sağlık ölçümü anlamsız olur
	// (geri dönüş kararı verilemez) → reddet.
	if baslat {
		if err := sv.Saglikli(ctx, 15*time.Second); err != nil {
			fmt.Fprintf(hata, "✗ the panel is unhealthy right now — rotation REFUSED (secrets untouched): %v\n", err)
			return 1
		}
	}
	s, err := r.Yenile(ctx, hedefler)
	if err != nil {
		fmt.Fprintf(hata, "✗ secret rotation failed — OLD secrets still in place: %v\n", err)
		return 1
	}
	for h, neden := range s.Atlanan {
		fmt.Fprintf(out, "  - %s skipped: %s\n", h, neden)
	}
	for _, u := range s.Uyarilar {
		fmt.Fprintf(hata, "  ! %s\n", u)
	}
	if len(s.Yenilenen) == 0 {
		fmt.Fprintln(out, "no secrets were rotated")
		return 0
	}
	if !baslat {
		fmt.Fprintf(out, "rotated: %s — panel NOT RESTARTED (the new secrets take effect at restart): systemctl restart girginospanel\n", strings.Join(s.Yenilenen, ", "))
		return 0
	}
	saglikHata := sv.YenidenBaslat(ctx)
	if saglikHata == nil {
		saglikHata = sv.Saglikli(ctx, sure)
	}
	if saglikHata != nil {
		fmt.Fprintf(hata, "✗ the panel did not come up healthy with the new secrets (%v) — rolling back to the OLD secrets\n", saglikHata)
		gerr := r.GeriDon(ctx, s)
		if err := sv.YenidenBaslat(ctx); err != nil && gerr == nil {
			gerr = err
		}
		if gerr == nil {
			gerr = sv.Saglikli(ctx, sure)
		}
		if gerr != nil {
			fmt.Fprintf(hata, "✗ CRITICAL: the panel is not healthy after the rollback: %v — journalctl -u girginospanel -n 60; then: girginospanel-server sir kurtar\n", gerr)
			return 3
		}
		fmt.Fprintln(hata, "  panel is up on the old secrets — rotation NOT applied")
		return 1
	}
	var ekBirim []string
	for _, ad := range s.Eklentiler {
		ekBirim = append(ekBirim, "girginospanel-eklenti-"+ad+".service")
	}
	for _, h := range s.Yenilenen {
		if h == sirrota.DB {
			// Panel DB'sine kendi bağlantısını açan uzun ömürlü ajanlar.
			ekBirim = append(ekBirim, "girginospanel-avizle.service", "girginospanel-avsurec.service")
		}
	}
	if len(s.Yenilenen) > 0 {
		// Telegram destek ajanı AYRI hizmettir: sırları (DB DSN'i, maske listesi) kendi LoadCredential
		// kopyasında tutar; yenilenen sırlarla yeniden açılmazsa DB bağlantısı kopar (try-restart:
		// çalışmıyorsa dokunulmaz).
		ekBirim = append(ekBirim, "girginospanel-destek-ajan.service")
	}
	if len(ekBirim) > 0 {
		sv.DeneYenidenBaslat(ctx, ekBirim...)
	}
	fmt.Fprintf(out, "✓ rotated: %s — panel ACTIVE + /healthz with the new secrets", strings.Join(s.Yenilenen, ", "))
	for _, h := range s.Yenilenen {
		if h == sirrota.JWT {
			fmt.Fprint(out, " (all sessions dropped: sign in again)")
		}
	}
	fmt.Fprintln(out)
	return 0
}

// sirDurum: sır düzeninin değer yazdırmayan özeti.
func sirDurum(r *sirrota.Rotator, out io.Writer) int {
	kod := 0
	b, st, err := sirdosya.Oku(r.EnvYolu, 1<<20)
	if err != nil {
		fmt.Fprintf(out, "env: COULD NOT READ (%v)\n", err)
		return 1
	}
	uid, gid, _ := sirdosya.Sahip(st)
	fmt.Fprintf(out, "env: %s  mod=%04o uid=%d gid=%d\n", r.EnvYolu, st.Mode().Perm(), uid, gid)
	if st.Mode().Perm()&0o077 != 0 || uid != 0 {
		fmt.Fprintln(out, "  CRITICAL: env must be root:root 0600")
		kod = 1
	}
	m := config.EnvAyristir(b)
	for _, k := range config.SirAdlari {
		d := "missing"
		if strings.TrimSpace(m[k]) != "" {
			d = "set"
		}
		fmt.Fprintf(out, "  %s: %s\n", k, d)
	}
	birim, err := os.ReadFile("/etc/systemd/system/girginospanel.service")
	switch {
	case err != nil:
		fmt.Fprintf(out, "unit: could not read (%v)\n", err)
	case birimKimlikli(string(birim)):
		fmt.Fprintln(out, "unit: LoadCredential (secrets do not enter the process environment)")
	default:
		fmt.Fprintln(out, "unit: OLD (EnvironmentFile — secrets are in the process environment) → run girginospanel-update")
		kod = 1
	}
	if r.BekleyenVar() {
		fmt.Fprintln(out, "WARNING: an interrupted rotation exists → girginospanel-server sir kurtar")
		kod = 1
	}
	return kod
}

// birimKimlikli: birim env'i kimlik olarak veriyor ve EnvironmentFile ile
// YÜKLEMİYOR mu? (yorum satırları yok sayılır)
func birimKimlikli(icerik string) bool {
	kimlik, envf := false, false
	for _, s := range strings.Split(icerik, "\n") {
		s = strings.TrimSpace(s)
		if s == "" || s[0] == '#' || s[0] == ';' {
			continue
		}
		if strings.HasPrefix(s, "LoadCredential=") && strings.Contains(s, config.KimlikAdi+":"+config.EnvYolu) {
			kimlik = true
		}
		if strings.HasPrefix(s, "EnvironmentFile=") && strings.Contains(s, config.EnvYolu) {
			envf = true
		}
	}
	return kimlik && !envf
}
