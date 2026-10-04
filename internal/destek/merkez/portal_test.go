package merkez

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/destek/bilet"
	"girginospanel/internal/destek/protokol"
)

var tFP = strings.Repeat("ab", 32)

func kodGovde(fp string) map[string]any {
	return map[string]any{"parmak_izi": fp, "duzey": "okuma", "sure_gun": 3, "portal_kullanici_id": "u_42", "aydinlatma_surum": "2026-10-04.1"}
}

// Bearer: yok / yanlış / farklı uzunlukta / "Basic" → 401; her uç korunur; sır tanımsızsa 503.
func TestPortalBearerYetkisi(t *testing.T) {
	o := yeniOrtam(t, nil)
	ajan := sahteAjanBagla(t, o, nil)
	uclar := []struct {
		yontem, yol string
		govde       any
	}{
		{"GET", "/ic/v1/sunucu?parmak_izi=" + tFP, nil},
		{"POST", "/ic/v1/eslesme-kodu", kodGovde(ajan.kayit.LisansParmakIzi)},
		{"GET", "/ic/v1/baglantilar?parmak_izi=" + tFP, nil},
		{"POST", "/ic/v1/baglanti/iptal", map[string]any{"parmak_izi": tFP, "baglanti_id": "bag_00000001"}},
	}
	for _, u := range uclar {
		for _, sir := range []string{"", "yanlis-sir", tPortalIc[:len(tPortalIc)-1], tPortalIc + "x"} {
			if d, m := o.portal(t, u.yontem, u.yol, u.govde, sir); d != http.StatusUnauthorized || m["hata"] != "yetkisiz" {
				t.Fatalf("%s %s sir=%q: %d %v", u.yontem, u.yol, sir, d, m)
			}
		}
		// Bearer dışı şema
		req, _ := http.NewRequest(u.yontem, o.ic.URL+u.yol, bytes.NewReader([]byte("{}")))
		req.Header.Set("Authorization", "Basic "+tPortalIc)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("Basic şema: %d", resp.StatusCode)
		}
	}
	if o.m.sayacAl("ic_yetkisiz") == 0 {
		t.Fatal("yetkisiz denemeler sayılmadı")
	}
	if d, _ := o.portal(t, "GET", "/ic/v1/sunucu?parmak_izi="+tFP, nil, tPortalIc); d != http.StatusOK {
		t.Fatalf("doğru sır: %d", d)
	}
	// sır yapılandırılmamışsa uçlar tümüyle kapalı (hatta doğru "sır" boş olsa bile)
	o.m.sir.PortalIcAnahtar = ""
	for _, sir := range []string{"", tPortalIc} {
		if d, m := o.portal(t, "GET", "/ic/v1/sunucu?parmak_izi="+tFP, nil, sir); d != http.StatusServiceUnavailable || m["hata"] != "ic_api_kapali" {
			t.Fatalf("sırsız kurulum: %d %v", d, m)
		}
	}
	// sağlık ucu portal sırrına bağlı değil (iç dinleyici yalnız loopback)
	o.m.sir.PortalIcAnahtar = tPortalIc
}

func TestPortalSunucuDurumu(t *testing.T) {
	o := yeniOrtam(t, nil)
	if d, m := o.portal(t, "GET", "/ic/v1/sunucu?parmak_izi=bozuk", nil, tPortalIc); d != http.StatusBadRequest || m["hata"] != "gecersiz_istek" {
		t.Fatalf("bozuk parmak izi: %d %v", d, m)
	}
	d, m := o.portal(t, "GET", "/ic/v1/sunucu?parmak_izi="+tFP, nil, tPortalIc)
	if d != 200 || m["bulundu"] != false || m["cevrimici"] != false || m["dogrulandi"] != false {
		t.Fatalf("bilinmeyen sunucu: %d %v", d, m)
	}
	ajan := sahteAjanBagla(t, o, nil)
	d, m = o.portal(t, "GET", "/ic/v1/sunucu?parmak_izi="+tFP, nil, tPortalIc)
	if d != 200 || m["bulundu"] != true || m["cevrimici"] != true || m["dogrulandi"] != true || m["sunucu_kimligi"] != ajan.Sunucu || m["panel_surumu"] != "0.3.0-test" || m["son_gorulme"].(float64) <= 0 {
		t.Fatalf("doğrulanmış çevrimiçi sunucu: %d %v", d, m)
	}
	ajan.ws.c.Close()
	bekle(t, func() bool { return !o.m.ajan.Cevrimici(ajan.Sunucu) })
	_, m = o.portal(t, "GET", "/ic/v1/sunucu?parmak_izi="+tFP, nil, tPortalIc)
	if m["bulundu"] != true || m["cevrimici"] != false || m["dogrulandi"] != true {
		t.Fatalf("çevrimdışı sunucu: %v", m)
	}
}

// Anti-spoof: bilet doğrulanmayan sunucu (yok / süresi dolmuş / başka parmak izi / yanlış anahtar /
// askıda / kurcalanmış) KOD ALAMAZ; doğrulanmış olan alır.
func TestPortalDogrulanmamisSunucuKodAlamaz(t *testing.T) {
	o := yeniOrtam(t, nil)
	var bozuk = map[string]func(fp string) []byte{
		"bilet yok": func(string) []byte { return []byte("bilet-yok") },
		"süresi dolmuş": func(fp string) []byte {
			return o.bilet(fp, func(g *bilet.Govde) { g.Bitis = time.Now().Add(-time.Hour).Unix() })
		},
		"başka fp": func(string) []byte { return o.bilet(strings.Repeat("cd", 32), nil) },
		"askıda":   func(fp string) []byte { return o.bilet(fp, func(g *bilet.Govde) { g.Durum = bilet.DurumAskida }) },
		"askı anı geçmiş": func(fp string) []byte {
			return o.bilet(fp, func(g *bilet.Govde) { g.Askiya = time.Now().Add(-time.Minute).Unix() })
		},
		"yanlış anahtar": func(fp string) []byte {
			y := yeniOrtam(t, nil)
			return y.bilet(fp, nil)
		},
		"kurcalanmış": func(fp string) []byte {
			b := string(o.bilet(fp, nil))
			return []byte("A" + b[1:])
		},
	}
	i := 0
	for ad, ispat := range bozuk {
		i++
		fp := strings.Repeat(string(rune('0'+i)), 64)
		sahteAjanBaglaFPIspat(t, o, fp, ispat(fp))
		d, m := o.portal(t, "GET", "/ic/v1/sunucu?parmak_izi="+fp, nil, tPortalIc)
		if d != 200 || m["bulundu"] != true || m["dogrulandi"] != false {
			t.Fatalf("%s: doğrulanmış göründü: %v", ad, m)
		}
		if d, m := o.portal(t, "POST", "/ic/v1/eslesme-kodu", kodGovde(fp), tPortalIc); d != http.StatusForbidden || m["hata"] != "dogrulanmadi" {
			t.Fatalf("%s: kod verildi: %d %v", ad, d, m)
		}
	}
	if bekleyen, _ := o.m.ajan.bekleyenSayilari(); bekleyen != 0 {
		t.Fatalf("doğrulanmamış sunuculara %d kod bırakıldı", bekleyen)
	}
	// doğrulanmış sunucu kod alır
	iyi := strings.Repeat("ef", 32)
	sahteAjanBaglaFP(t, o, nil, iyi, true)
	if d, m := o.portal(t, "POST", "/ic/v1/eslesme-kodu", kodGovde(iyi), tPortalIc); d != 200 || m["kod"] == "" {
		t.Fatalf("doğrulanmış sunucu: %d %v", d, m)
	}
}

// sahteAjanBaglaFPIspat: ham ispat baytlarıyla bağlanır (bozuk bilet testleri).
func sahteAjanBaglaFPIspat(t *testing.T, o *ortam, fp string, ispat []byte) *sahteAjan {
	t.Helper()
	return sahteAjanBaglaOzel(t, o, nil, fp, ispat)
}

func TestPortalKodSozlesmesi(t *testing.T) {
	o := yeniOrtam(t, nil)
	if d, m := o.portal(t, "POST", "/ic/v1/eslesme-kodu", kodGovde(tFP), tPortalIc); d != http.StatusNotFound || m["hata"] != "sunucu_yok" {
		t.Fatalf("bilinmeyen sunucu: %d %v", d, m)
	}
	ajan := sahteAjanBagla(t, o, nil)
	gecersiz := map[string]func(m map[string]any){
		"duzey kapali":   func(m map[string]any) { m["duzey"] = "kapali" },
		"duzey bos":      func(m map[string]any) { m["duzey"] = "" },
		"sure 0":         func(m map[string]any) { m["sure_gun"] = 0 },
		"sure 31":        func(m map[string]any) { m["sure_gun"] = 31 },
		"kisa fp":        func(m map[string]any) { m["parmak_izi"] = "ab" },
		"büyük harf fp":  func(m map[string]any) { m["parmak_izi"] = strings.ToUpper(tFP) },
		"portal bos":     func(m map[string]any) { m["portal_kullanici_id"] = "" },
		"portal 65":      func(m map[string]any) { m["portal_kullanici_id"] = strings.Repeat("a", 65) },
		"portal bosluk":  func(m map[string]any) { m["portal_kullanici_id"] = "a b" },
		"aydinlatma bos": func(m map[string]any) { m["aydinlatma_surum"] = "" },
		"bilinmeyen":     func(m map[string]any) { m["ekstra"] = 1 },
	}
	for ad, f := range gecersiz {
		g := kodGovde(ajan.kayit.LisansParmakIzi)
		f(g)
		if d, m := o.portal(t, "POST", "/ic/v1/eslesme-kodu", g, tPortalIc); d != http.StatusBadRequest || m["hata"] != "gecersiz_istek" {
			t.Fatalf("%s: %d %v", ad, d, m)
		}
	}
	// büyük gövde 413
	buyuk := kodGovde(ajan.kayit.LisansParmakIzi)
	buyuk["portal_kullanici_id"] = strings.Repeat("a", 5000)
	if d, m := o.portal(t, "POST", "/ic/v1/eslesme-kodu", buyuk, tPortalIc); d != http.StatusRequestEntityTooLarge || m["hata"] != "govde_buyuk" {
		t.Fatalf("büyük gövde: %d %v", d, m)
	}
	// başarı: sözleşme alanları
	d, m := o.portal(t, "POST", "/ic/v1/eslesme-kodu", kodGovde(ajan.kayit.LisansParmakIzi), tPortalIc)
	if d != 200 {
		t.Fatalf("başarı: %d %v", d, m)
	}
	kod, _ := m["kod"].(string)
	if n, ok := protokol.EslesmeKoduNormal(kod); !ok || n != kod {
		t.Fatalf("kod biçimi (EslesmeKoduUret alfabesi/uzunluğu): %q", kod)
	}
	omur := time.Until(time.Unix(int64(m["bitis"].(float64)), 0))
	if omur <= 0 || omur > 15*time.Minute {
		t.Fatalf("kod ömrü %v (≤15 dk olmalı)", omur)
	}
	if m["bot_url"] != "" { // testYap'ta bot_kullanici_adi yok
		t.Fatalf("bot_url: %v", m["bot_url"])
	}
	// bot adı varsa bağlantı kod içerir
	o.m.yap.BotKullaniciAdi = "GirginOSDestekBot"
	_, m = o.portal(t, "POST", "/ic/v1/eslesme-kodu", kodGovde(ajan.kayit.LisansParmakIzi), tPortalIc)
	if u, _ := m["bot_url"].(string); !strings.HasPrefix(u, "https://t.me/GirginOSDestekBot?start=") || !strings.HasSuffix(u, m["kod"].(string)) {
		t.Fatalf("bot_url: %v", m)
	}
	// sunucu başına bekleyen kod sınırı (3): 4. istek 429 kota
	d, m = o.portal(t, "POST", "/ic/v1/eslesme-kodu", kodGovde(ajan.kayit.LisansParmakIzi), tPortalIc)
	if d != 200 {
		t.Fatalf("3. kod: %d %v", d, m)
	}
	if d, m = o.portal(t, "POST", "/ic/v1/eslesme-kodu", kodGovde(ajan.kayit.LisansParmakIzi), tPortalIc); d != http.StatusTooManyRequests || m["hata"] != "kota" {
		t.Fatalf("4. kod: %d %v", d, m)
	}
	// çevrimdışı sunucu: kod yok
	ajan.ws.c.Close()
	bekle(t, func() bool { return !o.m.ajan.Cevrimici(ajan.Sunucu) })
	if d, m = o.portal(t, "POST", "/ic/v1/eslesme-kodu", kodGovde(ajan.kayit.LisansParmakIzi), tPortalIc); d != http.StatusConflict || m["hata"] != "cevrimdisi" {
		t.Fatalf("çevrimdışı: %d %v", d, m)
	}
}

// Aynı parmak izini iki DOĞRULANMIŞ cihaz aynı anda çevrimiçi iddia ederse kod verilmez.
func TestPortalParmakIziCakismasi(t *testing.T) {
	o := yeniOrtam(t, nil)
	sahteAjanBagla(t, o, nil)
	sahteAjanBagla(t, o, nil) // farklı cihaz anahtarı, aynı parmak izi, geçerli bilet
	d, m := o.portal(t, "POST", "/ic/v1/eslesme-kodu", kodGovde(tFP), tPortalIc)
	if d != http.StatusConflict || m["hata"] != "parmak_izi_cakisma" {
		t.Fatalf("çakışma: %d %v", d, m)
	}
	if _, m = o.portal(t, "GET", "/ic/v1/sunucu?parmak_izi="+tFP, nil, tPortalIc); m["cakisma"] != true {
		t.Fatalf("sunucu çakışma bayrağı: %v", m)
	}
}

// Doğrulanmamış bir cihaz doğrulanmışın yerine GEÇEMEZ (parmak izini sahiplenemez).
func TestPortalDogrulanmamisCihazSahiplenemez(t *testing.T) {
	o := yeniOrtam(t, nil)
	gercek := sahteAjanBagla(t, o, nil)
	sahteAjanBaglaFPIspat(t, o, tFP, []byte("bilet-yok")) // sahte: aynı fp, biletsiz, çevrimiçi
	d, m := o.portal(t, "POST", "/ic/v1/eslesme-kodu", kodGovde(tFP), tPortalIc)
	if d != 200 {
		t.Fatalf("gerçek cihaz varken kod: %d %v", d, m)
	}
	if m["sunucu_kimligi"] != gercek.Sunucu {
		t.Fatalf("kod sahte cihaza bağlandı: %v (gerçek %s)", m["sunucu_kimligi"], gercek.Sunucu)
	}
	// Gerçek cihaz çevrimdışıyken sahte (daha yeni görülen, biletsiz) onun yerine GEÇMEZ:
	// sunucu hâlâ gerçek cihazdır (doğrulanmış, çevrimdışı) ve kod verilmez.
	gercek.ws.c.Close()
	bekle(t, func() bool { return !o.m.ajan.Cevrimici(gercek.Sunucu) })
	if _, m = o.portal(t, "GET", "/ic/v1/sunucu?parmak_izi="+tFP, nil, tPortalIc); m["sunucu_kimligi"] != gercek.Sunucu || m["dogrulandi"] != true || m["cevrimici"] != false {
		t.Fatalf("sahte cihaz doğrulanmışın yerine geçti: %v", m)
	}
	if d, m := o.portal(t, "POST", "/ic/v1/eslesme-kodu", kodGovde(tFP), tPortalIc); d != http.StatusConflict || m["hata"] != "cevrimdisi" {
		t.Fatalf("sahte cihaza kod: %d %v", d, m)
	}
}

// Kod ömrü: süresi dolmuş kod geçersiz; bilet doğrulaması düşen sunucunun kodu /start'ta yanar.
func TestPortalKodSuresiVeDogrulamaDususu(t *testing.T) {
	o := yeniOrtam(t, nil)
	ajan := sahteAjanBagla(t, o, nil)
	simdi := time.Now()
	kod, _ := o.kodAl(t, tFP, "okuma", 1)
	o.m.saat = func() time.Time { return simdi.Add(11 * time.Minute) }
	o.mesaj(t, tTg, "/start "+kod)
	if !strings.Contains(o.tg.son(tTg), "geçersiz") {
		t.Fatalf("süresi dolan kod: %q", o.tg.son(tTg))
	}
	ajan.mu.Lock()
	n := len(ajan.eslesmeler)
	ajan.mu.Unlock()
	if n != 0 {
		t.Fatal("süresi dolan kod ajana gitti")
	}
	// bilet bitişi (24 sa) geçince: sunucu doğrulanmamış, taze kod da /start'ta reddedilir
	o.m.saat = func() time.Time { return simdi }
	kod2, _ := o.kodAl(t, tFP, "okuma", 1)
	o.m.saat = func() time.Time { return simdi.Add(25 * time.Hour) }
	if _, m := o.portal(t, "GET", "/ic/v1/sunucu?parmak_izi="+tFP, nil, tPortalIc); m["dogrulandi"] != false {
		t.Fatalf("bilet bitince hâlâ doğrulanmış: %v", m)
	}
	if d, m := o.portal(t, "POST", "/ic/v1/eslesme-kodu", kodGovde(tFP), tPortalIc); d != http.StatusForbidden || m["hata"] != "dogrulanmadi" {
		t.Fatalf("bilet bitince kod: %d %v", d, m)
	}
	o.mesaj(t, tTg, "/start "+kod2)
	if !strings.Contains(o.tg.son(tTg), "geçersiz") && !strings.Contains(o.tg.son(tTg), "doğrulanamadığı") {
		t.Fatalf("doğrulaması düşen sunucuda /start: %q", o.tg.son(tTg))
	}
	ajan.mu.Lock()
	n = len(ajan.eslesmeler)
	ajan.mu.Unlock()
	if n != 0 {
		t.Fatal("doğrulaması düşen sunucuya onay gitti")
	}
}

// Yeniden kayıtta bilet YENİDEN denetlenir: aynı cihaz önce geçerli, sonra bozuk biletle gelirse
// doğrulama düşer.
func TestBiletHerKayittaYenidenDenetlenir(t *testing.T) {
	o := yeniOrtam(t, nil)
	cift, _ := protokol.AnahtarCiftiUret(nil)
	kayit := func(ispat []byte) {
		kay := yeniKayit(t, cift)
		kay.LisansParmakIzi = tFP
		kay.LisansIspati = base64.RawURLEncoding.EncodeToString(ispat)
		md, err := o.m.imza.MeydanOlustur(kay)
		if err != nil {
			t.Fatal(err)
		}
		y, err := protokol.MeydanYanitla(cift.Ozel, md, kay)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := o.m.ajan.kimlikTamamla(kay, md, y); err != nil {
			t.Fatal(err)
		}
	}
	sunucu, _ := sunucuKimligi(protokol.AcikAnahtarKodla(cift.Acik))
	kayit(o.bilet(tFP, nil))
	if c, _ := o.depo.CihazAl(sunucu); !c.Dogrulandi(time.Now()) || c.DogrulamaNeden != "" {
		t.Fatalf("geçerli bilet: %+v", c)
	}
	kayit([]byte("bilet-yok"))
	if c, _ := o.depo.CihazAl(sunucu); c.Dogrulandi(time.Now()) || c.DogrulamaNeden != "bicim" {
		t.Fatalf("bozuk bilet doğrulamayı düşürmedi: %+v", c)
	}
	kayit(o.bilet(tFP, func(g *bilet.Govde) { g.Durum = bilet.DurumAskida }))
	if c, _ := o.depo.CihazAl(sunucu); c.Dogrulandi(time.Now()) || c.DogrulamaNeden != "askida" {
		t.Fatalf("askıda bilet: %+v", c)
	}
	kayit(o.bilet(tFP, nil))
	if c, _ := o.depo.CihazAl(sunucu); !c.Dogrulandi(time.Now()) {
		t.Fatalf("yeniden geçerli bilet: %+v", c)
	}
}

func TestPortalBaglantilarVeIptal(t *testing.T) {
	o := yeniOrtam(t, nil)
	ajan := sahteAjanBagla(t, o, nil)
	kod, ozet := o.kodAl(t, tFP, "onarim", 7)
	o.mesaj(t, tTg, "/start "+kod)
	bekle(t, func() bool { ajan.mu.Lock(); defer ajan.mu.Unlock(); return len(ajan.eslesmeler) == 1 })
	ajan.gonder(&protokol.BaglantiDurum{BaglantiID: "bag_portal_0001", KodOzet: ozet, TgUserID: tTg, Durum: protokol.BaglantiEtkin,
		Rol: protokol.RolKok, Duzey: protokol.DuzeyOnarim, Hesaplar: []protokol.HesapEtiketi{{Sira: 1, Etiket: "ornek.com.tr"}}})
	bekle(t, func() bool { _, ok := o.depo.BaglantiAl("bag_portal_0001"); return ok })
	o.mesaj(t, tTg, "sorun var") // konuşma içeriği API'den dönmemeli

	d, m := o.portal(t, "GET", "/ic/v1/baglantilar?parmak_izi="+tFP, nil, tPortalIc)
	l, _ := m["baglantilar"].([]any)
	if d != 200 || len(l) != 1 {
		t.Fatalf("liste: %d %v", d, m)
	}
	b := l[0].(map[string]any)
	if b["baglanti_id"] != "bag_portal_0001" || b["durum"] != "etkin" || b["duzey"] != "onarim" || b["kvkk_kabul"] != true || b["kvkk_kaynak"] != "portal" || b["tg_kisa"] != tgKisa(tTg) {
		t.Fatalf("liste öğesi: %v", b)
	}
	for k, v := range b {
		if s, ok := v.(string); ok && (strings.Contains(s, "sorun var") || strings.Contains(s, "ornek.com.tr")) {
			t.Fatalf("liste konuşma/hesap içeriği sızdırdı (%s)", k)
		}
	}
	if _, bos := o.portal(t, "GET", "/ic/v1/baglantilar?parmak_izi="+strings.Repeat("9", 64), nil, tPortalIc); len(bos["baglantilar"].([]any)) != 0 {
		t.Fatalf("başka sunucunun listesi: %v", bos)
	}
	// başka parmak izi bu bağlantıyı iptal EDEMEZ (varlık da sızmaz)
	if d, m := o.portal(t, "POST", "/ic/v1/baglanti/iptal", map[string]any{"parmak_izi": strings.Repeat("9", 64), "baglanti_id": "bag_portal_0001"}, tPortalIc); d != http.StatusNotFound || m["hata"] != "baglanti_yok" {
		t.Fatalf("yabancı parmak izi iptali: %d %v", d, m)
	}
	if _, var_ := o.depo.BaglantiAl("bag_portal_0001"); !var_ {
		t.Fatal("yabancı iptal bağlantıyı sildi")
	}
	if d, m := o.portal(t, "POST", "/ic/v1/baglanti/iptal", map[string]any{"parmak_izi": tFP, "baglanti_id": "bag_portal_0001"}, tPortalIc); d != 200 || m["ajana_iletildi"] != true {
		t.Fatalf("iptal: %d %v", d, m)
	}
	if _, var_ := o.depo.BaglantiAl("bag_portal_0001"); var_ {
		t.Fatal("iptal sonrası bağlantı duruyor")
	}
	bekle(t, func() bool { ajan.mu.Lock(); defer ajan.mu.Unlock(); return len(ajan.iptaller) == 1 })
	ajan.mu.Lock()
	ip := ajan.iptaller[0]
	ajan.mu.Unlock()
	if ip.BaglantiID != "bag_portal_0001" || ip.Neden != protokol.IptalKullanici {
		t.Fatalf("ajana giden iptal: %+v", ip)
	}
	bekle(t, func() bool { return strings.Contains(o.tg.son(tTg), "kapatıldı") })
	if d, m := o.portal(t, "POST", "/ic/v1/baglanti/iptal", map[string]any{"parmak_izi": tFP, "baglanti_id": "bag_portal_0001"}, tPortalIc); d != http.StatusNotFound {
		t.Fatalf("ikinci iptal: %d %v", d, m)
	}
}
