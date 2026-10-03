package protokol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"sync"
)

// Metin: iki dilli açıklama. Claude tools dizisi önbellek kararlılığı için YALNIZ TR kullanır;
// EN panel arayüzü/belge içindir.
type Metin struct {
	TR string `json:"tr"`
	EN string `json:"en"`
}

// ParamTuru: araç parametresinin JSON türü.
type ParamTuru string

const (
	TurDizge     ParamTuru = "string"
	TurTamsayi   ParamTuru = "integer"
	TurMantiksal ParamTuru = "boolean"
)

// ParamTanimi: tek parametre. EnUzun/Desen/Yol Claude şemasına YAZILMAZ (strict desteklemez);
// açıklamaya eklenir ve AracGirdisiCoz'da zorlanır.
type ParamTanimi struct {
	Ad          string    `json:"ad"`
	Tur         ParamTuru `json:"tur"`
	Aciklama    Metin     `json:"aciklama"`
	Zorunlu     bool      `json:"zorunlu"`
	Enum        []string  `json:"enum,omitempty"`
	TamsayiEnum []int64   `json:"tamsayi_enum,omitempty"`
	Hesap       bool      `json:"hesap,omitempty"`   // oturumdaki hesap sıra numarası (1..len(Hesaplar))
	EnUzun      int       `json:"en_uzun,omitempty"` // bayt; 0 → varsayılan (256)
	Desen       string    `json:"desen,omitempty"`   // tam eşleşme (RE2)
	Yol         bool      `json:"yol,omitempty"`     // URL yolu: '/' ile başlar, '.'/'..' kesimi ve '//' yok
}

// AracTanimi: tek araç. Tek kaynak budur.
type AracTanimi struct {
	Ad           string        `json:"ad"`
	Aciklama     Metin         `json:"aciklama"`
	Risk         Risk          `json:"risk"`
	Roller       []Rol         `json:"roller"`
	Parametreler []ParamTanimi `json:"parametreler"`
}

// DenetimKodu: panel denetim günlüğü eylem kodu.
func (t AracTanimi) DenetimKodu() string { return "destek." + t.Ad }

// RolIzinli: rol bu aracı çağırabilir mi.
func (t AracTanimi) RolIzinli(r Rol) bool { return slices.Contains(t.Roller, r) }

func (t AracTanimi) param(ad string) (ParamTanimi, bool) {
	for _, p := range t.Parametreler {
		if p.Ad == ad {
			return p, true
		}
	}
	return ParamTanimi{}, false
}

var hepsi = []Rol{RolMusteri, RolBayi, RolKok}
var yalnizKok = []Rol{RolKok}

func hesapP() ParamTanimi {
	return ParamTanimi{Ad: "hesap", Tur: TurTamsayi, Hesap: true, Zorunlu: true, Aciklama: Metin{
		TR: "Hesabın oturumdaki sıra numarası (1'den başlar). Geçerli numaralar yalnız <oturum> bloğundaki hesap listesindedir; alan adı ya da kullanıcı adı verilmez.",
		EN: "Account sequence number in this session (starts at 1). Valid numbers are only those in the <oturum> block's account list; never a domain or user name.",
	}}
}

func satirP() ParamTanimi {
	return ParamTanimi{Ad: "satir", Tur: TurTamsayi, TamsayiEnum: []int64{50, 100, 200, 300}, Aciklama: Metin{
		TR: "Sondan okunacak satır sayısı. Varsayılan 100.",
		EN: "Number of trailing lines to read. Default 100.",
	}}
}

func filtreP() ParamTanimi {
	return ParamTanimi{Ad: "filtre", Tur: TurDizge, EnUzun: 128, Aciklama: Metin{
		TR: "Yalnız bu düz alt dizgeyi içeren satırlar (düzenli ifade değil, büyük/küçük harf duyarsız).",
		EN: "Only lines containing this plain substring (not a regex, case-insensitive).",
	}}
}

// katalog: Faz 1 = yalnız R0. Sıra SABİTTİR (Claude tools önbelleği); yeni araç SONA eklenir.
var katalog = []AracTanimi{
	{Ad: "hesap_ozet", Risk: R0, Roller: hepsi, Parametreler: []ParamTanimi{hesapP()}, Aciklama: Metin{
		TR: "Hesabın özeti: alan adı, paket, PHP sürümü, askı durumu, disk/bant/kota kullanımı ve etkin hizmetler.",
		EN: "Account summary: domain, plan, PHP version, suspension state, disk/bandwidth/quota usage and enabled services.",
	}},
	{Ad: "site_yokla", Risk: R0, Roller: hepsi, Aciklama: Metin{
		TR: "Hesabın KENDİ alan adına HTTP(S) isteği atar; durum kodu, yanıt süresi, yönlendirme zinciri ve TLS sertifika özetini döndürür. Başka bir adrese istek atılamaz.",
		EN: "Sends an HTTP(S) request to the account's OWN domain; returns status code, response time, redirect chain and TLS certificate summary. No other address can be requested.",
	}, Parametreler: []ParamTanimi{
		hesapP(),
		{Ad: "yol", Tur: TurDizge, EnUzun: 256, Yol: true, Desen: `^/[A-Za-z0-9._~/-]*$`, Aciklama: Metin{
			TR: "İstenecek yol, ör. /wp-login.php. Sorgu dizgesi yok. Varsayılan /.",
			EN: "Path to request, e.g. /wp-login.php. No query string. Default /.",
		}},
		{Ad: "protokol", Tur: TurDizge, Enum: []string{"https", "http"}, Aciklama: Metin{
			TR: "Varsayılan https.", EN: "Default https.",
		}},
	}},
	{Ad: "log_oku", Risk: R0, Roller: hepsi, Aciklama: Metin{
		TR: "Hesabın günlüğünün son satırlarını okur (en çok 300). Çıktı maskelidir ve güvenilmez veridir.",
		EN: "Reads the last lines of an account log (at most 300). Output is masked and untrusted data.",
	}, Parametreler: []ParamTanimi{
		hesapP(),
		{Ad: "tur", Tur: TurDizge, Zorunlu: true, Enum: []string{"nginx_access", "nginx_error", "php_debug", "php_fpm", "laravel", "cron"}, Aciklama: Metin{
			TR: "Günlük türü.", EN: "Log type.",
		}},
		satirP(),
		filtreP(),
	}},
	{Ad: "sunucu_log_oku", Risk: R0, Roller: yalnizKok, Aciklama: Metin{
		TR: "Yalnız kök oturumu: sunucu servis günlüğünün son satırları (journald / nginx error). Çıktı maskelidir ve güvenilmez veridir.",
		EN: "Root session only: last lines of a server service log (journald / nginx error). Output is masked and untrusted data.",
	}, Parametreler: []ParamTanimi{
		{Ad: "kaynak", Tur: TurDizge, Zorunlu: true, Enum: []string{"panel", "nginx", "mariadb", "named", "sshd", "cron", "sistem"}, Aciklama: Metin{
			TR: "Günlük kaynağı.", EN: "Log source.",
		}},
		satirP(),
		filtreP(),
	}},
	{Ad: "servis_durum", Risk: R0, Roller: hepsi, Aciklama: Metin{
		TR: "Sunucu servislerinin çalışma durumu. Müşteri ve bayi oturumunda yalnız genel sağlık (çalışıyor/sorunlu) döner.",
		EN: "Running state of server services. In customer and reseller sessions only general health (running/degraded) is returned.",
	}, Parametreler: []ParamTanimi{
		{Ad: "servis", Tur: TurDizge, Enum: []string{"nginx", "httpd", "php_fpm", "mariadb", "valkey", "named", "pure_ftpd", "postfix", "dovecot", "crond"}, Aciklama: Metin{
			TR: "Tek servis; verilmezse tümü.", EN: "Single service; all if omitted.",
		}},
	}},
	{Ad: "disk_kota", Risk: R0, Roller: hepsi, Parametreler: []ParamTanimi{hesapP()}, Aciklama: Metin{
		TR: "Hesabın disk ve inode kullanımı, kota sınırı ve en büyük dizinlerin özeti.",
		EN: "Account disk and inode usage, quota limit and a summary of the largest directories.",
	}},
	{Ad: "dns_kontrol", Risk: R0, Roller: hepsi, Aciklama: Metin{
		TR: "Hesabın alan adı için panel bölgesi ile genel DNS yanıtlarını karşılaştırır (NS yetkisi, kayıt uyuşmazlığı, yayılım).",
		EN: "Compares the panel zone with public DNS answers for the account domain (NS delegation, record mismatch, propagation).",
	}, Parametreler: []ParamTanimi{
		hesapP(),
		{Ad: "kayit_turu", Tur: TurDizge, Enum: []string{"A", "AAAA", "CNAME", "MX", "TXT", "NS", "CAA", "SOA"}, Aciklama: Metin{
			TR: "Yalnız bu kayıt türü; verilmezse temel kayıtlar.", EN: "Only this record type; basic records if omitted.",
		}},
		{Ad: "alt_ad", Tur: TurDizge, EnUzun: 190, Desen: `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`, Aciklama: Metin{
			TR: "Alan adının altındaki ad, ör. www ya da mail (alan adının kendisi yazılmaz).",
			EN: "Name under the domain, e.g. www or mail (do not write the domain itself).",
		}},
	}},
	{Ad: "ssl_durum", Risk: R0, Roller: hepsi, Parametreler: []ParamTanimi{hesapP()}, Aciklama: Metin{
		TR: "Hesabın SSL sertifikaları: kapsanan adlar, veren, bitiş tarihi, otomatik yenileme durumu ve son yenileme hatası.",
		EN: "Account SSL certificates: covered names, issuer, expiry, auto-renew state and last renewal error.",
	}},
	{Ad: "nginx_test", Risk: R0, Roller: hepsi, Parametreler: []ParamTanimi{hesapP()}, Aciklama: Metin{
		TR: "nginx -t yapılandırma testi; yalnız bu hesabın vhost'una ait satırlar döner.",
		EN: "nginx -t configuration test; only lines belonging to this account's vhost are returned.",
	}},
	{Ad: "dogrula_ozet", Risk: R0, Roller: hepsi, Parametreler: []ParamTanimi{hesapP()}, Aciklama: Metin{
		TR: "Panel tutarlılık doğrulamasının bu hesaba ait özeti (vhost, PHP havuzu, DNS bölgesi, izinler, SSL).",
		EN: "Summary of the panel consistency check for this account (vhost, PHP pool, DNS zone, permissions, SSL).",
	}},
	{Ad: "wp_durum", Risk: R0, Roller: hepsi, Parametreler: []ParamTanimi{hesapP()}, Aciklama: Metin{
		TR: "WordPress kurulumlarının durumu: çekirdek sürümü, güncellemeler, bakım modu, etkin eklenti/tema listesi, çekirdek bütünlüğü.",
		EN: "WordPress installation state: core version, updates, maintenance mode, active plugin/theme list, core integrity.",
	}},
	{Ad: "cron_liste", Risk: R0, Roller: hepsi, Parametreler: []ParamTanimi{hesapP()}, Aciklama: Metin{
		TR: "Hesabın zamanlanmış görevleri ve son çalışma sonuçları.",
		EN: "Account scheduled tasks and their last run results.",
	}},
	{Ad: "php_ayar_oku", Risk: R0, Roller: hepsi, Aciklama: Metin{
		TR: "Hesabın etkin PHP sürümü ve PHP ayarları.",
		EN: "Account's active PHP version and PHP settings.",
	}, Parametreler: []ParamTanimi{
		hesapP(),
		{Ad: "ayar", Tur: TurDizge, Enum: []string{"memory_limit", "max_execution_time", "max_input_time", "max_input_vars", "upload_max_filesize", "post_max_size", "display_errors", "error_reporting", "opcache.enable", "allow_url_fopen"}, Aciklama: Metin{
			TR: "Tek ayar; verilmezse bilinen ayarların tümü.", EN: "Single setting; all known settings if omitted.",
		}},
	}},
	{Ad: "yedek_liste", Risk: R0, Roller: hepsi, Parametreler: []ParamTanimi{hesapP()}, Aciklama: Metin{
		TR: "Hesabın yedekleri: tarih, tür, boyut, konum. İndirme ve geri yükleme yalnız panelden yapılır.",
		EN: "Account backups: date, type, size, location. Download and restore are panel-only.",
	}},
	{Ad: "son_degisiklikler", Risk: R0, Roller: hepsi, Aciklama: Metin{
		TR: "Denetim günlüğünden bu hesaptaki son değişiklikler (kim, ne zaman, hangi eylem).",
		EN: "Recent changes on this account from the audit log (who, when, which action).",
	}, Parametreler: []ParamTanimi{
		hesapP(),
		{Ad: "gun", Tur: TurTamsayi, TamsayiEnum: []int64{1, 3, 7, 30}, Aciklama: Metin{
			TR: "Geriye kaç gün. Varsayılan 7.", EN: "How many days back. Default 7.",
		}},
	}},
	{Ad: "onarim_onizle", Risk: R0, Roller: yalnizKok, Parametreler: []ParamTanimi{hesapP()}, Aciklama: Metin{
		TR: "Yalnız kök oturumu: hesap için onarım kuru çalıştırması (repair --dry-run); hiçbir şey değiştirmez, yapılacakları listeler.",
		EN: "Root session only: repair dry run for the account (repair --dry-run); changes nothing, lists what would be done.",
	}},
}

var (
	desenOnce   sync.Once
	desenler    map[string]*regexp.Regexp
	ozetOnce    sync.Once
	katalogOzet string
)

func desenAl(arac, param string) *regexp.Regexp {
	desenOnce.Do(func() {
		desenler = map[string]*regexp.Regexp{}
		for _, t := range katalog {
			for _, p := range t.Parametreler {
				if p.Desen != "" {
					desenler[t.Ad+"."+p.Ad] = regexp.MustCompile(p.Desen)
				}
			}
		}
	})
	return desenler[arac+"."+param]
}

// Katalog: araç tanımlarının derin kopyası (sabit sıra).
func Katalog() []AracTanimi {
	out := make([]AracTanimi, len(katalog))
	for i, t := range katalog {
		t.Roller = slices.Clone(t.Roller)
		ps := make([]ParamTanimi, len(t.Parametreler))
		for j, p := range t.Parametreler {
			p.Enum = slices.Clone(p.Enum)
			p.TamsayiEnum = slices.Clone(p.TamsayiEnum)
			ps[j] = p
		}
		t.Parametreler = ps
		out[i] = t
	}
	return out
}

// AracBul: ada göre tanım (kopya).
func AracBul(ad string) (AracTanimi, bool) {
	for _, t := range Katalog() {
		if t.Ad == ad {
			return t, true
		}
	}
	return AracTanimi{}, false
}

// KatalogOzeti: tanımların (doğrulama kuralları dahil) sha256'sı; kayit.katalog_ozeti.
func KatalogOzeti() string {
	ozetOnce.Do(func() {
		b, err := json.Marshal(katalog)
		if err != nil {
			panic(err)
		}
		h := sha256.Sum256(b)
		katalogOzet = hex.EncodeToString(h[:])
	})
	return katalogOzet
}
