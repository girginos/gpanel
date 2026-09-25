package bildirim

// bildirim_kilit_test.go — BLD-1 / BLD-2: "okundu" yazımı ile sahiplik devrinin
// bildirim taşıması, eşzamanlı koşunca InnoDB kilitlenmesine (1213) düşmemeli;
// düşerse (kurban seçilirse) işlem baştan denenmeli.
//
// Gerçek MariaDB (REPEATABLE-READ, InnoDB) gerekir — bkz. bildirim_test.go
// (GOSP_TEST_GECICI_DSN). DB yoksa testler ATLANIR.

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// bk_yukle — cp_bildirim'i baştan kurar. Satırlar araya karışık dizilir ki farklı
// kapsamların okundu kayıtları cp_bildirim_okundu'da aynı aralıklara düşsün
// (paylaşımlı aralık kilidi ↔ ekleme çakışması da denensin).
func bk_yukle(t *testing.T, db *sql.DB) {
	t.Helper()
	bt_exec(t, db, `DELETE FROM cp_bildirim_okundu`)
	bt_exec(t, db, `DELETE FROM cp_bildirim`)
	bt_exec(t, db, `UPDATE domains SET reseller_id = CASE id WHEN 11 THEN 8 ELSE 7 END`)
	var deger []string
	var arg []any
	for i := 0; i < 30; i++ {
		ekle := func(dom, rid any) {
			deger = append(deger, "('kritik','yedek',?,'m',?,?)")
			arg = append(arg, fmt.Sprintf("b%d", i), dom, rid)
		}
		ekle(10, 7)    // bayi 7 + müşteri 10 ortak
		ekle(11, 8)    // bayi 8 + müşteri 11
		ekle(nil, nil) // panel geneli (yönetici)
		ekle(12, 7)    // bayi 7'nin başka domaini
		if i%5 == 0 {
			ekle(10, 8) // domain 10'un ESKİ sahibi (8) döneminden kalan geçmiş
		}
	}
	bt_exec(t, db, `INSERT INTO cp_bildirim (seviye,kategori,baslik,mesaj,domain_id,reseller_id) VALUES `+
		strings.Join(deger, ","), arg...)
}

// bk_devirTx — devret()'in bildirim tarafı: TEK işlemde sahiplik + SahipDevri.
func bk_devirTx(ctx context.Context, db *sql.DB, did, eski, yeni int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE domains SET reseller_id=? WHERE id=?`, yeni, did); err != nil {
		return err
	}
	if err := SahipDevri(ctx, tx, did, eski, yeni); err != nil {
		return err
	}
	return tx.Commit()
}

// 🔴 BLD-1/BLD-2 KÖK NEDEN: kilit sırası. TEK deneme (yeniden deneme YOK) ile
// eşzamanlı "tümünü okundu"lar ve sahiplik devri kilitlenmeye düşmemeli. Eski
// okunduYaz (INSERT IGNORE…SELECT ile S kilit → UPDATE ile X'e yükseltme) aynı
// yarışta turların çoğunda 1213 veriyordu.
func TestOkunduYazTek_EszamanliKilitlenmez(t *testing.T) {
	db := bt_semaHazir(t)
	db.SetMaxOpenConns(16)
	ctx := context.Background()
	bt_exec(t, db, `INSERT INTO domains (id, reseller_id) VALUES (10, 7), (11, 8), (12, 7)`)

	bayi7, bayi8 := okuyan{okuyanBayi, 7}, okuyan{okuyanBayi, 8}
	m10, m11, yon := okuyan{okuyanMusteri, 10}, okuyan{okuyanMusteri, 11}, okuyan{okuyanAdmin, 1}
	okuma := func(o okuyan) func() error {
		return func() error { return okunduYazTek(ctx, db, o, 0) }
	}
	devir := func() error { return bk_devirTx(ctx, db, 10, 7, 8) }

	senaryolar := []struct {
		ad    string
		isler []func() error
	}{
		{"bayi+müşteri aynı satırlar", []func() error{okuma(bayi7), okuma(m10)}},
		{"aynı bayi çift istek", []func() error{okuma(bayi7), okuma(bayi7)}},
		{"ayrık kapsamlar", []func() error{okuma(bayi7), okuma(bayi8), okuma(yon), okuma(m11)}},
		{"devir 7→8 + tüm okuyanlar", []func() error{devir, okuma(bayi7), okuma(m10), okuma(bayi8), okuma(yon)}},
	}
	const tur = 60
	for _, s := range senaryolar {
		var hatalar []string
		hataliTur := 0
		for i := 0; i < tur; i++ {
			bk_yukle(t, db)
			var wg sync.WaitGroup
			basla := make(chan struct{})
			hata := make([]error, len(s.isler))
			for j, is := range s.isler {
				wg.Add(1)
				go func(j int, is func() error) {
					defer wg.Done()
					<-basla
					time.Sleep(time.Duration(rand.IntN(1500)) * time.Microsecond)
					hata[j] = is()
				}(j, is)
			}
			close(basla)
			wg.Wait()
			onceki := len(hatalar)
			for _, e := range hata {
				if e != nil {
					hatalar = append(hatalar, e.Error())
				}
			}
			if len(hatalar) > onceki {
				hataliTur++
			}
		}
		if len(hatalar) > 0 {
			t.Errorf("%s: %d/%d turda toplam %d hata (tek deneme, yeniden deneme yok) — ilk: %s",
				s.ad, hataliTur, tur, len(hatalar), hatalar[0])
		} else {
			t.Logf("%s: %d tur, 0 hata", s.ad, tur)
		}
	}

	// Son durum doğru mu: her okuyanın görünür satırlarının tamamı onun için okundu.
	bk_yukle(t, db)
	for _, o := range []okuyan{bayi7, bayi8, m10, m11, yon} {
		if err := okunduYazTek(ctx, db, o, 0); err != nil {
			t.Fatal(err)
		}
		kos, arg := o.gorunurluk()
		okK, okArg := o.okumadi()
		if n := bt_sayi(t, db, `SELECT COUNT(*) FROM cp_bildirim b WHERE `+kos+` AND `+okK, append(arg, okArg...)...); n != 0 {
			t.Fatalf("%s #%d: tümünü okundu sonrası %d okunmamış kaldı", o.tur, o.id, n)
		}
	}
	if n := bt_sayi(t, db, `SELECT COUNT(*) FROM cp_bildirim WHERE okundu = 0`); n != 0 {
		t.Fatalf("'en az biri okudu' bayrağı %d satırda yazılmadı", n)
	}
}

// 🔴 BLD-2: gerçek bir kilitlenmede okunduYaz KURBAN seçilirse işlem baştan
// denenmeli (istek 500 almamalı). Kilitlenme belirlenimci kurulur: ağır bir
// işlem (A) r2'yi tutar; okunduYaz r1'i kilitleyip r2'yi bekler; A r1'i ister →
// döngü. InnoDB küçük işlemi (okunduYaz) geri alır.
func TestOkunduYaz_KilitlenmeKurbaniYenidenDener(t *testing.T) {
	db := bt_semaHazir(t)
	ctx := context.Background()
	gunluk := bk_logYakala(t)
	bt_exec(t, db, `INSERT INTO domains (id, reseller_id) VALUES (10, 7)`)
	r1 := bk_satir(t, db, 10, 7)
	r2 := bk_satir(t, db, 10, 7)
	agir := bk_agirTablo(t, db)

	a, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Rollback() }()
	if _, err := a.ExecContext(ctx, `UPDATE `+agir+` SET v = v + 1`); err != nil {
		t.Fatal(err)
	}
	var x int64
	if err := a.QueryRowContext(ctx, `SELECT id FROM cp_bildirim WHERE id=? FOR UPDATE`, r2).Scan(&x); err != nil {
		t.Fatal(err)
	}
	aBag := bk_baglantiID(t, a)

	sonuc := make(chan error, 1)
	go func() { sonuc <- okunduYaz(ctx, db, okuyan{okuyanBayi, 7}, 0) }()
	bk_bekleyeniBekle(t, db, aBag, sonuc)

	// Döngüyü kapat: A, okunduYaz'ın tuttuğu r1'i istiyor.
	if err := a.QueryRowContext(ctx, `SELECT id FROM cp_bildirim WHERE id=? FOR UPDATE`, r1).Scan(&x); err != nil {
		t.Fatalf("ağır işlem kurban seçildi (senaryo kurulamadı): %v", err)
	}
	if err := a.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-sonuc; err != nil {
		t.Fatalf("BLD-2: okunduYaz kilitlenme kurbanı olunca yeniden denemedi, hata döndü: %v", err)
	}
	if g := gunluk.String(); !strings.Contains(g, "kilit çakışması") || !strings.Contains(g, "1213") {
		t.Fatalf("senaryo okunduYaz'ı kilitlenme kurbanı yapmadı (yeniden deneme günlüğü yok) — test geçersiz: %q", g)
	}
	if n := bt_sayi(t, db, `SELECT COUNT(*) FROM cp_bildirim_okundu WHERE okuyan_tur='bayi' AND okuyan_id=7 AND bildirim_id IN (?,?)`, r1, r2); n != 2 {
		t.Fatalf("yeniden denemeden sonra iki bildirim de okundu olmalı, bulunan=%d", n)
	}
}

// BLD-2: işlem başarısız olsa bile istemciye ham SQL hatası (tablo adı, sürücü
// metni) gitmez.
func TestOkundu_HataHamSQLSizdirmaz(t *testing.T) {
	db := bt_semaHazir(t)
	h := &Handlers{DB: db}
	bt_exec(t, db, `INSERT INTO domains (id, reseller_id) VALUES (10, 7)`)
	bk_satir(t, db, 10, 7)
	bt_exec(t, db, `DROP TABLE cp_bildirim_okundu`)

	req := httptest.NewRequest(http.MethodPost, "/bildirimler/0/okundu", nil)
	req.Header.Set("Authorization", "Bearer "+bt_bayi(t, 7))
	rec := httptest.NewRecorder()
	bt_router(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("tablo yokken 500 beklenir, gelen %d %s", rec.Code, rec.Body.String())
	}
	govde := rec.Body.String()
	for _, yasak := range []string{"cp_bildirim", "Error ", "1146", "exist"} {
		if strings.Contains(govde, yasak) {
			t.Fatalf("istemciye ham SQL hatası sızdı (%q): %s", yasak, govde)
		}
	}
}

// 🔴 BLD-1: SahipDevri eski sahibin okundu kayıtlarını KİLİTSİZ okur. Kilitli
// okuma (ya da eski NOT EXISTS alt sorgusu) cp_bildirim_okundu'ya paylaşımlı
// aralık kilidi koyar; o sırada YENİ sahibin "tümünü okundu"su (dizin aralığını
// tutup okundu kaydı ekleyen) ile döngü kurulur: devir yeni sahibin dizin
// aralığına ekleme için onu, o da okundu eklemesi için devri bekler → 1213.
// Belirlenimci: yeni sahip (9) önce aralığını kilitler, devir ona takılır, sonra
// yeni sahip okundu kaydını ekler — ikisi de hatasız bitmeli.
func TestSahipDevri_OkunduKaydiniKilitlemez(t *testing.T) {
	db := bt_semaHazir(t)
	ctx := context.Background()
	bt_exec(t, db, `INSERT INTO domains (id, reseller_id) VALUES (10, 7), (11, 8), (13, 9)`)
	bk_satir(t, db, 10, 7)      // devredilecek (açık)
	bk_satir(t, db, 11, 8)      // aralıkları ayıran başka bayi
	y := bk_satir(t, db, 13, 9) // yeni sahibin kendi bildirimi

	// Yeni sahibin okunduYazTek'i, adım adım: 1) kapsamını kilitle.
	b9, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b9.Rollback() }()
	if _, err := kimlikler(ctx, b9, `SELECT b.id FROM cp_bildirim b WHERE b.reseller_id = ? FOR UPDATE`, 9); err != nil {
		t.Fatal(err)
	}
	b9Bag := bk_baglantiID(t, b9)

	sonuc := make(chan error, 1)
	go func() { sonuc <- bk_devirTx(ctx, db, 10, 7, 9) }()
	bk_bekleyeniBekle(t, db, b9Bag, sonuc) // devir, yeni sahibin aralığına eklemede bekliyor

	// 2) okundu kaydını ekle — devir cp_bildirim_okundu'yu kilitlemediyse beklemez.
	if _, err := b9.ExecContext(ctx, `INSERT IGNORE INTO cp_bildirim_okundu (bildirim_id, okuyan_tur, okuyan_id) VALUES (?, 'bayi', 9)`, y); err != nil {
		t.Fatalf("BLD-1: yeni sahibin okundu kaydı devrin kilidine takıldı (kilitlenme): %v", err)
	}
	if err := b9.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-sonuc; err != nil {
		t.Fatalf("BLD-1: bildirim devri yeni sahibin okumasıyla kilitlendi: %v", err)
	}
	if n := bt_sayi(t, db, `SELECT COUNT(*) FROM cp_bildirim WHERE domain_id=10 AND reseller_id=9`); n != 1 {
		t.Fatalf("açık bildirim yeni sahibe taşınmadı")
	}
}

// SahipDevri kilit sırası için idx_bildirim_reseller'ı zorlar; dizin (0082) bir
// kurulumda yoksa devir DÜŞMEMELİ, ipucusuz sorguyla aynı sonucu vermeli.
func TestSahipDevri_DizinYoksaIpucusuzCalisir(t *testing.T) {
	db := bt_semaHazir(t)
	ctx := context.Background()
	bt_exec(t, db, `INSERT INTO domains (id, reseller_id) VALUES (40, 7)`)
	acik := bk_satir(t, db, 40, 7)
	okunan := bk_satir(t, db, 40, 7)
	bt_exec(t, db, `INSERT INTO cp_bildirim_okundu (bildirim_id, okuyan_tur, okuyan_id) VALUES (?, 'bayi', 7)`, okunan)
	bt_exec(t, db, `ALTER TABLE cp_bildirim DROP INDEX idx_bildirim_reseller`)

	if err := bk_devirTx(ctx, db, 40, 7, 9); err != nil {
		t.Fatalf("dizin yokken bildirim devri düştü: %v", err)
	}
	if n := bt_sayi(t, db, `SELECT COUNT(*) FROM cp_bildirim WHERE id=? AND reseller_id=9`, acik); n != 1 {
		t.Fatalf("açık bildirim yeni sahibe taşınmadı")
	}
	if n := bt_sayi(t, db, `SELECT COUNT(*) FROM cp_bildirim WHERE id=? AND reseller_id=7`, okunan); n != 1 {
		t.Fatalf("eski sahibin okuduğu bildirim onda kalmalıydı")
	}
}

// KilitCakismasi / KilitDenemeli sözleşmesi: yalnız 1213/1205 yeniden denenir,
// deneme sayısı sınırlıdır, başka hata hemen döner.
func TestKilitDenemeli_Sozlesme(t *testing.T) {
	ctx := context.Background()
	kilit := fmt.Errorf("sarılı: %w", &mysqlHata1213)
	n := 0
	if err := KilitDenemeli(ctx, func() error {
		n++
		if n < 3 {
			return kilit
		}
		return nil
	}); err != nil || n != 3 {
		t.Fatalf("kilitlenme 2 kez yeniden denenip başarmalıydı: err=%v deneme=%d", err, n)
	}
	n = 0
	if err := KilitDenemeli(ctx, func() error { n++; return kilit }); err == nil || n != KilitDenemeSayisi {
		t.Fatalf("deneme sınırlı olmalı (%d), yapılan=%d err=%v", KilitDenemeSayisi, n, err)
	}
	n = 0
	baska := fmt.Errorf("başka hata")
	if err := KilitDenemeli(ctx, func() error { n++; return baska }); err != baska || n != 1 {
		t.Fatalf("kilit dışı hata yeniden denenmemeli: deneme=%d err=%v", n, err)
	}
}

// --- yardımcılar ---

var mysqlHata1213 = mysql.MySQLError{Number: 1213, Message: "Deadlock found when trying to get lock; try restarting transaction"}

func bk_satir(t *testing.T, db *sql.DB, dom, rid int64) int64 {
	t.Helper()
	res := bt_exec(t, db, `INSERT INTO cp_bildirim (seviye,kategori,baslik,mesaj,domain_id,reseller_id) VALUES ('kritik','yedek','b','m',?,?)`, dom, rid)
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// bk_agirTablo — kilitlenmede kurban OLMAMASI gereken işleme ağırlık (geri alma
// kaydı) kazandırmak için 3000 satırlık tablo.
func bk_agirTablo(t *testing.T, db *sql.DB) string {
	t.Helper()
	bt_exec(t, db, `CREATE TABLE bk_agir (id INT PRIMARY KEY, v INT NOT NULL) ENGINE=InnoDB`)
	var parca []string
	for i := 1; i <= 3000; i++ {
		parca = append(parca, fmt.Sprintf("(%d,0)", i))
	}
	bt_exec(t, db, `INSERT INTO bk_agir VALUES `+strings.Join(parca, ","))
	return "bk_agir"
}

// bk_baglantiID — tx'in sunucu bağlantı kimliği.
func bk_baglantiID(t *testing.T, tx *sql.Tx) uint64 {
	t.Helper()
	var id uint64
	if err := tx.QueryRow(`SELECT CONNECTION_ID()`).Scan(&id); err != nil {
		t.Fatalf("bağlantı kimliği okunamadı: %v", err)
	}
	return id
}

// bk_bekleyeniBekle — BU test veritabanında, A'nın (haric) ve sorgulayanın
// dışındaki bir bağlantının ifadesi 100 ms'den uzun sürene (= A'nın kilidini
// bekleyene) dek bekler. information_schema.PROCESSLIST canlıdır;
// INNODB_TRX/INNODB_LOCK_WAITS ise 100 ms'lik önbellekten okunur ve sık
// sorgulanınca (başka paketlerin testleri dahil) hiç tazelenmez.
func bk_bekleyeniBekle(t *testing.T, db *sql.DB, haric uint64, bitti <-chan error) {
	t.Helper()
	son := time.Now().Add(10 * time.Second)
	for time.Now().Before(son) {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.PROCESSLIST
			WHERE DB = DATABASE() AND ID <> CONNECTION_ID() AND ID <> ?
			  AND COMMAND IN ('Query','Execute') AND TIME_MS > 100`, haric).Scan(&n); err == nil && n > 0 {
			return
		}
		select {
		case err := <-bitti:
			t.Fatalf("işlem kilidi beklemeden bitti (err=%v) — senaryo kurulamadı", err)
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("hiçbir işlem kilit beklemeye girmedi — senaryo kurulamadı\n%s", bk_kilitDokumu(db))
}

// bk_kilitDokumu — tanı için işlem/kilit bekleme durumu.
func bk_kilitDokumu(db *sql.DB) string {
	var b strings.Builder
	for _, q := range []string{
		`SELECT CONCAT_WS(' | ', trx_id, trx_state, trx_mysql_thread_id, LEFT(COALESCE(trx_query,''),120)) FROM information_schema.INNODB_TRX`,
		`SELECT CONCAT_WS(' | ', requesting_trx_id, blocking_trx_id) FROM information_schema.INNODB_LOCK_WAITS`,
		`SELECT CONCAT_WS(' | ', ID, COALESCE(DB,''), COMMAND, COALESCE(STATE,''), LEFT(COALESCE(INFO,''),120)) FROM information_schema.PROCESSLIST WHERE DB = DATABASE()`,
	} {
		rows, err := db.Query(q)
		if err != nil {
			fmt.Fprintf(&b, "%v\n", err)
			continue
		}
		for rows.Next() {
			var v string
			_ = rows.Scan(&v)
			b.WriteString(v + "\n")
		}
		rows.Close()
	}
	return b.String()
}

// bk_logYakala — standart günlüğü test boyunca tampona yönlendirir.
func bk_logYakala(t *testing.T) *bytes.Buffer {
	t.Helper()
	var b bytes.Buffer
	eski := log.Writer()
	log.SetOutput(&b)
	t.Cleanup(func() { log.SetOutput(eski) })
	return &b
}
