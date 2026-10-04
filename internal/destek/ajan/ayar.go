package ajan

import (
	p "girginospanel/internal/destek/protokol"
)

// duzeySira: düzeylerin sırası (min kuralı için).
var duzeySira = map[p.Duzey]int{p.DuzeyKapali: 0, p.DuzeyOkuma: 1, p.DuzeyOnarim: 2, p.DuzeyTam: 3}

// minDuzey: en düşük düzey; geçersiz/bilinmeyen değer KAPALI sayılır (kapalı başarısız).
func minDuzey(ds ...p.Duzey) p.Duzey {
	en := p.DuzeyTam
	for _, d := range ds {
		s, ok := duzeySira[d]
		if !ok {
			return p.DuzeyKapali
		}
		if s < duzeySira[en] {
			en = d
		}
	}
	return en
}

// EtkinDuzey: bir bağlantının etkin erişim düzeyi TAM OLARAK sunucu sahibinin portalda seçtiği
// ve merkezin EslesmeOnayla v2 içinde imzaladığı düzeydir (bağlantı satırındaki duzey).
// 🔴 2026-10-04 ürün kararı: panelde Telegram Destek özelliği YOKTUR; eski kök şalteri
// (destek_ayar.modul_duzey) ve rol tavanları (Tavan) kaldırıldı — değiştirecek arayüz kalmadı.
// Üst sınırı araç kataloğu koyar (bugün tüm araçlar R0; is.go) ve OturumBaglami.Duzey
// ile protokol.AracGirdisiCoz uygular. Geçersiz/bilinmeyen düzey KAPALI sayılır.
func EtkinDuzey(d p.Duzey) p.Duzey { return minDuzey(d) }
