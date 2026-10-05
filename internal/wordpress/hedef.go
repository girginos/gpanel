package wordpress

// hedef.go — WordPress kurulum/yönetim HEDEFİ: ana alan adı (public_html), ek (addon) alan
// adı (domains/<alan>/public_html) ve alt alan adı (subdomains/<tam_ad>).
//
// Rota tasarımı (ADR): ek alan adı alt alanla SİMETRİK ayrı rota öneki alır
// (/domains/{id}/ek-alan-adlari/{eid}/wordpress/...) ve AYNI işleyicileri kullanır; hedef
// URL parametresinden (kapsam) çözülür. {eid}/{sid} hesaba (domain_id) bağlanır — başka
// hesabın kimliği 404 (IDOR). Eksik/yabancı kimlik ANA alana DÜŞMEZ (fail-closed).
//
// Docroot güvenliği: yol yalnız DB'deki doğrulanmış alan adından üretilir; kök yetkili
// adımlar (MkdirAll/chown) öncesi sembolik bağ çözülmüş yol hesap dizininde kalmalı
// (docrootGuvenli); docroot içeriği ölçümü openat2(RESOLVE_BENEATH|NO_SYMLINKS) ile.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"
	"girginospanel/internal/provisioner"
	"girginospanel/internal/subdomain"

	"github.com/go-chi/chi/v5"
	"golang.org/x/sys/unix"
)

// wpEv: hesap ev dizinlerinin kökü (üretimde /home; testler geçici dizine yönlendirir).
var wpEv = "/home"

// Dış etkili adımlar (testler sahteyle değiştirir; üretimde değerler SABİTTİR).
var (
	welcomeFn = provisioner.WelcomeHTML
	silFn     = guvenliSilTenant // kiracı kimliğinde silme
)

const (
	TurAna = "ana"
	TurEk  = "ek"
	TurAlt = "alt"

	// hedefAzami: tek hesapta taranan ek/alt alan adı üst sınırı (aşırı büyük hesapta tarama taşmasın).
	hedefAzami = 200
	// docrootGirdiAzami: docroot ölçümünde okunan girdi sayısı üst sınırı.
	docrootGirdiAzami = 2000
)

var errHedefYok = errors.New("hedef alan adı bulunamadı")

// kapsam: işlemin hedefi.
type kapsam struct {
	Tur  string
	Host string // ana alan için "" (çağıran domains.alan_adi'ni kullanır)
	Root string // docroot (mutlak)
	SSL  bool   // yalnız Tur != ana için anlamlı (sertifika dosyalarından)
	EkID int64
	Alt  int64
}

// hedefAdGuvenli: DB'den gelse bile yol bileşeni olarak kullanmadan önce son süzgeç.
func hedefAdGuvenli(ad string) bool {
	if ad == "" || len(ad) > 253 || strings.HasPrefix(ad, ".") || strings.Contains(ad, "..") {
		return false
	}
	return !strings.ContainsAny(ad, "/\\\x00 \t\r\n")
}

// kapsam: URL'deki {eid} (ek alan) / {sid} (alt alan) / hiçbiri (ana) → hedef. Hesaba
// bağlı değilse ok=false (çağıran 404 döner).
func (h *Handlers) kapsam(r *http.Request, sk string) (kapsam, bool) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if e := chi.URLParam(r, "eid"); e != "" {
		eid, _ := strconv.ParseInt(e, 10, 64)
		var ad string
		if err := h.DB.QueryRowContext(r.Context(),
			`SELECT alan_adi FROM ek_alan_adlari WHERE id=? AND domain_id=?`, eid, id).Scan(&ad); err != nil || !hedefAdGuvenli(ad) {
			return kapsam{}, false
		}
		return kapsam{Tur: TurEk, Host: ad, Root: subdomain.EkDocrootYolu(wpEv, sk, ad),
			SSL: subdomain.SSLAktifYol(wpEv, sk, ad), EkID: eid}, true
	}
	if s := chi.URLParam(r, "sid"); s != "" {
		sid, _ := strconv.ParseInt(s, 10, 64)
		var tamAd string
		if err := h.DB.QueryRowContext(r.Context(),
			`SELECT tam_ad FROM subdomanlar WHERE id=? AND domain_id=?`, sid, id).Scan(&tamAd); err != nil || !hedefAdGuvenli(tamAd) {
			return kapsam{}, false
		}
		return kapsam{Tur: TurAlt, Host: tamAd, Root: subdomain.AltDocrootYolu(wpEv, sk, tamAd),
			SSL: subdomain.SSLAktifYol(wpEv, sk, tamAd), Alt: sid}, true
	}
	return kapsam{Tur: TurAna, Root: wpEv + "/" + sk + "/public_html"}, true
}

// hedefHata: dizin çözümü hatasını HTTP koduna çevirir (hedef yok = 404, gerisi 400).
func hedefHata(w http.ResponseWriter, err error) {
	if errors.Is(err, errHedefYok) {
		httpx.WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	httpx.WriteError(w, http.StatusBadRequest, err.Error())
}

// gercekYol: sembolik bağlantıları çözülmüş yol; var olmayan kısım için en yakın var olan
// üst dizin çözülür ve kalan bileşenler eklenir.
func gercekYol(p string) (string, error) {
	kalan := ""
	for {
		if g, err := filepath.EvalSymlinks(p); err == nil {
			if kalan == "" {
				return g, nil
			}
			return filepath.Join(g, kalan), nil
		}
		ust := filepath.Dir(p)
		if ust == p {
			return "", errors.New("yol çözümlenemedi")
		}
		kalan = filepath.Join(filepath.Base(p), kalan)
		p = ust
	}
}

// docrootGuvenli: yol hesap dizininde kalmalı VE sembolik bağlantılar çözüldükten sonra da
// kalmalı. Kök yetkili adımlardan (MkdirAll/chown) ÖNCE çağrılır; kiracı domains/<alan>
// ya da subdomains dizinini başka hesabın ağacına sembolik bağla değiştiremez.
func docrootGuvenli(sk, yol string) error {
	taban := filepath.Clean(wpEv + "/" + sk)
	temiz := filepath.Clean(yol)
	if temiz != taban && !strings.HasPrefix(temiz, taban+string(filepath.Separator)) {
		return errors.New("yol hesap dizini dışında")
	}
	gTaban, err := filepath.EvalSymlinks(taban)
	if err != nil {
		return errors.New("hesap dizini çözümlenemedi")
	}
	g, err := gercekYol(temiz)
	if err != nil {
		return err
	}
	if g != gTaban && !strings.HasPrefix(g, gTaban+string(filepath.Separator)) {
		return errors.New("yol sembolik bağlantıyla hesap dizini dışına çıkıyor")
	}
	return nil
}

// zararsizDosya: kurulumu ENGELLEMEYEN placeholder/sistem dosyası ("boş" sayılır).
func zararsizDosya(ad string) bool {
	switch strings.ToLower(ad) {
	case "index.html", "index.htm", "favicon.ico", "robots.txt",
		"error_log", ".user.ini", ".well-known", "cgi-bin",
		".ftpquota", ".htaccess", ".git", ".gitkeep":
		return true
	}
	return false
}

// DocrootDurum değerleri.
const (
	DocBos  = "bos"
	DocDolu = "dolu"
	DocWP   = "wp_var"
	DocYok  = "yok"
)

// docrootAc: root'u hesap dizinine göre RESOLVE_BENEATH|NO_SYMLINKS ile dizin olarak açar.
func docrootAc(sk, root string) (*os.File, error) {
	home := wpEv + "/" + sk
	rel, err := filepath.Rel(home, root)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, errors.New("yol hesap dizini dışında")
	}
	hf, err := unix.Open(home, unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(hf)
	fd, err := unix.Openat2(hf, rel, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NONBLOCK,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), rel), nil
}

// docrootDurum: docroot'un içeriğini GÜVENLE ölçer: yok | bos | dolu | wp_var. Sembolik bağ
// ya da hesap dışı yol "yok" sayılır (kurulum yapılamaz).
func docrootDurum(sk, root string) string {
	f, err := docrootAc(sk, root)
	if err != nil {
		return DocYok
	}
	defer f.Close()
	adlar, err := f.Readdirnames(docrootGirdiAzami + 1)
	if err != nil && !errors.Is(err, io.EOF) && len(adlar) == 0 {
		return DocYok
	}
	if len(adlar) > docrootGirdiAzami {
		return DocDolu
	}
	for _, a := range adlar {
		if a == "wp-config.php" {
			return DocWP
		}
	}
	for _, a := range adlar {
		if !zararsizDosya(a) {
			return DocDolu
		}
	}
	return DocBos
}

// guvenliOku: hesap dizini altındaki DÜZ dosyayı (sembolik bağ yok, FIFO'da takılmaz) en
// çok max bayt okur. Başarısızsa ok=false.
func guvenliOku(sk, yol string, max int64) ([]byte, bool) {
	home := wpEv + "/" + sk
	rel, err := filepath.Rel(home, yol)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return nil, false
	}
	hf, err := unix.Open(home, unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, false
	}
	defer unix.Close(hf)
	fd, err := unix.Openat2(hf, rel, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK | unix.O_NOCTTY,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return nil, false
	}
	f := os.NewFile(uintptr(fd), rel)
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() || st.Size() > max {
		return nil, false
	}
	b := make([]byte, max+1)
	n, _ := f.Read(b)
	return b[:n], true
}

// karsilamaMi: içerik panelin yeni hesap/ek alan için yazdığı karşılama sayfası mı?
// Tespit panelin kendi işaretleri: tam eşleşme (güncel sürüm) VEYA sayfanın iki değişmez
// işareti birlikte ("Web siteniz hazır" + girginos damgası) ve tek dosya küçük. Kullanıcının
// kendi index.html'i bu iki işareti birlikte taşımaz → ASLA silinmez.
func karsilamaMi(icerik []byte, tam string) bool {
	s := string(icerik)
	if s == tam {
		return true
	}
	return len(icerik) < 64<<10 && strings.Contains(s, "Web siteniz hazır") && strings.Contains(s, `<div class="dev">girginos</div>`)
}

// karsilamaTemizle: hedef docroot'taki index.html panelin karşılama sayfasıysa kiracı
// kimliğinde kaldırır (WordPress index.php'si öne geçer; eski sayfa kalıntı olmasın).
// Kullanıcının kendi dosyasına dokunmaz. Kaldırılırsa true.
func karsilamaTemizle(sk, hedef, alan string) bool {
	yol := filepath.Join(hedef, "index.html")
	b, ok := guvenliOku(sk, yol, 64<<10)
	if !ok || !karsilamaMi(b, welcomeFn(alan)) {
		return false
	}
	return silFn(sk, yol) == nil
}

// ---------------------------------------------------------------- hedef listesi

// Hedef: kurulum hedefi (müşteriye dönen biçim — mutlak yol DÖNMEZ).
type Hedef struct {
	Tur          string `json:"tur"` // ana | ek | alt
	AlanAdi      string `json:"alan_adi"`
	EkAlanID     int64  `json:"ek_alan_id,omitempty"`
	AltAlanID    int64  `json:"alt_alan_id,omitempty"`
	DocrootDurum string `json:"docroot_durum"` // bos | dolu | wp_var | yok
	SSL          bool   `json:"ssl"`
	URLOnizleme  string `json:"url_onizleme"`
	// Kurulabilir: bu alan adına (en azından alt dizinle) WordPress kurulabilir.
	Kurulabilir bool `json:"kurulabilir"`
	// KokKurulabilir: docroot kökü kuruluma uygun (boş). wp_var/dolu iken yalnız alt dizin.
	KokKurulabilir bool   `json:"kok_kurulabilir"`
	Neden          string `json:"neden,omitempty"`

	root string // mutlak docroot (JSON'a girmez)
}

func (h *Handlers) hedefler(ctx context.Context, id int64, sk, alanAdi string, ssl, demo bool) []Hedef {
	satir := func(tur, ad string, ek, alt int64, root string, s bool) Hedef {
		d := Hedef{Tur: tur, AlanAdi: ad, EkAlanID: ek, AltAlanID: alt, SSL: s, root: root,
			URLOnizleme: h.scheme(s) + ad, DocrootDurum: docrootDurum(sk, root)}
		switch {
		case demo:
			d.Neden = "demo aboneliğinde kullanılamaz"
		case d.DocrootDurum == DocYok && tur == TurEk:
			d.Neden = "Ek alan adı dizini bulunamadı; ek alan adı sayfasından kontrol edin"
		case d.DocrootDurum == DocYok:
			// ana/alt: kurulum dizini kendisi oluşturur (mevcut davranış)
			d.Kurulabilir, d.KokKurulabilir = true, true
		case d.DocrootDurum == DocWP:
			d.Kurulabilir = true
			d.Neden = "Bu alan adında WordPress kurulu; yalnız alt dizine kurulabilir"
		case d.DocrootDurum == DocDolu:
			d.Kurulabilir = true
			d.Neden = "Dizin boş değil; bir alt dizin seçin"
		default:
			d.Kurulabilir, d.KokKurulabilir = true, true
		}
		return d
	}
	out := []Hedef{satir(TurAna, alanAdi, 0, 0, wpEv+"/"+sk+"/public_html", ssl)}
	if rows, err := h.DB.QueryContext(ctx,
		`SELECT id, alan_adi FROM ek_alan_adlari WHERE domain_id=? ORDER BY alan_adi LIMIT ?`, id, hedefAzami); err == nil {
		for rows.Next() {
			var eid int64
			var ad string
			if rows.Scan(&eid, &ad) == nil && hedefAdGuvenli(ad) {
				out = append(out, satir(TurEk, ad, eid, 0, subdomain.EkDocrootYolu(wpEv, sk, ad), subdomain.SSLAktifYol(wpEv, sk, ad)))
			}
		}
		rows.Close()
	}
	if rows, err := h.DB.QueryContext(ctx,
		`SELECT id, tam_ad FROM subdomanlar WHERE domain_id=? ORDER BY tam_ad LIMIT ?`, id, hedefAzami); err == nil {
		for rows.Next() {
			var sid int64
			var ad string
			if rows.Scan(&sid, &ad) == nil && hedefAdGuvenli(ad) {
				out = append(out, satir(TurAlt, ad, 0, sid, subdomain.AltDocrootYolu(wpEv, sk, ad), subdomain.SSLAktifYol(wpEv, sk, ad)))
			}
		}
		rows.Close()
	}
	return out
}

// GET /domains/{id}/wordpress/hedefler — hesabın tüm WordPress kurulum hedefleri.
func (h *Handlers) Hedefler(w http.ResponseWriter, r *http.Request) {
	id, sk, alanAdi, ssl, demo, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.hedefler(r.Context(), id, sk, alanAdi, ssl, demo))
}
