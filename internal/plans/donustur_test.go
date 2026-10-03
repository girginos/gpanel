package plans_test

// donustur_test.go — D1 açılış dönüşümü: eski "<alan> — Özel" kopyaları → hostinge
// özel üst yazım. Gerçek MariaDB (GOSP_TEST_GECICI_DSN); göç 0120 dosyadan koşulur
// (iki kez — idempotent).
//
// Kullanıcı kararı: yalnız farklar üst yazım, gerisi (posta dahil) tabandan miras;
// tabanı belirsiz kopya dönüştürülmez + raporlanır. Değişmez: her hostingin 22 alanı
// (posta mirası hariç) ve efektif WAF'ı dönüşümden önce ve sonra AYNI.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"girginospanel/internal/efektiflimit"
	"girginospanel/internal/plans"
)

func dn_goc(t *testing.T, db *sql.DB) {
	t.Helper()
	b, err := os.ReadFile("../../migrations/0120_hosting_limit_ust.sql")
	if err != nil {
		t.Fatal(err)
	}
	var temiz []string
	for _, l := range strings.Split(string(b), "\n") {
		if s := strings.TrimSpace(l); s == "" || strings.HasPrefix(s, "--") {
			continue
		}
		temiz = append(temiz, l)
	}
	for tur := 0; tur < 2; tur++ { // göç koşucusu gibi: ikinci koşu zararsız olmalı
		for _, st := range strings.Split(strings.Join(temiz, "\n"), ";") {
			if st = strings.TrimSpace(st); st == "" {
				continue
			}
			if _, err := db.Exec(st); err != nil {
				m := strings.ToLower(err.Error())
				if tur == 1 && (strings.Contains(m, "duplicate") || strings.Contains(m, "already exists")) {
					continue
				}
				t.Fatalf("göç (tur %d): %v\n%s", tur+1, err, st)
			}
		}
	}
}

func dn_waf(t *testing.T, db *sql.DB, did int64) string {
	t.Helper()
	var en, pl int
	var mode string
	if err := db.QueryRow(`SELECT COALESCE(d.waf_enabled, p.waf_enabled, 0),
		LOWER(TRIM(COALESCE(NULLIF(TRIM(d.waf_mode),''), p.waf_mode, 'on'))), COALESCE(NULLIF(d.waf_paranoia,0), p.waf_paranoia, 1)
		FROM domains d LEFT JOIN service_plans p ON p.id=d.plan_id WHERE d.id=?`, did).Scan(&en, &mode, &pl); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%d/%s/%d", en, mode, pl)
}

func dn_kopya(t *testing.T, db *sql.DB, id int64, ad string, sahip, domainID int64, kaynak int64, set string) {
	t.Helper()
	pk_exec(t, db, `INSERT INTO service_plans (id, ad, aciklama, disk_kota_mb, trafik_kota_mb, max_domain, max_db, max_email, max_ftp,
		varsayilan, cpu_yuzde, ram_mb, max_process, inode_kota, io_agirlik, mysql_max_baglanti, php_surum, fastcgi_cache,
		client_max_body_mb, nginx_ek_direktifler, pm_max_children, io_read_mbps, io_write_mbps, io_read_iops, io_write_iops,
		db_max_queries_per_hour, db_max_updates_per_hour, db_max_query_seconds, waf_enabled, waf_mode, waf_paranoia,
		reseller_id, domain_id, saatlik_mail_limiti, mail_kutu_kota_mb)
		SELECT ?, ?, 'kopya', disk_kota_mb, trafik_kota_mb, max_domain, max_db, max_email, max_ftp,
		0, cpu_yuzde, ram_mb, max_process, inode_kota, io_agirlik, mysql_max_baglanti, php_surum, fastcgi_cache,
		client_max_body_mb, nginx_ek_direktifler, pm_max_children, io_read_mbps, io_write_mbps, io_read_iops, io_write_iops,
		db_max_queries_per_hour, db_max_updates_per_hour, db_max_query_seconds, waf_enabled, waf_mode, waf_paranoia,
		?, ?, saatlik_mail_limiti, mail_kutu_kota_mb FROM service_plans WHERE id=?`, id, ad, sahip, domainID, kaynak)
	if set != "" {
		pk_exec(t, db, "UPDATE service_plans SET "+set+" WHERE id=?", id)
	}
}

func dn_denetim(t *testing.T, db *sql.DB, eylem, hedef, not string) {
	t.Helper()
	pk_exec(t, db, `INSERT INTO audit_log (action, target, detail, reseller_id) VALUES (?,?,JSON_OBJECT('not', ?),0)`, eylem, hedef, not)
}

func dn_hepsi(t *testing.T, db *sql.DB) map[int64]efektiflimit.Sonuc {
	t.Helper()
	rows, err := db.Query(`SELECT id FROM domains`)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	m := map[int64]efektiflimit.Sonuc{}
	for _, id := range ids {
		s, err := efektiflimit.Oku(context.Background(), db, id)
		if err != nil {
			t.Fatal(err)
		}
		m[id] = s
	}
	return m
}

func TestOzelPlanDonusum(t *testing.T) {
	db := pk_geciciDB(t)
	dn_goc(t, db)
	ctx := context.Background()
	// Plan 200'e posta değerleri (posta mirası durumu için).
	pk_exec(t, db, `UPDATE service_plans SET max_email=5, saatlik_mail_limiti=50, mail_kutu_kota_mb=300 WHERE id IN (200,201,202)`)
	pk_exec(t, db, `UPDATE service_plans SET max_email=5, saatlik_mail_limiti=50, mail_kutu_kota_mb=300 WHERE id=210`)

	// Hosting 20 (A) kopya 210 ← A Plani (200): denetim ipucu. 200/201/202 BİREBİR
	// aynı değerde: vektöre bakan bir dönüşüm tabanı seçemezdi — ipucu çözer.
	dn_denetim(t, db, "hosting.olustur", "a-site.test", "plan=200")
	dn_denetim(t, db, "hosting.ozel_plan", "a-site.test — Özel", "plan=210")
	// Hosting 10 (kök) kopya 100: ilk koşuda ipucu YOK → belirsiz (dokunulmaz).
	// Hosting 41 (B) kopya 410 = B Plani birebir (ipucu yok) → birebir eşleşme.
	pk_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, plan_id, reseller_id) VALUES
		(41,'b-ozel.test','c_b5',410,3), (42,'a-posta.test','c_a5',420,2), (43,'a-kokkopya.test','c_a6',430,2),
		(44,'a-paylasim.test','c_a7',440,2), (45,'a-paylasim2.test','c_a8',440,2), (46,'a-yabanci.test','c_a9',460,2),
		(47,'a-birebir-cok.test','c_a10',470,2)`)
	dn_kopya(t, db, 410, "b-ozel.test — Özel", 3, 41, 300, "")
	// Posta hatası: kopyada saatlik/kutu 0 (OzelPlan bunları kopyalamıyordu), max_email elle 7.
	dn_kopya(t, db, 420, "a-posta.test — Özel", 2, 42, 200, "saatlik_mail_limiti=0, mail_kutu_kota_mb=0, max_email=7")
	dn_denetim(t, db, "hosting.plan", "a-posta.test", "yeni plan=200")
	dn_denetim(t, db, "hosting.ozel_plan", "a-posta.test — Özel", "plan=420")
	// Kökün açtığı kopya bayi hostingünde → alanlar kök kilidi altında.
	dn_kopya(t, db, 430, "a-kokkopya.test — Özel", 0, 43, 1, "ram_mb=6144, io_read_mbps=99")
	dn_denetim(t, db, "hosting.plan", "a-kokkopya.test", "yeni plan=1")
	dn_denetim(t, db, "hosting.ozel_plan", "a-kokkopya.test — Özel", "plan=430")
	// Paylaşılan kopya (iki hosting) → dokunulmaz.
	dn_kopya(t, db, 440, "a-paylasim.test — Özel", 2, 44, 200, "ram_mb=999")
	dn_denetim(t, db, "hosting.plan", "a-paylasim.test", "yeni plan=200") // taban bilinse de paylaşılan → dokunulmaz
	dn_denetim(t, db, "hosting.ozel_plan", "a-paylasim.test — Özel", "plan=440")
	// İpucu bayinin GÖREMEDİĞİ plana (B'nin 300'ü) işaret ediyor → kabul edilmez → belirsiz.
	dn_kopya(t, db, 460, "a-yabanci.test — Özel", 2, 46, 300, "ram_mb=777")
	dn_denetim(t, db, "hosting.plan", "a-yabanci.test", "yeni plan=300")
	dn_denetim(t, db, "hosting.ozel_plan", "a-yabanci.test — Özel", "plan=460")
	// Birebir ama 200/201/202 üçüyle de aynı → belirsiz.
	dn_kopya(t, db, 470, "a-birebir-cok.test — Özel", 2, 47, 200, "")
	// Hiçbir hostingin kullanmadığı eski kopya → arşivlenir.
	dn_kopya(t, db, 480, "eski.test — Özel", 2, 22, 200, "")
	// 0037 mirası: hosting 42'de disk alan değeri varsa plan farkı yazılmaz.
	pk_exec(t, db, `UPDATE domains SET disk_kota_mb=4321 WHERE id=42`)
	pk_exec(t, db, `UPDATE service_plans SET disk_kota_mb=1 WHERE id=420`)

	once := dn_hepsi(t, db)
	wafOnce := map[int64]string{}
	for id := range once {
		wafOnce[id] = dn_waf(t, db, id)
	}
	katalogOnce := pk_sayi(t, db, `SELECT COUNT(*) FROM service_plans WHERE domain_id IS NULL`)

	rapor, err := plans.OzelPlanlariDonustur(ctx, db)
	if err != nil {
		t.Fatalf("dönüşüm: %v", err)
	}
	donen := map[int64]plans.DonusumKalemi{}
	for _, k := range rapor.Donusen {
		donen[k.PlanID] = k
	}
	atlanan := map[int64]string{}
	for _, k := range rapor.Atlanan {
		atlanan[k.PlanID] = k.Neden
	}
	// Beklenen: 210 (denetim), 410 (birebir), 420 (denetim + posta mirası), 430 (denetim, kilit).
	for _, id := range []int64{210, 410, 420, 430} {
		if _, ok := donen[id]; !ok {
			t.Errorf("kopya #%d dönüşmedi (neden: %q)", id, atlanan[id])
		}
	}
	for _, id := range []int64{100, 440, 460, 470} {
		if _, ok := atlanan[id]; !ok {
			t.Errorf("kopya #%d atlanmalıydı (belirsiz/paylaşılan) — dönüştü: %+v", id, donen[id])
		}
	}
	if len(rapor.Temizlenen) != 1 || rapor.Temizlenen[0] != 480 {
		t.Errorf("kullanılmayan kopya arşivi: %v", rapor.Temizlenen)
	}

	// Ayrıntılar.
	k := donen[210]
	sort.Strings(k.Alanlar)
	if k.TabanID != 200 || k.Yontem != "denetim" || strings.Join(k.Alanlar, ",") != "cpu_yuzde,disk_kota_mb,ram_mb,trafik_kota_mb" {
		t.Errorf("210: %+v", k)
	}
	if k := donen[410]; k.TabanID != 300 || k.Yontem != "birebir" || len(k.Alanlar) != 0 {
		t.Errorf("410: %+v", k)
	}
	if k := donen[420]; k.TabanID != 200 || strings.Join(k.Alanlar, ",") != "max_email" ||
		strings.Join(k.PostaMiras, ",") != "saatlik_mail_limiti,mail_kutu_kota_mb" {
		t.Errorf("420 (posta mirası, 0037 disk farkı yazılmaz): %+v", k)
	}
	if k := donen[430]; k.TabanID != 1 {
		t.Errorf("430: %+v", k)
	}
	sonra := dn_hepsi(t, db)
	if s := sonra[43]; !s.Degerler["ram_mb"].Kok || !s.Degerler["io_read_mbps"].Kok || s.Efektif("ram_mb", 0) != 6144 {
		t.Errorf("kökün kopyası bayi hostingünde kilitli üst yazım olmalı: %+v", s.KokAlanlar)
	}
	if s := sonra[20]; len(s.KokAlanlar) != 0 || s.Degerler["ram_mb"].Ust == nil {
		t.Errorf("bayinin kendi kopyası kilitsiz olmalı: %v", s.KokAlanlar)
	}
	if s := sonra[41]; s.UstVar || *s.PlanID != 300 {
		t.Errorf("birebir kopya: üst yazım açılmamalı: %+v", s.PlanID)
	}
	if s := sonra[42]; s.Efektif("saatlik_mail_limiti", -1) != 50 || s.Efektif("mail_kutu_kota_mb", -1) != 300 || s.Efektif("max_email", -1) != 7 {
		t.Errorf("posta: saatlik=%d kutu=%d max=%d", s.Efektif("saatlik_mail_limiti", -1), s.Efektif("mail_kutu_kota_mb", -1), s.Efektif("max_email", -1))
	}

	// 🔴 DEĞİŞMEZ: her hostingte 22 alan (posta mirası hariç) + efektif WAF aynı.
	for id, o := range once {
		haric := map[string]bool{}
		if id == 42 {
			haric["saatlik_mail_limiti"], haric["mail_kutu_kota_mb"] = true, true
		}
		if f := efektiflimit.Esit(o, sonra[id], haric); len(f) > 0 {
			t.Errorf("hosting %d efektif limiti değişti: %v", id, f)
		}
		if w := dn_waf(t, db, id); w != wafOnce[id] {
			t.Errorf("hosting %d efektif WAF değişti: %s → %s", id, wafOnce[id], w)
		}
	}
	// Katalog dokunulmadı; dönüşen kopyalar service_plans'tan çıktı, arşivde.
	if n := pk_sayi(t, db, `SELECT COUNT(*) FROM service_plans WHERE domain_id IS NULL`); n != katalogOnce {
		t.Errorf("katalog değişti: %d → %d", katalogOnce, n)
	}
	if n := pk_sayi(t, db, `SELECT COUNT(*) FROM service_plans WHERE id IN (210,410,420,430,480)`); n != 0 {
		t.Errorf("dönüşen/kullanılmayan kopya service_plans'ta kaldı: %d", n)
	}
	if n := pk_sayi(t, db, `SELECT COUNT(*) FROM service_plans_ozel_arsiv WHERE satir_json LIKE '%a-site.test — Özel%' AND taban_plan_id=200 AND domain_id=20`); n != 1 {
		t.Errorf("arşiv satırı yok/eksik")
	}
	if n := pk_sayi(t, db, `SELECT COUNT(*) FROM audit_log WHERE action='sistem.ozel_plan_donusum' AND reseller_id=0`); n != 4 {
		t.Errorf("dönüşüm denetim kaydı (yalnız kök kapsamı): %d", n)
	}

	// İkinci koşu: hiçbir şey dönüşmez; belirsizler yine raporlanır.
	r2, err := plans.OzelPlanlariDonustur(ctx, db)
	if err != nil || len(r2.Donusen) != 0 || len(r2.Temizlenen) != 0 || len(r2.Atlanan) != 4 {
		t.Errorf("ikinci koşu idempotent değil: %+v %v", r2, err)
	}
	// Kökün kopyası için ipucu eklenince (kökün kendi hostingi): dönüşür, WAF farkı hostinge.
	dn_denetim(t, db, "hosting.plan", "kok-site.test", "yeni plan=1")
	dn_denetim(t, db, "hosting.ozel_plan", "kok-site.test — Özel", "plan=100")
	o10, w10 := dn_hepsi(t, db)[10], dn_waf(t, db, 10)
	r3, _ := plans.OzelPlanlariDonustur(ctx, db)
	if len(r3.Donusen) != 1 || r3.Donusen[0].PlanID != 100 || len(r3.Donusen[0].WAF) != 2 {
		t.Fatalf("ipucuyla kök kopyası: %+v", r3)
	}
	if f := efektiflimit.Esit(o10, dn_hepsi(t, db)[10], nil); len(f) > 0 || dn_waf(t, db, 10) != w10 {
		t.Errorf("kök kopya dönüşümünde efektif değişti: %v %s→%s", f, w10, dn_waf(t, db, 10))
	}
	if s := dn_hepsi(t, db)[10]; len(s.KokAlanlar) != 0 {
		t.Errorf("kökün kendi hostingünde kilit gereksiz: %v", s.KokAlanlar)
	}
}

func pk_sayi(t *testing.T, db *sql.DB, q string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
