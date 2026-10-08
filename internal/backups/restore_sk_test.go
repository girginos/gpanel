package backups

// restore_sk_test.go — Restore'un sk kapısı + dürüst sonuç davranışı.
//
// 🔴 Arşivde domain_id YOKTUR; kimlik = sk (üst-seviye "<sk>/" dizini) + DB adları. Restore bu
// kimliği hostingin sk'sıyla karşılaştırmıyordu: mod=tam'da hostingin home üyesi arşivde yoksa
// yalnız DB çıkarılıyor, homeGeriYukle sessizce dönüyor ama yanıt "Yedekteki dosyalar üzerine
// yazıldı" diyordu; DB modlarında başka sk'ya ait DB adları "kurtarma yolu" ile bu hostinge
// kaydedilebiliyordu. Burada kilitlenenler: (1) uyuşmazlıkta TÜM modlar 409 + hiçbir yazma/DB,
// (2) yönetici iki sk'yı görür, bayi/müşteri HİÇBİR sk görmez, (3) home gerçekten yazılmadıysa
// başarı mesajı yok.
//
// Düzenek: DB sahtedb (gerçek SQL/mysql yok), arşiv gerçek bir .tar.gz (geçici dizinde),
// yedekHazirla dikişi arşivi verir, homeKok geçici dizine çevrilir → gerçek /home'a ve gerçek
// mysql'e ASLA dokunulmaz. Dikişleri değiştiren testler paralel DEĞİLDİR.

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/middleware"
	"girginospanel/internal/sahtedb"
)

const (
	skt_host    = "c_skt_hedef"   // hostingin sistem kullanıcısı
	skt_yabanci = "c_skt_yabanci" // arşivin ait olduğu BAŞKA hesap
)

// ---- arşiv üretimi ----

type skt_uye struct {
	ad, icerik string
	dizin      bool
	hedef      string // dolu → sembolik bağ üyesi
}

func skt_d(ad string) skt_uye         { return skt_uye{ad: ad, dizin: true} }
func skt_f(ad, icerik string) skt_uye { return skt_uye{ad: ad, icerik: icerik} }
func skt_l(ad, hedef string) skt_uye  { return skt_uye{ad: ad, hedef: hedef} }

// skt_arsiv: gerçek bir .tar.gz üretir (Restore'un tar + ön-tarama hattından geçer).
func skt_arsiv(t *testing.T, uyeler ...skt_uye) string {
	t.Helper()
	yol := filepath.Join(t.TempDir(), "yedek.tar.gz")
	f, err := os.Create(yol)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, u := range uyeler {
		hd := &tar.Header{Name: u.ad, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(u.icerik)), ModTime: time.Unix(1_700_000_000, 0)}
		bos := u.dizin || u.hedef != ""
		if u.dizin {
			hd.Name, hd.Typeflag, hd.Mode, hd.Size = strings.TrimSuffix(u.ad, "/")+"/", tar.TypeDir, 0o755, 0
		}
		if u.hedef != "" {
			hd.Typeflag, hd.Linkname, hd.Size = tar.TypeSymlink, u.hedef, 0
		}
		if err := tw.WriteHeader(hd); err != nil {
			t.Fatal(err)
		}
		if !bos {
			if _, err := tw.Write([]byte(u.icerik)); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, c := range []interface{ Close() error }{tw, gz, f} {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return yol
}

// skt_manifest: arsivOlustur'un yazdığı __db__/manifest.json.
func skt_manifest(home string) skt_uye {
	b, _ := json.Marshal(arsivManifest{Olusturma: "2026-10-08 03:00:00", Home: home, AnaDB: home + "_main", Veritabanlari: []string{}})
	return skt_f("__db__/manifest.json", string(b))
}

// skt_standart: arsivOlustur'un düzeninin aynısı — "<sk>/..." + "__db__/manifest.json".
func skt_standart(t *testing.T, sk string, ek ...skt_uye) string {
	t.Helper()
	uyeler := []skt_uye{
		skt_d(sk), skt_f(sk+"/index.html", "yeni"), skt_d(sk + "/alt"), skt_f(sk+"/alt/x.txt", "x"),
		skt_d("__db__"), skt_manifest(sk),
	}
	return skt_arsiv(t, append(uyeler, ek...)...)
}

// ---- düzenek ----

var skt_idSayac atomic.Int64

type skt_ortam struct {
	sd    *sahtedb.DB
	h     *Handlers
	home  string // homeKok (geçici)
	id    int64  // domain id — her test farklı (ilerleme kayıtları karışmasın)
	dosya string
}

func skt_kur(t *testing.T, arsiv string) *skt_ortam {
	t.Helper()
	eskiHazir, eskiHome := yedekHazirla, homeKok
	t.Cleanup(func() { yedekHazirla, homeKok = eskiHazir, eskiHome })
	o := &skt_ortam{home: t.TempDir(), id: 7000 + skt_idSayac.Add(1), dosya: skt_host + "-20260924-010101.tar.gz"}
	yedekHazirla = func(context.Context, *sql.DB, string, string) (string, error) { return arsiv, nil }
	homeKok = o.home
	var db *sql.DB
	o.sd, db = sahtedb.Yeni()
	o.h = &Handlers{DB: db}
	// Restore'un ilk sorgusu: sk, alan_adi, is_demo, dosya
	o.sd.Satirlar("FROM backups b", []any{skt_host, "skkapi.test", int64(0), o.dosya})
	return o
}

func (o *skt_ortam) cagir(t *testing.T, tok, govde string) (int, map[string]any, string) {
	t.Helper()
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(hh_sir))
		r.Post("/domains/{id}/backups/{bid}/geriyukle", o.h.Restore)
	})
	req := httptest.NewRequest(http.MethodPost, "/domains/"+strconv.FormatInt(o.id, 10)+"/backups/5/geriyukle", strings.NewReader(govde))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	js := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &js)
	return rec.Code, js, rec.Body.String()
}

// dokunulmadi: kapı reddettiyse ne ev dizinine yazılmış ne de DB aşamasına inilmiş olmalı.
func (o *skt_ortam) dokunulmadi(t *testing.T, ne string) {
	t.Helper()
	if ents, _ := os.ReadDir(o.home); len(ents) != 0 {
		t.Errorf("%s: ev dizinine yazılmış (%d girdi)", ne, len(ents))
	}
	if o.sd.Sorgulandi("db_accounts") || o.sd.Yurutuldu("db_accounts") {
		t.Errorf("%s: DB aşamasına inilmiş (db_accounts'a dokunuldu)", ne)
	}
}

func skt_s(m map[string]any, k string) string { s, _ := m[k].(string); return s }

func skt_yaz(t *testing.T, yol, icerik string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(yol), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yol, []byte(icerik), 0o644); err != nil {
		t.Fatal(err)
	}
}

func skt_oku(yol string) string { b, _ := os.ReadFile(yol); return string(b) }

func skt_var(yol string) bool { _, err := os.Lstat(yol); return err == nil }

// skt_rsyncGerek: rsync olmayan ortamda homeGeriYukleSonuc cp -af yedeğine düşer ve --delete
// (temiz) uygulanamaz; silme bekleyen testler orada atlanır.
func skt_rsyncGerek(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync yok: temiz geri yükleme (--delete) sınanamaz")
	}
}

// skt_rsyncsiz: PATH'i yalnız tar/cp/chown/gzip içeren bir dizine çevirir (rsync YOK) →
// homeGeriYukleSonuc'un cp -af yedeği devreye girer.
func skt_rsyncsiz(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	for _, ad := range []string{"tar", "cp", "chown", "gzip"} {
		yol, err := exec.LookPath(ad)
		if err != nil {
			t.Skipf("%s yok: %v", ad, err)
		}
		if err := os.Symlink(yol, filepath.Join(bin, ad)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
}

// skt_kiraciDikisi: mod=dosya'nın kiracı komutlarını (runuser) gerçek komutlara çevirir ve
// /home/<sk> yollarını geçici ev dizinine yönlendirir. bozuk(argv) true dönerse komut BAŞARISIZ olur.
func skt_kiraciDikisi(t *testing.T, home string, bozuk func(argv []string) bool) {
	t.Helper()
	eskiK, eskiS := restoreKiraciKomut, sahnelemeyiKiraciyaDevret
	t.Cleanup(func() { restoreKiraciKomut, sahnelemeyiKiraciyaDevret = eskiK, eskiS })
	sahnelemeyiKiraciyaDevret = func(string, string) error { return nil }
	restoreKiraciKomut = func(_ string, argv ...string) ([]byte, error) {
		if bozuk != nil && bozuk(argv) {
			return []byte("cp: Disk quota exceeded"), errors.New("exit status 1")
		}
		yeni := make([]string, len(argv))
		for i, a := range argv {
			if strings.HasPrefix(a, "/home/"+skt_host) {
				a = home + strings.TrimPrefix(a, "/home")
			}
			yeni[i] = a
		}
		return exec.Command(yeni[0], yeni[1:]...).CombinedOutput()
	}
}

var skt_modlar = []struct{ ad, govde string }{
	{"tam", `{"mod":"tam"}`},
	{"tam-temiz", `{"mod":"tam","temiz":true}`},
	{"dosyalar", `{"mod":"dosyalar"}`},
	{"dosya", `{"mod":"dosya","yollar":["index.html"]}`},
	{"veritabani", `{"mod":"veritabani"}`},
	{"db", `{"mod":"db","db":"` + skt_yabanci + `_main"}`},
}

// ---- (a) sk uyuşmazlığı: TÜM modlar ----

func TestRestoreSK_Uyusmuyor_TumModlar409(t *testing.T) {
	admin, bayi, musteri := hh_tokenlar(t)
	// Arşiv BAŞKA bir hesabın (home dizini + DB dökümü + manifest).
	arsiv := skt_standart(t, skt_yabanci, skt_f("__db__/"+skt_yabanci+"_main.sql", "-- Dump completed\n"))
	for _, m := range skt_modlar {
		t.Run(m.ad, func(t *testing.T) {
			o := skt_kur(t, arsiv)
			kod, js, govde := o.cagir(t, admin, m.govde)
			if kod != http.StatusConflict || js["ok"] != false || skt_s(js, "kod") != "sk_uyusmuyor" {
				t.Fatalf("yönetici: 409 sk_uyusmuyor beklenirdi: %d %s", kod, govde)
			}
			if h := skt_s(js, "hata"); !strings.Contains(h, skt_host) || !strings.Contains(h, skt_yabanci) {
				t.Errorf("yönetici mesajı iki sk'yı da göstermeli: %q", h)
			}
			o.dokunulmadi(t, m.ad+"/yönetici")
			// İlerleme kaydı temiz kapanır ve HERKESİN okuyabildiği kayıtta sk yoktur.
			if IlerlemeAktifMi(o.id) {
				t.Error("ilerleme kaydı açık kaldı")
			}
			if ilr := IlerlemeOku(o.id); !ilr.Bitti || ilr.Hata != skGenelMesaj {
				t.Errorf("ilerleme kaydı genel hata ile kapanmalı: %+v", ilr)
			}
			// Denetim: BAŞARISIZ kayıt var, yabancı sk'yı taşımıyor (bayi de denetim listesini görür).
			if !o.sd.Yurutuldu("reddedildi: sk uyuşmuyor") || o.sd.Yurutuldu(skt_yabanci) {
				t.Error("denetim kaydı: reddedildi yazılmalı, yabancı sk yazılmamalı")
			}

			for rol, tok := range map[string]string{"bayi": bayi, "müşteri": musteri} {
				o := skt_kur(t, arsiv)
				kod, js, govde := o.cagir(t, tok, m.govde)
				if kod != http.StatusConflict || skt_s(js, "kod") != "sk_uyusmuyor" || skt_s(js, "hata") != skGenelMesaj {
					t.Errorf("%s: genel 409 beklenirdi: %d %s", rol, kod, govde)
				}
				if strings.Contains(govde, "c_skt") {
					t.Errorf("%s: yanıt bir sk sızdırıyor: %s", rol, govde)
				}
				o.dokunulmadi(t, m.ad+"/"+rol)
			}
		})
	}
}

// Birden çok hesabın dizini: hangisine ait olduğu belirsiz → hosting sk'sı da içlerinde olsa reddedilir.
func TestRestoreSK_BirdenCokSK_Reddedilir(t *testing.T) {
	admin, bayi, _ := hh_tokenlar(t)
	arsiv := skt_arsiv(t,
		skt_d(skt_host), skt_f(skt_host+"/index.html", "yeni"),
		skt_d(skt_yabanci), skt_f(skt_yabanci+"/gizli.txt", "g"),
		skt_d("__db__"), skt_manifest(skt_host))
	for _, m := range skt_modlar[:5] {
		t.Run(m.ad, func(t *testing.T) {
			o := skt_kur(t, arsiv)
			kod, js, govde := o.cagir(t, admin, m.govde)
			h := skt_s(js, "hata")
			if kod != http.StatusConflict || skt_s(js, "kod") != "sk_uyusmuyor" ||
				!strings.Contains(h, "birden çok") || !strings.Contains(h, skt_yabanci) || !strings.Contains(h, skt_host) {
				t.Fatalf("yönetici: belirsiz arşiv reddedilmeli: %d %s", kod, govde)
			}
			o.dokunulmadi(t, m.ad)
			o = skt_kur(t, arsiv)
			kod, js, govde = o.cagir(t, bayi, m.govde)
			if kod != http.StatusConflict || skt_s(js, "hata") != skGenelMesaj || strings.Contains(govde, "c_skt") {
				t.Errorf("bayi: genel ret beklenirdi, sk sızmamalı: %d %s", kod, govde)
			}
		})
	}
}

// Home üyesi hiç yok + dosya geri yükleyen mod → 400 (eskiden: yalnız DB çıkarılır, "dosyalar
// üzerine yazıldı" denirdi). Mesaj sk içermez → roller arasında fark yok.
func TestRestoreSK_EvDiziniYok_DosyaModlari400(t *testing.T) {
	admin, bayi, _ := hh_tokenlar(t)
	arsiv := skt_arsiv(t, skt_d("__db__"), skt_manifest(skt_host), skt_f("__db__/"+skt_host+"_main.sql", "-- Dump completed\n"))
	for _, m := range skt_modlar[:4] { // tam, tam-temiz, dosyalar, dosya
		t.Run(m.ad, func(t *testing.T) {
			for rol, tok := range map[string]string{"yönetici": admin, "bayi": bayi} {
				o := skt_kur(t, arsiv)
				kod, js, govde := o.cagir(t, tok, m.govde)
				if kod != http.StatusBadRequest || skt_s(js, "hata") != "yedekte bu hesabın dosyaları yok" || js["uyari"] != nil {
					t.Errorf("%s: 400 'dosyaları yok' beklenirdi: %d %s", rol, kod, govde)
				}
				o.dokunulmadi(t, m.ad+"/"+rol)
				if ilr := IlerlemeOku(o.id); !ilr.Bitti || ilr.Hata == "" {
					t.Errorf("%s: ilerleme kaydı hata ile kapanmalı: %+v", rol, ilr)
				}
			}
		})
	}
}

// Kapı geçilir (üst-seviye adı hosting sk'sı) ama o üye DİZİN değil düz dosya: çıkarılan ağaçta
// ev dizini yok → sk kapısının İKİNCİ katı (homeGeriYukleSonuc) AYNI 400'ü verir, DB'ye geçilmez.
func TestRestoreSK_HomeUyesiDizinDegilse_IkinciKat400(t *testing.T) {
	admin, _, _ := hh_tokenlar(t)
	arsiv := skt_arsiv(t, skt_f(skt_host, "ben bir dizin degilim"), skt_d("__db__"), skt_manifest(skt_host))
	for _, mod := range []string{"tam", "dosyalar"} {
		o := skt_kur(t, arsiv)
		kod, js, g := o.cagir(t, admin, `{"mod":"`+mod+`"}`)
		if kod != http.StatusBadRequest || skt_s(js, "hata") != "yedekte bu hesabın dosyaları yok" || js["uyari"] != nil {
			t.Errorf("%s: 400 'dosyaları yok' beklenirdi: %d %s", mod, kod, g)
		}
		o.dokunulmadi(t, mod)
		if ilr := IlerlemeOku(o.id); !ilr.Bitti || ilr.Hata == "" {
			t.Errorf("%s: ilerleme kaydı hata ile kapanmalı: %+v", mod, ilr)
		}
	}
}

// Okunamayan (gzip olmayan) arşivde yanıt "yedekte bu hesabın dosyaları yok" DEĞİL, "arşiv
// okunamadı" olmalı (yanlış teşhis); hiçbir yazma/DB yok.
func TestRestoreSK_OkunamayanArsiv_500(t *testing.T) {
	admin, _, _ := hh_tokenlar(t)
	bozuk := filepath.Join(t.TempDir(), "bozuk.tar.gz")
	skt_yaz(t, bozuk, "gzip degil")
	for _, m := range skt_modlar {
		t.Run(m.ad, func(t *testing.T) {
			o := skt_kur(t, bozuk)
			kod, js, g := o.cagir(t, admin, m.govde)
			if kod != http.StatusInternalServerError || skt_s(js, "hata") != "yedek arşivi okunamadı" {
				t.Fatalf("500 'yedek arşivi okunamadı' beklenirdi: %d %s", kod, g)
			}
			o.dokunulmadi(t, m.ad)
			if ilr := IlerlemeOku(o.id); !ilr.Bitti || ilr.Hata == "" {
				t.Errorf("ilerleme kaydı hata ile kapanmalı: %+v", ilr)
			}
		})
	}
	_, db, home := skt_cekirdek(t, bozuk)
	if _, err := geriYukleCekirdek(db, 1, 5, "tam", false); err == nil || !strings.Contains(err.Error(), "yedek arşivi okunamadı") {
		t.Errorf("toplu iş: 'yedek arşivi okunamadı' beklenirdi: %v", err)
	}
	if ents, _ := os.ReadDir(home); len(ents) != 0 {
		t.Error("toplu iş: ev dizinine yazılmış")
	}
}

// Dizin yok, kimliği yalnız manifest taşıyor (DB modları): manifest çapraz kontrolü.
func TestRestoreSK_ManifestKimligi_DBModlari(t *testing.T) {
	admin, _, _ := hh_tokenlar(t)
	dump := func(sk string) skt_uye { return skt_f("__db__/"+sk+"_main.sql", "-- Dump completed\n") }

	t.Run("manifest yabancı → 409", func(t *testing.T) {
		arsiv := skt_arsiv(t, skt_d("__db__"), skt_manifest(skt_yabanci), dump(skt_yabanci))
		for _, govde := range []string{`{"mod":"veritabani"}`, `{"mod":"db","db":"` + skt_yabanci + `_main"}`} {
			o := skt_kur(t, arsiv)
			kod, js, g := o.cagir(t, admin, govde)
			if kod != http.StatusConflict || skt_s(js, "kod") != "sk_uyusmuyor" ||
				!strings.Contains(skt_s(js, "hata"), skt_yabanci) || !strings.Contains(skt_s(js, "hata"), skt_host) {
				t.Errorf("%s: manifest'teki yabancı sk reddedilmeli: %d %s", govde, kod, g)
			}
			o.dokunulmadi(t, govde)
		}
	})

	// Eşit manifest / okunamayan manifest: kapı geçer (eski davranış); DB aşamasına inilir ve
	// (sahtedb'de DB kaydı olmadığı için) orada reddedilir — 409 DEĞİL.
	for ad, arsiv := range map[string]string{
		"manifest eşit":                  skt_arsiv(t, skt_d("__db__"), skt_manifest(skt_host), dump(skt_host)),
		"manifest yok (legacy)":          skt_arsiv(t, skt_d("__db__"), dump(skt_host)),
		"manifest home biçimsiz":         skt_arsiv(t, skt_d("__db__"), skt_manifest("../../etc"), dump(skt_host)),
		"manifest bozuk JSON":            skt_arsiv(t, skt_d("__db__"), skt_f("__db__/manifest.json", "{bozuk"), dump(skt_host)),
		"manifest büyük harf (biçimsiz)": skt_arsiv(t, skt_d("__db__"), skt_manifest("C_YABANCI"), dump(skt_host)),
	} {
		t.Run(ad, func(t *testing.T) {
			o := skt_kur(t, arsiv)
			kod, js, g := o.cagir(t, admin, `{"mod":"veritabani"}`)
			if kod == http.StatusConflict || skt_s(js, "kod") != "" {
				t.Fatalf("kapı bu arşivi reddetmemeli: %d %s", kod, g)
			}
			if !strings.Contains(g, "reddedildi") {
				t.Errorf("DB aşamasına inilmeliydi: %d %s", kod, g)
			}
		})
	}
}

// Legacy arşiv (kökte *.sql, c_* dizini yok): DB modunda ESKİSİ GİBİ (kapı geçer).
func TestRestoreSK_Legacy_DBModuEskisiGibi(t *testing.T) {
	admin, _, _ := hh_tokenlar(t)
	arsiv := skt_arsiv(t, skt_f("dump.sql", "-- Dump completed\n"))
	o := skt_kur(t, arsiv)
	kod, js, g := o.cagir(t, admin, `{"mod":"veritabani"}`)
	if kod == http.StatusConflict || skt_s(js, "kod") != "" || !strings.Contains(g, "reddedildi") {
		t.Fatalf("legacy arşiv DB modunda kapıdan geçip DB aşamasına inmeli: %d %s", kod, g)
	}
	// Aynı legacy arşiv dosya modunda: home yok → 400.
	o = skt_kur(t, arsiv)
	if kod, js, g = o.cagir(t, admin, `{"mod":"tam"}`); kod != http.StatusBadRequest || skt_s(js, "hata") != "yedekte bu hesabın dosyaları yok" {
		t.Errorf("legacy + tam: 400 beklenirdi: %d %s", kod, g)
	}
}

// ---- eşleşen sk: davranış korunur ----

func TestRestoreSK_Eslesen_DosyalarGeriYuklenir(t *testing.T) {
	admin, _, _ := hh_tokenlar(t)
	arsiv := skt_standart(t, skt_host)

	t.Run("ezmez", func(t *testing.T) {
		o := skt_kur(t, arsiv)
		skt_yaz(t, filepath.Join(o.home, skt_host, "index.html"), "eski-surum-farkli-boyut")
		skt_yaz(t, filepath.Join(o.home, skt_host, "korunan.txt"), "aktif")
		kod, js, g := o.cagir(t, admin, `{"mod":"dosyalar"}`)
		if kod != http.StatusOK || js["ok"] != true || !strings.Contains(skt_s(js, "uyari"), "Yedekteki dosyalar üzerine yazıldı") {
			t.Fatalf("200 + dürüst mesaj beklenirdi: %d %s", kod, g)
		}
		if got := skt_oku(filepath.Join(o.home, skt_host, "index.html")); got != "yeni" {
			t.Errorf("index.html yedekten gelmedi: %q", got)
		}
		if skt_oku(filepath.Join(o.home, skt_host, "alt", "x.txt")) != "x" {
			t.Error("alt/x.txt geri yüklenmedi")
		}
		if skt_oku(filepath.Join(o.home, skt_host, "korunan.txt")) != "aktif" {
			t.Error("temiz=false: yedekte olmayan aktif dosya korunmalıydı")
		}
		if ilr := IlerlemeOku(o.id); !ilr.Bitti || ilr.Hata != "" {
			t.Errorf("ilerleme başarıyla kapanmalı: %+v", ilr)
		}
	})

	t.Run("temiz", func(t *testing.T) {
		skt_rsyncGerek(t)
		o := skt_kur(t, arsiv)
		skt_yaz(t, filepath.Join(o.home, skt_host, "korunan.txt"), "aktif")
		kod, js, g := o.cagir(t, admin, `{"mod":"dosyalar","temiz":true}`)
		if kod != http.StatusOK || !strings.Contains(skt_s(js, "uyari"), "SİLİNDİ") {
			t.Fatalf("temiz: 200 + 'SİLİNDİ' beklenirdi: %d %s", kod, g)
		}
		if skt_var(filepath.Join(o.home, skt_host, "korunan.txt")) {
			t.Error("temiz=true: yedekte olmayan dosya silinmeliydi")
		}
		if skt_oku(filepath.Join(o.home, skt_host, "index.html")) != "yeni" {
			t.Error("index.html geri yüklenmedi")
		}
	})

	t.Run("tam", func(t *testing.T) {
		o := skt_kur(t, arsiv)
		kod, js, g := o.cagir(t, admin, `{"mod":"tam"}`)
		if kod != http.StatusOK || js["ok"] != true || !strings.Contains(skt_s(js, "uyari"), "Yedekteki dosyalar üzerine yazıldı") {
			t.Fatalf("tam: 200 beklenirdi: %d %s", kod, g)
		}
		if _, ok := js["db"]; !ok {
			t.Error("tam: yanıtta db sonucu olmalı")
		}
		if skt_oku(filepath.Join(o.home, skt_host, "index.html")) != "yeni" {
			t.Error("tam: index.html geri yüklenmedi")
		}
	})
}

// Legacy biçim (home dizini + kökte *.sql) ve eşleşen sk, mod=tam: dosyalar gelir, DB tarafı
// (burada kayıtsız → reddedilir) uyarıda SÖYLENİR — eski davranış.
func TestRestoreSK_Eslesen_LegacyTam_DBUyarisi(t *testing.T) {
	admin, _, _ := hh_tokenlar(t)
	o := skt_kur(t, skt_arsiv(t, skt_d(skt_host), skt_f(skt_host+"/index.html", "yeni"), skt_f("dump.sql", "-- Dump completed\n")))
	kod, js, g := o.cagir(t, admin, `{"mod":"tam"}`)
	if kod != http.StatusOK || !strings.Contains(skt_s(js, "uyari"), "veritabanı tarafında sorun var") {
		t.Fatalf("legacy tam: dosyalar + DB uyarısı beklenirdi: %d %s", kod, g)
	}
	if skt_oku(filepath.Join(o.home, skt_host, "index.html")) != "yeni" {
		t.Error("legacy tam: dosyalar geri yüklenmedi")
	}
}

// ---- (b) dürüst sonuç ----

// rsync de cp de yazamıyorsa (ev dizini erişilemez) başarı mesajı VERİLMEZ; tam modda DB'ye GEÇİLMEZ.
func TestRestoreSK_HomeYazilamazsaBasariMesajiYok(t *testing.T) {
	admin, bayi, _ := hh_tokenlar(t)
	arsiv := skt_standart(t, skt_host)
	for _, mod := range []string{"tam", "dosyalar"} {
		for rol, tok := range map[string]string{"yönetici": admin, "bayi": bayi} {
			t.Run(mod+"/"+rol, func(t *testing.T) {
				o := skt_kur(t, arsiv)
				engel := filepath.Join(t.TempDir(), "engel") // düz DOSYA: altına dizin açılamaz
				skt_yaz(t, engel, "x")
				homeKok = filepath.Join(engel, "home")

				kod, js, g := o.cagir(t, tok, `{"mod":"`+mod+`"}`)
				if kod != http.StatusInternalServerError || js["ok"] != false || !strings.Contains(skt_s(js, "hata"), "dosyalar geri yüklenemedi") {
					t.Fatalf("500 + ok:false beklenirdi: %d %s", kod, g)
				}
				if _, uyariVar := js["uyari"]; uyariVar || strings.Contains(g, "üzerine yazıldı") || strings.Contains(g, "SİLİNDİ") {
					t.Errorf("başarı/uyarı metni olmamalı: %s", g)
				}
				if strings.Contains(g, "rsync") || strings.Contains(g, "cp:") || strings.Contains(g, engel) {
					t.Errorf("ham komut çıktısı/yol istemciye gitmemeli: %s", g)
				}
				if mod == "tam" {
					if !strings.Contains(skt_s(js, "hata"), "veritabanına dokunulmadı") || o.sd.Sorgulandi("db_accounts") {
						t.Errorf("tam: DB aşamasına geçilmemeli: %s", g)
					}
				}
				if ilr := IlerlemeOku(o.id); !ilr.Bitti || !strings.Contains(ilr.Hata, "geri yüklenemedi") {
					t.Errorf("ilerleme kaydı hata ile kapanmalı: %+v", ilr)
				}
			})
		}
	}
}

// rsync çalışmıyorsa cp -af yedeği dosyaları yazar ama yedekte olmayanları SİLEMEZ:
// temiz istenmişse yanıt "SİLİNDİ" DEMEMELİ.
func TestRestoreSK_TemizYapilamazsaSilindiDenmez(t *testing.T) {
	admin, _, _ := hh_tokenlar(t)
	arsiv := skt_standart(t, skt_host)
	skt_rsyncsiz(t)
	o := skt_kur(t, arsiv)
	skt_yaz(t, filepath.Join(o.home, skt_host, "korunan.txt"), "aktif")
	kod, js, g := o.cagir(t, admin, `{"mod":"dosyalar","temiz":true}`)
	u := skt_s(js, "uyari")
	if kod != http.StatusOK || !strings.Contains(u, "SİLİNMEDİ") || strings.Contains(u, "SİLİNDİ") {
		t.Fatalf("200 + 'SİLİNMEDİ' (ve 'SİLİNDİ' DEĞİL) beklenirdi: %d %s", kod, g)
	}
	if skt_oku(filepath.Join(o.home, skt_host, "index.html")) != "yeni" || !skt_var(filepath.Join(o.home, skt_host, "korunan.txt")) {
		t.Error("cp yedeği dosyaları yazmalı, fazlalığı silmemeli")
	}
	// temiz istenmediyse cp yedeği normal başarıdır.
	o = skt_kur(t, arsiv)
	if kod, js, g = o.cagir(t, admin, `{"mod":"dosyalar"}`); kod != http.StatusOK || !strings.Contains(skt_s(js, "uyari"), "Yedekteki dosyalar üzerine yazıldı") {
		t.Errorf("temiz=false + cp yedeği: normal başarı beklenirdi: %d %s", kod, g)
	}
}

// mod=dosya: hiçbir öğe kopyalanamadıysa "klasörüne açıldı" denmez; kısmi başarı uyarıda söylenir.
func TestRestoreSK_DosyaModu_DurustSonuc(t *testing.T) {
	admin, _, _ := hh_tokenlar(t)
	arsiv := skt_standart(t, skt_host, skt_f(skt_host+"/b.txt", "b"))
	iki := `{"mod":"dosya","yollar":["index.html","b.txt"]}`

	t.Run("hepsi başarılı", func(t *testing.T) {
		o := skt_kur(t, arsiv)
		skt_kiraciDikisi(t, o.home, nil)
		kod, js, g := o.cagir(t, admin, iki)
		if kod != http.StatusOK || js["dosya_sayisi"] != float64(2) || !strings.Contains(skt_s(js, "uyari"), "klasörüne açıldı") || strings.Contains(skt_s(js, "uyari"), "⚠") {
			t.Fatalf("200 + 2 dosya beklenirdi: %d %s", kod, g)
		}
	})
	t.Run("hiçbiri kopyalanamadı", func(t *testing.T) {
		o := skt_kur(t, arsiv)
		skt_kiraciDikisi(t, o.home, func(argv []string) bool { return argv[0] == "cp" })
		kod, js, g := o.cagir(t, admin, iki)
		if kod != http.StatusInternalServerError || js["ok"] != false || js["uyari"] != nil || strings.Contains(g, "klasörüne açıldı") {
			t.Fatalf("500, başarı mesajı yok beklenirdi: %d %s", kod, g)
		}
		if ilr := IlerlemeOku(o.id); !ilr.Bitti || ilr.Hata == "" {
			t.Errorf("ilerleme kaydı hata ile kapanmalı: %+v", ilr)
		}
	})
	t.Run("kısmi", func(t *testing.T) {
		o := skt_kur(t, arsiv)
		skt_kiraciDikisi(t, o.home, func(argv []string) bool { return argv[0] == "cp" && strings.Contains(strings.Join(argv, " "), "b.txt") })
		kod, js, g := o.cagir(t, admin, iki)
		if kod != http.StatusOK || js["dosya_sayisi"] != float64(1) || !strings.Contains(skt_s(js, "uyari"), "2 seçimden yalnız 1 tanesi") {
			t.Fatalf("200 + kısmi uyarı beklenirdi: %d %s", kod, g)
		}
	})
}

// ---- saf mantık ----

func TestSKKarar_Tablo(t *testing.T) {
	h, y := skt_host, skt_yabanci
	tablo := []struct {
		ad         string
		mod        string
		sklar      []string
		manifestSK string
		durum      int // 0 = geçti
		kod        string
	}{
		{"eşleşir tam", "tam", []string{h}, "", 0, ""},
		{"eşleşir db", "db", []string{h}, "", 0, ""},
		{"yabancı tam", "tam", []string{y}, "", 409, "sk_uyusmuyor"},
		{"yabancı dosyalar", "dosyalar", []string{y}, "", 409, "sk_uyusmuyor"},
		{"yabancı dosya", "dosya", []string{y}, "", 409, "sk_uyusmuyor"},
		{"yabancı veritabani", "veritabani", []string{y}, "", 409, "sk_uyusmuyor"},
		{"yabancı db", "db", []string{y}, "", 409, "sk_uyusmuyor"},
		{"yabancı bilinmeyen mod da", "foo", []string{y}, "", 409, "sk_uyusmuyor"},
		{"iki sk (host dahil)", "tam", []string{h, y}, "", 409, "sk_uyusmuyor"},
		{"iki yabancı sk", "veritabani", []string{y, "c_baska"}, "", 409, "sk_uyusmuyor"},
		{"dizin yok, tam", "tam", nil, "", 400, ""},
		{"dizin yok, dosyalar", "dosyalar", nil, "", 400, ""},
		{"dizin yok, dosya", "dosya", nil, "", 400, ""},
		{"dizin yok, veritabani (legacy)", "veritabani", nil, "", 0, ""},
		{"dizin yok, db (legacy)", "db", nil, "", 0, ""},
		{"dizin yok, bilinmeyen mod", "foo", nil, "", 0, ""}, // cikarUyeleri zaten 400 verir
		{"dizin yok, manifest eşit, db", "db", nil, h, 0, ""},
		{"dizin yok, manifest eşit, tam", "tam", nil, h, 400, ""},
		{"dizin yok, manifest yabancı, db", "db", nil, y, 409, "sk_uyusmuyor"},
		{"dizin yok, manifest yabancı, tam", "tam", nil, y, 409, "sk_uyusmuyor"},
		{"dizin eşit, manifest yoksayılır", "veritabani", []string{h}, y, 0, ""}, // manifest yalnız dizin YOKKEN okunur
	}
	for _, c := range tablo {
		t.Run(c.ad, func(t *testing.T) {
			ret := skKarar(c.mod, h, c.sklar, c.manifestSK)
			if c.durum == 0 {
				if ret != nil {
					t.Fatalf("geçmeliydi, ret=%+v", ret)
				}
				return
			}
			if ret == nil || ret.Durum != c.durum || ret.Kod != c.kod {
				t.Fatalf("durum=%d kod=%q beklenirdi, ret=%+v", c.durum, c.kod, ret)
			}
			// Yönetici olmayan metin HİÇBİR sk içermez; yönetici 409'da hosting sk'sını gösterir.
			if strings.Contains(ret.Genel, "c_") {
				t.Errorf("genel metin sk içeriyor: %q", ret.Genel)
			}
			if ret.Durum == 409 && !strings.Contains(ret.Admin, h) {
				t.Errorf("yönetici metni hosting sk'sını göstermeli: %q", ret.Admin)
			}
			if ret.mesaj(false) != ret.Genel || ret.mesaj(true) != ret.Admin {
				t.Error("mesaj(admin) rol ayrımı bozuk")
			}
		})
	}
}

func TestArsivUstSKler(t *testing.T) {
	uzunOk := "c_" + strings.Repeat("a", 62)
	uzunFazla := "c_" + strings.Repeat("a", 63)
	tablo := []struct {
		ad     string
		uyeler []string
		bek    []string
	}{
		{"standart", []string{"c_a", "c_a/", "c_a/x/y.txt", "__db__", "__db__/manifest.json"}, []string{"c_a"}},
		{"./ öneki", []string{"./c_a/x"}, []string{"c_a"}},
		{"sıralı + tekil", []string{"c_b/y", "c_a/x", "c_b/z"}, []string{"c_a", "c_b"}},
		{"çözülmüş .. (etkin sk c_b)", []string{"c_a/../c_b/x"}, []string{"c_b"}},
		{"dışarıdan geri dönen yol", []string{"x/../c_a/y"}, []string{"c_a"}},
		{"mutlak yol sayılmaz", []string{"/c_a/x"}, nil},
		{"kaçış sayılmaz", []string{"../c_a/x"}, nil},
		{"biçimsiz adlar sayılmaz", []string{"C_A/x", "c_a.txt", "c_/x", "c_a b/x", "xc_a/x"}, nil},
		{"legacy kök .sql", []string{"dump.sql", "__db__/a.sql"}, nil},
		{"62 karakter sınırı içinde", []string{uzunOk + "/x"}, []string{uzunOk}},
		{"62 karakter sınırı dışında", []string{uzunFazla + "/x"}, nil},
		{"boş", nil, nil},
	}
	for _, c := range tablo {
		t.Run(c.ad, func(t *testing.T) {
			if got := arsivUstSKler(c.uyeler); !reflect.DeepEqual(got, c.bek) {
				t.Fatalf("beklenen %v, gelen %v", c.bek, got)
			}
		})
	}
	if s := skListe([]string{"c_a", "c_b", "c_c", "c_d", "c_e", "c_f", "c_g"}); s != "c_a, c_b, c_c, c_d, c_e … (+2)" {
		t.Errorf("uzun sk listesi kısaltılmalı: %q", s)
	}
}

func TestArsivManifestHome(t *testing.T) {
	devasa := `{"home":"` + skt_host + `","not":"` + strings.Repeat("x", 70<<10) + `"}`
	tablo := []struct {
		ad  string
		yol string
		bek string
	}{
		{"standart", skt_standart(t, skt_host), skt_host},
		{"./ önekli", skt_arsiv(t, skt_f("./__db__/manifest.json", `{"home":"`+skt_host+`"}`)), skt_host},
		{"manifest yok", skt_arsiv(t, skt_d("__db__")), ""},
		{"bozuk JSON", skt_arsiv(t, skt_f("__db__/manifest.json", "{")), ""},
		{"home biçimsiz", skt_arsiv(t, skt_f("__db__/manifest.json", `{"home":"../../etc"}`)), ""},
		{"home büyük harf", skt_arsiv(t, skt_f("__db__/manifest.json", `{"home":"C_X"}`)), ""},
		{"home boş", skt_arsiv(t, skt_f("__db__/manifest.json", `{"home":""}`)), ""},
		{"dizin üye", skt_arsiv(t, skt_d("__db__/manifest.json")), ""},
		{"düzenli olmayan aynı adlı üye atlanır, sonraki düzenli manifest okunur", skt_arsiv(t, skt_l("__db__/manifest.json", "baska"), skt_manifest(skt_host)), skt_host},
		{"64 KiB üstü okunmaz", skt_arsiv(t, skt_f("__db__/manifest.json", devasa)), ""},
		{"gzip değil", func() string { p := filepath.Join(t.TempDir(), "x.tar.gz"); skt_yaz(t, p, "düz metin"); return p }(), ""},
		{"dosya yok", filepath.Join(t.TempDir(), "yok.tar.gz"), ""},
	}
	for _, c := range tablo {
		t.Run(c.ad, func(t *testing.T) {
			if got := arsivManifestHome(c.yol); got != c.bek {
				t.Fatalf("beklenen %q, gelen %q", c.bek, got)
			}
		})
	}
}

// ---- homeGeriYukleSonuc ----

func skt_tmpAgac(t *testing.T, sk string, dosyalar map[string]string) string {
	t.Helper()
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, sk), 0o755); err != nil {
		t.Fatal(err)
	}
	for ad, ic := range dosyalar {
		skt_yaz(t, filepath.Join(tmp, sk, ad), ic)
	}
	return tmp
}

func skt_homeAyarla(t *testing.T) string {
	t.Helper()
	eski := homeKok
	t.Cleanup(func() { homeKok = eski })
	homeKok = t.TempDir()
	return homeKok
}

func TestHomeGeriYukleSonuc_Yazar(t *testing.T) {
	home := skt_homeAyarla(t)
	tmp := skt_tmpAgac(t, skt_host, map[string]string{"index.html": "yeni", "alt/x.txt": "x"})
	skt_yaz(t, filepath.Join(home, skt_host, "index.html"), "eski-surum-farkli-boyut")
	skt_yaz(t, filepath.Join(home, skt_host, "korunan.txt"), "aktif")

	if err := homeGeriYukleSonuc(tmp, skt_host, false); err != nil {
		t.Fatalf("hata: %v", err)
	}
	if skt_oku(filepath.Join(home, skt_host, "index.html")) != "yeni" || skt_oku(filepath.Join(home, skt_host, "alt", "x.txt")) != "x" {
		t.Error("dosyalar yazılmadı")
	}
	if !skt_var(filepath.Join(home, skt_host, "korunan.txt")) {
		t.Error("temiz=false: fazlalık korunmalı")
	}
	skt_rsyncGerek(t)
	if err := homeGeriYukleSonuc(tmp, skt_host, true); err != nil {
		t.Fatalf("temiz: hata: %v", err)
	}
	if skt_var(filepath.Join(home, skt_host, "korunan.txt")) {
		t.Error("temiz=true: fazlalık silinmeli")
	}
}

func TestHomeGeriYukleSonuc_Hatalar(t *testing.T) {
	t.Run("geçersiz sk", func(t *testing.T) {
		home := skt_homeAyarla(t)
		for _, sk := range []string{"", "root", "c_", "c_x/../y", "../etc", "C_X", "c_x y"} {
			if err := homeGeriYukleSonuc(t.TempDir(), sk, false); !errors.Is(err, errHomeGecersizSK) {
				t.Errorf("%q: errHomeGecersizSK beklenirdi: %v", sk, err)
			}
		}
		if ents, _ := os.ReadDir(home); len(ents) != 0 {
			t.Errorf("geçersiz sk ev dizinine yazdı: %d girdi", len(ents))
		}
	})
	t.Run("yedekte yok", func(t *testing.T) {
		home := skt_homeAyarla(t)
		if err := homeGeriYukleSonuc(t.TempDir(), skt_host, false); !errors.Is(err, errHomeYedekteYok) {
			t.Errorf("çıkarılmış dizin yokken errHomeYedekteYok beklenirdi: %v", err)
		}
		tmp := t.TempDir()
		skt_yaz(t, filepath.Join(tmp, skt_host), "dizin değil dosya")
		if err := homeGeriYukleSonuc(tmp, skt_host, false); !errors.Is(err, errHomeYedekteYok) {
			t.Errorf("<sk> düz dosyaysa errHomeYedekteYok beklenirdi: %v", err)
		}
		if ents, _ := os.ReadDir(home); len(ents) != 0 {
			t.Errorf("ev dizinine yazılmış: %d girdi", len(ents))
		}
	})
	t.Run("rsync de cp de yazamaz", func(t *testing.T) {
		skt_homeAyarla(t)
		engel := filepath.Join(t.TempDir(), "engel")
		skt_yaz(t, engel, "x")
		homeKok = filepath.Join(engel, "home")
		tmp := skt_tmpAgac(t, skt_host, map[string]string{"a.txt": "a"})
		err := homeGeriYukleSonuc(tmp, skt_host, false)
		if !errors.Is(err, errHomeYazilamadi) {
			t.Fatalf("errHomeYazilamadi beklenirdi: %v", err)
		}
		if strings.Contains(err.Error(), engel) || strings.Contains(err.Error(), "rsync") {
			t.Errorf("hata metni ham çıktı/yol taşımamalı: %v", err)
		}
	})
}

func TestHomeGeriYukleSonuc_RsyncsizCpYedegi(t *testing.T) {
	skt_rsyncsiz(t)
	for _, temiz := range []bool{false, true} {
		home := skt_homeAyarla(t)
		tmp := skt_tmpAgac(t, skt_host, map[string]string{"index.html": "yeni"})
		skt_yaz(t, filepath.Join(home, skt_host, "korunan.txt"), "aktif")
		err := homeGeriYukleSonuc(tmp, skt_host, temiz)
		if temiz && !errors.Is(err, errHomeTemizYapilamadi) {
			t.Errorf("temiz + cp yedeği: errHomeTemizYapilamadi beklenirdi: %v", err)
		}
		if !temiz && err != nil {
			t.Errorf("temiz=false + cp yedeği: başarı beklenirdi: %v", err)
		}
		if skt_oku(filepath.Join(home, skt_host, "index.html")) != "yeni" {
			t.Errorf("temiz=%v: cp yedeği dosyaları yazmalı", temiz)
		}
		if !skt_var(filepath.Join(home, skt_host, "korunan.txt")) {
			t.Errorf("temiz=%v: cp yedeği fazlalığı silmez", temiz)
		}
	}
}

// homeGeriYukle (toplu iş yolu) sonucu YOK SAYAR ama yine de yazar; hatada panik/çıktı yok.
func TestHomeGeriYukle_EskiSarmalDavranisAyni(t *testing.T) {
	home := skt_homeAyarla(t)
	homeGeriYukle(skt_tmpAgac(t, skt_host, map[string]string{"a.txt": "a"}), skt_host, false)
	if skt_oku(filepath.Join(home, skt_host, "a.txt")) != "a" {
		t.Error("sarmal dosyaları yazmalı")
	}
	homeGeriYukle(t.TempDir(), skt_host, false) // yedekte yok → sessizce döner
	homeGeriYukle(t.TempDir(), "../etc", true)  // geçersiz sk → sessizce döner
	if ents, _ := os.ReadDir(home); len(ents) != 1 {
		t.Errorf("hatalı çağrılar ev dizinine yazmamalı: %d girdi", len(ents))
	}
}

// ---- toplu iş yolu (geriYukleCekirdek) AYNI kapıyı kullanır ----

func skt_cekirdek(t *testing.T, arsiv string) (*sahtedb.DB, *sql.DB, string) {
	t.Helper()
	eskiHazir, eskiHome := yedekHazirla, homeKok
	t.Cleanup(func() { yedekHazirla, homeKok = eskiHazir, eskiHome })
	home := t.TempDir()
	yedekHazirla = func(context.Context, *sql.DB, string, string) (string, error) { return arsiv, nil }
	homeKok = home
	sd, db := sahtedb.Yeni()
	// geriYukleCekirdek sorgusu: sk, is_demo, dosya
	sd.Satirlar("FROM backups b", []any{skt_host, int64(0), skt_host + "-20260924-010101.tar.gz"})
	return sd, db, home
}

func TestGeriYukleCekirdek_SKKapisi(t *testing.T) {
	yabanci := skt_standart(t, skt_yabanci, skt_f("__db__/"+skt_yabanci+"_main.sql", "-- Dump completed\n"))
	for _, mod := range []string{"tam", "dosyalar", "veritabani"} {
		t.Run("uyuşmuyor/"+mod, func(t *testing.T) {
			sd, db, home := skt_cekirdek(t, yabanci)
			msg, err := geriYukleCekirdek(db, 1, 5, mod, false)
			if err == nil || msg != "" || !strings.Contains(err.Error(), skt_host) || !strings.Contains(err.Error(), skt_yabanci) {
				t.Fatalf("toplu iş: iki sk'yı içeren hata beklenirdi: %q %v", msg, err)
			}
			if ents, _ := os.ReadDir(home); len(ents) != 0 || sd.Sorgulandi("db_accounts") || sd.Yurutuldu("db_accounts") {
				t.Errorf("toplu iş: hiçbir yazma/DB olmamalı (home girdi=%d)", len(ents))
			}
		})
	}
	t.Run("ev dizini yok/tam", func(t *testing.T) {
		_, db, home := skt_cekirdek(t, skt_arsiv(t, skt_d("__db__"), skt_manifest(skt_host)))
		if _, err := geriYukleCekirdek(db, 1, 5, "tam", false); err == nil || !strings.Contains(err.Error(), "yedekte bu hesabın dosyaları yok") {
			t.Fatalf("beklenen hata: %v", err)
		}
		if ents, _ := os.ReadDir(home); len(ents) != 0 {
			t.Error("ev dizinine yazılmış")
		}
	})
	t.Run("eşleşen/dosyalar", func(t *testing.T) {
		_, db, home := skt_cekirdek(t, skt_standart(t, skt_host))
		msg, err := geriYukleCekirdek(db, 1, 5, "dosyalar", false)
		if err != nil || msg != "dosyalar geri yüklendi" {
			t.Fatalf("eşleşen sk geri yüklenmeli: %q %v", msg, err)
		}
		if skt_oku(filepath.Join(home, skt_host, "index.html")) != "yeni" {
			t.Error("dosyalar yazılmadı")
		}
	})
}

// Toplu iş yolu da home yazılamazsa BAŞARI demez ve tam modda DB'ye geçmez (HTTP yoluyla aynı kural).
func TestGeriYukleCekirdek_HomeYazilamazsaHata(t *testing.T) {
	for _, mod := range []string{"tam", "dosyalar"} {
		t.Run(mod, func(t *testing.T) {
			sd, db, _ := skt_cekirdek(t, skt_standart(t, skt_host))
			engel := filepath.Join(t.TempDir(), "engel") // düz DOSYA: altına dizin açılamaz
			skt_yaz(t, engel, "x")
			homeKok = filepath.Join(engel, "home")
			msg, err := geriYukleCekirdek(db, 1, 5, mod, false)
			if err == nil || msg != "" || !strings.Contains(err.Error(), "dosyalar geri yüklenemedi") {
				t.Fatalf("hata beklenirdi, başarı mesajı değil: %q %v", msg, err)
			}
			if mod == "tam" && (sd.Sorgulandi("db_accounts") || sd.Yurutuldu("db_accounts")) {
				t.Error("tam: dosyalar yazılamadıysa DB aşamasına geçilmemeli")
			}
		})
	}
}
