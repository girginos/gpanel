package domains

// olustur_bayi_adina_test.go — POST /domains `bayi_id` (yalnız kök): hosting bayi ADINA açılır ve
// bayinin kuralları (kota, askı, plan kapsamı) bayi kendisi açıyormuş gibi uygulanır.
// Yardımcılar olustur_plan_php_test.go (opp_olustur) ve sunucu_hatasi_test.go'da.

import (
	"net/http"
	"testing"

	"girginospanel/internal/sahtedb"
)

func TestCreate_BayiAdina_YalnizKok_KurallarBayininki(t *testing.T) {
	tok := sh_tokenlar(t)
	bayiVar := func(sd *sahtedb.DB) {
		sd.Satirlar("SELECT 1 FROM users WHERE id=? AND role='reseller'", []any{int64(1)})
	}

	// Bayi oturumu bayi_id kullanamaz (başka bayi adına hosting = IDOR).
	kod, govde, n := opp_olustur(t, tok["bayi"], `{"alan_adi":"adina1.test","bayi_id":8}`, bayiVar, errSaglamayaIndi)
	if kod != http.StatusForbidden || n != 0 {
		t.Fatalf("bayi oturumu bayi_id: 403 beklenirdi: %d %s (sağlama %d)", kod, govde, n)
	}
	// Olmayan / bayi olmayan / sıfır kimlik → 400, sağlamaya inilmez.
	for _, g := range []string{`{"alan_adi":"adina2.test","bayi_id":999}`, `{"alan_adi":"adina2.test","bayi_id":0}`, `{"alan_adi":"adina2.test","bayi_id":-3}`} {
		kod, govde, n = opp_olustur(t, tok["admin"], g, nil, errSaglamayaIndi)
		if kod != http.StatusBadRequest || n != 0 || sh_hataMetni(t, govde) != "bayi bulunamadı" {
			t.Errorf("%s: 400 'bayi bulunamadı' beklenirdi: %d %s (sağlama %d)", g, kod, govde, n)
		}
	}
	// Var olan bayi: bayinin kota/askı kapısından geçer, sağlamaya iner.
	if kod, govde, n = opp_olustur(t, tok["admin"], `{"alan_adi":"adina3.test","bayi_id":7}`, bayiVar, errSaglamayaIndi); n != 1 {
		t.Fatalf("kök + var olan bayi sağlamaya inmeliydi: %d %s", kod, govde)
	}
	// Bayinin KOTASI uygulanır: limit dolu → 403 (kök kendi adına açsaydı kota yoktu).
	dolu := func(sd *sahtedb.DB) {
		bayiVar(sd)
		sd.Satirlar("SELECT max_domain, max_disk_mb, status FROM users", []any{int64(1), int64(0), "active"})
		sd.Satirlar("SELECT COUNT(*) FROM domains WHERE reseller_id=?", []any{int64(1)})
	}
	kod, govde, n = opp_olustur(t, tok["admin"], `{"alan_adi":"adina4.test","bayi_id":7}`, dolu, errSaglamayaIndi)
	if kod != http.StatusForbidden || n != 0 || sh_hataMetni(t, govde) != "hosting hesabı limitiniz dolu (1/1)" {
		t.Errorf("bayinin kotası uygulanmalı: %d %s (sağlama %d)", kod, govde, n)
	}
	// Bayi ASKIDA → hosting açılmaz.
	askida := func(sd *sahtedb.DB) {
		bayiVar(sd)
		sd.Satirlar("SELECT max_domain, max_disk_mb, status FROM users", []any{int64(0), int64(0), "suspended"})
	}
	kod, govde, n = opp_olustur(t, tok["admin"], `{"alan_adi":"adina5.test","bayi_id":7}`, askida, errSaglamayaIndi)
	if kod != http.StatusForbidden || n != 0 {
		t.Errorf("askıdaki bayi adına hosting açılmamalı: %d %s (sağlama %d)", kod, govde, n)
	}
	// Bayinin GÖREMEDİĞİ plan → 403 (kök kendi adına açsaydı katalog planı serbestti).
	kapsamDisi := func(sd *sahtedb.DB) { bayiVar(sd) }
	kod, govde, n = opp_olustur(t, tok["admin"], `{"alan_adi":"adina6.test","bayi_id":7,"plan_id":5}`, kapsamDisi, errSaglamayaIndi)
	if kod != http.StatusForbidden || n != 0 {
		t.Errorf("bayinin plan kapsamı uygulanmalı: %d %s (sağlama %d)", kod, govde, n)
	}
}
