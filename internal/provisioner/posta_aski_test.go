package provisioner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Kurucunun ÖNCEKİ sürümünün yazdığı dosyalar (181'de ölçülen biçim; parola sahte).
const postaEskiSQLConf = "driver = mysql\n" +
	"connect = host=127.0.0.1 dbname=mailserver user=mailuser password=SAHTE\n" +
	"default_pass_scheme = SHA512-CRYPT\n" +
	"password_query = SELECT email as user, password FROM virtual_users WHERE email='%u' AND aktif=1\n" +
	"user_query = SELECT '/var/vmail/%d/%n' AS home, 5000 AS uid, 5000 AS gid, CONCAT('*:bytes=', COALESCE(quota_bytes,0)) AS quota_rule FROM virtual_users WHERE email='%u' AND aktif=1\n"

const postaEskiWebmail = `# GirginOSPanel Mail — webmail tek-tık giriş için MASTER kullanıcı.
# OTOMATİK ÜRETİLDİ. "kutu@domain*gospmaster" biçimiyle giriş yapılır.
auth_master_user_separator = *
passdb {
  driver = passwd-file
  args = /etc/dovecot/gosp-master-users
  master = yes
}
`

func satirDegeri(t *testing.T, metin, anahtar string) string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^`+anahtar+` = (.*)$`).FindAllStringSubmatch(metin, -1)
	if len(m) != 1 {
		t.Fatalf("%s tam bir kez olmalı: %d\n%s", anahtar, len(m), metin)
	}
	return m[0][1]
}

func TestPostaSQLConfDonustur_YalnizPasswordQuery(t *testing.T) {
	yeni, degisti, err := postaSQLConfDonustur(postaEskiSQLConf)
	if err != nil || !degisti {
		t.Fatalf("eski sorgu dönüştürülmeli: %v %v", degisti, err)
	}
	pq := satirDegeri(t, yeni, "password_query")
	if !strings.Contains(pq, "d.askida=0") || !strings.Contains(pq, "JOIN virtual_domains d ON d.id=u.domain_id") || !strings.Contains(pq, "u.aktif=1") {
		t.Fatalf("password_query askı koşulu içermeli: %s", pq)
	}
	// 🔴 user_query (LMTP teslimi) aynen kalmalı, askı koşulu OLMAMALI.
	uq := satirDegeri(t, yeni, "user_query")
	if uq != satirDegeri(t, postaEskiSQLConf, "user_query") || strings.Contains(uq, "askida") {
		t.Fatalf("user_query değişmemeli / askı içermemeli: %s", uq)
	}
	// Parola satırı ve öteki satırlar korunur.
	if strings.Replace(yeni, "password_query = "+PostaPasswordQuery, "", 1) !=
		strings.Replace(postaEskiSQLConf, "password_query = "+postaEskiPasswordQuery[0], "", 1) {
		t.Fatalf("password_query dışında satır değişmemeli:\n%s", yeni)
	}
	// İdempotent.
	if y2, d2, err := postaSQLConfDonustur(yeni); err != nil || d2 || y2 != yeni {
		t.Fatalf("ikinci dönüşüm değişiklik yapmamalı: %v %v", d2, err)
	}
}

func TestPostaSQLConfDonustur_TaninmayanaDokunmaz(t *testing.T) {
	for _, ic := range []string{
		"driver = mysql\n",
		strings.Replace(postaEskiSQLConf, "AND aktif=1\nuser_query", "AND aktif=1 AND x=1\nuser_query", 1),
		postaEskiSQLConf + "password_query = SELECT 1\n",
	} {
		if _, _, err := postaSQLConfDonustur(ic); !errors.Is(err, ErrPostaTaninmayan) {
			t.Fatalf("tanınmayan yapılandırma reddedilmeli: %v\n%s", err, ic)
		}
	}
}

func TestPostaWebmailDonustur_PassYes(t *testing.T) {
	yeni, degisti, err := postaWebmailDonustur(postaEskiWebmail)
	if err != nil || !degisti {
		t.Fatalf("pass = yes eklenmeli: %v %v", degisti, err)
	}
	if !strings.Contains(yeni, "  master = yes\n  pass = yes\n}") {
		t.Fatalf("pass = yes master passdb bloğunda olmalı:\n%s", yeni)
	}
	if y2, d2, err := postaWebmailDonustur(yeni); err != nil || d2 || y2 != yeni {
		t.Fatalf("idempotent olmalı: %v %v", d2, err)
	}
	// pass = no → yes.
	hayir := strings.Replace(postaEskiWebmail, "  master = yes\n", "  master = yes\n  pass = no\n", 1)
	if y, d, err := postaWebmailDonustur(hayir); err != nil || !d || !strings.Contains(y, "  pass = yes\n") || strings.Contains(y, "pass = no") {
		t.Fatalf("pass = no düzeltilmeli: %v %v\n%s", d, err, y)
	}
	if _, _, err := postaWebmailDonustur("auth_master_user_separator = *\n"); !errors.Is(err, ErrPostaTaninmayan) {
		t.Fatalf("master passdb yoksa dokunulmamalı: %v", err)
	}
}

type postaSahte struct {
	kolonVar, tabloYok bool
	doveconfHata       error
	reloadHata         error
	cagri              []string
}

func postaOrtam(t *testing.T, sqlConf string, webmail *string) (*postaSahte, string, string) {
	t.Helper()
	dir := t.TempDir()
	sy := filepath.Join(dir, "dovecot-sql.conf.ext")
	wy := filepath.Join(dir, "99-gosp-webmail.conf")
	if sqlConf != "" {
		if err := os.WriteFile(sy, []byte(sqlConf), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if webmail != nil {
		if err := os.WriteFile(wy, []byte(*webmail), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := &postaSahte{}
	eskiS, eskiW, eskiK := postaDovecotSQLYol, postaWebmailYol, postaKomut
	postaDovecotSQLYol, postaWebmailYol = sy, wy
	postaKomut = func(_ context.Context, stdin, ad string, args ...string) (string, error) {
		k := ad + " " + strings.Join(args, " ")
		switch {
		case ad == "mysql" && strings.HasPrefix(stdin, "SELECT"):
			s.cagri = append(s.cagri, "kolon-oku")
			switch {
			case s.tabloYok:
				return "0\t0", nil
			case s.kolonVar:
				return "1\t1", nil
			}
			return "1\t0", nil
		case ad == "mysql" && strings.HasPrefix(stdin, "ALTER TABLE mailserver.virtual_domains ADD COLUMN askida"):
			s.cagri = append(s.cagri, "kolon-ekle")
			s.kolonVar = true
			return "", nil
		case k == "doveconf -n":
			// doveconf çağrıldığında kolon mutlaka var olmalı (sıra).
			if !s.kolonVar {
				t.Fatalf("doveconf kolondan ÖNCE çağrıldı")
			}
			s.cagri = append(s.cagri, "doveconf")
			return "", s.doveconfHata
		case k == "systemctl reload dovecot":
			s.cagri = append(s.cagri, "reload")
			return "", s.reloadHata
		}
		t.Fatalf("beklenmeyen komut: %s", k)
		return "", nil
	}
	t.Cleanup(func() { postaDovecotSQLYol, postaWebmailYol, postaKomut = eskiS, eskiW, eskiK })
	return s, sy, wy
}

func dosya(t *testing.T, y string) string {
	t.Helper()
	b, err := os.ReadFile(y)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPostaAskiAltyapisi_EskiKurulumUzlasir(t *testing.T) {
	wm := postaEskiWebmail
	s, sy, wy := postaOrtam(t, postaEskiSQLConf, &wm)
	if PostaAskiEtkin() {
		t.Fatal("eski yapılandırma etkin sayılmamalı")
	}
	degisti, err := PostaAskiAltyapisi(context.Background())
	if err != nil || !degisti {
		t.Fatalf("uzlaştırma: %v %v", degisti, err)
	}
	if got := strings.Join(s.cagri, " "); got != "kolon-oku kolon-ekle doveconf reload" {
		t.Fatalf("sıra (kolon → doğrula → reload): %s", got)
	}
	if !strings.Contains(dosya(t, sy), PostaPasswordQuery) || !strings.Contains(dosya(t, wy), "pass = yes") {
		t.Fatal("dosyalar güncellenmeli")
	}
	if fi, _ := os.Stat(sy); fi.Mode().Perm() != 0o600 {
		t.Fatalf("parola içeren dosya 0600 kalmalı: %v", fi.Mode())
	}
	if y, _ := filepath.Glob(sy + ".gosp.*"); len(y) != 1 {
		t.Fatalf("yedek alınmalı: %v", y)
	}
	if !PostaAskiEtkin() {
		t.Fatal("uzlaştırma sonrası etkin olmalı")
	}
	// İkinci açılış: hiçbir şey değişmez, dovecot'a dokunulmaz.
	s.cagri = nil
	if d, err := PostaAskiAltyapisi(context.Background()); err != nil || d || strings.Join(s.cagri, " ") != "kolon-oku" {
		t.Fatalf("idempotent olmalı: %v %v %v", d, err, s.cagri)
	}
}

func TestPostaAskiAltyapisi_MailYoksaDokunmaz(t *testing.T) {
	s, _, _ := postaOrtam(t, "", nil)
	if d, err := PostaAskiAltyapisi(context.Background()); err != nil || d || len(s.cagri) != 0 {
		t.Fatalf("mail kurulu değilse hiçbir şey yapılmamalı: %v %v %v", d, err, s.cagri)
	}
}

func TestPostaAskiAltyapisi_WebmailYok(t *testing.T) {
	s, sy, _ := postaOrtam(t, postaEskiSQLConf, nil)
	s.kolonVar = true
	if d, err := PostaAskiAltyapisi(context.Background()); err != nil || !d {
		t.Fatalf("webmail yokken de sorgu düzeltilmeli: %v %v", d, err)
	}
	if !strings.Contains(dosya(t, sy), PostaPasswordQuery) || !PostaAskiEtkin() {
		t.Fatal("sorgu güncellenmeli, etkin olmalı")
	}
}

func TestPostaAskiAltyapisi_DoveconfDuserseGeriAlinir(t *testing.T) {
	wm := postaEskiWebmail
	s, sy, wy := postaOrtam(t, postaEskiSQLConf, &wm)
	s.doveconfHata = errors.New("exit 89")
	if _, err := PostaAskiAltyapisi(context.Background()); err == nil || !strings.Contains(err.Error(), "GERİ ALINDI") {
		t.Fatalf("hata + geri alma beklenirdi: %v", err)
	}
	if dosya(t, sy) != postaEskiSQLConf || dosya(t, wy) != postaEskiWebmail {
		t.Fatal("dosyalar eski hâline dönmeli")
	}
	if strings.Contains(strings.Join(s.cagri, " "), "reload") {
		t.Fatalf("doğrulama düşünce reload edilmemeli: %v", s.cagri)
	}
}

func TestPostaAskiAltyapisi_ReloadDuserseGeriAlinir(t *testing.T) {
	s, sy, _ := postaOrtam(t, postaEskiSQLConf, nil)
	s.reloadHata = errors.New("failed")
	if _, err := PostaAskiAltyapisi(context.Background()); err == nil {
		t.Fatal("hata beklenirdi")
	}
	if dosya(t, sy) != postaEskiSQLConf {
		t.Fatal("dosya eski hâline dönmeli")
	}
}

func TestPostaAskiAltyapisi_KolonYoksaSorguyaDokunmaz(t *testing.T) {
	s, sy, _ := postaOrtam(t, postaEskiSQLConf, nil)
	s.tabloYok = true
	if _, err := PostaAskiAltyapisi(context.Background()); !errors.Is(err, ErrPostaTaninmayan) {
		t.Fatalf("tablo yoksa hata: %v", err)
	}
	if dosya(t, sy) != postaEskiSQLConf {
		t.Fatal("kolon yokken password_query değişirse herkesin girişi düşerdi")
	}
}

func TestPostaAskiMetinEtkin_UserQueryAskiEtkinSayilmaz(t *testing.T) {
	yeni, _, _ := postaSQLConfDonustur(postaEskiSQLConf)
	bozuk := strings.Replace(yeni, "WHERE email='%u' AND aktif=1\n", "WHERE email='%u' AND aktif=1 AND askida=0\n", 1)
	if bozuk == yeni || postaAskiMetinEtkin(bozuk, nil) {
		t.Fatal("user_query'de askı koşulu (gelen postayı keser) etkin sayılmamalı")
	}
	wm := postaEskiWebmail
	if postaAskiMetinEtkin(yeni, &wm) {
		t.Fatal("webmail pass=yes yoksa etkin sayılmamalı")
	}
}
