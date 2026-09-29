package backups

// hedef_lftp_test.go — lftp adaptörünün ad eşlemesi, atomik indirme, hata
// sınıflama ve maskelemesi. Ağa ÇIKILMAZ: PATH'in başına sahte `lftp`/`curl`
// konur; sahte betik aldığı lftp betiğini bir günlüğe yazar. Geçici dosyalar
// t.TempDir altında (TMPDIR yönlendirilir).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"girginospanel/internal/backups/hedef"
)

const lh_nobetci = "Lftp-N0betci-Parola-51c2"

const lh_sahteLftp = `#!/bin/sh
b=$(cat "$2")
printf '%s\n' "$b" >> "$GOSP_SAHTE_LOG"
case "$b" in
  *" get "*) yol=$(printf '%s' "$b" | sed -n 's/.* -o "\([^"]*\)".*/\1/p'); printf 'sahte-icerik' > "$yol";;
  *"cls -l "*) ad=$(printf '%s' "$b" | sed -n 's/.*cls -l "\([^"]*\)".*/\1/p'); printf -- '-rw-r--r--  kul/grp  12 2026-08-25 10:00 %s\n' "$ad";;
esac
printf '%s' "$GOSP_SAHTE_CIKTI"
exit ${GOSP_SAHTE_CIKIS:-0}
`

const lh_sahteCurl = `#!/bin/sh
cat > /dev/null
printf '%s' "$GOSP_SAHTE_CIKTI" >&2
exit ${GOSP_SAHTE_CIKIS:-0}
`

type lh_ortam struct {
	log    string
	gecici string
}

// lh_kur — sahte ikilileri PATH'in başına koyar.
func lh_kur(t *testing.T) *lh_ortam {
	t.Helper()
	bin := t.TempDir()
	for ad, icerik := range map[string]string{"lftp": lh_sahteLftp, "curl": lh_sahteCurl} {
		if err := os.WriteFile(filepath.Join(bin, ad), []byte(icerik), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	o := &lh_ortam{log: filepath.Join(t.TempDir(), "lftp.log"), gecici: t.TempDir()}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("GOSP_SAHTE_LOG", o.log)
	t.Setenv("GOSP_SAHTE_CIKTI", "")
	t.Setenv("GOSP_SAHTE_CIKIS", "0")
	t.Setenv("TMPDIR", o.gecici)
	return o
}

func (o *lh_ortam) betikler(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(o.log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

func lh_hedef(t *testing.T, tip string) *lftpHedef {
	t.Helper()
	h, err := yeniLftpHedef(&Destination{Tip: tip, Host: "depo.ornek.test", Port: 22, Kullanici: "yedekci",
		Parola: lh_nobetci, UzakDizin: "/yedek", Aktif: false})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func lh_yerel(t *testing.T, ad, icerik string) string {
	t.Helper()
	yol := filepath.Join(t.TempDir(), ad)
	if err := os.WriteFile(yol, []byte(icerik), 0o600); err != nil {
		t.Fatal(err)
	}
	return yol
}

// Uzak ad taban dizine katlanır, yerel ad farklıysa uzak adı taşıyan bağ verilir;
// Aktif=false hedef SESSİZCE atlanmaz; geçici dizin temizlenir.
func TestLftpHedef_YukleAdEslemesi(t *testing.T) {
	o := lh_kur(t)
	h := lh_hedef(t, "sftp")
	yerel := lh_yerel(t, "is-123.age.tmp", "abc")
	if err := h.Yukle(context.Background(), yerel, "2026-08-25/müşteri yedeği.tar.gz.age", hedef.YukleSecenek{}); err != nil {
		t.Fatalf("Yukle: %v", err)
	}
	b := o.betikler(t)
	for _, beklenen := range []string{
		`mkdir -p -f "/yedek/2026-08-25"`, `cd "/yedek/2026-08-25"`, `/müşteri yedeği.tar.gz.age"`, `sftp://depo.ornek.test:22`,
	} {
		if !strings.Contains(b, beklenen) {
			t.Errorf("betikte %q yok:\n%s", beklenen, b)
		}
	}
	if strings.Contains(b, "is-123.age.tmp") {
		t.Errorf("uzak ad yerine yerel ad kullanıldı:\n%s", b)
	}
	if kalan, _ := filepath.Glob(filepath.Join(o.gecici, "gosp-*")); len(kalan) != 0 {
		t.Errorf("geçici dosya/dizin kaldı: %v", kalan)
	}
	// Yerel ad zaten uzak adla aynıysa bağ gerekmez.
	yerel2 := lh_yerel(t, "c_x-20260825-233538.tar.gz", "abc")
	if err := h.Yukle(context.Background(), yerel2, "c_x-20260825-233538.tar.gz", hedef.YukleSecenek{}); err != nil {
		t.Fatalf("Yukle aynı ad: %v", err)
	}
	if !strings.Contains(o.betikler(t), `put -O . "`+yerel2+`"`) {
		t.Errorf("aynı adlı yerel dosya doğrudan verilmeliydi")
	}
}

// Hata Error()'da YALNIZ kategori; parola ne Error()'da ne Ayrinti()'de.
func TestLftpHedef_HataMaskeliKategorili(t *testing.T) {
	lh_kur(t)
	h := lh_hedef(t, "sftp")
	t.Setenv("GOSP_SAHTE_CIKIS", "1")
	t.Setenv("GOSP_SAHTE_CIKTI", "open -u yedekci,"+lh_nobetci+" failed; Login failed: 530 Login incorrect. parola="+lh_nobetci)
	err := h.Yukle(context.Background(), lh_yerel(t, "a.age", "x"), "a.age", hedef.YukleSecenek{})
	if err == nil {
		t.Fatal("hata bekleniyordu")
	}
	if err.Error() != "hedef yukle: kimlik" {
		t.Errorf("Error() = %q", err.Error())
	}
	var hh *hedef.Hata
	if !errors.As(err, &hh) {
		t.Fatalf("*hedef.Hata değil: %T", err)
	}
	if strings.Contains(err.Error(), lh_nobetci) || strings.Contains(hh.Ayrinti(), lh_nobetci) {
		t.Fatalf("parola sızdı: %q / %q", err.Error(), hh.Ayrinti())
	}
	if !strings.Contains(hh.Ayrinti(), "Login failed") {
		t.Errorf("ayrıntı tanı bilgisini kaybetti: %q", hh.Ayrinti())
	}
	// FTP test yolu (curl) da aynı kurala uyar.
	t.Setenv("GOSP_SAHTE_CIKIS", "67")
	t.Setenv("GOSP_SAHTE_CIKTI", "curl: (67) Access denied: 530 "+lh_nobetci)
	terr := lh_hedef(t, "ftp").Test(context.Background())
	if hedef.KategoriAl(terr) != hedef.KategoriKimlik || strings.Contains(terr.Error(), lh_nobetci) {
		t.Fatalf("Test hatası: %v", terr)
	}
	if errors.As(terr, &hh) && strings.Contains(hh.Ayrinti(), lh_nobetci) {
		t.Fatalf("Test ayrıntısında parola: %q", hh.Ayrinti())
	}
}

// İndirme önce geçici ada; başarısızlıkta nihai ad OLUŞMAZ ve kalıntı kalmaz.
func TestLftpHedef_IndirAtomik(t *testing.T) {
	o := lh_kur(t)
	h := lh_hedef(t, "sftp")
	dizin := t.TempDir()
	hedefYol := filepath.Join(dizin, "inen.tar.gz")
	if err := h.Indir(context.Background(), "2026-08-25/c_x.tar.gz", hedefYol); err != nil {
		t.Fatalf("Indir: %v", err)
	}
	if b, _ := os.ReadFile(hedefYol); string(b) != "sahte-icerik" {
		t.Fatalf("içerik %q", b)
	}
	if b := o.betikler(t); !strings.Contains(b, `cd "/yedek/2026-08-25"`) || !strings.Contains(b, `get "c_x.tar.gz" -o "`) {
		t.Errorf("indirme betiği yanlış:\n%s", b)
	}
	// Sahte lftp yarım dosyayı yazar AMA hata döner (kesilen aktarım).
	t.Setenv("GOSP_SAHTE_CIKIS", "1")
	t.Setenv("GOSP_SAHTE_CIKTI", "get: Access failed: 550 Failed to open file. (c_y.tar.gz)")
	yol2 := filepath.Join(dizin, "olmamali.tar.gz")
	err := h.Indir(context.Background(), "c_y.tar.gz", yol2)
	if hedef.KategoriAl(err) != hedef.KategoriBulunamadi {
		t.Errorf("kategori %q (%v)", hedef.KategoriAl(err), err)
	}
	girdiler, _ := os.ReadDir(dizin)
	if len(girdiler) != 1 {
		var adlar []string
		for _, g := range girdiler {
			adlar = append(adlar, g.Name())
		}
		t.Errorf("başarısız indirme dosya bıraktı: %v", adlar)
	}
}

func TestLftpHedef_ListeleVeSil(t *testing.T) {
	o := lh_kur(t)
	h := lh_hedef(t, "ftp")
	l, err := h.Listele(context.Background(), "2026-08-25/c_x.tar.gz")
	if err != nil || len(l) != 1 || l[0].Ad != "2026-08-25/c_x.tar.gz" || l[0].Boyut != 12 {
		t.Fatalf("Listele: %v %v", l, err)
	}
	if _, err := h.Listele(context.Background(), "2026-08-25/"); !errors.Is(err, hedef.ErrDesteklenmiyor) {
		t.Errorf("dizin öneki desteklenmiyor olmalı: %v", err)
	}
	if err := h.Sil(context.Background(), "2026-08-25/c_x.tar.gz"); err != nil {
		t.Fatalf("Sil: %v", err)
	}
	b := o.betikler(t)
	if !strings.Contains(b, `cd "/yedek"; rm -f "/yedek/2026-08-25/c_x.tar.gz"`) {
		t.Errorf("silme betiği yanlış:\n%s", b)
	}
	if !strings.Contains(b, `ftp://depo.ornek.test:22`) {
		t.Errorf("ftp url yok:\n%s", b)
	}
	// lftp yoksa ErrLftpYok errors.Is ile tanınır.
	t.Setenv("PATH", t.TempDir())
	if err := h.Sil(context.Background(), "c_x.tar.gz"); !errors.Is(err, ErrLftpYok) {
		t.Errorf("ErrLftpYok bekleniyordu: %v", err)
	}
}

// Geçersiz adlar lftp'ye HİÇ ulaşmaz.
func TestLftpHedef_GecersizAdLftpCagirmaz(t *testing.T) {
	o := lh_kur(t)
	h := lh_hedef(t, "sftp")
	ctx := context.Background()
	yerel := lh_yerel(t, "a.age", "x")
	for _, ad := range []string{"", "../kurban.tar.gz", "/etc/passwd", "a/../../b", `a\b`, "a\nb"} {
		if err := h.Yukle(ctx, yerel, ad, hedef.YukleSecenek{}); err == nil {
			t.Errorf("Yukle %q kabul etti", ad)
		}
		if err := h.Indir(ctx, ad, filepath.Join(t.TempDir(), "x")); err == nil {
			t.Errorf("Indir %q kabul etti", ad)
		}
		if err := h.Sil(ctx, ad); err == nil {
			t.Errorf("Sil %q kabul etti", ad)
		}
	}
	if b := o.betikler(t); b != "" {
		t.Errorf("geçersiz adla lftp çağrıldı:\n%s", b)
	}
	// Olmayan yerel dosya da lftp'ye gitmez.
	if err := h.Yukle(ctx, filepath.Join(t.TempDir(), "yok.age"), "yok.age", hedef.YukleSecenek{}); hedef.KategoriAl(err) != hedef.KategoriBulunamadi {
		t.Errorf("olmayan yerel dosya: %v", err)
	}
}

func TestLftpHedef_KurucuVeGenelAd(t *testing.T) {
	for _, d := range []*Destination{nil, {Tip: "s3", Host: "x"}, {Tip: "sftp", Host: " "}} {
		if _, err := yeniLftpHedef(d); err == nil {
			t.Errorf("geçersiz hedef kabul edildi: %+v", d)
		}
	}
	g := &GenelAyar{UzakTip: "sftp", UzakHost: "depo.ornek.test", UzakPort: 23, UzakKullanici: "u", UzakParola: "p", UzakDizin: "/g", UzakAktif: false}
	h, err := genelLftpHedefi(g)
	if err != nil || h.Tur() != "sftp" || h.d.Port != 23 || h.d.UzakDizin != "/g" || !h.d.Aktif || h.Yetenekler() != 0 {
		t.Fatalf("genel hedef: %+v %v", h, err)
	}
	if _, err := genelLftpHedefi(nil); err == nil {
		t.Error("nil genel ayar kabul edildi")
	}
	if got := lftpGenelAd("c_x-20260825-233538.tar.gz"); got != "2026-08-25/c_x-20260825-233538.tar.gz" {
		t.Errorf("lftpGenelAd: %q", got)
	}
	if got := lftpGenelAd("eski.tar.gz"); got != "eski.tar.gz" {
		t.Errorf("lftpGenelAd damgasız: %q", got)
	}
}
