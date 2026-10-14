package dbgovernor

// sink.go — bildirim ve denetim yazıcıları. Eylem kodları BURADA literal olarak yazılır
// (denetim/kaynak_tarama_test.go kaynak taramasıyla katalogla eşleştirir). DB nil ise (birim
// testleri) yalnız Olaylar() kaydına düşer.

import (
	"time"

	"girginospanel/internal/bildirim"
	"girginospanel/internal/httpx"
)

// surekliBaslik: 3. seviyede kesintisiz aşım Soguma'dan uzun sürünce yöneticiye giden bildirimin başlığı.
const surekliBaslik = "Veritabanı CPU aşımı sürüyor: izole kopya ya da askı önerilir"

// bildirimBastir: aynı bildirim bu süre içinde yeniden yazılmaz (kısıt/kaldır döngüsü spam üretmesin).
const bildirimBastir = 10 * time.Minute

// bildirimYaz: domainID>0 → hostingin sahibine (bayi ya da yönetici); değilse bayiID'ye.
// Kategori "kota": yalnız sahip görür, müşteriye gitmez (bildirim.musteriyeKapali).
func (m *Motor) bildirimYaz(seviye, baslik, mesaj string, domainID, bayiID int64, _ Ayar) {
	m.olayKaydet(Olay{Tur: "bildirim", Eylem: baslik, Detay: mesaj, Domain: domainID, Bayi: bayiID})
	if m.DB == nil {
		return
	}
	o := bildirim.Olay{Seviye: seviye, Kategori: "kota", Baslik: baslik, Mesaj: mesaj, RefTur: "db_cpu"}
	if domainID > 0 {
		o.RefID = domainID
		bildirim.YazOlay(m.DB, o, domainID, bildirim.AktorYok, bildirimBastir)
		return
	}
	if bayiID > 0 {
		o.RefID = bayiID
		bildirim.YazBayi(m.DB, bayiID, o, bildirimBastir)
	}
}

// yoneticiyeBildir: panel geneli (yönetici) bildirim. Bayiye ait hostingde bildirimin sahibi bayidir;
// kararı yalnız yöneticinin verebildiği durumlar (izole kopya) için yöneticiye ayrıca yazılır.
func (m *Motor) yoneticiyeBildir(seviye, baslik, mesaj string, refID int64) {
	m.olayKaydet(Olay{Tur: "bildirim", Eylem: baslik, Detay: mesaj})
	if m.DB == nil {
		return
	}
	o := bildirim.Olay{Seviye: seviye, Kategori: "kota", Baslik: baslik, Mesaj: mesaj, RefTur: "db_cpu", RefID: refID}
	bildirim.YazOlay(m.DB, o, 0, bildirim.AktorYok, bildirimBastir)
}

func (m *Motor) denetimKisit(hedef, detay string, kapsam int64) {
	m.olayKaydet(Olay{Tur: "denetim", Eylem: "db.cpu_kisit", Hedef: hedef, Detay: detay, Bayi: kapsam})
	httpx.DenetimSistem(m.DB, 0, "sistem", "db.cpu_kisit", hedef, detay, kapsam, true)
}

func (m *Motor) denetimYukselt(hedef, detay string, kapsam int64) {
	m.olayKaydet(Olay{Tur: "denetim", Eylem: "db.cpu_kisit_yukselt", Hedef: hedef, Detay: detay, Bayi: kapsam})
	httpx.DenetimSistem(m.DB, 0, "sistem", "db.cpu_kisit_yukselt", hedef, detay, kapsam, true)
}

func (m *Motor) denetimGozlem(hedef, detay string, kapsam int64) {
	m.olayKaydet(Olay{Tur: "denetim", Eylem: "db.cpu_asim_gozlem", Hedef: hedef, Detay: detay, Bayi: kapsam})
	httpx.DenetimSistem(m.DB, 0, "sistem", "db.cpu_asim_gozlem", hedef, detay, kapsam, true)
}

func (m *Motor) denetimKes(hedef, detay string, kapsam int64) {
	m.olayKaydet(Olay{Tur: "denetim", Eylem: "db.cpu_baglanti_kes", Hedef: hedef, Detay: detay, Bayi: kapsam})
	httpx.DenetimSistem(m.DB, 0, "sistem", "db.cpu_baglanti_kes", hedef, detay, kapsam, true)
}

func (m *Motor) denetimKaldir(hedef, detay string, kapsam int64) {
	m.olayKaydet(Olay{Tur: "denetim", Eylem: "db.cpu_kisit_kaldir", Hedef: hedef, Detay: detay, Bayi: kapsam})
	httpx.DenetimSistem(m.DB, 0, "sistem", "db.cpu_kisit_kaldir", hedef, detay, kapsam, true)
}
