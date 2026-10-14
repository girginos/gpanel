package bayikaynak

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	"girginospanel/internal/bayidilim"
)

// ErrBayiYok: istenen bayi kaydı yok.
var ErrBayiYok = errors.New("bayi bulunamadı")

// DiskBilgi: bayinin disk havuzu görünümü.
type DiskBilgi struct {
	SinirMB      int64  `json:"sinir_mb"`
	KullanilanMB int64  `json:"kullanilan_mb"`
	Kaynak       string `json:"kullanim_kaynagi"` // proje | kullanici_kotasi | veritabani
	Mod          string `json:"mod"`              // sert | yeniden_baslatma_bekleniyor | olcum_uyari | desteklenmiyor | sinirsiz
	Zorlama      bool   `json:"sert_sinir_etkin"`
	Mesaj        string `json:"mesaj"`
}

// Durum: bir bayinin havuzunun anlık görünümü (bayi kendi kaydını, yönetici herkesinkini görür).
type Durum struct {
	BayiID                 int64     `json:"bayi_id"`
	Havuz                  Havuz     `json:"havuz"`
	DilimAktif             bool      `json:"dilim_aktif"`
	Hostingler             int       `json:"hosting_sayisi"`
	Uyeler                 int       `json:"havuz_uye_sayisi"`
	Bekleyen               int       `json:"bekleyen_hosting"` // CPU/RAM taşıması yeniden başlatma bekliyor
	BekleyenAlanlar        []string  `json:"bekleyen_alan_adlari,omitempty"`
	YenidenBaslatmaGerekli bool      `json:"yeniden_baslatma_gerekli"`
	Disk                   DiskBilgi `json:"disk"`
	Anlik                  *Nokta    `json:"anlik,omitempty"`
}

// sonNokta: son kayıtlı örnek (yoksa nil).
func sonNokta(ctx context.Context, db *sql.DB, bayiID int64) *Nokta {
	var tsS string
	var n Nokta
	var cpu, kis, ram, oom, io1, io2, disk sql.NullInt64
	err := db.QueryRowContext(ctx,
		`SELECT DATE_FORMAT(ts,'%Y-%m-%dT%H:%i:%sZ'), cpu_milli, cpu_sinir_milli, kisitlama_ms, ram_mb, ram_sinir_mb,
		        oom_olay, disk_mb, disk_sinir_mb, io_oku_kbs, io_yaz_kbs, kaynak
		   FROM bayi_kaynak_kullanim WHERE bayi_id=? ORDER BY ts DESC LIMIT 1`, bayiID).
		Scan(&tsS, &cpu, &n.CPUSinirMilli, &kis, &ram, &n.RAMSinirMB, &oom, &disk, &n.DiskSinirMB, &io1, &io2, &n.Kaynak)
	if err != nil {
		return nil
	}
	n.T = tsS
	pi := func(v sql.NullInt64) *int {
		if !v.Valid {
			return nil
		}
		x := int(v.Int64)
		return &x
	}
	n.CPUMilli, n.KisitlamaMS, n.RAMMB, n.OOM, n.IOOkuKBs, n.IOYazKBs = pi(cpu), pi(kis), pi(ram), pi(oom), pi(io1), pi(io2)
	if disk.Valid {
		d := disk.Int64
		n.DiskMB = &d
	}
	return &n
}

// Durumu: bayinin havuz durumunu derler. Bekleyenler systemd'ye sorulur (yalnız havuz
// kurulu ya da dilim dosyası duruyorsa; havuzsuz bayide exec yapılmaz).
func Durumu(ctx context.Context, db *sql.DB, bayiID int64) (Durum, error) {
	d := Durum{BayiID: bayiID}
	hv, err := HavuzlariOku(ctx, db, bayiID)
	if err != nil {
		return d, err
	}
	h, ok := hv[bayiID]
	if !ok {
		return d, ErrBayiYok
	}
	d.Havuz, d.DilimAktif = h, h.DilimAktif()
	uyeler, err := UyeleriOku(ctx, db, bayiID, 0)
	if err != nil {
		return d, err
	}
	d.Hostingler = len(uyeler)
	skAlan := map[string]string{}
	var skler []string
	for _, u := range uyeler {
		skAlan[u.SK] = u.Alan
		skler = append(skler, u.SK)
		if bayidilim.Uyelik(u.SK) == bayiID {
			d.Uyeler++
		}
	}
	if d.DilimAktif || imzaliHavuzDilimi(birimYolu(bayidilim.BayiDilim(bayiID))) {
		for _, sk := range BekleyenFPM(ctx, skler) {
			d.Bekleyen++
			if len(d.BekleyenAlanlar) < 20 {
				d.BekleyenAlanlar = append(d.BekleyenAlanlar, skAlan[sk])
			}
		}
		sort.Strings(d.BekleyenAlanlar)
	}

	dd := DiskDurumu(ctx)
	d.Disk = DiskBilgi{SinirMB: h.DiskMB, Mod: dd.Mod, Zorlama: dd.ZorlamaAcik && h.DiskAktif(), Mesaj: dd.Mesaj}
	if !h.DiskAktif() {
		d.Disk.Mod, d.Disk.Mesaj = "sinirsiz", "bu bayi için disk havuzu tanımlı değil"
	}
	if k, ok := DiskKullanimlari(ctx, db, []int64{bayiID})[bayiID]; ok {
		d.Disk.KullanilanMB, d.Disk.Kaynak = k.KullanilanKB/1024, k.Kaynak
	}
	d.YenidenBaslatmaGerekli = d.Bekleyen > 0 || (h.DiskAktif() && dd.YenidenBaslatma)
	d.Anlik = sonNokta(ctx, db, bayiID)
	return d, nil
}

// BayiOzet: yönetici listesi satırı (pahalı systemd sorgusu YOK).
type BayiOzet struct {
	BayiID     int64  `json:"bayi_id"`
	Kullanici  string `json:"kullanici"`
	Havuz      Havuz  `json:"havuz"`
	DilimAktif bool   `json:"dilim_aktif"`
	Hostingler int    `json:"hosting_sayisi"`
	Uyeler     int    `json:"havuz_uye_sayisi"`
	Anlik      *Nokta `json:"anlik,omitempty"`
}

// TumOzetler: tüm bayilerin havuz özeti (yönetici).
func TumOzetler(ctx context.Context, db *sql.DB) ([]BayiOzet, error) {
	hv, err := HavuzlariOku(ctx, db, 0)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT id, username FROM users WHERE role='reseller' ORDER BY username`)
	if err != nil {
		return nil, err
	}
	var out []BayiOzet
	for rows.Next() {
		var o BayiOzet
		if rows.Scan(&o.BayiID, &o.Kullanici) == nil {
			out = append(out, o)
		}
	}
	_ = rows.Close()
	uyeler, err := UyeleriOku(ctx, db, 0, 0)
	if err != nil {
		return nil, err
	}
	say, uye := map[int64]int{}, map[int64]int{}
	for _, u := range uyeler {
		say[u.BayiID]++
		if bayidilim.Uyelik(u.SK) == u.BayiID {
			uye[u.BayiID]++
		}
	}
	for i := range out {
		o := &out[i]
		o.Havuz = hv[o.BayiID]
		o.DilimAktif = o.Havuz.DilimAktif()
		o.Hostingler, o.Uyeler = say[o.BayiID], uye[o.BayiID]
		o.Anlik = sonNokta(ctx, db, o.BayiID)
	}
	return out, nil
}

// SimdiUTC: testlerde sabitlenebilir saat.
var SimdiUTC = func() time.Time { return time.Now().UTC() }

// AsimBayi: disk kullanımı havuzunu ZATEN aşan bayi. Sert sınır devreye girdiğinde bu bayinin
// hostingleri yeni veri yazamaz (EDQUOT); yönetici sınırı açmadan önce bunu görmelidir.
type AsimBayi struct {
	BayiID       int64  `json:"bayi_id"`
	Kullanici    string `json:"kullanici"`
	KullanilanMB int64  `json:"kullanilan_mb"`
	SinirMB      int64  `json:"sinir_mb"`
}

// AsimdakiBayiler: disk havuzu tanımlı olup kullanımı havuzu aşan bayiler.
func AsimdakiBayiler(ctx context.Context, db *sql.DB) ([]AsimBayi, error) {
	hv, err := HavuzlariOku(ctx, db, 0)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for id, h := range hv {
		if h.DiskAktif() {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) == 0 {
		return nil, nil
	}
	kul := DiskKullanimlari(ctx, db, ids)
	ad := map[int64]string{}
	if rows, err := db.QueryContext(ctx, `SELECT id, username FROM users WHERE role='reseller'`); err == nil {
		for rows.Next() {
			var id int64
			var u string
			if rows.Scan(&id, &u) == nil {
				ad[id] = u
			}
		}
		_ = rows.Close()
	}
	var out []AsimBayi
	for _, id := range ids {
		k, ok := kul[id]
		if !ok {
			continue
		}
		if mb := k.KullanilanKB / 1024; mb > hv[id].DiskMB {
			out = append(out, AsimBayi{BayiID: id, Kullanici: ad[id], KullanilanMB: mb, SinirMB: hv[id].DiskMB})
		}
	}
	return out, nil
}

// BayiGorunumu: bayiye gösterilecek sadeleştirilmiş kopya — sunucu iç ayrıntıları (bağlama
// noktası, kernel komut satırı, yönetici eylemi gerektiren yeniden başlatma) çıkarılır.
func (d Durum) BayiGorunumu() Durum {
	d.Disk.Mod, d.Disk.Mesaj = "", ""
	d.YenidenBaslatmaGerekli = false
	return d
}
