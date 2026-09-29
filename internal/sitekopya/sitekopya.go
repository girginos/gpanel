// Package sitekopya: sitenin dosyalarını aynı ev dizininde zaman-damgalı bir
// staging kopyaya klonlar (public_html → ~/kopyalar/kopya_<ts>/). Değişiklik
// öncesi anlık-görüntü almak için. Sadece DOSYALAR (veritabanı dahil değil).
//
// 🔴 GÜVENLİK MODELİ (CWE-59 link-following / kiracılar arası):
// public_html ve kopyalar/ kiracının KENDİ sahip olduğu, FTP/SSH/PHP/cron ile
// symlink'e çevirebildiği yollardır. Bu yüzden panel bu ağaçta KÖK olarak yol
// dizgesiyle dosya işlemi YAPMAZ:
//   - kopyalama (mkdir/rsync/du/restorecon) KİRACI kimliğinde koşar (kiraciKomut);
//     çekirdek her erişimde DAC'ı uygular → kiracı yalnız zaten okuyabildiğini
//     okur, yalnız zaten yazabildiği yere yazar; TOCTOU penceresi yoktur.
//   - listeleme ve silme kökte kalır ama ev dizinine KİLİTLİ os.Root üzerinden
//     (openat + bileşen bileşen O_NOFOLLOW) yapılır (kopyalarKoku).
//
// Eski hâlde rsync/chown/os.RemoveAll kök olarak koşuyordu: public_html → /etc
// (ya da komşu ev) symlink'i ile kök-okunur her dosya kopyalanıp müşteriye
// chown'lanıyor, kopyalar → komşu dizin symlink'i ile `chown` (-h yok) bağın
// HEDEFİNİN sahipliğini müşteriye devrediyor, Sil komşunun kopyalarını siliyordu.
package sitekopya

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"

	"github.com/go-chi/chi/v5"
)

type Handlers struct{ DB *sql.DB }

const maxKopyaBayt = 3 * 1024 * 1024 * 1024 // 3 GB üstü → Yedekler'e yönlendir

// kopyaZamanAsimi: tek kopyalama işinin (ölçüm + mkdir + rsync) üst sınırı.
const kopyaZamanAsimi = 5 * time.Minute

var reKopyaAd = regexp.MustCompile(`^kopya_[0-9]{8}_[0-9]{6}$`)

// reSK: panelin oluşturduğu sistem kullanıcıları (c_<slug>; provisioner
// gecerliSKAdi ile aynı alfabe, en çok 32). Yol bileşenine ve runuser argv'sine
// girer → dar doğrula.
var reSK = regexp.MustCompile(`^c_[a-z0-9_]{1,30}$`)

// evKoku: kiracı ev dizinlerinin kökü. Testler t.TempDir ile değiştirir.
var evKoku = "/home"

// osKullanici: sk → komutların koşturulacağı işletim sistemi kullanıcısı.
// Üretimde birebir; testler ayrıcalıksız "nobody" ile değiştirir.
var osKullanici = func(sk string) string { return sk }

type Kopya struct {
	Ad      string `json:"ad"`
	BoyutMB int64  `json:"boyut_mb"`
	Tarih   string `json:"tarih"`
}

// islemHatasi: çekirdek işlevlerin HTTP durum kodu taşıyan hatası.
type islemHatasi struct {
	kod   int
	mesaj string
}

func (e *islemHatasi) Error() string { return e.mesaj }

func hataYaz(w http.ResponseWriter, err error) {
	var ih *islemHatasi
	if errors.As(err, &ih) {
		httpx.WriteError(w, ih.kod, ih.mesaj)
		return
	}
	httpx.WriteError(w, http.StatusInternalServerError, "işlem başarısız")
}

func evDizini(sk string) string { return evKoku + "/" + sk }

// domain: URL'deki {id}'yi çözer. Rota MusteriScope ile korunuyor; handler da
// kapsamı KENDİSİ doğrular (savunmada derinlik: bayi yalnız kendi hostingi,
// müşteri yalnız kendi domaini) — DB'ye dokunmadan ÖNCE. Kapsam dışı domain
// "bulunamadı" döner (varlık sızdırmaz).
func (h *Handlers) domain(r *http.Request) (id int64, sk string, demo, ok bool) {
	id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if !middleware.DomainSahibiMi(r, id) {
		return id, "", false, false
	}
	var isDemo int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, COALESCE(is_demo,0) FROM domains WHERE id=?`, id).Scan(&sk, &isDemo); err != nil {
		return id, "", false, false
	}
	return id, sk, isDemo == 1, true
}

// GET /domains/{id}/kopya
func (h *Handlers) Liste(w http.ResponseWriter, r *http.Request) {
	_, sk, _, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, kopyaListe(r.Context(), sk))
}

// POST /domains/{id}/kopya  → yeni staging kopya oluştur
func (h *Handlers) Olustur(w http.ResponseWriter, r *http.Request) {
	_, sk, demo, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kullanılamaz")
		return
	}
	ad, err := kopyaOlustur(r.Context(), sk, time.Now())
	if err != nil {
		hataYaz(w, err)
		return
	}
	boyut := kiraciBoyutBayt(r.Context(), sk, evDizini(sk)+"/kopyalar/"+ad) / (1024 * 1024)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "ad": ad, "boyut_mb": boyut})
}

// DELETE /domains/{id}/kopya/{ad}
func (h *Handlers) Sil(w http.ResponseWriter, r *http.Request) {
	_, sk, _, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err := kopyaKaldir(sk, chi.URLParam(r, "ad")); err != nil {
		hataYaz(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// kiraciKomut: argv'yi KİRACI kimliğinde (runuser -u sk; shell yok, panel env'i
// yok) çalıştıracak komutu hazırlar — cron.go/archivex runuserKomut deseni.
// Kopyalanan ağaç kiracınındır; kök yerine kiracı olarak koşmak tüm symlink
// hilelerini yapısal olarak kapatır (çözülen bağ /etc/shadow'u ya da komşu evi
// gösterse bile EACCES) ve dosyalar doğrudan kiracıya ait oluşur (chown yok,
// disk kotası kiracıya işler).
func kiraciKomut(ctx context.Context, sk string, argv ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "runuser", append([]string{"-u", osKullanici(sk), "--"}, argv...)...)
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=" + evDizini(sk),
		"LANG=C",
	}
	cmd.Dir = "/"
	// runuser SIGTERM'i çocuğa iletir (SIGKILL'i iletemez) → zaman aşımında önce
	// TERM; çocuk takılırsa WaitDelay sonunda Go süreci öldürür.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 15 * time.Second
	return cmd
}

// kiraciBoyutBayt: `du -sb` KİRACI kimliğinde (kök olarak bağ izleyip başka
// dizinin boyutunu sızdırmasın). du okunamayan alt dizinde çıkış kodu 1 verse
// de toplamı yazar → stdout'taki sayı kullanılır (0'a düşüp 3 GB kapağını
// atlatmasın).
func kiraciBoyutBayt(ctx context.Context, sk, p string) int64 {
	out, _ := kiraciKomut(ctx, sk, "du", "-sb", "--", p).Output()
	f := strings.Fields(string(out))
	if len(f) == 0 {
		return 0
	}
	n, _ := strconv.ParseInt(f[0], 10, 64)
	return n
}

// kopyalarKoku: ~/kopyalar'ı ev dizinine KİLİTLİ bir os.Root olarak açar.
// /home kök sahipli olduğundan ev dizininin kendisi kiracı tarafından
// değiştirilemez; kopyalar ise kiracınındır ve symlink olabilir — os.Root onu
// yalnız ev İÇİNDE kalıyorsa izler, dışarı (komşu ev, /etc) çıkan bağı reddeder
// ve sonraki tüm işlemler openat ile bu kökün altında kalır (TOCTOU yok).
func kopyalarKoku(sk string) (*os.Root, error) {
	ev, err := os.OpenRoot(evDizini(sk))
	if err != nil {
		return nil, err
	}
	defer ev.Close()
	return ev.OpenRoot("kopyalar")
}

// kopyaListe: ~/kopyalar altındaki kopyalar (en yeni üstte). Dizin girdileri
// kilitli kökten okunur (Info = fstatat NOFOLLOW); boyut kiracı kimliğinde.
func kopyaListe(ctx context.Context, sk string) []Kopya {
	out := []Kopya{}
	if !reSK.MatchString(sk) {
		return out
	}
	kd, err := kopyalarKoku(sk)
	if err != nil {
		return out
	}
	defer kd.Close()
	d, err := kd.Open(".")
	if err != nil {
		return out
	}
	entries, _ := d.ReadDir(-1)
	_ = d.Close()
	for _, e := range entries {
		if !e.IsDir() || !reKopyaAd.MatchString(e.Name()) {
			continue
		}
		k := Kopya{Ad: e.Name()}
		if fi, err := e.Info(); err == nil {
			k.Tarih = fi.ModTime().Format("2006-01-02 15:04")
		}
		k.BoyutMB = kiraciBoyutBayt(ctx, sk, evDizini(sk)+"/kopyalar/"+e.Name()) / (1024 * 1024)
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ad > out[j].Ad }) // en yeni üstte
	return out
}

// kopyaOlustur: public_html'i ~/kopyalar/<ad>/public_html altına KİRACI
// kimliğinde kopyalar; oluşan kopya adını döner.
func kopyaOlustur(ctx context.Context, sk string, simdi time.Time) (string, error) {
	if !reSK.MatchString(sk) {
		return "", &islemHatasi{http.StatusBadRequest, "geçersiz kullanıcı"}
	}
	ctx, cancel := context.WithTimeout(ctx, kopyaZamanAsimi)
	defer cancel()
	home := evDizini(sk)
	kaynak := home + "/public_html"
	// Varlık ve boyut kiracı gözünden: public_html kiracının erişebildiği bir
	// yere bağlıysa o içerik (rsync'in göreceği) ölçülür; erişemediği hedef
	// "bulunamadı"/rsync EACCES olur.
	if err := kiraciKomut(ctx, sk, "test", "-d", kaynak+"/").Run(); err != nil {
		return "", &islemHatasi{http.StatusBadRequest, "public_html bulunamadı"}
	}
	if b := kiraciBoyutBayt(ctx, sk, kaynak+"/"); b > maxKopyaBayt {
		return "", &islemHatasi{http.StatusRequestEntityTooLarge, "site 3 GB'den büyük — lütfen Yedekler aracını kullanın"}
	}
	kopyaDir := home + "/kopyalar"
	if err := kiraciKomut(ctx, sk, "mkdir", "-p", "-m", "0711", "--", kopyaDir).Run(); err != nil {
		return "", &islemHatasi{http.StatusInternalServerError, "kopya dizini oluşturulamadı"}
	}
	ad := "kopya_" + simdi.Format("20060102_150405")
	hedef := kopyaDir + "/" + ad + "/public_html"
	if err := kiraciKomut(ctx, sk, "mkdir", "-p", "--", hedef).Run(); err != nil {
		return "", &islemHatasi{http.StatusInternalServerError, "hedef oluşturulamadı"}
	}
	// rsync (trailing slash: içeriği kopyala). --delete YOK (yıkıcı değil).
	// Kiracı olarak koştuğu için ağaç içi bağlar olduğu gibi (bağ olarak) taşınır;
	// kök bağı çözülse bile yalnız kiracının okuyabildiği içerik gelir.
	if out, err := kiraciKomut(ctx, sk, "rsync", "-a", "--no-owner", "--no-group", kaynak+"/", hedef+"/").CombinedOutput(); err != nil {
		_ = kopyaKaldir(sk, ad)
		return "", &islemHatasi{http.StatusInternalServerError, "kopyalama başarısız: " + strings.TrimSpace(string(out))}
	}
	_ = kiraciKomut(ctx, sk, "restorecon", "-R", kopyaDir+"/"+ad).Run()
	return ad, nil
}

// kopyaKaldir: ~/kopyalar/<ad>'ı siler. Kökte kalır (kiracının yazılamaz 0555
// dizin bıraktığı kopya da silinebilsin) ama yalnız kilitli kök içinde: ara
// bileşen `kopyalar` komşu eve bağlıysa açılış reddedilir; son bileşen bağsa
// kopya sayılmaz (404).
func kopyaKaldir(sk, ad string) error {
	if !reSK.MatchString(sk) {
		return &islemHatasi{http.StatusBadRequest, "geçersiz kullanıcı"}
	}
	if !reKopyaAd.MatchString(ad) { // katı isim doğrulama → path traversal imkânsız
		return &islemHatasi{http.StatusBadRequest, "geçersiz kopya adı"}
	}
	kd, err := kopyalarKoku(sk)
	if err != nil {
		return &islemHatasi{http.StatusNotFound, "kopya bulunamadı"}
	}
	defer kd.Close()
	fi, err := kd.Lstat(ad)
	if err != nil || !fi.IsDir() {
		return &islemHatasi{http.StatusNotFound, "kopya bulunamadı"}
	}
	if err := kd.RemoveAll(ad); err != nil { // normal dizin, bind-mount değil; fuser YOK
		return &islemHatasi{http.StatusInternalServerError, "silinemedi"}
	}
	return nil
}
