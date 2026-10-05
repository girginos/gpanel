package uzak

import (
	"os"
	"strings"
	"testing"
)

// Koşucu yalnız başında "--" olan satırları atar ve ";" ile böler: satır içi yorum ya da
// yorumda ";" ifadeyi bozar. DB gerektirmez.
func TestGoc0140_KosucuUyumu(t *testing.T) {
	body, err := os.ReadFile(gocYolu140)
	if err != nil {
		t.Fatal(err)
	}
	for i, satir := range strings.Split(string(body), "\n") {
		s := strings.TrimSpace(satir)
		if strings.HasPrefix(s, "--") {
			if strings.Contains(s, ";") {
				t.Errorf("satır %d: yorumda ';' var", i+1)
			}
			continue
		}
		if strings.Contains(s, "--") || strings.Contains(s, "/*") || strings.Contains(s, "#") {
			t.Errorf("satır %d: satır içi yorum: %s", i+1, s)
		}
	}
}

func TestGoc0140_IdempotentVeKisitlar(t *testing.T) {
	db := gecicidb(t)            // 0113 + 0140 bir kez uygulandı
	gocUygula(t, db, gocYolu140) // ikinci koşu: hata yok
	gocUygula(t, db, gocYolu140)
	if n := say(t, db, `SELECT COUNT(*) FROM backup_uzak_kopya_ayar WHERE id=1 AND aktif=0 AND hedef_id IS NULL AND saklama_n=7`); n != 1 {
		t.Fatalf("varsayılan ayar satırı KAPALI olmalı: %d", n)
	}
	for _, k := range []string{"boyut_duz", "sha256_duz", "sifreleme", "manifest_ad", "son_hata", "yuklenen_b", "baslangic_at", "bitis_at", "yedek_dosya"} {
		if n := say(t, db, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='backup_uzak_kopyalar' AND column_name=?`, k); n != 1 {
			t.Errorf("kolon yok: %s", k)
		}
	}
	if _, err := db.Exec(`INSERT INTO backup_uzak_kopya_ayar (id) VALUES (2)`); err == nil {
		t.Error("ayar tek satır olmalı (id=1)")
	}
	if _, err := db.Exec(`UPDATE backup_uzak_kopya_ayar SET saklama_n=0`); err == nil {
		t.Error("saklama_n >= 1 olmalı")
	}
	_, _ = db.Exec(`INSERT INTO domains (id) VALUES (1)`)
	_, _ = db.Exec(`INSERT INTO backup_hedefler (ad, tur, ayar, sirlar) VALUES ('h','bellek','{}','')`)
	for _, d := range []string{"bekliyor", "sifreleniyor", "yukleniyor", "dogrulaniyor", "tamam", "hata", "iptal", "siliniyor", "silindi", "sahipsiz"} {
		if _, err := db.Exec(`INSERT INTO backup_uzak_kopyalar (domain_id, kaynak, hedef_id, durum, backup_id) VALUES (1,'hedef',1,?,NULL)`, d); err != nil {
			t.Errorf("durum %s kabul edilmeli: %v", d, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO backup_uzak_kopyalar (domain_id, kaynak, hedef_id, durum) VALUES (1,'hedef',1,'bilinmeyen')`); err == nil {
		t.Error("bilinmeyen durum reddedilmeli")
	}
	// hedef silinince ayar hedefi NULL olur (uzak kopya kapanır), kopya kayıtları CASCADE silinir
	if _, err := db.Exec(`UPDATE backup_uzak_kopya_ayar SET aktif=1, hedef_id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM backup_hedefler WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if n := say(t, db, `SELECT COUNT(*) FROM backup_uzak_kopya_ayar WHERE hedef_id IS NULL`); n != 1 {
		t.Error("hedef silinince ayar hedefi NULL olmalı")
	}
}
