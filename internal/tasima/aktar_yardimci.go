package tasima

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"girginospanel/internal/evalti"
	"girginospanel/internal/kiraci"
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

// tasimaYedekKok — yeniden yazılan yapılandırmaların özgün kopyaları (kök
// alanı). Testler geçici dizinle değiştirir.
var tasimaYedekKok = "/var/lib/girginospanel/tasima-yedek"

// yedekYaz — webRoot altındaki rel yapılandırmasının özgün içeriğini
// <tasimaYedekKok>/<sk>/<web kökü>/<rel> olarak saklar. Ad kiracı (sk) ve web
// kökünün ev'e göre yoluyla BENZERSİZDİR (her bileşen PathEscape'li tek dizin
// adı: "/" içermez, farklı yollar aynı ada düşmez): eskiden yalnız
// filepath.Base(filepath.Dir(webRoot)) kullanılıyordu → public_html/public
// (Laravel) belge köklü her kiracı "public_html"a düşüp birbirinin yedeğini
// eziyordu. Hata ayrıntısı (panel yolları) burada sunucu loguna yazılır;
// çağıran iş günlüğüne (bayiye açılır) yalnız genel metin yazar.
func yedekYaz(webRoot, rel string, ham []byte) error {
	ki, kok, err := kiraciYolu(webRoot)
	if err == nil {
		dizin := filepath.Join(tasimaYedekKok, filepath.Base(ki.Ev), url.PathEscape(kok))
		if err = os.MkdirAll(dizin, 0o700); err == nil {
			err = os.WriteFile(filepath.Join(dizin, url.PathEscape(rel)), ham, 0o600)
		}
	}
	if err != nil {
		log.Printf("tasima: %s yapilandirma yedegi yazilamadi: %v", rel, err)
	}
	return err
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

// ---------------------------------------------------------------------------
// Kiracı ağacı (bkz. internal/kiraci)
// ---------------------------------------------------------------------------

// evKoku / kiraciKul — kiracının ev çapası ve komut kimliği. Üretimde
// <kiraci.EvKoku>/<sk> ve sk; testler geçici ev + ayrıcalıksız kimlikle değiştirir.
var (
	evKoku    = kiraci.EvKoku
	kiraciKul = func(sk string) string { return sk }
)

// yapilandirmaMaxBayt — kökün okuyup yeniden yazdığı kiracı dosyası üst sınırı.
const yapilandirmaMaxBayt = 4 << 20

// kiraciAl — hedef sistem kullanıcısı (sk) → kiracı.
func kiraciAl(sk string) (kiraci.Kiraci, error) {
	if !kiraci.GecerliSK(sk) {
		return kiraci.Kiraci{}, fmt.Errorf("gecersiz sistem kullanicisi")
	}
	// Dilim: kiracı kimliğinde rsync/cp yerleşimi kiracının kaynak diliminde koşar;
	// slice adı panel sk'sından (Kul testte ayrıcalıksız kimlik olabilir).
	return kiraci.Kiraci{Kul: kiraciKul(sk), Ev: filepath.Join(evKoku, sk), Dilim: true, DilimSK: sk}, nil
}

// kiraciYolu — <evKoku>/<sk>/<göreli> biçimindeki mutlak yolu kiracıya ve ev'e
// göreli yola ayırır (ev kökü dışı ".." bileşeni geçersiz sk sayılır; ev'in
// kendisi → hata). Kiracı yalnız DİZGEDEN çıkarılır; bileşenleri çözmek
// evalti.Ac'ın (bağ izlemeden) işi.
func kiraciYolu(yol string) (kiraci.Kiraci, string, error) {
	rel, err := filepath.Rel(evKoku, filepath.Clean(yol))
	if err != nil {
		return kiraci.Kiraci{}, "", kiraci.ErrEvDisi
	}
	sk, _, _ := strings.Cut(rel, string(filepath.Separator))
	ki, err := kiraciAl(sk)
	if err != nil {
		return kiraci.Kiraci{}, "", err
	}
	kok, err := ki.Goreli(yol)
	return ki, kok, err
}

var errKiraciDosyasiDegil = errors.New("kiracinin duzenli dosyasi degil")

// kiraciDosyasi — ev köküne çapalı, bağ izlenmeden açılmış bir dizindeki, kiracının KENDİ
// düzenli dosyası. Sonraki işlemler (okuma, fchmod, yerine yazma) açılmış
// tanıtıcılar ve dizine göreli *at çağrılarıyla yapılır: denetimden sonra yolun
// takası işlemi başka dosyaya yönlendiremez.
type kiraciDosyasi struct {
	dizin *os.File
	ad    string
	f     *os.File
	st    unix.Stat_t
}

// kiraciDosyasiAc — ev (kiracının ev dizini, mutlak) altındaki rel'i yalnız
// kiracının KENDİ düzenli dosyasıysa açar; aksi halde hata. Çağıran Kapat'ı
// çağırır.
func kiraciDosyasiAc(ev, rel string, uid int) (*kiraciDosyasi, error) {
	rel = path.Clean(rel)
	// 🔴 Dizin evalti.Ac ile açılır (openat2 RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS):
	// yol boyunca HİÇBİR bağ izlenmez — ev DIŞINA çıkan da, ev İÇİNDE kalan da
	// (os.Root ev içi bağı izlerdi: kiracı kendi evindeki kök sahipli bir alanı
	// gösteren bağla kökün okuma/yazmasını oraya yönlendirebilirdi).
	// O_DIRECTORY: dizin yerine FIFO olursa openat bir yazıcı bekleyip SÜRESİZ
	// takılırdı (bağlam iptali kesemez; tek taşıma yuvası kilitli kalır); çekirdek
	// açmadan önce ENOTDIR döner.
	dfd, err := evalti.Ac(ev, path.Dir(rel), unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return nil, err
	}
	dizin := os.NewFile(uintptr(dfd), path.Dir(rel))
	ad := path.Base(rel)
	// Son bileşen de bağ İZLENMEDEN açılır: izlenen mutlak bağ komşu
	// kiracıya/sisteme uzanırdı. O_NONBLOCK: FIFO'da
	// (kiracı .env yerine koyabilir) takılma yok.
	fd, err := unix.Openat(int(dizin.Fd()), ad, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		dizin.Close()
		return nil, err
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		unix.Close(fd)
		dizin.Close()
		return nil, err
	}
	d := &kiraciDosyasi{dizin: dizin, ad: ad, st: st}
	switch {
	case st.Mode&unix.S_IFMT != unix.S_IFREG:
		// FIFO/aygıt/dizin: okunmaz, izni değiştirilmez.
		err = errKiraciDosyasiDegil
	case int(st.Uid) != uid:
		// Yalnız kiracının KENDİ dosyası: kökün/panelin koyduğuna dokunulmaz.
		err = errKiraciDosyasiDegil
	case st.Nlink != 1:
		// Sabit bağ: aynı inode ev dışında (komşu kiracı/sistem) da görünür;
		// fchmod ya da okuma onu etkiler/sızdırır.
		err = errKiraciDosyasiDegil
	}
	if err != nil {
		unix.Close(fd)
		dizin.Close()
		return nil, err
	}
	d.f = os.NewFile(uintptr(fd), ad)
	return d, nil
}

// Kapat — dosya ve dizin tanıtıcılarını kapatır.
func (d *kiraciDosyasi) Kapat() {
	d.f.Close()
	d.dizin.Close()
}

// Oku — en çok max bayt; fazlası hata (sessiz kesme geri yazımda veriyi bozardı).
func (d *kiraciDosyasi) Oku(max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(d.f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%s cok buyuk", d.ad)
	}
	return b, nil
}

// izin — dosyanın özgün izin bitleri.
func (d *kiraciDosyasi) izin() os.FileMode { return os.FileMode(d.st.Mode & 0o777) }

// Degistir — içeriği atomik olarak yeniler: aynı dizinde geçici dosya
// (O_EXCL|O_NOFOLLOW) + sahiplik özgün dosyanınki (doğrulandı: kiracı) + izin +
// renameat. Yarıda çökme yarım dosya bırakmaz; özgün inode'a yazılmaz.
func (d *kiraciDosyasi) Degistir(veri []byte, izin os.FileMode) error {
	dfd := int(d.dizin.Fd())
	gecici := ".gosp_cfg_" + rand.Text()
	fd, err := unix.Openat(dfd, gecici, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), gecici)
	_, err = f.Write(veri)
	if err == nil {
		err = f.Chown(int(d.st.Uid), int(d.st.Gid))
	}
	if err == nil {
		err = f.Chmod(izin)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = unix.Renameat(dfd, gecici, dfd, d.ad)
	}
	if err != nil {
		_ = unix.Unlinkat(dfd, gecici, 0)
	}
	return err
}

// docrootDisiBagimlilik — belge kokundeki PHP giris dosyalarinda "../<dizin>/"
// referanslarini bulur (Laravel/Symfony deseni). Donen adlar TEK SEVIYE dizin
// adidir; ".."/slash iceren veya supheli degerler elenir. Dosyalar bağ
// izlenmeden, yalnız kiracının kendi düzenli dosyasıysa okunur (kiraciDosyasiAc).
func docrootDisiBagimlilik(ev, dizin string, uid int) []string {
	re := regexp.MustCompile(`\.\./([A-Za-z0-9_][A-Za-z0-9._-]{0,63})/`)
	gorulen := map[string]bool{}
	var out []string
	for _, ad := range []string{"index.php", "server.php", "bootstrap.php"} {
		d, err := kiraciDosyasiAc(ev, path.Join(dizin, ad), uid)
		if err != nil {
			continue
		}
		b, err := d.Oku(1 << 20)
		d.Kapat()
		if err != nil {
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
//
// 🔴 dizin ev'e (kiracının ev dizini) göredir; kök kiracı ağacında yol
// dizgesiyle işlem YAPMAZ ve hiçbir bağı izlemez (evalti.Ac): .env yalnız
// kiracının kendi düzenli dosyasıysa (kiraciDosyasiAc) okunur/yazılır, önbellek
// dosyaları bağ izlenmeden açılmış önbellek dizininden silinir. Dosyalar kiracı
// kimliğinde yerleştirildiği için sahiplik zaten kiracınındır.
func laravelSonrasiDuzelt(ev, dizin, alanAdi string, uid int, log func(string, ...any)) {
	dfd, err := evalti.Ac(ev, dizin, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return // dizin yok, bağ ya da ev dışı
	}
	var st unix.Stat_t
	err = unix.Fstatat(dfd, "artisan", &st, unix.AT_SYMLINK_NOFOLLOW)
	unix.Close(dfd)
	if err != nil {
		return // Laravel degil
	}
	if env, err := kiraciDosyasiAc(ev, path.Join(dizin, ".env"), uid); err == nil {
		if b, err := env.Oku(yapilandirmaMaxBayt); err == nil {
			yeni := regexp.MustCompile(`(?m)^APP_URL=.*$`).ReplaceAllString(string(b), "APP_URL=https://"+alanAdi)
			if yeni != string(b) && env.Degistir([]byte(yeni), 0o600) == nil {
				log("laravel: APP_URL → https://%s", alanAdi)
			} else {
				_ = env.f.Chmod(0o600)
			}
		}
		env.Kapat()
	}
	if cfd, err := evalti.Ac(ev, path.Join(dizin, "bootstrap", "cache"), unix.O_RDONLY|unix.O_DIRECTORY); err == nil {
		for _, c := range []string{"config.php", "services.php", "packages.php", "routes-v7.php"} {
			_ = unix.Unlinkat(cfd, c, 0)
		}
		unix.Close(cfd)
	}
	log("laravel: onbellek temizlendi, .env 0600")
}
