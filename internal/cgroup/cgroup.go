// Package cgroup: cgroup v2 SALT-OKUR okuyucu — paneldeki tek cgroup okuma noktası.
//
// Kullananlar:
//   - kaynak.Canli (GET /domains/{id}/canli): kiracının systemd slice'ı
//     /sys/fs/cgroup/girginos.slice/girginos-<sk>.slice
//   - hostuyg.MetrikTopla (AdminOnly; hostUygulamalariAcik=false iken rotası kayıtsız):
//     /sys/fs/cgroup/system.slice/<unit>.service
//
// 🔴 Bu paket hiçbir cgroup dosyasına YAZMAZ. Limit yazımı kaynaklimit'tedir
// (slice dosyası + systemctl set-property).
package cgroup

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/bayidilim"
)

// Kok: cgroup v2 bağlama noktası. Testler geçici bir sahte ağaca çevirir.
var Kok = "/sys/fs/cgroup"

// blokKok: /sys/dev/block — io.stat'taki "MAJ:MIN" cihazlarının yığın (dm/md)
// ilişkisini okumak için. Testler geçici bir sahte ağaca çevirir.
var blokKok = "/sys/dev/block"

// Sinirsiz: tek değerli dosyada "max" (memory.max, pids.max, cpu.max kotası).
const Sinirsiz int64 = -1

// reKiraciSK: provisioner.SlugFromDomain biçimi ("c_" + [a-z0-9_]); kaynaklimit'in
// kota allowlist'iyle aynı.
var reKiraciSK = regexp.MustCompile(`^c_[a-z0-9_]{1,60}$`)

var reMajMin = regexp.MustCompile(`^[0-9]+:[0-9]+$`)

// KiraciDizin: kiracının slice cgroup dizini; sk biçim dışıysa "".
//
// Yol birleştirmeye "/", ".." ya da "-" giremez. systemd "-"yi hiyerarşi ayracı
// sayar (girginos-c_x.slice → girginos.slice/girginos-c_x.slice); sk'de "-"
// olmadığından yol tek düzeydir (havuz üyesinde iki düzey: bkz. aşağısı).
func KiraciDizin(sk string) string {
	if !reKiraciSK.MatchString(sk) {
		return ""
	}
	// Bayi kaynak havuzuna üye hosting iç içe dilimde yaşar (girginos-bayi<N>-<sk>.slice).
	// Üyelik YENİ yazıldı ama birim henüz yeniden başlamadıysa süreçler hâlâ ESKİ dilimde:
	// iç içe dizin yoksa eski dizine düş (canlı kullanım sıfır görünmesin).
	if id := bayidilim.Uyelik(sk); id > 0 {
		ic := filepath.Join(Kok, bayidilim.CgroupGoreli(sk))
		if st, err := os.Stat(ic); err == nil && st.IsDir() {
			return ic
		}
	}
	return filepath.Join(Kok, "girginos.slice", "girginos-"+sk+".slice")
}

// ServisDizin: system.slice altındaki bir servis biriminin cgroup dizini.
func ServisDizin(unit string) string {
	return filepath.Join(Kok, "system.slice", unit+".service")
}

// Deger: tek değerli dosya (memory.current, memory.max, pids.current, pids.max…).
// "max" → Sinirsiz. Dosya yok, okunamıyor ya da sayı değil → ok=false.
func Deger(yol string) (int64, bool) {
	b, err := os.ReadFile(yol)
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(b))
	if s == "max" {
		return Sinirsiz, true
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// CPUUsec: cpu.stat "usage_usec" — birikimli CPU süresi (mikro saniye).
func CPUUsec(dizin string) (int64, bool) {
	b, err := os.ReadFile(filepath.Join(dizin, "cpu.stat"))
	if err != nil {
		return 0, false
	}
	for _, satir := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(satir, "usage_usec "); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			return n, err == nil
		}
	}
	return 0, false
}

// CPUMax: cpu.max "<kota> <periyot>" (µs). Kota "max" ise kota=Sinirsiz.
func CPUMax(dizin string) (kota, periyot int64, ok bool) {
	b, err := os.ReadFile(filepath.Join(dizin, "cpu.max"))
	if err != nil {
		return 0, 0, false
	}
	f := strings.Fields(string(b))
	if len(f) != 2 {
		return 0, 0, false
	}
	periyot, err = strconv.ParseInt(f[1], 10, 64)
	if err != nil || periyot <= 0 {
		return 0, 0, false
	}
	if f[0] == "max" {
		return Sinirsiz, periyot, true
	}
	kota, err = strconv.ParseInt(f[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return kota, periyot, true
}

// IOToplam: io.stat'taki cihazların rbytes/wbytes toplamı (birikimli bayt).
//
// Yığın cihazlar çift sayılmasın diye atlanır. dm-0'a (LVM; AlmaLinux'un varsayılan
// kurulumu) yapılan G/Ç, alttaki diske iletilen bio'larda aynı cgroup'a bir kez daha
// yazılır. "slaves" dizini dolu olan cihaz (başka cihazların üstüne kurulu) sayılmaz,
// yalnız yaprak cihazlar toplanır. Listede hiç yaprak cihaz yoksa (alt diske bu
// cgroup'tan hiç G/Ç gitmemişse) hepsi toplanır. Boş io.stat → (0, 0, true).
func IOToplam(dizin string) (okuma, yazma int64, ok bool) {
	b, err := os.ReadFile(filepath.Join(dizin, "io.stat"))
	if err != nil {
		return 0, 0, false
	}
	type cihaz struct {
		r, w  int64
		yigin bool
	}
	var liste []cihaz
	yaprakVar := false
	for _, satir := range strings.Split(string(b), "\n") {
		f := strings.Fields(satir)
		if len(f) < 2 {
			continue
		}
		c := cihaz{yigin: yiginMi(f[0])}
		for _, kv := range f[1:] {
			k, v, _ := strings.Cut(kv, "=")
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				continue
			}
			switch k {
			case "rbytes":
				c.r = n
			case "wbytes":
				c.w = n
			}
		}
		if !c.yigin {
			yaprakVar = true
		}
		liste = append(liste, c)
	}
	for _, c := range liste {
		if c.yigin && yaprakVar {
			continue
		}
		okuma += c.r
		yazma += c.w
	}
	return okuma, yazma, true
}

// yiginMi: MAJ:MIN cihazı başka cihazların üstüne mi kurulu (dm/md → slaves dolu)?
func yiginMi(majmin string) bool {
	if !reMajMin.MatchString(majmin) {
		return false
	}
	g, err := os.ReadDir(filepath.Join(blokKok, majmin, "slaves"))
	return err == nil && len(g) > 0
}

// Sayac: bir cgroup'un birikimli sayaçlarından tek örnek. *Var=false → o sayaç okunamadı.
type Sayac struct {
	Zaman            time.Time
	CPUUsec          int64
	CPUVar           bool
	IOOkuma, IOYazma int64
	IOVar            bool
}

// Hiz: iki örnek arasındaki hızlar. nil = bilinmiyor (ilk örnek, bayat örnek,
// sayaç geri gitti, dosya okunamadı). Dönen işaretçiler salt okunurdur.
type Hiz struct {
	CPUYuzde   *float64 // tek çekirdek = 100; çok çekirdekte 100'ü aşabilir
	IOOkumaBps *float64
	IOYazmaBps *float64
}

// Ornekleyici: anahtar başına son sayaç örneğini süreç belleğinde (kilitli) tutar ve
// yeni örnekle arasındaki farktan hız hesaplar.
type Ornekleyici struct {
	// EnAz: bundan kısa aralık gürültülüdür. Taban güncellenmez, son hesaplanan hız
	// döner. Aynı anahtarı birkaç görüntüleyen birlikte yoklarken de hız tutarlı kalır.
	EnAz time.Duration
	// EnCok: bundan uzun aralığın ortalaması "canlı" değildir. Yeni taban alınır, hız
	// bilinmiyor döner. Bu yaştan eski girdiler budanır (bellek sınırlı kalır).
	// 0 = sınırsız.
	EnCok time.Duration

	mu        sync.Mutex
	son       map[string]ornekKaydi
	sonBudama time.Time
}

type ornekKaydi struct {
	s   Sayac
	hiz Hiz
}

// Hesapla: s'yi anahtarın yeni tabanı yapar ve bir öncekiyle arasındaki hızı döner.
func (o *Ornekleyici) Hesapla(anahtar string, s Sayac) Hiz {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.son == nil {
		o.son = map[string]ornekKaydi{}
	}
	o.buda(s.Zaman)
	if onceki, var_ := o.son[anahtar]; var_ {
		dt := s.Zaman.Sub(onceki.s.Zaman)
		if dt >= 0 && dt < o.EnAz {
			return onceki.hiz
		}
		if dt > 0 && (o.EnCok == 0 || dt <= o.EnCok) {
			h := hizHesapla(onceki.s, s, dt)
			o.son[anahtar] = ornekKaydi{s: s, hiz: h}
			return h
		}
	}
	o.son[anahtar] = ornekKaydi{s: s}
	return Hiz{}
}

func hizHesapla(a, b Sayac, dt time.Duration) Hiz {
	var h Hiz
	if a.CPUVar && b.CPUVar && b.CPUUsec >= a.CPUUsec {
		v := float64(b.CPUUsec-a.CPUUsec) * 100 / float64(dt.Microseconds())
		h.CPUYuzde = &v
	}
	if a.IOVar && b.IOVar && b.IOOkuma >= a.IOOkuma && b.IOYazma >= a.IOYazma {
		sn := dt.Seconds()
		r := float64(b.IOOkuma-a.IOOkuma) / sn
		w := float64(b.IOYazma-a.IOYazma) / sn
		h.IOOkumaBps, h.IOYazmaBps = &r, &w
	}
	return h
}

// buda: EnCok'tan eski girdileri siler; en çok EnCok'ta bir çalışır. Kilit tutulurken çağrılır.
func (o *Ornekleyici) buda(simdi time.Time) {
	if o.EnCok <= 0 || simdi.Sub(o.sonBudama) < o.EnCok {
		return
	}
	for k, v := range o.son {
		if simdi.Sub(v.s.Zaman) > o.EnCok {
			delete(o.son, k)
		}
	}
	o.sonBudama = simdi
}

// Sil: anahtarın örneğini unutur (ör. birim kaldırıldı).
func (o *Ornekleyici) Sil(anahtar string) {
	o.mu.Lock()
	delete(o.son, anahtar)
	o.mu.Unlock()
}

// Suz: tut(anahtar)=false olan örnekleri siler.
func (o *Ornekleyici) Suz(tut func(anahtar string) bool) {
	o.mu.Lock()
	for k := range o.son {
		if !tut(k) {
			delete(o.son, k)
		}
	}
	o.mu.Unlock()
}
