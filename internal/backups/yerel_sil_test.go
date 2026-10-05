package backups

// yerel_sil_test.go — "Drive'a gönderilince yerel kopyayı sil" özelliğinin backups paketi tarafı:
// yerel dosyası olmayan (yalnız Drive'da) yedeğin liste/indirme/geri yükleme/bozulma taraması davranışı
// ve ayar/temizlik uçları. Gerçek yedek dizinine (BackupRoot) YAZILMAZ: dosya hiç oluşturulmaz, böylece
// "yerelde yok" durumu doğal olarak elde edilir. Geçici MariaDB gerekir (GOSP_TEST_GECICI_DSN).

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/auth"
	"girginospanel/internal/backups/hedef"
	"girginospanel/internal/middleware"
)

const ysSK = "c_yerelsil_testi"

// ysYedek — yerelde dosyası OLMAYAN yedek kaydı (+ istenirse doğrulanmış Drive kopyası).
func ysYedek(t *testing.T, o *uzakOrtam, domainID int64, tip, dosya string, driveTamam, yerelYok bool) int64 {
	t.Helper()
	g113_exec(t, o.db, `INSERT IGNORE INTO domains (id, alan_adi, sistem_kullanici) VALUES (?,?,?)`, domainID, "yerelsil.test", ysSK)
	res, err := o.db.Exec(`INSERT INTO backups (domain_id, tip, dosya, boyut_b, sha256) VALUES (?,?,?,?,?)`,
		domainID, tip, dosya, 5000, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	if yerelYok {
		g113_exec(t, o.db, `UPDATE backups SET yerel_yok=NOW() WHERE id=?`, id)
	}
	if driveTamam {
		g113_exec(t, o.db, `INSERT INTO backup_uzak_kopyalar (backup_id, domain_id, kaynak, hedef_id, durum, yedek_dosya, uzak_ad, manifest_ad, boyut_b, boyut_duz, son_dogrulama, sifreleme, anahtar_parmak)
			VALUES (?,?,'hedef',?,'tamam',?,?,?,5100,5000,NOW(),'x25519',?)`,
			id, domainID, o.hedefID, dosya, "k/"+dosya+".age", "k/"+dosya+".age.manifest.json", strings.Repeat("b", 64))
	}
	return id
}

func ysRouter(t *testing.T, o *uzakOrtam) http.Handler {
	t.Helper()
	o.db.SetMaxOpenConns(4) // List, satırlar açıkken ikinci sorgu çalıştırır (g113_db tek bağlantı verir)
	h := &Handlers{DB: o.db}
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(hh_sir))
		r.Get("/domains/{id}/backups", h.List)
		r.Get("/domains/{id}/backups/{bid}/indir", h.Download)
	})
	return r
}

func ysGet(t *testing.T, r http.Handler, tok, yol string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", yol, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestYerelSil_ListeYalnizDriveRozeti(t *testing.T) {
	o := yeniUzakOrtam(t)
	admin, _, _ := hh_tokenlar(t)
	drive := ysYedek(t, o, 1, "oto", ysSK+"-20260801-010101.tar.gz", true, true)
	yok := ysYedek(t, o, 1, "oto", ysSK+"-20260802-010101.tar.gz", false, false)
	kod, g := ysGet(t, ysRouter(t, o), admin, "/domains/1/backups")
	if kod != 200 {
		t.Fatalf("%d %s", kod, g)
	}
	var liste []Yedek
	if err := json.Unmarshal([]byte(g), &liste); err != nil || len(liste) != 2 {
		t.Fatalf("liste: %v %s", err, g)
	}
	for _, y := range liste {
		switch y.ID {
		case drive:
			if y.Mevcut != "drive" || y.YerelVar || !y.DriveVar {
				t.Errorf("Drive'da duran yedek: %+v", y)
			}
		case yok:
			if y.Mevcut != "yok" || y.YerelVar || y.DriveVar {
				t.Errorf("hiçbir yerde olmayan yedek: %+v", y)
			}
		}
	}
	// Drive kopyası sonradan silinmişse (saklama) kayıt "yalnız Drive'da" DEĞİL "yok" görünmeli
	g113_exec(t, o.db, `UPDATE backup_uzak_kopyalar SET durum='silindi'`)
	_, g = ysGet(t, ysRouter(t, o), admin, "/domains/1/backups")
	if strings.Contains(g, `"mevcut":"drive"`) {
		t.Error("Drive kopyası silinmişken 'drive' görünmemeli")
	}
}

func TestYerelSil_IndirmeDosyaYokkenSadeHata(t *testing.T) {
	o := yeniUzakOrtam(t)
	admin, bayi, _ := hh_tokenlar(t)
	r := ysRouter(t, o)
	dosya := ysSK + "-20260801-010101.tar.gz"
	drive := ysYedek(t, o, 1, "oto", dosya, true, true)
	yok := ysYedek(t, o, 1, "oto", ysSK+"-20260802-010101.tar.gz", false, false)
	var isNo int64
	_ = o.db.QueryRow(`SELECT id FROM backup_uzak_kopyalar WHERE backup_id=?`, drive).Scan(&isNo)

	kod, g := ysGet(t, r, admin, "/domains/1/backups/"+itoa(drive)+"/indir")
	if kod != http.StatusConflict || !strings.Contains(g, "yalnız Google Drive") || !strings.Contains(g, "yedek-coz --indir "+itoa(isNo)) {
		t.Fatalf("yönetici: açık yönlendirme beklenirdi: %d %s", kod, g)
	}
	for _, yasak := range []string{BackupRoot, ysSK, "no such file", "open "} {
		if strings.Contains(g, yasak) {
			t.Errorf("yanıt yerel yol/ham hata sızdırıyor: %q", yasak)
		}
	}
	kod, g = ysGet(t, r, bayi, "/domains/1/backups/"+itoa(drive)+"/indir")
	if kod != http.StatusConflict || strings.Contains(g, "girginospanel-server") || !strings.Contains(g, "yalnız Google Drive") {
		t.Errorf("bayi: sunucu komutu görmemeli: %d %s", kod, g)
	}
	// Drive kopyası da yoksa: 500 DEĞİL 404, yol yok
	kod, g = ysGet(t, r, admin, "/domains/1/backups/"+itoa(yok)+"/indir")
	if kod != http.StatusNotFound || strings.Contains(g, BackupRoot) {
		t.Errorf("dosyası hiçbir yerde olmayan yedek: %d %s", kod, g)
	}
}

func TestYerelSil_GeriYuklemeHazirligiDriveYalnizHatasi(t *testing.T) {
	o := yeniUzakOrtam(t)
	dosya := ysSK + "-20260801-010101.tar.gz"
	y := ysYedek(t, o, 1, "oto", dosya, true, true)
	var isNo int64
	_ = o.db.QueryRow(`SELECT id FROM backup_uzak_kopyalar WHERE backup_id=?`, y).Scan(&isNo)
	_, err := YerelDosyaHazirla(t.Context(), o.db, ysSK, dosya)
	if err == nil || !isYalnizDrive(err) || !strings.Contains(err.Error(), "yedek-coz --indir "+itoa(isNo)) {
		t.Fatalf("açık Drive yönlendirmesi beklenirdi: %v", err)
	}
	if _, _, err := UzakIndirmeBaslat(o.db, ysSK, dosya); err == nil || !isYalnizDrive(err) {
		t.Fatalf("UzakIndirmeBaslat da aynı: %v", err)
	}
	// Drive kopyası olmayan yedek eski davranışı korur ("diskte bulunamadı")
	d2 := ysSK + "-20260802-010101.tar.gz"
	ysYedek(t, o, 1, "oto", d2, false, false)
	if _, err := YerelDosyaHazirla(t.Context(), o.db, ysSK, d2); err != errYedekDiskteYok {
		t.Fatalf("eski davranış: %v", err)
	}
	// hazirlikHatasi: bayi sunucu komutunu görmez
	admin, bayi, _ := hh_tokenlar(t)
	for rol, tok := range map[string]string{"admin": admin, "bayi": bayi} {
		var kod int
		var govde string
		uc := hh_uclar[0]
		kod, govde = hh_cagir(t, uc, tok, &driveYalnizHata{is: isNo})
		if kod != http.StatusNotFound || !strings.Contains(govde, "yalnız Google Drive") {
			t.Errorf("%s: %d %s", rol, kod, govde)
		}
		if rol == "bayi" && strings.Contains(govde, "girginospanel-server") {
			t.Errorf("bayi sunucu komutu görüyor: %s", govde)
		}
	}
}

func isYalnizDrive(err error) bool {
	for e := err; e != nil; {
		if e == errYedekYalnizDrive {
			return true
		}
		if x, ok := e.(interface{ Is(error) bool }); ok && x.Is(errYedekYalnizDrive) {
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

// Bit-rot taraması: yerel dosyası Drive'a gönderilince silinen yedek BOZUK sayılmaz (yanlış kritik alarm yok);
// Drive kopyası da yoksa alarm hâlâ çalışır.
func TestYerelSil_BozulmaTaramasiYalnizDriveyiBozukSaymaz(t *testing.T) {
	o := yeniUzakOrtam(t)
	drive := ysYedek(t, o, 1, "oto", ysSK+"-20260801-010101.tar.gz", true, true)
	toplam, bozuk := verifyYedekBozulma(o.db)
	if toplam != 1 || bozuk != 0 {
		t.Fatalf("yalnız Drive'daki yedek bozuk sayılmamalı: toplam=%d bozuk=%d", toplam, bozuk)
	}
	var d string
	_ = o.db.QueryRow(`SELECT dogrulama FROM backups WHERE id=?`, drive).Scan(&d)
	if d != "uzakta" {
		t.Errorf("dogrulama=%q (uzakta bekleniyordu)", d)
	}
	// kontrol: Drive kopyası silinmişse (hiçbir yerde yok) alarm hâlâ çalışır
	g113_exec(t, o.db, `UPDATE backup_uzak_kopyalar SET durum='silindi'`)
	g113_exec(t, o.db, `UPDATE backups SET dogrulama=''`)
	_, bozuk = verifyYedekBozulma(o.db)
	if bozuk != 1 {
		t.Errorf("hiçbir yerde olmayan yedek için alarm çalışmalı: bozuk=%d", bozuk)
	}
}

func TestYerelSil_AyarUclari(t *testing.T) {
	o := yeniUzakOrtam(t)
	h := itoa(o.hedefID)
	oku := func() (yerelSil bool, tut int) {
		_, g := o.cagri("GET", "/uzak-kopya", "")
		var d struct {
			Ayar struct {
				YerelSil bool `json:"yerel_sil"`
				YerelTut int  `json:"yerel_tut"`
			} `json:"ayar"`
			Yerel map[string]any `json:"yerel"`
		}
		if err := json.Unmarshal([]byte(g), &d); err != nil || d.Yerel == nil {
			t.Fatalf("durum: %v %s", err, g)
		}
		return d.Ayar.YerelSil, d.Ayar.YerelTut
	}
	if ys, tut := oku(); ys || tut != 1 {
		t.Fatalf("varsayılan kapalı/1: %v %d", ys, tut)
	}
	if kod, g := o.cagri("PUT", "/uzak-kopya/ayar", `{"aktif":true,"hedef_id":`+h+`,"klasor":"gp","saklama_n":7,"yerel_sil":true,"yerel_tut":3}`); kod != 200 {
		t.Fatalf("ayar: %d %s", kod, g)
	}
	if ys, tut := oku(); !ys || tut != 3 {
		t.Fatalf("kaydedilmedi: %v %d", ys, tut)
	}
	// alanlar HİÇ gönderilmezse mevcut değer korunur (eski istemci 0'a düşüremez)
	if kod, g := o.cagri("PUT", "/uzak-kopya/ayar", `{"aktif":true,"hedef_id":`+h+`,"klasor":"gp","saklama_n":8}`); kod != 200 {
		t.Fatalf("ayar2: %d %s", kod, g)
	}
	if ys, tut := oku(); !ys || tut != 3 {
		t.Fatalf("eski istemci yerel ayarı bozdu: %v %d", ys, tut)
	}
	if kod, _ := o.cagri("PUT", "/uzak-kopya/ayar", `{"aktif":true,"hedef_id":`+h+`,"klasor":"gp","saklama_n":8,"yerel_tut":11}`); kod != 400 {
		t.Errorf("yerel_tut=11 400 olmalı: %d", kod)
	}
	if kod, _ := o.cagri("PUT", "/uzak-kopya/ayar", `{"aktif":true,"hedef_id":`+h+`,"klasor":"gp","saklama_n":8,"yerel_sil":false}`); kod != 200 {
		t.Fatal("kapatma")
	}
	if ys, tut := oku(); ys || tut != 3 {
		t.Fatalf("kapatma: %v %d", ys, tut)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	var say int
	for _, d := range o.denetim {
		if strings.HasPrefix(d, "yedek.uzak.yerel_sil|") {
			say++
			if strings.Contains(d, "AGE-") || strings.Contains(d, BackupRoot) {
				t.Errorf("denetim sızdırıyor: %s", d)
			}
		}
	}
	if say != 2 { // açıldı + kapatıldı (tut değişmeden saklama değişikliği denetim üretmez)
		t.Errorf("yerel_sil denetim kayıtları: %d (%v)", say, o.denetim)
	}
}

func TestYerelSil_TemizlikUclariOnizlemeVeCalistirma(t *testing.T) {
	o := yeniUzakOrtam(t)
	// yedek yok: boş önizleme + boş sonuç (ağa çıkılmaz, 500 yok)
	kod, g := o.cagri("GET", "/uzak-kopya/yerel-temizle", "")
	var on struct {
		Silinecek int `json:"silinecek_adet"`
		YerelTut  int `json:"yerel_tut"`
	}
	if kod != 200 || json.Unmarshal([]byte(g), &on) != nil || on.Silinecek != 0 || on.YerelTut != 1 {
		t.Fatalf("önizleme: %d %s", kod, g)
	}
	// dosyası zaten olmayan, Drive'da doğrulanmış yedek: yalnız İŞARETLENİR (silinecek bir şey yok)
	y := ysYedek(t, o, 1, "oto", ysSK+"-20260801-010101.tar.gz", true, false)
	// Drive hedefi ulaşılamaz: işaretleme de yapılmaz (şüphede dokunma), neden raporlanır
	eski := uzakHedefKur
	t.Cleanup(func() { uzakHedefKur = eski })
	uzakHedefKur = func(string, json.RawMessage, map[string]string, func(string)) (hedef.Hedef, error) {
		return nil, errors.New("test: hedef yok")
	}
	kod, g = o.cagri("POST", "/uzak-kopya/yerel-temizle", "")
	var s struct {
		Silinen  int `json:"silinen"`
		Atlanan  int `json:"atlanan"`
		Nedenler []struct {
			Neden string `json:"neden"`
		} `json:"nedenler"`
	}
	if kod != 200 || json.Unmarshal([]byte(g), &s) != nil || s.Silinen != 0 || s.Atlanan != 1 || len(s.Nedenler) != 1 {
		t.Fatalf("temizle: %d %s", kod, g)
	}
	var yy int
	_ = o.db.QueryRow(`SELECT yerel_yok IS NOT NULL FROM backups WHERE id=?`, y).Scan(&yy)
	if yy != 0 {
		t.Error("Drive'a ulaşılamazken işaret yazılmamalı")
	}
	if strings.Contains(g, BackupRoot) {
		t.Error("yanıt yerel yol içeriyor")
	}
}

var _ = sql.ErrNoRows
var _ = auth.IssueAt
