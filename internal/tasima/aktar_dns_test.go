package tasima

// aktar_dns_test.go — H4: kaynak zone'u güvenilmez girdidir. dns.WriteZone
// tip/ad/değeri zone dosyasına satır olarak yazar; $INCLUDE, satır sonu, boşluk,
// denetim karakteri ve geçersiz etiket DB'ye ULAŞMAMALI, geçersiz bir kayıt aynı
// addaki mevcut kaydı SİLMEMELİ.
//
// Uçtan uca test geçici MariaDB ister (GOSP_TEST_GECICI_DSN yoksa ATLANIR; şema
// yardımcısı adalani_test.go). Zone yazımı ve varsayılan tohumlama sahtedir:
// /var/named, named-checkzone, rndc ve opendkim'e dokunulmaz; uzak komut
// kaydedici sahte kabuktan döner (ağa çıkılmaz).

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

func TestZoneKaydiGecerli(t *testing.T) {
	const alan = "ornek-alan.com.tr" // 17 bayt
	// 63.63.63.43 → 235 bayt ad + "." + alan = tam 253 (sınır içinde).
	sinir := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." +
		strings.Repeat("c", 63) + "." + strings.Repeat("d", 43)
	iyi := []struct{ tip, ad, deger string }{
		{"A", "@", "192.0.2.10"},
		{"A", "www", "192.0.2.10"},
		{"A", "*", "192.0.2.10"},
		{"A", "*.alt", "192.0.2.10"},
		{"TXT", "_dmarc", "v=DMARC1; p=none"},
		{"TXT", "default._domainkey", "v=DKIM1; k=rsa; p=MIGfMA0"},
		{"SRV", "_sip._tcp", "5 5060 sip.ornek.test"},
		{"CNAME", "Mail-2", "ghs.ornek.test"},
		{"CAA", "@", `0 issue "letsencrypt.org"`},
		{"MX", "@", "mx.ornek.test"},
		{"A", strings.Repeat("a", 63), "192.0.2.10"},
		{"A", sinir, "192.0.2.10"},
	}
	for _, c := range iyi {
		if !zoneKaydiGecerli(alan, c.tip, c.ad, c.deger) {
			t.Errorf("geçerli kayıt reddedildi: %s %q %q", c.tip, c.ad, c.deger)
		}
	}

	kotu := []struct{ neden, tip, ad, deger string }{
		{"$INCLUDE ad", "A", "$INCLUDE", "192.0.2.10"},
		{"$ ile başlayan değer", "CNAME", "www", "$INCLUDE /etc/shadow"},
		{"değerde satır sonu", "TXT", "x", "a\n$INCLUDE /etc/shadow"},
		{"değerde CR", "TXT", "x", "a\r$INCLUDE /etc/shadow"},
		{"değerde NUL", "TXT", "x", "abc\x00def"},
		{"değerde ESC", "TXT", "x", "abc\x1b[2Jdef"},
		{"apekste denetim karakterli değer", "TXT", "@", "abc\x00"},
		{"taşınamaz tip", "HINFO", "x", "a b"},
		{"tip enjeksiyonu", "A\n$INCLUDE /etc/shadow", "x", "192.0.2.10"},
		{"adda boşluk", "A", "a b", "192.0.2.10"},
		{"adda sekme", "A", "a\tb", "192.0.2.10"},
		{"adda satır sonu", "A", "www\n", "192.0.2.10"},
		{"boş ad", "A", "", "192.0.2.10"},
		{"boş etiket", "A", "a..b", "192.0.2.10"},
		{"sondaki nokta (zone dışı mutlak ad)", "A", "kurban.test.", "192.0.2.10"},
		{"başta tire", "A", "-a", "192.0.2.10"},
		{"sonda tire", "A", "a-", "192.0.2.10"},
		{"eğik çizgi", "A", "a/b", "192.0.2.10"},
		{"64 baytlık etiket", "A", strings.Repeat("a", 64), "192.0.2.10"},
		{"joker ilk etiket değil", "A", "a.*", "192.0.2.10"},
		{"joker parça", "A", "*a", "192.0.2.10"},
		{"FQDN 253 baytı aşar", "A", sinir + "d", "192.0.2.10"},
	}
	for _, c := range kotu {
		if zoneKaydiGecerli(alan, c.tip, c.ad, c.deger) {
			t.Errorf("%s: kabul edildi (%s %q %q)", c.neden, c.tip, c.ad, c.deger)
		}
	}
}

// 🔴 Kapı dnsAktar'ın TEK yazma yolunda: Plesk ayrıştırıcısının geçirdiği
// '$INCLUDE' adı, '$' değerli CNAME, boş etiket ve ESC'li TXT DB'ye yazılmaz;
// '$' değerli 'www CNAME' panelin 'www A' kaydını SİLMEZ; günlükte kaynak metni yok.
func TestDnsAktar_GecersizKayitlarDByeUlasmaz(t *testing.T) {
	db := ta_db(t) // domains: (20, 'bayi7.test'), ipv4 varsayılanı 203.0.113.10
	if _, err := db.Exec(`CREATE TABLE dns_records (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
		domain_id BIGINT UNSIGNED NOT NULL, ad VARCHAR(253) NOT NULL, tip VARCHAR(16) NOT NULL,
		deger TEXT NOT NULL, ttl INT NOT NULL DEFAULT 3600, oncelik INT NOT NULL DEFAULT 0,
		aktif TINYINT(1) NOT NULL DEFAULT 1) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`); err != nil {
		t.Fatalf("şema: %v", err)
	}
	eskiT, eskiZ := dnsTohumla, dnsZoneYaz
	t.Cleanup(func() { dnsTohumla, dnsZoneYaz = eskiT, eskiZ })
	dnsTohumla = func(ctx context.Context, db *sql.DB, domainID int64, _, ipv4 string) (int, error) {
		for _, ad := range []string{"@", "www"} {
			if _, err := db.ExecContext(ctx,
				`INSERT INTO dns_records (domain_id, ad, tip, deger) VALUES (?,?,'A',?)`, domainID, ad, ipv4); err != nil {
				return 0, err
			}
		}
		return 2, nil
	}
	zoneYazim := 0
	dnsZoneYaz = func(context.Context, *sql.DB, int64) error {
		zoneYazim++
		return nil
	}

	plesk := strings.Join([]string{
		"bayi7.test. A 192.0.2.10",
		"www.bayi7.test. CNAME $INCLUDE",
		"$INCLUDE. A 192.0.2.11",
		"kotu..bayi7.test. A 192.0.2.12",
		"esc.bayi7.test. TXT abc\x1b[2Jdef",
		"_dmarc.bayi7.test. TXT v=DMARC1; p=none",
		"*.bayi7.test. A 192.0.2.10",
		"bayi7.test. MX 10 mx.baska.test.",
	}, "\n")
	kay := &kaydedici{yanit: func(komut string) string {
		if strings.HasPrefix(komut, "plesk bin dns --info ") {
			return plesk
		}
		return ""
	}}
	var gunluk []string
	n, err := (&Handlers{DB: db}).dnsAktar(context.Background(),
		&Kaynak{Tip: "plesk", Host: "192.0.2.10", kabuk: kay}, 20, "bayi7.test",
		func(f string, a ...any) { gunluk = append(gunluk, fmt.Sprintf(f, a...)) })
	if err != nil {
		t.Fatalf("dnsAktar: %v", err)
	}
	if n != 3 || zoneYazim != 1 {
		t.Errorf("eklenen=%d (3 beklenir), zone yazımı=%d (1 beklenir)", n, zoneYazim)
	}

	rows, err := db.Query(`SELECT ad, tip, deger, oncelik FROM dns_records WHERE domain_id=20`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	bulunan := map[string]bool{}
	for rows.Next() {
		var ad, tip, deger string
		var oncelik int
		if err := rows.Scan(&ad, &tip, &deger, &oncelik); err != nil {
			t.Fatal(err)
		}
		bulunan[fmt.Sprintf("%s|%s|%s|%d", ad, tip, deger, oncelik)] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	beklenen := []string{
		"@|A|203.0.113.10|0",
		"www|A|203.0.113.10|0",
		"_dmarc|TXT|v=DMARC1; p=none|0",
		"*|A|203.0.113.10|0",
		"@|MX|mx.baska.test|10",
	}
	for _, b := range beklenen {
		if !bulunan[b] {
			t.Errorf("beklenen kayıt yok: %q", b)
		}
	}
	if len(bulunan) != len(beklenen) {
		t.Errorf("DB kayıtları = %v, beklenen yalnız %v", bulunan, beklenen)
	}

	uyari := false
	for _, s := range gunluk {
		if strings.Contains(s, "$INCLUDE") || strings.ContainsRune(s, '\x1b') {
			t.Errorf("günlüğe kaynak metni basıldı: %q", s)
		}
		if strings.Contains(s, "4 kayit gecersiz") {
			uyari = true
		}
	}
	if !uyari {
		t.Errorf("reddedilen sayısı günlükte yok: %q", gunluk)
	}
}
