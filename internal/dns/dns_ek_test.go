package dns

// dns_ek_test.go — mutasyon koşusunda kaçan dallar: commit hatasında include
// uzlaştırması, içe aktarılan SOA'nın sayısal sınırı, yalnız öncelik değişiminin
// içerik doğrulamasını tetiklemesi.

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// Commit başarısız → zone dosyası geri döner ve include commit edilmiş duruma uzlaşır
// (işlemin görünümünde ya da bayat anlıkta kalmaz).
func TestZoneliIslem_CommitHatasi_IncludeUzlasir(t *testing.T) {
	_, inc, _ := zd_kur(t)
	db := zi_db(t)
	zd_yaz(t, inc, zdEskiInclude)
	eski := zoneUygula
	t.Cleanup(func() { zoneUygula = eski })
	zoneUygula = func(ctx context.Context, q sorgucu, id int64) (func(), bool, error) {
		geri, tazele, err := zoneUygulaGercek(ctx, q, id)
		_ = q.(*sql.Tx).Rollback() // Commit başarısız olsun
		return geri, tazele, err
	}
	h := &Handlers{DB: db}
	if err := h.zoneliIslem(context.Background(), 42, zinc_ekle); err == nil || !strings.Contains(err.Error(), "kaydedilemedi") {
		t.Fatalf("commit hatası beklenirdi: %v", err)
	}
	i, _ := zd_oku(t, inc)
	if i != zinc_beklenen(t, db) || strings.Contains(i, `zone "bayialan.test"`) {
		t.Errorf("commit hatasında include commit edilmiş durumla uzlaşmadı: %q", i)
	}
}

// İçe aktarılan dosyanın SOA sayıları da sınırdan geçer (eskiden DB'de 500).
func TestIceAktar_SOASayisiTasarsa400(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	zone := "$ORIGIN bayialan.test.\n" +
		"@ 3600 IN SOA ns1.bayialan.test. admin.bayialan.test. 1 4294967296 900 1209600 3600\n" +
		"@ 3600 IN NS ns1.bayialan.test.\nns1 3600 IN A 203.0.113.1\n"
	kod, govde := zh_cagir(t, db, zh_tokenlar(t)["admin"], http.MethodPost, "/domains/{id}/dns/ice-aktar",
		"/domains/42/dns/ice-aktar", "text/plain", zone, func(h *Handlers) http.HandlerFunc { return h.IceAktar })
	if kod != http.StatusBadRequest || !strings.Contains(govde, "SOA") {
		t.Errorf("taşan SOA sayısı 400 beklenirdi: %d %s", kod, govde)
	}
	if n := dk_sayi(t, db, `SELECT COUNT(*) FROM dns_records WHERE domain_id=42`); n != 0 {
		t.Errorf("reddedilen dosyadan kayıt eklendi: %d", n)
	}
}

// Yalnız öncelik değişimi de içerik değişimidir: aralık dışı öncelik 400.
func TestUpdate_YalnizOncelikDegisimiDogrulanir(t *testing.T) {
	db := zi_db(t)
	zh_dikis(t, nil)
	rid := zi_ekle(t, db, 42, "@", "MX", "mail.bayialan.test", 600, 10, 1)
	if kod, govde := zi_put(t, db, fmt.Sprint(rid), `{"oncelik":70000}`); kod != http.StatusBadRequest {
		t.Errorf("aralık dışı öncelik 400 beklenirdi: %d %s", kod, govde)
	}
	if n := dk_sayi(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM dns_records WHERE id=%d AND oncelik=10`, rid)); n != 1 {
		t.Error("reddedilen öncelik yazıldı")
	}
}
