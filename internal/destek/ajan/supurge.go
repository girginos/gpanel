package ajan

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"girginospanel/internal/destek/iptal"
	p "girginospanel/internal/destek/protokol"
	"girginospanel/internal/httpx"
)

const (
	supurgeAraligi = time.Minute
	ozetAraligi    = time.Hour
	isSaklama      = 180 // gün
)

func (a *Ajan) supurgeDongusu(ctx context.Context) {
	var sonOzet time.Time
	tk := time.NewTicker(supurgeAraligi)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
		}
		a.SupurgeTur(ctx)
		if time.Since(sonOzet) >= ozetAraligi {
			a.R0Ozet(ctx)
			sonOzet = time.Now()
		}
	}
}

// SupurgeTur: süresi dolan bekleyen/aktif bağlantıları ve kimlik izi bozulanları
// iptal eder (modül kapalıyken de: iptal tetikleri her zaman çalışmalı).
func (a *Ajan) SupurgeTur(ctx context.Context) {
	simdi := a.Saat().Unix()
	bs, err := baglantilarSorgu(ctx, a.DB, `durum IN ('bekliyor','aktif') ORDER BY id LIMIT 5000`)
	if err != nil {
		if !iptal.TabloYok(err) {
			log.Printf("destek süpürge: %v", err)
		}
		return
	}
	for _, b := range bs {
		if b.Durum == durumBekliyor && simdi-b.OlusturmaAt >= int64(bekleyenOmru/time.Second) {
			a.otomatikIptal(ctx, b, p.IptalSuresiDoldu)
			continue
		}
		if b.Durum == durumAktif && b.Bitis > 0 && simdi >= b.Bitis {
			a.otomatikIptal(ctx, b, p.IptalSuresiDoldu)
			continue
		}
		kd, err := kimlikIzi(ctx, a.DB, b.Rol, b.DomainID, b.FTPHesapID, b.PanelUserID)
		if err != nil {
			continue // DB hatası: bu tur atla (iş emri yolunda kapalı başarısız)
		}
		n := kd.neden
		if b.Durum == durumAktif {
			n = izDenetle(b, kd)
		}
		if n != "" {
			a.otomatikIptal(ctx, b, n)
		}
	}
	_, _ = a.DB.ExecContext(ctx, `DELETE FROM destek_islemleri WHERE zaman < NOW() - INTERVAL ? DAY LIMIT 5000`, isSaklama)
}

// R0Ozet: R0 iş kayıtlarını bağlantı başına TEK denetim satırına toplar
// ("destek.r0_ozet": araç × adet, başarısız sayısı). Tek tek R0 kaydı denetimi
// boğmasın diye (tasarım: R0 saatlik özet, R1/R2 tek tek).
func (a *Ajan) R0Ozet(ctx context.Context) {
	var enCok int64
	if err := a.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM destek_islemleri WHERE denetim_ozet=0 AND risk='R0' AND sonuc<>'yurutuluyor'`).Scan(&enCok); err != nil || enCok == 0 {
		return
	}
	rows, err := a.DB.QueryContext(ctx, `SELECT baglanti_id, arac, COUNT(*), SUM(sonuc<>'ok') FROM destek_islemleri
		WHERE denetim_ozet=0 AND risk='R0' AND sonuc<>'yurutuluyor' AND id<=? GROUP BY baglanti_id, arac`, enCok)
	if err != nil {
		return
	}
	type ozet struct {
		araclar   map[string]int
		toplam    int
		basarisiz int
	}
	gruplar := map[int64]*ozet{}
	for rows.Next() {
		var bid int64
		var arac string
		var n, hata int
		if rows.Scan(&bid, &arac, &n, &hata) != nil {
			continue
		}
		o := gruplar[bid]
		if o == nil {
			o = &ozet{araclar: map[string]int{}}
			gruplar[bid] = o
		}
		o.araclar[arac] += n
		o.toplam += n
		o.basarisiz += hata
	}
	_ = rows.Close()
	for bid, o := range gruplar {
		b, err := baglantiTara(a.DB.QueryRowContext(ctx, `SELECT `+bagKolonlar+` FROM destek_baglantilari WHERE id=?`, bid))
		if err != nil {
			continue
		}
		adlar := make([]string, 0, len(o.araclar))
		for ad := range o.araclar {
			adlar = append(adlar, ad)
		}
		sort.Strings(adlar)
		parca := make([]string, 0, len(adlar))
		for _, ad := range adlar {
			parca = append(parca, fmt.Sprintf("%s×%d", ad, o.araclar[ad]))
		}
		detay := kesRune(fmt.Sprintf("işlem=%d başarısız=%d: %s", o.toplam, o.basarisiz, strings.Join(parca, ", ")), 900)
		httpx.DenetimSistem(a.DB, 0, botAktor(b), dR0Ozet, a.hedefAdi(ctx, b), detay, b.ResellerID, o.basarisiz == 0)
	}
	_, _ = a.DB.ExecContext(ctx, `UPDATE destek_islemleri SET denetim_ozet=1 WHERE denetim_ozet=0 AND risk='R0' AND sonuc<>'yurutuluyor' AND id<=?`, enCok)
}
