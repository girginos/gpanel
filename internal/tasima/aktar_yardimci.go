package tasima

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"girginospanel/internal/phpsurum"
)

// ---------------------------------------------------------------------------
// PHP surumu
// ---------------------------------------------------------------------------

// kuruluPHPSec — kaynak surumu hedefte kurulu degilse en uygun kurulu surume
// duser. 🔴 Ayni ANA surum icinde once YUKARI bakilir: "8" veya "8.1" istegi
// 7.4'e dusurulurse PHP 8 kodu patlar.
func kuruluPHPSec(istenen string) string {
	var kurulu []string
	for _, v := range phpsurum.TumSurumler() {
		if v.Yuklu {
			kurulu = append(kurulu, v.Surum)
		}
	}
	if len(kurulu) == 0 || istenen == "" {
		return istenen
	}
	for _, v := range kurulu {
		if v == istenen {
			return istenen
		}
	}
	ia, ib := phpParcala(istenen)
	// 1) Ayni ana surum — once >= istenen, sonra en yuksek ayni ana surum
	var ayniUst, ayniAlt string
	for _, v := range kurulu {
		va, vb := phpParcala(v)
		if va != ia {
			continue
		}
		if vb >= ib {
			if ayniUst == "" {
				ayniUst = v
			} else if _, ub := phpParcala(ayniUst); vb < ub {
				ayniUst = v
			}
		} else if ayniAlt == "" {
			ayniAlt = v
		} else if _, ab := phpParcala(ayniAlt); vb > ab {
			ayniAlt = v
		}
	}
	if ayniUst != "" {
		return ayniUst
	}
	if ayniAlt != "" {
		return ayniAlt
	}
	// 2) Farkli ana surum — en yakin buyuk, yoksa en yakin kucuk
	var ust, alt string
	for _, v := range kurulu {
		va, vb := phpParcala(v)
		if va > ia || (va == ia && vb > ib) {
			if ust == "" {
				ust = v
			} else if ua, ub := phpParcala(ust); va < ua || (va == ua && vb < ub) {
				ust = v
			}
		} else {
			if alt == "" {
				alt = v
			} else if aa, ab := phpParcala(alt); va > aa || (va == aa && vb > ab) {
				alt = v
			}
		}
	}
	if ust != "" {
		return ust
	}
	if alt != "" {
		return alt
	}
	return istenen
}

func phpParcala(s string) (int, int) {
	var a, b int
	_, _ = fmt.Sscanf(s, "%d.%d", &a, &b)
	return a, b
}

// ---------------------------------------------------------------------------
// Yardimcilar
// ---------------------------------------------------------------------------

const tasimaYedekKok = "/var/lib/girginospanel/tasima-yedek"

func yedekYaz(webRoot, rel string, ham []byte) error {
	dizin := filepath.Join(tasimaYedekKok, filepath.Base(filepath.Dir(webRoot)))
	if err := os.MkdirAll(dizin, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dizin, strings.ReplaceAll(rel, "/", "_")), ham, 0o600)
}

func uidGid(sk string) (int, int, error) {
	u, err := user.Lookup(sk)
	if err != nil {
		return 0, 0, err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return uid, gid, nil
}

func dizinBoyut(yol string) (int64, error) {
	var t int64
	err := filepath.Walk(yol, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && fi != nil && fi.Mode().IsRegular() {
			t += fi.Size()
		}
		return nil
	})
	return t, err
}

func (h *Handlers) sunucuIP() string {
	var ip string
	_ = h.DB.QueryRow(`SELECT ipv4 FROM domains WHERE ipv4 IS NOT NULL AND ipv4<>'' LIMIT 1`).Scan(&ip)
	if ip != "" {
		return ip
	}
	out, err := exec.Command("hostname", "-I").Output()
	if err == nil {
		if f := strings.Fields(string(out)); len(f) > 0 {
			return f[0]
		}
	}
	return "127.0.0.1"
}

// docrootDisiBagimlilik — belge kokundeki PHP giris dosyalarinda "../<dizin>/"
// referanslarini bulur (Laravel/Symfony deseni). Donen adlar TEK SEVIYE dizin
// adidir; ".."/slash iceren veya supheli degerler elenir.
func docrootDisiBagimlilik(webRoot string) []string {
	re := regexp.MustCompile(`\.\./([A-Za-z0-9_][A-Za-z0-9._-]{0,63})/`)
	gorulen := map[string]bool{}
	var out []string
	for _, ad := range []string{"index.php", "server.php", "bootstrap.php"} {
		b, err := os.ReadFile(filepath.Join(webRoot, ad))
		if err != nil || len(b) > 1<<20 {
			continue
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			d := m[1]
			if d == "" || d == "." || d == ".." || gorulen[d] {
				continue
			}
			gorulen[d] = true
			out = append(out, d)
		}
	}
	return out
}

// laravelSonrasiDuzelt — tasinan Laravel uygulamasinda kaynak sunucuya ait kalintilari
// temizler: APP_URL hedef domaine cekilir (aksi halde link/asset yanlis uretilir),
// onbelleklenmis config/route (eski YOLLARI icerir) silinir, .env 0600 yapilir
// (kaynaktan 0644 gelir — APP_KEY/DB parolasi dunyaya okunur kalirdi).
func laravelSonrasiDuzelt(dir, alanAdi, sk string, log func(string, ...any)) {
	if _, err := os.Stat(filepath.Join(dir, "artisan")); err != nil {
		return // Laravel degil
	}
	envYol := filepath.Join(dir, ".env")
	if b, err := os.ReadFile(envYol); err == nil {
		yeni := regexp.MustCompile(`(?m)^APP_URL=.*$`).ReplaceAllString(string(b), "APP_URL=https://"+alanAdi)
		if yeni != string(b) {
			if st, e := os.Stat(envYol); e == nil {
				_ = atomikYaz(envYol, []byte(yeni), st)
			}
			log("laravel: APP_URL → https://%s", alanAdi)
		}
		_ = os.Chmod(envYol, 0o600)
	}
	for _, c := range []string{"config.php", "services.php", "packages.php", "routes-v7.php"} {
		_ = os.Remove(filepath.Join(dir, "bootstrap", "cache", c))
	}
	_ = exec.Command("chown", "-R", sk+":"+sk, filepath.Join(dir, "storage"), filepath.Join(dir, "bootstrap", "cache")).Run()
	log("laravel: onbellek temizlendi, .env 0600, storage sahipligi ayarlandi")
}
