package ajan

// unutma_db_test.go — KVKK: /unutbeni (BaglantiIptal neden=unutbeni), merkez ve kullanıcı iptallerinde
// Telegram kimliğinin sunucudaki izleri anonimleşir (geçici MariaDB; DSN yoksa atlanır).

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/destek/iptal"
	p "girginospanel/internal/destek/protokol"
)

const (
	kvTg    = int64(777000111)
	kvAd    = "ali_veli"
	kvBaska = "baska_kisi"
)

// kvKalintilar — DB'de bu kullanıcının kimliğini taşıyan her yer (bağlantı + denetim, tüm kolonlar).
func kvKalintilar(t *testing.T, o *ortam, dizgeler ...string) []string {
	t.Helper()
	var bulunan []string
	tara := func(sorgu, ad string) {
		rows, err := o.db.Query(sorgu)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		for rows.Next() {
			vals := make([]any, len(cols))
			ptr := make([]any, len(cols))
			for i := range vals {
				ptr[i] = &vals[i]
			}
			if err := rows.Scan(ptr...); err != nil {
				t.Fatal(err)
			}
			var sb strings.Builder
			for _, v := range vals {
				fmt.Fprintf(&sb, "%v|", v)
				if b, ok := v.([]byte); ok {
					fmt.Fprintf(&sb, "%s|", b)
				}
			}
			for _, d := range dizgeler {
				if strings.Contains(sb.String(), d) {
					bulunan = append(bulunan, ad+":"+d+" → "+sb.String())
				}
			}
		}
	}
	tara(`SELECT * FROM destek_baglantilari`, "destek_baglantilari")
	tara(`SELECT * FROM audit_log`, "audit_log")
	tara(`SELECT * FROM destek_islemleri`, "destek_islemleri")
	return bulunan
}

func kvDenetimEkle(t *testing.T, o *ortam, aktor, hedef string) {
	t.Helper()
	if _, err := o.db.Exec(`INSERT INTO audit_log (actor_username, action, target) VALUES (?,?,?)`, aktor, "destek.test", hedef); err != nil {
		t.Fatal(err)
	}
}

func kvBekleIptalAnon(t *testing.T, o *ortam, kimlik string) {
	t.Helper()
	son := time.Now().Add(5 * time.Second)
	for time.Now().Before(son) {
		var d string
		var tg int64
		var ad string
		_ = o.db.QueryRow(`SELECT durum, tg_user_id, tg_kullanici_adi FROM destek_baglantilari WHERE kimlik=?`, kimlik).Scan(&d, &tg, &ad)
		if d == durumIptal && tg == 0 && ad == "" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	var d string
	var tg int64
	var ad string
	_ = o.db.QueryRow(`SELECT durum, tg_user_id, tg_kullanici_adi FROM destek_baglantilari WHERE kimlik=?`, kimlik).Scan(&d, &tg, &ad)
	t.Fatalf("bağlantı anonimleşmedi: durum=%s tg=%d ad=%q", d, tg, ad)
}

func TestUnutBeni_TelegramKimligiAnonimlesir(t *testing.T) {
	o := kurOrtam(t, false, true)
	id := o.kokEslestir(kvTg, kvAd, "okuma")
	// eşleştirme denetim satırı bot aktörüyle yazılmış olmalı (ön koşul)
	if len(kvKalintilar(t, o, kvAd)) == 0 {
		t.Fatal("ön koşul: eşleştirme denetimi kullanıcı adını taşımıyor")
	}
	// araç yolunun yazdığı biçimler (aracTgKisa: @'siz ad ve son 6 hane) + alan adlı hedef + başka kullanıcı
	kvDenetimEkle(t, o, "destek-bot (tg:"+kvAd+")", "a.test")
	kvDenetimEkle(t, o, "destek-bot (tg:000111)", "a.test")
	kvDenetimEkle(t, o, "destek-bot (tg:…000111)", "tg:…000111")
	kvDenetimEkle(t, o, "destek-bot (tg:"+kvAd+") (adına musteri:42)", "b.test (tg:@"+kvAd+")")
	kvDenetimEkle(t, o, "destek-bot (tg:@"+kvBaska+")", "tg:@"+kvBaska)
	kvDenetimEkle(t, o, "destek-bot (tg:@"+kvAd+"2)", "tg:@"+kvAd+"2") // öneki aynı, FARKLI kullanıcı
	kvDenetimEkle(t, o, "root", "tg:@"+kvBaska)

	o.m.gonder(&p.BaglantiIptal{BaglantiID: id, Neden: p.IptalUnutBeni})
	kvBekleIptalAnon(t, o, id)

	if k := kvKalintilar(t, o, kvAd+")", "@"+kvAd+"'", "tg:@"+kvAd, "tg:"+kvAd+")", "777000111", "tg:…000111", "tg:000111"); len(k) > 0 {
		// "ali_veli2" (başka kullanıcı) bu desenlere UYMAMALI: "tg:@ali_veli" öneki eşleşir → yalnız o satırlar serbest
		for _, s := range k {
			if !strings.Contains(s, kvAd+"2") {
				t.Errorf("kimlik kalıntısı: %s", s)
			}
		}
	}
	var n int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE actor_username LIKE '%(tg:silindi)%' OR target LIKE '%tg:silindi%'`).Scan(&n)
	if n < 5 {
		t.Errorf("anonim işaretli denetim satırı %d (beklenen ≥5)", n)
	}
	// başka kullanıcıların izleri ve denetim olgusu (eylem, kapsam) DOKUNULMADI
	for _, s := range []string{"destek-bot (tg:@" + kvBaska + ")", "destek-bot (tg:@" + kvAd + "2)"} {
		var c int
		_ = o.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE actor_username=?`, s).Scan(&c)
		if c != 1 {
			t.Errorf("başka kullanıcının satırı değişti: %s (%d)", s, c)
		}
	}
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action=? AND target='tg:silindi'`, dEslesmePortal).Scan(&n)
	if n != 1 {
		t.Errorf("eşleştirme denetim olgusu anonim biçimde kalmalı: %d", n)
	}
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action=? AND actor_username='destek-bot (tg:silindi)'`, dBaglantiIptal).Scan(&n)
	if n != 1 {
		t.Errorf("iptal denetim olgusu anonim aktörle kalmalı: %d", n)
	}
	// iptal olgusu korunur
	if d, nd := o.durum(id); d != durumIptal || nd != string(p.IptalUnutBeni) {
		t.Errorf("iptal olgusu: %s %s", d, nd)
	}
}

// Çevrimdışı ajan: merkez kaydı yok → ajan yeniden bağlanınca merkez BaglantiIptal{merkez} döner;
// bu yol da anonimleştirir. Panelde zaten 'iptal' olan bağlantıya gelen unutbeni de anonimleştirir.
func TestUnutBeni_CevrimdisiYenidenBaglanma_VeZatenIptal(t *testing.T) {
	o := kurOrtam(t, false, true)
	id := o.kokEslestir(kvTg, kvAd, "okuma")
	o.m.gonder(&p.BaglantiIptal{BaglantiID: id, Neden: p.IptalMerkez}) // merkezin "kaydım yok" yanıtı
	kvBekleIptalAnon(t, o, id)
	if k := kvKalintilar(t, o, "777000111", "tg:@"+kvAd, "tg:"+kvAd); len(k) > 0 {
		t.Fatalf("kalıntı (merkez iptali): %v", k)
	}

	// panelden iptal edilmiş (kullanıcı kimliği hâlâ satırda) + sonra unutbeni
	id2 := o.kokEslestir(kvTg+1, kvAd+"b", "okuma")
	var rowID int64
	_ = o.db.QueryRow(`SELECT id FROM destek_baglantilari WHERE kimlik=?`, id2).Scan(&rowID)
	if !iptal.Tek(context.Background(), o.db, rowID, p.IptalKullanici, "root") {
		t.Fatal("panel iptali")
	}
	var tg int64
	_ = o.db.QueryRow(`SELECT tg_user_id FROM destek_baglantilari WHERE kimlik=?`, id2).Scan(&tg)
	if tg != kvTg+1 {
		t.Fatal("panel iptali kimliği silmemeli (otomatik iptal kimliği hemen anonimleştirmez)")
	}
	o.m.gonder(&p.BaglantiIptal{BaglantiID: id2, Neden: p.IptalUnutBeni})
	kvBekleIptalAnon(t, o, id2)
	if k := kvKalintilar(t, o, "777000112", "tg:@"+kvAd+"b"); len(k) > 0 {
		t.Fatalf("kalıntı (zaten iptal): %v", k)
	}
}

// Aynı kullanıcının başka (zaten iptal) satırları da, otomatik iptal olmuş olsa bile, unutbeni ile temizlenir.
func TestUnutBeni_AyniKullanicininEskiIptalSatirlari(t *testing.T) {
	o := kurOrtam(t, false, true)
	eski := o.kokEslestir(kvTg, kvAd, "okuma")
	var eskiID int64
	_ = o.db.QueryRow(`SELECT id FROM destek_baglantilari WHERE kimlik=?`, eski).Scan(&eskiID)
	o.a.otomatikIptal(context.Background(), mustBag(t, o, eski), p.IptalSuresiDoldu)
	// otomatik iptal kimliği hemen silmez (panel "kim bağlanmıştı" görsün)
	var tg int64
	_ = o.db.QueryRow(`SELECT tg_user_id FROM destek_baglantilari WHERE id=?`, eskiID).Scan(&tg)
	if tg != kvTg {
		t.Fatal("otomatik iptal kimliği hemen silmemeli")
	}
	yeni := o.kokEslestir(kvTg, kvAd, "okuma")
	o.m.gonder(&p.BaglantiIptal{BaglantiID: yeni, Neden: p.IptalUnutBeni})
	kvBekleIptalAnon(t, o, yeni)
	kvBekleIptalAnon(t, o, eski)
	if k := kvKalintilar(t, o, "777000111", "tg:@"+kvAd, "tg:"+kvAd); len(k) > 0 {
		t.Fatalf("kalıntı: %v", k)
	}
}

func mustBag(t *testing.T, o *ortam, kimlik string) *baglanti {
	t.Helper()
	b, err := baglantiKimlikle(context.Background(), o.db, kimlik)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Süpürge: 'iptal' olalı anonimSaklamaGun geçen satırın kimliği silinir; denetim satırına DOKUNULMAZ.
func TestSupurge_EskiIptalAnonimlesir(t *testing.T) {
	o := kurOrtam(t, false, true)
	id := o.kokEslestir(kvTg, kvAd, "okuma")
	o.a.otomatikIptal(context.Background(), mustBag(t, o, id), p.IptalSuresiDoldu)
	o.a.SupurgeTur(context.Background())
	var tg int64
	_ = o.db.QueryRow(`SELECT tg_user_id FROM destek_baglantilari WHERE kimlik=?`, id).Scan(&tg)
	if tg != kvTg {
		t.Fatal("taze iptalde kimlik silindi")
	}
	if _, err := o.db.Exec(`UPDATE destek_baglantilari SET iptal_at = NOW() - INTERVAL ? DAY WHERE kimlik=?`, anonimSaklamaGun+1, id); err != nil {
		t.Fatal(err)
	}
	o.a.SupurgeTur(context.Background())
	kvBekleIptalAnon(t, o, id)
	var n int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE INSTR(target, ?)>0 OR INSTR(actor_username, ?)>0`, "@"+kvAd, "@"+kvAd).Scan(&n)
	if n == 0 {
		t.Error("süpürge denetim izine dokundu (ürün sahibi kararı olmadan değişmemeli)")
	}
}
