package e2e

import (
	"crypto/ed25519"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"girginospanel/internal/destek/ajan"
	"girginospanel/internal/destek/merkez"
	"girginospanel/internal/destek/protokol"
	"girginospanel/internal/maske"
)

const (
	tgA    = int64(710001) // müşteri A (a.test)
	tgB    = int64(720002) // müşteri B (b.test, başka bayi)
	tgBayi = int64(730003) // bayi A (a.test, a2.test)
)

// teshisSenaryosu — "sitem 502": Sonnet sahtesi site_yokla + log_oku ister, sonra
// araç sonuçlarını (bir modelin yapabileceği gibi) yanıtına AYNEN kopyalar — kanarya
// ajandan sızmışsa Telegram'a kadar taşınsın diye.
func teshisSenaryosu(t *testing.T, mu *sync.Mutex, hatalar *[]string) func(int, []byte) *merkez.ClaudeYanit {
	kaydet := func(f string, a ...any) {
		mu.Lock()
		*hatalar = append(*hatalar, fmt.Sprintf(f, a...))
		mu.Unlock()
	}
	return func(n int, govde []byte) *merkez.ClaudeYanit {
		switch n {
		case 0:
			if m := govdeMetni(govde); !strings.Contains(m, "<oturum>") || !strings.Contains(m, "1: a.test") || strings.Contains(m, "b.test") {
				kaydet("ilk istekte oturum bloğu beklenmedik: %q", m)
			}
			return yanit("tool_use", metinBlok("Bakıyorum."),
				aracBlok("toolu_e2e_1", "site_yokla", `{"hesap":1}`),
				aracBlok("toolu_e2e_2", "log_oku", `{"hesap":1,"tur":"nginx_error"}`))
		default:
			sonuclar := sonAracSonuclari(govde)
			if len(sonuclar) != 2 {
				kaydet("araç sonucu sayısı %d", len(sonuclar))
			}
			return yanit("end_turn", metinBlok("Teşhis: siteniz HTTP 502 döndürüyor; nginx hata günlüğünde PHP-FPM zaman aşımı görünüyor.\nGözlem:\n"+
				strings.Join(sonuclar, "\n")+"\nÖneri: panelden PHP-FPM hizmetini yeniden başlatın."))
		}
	}
}

// (a) eşleştirme + (b) teşhis + (c) iki taşıma + (g) kanarya.
func TestUcuncaUcEslesmeVeTeshis(t *testing.T) {
	for _, tasima := range []string{"wss", "yoklama"} {
		t.Run(tasima, func(t *testing.T) {
			o := kur(t, tasima == "yoklama", nil)
			o.bagliBekle()
			if d := o.ajan().DurumAl(); d.Tasima != tasima || d.Durum != ajan.BagBagli {
				t.Fatalf("taşıma: %+v", d)
			}
			kim := o.eslestir(o.mA, tgA, "musteri_a", "okuma", true)

			var mu sync.Mutex
			var hatalar []string
			o.c.senaryo(teshisSenaryosu(t, &mu, &hatalar))
			o.mesaj(tgA, "musteri_a", "sitem 502 veriyor, bakar mısın?")
			son := o.tg.bekle(t, tgA, "Teşhis:")
			mu.Lock()
			for _, h := range hatalar {
				t.Error(h)
			}
			mu.Unlock()

			// (b) gerçek zincir: imzalı iş emri → ajan → gerçek yürütücü → maskeli sonuç → Telegram
			if y := o.yoklananlar(); !slices.Equal(y, []string{"https://a.test/"}) {
				t.Fatalf("site_yokla hedefleri: %q", y)
			}
			if l := o.logAlanlari(); !slices.Equal(l, []string{"42"}) {
				t.Fatalf("log rotası: %q", l)
			}
			for _, parca := range []string{"durum_kodu: 502", "upstream timed out", "DB_PASSWORD=" + maske.Isaret} {
				if !strings.Contains(son, parca) {
					t.Fatalf("Telegram teşhisinde %q yok:\n%s", parca, son)
				}
			}
			if is := o.islemSonuclari(kim); !slices.Equal(is, []string{"site_yokla=ok", "log_oku=ok"}) {
				t.Fatalf("ajan iş kayıtları: %q", is)
			}
			if o.c.anaSayisi() != 2 {
				t.Fatalf("ana model çağrısı %d", o.c.anaSayisi())
			}

			// (g) kanarya: test hesabının günlüğündeki sır ve panel sırrı ajandan ÇIKMAZ —
			// merkezin Claude'a gönderdiği hiçbir şeyde, Telegram'da ve merkez kaydında yok.
			claude := o.c.tumGovde()
			if !strings.Contains(claude, "SQLSTATE") || !strings.Contains(claude, "panel ayar dökümü") {
				t.Fatal("kanarya satırları merkeze hiç ulaşmadı — denetim anlamsız")
			}
			for _, k := range []string{kanaryaLog, panelSirri} {
				if strings.Contains(claude, k) {
					t.Fatalf("kanarya %q Claude isteğine sızdı", k)
				}
				if strings.Contains(o.tg.tumMetin(), k) {
					t.Fatalf("kanarya %q Telegram çıktısına sızdı", k)
				}
				if strings.Contains(o.depoIcerigi(), k) {
					t.Fatalf("kanarya %q merkez konuşma kaydına sızdı", k)
				}
			}
			if dk := o.depoIcerigi(); !strings.Contains(dk, "site_yokla") || !strings.Contains(dk, "Teşhis") {
				t.Fatal("merkez konuşma kaydı yazılmadı — kanarya denetimi anlamsız")
			}
		})
	}
}

// Portal akışı uçtan uca (WSS ve uzun yoklama): sahte portal → /ic/v1/eslesme-kodu → Telegram /start →
// ajan doğrudan etkin kök bağlantısı → soru yanıtlanır → portal listeler ve iptal eder. Ayrıca iç API
// kimlik doğrulaması ve doğrulanmamış sunucunun kod alamaması.
func TestPortalUctanUca(t *testing.T) {
	for _, tasima := range []string{"wss", "yoklama"} {
		t.Run(tasima, func(t *testing.T) {
			o := kur(t, tasima == "yoklama", nil)
			o.bagliBekle()
			// Bearer: yok / yanlış → 401; yanlış sırla kod alınamaz
			for _, sir := range []string{"", "yanlis-sir"} {
				if d, _ := o.portal("POST", "/ic/v1/eslesme-kodu", map[string]any{"parmak_izi": e2eParmakIzi}, sir); d != http.StatusUnauthorized {
					t.Fatalf("sır %q: %d", sir, d)
				}
			}
			if d, m := o.portal("GET", "/ic/v1/sunucu?parmak_izi="+e2eParmakIzi, nil, portalIc); d != 200 || m["dogrulandi"] != true || m["cevrimici"] != true || m["bulundu"] != true {
				t.Fatalf("sunucu durumu: %d %v", d, m)
			}
			kim := o.portalEslestir(tgA, "musteri_a", "okuma")

			var rol, durum, olusturan string
			if err := o.db.QueryRow(`SELECT rol, durum, olusturan FROM destek_baglantilari WHERE kimlik=?`, kim).Scan(&rol, &durum, &olusturan); err != nil {
				t.Fatal(err)
			}
			if rol != "kok" || durum != "aktif" || olusturan != "portal:u_42" {
				t.Fatalf("ajan DB satırı: %s %s %s", rol, durum, olusturan)
			}
			// soru yanıtlanır (KVKK düğmesi / panel onayı beklenmeden)
			o.c.senaryo(func(n int, govde []byte) *merkez.ClaudeYanit {
				if n == 0 {
					return yanit("tool_use", aracBlok("toolu_p_1", "site_yokla", `{"hesap":1}`))
				}
				return yanit("end_turn", metinBlok("Teşhis: portal eşleştirmesiyle yanıt."))
			})
			o.mesaj(tgA, "musteri_a", "sitem açılmıyor")
			o.tg.bekle(t, tgA, "Teşhis: portal eşleştirmesiyle")
			if is := o.islemSonuclari(kim); !slices.Equal(is, []string{"site_yokla=ok"}) {
				t.Fatalf("iş kayıtları: %q", is)
			}
			// portal listesi + iptal
			d, m := o.portal("GET", "/ic/v1/baglantilar?parmak_izi="+e2eParmakIzi, nil, portalIc)
			l, _ := m["baglantilar"].([]any)
			if d != 200 || len(l) != 1 || l[0].(map[string]any)["baglanti_id"] != kim || l[0].(map[string]any)["kvkk_kaynak"] != "portal" {
				t.Fatalf("portal listesi: %d %v", d, m)
			}
			if d, m := o.portal("POST", "/ic/v1/baglanti/iptal", map[string]any{"parmak_izi": e2eParmakIzi, "baglanti_id": kim}, portalIc); d != 200 || m["ajana_iletildi"] != true {
				t.Fatalf("portal iptali: %d %v", d, m)
			}
			o.kosulBekle("ajan portal iptalini uyguladı", 10*time.Second, func() bool {
				d, n := o.dbDurum(kim)
				return d == "iptal" && n == string(protokol.IptalKullanici)
			})
			o.tg.bekle(t, tgA, "Destek bağlantınız kapatıldı (kullanici)")
			n := o.c.anaSayisi()
			o.mesaj(tgA, "musteri_a", "tekrar yazıyorum")
			o.tg.bekle(t, tgA, "bağlı bir sunucunuz yok")
			if o.c.anaSayisi() != n {
				t.Fatal("iptal edilmiş bağlantı Claude'a gitti")
			}
		})
	}
}

// (d) /unutbeni ve panelden iptal iki yönde yansır; ajan çevrimdışıyken /unutbeni
// yeniden bağlanınca ajana da kapattırılır.
func TestIptalIkiYonde(t *testing.T) {
	o := kur(t, false, nil)
	o.bagliBekle()

	// Telegram → panel: /unutbeni
	kimA := o.eslestir(o.mA, tgA, "musteri_a", "okuma", false)
	o.mesaj(tgA, "musteri_a", "/unutbeni")
	o.callback(tgA, "unut:evet")
	o.tg.bekle(t, tgA, "Silindi")
	o.kosulBekle("panelde unutbeni iptali", 10*time.Second, func() bool {
		d, n := o.dbDurum(kimA)
		return d == "iptal" && n == string(protokol.IptalUnutBeni)
	})
	if _, ok := o.merkezBaglantisi(tgA); ok {
		t.Fatal("unutbeni sonrası merkezde bağlantı kaldı")
	}

	// panel → Telegram: kök panelden iptal eder (bayi/müşteri panelden bağlantı yönetemez)
	kimBayi := o.eslestir(o.bA, tgBayi, "bayi_a_tg", "okuma", false)
	if y := o.istek("POST", "/destek/baglantilar/"+kimBayi+"/iptal", o.bA, nil); y.kod != 403 {
		t.Fatalf("bayi panelden iptal edebildi: %d", y.kod)
	}
	o.istekOK("POST", "/destek/baglantilar/"+kimBayi+"/iptal", o.kok, nil)
	o.tg.bekle(t, tgBayi, "Destek bağlantınız kapatıldı (kullanici)")
	o.kosulBekle("merkez bağlantıyı sildi", 10*time.Second, func() bool { _, ok := o.merkezBaglantisi(tgBayi); return !ok })
	n := o.c.anaSayisi()
	o.mesaj(tgBayi, "bayi_a_tg", "sitem yavaş")
	o.tg.bekle(t, tgBayi, "bağlı bir sunucunuz yok")
	if o.c.anaSayisi() != n {
		t.Fatal("iptal edilmiş bağlantı Claude'a gitti")
	}

	// ajan çevrimdışıyken /unutbeni → yeniden bağlanınca ajan da kapatır (merkez kaydı yok)
	kimB := o.eslestir(o.mB, tgB, "musteri_b", "okuma", false)
	o.ajanDurdur()
	o.kosulBekle("merkez ajanı çevrimdışı gördü", 10*time.Second, func() bool { return o.metrikSayi("cevrimici_ajan") == 0 })
	o.mesaj(tgB, "musteri_b", "/unutbeni")
	o.callback(tgB, "unut:evet")
	o.tg.bekle(t, tgB, "Silindi")
	if d, _ := o.dbDurum(kimB); d != "aktif" {
		t.Fatalf("çevrimdışı ajanın bağlantısı: %s", d)
	}
	o.ajanBaslat(nil, nil)
	o.bagliBekle()
	o.kosulBekle("yeniden bağlanınca merkez kaydı olmayan bağlantı kapandı", 10*time.Second, func() bool {
		d, n := o.dbDurum(kimB)
		return d == "iptal" && n == string(protokol.IptalMerkez)
	})
	// senkron, önceki iptalleri yeniden bildirdi: merkez sessiz kalır, oturum düşmez
	time.Sleep(300 * time.Millisecond)
	if !o.ajan().Bagli() {
		t.Fatalf("senkron sonrası ajan oturumu düştü: %+v", o.ajan().DurumAl())
	}
}

// (e) Müşteri A'nın Telegram'ı B'nin hesabına erişemez: merkez hattı (oturum kapsamı,
// bağlantı seçimi, 1. hat girdi denetimi) ve ajan hattı (merkez kaydı bozulsa bile
// yerel kapsam).
func TestMusteriBaskaHesabaErisemez(t *testing.T) {
	o := kur(t, false, nil)
	o.bagliBekle()
	kimA := o.eslestir(o.mA, tgA, "musteri_a", "okuma", false)
	o.eslestir(o.mB, tgB, "musteri_b", "okuma", false)
	bB, _ := o.merkezBaglantisi(tgB)

	// merkez hattı 1: A, B'nin bağlantısını seçemez
	o.callback(tgA, "sec:"+baglantiKisa(bB.BaglantiID))
	if k, _ := o.depo.KullaniciAl(tgA); k.SeciliBaglanti == bB.BaglantiID {
		t.Fatal("A, B'nin bağlantısını seçebildi")
	}

	// merkez hattı 2: kapsam dışı hesap numarası ajana hiç gitmez
	redOnce := o.sayac("arac_red_merkez")
	o.c.senaryo(func(n int, govde []byte) *merkez.ClaudeYanit {
		switch n {
		case 0:
			return yanit("tool_use", aracBlok("toolu_e_1", "site_yokla", `{"hesap":2}`))
		case 1:
			return yanit("tool_use", aracBlok("toolu_e_2", "site_yokla", `{"hesap":1}`))
		}
		return yanit("end_turn", metinBlok("Teşhis: yalnız kendi hesabınıza bakabildim."))
	})
	o.mesaj(tgA, "musteri_a", "komşu siteye de bakar mısın")
	o.tg.bekle(t, tgA, "Teşhis: yalnız kendi")
	if o.sayac("arac_red_merkez") != redOnce+1 {
		t.Fatalf("merkez 1. hat reddi sayılmadı (%d→%d)", redOnce, o.sayac("arac_red_merkez"))
	}
	if y := o.yoklananlar(); !slices.Equal(y, []string{"https://a.test/"}) {
		t.Fatalf("yoklanan: %q", y)
	}
	if is := o.islemSonuclari(kimA); !slices.Equal(is, []string{"site_yokla=ok"}) {
		t.Fatalf("kapsam dışı istek ajana ulaştı: %q", is)
	}
	if strings.Contains(o.c.tumGovde(), "b.test") {
		t.Fatal("B'nin hesabı A'nın oturumunda görünüyor")
	}

	// ajan hattı: merkez kaydı bozulmuş/ele geçirilmiş — A'nın bağlantısı B'nin hesabını
	// listeliyor. Merkez 1. hattı geçer, İMZALI iş emri gider; ajan YEREL kapsamla reddeder.
	// Önce açık oturum kapansın (yeni oturum kapsamı kayıttan okur): panel ayarı
	// değişince ajan bağlantı durumlarını yeniden bildirir → merkez oturumu kapatır.
	once, _ := o.merkezBaglantisi(tgA)
	o.istekOK("PUT", "/destek/ayar", o.kok, map[string]any{"modul_duzey": "tam"})
	o.kosulBekle("ajan durumu yeniden bildirdi", 10*time.Second, func() bool {
		b, _ := o.merkezBaglantisi(tgA)
		return b.Guncelleme.After(once.Guncelleme)
	})
	bA, _ := o.merkezBaglantisi(tgA)
	bA.Hesaplar = append(bA.Hesaplar, protokol.HesapEtiketi{Sira: 2, Etiket: "b.test"})
	if err := o.depo.BaglantiKaydet(bA); err != nil {
		t.Fatal(err)
	}
	o.c.senaryo(func(n int, govde []byte) *merkez.ClaudeYanit {
		if n == 0 {
			if !strings.Contains(govdeMetni(govde), "2: b.test") {
				t.Errorf("bozuk merkez kaydı oturuma girmedi")
			}
			return yanit("tool_use", aracBlok("toolu_e_3", "site_yokla", `{"hesap":2}`))
		}
		return yanit("end_turn", metinBlok("Teşhis: ikinci hesaba erişim reddedildi."))
	})
	o.mesaj(tgA, "musteri_a", "ikinci hesaba bak")
	o.tg.bekle(t, tgA, "erişim reddedildi")
	if y := o.yoklananlar(); slices.Contains(y, "https://b.test/") || len(y) != 1 {
		t.Fatalf("ajan B'nin hesabını yokladı: %q", y)
	}
	if l := o.logAlanlari(); slices.Contains(l, "43") {
		t.Fatalf("B'nin günlüğü okundu: %q", l)
	}
	if is := o.islemSonuclari(kimA); !slices.Equal(is, []string{"site_yokla=ok", "site_yokla=" + string(protokol.HataKapsam)}) {
		t.Fatalf("ajan reddi kayda geçmedi: %q", is)
	}
	if strings.Contains(o.tg.tumMetin(), "KOMSU-GUNLUK") {
		t.Fatal("B'nin verisi Telegram'a ulaştı")
	}
}

// (f) Yanlış merkez anahtarı → ajan bağlanmaz (meydan okuma sabit anahtarla doğrulanır);
// anahtar döndürmede (eski+yeni) doğru anahtar listede ise bağlanır.
func TestYanlisMerkezAnahtari(t *testing.T) {
	for _, tasima := range []string{"wss", "yoklama"} {
		t.Run(tasima, func(t *testing.T) {
			yanlis, _ := protokol.AnahtarCiftiUret(nil)
			o := kur(t, tasima == "yoklama", []ed25519.PublicKey{yanlis.Acik})
			o.kosulBekle("ajan doğrulama hatası", 10*time.Second, func() bool {
				return strings.Contains(o.ajan().DurumAl().SonHata, "meydan okuma doğrulanamadı")
			})
			if o.ajan().Bagli() || o.metrikSayi("cevrimici_ajan") != 0 {
				t.Fatal("yanlış merkez anahtarıyla bağlanıldı")
			}
			sunucu := "s_" + o.ajan().DurumAl().CihazKimligi
			if _, var_ := o.depo.CihazAl(sunucu); var_ {
				t.Fatal("cihaz imzası taklitçi merkeze verildi (merkez cihazı kaydetti)")
			}
			o.ajanDurdur()
			o.ajanBaslat([]ed25519.PublicKey{yanlis.Acik, o.cift.Acik}, nil)
			o.bagliBekle()
			if d := o.ajan().DurumAl(); d.SunucuKimligi != sunucu {
				t.Fatalf("sunucu kimliği %q (beklenen %q)", d.SunucuKimligi, sunucu)
			}
		})
	}
}

// (h) Hesap listesi değişimi: model eski listeyle "hesap 2" der; ajan reddeder ve güncel
// listeyi gönderir; merkez bayat oturumla YENİDEN DENEMEZ (yanlış hesaba yürütme yok);
// kullanıcının yeni mesajı güncel listeyle çalışır. Uzun yoklama üzerinden (sıralama).
func TestHesapListesiDegisimi(t *testing.T) {
	o := kur(t, true, nil)
	o.bagliBekle()
	kim := o.eslestir(o.bA, tgBayi, "bayi_a_tg", "okuma", false)
	if b, _ := o.merkezBaglantisi(tgBayi); len(b.Hesaplar) != 2 || b.Hesaplar[1].Etiket != "a2.test" {
		t.Fatalf("bayi hesapları: %+v", b.Hesaplar)
	}
	// bayiye yeni hesap: sıra değişir (a.test, a3.test, a2.test)
	if _, err := o.db.Exec(`INSERT INTO domains (id, alan_adi, ftp_user, sistem_kullanici, reseller_id) VALUES (44,'a3.test','a3mus','c_a3_test',7)`); err != nil {
		t.Fatal(err)
	}
	o.c.senaryo(func(n int, govde []byte) *merkez.ClaudeYanit {
		m := govdeMetni(govde)
		ikinciVar := strings.Contains(string(govde), "toolu_h_2")
		switch {
		case n == 0:
			if !strings.Contains(m, "2: a2.test") {
				t.Errorf("ilk oturum eski listeyi görmeli")
			}
			return yanit("tool_use", aracBlok("toolu_h_1", "site_yokla", `{"hesap":2}`))
		case strings.Contains(m, "3: a2.test") && !ikinciVar:
			return yanit("tool_use", aracBlok("toolu_h_2", "site_yokla", `{"hesap":3}`))
		case ikinciVar:
			return yanit("end_turn", metinBlok("Teşhis: a2.test 502 döndürüyor."))
		}
		// bayat oturumda "yeniden deneyin"e uyan model (aynı numara, artık başka hesap)
		return yanit("tool_use", aracBlok(fmt.Sprintf("toolu_h_r%d", n), "site_yokla", `{"hesap":2}`))
	})
	o.mesaj(tgBayi, "bayi_a_tg", "ikinci sitem açılmıyor")
	o.tg.bekle(t, tgBayi, "mesajınızı yeniden gönderin")
	if y := o.yoklananlar(); len(y) != 0 {
		t.Fatalf("bayat hesap listesiyle yürütüldü: %q", y)
	}
	if is := o.islemSonuclari(kim); !slices.Equal(is, []string{"site_yokla=" + string(protokol.HataKapsam)}) {
		t.Fatalf("iş kayıtları: %q", is)
	}
	if b, _ := o.merkezBaglantisi(tgBayi); len(b.Hesaplar) != 3 || b.Hesaplar[1].Etiket != "a3.test" {
		t.Fatalf("merkez güncel listeyi almadı: %+v", b.Hesaplar)
	}
	if o.c.anaSayisi() != 1 {
		t.Fatalf("bayat oturumla model yeniden çağrıldı: %d", o.c.anaSayisi())
	}
	o.mesaj(tgBayi, "bayi_a_tg", "ikinci sitem açılmıyor")
	o.tg.bekle(t, tgBayi, "Teşhis: a2.test")
	if y := o.yoklananlar(); !slices.Equal(y, []string{"https://a2.test/"}) {
		t.Fatalf("güncel listeyle hedef: %q", y)
	}
}
