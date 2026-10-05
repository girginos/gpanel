package websec

// geri_bildirim_test.go — "Tara" sonrası geri bildirim: domain düzeyi son sonuç,
// uygulamasız domainde son_tarama, feed uyarısı mantığı, tekrar tıklama kapısı.
// DB'li testler GOSP_TEST_GECICI_DSN yoksa atlanır (kapsam_db_test.go ile aynı kurulum).

import (
	"encoding/json"
	"strings"
	"testing"

	"girginospanel/internal/auth"
)

func TestFeedDegerlendir(t *testing.T) {
	for _, c := range []struct {
		ad                       string
		wpD, wpO, nD, nO, pD, pO bool
		denendi, hatali          bool
	}{
		{"0 uygulama: yem sorgulanmadı", false, false, false, false, false, false, false, false},
		{"wp denendi hiç yanıt yok", true, false, false, false, false, false, true, true},
		{"wp denendi başarılı", true, true, false, false, false, false, true, false},
		{"wp ok ama osv denendi ve düştü", true, true, true, false, false, false, true, true},
		{"yalnız php denendi başarılı", false, false, false, false, true, true, true, false},
	} {
		d, h := feedDegerlendir(c.wpD, c.wpO, c.nD, c.nO, c.pD, c.pO)
		if d != c.denendi || h != c.hatali {
			t.Errorf("%s: (%v,%v) beklenen (%v,%v)", c.ad, d, h, c.denendi, c.hatali)
		}
	}
}

func TestFeedHataIfadesi(t *testing.T) {
	if e, a := feedHataIfadesi(true, true); e != "last_error=?" || a != feedHataMetni {
		t.Errorf("hatalı: %q %v", e, a)
	}
	if e, a := feedHataIfadesi(true, false); e != "last_error=NULL" || a != nil {
		t.Errorf("sağlıklı: %q %v", e, a)
	}
	e, a := feedHataIfadesi(false, false)
	if !strings.Contains(e, "IF(last_error LIKE ?") || a != feedHataOnek+"%" {
		t.Errorf("denenmedi önceki uyarıyı korumalı: %q %v", e, a)
	}
	if !strings.HasPrefix(feedHataMetni, feedHataOnek) {
		t.Error("metin önekle başlamalı")
	}
}

// Uygulamasız domain: son_tarama + son_sonuc{0,0} dolu; taranmamış domain boş.
// Bayi yalnız kendi domaininin sonucunu görür (IDOR).
func TestDB_UygulamasizDomainSonSonuc(t *testing.T) {
	db := wsGeciciDB(t)
	wsTohum(t, db)
	wsExec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES
		(20, 'bos.test', 'bos', 7), (21, 'yeni.test', 'yeni', 7), (22, 'yabanci.test', 'yab', 8)`)
	wsExec(t, db, `INSERT INTO cp_websec_domain_tarama (domain_id, son_tarama) VALUES
		(20, '2026-10-05 14:57:17'), (22, '2026-10-05 15:00:00')`)
	kuyrukKur(t)
	h := &Handler{DB: db}

	kimlikKur(t, bayiJeton)
	rr := istek(t, h.Apps, "GET", "/websec/apps", "")
	var a envanterYanit
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &a) != nil {
		t.Fatalf("apps: %d %s", rr.Code, rr.Body)
	}
	bulunan := map[int64]envanterSatir{}
	for _, it := range a.Items {
		bulunan[it.DomainID] = it
	}
	if _, var_ := bulunan[22]; var_ {
		t.Fatal("bayi 7 başka bayinin domaininin sonucunu görmemeli")
	}
	bos := bulunan[20]
	if bos.Durum != "desteklenmiyor" || bos.SonTarama != "2026-10-05 14:57:17" || bos.DomainSonTarama != bos.SonTarama ||
		bos.SonSonuc == nil || bos.SonSonuc.UygulamaSayisi != 0 || bos.SonSonuc.BulguSayisi != 0 {
		t.Fatalf("uygulamasız domain: %+v", bos)
	}
	yeni := bulunan[21]
	if yeni.Durum != "beklemede" || yeni.SonTarama != "" || yeni.SonSonuc != nil {
		t.Fatalf("taranmamış domain boş kalmalı: %+v", yeni)
	}
	kendi := bulunan[10]
	if kendi.SonSonuc == nil || kendi.SonSonuc.UygulamaSayisi != 1 || kendi.SonSonuc.BulguSayisi != 1 {
		t.Fatalf("uygulamalı domain sonucu: %+v", kendi.SonSonuc)
	}
}

// Gerçek tarama (uygulamasız domain, ağ yok): son_tarama yazılır, feed alarmı YOK;
// sonra feed uyarısı kalıcı: bayi bool görür (metin yok), 0 uygulamalı tarama silmez.
func TestDB_FeedUyarisiVeSifirUygulama(t *testing.T) {
	db := wsGeciciDB(t)
	wsTohum(t, db)
	kuyrukKur(t)
	h := &Handler{DB: db}
	wsExec(t, db, `UPDATE cp_websec_status SET last_error=NULL WHERE id=1`)
	wsExec(t, db, `DELETE FROM cp_websec_domain_tarama`)

	if err := TaraDomains(t.Context(), db, []int64{10}); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM cp_websec_domain_tarama WHERE domain_id=10`).Scan(&n)
	var le *string
	_ = db.QueryRow(`SELECT last_error FROM cp_websec_status WHERE id=1`).Scan(&le)
	if n != 1 || le != nil {
		t.Fatalf("0 uygulamalı tarama: damga=%d last_error=%v (alarm olmamalı)", n, le)
	}

	wsExec(t, db, `UPDATE cp_websec_status SET last_error=? WHERE id=1`, feedHataMetni)
	feedUyariBayrak.Store(true)
	t.Cleanup(func() { feedUyariBayrak.Store(false) })
	kimlikKur(t, bayiJeton)
	rr := istek(t, h.Status, "GET", "/websec/status", "")
	var d map[string]json.RawMessage
	_ = json.Unmarshal(rr.Body.Bytes(), &d)
	if string(d["feed_uyari"]) != "true" {
		t.Fatalf("bayi feed_uyari true olmalı: %s", rr.Body)
	}
	if _, var_ := d["last_error"]; var_ || strings.Contains(rr.Body.String(), "erişilemedi") {
		t.Fatalf("bayiye hata metni gitmemeli: %s", rr.Body)
	}
	kimlikKur(t, kokJeton)
	rr = istek(t, h.Status, "GET", "/websec/status", "")
	_ = json.Unmarshal(rr.Body.Bytes(), &d)
	if string(d["feed_uyari"]) != "true" {
		t.Fatalf("kök feed_uyari true olmalı: %s", rr.Body)
	}

	// 0 uygulamalı kısmi tarama uyarıyı SİLMEZ (yem sorgulanmadı → bilgi yok).
	if err := TaraDomains(t.Context(), db, []int64{10}); err != nil {
		t.Fatal(err)
	}
	_ = db.QueryRow(`SELECT last_error FROM cp_websec_status WHERE id=1`).Scan(&le)
	if le == nil || *le != feedHataMetni {
		t.Fatalf("uyarı korunmalı: %v", le)
	}
	if !feedUyariBayrak.Load() {
		t.Fatal("0 uygulamalı tarama bayrağı silmemeli")
	}
	// Başka bir hata ise 0 uygulamalı taramada temizlenir (eski davranış).
	wsExec(t, db, `UPDATE cp_websec_status SET last_error='başka hata' WHERE id=1`)
	if err := TaraDomains(t.Context(), db, []int64{10}); err != nil {
		t.Fatal(err)
	}
	_ = db.QueryRow(`SELECT last_error FROM cp_websec_status WHERE id=1`).Scan(&le)
	if le != nil {
		t.Fatalf("başka hata temizlenmeli: %v", *le)
	}
	feedBayrakGuncelle(true, false) // yem sağlıklı sorgulandı
	kimlikKur(t, bayiJeton)
	rr = istek(t, h.Status, "GET", "/websec/status", "")
	_ = json.Unmarshal(rr.Body.Bytes(), &d)
	if string(d["feed_uyari"]) != "false" {
		t.Fatalf("uyarı yok: %s", rr.Body)
	}
}

// Tekrar tıklama: sırada ya da taranıyorsa "zaten", kuyruğa/denetime ikinci satır yok.
func TestDB_TekrarTiklamaKuyruguSisirmez(t *testing.T) {
	db := wsGeciciDB(t)
	wsTohum(t, db)
	k := kuyrukKur(t)
	saatKur(t)
	h := &Handler{DB: db}

	kimlikKur(t, bayiJeton)
	ilk := istek(t, h.Rescan, "POST", "/websec/rescan?domain_id=10", "")
	if ilk.Code != 202 || strings.TrimSpace(ilk.Body.String()) != `{"sirada":1}` {
		t.Fatalf("ilk: %d %s", ilk.Code, ilk.Body)
	}
	ikinci := istek(t, h.Rescan, "POST", "/websec/rescan?domain_id=10", "")
	if ikinci.Code != 202 || !strings.Contains(ikinci.Body.String(), `"zaten":true`) {
		t.Fatalf("ikinci tıklama zaten dönmeli: %d %s", ikinci.Code, ikinci.Body)
	}
	if wsDenetimSayisi(t, db) != 1 || k.bekleyenSayisi() != 1 {
		t.Fatalf("denetim=%d kuyruk=%d, ikisi de 1 olmalı", wsDenetimSayisi(t, db), k.bekleyenSayisi())
	}

	// Taranıyor: bayi ve kök tekrar istek → zaten, kuyruğa girmez.
	k.sonrakiIs()
	kosmaMu.Lock()
	kosuyor, taranEden = true, map[int64]bool{10: true}
	kosmaMu.Unlock()
	t.Cleanup(func() {
		kosmaMu.Lock()
		kosuyor, taranEden = false, map[int64]bool{}
		kosmaMu.Unlock()
	})
	bay := istek(t, h.Rescan, "POST", "/websec/rescan?domain_id=10", "")
	if bay.Code != 202 || !strings.Contains(bay.Body.String(), `"zaten":true`) {
		t.Fatalf("bayi taranan domain: %d %s", bay.Code, bay.Body)
	}
	kimlikKur(t, kokJeton)
	kok := istek(t, h.Rescan, "POST", "/websec/rescan?domain_id=10", "")
	if kok.Code != 202 || !strings.Contains(kok.Body.String(), `"zaten":true`) {
		t.Fatalf("kök taranan domain: %d %s", kok.Code, kok.Body)
	}
	if wsDenetimSayisi(t, db) != 1 || k.bekleyenSayisi() != 0 {
		t.Fatalf("taranan domain kuyruğa/denetime girmemeli: denetim=%d kuyruk=%d", wsDenetimSayisi(t, db), k.bekleyenSayisi())
	}
	// Başka domain taranıyorken diğeri normal sıraya girer.
	kimlikKur(t, &auth.Claims{UserID: 8, Username: "bayi8", Role: "reseller", ResellerID: 8})
	d11 := istek(t, h.Rescan, "POST", "/websec/rescan?domain_id=11", "")
	if d11.Code != 202 || strings.TrimSpace(d11.Body.String()) != `{"sirada":1}` {
		t.Fatalf("farklı domain normal sıraya girmeli: %d %s", d11.Code, d11.Body)
	}
}
