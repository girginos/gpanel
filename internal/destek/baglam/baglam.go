// Package baglam — destek iş emrinin YEREL kapsamını araç yürütücüsüne
// (internal/destek/arac) taşıyan yaprak paket. protokol.OturumBaglami yalnız rol,
// düzey ve hesap etiketlerini taşır; yürütücünün denetim/sorgu kapsamı için
// gereken panel kimlikleri (domain/bayi) burada, ajanın eşleştirme kaydından
// kurulmuş haliyle bulunur. Merkezden gelen hiçbir değer buraya girmez.
//
// Ajan her iş emrinde Ile ile bağlamı ctx'e koyar; yürütücü Al ile okur.
// Hesaplar[i].Sira == OturumBaglami.Hesaplar[i].Sira ve Etiket aynıdır.
package baglam

import (
	"context"

	"girginospanel/internal/destek/protokol"
)

// Hesap: oturumdaki hesabın sıra numarası, etiketi (alan adı) ve panel domain kimliği.
type Hesap struct {
	Sira     int
	Etiket   string
	DomainID int64
}

// Kapsam: iş emrinin yerel kapsamı (ajan kurar; merkez iddiası DEĞİL).
type Kapsam struct {
	BaglantiID  string // protokoldeki baglanti_id
	Rol         protokol.Rol
	Duzey       protokol.Duzey // etkin düzey (min kuralı uygulanmış)
	ResellerID  int64          // bayi: kendi id; müşteri: domainin bayisi; kök: 0
	PanelUserID int64          // kök/bayi users.id; müşteri 0
	DomainID    int64          // müşteri: kendi domaini; diğerleri 0
	FTPHesapID  int64          // müşteri: eşleştirmeyi yapan FTP hesabı
	Aktor       string         // denetim aktörü: "destek-bot (tg:<kısa>)"
	Hesaplar    []Hesap
}

type anahtar struct{}

// Ile: k'yi taşıyan ctx.
func Ile(ctx context.Context, k Kapsam) context.Context {
	return context.WithValue(ctx, anahtar{}, k)
}

// Al: ctx'teki kapsam (yoksa ok=false — yürütücü bu durumda REDDETMELİ).
func Al(ctx context.Context) (Kapsam, bool) {
	k, ok := ctx.Value(anahtar{}).(Kapsam)
	return k, ok
}

// HesapDomain: sıra numarasının domain kimliği (yoksa 0, false).
func (k Kapsam) HesapDomain(sira int) (int64, bool) {
	for _, h := range k.Hesaplar {
		if h.Sira == sira {
			return h.DomainID, true
		}
	}
	return 0, false
}
