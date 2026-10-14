package dns

// zone_dosya_test.go — zone yazımının DOSYA düzeyi garantileri (zoneUygulaGercek):
// hata hâlinde zone ve include dosyaları önceki geçerli hâline döner, sorunlu kayıt
// adlandırılır. Zone araçları komutCalistir dikişiyle SAHTELENİR; yollar geçici dizindir
// (/var/named, /etc/named ve named'e dokunulmaz). DB'li (GOSP_TEST_GECICI_DSN yoksa ATLANIR).

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// zd_komutlar — sahte komut çalıştırıcı: çağrıları kaydeder, anahtara göre hata/çıktı döner.
// Anahtar: "named-checkzone", "named-checkconf:dosya" (tmp include sınaması),
// "named-checkconf:bos" (argümansız, restart kapısı), "rndc", "systemctl:reload", "systemctl:restart".
type zd_komutlar struct {
	cagrilar []string
	davranis map[string]func(arg []string) ([]byte, error)
}

func (z *zd_komutlar) say(ad string) int {
	n := 0
	for _, c := range z.cagrilar {
		if c == ad || strings.HasPrefix(c, ad+" ") {
			n++
		}
	}
	return n
}

func zd_kur(t *testing.T) (zoneDir, includeDosya string, kmd *zd_komutlar) {
	t.Helper()
	dizin := t.TempDir()
	eskiZ, eskiI, eskiK := zoneDizini, includeYolu, komutCalistir
	t.Cleanup(func() { zoneDizini, includeYolu, komutCalistir = eskiZ, eskiI, eskiK })
	zoneDizini = filepath.Join(dizin, "zones")
	includeYolu = filepath.Join(dizin, "zones.conf")
	kmd = &zd_komutlar{davranis: map[string]func([]string) ([]byte, error){}}
	komutCalistir = func(ad string, arg ...string) ([]byte, error) {
		anahtar := ad
		switch {
		case ad == "named-checkconf" && len(arg) > 0:
			anahtar = "named-checkconf:dosya"
		case ad == "named-checkconf":
			anahtar = "named-checkconf:bos"
		case ad == "systemctl" && len(arg) > 0:
			anahtar = "systemctl:" + arg[0]
		}
		kmd.cagrilar = append(kmd.cagrilar, strings.TrimSpace(ad+" "+strings.Join(arg, " ")))
		if f, ok := kmd.davranis[anahtar]; ok {
			return f(arg)
		}
		return nil, nil
	}
	return zoneDizini, includeYolu, kmd
}

func zd_yaz(t *testing.T, yol, icerik string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(yol), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yol, []byte(icerik), 0o640); err != nil {
		t.Fatal(err)
	}
}

func zd_oku(t *testing.T, yol string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(yol)
	if errors.Is(err, os.ErrNotExist) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b), true
}

// zd_hata — sahte named-checkzone: ikinci argümandaki dosyada `iz` geçen satırı bulur ve
// gerçek named-checkzone biçiminde "<dosya>:<satır>: ..." üretir.
func zd_hata(iz, mesaj string) func([]string) ([]byte, error) {
	return func(arg []string) ([]byte, error) {
		b, _ := os.ReadFile(arg[1])
		for i, satir := range strings.Split(string(b), "\n") {
			if strings.Contains(satir, iz) {
				return []byte(fmt.Sprintf("%s:%d: %s\nzone x/IN: loading from master file %s failed: %s\n", arg[1], i+1, mesaj, arg[1], mesaj)), errors.New("exit status 1")
			}
		}
		return []byte("beklenen iz yok"), errors.New("exit status 1")
	}
}

func zd_alan(t *testing.T) (d *sql.DB, kimlikler map[string]int64) {
	t.Helper()
	d = zi_db(t)
	kimlikler = map[string]int64{
		"ns":  zi_ekle(t, d, 42, "@", "NS", "ns1.bayialan.test", 86400, 0, 1),
		"glu": zi_ekle(t, d, 42, "ns1", "A", "203.0.113.1", 3600, 0, 1),
	}
	return d, kimlikler
}

const (
	zdEskiZone    = "ESKI-ZONE-ICERIGI\n"
	zdEskiInclude = "// ESKI-INCLUDE\n"
)

// named-checkzone başarısız: canlı zone ve include DEĞİŞMEZ; sorunlu kayıt adlandırılır.
func TestZoneUygula_CheckzoneHatasi_CanliDosyalarDegismez_SorunluKayitAdlandirilir(t *testing.T) {
	zoneDir, inc, kmd := zd_kur(t)
	db, _ := zd_alan(t)
	zonePath := filepath.Join(zoneDir, "bayialan.test.zone")
	zd_yaz(t, zonePath, zdEskiZone)
	zd_yaz(t, inc, zdEskiInclude)
	bozuk := zi_ekle(t, db, 42, "www", "A", "BOZUK-DEGER", 3600, 0, 1)
	kmd.davranis["named-checkzone"] = zd_hata("BOZUK-DEGER", "near 'BOZUK-DEGER': bad dotted quad")

	geri, tazele, err := zoneUygulaGercek(context.Background(), db, 42)
	var zg *zoneGecersizHatasi
	if !errors.As(err, &zg) || geri != nil || tazele {
		t.Fatalf("*zoneGecersizHatasi beklenirdi: %v", err)
	}
	if iz := fmt.Sprintf("sorunlu kayıt: #%d www A BOZUK-DEGER", bozuk); !strings.Contains(err.Error(), iz) {
		t.Errorf("hata hangi kaydın sorunlu olduğunu söylemeli (%q): %v", iz, err)
	}
	if z, _ := zd_oku(t, zonePath); z != zdEskiZone {
		t.Errorf("canlı zone değişti: %q", z)
	}
	if i, _ := zd_oku(t, inc); i != zdEskiInclude {
		t.Errorf("include değişti: %q", i)
	}
	if _, bulundu := zd_oku(t, zonePath+".tmp"); bulundu {
		t.Error("geçici .tmp dosyası kaldı")
	}
	if kmd.say("rndc") != 0 || kmd.say("systemctl") != 0 {
		t.Errorf("zone geçersizken named yeniden yüklenmemeli: %v", kmd.cagrilar)
	}
}

// include (named-checkconf) başarısız: zone dosyası dönüştürülmüş olsa bile ESKİ hâline döner.
func TestZoneUygula_IncludeHatasi_ZoneDosyasiOncekiHaleDoner(t *testing.T) {
	zoneDir, inc, kmd := zd_kur(t)
	db, _ := zd_alan(t)
	zonePath := filepath.Join(zoneDir, "bayialan.test.zone")
	zd_yaz(t, zonePath, zdEskiZone)
	zd_yaz(t, inc, zdEskiInclude)
	kmd.davranis["named-checkconf:dosya"] = func([]string) ([]byte, error) {
		return []byte("include.tmp:3: unknown option"), errors.New("exit status 1")
	}
	geri, tazele, err := zoneUygulaGercek(context.Background(), db, 42)
	if err == nil || geri != nil || tazele || !strings.Contains(err.Error(), "zone-include geçersiz") {
		t.Fatalf("include hatası beklenirdi: %v", err)
	}
	if z, _ := zd_oku(t, zonePath); z != zdEskiZone {
		t.Errorf("zone dosyası ESKİ hâline dönmedi: %q", z)
	}
	if i, _ := zd_oku(t, inc); i != zdEskiInclude {
		t.Errorf("include değişti: %q", i)
	}
	if kmd.say("chown") == 0 {
		t.Errorf("geri yüklenen zone'un sahibi (named) yeniden atanmalı: %v", kmd.cagrilar)
	}
	if m, _ := filepath.Glob(inc + ".tmp*"); len(m) != 0 {
		t.Error("geçersiz include'un .tmp dosyası silinmeliydi")
	}
}

// Yeniden yükleme başarısız: zone önceki hâline döner (ilk kez yazılan zone silinir);
// tazele=true → çağıranın includeTazele'si include'u (bayat anlığı değil) commit edilmiş
// DB durumuna uzlaştırır ve önceki hâli yükler.
func TestZoneUygula_YenidenYuklemeHatasi_DosyalarOncekiHaleDoner(t *testing.T) {
	for _, onceden := range []bool{true, false} {
		t.Run(fmt.Sprintf("onceki_zone_var=%t", onceden), func(t *testing.T) {
			zoneDir, inc, kmd := zd_kur(t)
			db, _ := zd_alan(t)
			zonePath := filepath.Join(zoneDir, "bayialan.test.zone")
			if onceden {
				zd_yaz(t, zonePath, zdEskiZone)
				zd_yaz(t, inc, zdEskiInclude)
			}
			hata := func([]string) ([]byte, error) { return []byte("fail"), errors.New("exit status 1") }
			kmd.davranis["rndc"] = hata
			kmd.davranis["systemctl:reload"] = hata
			kmd.davranis["named-checkconf:bos"] = hata
			geri, tazele, err := zoneUygulaGercek(context.Background(), db, 42)
			if err == nil || geri != nil || !tazele || !strings.Contains(err.Error(), "named yeniden yüklenemedi") {
				t.Fatalf("yeniden yükleme hatası (tazele=true) beklenirdi: tazele=%t %v", tazele, err)
			}
			z, zVar := zd_oku(t, zonePath)
			if onceden && z != zdEskiZone {
				t.Errorf("zone ESKİ hâline dönmedi: %q", z)
			} else if !onceden && zVar {
				t.Error("hiç olmayan zone geri alınınca silinmeli")
			}
			includeTazele(context.Background(), db, true)
			beklenen, _ := buildZoneIncludes(context.Background(), db)
			if i, _ := zd_oku(t, inc); i != beklenen {
				t.Errorf("include commit edilmiş durumla uzlaşmadı: %q", i)
			}
			if kmd.say("rndc") < 2 {
				t.Errorf("önceki hâli yüklemek için en iyi çaba yeniden yükleme denenmeli: %v", kmd.cagrilar)
			}
		})
	}
}

// Başarı: yeni zone + include yazılır, named yüklenir; geri() (commit başarısızlığı yolu) eskisini getirir.
func TestZoneUygula_Basari_GeriCagrisiOncekiHaleDondurur(t *testing.T) {
	zoneDir, inc, kmd := zd_kur(t)
	db, _ := zd_alan(t)
	zonePath := filepath.Join(zoneDir, "bayialan.test.zone")
	zd_yaz(t, zonePath, zdEskiZone)
	zd_yaz(t, inc, zdEskiInclude)

	geri, tazele, err := zoneUygulaGercek(context.Background(), db, 42)
	if err != nil || geri == nil || !tazele {
		t.Fatalf("başarı (tazele=true) beklenirdi: %v", err)
	}
	z, _ := zd_oku(t, zonePath)
	i, _ := zd_oku(t, inc)
	if !strings.Contains(z, "ns1\t3600\tIN\tA\t203.0.113.1") || !strings.Contains(z, "SOA") || z == zdEskiZone {
		t.Errorf("yeni zone yazılmadı: %q", z)
	}
	if !strings.Contains(i, `zone "bayialan.test"`) {
		t.Errorf("include güncellenmedi: %q", i)
	}
	if n := kmd.say("rndc"); n != 1 {
		t.Errorf("named tam 1 kez yüklenmeli: %d", n)
	}
	if bak, _ := zd_oku(t, zonePath+".bak"); bak != zdEskiZone {
		t.Errorf(".bak yedeği önceki zone olmalı: %q", bak)
	}
	for _, beklenen := range []string{"chown named:named " + zonePath, "restorecon " + zonePath, "restorecon " + inc} {
		var bulundu bool
		for _, c := range kmd.cagrilar {
			if c == beklenen {
				bulundu = true
			}
		}
		if !bulundu {
			t.Errorf("%q çağrılmalıydı: %v", beklenen, kmd.cagrilar)
		}
	}
	geri()
	if z, _ := zd_oku(t, zonePath); z != zdEskiZone {
		t.Errorf("geri() sonrası zone eski değil: %q", z)
	}
	if n := kmd.say("rndc"); n != 1 {
		t.Errorf("geri() named'i yüklemez (includeTazele yükler): rndc=%d", n)
	}
	// zoneliIslem'in commit hatası yolu: include commit edilmiş durumla uzlaştırılır, named yüklenir.
	includeTazele(context.Background(), db, true)
	if beklenen, _ := buildZoneIncludes(context.Background(), db); i != beklenen {
		t.Errorf("include commit edilmiş durumdan farklı yazılmıştı: %q", i)
	}
	if n := kmd.say("rndc"); n != 2 {
		t.Errorf("includeTazele(yukle) named'i yeniden yüklemeli: rndc=%d", n)
	}
}

// BIND kurulu değil → kayıtlar yalnız DB'de (eski davranış); hata DEĞİL.
func TestZoneUygula_ZoneMotoruYok_HataDegil(t *testing.T) {
	zoneDir, inc, kmd := zd_kur(t)
	db, _ := zd_alan(t)
	kmd.davranis["named-checkzone"] = func([]string) ([]byte, error) {
		return nil, &exec.Error{Name: "named-checkzone", Err: exec.ErrNotFound}
	}
	geri, tazele, err := zoneUygulaGercek(context.Background(), db, 42)
	if err != nil || geri == nil || tazele {
		t.Fatalf("BIND yokken hata dönmemeli: %v", err)
	}
	geri() // no-op
	if _, v := zd_oku(t, filepath.Join(zoneDir, "bayialan.test.zone")); v {
		t.Error("zone motoru yokken zone dosyası yazılmamalı")
	}
	if _, v := zd_oku(t, inc); v {
		t.Error("zone motoru yokken include yazılmamalı")
	}
	// WriteZone (diğer paketlerin kullandığı) ise hatayı DÖNER.
	if err := WriteZone(context.Background(), db, 42); !errors.Is(err, ErrZoneMotoruYok) {
		t.Errorf("WriteZone ErrZoneMotoruYok dönmeli: %v", err)
	}
}

// Aktif kayıt yoksa zone'a dokunulmaz, hiçbir komut çalışmaz.
func TestZoneUygula_AktifKayitYok_Dokunmaz(t *testing.T) {
	zoneDir, _, kmd := zd_kur(t)
	d := zi_db(t)
	zi_ekle(t, d, 42, "www", "A", "203.0.113.5", 600, 0, 0)
	geri, tazele, err := zoneUygulaGercek(context.Background(), d, 42)
	if err != nil || geri == nil || tazele {
		t.Fatalf("hata beklenmezdi: %v", err)
	}
	geri()
	if len(kmd.cagrilar) != 0 {
		t.Errorf("komut çalıştırılmamalı: %v", kmd.cagrilar)
	}
	if _, v := zd_oku(t, filepath.Join(zoneDir, "bayialan.test.zone")); v {
		t.Error("zone yazılmamalı")
	}
}

// NS'siz zone (named-checkzone "has no NS records"): ipucu iletiye girer.
func TestZoneUygula_NSYok_IpucuVerir(t *testing.T) {
	_, _, kmd := zd_kur(t)
	d := zi_db(t)
	zi_ekle(t, d, 42, "www", "A", "203.0.113.5", 600, 0, 1)
	kmd.davranis["named-checkzone"] = func(arg []string) ([]byte, error) {
		return []byte("zone bayialan.test/IN: has no NS records\nzone bayialan.test/IN: not loaded due to errors.\n"), errors.New("exit status 1")
	}
	_, _, err := zoneUygulaGercek(context.Background(), d, 42)
	if err == nil || !strings.Contains(err.Error(), "en az bir etkin NS kaydı") {
		t.Errorf("NS ipucu beklenirdi: %v", err)
	}
}

// GERÇEK named-checkzone ile uçtan uca: DB'de ZATEN bozuk kayıt varsa ileti onu adlandırır.
func TestZoneUygula_GercekNamedCheckzone_BozukKayitAdlandirilir(t *testing.T) {
	if _, err := exec.LookPath("named-checkzone"); err != nil {
		t.Skip("named-checkzone yok")
	}
	zoneDir, _, kmd := zd_kur(t)
	kmd.davranis["named-checkzone"] = func(arg []string) ([]byte, error) {
		return exec.Command("named-checkzone", arg...).CombinedOutput()
	}
	db, _ := zd_alan(t)
	zonePath := filepath.Join(zoneDir, "bayialan.test.zone")
	zd_yaz(t, zonePath, zdEskiZone)
	zi_ekle(t, db, 42, "iyi", "A", "203.0.113.9", 3600, 0, 1)
	bozuk := zi_ekle(t, db, 42, "kotu", "A", "bu-ip-degil", 3600, 0, 1)
	_, _, err := zoneUygulaGercek(context.Background(), db, 42)
	var zg *zoneGecersizHatasi
	if !errors.As(err, &zg) {
		t.Fatalf("gerçek named-checkzone reddetmeliydi: %v", err)
	}
	if iz := fmt.Sprintf("#%d kotu A bu-ip-degil", bozuk); !strings.Contains(err.Error(), iz) {
		t.Errorf("gerçek named-checkzone satırı kayda eşlenmeli (%q): %v", iz, err)
	}
	if strings.Contains(err.Error(), "#"+fmt.Sprint(bozuk+1)) || strings.Contains(err.Error(), " iyi A") {
		t.Errorf("sağlam kayıt suçlanmamalı: %v", err)
	}
	if z, _ := zd_oku(t, zonePath); z != zdEskiZone {
		t.Errorf("canlı zone değişti: %q", z)
	}
}

// KayitGecerli'nin kabul ettiği HER örnek gerçek named-checkzone'dan da geçer
// (doğrulayıcı gerçek zone'a göre fazla gevşek/sıkı olmasın).
func TestKayitGecerli_GecerliDedigimizZoneDogrulamasindanGecer(t *testing.T) {
	if _, err := exec.LookPath("named-checkzone"); err != nil {
		t.Skip("named-checkzone yok")
	}
	dizin := t.TempDir()
	for i, c := range gecerliOrnekler() {
		c.Tip = strings.ToUpper(c.Tip)
		if c.Ad == "" {
			c.Ad = "@"
		}
		if c.TTL == 0 {
			c.TTL = 3600
		}
		kayitlar := []Kayit{
			{Ad: "@", Tip: "NS", Deger: "ns1.ornek.test", TTL: 3600},
			{Ad: "ns1", Tip: "A", Deger: "203.0.113.1", TTL: 3600},
			c,
		}
		var buf bytes.Buffer
		if err := zoneTmpl.Execute(&buf, zoneCtx{AlanAdi: "ornek.test", Serial: "2026101301", SOA: defaultSOA("ornek.test"), Kayitlar: kayitlar}); err != nil {
			t.Fatal(err)
		}
		yol := filepath.Join(dizin, fmt.Sprintf("z%d.zone", i))
		if err := os.WriteFile(yol, buf.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("named-checkzone", "ornek.test", yol).CombinedOutput(); err != nil {
			t.Errorf("KayitGecerli kabul etti ama named-checkzone reddetti: %s %q %q\n%s", c.Tip, c.Ad, c.Deger, out)
		}
	}
}

// reloadNamedErr: rndc → systemctl reload → (named-checkconf kapısı) → systemctl restart.
func TestReloadNamedErr_Sirasi(t *testing.T) {
	hata := func([]string) ([]byte, error) { return []byte("x"), errors.New("exit status 1") }
	tablo := []struct {
		ad       string
		davranis map[string]func([]string) ([]byte, error)
		hataVar  bool
		restart  int
	}{
		{"rndc başarılı", map[string]func([]string) ([]byte, error){}, false, 0},
		{"rndc başarısız, systemctl reload başarılı", map[string]func([]string) ([]byte, error){"rndc": hata}, false, 0},
		{"ikisi de başarısız, checkconf geçersiz → restart ATLANIR", map[string]func([]string) ([]byte, error){
			"rndc": hata, "systemctl:reload": hata, "named-checkconf:bos": hata}, true, 0},
		{"checkconf geçerli → restart denenir ve başarılı", map[string]func([]string) ([]byte, error){
			"rndc": hata, "systemctl:reload": hata}, false, 1},
		{"restart de başarısız", map[string]func([]string) ([]byte, error){
			"rndc": hata, "systemctl:reload": hata, "systemctl:restart": hata}, true, 1},
	}
	for _, c := range tablo {
		t.Run(c.ad, func(t *testing.T) {
			_, _, kmd := zd_kur(t)
			for k, v := range c.davranis {
				kmd.davranis[k] = v
			}
			err := reloadNamedErr()
			if (err != nil) != c.hataVar {
				t.Fatalf("hata=%v beklenen=%v (%v)", err, c.hataVar, kmd.cagrilar)
			}
			if n := kmd.say("systemctl restart"); n != c.restart {
				t.Errorf("restart denemesi %d, beklenen %d: %v", n, c.restart, kmd.cagrilar)
			}
		})
	}
}

// Kayıt değeri satır sonu taşıyorsa satır → kayıt eşlemesi kayar: yanlış kaydı SUÇLAMA.
func TestZoneGecersizOlustur_SatirSayisiTutmuyorsaSuclamaz(t *testing.T) {
	kayitlar := []Kayit{{ID: 1, Ad: "a", Tip: "A", Deger: "1.2.3.4"}, {ID: 2, Ad: "b", Tip: "A", Deger: "x\ny"}}
	baslik := 8
	tutan := strings.Repeat("h\n", baslik) + "k1\nk2\n"
	e := zoneGecersizOlustur("a.test", "z.zone.tmp:10: bad", tutan, baslik, kayitlar)
	if len(e.kayitlar) != 1 || !strings.Contains(e.kayitlar[0], "#2 b A") {
		t.Fatalf("10. satır 2. kayıt olmalı: %v", e.kayitlar)
	}
	tutmayan := tutan + "fazladan\n" // kayıt değeri satır sonu taşıdı
	if e := zoneGecersizOlustur("a.test", "z.zone.tmp:10: bad", tutmayan, baslik, kayitlar); len(e.kayitlar) != 0 {
		t.Errorf("satır sayısı tutmuyorsa kayıt suçlanmamalı: %v", e.kayitlar)
	}
	// Başlık satırlarındaki (SOA) hata kayda eşlenmez.
	if e := zoneGecersizOlustur("a.test", "z.zone.tmp:3: bad", tutan, baslik, kayitlar); len(e.kayitlar) != 0 {
		t.Errorf("SOA satırı kayıt değil: %v", e.kayitlar)
	}
	// En çok 5 sorunlu kayıt listelenir.
	cok := make([]Kayit, 9)
	var cikti, render strings.Builder
	render.WriteString(strings.Repeat("h\n", baslik))
	for i := range cok {
		cok[i] = Kayit{ID: int64(i + 1), Ad: "r", Tip: "A", Deger: "x"}
		render.WriteString("r\n")
		fmt.Fprintf(&cikti, "z.zone.tmp:%d: bad\n", baslik+1+i)
	}
	if e := zoneGecersizOlustur("a.test", cikti.String(), render.String(), baslik, cok); len(e.kayitlar) != 5 {
		t.Errorf("en çok 5 kayıt listelenmeli: %d", len(e.kayitlar))
	}
}

// WriteZone (diğer paketlerin kullandığı genel giriş): değişiklik varsa named 1 kez yüklenir; aktif kayıt
// yoksa zone'a dokunulmaz ve named yüklenmez.
func TestWriteZone_YenidenYuklemeYalnizDegisincePaket(t *testing.T) {
	zoneDir, _, kmd := zd_kur(t)
	db, _ := zd_alan(t)
	if err := WriteZone(context.Background(), db, 42); err != nil {
		t.Fatalf("WriteZone: %v", err)
	}
	if n := kmd.say("rndc reload"); n != 1 {
		t.Errorf("zone değişince named tam 1 kez yüklenmeli: %d (%v)", n, kmd.cagrilar)
	}
	if z, _ := zd_oku(t, filepath.Join(zoneDir, "bayialan.test.zone")); !strings.Contains(z, "SOA") {
		t.Errorf("zone yazılmadı: %q", z)
	}
	kmd.cagrilar = nil
	d2 := zi_db(t)
	zi_ekle(t, d2, 42, "www", "A", "203.0.113.5", 600, 0, 0)
	if err := WriteZone(context.Background(), d2, 42); err != nil {
		t.Fatalf("WriteZone (aktif kayıt yok): %v", err)
	}
	if len(kmd.cagrilar) != 0 {
		t.Errorf("aktif kayıt yokken komut çalışmamalı: %v", kmd.cagrilar)
	}
}

// DB'den okunan alan adı ZoneDir dışına çıkamaz (yol gezintisi): ne yazılır ne silinir.
func TestZoneYolu_AlanAdiGezintisiReddedilir(t *testing.T) {
	zoneDir, _, kmd := zd_kur(t)
	db := zi_db(t)
	dk_exec(t, db, `UPDATE domains SET alan_adi='../kacak.test' WHERE id=42`)
	zi_ekle(t, db, 42, "www", "A", "203.0.113.5", 600, 0, 1)
	dis := filepath.Join(filepath.Dir(zoneDir), "kacak.test.zone")
	_, _, err := zoneUygulaGercek(context.Background(), db, 42)
	if err == nil || !strings.Contains(err.Error(), "geçersiz alan adı") {
		t.Fatalf("yol gezintisi reddedilmeliydi: %v", err)
	}
	if _, bulundu := zd_oku(t, dis); bulundu {
		t.Error("ZoneDir dışına zone dosyası yazıldı")
	}
	if len(kmd.cagrilar) != 0 {
		t.Errorf("reddedilen alan adı için komut çalışmamalı: %v", kmd.cagrilar)
	}
	zd_yaz(t, dis, "DOKUNMA")
	if err := DeleteZone(context.Background(), db, "../kacak.test"); err == nil {
		t.Error("DeleteZone yol gezintisini reddetmeliydi")
	}
	if i, _ := zd_oku(t, dis); i != "DOKUNMA" {
		t.Errorf("ZoneDir dışındaki dosya silindi/değişti: %q", i)
	}
	// Üç yasak parça AYRI AYRI yakalanır: "/", "\\" ve "..".
	for _, ad := range []string{"alt/dizin.test", `ters\bolu.test`, "iki..nokta.test"} {
		dk_exec(t, db, `UPDATE domains SET alan_adi=? WHERE id=42`, ad)
		if _, _, err := zoneUygulaGercek(context.Background(), db, 42); err == nil || !strings.Contains(err.Error(), "geçersiz alan adı") {
			t.Errorf("zoneHazirla %q adını reddetmeliydi: %v", ad, err)
		}
		if err := DeleteZone(context.Background(), db, ad); err == nil {
			t.Errorf("DeleteZone %q adını reddetmeliydi", ad)
		}
	}
	if len(kmd.cagrilar) != 0 {
		t.Errorf("reddedilen alan adları için komut çalışmamalı: %v", kmd.cagrilar)
	}
}

// dosyaAnlik.geriYukle: sahip + SELinux etiketi, sahipsiz dosyaya chown YOK, olmayan dosyanın silinmesi hata değil.
func TestDosyaAnlik_GeriYukle(t *testing.T) {
	_, _, kmd := zd_kur(t)
	yol := filepath.Join(t.TempDir(), "a.zone")
	zd_yaz(t, yol, "YENI")
	if err := (dosyaAnlik{yol: yol, sahip: "named:named", mod: 0o640, icerik: []byte("ESKI"), vardi: true}).geriYukle(); err != nil {
		t.Fatal(err)
	}
	if i, _ := zd_oku(t, yol); i != "ESKI" {
		t.Errorf("içerik geri yüklenmedi: %q", i)
	}
	if kmd.say("chown named:named "+yol) != 1 || kmd.say("restorecon "+yol) != 1 {
		t.Errorf("sahip + etiket yeniden atanmalı: %v", kmd.cagrilar)
	}
	kmd.cagrilar = nil
	if err := (dosyaAnlik{yol: yol, mod: 0o644, icerik: []byte("ESKI2"), vardi: true}).geriYukle(); err != nil {
		t.Fatal(err)
	}
	if kmd.say("chown") != 0 || kmd.say("restorecon "+yol) != 1 {
		t.Errorf("sahipsiz dosyaya chown çağrılmamalı, etiket yenilenmeli: %v", kmd.cagrilar)
	}
	// Önceden yoksa silinir; zaten yoksa da hata değildir.
	for i := 0; i < 2; i++ {
		if err := (dosyaAnlik{yol: yol}).geriYukle(); err != nil {
			t.Fatalf("önceden olmayan dosyanın geri yüklenmesi hata vermemeli (%d. çağrı): %v", i+1, err)
		}
	}
	if _, bulundu := zd_oku(t, yol); bulundu {
		t.Error("önceden olmayan dosya silinmeliydi")
	}
}

// zoneGeri: nil güvenli; değişmediyse DOKUNMAZ; ikinci çağrı etkisizdir.
func TestZoneGeri_Korumalar(t *testing.T) {
	zd_kur(t)
	var bos *zoneGeri
	bos.Geri() // panik yok
	if bos.Degisti() {
		t.Error("nil zoneGeri değişmiş sayılmamalı")
	}
	yol := filepath.Join(t.TempDir(), "a.zone")
	zd_yaz(t, yol, "SIMDI")
	anlik := []dosyaAnlik{{yol: yol, mod: 0o640, icerik: []byte("ESKI"), vardi: true}}
	(&zoneGeri{anliklar: anlik}).Geri()
	if i, _ := zd_oku(t, yol); i != "SIMDI" {
		t.Errorf("değişmemiş zoneGeri dosyaya dokunmamalı: %q", i)
	}
	g := &zoneGeri{degisti: true, anliklar: anlik}
	g.Geri()
	if i, _ := zd_oku(t, yol); i != "ESKI" {
		t.Errorf("Geri() önceki hâli getirmeli: %q", i)
	}
	if g.Degisti() {
		t.Error("Geri() sonrası değişmiş sayılmamalı")
	}
	zd_yaz(t, yol, "SIMDI2")
	g.Geri()
	if i, _ := zd_oku(t, yol); i != "SIMDI2" {
		t.Errorf("ikinci Geri() etkisiz olmalı: %q", i)
	}
}

// named-checkzone satır → kayıt eşlemesi: tekrar eden satır bir kez, değer 60 karakterde kırpılır, en çok 5 kayıt.
func TestZoneGecersizOlustur_TekrarKirpmaVeSinir(t *testing.T) {
	const baslik = 8
	satirlar := func(n int) (cikti, render string, kayitlar []Kayit) {
		var c, r strings.Builder
		r.WriteString(strings.Repeat("h\n", baslik))
		for i := 1; i <= n; i++ {
			kayitlar = append(kayitlar, Kayit{ID: int64(i), Ad: fmt.Sprintf("r%d", i), Tip: "A", Deger: "x"})
			r.WriteString("r\n")
			fmt.Fprintf(&c, "z.zone.tmp:%d: bad\n", baslik+i)
		}
		return c.String(), r.String(), kayitlar
	}
	for _, c := range []struct{ n, bek int }{{1, 1}, {4, 4}, {5, 5}, {6, 5}, {9, 5}} {
		cikti, render, kayitlar := satirlar(c.n)
		if e := zoneGecersizOlustur("a.test", cikti, render, baslik, kayitlar); len(e.kayitlar) != c.bek {
			t.Errorf("%d sorunlu kayıttan %d listelenmeli: %d", c.n, c.bek, len(e.kayitlar))
		}
	}
	// Aynı satır birden çok kez raporlansa da kayıt tek sefer listelenir.
	kayitlar := []Kayit{{ID: 1, Ad: "uzun", Tip: "TXT", Deger: strings.Repeat("a", 70)}, {ID: 2, Ad: "tam", Tip: "TXT", Deger: strings.Repeat("b", 60)}}
	render := strings.Repeat("h\n", baslik) + "k1\nk2\n"
	e := zoneGecersizOlustur("a.test", "z.zone.tmp:9: bad\nz.zone.tmp:9: bad again\nz.zone.tmp:10: bad\n", render, baslik, kayitlar)
	if len(e.kayitlar) != 2 {
		t.Fatalf("2 benzersiz kayıt beklenirdi: %v", e.kayitlar)
	}
	if !strings.HasSuffix(e.kayitlar[0], strings.Repeat("a", 60)+"…") || strings.Contains(e.kayitlar[0], strings.Repeat("a", 61)) {
		t.Errorf("70 karakterlik değer 60'ta kırpılmalı: %q", e.kayitlar[0])
	}
	if !strings.HasSuffix(e.kayitlar[1], strings.Repeat("b", 60)) {
		t.Errorf("tam 60 karakter kırpılmamalı: %q", e.kayitlar[1])
	}
}

// buildZoneIncludes: yalnız AKTİF kaydı olan domainler; AXFR HER ZAMAN kapalı; DNSSEC yalnız bayrak açıkken.
func TestBuildZoneIncludes_KapsamAXFRVeDNSSEC(t *testing.T) {
	db := zi_db(t)
	ctx := context.Background()
	zi_ekle(t, db, 42, "www", "A", "203.0.113.5", 600, 0, 1)
	zi_ekle(t, db, 43, "www", "A", "203.0.113.6", 600, 0, 0) // yalnız pasif kayıt
	s, err := buildZoneIncludes(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, `zone "bayialan.test" {`) || strings.Contains(s, "baska.test") {
		t.Errorf("yalnız aktif kaydı olan domain include'a girmeli:\n%s", s)
	}
	if strings.Contains(s, "dnssec-policy") {
		t.Errorf("DNSSEC kapalıyken politika eklenmemeli:\n%s", s)
	}
	dk_exec(t, db, `UPDATE domains SET dnssec_aktif=1 WHERE id=42`)
	zi_ekle(t, db, 43, "w2", "A", "203.0.113.7", 600, 0, 1)
	s, _ = buildZoneIncludes(ctx, db)
	for _, satir := range strings.Split(strings.TrimSpace(s), "\n") {
		if !strings.HasPrefix(satir, "zone ") {
			continue
		}
		if !strings.Contains(satir, "allow-transfer { none; };") || strings.Contains(satir, "allow-transfer { any") {
			t.Errorf("AXFR HER ZAMAN kapalı olmalı: %s", satir)
		}
		if strings.Contains(satir, `"bayialan.test"`) != strings.Contains(satir, "dnssec-policy default; key-directory") {
			t.Errorf("DNSSEC politikası yalnız bayrağı açık domainde olmalı: %s", satir)
		}
	}
	if strings.Count(s, "zone ") != 2 {
		t.Errorf("iki zone beklenirdi:\n%s", s)
	}
}

// HealZoneIncludes: include'u günceller ve named'i yükler; include geçersizse hata döner, named yüklenmez.
func TestHealZoneIncludes(t *testing.T) {
	_, inc, kmd := zd_kur(t)
	db, _ := zd_alan(t)
	if err := HealZoneIncludes(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if i, _ := zd_oku(t, inc); !strings.Contains(i, `zone "bayialan.test"`) {
		t.Errorf("include güncellenmedi: %q", i)
	}
	if n := kmd.say("rndc reload"); n != 1 {
		t.Errorf("named tam 1 kez yüklenmeli: %d", n)
	}
	kmd.cagrilar = nil
	kmd.davranis["named-checkconf:dosya"] = func([]string) ([]byte, error) { return []byte("bozuk"), errors.New("exit status 1") }
	if err := HealZoneIncludes(context.Background(), db); err == nil || !strings.Contains(err.Error(), "zone-include geçersiz") {
		t.Errorf("geçersiz include hata döndürmeli: %v", err)
	}
	if kmd.say("rndc") != 0 {
		t.Errorf("geçersiz include'da named yüklenmemeli: %v", kmd.cagrilar)
	}
}

// DeleteZone: zone dosyasını siler, include'u yeniden üretir ve named'i yükler.
func TestDeleteZone_DosyaSilinirIncludeYenilenirNamedYuklenir(t *testing.T) {
	zoneDir, inc, kmd := zd_kur(t)
	db, _ := zd_alan(t)
	zonePath := filepath.Join(zoneDir, "bayialan.test.zone")
	zd_yaz(t, zonePath, "ESKI")
	if err := DeleteZone(context.Background(), db, "bayialan.test"); err != nil {
		t.Fatal(err)
	}
	if _, bulundu := zd_oku(t, zonePath); bulundu {
		t.Error("zone dosyası silinmeliydi")
	}
	if _, bulundu := zd_oku(t, inc); !bulundu {
		t.Error("include yeniden üretilmeliydi")
	}
	if n := kmd.say("rndc reload"); n != 1 {
		t.Errorf("named tam 1 kez yüklenmeli: %d", n)
	}
}

// Ek alan adı bölgesi: yazılır, include'a girer, named yüklenir; named-checkzone hatasında yazılmaz ve yüklenmez.
func TestWriteEkZone_YazarYukler_HataDaYazmaz(t *testing.T) {
	zoneDir, inc, kmd := zd_kur(t)
	db := zi_db(t)
	dk_goc(t, db, "0160_ek_alan_adlari.sql")
	dk_exec(t, db, `INSERT INTO ek_alan_adlari (id, domain_id, alan_adi) VALUES (1, 42, 'ek-bir.org')`)
	dk_exec(t, db, `INSERT INTO ek_dns_kayitlari (ek_alan_id, ad, tip, deger) VALUES (1,'@','NS','ns1.ek-bir.org'),(1,'ns1','A','198.51.100.1')`)
	zonePath := filepath.Join(zoneDir, "ek-bir.org.zone")
	kmd.davranis["named-checkzone"] = func([]string) ([]byte, error) { return []byte("bad"), errors.New("exit status 1") }
	if err := WriteEkZone(context.Background(), db, 1); err == nil {
		t.Fatal("named-checkzone hatası dönmeliydi")
	}
	if _, bulundu := zd_oku(t, zonePath); bulundu || kmd.say("rndc") != 0 {
		t.Errorf("geçersiz ek zone yazılmamalı/yüklenmemeli: %v", kmd.cagrilar)
	}
	delete(kmd.davranis, "named-checkzone")
	if err := WriteEkZone(context.Background(), db, 1); err != nil {
		t.Fatal(err)
	}
	if z, _ := zd_oku(t, zonePath); !strings.Contains(z, "SOA") || !strings.Contains(z, "ns1\t3600\tIN\tA\t198.51.100.1") {
		t.Errorf("ek zone yazılmadı: %q", z)
	}
	if i, _ := zd_oku(t, inc); !strings.Contains(i, `zone "ek-bir.org"`) {
		t.Errorf("ek zone include'a girmedi: %q", i)
	}
	if n := kmd.say("rndc reload"); n != 1 {
		t.Errorf("named tam 1 kez yüklenmeli: %d", n)
	}
}
