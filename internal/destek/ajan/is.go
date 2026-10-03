package ajan

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"girginospanel/internal/destek/baglam"
	p "girginospanel/internal/destek/protokol"
)

// isEmriKabul: iş emrini eşzamanlılık sınırıyla (4) ayrı goroutine'de yürütür.
func (a *Ajan) isEmriKabul(ctx context.Context, o *oturum, m *p.IsEmri) {
	select {
	case a.isSem <- struct{}{}:
	default:
		o.gonder(&p.IsSonucu{IsKodu: m.IsKodu, HataKodu: p.HataMesgul, Ozet: "sunucu meşgul"})
		return
	}
	go func() {
		defer func() { <-a.isSem }()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("🔴 destek: iş emri %s işlenirken panik: %v", m.IsKodu, r)
				o.gonder(&p.IsSonucu{IsKodu: m.IsKodu, HataKodu: p.HataIc})
			}
		}()
		o.gonder(a.isYurut(ctx, m))
	}()
}

func red(isKodu string, kod p.HataKodu, ozet string) *p.IsSonucu {
	return &p.IsSonucu{IsKodu: isKodu, HataKodu: kod, Ozet: ozet}
}

// imzaRedSayaci: imzası/zamanı geçersiz iş emri günlük satırı seli olmasın (her 100'de bir).
var imzaRedSayaci atomic.Int64

// isYurut: iş emrinin tam denetim zinciri. Sıra BİLİNÇLİ: önce imza/zaman/tekrar
// (merkez kimliği), sonra YEREL bağlantı kaydı ve kimlik izi, sonra düzey ve katalog,
// sonra iş kaydı (is_kodu tekil), sonra hesap listesi ve katı girdi, en son yürütme.
func (a *Ajan) isYurut(ctx context.Context, m *p.IsEmri) *p.IsSonucu {
	basla := a.Saat()
	if a.dogrulayici == nil {
		return red(m.IsKodu, p.HataAnahtarBilinmiyor, "")
	}
	if err := a.dogrulayici.Dogrula(m); err != nil {
		if imzaRedSayaci.Add(1)%100 == 1 {
			log.Printf("destek: iş emri reddedildi (%s): %v", m.IsKodu, err)
		}
		return red(m.IsKodu, p.HataKoduAl(err), "")
	}
	if !modulAcik(ctx, a.DB) {
		return red(m.IsKodu, p.HataDevreDisi, "")
	}
	b, err := baglantiKimlikle(ctx, a.DB, m.BaglantiID)
	if errors.Is(err, sql.ErrNoRows) {
		return red(m.IsKodu, p.HataBaglantiYok, "")
	}
	if err != nil {
		return red(m.IsKodu, p.HataIc, "")
	}
	switch b.Durum {
	case durumBekliyor:
		return red(m.IsKodu, p.HataOnayBekliyor, "")
	case durumIptal:
		return red(m.IsKodu, p.HataBaglantiIptal, "")
	}
	if b.Bitis > 0 && a.Saat().Unix() >= b.Bitis {
		a.otomatikIptal(ctx, b, p.IptalSuresiDoldu)
		return red(m.IsKodu, p.HataBaglantiIptal, "bağlantının süresi doldu")
	}
	kd, err := kimlikIzi(ctx, a.DB, b.Rol, b.DomainID, b.FTPHesapID, b.PanelUserID)
	if err != nil {
		return red(m.IsKodu, p.HataIc, "")
	}
	if n := izDenetle(b, kd); n != "" {
		a.otomatikIptal(ctx, b, n)
		return red(m.IsKodu, p.HataBaglantiIptal, "")
	}
	etkin := EtkinDuzey(ctx, a.DB, b.Rol, b.ResellerID, b.Duzey)
	if etkin == p.DuzeyKapali {
		return red(m.IsKodu, p.HataDevreDisi, "")
	}

	// İş kaydı: is_kodu TEKİL (aynı iş iki kez yürütülmez; tekrar önbelleğinin
	// yeniden başlatmayı aşan ikinci kilidi). Politika reddi de kayda geçer.
	var sik bytes.Buffer
	if json.Compact(&sik, m.Param) != nil {
		sik.Reset()
	}
	isID, err := isBaslat(ctx, a.DB, m.IsKodu, b.ID, m.Arac, m.Risk, a.maske.Satir(sik.String()))
	if errors.Is(err, errTekrarIs) {
		return red(m.IsKodu, p.HataTekrar, "")
	}
	if err != nil {
		return red(m.IsKodu, p.HataIc, "")
	}
	politikaRed := func(kod p.HataKodu, ozet string) *p.IsSonucu {
		isBitir(ctx, a.DB, isID, string(kod), a.Saat().Sub(basla))
		a.denetimBot(ctx, b, dIsRed, "arac="+m.Arac+" kod="+string(kod), false)
		return red(m.IsKodu, kod, ozet)
	}

	tanim, ok := p.AracBul(m.Arac)
	if !ok {
		return politikaRed(p.HataAracBilinmiyor, "")
	}
	if tanim.Risk != m.Risk {
		return politikaRed(p.HataParam, "risk sınıfı katalogla uyuşmuyor")
	}
	// Faz 1: yalnız R0 (okuma). R1/R2 düzeyden bağımsız REDDEDİLİR.
	if tanim.Risk != p.R0 {
		return politikaRed(p.HataDuzey, "bu sürümde yalnız okuma araçları açık")
	}
	hs, err := hesaplarOku(ctx, a.DB, b)
	if err != nil {
		isBitir(ctx, a.DB, isID, string(p.HataIc), a.Saat().Sub(basla))
		return red(m.IsKodu, p.HataIc, "")
	}
	// Modelin gördüğü hesap listesi (son baglanti_durum) ile şimdiki liste aynı olmalı:
	// aksi halde "hesap 3" başka bir hesaba işaret edebilir. Değiştiyse güncel liste
	// gönderilir ve iş reddedilir (yanlış hedefe yürütme yok).
	oz := hesapListesiOzeti(hs)
	a.mu.Lock()
	son := a.hesapIzi[b.Kimlik]
	a.mu.Unlock()
	if son != oz {
		a.durumBildir(ctx, b)
		isBitir(ctx, a.DB, isID, string(p.HataKapsam), a.Saat().Sub(basla))
		return red(m.IsKodu, p.HataKapsam, "hesap listesi değişti; güncel liste gönderildi, yeniden deneyin")
	}
	ob := p.OturumBaglami{Rol: b.Rol, Duzey: etkin, Hesaplar: protokolHesaplari(hs), Dil: "tr"}
	if _, err := p.AracGirdisiCoz(ob, m.Arac, m.Param); err != nil {
		return politikaRed(p.HataKoduAl(err), "")
	}
	_, _ = a.DB.ExecContext(ctx, `UPDATE destek_baglantilari SET son_kullanim=CURRENT_TIMESTAMP WHERE id=?`, b.ID)

	var sonuc p.IsSonucu
	if a.Yurutucu == nil {
		sonuc = p.IsSonucu{IsKodu: m.IsKodu, HataKodu: p.HataIc, Ozet: "arac_yok: araç yürütücüsü bu panelde bağlı değil"}
	} else {
		kap := baglam.Kapsam{BaglantiID: b.Kimlik, Rol: b.Rol, Duzey: etkin, ResellerID: b.ResellerID, PanelUserID: b.PanelUserID,
			DomainID: b.DomainID, FTPHesapID: b.FTPHesapID, Aktor: botAktor(b), Hesaplar: hs}
		if b.Rol != p.RolMusteri {
			kap.DomainID = 0
		}
		yctx, iptal := context.WithTimeout(baglam.Ile(ctx, kap), a.IsZamanAsimi)
		sonuc = a.yurutKorumali(yctx, ob, *m)
		iptal()
	}
	s := a.sonucTemizle(m.IsKodu, sonuc)
	kod := "ok"
	if !s.Ok {
		kod = string(s.HataKodu)
	}
	isBitir(ctx, a.DB, isID, kod, a.Saat().Sub(basla))
	return s
}

func (a *Ajan) yurutKorumali(ctx context.Context, ob p.OturumBaglami, m p.IsEmri) (s p.IsSonucu) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("🔴 destek: araç %s yürütülürken panik: %v", m.Arac, r)
			s = p.IsSonucu{IsKodu: m.IsKodu, HataKodu: p.HataIc}
		}
	}()
	return a.Yurutucu.Yurut(ctx, ob, m)
}

var ozetSatirSonu = strings.NewReplacer("\r\n", " / ", "\n", " / ", "\r", " / ")

// sonucTemizle: yürütücünün sonucunu protokol sınırlarına ve İKİNCİ maske geçişine
// sokar: is_kodu zorlanır, özet tek satır + maskeli + kesik, satırlar maskeli + kesik
// (≤300 × ≤1024 bayt), biçimsiz kanıt düşer. Sonuç hâlâ geçersizse ic_hata.
func (a *Ajan) sonucTemizle(isKodu string, s p.IsSonucu) *p.IsSonucu {
	out := &p.IsSonucu{IsKodu: isKodu, Ok: s.Ok, HataKodu: s.HataKodu, Kesildi: s.Kesildi, Kanit: s.Kanit}
	if out.Ok {
		out.HataKodu = ""
	} else if !out.HataKodu.Gecerli() {
		out.HataKodu = p.HataIc
	}
	oz, kes := p.SatirTemizle(a.maske.Satir(ozetSatirSonu.Replace(s.Ozet)))
	out.Ozet = kesBayt(oz, p.OzetSiniri)
	out.Kesildi = out.Kesildi || kes
	satirlar := s.Satirlar
	if len(satirlar) > p.SatirSayisiSiniri {
		satirlar = satirlar[:p.SatirSayisiSiniri]
		out.Kesildi = true
	}
	if len(satirlar) > 0 {
		out.Satirlar = make([]string, 0, len(satirlar))
		for _, sat := range a.maske.Satirlar(satirlar) {
			t, k := p.SatirTemizle(sat)
			out.Satirlar = append(out.Satirlar, t)
			out.Kesildi = out.Kesildi || k
		}
	}
	if err := out.Dogrula(); err != nil {
		out.Kanit = ""
		if err := out.Dogrula(); err != nil {
			log.Printf("destek: iş sonucu biçimsiz (%s): %v", isKodu, err)
			return red(isKodu, p.HataIc, "")
		}
	}
	return out
}

// kesBayt: UTF-8 rune sınırında en çok n bayt.
func kesBayt(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
