package backups

// silinen_sahipsiz_supurme_dogrulama_test.go — DÜŞMANCA DOĞRULAMA (yetim yedek tasarımı, commit e449f18).
//
//	F  S2: sahipsiz satır işaretleme (dosya adı varyantları, kapsam, 20k satır performansı, açlık)
//	G  04:00 süpürmesi (sınırlar, sembolik bağlantı, TryLock, ctx iptali)
//	H  silme akışı bağımsızlığı (bozuk/eksik/yavaş DB, nil, iptal; HostingYik sırası)
//	+  ek bulgular: D6 (S2 eşiği), D3 (yerel dosya silme hatası yutuluyor), D2 (kısmi silmede denetim yok)
//
// Gerçek BackupRoot'a YAZILMAZ, AĞA ÇIKILMAZ (uzak silme sahte). Geçici MariaDB gerekir.
// Adında "Bulgu" geçen testler ÜRETİM KODUNDAKİ GERÇEK BİR HATAYI kanıtlar: bugün KIRMIZIDIR.

import (
	"context"
	"database/sql"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ================================================================ F — S2 sahipsiz işaretleme

// Dosya adından sk türetme: geçerli biçimler türetilir; bozuk/saldırgan adlar türetilmez.
func TestDG_F1_SkDosyaAdindan_GecerliVeBozukAdlar(t *testing.T) {
	gecerli := map[string]string{
		"c_x-20261008-150528.tar.gz":                               "c_x",
		"c_x-20261008-150528-planli.tar.gz":                        "c_x",
		"c_x-20261008-150528-auto.tar.gz":                          "c_x",
		"c_a_b1-20261008-150528.tar.gz":                            "c_a_b1",
		"c_" + strings.Repeat("a", 40) + "-20261008-150528.tar.gz": "c_" + strings.Repeat("a", 40),
	}
	for ad, sk := range gecerli {
		if g, ok := skDosyaAdindan(ad); !ok || g != sk {
			t.Errorf("%q → (%q,%v), %q beklenirdi", ad, g, ok, sk)
		}
	}
	bozuk := []string{
		"", ".", "..", "x.tar.gz", "c_x.tar.gz", "c_x-20261008.tar.gz", "c_x-20261008-150528",
		"C_x-20261008-150528.tar.gz", "c_X-20261008-150528.tar.gz", "c_" + strings.Repeat("a", 41) + "-20261008-150528.tar.gz",
		"c_-20261008-150528.tar.gz", "c_x-2026100-150528.tar.gz", "c_x-20261008-15052.tar.gz", "c_x-2026100a-150528.tar.gz",
		"c_x-20261008-150528.tar", "c_x-20261008-150528.TAR.GZ", "c_x-20261008-150528.tar.gz.bak", "c_x-20261008-150528.tar.gz\n",
		"c_x-20261008-150528.tar.gz ", " c_x-20261008-150528.tar.gz", "\nc_x-20261008-150528.tar.gz",
		"../c_x-20261008-150528.tar.gz", "a/c_x-20261008-150528.tar.gz", "c_x-20261008-150528.tar.gz/x", "c_x/../c_y-20261008-150528.tar.gz",
		"c_x\x00-20261008-150528.tar.gz", "c_x--20261008-150528.tar.gz", "c_x-y-20261008-150528.tar.gz", "c_x-20261008-150528-planli-auto.tar.gz",
		"c_x-20261008-150528-x.tar.gz", "c_x-٢٠٢٦١٠٠٨-١٥٠٥٢٨.tar.gz", "c_ü-20261008-150528.tar.gz", "c_x.-20261008-150528.tar.gz",
	}
	for _, ad := range bozuk {
		if g, ok := skDosyaAdindan(ad); ok {
			t.Errorf("%q geçersiz olmalıydı, %q türetildi", ad, g)
		}
	}
}

// BULGU F3 — yedekDosyaAdi aynı saniye çakışmasında `<sk>-YYYYMMDD-HHMMSS-2.tar.gz` … `-99` üretir ve kod tabanı eski
// `<sk>-auto-YYYYMMDD-HHMMSS.tar.gz` adlarını da tanır (manuel_test.go); ama S2'nin ayrıştırıcısı (yedekDosyaSKRe) bu iki
// biçimi türetemez → silme izi yazılamamış böyle satırlar sonsuza dek "türetilemeyen" kalır: hiçbir listede görünmez,
// süpürülmez, diskte/DB'de birikir.
func TestDGBulgu_F3_SahipsizIsaretle_UreticiVeEskiAdlar_SkTuretilebilmeli(t *testing.T) {
	dir := t.TempDir()
	an := time.Date(2026, 10, 8, 15, 5, 28, 0, time.UTC)
	var cakisma []string
	for i := 0; i < 12; i++ { // taban + -2 … -12
		ad := yedekDosyaAdi(dir, "c_ornek", an)
		if err := os.WriteFile(filepath.Join(dir, ad), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		cakisma = append(cakisma, ad)
	}
	if sk, ok := skDosyaAdindan(cakisma[0]); !ok || sk != "c_ornek" {
		t.Fatalf("kontrol: taban ad türetilmeli: %q", cakisma[0])
	}
	var eksik []string
	for _, ad := range cakisma[1:] {
		if sk, ok := skDosyaAdindan(ad); !ok || sk != "c_ornek" {
			eksik = append(eksik, ad)
		}
	}
	if len(eksik) > 0 {
		t.Errorf("🔴 yedekDosyaAdi'nin ÇAKIŞMA EKİYLE (-2…) ürettiği %d addan sk türetilemedi: %v", len(eksik), eksik)
	}
	eski := "c_ornek-auto-20261008-150528.tar.gz"
	if sk, ok := skDosyaAdindan(eski); !ok || sk != "c_ornek" {
		t.Errorf("🔴 eski bash zamanlayıcısının `<sk>-auto-<damga>` adı için sk türetilemedi: %q (manuel_test.go bu adı tanıyor)", eski)
	}
	// sonuç: iz yazılamayan bu satırlar S2 turunda işaretlenmeli
	db := kDB(t)
	logYakala(t)
	for _, ad := range append(append([]string{}, cakisma...), eski) {
		g113_exec(t, db, `INSERT INTO backups (domain_id, dosya) VALUES (9700, ?)`, ad)
	}
	if m, tur := SilinenSahipsizIsaretle(context.Background(), db); tur != 0 {
		t.Errorf("🔴 S2: %d satır işaretlendi, %d satırın sk'sı türetilemedi (hiçbir listede görünmez, hiç silinmez)", m, tur)
	}
}

// BULGU F4 — SilinenSahipsizIsaretle her turda "ORDER BY id LIMIT 5000" alır; sk'sı türetilemeyen satırlar işaretlenmediği için
// her turda AYNI ilk 5000 satır gelir: ≥5000 türetilemeyen satır varsa sonrasındaki türetilebilir sahipsiz satırlar (id'si büyük)
// hiçbir turda işlenmez.
func TestDGBulgu_F4_SahipsizIsaretle_5000Turetilemeyen_SonrakiTuretilebilirleriAcliktanOldurmez(t *testing.T) {
	db := kDB(t)
	ctx := context.Background()
	logYakala(t)
	// D6 eşiği (aday > 100 ve aday > toplamın yarısı → toplu işaretleme ATLANIR) bu açlık testiyle çelişmesin diye gerçekçi bir DB:
	// canlı bir hosting + 6000 canlı yedek satırı; sahipsiz adaylar (5002) toplamın (11002) yarısından azdır.
	dgDomain(t, db, 8999, "canli-buyuk.test", "c_buyuk", 0)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya) SELECT 8999, CONCAT('c_buyuk-2026', LPAD(MOD(seq,10000),4,'0'), '-', LPAD(seq,6,'0'), '.tar.gz') FROM seq_1_to_6000`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya) SELECT 9000, CONCAT('elle-yuklendi-', seq, '.tar.gz') FROM seq_1_to_5000`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya) VALUES (9000, 'elle-yuklendi-5001.tar.gz'), (9001, 'c_son-20261001-000001.tar.gz')`)
	// keyset sayfalama (b.id > ?): TEK çağrı türetilemeyen 5001 satırı atlayıp ardındaki türetilebilir satırı da işaretler.
	if m, tur := SilinenSahipsizIsaretle(ctx, db); m != 1 || tur != 5001 {
		t.Errorf("tek turda 1 işaretlenen / 5001 türetilemeyen beklenirdi: %d / %d", m, tur)
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE dosya='c_son-20261001-000001.tar.gz' AND domain_id=0 AND silinme IS NOT NULL`) != 1 {
		t.Error("🔴 5000'den fazla türetilemeyen satır, ardındaki türetilebilir sahipsiz satırı işaretletmedi (LIMIT 5000 açlığı)")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=8999 AND silinme IS NULL`) != 6000 {
		t.Error("canlı hostingin satırlarına dokunulmamalı")
	}
}

// Kapsam: domaini VAR olan, zaten yetim olan, silme izi yarım (domain_id=0, silinme NULL) satırlara dokunulmaz; ikinci tur 0.
func TestDG_F5_SahipsizIsaretle_Kapsam_Idempotent(t *testing.T) {
	db := kDB(t)
	ctx := context.Background()
	logYakala(t)
	dgDomain(t, db, 9100, "canli.test", "c_canli", 0)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya) VALUES (9100, ?), (9101, ?), (0, ?)`, dgAd("c_canli", 0), dgAd("c_orfan", 0), dgAd("c_yarim", 0))
	yetim := silinenYetim(t, db, 9102, "z.test", "c_z", dgAd("c_z", 0), 0, "NULL")
	oncesi := dgDokumSorgu(t, db, "x", `SELECT id, domain_id, eski_domain_id, silinme, tip FROM backups WHERE id IN (1,3) OR id=?`, yetim)
	if m, tur := SilinenSahipsizIsaretle(ctx, db); m != 1 || tur != 0 {
		t.Fatalf("yalnız domaini olmayan 1 satır işaretlenmeli: %d,%d", m, tur)
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND eski_domain_id=9101 AND sistem_kullanici='c_orfan' AND alan_adi='c_orfan' AND reseller_id=0`) != 1 {
		t.Error("sahipsiz satır: domain_id=0, eski_domain_id=9101, alan_adi=sk beklenirdi")
	}
	if dgDokumSorgu(t, db, "x", `SELECT id, domain_id, eski_domain_id, silinme, tip FROM backups WHERE id IN (1,3) OR id=?`, yetim) != oncesi {
		t.Error("canlı / yarım iz / zaten yetim satırlar değişti")
	}
	if m, tur := SilinenSahipsizIsaretle(ctx, db); m != 0 || tur != 0 {
		t.Errorf("ikinci tur 0,0 olmalı: %d,%d", m, tur)
	}
}

func dgExplain(t *testing.T, db *sql.DB, q string, args ...any) []map[string]string {
	t.Helper()
	rows, err := db.Query("EXPLAIN "+q, args...)
	if err != nil {
		t.Fatalf("EXPLAIN: %v\n%s", err, q)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out []map[string]string
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		m := map[string]string{}
		for i, c := range cols {
			m[c] = vals[i].String
		}
		out = append(out, m)
	}
	return out
}

// 20.000 satır: iz UPDATE'i tek ifadede ve domain_id indeksiyle çalışır; sahipsiz tur 5000'lik dilimlerle tamamlanır.
func TestDG_F6_YirmiBinSatir_IzYazmaVeSahipsizTuru_SureVeIndeks(t *testing.T) {
	if testing.Short() {
		t.Skip("kısa modda atlandı (≈25 sn)")
	}
	db := kDB(t)
	ctx := context.Background()
	logYakala(t)
	// canlı yük: 200 başka domain × 1000 satır (200k) + silinecek domain 600'de 20000 satır (gerçekçi seçicilik ≈ %9)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici) SELECT 600+seq, CONCAT('d', seq, '.test'), CONCAT('c_d', seq) FROM seq_1_to_200`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, boyut_b) SELECT 601+MOD(seq,200), CONCAT('c_x-2026', LPAD(MOD(seq,10000),4,'0'), '-', LPAD(seq,6,'0'), '.tar.gz'), 5 FROM seq_1_to_200000`)
	dgDomain(t, db, 600, "perf.test", "c_perf", 0)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, boyut_b) SELECT 600, CONCAT('c_perf-2026', LPAD(MOD(seq,10000),4,'0'), '-', LPAD(seq,6,'0'), '.tar.gz'), 5 FROM seq_1_to_20000`)

	kaynak, err := os.ReadFile("silinen.go")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("(UPDATE backups SET alan_adi=\\?, sistem_kullanici=\\?, reseller_id=\\?, eski_domain_id=\\?, iz_id=\\?, domain_id=0, silinme=NOW\\(\\), son_kullanma=DATE_ADD\\(NOW\\(\\), INTERVAL \\? DAY\\) WHERE domain_id=\\?)").Find(kaynak)
	if m == nil {
		t.Fatal("iz UPDATE metni silinen.go'da bulunamadı (sorgu değişti: bu testi güncelleyin)")
	}
	plan := dgExplain(t, db, string(m), "perf.test", "c_perf", 0, 600, "iz-perf", 7, 600)
	var anahtar string
	for _, satir := range plan {
		if satir["table"] == "backups" {
			anahtar = satir["key"]
		}
	}
	// NOT: UPDATE WHERE'deki indeksli kolonu (domain_id) SET ettiği için MariaDB ix_backup_domain yerine PRIMARY taraması seçebilir
	// (EXPLAIN type=index); bu bir hata değil ama maliyet ölçülür: tablo boyutuyla doğrusal.
	dgOlcum(t, "iz UPDATE planı (anahtar=%q): %v", anahtar, plan)
	iz := SilinenIzHazirla(ctx, db, 600, "perf.test", "c_perf")
	g113_exec(t, db, `DELETE FROM domains WHERE id=600`)
	t0 := time.Now()
	iz.Yaz(ctx, db)
	sure := time.Since(t0)
	dgOlcum(t, "iz.Yaz (20000 satır, tek UPDATE): %v", sure)
	if sure > 10*time.Second {
		t.Errorf("iz.Yaz 20000 satırda %v sürdü (10 sn zaman aşımına yaklaşıyor)", sure)
	}
	// tipik hosting: 7 yedek satırı, 240k satırlık tabloda
	dgDomain(t, db, 5999, "kucuk.test", "c_kucuk", 0)
	for i := 0; i < 7; i++ {
		g113_exec(t, db, `INSERT INTO backups (domain_id, dosya) VALUES (5999, ?)`, dgAd("c_kucuk", i))
	}
	kiz := SilinenIzHazirla(ctx, db, 5999, "kucuk.test", "c_kucuk")
	g113_exec(t, db, `DELETE FROM domains WHERE id=5999`)
	t1 := time.Now()
	kiz.Yaz(ctx, db)
	dgOlcum(t, "iz.Yaz (7 satır, ~240k satırlık backups tablosu): %v", time.Since(t1))
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND eski_domain_id=600 AND silinme IS NOT NULL`) != 20000 {
		t.Error("20000 satırın hepsi tek ifadede yetim işaretlenmeliydi")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id BETWEEN 601 AND 800`) != 200000 {
		t.Error("başka domainlerin satırlarına dokunulmamalıydı")
	}

	// S2: 20000 sahipsiz satır (domain kayıtları yok), türetilebilir adlar → 5000'lik dilimler
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya) SELECT 5000+MOD(seq,50), CONCAT('c_orph-2026', LPAD(MOD(seq,10000),4,'0'), '-', LPAD(seq,6,'0'), '.tar.gz') FROM seq_1_to_20000`)
	plan2 := dgExplain(t, db, `SELECT b.id, b.domain_id, b.dosya FROM backups b
		 WHERE b.domain_id>0 AND b.silinme IS NULL AND NOT EXISTS (SELECT 1 FROM domains d WHERE d.id=b.domain_id)
		 ORDER BY b.id LIMIT 5000`)
	dgOlcum(t, "S2 seçim planı: %v", plan2)
	toplam, tur := 0, 0
	for ; tur < 8; tur++ {
		t0 := time.Now()
		m, _ := SilinenSahipsizIsaretle(ctx, db)
		d := time.Since(t0)
		dgOlcum(t, "S2 tur %d: %d satır işaretlendi, %v", tur+1, m, d)
		if d > 60*time.Second {
			t.Errorf("S2 turu %v sürdü", d)
		}
		toplam += m
		if m == 0 {
			break
		}
	}
	if toplam != 20000 {
		t.Errorf("S2 toplam %d satır işaretledi (20000 beklenirdi, %d turda)", toplam, tur)
	}
	for _, q := range []string{
		`SELECT id, eski_domain_id, sistem_kullanici, dosya FROM backups WHERE son_kullanma IS NOT NULL AND son_kullanma<=NOW() AND domain_id=0 AND silinme IS NOT NULL ORDER BY id LIMIT 200`,
		`SELECT id, eski_domain_id, dosya FROM backups WHERE sistem_kullanici='c_perf' AND domain_id=0 AND silinme IS NOT NULL ORDER BY id LIMIT 20000`,
		`SELECT DISTINCT eski_domain_id FROM backups WHERE sistem_kullanici='c_orph' AND alan_adi='c_orph' AND reseller_id=0 AND domain_id=0 AND silinme IS NOT NULL`,
	} {
		dgOlcum(t, "plan: %v", dgExplain(t, db, q))
	}
}

// ================================================================ G — süpürme

// Dosya (dizin değil) sembolik bağlantıysa YALNIZ bağlantı kaldırılır, hedef dosyaya dokunulmaz; sk "dizini" düz dosyaysa hiçbir şey silinmez.
func TestDG_G1_Supurme_SembolikBaglantiVeBozukDizin(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	ctx := context.Background()
	disari := filepath.Join(t.TempDir(), "disari.tar.gz")
	if err := os.WriteFile(disari, []byte("DISARI"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(o.kok, "c_ln"), 0o700); err != nil {
		t.Fatal(err)
	}
	lnk := filepath.Join(o.kok, "c_ln", dgAd("c_ln", 0))
	if err := os.Symlink(disari, lnk); err != nil {
		t.Fatal(err)
	}
	a := silinenYetim(t, o.db, 9300, "ln.test", "c_ln", dgAd("c_ln", 0), 0, "DATE_SUB(NOW(), INTERVAL 1 HOUR)")
	// sk dizini yerine DÜZ DOSYA
	if err := os.WriteFile(filepath.Join(o.kok, "c_df"), []byte("dosya"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := silinenYetim(t, o.db, 9301, "df.test", "c_df", dgAd("c_df", 0), 0, "DATE_SUB(NOW(), INTERVAL 1 HOUR)")
	// sk DİZİNİ sembolik bağlantı: dışarıdaki aynı adlı dosyaya dokunulmaz, satır korunur
	disDizin := t.TempDir()
	disDosya := filepath.Join(disDizin, dgAd("c_sd", 0))
	if err := os.WriteFile(disDosya, []byte("DISARI-2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(disDizin, filepath.Join(o.kok, "c_sd")); err != nil {
		t.Fatal(err)
	}
	c := silinenYetim(t, o.db, 9302, "sd.test", "c_sd", dgAd("c_sd", 0), 0, "DATE_SUB(NOW(), INTERVAL 1 HOUR)")
	SilinenSupur(ctx, o.db)
	if !varMi(disDosya) || !satirVarMi(t, o.db, c) {
		t.Error("🔴 sk dizini sembolik bağlantıyken dışarıdaki dosya silindi / satır silindi")
	}
	if !varMi(disari) {
		t.Fatal("🔴 sembolik bağlantının HEDEFİ (BackupRoot dışı dosya) silindi")
	}
	if varMi(lnk) || satirVarMi(t, o.db, a) {
		t.Error("bağlantının kendisi ve satırı silinmeliydi")
	}
	if !satirVarMi(t, o.db, b) || !varMi(filepath.Join(o.kok, "c_df")) {
		t.Error("sk 'dizini' düz dosyaysa satır/dosya korunmalı (güvensiz dizin)")
	}
}

// Yerel dosya yoksa, uzak hedef de yoksa satır yine silinir (yaşam döngüsü tıkanmaz).
func TestDG_G2_Supurme_YerelDosyaYoksa_SatirSilinir(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	id := silinenYetim(t, o.db, 9310, "yd.test", "c_yd", dgAd("c_yd", 0), 0, "DATE_SUB(NOW(), INTERVAL 1 HOUR)")
	SilinenSupur(context.Background(), o.db)
	if satirVarMi(t, o.db, id) {
		t.Error("diskte dosyası olmayan, uzak hedefi olmayan süresi dolmuş yetim satırı silinmeliydi")
	}
}

// TryLock: ilk süpürme uzak silmede takılıyken ikinci çağrı BEKLEMEZ ve hiçbir şey silmez; ilk bitince her satır TAM BİR kez işlenir.
func TestDG_G3_Supurme_EszamanliCagri_BeklemezVeCiftSilmez(t *testing.T) {
	o := dgYeni(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		silinenYetim(t, o.db, 9320, "es.test", "c_es", dgAd("c_es", i), 0, "DATE_SUB(NOW(), INTERVAL 1 DAY)")
	}
	eski := yetimUzakSil
	t.Cleanup(func() { yetimUzakSil = eski })
	girdi, birak := make(chan struct{}), make(chan struct{})
	var say int32
	yetimUzakSil = func(ctx context.Context, db *sql.DB, d *Destination, dosya string) error {
		if atomic.AddInt32(&say, 1) == 1 {
			close(girdi)
			<-birak
		}
		return nil
	}
	bitti := make(chan struct{})
	go func() { SilinenSupur(ctx, o.db); close(bitti) }()
	select {
	case <-girdi:
	case <-time.After(30 * time.Second):
		t.Fatal("ilk süpürme uzak silmeye girmedi")
	}
	t0 := time.Now()
	ikinci := make(chan struct{})
	go func() { SilinenSupur(ctx, o.db); close(ikinci) }()
	select {
	case <-ikinci:
	case <-time.After(5 * time.Second):
		t.Errorf("🔴 ikinci süpürme 5 sn içinde dönmedi: TryLock yerine bloklayan kilit (ilk süpürme uzak silmede takılı)")
	}
	if atomic.LoadInt32(&say) != 1 {
		t.Errorf("ikinci süpürme uzak silmeyi çağırdı: %d", say)
	}
	if d := time.Since(t0); d > 3*time.Second {
		t.Errorf("🔴 ikinci süpürme %v BEKLEDİ (TryLock anında dönmeli)", d)
	}
	close(birak)
	for _, k := range []chan struct{}{bitti, ikinci} {
		select {
		case <-k:
		case <-time.After(30 * time.Second):
			t.Fatal("süpürme bitmedi")
		}
	}
	if atomic.LoadInt32(&say) != 3 || g113_say(t, o.db, `SELECT COUNT(*) FROM backups`) != 0 {
		t.Errorf("her satır tam bir kez işlenip silinmeli: çağrı=%d", say)
	}
}

// Süpürme ortasında bağlam iptali: panik yok, kalan satırlar (dosya + kayıt) bozulmaz; yarım kalan ertesi tur tamamlanır.
func TestDG_G4_Supurme_OrtadaCtxIptali_KalanlarBozulmaz_ErtesiTurTamamlar(t *testing.T) {
	o := dgYeni(t)
	logYakala(t)
	var dosyalar []string
	var idler []int64
	for i := 0; i < 5; i++ {
		d := dgAd("c_ip", i)
		silinenDosya(t, o.kok, "c_ip", d)
		dosyalar = append(dosyalar, d)
		idler = append(idler, silinenYetim(t, o.db, 9330, "ip.test", "c_ip", d, 0, "DATE_SUB(NOW(), INTERVAL 1 DAY)"))
	}
	ctx, iptal := context.WithCancel(context.Background())
	defer iptal()
	eski := yetimUzakSil
	t.Cleanup(func() { yetimUzakSil = eski })
	var say int32
	yetimUzakSil = func(c context.Context, db *sql.DB, d *Destination, dosya string) error {
		if atomic.AddInt32(&say, 1) == 2 {
			iptal() // ikinci satırın uzak silmesi sırasında iptal
		}
		return nil
	}
	SilinenSupur(ctx, o.db)
	for i := 2; i < 5; i++ {
		if !satirVarMi(t, o.db, idler[i]) || !varMi(filepath.Join(o.kok, "c_ip", dosyalar[i])) {
			t.Errorf("iptalden sonraki satır %d dokunulmadan kalmalı", i)
		}
	}
	// ertesi tur (yeni bağlam, uzak sağlam): hepsi temizlenir, yarım kalan da
	yetimUzakSil = func(context.Context, *sql.DB, *Destination, string) error { return nil }
	SilinenSupur(context.Background(), o.db)
	if g113_say(t, o.db, `SELECT COUNT(*) FROM backups`) != 0 {
		t.Error("ertesi tur kalanları (yarım kalan dahil) temizlemeli")
	}
	for _, d := range dosyalar {
		if varMi(filepath.Join(o.kok, "c_ip", d)) {
			t.Errorf("dosya kaldı: %s", d)
		}
	}
}

// Sınır değerleri: son_kullanma tam "şimdi" → silinir; yarın → kalır; NULL → kalır.
func TestDG_G5_Supurme_SonKullanmaSinirDegerleri(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	simdi := silinenYetim(t, o.db, 9340, "s.test", "c_sn", dgAd("c_sn", 0), 0, "NOW()")
	yarin := silinenYetim(t, o.db, 9340, "s.test", "c_sn", dgAd("c_sn", 1), 0, "DATE_ADD(NOW(), INTERVAL 1 DAY)")
	yok := silinenYetim(t, o.db, 9340, "s.test", "c_sn", dgAd("c_sn", 2), 0, "NULL")
	time.Sleep(1100 * time.Millisecond) // saniye çözünürlüğü: "şimdi" kesinlikle <= NOW()
	SilinenSupur(context.Background(), o.db)
	if satirVarMi(t, o.db, simdi) {
		t.Error("son_kullanma<=NOW() silinmeli")
	}
	if !satirVarMi(t, o.db, yarin) || !satirVarMi(t, o.db, yok) {
		t.Error("yarın / NULL satırlar kalmalı")
	}
}

// ================================================================ H — silme akışı bağımsızlığı

// Altyapı bozukken (tablolar yok) iz hazırlama/yazma: panik yok, asılma yok (hepsi < 3 sn); kritik olmayan tablolar
// (cp_ayarlar, backup_destinations, backup_yetim_hedefler, domains) yoksa satırlar YİNE yetim işaretlenir.
func TestDG_H1_IzHazirlaYaz_TabloEksikken_PanikVeAsilmaYok_SatirlarYineIsaretlenir(t *testing.T) {
	vakalar := []struct {
		ad     string
		dusur  []string
		isaret bool // satırlar yine de yetim işaretlenmeli mi
	}{
		{"cp_ayarlar yok", []string{"cp_ayarlar"}, true},
		{"backup_destinations yok", []string{"backup_destinations"}, true},
		{"backup_yetim_hedefler yok", []string{"backup_yetim_hedefler"}, true},
		{"domains yok", []string{"domains"}, true},
		{"hepsi yok (backups hariç)", []string{"cp_ayarlar", "backup_destinations", "backup_yetim_hedefler", "backup_uzak_kopyalar"}, true},
		{"backups yok", []string{"backups"}, false},
	}
	for _, v := range vakalar {
		t.Run(v.ad, func(t *testing.T) {
			db := kDB(t)
			logYakala(t)
			ctx := context.Background()
			kHesap(t, db, 9400, "h.test", "c_h", 3, "ftp.h.invalid", dgAd("c_h", 0), dgAd("c_h", 1))
			var iz *SilinenIz
			t0 := time.Now()
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("🔴 PANİK: %v", r)
					}
				}()
				if v.ad == "domains yok" {
					g113_exec(t, db, `SET FOREIGN_KEY_CHECKS=0`)
				}
				for _, tb := range v.dusur {
					if tb == "domains" {
						continue // iz hazırlandıktan sonra düşürülür
					}
					g113_exec(t, db, "DROP TABLE "+tb)
				}
				iz = SilinenIzHazirla(ctx, db, 9400, "h.test", "c_h")
				if iz == nil {
					t.Fatal("geçerli girdiyle iz nil olmamalı")
				}
				if _, err := db.Exec(`DELETE FROM domains WHERE id=9400`); err != nil {
					t.Fatal(err)
				}
				if v.ad == "domains yok" {
					g113_exec(t, db, `DROP TABLE domains`)
				}
				iz.Yaz(ctx, db)
			}()
			if d := time.Since(t0); d > 3*time.Second {
				t.Errorf("🔴 %v sürdü (asılma?)", d)
			}
			if v.isaret {
				if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND eski_domain_id=9400 AND silinme IS NOT NULL AND sistem_kullanici='c_h'`) != 2 {
					t.Error("destek tabloları eksik olsa da satırlar yetim işaretlenmeli")
				}
			}
		})
	}
}

// nil DB, kapalı DB, iptal edilmiş bağlam: panik yok, hızlı dönüş; iptal edilmiş bağlamda satırlar DEĞİŞMEZ ve S2 turu sonradan kurtarır.
func TestDG_H2_NilKapaliVeIptalEdilmisBaglam(t *testing.T) {
	logYakala(t)
	ctx := context.Background()
	if iz := SilinenIzHazirla(ctx, nil, 1, "x.test", "c_x"); iz != nil {
		t.Error("nil db ile iz hazırlanamaz (panik yutulup nil dönmeli)")
	}
	(&SilinenIz{alanAdi: "x.test", sk: "c_x", domainID: 1, slot: 7}).Yaz(ctx, nil) // panik yok
	(&SilinenIz{alanAdi: "x.test", sk: "c_x", domainID: 1, slot: 7, hedef: &yetimHedefHam{host: "h", parola: "p"}}).Yaz(ctx, nil)

	db := kDB(t)
	kHesap(t, db, 9410, "i.test", "c_i", 0, "ftp.i.invalid", dgAd("c_i", 0))
	iz := SilinenIzHazirla(ctx, db, 9410, "i.test", "c_i")
	g113_exec(t, db, `DELETE FROM domains WHERE id=9410`)
	iptal, cancel := context.WithCancel(ctx)
	cancel()
	t0 := time.Now()
	iz.Yaz(iptal, db)
	if d := time.Since(t0); d > time.Second {
		t.Errorf("iptal edilmiş bağlamda %v sürdü", d)
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=9410 AND silinme IS NULL`) != 1 {
		t.Fatal("iptal edilmiş bağlam satırı DEĞİŞTİRMEMELİ")
	}
	if m, _ := SilinenSahipsizIsaretle(ctx, db); m != 1 {
		t.Errorf("S2 turu iz yazılamayan satırı kurtarmalı: %d", m)
	}
	kapali := kDB(t)
	iz2 := &SilinenIz{alanAdi: "k.test", sk: "c_k", domainID: 3, slot: 7}
	_ = kapali.Close()
	iz2.Yaz(ctx, kapali)
	// D3: hosting satırı OKUNAMIYORSA iz hazırlanmaz (nil): eskiden hata yutulup sahip=0 (= kök) yazılıyordu.
	if SilinenIzHazirla(ctx, kapali, 3, "k.test", "c_k") != nil {
		t.Error("kapalı DB'de sahip bilinmeden iz hazırlanmamalı (nil): reseller_id=0 (= kök) yazılırdı")
	}
}

// Yavaş/kilitli tablo: backups WRITE kilidi altında iz yazma, bağlam süresiyle SINIRLI kalır, satırları bozmaz; kilit kalkınca S2 kurtarır.
func TestDG_H3_KilitliBackupsTablosu_YazmaBaglamSuresiyleSinirli(t *testing.T) {
	db := kDB(t)
	logYakala(t)
	ctx := context.Background()
	kHesap(t, db, 9420, "l.test", "c_l", 0, "ftp.l.invalid", dgAd("c_l", 0))
	iz := SilinenIzHazirla(ctx, db, 9420, "l.test", "c_l")
	g113_exec(t, db, `DELETE FROM domains WHERE id=9420`)

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `LOCK TABLES backups WRITE`); err != nil {
		t.Fatal(err)
	}
	kilitli := true
	defer func() {
		if kilitli {
			_, _ = conn.ExecContext(ctx, `UNLOCK TABLES`)
		}
		_ = conn.Close()
	}()
	kisa, iptal := context.WithTimeout(ctx, 400*time.Millisecond)
	defer iptal()
	t0 := time.Now()
	iz.Yaz(kisa, db)
	if d := time.Since(t0); d > 3*time.Second {
		t.Errorf("🔴 kilitli tabloda Yaz bağlam süresini (400ms) aşıp %v sürdü", d)
	}
	_, _ = conn.ExecContext(ctx, `UNLOCK TABLES`)
	kilitli = false
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=9420 AND silinme IS NULL`) != 1 {
		t.Fatal("zaman aşımına uğrayan Yaz satırı değiştirmemeli")
	}
	if m, _ := SilinenSahipsizIsaretle(ctx, db); m != 1 {
		t.Errorf("kilit kalkınca S2 kurtarmalı: %d", m)
	}
}

// Silme isteğinin yedek tablosundaki kilitten kaynaklanan EN KÖTÜ gecikmesi (varsayılan bağlam): iz hazırlama ve yazma
// AYRI AYRI 3 sn (izZamanAsimi) ile sınırlı (kilit asılırsa DELETE isteği ≈ 3 sn gecikir; iz best-effort, S2 tamamlar).
func TestDG_H3b_KilitliBackupsTablosu_VarsayilanBaglam_EnKotuGecikmeSiniri(t *testing.T) {
	if testing.Short() {
		t.Skip("kısa modda atlandı (≈10 sn)")
	}
	db := kDB(t)
	logYakala(t)
	ctx := context.Background()
	kHesap(t, db, 9430, "m.test", "c_m", 0, "", dgAd("c_m", 0))
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `LOCK TABLES backups WRITE`); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, `UNLOCK TABLES`); _ = conn.Close() }()
	t0 := time.Now()
	iz := SilinenIzHazirla(ctx, db, 9430, "m.test", "c_m")
	hazir := time.Since(t0)
	yazildi := make(chan struct{})
	go func() { iz.Yaz(ctx, db); close(yazildi) }()
	select {
	case <-yazildi:
	case <-time.After(25 * time.Second):
		t.Errorf("🔴 kilitli tabloda iz.Yaz 25 sn içinde dönmedi (zaman aşımı yok): silme isteği süresiz bekler")
	}
	toplam := time.Since(t0)
	dgOlcum(t, "kilitli backups tablosu: IzHazirla=%v, toplam(IzHazirla+Yaz)=%v", hazir, toplam)
	// Karar (koordinatör): iz hazırlama/yazma zaman aşımı 3 sn — kilitli tabloda silme isteğine eklenen gecikme ≈ 3 sn (eskiden +10 sn).
	if izZamanAsimi != 3*time.Second {
		t.Errorf("izZamanAsimi 3 sn olmalı: %v", izZamanAsimi)
	}
	if toplam > 7*time.Second {
		t.Errorf("🔴 silme isteğine eklenen en kötü gecikme %v (iz hazırlama + yazma, her biri 3 sn ile sınırlı: ≤ 7 sn beklenirdi)", toplam)
	}
}

// HostingYik sırası (yik.go AST): SilinenIzHazirla yıkımdan ÖNCE, iz.Yaz ise `DELETE FROM domains` BAŞARILI döndükten SONRA
// (hata dalı return eder) ve tam bir kez çağrılır.
func TestDG_H4_HostingYik_SilmeSirasi_IzYazSilmedenSonra(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "../domains/yik.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var fn *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "HostingYik" {
			fn = fd
		}
	}
	if fn == nil {
		t.Fatal("HostingYik bulunamadı")
	}
	pos := map[string][]token.Pos{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ad := sel.Sel.Name
		if id, ok := sel.X.(*ast.Ident); ok {
			ad = id.Name + "." + ad
		}
		pos[ad] = append(pos[ad], c.Pos())
		if sel.Sel.Name == "ExecContext" {
			for _, a := range c.Args {
				if bl, ok := a.(*ast.BasicLit); ok && strings.Contains(bl.Value, "DELETE FROM domains") {
					pos["DELETE_domains"] = append(pos["DELETE_domains"], c.Pos())
				}
			}
		}
		return true
	})
	tek := func(ad string) token.Pos {
		if len(pos[ad]) != 1 {
			t.Fatalf("%s çağrısı %d kez (tam 1 beklenirdi)", ad, len(pos[ad]))
		}
		return pos[ad][0]
	}
	hazirla, yaz := tek("backups.SilinenIzHazirla"), tek("iz.Yaz")
	drop, deprov, sil := tek("hesaplar.MySQLDropAllForDomain"), tek("provisioner.Deprovision"), tek("DELETE_domains")
	if !(hazirla < drop && drop < deprov && deprov < sil && sil < yaz) {
		t.Errorf("sıra bozuk: IzHazirla<MySQLDrop<Deprovision<DELETE domains<iz.Yaz olmalı (%d %d %d %d %d)", hazirla, drop, deprov, sil, yaz)
	}
	// yapı: `if _, err = db.ExecContext(...DELETE FROM domains...); err != nil { return ... }` hemen ardından `iz.Yaz(...)`
	ok := false
	for i, st := range fn.Body.List {
		ifs, isIf := st.(*ast.IfStmt)
		if !isIf || ifs.Init == nil {
			continue
		}
		as, isAs := ifs.Init.(*ast.AssignStmt)
		if !isAs || len(as.Rhs) != 1 {
			continue
		}
		c, isCall := as.Rhs[0].(*ast.CallExpr)
		if !isCall {
			continue
		}
		silme := false
		for _, a := range c.Args {
			if bl, b := a.(*ast.BasicLit); b && strings.Contains(bl.Value, "DELETE FROM domains") {
				silme = true
			}
		}
		if !silme {
			continue
		}
		donus := false
		for _, s := range ifs.Body.List {
			if _, r := s.(*ast.ReturnStmt); r {
				donus = true
			}
		}
		if !donus {
			t.Error("DELETE FROM domains hata dalı return etmiyor: iz.Yaz başarısız silmede de çalışırdı")
		}
		if i+1 >= len(fn.Body.List) {
			t.Fatal("DELETE'ten sonra ifade yok")
		}
		es, isExpr := fn.Body.List[i+1].(*ast.ExprStmt)
		if !isExpr {
			t.Fatalf("DELETE hata dalından hemen sonra iz.Yaz bekleniyordu: %T", fn.Body.List[i+1])
		}
		call, isCall2 := es.X.(*ast.CallExpr)
		if !isCall2 {
			t.Fatal("iz.Yaz çağrısı bekleniyordu")
		}
		if sel, s := call.Fun.(*ast.SelectorExpr); !s || sel.Sel.Name != "Yaz" {
			t.Fatal("DELETE hata dalından hemen sonra iz.Yaz bekleniyordu")
		}
		ok = true
	}
	if !ok {
		t.Error("`if _, err = db.ExecContext(... DELETE FROM domains ...); err != nil { return }` yapısı bulunamadı")
	}
}

// domains satırını silen TEK yıkım yolu HostingYik'tir (iz yazan); SQL metni olarak "DELETE FROM domains" içeren başka bir kod yolu
// eklenirse iz yazılmaz → yedek satırları sahipsiz kalır (S2 kurtarır ama gecikmeli): bilinçli izin listesi (yorumlar sayılmaz).
func TestDG_H5_DomainsSilenYollar_IzinListesi(t *testing.T) {
	izinli := map[string]string{
		"../domains/yik.go":  "HostingYik (iz yazar)",
		"../tasima/aktar.go": "taşıma geri alma: yeni açılan, yedeği olmayan hesap",
	}
	var bulunan []string
	for _, kok := range []string{"..", "../../cmd"} {
		_ = filepath.WalkDir(kok, func(yol string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(yol, ".go") || strings.HasSuffix(yol, "_test.go") {
				return nil
			}
			f, e := parser.ParseFile(token.NewFileSet(), yol, nil, 0)
			if e != nil {
				return nil
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if bl, ok := n.(*ast.BasicLit); ok && bl.Kind == token.STRING && strings.Contains(strings.ToUpper(bl.Value), "DELETE FROM DOMAINS") {
					bulunan = append(bulunan, filepath.ToSlash(yol))
				}
				return true
			})
			return nil
		})
	}
	for _, y := range bulunan {
		if _, ok := izinli[y]; !ok {
			t.Errorf("%s SQL metni olarak 'DELETE FROM domains' içeriyor ve izin listesinde değil: hosting silerken yedek izi (SilinenIzHazirla/Yaz) yazılmıyor olabilir", y)
		}
	}
	if len(bulunan) == 0 {
		t.Error("hiç DELETE FROM domains bulunamadı: yol/arama bozuk")
	}
}

// dgOlcum: ölçüm/plan çıktısı (go test -v ile görünür).
func dgOlcum(t *testing.T, bicim string, a ...any) {
	t.Helper()
	t.Logf(bicim, a...)
}

// ================================================================ ek bulgular (S2 eşiği, yerel silme hatası, denetim)

// BULGU D6 — SilinenSahipsizIsaretle `domains` tablosuna TAM güvenir. Tablo boşsa/eksikse (başka bir DB yedeğinden dönülmüş, kısmen
// yüklenmiş, yanlış şema/bağlantı) TÜM canlı yedek satırları "sahipsiz" sayılıp yetim işaretlenir; saklama süresi (≥7 gün) sonunda
// 04:00 süpürmesi dosyaları + uzak kopyaları siler. Toplu işaretleme bir eşik/güvenlik kapısından geçmeli.
func TestDGBulgu_D6_SahipsizTuru_DomainsTablosuBosken_TumYedeklerYetimYapilmamali(t *testing.T) {
	db := kDB(t)
	ctx := context.Background()
	logYakala(t)
	const toplam = 180
	for d := 0; d < 3; d++ {
		dgDomain(t, db, int64(6000+d), fmt.Sprintf("d%d.test", d), fmt.Sprintf("c_live%d", d), 0)
		g113_exec(t, db, `INSERT INTO backups (domain_id, dosya) SELECT ?, CONCAT(?, '-2026', LPAD(seq,4,'0'), '-', LPAD(seq,6,'0'), '.tar.gz') FROM seq_1_to_60`,
			int64(6000+d), fmt.Sprintf("c_live%d", d))
	}
	g113_exec(t, db, `DELETE FROM domains`) // domains tablosu boşaldı
	m, _ := SilinenSahipsizIsaretle(ctx, db)
	if m*2 > toplam {
		t.Errorf("🔴 domains boşken %d/%d canlı yedek satırı 'sahipsiz' sayılıp yetim işaretlendi (eşik/kapı yok): %d gün sonra dosya + uzak kopyalar silinir", m, toplam, silinenEnAzGun)
	}
}

// BULGU D3 — yetimYedegiSil yerel dosya silme hatasını (yedekKaydiniYokEtFn'in döndürdüğü yerelHata) YUTAR: dosya silinemediğinde (salt-okunur/
// değişmez dosya, kilitli dizin, EIO) satır yine de silinir, dosya diskte kayıtsız/sahipsiz kalır; günlük "temizlendi" der.
func TestDGBulgu_D3_Supurme_YerelDosyaSilinemezse_SatirKorunmali(t *testing.T) {
	o := dgYeni(t)
	yetimUzakSahte(t)
	logs := logYakala(t)
	dosya := dgAd("c_dd", 0)
	yol := filepath.Join(o.kok, "c_dd", dosya)
	// silinemeyen "dosya": içi dolu bir dizin (os.Remove → ENOTEMPTY); gerçekte immutable/salt-okunur dosya aynı sonucu verir
	if err := os.MkdirAll(yol, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(yol, "x"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	id := silinenYetim(t, o.db, 9350, "dd.test", "c_dd", dosya, 0, "DATE_SUB(NOW(), INTERVAL 1 HOUR)")
	SilinenSupur(context.Background(), o.db)
	if !satirVarMi(t, o.db, id) && varMi(yol) {
		t.Errorf("🔴 yerel dosya SİLİNEMEDİ ama kayıt silindi: dosya diskte kayıtsız kalır (günlük: %q)", logs.String())
	}
}

// BULGU D2 — SilinenSil KISMİ başarısızlıkta (bazı yetimler kalıcı silindi, bazıları uzak hata yüzünden kaldı) hiç denetim kaydı yazmaz:
// kalıcı silinen yedeklerin izi kalmaz.
func TestDGBulgu_D2_SilUcu_KismiBasarisizlik_DenetimKaydiYazilmali(t *testing.T) {
	o := dgYeni(t)
	sahte := yetimUzakSahte(t)
	logYakala(t)
	kHesap(t, o.db, 7101, "a.com", "c_dn", 0, "ftp.a.invalid", dgAd("c_dn", 0))
	kSil(t, o.db, 7101, "a.com", "c_dn")
	kHesap(t, o.db, 7102, "b.com", "c_dn", 0, "ftp.b.invalid", dgAd("c_dn", 1))
	kSil(t, o.db, 7102, "b.com", "c_dn")
	sahte.hata = func(d *Destination, _ string) error {
		if d != nil && d.Host == "ftp.b.invalid" {
			return fmt.Errorf("uzak kapalı")
		}
		return nil
	}
	kod, g := advIstek(o.r, "DELETE", "/s/c_dn", "")
	if kod != 409 || !strings.Contains(g, `"silinen":1`) {
		t.Fatalf("kısmi başarı bekleniyordu: %d %s", kod, g)
	}
	if g113_say(t, o.db, `SELECT COUNT(*) FROM audit_log WHERE action LIKE 'silinen-yedek.%'`) == 0 {
		t.Error("🔴 1 yedek KALICI silindi ama denetim kaydı yok (kısmi başarısızlıkta yalnız tam başarıda denetim yazılıyor)")
	}
}
