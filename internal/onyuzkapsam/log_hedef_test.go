package onyuzkapsam

// log_hedef_test.go — Günlükler sayfası: dinamik alan adı seçici kaynak kilitleri
// (seçici render, derin bağlantı, hedef parametresi gönderimi, "Henüz log yok" pasif
// seçenek, ek alan sayfası kısayolu).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func logKaynak(t *testing.T, yol string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("../../frontend/src", yol))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLogSayfasi_AlanAdiSecici(t *testing.T) {
	s := logKaynak(t, "pages/DomainLogsPage.tsx")
	for _, parca := range []struct{ ad, metin string }{
		{"hedef uç çağrısı", "/log-hedefleri"},
		{"Select render", "<Select"},
		{"seçici etiketi", `cevir("Alan adı")`},
		{"derin bağlantı okuma", "arama_.get('hedef')"},
		{"derin bağlantı yazma", "setAramaPrm("},
		{"alt alan rotası hedefe eşlenir", "`alt:${sid}`"},
		{"oku hedef parametresi", "...hedefPrm"},
		{"canlı hedef parametresi", "&hedef=${encodeURIComponent(hedef)}"},
		{"pasif seçenek", "disabled: !h.access_var && !h.error_var"},
		{"Henüz log yok", `cevir("Henüz log yok")`},
		{"tür etiketleri", "'ek alan adı'"},
		{"hedef değişince temizlik", "setSatirlar([])"},
		{"geçersiz hedef ana'ya döner", "!hedefler.some(h => h.hedef === hedef)"},
		{"indirme", "indir"},
	} {
		if !strings.Contains(s, parca.metin) {
			t.Errorf("%s: kaynakta yok: %q", parca.ad, parca.metin)
		}
	}
	if strings.Contains(s, "/var/log/nginx") {
		t.Error("sunucu dosya yolu arayüzde gösterilmemeli")
	}
	if strings.Contains(s, "yol: string") && strings.Contains(s, "LogDosya = {") && strings.Contains(strings.SplitN(s, "type ReadResp", 2)[0], "yol: string") {
		t.Error("LogDosya tipinde yol alanı olmamalı")
	}
}

func TestEkAlanSayfasi_LogKisayolu(t *testing.T) {
	s := logKaynak(t, "pages/EkAlanYonetPage.tsx")
	if !strings.Contains(s, "/gunlukler?hedef=ek:${eid}") {
		t.Error("ek alan yönetim sayfasında hedefi önseçili Günlükler kısayolu yok")
	}
}
