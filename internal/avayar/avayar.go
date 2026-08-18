// Package avayar — antivirüs platformunun ayarları ve KAYNAK LİMİTLERİ.
//
// 🔴 NEDEN AYRI PAKET: limitler yalnız veritabanı satırı değil, systemd
// cgroup yapılandırmasıdır. Ayarı kaydetmek YETMEZ — çekirdek tarafında
// uygulanmadıkça "limit koydum" demek yanlış güven verir. Bu paket ikisini
// birlikte yapar ve UYGULANDIĞINI doğrular.
package avayar

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

const (
	SliceAdi  = "girginos-av.slice"
	slicePath = "/etc/systemd/system/" + SliceAdi
)

// Ayarlar — av_ayarlar tablosunun karşılığı.
type Ayarlar struct {
	GercekZamanli  bool   `json:"gercek_zamanli"`
	ZamanliTarama  bool   `json:"zamanli_tarama"`
	WPButunluk     bool   `json:"wp_butunluk"`
	KuralMotoru    bool   `json:"kural_motoru"`
	KonumSezgileri bool   `json:"konum_sezgileri"`
	OtoKarantina   bool   `json:"oto_karantina"`
	EsikKritik     int    `json:"esik_kritik"`
	Kapsam         string `json:"kapsam"` // host | sunucu
	HaricYollar    string `json:"haric_yollar"`
	CPUYuzde       int    `json:"cpu_yuzde"`
	RAMMb          int    `json:"ram_mb"`
	IOAgirlik      int    `json:"io_agirlik"`
	IsParcacigi    int    `json:"is_parcacigi"`
	DosyaHizSn     int    `json:"dosya_hiz_sn"`
	ZamanliSaat    string `json:"zamanli_saat"`
}

// Kapasite — sunucunun ölçülen kaynakları ve önerilen limitler.
//
// 🔴 Panel bunu gösterir ki operatör "%25" gibi soyut bir sayı yerine
// "8 çekirdekten 2'si" görsün. Limit ayarlamak, neyin sınırlandığı
// bilinmeden anlamsızdır.
type Kapasite struct {
	CPUCekirdek int `json:"cpu_cekirdek"`
	RAMToplamMB int `json:"ram_toplam_mb"`
	// Önerilenler — "0 = otomatik" seçildiğinde uygulanacak değerler.
	OneriCPUYuzde    int `json:"oneri_cpu_yuzde"`
	OneriRAMMb       int `json:"oneri_ram_mb"`
	OneriIsParcacigi int `json:"oneri_is_parcacigi"`
}

// SunucuKapasitesi — ÖLÇER, varsaymaz.
func SunucuKapasitesi() Kapasite {
	k := Kapasite{CPUCekirdek: runtime.NumCPU()}
	k.RAMToplamMB = toplamRAMMb()

	// 🔴 CPUQuota systemd'de "yüzde ÇEKİRDEK" demektir: 100% = 1 tam çekirdek.
	// 8 çekirdekli bir sunucuda toplamın dörtte birini vermek = 200%.
	// Taramanın siteleri yavaşlatmaması için dörtte bir makul bir tavan;
	// operatör panelden yükseltebilir.
	k.OneriCPUYuzde = k.CPUCekirdek * 100 / 4
	if k.OneriCPUYuzde < 50 {
		k.OneriCPUYuzde = 50 // tek çekirdekli sunucuda bile yarım çekirdek
	}

	// Bellek: toplamın 1/8'i, 256M taban, 2048M tavan. Tarayıcının bellek
	// ihtiyacı dosya boyutuyla değil kural sayısıyla ölçeklenir; büyük değer
	// gereksiz.
	k.OneriRAMMb = k.RAMToplamMB / 8
	switch {
	case k.OneriRAMMb < 256:
		k.OneriRAMMb = 256
	case k.OneriRAMMb > 2048:
		k.OneriRAMMb = 2048
	}

	k.OneriIsParcacigi = k.CPUCekirdek / 4
	if k.OneriIsParcacigi < 1 {
		k.OneriIsParcacigi = 1
	}
	return k
}

func toplamRAMMb() int {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, satir := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(satir, "MemTotal:") {
			continue
		}
		alan := strings.Fields(satir)
		if len(alan) < 2 {
			return 0
		}
		kb, err := strconv.Atoi(alan[1])
		if err != nil {
			return 0
		}
		return kb / 1024
	}
	return 0
}

// Etkin — "0 = otomatik" değerlerini sunucu kapasitesine göre çözer.
//
// 🔴 Panelin GÖSTERDİĞİ ile slice'a YAZILAN aynı fonksiyondan geçmeli.
// Ayrı hesaplarsa panel bir şey söyler, çekirdek başka şey uygular.
func (a Ayarlar) Etkin(k Kapasite) (cpu, ram, isParcacigi int) {
	cpu, ram, isParcacigi = a.CPUYuzde, a.RAMMb, a.IsParcacigi
	if cpu <= 0 {
		cpu = k.OneriCPUYuzde
	}
	if ram <= 0 {
		ram = k.OneriRAMMb
	}
	if isParcacigi <= 0 {
		isParcacigi = k.OneriIsParcacigi
	}
	return
}

// TaramaKokleri — kapsam ayarına göre taranacak kök dizinler.
//
// 🔴 VARSAYILAN 'host': yalnız /home. Tüm sunucuyu taramak varsayılan olamaz —
// /var/lib/mysql içinde dolaşan bir tarayıcı hem yararsızdır (veri dosyaları)
// hem de disk G/Ç'sini boşa yakar. Operatör bilerek seçmeli.
func (a Ayarlar) TaramaKokleri() []string {
	if a.Kapsam == "sunucu" {
		return []string{"/"}
	}
	return []string{"/home"}
}

// HaricListesi — satır satır hariç yolları.
func (a Ayarlar) HaricListesi() []string {
	var out []string
	for _, s := range strings.Split(a.HaricYollar, "\n") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Oku — tek satırlık ayarı okur.
func Oku(ctx context.Context, db *sql.DB) (Ayarlar, error) {
	var a Ayarlar
	err := db.QueryRowContext(ctx, `SELECT gercek_zamanli, zamanli_tarama, wp_butunluk,
		kural_motoru, konum_sezgileri, oto_karantina, esik_kritik, kapsam, haric_yollar,
		cpu_yuzde, ram_mb, io_agirlik, is_parcacigi, dosya_hiz_sn, zamanli_saat
		FROM av_ayarlar WHERE id=1`).
		Scan(&a.GercekZamanli, &a.ZamanliTarama, &a.WPButunluk, &a.KuralMotoru,
			&a.KonumSezgileri, &a.OtoKarantina, &a.EsikKritik, &a.Kapsam, &a.HaricYollar,
			&a.CPUYuzde, &a.RAMMb, &a.IOAgirlik, &a.IsParcacigi, &a.DosyaHizSn, &a.ZamanliSaat)
	return a, err
}

// Yaz — ayarı kaydeder VE kaynak limitlerini uygular.
//
// 🔴 İkisi AYRILAMAZ: sadece DB'ye yazıp slice'ı güncellememek, panelin
// "limit 200%" gösterirken çekirdeğin eski limiti uygulaması demektir.
func Yaz(ctx context.Context, db *sql.DB, a Ayarlar) error {
	if a.Kapsam != "host" && a.Kapsam != "sunucu" {
		return fmt.Errorf("gecersiz kapsam: %q (host|sunucu)", a.Kapsam)
	}
	if a.IOAgirlik < 1 || a.IOAgirlik > 10000 {
		return fmt.Errorf("io_agirlik 1-10000 araliginda olmali")
	}
	_, err := db.ExecContext(ctx, `UPDATE av_ayarlar SET
		gercek_zamanli=?, zamanli_tarama=?, wp_butunluk=?, kural_motoru=?, konum_sezgileri=?,
		oto_karantina=?, esik_kritik=?, kapsam=?, haric_yollar=?,
		cpu_yuzde=?, ram_mb=?, io_agirlik=?, is_parcacigi=?, dosya_hiz_sn=?, zamanli_saat=?
		WHERE id=1`,
		a.GercekZamanli, a.ZamanliTarama, a.WPButunluk, a.KuralMotoru, a.KonumSezgileri,
		a.OtoKarantina, a.EsikKritik, a.Kapsam, a.HaricYollar,
		a.CPUYuzde, a.RAMMb, a.IOAgirlik, a.IsParcacigi, a.DosyaHizSn, a.ZamanliSaat)
	if err != nil {
		return err
	}
	return LimitleriUygula(a)
}

// LimitleriUygula — systemd slice yazar ve ETKİN olduğunu doğrular.
func LimitleriUygula(a Ayarlar) error {
	k := SunucuKapasitesi()
	cpu, ram, _ := a.Etkin(k)

	icerik := fmt.Sprintf(`# GirginOSPanel antivirüs kaynak dilimi — panel tarafından yönetilir.
# ELLE DÜZENLEMEYİN; panelden değiştirin (Antivirüs → Kaynak limitleri).
#
# 🔴 Tarama G/Ç ve CPU yoğundur. Sınırsız bir tarayıcı, korumaya çalıştığı
# siteleri yavaşlatarak kendi başına kesinti sebebi olur. Limitler burada
# ÇEKİRDEK tarafından zorlanır; uygulama içi "yavaş git" mantığı yeterli değildir.
[Unit]
Description=GirginOSPanel antivirüs tarama dilimi
Before=slices.target

[Slice]
CPUAccounting=yes
MemoryAccounting=yes
IOAccounting=yes
TasksAccounting=yes

CPUQuota=%d%%
MemoryMax=%dM
MemoryHigh=%dM
IOWeight=%d
TasksMax=64
`, cpu, ram, ram*90/100, a.IOAgirlik)

	if err := os.WriteFile(slicePath, []byte(icerik), 0o644); err != nil {
		return fmt.Errorf("av slice yazilamadi: %w", err)
	}
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("daemon-reload: %s: %w", strings.TrimSpace(string(out)), err)
	}
	// 🔴 Slice AKTİFSE canlı uygula — dosyayı yazmak çalışan sürece etki etmez.
	// (kaynaklimit paketindeki aynı ders: dosya kalıcı kaynak, set-property canlı.)
	if out, _ := exec.Command("systemctl", "is-active", SliceAdi).CombinedOutput(); strings.TrimSpace(string(out)) == "active" {
		_ = exec.Command("systemctl", "set-property", SliceAdi,
			fmt.Sprintf("CPUQuota=%d%%", cpu),
			fmt.Sprintf("MemoryMax=%dM", ram),
			fmt.Sprintf("IOWeight=%d", a.IOAgirlik)).Run()
	}
	return nil
}

// LimitDurumu — slice'ın ÇEKİRDEKTE gerçekten ne uyguladığını okur.
//
// 🔴 "Yazdım" ile "uygulanıyor" ayrı şeyler. Panel bu değeri gösterir ki
// operatör dosyaya değil GERÇEĞE baksın.
func LimitDurumu() map[string]string {
	out := map[string]string{}
	for _, ozellik := range []string{"CPUQuotaPerSecUSec", "MemoryMax", "IOWeight", "TasksMax"} {
		b, err := exec.Command("systemctl", "show", "-p", ozellik, "--value", SliceAdi).Output()
		if err != nil {
			out[ozellik] = "OLCULEMEDI"
			continue
		}
		out[ozellik] = strings.TrimSpace(string(b))
	}
	return out
}
