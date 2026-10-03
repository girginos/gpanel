package tasima

// d3a_posta_test.go — kaynak sürücüsünün verdiği kutu maildir yolu da ad kuralından
// sonra YENİDEN doğrulanır (savunma derinliği): sürücü hatalı/düşman bir yol üretse
// bile rsync'e gitmez. Gerçek MariaDB (ta_db) + sahte mail eklentisi; ATLANIR yoksa.

import (
	"context"
	"girginospanel/internal/efektiflimit"
	"strings"
	"testing"
)

func TestMailAktar_SurucuUzakYoluDogrulanir(t *testing.T) {
	db := ta_db(t)
	_ = mailTestKok(t)
	ek := sahteMailEklenti(t)
	for _, q := range []string{
		`ALTER TABLE domains ADD COLUMN plan_id BIGINT UNSIGNED NULL`,
		`CREATE TABLE service_plans (id BIGINT UNSIGNED PRIMARY KEY, max_email INT NOT NULL DEFAULT 0,
			saatlik_mail_limiti INT NOT NULL DEFAULT 0, mail_kutu_kota_mb INT NOT NULL DEFAULT 0)`,
		efektiflimit.TabloDDL, // 0120
		`CREATE TABLE cp_eklentiler (ad VARCHAR(64) PRIMARY KEY, aktif TINYINT NOT NULL DEFAULT 0,
			soket VARCHAR(255) NOT NULL DEFAULT '')`,
		`INSERT INTO domains (id, alan_adi, reseller_id) VALUES (31,'surucu.test',0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	if _, err := db.Exec(`INSERT INTO cp_eklentiler VALUES ('mail',1,?)`, ek.soket); err != nil {
		t.Fatal(err)
	}
	// Geçici sürücü: biri düşman (kabuk metakarakterli), biri geçerli yol üretir.
	kaynakKayit["sahte-posta"] = &kaynakSurucu{Kod: "sahte-posta", Olgunluk: olgunlukDeneysel, kesif: cpanelKesfet,
		postaKesfet: func(context.Context, *Kaynak, Hesap) ([]mailHesap, error) {
			return []mailHesap{
				{Yerel: "kotu", uzakYol: func(y string) string { return "/home/a;rm -rf /x/" + y + "/" }},
				{Yerel: "iyi", uzakYol: func(y string) string { return "/home/u/mail/surucu.test/" + y + "/" }},
			}, nil
		}}
	t.Cleanup(func() { delete(kaynakKayit, "sahte-posta") })

	pk := &postaKabuk{kaydedici: &kaydedici{}}
	k := &Kaynak{Tip: "sahte-posta", Host: "192.0.2.10", Port: 22, Kullanici: "root", kabuk: pk}
	n, uyar, err := (&Handlers{DB: db}).mailAktar(context.Background(), k, Hesap{AlanAdi: "surucu.test"}, func(string, ...any) {})
	if err != nil {
		t.Fatalf("mailAktar: %v", err)
	}
	if n != 1 {
		t.Errorf("yalnız geçerli yollu kutu taşınmalı: n=%d uyarılar=%q", n, uyar)
	}
	for _, r := range pk.rsyncler {
		if strings.Contains(r[0], ";") {
			t.Errorf("geçersiz uzak yol rsync'e gitti: %q", r[0])
		}
	}
	if !strings.Contains(strings.Join(uyar, "|"), "kaynak yolu geçersiz") {
		t.Errorf("geçersiz yol uyarı olarak raporlanmalı: %q", uyar)
	}
}
