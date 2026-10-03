package ajan

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/auth"
	"girginospanel/internal/destek/arac"
	"girginospanel/internal/destek/iptal"
	p "girginospanel/internal/destek/protokol"
)

// Eşleştirme uçtan uca (WSS ve uzun yoklama): kod → merkez imzalı onay → bekleyen →
// panelde ikinci onay → etkin + hesap listesi → iş emri → yürütücü YEREL kapsamla →
// maskeli sonuç → iş kaydı + denetim.
func TestAkisMusteriEslesmeUctanUca(t *testing.T) {
	for _, tur := range []struct {
		ad      string
		yoklama bool
	}{{"wss", false}, {"yoklama", true}} {
		t.Run(tur.ad, func(t *testing.T) {
			o := kurOrtam(t, tur.yoklama, true)
			if d := o.a.DurumAl(); d.Tasima != map[bool]string{false: "wss", true: "yoklama"}[tur.yoklama] {
				t.Fatalf("taşıma: %+v", d)
			}
			y := o.istekOK("POST", "/destek/eslestir", o.mA, map[string]any{"duzey": "okuma", "sure_gun": 7})
			kod := y.govde["kod"].(string)
			if u := y.govde["baglanti_url"].(string); u != "https://t.me/GirginTestBot?start="+kod {
				t.Fatalf("bağlantı: %s", u)
			}
			if !strings.HasPrefix(y.govde["qr"].(string), "data:image/png;base64,") || y.govde["merkez_bagli"] != true {
				t.Fatalf("qr/merkez: %v", y.ham)
			}
			oz, _ := p.EslesmeKoduOzet(kod)
			ko := o.m.bekle(p.TipKodOzet, nil).(*p.KodOzet)
			if ko.KodOzet != oz || strings.Contains(o.m.srv.URL, kod) {
				t.Fatalf("merkeze özet yerine başka şey gitti: %+v", ko)
			}
			e := &p.EslesmeOnayla{KodOzet: oz, TgUserID: 111222333, TgKullaniciAdi: "musteri_a"}
			o.m.imzala(e)
			o.m.gonder(e)
			bd := o.m.bekle(p.TipBaglantiDurum, nil).(*p.BaglantiDurum)
			if bd.Durum != p.BaglantiOnayBekliyor || bd.Duzey != p.DuzeyKapali || len(bd.Hesaplar) != 0 || bd.Rol != p.RolMusteri {
				t.Fatalf("bekleyen: %+v", bd)
			}
			kim := bd.BaglantiID

			// Onaysız bağlantıya iş emri: panel_onayi_bekliyor, yürütücü çağrılmaz.
			e1 := o.m.isEmri(kim, "hesap_ozet", `{"hesap":1}`)
			o.m.gonder(e1)
			if s := o.m.isSonucu(e1.IsKodu); s.Ok || s.HataKodu != p.HataOnayBekliyor {
				t.Fatalf("onaysız iş: %+v", s)
			}

			// Kapsam: müşteri B ve bayi B görmez/onaylayamaz; bayi A görür ama sahibi değil → onaylayamaz.
			if y := o.istekOK("GET", "/destek/baglantilar", o.mB, nil); len(y.govde["baglantilar"].([]any)) != 0 {
				t.Fatalf("müşteri B A'nın bağlantısını gördü: %s", y.ham)
			}
			govde := map[string]any{"aydinlatma_surum": aydinlatmaSurum, "kabul": true}
			for _, tok := range []string{o.mB, o.bB, o.bA, o.mA2} {
				if y := o.istek("POST", "/destek/baglantilar/"+kim+"/onayla", tok, govde); y.kod != 404 {
					t.Fatalf("sahibi olmayan onayladı: %d %s", y.kod, y.ham)
				}
			}
			if y := o.istekOK("GET", "/destek/baglantilar", o.bA, nil); len(y.govde["baglantilar"].([]any)) != 1 {
				t.Fatalf("bayi A müşterisinin bağlantısını görmeli: %s", y.ham)
			}
			if y := o.istek("POST", "/destek/baglantilar/"+kim+"/onayla", o.mA, map[string]any{"aydinlatma_surum": "eski", "kabul": true}); y.kod != 400 {
				t.Fatalf("eski aydınlatma sürümü kabul edildi: %d", y.kod)
			}
			// Müşteri ADINA oturum (bayi "panele gir") onaylayamaz.
			adina := musteriJWT(t, 5, 42, "amus", "a.test", &auth.MusteriClaims{AktorUID: 7, AktorAd: "bayi_a", AktorRol: "reseller", AktorRID: 7})
			if y := o.istek("POST", "/destek/baglantilar/"+kim+"/onayla", adina, govde); y.kod != 403 {
				t.Fatalf("adına oturum onayladı: %d %s", y.kod, y.ham)
			}

			et := o.onayla(o.mA, kim)
			if et.Duzey != p.DuzeyOkuma || len(et.Hesaplar) != 1 || et.Hesaplar[0] != (p.HesapEtiketi{Sira: 1, Etiket: "a.test"}) {
				t.Fatalf("etkin bağlantı: %+v", et)
			}

			e2 := o.m.isEmri(kim, "hesap_ozet", `{"hesap":1}`)
			o.m.gonder(e2)
			s := o.m.isSonucu(e2.IsKodu)
			if !s.Ok {
				t.Fatalf("iş başarısız: %+v", s)
			}
			tum := s.Ozet + strings.Join(s.Satirlar, "\n")
			if strings.Contains(tum, dtKanarya) || strings.Contains(tum, "gizli123") {
				t.Fatalf("sonuç maskelenmedi: %q", tum)
			}
			c := o.y.son()
			if !c.ok || c.kap.DomainID != 42 || c.kap.FTPHesapID != 5 || c.kap.ResellerID != 7 || c.kap.Aktor != "destek-bot (tg:@musteri_a)" ||
				len(c.ob.Hesaplar) != 1 || c.ob.Hesaplar[0].Etiket != "a.test" || c.ob.Rol != p.RolMusteri || c.ob.Duzey != p.DuzeyOkuma {
				t.Fatalf("yürütücü bağlamı: %+v", c)
			}
			if h, ok := c.kap.HesapDomain(1); !ok || h != 42 {
				t.Fatal("hesap → domain eşlemesi")
			}

			// Kapsam dışı hesap (2) → kapsam_disi, yürütücü çağrılmaz, red denetime geçer.
			n := o.y.sayi()
			e3 := o.m.isEmri(kim, "hesap_ozet", `{"hesap":2}`)
			o.m.gonder(e3)
			if s := o.m.isSonucu(e3.IsKodu); s.Ok || s.HataKodu != p.HataKapsam {
				t.Fatalf("kapsam dışı: %+v", s)
			}
			// Müşteri kök aracı çağıramaz.
			e4 := o.m.isEmri(kim, "sunucu_log_oku", `{"kaynak":"nginx_error"}`)
			o.m.gonder(e4)
			if s := o.m.isSonucu(e4.IsKodu); s.Ok || (s.HataKodu != p.HataYetki && s.HataKodu != p.HataParam) {
				t.Fatalf("müşteri kök aracı: %+v", s)
			}
			if o.y.sayi() != n {
				t.Fatal("reddedilen iş yürütücüye ulaştı")
			}

			var ok, red int
			_ = o.db.QueryRow(`SELECT SUM(sonuc='ok'), SUM(sonuc<>'ok') FROM destek_islemleri`).Scan(&ok, &red)
			if ok != 1 || red != 2 {
				t.Fatalf("iş kayıtları ok=%d red=%d", ok, red)
			}
			if o.denetimSay(dEslesmeKod, "musteri:42") != 1 || o.denetimSay(dEslesmeBekliyor, "destek-bot (tg:@musteri_a)") != 1 ||
				o.denetimSay(dEslesmeOnay, "musteri:42") != 1 || o.denetimSay(dIsRed, "destek-bot") != 2 {
				t.Fatal("denetim kayıtları eksik")
			}
			// R0 özet: işler tek denetim satırına toplanır.
			o.a.R0Ozet(context.Background())
			if o.denetimSay(dR0Ozet, "destek-bot (tg:@musteri_a)") != 1 {
				t.Fatal("R0 özeti yazılmadı")
			}
			var kalan int
			_ = o.db.QueryRow(`SELECT COUNT(*) FROM destek_islemleri WHERE denetim_ozet=0`).Scan(&kalan)
			if kalan != 0 {
				t.Fatalf("özetlenmemiş R0 kaydı kaldı: %d", kalan)
			}

			// Panelden iptal → merkeze bildirim; iptal sonrası iş emri reddedilir.
			o.istekOK("POST", "/destek/baglantilar/"+kim+"/iptal", o.mA, nil)
			o.m.bekle(p.TipBaglantiIptal, func(g p.Govde) bool {
				i := g.(*p.BaglantiIptal)
				return i.BaglantiID == kim && i.Neden == p.IptalKullanici
			})
			e5 := o.m.isEmri(kim, "hesap_ozet", `{"hesap":1}`)
			o.m.gonder(e5)
			if s := o.m.isSonucu(e5.IsKodu); s.HataKodu != p.HataBaglantiIptal {
				t.Fatalf("iptal sonrası iş: %+v", s)
			}
		})
	}
}

// Kod: süresi dolmuş, yeniden kullanılmış ve aynı imzalı mesajın tekrarı reddedilir.
func TestKodSuresiVeTekrar(t *testing.T) {
	o := kurOrtam(t, false, true)
	y := o.istekOK("POST", "/destek/eslestir", o.mA, map[string]any{"duzey": "okuma", "sure_gun": 1})
	oz, _ := p.EslesmeKoduOzet(y.govde["kod"].(string))
	eski := o.a.kodlar.saat
	o.a.kodlar.saat = func() time.Time { return time.Now().Add(p.EslesmeKoduOmru + time.Second) }
	e := &p.EslesmeOnayla{KodOzet: oz, TgUserID: 5, TgKullaniciAdi: "musteri_a"}
	o.m.imzala(e)
	zk, _ := o.m.gonder(e)
	if h := o.m.hata(zk); h.Kod != p.HataEslesmeKodu {
		t.Fatalf("süresi dolmuş kod: %+v", h)
	}
	o.a.kodlar.saat = eski

	kim := o.eslestir(o.mA, 6, "musteri_a", "okuma")
	_ = kim
	// Aynı kod (yeni nonce ile yeniden imzalı) ikinci kez: tek kullanımlık.
	var oz2 string
	_ = o.db.QueryRow(`SELECT kod_ozet FROM destek_baglantilari WHERE kimlik=?`, kim).Scan(&oz2)
	e2 := &p.EslesmeOnayla{KodOzet: oz2, TgUserID: 999, TgKullaniciAdi: "saldirgan_x"}
	o.m.imzala(e2)
	zk2, ham := o.m.gonder(e2)
	if h := o.m.hata(zk2); h.Kod != p.HataEslesmeKodu {
		t.Fatalf("kullanılmış kod: %+v", h)
	}
	// Aynı çerçevenin birebir tekrarı: imza önbelleği (tekrar).
	o.m.gonderHam(ham)
	if h := o.m.hata(zk2); h.Kod != p.HataTekrar {
		t.Fatalf("birebir tekrar: %+v", h)
	}
	// İmzası bozuk eşleştirme (tg kimliği imzadan sonra değişti).
	y3 := o.istekOK("POST", "/destek/eslestir", o.mA, map[string]any{"duzey": "okuma", "sure_gun": 1})
	oz3, _ := p.EslesmeKoduOzet(y3.govde["kod"].(string))
	e3 := &p.EslesmeOnayla{KodOzet: oz3, TgUserID: 7, TgKullaniciAdi: "musteri_a"}
	o.m.imzala(e3)
	e3.TgUserID = 666
	zk3, _ := o.m.gonder(e3)
	if h := o.m.hata(zk3); h.Kod != p.HataImza {
		t.Fatalf("imzası bozuk eşleştirme: %+v", h)
	}
	var n int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM destek_baglantilari`).Scan(&n)
	if n != 1 {
		t.Fatalf("bağlantı sayısı %d (beklenen 1)", n)
	}
}

// İş emri: imzasız/bozuk, bilinmeyen anahtar, birebir tekrar, aynı iş kodu, eski → ret; yürütücü yalnız bir kez.
func TestIsEmriImzaTekrarEski(t *testing.T) {
	o := kurOrtam(t, false, true)
	kim := o.eslestir(o.mA, 42, "musteri_a", "okuma")
	o.onayla(o.mA, kim)

	bozuk := o.m.isEmri(kim, "hesap_ozet", `{"hesap":1}`)
	bozuk.Param = json.RawMessage(`{"hesap":2}`)
	o.m.gonder(bozuk)
	if s := o.m.isSonucu(bozuk.IsKodu); s.HataKodu != p.HataImza {
		t.Fatalf("bozuk imza: %+v", s)
	}
	yabanci, _ := p.AnahtarCiftiUret(nil)
	yi := p.YeniImzalayici(yabanci)
	k, _ := p.YeniIsKodu(nil)
	ye := &p.IsEmri{IsKodu: k, Oturum: "oturum-test-01", BaglantiID: kim, Arac: "hesap_ozet", Param: json.RawMessage(`{"hesap":1}`), Risk: p.R0}
	_ = yi.Imzala(ye, 30*time.Second)
	o.m.gonder(ye)
	if s := o.m.isSonucu(ye.IsKodu); s.HataKodu != p.HataAnahtarBilinmiyor {
		t.Fatalf("yabancı anahtar: %+v", s)
	}

	iyi := o.m.isEmri(kim, "hesap_ozet", `{"hesap":1}`)
	_, ham := o.m.gonder(iyi)
	if s := o.m.isSonucu(iyi.IsKodu); !s.Ok {
		t.Fatalf("geçerli iş: %+v", s)
	}
	o.m.gonderHam(ham)
	if s := o.m.isSonucu(iyi.IsKodu); s.HataKodu != p.HataTekrar {
		t.Fatalf("birebir tekrar: %+v", s)
	}
	// Aynı is_kodu, yeni nonce (merkez yeniden imzaladı): DB tekilliği.
	ayni := &p.IsEmri{IsKodu: iyi.IsKodu, Oturum: "oturum-test-01", BaglantiID: kim, Arac: "hesap_ozet", Param: json.RawMessage(`{"hesap":1}`), Risk: p.R0}
	o.m.imzala(ayni)
	o.m.gonder(ayni)
	if s := o.m.isSonucu(iyi.IsKodu); s.HataKodu != p.HataTekrar {
		t.Fatalf("aynı iş kodu: %+v", s)
	}
	// Eski: 2 dk önce verilmiş.
	o.m.imz.Saat = func() time.Time { return time.Now().Add(-2 * time.Minute) }
	eski := o.m.isEmri(kim, "hesap_ozet", `{"hesap":1}`)
	o.m.imz.Saat = time.Now
	o.m.gonder(eski)
	if s := o.m.isSonucu(eski.IsKodu); s.HataKodu != p.HataSuresiDoldu {
		t.Fatalf("eski iş: %+v", s)
	}
	// Başka bağlantı kimliği (var olmayan).
	yok := o.m.isEmri("YOKyokYOKyokYOKyokYOKy", "hesap_ozet", `{"hesap":1}`)
	o.m.gonder(yok)
	if s := o.m.isSonucu(yok.IsKodu); s.HataKodu != p.HataBaglantiYok {
		t.Fatalf("olmayan bağlantı: %+v", s)
	}
	if o.y.sayi() != 1 {
		t.Fatalf("yürütücü %d kez çağrıldı (beklenen 1)", o.y.sayi())
	}
}

// Modül kapalı: bağlantı yok, kod üretilemez; açılınca bağlanır, kapatılınca kopar
// ve iş emri destek_kapali ile reddedilir.
func TestModulKapali(t *testing.T) {
	o := kurOrtam(t, false, false)
	time.Sleep(700 * time.Millisecond)
	if o.m.baglanti.Load() != 0 || o.a.Bagli() {
		t.Fatal("modül kapalıyken merkeze bağlanıldı")
	}
	if y := o.istek("POST", "/destek/eslestir", o.kok, map[string]any{"duzey": "okuma", "sure_gun": 1}); y.kod != 403 {
		t.Fatalf("kapalı modülde kod üretildi: %d %s", y.kod, y.ham)
	}
	if y := o.istekOK("GET", "/destek/durum", o.kok, nil); y.govde["modul_acik"] != false {
		t.Fatalf("durum: %s", y.ham)
	}
	// Müşteri/bayi ayarı değiştiremez; bayi modül şalterine dokunamaz.
	if y := o.istek("PUT", "/destek/ayar", o.mA, map[string]any{"musteri_duzey": "tam"}); y.kod != 403 {
		t.Fatalf("müşteri ayar değiştirdi: %d", y.kod)
	}
	if y := o.istek("PUT", "/destek/ayar", o.bA, map[string]any{"modul_duzey": "tam"}); y.kod != 403 {
		t.Fatalf("bayi modül şalterini değiştirdi: %d", y.kod)
	}
	o.istekOK("PUT", "/destek/ayar", o.kok, map[string]any{"modul_duzey": "okuma", "bayi_duzey": "okuma", "musteri_duzey": "okuma"})
	o.bagliBekle()
	kim := o.eslestir(o.kok, 1, "kok_kullanici", "okuma")
	o.onayla(o.kok, kim)
	emir := o.m.isEmri(kim, "hesap_ozet", `{"hesap":1}`)

	o.istekOK("PUT", "/destek/ayar", o.kok, map[string]any{"modul_duzey": "kapali"})
	son := time.Now().Add(5 * time.Second)
	for o.a.Bagli() {
		if time.Now().After(son) {
			t.Fatal("modül kapatılınca bağlantı kesilmedi")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Yarış: kapanma anında gelen iş emri de reddedilir (yürütme yolunda yeniden denetim).
	if s := o.a.isYurut(context.Background(), emir); s.HataKodu != p.HataDevreDisi {
		t.Fatalf("kapalı modülde iş: %+v", s)
	}
	// Şalter, bağlantı kaydına bakılmadan ÖNCE denetlenir (kapalı modülde DB'de iz/sorgu yok).
	if s := o.a.isYurut(context.Background(), o.m.isEmri("YOKyokYOKyokYOKyokYOKy", "hesap_ozet", `{"hesap":1}`)); s.HataKodu != p.HataDevreDisi {
		t.Fatalf("kapalı modülde olmayan bağlantı: %+v", s)
	}
	if o.y.sayi() != 0 {
		t.Fatal("kapalı modülde yürütücü çağrıldı")
	}
	n := o.m.baglanti.Load()
	time.Sleep(700 * time.Millisecond)
	if o.m.baglanti.Load() != n {
		t.Fatal("kapalı modül yeniden bağlanmaya çalıştı")
	}
}

// Otomatik iptal: FTP parola değişimi (iz), hosting askısı (tetik), bayi askısı (tetik:
// bayi + müşterileri), bayi durumu (süpürge), 2FA kapatma (tetik + süpürge).
func TestOtomatikIptal(t *testing.T) {
	o := kurOrtam(t, false, true)
	o.istekOK("PUT", "/destek/ayar", o.bB, map[string]any{"musteri_duzey": "okuma"})
	mA := o.eslestir(o.mA, 101, "musteri_a", "okuma")
	o.onayla(o.mA, mA)
	mA2 := o.eslestir(o.mA2, 102, "musteri_a2", "okuma")
	o.onayla(o.mA2, mA2)
	bA := o.eslestir(o.bA, 103, "bayi_a_tg", "okuma")
	o.onayla(o.bA, bA)
	mB := o.eslestir(o.mB, 104, "musteri_b", "okuma")
	o.onayla(o.mB, mB)
	_, _ = o.db.Exec(`UPDATE users SET totp_enabled=1 WHERE id=1`)
	kk := o.eslestir(o.kok, 105, "kok_tg", "okuma")
	o.onayla(o.kok, kk)

	// 1) FTP parolası değişti (hangi yoldan olursa olsun) → ilk iş emrinde iptal.
	_, _ = o.db.Exec(`UPDATE ftp_accounts SET password_md5='$6$yeni' WHERE id=5`)
	e := o.m.isEmri(mA, "hesap_ozet", `{"hesap":1}`)
	o.m.gonder(e)
	if s := o.m.isSonucu(e.IsKodu); s.HataKodu != p.HataBaglantiIptal {
		t.Fatalf("FTP parola sonrası iş: %+v", s)
	}
	o.m.bekle(p.TipBaglantiIptal, func(g p.Govde) bool {
		i := g.(*p.BaglantiIptal)
		return i.BaglantiID == mA && i.Neden == p.IptalFTPParola
	})
	if d, n := o.durum(mA); d != durumIptal || n != string(p.IptalFTPParola) {
		t.Fatalf("FTP iptal: %s %s", d, n)
	}

	// 2) Bayi askısı tetiği: bayi A'nın kendi ve müşterisinin bağlantıları; bayi B'nin müşterisi kalır.
	if n := iptal.Bayi(context.Background(), o.db, 7, true, 1, "root"); n != 2 {
		t.Fatalf("bayi askısı %d bağlantı iptal etti (beklenen 2)", n)
	}
	for _, k := range []string{mA2, bA} {
		o.m.bekle(p.TipBaglantiIptal, func(g p.Govde) bool { i := g.(*p.BaglantiIptal); return i.BaglantiID == k && i.Neden == p.IptalBayi })
	}
	if d, _ := o.durum(mB); d != durumAktif {
		t.Fatal("başka bayinin müşterisi etkilendi")
	}
	if o.denetimSay(dBaglantiIptal, "root") != 2 {
		t.Fatal("tetik iptali denetime yazılmadı")
	}

	// 3) Hosting askısı (süpürge yolu: askida=1 doğrudan) → alan_silme.
	_, _ = o.db.Exec(`UPDATE domains SET askida=1 WHERE id=43`)
	o.a.SupurgeTur(context.Background())
	if d, n := o.durum(mB); d != durumIptal || n != string(p.IptalAlanSilme) {
		t.Fatalf("askı süpürgesi: %s %s", d, n)
	}

	// 4) 2FA kapatma: kök bağlantısı süpürgede iki_fa ile düşer.
	_, _ = o.db.Exec(`UPDATE users SET totp_enabled=0 WHERE id=1`)
	o.a.SupurgeTur(context.Background())
	if d, n := o.durum(kk); d != durumIptal || n != string(p.IptalIkiFA) {
		t.Fatalf("2FA süpürgesi: %s %s", d, n)
	}

	// 5) Tetik fonksiyonları: domain silme ve 2FA (doğrudan).
	_, _ = o.db.Exec(`UPDATE domains SET askida=0 WHERE id=43`)
	mB2 := o.eslestir(o.mB, 106, "musteri_b", "okuma")
	o.onayla(o.mB, mB2)
	if n := iptal.Domain(context.Background(), o.db, 43, p.IptalAlanSilme, 1, "root"); n != 1 {
		t.Fatalf("domain tetiği %d", n)
	}
	_, _ = o.db.Exec(`UPDATE users SET totp_enabled=1 WHERE id=1`)
	kk2 := o.eslestir(o.kok, 107, "kok_tg", "okuma")
	o.onayla(o.kok, kk2)
	if n := iptal.Kullanici(context.Background(), o.db, 1, p.IptalIkiFA, 1, "root"); n != 1 {
		t.Fatalf("2FA tetiği %d", n)
	}
	// Bayi askısı süpürge yolu (tetik kaçırıldıysa): bayi B askıda → müşterisi düşer.
	mB3 := o.eslestir(o.mB, 108, "musteri_b", "okuma")
	o.onayla(o.mB, mB3)
	_, _ = o.db.Exec(`UPDATE users SET status='suspended' WHERE id=8`)
	o.a.SupurgeTur(context.Background())
	if d, n := o.durum(mB3); d != durumIptal || n != string(p.IptalBayi) {
		t.Fatalf("bayi askısı süpürgesi: %s %s", d, n)
	}
	if o.y.sayi() != 0 {
		t.Fatal("iptal edilen bağlantıda yürütücü çağrıldı")
	}
}

// Düzey min kuralı + süre kuralları.
func TestDuzeyMinVeSure(t *testing.T) {
	o := kurOrtam(t, false, true)
	ctx := context.Background()
	// kök: modül tam, müşteri üst sınırı onarim; bayi A müşterileri için okuma.
	o.istekOK("PUT", "/destek/ayar", o.kok, map[string]any{"modul_duzey": "tam", "bayi_duzey": "onarim", "musteri_duzey": "onarim"})
	o.istekOK("PUT", "/destek/ayar", o.bA, map[string]any{"musteri_duzey": "okuma"})
	cases := []struct {
		rol  p.Rol
		rid  int64
		bag  p.Duzey
		bekl p.Duzey
	}{
		{p.RolKok, 0, p.DuzeyTam, p.DuzeyTam},
		{p.RolBayi, 7, p.DuzeyTam, p.DuzeyOnarim},
		{p.RolMusteri, 7, p.DuzeyTam, p.DuzeyOkuma},  // bayi izni
		{p.RolMusteri, 0, p.DuzeyTam, p.DuzeyOnarim}, // kök müşterisi: kök üst sınırı
		{p.RolMusteri, 8, p.DuzeyTam, p.DuzeyKapali}, // bayi B müşterilerine izin vermedi
		{p.RolMusteri, 0, p.DuzeyOkuma, p.DuzeyOkuma},
	}
	for _, c := range cases {
		if g := EtkinDuzey(ctx, o.db, c.rol, c.rid, c.bag); g != c.bekl {
			t.Errorf("EtkinDuzey(%s, rid=%d, %s) = %s, beklenen %s", c.rol, c.rid, c.bag, g, c.bekl)
		}
	}
	// Bayi B müşterisi: kod üretilemez (tavan kapalı).
	if y := o.istek("POST", "/destek/eslestir", o.mB, map[string]any{"duzey": "okuma", "sure_gun": 7}); y.kod != 403 {
		t.Fatalf("izin verilmeyen müşteri kod üretti: %d", y.kod)
	}
	// Bağlantı "tam" ama bayi izni "okuma": merkeze ve yürütücüye okuma gider.
	kim := o.eslestir(o.mA, 201, "musteri_a", "tam")
	if et := o.onayla(o.mA, kim); et.Duzey != p.DuzeyOkuma {
		t.Fatalf("etkin düzey merkeze %s gitti", et.Duzey)
	}
	e := o.m.isEmri(kim, "hesap_ozet", `{"hesap":1}`)
	o.m.gonder(e)
	o.m.isSonucu(e.IsKodu)
	if c := o.y.son(); c.ob.Duzey != p.DuzeyOkuma || c.kap.Duzey != p.DuzeyOkuma {
		t.Fatalf("yürütücüye giden düzey: %+v", c.ob)
	}
	// Bayi izni kapatılınca etkin düzey kapalı → iş reddedilir.
	o.istekOK("PUT", "/destek/ayar", o.bA, map[string]any{"musteri_duzey": "kapali"})
	e2 := o.m.isEmri(kim, "hesap_ozet", `{"hesap":1}`)
	o.m.gonder(e2)
	if s := o.m.isSonucu(e2.IsKodu); s.HataKodu != p.HataDevreDisi {
		t.Fatalf("bayi izni kapalıyken iş: %+v", s)
	}
	// Süre: süresiz (0) ve > 30 gün kök dahil reddedilir; 30 kabul.
	for _, gun := range []int{0, -1, 31, 3650} {
		if y := o.istek("POST", "/destek/eslestir", o.kok, map[string]any{"duzey": "okuma", "sure_gun": gun}); y.kod != 400 {
			t.Fatalf("kök %d gün seçebildi: %d", gun, y.kod)
		}
	}
	o.istekOK("POST", "/destek/eslestir", o.kok, map[string]any{"duzey": "okuma", "sure_gun": 30})
	// Bağlantının süresi dolunca iş reddedilir ve bağlantı iptal olur.
	kim2 := o.eslestir(o.kok, 202, "kok_tg", "okuma")
	o.onayla(o.kok, kim2)
	_, _ = o.db.Exec(`UPDATE destek_baglantilari SET bitis=NOW() - INTERVAL 1 SECOND WHERE kimlik=?`, kim2)
	e3 := o.m.isEmri(kim2, "hesap_ozet", `{"hesap":1}`)
	o.m.gonder(e3)
	if s := o.m.isSonucu(e3.IsKodu); s.HataKodu != p.HataBaglantiIptal {
		t.Fatalf("süresi dolmuş bağlantı: %+v", s)
	}
	if d, n := o.durum(kim2); d != durumIptal || n != string(p.IptalSuresiDoldu) {
		t.Fatalf("süre iptali: %s %s", d, n)
	}
}

// Hesap listesi değişirse (yeni hosting) eski numarayla iş reddedilir, güncel liste gönderilir.
func TestHesapListesiDegisimi(t *testing.T) {
	o := kurOrtam(t, false, true)
	kim := o.eslestir(o.bA, 301, "bayi_a_tg", "okuma")
	et := o.onayla(o.bA, kim)
	if len(et.Hesaplar) != 2 || et.Hesaplar[0].Etiket != "a.test" || et.Hesaplar[1].Etiket != "a2.test" {
		t.Fatalf("bayi hesapları: %+v", et.Hesaplar)
	}
	_, _ = o.db.Exec(`INSERT INTO domains (id, alan_adi, reseller_id) VALUES (46,'a3.test',7)`)
	e := o.m.isEmri(kim, "hesap_ozet", `{"hesap":2}`)
	o.m.gonder(e)
	if s := o.m.isSonucu(e.IsKodu); s.HataKodu != p.HataKapsam {
		t.Fatalf("liste değişince iş: %+v", s)
	}
	yeni := o.m.bekle(p.TipBaglantiDurum, func(g p.Govde) bool { return g.(*p.BaglantiDurum).BaglantiID == kim }).(*p.BaglantiDurum)
	if len(yeni.Hesaplar) != 3 {
		t.Fatalf("güncel liste: %+v", yeni.Hesaplar)
	}
	e2 := o.m.isEmri(kim, "hesap_ozet", `{"hesap":3}`)
	o.m.gonder(e2)
	if s := o.m.isSonucu(e2.IsKodu); !s.Ok {
		t.Fatalf("güncel listeyle iş: %+v", s)
	}
	if c := o.y.son(); c.kap.Hesaplar[2].DomainID != 46 || c.kap.DomainID != 0 || c.kap.ResellerID != 7 {
		t.Fatalf("bayi kapsamı: %+v", c.kap)
	}
}

// Merkez kaydı reddederse ajan bağlanmış sayılmaz; durum hata gösterir.
func TestKayitReddi(t *testing.T) {
	db := dtGeciciDB(t)
	m := yeniSahteMerkez(t)
	m.kayitRed.Store(p.HataLisans)
	eskiPI := lisansParmakIzi
	lisansParmakIzi = func() string { return strings.Repeat("cd", 32) }
	defer func() { lisansParmakIzi = eskiPI }()
	a := Yeni(db, Yapilandirma{MerkezURL: m.srv.URL, MerkezAnahtarlari: []ed25519.PublicKey{m.cift.Acik},
		CihazAnahtarYolu: t.TempDir() + "/c.key", GuvensizIzin: true})
	_, _ = db.Exec(`INSERT INTO destek_ayar (reseller_id, modul_duzey) VALUES (0,'okuma')`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.Baslat(ctx)
	son := time.Now().Add(5 * time.Second)
	for a.DurumAl().Durum != BagHata {
		if time.Now().After(son) {
			t.Fatalf("red durumu görünmedi: %+v", a.DurumAl())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if a.Bagli() || !strings.Contains(a.DurumAl().SonHata, string(p.HataLisans)) {
		t.Fatalf("durum: %+v", a.DurumAl())
	}
}

// Gerçek araç yürütücüsü (internal/destek/arac) ile sözleşme: ajanın kurduğu yerel
// kimlik (arac.KimlikBagla) ve hesap listesi (arac.HesapListesi) yürütücünün kimlik
// ve kapsam kapılarından geçer — müşteri ve bayi bağlantısı için yetki_yok/kapsam_disi YOK.
func TestGercekYurutucuSozlesmesi(t *testing.T) {
	o := kurOrtam(t, false, true)
	o.a.YurutucuAyarla(arac.Yeni(o.db, o.h, []string{dtKanarya}))
	for _, c := range []struct {
		tok, tg string
		tgID    int64
		param   string
	}{{o.mA, "musteri_a", 401, `{"hesap":1}`}, {o.bA, "bayi_a_tg", 402, `{"hesap":2}`}, {o.kok, "", 403, `{"hesap":3}`}} {
		kim := o.eslestir(c.tok, c.tgID, c.tg, "okuma")
		o.onayla(c.tok, kim)
		e := o.m.isEmri(kim, "hesap_ozet", c.param)
		o.m.gonder(e)
		s := o.m.isSonucu(e.IsKodu)
		if s.HataKodu == p.HataYetki || s.HataKodu == p.HataKapsam || strings.Contains(s.Ozet, "arac_yok") {
			t.Fatalf("%s: gerçek yürütücü yerel kimliği reddetti: %+v", c.tg, s)
		}
		if strings.Contains(s.Ozet+strings.Join(s.Satirlar, ""), dtKanarya) {
			t.Fatal("kanarya sızdı")
		}
	}
}
