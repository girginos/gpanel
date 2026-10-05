package provisioner

// panelmarka_test.go — bayi özel panel adı G/Ç katmanı: sahte acme.sh / nginx / TLS
// dikişleri + GERÇEK MariaDB (GOSP_TEST_GECICI_DSN; yoksa DB'li testler ATLANIR).
// Hiçbir test gerçek /etc yoluna yazmaz: her yol geçici dizine çevrilir.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"girginospanel/internal/marka"
)

const pmTestConf = `server {
    listen 8443 ssl;
    listen [::]:8443 ssl;
    ssl_certificate /etc/ssl/girginospanel/panel.crt;
    ssl_certificate_key /etc/ssl/girginospanel/panel.key;
    server_name panel.kok.test 1.2.3.4 localhost;
    location / { proxy_pass http://127.0.0.1:8080; }
}
`

const pmAd = "panel.bayi.test"

type pmSahte struct {
	*kapiSahte
	acme      []string
	acmeHata  map[string]bool // ad → --issue başarısız
	dns       map[string]bool // ad → A kaydı bu sunucuya
	sunma     map[string]bool // ad → nginx bu adın sertifikasını sunmuyor
	tlsHata   func(sni string) error
	sniTHata  bool // SNI dosyası varken `nginx -t` düşer
	grup      []string
	sertBitis time.Duration
}

func pmSertUret(t *testing.T, crtYol, keyYol string, adlar []string, omur time.Duration) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sb, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	sab := &x509.Certificate{SerialNumber: sb, Subject: pkix.Name{CommonName: adlar[0]}, DNSNames: adlar,
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(omur)}
	der, err := x509.CreateCertificate(rand.Reader, sab, sab, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalECPrivateKey(k)
	// os.WriteFile var olan dosyanın iznini KORUR (acme.sh'nin `cat >` davranışı gibi).
	if err := os.WriteFile(crtYol, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyYol, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		t.Fatal(err)
	}
}

func argDeger(arg []string, ad string) string {
	for i := 0; i+1 < len(arg); i++ {
		if arg[i] == ad {
			return arg[i+1]
		}
	}
	return ""
}

func pmTestKur(t *testing.T) *pmSahte {
	t.Helper()
	ks := kapiTestKur(t, pmTestConf, nil)
	s := &pmSahte{kapiSahte: ks, acmeHata: map[string]bool{}, dns: map[string]bool{pmAd: true}, sunma: map[string]bool{}, sertBitis: 90 * 24 * time.Hour}
	d := ks.dir
	eski := []any{pmSertDizin, pmVarsCrt, pmVarsKey, pmIncYolu, pmSNIYolu, pmPanelCrt, pmPanelKey, pmWebroot, pmAcmeHome, pmStagingIsaret}
	eskiF := struct {
		komut func(context.Context, ...string) ([]byte, error)
		var_  func() bool
		dns   func(string, []string) bool
		tls   func(int, string) ([]byte, error)
		grup  func(string)
		rc    func(string)
		bekle time.Duration
	}{pmAcmeKomut, pmAcmeVar, pmBuSunucuya, pmTLSOlc, pmNginxGrup, pmRestorecon, pmOlcBekle}
	t.Cleanup(func() {
		pmSertDizin, pmVarsCrt, pmVarsKey = eski[0].(string), eski[1].(string), eski[2].(string)
		pmIncYolu, pmSNIYolu, pmPanelCrt, pmPanelKey = eski[3].(string), eski[4].(string), eski[5].(string), eski[6].(string)
		pmWebroot, pmAcmeHome, pmStagingIsaret = eski[7].(string), eski[8].(string), eski[9].(string)
		pmAcmeKomut, pmAcmeVar, pmBuSunucuya, pmTLSOlc = eskiF.komut, eskiF.var_, eskiF.dns, eskiF.tls
		pmNginxGrup, pmRestorecon, pmOlcBekle = eskiF.grup, eskiF.rc, eskiF.bekle
		pmGeriMu.Lock()
		pmGeri = map[string]pmGeriKayit{}
		pmGeriMu.Unlock()
	})
	pmGeriMu.Lock()
	pmGeri = map[string]pmGeriKayit{}
	pmGeriMu.Unlock()
	pmSertDizin = filepath.Join(d, "ssl", "bayi")
	pmVarsCrt, pmVarsKey = filepath.Join(pmSertDizin, "_varsayilan.crt"), filepath.Join(pmSertDizin, "_varsayilan.key")
	pmIncYolu = filepath.Join(d, "gosp", "marka-adlari.inc")
	pmSNIYolu = filepath.Join(d, "00-gosp-marka-sni.conf")
	pmPanelCrt, pmPanelKey = filepath.Join(d, "ssl", "panel.crt"), filepath.Join(d, "ssl", "panel.key")
	pmWebroot = filepath.Join(d, "webroot")
	pmAcmeHome = filepath.Join(d, "acme")
	pmStagingIsaret = filepath.Join(d, "staging")
	pmOlcBekle = time.Millisecond
	if err := os.MkdirAll(filepath.Join(d, "ssl"), 0o755); err != nil {
		t.Fatal(err)
	}
	pmSertUret(t, pmPanelCrt, pmPanelKey, []string{"panel.kok.test"}, 60*24*time.Hour)
	// Panelin KENDİ acme kaydı: temizlik ona ASLA dokunmamalı.
	kendi := filepath.Join(pmAcmeHome, "panel.kok.test_ecc")
	_ = os.MkdirAll(kendi, 0o700)
	_ = os.WriteFile(filepath.Join(kendi, "panel.kok.test.conf"), []byte("Le_Domain='panel.kok.test'\nLe_RealFullChainPath='"+pmPanelCrt+"'\n"), 0o600)

	eskiYol := pmAcmeYoluBekle
	t.Cleanup(func() { pmAcmeYoluBekle = eskiYol })
	pmAcmeYoluBekle = func(string) error { return nil } // gerçek nginx yok; ölçüm ayrı testte
	pmAcmeVar = func() bool { return true }
	pmBuSunucuya = func(ad string, _ []string) bool { return s.dns[ad] }
	pmNginxGrup = func(yol string) { s.grup = append(s.grup, yol) }
	pmRestorecon = func(string) {}
	pmAcmeKomut = func(_ context.Context, arg ...string) ([]byte, error) {
		ad := argDeger(arg, "-d")
		s.acme = append(s.acme, arg[0]+" "+ad)
		switch arg[0] {
		case "--issue":
			if s.acmeHata[ad] {
				return []byte("Verify error: Invalid response from http://" + ad + "/.well-known/acme-challenge/x: 404"), errors.New("exit status 1")
			}
		case "--install-cert":
			pmSertUret(t, argDeger(arg, "--fullchain-file"), argDeger(arg, "--key-file"), []string{ad}, s.sertBitis)
			dz := filepath.Join(pmAcmeHome, ad+"_ecc")
			_ = os.MkdirAll(dz, 0o700)
			_ = os.WriteFile(filepath.Join(dz, ad+".conf"), []byte("Le_Domain='"+ad+"'\nLe_RealFullChainPath='"+argDeger(arg, "--fullchain-file")+"'\n"), 0o600)
		}
		return []byte("ok"), nil
	}
	// Sahte nginx TLS: _panel.conf'un o anki kipine ve SNI haritasına göre sunar.
	pmTLSOlc = func(_ int, sni string) ([]byte, error) {
		if s.tlsHata != nil {
			if err := s.tlsHata(sni); err != nil {
				return nil, err
			}
		}
		conf, _ := os.ReadFile(kapiVhostYolu)
		if !marka.PanelConfDegiskenli(string(conf)) {
			return pmYaprakDER(pmPanelCrt), nil
		}
		if sni != "" && !s.sunma[sni] {
			if b, err := os.ReadFile(pmSNIYolu); err == nil && strings.Contains(string(b), "    "+sni+" ") {
				return pmYaprakDER(pmCrt(sni)), nil
			}
		}
		if y := pmYaprakDER(pmVarsCrt); y != nil {
			return y, nil
		}
		return nil, errors.New("handshake failure")
	}
	ks.nginxT = func(int) error {
		if _, err := os.Stat(pmSNIYolu); err == nil && s.sniTHata {
			return errors.New("emerg")
		}
		return nil
	}
	return s
}

func (s *pmSahte) acmeSay(onek string) int {
	n := 0
	for _, c := range s.acme {
		if strings.HasPrefix(c, onek) {
			n++
		}
	}
	return n
}

// ── DB ──────────────────────────────────────────────────────────────────────

func pmTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOSP_TEST_GECICI_DSN")
	if dsn == "" {
		t.Skip("GOSP_TEST_GECICI_DSN yok — DB'li test atlandı")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	k := cfg.Clone()
	k.DBName = ""
	kok, err := sql.Open("mysql", k.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	ad := fmt.Sprintf("gosp_test_pm_%d", time.Now().UnixNano())
	if _, err := kok.Exec("CREATE DATABASE " + ad + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = kok.Exec("DROP DATABASE IF EXISTS " + ad); _ = kok.Close() })
	c := cfg.Clone()
	c.DBName = ad
	db, err := sql.Open("mysql", c.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, q := range []string{
		`CREATE TABLE users (id BIGINT UNSIGNED NOT NULL PRIMARY KEY, username VARCHAR(64) NOT NULL, role VARCHAR(16) NOT NULL) ENGINE=InnoDB`,
		`CREATE TABLE cp_ayarlar (anahtar VARCHAR(64) NOT NULL PRIMARY KEY, deger TEXT NOT NULL) ENGINE=InnoDB`,
		`INSERT INTO users VALUES (7,'bayiA','reseller'),(8,'bayiB','reseller')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("hazırlık: %v", err)
		}
	}
	for _, dosya := range []string{"0122_bayi_marka.sql", "0123_bayi_marka_alanadi.sql"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "migrations", dosya))
		if err != nil {
			t.Fatal(err)
		}
		var temiz []string
		for _, sat := range strings.Split(string(body), "\n") {
			if x := strings.TrimSpace(sat); x == "" || strings.HasPrefix(x, "--") {
				continue
			}
			temiz = append(temiz, sat)
		}
		for _, ifade := range strings.Split(strings.Join(temiz, "\n"), ";") {
			if q := strings.TrimSpace(ifade); q != "" {
				if _, err := db.Exec(q); err != nil {
					t.Fatalf("göç %s: %v", dosya, err)
				}
			}
		}
	}
	for _, q := range []string{
		`INSERT INTO cp_marka (reseller_id, anahtar, ayar_json, alanadi_izni) VALUES (7,'aaaaaaaaaaaaaaaaaaaa','{}',1),(8,'bbbbbbbbbbbbbbbbbbbb','{}',1)`,
		`INSERT INTO cp_marka_alanadi (alan_adi, reseller_id, dogrulama, durum) VALUES ('` + pmAd + `',7,'0123456789abcdef0123456789abcdef','ssl_bekliyor')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("satır: %v", err)
		}
	}
	return db
}

func pmDurum(t *testing.T, db *sql.DB, ad string) (durum, genel, ayrinti string, bitis sql.NullTime) {
	t.Helper()
	err := db.QueryRow(`SELECT durum, son_hata_genel, COALESCE(son_hata_ayrinti,''), ssl_bitis FROM cp_marka_alanadi WHERE alan_adi=?`, ad).
		Scan(&durum, &genel, &ayrinti, &bitis)
	if err != nil {
		t.Fatalf("durum okunamadı: %v", err)
	}
	return
}

func dosyaYok(t *testing.T, yol string) {
	t.Helper()
	if _, err := os.Stat(yol); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s olmamalıydı (err=%v)", filepath.Base(yol), err)
	}
}

func izin(t *testing.T, yol string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(yol)
	if err != nil {
		t.Fatalf("%s: %v", yol, err)
	}
	return fi.Mode().Perm()
}

// ── testler ─────────────────────────────────────────────────────────────────

func TestPanelMarka_UcUcaEtkinlestirir(t *testing.T) {
	db := pmTestDB(t)
	s := pmTestKur(t)
	sonuc := pmDongu(context.Background(), db)
	if len(sonuc.Etkin) != 1 || sonuc.Etkin[0] != pmAd || !sonuc.Uygulama {
		t.Fatalf("ad etkinleşmeliydi: %+v", sonuc)
	}
	if s.acmeSay("--issue "+pmAd) != 1 || s.acmeSay("--install-cert "+pmAd) != 1 {
		t.Fatalf("acme çağrıları: %v", s.acme)
	}
	conf := oku(t, kapiVhostYolu)
	if !strings.Contains(conf, "ssl_certificate $gp_panel_crt;") || !strings.Contains(conf, "include "+marka.MarkaAdlariInc+";") {
		t.Fatalf("_panel.conf değişkenli kipe geçmedi:\n%s", conf)
	}
	if sni := oku(t, pmSNIYolu); !strings.Contains(sni, "    "+pmAd+" "+marka.BayiSertDizin+"/"+pmAd+".crt;") {
		t.Errorf("SNI haritası:\n%s", sni)
	}
	if inc := oku(t, pmIncYolu); inc != "server_name "+pmAd+";\n" {
		t.Errorf("include: %q", inc)
	}
	if a := oku(t, kapiMarkaAcmeYolu); !strings.Contains(a, "server_name "+pmAd+";") || !strings.Contains(a, "https://$server_name:8443$request_uri;") {
		t.Errorf(":80 vhost:\n%s", a)
	}
	// Anahtar izinleri: worker (grup nginx) okur, kiracı okuyamaz.
	for _, y := range []string{pmKey(pmAd), pmVarsKey} {
		if m := izin(t, y); m != 0o640 {
			t.Errorf("%s izni %o, beklenen 0640", filepath.Base(y), m)
		}
		if !strings.Contains(strings.Join(s.grup, "|"), y) {
			t.Errorf("%s grubu nginx yapılmadı", filepath.Base(y))
		}
	}
	if m := izin(t, pmSertDizin); m != 0o750 {
		t.Errorf("sertifika dizini %o, beklenen 0750", m)
	}
	if m := izin(t, pmPanelKey); m != 0o600 {
		t.Errorf("panel.key iznine DOKUNULMAMALI: %o", m)
	}
	durum, genel, _, bitis := pmDurum(t, db, pmAd)
	if durum != "aktif" || genel != "" || !bitis.Valid {
		t.Fatalf("DB: %s %q %v", durum, genel, bitis)
	}
	// İdempotent: ikinci döngü LE'ye gitmez, nginx'e dokunmaz.
	s.acme, s.cagrilar = nil, nil
	sonuc = pmDongu(context.Background(), db)
	if len(s.acme) != 0 || len(s.cagrilar) != 0 || len(sonuc.Etkin) != 1 {
		t.Fatalf("ikinci döngü no-op olmalı: acme=%v nginx=%v sonuc=%+v", s.acme, s.cagrilar, sonuc)
	}
}

func TestPanelMarka_StagingIsareti(t *testing.T) {
	db := pmTestDB(t)
	s := pmTestKur(t)
	var sunucu string
	ic := pmAcmeKomut
	pmAcmeKomut = func(ctx context.Context, arg ...string) ([]byte, error) {
		if arg[0] == "--issue" {
			sunucu = argDeger(arg, "--server")
		}
		return ic(ctx, arg...)
	}
	_ = os.WriteFile(pmStagingIsaret, nil, 0o644)
	pmDongu(context.Background(), db)
	if sunucu != "letsencrypt_test" {
		t.Fatalf("staging işareti varken sunucu %q", sunucu)
	}
	_ = s
}

func TestPanelMarka_DNSOnKosulu(t *testing.T) {
	db := pmTestDB(t)
	s := pmTestKur(t)
	s.dns[pmAd] = false
	sonuc := pmDongu(context.Background(), db)
	if s.acmeSay("--issue") != 0 {
		t.Fatalf("DNS bu sunucuya değilken LE'ye gidildi: %v", s.acme)
	}
	if durum, genel, _, _ := pmDurum(t, db, pmAd); durum != "hata" || genel != pmMsjDNS || sonuc.Hatalar[pmAd] != pmMsjDNS {
		t.Fatalf("DB: %s %q", durum, genel)
	}
	if marka.PanelConfDegiskenli(oku(t, kapiVhostYolu)) {
		t.Error("sertifikasız ad için _panel.conf değişmemeli")
	}
}

func TestPanelMarka_AcmeHatasiConfYazmazVeGeriCekilir(t *testing.T) {
	db := pmTestDB(t)
	s := pmTestKur(t)
	s.acmeHata[pmAd] = true
	pmDongu(context.Background(), db)
	if conf := oku(t, kapiVhostYolu); conf != pmTestConf {
		t.Fatalf("acme hatasında _panel.conf DEĞİŞMEMELİ:\n%s", conf)
	}
	dosyaYok(t, pmSNIYolu)
	dosyaYok(t, pmIncYolu)
	dosyaYok(t, pmCrt(pmAd))
	durum, genel, ayrinti, _ := pmDurum(t, db, pmAd)
	if durum != "hata" || genel != pmMsjAcme || !strings.Contains(ayrinti, "Verify error") {
		t.Fatalf("DB: %s %q %q", durum, genel, ayrinti)
	}
	if strings.Contains(genel, "http://") {
		t.Error("bayiye giden genel gerekçe ham acme çıktısı taşımamalı")
	}
	// Geri çekilme: hemen ikinci döngü LE'ye GİTMEZ.
	s.acme = nil
	pmDongu(context.Background(), db)
	if s.acmeSay("--issue") != 0 {
		t.Fatalf("geri çekilmede yeniden LE isteği: %v", s.acme)
	}
	// Süre dolunca dener (ve DNS düzelmiş, acme başarılı → aktif).
	eskiSimdi := pmSimdi
	t.Cleanup(func() { pmSimdi = eskiSimdi })
	pmSimdi = func() time.Time { return time.Now().Add(11 * time.Minute) }
	s.acmeHata[pmAd] = false
	pmDongu(context.Background(), db)
	if d, _, _, _ := pmDurum(t, db, pmAd); d != "aktif" {
		t.Fatalf("geri çekilme bitince etkinleşmeliydi: %s (acme=%v)", d, s.acme)
	}
}

func TestPanelMarka_NginxTBasarisizHepsiniGeriAlir(t *testing.T) {
	db := pmTestDB(t)
	s := pmTestKur(t)
	s.sniTHata = true
	sonuc := pmDongu(context.Background(), db)
	if sonuc.Uygulama {
		t.Fatal("nginx -t düşerken uygulama başarılı sayıldı")
	}
	if conf := oku(t, kapiVhostYolu); conf != pmTestConf {
		t.Fatalf("_panel.conf BAYT BAYT geri alınmalıydı:\n%s", conf)
	}
	dosyaYok(t, pmSNIYolu)
	dosyaYok(t, pmIncYolu)
	if s.rSayac != 1 { // yalnız 1. adımın :80 vhost'u yüklendi; -t geçmeyen SNI asla
		t.Errorf("-t geçmeyen yapılandırma reload edildi: %d reload, %v", s.rSayac, s.cagrilar)
	}
	if d, g, _, _ := pmDurum(t, db, pmAd); d != "hata" || g != pmMsjNginx {
		t.Fatalf("DB: %s %q", d, g)
	}
	// Sertifika dosyası tut kümesinde: silinmez (sonraki döngü LE'ye gitmeden yeniden dener).
	if _, ok := pmSertGecerli(pmAd); !ok {
		t.Error("nginx hatasında alınmış sertifika korunmalı")
	}
}

// 🔴 Kilitlenme koruması: değişiklikten sonra varsayılan (SNI'sız) el sıkışma bozulursa
// (worker anahtarı okuyamıyor, SELinux...) her şey geri alınır.
func TestPanelMarka_VarsayilanTLSBozulursaGeriAlir(t *testing.T) {
	db := pmTestDB(t)
	s := pmTestKur(t)
	s.tlsHata = func(sni string) error {
		if conf, _ := os.ReadFile(kapiVhostYolu); sni == "" && marka.PanelConfDegiskenli(string(conf)) {
			return errors.New("handshake failure")
		}
		return nil
	}
	sonuc := pmDongu(context.Background(), db)
	if !sonuc.GeriAlma || sonuc.Uygulama {
		t.Fatalf("geri alma bekleniyordu: %+v", sonuc)
	}
	if conf := oku(t, kapiVhostYolu); conf != pmTestConf {
		t.Fatalf("_panel.conf geri alınmalıydı:\n%s", conf)
	}
	dosyaYok(t, pmSNIYolu)
	if d, _, _, _ := pmDurum(t, db, pmAd); d != "hata" {
		t.Fatalf("DB: %s", d)
	}
}

func TestPanelMarka_SunulmayanAdHata(t *testing.T) {
	db := pmTestDB(t)
	s := pmTestKur(t)
	s.sunma[pmAd] = true
	pmDongu(context.Background(), db)
	if d, g, _, _ := pmDurum(t, db, pmAd); d != "hata" || g != pmMsjSunum {
		t.Fatalf("DB: %s %q", d, g)
	}
}

func TestPanelMarka_KaldirmaTemizligi(t *testing.T) {
	db := pmTestDB(t)
	s := pmTestKur(t)
	pmDongu(context.Background(), db)
	if d, _, _, _ := pmDurum(t, db, pmAd); d != "aktif" {
		t.Fatalf("ön koşul: %s", d)
	}
	if _, err := db.Exec(`DELETE FROM cp_marka_alanadi WHERE alan_adi=?`, pmAd); err != nil {
		t.Fatal(err)
	}
	s.acme = nil
	sonuc := pmDongu(context.Background(), db)
	if !sonuc.Uygulama || len(sonuc.Etkin) != 0 {
		t.Fatalf("%+v", sonuc)
	}
	if conf := oku(t, kapiVhostYolu); conf != pmTestConf {
		t.Fatalf("son ad gidince _panel.conf statiğe BAYT BAYT dönmeli:\n%s", conf)
	}
	for _, y := range []string{pmSNIYolu, pmIncYolu, kapiMarkaAcmeYolu, pmCrt(pmAd), pmKey(pmAd), pmVarsCrt, pmVarsKey, filepath.Join(pmAcmeHome, pmAd+"_ecc")} {
		dosyaYok(t, y)
	}
	if s.acmeSay("--remove "+pmAd) != 1 {
		t.Errorf("acme yenileme kaydı kaldırılmadı: %v", s.acme)
	}
	if _, err := os.Stat(filepath.Join(pmAcmeHome, "panel.kok.test_ecc", "panel.kok.test.conf")); err != nil {
		t.Fatalf("🔴 panelin KENDİ acme kaydı silindi: %v", err)
	}
	if _, err := os.Stat(pmPanelKey); err != nil {
		t.Fatal("panel.key silindi")
	}
}

func TestPanelMarka_IzinKapaninca(t *testing.T) {
	db := pmTestDB(t)
	s := pmTestKur(t)
	pmDongu(context.Background(), db)
	if _, err := db.Exec(`UPDATE cp_marka SET alanadi_izni=0 WHERE reseller_id=7`); err != nil {
		t.Fatal(err)
	}
	pmDongu(context.Background(), db)
	if conf := oku(t, kapiVhostYolu); conf != pmTestConf {
		t.Fatalf("izin kapanınca nginx'ten çıkmalı:\n%s", conf)
	}
	dosyaYok(t, pmCrt(pmAd))
	// Yeniden açılınca: sertifika yok → (DNS hâlâ burada) yeniden alınır.
	if _, err := db.Exec(`UPDATE cp_marka SET alanadi_izni=1 WHERE reseller_id=7`); err != nil {
		t.Fatal(err)
	}
	s.acme = nil
	sonuc := pmDongu(context.Background(), db)
	if s.acmeSay("--issue "+pmAd) != 1 || len(sonuc.Etkin) != 1 {
		t.Fatalf("yeniden açılışta sertifika alınmalı: %v %+v", s.acme, sonuc)
	}
}

func TestPanelMarka_PanelAdiVeKotuAdAsla(t *testing.T) {
	db := pmTestDB(t)
	s := pmTestKur(t)
	// Panelin kendi adı (sonradan çakışma) + DB'ye elle yazılmış enjeksiyon denemesi.
	for _, q := range []string{
		`DELETE FROM cp_marka_alanadi`,
		`INSERT INTO cp_marka_alanadi (alan_adi, reseller_id, dogrulama, durum) VALUES ('panel.kok.test',7,'0123456789abcdef0123456789abcdef','ssl_bekliyor')`,
		"INSERT INTO cp_marka_alanadi (alan_adi, reseller_id, dogrulama, durum) VALUES ('x.test;\\n}\\nserver{listen 1',8,'0123456789abcdef0123456789abcdef','aktif')",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s.dns["panel.kok.test"] = true
	pmDongu(context.Background(), db)
	if len(s.acme) != 0 {
		t.Fatalf("panel adı / kötü ad için acme çağrıldı: %v", s.acme)
	}
	if conf := oku(t, kapiVhostYolu); conf != pmTestConf {
		t.Fatalf("_panel.conf değişmemeli:\n%s", conf)
	}
	dosyaYok(t, kapiMarkaAcmeYolu)
	if d, g, _, _ := pmDurum(t, db, "panel.kok.test"); d != "hata" || g != pmMsjAdKotu {
		t.Fatalf("panel adı: %s %q", d, g)
	}
}

// Döngünün bayat kararı, bu arada silinip yeniden eklenen satırı EZMEZ.
func TestPanelMarka_BayatKararEzmez(t *testing.T) {
	db := pmTestDB(t)
	pmTestKur(t)
	eski := pmSatir{ad: pmAd, rid: 7, durum: "ssl_bekliyor", jeton: "ffffffffffffffffffffffffffffffff", izin: true}
	pmDurumYaz(context.Background(), db, eski, "aktif", "", "", nil)
	if d, _, _, _ := pmDurum(t, db, pmAd); d != "ssl_bekliyor" {
		t.Fatalf("farklı jetonlu satır güncellendi: %s", d)
	}
	eski.jeton = "0123456789abcdef0123456789abcdef"
	eski.durum = "dns_bekliyor" // bayi bu arada yeniden ekledi
	pmDurumYaz(context.Background(), db, eski, "aktif", "", "", nil)
	if d, _, _, _ := pmDurum(t, db, pmAd); d != "ssl_bekliyor" {
		t.Fatalf("farklı durumdaki satır güncellendi: %s", d)
	}
}

// ── DB'siz ──────────────────────────────────────────────────────────────────

func TestPanelMarka_GeriCekilmeUstel(t *testing.T) {
	pmGeriMu.Lock()
	pmGeri = map[string]pmGeriKayit{}
	pmGeriMu.Unlock()
	t.Cleanup(func() { pmGeriMu.Lock(); pmGeri = map[string]pmGeriKayit{}; pmGeriMu.Unlock() })
	beklenen := []time.Duration{10 * time.Minute, 30 * time.Minute, 90 * time.Minute, 270 * time.Minute, 810 * time.Minute, 24 * time.Hour, 24 * time.Hour}
	for i, b := range beklenen {
		pmGeriBasarisiz("a.test")
		g := pmGeri["a.test"]
		if g.sonraki.Sub(g.son) != b {
			t.Errorf("%d. başarısızlık: bekleme %v, beklenen %v", i+1, g.sonraki.Sub(g.son), b)
		}
	}
	if pmGeriIzinli("a.test") {
		t.Error("geri çekilmedeyken izin verildi")
	}
	// Yeni dürtme son denemeden 15 dk geçmeden sıfırlamaz.
	PanelMarkaDurt("a.test")
	if pmGeriIzinli("a.test") {
		t.Error("15 dk dolmadan dürtme geri çekilmeyi sıfırladı")
	}
	select {
	case <-pmDurtKanal:
	default:
	}
}

func TestKapiHeal_MarkaAcmeYonlendirmesi(t *testing.T) {
	a := rolAyar()
	a.EskiPortlar, a.EskiBitis = []int{8443}, 5000
	kapiTestKur(t, kapiTestCanli, a)
	v, err := marka.AcmeVhost80([]string{"panel.a.test", "panel.b.test"}, 8443)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kapiMarkaAcmeYolu, []byte(v), 0o640); err != nil {
		t.Fatal(err)
	}
	kapiUzlastir(1000)
	if c := oku(t, kapiMarkaAcmeYolu); strings.Count(c, "https://$server_name:23003$request_uri;") != 2 {
		t.Fatalf("bayi :80 yönlendirmesi müşteri kapısına gitmeli:\n%s", c)
	}
}

func TestNginxUygula_SilVeGeriAl(t *testing.T) {
	s := kapiTestKur(t, pmTestConf, nil)
	yol := filepath.Join(s.dir, "silinecek.conf")
	if err := os.WriteFile(yol, []byte("# eski\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	d := []nginxDosya{{yol: yol, eski: []byte("# eski\n"), sil: true, ozel: true}}
	if ok, _ := kapiNginxUygula(d, nil, false); !ok {
		t.Fatal("silme uygulanmadı")
	}
	dosyaYok(t, yol)
	// Geri alma: -t düşerse silinen dosya geri yazılır (özel izinle).
	if err := os.WriteFile(yol, []byte("# eski\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	s.nginxT = func(n int) error {
		if _, err := os.Stat(yol); err != nil {
			return errors.New("emerg")
		}
		return nil
	}
	if ok, _ := kapiNginxUygula(d, nil, false); ok {
		t.Fatal("-t düşerken silme başarılı sayıldı")
	}
	if b, err := os.ReadFile(yol); err != nil || string(b) != "# eski\n" || izin(t, yol) != 0o640 {
		t.Fatalf("silinen dosya geri yazılmadı: %q %v", b, err)
	}
	// Var olmayan dosyayı silmek değişiklik değildir.
	s.cagrilar = nil
	if ok, _ := kapiNginxUygula([]nginxDosya{{yol: filepath.Join(s.dir, "yok.conf"), sil: true}}, nil, false); !ok || len(s.cagrilar) != 0 {
		t.Fatalf("no-op silme nginx'e dokundu: %v", s.cagrilar)
	}
}
