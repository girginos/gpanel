package bildirim

// esik_musteri_test.go — kullanıcı kararı 2026-10-03: kota/kaynak EŞİK
// bildirimleri (kategori "kota") müşteriye gitmez, yalnız domain sahibine
// (bayi ya da yönetici). Öteki domain türleri (ssl, aski, yedek, websec,
// antivirus, zincir) müşteriye gitmeye devam eder.

import (
	"net/http"
	"strings"
	"testing"
)

// em_musteriGorur — müşteri listesinde id var mı + yanıttaki okunmamış + sayaç.
func em_gorunum(t *testing.T, h *Handlers, tok string, id int64) (gordu bool, listeOkunmamis, sayac int) {
	t.Helper()
	y := bt_liste(t, h, tok, "")
	_, gordu = bt_idler(y)[id]
	n, _ := bh_sayac(t, h, tok)
	return gordu, y.Okunmamis, n
}

// Eşik bildirimi: sahip (bayi / kök) görür ve sayar; müşteri ne listede ne rozette.
func TestEsik_MusteriyeGitmez_SahipGorur(t *testing.T) {
	h := bh_kur(t)
	esik := Olay{Seviye: "uyari", Kategori: "kota", Baslik: "Disk kotası %85'i aştı", Mesaj: "a.com: %86", RefTur: "esik_disk", RefID: 85}

	// Bayi sahipli domain 10.
	id := YazOlay(h.DB, esik, 10, AktorYok, 0)
	if id <= 0 {
		t.Fatal("eşik bildirimi yazılmalı")
	}
	if g, lo, n := em_gorunum(t, h, bt_bayi(t, 7), id); !g || lo != 1 || n != 1 {
		t.Errorf("bayi7 eşiği görmeli ve saymalı: gordu=%v liste=%d sayac=%d", g, lo, n)
	}
	if g, lo, n := em_gorunum(t, h, bt_musteri(t, 10), id); g || lo != 0 || n != 0 {
		t.Errorf("müşteri10 eşiği GÖRMEMELİ: gordu=%v liste=%d sayac=%d", g, lo, n)
	}
	// Kök sahipli domain 30 → yönetici görür, müşteri görmez.
	id30 := YazOlay(h.DB, esik, 30, AktorYok, 0)
	if id30 <= 0 {
		t.Fatal("kök domain eşiği yazılmalı")
	}
	if g, _, n := em_gorunum(t, h, bt_admin(t), id30); !g || n != 1 {
		t.Errorf("yönetici kök domainin eşiğini görmeli: gordu=%v sayac=%d", g, n)
	}
	if g, lo, n := em_gorunum(t, h, bt_musteri(t, 30), id30); g || lo != 0 || n != 0 {
		t.Errorf("müşteri30 eşiği GÖRMEMELİ: gordu=%v liste=%d sayac=%d", g, lo, n)
	}
	// Kategori süzgeciyle de kaçmaz (kapsam AND'lenir, genişletilmez).
	if y := bt_liste(t, h, bt_musteri(t, 10), "?kategori=kota"); len(y.Bildirimler) != 0 {
		t.Errorf("?kategori=kota müşteriye eşik göstermemeli: %+v", y.Bildirimler)
	}
	// Yazıcı: müşteri için okundu/tekilleştirme kaydı yok.
	if n := bt_sayi(t, h.DB, `SELECT COUNT(*) FROM cp_bildirim_okundu WHERE okuyan_tur='musteri'`); n != 0 {
		t.Errorf("müşteri okundu kaydı olmamalı: %d", n)
	}
}

// Öteki domain türleri müşteriye gitmeye DEVAM eder.
func TestEsik_OtekiTurlerMusteriyeGider(t *testing.T) {
	h := bh_kur(t)
	turler := []string{"ssl", "aski", "yedek", "websec", "antivirus", "zincir"}
	idler := map[string]int64{}
	for i, k := range turler {
		id := YazOlay(h.DB, Olay{Seviye: "uyari", Kategori: k, Baslik: "B " + k, Mesaj: "m", RefTur: "t", RefID: int64(i)}, 10, AktorYok, 0)
		if id <= 0 {
			t.Fatalf("%s yazılmalı", k)
		}
		idler[k] = id
	}
	YazOlay(h.DB, Olay{Seviye: "kritik", Kategori: "kota", Baslik: "Disk kotası doldu", Mesaj: "m", RefTur: "esik_disk", RefID: 100}, 10, AktorYok, 0)
	mus := bt_musteri(t, 10)
	y := bt_liste(t, h, mus, "")
	gor := bt_idler(y)
	for k, id := range idler {
		if _, ok := gor[id]; !ok {
			t.Errorf("müşteri %s bildirimini görmeli", k)
		}
	}
	if len(y.Bildirimler) != len(turler) || y.Okunmamis != len(turler) {
		t.Errorf("müşteri: %d satır / %d okunmamış, beklenen %d (kota hariç)", len(y.Bildirimler), y.Okunmamis, len(turler))
	}
	if n, _ := bh_sayac(t, h, mus); n != len(turler) {
		t.Errorf("müşteri sayacı %d, beklenen %d", n, len(turler))
	}
	if n, _ := bh_sayac(t, h, bt_bayi(t, 7)); n != len(turler)+1 {
		t.Errorf("bayi sayacı %d, beklenen %d (kota dahil)", n, len(turler)+1)
	}
}

// Karar öncesi müşteriye görünür yazılmış ESKİ eşik satırları da (göç yok)
// müşteri listesinden/rozetinden düşer; müşteri okundu işaretleyemez; sahip görür.
func TestEsik_EskiSatirlarMusteridenSuzulur(t *testing.T) {
	h := bh_kur(t)
	res := bt_exec(t, h.DB, `INSERT INTO cp_bildirim (seviye, kategori, baslik, mesaj, domain_id, reseller_id, ref_tur, ref_id)
		VALUES ('kritik','kota','Disk kotası doldu','m',10,7,'esik_disk',100)`)
	eski, _ := res.LastInsertId()
	Yaz(h.DB, "uyari", "ssl", "SSL sertifikası yenilenemedi", "m", 10, "esik_ssl", 1)
	mus := bt_musteri(t, 10)
	if g, lo, n := em_gorunum(t, h, mus, eski); g || lo != 1 || n != 1 {
		t.Errorf("eski eşik satırı müşteriden süzülmeli (yalnız ssl kalır): gordu=%v liste=%d sayac=%d", g, lo, n)
	}
	// Müşteri tek tek ve "tümünü okundu" ile eşik satırına okundu yazamaz.
	bt_okundu(t, h, mus, eski)
	bt_okundu(t, h, mus, 0)
	if n := bt_sayi(t, h.DB, `SELECT COUNT(*) FROM cp_bildirim_okundu WHERE bildirim_id=? AND okuyan_tur='musteri'`, eski); n != 0 {
		t.Errorf("müşteri kapsam dışı eşik satırını okundu işaretlememeli: %d", n)
	}
	if g, lo, n := em_gorunum(t, h, bt_bayi(t, 7), eski); !g || lo != 2 || n != 2 {
		t.Errorf("bayi eski eşiği görmeye devam etmeli: gordu=%v liste=%d sayac=%d", g, lo, n)
	}
}

// Tercih ekranı: müşteride "kota" seçeneği yok ve kaydedilemez; bayide var.
func TestEsik_MusteriTercihindeKotaYok(t *testing.T) {
	h := bh_kur(t)
	mus := bt_musteri(t, 10)
	kod, g := bh_istek(t, h, http.MethodGet, "/bildirim-tercihleri", mus, "")
	if kod != http.StatusOK || strings.Contains(g, `"kota"`) || !strings.Contains(g, `"ssl"`) || !strings.Contains(g, `"aski"`) {
		t.Errorf("müşteri tercih listesi kota içermemeli, ssl/aski içermeli: %d %s", kod, g)
	}
	if kod, _ := bh_istek(t, h, http.MethodPut, "/bildirim-tercihleri", mus, `{"tercihler":[{"kategori":"kota","panel":false}]}`); kod != http.StatusBadRequest {
		t.Errorf("müşteri kota tercihi kaydedememeli (400): %d", kod)
	}
	for _, tok := range []string{bt_bayi(t, 7), bt_admin(t)} {
		if kod, g := bh_istek(t, h, http.MethodGet, "/bildirim-tercihleri", tok, ""); kod != http.StatusOK || !strings.Contains(g, `"kota"`) {
			t.Errorf("sahip tercih listesi kota içermeli: %d %s", kod, g)
		}
	}
}
