package tasima

import (
	"context"
	"fmt"
	"strings"
)

// Hesap — kaynak sunucuda bulunan tasinabilir bir site.
type Hesap struct {
	KaynakHesap string   `json:"kaynak_hesap"` // uzak panel kullanicisi (sistem kullanicisi)
	AlanAdi     string   `json:"alan_adi"`
	WebRoot     string   `json:"web_root"`
	PHPSurum    string   `json:"php_surum"`
	DBler       []string `json:"dbler"`
	BoyutMB     int64    `json:"boyut_mb"`
	Not         string   `json:"not"`
	// Sahiplik (Plesk/cPanel'de bu domainin sahibi). Reseller/müşteri taşımada
	// domain doğru sahibe atanır. Boşsa admin-sahipli sayılır.
	SahipLogin string `json:"sahip_login"` // domain sahibi panel client login
	SahipTip   string `json:"sahip_tip"`   // admin | reseller | client
	Reseller   string `json:"reseller"`    // üst reseller login (client ise; reseller-sahipli ise kendi login'i)
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

// KaynakSahip — kaynaktaki bir reseller veya müşteri (client). Reseller/sahip
// taşımada GVM'de karşılık oluşturulur. Limitler MB/adet; 0 = sınırsız.
type KaynakSahip struct {
	Login     string `json:"login"`
	Tip       string `json:"tip"` // reseller | client
	Ad        string `json:"ad"`  // kişi/firma adı
	Eposta    string `json:"eposta"`
	Reseller  string `json:"reseller"` // üst reseller login (client ise)
	DiskMB    int64  `json:"disk_mb"`
	MaxDomain int    `json:"max_domain"`
	TrafikMB  int64  `json:"trafik_mb"`
}

// KesfetSahipler — kaynaktaki reseller + müşteri hesaplarını (iletişim + limit)
// keşfeder. Şimdilik yalnız Plesk (cPanel/DA reseller yapısı ayrı ele alınır).
func (k *Kaynak) KesfetSahipler(ctx context.Context) ([]KaynakSahip, error) {
	if k.Tip != "plesk" {
		return nil, nil
	}
	ctx, iptal := context.WithTimeout(ctx, kesifTimeout)
	defer iptal()
	cikti, err := k.Calistir(ctx, komutPleskSahipler)
	if err != nil {
		return nil, err
	}
	return ayristirSahipler(cikti), nil
}

// komutPleskSahipler — reseller + client'ları CONCAT_WS('|', ...) ile tek satırda
// döndürür: login|type|ad|eposta|ust_reseller|disk_bayt|max_dom|max_traffic_bayt.
const komutPleskSahipler = `plesk db -Ne "SELECT CONCAT_WS('|', c.login, c.type, COALESCE(NULLIF(c.pname,''), NULLIF(c.cname,''), c.login), COALESCE(c.email,''), COALESCE((SELECT p.login FROM clients p WHERE p.id=c.parent_id AND p.type='reseller'),''), COALESCE((SELECT l.value FROM Limits l WHERE l.id=c.limits_id AND l.limit_name='disk_space'),'-1'), COALESCE((SELECT l.value FROM Limits l WHERE l.id=c.limits_id AND l.limit_name='max_dom'),'-1'), COALESCE((SELECT l.value FROM Limits l WHERE l.id=c.limits_id AND l.limit_name='max_traffic'),'-1')) FROM clients c WHERE c.type IN ('reseller','client')" 2>/dev/null`

func ayristirSahipler(cikti string) []KaynakSahip {
	var out []KaynakSahip
	for _, satir := range strings.Split(cikti, "\n") {
		satir = strings.TrimSpace(satir)
		if satir == "" {
			continue
		}
		p := strings.Split(satir, "|")
		if len(p) < 8 {
			continue
		}
		login := strings.TrimSpace(p[0])
		tip := strings.TrimSpace(p[1])
		if !reHesap.MatchString(login) || (tip != "reseller" && tip != "client") {
			continue // uzak veri düşmandır: allowlist
		}
		s := KaynakSahip{
			Login: login, Tip: tip,
			Ad:       kisaltAlan(strings.TrimSpace(p[2]), 128),
			Eposta:   temizEposta(strings.TrimSpace(p[3])),
			Reseller: strings.TrimSpace(p[4]),
		}
		if !reHesap.MatchString(s.Reseller) {
			s.Reseller = ""
		}
		s.DiskMB = pleskBaytMB(p[5])
		s.MaxDomain = pleskLimitInt(p[6])
		s.TrafikMB = pleskBaytMB(p[7])
		out = append(out, s)
	}
	return out
}

// pleskBaytMB — Plesk bayt limitini MB'ye çevirir; -1/boş/negatif = 0 (sınırsız).
func pleskBaytMB(s string) int64 {
	var v int64
	fmt.Sscanf(strings.TrimSpace(s), "%d", &v)
	if v < 0 {
		return 0
	}
	return v / (1024 * 1024)
}

func pleskLimitInt(s string) int {
	var v int
	fmt.Sscanf(strings.TrimSpace(s), "%d", &v)
	if v < 0 {
		return 0
	}
	return v
}

func kisaltAlan(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// temizEposta — basit doğrulama; şüpheli/geçersizse boş döner (uzak veri).
func temizEposta(s string) string {
	if s == "" || len(s) > 190 || !strings.Contains(s, "@") || strings.ContainsAny(s, " \r\n\t'\"`;|") {
		return ""
	}
	return s
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
  echo "###OWNER:$(plesk db -Ne "SELECT CONCAT_WS('|', cl.login, cl.type, COALESCE(p.login,''), COALESCE(p.type,'')) FROM domains dom JOIN clients cl ON cl.id=dom.cl_id LEFT JOIN clients p ON p.id=cl.parent_id WHERE dom.name='$d' LIMIT 1" 2>/dev/null)"
  echo "###DB:$(plesk db -Ne "SELECT db.name FROM data_bases db JOIN domains dom ON db.dom_id=dom.id WHERE dom.name='$d'" 2>/dev/null | tr '\n' ',')"
  ana=1
  for sub in $d $(plesk db -Ne "SELECT dom.name FROM domains dom WHERE dom.webspace_id=(SELECT id FROM domains WHERE name='$d') AND dom.name<>'$d'" 2>/dev/null); do
    dr=$(plesk db -Ne "SELECT CONCAT(h.www_root) FROM domains dom JOIN hosting h ON h.dom_id=dom.id WHERE dom.name='$sub' LIMIT 1" 2>/dev/null)
    if [ -z "$dr" ]; then if [ "$sub" = "$d" ]; then dr="/var/www/vhosts/$d/httpdocs"; else dr=""; fi; fi
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
	var sahipLogin, sahipTip, reseller string // ###OWNER'dan (abonelik başına)

	for _, satir := range strings.Split(cikti, "\n") {
		satir = strings.TrimSpace(satir)
		switch {
		case strings.HasPrefix(satir, "###USER:"):
			u := strings.TrimPrefix(satir, "###USER:")
			aktifHesap, dbler = "", ""
			sahipLogin, sahipTip, reseller = "", "", ""
			if reHesap.MatchString(u) {
				aktifHesap = u
			}
		case strings.HasPrefix(satir, "###OWNER:"):
			// login|type|parent_login|parent_type
			p := strings.Split(strings.TrimPrefix(satir, "###OWNER:"), "|")
			if len(p) >= 2 {
				lg, tp := strings.TrimSpace(p[0]), strings.TrimSpace(p[1])
				if reHesap.MatchString(lg) {
					switch tp {
					case "reseller":
						sahipLogin, sahipTip, reseller = lg, "reseller", lg // reseller-sahipli
					case "client":
						sahipLogin, sahipTip = lg, "client"
						if len(p) >= 4 && strings.TrimSpace(p[3]) == "reseller" && reHesap.MatchString(strings.TrimSpace(p[2])) {
							reseller = strings.TrimSpace(p[2]) // müşterinin üst reseller'ı
						}
					default: // admin veya diğer → admin-sahipli
						sahipLogin, sahipTip, reseller = "", "admin", ""
					}
				}
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
				SahipLogin:  sahipLogin,
				SahipTip:    sahipTip,
				Reseller:    reseller,
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
