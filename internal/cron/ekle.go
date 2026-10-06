package cron

// Toplu ekleme: dışarıdan gelen (ör. cPanel/DirectAdmin arşivi) görevleri
// hesabın panel deposuna ekler. HTTP uçlarıyla aynı kapılardan geçer: hesap
// askıdaysa yazmaz, her görev validate edilir, görev sınırı (maxGorev) korunur.

import (
	"fmt"

	"girginospanel/internal/kiraci"
)

// ErrAskida — hesap askıdayken Ekle'nin döndürdüğü hata (errAskida ile aynı değer).
var ErrAskida = errAskida

// Red — eklenmeyen görev: Idx girdi listesindeki sıra.
type Red struct {
	Idx   int
	Neden string
}

// EkleSonuc — toplu eklemenin özeti.
type EkleSonuc struct {
	Eklenen    int
	Yinelenen  int // depoda zaten olan ya da girdide tekrar eden (eklenmedi)
	Reddedilen []Red
}

// ZamanGecerli — tek zaman alanı cron dilbilgisine uyuyor mu (çevirici ön
// doğrulaması için; validate ile aynı kural).
func ZamanGecerli(f string) bool { return zamanGecerli(f) }

// Ekle — görevleri sk'nın deposuna ekler. Tip boşsa "komut", Bildirim boşsa
// "bilgi", Etkin her zaman true yapılır. Geçersiz görev Reddedilen'e girer;
// depoda olan ya da girdide tekrar eden (zaman+komut) görev eklenmez, Yinelenen
// sayılır; maxGorev aşılırsa kalanlar reddedilir. Hesap askıdaysa hiçbir şey
// yazılmaz ve ErrAskida döner. Eklenen yoksa depoya dokunulmaz.
func Ekle(sk string, gorevler []Gorev) (EkleSonuc, error) {
	var s EkleSonuc
	if !kiraci.GecerliSK(sk) {
		return s, errBad
	}
	yazKilit.Lock()
	defer yazKilit.Unlock()
	if askida(sk) {
		return s, errAskida
	}
	liste, err := read(sk)
	if err != nil {
		return s, err
	}
	var mevcut = make(map[string]bool, len(liste)+len(gorevler))
	for _, g := range liste {
		mevcut[anahtar(g)] = true
	}
	for i, g := range gorevler {
		if g.Tip == "" {
			g.Tip = "komut"
		}
		if g.Bildirim == "" {
			g.Bildirim = "bilgi"
		}
		g.Etkin = true
		if err := validate(g); err != nil {
			s.Reddedilen = append(s.Reddedilen, Red{Idx: i, Neden: err.Error()})
			continue
		}
		if mevcut[anahtar(g)] {
			s.Yinelenen++
			continue
		}
		if len(liste) >= maxGorev {
			s.Reddedilen = append(s.Reddedilen, Red{Idx: i, Neden: fmt.Sprintf("görev sınırı (%d)", maxGorev)})
			continue
		}
		mevcut[anahtar(g)] = true
		g.Idx = len(liste)
		liste = append(liste, g)
		s.Eklenen++
	}
	if s.Eklenen == 0 {
		return s, nil
	}
	if err := write(sk, liste); err != nil {
		return EkleSonuc{}, err
	}
	return s, nil
}
