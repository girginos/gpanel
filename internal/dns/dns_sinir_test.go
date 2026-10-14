package dns

// dns_sinir_test.go — canlı API doğrulamasında (VM, 2026-10-13) bulunan sınır hataları:
// negatif TTL sessizce 3600, yabancı/olmayan kaydı DELETE 200, SOA sayısal taşma 500 +
// ham MySQL iletisi, hostmaster'da ikinci '@', 2 MiB üstü içe aktarma gövdesinin sessizce
// kesilmesi, TXT içindeki tırnağın zone'da silinmesi.

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func ds_cagir(t *testing.T, db *sql.DB, yontem, desen, yol, tip, govde string, isle func(*Handlers) http.HandlerFunc) (int, string) {
	t.Helper()
	return zh_cagir(t, db, zh_tokenlar(t)["admin"], yontem, desen, yol, tip, govde, isle)
}

func TestCreateUpdate_NegatifTTLReddedilir(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	kod, govde := ds_cagir(t, db, http.MethodPost, "/domains/{id}/dns", "/domains/42/dns", "application/json",
		`{"ad":"neg","tip":"A","deger":"192.0.2.8","ttl":-5}`, func(h *Handlers) http.HandlerFunc { return h.Create })
	if kod != http.StatusBadRequest {
		t.Errorf("negatif TTL 400 beklenirdi: %d %s", kod, govde)
	}
	kod, govde = ds_cagir(t, db, http.MethodPost, "/domains/{id}/dns", "/domains/42/dns", "application/json",
		`{"ad":"sifir","tip":"A","deger":"192.0.2.8","ttl":0}`, func(h *Handlers) http.HandlerFunc { return h.Create })
	if kod != http.StatusCreated && kod != http.StatusOK {
		t.Fatalf("ttl=0 (varsayılan) kabul edilmeli: %d %s", kod, govde)
	}
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_records WHERE ad='sifir' AND ttl=3600`); n != 1 {
		t.Error("ttl=0 varsayılan 3600 olmalı")
	}
	rid := zi_ekle(t, db, 42, "www", "A", "192.0.2.9", 600, 0, 1)
	if kod, govde := zi_put(t, db, fmt.Sprint(rid), `{"ttl":-1}`); kod != http.StatusBadRequest {
		t.Errorf("PUT negatif TTL 400 beklenirdi: %d %s", kod, govde)
	}
	if n := dk_sayi(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM dns_records WHERE id=%d AND ttl=600`, rid)); n != 1 {
		t.Error("reddedilen PUT TTL'yi değiştirdi")
	}
}

func TestDelete_OlmayanVeYabanciKayit404(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	yabanci := zi_ekle(t, db, 43, "x", "A", "192.0.2.10", 600, 0, 1)
	for _, rid := range []string{fmt.Sprint(yabanci), "99999999"} {
		kod, govde := ds_cagir(t, db, http.MethodDelete, "/domains/{id}/dns/{rid}", "/domains/42/dns/"+rid, "", "",
			func(h *Handlers) http.HandlerFunc { return h.Delete })
		if kod != http.StatusNotFound || !strings.Contains(govde, "bulunamadı") {
			t.Errorf("rid=%s: 404 beklenirdi: %d %s", rid, kod, govde)
		}
	}
	if n := dk_sayi(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM dns_records WHERE id=%d`, yabanci)); n != 1 {
		t.Error("başka domainin kaydı silindi")
	}
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM audit_log WHERE action='dns.kayit.sil'`); n != 0 {
		t.Errorf("silinmeyen kayıt için denetim satırı yazılmamalı: %d", n)
	}
	kendi := zi_ekle(t, db, 42, "y", "A", "192.0.2.11", 600, 0, 1)
	if kod, govde := ds_cagir(t, db, http.MethodDelete, "/domains/{id}/dns/{rid}", fmt.Sprintf("/domains/42/dns/%d", kendi), "", "",
		func(h *Handlers) http.HandlerFunc { return h.Delete }); kod != http.StatusOK {
		t.Errorf("kendi kaydını silme 200 beklenirdi: %d %s", kod, govde)
	}
}

func TestPutSOA_SayisalSinirVeHostmaster(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	soa := func(govde string) (int, string) {
		return ds_cagir(t, db, http.MethodPut, "/domains/{id}/dns/soa", "/domains/42/dns/soa", "application/json", govde,
			func(h *Handlers) http.HandlerFunc { return h.PutSOA })
	}
	for _, g := range []string{`{"refresh":2147483648}`, `{"retry":4294967296}`, `{"expire":9999999999}`,
		`{"minimum":2147483648}`, `{"ttl":2147483648}`, `{"hostmaster":"a@b@c.com"}`} {
		if kod, govde := soa(g); kod != http.StatusBadRequest || strings.Contains(govde, "Error 1264") {
			t.Errorf("%s: 400 (ham DB iletisi olmadan) beklenirdi: %d %s", g, kod, govde)
		}
	}
	if kod, govde := soa(`{"refresh":2147483647,"hostmaster":"admin@bayialan.test"}`); kod != http.StatusOK {
		t.Errorf("sınırdaki değer kabul edilmeli: %d %s", kod, govde)
	}
}

func TestIceAktar_SiniriAsanGovde413VeHicbirKayitEklenmez(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	govde := "$ORIGIN bayialan.test.\nimp9 3600 IN A 192.0.2.219\n" + strings.Repeat("; dolgu satırı\n", 3<<20/15)
	kod, yanit := ds_cagir(t, db, http.MethodPost, "/domains/{id}/dns/ice-aktar", "/domains/42/dns/ice-aktar", "text/plain", govde,
		func(h *Handlers) http.HandlerFunc { return h.IceAktar })
	if kod != http.StatusRequestEntityTooLarge {
		t.Errorf("2 MiB üstü gövde 413 beklenirdi: %d %s", kod, yanit)
	}
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_records WHERE domain_id=42`); n != 0 {
		t.Errorf("kesilen gövdeden kayıt eklendi: %d", n)
	}
}

// TXT içindeki tırnak zone'da kaçışla yayınlanır (DB'deki metinle aynı); ters bölü içeren
// eski değerlerde eski davranış (tırnak silinir) korunur.
func TestTxtQuote_IcTirnakKacislaYayinlanir(t *testing.T) {
	if q := txtQuote(`abc"def`); q != `"abc\"def"` {
		t.Errorf("iç tırnak kaçışla yazılmalı: %s", q)
	}
	parcalar, err := tirnakliBol(txtQuote(strings.Repeat(`a"`, 200)))
	if err != nil {
		t.Fatalf("uzun tırnaklı metin ayrıştırılamadı: %v", err)
	}
	var cozulen strings.Builder
	for _, p := range parcalar {
		cozulen.WriteString(txtCoz(p[1 : len(p)-1]))
	}
	if cozulen.String() != strings.Repeat(`a"`, 200) {
		t.Error("bölünmüş tırnaklı metin özgün metni vermiyor")
	}
	if q := txtQuote(`a\"b`); q != `"a\b"` {
		t.Errorf("ters bölülü eski değerde tırnak silinmeli: %s", q)
	}
}
