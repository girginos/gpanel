package tasima

import (
	"context"
	"fmt"
	"strings"
)

// Hesap — kaynak sunucuda bulunan tasinabilir bir site.
type Hesap struct {
	KaynakHesap string   `json:"kaynak_hesap"` // uzak panel kullanicisi
	AlanAdi     string   `json:"alan_adi"`
	WebRoot     string   `json:"web_root"`
	PHPSurum    string   `json:"php_surum"`
	DBler       []string `json:"dbler"`
	BoyutMB     int64    `json:"boyut_mb"`
	Not         string   `json:"not"`
}

// PanelTespit — uzak sunucuda hangi panelin kurulu oldugunu bulur.
func (k *Kaynak) PanelTespit(ctx context.Context) (string, error) {
	ctx, iptal := context.WithTimeout(ctx, kesifTimeout)
	defer iptal()
	cikti, err := k.Calistir(ctx,
		"if [ -d /usr/local/cpanel ]; then echo cpanel; "+
			"elif [ -d /usr/local/psa ] || command -v plesk >/dev/null 2>&1; then echo plesk; "+
			"elif [ -d /usr/local/directadmin ]; then echo directadmin; "+
			"else echo bilinmiyor; fi")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(cikti), nil
}

// Kesfet — kaynak paneldeki tum hesaplari/siteleri listeler.
func (k *Kaynak) Kesfet(ctx context.Context) ([]Hesap, error) {
	ctx, iptal := context.WithTimeout(ctx, kesifTimeout*3)
	defer iptal()
	switch k.Tip {
	case "cpanel":
		return k.ayristir(k.Calistir(ctx, komutCpanel))
	case "plesk":
		return k.ayristir(k.Calistir(ctx, komutPlesk))
	case "directadmin":
		return k.ayristir(k.Calistir(ctx, komutDirectAdmin))
	}
	return nil, fmt.Errorf("desteklenmeyen panel tipi")
}

func (k *Kaynak) ayristir(cikti string, err error) ([]Hesap, error) {
	if err != nil {
		return nil, err
	}
	return ayristirBlok(cikti), nil
}

// ---------------------------------------------------------------------------
// Uzak kesif komutlari
// ---------------------------------------------------------------------------
//
// 🔴 Her DOMAIN icin AYRI docroot ve PHP surumu uretilir. Onceki surumde tum
// domainler hesabin ana docroot'una isaret ediyordu; ek (addon) domainler ana
// sitenin dosyalarini aliyor ve ayni veritabanlari birden cok kez tasiniyordu.
//
// Cikti bicimi (satir tabanli):
//   ###USER:<panel kullanicisi>
//   ###DB:<db1,db2,...>          (hesap geneli — YALNIZ ana domaine atanir)
//   ###DOM:<alan adi>|<docroot>|<php surumu>|<boyut MB>|<ana|ek>

const komutCpanel = `for f in /var/cpanel/users/*; do
  [ -f "$f" ] || continue
  u=$(basename "$f")
  case "$u" in system|root|*.cache|*.lock) continue ;; esac
  echo "###USER:$u"
  echo "###DB:$(mysql -N -B -e "SHOW DATABASES" 2>/dev/null | grep -E "^${u}_" | tr '\n' ',')"
  ana=1
  for d in $(grep -E '^DNS[0-9]*=' "$f" 2>/dev/null | sed 's/^DNS[0-9]*=//'); do
    [ -n "$d" ] || continue
    ud="/var/cpanel/userdata/$u/$d"
    dr=$(grep -E '^documentroot:' "$ud" 2>/dev/null | head -1 | sed 's/^documentroot: *//')
    pv=$(grep -E '^phpversion:' "$ud" 2>/dev/null | head -1 | sed 's/^phpversion: *//')
    [ -n "$dr" ] || dr="/home/$u/public_html"
    [ -n "$pv" ] || pv=$(grep -E '^phpversion=' "$f" 2>/dev/null | head -1 | sed 's/^phpversion=//')
    sz=$(du -sm "$dr" 2>/dev/null | cut -f1)
    if [ "$ana" = "1" ]; then tur=ana; ana=0; else tur=ek; fi
    echo "###DOM:$d|$dr|$pv|$sz|$tur"
  done
done`

const komutPlesk = `for d in $(plesk bin subscription --list 2>/dev/null); do
  su=$(plesk db -Ne "SELECT s.login FROM domains dom JOIN hosting h ON h.dom_id=dom.id JOIN sys_users s ON s.id=h.sys_user_id WHERE dom.name='$d' LIMIT 1" 2>/dev/null)
  [ -n "$su" ] || su="$d"
  echo "###USER:$su"
  echo "###DB:$(plesk db -Ne "SELECT db.name FROM data_bases db JOIN domains dom ON db.dom_id=dom.id WHERE dom.name='$d'" 2>/dev/null | tr '\n' ',')"
  ana=1
  for sub in $d $(plesk db -Ne "SELECT dom.name FROM domains dom WHERE dom.webspace_id=(SELECT id FROM domains WHERE name='$d') AND dom.name<>'$d'" 2>/dev/null); do
    dr=$(plesk db -Ne "SELECT CONCAT(h.www_root) FROM domains dom JOIN hosting h ON h.dom_id=dom.id WHERE dom.name='$sub' LIMIT 1" 2>/dev/null)
    [ -n "$dr" ] || dr="/var/www/vhosts/$d/httpdocs"
    pv=$(plesk db -Ne "SELECT h.php_handler_id FROM domains dom JOIN hosting h ON h.dom_id=dom.id WHERE dom.name='$sub' LIMIT 1" 2>/dev/null | grep -oE 'php[0-9]+' | head -1 | sed 's/php//')
    sz=$(du -sm "$dr" 2>/dev/null | cut -f1)
    if [ "$ana" = "1" ]; then tur=ana; ana=0; else tur=ek; fi
    echo "###DOM:$sub|$dr|$pv|$sz|$tur"
  done
done`

// DirectAdmin: php1_select bir INDEKS'tir (1/2/3), surum degil — custombuild
// options.conf'taki phpN_release ile eslenir.
const komutDirectAdmin = `oc=/usr/local/directadmin/custombuild/options.conf
for ud in /usr/local/directadmin/data/users/*; do
  [ -d "$ud" ] || continue
  u=$(basename "$ud")
  case "$u" in admin) continue ;; esac
  echo "###USER:$u"
  echo "###DB:$(mysql -N -B -e "SHOW DATABASES" 2>/dev/null | grep -E "^${u}_" | tr '\n' ',')"
  ana=1
  while read -r d; do
    [ -n "$d" ] || continue
    dr="/home/$u/domains/$d/public_html"
    idx=$(grep -h -E '^php1_select=' "$ud/domains/$d.conf" 2>/dev/null | head -1 | sed 's/^php1_select=//')
    [ -n "$idx" ] || idx=1
    pv=$(grep -E "^php${idx}_release=" "$oc" 2>/dev/null | head -1 | sed "s/^php${idx}_release=//")
    sz=$(du -sm "$dr" 2>/dev/null | cut -f1)
    if [ "$ana" = "1" ]; then tur=ana; ana=0; else tur=ek; fi
    echo "###DOM:$d|$dr|$pv|$sz|$tur"
  done < "$ud/domains.list"
done`

// ---------------------------------------------------------------------------
// Ayristirici
// ---------------------------------------------------------------------------
//
// UZAKTAN GELEN VERI DUSMANDIR: hesap / alan adi / DB adi / yol allowlist'ten
// gecirilir; gecmeyen SESSIZCE ATILIR.

func ayristirBlok(cikti string) []Hesap {
	var sonuc []Hesap
	var aktifHesap, dbler string

	for _, satir := range strings.Split(cikti, "\n") {
		satir = strings.TrimSpace(satir)
		switch {
		case strings.HasPrefix(satir, "###USER:"):
			u := strings.TrimPrefix(satir, "###USER:")
			aktifHesap, dbler = "", ""
			if reHesap.MatchString(u) {
				aktifHesap = u
			}
		case strings.HasPrefix(satir, "###DB:"):
			dbler = strings.TrimPrefix(satir, "###DB:")
		case strings.HasPrefix(satir, "###DOM:"):
			if aktifHesap == "" {
				continue
			}
			p := strings.Split(strings.TrimPrefix(satir, "###DOM:"), "|")
			if len(p) < 5 {
				continue
			}
			d := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(p[0], ".")))
			if d == "" || !reAlanAdi.MatchString(d) || !strings.Contains(d, ".") {
				continue
			}
			kok := strings.TrimSpace(p[1])
			if !gecerliUzakYol(kok) {
				continue
			}
			var boyut int64
			fmt.Sscanf(strings.TrimSpace(p[3]), "%d", &boyut)
			anaMi := strings.TrimSpace(p[4]) == "ana"

			h := Hesap{
				KaynakHesap: aktifHesap,
				AlanAdi:     d,
				WebRoot:     kok,
				PHPSurum:    normalizePHP(p[2]),
				BoyutMB:     boyut,
			}
			// 🔴 Veritabanlari YALNIZ ana domaine atanir. Hesap geneli liste
			// ek domainlere de verilirse ayni DB birden cok kez tasinir.
			if anaMi {
				for _, db := range strings.Split(dbler, ",") {
					db = strings.TrimSpace(db)
					if db != "" && reDBAd.MatchString(db) && !sistemDB(db) {
						h.DBler = append(h.DBler, db)
					}
				}
			} else {
				h.Not = "ek domain — veritabanı ana alan adıyla taşınır"
			}
			sonuc = append(sonuc, h)
		}
	}
	return sonuc
}

// gecerliUzakYol — uzak docroot: mutlak, gezinme yok, kabuk metakarakteri yok.
func gecerliUzakYol(y string) bool {
	if y == "" || !strings.HasPrefix(y, "/") || len(y) > 512 {
		return false
	}
	if strings.Contains(y, "..") {
		return false
	}
	return !strings.ContainsAny(y, "\x00\r\n'\"`$;|&<>*?")
}

var sistemDBler = map[string]bool{
	"information_schema": true, "performance_schema": true, "mysql": true,
	"sys": true, "test": true, "psa": true, "horde": true, "roundcube": true,
	"phpmyadmin": true, "leechprotect": true, "eximstats": true, "modsec": true,
	"cphulkd": true, "whmxfer": true,
}

func sistemDB(s string) bool { return sistemDBler[strings.ToLower(s)] }

// normalizePHP — "ea-php81", "php81", "8.1.30", "8" gibi degerleri normalize eder.
// 🔴 Tek haneli girdi ("8") ANA SURUM olarak korunur ("8"); kuruluPHPSec bunu
// ayni ana surum icinde eslestirir. Eskiden "8" oldugu gibi kalip surum
// karsilastirmasinda 8.0'in ALTINA (7.4) dusuyordu.
func normalizePHP(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	s = strings.TrimPrefix(s, "ea-")
	s = strings.TrimPrefix(s, "alt-")
	s = strings.TrimPrefix(s, "php")
	s = strings.TrimSpace(strings.Trim(s, "-_"))
	if s == "" {
		return ""
	}
	// "81" -> "8.1"
	if !strings.Contains(s, ".") {
		if len(s) >= 2 {
			return s[:1] + "." + s[1:]
		}
		return s // tek hane: ana surum (or. "8")
	}
	p := strings.Split(s, ".")
	if len(p) >= 2 {
		return p[0] + "." + p[1]
	}
	return s
}
