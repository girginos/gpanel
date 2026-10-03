package plans_test

// donustur_geri_test.go — D1 GERİ DÖNÜŞ senaryosu (kalıcı fix). Gerçek MariaDB.
//
// Senaryo: güncelleme yeni ikiliyle açılır, dönüşüm kopyaları üst yazıma çevirir,
// sonra sağlık kontrolü düşer → girginospanel-update ESKİ ikiliyi + güncelleme ÖNCESİ
// dump'ı geri yükler. Dump yalnız eski tabloları (service_plans, domains, audit_log)
// DROP+CREATE eder; 0120 tabloları (hosting_limit_ust, service_plans_ozel_arsiv)
// geri alınan zaman çizgisinin satırlarıyla kalır (betik artık onları kaldırıyor;
// elle `zcat dump | mysql` yapılırsa yine kalır — bu test o en kötü durumu kurar).
//
// Kanıtlanan:
//  1. Eski ikilinin okuma sorgusu (plan_id → service_plans, 0037 disk/inode mirası)
//     dönüşümden önce ile geri yüklemeden sonra AYNI → uygulanan limit değişmez.
//  2. Dönüşüm sonrası DB'yi (geri yüklemeden) eski ikili OKURSA limitler DEĞİŞİR →
//     geri dönüş kapısı (assets/ops geri_donus_kapisi) neden şart.
//  3. Yeni ikilinin efektif limiti (tek kaynak efektiflimit) = eski ikilinin dönüşüm
//     öncesi okuduğu değer (posta mirası hariç).
//  4. Tekrar yeni ikiliye geçiş: bayat kalıntı temizlenir, eski ikilinin arada
//     kopyada yaptığı değişiklik KORUNUR (bayat üst yazım onu ezmez), arşivde çift
//     satır yok, dönüşüm takılmaz; üçüncü koşu no-op (idempotent).

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"girginospanel/internal/efektiflimit"
	"girginospanel/internal/plans"
)

// eskiIkiliOku — D1 ÖNCESİ ikilinin okuma sorgusunun taklidi: limit plan_id'deki
// service_plans satırından; disk/inode'da domains.disk_kota_mb/inode_kota (0037) >0
// ise o. hosting_limit_ust'u BİLMEZ.
func eskiIkiliOku(t *testing.T, db *sql.DB) map[int64]map[string]int {
	t.Helper()
	var b strings.Builder
	b.WriteString("SELECT d.id, COALESCE(d.disk_kota_mb,0), COALESCE(d.inode_kota,0)")
	for _, a := range efektiflimit.Alanlar {
		fmt.Fprintf(&b, ", COALESCE(p.`%s`,0)", a.Sutun)
	}
	b.WriteString(" FROM domains d LEFT JOIN service_plans p ON p.id=d.plan_id")
	rows, err := db.Query(b.String())
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[int64]map[string]int{}
	for rows.Next() {
		var id int64
		var dDisk, dInode int
		v := make([]int, len(efektiflimit.Alanlar))
		h := []any{&id, &dDisk, &dInode}
		for i := range v {
			h = append(h, &v[i])
		}
		if err := rows.Scan(h...); err != nil {
			t.Fatal(err)
		}
		m := map[string]int{}
		for i, a := range efektiflimit.Alanlar {
			m[a.Sutun] = v[i]
		}
		if dDisk > 0 {
			m["disk_kota_mb"] = dDisk
		}
		if dInode > 0 {
			m["inode_kota"] = dInode
		}
		out[id] = m
	}
	return out
}

// gd_dumpAl / gd_dumpGeriYukle — `mysqldump --databases panel` + `zcat | mysql`
// taklidi: yalnız dump'taki (D1 öncesi) tablolar FK denetimi KAPALI geri yazılır
// (dump başlığı FOREIGN_KEY_CHECKS=0 → domains'in yeniden yazılması üst yazımı
// CASCADE ile silmez — gerçekteki gibi kalıntı kalır).
var gdDumpTablolari = []string{"service_plans", "domains", "audit_log"}

func gd_dumpAl(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, tb := range gdDumpTablolari {
		pk_exec(t, db, "CREATE TABLE dump_"+tb+" LIKE "+tb)
		pk_exec(t, db, "INSERT INTO dump_"+tb+" SELECT * FROM "+tb)
	}
}

func gd_dumpGeriYukle(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	c, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	q := []string{"SET FOREIGN_KEY_CHECKS=0"}
	for _, tb := range gdDumpTablolari {
		q = append(q, "DELETE FROM "+tb, "INSERT INTO "+tb+" SELECT * FROM dump_"+tb)
	}
	q = append(q, "SET FOREIGN_KEY_CHECKS=1")
	for _, s := range q {
		if _, err := c.ExecContext(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

func gd_esit(a, b map[int64]map[string]int, haric map[int64]map[string]bool) []string {
	var f []string
	for id, m := range a {
		for k, v := range m {
			if haric[id][k] {
				continue
			}
			if b[id][k] != v {
				f = append(f, fmt.Sprintf("hosting %d %s: %d → %d", id, k, v, b[id][k]))
			}
		}
	}
	return f
}

func gd_yeniIkiliOku(t *testing.T, db *sql.DB) map[int64]map[string]int {
	t.Helper()
	out := map[int64]map[string]int{}
	for id, s := range dn_hepsi(t, db) {
		m := map[string]int{}
		for _, a := range efektiflimit.Alanlar {
			m[a.Sutun] = s.Efektif(a.Sutun, 0)
		}
		out[id] = m
	}
	return out
}

func TestOzelPlanGeriDonusVeYenidenGecis(t *testing.T) {
	db := pk_geciciDB(t)
	dn_goc(t, db)
	ctx := context.Background()
	pk_exec(t, db, `UPDATE service_plans SET max_email=5, saatlik_mail_limiti=50, mail_kutu_kota_mb=300 WHERE id IN (1,200,201,202)`)
	pk_exec(t, db, `UPDATE service_plans SET max_email=5, saatlik_mail_limiti=50, mail_kutu_kota_mb=300 WHERE id IN (100,210)`)
	// Kopya 210 (hosting 20) ← A Plani 200; kopya 100 (hosting 10, kök) ← Standart 1.
	dn_denetim(t, db, "hosting.olustur", "a-site.test", "plan=200")
	dn_denetim(t, db, "hosting.ozel_plan", "a-site.test — Özel", "plan=210")
	dn_denetim(t, db, "hosting.plan", "kok-site.test", "yeni plan=1")
	dn_denetim(t, db, "hosting.ozel_plan", "kok-site.test — Özel", "plan=100")
	// Kopya 420 (hosting 42): posta mirası kuralı (kopyada saatlik/kutu 0).
	pk_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, plan_id, reseller_id) VALUES (42,'a-posta.test','c_a5',420,2)`)
	dn_kopya(t, db, 420, "a-posta.test — Özel", 2, 42, 200, "saatlik_mail_limiti=0, mail_kutu_kota_mb=0, ram_mb=3000")
	dn_denetim(t, db, "hosting.plan", "a-posta.test", "yeni plan=200")
	dn_denetim(t, db, "hosting.ozel_plan", "a-posta.test — Özel", "plan=420")
	// 0037 mirası (disk alan değeri) olan hosting.
	pk_exec(t, db, `UPDATE domains SET disk_kota_mb=4321 WHERE id=20`)
	// Bayat SAYILMAMASI gereken satır: hosting 22'nin üst yazımı kaynak olarak bir KATALOG
	// planını gösteriyor (kimlik yeniden kullanımı taklidi; katalog planında domain_id yok).
	pk_exec(t, db, `INSERT INTO hosting_limit_ust (domain_id, ram_mb, kaynak_ozel_plan_id) VALUES (22, 1500, 200)`)

	postaHaric := map[int64]map[string]bool{42: {"saatlik_mail_limiti": true, "mail_kutu_kota_mb": true}}
	eskiOnce := eskiIkiliOku(t, db)
	eskiOnce[22]["ram_mb"] = 1500 // 22'nin önceden var olan üst yazımı (eski ikili bilmez; karşılaştırma yeni ikiliyle)
	gd_dumpAl(t, db)

	// ── Güncelleme: yeni ikili açılır, dönüşüm.
	r1, err := plans.OzelPlanlariDonustur(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(r1.Donusen) != 3 || len(r1.Atlanan) != 0 || r1.BayatUst != 0 || r1.BayatArsiv != 0 {
		t.Fatalf("ilk dönüşüm: %+v", r1)
	}
	// (3) Tek kaynak: yeni ikilinin efektif limiti = eski ikilinin dönüşüm öncesi değeri.
	if f := gd_esit(eskiOnce, gd_yeniIkiliOku(t, db), postaHaric); len(f) > 0 {
		t.Errorf("dönüşüm efektif limiti değiştirdi: %v", f)
	}
	// (2) Senaryo kanıtı: dönüşmüş DB'yi eski ikili OKURSA limitler değişir (kapı şart).
	eskiAz := eskiIkiliOku(t, db)
	eskiAz[22]["ram_mb"] = 1500
	if f := gd_esit(eskiOnce, eskiAz, postaHaric); len(f) == 0 {
		t.Errorf("senaryo değişti: dönüşüm sonrası eski okuyucu aynı değeri görüyor — geri dönüş kapısını yeniden değerlendirin")
	} else {
		t.Logf("geri yüklemesiz eski ikili şunları YANLIŞ görürdü (kapı bunu engeller): %v", f)
	}

	// ── Sağlık düştü → eski ikili + güncelleme öncesi dump geri yüklenir (kalıntı kalır).
	gd_dumpGeriYukle(t, db)
	if n := pk_sayi(t, db, `SELECT COUNT(*) FROM hosting_limit_ust WHERE domain_id IN (10,20,42)`); n == 0 {
		t.Fatal("test kurgusu: kalıntı üst yazım bekleniyordu")
	}
	// (1) Geri dönüş sonrası eski ikilinin gördüğü = güncelleme öncesi (uygulanan limit değişmez).
	eskiSonra := eskiIkiliOku(t, db)
	eskiSonra[22]["ram_mb"] = 1500
	if f := gd_esit(eskiOnce, eskiSonra, nil); len(f) > 0 {
		t.Errorf("geri dönüş sonrası eski ikilinin uyguladığı limit değişti: %v", f)
	}

	// ── Eski ikili ARADA çalışır: yönetici kopyayı düzenler (eski arayüz).
	pk_exec(t, db, `UPDATE service_plans SET ram_mb=3333, cpu_yuzde=175 WHERE id=210`)
	eskiAra := eskiIkiliOku(t, db)
	eskiAra[22]["ram_mb"] = 1500

	// ── Tekrar yeni ikiliye geçiş.
	r2, err := plans.OzelPlanlariDonustur(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if r2.BayatUst != 3 || r2.BayatArsiv != 3 || len(r2.Donusen) != 3 || len(r2.Atlanan) != 0 {
		t.Fatalf("yeniden geçiş: bayat ust=%d arsiv=%d dönüşen=%d atlanan=%+v", r2.BayatUst, r2.BayatArsiv, len(r2.Donusen), r2.Atlanan)
	}
	// (4) Eski ikilinin arada yaptığı değişiklik korunur; tüm hostinglerde efektif = eski ikilinin son gördüğü.
	yeni := gd_yeniIkiliOku(t, db)
	if f := gd_esit(eskiAra, yeni, postaHaric); len(f) > 0 {
		t.Errorf("yeniden geçişte efektif limit değişti (bayat üst yazım?): %v", f)
	}
	if yeni[20]["ram_mb"] != 3333 || yeni[20]["cpu_yuzde"] != 175 {
		t.Errorf("eski ikilinin kopyadaki değişikliği ezildi: ram=%d cpu=%d", yeni[20]["ram_mb"], yeni[20]["cpu_yuzde"])
	}
	if yeni[22]["ram_mb"] != 1500 || pk_sayi(t, db, `SELECT COUNT(*) FROM hosting_limit_ust WHERE domain_id=22`) != 1 {
		t.Errorf("kanıtsız (katalog kaynaklı) üst yazım silinmemeliydi")
	}
	if n := pk_sayi(t, db, `SELECT COUNT(*) FROM service_plans_ozel_arsiv`); n != 3 {
		t.Errorf("arşivde çift/eksik satır: %d (beklenen 3)", n)
	}
	if n := pk_sayi(t, db, `SELECT COUNT(*) FROM service_plans WHERE id IN (100,210,420)`); n != 0 {
		t.Errorf("kopyalar yeniden dönüşmedi: %d kaldı", n)
	}
	// Posta mirası yeniden geçişte de yalnız 0 değerler için (tabandan 50/300).
	if yeni[42]["saatlik_mail_limiti"] != 50 || yeni[42]["mail_kutu_kota_mb"] != 300 || yeni[42]["ram_mb"] != 3000 {
		t.Errorf("42 posta/ram: %v", yeni[42])
	}

	// Üçüncü koşu: no-op.
	r3, err := plans.OzelPlanlariDonustur(ctx, db)
	if err != nil || len(r3.Donusen)+len(r3.Atlanan)+len(r3.Temizlenen) != 0 || r3.BayatUst+r3.BayatArsiv != 0 {
		t.Errorf("üçüncü koşu idempotent değil: %+v %v", r3, err)
	}
	if f := gd_esit(yeni, gd_yeniIkiliOku(t, db), nil); len(f) > 0 {
		t.Errorf("üçüncü koşu efektifi değiştirdi: %v", f)
	}
}
