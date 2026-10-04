package ajan

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/destek/arac"
	"girginospanel/internal/destek/iptal"
	p "girginospanel/internal/destek/protokol"
)

// Portal eşleştirmesi uçtan uca (WSS ve uzun yoklama): merkez imzalı eslesme_onayla_v2 →
// bağlantı DOĞRUDAN etkin + rol=kök (yerel kod / panel onayı YOK) → iş emri → yürütücü YEREL
// kapsamla → maskeli sonuç → iş kaydı + denetim → panelden iptal.
func TestAkisPortalEslesmeUctanUca(t *testing.T) {
	for _, tur := range []struct {
		ad      string
		yoklama bool
	}{{"wss", false}, {"yoklama", true}} {
		t.Run(tur.ad, func(t *testing.T) {
			o := kurOrtam(t, tur.yoklama, true)
			if d := o.a.DurumAl(); d.Tasima != map[bool]string{false: "wss", true: "yoklama"}[tur.yoklama] {
				t.Fatalf("taşıma: %+v", d)
			}
			e := o.v2(111222333, "musteri_a", "okuma", 7)
			o.m.imzala(e)
			o.m.gonder(e)
			bd := o.m.bekle(p.TipBaglantiDurum, nil).(*p.BaglantiDurum)
			// Doğrudan etkin: 'bekliyor' ara durumu YOK; kök rolü; düzey imzalı mesajdan; tüm hesaplar.
			if bd.Durum != p.BaglantiEtkin || bd.Rol != p.RolKok || bd.Duzey != p.DuzeyOkuma || bd.KodOzet != e.KodOzet || len(bd.Hesaplar) != 4 {
				t.Fatalf("etkin bağlantı: %+v", bd)
			}
			kim := bd.BaglantiID
			var rol, durum, olusturan, onaylayan, ayd string
			var panelUID, sureGun int64
			var bitisVar, onayVar bool
			if err := o.db.QueryRow(`SELECT rol, durum, olusturan, onaylayan, aydinlatma_surum, panel_user_id, sure_gun, bitis IS NOT NULL, onay_at IS NOT NULL
				FROM destek_baglantilari WHERE kimlik=?`, kim).Scan(&rol, &durum, &olusturan, &onaylayan, &ayd, &panelUID, &sureGun, &bitisVar, &onayVar); err != nil {
				t.Fatal(err)
			}
			if rol != "kok" || durum != durumAktif || olusturan != "portal:u_42" || onaylayan != "portal:u_42" || ayd != "2026-10-04.1" || panelUID != 1 || sureGun != 7 || !bitisVar || !onayVar {
				t.Fatalf("DB satırı: %s %s %s %s %s uid=%d gün=%d bitis=%v onay=%v", rol, durum, olusturan, onaylayan, ayd, panelUID, sureGun, bitisVar, onayVar)
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
			if !c.ok || c.kap.Aktor != "destek-bot (tg:@musteri_a)" || c.ob.Rol != p.RolKok || c.ob.Duzey != p.DuzeyOkuma || len(c.ob.Hesaplar) != 4 {
				t.Fatalf("yürütücü bağlamı: %+v", c)
			}
			// Düzey okuma: R1/R2 araç reddedilir; hesap sırası dışı → kapsam_disi. Yürütücü çağrılmaz.
			n := o.y.sayi()
			e3 := o.m.isEmri(kim, "hesap_ozet", `{"hesap":9}`)
			o.m.gonder(e3)
			if s := o.m.isSonucu(e3.IsKodu); s.Ok || s.HataKodu != p.HataKapsam {
				t.Fatalf("kapsam dışı: %+v", s)
			}
			if o.y.sayi() != n {
				t.Fatal("reddedilen iş yürütücüye ulaştı")
			}
			if o.denetimSay(dEslesmePortal, "destek-bot (tg:@musteri_a)") != 1 || o.denetimSay(dIsRed, "destek-bot") != 1 {
				t.Fatal("denetim kayıtları eksik")
			}
			o.a.R0Ozet(context.Background())
			if o.denetimSay(dR0Ozet, "destek-bot (tg:@musteri_a)") != 1 {
				t.Fatal("R0 özeti yazılmadı")
			}

			// Sahibi iptal etti (panelde HTTP ucu YOK; iptal DB'ye yazılır) → ajan DB yoklamasıyla
			// görüp merkeze bildirir; iptal sonrası iş emri reddedilir.
			if !iptal.Tek(context.Background(), o.db, mustID(t, o, kim), p.IptalKullanici, "root") {
				t.Fatal("iptal yazılamadı")
			}
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

// Eski (bayi/müşteri) kayıtlar: eşleştirme KALDIRILDI ama ajan/arac kapsam davranışı bu kayıtlar
// için de bozulmamalı (müşteri A↛B, kök aracı yasak, kapsam dışı hesap). Satırlar DB'ye doğrudan yazılır.
func TestEskiKayitKapsami(t *testing.T) {
	o := kurOrtam(t, false, true)
	kim := o.eslestir(o.mA, 111222333, "musteri_a", "okuma")
	o.m.gonder(o.m.isEmri(kim, "hesap_ozet", `{"hesap":1}`)) // onaysız: panel_onayi_bekliyor
	e1 := o.m.isEmri(kim, "hesap_ozet", `{"hesap":1}`)
	o.m.gonder(e1)
	if s := o.m.isSonucu(e1.IsKodu); s.Ok || s.HataKodu != p.HataOnayBekliyor {
		t.Fatalf("onaysız iş: %+v", s)
	}
	et := o.onayla(o.mA, kim)
	if et.Duzey != p.DuzeyOkuma || len(et.Hesaplar) != 1 || et.Hesaplar[0] != (p.HesapEtiketi{Sira: 1, Etiket: "a.test"}) {
		t.Fatalf("etkin bağlantı: %+v", et)
	}
	e2 := o.m.isEmri(kim, "hesap_ozet", `{"hesap":1}`)
	o.m.gonder(e2)
	if s := o.m.isSonucu(e2.IsKodu); !s.Ok {
		t.Fatalf("iş başarısız: %+v", s)
	}
	c := o.y.son()
	if !c.ok || c.kap.DomainID != 42 || c.kap.FTPHesapID != 5 || c.kap.ResellerID != 7 || c.ob.Rol != p.RolMusteri {
		t.Fatalf("yürütücü bağlamı: %+v", c)
	}
	n := o.y.sayi()
	e3 := o.m.isEmri(kim, "hesap_ozet", `{"hesap":2}`)
	o.m.gonder(e3)
	if s := o.m.isSonucu(e3.IsKodu); s.Ok || s.HataKodu != p.HataKapsam {
		t.Fatalf("kapsam dışı: %+v", s)
	}
	e4 := o.m.isEmri(kim, "sunucu_log_oku", `{"kaynak":"nginx_error"}`)
	o.m.gonder(e4)
	if s := o.m.isSonucu(e4.IsKodu); s.Ok || (s.HataKodu != p.HataYetki && s.HataKodu != p.HataParam) {
		t.Fatalf("müşteri kök aracı: %+v", s)
	}
	if o.y.sayi() != n {
		t.Fatal("reddedilen iş yürütücüye ulaştı")
	}
}

// Eşleştirme v2 reddi: imzası bozulan HER alan (düzey, süre, portal kullanıcı, aydınlatma, Telegram kimliği),
// yanlış hedef sunucu, tekrar, kullanılmış kod, yabancı anahtar, eski mesaj, süresi dolmuş, aktif sınır.
func TestEslesmeV2Redleri(t *testing.T) {
	o := kurOrtam(t, false, true)
	say := func() int {
		var n int
		_ = o.db.QueryRow(`SELECT COUNT(*) FROM destek_baglantilari`).Scan(&n)
		return n
	}
	// 1) imzadan sonra değiştirilen her alan → imza hatası, kayıt açılmaz
	for ad, f := range map[string]func(e *p.EslesmeOnayla){
		"tg_user_id":       func(e *p.EslesmeOnayla) { e.TgUserID = 666 },
		"duzey":            func(e *p.EslesmeOnayla) { e.Duzey = p.DuzeyTam },
		"sure_gun":         func(e *p.EslesmeOnayla) { e.SureGun = 30 },
		"portal_kullanici": func(e *p.EslesmeOnayla) { e.PortalKullanici = "u_99" },
		"aydinlatma_surum": func(e *p.EslesmeOnayla) { e.AydinlatmaSurum = "baska" },
		"sunucu_kimligi":   func(e *p.EslesmeOnayla) { e.SunucuKimligi = "sunucu-baska-01" },
		"kod_ozet":         func(e *p.EslesmeOnayla) { e.KodOzet = strings.Repeat("0", 64) },
	} {
		e := o.v2(7, "musteri_a", "okuma", 1)
		o.m.imzala(e)
		f(e)
		zk, _ := o.m.gonder(e)
		if h := o.m.hata(zk); h.Kod != p.HataImza {
			t.Fatalf("%s değişikliği imzayla yakalanmadı: %+v", ad, h)
		}
	}
	// 2) doğru imzalı ama başka sunucuya yönlendirilmiş onay
	yanlis := o.v2(7, "musteri_a", "okuma", 1)
	yanlis.SunucuKimligi = "sunucu-baska-01"
	o.m.imzala(yanlis)
	zk, _ := o.m.gonder(yanlis)
	if h := o.m.hata(zk); h.Kod != p.HataYetki {
		t.Fatalf("başka sunucu için onay: %+v", h)
	}
	if say() != 0 {
		t.Fatalf("reddedilen onaylar %d kayıt açtı", say())
	}
	// 3) yabancı anahtar, eski, birebir tekrar, kullanılmış kod
	yabanci, _ := p.AnahtarCiftiUret(nil)
	ye := o.v2(7, "musteri_a", "okuma", 1)
	_ = p.YeniImzalayici(yabanci).Imzala(ye, 30*time.Second)
	zk, _ = o.m.gonder(ye)
	if h := o.m.hata(zk); h.Kod != p.HataAnahtarBilinmiyor {
		t.Fatalf("yabancı anahtar: %+v", h)
	}
	o.m.imz.Saat = func() time.Time { return time.Now().Add(-2 * time.Minute) }
	eski := o.v2(7, "musteri_a", "okuma", 1)
	o.m.imzala(eski)
	o.m.imz.Saat = time.Now
	zk, _ = o.m.gonder(eski)
	if h := o.m.hata(zk); h.Kod != p.HataSuresiDoldu {
		t.Fatalf("eski onay: %+v", h)
	}
	iyi := o.v2(7, "musteri_a", "okuma", 1)
	o.m.imzala(iyi)
	zk, ham := o.m.gonder(iyi)
	_ = zk
	o.m.bekle(p.TipBaglantiDurum, func(g p.Govde) bool { return g.(*p.BaglantiDurum).KodOzet == iyi.KodOzet })
	o.m.gonderHam(ham)
	if h := o.m.hata(zk); h.Kod != p.HataTekrar {
		t.Fatalf("birebir tekrar: %+v", h)
	}
	kullanilmis := o.v2(8, "musteri_b", "okuma", 1)
	kullanilmis.KodOzet = iyi.KodOzet // aynı kod, yeni imza, başka Telegram kullanıcısı
	o.m.imzala(kullanilmis)
	zk, _ = o.m.gonder(kullanilmis)
	if h := o.m.hata(zk); h.Kod != p.HataEslesmeKodu {
		t.Fatalf("kullanılmış kod: %+v", h)
	}
	if say() != 1 {
		t.Fatalf("bağlantı sayısı %d (beklenen 1)", say())
	}
	// 4) eski mesaj tipi artık tanınmaz
	o.m.gonderHam([]byte(`{"surum":1,"tip":"eslesme_onayla","kimlik":"zarf_eski_0001","govde":{"kod_ozet":"` + strings.Repeat("a", 64) + `","tg_user_id":5}}`))
	o.m.bekle(p.TipHata, func(g p.Govde) bool { return g.(*p.HataMesaji).Kod == p.HataTipBilinmiyor })
	// 5) sunucu başına aktif bağlantı sınırı
	for i := say(); i < aktifBaglantiSiniri; i++ {
		o.kokEslestir(int64(1000+i), "", "okuma")
	}
	fazla := o.v2(9999, "", "okuma", 1)
	o.m.imzala(fazla)
	zk, _ = o.m.gonder(fazla)
	if h := o.m.hata(zk); h.Kod != p.HataKota {
		t.Fatalf("aktif sınır: %+v", h)
	}
	if say() != aktifBaglantiSiniri {
		t.Fatalf("bağlantı sayısı %d", say())
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

// Yerel acil kapatma dosyası varken gelen iş emri (kapanma anındaki yarış dahil) destek_kapali
// ile reddedilir ve yürütücüye ulaşmaz. (Bağlantı e2e'si: kapatma_db_test.go.)
func TestYerelKapaliIsEmriReddi(t *testing.T) {
	o := kurOrtam(t, false, true)
	kim := o.eslestir(o.kok, 1, "kok_kullanici", "okuma")
	emir := o.m.isEmri(kim, "hesap_ozet", `{"hesap":1}`)
	if err := os.WriteFile(o.kapatma, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if s := o.a.isYurut(context.Background(), emir); s.HataKodu != p.HataDevreDisi {
		t.Fatalf("kapalıyken iş: %+v", s)
	}
	// Kapatma, bağlantı kaydına bakılmadan ÖNCE denetlenir.
	if s := o.a.isYurut(context.Background(), o.m.isEmri("YOKyokYOKyokYOKyokYOKy", "hesap_ozet", `{"hesap":1}`)); s.HataKodu != p.HataDevreDisi {
		t.Fatalf("kapalıyken olmayan bağlantı: %+v", s)
	}
	if o.y.sayi() != 0 {
		t.Fatal("kapalıyken yürütücü çağrıldı")
	}
}

// Otomatik iptal: FTP parola değişimi (iz), hosting askısı (tetik), bayi askısı (tetik:
// bayi + müşterileri), bayi durumu (süpürge), 2FA kapatma (tetik + süpürge).
func TestOtomatikIptal(t *testing.T) {
	o := kurOrtam(t, false, true)
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

// Etkin düzey = portalda seçilip merkezin İMZALADIĞI düzey (şalter/tavan YOK) + süre kuralları.
func TestImzaliDuzeyVeSure(t *testing.T) {
	o := kurOrtam(t, false, true)
	// Birim: bağlantı düzeyi olduğu gibi geçer; geçersiz/bilinmeyen KAPALI sayılır.
	for _, c := range []struct{ bag, bekl p.Duzey }{
		{p.DuzeyTam, p.DuzeyTam}, {p.DuzeyOnarim, p.DuzeyOnarim}, {p.DuzeyOkuma, p.DuzeyOkuma},
		{p.DuzeyKapali, p.DuzeyKapali}, {"root", p.DuzeyKapali}, {"", p.DuzeyKapali},
	} {
		if g := EtkinDuzey(c.bag); g != c.bekl {
			t.Errorf("EtkinDuzey(%q) = %q, beklenen %q", c.bag, g, c.bekl)
		}
	}
	// Uçtan uca: destek_ayar satırı YOK; merkeze ve yürütücüye giden düzey imzalı düzeyin kendisi.
	var nAyar int
	_ = o.db.QueryRow(`SELECT COUNT(*) FROM destek_ayar`).Scan(&nAyar)
	if nAyar != 0 {
		t.Fatalf("ön koşul: destek_ayar boş olmalı (%d satır)", nAyar)
	}
	for i, d := range []p.Duzey{p.DuzeyOkuma, p.DuzeyOnarim, p.DuzeyTam} {
		kim := o.eslestir(o.kok, 200+int64(i), "kok_"+string(d), string(d))
		if et := o.onayla(o.kok, kim); et.Duzey != d {
			t.Fatalf("imzalı %s → merkeze %s gitti", d, et.Duzey)
		}
		e := o.m.isEmri(kim, "hesap_ozet", `{"hesap":1}`)
		o.m.gonder(e)
		if s := o.m.isSonucu(e.IsKodu); !s.Ok {
			t.Fatalf("%s düzeyinde R0 araç: %+v", d, s)
		}
		if c := o.y.son(); c.ob.Duzey != d || c.kap.Duzey != d {
			t.Fatalf("yürütücüye giden düzey %s/%s, beklenen %s", c.ob.Duzey, c.kap.Duzey, d)
		}
	}
	// Düzeyin ÜSTÜ: 'okuma' bağlantısında katalogla uyuşmayan yüksek risk (R1) → reddedilir,
	// yürütücüye ulaşmaz; düzey arttırılamaz.
	kimOku := o.eslestir(o.kok, 250, "kok_okuma2", "okuma")
	o.onayla(o.kok, kimOku)
	n := o.y.sayi()
	ust := o.m.isEmri(kimOku, "hesap_ozet", `{"hesap":1}`)
	ust.Risk = p.R1
	o.m.imzala(ust) // imza yeni riskle yeniden atılır (imza reddi değil, düzey/katalog reddi sınanır)
	o.m.gonder(ust)
	if s := o.m.isSonucu(ust.IsKodu); s.Ok || s.HataKodu != p.HataParam {
		t.Fatalf("üst risk: %+v", s)
	}
	if o.y.sayi() != n {
		t.Fatal("düzey üstü iş yürütücüye ulaştı")
	}
	// Süre: protokol 0 ve > 30 gün bağlantıyı temsil ETMEZ (imzalama/biçim aşamasında reddedilir).
	for _, gun := range []int{0, -1, 31, 3650} {
		e := o.v2(1, "", "okuma", gun)
		if err := o.m.imz.Imzala(e, 30*time.Second); err == nil {
			t.Fatalf("%d gün imzalanabildi", gun)
		}
	}
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
	a.KapatmaDosyasi = t.TempDir() + "/destek-kapali"
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

// Merkez taklitçisi (Cloudflare arkasında TLS merkezi kanıtlamaz): meydan okumayı
// sabitlenmiş anahtar dışında bir anahtarla imzalayan "merkeze" cihaz imzası VERİLMEZ.
func TestSahteMerkezeCihazImzasiYok(t *testing.T) {
	db := dtGeciciDB(t)
	m := yeniSahteMerkez(t)
	sahte, _ := p.AnahtarCiftiUret(nil)
	m.sahteci = p.YeniImzalayici(sahte)
	eskiPI := lisansParmakIzi
	lisansParmakIzi = func() string { return strings.Repeat("ef", 32) }
	defer func() { lisansParmakIzi = eskiPI }()
	a := Yeni(db, Yapilandirma{MerkezURL: m.srv.URL, MerkezAnahtarlari: []ed25519.PublicKey{m.cift.Acik},
		CihazAnahtarYolu: t.TempDir() + "/c.key", GuvensizIzin: true})
	a.KapatmaDosyasi = t.TempDir() + "/destek-kapali"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.Baslat(ctx)
	son := time.Now().Add(5 * time.Second)
	for m.baglanti.Load() == 0 || a.DurumAl().Durum != BagHata {
		if time.Now().After(son) {
			t.Fatalf("durum: %+v", a.DurumAl())
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if m.yanitSay.Load() != 0 || a.Bagli() {
		t.Fatal("taklitçi merkeze meydan yanıtı (cihaz imzası) verildi")
	}
}

// Uzun ömürlü oturum lisans biletini tazeler: belirli aralıkla oturum kapatılıp yeniden kurulur
// (kayıtta diskteki güncel bilet gider); bağlantı kendiliğinden geri gelir.
func TestBiletTazelemesiIcinYenidenBaglanir(t *testing.T) {
	db := dtGeciciDB(t)
	m := yeniSahteMerkez(t)
	a := Yeni(db, Yapilandirma{MerkezURL: m.srv.URL, MerkezAnahtarlari: []ed25519.PublicKey{m.cift.Acik},
		CihazAnahtarYolu: t.TempDir() + "/c.key", GuvensizIzin: true})
	a.BiletYenilemeAraligi = 300 * time.Millisecond
	a.KapatmaDosyasi = t.TempDir() + "/destek-kapali"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.Baslat(ctx)
	son := time.Now().Add(15 * time.Second)
	for m.baglanti.Load() < 2 {
		if time.Now().After(son) {
			t.Fatalf("oturum yeniden kurulmadı (bağlantı %d): %+v", m.baglanti.Load(), a.DurumAl())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
