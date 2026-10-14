package bayikaynak

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/bayidilim"
)

// Dış dünya dikişleri. Üretimde sabit; testler geçici dizine / sahte komuta çevirir.
var (
	birimDizini      = "/etc/systemd/system"
	runKontrolDizini = "/run/systemd/system.control"

	// komutCalistir: dış komut (systemctl, xfs_quota, grubby, xfs_io). Kabuk YOK; argv dilimi.
	komutCalistir = func(ctx context.Context, ad string, arg ...string) ([]byte, error) {
		c, iptal := context.WithTimeout(ctx, 2*time.Minute)
		defer iptal()
		return exec.CommandContext(c, ad, arg...).CombinedOutput()
	}

	// restorecon: yazılan birim dosyalarının SELinux etiketini politikaya döndürür (en iyi çaba).
	restorecon = func(yol string) { _, _ = exec.Command("restorecon", yol).CombinedOutput() }

	// mu: tüm uzlaştırma yazıcılarını sıraya koyar (iki istek aynı dosyayı yarım yazmasın).
	mu sync.Mutex
)

func birimYolu(ad string) string { return filepath.Join(birimDizini, ad) }

func komutMetni(ad string, arg ...string) string { return ad + " " + strings.Join(arg, " ") }

func dilimAktif(ctx context.Context, ad string) bool {
	out, _ := komutCalistir(ctx, "systemctl", "is-active", ad)
	return strings.TrimSpace(string(out)) == "active"
}

// bosDilimiDurdur: ETKİN ama İÇİNDE SÜREÇ OLMAYAN dilimi durdurur. true: dilim artık etkin değil
// (dosyası silinebilir).
//
// 🔴 systemd boşalan dilimi kendiliğinden DURDURMAZ. "Etkin = içinde süreç var" varsayımıyla
// etkin dilim beklemeye alınıyordu; silinen bayinin/hostingin dilimi sunucu yeniden başlayana
// dek kalıyordu (VPS'te ölçüldü: bayi silindikten 1 saat sonra hâlâ etkin, 0 süreç). Süreç sayısı
// (TasksCurrent — alt dilimler dahil) 0 iken durdurmak hiçbir süreci öldürmez. Sayı okunamazsa
// (muhasebe kapalı "[not set]", komut hatası) BOŞ SAYILMAZ, dilime dokunulmaz. Çağıranlar mu
// kilidini tutar: üyelik drop-in'leri de bu kilit altında yazılır/silinir.
func bosDilimiDurdur(ctx context.Context, ad string) bool {
	out, err := komutCalistir(ctx, "systemctl", "show", ad, "-p", "TasksCurrent", "--value")
	if err != nil {
		return false
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil || n != 0 {
		return false
	}
	if o, err := komutCalistir(ctx, "systemctl", "stop", ad); err != nil {
		log.Printf("bayikaynak: boş dilim durdurulamadı %s: %s", ad, strings.TrimSpace(string(o)))
		return false
	}
	return true
}

func daemonReload(ctx context.Context) error {
	if out, err := komutCalistir(ctx, "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// Rapor: bir uzlaştırma turunun özeti (günlük + API).
type Rapor struct {
	Havuzlar      int      `json:"havuzlar"`                 // dilimi aktif bayi sayısı
	Uyeler        int      `json:"uyeler"`                   // havuza üye hosting sayısı
	DilimYazilan  []int64  `json:"dilim_yazilan,omitempty"`  // içeriği değişen havuz dilimleri (bayi kimliği)
	DilimSilinen  []string `json:"dilim_silinen,omitempty"`  // kaldırılan havuz dilimleri
	UyelikEklenen []Uye    `json:"uyelik_eklenen,omitempty"` // yeni/taşınan üyelik (üye plan dilimi yeniden yazılmalı)
	UyelikKalkan  []string `json:"uyelik_kalkan,omitempty"`  // sk: havuzdan çıktı (eski ada dönmeli)
	Dropin        int      `json:"dropin"`                   // yazılan/silinen drop-in sayısı
	Hatalar       []string `json:"hatalar,omitempty"`
}

func (r *Rapor) hata(f string, a ...any) {
	m := fmt.Sprintf(f, a...)
	log.Printf("bayikaynak: %s", m)
	r.Hatalar = append(r.Hatalar, m)
}

// Degisti: diskte/üyelikte bir şey değişti mi?
func (r Rapor) Degisti() bool {
	return len(r.DilimYazilan)+len(r.DilimSilinen)+len(r.UyelikEklenen)+len(r.UyelikKalkan)+r.Dropin > 0
}

// dilimKur: havuz dilimi dosyasını yazar (içerik aynıysa dokunmaz). true = değişti.
// Çağıran daemon-reload + canlı uygulamayı toplu yapar.
func dilimKur(h Havuz) (bool, error) {
	ad := bayidilim.BayiDilim(h.BayiID)
	if ad == "" {
		return false, fmt.Errorf("geçersiz bayi kimliği %d", h.BayiID)
	}
	yol := birimYolu(ad)
	want := DilimIcerik(h)
	if cur, err := os.ReadFile(yol); err == nil && string(cur) == want {
		return false, nil
	}
	if err := os.MkdirAll(birimDizini, 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(yol, []byte(want), 0o644); err != nil {
		return false, err
	}
	restorecon(yol)
	// Eski `set-property --runtime` düşürücüsü dosyadaki YENİ değeri ezerdi: sıfırla.
	_ = os.RemoveAll(filepath.Join(runKontrolDizini, ad+".d"))
	return true, nil
}

// canliUygula: etkin havuz dilimine limitleri anında (süreç öldürmeden) uygular.
func canliUygula(ctx context.Context, h Havuz) {
	ad := bayidilim.BayiDilim(h.BayiID)
	if ad == "" || !dilimAktif(ctx, ad) {
		return // pasif dilim: sonraki başlatmada dosyadaki değerleri alır
	}
	args := append([]string{"set-property", "--runtime", ad}, CanliOzellikArgs(h)...)
	if out, err := komutCalistir(ctx, "systemctl", args...); err != nil {
		log.Printf("bayikaynak: %s canlı uygulanamadı: %s: %v", ad, strings.TrimSpace(string(out)), err)
	}
}

// bayiDilimiSil: havuz dilimini kaldırır — YALNIZ içinde süreç yoksa (pasifse). Etkinse
// limitsiz kalıba (stub) çevrilir ve sonraki turda silinir: çalışan sitelerin cgroup'u
// altından çekilmez. true = silindi.
func bayiDilimiSil(ctx context.Context, id int64, r *Rapor) (silindi, reloadGerek bool) {
	ad := bayidilim.BayiDilim(id)
	yol := birimYolu(ad)
	if _, err := os.Lstat(yol); err != nil {
		return false, false
	}
	if dilimAktif(ctx, ad) && !bosDilimiDurdur(ctx, ad) {
		// İçinde süreç var: limitsiz kalıba çevrilir, boşalınca sonraki turda silinir.
		stub := Havuz{BayiID: id}
		if d, err := dilimKur(stub); err != nil {
			r.hata("%s limitsiz kalıba çevrilemedi: %v", ad, err)
		} else if d {
			r.DilimYazilan = append(r.DilimYazilan, id)
			return false, true
		}
		return false, false
	}
	if err := os.Remove(yol); err != nil {
		r.hata("%s silinemedi: %v", ad, err)
		return false, false
	}
	_ = os.RemoveAll(filepath.Join(runKontrolDizini, ad+".d"))
	r.DilimSilinen = append(r.DilimSilinen, ad)
	return true, true
}

// imzaliHavuzDilimi: dosya panelin yazdığı havuz dilimi mi?
func imzaliHavuzDilimi(yol string) bool {
	b, err := os.ReadFile(yol)
	return err == nil && strings.HasPrefix(string(b), DilimImza)
}

// Uzlastir: TÜM bayilerin havuz dilimlerini, üyelik kayıtlarını ve FPM/kuyruk drop-in'lerini
// veritabanıyla eşitler. İdempotent; çalışan hiçbir servisi yeniden başlatmaz. Bir bayinin
// hatası diğerlerini durdurmaz (Rapor.Hatalar). Veritabanı okunamazsa HİÇBİR ŞEY değiştirmez.
func Uzlastir(ctx context.Context, db *sql.DB) (Rapor, error) {
	mu.Lock()
	defer mu.Unlock()
	var r Rapor
	havuzlar, err := HavuzlariOku(ctx, db, 0)
	if err != nil {
		return r, err
	}
	uyeler, err := UyeleriOku(ctx, db, 0, 0)
	if err != nil {
		return r, err
	}
	reload := false
	var canli []Havuz

	// 1) Havuz dilimleri.
	ids := make([]int64, 0, len(havuzlar))
	for id := range havuzlar {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	aktif := map[int64]bool{}
	for _, id := range ids {
		h := havuzlar[id]
		if !h.DilimAktif() {
			if _, rl := bayiDilimiSil(ctx, id, &r); rl {
				reload = true
			}
			continue
		}
		aktif[id] = true
		r.Havuzlar++
		d, err := dilimKur(h)
		if err != nil {
			r.hata("bayi %d dilimi yazılamadı: %v", id, err)
			delete(aktif, id) // dilim yoksa üyelik de kurulmaz (hosting eski adla kalır)
			r.Havuzlar--
			continue
		}
		if d {
			r.DilimYazilan = append(r.DilimYazilan, id)
			reload = true
		}
		canli = append(canli, h)
	}
	// Veritabanında olmayan bayinin artık dilimleri (bayi silindi).
	if ents, err := os.ReadDir(birimDizini); err == nil {
		for _, e := range ents {
			id, ok := bayidilim.BayiDilimindenID(e.Name())
			if !ok || e.IsDir() {
				continue
			}
			if _, var_ := havuzlar[id]; var_ || !imzaliHavuzDilimi(birimYolu(e.Name())) {
				continue
			}
			if _, rl := bayiDilimiSil(ctx, id, &r); rl {
				reload = true
			}
		}
	}

	// 2) Üyelik kayıtları.
	istenen := map[string]Uye{}
	for _, u := range uyeler {
		if aktif[u.BayiID] {
			istenen[u.SK] = u
		}
	}
	r.Uyeler = len(istenen)
	skler := make([]string, 0, len(istenen))
	for sk := range istenen {
		skler = append(skler, sk)
	}
	sort.Strings(skler)
	for _, sk := range skler {
		u := istenen[sk]
		d, err := bayidilim.UyelikYaz(sk, u.BayiID)
		if err != nil {
			r.hata("üyelik yazılamadı (%s): %v", sk, err)
			continue
		}
		if d {
			r.UyelikEklenen = append(r.UyelikEklenen, u)
		}
	}
	for sk := range bayidilim.UyelikListe() {
		if _, var_ := istenen[sk]; var_ {
			continue
		}
		if d, err := bayidilim.UyelikSil(sk); err != nil {
			r.hata("üyelik silinemedi (%s): %v", sk, err)
		} else if d {
			r.UyelikKalkan = append(r.UyelikKalkan, sk)
		}
	}
	sort.Strings(r.UyelikKalkan)

	// 3) Drop-in'ler (FPM + Laravel kuyruk işleyicisi). Birimi olmayan hosting için dosya
	//    YARATILMAZ: EnableTenantFPM birimi yazarken bayidilim.DropinUygula'yı çağırır.
	for _, sk := range skler {
		u := istenen[sk]
		for _, birim := range uyeBirimleri(u) {
			if _, err := os.Stat(birimYolu(birim)); err != nil {
				continue
			}
			d, err := bayidilim.DropinUygula(birimDizini, birim, sk)
			if err != nil {
				r.hata("drop-in %s: %v", birim, err)
				continue
			}
			if d {
				r.Dropin++
				reload = true
			}
		}
	}
	if n := fazlaDropinTemizle(&r); n > 0 {
		r.Dropin += n
		reload = true
	}

	// 4) systemd'ye bildir + canlı limit.
	if reload {
		if err := daemonReload(ctx); err != nil {
			r.hata("%v", err)
		}
	}
	for _, h := range canli {
		canliUygula(ctx, h)
	}
	if r.Degisti() {
		log.Printf("bayikaynak: uzlaştırma — %d havuz, %d üye; dilim yazılan=%d silinen=%d, üyelik +%d/-%d, drop-in %d, hata %d",
			r.Havuzlar, r.Uyeler, len(r.DilimYazilan), len(r.DilimSilinen), len(r.UyelikEklenen), len(r.UyelikKalkan), r.Dropin, len(r.Hatalar))
	}
	return r, nil
}

// uyeBirimleri: üyenin Slice=girginos-<sk>.slice taşıyan birimleri (FPM + Laravel kuyruğu).
func uyeBirimleri(u Uye) []string {
	return []string{
		"php-fpm-" + u.SK + ".service",
		"girginos-laravel-queue-" + strconv.FormatInt(u.DomainID, 10) + ".service",
	}
}

// fazlaDropinTemizle: üyeliği artık tutmayan drop-in'leri siler (hosting havuzdan çıktı /
// başka bayiye geçti / silindi). Döner: silinen sayısı.
func fazlaDropinTemizle(r *Rapor) int {
	dizinler, _ := filepath.Glob(filepath.Join(birimDizini, "*.service.d", bayidilim.DropinAd))
	n := 0
	for _, yol := range dizinler {
		b, err := os.ReadFile(yol)
		if err != nil {
			continue
		}
		dilim := ""
		for _, ln := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(ln), "Slice="); ok {
				dilim = strings.TrimSpace(v)
			}
		}
		id, sk, ok := bayidilim.UyeDilimindenAyir(dilim)
		if ok && bayidilim.Uyelik(sk) == id {
			continue // güncel
		}
		if err := os.Remove(yol); err != nil {
			r.hata("fazla drop-in silinemedi (%s): %v", yol, err)
			continue
		}
		_ = os.Remove(filepath.Dir(yol)) // boşsa kalkar
		n++
	}
	return n
}

// HostingUye: TEK hostingin üyeliğini veritabanına göre kurar — hosting oluşturma / plan
// değişimi / yeniden uygulama yolunda, per-tenant FPM başlamadan ÖNCE çağrılır (yeni hosting
// ilk başlatmada havuzun içinde doğsun). Havuz dilimini gerekirse yazar; drop-in'i
// EnableTenantFPM yazar. true = üyelik/dilim değişti.
func HostingUye(ctx context.Context, db *sql.DB, domainID int64) (bool, error) {
	mu.Lock()
	defer mu.Unlock()
	var sk string
	var rid int64
	if err := db.QueryRowContext(ctx,
		`SELECT sistem_kullanici, COALESCE(reseller_id,0) FROM domains WHERE id=?`, domainID).Scan(&sk, &rid); err != nil {
		return false, err
	}
	if !bayidilim.GecerliSK(sk) {
		return false, nil
	}
	var h Havuz
	if rid > 0 {
		hv, err := HavuzlariOku(ctx, db, rid)
		if err != nil {
			return false, err
		}
		h = hv[rid]
	}
	if rid <= 0 || !h.DilimAktif() {
		d, err := bayidilim.UyelikSil(sk)
		return d, err
	}
	reload := false
	d, err := dilimKur(h)
	if err != nil {
		return false, err // havuz dilimi yazılamadıysa üye yapma (eski adla kalsın)
	}
	reload = d
	u, err := bayidilim.UyelikYaz(sk, rid)
	if err != nil {
		return reload, err
	}
	if reload {
		if err := daemonReload(ctx); err != nil {
			return true, err
		}
		canliUygula(ctx, h)
	}
	return d || u, nil
}

// ArtikTemizle: üyelik değişince geride kalan ESKİ plan dilimi dosyalarını kaldırır — YALNIZ
// dilim pasifse (içinde süreç yoksa). Etkin dilim (FPM henüz yeni dilime taşınmadı) bekler.
//
//	üye hosting   → girginos-<sk>.slice               (eski ad) artık gereksiz
//	üye değil     → girginos-bayi<N>-<sk>.slice       (havuzdan çıktı / başka bayiye geçti)
//
// Yalnız panelin imzasını ("# GirginOSPanel per-domain resource slice — <sk>") taşıyanlara
// dokunur. Döner: silinen dilimler.
func ArtikTemizle(ctx context.Context) []string {
	mu.Lock()
	defer mu.Unlock()
	ents, err := os.ReadDir(birimDizini)
	if err != nil {
		return nil
	}
	var silinen []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		ad := e.Name()
		var sk string
		if id, s, ok := bayidilim.UyeDilimindenAyir(ad); ok {
			sk = s
			if bayidilim.Uyelik(sk) == id {
				continue // geçerli üye dilimi
			}
		} else if strings.HasPrefix(ad, bayidilim.Onek+"c_") && strings.HasSuffix(ad, ".slice") {
			sk = strings.TrimSuffix(strings.TrimPrefix(ad, bayidilim.Onek), ".slice")
			if !bayidilim.GecerliSK(sk) || bayidilim.Uyelik(sk) == 0 {
				continue // üye değil → eski ad meşru
			}
		} else {
			continue
		}
		b, err := os.ReadFile(birimYolu(ad))
		if err != nil || !strings.HasPrefix(string(b), "# GirginOSPanel per-domain resource slice — "+sk+"\n") {
			continue // yabancı dosya
		}
		if dilimAktif(ctx, ad) && !bosDilimiDurdur(ctx, ad) {
			continue // süreçler hâlâ içinde: yeniden başlatma bekleniyor
		}
		if err := os.Remove(birimYolu(ad)); err != nil {
			log.Printf("bayikaynak: artık dilim silinemedi %s: %v", ad, err)
			continue
		}
		_ = os.RemoveAll(filepath.Join(runKontrolDizini, ad+".d"))
		silinen = append(silinen, ad)
	}
	if len(silinen) > 0 {
		_ = daemonReload(ctx)
		for _, ad := range silinen {
			_, _ = komutCalistir(ctx, "systemctl", "reset-failed", ad)
		}
		log.Printf("bayikaynak: %d artık dilim kaldırıldı: %v", len(silinen), silinen)
	}
	// Süreçsiz kalan havuz dilimi stub'larını da süpür (bayi silindi / havuz kapatıldı).
	return silinen
}

// BayiTemizle: bayi silindikten sonra havuz dilimini, usage satırlarını ve proje kotası
// sınırını kaldırır. Hosting kalıntıları Uzlastir/ArtikTemizle'nin işidir.
func BayiTemizle(ctx context.Context, db *sql.DB, bayiID int64) {
	if !bayidilim.GecerliBayiID(bayiID) {
		return
	}
	mu.Lock()
	var r Rapor
	_, rl := bayiDilimiSil(ctx, bayiID, &r)
	if rl {
		_ = daemonReload(ctx)
	}
	mu.Unlock()
	for _, h := range r.Hatalar {
		log.Printf("bayikaynak: bayi %d temizliği: %s", bayiID, h)
	}
	if db != nil {
		if _, err := db.ExecContext(ctx, `DELETE FROM bayi_kaynak_kullanim WHERE bayi_id=?`, bayiID); err != nil {
			log.Printf("bayikaynak: bayi %d kullanım satırları silinemedi: %v", bayiID, err)
		}
	}
	projeSiniriKaldir(ctx, bayiID)
}

// ── Bekleyen (yeniden başlatma gerektiren) hostingler ─────────────────────────────────

// BekleyenFPM: üyeliği değişmiş ama çalışan FPM'i henüz doğru dilimde olmayan hostingler.
// systemctl'in YAPILANDIRILMIŞ Slice= değeri ile gerçek ControlGroup yolunu karşılaştırır
// (daemon-reload yapılandırmayı günceller, çalışan servisi taşımaz).
func BekleyenFPM(ctx context.Context, skler []string) []string {
	var bekleyen []string
	for i := 0; i < len(skler); i += 80 {
		son := i + 80
		if son > len(skler) {
			son = len(skler)
		}
		args := []string{"show", "-p", "Id", "-p", "ActiveState", "-p", "Slice", "-p", "ControlGroup"}
		sira := map[string]string{}
		for _, sk := range skler[i:son] {
			if !bayidilim.GecerliSK(sk) {
				continue
			}
			u := "php-fpm-" + sk + ".service"
			sira[u] = sk
			args = append(args, u)
		}
		if len(sira) == 0 {
			continue
		}
		out, err := komutCalistir(ctx, "systemctl", args...)
		if err != nil {
			continue
		}
		for _, blok := range strings.Split(string(out), "\n\n") {
			ozel := map[string]string{}
			for _, ln := range strings.Split(blok, "\n") {
				if k, v, ok := strings.Cut(ln, "="); ok {
					ozel[k] = v
				}
			}
			sk, ok := sira[ozel["Id"]]
			if !ok || ozel["ActiveState"] != "active" || ozel["Slice"] == "" || ozel["ControlGroup"] == "" {
				continue
			}
			if !strings.Contains(ozel["ControlGroup"]+"/", "/"+ozel["Slice"]+"/") {
				bekleyen = append(bekleyen, sk)
			}
		}
	}
	sort.Strings(bekleyen)
	return bekleyen
}
