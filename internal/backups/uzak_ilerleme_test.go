package backups

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"girginospanel/internal/backups/uzak"
)

// GET /uzak-kopya/ilerleme: sözleşme (anahtarlar), işçisizken aktif_mi=false, işçi canlıyken
// true, önbelleksiz ve sızıntısız.
func TestUzakKopyaIlerleme_SozlesmeVeSizintiYok(t *testing.T) {
	o := yeniUzakOrtam(t)
	_, gu := o.cagri("POST", "/uzak-kopya/anahtar-uret", `{}`)
	var tam map[string]any
	_ = json.Unmarshal([]byte(gu), &tam)
	ozel := ozelAnahtarSatiri(tam["kurtarma_dosyasi"].(string))
	o.cagri("POST", "/uzak-kopya/anahtar-onayla", `{"parmak":"`+tam["parmak"].(string)+`","sakladim":true}`)
	o.cagri("PUT", "/uzak-kopya/ayar", `{"aktif":true,"hedef_id":`+itoa(o.hedefID)+`,"klasor":"gp","saklama_n":7}`)
	g113_exec(t, o.db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) VALUES (1,'a.example','c_a')`)
	g113_exec(t, o.db, `INSERT INTO backups (id, domain_id, tip, dosya, boyut_b) VALUES (10,1,'oto','c_a-20260825-020000.tar.gz',5000)`)

	// boş kuyruk, işçi yok
	uzakIsci.Store(nil)
	req := httptest.NewRequest("GET", "/uzak-kopya/ilerleme", nil)
	rec := httptest.NewRecorder()
	o.r.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"aktif", "kuyruk", "aktif_mi"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("yanıtta %q yok: %s", k, rec.Body.String())
		}
	}
	if string(m["aktif"]) != "null" || string(m["aktif_mi"]) != "false" {
		t.Fatalf("boş kuyruk: %s", rec.Body.String())
	}
	var kq map[string]json.RawMessage
	_ = json.Unmarshal(m["kuyruk"], &kq)
	for _, k := range []string{"toplam_is", "tamam", "hata", "bekleyen", "toplam_bayt", "tamamlanan_bayt", "kalan_bayt", "yuzde", "hiz_bps", "eta_sn"} {
		if _, ok := kq[k]; !ok {
			t.Fatalf("kuyruk.%s yok: %s", k, m["kuyruk"])
		}
	}
	if string(kq["eta_sn"]) != "null" {
		t.Fatalf("ölçüm yokken eta_sn null: %s", kq["eta_sn"])
	}

	// iş kuyrukta, işçi var ve canlı → aktif_mi true
	if kod, g := o.cagri("POST", "/uzak-kopya/simdi-gonder/10", ""); kod != 202 {
		t.Fatalf("%d %s", kod, g)
	}
	kod, g := o.cagri("GET", "/uzak-kopya/ilerleme", "")
	if kod != 200 || !strings.Contains(g, `"aktif_mi":false`) || !strings.Contains(g, `"bekleyen":1`) {
		t.Fatalf("işçi yokken bekleyen iş: %d %s", kod, g)
	}
	i := &uzak.Isci{}
	i.Izle().Calisiyor(true)
	uzakIsci.Store(i)
	t.Cleanup(func() { uzakIsci.Store(nil) })
	kod, g = o.cagri("GET", "/uzak-kopya/ilerleme", "")
	if kod != 200 || !strings.Contains(g, `"aktif_mi":true`) || !strings.Contains(g, `"toplam_bayt":5000`) {
		t.Fatalf("işçi canlı: %d %s", kod, g)
	}
	for _, yasak := range []string{ozel, "AGE-SECRET", "R-TEST", BackupRoot, "/var/", "gos1:"} {
		if yasak != "" && strings.Contains(g, yasak) {
			t.Fatalf("ilerleme yanıtı yasak içerik taşıyor: %q", yasak)
		}
	}
}
