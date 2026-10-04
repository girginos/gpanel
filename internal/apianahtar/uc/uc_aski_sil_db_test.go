package uc

// uc_aski_sil_db_test.go — (1) bayi askısı: anahtar İPTAL EDİLMEZ, askıda kalır;
// askıdan alınınca kendiliğinden yeniden çalışır (kullanıcı kararı 2026-10-03).
// (2) kalıcı silme: DELETE /api-anahtarlari/{id} (etkin anahtarda onay_ad), sahiplik
// (IDOR), denetim kaydı, anahtar bu uçlara erişemez.
//
// 🔴 Askı testleri hostingi OLMAYAN bayi_d (10) ile yapılır: hesapaski bayi zinciri
// hiçbir domaine dokunmaz (sistem adımı çalışmaz).

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/apianahtar"
)

// kayitDurumu: anahtarın görünen durumu + sahibi askıda mı (depodan; askıdaki bayinin
// JWT'si düşer, kök de bayi anahtarlarını arayüzden listeleyemez).
func kayitDurumu(t *testing.T, id int64) (string, bool) {
	t.Helper()
	k, err := apianahtar.IDGetir(t.Context(), ucDB, id)
	if err != nil || k == nil {
		t.Fatalf("kayıt okunamadı: %v", err)
	}
	return k.GorunenDurum(time.Now().Unix()), k.SahipAskida()
}

func TestAPIAnahtarBayiAskiAskidaKalir(t *testing.T) {
	db := ucGeciciDB(t)
	h := ucRouter(db)
	jD, jK := jwtBayi(t, 10, "bayi_d"), jwtKok(t)
	d := olustur(t, h, jD, map[string]any{"ad": "WHMCS-D", "izinler": []string{"hesap:oku"}, "parola": "dogru-parola-b"})
	k := olustur(t, h, jK, map[string]any{"ad": "Kok", "izinler": []string{"hesap:oku"}, "izinli_ipler": []string{"192.0.2.0/24"}, "parola": "kok-parola"})
	if s := ucIstek(t, h, "GET", "/api/v1/domains", d.Anahtar, nil); s.kod != 200 {
		t.Fatalf("askı öncesi bayi anahtarı: %d %s", s.kod, s.govde)
	}

	// ── Askıya al → 403 bayi_askida; kayıt DEĞİŞMEZ; toplu iptal denetimi YOK ──
	if s := ucIstek(t, h, "PUT", "/api/v1/resellers/10", jK, map[string]any{"durum": "suspended"}); s.kod != 200 {
		t.Fatalf("askıya alma: %d %s", s.kod, s.govde)
	}
	s := ucIstek(t, h, "GET", "/api/v1/domains", d.Anahtar, nil)
	if s.kod != 403 || !strings.Contains(s.govde, `"kod":"bayi_askida"`) || strings.Contains(s.govde, "hesap_askida") {
		t.Fatalf("askıdaki bayinin anahtarı 403 bayi_askida olmalı: %d %s", s.kod, s.govde)
	}
	var iptalAt, neden string
	if err := db.QueryRow(`SELECT COALESCE(CAST(iptal_at AS CHAR),''), iptal_neden FROM api_anahtarlari WHERE id=?`, d.Kayit.ID).Scan(&iptalAt, &neden); err != nil {
		t.Fatal(err)
	}
	if iptalAt != "" || neden != "" {
		t.Fatalf("askı anahtarı İPTAL ETMEMELİ: iptal_at=%q neden=%q", iptalAt, neden)
	}
	var topluIptal, redKaydi int
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='api_anahtar.toplu_iptal'`).Scan(&topluIptal)
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='api_anahtar.red' AND reseller_id=10 AND detail LIKE '%bayi askıda%'`).Scan(&redKaydi)
	if topluIptal != 0 || redKaydi != 1 {
		t.Fatalf("denetim: toplu_iptal=%d (0 olmalı) red(bayi askıda)=%d (1 olmalı)", topluIptal, redKaydi)
	}
	// Seyreltme: tekrar eden ret denetimi gürültü yapmaz.
	_ = ucIstek(t, h, "GET", "/api/v1/domains", d.Anahtar, nil)
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='api_anahtar.red' AND reseller_id=10`).Scan(&redKaydi)
	if redKaydi != 1 {
		t.Fatalf("ret denetimi seyreltilmeli: %d", redKaydi)
	}
	// Kök listesi: "askida" (iptal değil).
	if durum, askida := kayitDurumu(t, d.Kayit.ID); durum != "askida" || !askida {
		t.Fatalf("kök bayi anahtarları listesinde askıda görünmeli: durum=%q sahip_askida=%v", durum, askida)
	}
	// Kök anahtarı etkilenmez.
	if s := ucIstek(t, h, "GET", "/api/v1/domains/44", k.Anahtar, nil); s.kod != 200 {
		t.Fatalf("kök anahtarı bayi askısından etkilenmemeli: %d %s", s.kod, s.govde)
	}

	// ── Askıdan al → anahtar kendiliğinden 200 ──
	if s := ucIstek(t, h, "PUT", "/api/v1/resellers/10", jK, map[string]any{"durum": "active"}); s.kod != 200 {
		t.Fatalf("askıdan alma: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "GET", "/api/v1/domains", d.Anahtar, nil); s.kod != 200 || !strings.Contains(s.govde, `"rid":10`) {
		t.Fatalf("askıdan alınan bayinin anahtarı yeniden çalışmalı: %d %s", s.kod, s.govde)
	}
	// (bayinin JWT'si askıda düşürüldü — listeyi kök okur)
	if durum, askida := kayitDurumu(t, d.Kayit.ID); durum != "etkin" || askida {
		t.Fatalf("askıdan sonra etkin görünmeli: %q %v", durum, askida)
	}

	// ── Parola sıfırlama → KALICI iptal (değişmedi); askıdan alma geri getirmez ──
	if s := ucIstek(t, h, "PUT", "/api/v1/resellers/10", jK, map[string]any{"parola": "yeni-parola-123"}); s.kod != 200 {
		t.Fatalf("parola sıfırlama: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "GET", "/api/v1/domains", d.Anahtar, nil); s.kod != 401 {
		t.Fatalf("parolası sıfırlanan bayinin anahtarı 401 olmalı: %d %s", s.kod, s.govde)
	}
	_ = db.QueryRow(`SELECT iptal_neden FROM api_anahtarlari WHERE id=?`, d.Kayit.ID).Scan(&neden)
	if neden != "parola_sifirlama" {
		t.Fatalf("iptal nedeni: %q", neden)
	}
	_ = ucIstek(t, h, "PUT", "/api/v1/resellers/10", jK, map[string]any{"durum": "suspended"})
	_ = ucIstek(t, h, "PUT", "/api/v1/resellers/10", jK, map[string]any{"durum": "active"})
	if s := ucIstek(t, h, "GET", "/api/v1/domains", d.Anahtar, nil); s.kod != 401 {
		t.Fatalf("iptal edilmiş anahtar askı döngüsüyle geri gelmemeli: %d", s.kod)
	}
	if durum, _ := kayitDurumu(t, d.Kayit.ID); durum != "iptal" {
		t.Fatalf("iptal edilmiş anahtar listede iptal görünmeli: %q", durum)
	}
}

func TestAPIAnahtarKaliciSil(t *testing.T) {
	db := ucGeciciDB(t)
	h := ucRouter(db)
	jA, jB, jK := jwtBayi(t, 7, "bayi_a"), jwtBayi(t, 8, "bayi_b"), jwtKok(t)
	a := olustur(t, h, jA, map[string]any{"ad": "WHMCS-A", "izinler": []string{"hesap:yaz"}, "parola": "dogru-parola-a"})
	a2 := olustur(t, h, jA, map[string]any{"ad": "Yedek-A", "izinler": []string{"hesap:oku"}, "parola": "dogru-parola-a"})
	b := olustur(t, h, jB, map[string]any{"ad": "WHMCS-B", "izinler": []string{"hesap:oku"}, "parola": "dogru-parola-b"})
	k := olustur(t, h, jK, map[string]any{"ad": "Kok", "izinler": []string{"hesap:oku", "bayi:yaz"}, "izinli_ipler": []string{"192.0.2.0/24"}, "parola": "kok-parola"})
	yol := func(id int64) string { return fmt.Sprintf("/api/v1/api-anahtarlari/%d", id) }
	satirVar := func(id int64) bool {
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM api_anahtarlari WHERE id=?`, id).Scan(&n)
		return n == 1
	}

	// ── Anahtarın kendisi iptal/silme uçlarına erişemez ──
	if s := ucIstek(t, h, "DELETE", yol(a.Kayit.ID), a.Anahtar, map[string]any{"onay_ad": "WHMCS-A"}); s.kod != 403 || !satirVar(a.Kayit.ID) {
		t.Fatalf("anahtar kendini silmemeli: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "POST", yol(a.Kayit.ID)+"/iptal", k.Anahtar, nil); s.kod != 403 {
		t.Fatalf("kök anahtarı bile iptal ucuna erişemez: %d %s", s.kod, s.govde)
	}

	// ── IDOR: bayi B, A'nın anahtarını silemez/iptal edemez (404, satır kalır) ──
	if s := ucIstek(t, h, "DELETE", yol(a.Kayit.ID), jB, map[string]any{"onay_ad": "WHMCS-A"}); s.kod != 404 || !satirVar(a.Kayit.ID) {
		t.Fatalf("IDOR: bayi B, A'nın anahtarını sildi: %d %s", s.kod, s.govde)
	}
	// Onaysız yabancı istek de 404 (409 anahtar_etkin DEĞİL — varlık/durum sızmaz).
	if s := ucIstek(t, h, "DELETE", yol(a.Kayit.ID), jB, nil); s.kod != 404 || strings.Contains(s.govde, "anahtar_etkin") {
		t.Fatalf("IDOR: yabancı anahtarın varlığı/durumu sızdı: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "POST", yol(a.Kayit.ID)+"/iptal", jB, nil); s.kod != 404 {
		t.Fatalf("IDOR: bayi B, A'nın anahtarını iptal: %d %s", s.kod, s.govde)
	}
	// Depo katmanı ikinci kilit: sahip koşullu silme yabancı satıra DOKUNMAZ;
	// yalnizGecersiz etkin satırı silmez (karar ile silme arası yarış kilidi).
	if silindi, err := apianahtar.Sil(t.Context(), db, a.Kayit.ID, 8, false, time.Now().Unix()); err != nil || silindi || !satirVar(a.Kayit.ID) {
		t.Fatalf("depo: sahip koşullu silme yabancı anahtarı sildi: %v %v", silindi, err)
	}
	if silindi, err := apianahtar.Sil(t.Context(), db, a.Kayit.ID, 7, true, time.Now().Unix()); err != nil || silindi || !satirVar(a.Kayit.ID) {
		t.Fatalf("depo: yalnizGecersiz etkin anahtarı sildi: %v %v", silindi, err)
	}
	// Bayi kök anahtarını da silemez.
	if s := ucIstek(t, h, "DELETE", yol(k.Kayit.ID), jA, map[string]any{"onay_ad": "Kok"}); s.kod != 404 || !satirVar(k.Kayit.ID) {
		t.Fatalf("IDOR: bayi kök anahtarını sildi: %d %s", s.kod, s.govde)
	}
	// Etkin anahtar onaysız: 409; yanlış ad: 400; satır kalır, anahtar çalışır.
	if s := ucIstek(t, h, "DELETE", yol(a.Kayit.ID), jA, nil); s.kod != 409 || !strings.Contains(s.govde, "anahtar_etkin") {
		t.Fatalf("etkin anahtar onaysız silinmemeli: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "DELETE", yol(a.Kayit.ID), jA, map[string]any{"onay_ad": "yanlis"}); s.kod != 400 || !strings.Contains(s.govde, "onay_ad_uyusmuyor") {
		t.Fatalf("yanlış onay adı: %d %s", s.kod, s.govde)
	}
	if !satirVar(a.Kayit.ID) || ucIstek(t, h, "GET", "/api/v1/domains/42", a.Anahtar, nil).kod != 200 {
		t.Fatal("reddedilen silme anahtarı etkilememeli")
	}

	// ── İptal (POST /iptal) → sonra onaysız kalıcı silme ──
	if s := ucIstek(t, h, "POST", yol(a2.Kayit.ID)+"/iptal", jA, nil); s.kod != 200 {
		t.Fatalf("iptal: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "DELETE", yol(a2.Kayit.ID), jA, nil); s.kod != 200 || satirVar(a2.Kayit.ID) {
		t.Fatalf("iptal edilmiş anahtar onaysız silinmeli: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "DELETE", yol(a2.Kayit.ID), jA, nil); s.kod != 404 {
		t.Fatalf("silinmiş anahtar 404: %d", s.kod)
	}

	// ── Tek adımda "iptal et ve sil" (adı yazarak) → satır gider, anahtar 401 ──
	if s := ucIstek(t, h, "DELETE", yol(a.Kayit.ID), jA, map[string]any{"onay_ad": "WHMCS-A"}); s.kod != 200 || satirVar(a.Kayit.ID) {
		t.Fatalf("iptal et ve sil: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "GET", "/api/v1/domains/42", a.Anahtar, nil); s.kod != 401 {
		t.Fatalf("silinen anahtar 401 olmalı: %d", s.kod)
	}

	// ── Kök bayi anahtarını SİLEMEZ (404, satır kalır); sahibi kendisi siler ──
	if s := ucIstek(t, h, "DELETE", yol(b.Kayit.ID), jK, map[string]any{"onay_ad": "WHMCS-B"}); s.kod != 404 || !satirVar(b.Kayit.ID) {
		t.Fatalf("kök bayi anahtarını silememeli: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "DELETE", yol(b.Kayit.ID), jK, nil); s.kod != 404 || strings.Contains(s.govde, "anahtar_etkin") {
		t.Fatalf("kök için yabancı anahtar durumu sızmamalı: %d %s", s.kod, s.govde)
	}
	if s := ucIstek(t, h, "DELETE", yol(b.Kayit.ID), jB, map[string]any{"onay_ad": "WHMCS-B"}); s.kod != 200 || satirVar(b.Kayit.ID) {
		t.Fatalf("bayi kendi anahtarını silmeli: %d %s", s.kod, s.govde)
	}

	// ── Denetim: her silme api_anahtar.sil; oluşturma geçmişi SİLİNMEZ ──
	var sil, olusturma int
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='api_anahtar.sil' AND ok=1`).Scan(&sil)
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='api_anahtar.olustur'`).Scan(&olusturma)
	if sil != 3 || olusturma != 4 {
		t.Fatalf("denetim: sil=%d (3) olustur=%d (4)", sil, olusturma)
	}
	var etkinSil int
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='api_anahtar.sil' AND reseller_id=7 AND detail LIKE '%iptal edilip silindi%'`).Scan(&etkinSil)
	if etkinSil != 1 {
		t.Fatalf("etkin anahtarın silinmesi denetimde belirtilmeli: %d", etkinSil)
	}
	// Kök anahtarı kendi anahtarını silebilir.
	if s := ucIstek(t, h, "DELETE", yol(k.Kayit.ID), jK, map[string]any{"onay_ad": "Kok"}); s.kod != 200 || satirVar(k.Kayit.ID) {
		t.Fatalf("kök kendi anahtarı: %d %s", s.kod, s.govde)
	}
}
