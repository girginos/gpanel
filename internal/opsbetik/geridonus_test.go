package opsbetik

// geridonus_test.go — güncelleme/onarım betiklerinin GERİ DÖNÜŞ korumaları (D1 kalıcı
// fix). CANLI SİSTEME DOKUNMAZ: iki betikteki "geri-donus-kapisi" bloğu işaretleri
// arasından çıkarılır, sahte mysql/mysqldump (PATH başında) ile geçici dizinde koşulur.
//
//   - geri_donus_kapisi: DB'de uygulanmış geri dönüşsüz göç (0120) pakette yoksa
//     kurulum REDDEDİLİR (eski ikili hosting_limit_ust'u bilmez → limitler düşer).
//   - yeni_tablolari_kenara_al: geri dönüşte dump geri yüklendikten sonra dump'ta
//     OLMAYAN tablolar yedeklenip kaldırılır (DB güncelleme öncesiyle birebir aynı).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	gdBas = "# >>> geri-donus-kapisi"
	gdSon = "# <<< geri-donus-kapisi"
)

func gdBlok(t *testing.T, betik string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("../../assets/ops", betik))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	i, j := strings.Index(s, gdBas), strings.Index(s, gdSon)
	if i < 0 || j < i || strings.Count(s, gdBas) != 1 {
		t.Fatalf("%s: geri-donus-kapisi bloğu yok/çift", betik)
	}
	return s[i : j+len(gdSon)]
}

// TestGeriDonusBlokIkiBetikteAyni — update ve restore AYNI kapıyı taşımalı ve
// ikisi de kapıyı paket ikilisine dokunmadan ÖNCE, artık temizliğini geri yüklemeden
// SONRA çağırmalı.
func TestGeriDonusBlokIkiBetikteAyni(t *testing.T) {
	u, r := gdBlok(t, "girginospanel-update"), gdBlok(t, "girginospanel-restore")
	if u != r {
		t.Fatal("girginospanel-update ve girginospanel-restore'daki geri-donus-kapisi blokları farklı")
	}
	for _, b := range []struct{ ad, once, sonra string }{
		{"girginospanel-update", `geri_donus_kapisi "$A"`, `install -m 0755 "$NEWBIN" "$BIN"`},
		{"girginospanel-update", `geri_donus_kapisi "$A"`, `exec "$SELF_KURULU"`},
		{"girginospanel-restore", `geri_donus_kapisi "$A"`, `install -m 0755 "$NEWBIN" "$BIN"`},
		{"girginospanel-update", `zcat "$PREDUMP" | mysql`, `yeni_tablolari_kenara_al "$PREDUMP"`},
		{"girginospanel-restore", `zcat "$PREDUMP" | mysql`, `yeni_tablolari_kenara_al "$PREDUMP"`},
	} {
		raw, _ := os.ReadFile(filepath.Join("../../assets/ops", b.ad))
		s := string(raw)
		// Blok tanımının dışındaki (çağrı) konumlar.
		s = strings.Replace(s, gdBlok(t, b.ad), "", 1)
		i, j := strings.Index(s, b.once), strings.Index(s, b.sonra)
		if i < 0 || j < 0 || i > j {
			t.Errorf("%s: %q, %q'dan ÖNCE çağrılmalı (i=%d j=%d)", b.ad, b.once, b.sonra, i, j)
		}
	}
}

type gdOrtam struct {
	t                    *testing.T
	kok, blok, log, dbdr string
	env                  []string
}

func gdKur(t *testing.T) *gdOrtam {
	t.Helper()
	o := &gdOrtam{t: t, kok: t.TempDir()}
	o.blok = filepath.Join(o.kok, "blok.sh")
	if err := os.WriteFile(o.blok, []byte(gdBlok(t, "girginospanel-update")), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(o.kok, "bin")
	_ = os.MkdirAll(bin, 0o755)
	o.log = filepath.Join(o.kok, "cagri.log")
	o.dbdr = filepath.Join(o.kok, "yedek")
	sahteMysql := `#!/bin/bash
echo "mysql $*" >> "$SAHTE_LOG"
case "$*" in
  *schema_migrations*) [ -n "${SAHTE_DEFTER_HATA:-}" ] && exit 1; cat "$SAHTE_DEFTER" ;;
  *information_schema.tables*) cat "$SAHTE_TABLOLAR" ;;
esac
exit 0
`
	sahteDump := `#!/bin/bash
echo "mysqldump $*" >> "$SAHTE_LOG"
[ -n "${SAHTE_DUMP_HATA:-}" ] && exit 2
echo "-- sahte artik dump"
`
	for ad, icerik := range map[string]string{"mysql": sahteMysql, "mysqldump": sahteDump} {
		if err := os.WriteFile(filepath.Join(bin, ad), []byte(icerik), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	o.env = append(os.Environ(), "LC_ALL=C", "PATH="+bin+":"+os.Getenv("PATH"),
		"SAHTE_LOG="+o.log, "DBDIR="+o.dbdr,
		"SAHTE_DEFTER="+filepath.Join(o.kok, "defter"), "SAHTE_TABLOLAR="+filepath.Join(o.kok, "tablolar"))
	return o
}

func (o *gdOrtam) yaz(ad, icerik string) string {
	o.t.Helper()
	p := filepath.Join(o.kok, ad)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(icerik), 0o644); err != nil {
		o.t.Fatal(err)
	}
	return p
}

// paket: göç dosyalarıyla assets dizini (migrations.tar.gz paketle.sh gibi "./" önekli).
func (o *gdOrtam) paket(ad string, gocler ...string) string {
	o.t.Helper()
	a := filepath.Join(o.kok, ad)
	m := filepath.Join(o.kok, ad+"-m")
	_ = os.MkdirAll(a, 0o755)
	_ = os.MkdirAll(m, 0o755)
	for _, g := range gocler {
		o.yaz(filepath.Join(ad+"-m", g), "-- x\n")
	}
	if out, err := exec.Command("tar", "czf", filepath.Join(a, "migrations.tar.gz"), "-C", m, ".").CombinedOutput(); err != nil {
		o.t.Fatalf("tar: %v %s", err, out)
	}
	return a
}

func (o *gdOrtam) kos(govde string, ek ...string) (string, int) {
	o.t.Helper()
	_ = os.Remove(o.log)
	betik := `set -euo pipefail
warn() { echo "W: $*"; }
ok()   { echo "OK: $*"; }
die()  { echo "DIE: $*"; exit 7; }
source "$1"
` + govde
	cmd := exec.Command("bash", "-c", betik, "bash", o.blok)
	cmd.Env = append(append([]string{}, o.env...), ek...)
	out, err := cmd.CombinedOutput()
	kod := 0
	if ee, ok := err.(*exec.ExitError); ok {
		kod = ee.ExitCode()
	} else if err != nil {
		o.t.Fatal(err)
	}
	return string(out), kod
}

func (o *gdOrtam) cagrilar() string {
	b, _ := os.ReadFile(o.log)
	return string(b)
}

func TestGeriDonusKapisi(t *testing.T) {
	o := gdKur(t)
	const d1 = "0120_hosting_limit_ust.sql"
	defterD1 := "0001_ilk.sql\n0119_x.sql\n" + d1 + "\n0121_y.sql\n"
	eski := o.paket("eski", "0001_ilk.sql", "0119_x.sql")
	yeni := o.paket("yeni", "0001_ilk.sql", "0119_x.sql", d1, "0121_y.sql")

	// G1: DB 0120'yi geçti, paket onu içermiyor → RED (die), hiçbir yan etki yok.
	o.yaz("defter", defterD1)
	out, kod := o.kos(`geri_donus_kapisi "` + eski + `"; echo GECTI`)
	if kod != 7 || !strings.Contains(out, "DOWNGRADE REFUSED") || !strings.Contains(out, d1) || strings.Contains(out, "GECTI") {
		t.Errorf("eski paket reddedilmeliydi: kod=%d %s", kod, out)
	}
	// G2: güncel/yeni paket → geçer.
	if out, kod := o.kos(`geri_donus_kapisi "` + yeni + `"; echo GECTI`); kod != 0 || !strings.Contains(out, "GECTI") || strings.Contains(out, "W:") {
		t.Errorf("yeni paket geçmeliydi: kod=%d %s", kod, out)
	}
	// G3: DB henüz D1 öncesi (0120 defterde yok) → eski paket de geçer (normal güncelleme/geri dönüş).
	o.yaz("defter", "0001_ilk.sql\n0119_x.sql\n")
	if out, kod := o.kos(`geri_donus_kapisi "` + eski + `"; echo GECTI`); kod != 0 || !strings.Contains(out, "GECTI") {
		t.Errorf("D1 öncesi DB'de eski paket geçmeliydi: kod=%d %s", kod, out)
	}
	// G4: bilerek zorlama → geçer ama 🔴 uyarı basar.
	o.yaz("defter", defterD1)
	if out, kod := o.kos(`geri_donus_kapisi "`+eski+`"; echo GECTI`, "GOSP_GERI_DONUS_ZORLA=1"); kod != 0 || !strings.Contains(out, "FORCED") {
		t.Errorf("zorlama: kod=%d %s", kod, out)
	}
	// G5: paket göç listesi taşımıyor (bilinmiyor) + DB 0120'yi geçti → RED (fail-closed).
	bos := filepath.Join(o.kok, "bos")
	_ = os.MkdirAll(bos, 0o755)
	if out, kod := o.kos(`geri_donus_kapisi "` + bos + `"; echo GECTI`); kod != 7 {
		t.Errorf("göç listesiz paket reddedilmeliydi: kod=%d %s", kod, out)
	}
	// G6: paket göçleri dizin olarak (restore'un ikinci biçimi) → içeriyorsa geçer.
	o.yaz("dizinli/migrations/"+d1, "-- x\n")
	if out, kod := o.kos(`geri_donus_kapisi "` + filepath.Join(o.kok, "dizinli") + `"; echo GECTI`); kod != 0 || !strings.Contains(out, "GECTI") {
		t.Errorf("dizin biçimli paket: kod=%d %s", kod, out)
	}
	// G7: defter okunamıyor (mysql kapalı / defter yok) → uyarı + geç (DB yedeği zaten fail-closed).
	if out, kod := o.kos(`geri_donus_kapisi "`+eski+`"; echo GECTI`, "SAHTE_DEFTER_HATA=1"); kod != 0 || !strings.Contains(out, "W: downgrade guard") {
		t.Errorf("defter hatası: kod=%d %s", kod, out)
	}
	// Ad yalnız TAM satır eşleşmesiyle sayılır (önek/sonek benzerleri değil).
	o.yaz("defter", "x"+d1+"\n"+d1+".bak\n")
	if _, kod := o.kos(`geri_donus_kapisi "` + eski + `"`); kod != 0 {
		t.Errorf("benzer adlar 0120 sayıldı")
	}
}

func TestYeniTablolariKenaraAl(t *testing.T) {
	o := gdKur(t)
	dumpYaz := func(ad, icerik string) string {
		p := o.yaz(ad+".sql", icerik)
		if out, err := exec.Command("gzip", "-f", p).CombinedOutput(); err != nil {
			t.Fatalf("gzip: %v %s", err, out)
		}
		return p + ".gz"
	}
	dump := dumpYaz("once", "-- MariaDB dump\nDROP TABLE IF EXISTS `domains`;\nCREATE TABLE `domains` (\n  `id` int\n);\n"+
		"CREATE TABLE `service_plans` (\n `id` int\n);\nCREATE TABLE IF NOT EXISTS `schema_migrations` (x int);\n")
	o.yaz("tablolar", "domains\nhosting_limit_ust\nschema_migrations\nservice_plans\nservice_plans_ozel_arsiv\n")

	// K1: dump'ta olmayan iki tablo → önce yedek (tek mysqldump), sonra yalnız onlar DROP.
	out, kod := o.kos(`yeni_tablolari_kenara_al "` + dump + `"`)
	c := o.cagrilar()
	if kod != 0 || !strings.Contains(c, "mysqldump --single-transaction panel hosting_limit_ust service_plans_ozel_arsiv") {
		t.Fatalf("artık yedeği: kod=%d %s\n%s", kod, out, c)
	}
	for _, tb := range []string{"hosting_limit_ust", "service_plans_ozel_arsiv"} {
		if !strings.Contains(c, "DROP TABLE `panel`.`"+tb+"`") {
			t.Errorf("%s kaldırılmadı:\n%s", tb, c)
		}
	}
	for _, tb := range []string{"domains", "service_plans", "schema_migrations"} {
		if strings.Contains(c, "DROP TABLE `panel`.`"+tb+"`") {
			t.Errorf("dump'taki %s KALDIRILDI:\n%s", tb, c)
		}
	}
	if !strings.Contains(c, "SET FOREIGN_KEY_CHECKS=0") {
		t.Errorf("DROP FK kontrolü kapalı yapılmalı:\n%s", c)
	}
	if i, j := strings.Index(c, "mysqldump"), strings.Index(c, "DROP TABLE"); i < 0 || j < i {
		t.Errorf("yedek DROP'tan önce alınmalı:\n%s", c)
	}
	if m, _ := filepath.Glob(filepath.Join(o.dbdr, "geri-donus-artik-*.sql.gz")); len(m) != 1 {
		t.Errorf("artık yedek dosyası yok: %v", m)
	}

	// K2: yedek alınamazsa HİÇBİR tablo kaldırılmaz.
	out, kod = o.kos(`yeni_tablolari_kenara_al "`+dump+`"`, "SAHTE_DUMP_HATA=1")
	if c := o.cagrilar(); kod != 1 || strings.Contains(c, "DROP TABLE") {
		t.Errorf("yedeksiz DROP: kod=%d %s\n%s", kod, out, c)
	}
	// K3: dump okunamıyor/boş → hiçbir şey kaldırılmaz (tüm tabloları "yeni" sanma).
	bozuk := o.yaz("bozuk.sql.gz", "gzip degil")
	out, kod = o.kos(`yeni_tablolari_kenara_al "` + bozuk + `"`)
	if c := o.cagrilar(); kod != 1 || strings.Contains(c, "DROP") || strings.Contains(c, "mysqldump") {
		t.Errorf("bozuk dump: kod=%d %s\n%s", kod, out, c)
	}
	// K4: artık yok → dokunulmaz.
	o.yaz("tablolar", "domains\nschema_migrations\nservice_plans\n")
	if _, kod := o.kos(`yeni_tablolari_kenara_al "` + dump + `"`); kod != 0 || o.cagrilar() != "mysql -N -e SELECT table_name FROM information_schema.tables WHERE table_schema='panel' AND table_type='BASE TABLE'\n" {
		t.Errorf("artıksız durumda yan etki: kod=%d\n%s", kod, o.cagrilar())
	}
	// K5: şüpheli çok artık (>20) → hiçbiri kaldırılmaz.
	var cok strings.Builder
	cok.WriteString("domains\nschema_migrations\nservice_plans\n")
	for i := 0; i < 21; i++ {
		cok.WriteString("t_" + string(rune('a'+i)) + "\n")
	}
	o.yaz("tablolar", cok.String())
	if _, kod := o.kos(`yeni_tablolari_kenara_al "` + dump + `"`); kod != 1 || strings.Contains(o.cagrilar(), "DROP") {
		t.Errorf("çok artıkta DROP: kod=%d\n%s", kod, o.cagrilar())
	}
}
