package tasima

// arsiv_yol.go — arşiv kaynağının DOSYASI: sunucudaki hazır dosya yolu (yalnız kök) ve
// yüklenmiş dosya (bayi + kök) tek yerden çözülür; yol doğrulaması ve disk ön kontrolü.
//
// 🔴 Yol kökün girdisidir ama yine DÜŞMAN sayılır (yanlışlıkla/ele geçirilmiş oturumla
// panel sırlarını, kiracı ağacını ya da başka bir dosyayı okutma yolu olmasın):
//   - mutlak, temiz (filepath.Clean ile birebir), NUL/satır sonu yok, ≤ 1024 bayt;
//   - izinli dizin kökü ALTINDA (varsayılan liste ya da cp_ayarlar 'tasima_arsiv_dizinleri');
//   - panel/kiracı/sistem dizinlerinde DEĞİL (/home, /var/lib/girginospanel, /opt/girginospanel,
//     /etc, /var/vmail, /proc ...) — izinli kök bunların altına konamaz;
//   - yol boyunca sembolik bağ YOK (her bileşen Lstat + EvalSymlinks eşitliği);
//   - dosya düzenli, boyutu (0, tavan], sahibi root ve grup/diğer yazamaz; izinli kökten
//     dosyaya kadar TÜM dizinler de root sahipli ve grup/diğer yazamaz → kök dışında kimse
//     doğrulama ile okuma arasında dosyayı değiştiremez (TOCTOU kapanır).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"

	"golang.org/x/sys/unix"

	"girginospanel/internal/tasima/arsiv"
)

var (
	errArsivYol    = errors.New("arşiv yolu geçersiz ya da izinli değil")
	errArsivYolBay = errors.New("sunucudaki dosya yolu yalnız yönetici içindir; arşivi yükleyin")
	errArsivBoyut  = errors.New("arşiv dosyası boyut sınırını aşıyor")
	errArsivYok    = errors.New("arşiv bulunamadı ya da hazır değil")
	errArsivDisk   = errors.New("hedefte yeterli boş alan yok: taşıma için arşiv boyutunun yaklaşık 2,5 katı boş alan gerekir")
	errArsivKota   = errors.New("disk kotanız arşivin açılmış boyutu için yetersiz")
	// errArsivDegisti: keşiften sonra arşiv dosyası değişti (boyut/zaman damgası).
	errArsivDegisti = errors.New("arşiv dosyası keşiften sonra değişti; yeniden keşfedin")
)

// arsivVarsayilanKokler — cp_ayarlar 'tasima_arsiv_dizinleri' yoksa izinli kökler.
var arsivVarsayilanKokler = []string{"/root", "/srv", "/mnt", "/backup", "/backups", "/var/backups"}

// arsivYasakOnekler — hiçbir izinli kök ve hiçbir arşiv yolu bunların altında olamaz.
var arsivYasakOnekler = []string{
	"/home", "/var/lib/girginospanel", "/opt/girginospanel", "/etc", "/var/vmail",
	"/var/backups/girginospanel", "/var/lib/mysql", "/var/spool", "/var/log",
	"/proc", "/sys", "/dev", "/run", "/boot", "/usr", "/bin", "/sbin", "/lib", "/lib64",
}

// arsivSahipUID — dosyanın ve üst dizinlerinin zorunlu sahibi (testler değiştirir).
var arsivSahipUID uint32 = 0

// altinda — p, k'nın kendisi ya da altında mı (bileşen sınırında).
func altinda(p, k string) bool {
	return p == k || strings.HasPrefix(p, strings.TrimSuffix(k, "/")+"/")
}

// arsivIzinliKokler — kökün ayarladığı (':' ya da satır sonuyla ayrılmış) ya da varsayılan
// dizin kökleri. Geçersiz/yasak girdi atlanır; hiç geçerli kalmazsa varsayılan.
func (h *Handlers) arsivIzinliKokler(ctx context.Context) []string {
	var v string
	err := h.DB.QueryRowContext(ctx, `SELECT deger FROM cp_ayarlar WHERE anahtar='tasima_arsiv_dizinleri'`).Scan(&v)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Printf("tasima: tasima_arsiv_dizinleri okunamadi: %v", err)
		}
		return arsivVarsayilanKokler
	}
	var out []string
	for _, p := range strings.FieldsFunc(v, func(r rune) bool { return r == ':' || r == '\n' || r == ',' }) {
		p = strings.TrimSpace(p)
		if p == "" || !filepath.IsAbs(p) || filepath.Clean(p) != p || p == "/" {
			continue
		}
		yasak := false
		for _, y := range arsivYasakOnekler {
			if altinda(p, y) {
				yasak = true
			}
		}
		if !yasak {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return arsivVarsayilanKokler
	}
	return out
}

// guvenliSahip — kök sahipli ve grup/diğer YAZAMAZ mı (sticky de kurtarmaz).
func guvenliSahip(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Uid == arsivSahipUID && fi.Mode().Perm()&0o022 == 0
}

// arsivYolDogrula — bkz. dosya başı. Döner: dosya bilgisi; hata her zaman errArsivYol /
// errArsivBoyut sarmalıdır (kısa neden yalnız yöneticiye gider).
func arsivYolDogrula(yol string, kokler []string, tavan int64) (os.FileInfo, error) {
	red := func(neden string) (os.FileInfo, error) { return nil, fmt.Errorf("%w (%s)", errArsivYol, neden) }
	if yol == "" || len(yol) > 1024 || !utf8.ValidString(yol) || strings.ContainsAny(yol, "\x00\r\n") {
		return red("biçim")
	}
	if !filepath.IsAbs(yol) || filepath.Clean(yol) != yol {
		return red("mutlak ve temiz olmalı")
	}
	for _, y := range arsivYasakOnekler {
		if altinda(yol, y) {
			return red("panel/kiracı/sistem dizini")
		}
	}
	kok := ""
	for _, k := range kokler {
		if k != yol && altinda(yol, k) && len(k) > len(kok) {
			kok = k
		}
	}
	if kok == "" {
		return red("izinli dizin kökü altında değil")
	}
	// İzinli kökten dosyanın üst dizinine: gerçek dizin (bağ değil), root sahipli, kapalı yazma.
	d := kok
	for i := 0; ; i++ {
		if i > 64 { // yol 1024 baytla sınırlı; sonsuz döngü savunması
			return red("yol çok derin")
		}
		fi, err := os.Lstat(d)
		if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return red("üst dizin yok ya da dizin/bağ değil")
		}
		if !guvenliSahip(fi) {
			return red("üst dizin root sahipli ve yazılamaz değil")
		}
		if d == filepath.Dir(yol) {
			break
		}
		rel, _ := filepath.Rel(d, yol)
		d = filepath.Join(d, strings.SplitN(rel, string(filepath.Separator), 2)[0])
	}
	fi, err := os.Lstat(yol)
	if err != nil {
		return red("dosya yok")
	}
	if !fi.Mode().IsRegular() {
		return red("düzenli dosya değil (bağ/aygıt/dizin)")
	}
	if !guvenliSahip(fi) {
		return red("dosya root sahipli ve yazılamaz değil")
	}
	if r, err := filepath.EvalSymlinks(yol); err != nil || r != yol {
		return red("yol sembolik bağ içeriyor")
	}
	if fi.Size() <= 0 {
		return red("dosya boş")
	}
	if fi.Size() > tavan {
		return nil, fmt.Errorf("%w (%d GiB üstü)", errArsivBoyut, tavan>>30)
	}
	return fi, nil
}

// arsivKaynakBilgi — çözülmüş arşiv dosyası.
type arsivKaynakBilgi struct {
	Yol       string
	Boyut     int64
	MtimeNs   int64
	YuklemeID int64
	Secenek   arsiv.Secenek
}

// arsivParmakIzi — işe kaydedilir; iş başlarken dosya değişmişse (boyut/mtime) iş reddedilir.
type arsivParmakIzi struct {
	Boyut   int64 `json:"boyut"`
	MtimeNs int64 `json:"mtime_ns"`
}

// arsivKaynakCoz — (yol | yükleme) → doğrulanmış dosya. Kapsam: bayi yalnız KENDİ hazır
// yüklemesini çözer (başkasınınki "bulunamadı" ile aynı yanıt); yol yalnız kökte.
func (h *Handlers) arsivKaynakCoz(ctx context.Context, kp Kapsam, yol string, yuklemeID int64) (*arsivKaynakBilgi, error) {
	switch {
	case yuklemeID > 0:
		var boyut int64
		var durum string
		err := h.DB.QueryRowContext(ctx,
			`SELECT boyut, durum FROM tasima_yuklemeleri WHERE id=? AND sahip_rid=?`, yuklemeID, kp.RID).Scan(&boyut, &durum)
		if err != nil || durum != "hazir" {
			return nil, errArsivYok
		}
		p := yuklemeYolu(kp.RID, yuklemeID)
		fi, err := os.Lstat(p)
		if err != nil || !fi.Mode().IsRegular() || fi.Size() != boyut {
			return nil, errArsivYok
		}
		return &arsivKaynakBilgi{Yol: p, Boyut: fi.Size(), MtimeNs: fi.ModTime().UnixNano(), YuklemeID: yuklemeID, Secenek: arsiv.SecenekYukleme()}, nil
	case yol != "":
		if kp.Bayi() {
			return nil, errArsivYolBay
		}
		fi, err := arsivYolDogrula(yol, h.arsivIzinliKokler(ctx), arsiv.YolTavan)
		if err != nil {
			return nil, err
		}
		return &arsivKaynakBilgi{Yol: yol, Boyut: fi.Size(), MtimeNs: fi.ModTime().UnixNano(), Secenek: arsiv.SecenekYol()}, nil
	}
	return nil, errArsivYok
}

// parmakIziEslesir — işin kaydındaki parmak izi ile dosyanın şimdiki hâli.
func (b *arsivKaynakBilgi) parmakIziEslesir(p arsivParmakIzi) bool {
	return b.Boyut == p.Boyut && b.MtimeNs == p.MtimeNs
}

// diskBosAlan — yolun dosya sistemindeki kullanılabilir bayt (testler değiştirir).
var diskBosAlan = func(yol string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(yol, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// arsivDiskOnkontrol — kullanıcı kararı: hedefte (kiracı evlerinin dosya sistemi) boş alan
// < arşiv boyutu × 2,5 ise taşıma BAŞLAMADAN reddedilir. Hata metni sayı içermez.
func arsivDiskOnkontrol(arsivBayt int64) error {
	bos, err := diskBosAlan(evKoku)
	if err != nil {
		return fmt.Errorf("hedef disk ölçülemedi: %w", err)
	}
	if bos < gerekliBosBayt(arsivBayt) {
		return errArsivDisk
	}
	return nil
}

// gerekliBosBayt — arşiv × 2,5 (taşmasız; arayüze de gösterilen ön kontrol değeri).
func gerekliBosBayt(arsivBayt int64) int64 { return arsivBayt/2*5 + (arsivBayt%2)*5/2 }
