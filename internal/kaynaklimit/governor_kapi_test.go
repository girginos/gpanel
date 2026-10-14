package kaynaklimit

// governor_kapi_test.go — yavaş-sorgu watchdog'unun "etkin limit yoksa processlist'i tarama" kapısı:
//   - hiçbir tenant için efektif db_max_query_seconds > 0 değilken `mysql` istemcisi fork EDİLMEZ,
//   - karar governorKararTTL boyunca saklanır (kaynağa her tikte gidilmez), değişim en geç TTL sonra görülür,
//   - karar okunamazsa eski davranış (tara) sürer (fail-open).

import (
	"bytes"
	"context"
	"database/sql"
	"log"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// gkSahte: governorLimitVar'ı sabitler (çağrı sayacıyla); t.Cleanup ile eskisini geri koyar.
func gkSahte(t *testing.T, limitVar func() bool) *int64 {
	t.Helper()
	eski := governorLimitVar
	t.Cleanup(func() { governorLimitVar = eski })
	var cagri int64
	governorLimitVar = func(*sql.DB) bool {
		atomic.AddInt64(&cagri, 1)
		return limitVar()
	}
	return &cagri
}

// gkBekle: koşul sağlanana dek (en çok sure) bekler.
func gkBekle(t *testing.T, kosul func() bool, sure time.Duration) {
	t.Helper()
	son := time.Now().Add(sure)
	for !kosul() {
		if time.Now().After(son) {
			t.Fatal("koşul zamanında sağlanmadı")
		}
		time.Sleep(time.Millisecond)
	}
}

// gkTavan: ceil(a/b).
func gkTavan(a, b time.Duration) int { return int((a + b - 1) / b) }

// Limit yokken (ve varken) karar TTL boyunca saklanır: 5 sn'lik tiklerde kaynağa her tikte DEĞİL,
// TTL'de bir bakılır; limit yoksa hiçbir tikte tarama istenmez.
func TestGovernorKapisi_LimitYokkenTaramaYokKararSaklanir(t *testing.T) {
	cagri := gkSahte(t, func() bool { return false })
	var k governorKapisi
	t0 := time.Unix(1_000_000, 0)
	toplam := 100 * time.Second
	for g := time.Duration(0); g < toplam; g += governorPollInterval {
		if k.taramaGerekli(nil, t0.Add(g)) {
			t.Fatalf("limit yokken %v'de tarama istendi", g)
		}
	}
	if got, want := int(atomic.LoadInt64(cagri)), gkTavan(toplam, governorKararTTL); got != want {
		t.Fatalf("karar kaynağına bakış sayısı = %d, want %d (TTL=%v)", got, want, governorKararTTL)
	}
}

func TestGovernorKapisi_LimitVarkenHerTiktaTarar(t *testing.T) {
	cagri := gkSahte(t, func() bool { return true })
	var k governorKapisi
	t0 := time.Unix(1_000_000, 0)
	toplam := 100 * time.Second
	tik := 0
	for g := time.Duration(0); g < toplam; g += governorPollInterval {
		tik++
		if !k.taramaGerekli(nil, t0.Add(g)) {
			t.Fatalf("limit varken %v'de tarama istenmedi", g)
		}
	}
	if tik == 0 {
		t.Fatal("test verisi: tik yok")
	}
	if got, want := int(atomic.LoadInt64(cagri)), gkTavan(toplam, governorKararTTL); got != want {
		t.Fatalf("karar kaynağına bakış sayısı = %d, want %d", got, want)
	}
}

// Karar değişimi (limit tanımlandı / kaldırıldı) en geç governorKararTTL sonra görülür; önbellek
// TTL içinde HONOR edilir (erken görülmez).
func TestGovernorKapisi_KararDegisimiEnGecTTLSonraGorulur(t *testing.T) {
	limit := false
	gkSahte(t, func() bool { return limit })
	var gunluk bytes.Buffer // karar değişimi bir kez loglanır (her kontrolde değil)
	eskiYazici := log.Writer()
	log.SetOutput(&gunluk)
	t.Cleanup(func() { log.SetOutput(eskiYazici) })
	var k governorKapisi
	t0 := time.Unix(1_000_000, 0)
	ac, kapa := 40*time.Second, 100*time.Second
	ilkTarama, sonTarama := time.Duration(-1), time.Duration(-1)
	for g := time.Duration(0); g < 220*time.Second; g += governorPollInterval {
		limit = g >= ac && g < kapa
		if k.taramaGerekli(nil, t0.Add(g)) {
			if ilkTarama < 0 {
				ilkTarama = g
			}
			sonTarama = g
		}
	}
	if ilkTarama < 0 {
		t.Fatal("limit tanımlandığı halde hiç tarama istenmedi")
	}
	if ilkTarama <= ac || ilkTarama > ac+governorKararTTL {
		t.Fatalf("tarama %v'de başladı; (%v, %v] aralığında olmalı (önbellek honor edilmeli, gecikme <= TTL)", ilkTarama, ac, ac+governorKararTTL)
	}
	if sonTarama < kapa-governorPollInterval || sonTarama >= kapa+governorKararTTL {
		t.Fatalf("son tarama %v; limit kalkınca en geç %v'de durmalı", sonTarama, kapa+governorKararTTL)
	}
	// yok (ilk karar) → var → yok: 2 "askıda" + 1 "açık" satırı; kontrol sayısı kadar değil.
	if a, b := strings.Count(gunluk.String(), "taraması askıda"), strings.Count(gunluk.String(), "taraması açık"); a != 2 || b != 1 {
		t.Fatalf("karar-değişimi logları: askıda=%d açık=%d, want 2/1\n%s", a, b, gunluk.String())
	}
}

// Karar okunamazsa (DB hatası) eski davranış sürer: tara (fail-open).
func TestGovernorLimitVarMi_DBOkunamazsaTaramaSurer(t *testing.T) {
	db, err := sql.Open("mysql", "u:p@tcp(127.0.0.1:1)/x")
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close() // sonraki her sorgu hata verir
	if !governorLimitVarMi(db) {
		t.Fatal("DB okunamayınca fail-open (true) beklenirdi")
	}
}

// Efektif limit = plan ⊕ hostinge özel üst yazım; yalnız db_accounts'ta hesabı olan hostingler sayılır.
// Yalnız GEÇİCİ MariaDB (ol_db: GOSP_TEST_GECICI_DSN yoksa atlanır).
func TestGovernorLimitVarMi_EfektifLimiteBakar(t *testing.T) {
	db := ol_db(t) // plan 1: 30 sn; hosting 1: üst yazım 5 sn + hesap c_a_db; hosting 2: plansız, hesapsız
	adim := func(sql_ string) {
		t.Helper()
		if _, err := db.Exec(sql_); err != nil {
			t.Fatalf("%v\n%s", err, sql_)
		}
	}
	kontrol := func(ad string, want bool) {
		t.Helper()
		if got := governorLimitVarMi(db); got != want {
			t.Fatalf("%s: governorLimitVarMi = %v, want %v", ad, got, want)
		}
	}
	kontrol("üst yazım 5 sn (plan 30)", true)
	adim("UPDATE hosting_limit_ust SET db_max_query_seconds=0 WHERE domain_id=1")
	kontrol("üst yazım 0 plan limitini ezer", false)
	adim("UPDATE hosting_limit_ust SET db_max_query_seconds=NULL WHERE domain_id=1")
	kontrol("üst yazım yok → plan 30", true)
	adim("UPDATE service_plans SET db_max_query_seconds=0")
	kontrol("plan 0 + üst yazım yok", false)
	adim("INSERT INTO db_accounts (domain_id, db_name, db_user) VALUES (2, 'c_b_db', 'c_b_db')")
	kontrol("iki hosting da limitsiz", false)
	adim("UPDATE hosting_limit_ust SET db_max_query_seconds=7 WHERE domain_id=1")
	kontrol("biri 7 sn (diğeri 0: ÜST SINIR alınır)", true)
	adim("DELETE FROM db_accounts")
	kontrol("limit var ama DB hesabı yok → öldürülecek sorgu sahibi yok", false)
}

// SlowQueryWatchdog döngüsü taramayı kapıya bağlar: limit yokken governorTara HİÇ çağrılmaz,
// limit varken sürekli çağrılır; karar kaynağına TTL içinde bir kez bakılır.
func TestSlowQueryWatchdog_TaramayiKapiyaBaglar(t *testing.T) {
	db, err := sql.Open("mysql", "u:p@tcp(127.0.0.1:1)/x") // seam'ler araya girer; hiç bağlanılmaz
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	eskiTik, eskiTara := governorTikAraligi, governorTara
	t.Cleanup(func() { governorTikAraligi, governorTara = eskiTik, eskiTara })
	governorTikAraligi = time.Millisecond

	for _, c := range []struct {
		ad    string
		limit bool
	}{{"limit yok", false}, {"limit var", true}} {
		t.Run(c.ad, func(t *testing.T) {
			var tarama int64
			bakis := gkSahte(t, func() bool { return c.limit })
			governorTara = func(*sql.DB) { atomic.AddInt64(&tarama, 1) }
			ctx, cancel := context.WithCancel(context.Background())
			bitti := make(chan struct{})
			go func() {
				defer close(bitti)
				SlowQueryWatchdog(ctx, db)
			}()
			t.Cleanup(func() { cancel(); <-bitti })

			gkBekle(t, func() bool { return atomic.LoadInt64(bakis) >= 1 }, 3*time.Second)
			if c.limit {
				gkBekle(t, func() bool { return atomic.LoadInt64(&tarama) >= 5 }, 3*time.Second)
			} else {
				time.Sleep(100 * time.Millisecond) // ~100 tik daha: hiçbirinde tarama olmamalı
				if n := atomic.LoadInt64(&tarama); n != 0 {
					t.Fatalf("limit yokken %d tarama yapıldı", n)
				}
			}
			if b := atomic.LoadInt64(bakis); b != 1 {
				t.Fatalf("TTL (%v) içinde karar kaynağına %d kez bakıldı, 1 bekleniyordu", governorKararTTL, b)
			}
		})
	}
}
