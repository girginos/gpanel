// Package bayidilim: bayi kaynak havuzunun (VPS benzeri CPU/RAM tavanı) systemd dilim
// ADLARI, üyelik kaydı ve FPM drop-in'i. YAPRAK paket: yalnız standart kütüphane; kiraci,
// cgroup, provisioner, laravel ve kaynaklimit bunu içe aktarır, bu paket onları ASLA.
//
// 🔴 HİYERARŞİ İSİMLE KURULUR. systemd bir dilimin ebeveynini adındaki "-" ayracından
// çıkarır; dilim birimine Slice= verilemez (VM-C'de ölçüldü). Bu yüzden bayi havuzu:
//
//	girginos.slice
//	└ girginos-bayi<ID>.slice                 ← havuz (CPUQuota/MemoryMax burada)
//	  └ girginos-bayi<ID>-<sk>.slice          ← üye hostingin plan dilimi (eskiden girginos-<sk>.slice)
//	    └ php-fpm-<sk>.service
//
// Hosting plan limitleri üye dilimde AYNEN çalışmaya devam eder; havuz onları sarar.
// Havuzu olmayan (ya da havuzu kapalı) bayinin hostingleri ESKİ adla (girginos-<sk>.slice)
// kalır: değer verilene kadar mevcut bayiler etkilenmez.
//
// Üyelik kaydı dosyadır (UyelikDizini/<sk> = bayi kimliği): veritabanını bilmeyen yerler
// (cron çalıştırıcı, systemd-run, birim şablonları) tek bir ucuz okumayla doğru dilim adını
// alır. Dosyayı YALNIZ bayikaynak.Uzlastir yazar (veritabanından türetilmiş, her açılışta
// yeniden kurulur).
package bayidilim

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Onek: tüm panel kiracı dilimlerinin öneki (kiraci.DilimOnek ile aynı).
const Onek = "girginos-"

// DropinAd: üye birimlere yazılan drop-in dosya adı. 10-gpanel-startlimit.conf'tan SONRA
// okunur; Slice= tek değerlidir, son yazan kazanır.
const DropinAd = "20-gpanel-bayi-havuz.conf"

// UyelikDizini: sk başına üyelik dosyaları. Üretimde SABİT; testler geçici dizine çevirir.
var UyelikDizini = "/etc/girginospanel/bayi-kaynak/uyelik"

// EnBuyukBayiID: dilim/proje adlarına girebilecek en büyük bayi kimliği (adlandırma ve
// XFS proje kimliği taşmasın).
const EnBuyukBayiID = 900_000_000

var reSK = regexp.MustCompile(`^c_[a-z0-9_]{1,60}$`)

// GecerliSK: panel kiracı kimliği biçimi (provisioner.SlugFromDomain: "c_" + [a-z0-9_]).
// "-", "/", ".." ya da boşluk içeremez → dilim adına, yola ve komut satırına enjeksiyon yok.
func GecerliSK(sk string) bool { return reSK.MatchString(sk) }

// GecerliBayiID: pozitif ve sınırlı.
func GecerliBayiID(id int64) bool { return id > 0 && id <= EnBuyukBayiID }

// BayiDilim: havuz dilimi adı ("girginos-bayi12.slice"); geçersiz kimlik → "".
func BayiDilim(id int64) string {
	if !GecerliBayiID(id) {
		return ""
	}
	return Onek + "bayi" + strconv.FormatInt(id, 10) + ".slice"
}

// UyeDilim: havuza üye hostingin dilimi ("girginos-bayi12-c_x.slice"); geçersiz girdi → "".
func UyeDilim(id int64, sk string) string {
	if !GecerliBayiID(id) || !GecerliSK(sk) {
		return ""
	}
	return Onek + "bayi" + strconv.FormatInt(id, 10) + "-" + sk + ".slice"
}

// EskiDilim: havuzsuz hostingin dilimi ("girginos-c_x.slice"); geçersiz sk → "".
func EskiDilim(sk string) string {
	if !GecerliSK(sk) {
		return ""
	}
	return Onek + sk + ".slice"
}

// reBayiDilim: "girginos-bayi<N>.slice".
var reBayiDilim = regexp.MustCompile(`^girginos-bayi([0-9]{1,9})\.slice$`)

// reUyeDilim: "girginos-bayi<N>-c_<sk>.slice".
var reUyeDilim = regexp.MustCompile(`^girginos-bayi([0-9]{1,9})-(c_[a-z0-9_]{1,60})\.slice$`)

// BayiDilimindenID: "girginos-bayi12.slice" → 12.
func BayiDilimindenID(ad string) (int64, bool) {
	m := reBayiDilim.FindStringSubmatch(ad)
	if m == nil {
		return 0, false
	}
	id, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || !GecerliBayiID(id) {
		return 0, false
	}
	return id, true
}

// UyeDilimindenAyir: "girginos-bayi12-c_x.slice" → (12, "c_x").
func UyeDilimindenAyir(ad string) (int64, string, bool) {
	m := reUyeDilim.FindStringSubmatch(ad)
	if m == nil {
		return 0, "", false
	}
	id, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || !GecerliBayiID(id) {
		return 0, "", false
	}
	return id, m[2], true
}

// Uyelik: sk hangi bayi havuzunun üyesi (0 = değil). Dosya yok / bozuk / geçersiz kimlik → 0
// (güvenli varsayılan: eski ad).
func Uyelik(sk string) int64 {
	if !GecerliSK(sk) {
		return 0
	}
	b, err := os.ReadFile(filepath.Join(UyelikDizini, sk))
	if err != nil {
		return 0
	}
	id, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil || !GecerliBayiID(id) {
		return 0
	}
	return id
}

// Adi: sk'nin GEÇERLİ dilim birimi adı — üyeyse havuza iç içe, değilse eski ad.
// Geçersiz sk → "" (çağıran reddetmeli).
func Adi(sk string) string {
	if !GecerliSK(sk) {
		return ""
	}
	if id := Uyelik(sk); id > 0 {
		return UyeDilim(id, sk)
	}
	return EskiDilim(sk)
}

// AdiYedekli: Adi, ama sk bu paketin katı biçimine uymuyorsa ESKİ davranışı korur
// ("girginos-<sk>.slice"). Kendi sk doğrulaması olan çağıranlar (laravel) için: boş Slice=
// systemd'de "dilimi sıfırla" demektir, yani sessizce sınırsız çalışmak — asla boş dönmez.
func AdiYedekli(sk string) string {
	if a := Adi(sk); a != "" {
		return a
	}
	return Onek + sk + ".slice"
}

// CgroupGoreli: sk dilimi cgroup kökünden göreli dizin ("girginos.slice/girginos-bayi12.slice/
// girginos-bayi12-c_x.slice"); geçersiz sk → "".
func CgroupGoreli(sk string) string {
	if !GecerliSK(sk) {
		return ""
	}
	if id := Uyelik(sk); id > 0 {
		return filepath.Join("girginos.slice", BayiDilim(id), UyeDilim(id, sk))
	}
	return filepath.Join("girginos.slice", EskiDilim(sk))
}

// BayiCgroupGoreli: havuz diliminin cgroup dizini (göreli); geçersiz kimlik → "".
func BayiCgroupGoreli(id int64) string {
	if !GecerliBayiID(id) {
		return ""
	}
	return filepath.Join("girginos.slice", BayiDilim(id))
}

// UyelikYaz: sk'yi bayi havuzuna üye kaydeder (atomik). true = değişti.
func UyelikYaz(sk string, id int64) (bool, error) {
	if !GecerliSK(sk) || !GecerliBayiID(id) {
		return false, fmt.Errorf("bayidilim: geçersiz üyelik (%q, %d)", sk, id)
	}
	if Uyelik(sk) == id {
		return false, nil
	}
	if err := os.MkdirAll(UyelikDizini, 0o755); err != nil {
		return false, err
	}
	yol := filepath.Join(UyelikDizini, sk)
	gecici := yol + ".tmp"
	if err := os.WriteFile(gecici, []byte(strconv.FormatInt(id, 10)+"\n"), 0o644); err != nil {
		return false, err
	}
	if err := os.Rename(gecici, yol); err != nil {
		_ = os.Remove(gecici)
		return false, err
	}
	return true, nil
}

// UyelikSil: üyelik kaydını kaldırır. true = vardı.
func UyelikSil(sk string) (bool, error) {
	if !GecerliSK(sk) {
		return false, fmt.Errorf("bayidilim: geçersiz sk %q", sk)
	}
	err := os.Remove(filepath.Join(UyelikDizini, sk))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// UyelikListe: tüm geçerli üyelikler (sk → bayi kimliği). Bozuk/yabancı dosyalar atlanır.
func UyelikListe() map[string]int64 {
	out := map[string]int64{}
	ents, err := os.ReadDir(UyelikDizini)
	if err != nil {
		return out
	}
	for _, e := range ents {
		if e.IsDir() || !GecerliSK(e.Name()) {
			continue
		}
		if id := Uyelik(e.Name()); id > 0 {
			out[e.Name()] = id
		}
	}
	return out
}

// DropinIcerik: üye birimin Slice= drop-in'i. Bu dosya BİRİMİN yeniden başlamasıyla etkin
// olur; daemon-reload çalışan servisi ESKİ cgroup'ta bırakır (VM-C'de ölçüldü).
func DropinIcerik(id int64, sk string) string {
	return fmt.Sprintf("# gpanel-bayi-havuz: v1 — GirginOSPanel yonetir (internal/bayidilim); elle degistirme\n"+
		"[Service]\nSlice=%s\n", UyeDilim(id, sk))
}

// DropinUygula: birimDizin/<birim>.d/DropinAd dosyasını sk'nin üyeliğine göre kurar.
//
//	üye       → drop-in yazılır (yoksa/farklıysa)
//	üye değil → drop-in varsa SİLİNİR (boş kalan .d dizini de kaldırılır)
//
// birim = "php-fpm-c_x.service" gibi; ana birim dosyasının varlığına BAKMAZ (EnableTenantFPM
// birimi bu çağrıdan hemen önce yazar; ilk kurulumda drop-in birimle birlikte hazır olsun).
// Dönen true = disk içeriği değişti (çağıran daemon-reload yapar). Süreç/servis BAŞLATMAZ.
func DropinUygula(birimDizin, birim, sk string) (bool, error) {
	if !GecerliSK(sk) || birim == "" || strings.ContainsAny(birim, "/\\") || strings.HasPrefix(birim, ".") {
		return false, fmt.Errorf("bayidilim: geçersiz drop-in hedefi (%q, %q)", birim, sk)
	}
	dizin := filepath.Join(birimDizin, birim+".d")
	yol := filepath.Join(dizin, DropinAd)
	id := Uyelik(sk)
	if id <= 0 {
		if _, err := os.Lstat(yol); err != nil {
			return false, nil
		}
		if err := os.Remove(yol); err != nil {
			return false, err
		}
		_ = os.Remove(dizin) // boşsa kalkar, doluysa (StartLimit drop-in'i) dokunulmaz
		return true, nil
	}
	want := DropinIcerik(id, sk)
	if cur, err := os.ReadFile(yol); err == nil && string(cur) == want {
		return false, nil
	}
	if err := os.MkdirAll(dizin, 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(yol, []byte(want), 0o644); err != nil {
		return false, err
	}
	return true, nil
}
