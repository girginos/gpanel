package dns

// ek_zone_test.go — ek (addon) alan adı bölgesi: şablondan posta kayıtları ELENİR
// (v1: e-posta kapalı), NS/@ A/www garantilenir, kayıt enjeksiyonu süzülür; tohum
// idempotent; zone include'u ek bölgeleri de içerir ve silinince düşürür.

import (
	"context"
	"strings"
	"testing"
)

func ekSatirMap(k []EkKayitSatiri) map[string]bool {
	m := map[string]bool{}
	for _, x := range k {
		m[x.Ad+"|"+x.Tip+"|"+x.Deger] = true
	}
	return m
}

func TestEkSablonKayitlari_PostaYok_NSVarWWWVar(t *testing.T) {
	k := EkSablonKayitlari(builtinDefaults(), "ornek.org", "198.51.100.9")
	m := ekSatirMap(k)
	for _, bek := range []string{
		"@|A|198.51.100.9", "www|A|198.51.100.9", "ns1|A|198.51.100.9", "ns2|A|198.51.100.9",
		"@|NS|ns1.ornek.org", "@|NS|ns2.ornek.org",
	} {
		if !m[bek] {
			t.Errorf("beklenen kayıt yok: %s (%v)", bek, m)
		}
	}
	for _, x := range k {
		switch x.Tip {
		case "MX", "TXT", "SRV":
			t.Errorf("ek alan adı bölgesinde posta/kimlik kaydı olmamalı: %+v", x)
		}
		switch x.Ad {
		case "mail", "webmail", "smtp", "imap", "pop", "autoconfig", "autodiscover", "_dmarc":
			t.Errorf("posta ana makinesi ek bölgeye alınmamalı: %+v", x)
		}
		if strings.Contains(x.Deger, "{") {
			t.Errorf("çözülmemiş yer tutucu: %+v", x)
		}
	}
}

func TestEkSablonKayitlari_BozukSablonaVarsayilan(t *testing.T) {
	// Şablonda yalnız posta kayıtları: NS ve @ A varsayılanı eklenir (named-checkzone NS ister).
	k := EkSablonKayitlari([]TemplateRow{{Ad: "@", Tip: "MX", Deger: "mail.{DOMAIN}", Aktif: true}}, "ornek.org", "")
	m := ekSatirMap(k)
	for _, bek := range []string{"@|A|127.0.0.1", "@|NS|ns1.ornek.org", "@|NS|ns2.ornek.org", "ns1|A|127.0.0.1"} {
		if !m[bek] {
			t.Errorf("varsayılan eksik: %s (%v)", bek, m)
		}
	}
	// Pasif satır alınmaz.
	k = EkSablonKayitlari([]TemplateRow{{Ad: "@", Tip: "A", Deger: "{IP}", Aktif: false}}, "ornek.org", "198.51.100.1")
	if !ekSatirMap(k)["@|A|198.51.100.1"] {
		t.Error("pasif şablon satırı yerine varsayılan @ A beklenir")
	}
}

func TestEkSablonKayitlari_EnjeksiyonSuzulur(t *testing.T) {
	k := EkSablonKayitlari([]TemplateRow{
		{Ad: "@", Tip: "A", Deger: "198.51.100.1\n@ IN A 6.6.6.6", Aktif: true},
		{Ad: "www evil", Tip: "A", Deger: "{IP}", Aktif: true},
		{Ad: "$ORIGIN x", Tip: "NS", Deger: "{IP}", Aktif: true},
		{Ad: "@", Tip: "NS", Deger: "$INCLUDE /etc/passwd", Aktif: true},
	}, "ornek.org", "198.51.100.5")
	for _, x := range k {
		if strings.ContainsAny(x.Deger, "\r\n") || strings.ContainsAny(x.Ad, " \t\r\n") ||
			strings.HasPrefix(x.Ad, "$") || strings.HasPrefix(x.Deger, "$") {
			t.Errorf("enjeksiyon süzülmedi: %+v", x)
		}
	}
}

func TestEkSeed_Idempotent_VeIncludeler(t *testing.T) {
	db := dk_db(t)
	dk_goc(t, db, "0007_dns.sql")
	dk_exec(t, db, `ALTER TABLE domains ADD COLUMN dnssec_aktif TINYINT NOT NULL DEFAULT 0`)
	dk_goc(t, db, "0160_ek_alan_adlari.sql")
	dk_exec(t, db, `INSERT INTO ek_alan_adlari (id, domain_id, alan_adi) VALUES (1, 10, 'ek-bir.org'), (2, 10, 'ek-iki.org')`)
	ctx := context.Background()
	n, err := EkSeed(ctx, db, 1, "ek-bir.org", "198.51.100.5", 0)
	if err != nil || n == 0 {
		t.Fatalf("tohum: n=%d err=%v", n, err)
	}
	if n2, err := EkSeed(ctx, db, 1, "ek-bir.org", "198.51.100.5", 0); err != nil || n2 != 0 {
		t.Fatalf("ikinci tohum eklememeli: n=%d err=%v", n2, err)
	}
	if dk_sayi(t, db, `SELECT COUNT(*) FROM ek_dns_kayitlari WHERE ek_alan_id=2`) != 0 {
		t.Fatal("başka ek alanın kaydı oluştu")
	}
	// Ana bölge (dns_records) ek tohumundan etkilenmez.
	if dk_sayi(t, db, `SELECT COUNT(*) FROM dns_records`) != 0 {
		t.Fatal("ek tohum ana bölgeye yazdı")
	}
	kayitlar, err := EkKayitlari(ctx, db, 1)
	if err != nil || len(kayitlar) == 0 {
		t.Fatalf("EkKayitlari: %v %v", kayitlar, err)
	}
	// include: yalnız KAYDI olan ek bölgeler (ek-iki kayıtsız).
	s, err := buildZoneIncludes(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, `zone "ek-bir.org" { type master; file "`+ZoneDir+`/ek-bir.org.zone"; allow-query { any; }; allow-transfer { none; }; };`) {
		t.Fatalf("ek bölge include'ta yok:\n%s", s)
	}
	if strings.Contains(s, "ek-iki.org") {
		t.Fatalf("kayıtsız ek bölge include'a girmemeli:\n%s", s)
	}
	// Ek alan silinince (CASCADE) include'tan düşer.
	dk_exec(t, db, `DELETE FROM ek_alan_adlari WHERE id=1`)
	s, _ = buildZoneIncludes(ctx, db)
	if strings.Contains(s, "ek-bir.org") {
		t.Fatalf("silinen ek bölge include'ta kaldı:\n%s", s)
	}
	// Hesap tablosu olmayan şemada (0160 koşmamış) include üretimi bozulmaz.
	dk_exec(t, db, `DROP TABLE ek_dns_kayitlari`)
	dk_exec(t, db, `DROP TABLE ek_alan_adlari`)
	if _, err := buildZoneIncludes(ctx, db); err != nil {
		t.Fatalf("0160 yokken include üretilebilmeli: %v", err)
	}
}

// Include dosyasına yazılan ek alan adı kaçış/enjeksiyon içeremez (DB elle bozulmuş).
func TestEkZoneInclude_GuvensizAdAtlanir(t *testing.T) {
	db := dk_db(t)
	dk_goc(t, db, "0007_dns.sql")
	dk_exec(t, db, `ALTER TABLE domains ADD COLUMN dnssec_aktif TINYINT NOT NULL DEFAULT 0`)
	dk_goc(t, db, "0160_ek_alan_adlari.sql")
	dk_exec(t, db, `INSERT INTO ek_alan_adlari (id, domain_id, alan_adi) VALUES (1, 10, 'x.org" { type master; file "/etc/passwd"; }; zone "y.org')`)
	dk_exec(t, db, `INSERT INTO ek_dns_kayitlari (ek_alan_id, ad, tip, deger) VALUES (1,'@','A','198.51.100.1')`)
	s, err := buildZoneIncludes(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s, "/etc/passwd") || strings.Count(s, "zone ") != 0 {
		t.Fatalf("güvensiz ad include'a girdi:\n%s", s)
	}
}
