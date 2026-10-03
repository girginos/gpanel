package tasima

// mail_test.go — H2: posta aktarımında kaynaktan gelen kutu adları, hedef yolları,
// sieve dosyaları, eklenti kimliği ve plan limitleri.
//
// Ağa ve canlı sisteme dokunulmaz: uzak kabuk sahte (postaKabuk), mail eklentisi
// geçici unix soketindeki sahte sunucu (mailEklentiSoket + cp_eklentiler.soket),
// /var/vmail yerine t.TempDir (vmailKok). DB'li test GOSP_TEST_GECICI_DSN ister.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"

	"girginospanel/internal/efektiflimit"
)

func TestMailYerelGecerli(t *testing.T) {
	for _, s := range []string{"info", "a", "0abc", "ok.user+etiket", "x-y_z", strings.Repeat("a", 64)} {
		if !mailYerelGecerli(s) {
			t.Errorf("geçerli ad reddedildi: %q", s)
		}
	}
	for _, s := range []string{"", ".", "..", "../komsu.test/kurban", "a..b", "ab..", "-bas", ".gizli",
		"_alt", "+arti", "x/y", `x\y`, "Buyuk", "bos luk", "a\nb", "a@b", "a\x00b", "ş",
		strings.Repeat("a", 65)} {
		if mailYerelGecerli(s) {
			t.Errorf("kötü ad kabul edildi: %q", s)
		}
	}
}

// mailTestKok — vmailKok'u geçici dizine çevirir.
func mailTestKok(t *testing.T) string {
	t.Helper()
	kok := filepath.Join(t.TempDir(), "vmail")
	if err := os.MkdirAll(kok, 0o755); err != nil {
		t.Fatal(err)
	}
	eski := vmailKok
	vmailKok = kok
	t.Cleanup(func() { vmailKok = eski })
	return kok
}

func mailYaz(t *testing.T, yol, icerik string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(yol), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yol, []byte(icerik), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMailHedefDizin(t *testing.T) {
	kok := mailTestKok(t)
	mailYaz(t, filepath.Join(kok, "komsu.test", "kurban", "cur", "1.eml"), "komsu")
	mailYaz(t, filepath.Join(kok, "alan.test", "var", "cur", "2.eml"), "var")
	mailYaz(t, filepath.Join(kok, "alan.test", "dosya"), "dosya")
	if err := os.Symlink("../komsu.test/kurban", filepath.Join(kok, "alan.test", "bag")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("komsu.test", filepath.Join(kok, "sembol.test")); err != nil {
		t.Fatal(err)
	}

	kabul := []struct{ alan, yerel string }{{"alan.test", "yeni"}, {"alan.test", "var"}, {"yok.test", "info"}}
	for _, c := range kabul {
		d, h, err := mailHedefDizin(c.alan, c.yerel)
		if err != nil {
			t.Errorf("%s/%s reddedildi: %v", c.alan, c.yerel, err)
			continue
		}
		if d != filepath.Join(kok, c.alan) || h != filepath.Join(kok, c.alan, c.yerel) {
			t.Errorf("%s/%s → %q %q", c.alan, c.yerel, d, h)
		}
	}

	ret := []struct{ alan, yerel string }{
		// Yol kuralı (dogrudanCocuk): tek ve kanonik bileşen değil.
		{"alan.test", ""}, {"alan.test", "."}, {"alan.test", ".."}, {"alan.test", "a/b"},
		{"alan.test", "a/../b"}, {"alan.test", "b/"}, {"alan.test", "./b"},
		{"..", "x"}, {"a/b", "x"}, {"", "x"},
		// Lstat: kutu dizini bağ / dosya, alan dizini bağ.
		{"alan.test", "bag"}, {"alan.test", "dosya"}, {"sembol.test", "info"},
	}
	for _, c := range ret {
		if d, h, err := mailHedefDizin(c.alan, c.yerel); err == nil {
			t.Errorf("%q/%q kabul edildi → %q %q", c.alan, c.yerel, d, h)
		} else if strings.Contains(err.Error(), kok) {
			t.Errorf("hata iletisi panel yolunu içeriyor: %v", err)
		}
	}
}

// ---- sahte mail eklentisi (geçici unix soket) ----

type sahteIstek struct {
	Host, Yontem, Yol, Sorgu, Rol, Uid, Sahip string
	Govde                                     map[string]any
}

type sahteEklenti struct {
	mu        sync.Mutex
	soket     string
	sonID     int
	domainler map[string]int
	kutular   map[string]bool
	istekler  []sahteIstek
}

func sahteMailEklenti(t *testing.T) *sahteEklenti {
	t.Helper()
	// Kısa yol: unix soket adı 108 baytla sınırlı (t.TempDir uzun olabilir).
	dizin, err := os.MkdirTemp("", "gm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dizin) })
	s := &sahteEklenti{soket: filepath.Join(dizin, "m.sock"), sonID: 100,
		domainler: map[string]int{}, kutular: map[string]bool{}}
	ln, err := net.Listen("unix", s.soket)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: s}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	eski := mailEklentiSoket
	mailEklentiSoket = s.soket
	t.Cleanup(func() { mailEklentiSoket = eski })
	return s
}

func (s *sahteEklenti) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var g map[string]any
	_ = json.NewDecoder(r.Body).Decode(&g)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.istekler = append(s.istekler, sahteIstek{Host: r.Host, Yontem: r.Method, Yol: r.URL.Path,
		Sorgu: r.URL.RawQuery, Rol: r.Header.Get("X-Gosp-Rol"), Uid: r.Header.Get("X-Gosp-Uid"),
		Sahip: r.Header.Get("X-Gosp-Sahip"), Govde: g})
	yaz := func(kod int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(kod)
		_ = json.NewEncoder(w).Encode(v)
	}
	switch {
	case r.Method == "GET" && r.URL.Path == "/domainler":
		l := []map[string]any{}
		for ad, id := range s.domainler {
			l = append(l, map[string]any{"id": id, "ad": ad, "kutu_sayisi": 0})
		}
		yaz(200, map[string]any{"domainler": l})
	case r.Method == "POST" && r.URL.Path == "/domainler":
		ad, _ := g["ad"].(string)
		if _, varMi := s.domainler[ad]; varMi {
			yaz(500, map[string]string{"hata": "işlem başarısız"})
			return
		}
		s.sonID++
		s.domainler[ad] = s.sonID
		yaz(201, map[string]any{"id": s.sonID, "ad": ad})
	case r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/limit"):
		yaz(200, map[string]string{"durum": "kaydedildi"})
	case r.Method == "GET" && r.URL.Path == "/hesaplar":
		l := []map[string]any{}
		for e := range s.kutular {
			if strconv.Itoa(s.domainler[e[strings.IndexByte(e, '@')+1:]]) == r.URL.Query().Get("domain") {
				l = append(l, map[string]any{"email": e})
			}
		}
		yaz(200, map[string]any{"hesaplar": l})
	case r.Method == "POST" && r.URL.Path == "/hesaplar":
		e, _ := g["email"].(string)
		switch {
		case strings.HasPrefix(e, "dolu@"):
			yaz(409, map[string]string{"hata": "posta kutusu limiti dolu — planı yükseltin"})
		case s.kutular[e]:
			yaz(500, map[string]string{"hata": "işlem başarısız"})
		default:
			s.kutular[e] = true
			yaz(201, map[string]any{"id": 1, "email": e})
		}
	default:
		yaz(404, map[string]string{"hata": "yok"})
	}
}

func (s *sahteEklenti) al() []sahteIstek {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.istekler
	s.istekler = nil
	return out
}

// ---- sahte (düşman) kaynak ----

// postaKabuk — altın testin kaydedicisi + dosya yazan RsyncCek: kaynağın gönderdiği
// içerik verilen hedefe düşer (yol dışarı taşarsa komşu ağacın özeti değişir).
type postaKabuk struct {
	*kaydedici
	rsyncler [][]string
}

func (p *postaKabuk) RsyncCek(_ context.Context, uzakYol, yerelYol string, ekstra ...string) (string, error) {
	p.rsyncler = append(p.rsyncler, append([]string{uzakYol, yerelYol}, ekstra...))
	yol := filepath.Join(yerelYol, "cur", "zarar.eml")
	if err := os.MkdirAll(filepath.Dir(yol), 0o700); err != nil {
		return "", err
	}
	return "", os.WriteFile(yol, []byte("düşman içerik"), 0o600)
}

func postaKaynak(adlar, parolalar string) (*Kaynak, *postaKabuk) {
	pk := &postaKabuk{kaydedici: &kaydedici{yanit: func(komut string) string {
		switch {
		case strings.Contains(komut, "SELECT m.mail_name"):
			return adlar
		case strings.Contains(komut, "mail_auth_view"):
			return parolalar
		}
		return ""
	}}}
	return &Kaynak{Tip: "plesk", Host: "192.0.2.10", Port: 22, Kullanici: "root", Parola: "kaynak-parola", kabuk: pk}, pk
}

// agacOzet — ağacın (yol, tür/mod, sahip, boyut, içerik/bağ hedefi) sha256'sı.
func agacOzet(t *testing.T, kok string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(kok, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(kok, p)
		st := fi.Sys().(*syscall.Stat_t)
		fmt.Fprintf(h, "%s|%v|%d|%d|%d\n", rel, fi.Mode(), st.Uid, st.Gid, fi.Size())
		switch {
		case fi.Mode().IsRegular():
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			h.Write(b)
		case fi.Mode()&fs.ModeSymlink != 0:
			l, _ := os.Readlink(p)
			h.Write([]byte(l))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func mailIstekleri(ist []sahteIstek, host string) []sahteIstek {
	var out []sahteIstek
	for _, i := range ist {
		if i.Host == host {
			out = append(out, i)
		}
	}
	return out
}

func TestMailAktarGuvenilmezKaynak(t *testing.T) {
	db := ta_db(t)
	kok := mailTestKok(t)
	ek := sahteMailEklenti(t)
	for _, q := range []string{
		`ALTER TABLE domains ADD COLUMN plan_id BIGINT UNSIGNED NULL`,
		`CREATE TABLE service_plans (id BIGINT UNSIGNED PRIMARY KEY, max_email INT NOT NULL DEFAULT 0,
			saatlik_mail_limiti INT NOT NULL DEFAULT 0, mail_kutu_kota_mb INT NOT NULL DEFAULT 0)`,
		efektiflimit.TabloDDL, // 0120: efektif posta limiti (plan ⊕ üst yazım)
		`CREATE TABLE cp_eklentiler (ad VARCHAR(64) PRIMARY KEY, aktif TINYINT NOT NULL DEFAULT 0,
			soket VARCHAR(255) NOT NULL DEFAULT '')`,
		`INSERT INTO service_plans VALUES (3, 9, 50, 250)`,
		`INSERT INTO domains (id, alan_adi, reseller_id, plan_id) VALUES (30,'posta-bayi.test',7,3)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("şema: %v\n%s", err, q)
		}
	}
	if _, err := db.Exec(`INSERT INTO cp_eklentiler VALUES ('mail',1,?)`, ek.soket); err != nil {
		t.Fatal(err)
	}
	h := &Handlers{DB: db}
	ctx := context.Background()
	gunluk := func(string, ...any) {}

	t.Run("bayi-dusman-kaynak", func(t *testing.T) {
		const alan = "posta-bayi.test"
		// Komşular: başka alanın kutusu + aynı alanda kaynakta OLMAYAN kutu. "bag" kutu
		// dizini komşuya bağ; "mevcut" eklentide zaten var olan kutu.
		mailYaz(t, filepath.Join(kok, "komsu.test", "kurban", "cur", "1.eml"), "komsu gizli")
		mailYaz(t, filepath.Join(kok, alan, "komsu", "cur", "2.eml"), "ayni alan komsu")
		mailYaz(t, filepath.Join(kok, alan, "mevcut", "cur", "eski.eml"), "eski")
		if err := os.Symlink("../komsu.test/kurban", filepath.Join(kok, alan, "bag")); err != nil {
			t.Fatal(err)
		}
		ek.mu.Lock()
		ek.domainler[alan] = 5
		ek.kutular["mevcut@"+alan] = true
		ek.mu.Unlock()
		kurbanOnce := agacOzet(t, filepath.Join(kok, "komsu.test"))
		komsuOnce := agacOzet(t, filepath.Join(kok, alan, "komsu"))

		kotuAdlar := []string{"../komsu.test/kurban", "..", ".", "a..b", "-bas", "x/y", "bos luk", strings.Repeat("a", 65)}
		adlar := "info\nOK.User+etiket\nmevcut\ndolu\nbag\n" + strings.Join(kotuAdlar, "\n") + "\n"
		k, pk := postaKaynak(adlar, "info@"+alan+" gizliparola1\nok.user+etiket@"+alan+" gizliparola2\n")

		n, uyar, err := h.mailAktar(ctx, k, alan, gunluk)
		if err != nil {
			t.Fatalf("mailAktar: %v", err)
		}
		if n != 3 {
			t.Errorf("taşınan = %d, beklenen 3 (info, ok.user+etiket, mevcut); uyarılar: %q", n, uyar)
		}

		// Komşu ağaçlar bayt bayt aynı.
		if agacOzet(t, filepath.Join(kok, "komsu.test")) != kurbanOnce {
			t.Error("başka alanın kutu ağacı DEĞİŞTİ")
		}
		if agacOzet(t, filepath.Join(kok, alan, "komsu")) != komsuOnce {
			t.Error("aynı alandaki komşu kutu ağacı DEĞİŞTİ")
		}
		if fi, err := os.Lstat(filepath.Join(kok, alan, "bag")); err != nil || fi.Mode()&fs.ModeSymlink == 0 {
			t.Errorf("bag bağı değişti: %v", err)
		}

		// Yalnız geçerli adlar için rsync; hedef alan dizininin doğrudan çocuğu;
		// sieve ve dovecot iç dosyaları hariç.
		haric := []string{"--exclude=dovecot*", "--exclude=maildirsize",
			"--exclude=.dovecot.sieve", "--exclude=.dovecot.svbin", "--exclude=sieve"}
		var rsyncHedef []string
		for _, r := range pk.rsyncler {
			rsyncHedef = append(rsyncHedef, r[1])
			yerel := filepath.Base(r[1])
			if r[0] != "/var/qmail/mailnames/"+alan+"/"+yerel+"/Maildir/" {
				t.Errorf("uzak yol %q", r[0])
			}
			if !reflect.DeepEqual(r[2:], haric) {
				t.Errorf("rsync hariç listesi = %q, beklenen %q", r[2:], haric)
			}
		}
		sort.Strings(rsyncHedef)
		beklenenHedef := []string{
			filepath.Join(kok, alan, "info") + "/",
			filepath.Join(kok, alan, "mevcut") + "/",
			filepath.Join(kok, alan, "ok.user+etiket") + "/",
		}
		if !reflect.DeepEqual(rsyncHedef, beklenenHedef) {
			t.Errorf("rsync hedefleri = %q, beklenen %q", rsyncHedef, beklenenHedef)
		}

		// Eklenti: yalnız bayi kimliği + yalnız bu alan; kutu açmadan limit senkronu;
		// kota plandan; var olan kutuya POST yok; limiti dolu kutuya veri yok.
		tum := ek.al()
		ist := mailIstekleri(tum, "mail")
		if len(ist) == 0 {
			t.Fatal("eklentiye istek gitmedi")
		}
		ilkPost, limit := -1, -1
		var postlanan []string
		for i, x := range tum {
			if x.Yontem == "PUT" && strings.HasSuffix(x.Yol, "/limit") && limit < 0 {
				limit = i
				if x.Yol != "/domainler/5/limit" || x.Govde["kutu_limiti"] != float64(9) ||
					x.Govde["saatlik_limiti"] != float64(50) || x.Govde["kota_mb"] != float64(250) {
					t.Errorf("limit senkronu = %s %v", x.Yol, x.Govde)
				}
			}
			if x.Host == "mail" && x.Yontem == "POST" && x.Yol == "/hesaplar" {
				if ilkPost < 0 {
					ilkPost = i
				}
				postlanan = append(postlanan, fmt.Sprint(x.Govde["email"]))
				if x.Govde["quota_mb"] != float64(250) {
					t.Errorf("quota_mb = %v, beklenen plan kotası 250", x.Govde["quota_mb"])
				}
			}
		}
		if limit < 0 || ilkPost < 0 || limit > ilkPost {
			t.Errorf("limit senkronu kutu açmadan önce değil (limit=%d, ilk POST=%d)", limit, ilkPost)
		}
		sort.Strings(postlanan)
		if want := []string{"dolu@" + alan, "info@" + alan, "ok.user+etiket@" + alan}; !reflect.DeepEqual(postlanan, want) {
			t.Errorf("POST /hesaplar = %q, beklenen %q", postlanan, want)
		}
		for _, x := range ist {
			if x.Rol != "reseller" || x.Uid != "7" || x.Sahip != alan {
				t.Errorf("%s %s kimlik = rol %q uid %q sahip %q", x.Yontem, x.Yol, x.Rol, x.Uid, x.Sahip)
			}
		}

		// Uyarılar: her kötü ad, bağ ve limiti dolu kutu görünür; panel yolu yok.
		gecersiz := 0
		var bagUyari, doluUyari bool
		for _, u := range uyar {
			if strings.Contains(u, kok) || strings.Contains(u, "/var/vmail") {
				t.Errorf("uyarı panel yolu içeriyor: %q", u)
			}
			switch {
			case strings.Contains(u, "geçersiz kutu adı"):
				gecersiz++
			case strings.Contains(u, "bag@"+alan+" hedef dizini güvenli değil"):
				bagUyari = true
			case strings.Contains(u, "dolu@"+alan+" oluşturulamadı (409: posta kutusu limiti dolu"):
				doluUyari = true
			}
		}
		if gecersiz != len(kotuAdlar) || !bagUyari || !doluUyari {
			t.Errorf("uyarılar eksik (geçersiz=%d/%d bag=%v dolu=%v): %q", gecersiz, len(kotuAdlar), bagUyari, doluUyari, uyar)
		}
	})

	t.Run("kok-domain-admin", func(t *testing.T) {
		const alan = "kokdomain.test" // ta_db: reseller_id=0, plan yok
		k, pk := postaKaynak("info\n", "")
		n, _, err := h.mailAktar(ctx, k, alan, gunluk)
		if err != nil || n != 1 || len(pk.rsyncler) != 1 {
			t.Fatalf("n=%d rsync=%d err=%v", n, len(pk.rsyncler), err)
		}
		ist := mailIstekleri(ek.al(), "mail")
		var domainPost bool
		for _, x := range ist {
			if x.Rol != "admin" || x.Uid != "" || x.Sahip != "" {
				t.Errorf("%s %s kimlik = rol %q uid %q sahip %q", x.Yontem, x.Yol, x.Rol, x.Uid, x.Sahip)
			}
			if x.Yontem == "POST" && x.Yol == "/domainler" {
				domainPost = true
			}
			if x.Yontem == "POST" && x.Yol == "/hesaplar" && x.Govde["quota_mb"] != float64(0) {
				t.Errorf("plansız domainde quota_mb = %v", x.Govde["quota_mb"])
			}
		}
		if !domainPost {
			t.Error("eklentide olmayan mail domaini oluşturulmadı")
		}
	})
}
