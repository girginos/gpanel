package ajan

// izleme.go — panel ↔ ajan bağı DB üzerinden (AYRI süreçler).
//
// Panel süreci ajanı süreç içinde ÇAĞIRAMAZ (ajan ayrı hizmettir; panel çökse de
// destek sürer). Bu yüzden panel yalnız DB'ye yazar; ajan şunları ≤ DBYoklamaAraligi
// (3 sn) içinde fark eder:
//
//	(a) destek_ayar değişti (şalter/tavan/düzey)  → hesap durumları merkeze yeniden bildirilir
//	    (etkin düzeyi KAPALI'ya inen bağlantıların oturumu merkezde kapanır) + döngü uyanır.
//	(b) bağlantı 'iptal' oldu ama merkeze bildirilmedi (hub_bildirildi_at IS NULL: panel UI
//	    iptali, parola/2FA/hosting silme tetikleri) → baglanti_iptal gider, iz temizlenir.
//	(c) süre dolumu süpürgesi (supurge.go) bu süreçte çalışır.
//
// Ajan ayrıca destek_ajan_nabiz satırına ≤ NabizAraligi'nda canlılık yazar; panel durum
// sayfası "ajan çalışıyor mu" bilgisini buradan okur.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"girginospanel/internal/destek/iptal"
	p "girginospanel/internal/destek/protokol"

	"github.com/go-sql-driver/mysql"
)

// kolonYok: MySQL 1054 (göç 0130 henüz koşmamış).
func kolonYok(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1054
}

func (a *Ajan) dbYoklamaDongusu(ctx context.Context) {
	tk := time.NewTicker(a.dbAralik())
	defer tk.Stop()
	a.dbYoklaTur(ctx) // taban çizgisi (ayar parmak izi) hemen alınır
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
		}
		a.dbYoklaTur(ctx)
	}
}

func (a *Ajan) dbAralik() time.Duration {
	if a.DBYoklamaAraligi > 0 {
		return a.DBYoklamaAraligi
	}
	return 3 * time.Second
}

// dbYoklaTur: tek yoklama turu (test doğrudan çağırabilir).
func (a *Ajan) dbYoklaTur(ctx context.Context) {
	a.ayarYokla(ctx)
	a.iptalYokla(ctx)
}

// ayarParmakIzi: destek_ayar'ın tüm satırlarının sıralı dökümü (değerler + yazım sayacı surum).
func (a *Ajan) ayarParmakIzi(ctx context.Context) (string, error) {
	rows, err := a.DB.QueryContext(ctx, `SELECT reseller_id, modul_duzey, bayi_duzey, musteri_duzey, surum FROM destek_ayar ORDER BY reseller_id LIMIT 5000`)
	if kolonYok(err) { // göç 0130 yok: yalnız değerler
		rows, err = a.DB.QueryContext(ctx, `SELECT reseller_id, modul_duzey, bayi_duzey, musteri_duzey, 0 FROM destek_ayar ORDER BY reseller_id LIMIT 5000`)
	}
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var rid, sv int64
		var m, by, mu string
		if err := rows.Scan(&rid, &m, &by, &mu, &sv); err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "%d|%s|%s|%s|%d;", rid, m, by, mu, sv)
	}
	return b.String(), rows.Err()
}

func (a *Ajan) ayarYokla(ctx context.Context) {
	iz, err := a.ayarParmakIzi(ctx)
	if err != nil {
		if !iptal.TabloYok(err) {
			log.Printf("destek: ayar yoklanamadı: %v", err)
		}
		return
	}
	if !a.ayarHazir {
		a.ayarIzi, a.ayarHazir = iz, true
		return
	}
	if iz == a.ayarIzi {
		return
	}
	a.ayarIzi = iz
	log.Printf("destek: ayar değişti — bağlantı durumları merkeze yeniden bildiriliyor")
	a.Durt() // modül kapandı/açıldıysa döngü hemen karar versin
	if a.Bagli() {
		a.tumDurumlariBildir(ctx)
	}
}

// tumDurumlariBildir: etkin düzey değişmiş olabilir — aktif bağlantıların durumunu tazele.
func (a *Ajan) tumDurumlariBildir(ctx context.Context) {
	bs, err := baglantilarSorgu(ctx, a.DB, `durum='aktif' ORDER BY id LIMIT 1000`)
	if err != nil {
		return
	}
	for _, b := range bs {
		a.durumBildir(ctx, b)
	}
}

// iptalYokla: panelin (ya da kimlik olaylarının) iptal ettiği ama merkeze bildirilmemiş bağlantılar.
func (a *Ajan) iptalYokla(ctx context.Context) {
	if a.iptalSutunYok.Load() {
		return
	}
	bs, err := baglantilarSorgu(ctx, a.DB, `durum='iptal' AND hub_bildirildi_at IS NULL ORDER BY id LIMIT 200`)
	if err != nil {
		switch {
		case kolonYok(err):
			a.iptalSutunYok.Store(true)
			log.Printf("destek: göç 0130 uygulanmamış (hub_bildirildi_at yok) — panel tarafı iptaller yalnız yeniden bağlanınca bildirilir")
		case !iptal.TabloYok(err):
			log.Printf("destek: iptal yoklanamadı: %v", err)
		}
		return
	}
	for _, b := range bs {
		a.temizleIz(b.Kimlik)
		if !a.bildir(&p.BaglantiIptal{BaglantiID: b.Kimlik, Neden: iptalNedeniProtokol(b.IptalNedeni)}) {
			return // merkeze bağlı değil: kalanlar da gidemez; bağlanınca senkron bildirir
		}
		a.hubBildirildiIsaretle(ctx, b.ID)
	}
}

// hubBildirildiIsaretle: iptalin merkeze iletildiğini kaydeder (sütun yoksa sessiz).
func (a *Ajan) hubBildirildiIsaretle(ctx context.Context, id int64) {
	if a.iptalSutunYok.Load() {
		return
	}
	if _, err := a.DB.ExecContext(ctx, `UPDATE destek_baglantilari SET hub_bildirildi_at=CURRENT_TIMESTAMP WHERE id=? AND hub_bildirildi_at IS NULL`, id); err != nil && kolonYok(err) {
		a.iptalSutunYok.Store(true)
	}
}

// ---- nabız ----

func (a *Ajan) nabizDongusu(ctx context.Context) {
	ar := a.NabizAraligi
	if ar <= 0 {
		ar = 10 * time.Second
	}
	tk := time.NewTicker(ar)
	defer tk.Stop()
	a.nabizYaz(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
		}
		a.nabizYaz(ctx)
	}
}

// KopruDurumlari — nabızdaki köprü durumu değerleri.
const (
	KopruTamam = "tamam"
	KopruYok   = "yok"
)

func (a *Ajan) nabizYaz(ctx context.Context) {
	d := a.DurumAl()
	kd := ""
	if a.KopruPing != nil {
		pctx, iptalF := context.WithTimeout(ctx, 3*time.Second)
		if a.KopruPing(pctx) == nil {
			kd = KopruTamam
		} else {
			kd = KopruYok
		}
		iptalF()
	}
	bagli := 0
	if a.Bagli() {
		bagli = 1
	}
	_, err := a.DB.ExecContext(ctx, `INSERT INTO destek_ajan_nabiz (id, pid, surum, basladi_at, nabiz_at, durum, tasima, son_hata, son_baglanti,
		sunucu_kimligi, cihaz_kimligi, merkez_bagli, kopru_durum) VALUES (1,?,?,FROM_UNIXTIME(?),CURRENT_TIMESTAMP,?,?,?,?,?,?,?,?)
		ON DUPLICATE KEY UPDATE pid=VALUES(pid), surum=VALUES(surum), basladi_at=VALUES(basladi_at), nabiz_at=CURRENT_TIMESTAMP,
		durum=VALUES(durum), tasima=VALUES(tasima), son_hata=VALUES(son_hata), son_baglanti=VALUES(son_baglanti),
		sunucu_kimligi=VALUES(sunucu_kimligi), cihaz_kimligi=VALUES(cihaz_kimligi), merkez_bagli=VALUES(merkez_bagli), kopru_durum=VALUES(kopru_durum)`,
		pidAl(), kesRune(PanelSurumu, 64), a.basladiAt.Unix(), kesRune(d.Durum, 24), kesRune(d.Tasima, 16), kesRune(d.SonHata, 200),
		d.SonBaglanti, kesRune(d.SunucuKimligi, 64), kesRune(d.CihazKimligi, 64), bagli, kd)
	if err != nil && !iptal.TabloYok(err) {
		log.Printf("destek: nabız yazılamadı: %v", err)
	}
}

// Durdur: temiz kapanışta nabzı "durdu" işaretler (panel hemen "ajan yok" göstersin).
func (a *Ajan) Durdur(ctx context.Context) {
	_, _ = a.DB.ExecContext(ctx, `UPDATE destek_ajan_nabiz SET nabiz_at = CURRENT_TIMESTAMP - INTERVAL 1 DAY, merkez_bagli=0, durum='durdu' WHERE id=1`)
}

// ---- panel tarafı ----

// Nabiz — destek_ajan_nabiz satırının panel görünümü.
type Nabiz struct {
	Canli      bool   `json:"canli"`
	YasSn      int64  `json:"yas_sn"`
	PID        int64  `json:"pid"`
	Surum      string `json:"surum"`
	Kopru      string `json:"kopru"`
	MerkezBagl bool   `json:"merkez_bagli"`
	Durum      Durum  `json:"-"`
}

// NabizCanliSn: nabız bu süreden eskiyse ajan çalışmıyor sayılır.
const NabizCanliSn = 30

// NabizOku — panelin kullanımı için. Satır/tablo yoksa Canli=false.
func NabizOku(ctx context.Context, db *sql.DB) Nabiz {
	var n Nabiz
	var son sql.NullInt64
	var yas sql.NullInt64
	var bagli bool
	err := db.QueryRowContext(ctx, `SELECT pid, surum, TIMESTAMPDIFF(SECOND, nabiz_at, CURRENT_TIMESTAMP), durum, tasima, son_hata, son_baglanti,
		sunucu_kimligi, cihaz_kimligi, merkez_bagli, kopru_durum FROM destek_ajan_nabiz WHERE id=1`).
		Scan(&n.PID, &n.Surum, &yas, &n.Durum.Durum, &n.Durum.Tasima, &n.Durum.SonHata, &son,
			&n.Durum.SunucuKimligi, &n.Durum.CihazKimligi, &bagli, &n.Kopru)
	if err != nil {
		return Nabiz{}
	}
	n.Durum.SonBaglanti = son.Int64
	n.YasSn = yas.Int64
	n.Canli = yas.Valid && yas.Int64 >= 0 && yas.Int64 <= NabizCanliSn && n.Durum.Durum != "durdu"
	n.MerkezBagl = n.Canli && bagli
	return n
}

func pidAl() int { return os.Getpid() }
