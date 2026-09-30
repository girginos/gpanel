package domains

// ssl_ilerleme_kapsam_test.go — bayi kapsamı (son ham hata yerleri):
//
//  1. SSL ilerleme kaydı (GET /domains/{id}/ssl/ilerleme, MusteriScope): adım
//     mesajlarına ve iş hatasına yazılan ham araç çıktısı (vhost render →
//     SUNUCU GENELİ `nginx -t`; webmail vhost'u; posta sunucusu; DKIM) bayiye ve
//     müşteriye DÖNMEZ. Kayıt, işi BAŞLATANDAN bağımsız olarak herkesçe okunur
//     (yönetici başlatır, bayi izler; bayi başlatır, yönetici izler) — bu yüzden
//     süzme OKURKEN, okuyanın rolüne göre yapılır. Yönetici metni birebir aynı.
//  2. Toplu durum (POST /domains/toplu/durum, AdminVeyaReseller): AskiUygula
//     hatası (vhost render → `nginx -t`) bayinin yanıtına da denetim kaydına da
//     girmez — yalnız sayaç döner, ayrıntı günlüğe.
//  3. Posta SSL'i (kiracının KENDİ alanı) sunucu GENELİ varsayılan (SNI'sız)
//     posta sertifikasına dokunmaz — bkz. TestPostaSSL_KiraciYolu_*.
//
// Dış etkili adımlar (sslSelfSigned, sslLetsEncrypt, posta/webmail/DKIM
// dikişleri, vhostYenidenCiz) sahte — ana makineye dokunulmaz. DB sahtedb.
// Yardımcılar (sh_sir, sh_tokenlar, sh_ham, sh_izler, sh_hataMetni):
// sunucu_hatasi_test.go.

import (
	"context"
	"database/sql"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/middleware"
	"girginospanel/internal/sahtedb"
)

// si_isBekle — id için SSL işi bitene dek bekler (en çok 5 sn).
func si_isBekle(t *testing.T, id int64) {
	t.Helper()
	son := time.Now().Add(5 * time.Second)
	for SSLSuruyor(id) {
		if time.Now().After(son) {
			t.Fatalf("SSL işi (domain=%d) bitmedi", id)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func si_temizle(t *testing.T, id int64) {
	t.Helper()
	t.Cleanup(func() {
		sslMu.Lock()
		delete(sslIsleri, id)
		sslMu.Unlock()
	})
}

// si_dikisler — SSL akışının tüm dış etkili adımlarını sahteye çevirir
// (varsayılan: hepsi başarılı; sertifika yolları var olmayan dosyalar).
func si_dikisler(t *testing.T) {
	t.Helper()
	e1, e2, e3, e4, e6, e7, e8 := sslSelfSigned, sslLetsEncrypt, mailSertAl, mailSertGonder,
		webmailVhostYaz, mailDKIMAl, mailDKIMYayinla
	t.Cleanup(func() {
		sslSelfSigned, sslLetsEncrypt, mailSertAl, mailSertGonder = e1, e2, e3, e4
		webmailVhostYaz, mailDKIMAl, mailDKIMYayinla = e6, e7, e8
	})
	yok := t.TempDir() + "/yok"
	sslSelfSigned = func(string, string, string, string) (string, string, error) { return yok + ".crt", yok + ".key", nil }
	sslLetsEncrypt = func(string, string, string, string) (string, string, error) { return yok + ".crt", yok + ".key", nil }
	mailSertAl = func(alan, _ string, _ []string) (string, string, []string, []string, error) {
		return yok + "-mail.crt", yok + "-mail.key", []string{"mail." + alan}, nil, nil
	}
	mailSertGonder = func(string, string, string) error { return nil }
	webmailVhostYaz = func(string, string, string) error { return nil }
	mailDKIMAl = func(string) (string, error) { return "v=DKIM1; k=rsa; p=AAAA", nil }
	mailDKIMYayinla = func(context.Context, *sql.DB, int64, string) error { return nil }
}

func si_db() (*sahtedb.DB, *sql.DB) {
	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT alan_adi, sistem_kullanici, php_surum, is_demo, COALESCE(web_backend",
		[]any{"bayialan.test", "c_bayialan", "8.3", int64(0), "php-fpm"})
	sd.Satirlar("SELECT aktif FROM cp_eklentiler", []any{int64(1)})
	return sd, db
}

// si_ilerleme — GET /domains/{id}/ssl/ilerleme (gerçek RequireAuth).
func si_ilerleme(t *testing.T, db *sql.DB, id, tok string) (string, SSLGoruntu) {
	t.Helper()
	r := chi.NewRouter()
	r.With(middleware.RequireAuth(sh_sir)).Get("/domains/{id}/ssl/ilerleme", (&Handlers{DB: db}).SSLIlerleme)
	req := httptest.NewRequest(http.MethodGet, "/domains/"+id+"/ssl/ilerleme", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ilerleme: 200 beklenirdi: %d %s", rec.Code, rec.Body.String())
	}
	var g SSLGoruntu
	if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil {
		t.Fatalf("ilerleme çözülemedi: %v", err)
	}
	return rec.Body.String(), g
}

func si_izYok(t *testing.T, neresi, metin string) {
	t.Helper()
	for _, iz := range sh_izler {
		if strings.Contains(metin, iz) {
			t.Errorf("%s: yabancı iz %q var: %s", neresi, iz, metin)
		}
	}
}

// Sertifika adımı hatası (vhost render → sunucu geneli `nginx -t`): işi kim
// başlatırsa başlatsın, bayi/müşteri okurken ham çıktı görmez; yönetici okurken
// öncekiyle birebir aynı metni görür.
func TestSSLIlerleme_SertifikaHatasi_OkuyanRolunaGoreSuzulur(t *testing.T) {
	tok := sh_tokenlar(t)
	for _, baslatan := range []string{"admin", "bayi", "müşteri"} {
		t.Run("baslatan="+baslatan, func(t *testing.T) {
			si_dikisler(t)
			sslSelfSigned = func(string, string, string, string) (string, string, error) { return "", "", sh_ham }
			si_temizle(t, 42)
			_, db := si_db()

			// İşi başlat: POST /domains/42/ssl/issue (self-signed).
			r := chi.NewRouter()
			r.With(middleware.RequireAuth(sh_sir)).Post("/domains/{id}/ssl/issue", (&Handlers{DB: db}).SSLIssue)
			req := httptest.NewRequest(http.MethodPost, "/domains/42/ssl/issue", strings.NewReader(`{"tip":"self-signed"}`))
			req.Header.Set("Authorization", "Bearer "+tok[baslatan])
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("issue: 202 beklenirdi: %d %s", rec.Code, rec.Body.String())
			}
			si_isBekle(t, 42)

			for _, okuyan := range []string{"bayi", "müşteri"} {
				govde, g := si_ilerleme(t, db, "42", tok[okuyan])
				si_izYok(t, okuyan+" ilerleme", govde)
				if g.Durum != "hata" || len(g.Adimlar) != 1 || g.Adimlar[0].Durum != "hata" {
					t.Fatalf("%s: hata durumlu tek adım beklenirdi: %s", okuyan, govde)
				}
				if !strings.Contains(g.Adimlar[0].Mesaj, "günlüğüne") || !strings.Contains(g.Hata, "günlüğüne") {
					t.Errorf("%s: genel mesaj beklenirdi: %s", okuyan, govde)
				}
			}
			_, g := si_ilerleme(t, db, "42", tok["admin"])
			if want := sslKirp(sh_ham.Error(), 800); len(g.Adimlar) != 1 || g.Adimlar[0].Mesaj != want {
				t.Errorf("yönetici adım metni birebir aynı kalmalı:\n  got: %+v\n want: %q", g.Adimlar, want)
			}
			if want := sslKirp(sh_ham.Error(), 400); g.Hata != want {
				t.Errorf("yönetici iş hatası birebir aynı kalmalı:\n  got: %q\n want: %q", g.Hata, want)
			}
		})
	}
}

// Posta SSL adımlarının UYARILARI (iş devam eder): ham ayrıntı (webmail vhost'u
// → `nginx -t`, posta eklentisi, DKIM/DNS) yalnız
// yöneticiye; bayi/müşteri öneki + kendi alt alanlarını görür.
func TestSSLIlerleme_PostaUyarilari_HamYalnizYoneticiye(t *testing.T) {
	tok := sh_tokenlar(t)
	ham := sh_ham.Error()
	durumlar := []struct {
		ad     string
		kur    func()
		admin  map[string]string // adım → yönetici metni (birebir)
		bayiEk map[string]string // adım → bayi metninde KALMASI gereken güvenli parça
	}{
		{
			ad: "mail-dogrula",
			kur: func() {
				mailSertAl = func(string, string, []string) (string, string, []string, []string, error) {
					return "", "", nil, []string{"smtp.bayialan.test"}, sh_ham
				}
			},
			admin:  map[string]string{"mail-dogrula": "posta sertifikası alınamadı: " + ham + " | atlanan: smtp.bayialan.test"},
			bayiEk: map[string]string{"mail-dogrula": "| atlanan: smtp.bayialan.test"},
		},
		{
			ad: "gonder-webmail-dkimal",
			kur: func() {
				mailSertGonder = func(string, string, string) error { return sh_ham }
				webmailVhostYaz = func(string, string, string) error { return sh_ham }
				mailDKIMAl = func(string) (string, error) { return "", sh_ham }
			},
			admin: map[string]string{
				"mail-kur": "sertifika alındı ama posta sunucusuna gönderilemedi: " + ham,
				"webmail":  "webmail vhost güncellenemedi: " + ham,
				"dkim-dns": "DKIM anahtarı eklentiden alınamadı: " + ham,
			},
		},
		{
			// (Eskiden bu durum "varsayılan (SNI-siz) güncellenemedi" uyarısını da
			// sınardı; kiracı akışı artık varsayılana hiç dokunmaz.)
			ad: "dkimyayinla",
			kur: func() {
				mailDKIMYayinla = func(context.Context, *sql.DB, int64, string) error { return sh_ham }
			},
			admin: map[string]string{
				"dkim-dns": "DKIM DNS'e yazılamadı: " + ham,
			},
		},
	}
	for i, d := range durumlar {
		t.Run(d.ad, func(t *testing.T) {
			id := int64(900 + i)
			si_dikisler(t)
			d.kur()
			si_temizle(t, id)
			_, db := si_db()
			(&Handlers{DB: db}).sslBaslat(id, "bayialan.test", "c_bayialan", "8.3", "php-fpm", "letsencrypt", true, nil)
			si_isBekle(t, id)
			ids := strconv.FormatInt(id, 10)

			adimlar := func(g SSLGoruntu) map[string]SSLAdim {
				m := map[string]SSLAdim{}
				for _, a := range g.Adimlar {
					m[a.Ad] = a
				}
				return m
			}
			for _, okuyan := range []string{"bayi", "müşteri"} {
				govde, g := si_ilerleme(t, db, ids, tok[okuyan])
				si_izYok(t, okuyan+" ilerleme", govde)
				am := adimlar(g)
				for ad := range d.admin {
					a, ok := am[ad]
					if !ok || a.Durum != "uyari" {
						t.Fatalf("%s: %s adımı uyarı olmalı: %s", okuyan, ad, govde)
					}
					if !strings.Contains(a.Mesaj, "günlüğüne") {
						t.Errorf("%s: %s adımında genel mesaj beklenirdi: %q", okuyan, ad, a.Mesaj)
					}
				}
				for ad, parca := range d.bayiEk {
					if !strings.Contains(am[ad].Mesaj, parca) {
						t.Errorf("%s: %s adımında kendi bilgisi (%q) kalmalı: %q", okuyan, ad, parca, am[ad].Mesaj)
					}
				}
			}
			_, g := si_ilerleme(t, db, ids, tok["admin"])
			am := adimlar(g)
			for ad, want := range d.admin {
				if got := am[ad].Mesaj; got != sslKirp(want, 800) {
					t.Errorf("yönetici %s metni birebir aynı kalmalı:\n  got: %q\n want: %q", ad, got, sslKirp(want, 800))
				}
			}
			if g.Durum != "tamam" {
				t.Errorf("uyarılar işi durdurmamalı: %+v", g)
			}
		})
	}
}

// Toplu durum (bayi): AskiUygula hatası (vhost render → sunucu geneli
// `nginx -t`) yanıta ve denetim kaydına girmez — yalnız sayaç.
func TestTopluDurum_AskiHatasi_YanitaVeDenetimeHamGirmez(t *testing.T) {
	sh_dikisler(t)
	tok := sh_tokenlar(t)
	for _, rol := range []string{"bayi", "admin"} {
		sd, db := sahtedb.Yeni()
		sd.Satirlar("SELECT COALESCE(reseller_id,0) FROM domains", []any{int64(7)})
		sd.Satirlar("SELECT alan_adi, sistem_kullanici, is_demo, COALESCE(php_surum",
			[]any{"bayialan.test", "c_bayialan", int64(0), "8.3", int64(0), ""})
		r := chi.NewRouter()
		r.With(middleware.RequireAuth(sh_sir)).Post("/domains/toplu/durum", (&Handlers{DB: db}).TopluDurum)
		req := httptest.NewRequest(http.MethodPost, "/domains/toplu/durum", strings.NewReader(`{"ids":[42],"durum":"pasif"}`))
		req.Header.Set("Authorization", "Bearer "+tok[rol])
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: 200 beklenirdi: %d %s", rol, rec.Code, rec.Body.String())
		}
		if !sd.Yurutuldu("UPDATE domains SET askida") || !sd.Yurutuldu("audit_log") {
			t.Fatalf("%s: askı uygulaması / denetim kaydı yoluna ulaşılmadı", rol)
		}
		var m map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		if m["atlanan"] != float64(1) || m["guncellenen"] != float64(0) {
			t.Fatalf("%s: render hatası atlanan sayılmalı: %s", rol, rec.Body.String())
		}
		for _, iz := range sh_izler {
			if strings.Contains(rec.Body.String(), iz) {
				t.Errorf("%s yanıtında yabancı iz %q var: %s", rol, iz, rec.Body.String())
			}
			if sd.Yurutuldu(iz) {
				t.Errorf("%s: denetim/DB kaydında yabancı iz %q var", rol, iz)
			}
		}
	}
}

// 🔴 GÜVENLİK REGRESYONU (kiracı → sunucu geneli durum). domains paketi
// müşteri/bayi kapsamlı uçları (MusteriScope arkasındaki /domains/{id}/ssl/…)
// barındırır; sunucunun VARSAYILAN (SNI'sız) posta sertifikasını
// (/etc/pki/mail/mail.crt+key — dovecot/postfix ve paylaşımlı webmail 443
// vhost'u, yani TÜM kiracılar) yazan hiçbir işleve başvuramaz. Eskiden
// sslBaslat'ın mail-kur adımı provisioner.MailVarsayilanKur'u başlatanın
// rolünü bilmeden çağırıyordu: varsayılan self-signed/süresi dolmuşsa kendi
// alanına LE posta SSL'i isteyen İLK kiracının mail.<alan> sertifikası
// sunucunun kimliği oluyor ve kalıcılaşıyordu. Varsayılanın tek kaynağı panel
// host sertifikasıdır (panelhost SSL akışı — AdminOnly — ve nöbetçi).
//
// Denetim yapısaldır (AST): paketin üretim dosyalarında provisioner'ın
// MailVarsayilan* işlevlerine (içe aktarma takma adı ne olursa olsun) ya da
// "/etc/pki/mail" yoluna tek bir başvuru bile olamaz.
func TestPostaSSL_KiraciYolu_SunucuVarsayilaninaBasvurmaz(t *testing.T) {
	const provYol = `"girginospanel/internal/provisioner"`
	girdiler, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	taranan := 0
	for _, g := range girdiler {
		ad := g.Name()
		if g.IsDir() || !strings.HasSuffix(ad, ".go") || strings.HasSuffix(ad, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, ad, nil, 0)
		if err != nil {
			t.Fatalf("%s ayrıştırılamadı: %v", ad, err)
		}
		taranan++
		yerel := map[string]bool{}
		for _, im := range f.Imports {
			if im.Path.Value != provYol {
				continue
			}
			if im.Name != nil {
				yerel[im.Name.Name] = true
			} else {
				yerel["provisioner"] = true
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && yerel[id.Name] && strings.HasPrefix(x.Sel.Name, "MailVarsayilan") {
					t.Errorf("%s: %s.%s — kiracı kapsamlı SSL akışı sunucu geneli varsayılan posta sertifikasına dokunamaz",
						fset.Position(x.Pos()), id.Name, x.Sel.Name)
				}
			case *ast.BasicLit:
				if x.Kind == token.STRING && strings.Contains(x.Value, "/etc/pki/mail") {
					t.Errorf("%s: %s — kiracı kapsamlı paket varsayılan posta sertifikası dizinine başvuramaz",
						fset.Position(x.Pos()), x.Value)
				}
			}
			return true
		})
	}
	if taranan < 5 {
		t.Fatalf("domains üretim dosyası bulunamadı (taranan=%d) — test boş koşuyor", taranan)
	}
}

// Meşru davranış korunur: müşteri, bayi ve yönetici kendi alanına LE + posta
// SSL'i kurduğunda posta sertifikası o alanın SNI'sı için posta eklentisine
// gönderilir (mail-kur "tamam"), webmail vhost'u yazılır, iş "tamam" biter.
// Adım metinleri sunucu geneli varsayılan sertifikadan söz etmez (eski "SNI +
// varsayılan" iletisi kiracı akışında artık OLUŞAMAZ).
func TestPostaSSL_KiraciYolu_YalnizKendiSNIKurulur(t *testing.T) {
	// SSLIssue(letsencrypt) yerel çözümleyici önbelleğini temizler (rndc,
	// unbound-control, resolvectl): PATH boş dizin → canlı makineye etki yok.
	t.Setenv("PATH", t.TempDir())
	tok := sh_tokenlar(t)
	for i, rol := range []string{"müşteri", "bayi", "admin"} {
		t.Run(rol, func(t *testing.T) {
			id := int64(960 + i)
			ids := strconv.FormatInt(id, 10)
			si_dikisler(t)
			si_temizle(t, id)
			var mc, mk string
			mailSertAl = func(alan, _ string, _ []string) (string, string, []string, []string, error) {
				mc, mk = "/var/lib/girginospanel/mail-certs/"+alan+"/fullchain.pem", "/var/lib/girginospanel/mail-certs/"+alan+"/key.pem"
				return mc, mk, []string{"mail." + alan}, nil, nil
			}
			var gonderilen, webmail []string
			mailSertGonder = func(alan, c, k string) error {
				gonderilen = append(gonderilen, alan+"|"+c+"|"+k)
				return nil
			}
			webmailVhostYaz = func(alan, c, k string) error {
				webmail = append(webmail, alan+"|"+c+"|"+k)
				return nil
			}
			_, db := si_db()

			r := chi.NewRouter()
			r.With(middleware.RequireAuth(sh_sir)).Post("/domains/{id}/ssl/issue", (&Handlers{DB: db}).SSLIssue)
			req := httptest.NewRequest(http.MethodPost, "/domains/"+ids+"/ssl/issue", strings.NewReader(`{"tip":"letsencrypt"}`))
			req.Header.Set("Authorization", "Bearer "+tok[rol])
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("issue: 202 beklenirdi: %d %s", rec.Code, rec.Body.String())
			}
			si_isBekle(t, id)

			want := "bayialan.test|" + mc + "|" + mk
			if len(gonderilen) != 1 || gonderilen[0] != want {
				t.Fatalf("posta sertifikası alanın SNI'sı için eklentiye BİR kez gönderilmeli: %q (beklenen %q)", gonderilen, want)
			}
			if len(webmail) != 1 || webmail[0] != want {
				t.Fatalf("webmail vhost'u alanın sertifikasıyla yazılmalı: %q", webmail)
			}
			govde, g := si_ilerleme(t, db, ids, tok["admin"])
			if g.Durum != "tamam" {
				t.Fatalf("iş tamam bitmeli: %s", govde)
			}
			var kur *SSLAdim
			for j := range g.Adimlar {
				if g.Adimlar[j].Ad == "mail-kur" {
					kur = &g.Adimlar[j]
				}
			}
			if kur == nil || kur.Durum != "tamam" {
				t.Fatalf("mail-kur adımı tamam olmalı: %s", govde)
			}
			if strings.Contains(strings.ToLower(govde), "varsayılan") {
				t.Errorf("kiracı akışının adımları sunucu geneli varsayılandan söz etmemeli: %s", govde)
			}
		})
	}
}
