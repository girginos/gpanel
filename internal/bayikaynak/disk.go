package bayikaynak

// disk.go — bayi disk havuzu: XFS PROJE kotası (sert sınır = havuz).
//
// Neden proje kotası: hostingler zaten kullanıcı kotası (uquota, hosting başına) altında;
// bayinin TOPLAMI için kullanıcı kotası yetmez. XFS proje kotası bir dizin ağacını (bayinin
// hosting ev dizinleri) tek sayaçta toplar; sert sınır dolunca yazma EDQUOT ile düşer.
//
// 🔴 Kök dosya sistemi kotası ancak MOUNT anında açılır; canlı remount ile açılamaz. Bu yüzden:
//   - proje kimlikleri pquota KAPALIYKEN de atanabilir (VM-C'de ölçüldü) → sunucu yeniden
//     başladığında XFS quotacheck bu kimlikleri sayar, sınırlar açılışta uygulanır;
//   - kernel komut satırı (rootflags) YALNIZ yöneticinin açık eylemiyle değişir
//     (Etkinlestir uygula=true); panel bunu kendiliğinden ASLA yapmaz;
//   - pquota yokken sınır UYGULANMAZ: kullanım ölçülür ve havuz dolunca uyarı verilir
//     ("uyar ama engelleme"; fail-open DEĞİL — durum API'de açıkça "olcum_uyari").

import (
	"context"
	"database/sql"
	"errors"
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

// ProjeTaban: bayi proje kimliği = ProjeTaban + bayi kimliği. Docker/podman (1..) ve elle
// kullanılan küçük kimliklerle çakışmaz. Panel YALNIZ bu aralıktaki projelere dokunur.
const ProjeTaban int64 = 3_000_000

// ProjeKimligi: bayinin XFS proje kimliği; geçersiz bayi → 0.
func ProjeKimligi(bayiID int64) int64 {
	if !bayidilim.GecerliBayiID(bayiID) {
		return 0
	}
	return ProjeTaban + bayiID
}

// ProjedenBayi: panel aralığındaki proje kimliğinden bayi kimliği.
func ProjedenBayi(proje int64) (int64, bool) {
	b := proje - ProjeTaban
	if !bayidilim.GecerliBayiID(b) {
		return 0, false
	}
	return b, true
}

// Dış dünya dikişleri (testler değiştirir).
var (
	evKoku            = "/home"
	procCmdline       = "/proc/cmdline"
	projeIsaretDizini = "/etc/girginospanel/bayi-kaynak/proje"
	diskSentinelYolu  = "/etc/girginospanel/reboot-required-prjquota"

	// uzunKomut: büyük bir ev ağacını gezen proje atamaları için (saatler sürebilir; kısa
	// 2 dk'lık sınır işi hep yarıda keser ve her turda baştan başlatırdı).
	uzunKomut = func(ctx context.Context, ad string, arg ...string) ([]byte, error) {
		c, iptal := context.WithTimeout(ctx, 3*time.Hour)
		defer iptal()
		return exec.CommandContext(c, ad, arg...).CombinedOutput()
	}

	// yolBul: komutun yolunu bulur (testler ana makineden bağımsız olsun diye değiştirir).
	yolBul = exec.LookPath

	// diskMu: proje atamaları tek tek çalışır (iki tur aynı ağacı aynı anda gezmesin).
	diskMu sync.Mutex
)

// DiskDurum: disk havuzu uygulanabilirliği.
type DiskDurum struct {
	FS               string `json:"fs"`
	Mount            string `json:"mount"`
	XFS              bool   `json:"xfs"`
	HesapAcik        bool   `json:"proje_hesap_acik"`   // pquota accounting
	ZorlamaAcik      bool   `json:"proje_zorlama_acik"` // pquota enforcement
	BootHazir        bool   `json:"boot_hazir"`         // varsayılan kernel girdisinde pquota var
	YenidenBaslatma  bool   `json:"yeniden_baslatma_bekleniyor"`
	Mod              string `json:"mod"` // sert | yeniden_baslatma_bekleniyor | olcum_uyari | desteklenmiyor
	Mesaj            string `json:"mesaj"`
	GrubbyVar        bool   `json:"grubby_var"`
	MevcutRootflags  string `json:"mevcut_rootflags"`
	OnerilenRootflag string `json:"onerilen_rootflags"`
	Elle             bool   `json:"elle"` // otomatik değiştirilemez (ayrı mount, grup kotası vb.)
}

// Mod sabitleri.
const (
	ModSert           = "sert"
	ModBekliyor       = "yeniden_baslatma_bekleniyor"
	ModOlcumUyari     = "olcum_uyari"
	ModDesteklenmiyor = "desteklenmiyor"
)

// findmntAyristir: "findmnt -n -o TARGET,FSTYPE,OPTIONS" çıktısı → (hedef, fs, seçenekler).
func findmntAyristir(out string) (hedef, fs, secenek string, ok bool) {
	f := strings.Fields(strings.TrimSpace(out))
	if len(f) < 3 {
		return "", "", "", false
	}
	return f[0], f[1], f[2], true
}

// projeDurumuAyristir: `xfs_quota -x -c 'state -p'` çıktısından (hesap, zorlama).
// Çıktı her kota türünü ayrı bölümde verir; yalnız "Project quota state" bölümü okunur.
func projeDurumuAyristir(out string) (hesap, zorlama bool) {
	bolum := false
	for _, ln := range strings.Split(out, "\n") {
		t := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(t, "Project quota state"):
			bolum = true
		case strings.Contains(t, "quota state on"):
			bolum = false
		case bolum && strings.HasPrefix(t, "Accounting:"):
			hesap = strings.Contains(t, "ON")
		case bolum && strings.HasPrefix(t, "Enforcement:"):
			zorlama = strings.Contains(t, "ON")
		}
	}
	return
}

// rootflagsAl: bir kernel komut satırından rootflags= değeri ("" = yok).
func rootflagsAl(cmdline string) string {
	v := ""
	for _, t := range strings.Fields(cmdline) {
		if x, ok := strings.CutPrefix(t, "rootflags="); ok {
			v = x
		}
	}
	return v
}

func bayrakVar(bayraklar []string, adaylar ...string) bool {
	for _, b := range bayraklar {
		for _, a := range adaylar {
			if b == a {
				return true
			}
		}
	}
	return false
}

// OnerilenRootflags: mevcut rootflags'e proje kotasını EKLER; çalışan montajda zaten açık
// olan kullanıcı kotasını korur (yoksa sonraki açılışta sessizce düşerdi). Grup kotası varsa
// (birlikte kullanım denenmedi) elle=true döner.
func OnerilenRootflags(mevcut, mountSecenekleri string) (yeni string, elle bool) {
	var bay []string
	for _, b := range strings.Split(mevcut, ",") {
		if b = strings.TrimSpace(b); b != "" {
			bay = append(bay, b)
		}
	}
	if bayrakVar(bay, "gquota", "grpquota", "gqnoenforce") || strings.Contains(","+mountSecenekleri+",", ",grpquota,") {
		return "", true
	}
	var cikti []string
	for _, b := range bay {
		if b == "pqnoenforce" { // yalnız hesap tutan eski bayrak → zorlayanla değişir
			continue
		}
		cikti = append(cikti, b)
	}
	if !bayrakVar(cikti, "uquota", "usrquota", "quota") {
		cikti = append(cikti, "uquota") // panelin kullanıcı kotası da şart (installer aynısını yazar)
	}
	if !bayrakVar(cikti, "pquota", "prjquota") {
		cikti = append(cikti, "pquota")
	}
	return strings.Join(cikti, ","), false
}

// bootArgs: varsayılan kernel girdisinin argümanları (grubby); alınamazsa ok=false.
func bootArgs(ctx context.Context) (string, bool) {
	out, err := komutCalistir(ctx, "grubby", "--info=DEFAULT")
	if err != nil {
		return "", false
	}
	for _, ln := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(ln), "args="); ok {
			return strings.Trim(v, `"`), true
		}
	}
	return "", false
}

// DiskDurumu: proje kotasının bu sunucuda durumu (yalnız okur).
func DiskDurumu(ctx context.Context) DiskDurum {
	d := DiskDurum{Mod: ModDesteklenmiyor}
	out, err := komutCalistir(ctx, "findmnt", "-n", "-o", "TARGET,FSTYPE,OPTIONS", "-T", evKoku)
	hedef, fs, secenek, ok := findmntAyristir(string(out))
	if err != nil || !ok {
		d.Mesaj = "ev dizininin dosya sistemi belirlenemedi"
		return d
	}
	d.Mount, d.FS = hedef, fs
	if fs != "xfs" {
		d.Mesaj = "disk havuzu yalnız XFS dosya sisteminde uygulanabilir (bulundu: " + fs + ")"
		return d
	}
	d.XFS = true
	qo, _ := komutCalistir(ctx, "xfs_quota", "-x", "-c", "state -p", hedef)
	d.HesapAcik, d.ZorlamaAcik = projeDurumuAyristir(string(qo))
	if _, err := yolBul("grubby"); err == nil {
		d.GrubbyVar = true
	}
	cmd, _ := os.ReadFile(procCmdline)
	d.MevcutRootflags = rootflagsAl(string(cmd))
	boot, bootOK := bootArgs(ctx)
	if bootOK {
		bf := strings.Split(rootflagsAl(boot), ",")
		d.BootHazir = bayrakVar(bf, "pquota", "prjquota")
	}
	yeni, elle := OnerilenRootflags(d.MevcutRootflags, secenek)
	d.OnerilenRootflag = yeni
	switch {
	case d.ZorlamaAcik:
		d.Mod, d.Mesaj = ModSert, "proje kotası etkin: bayi disk havuzu sert sınırla uygulanıyor"
	case d.BootHazir:
		d.Mod, d.YenidenBaslatma = ModBekliyor, true
		d.Mesaj = "kernel komut satırı hazır; sunucu yeniden başlatılınca sert sınır devreye girer"
	default:
		d.Mod = ModOlcumUyari
		d.Mesaj = "proje kotası kapalı: disk havuzu UYGULANMIYOR, yalnız ölçülüyor ve dolunca uyarı veriliyor"
		if hedef != "/" {
			d.Elle = true
			d.Mesaj += " (ev dizini ayrı bir bağlama noktasında: fstab'a prjquota elle eklenmeli)"
		} else if elle {
			d.Elle = true
			d.Mesaj += " (grup kotası açık: rootflags elle düzenlenmeli)"
		} else if !d.GrubbyVar {
			d.Elle = true
			d.Mesaj += " (grubby yok: rootflags elle düzenlenmeli)"
		}
	}
	return d
}

// EtkinlestirSonuc: proje kotasını açma eyleminin (ya da kuru çalıştırmasının) özeti.
type EtkinlestirSonuc struct {
	Uygulandi       bool       `json:"uygulandi"`
	Komut           string     `json:"komut"`
	MevcutRootflags string     `json:"mevcut_rootflags"`
	YeniRootflags   string     `json:"yeni_rootflags"`
	Mod             string     `json:"mod"`
	Mesaj           string     `json:"mesaj"`
	AsimdakiBayiler []AsimBayi `json:"asimdaki_bayiler,omitempty"` // kullanım zaten havuzu aşan bayiler (API doldurur)
}

// Etkinlestir: rootflags'e pquota ekler (grubby). uygula=false KURU ÇALIŞTIRMA (hiçbir şey
// değişmez). Yalnız yöneticinin açık eylemi çağırır (CLI `bayi-disk-kotasi --uygula` ya da
// POST /bayi-kaynak/disk-kotasi/etkinlestir); panel bunu kendiliğinden ASLA çağırmaz.
// Etkisi bir SONRAKİ yeniden başlatmada başlar; sentinel yazılır.
func Etkinlestir(ctx context.Context, uygula bool) (EtkinlestirSonuc, error) {
	d := DiskDurumu(ctx)
	s := EtkinlestirSonuc{MevcutRootflags: d.MevcutRootflags, YeniRootflags: d.OnerilenRootflag, Mod: d.Mod}
	switch {
	case !d.XFS:
		return s, errors.New(d.Mesaj)
	case d.ZorlamaAcik:
		s.Mesaj = "proje kotası zaten etkin"
		return s, nil
	case d.BootHazir:
		s.Mesaj = "kernel komut satırı zaten hazır; yeniden başlatma bekleniyor"
		return s, nil
	case d.Elle:
		return s, errors.New(d.Mesaj)
	}
	s.Komut = komutMetni("grubby", "--update-kernel=ALL", "--args=rootflags="+d.OnerilenRootflag)
	if !uygula {
		s.Mesaj = "KURU ÇALIŞTIRMA: komut çalıştırılmadı. Uygulamak için açıkça onaylayın."
		return s, nil
	}
	if out, err := komutCalistir(ctx, "grubby", "--update-kernel=ALL", "--args=rootflags="+d.OnerilenRootflag); err != nil {
		return s, fmt.Errorf("grubby: %s: %w", strings.TrimSpace(string(out)), err)
	}
	// Geri oku: komut "başarılı" dönüp değişiklik yapmamış olabilir.
	boot, ok := bootArgs(ctx)
	if !ok || !bayrakVar(strings.Split(rootflagsAl(boot), ","), "pquota") {
		return s, errors.New("grubby çalıştı ama varsayılan kernel girdisinde rootflags doğrulanamadı")
	}
	s.Uygulandi, s.Mod = true, ModBekliyor
	s.Mesaj = "rootflags yazıldı; sınır, sunucu yeniden başlatılınca devreye girer (açılışta quotacheck süresi kadar uzayabilir)"
	if err := os.MkdirAll(filepath.Dir(diskSentinelYolu), 0o755); err == nil {
		_ = os.WriteFile(diskSentinelYolu, []byte("bayi disk havuzu: rootflags=pquota yazıldı, yeniden başlatma bekleniyor\n"+time.Now().Format(time.RFC3339)+"\n"), 0o644)
	}
	log.Printf("bayikaynak: proje kotası rootflags yazıldı (%s → %s); yeniden başlatma gerekli", d.MevcutRootflags, d.OnerilenRootflag)
	return s, nil
}

// ── Proje ataması + sınır ────────────────────────────────────────────────────────

func evYolu(sk string) string { return filepath.Join(evKoku, sk) }

func projeIsaretiOku(sk string) int64 {
	b, err := os.ReadFile(filepath.Join(projeIsaretDizini, sk))
	if err != nil {
		return 0
	}
	v, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if _, ok := ProjedenBayi(v); !ok {
		return 0
	}
	return v
}

func projeIsaretiYaz(sk string, proje int64) error {
	yol := filepath.Join(projeIsaretDizini, sk)
	if proje <= 0 {
		if err := os.Remove(yol); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(projeIsaretDizini, 0o755); err != nil {
		return err
	}
	return os.WriteFile(yol, []byte(strconv.FormatInt(proje, 10)+"\n"), 0o644)
}

// projeAta komutları (argv; kabuk yok). sk ve kimlikler doğrulanmış girdiden gelir.
func projeAtaArgs(mount, ev string, proje int64) []string {
	return []string{"-x", "-c", "project -s -p " + ev + " " + strconv.FormatInt(proje, 10), mount}
}

func projeTemizleArgs(mount, ev string, proje int64) []string {
	return []string{"-x", "-c", "project -C -p " + ev + " " + strconv.FormatInt(proje, 10), mount}
}

// projeLimitArgs: sert sınır = havuz (MiB). Yumuşak sınır YOK (xfs yumuşak sınırı 7 gün sonra sert olur).
func projeLimitArgs(mount string, proje, diskMB int64) []string {
	if diskMB < 0 {
		diskMB = 0
	}
	return []string{"-x", "-c", fmt.Sprintf("limit -p bsoft=0 bhard=%dm %d", diskMB, proje), mount}
}

// raporSatir: `report -p -N -b` satırı → (proje, kullanılan KB, sert KB).
func projeRaporAyristir(out string) map[int64][2]int64 {
	m := map[int64][2]int64{}
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 4 {
			continue
		}
		ad := strings.TrimPrefix(f[0], "#")
		p, err := strconv.ParseInt(ad, 10, 64)
		if err != nil {
			continue
		}
		used, e1 := strconv.ParseInt(f[1], 10, 64)
		hard, e2 := strconv.ParseInt(f[3], 10, 64)
		if e1 != nil || e2 != nil {
			continue
		}
		m[p] = [2]int64{used, hard}
	}
	return m
}

// ProjeRapor: ProjeUygula özeti.
type ProjeRapor struct {
	Mod          string   `json:"mod"`
	Atanan       int      `json:"atanan"`
	Temizlenen   int      `json:"temizlenen"`
	SinirYazilan int      `json:"sinir_yazilan"`
	SinirKalkan  int      `json:"sinir_kalkan"`
	Atlanan      int      `json:"atlanan"`
	Hatalar      []string `json:"hatalar,omitempty"`
}

func (r *ProjeRapor) hata(f string, a ...any) {
	m := fmt.Sprintf(f, a...)
	log.Printf("bayikaynak: disk: %s", m)
	r.Hatalar = append(r.Hatalar, m)
}

// ProjeUygula: bayilerin disk havuzunu uygular (idempotent, süreç öldürmez):
//
//  1. her hostingin ev dizinini istenen projeye bağlar (bayi üyesi + disk havuzu>0 → bayi
//     projesi; değilse panel aralığındaki eski atama temizlenir). İşaret dosyası tamamlanmış
//     özyinelemeyi kaydeder: kesilen büyük bir ağaç bir sonraki turda baştan tamamlanır;
//  2. pquota ZORLAMASI açıksa her bayi projesine sert sınır (= disk_mb) yazılır; havuzu
//     kapatılan/silinen bayinin sınırı kaldırılır.
//
// bayiID>0 yalnız o bayinin hostinglerini işler (sınır temizliği hariç). Hata bir hostingi
// atlatır, kalanlar sürer.
func ProjeUygula(ctx context.Context, db *sql.DB, bayiID int64) (ProjeRapor, error) {
	return projeTur(ctx, db, bayiID, "")
}

// projeTur: ProjeUygula'nın çekirdeği. tekSK != "" ise yalnız o hosting işlenir ve sert
// sınır bölümü (genel) atlanır.
func projeTur(ctx context.Context, db *sql.DB, bayiID int64, tekSK string) (ProjeRapor, error) {
	diskMu.Lock()
	defer diskMu.Unlock()
	var r ProjeRapor
	d := DiskDurumu(ctx)
	r.Mod = d.Mod
	if !d.XFS {
		return r, nil
	}
	havuzlar, err := HavuzlariOku(ctx, db, 0)
	if err != nil {
		return r, err
	}
	tum, err := db.QueryContext(ctx, `SELECT COALESCE(reseller_id,0), sistem_kullanici FROM domains WHERE sistem_kullanici LIKE 'c\_%' ORDER BY id`)
	if err != nil {
		return r, fmt.Errorf("hostingler okunamadı: %w", err)
	}
	type hs struct {
		bayi int64
		sk   string
	}
	var liste []hs
	for tum.Next() {
		var x hs
		if tum.Scan(&x.bayi, &x.sk) == nil && bayidilim.GecerliSK(x.sk) {
			liste = append(liste, x)
		}
	}
	tamOkundu := tum.Err() == nil
	_ = tum.Close()
	// Silinen hostinglerin işaret dosyaları: hosting silme işareti kaldırmıyordu (VM doğrulaması
	// 2026-10-13). Yalnız tam turda ve hosting listesi eksiksiz okunduysa artıklar silinir.
	if tekSK == "" && tamOkundu {
		mevcut := make(map[string]bool, len(liste))
		for _, x := range liste {
			mevcut[x.sk] = true
		}
		if girdiler, err := os.ReadDir(projeIsaretDizini); err == nil {
			for _, g := range girdiler {
				if !g.IsDir() && bayidilim.GecerliSK(g.Name()) && !mevcut[g.Name()] {
					_ = os.Remove(filepath.Join(projeIsaretDizini, g.Name()))
				}
			}
		}
	}

	for _, x := range liste {
		if ctx.Err() != nil {
			r.hata("iptal: %v", ctx.Err())
			return r, nil
		}
		if tekSK != "" && x.sk != tekSK {
			continue
		}
		if bayiID > 0 && x.bayi != bayiID && projeIsaretiOku(x.sk) != ProjeKimligi(bayiID) {
			continue
		}
		istenen := int64(0)
		if h, ok := havuzlar[x.bayi]; ok && x.bayi > 0 && h.DiskAktif() {
			istenen = ProjeKimligi(x.bayi)
		}
		mevcut := projeIsaretiOku(x.sk)
		if istenen == mevcut {
			r.Atlanan++
			continue
		}
		ev := evYolu(x.sk)
		if st, err := os.Stat(ev); err != nil || !st.IsDir() {
			r.Atlanan++ // henüz kurulmamış / silinmiş
			continue
		}
		if istenen > 0 {
			if out, err := uzunKomut(ctx, "xfs_quota", projeAtaArgs(d.Mount, ev, istenen)...); err != nil {
				r.hata("%s proje %d atanamadı: %s: %v", x.sk, istenen, strings.TrimSpace(string(out)), err)
				continue
			}
			r.Atanan++
		} else {
			if out, err := uzunKomut(ctx, "xfs_quota", projeTemizleArgs(d.Mount, ev, mevcut)...); err != nil {
				r.hata("%s proje %d temizlenemedi: %s: %v", x.sk, mevcut, strings.TrimSpace(string(out)), err)
				continue
			}
			r.Temizlenen++
		}
		if err := projeIsaretiYaz(x.sk, istenen); err != nil {
			r.hata("%s proje işareti yazılamadı: %v", x.sk, err)
		}
	}

	// 2) Sert sınırlar — yalnız zorlama açıkken (kapalıyken `limit -p` "No such process" verir).
	if !d.ZorlamaAcik || tekSK != "" {
		return r, nil
	}
	out, err := komutCalistir(ctx, "xfs_quota", "-x", "-c", "report -p -N -b", d.Mount)
	if err != nil {
		r.hata("proje raporu okunamadı: %v", err)
		return r, nil
	}
	rapor := projeRaporAyristir(string(out))
	bayiler := make([]int64, 0, len(havuzlar))
	for id := range havuzlar {
		bayiler = append(bayiler, id)
	}
	sort.Slice(bayiler, func(i, j int) bool { return bayiler[i] < bayiler[j] })
	for _, id := range bayiler {
		h := havuzlar[id]
		p := ProjeKimligi(id)
		if !h.DiskAktif() {
			continue
		}
		want := h.DiskMB * 1024 // KB
		if cur, ok := rapor[p]; ok && cur[1] == want {
			continue
		}
		if o, err := komutCalistir(ctx, "xfs_quota", projeLimitArgs(d.Mount, p, h.DiskMB)...); err != nil {
			r.hata("bayi %d sınırı yazılamadı: %s: %v", id, strings.TrimSpace(string(o)), err)
			continue
		}
		r.SinirYazilan++
	}
	// Havuzu kapatılan / silinen bayi: kalan sert sınırı kaldır.
	for p, v := range rapor {
		id, ok := ProjedenBayi(p)
		if !ok || v[1] == 0 {
			continue
		}
		if h, var_ := havuzlar[id]; var_ && h.DiskAktif() {
			continue
		}
		if o, err := komutCalistir(ctx, "xfs_quota", projeLimitArgs(d.Mount, p, 0)...); err != nil {
			r.hata("bayi %d eski sınırı kaldırılamadı: %s: %v", id, strings.TrimSpace(string(o)), err)
			continue
		}
		r.SinirKalkan++
	}
	return r, nil
}

// projeSiniriKaldir: bayi silindi → proje sınırını kaldır (en iyi çaba; zorlama kapalıysa no-op).
func projeSiniriKaldir(ctx context.Context, bayiID int64) {
	p := ProjeKimligi(bayiID)
	if p == 0 {
		return
	}
	d := DiskDurumu(ctx)
	if !d.XFS || !d.ZorlamaAcik {
		return
	}
	if o, err := komutCalistir(ctx, "xfs_quota", projeLimitArgs(d.Mount, p, 0)...); err != nil {
		log.Printf("bayikaynak: bayi %d proje sınırı kaldırılamadı: %s: %v", bayiID, strings.TrimSpace(string(o)), err)
	}
}

// HostingDisk: tek hostingin proje atamasını günceller (hosting oluşturma / devir). Büyük
// bir ağaçta uzun sürebilir: çağıran arka planda çalıştırmalı.
func HostingDisk(ctx context.Context, db *sql.DB, domainID int64) error {
	var bayi int64
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(reseller_id,0) FROM domains WHERE id=?`, domainID).Scan(&bayi); err != nil {
		return err
	}
	var sk string
	if err := db.QueryRowContext(ctx, `SELECT sistem_kullanici FROM domains WHERE id=?`, domainID).Scan(&sk); err != nil {
		return err
	}
	if !bayidilim.GecerliSK(sk) {
		return nil
	}
	// Yalnız bu hosting: önceki sahibin kapsamı işaret dosyasından anlaşılır (eski proje temizlenir).
	_, err := projeTur(ctx, db, bayi, sk)
	return err
}

// ── Kullanım ölçümü (disk) ───────────────────────────────────────────────────────

// DiskKullanim: bayi başına kullanılan disk (KB) ve kaynağı.
type DiskKullanim struct {
	KullanilanKB int64
	Kaynak       string // proje | kullanici_kotasi | veritabani
}

func kullaniciRaporAyristir(out string) map[string]int64 {
	m := map[string]int64{}
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 4 || !bayidilim.GecerliSK(f[0]) {
			continue
		}
		if v, err := strconv.ParseInt(f[1], 10, 64); err == nil {
			m[f[0]] = v
		}
	}
	return m
}

// DiskKullanimlari: her bayinin disk kullanımı. Öncelik: proje sayacı (pquota hesap açık ve
// bayinin projesi atanmış) → hosting kullanıcı kotası toplamı → veritabanı boyut_kb toplamı.
func DiskKullanimlari(ctx context.Context, db *sql.DB, bayiler []int64) map[int64]DiskKullanim {
	out := map[int64]DiskKullanim{}
	if len(bayiler) == 0 {
		return out
	}
	d := DiskDurumu(ctx)
	var proje map[int64][2]int64
	var kullanici map[string]int64
	if d.XFS {
		if d.HesapAcik {
			if o, err := komutCalistir(ctx, "xfs_quota", "-x", "-c", "report -p -N -b", d.Mount); err == nil {
				proje = projeRaporAyristir(string(o))
			}
		}
		if o, err := komutCalistir(ctx, "xfs_quota", "-x", "-c", "report -u -N -b", d.Mount); err == nil {
			kullanici = kullaniciRaporAyristir(string(o))
		}
	}
	for _, id := range bayiler {
		if v, ok := proje[ProjeKimligi(id)]; ok {
			out[id] = DiskKullanim{v[0], "proje"}
			continue
		}
		uyeler, err := UyeleriOku(ctx, db, id, 0)
		if err != nil {
			continue
		}
		if len(kullanici) > 0 {
			var top int64
			for _, u := range uyeler {
				top += kullanici[u.SK]
			}
			out[id] = DiskKullanim{top, "kullanici_kotasi"}
			continue
		}
		var kb int64
		_ = db.QueryRowContext(ctx, `SELECT COALESCE(SUM(boyut_kb),0) FROM domains WHERE reseller_id=?`, id).Scan(&kb)
		out[id] = DiskKullanim{kb, "veritabani"}
	}
	return out
}
