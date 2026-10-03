package plans

// donustur.go — eski "<alan> — Özel" kopya planlarını hostinge özel ÜST YAZIMA
// dönüştürür (D1). Açılışta, göçlerden ve SeedSync'ten sonra, Heal işlerinden ÖNCE
// eşzamanlı çalışır. Her hosting ayrı bir işlemdedir; adım idempotenttir (dönüşen
// kopya service_plans'tan çıkar, ikinci koşu onu görmez).
//
// Kullanıcı kararı (2026-09-29): YALNIZ FARKLAR üst yazım olur, gerisi (posta dahil)
// tabandan miras alınır; tabanı BELİRSİZ kopya dönüştürülmez ve raporlanır.
//
// Taban (kopyanın türediği katalog planı) şöyle belirlenir — tahmin YOK:
//
//	1. Denetim kaydı: kopyayı üreten "hosting.ozel_plan" olayı (detay "plan=<kopya>")
//	   ve aynı alan adı için ondan HEMEN önceki "hosting.plan"/"hosting.olustur"
//	   olayındaki plan. O plan hâlâ katalogdaysa ve hosting sahibinin görebildiği
//	   bir plansa (kök global ya da sahibin kendi planı) taban odur.
//	2. Yoksa: 22 limit alanı + WAF'ı kopyayla BİREBİR aynı olan TEK görünür katalog
//	   planı (fark yok → üst yazım yok).
//	3. Hiçbiri → dokunulmaz, rapora "taban belirsiz" yazılır (hosting eski kopyada
//	   kalır, limitleri değişmez; yönetici bir katalog planı seçip özel limitleri
//	   kendisi girebilir).
//
// Posta: OzelPlan'ın sabit sütun listesi saatlik_mail_limiti ve mail_kutu_kota_mb'yi
// kopyalamıyordu (kopyada 0 = sınırsız). Kopyada 0 iken tabanda dolu olan bu iki
// alan tabandan MİRAS alınır (hatanın ürünü, bilinçli fark değil) ve raporlanır;
// kopyada sıfırdan farklı bir değer varsa (elle düzenlenmiş) üst yazım olur.
//
// Değişmez: dönüşümden önce ve sonra 22 alanın efektif değeri (posta mirası hariç)
// ve efektif WAF birebir aynı; değilse o hostingin işlemi geri alınır.
//
// Eski kopya service_plans_ozel_arsiv'e satır JSON'u olarak taşınır (veri kaybı yok).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"girginospanel/internal/efektiflimit"
	"girginospanel/internal/httpx"
)

// DonusumKalemi — tek kopyanın sonucu.
type DonusumKalemi struct {
	PlanID     int64
	DomainID   int64
	TabanID    int64
	Yontem     string   // denetim | birebir
	Alanlar    []string // üst yazıma yazılan alanlar
	PostaMiras []string // kopyada 0 → tabandan miras alınan posta alanları
	WAF        []string // hostinge taşınan WAF alanları
	Neden      string   // atlandıysa
}

// DonusumRaporu — açılış dönüşümünün özeti.
type DonusumRaporu struct {
	Donusen    []DonusumKalemi
	Atlanan    []DonusumKalemi
	Temizlenen []int64 // hiçbir hostingin kullanmadığı eski kopyalar (arşivlendi)
	// Geri yükleme kalıntısı (bkz. bayatKalintiTemizle): silinen bayat satır sayıları.
	BayatUst, BayatArsiv int64
}

// planSatiri — bir planın karşılaştırılan alanları.
type planSatiri struct {
	id, sahip int64
	domainID  sql.NullInt64
	limit     map[string]int
	wafEn     int
	wafMode   string
	wafPL     int
}

var planSatirSorgu = func() string {
	var b strings.Builder
	b.WriteString("SELECT id, reseller_id, domain_id, COALESCE(waf_enabled,0), COALESCE(waf_mode,'on'), COALESCE(waf_paranoia,1)")
	for _, a := range efektiflimit.Alanlar {
		fmt.Fprintf(&b, ", COALESCE(`%s`,0)", a.Sutun)
	}
	b.WriteString(" FROM service_plans")
	return b.String()
}()

func planSatirTara(rs interface{ Scan(...any) error }) (planSatiri, error) {
	p := planSatiri{limit: map[string]int{}}
	vals := make([]int, len(efektiflimit.Alanlar))
	hedef := []any{&p.id, &p.sahip, &p.domainID, &p.wafEn, &p.wafMode, &p.wafPL}
	for i := range vals {
		hedef = append(hedef, &vals[i])
	}
	if err := rs.Scan(hedef...); err != nil {
		return p, err
	}
	for i, a := range efektiflimit.Alanlar {
		p.limit[a.Sutun] = vals[i]
	}
	return p, nil
}

func planSatirOku(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id int64, kilitle bool) (planSatiri, error) {
	s := planSatirSorgu + " WHERE id=?"
	if kilitle {
		s += " FOR UPDATE"
	}
	return planSatirTara(q.QueryRowContext(ctx, s, id))
}

// gorunurKatalog — sahibin görebildiği katalog planları (kök global + kendi planları).
func gorunurKatalog(ctx context.Context, tx *sql.Tx, sahip int64) ([]planSatiri, error) {
	rows, err := tx.QueryContext(ctx, planSatirSorgu+" WHERE domain_id IS NULL AND reseller_id IN (0, ?) ORDER BY id", sahip)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []planSatiri
	for rows.Next() {
		p, err := planSatirTara(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func limitFark(a, b planSatiri) []string {
	var f []string
	for _, al := range efektiflimit.Alanlar {
		if a.limit[al.Sutun] != b.limit[al.Sutun] {
			f = append(f, al.Sutun)
		}
	}
	return f
}

func wafAyni(a, b planSatiri) bool {
	return a.wafEn == b.wafEn && strings.EqualFold(a.wafMode, b.wafMode) && a.wafPL == b.wafPL
}

// denetimTabani — kopyayı üreten olaydan hemen önceki plan ataması (0 = yok).
func denetimTabani(ctx context.Context, tx *sql.Tx, kopyaID int64, alanAdi string) int64 {
	var olay int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM audit_log WHERE action='hosting.ozel_plan' AND JSON_VALUE(detail,'$.not')=?
		 ORDER BY id DESC LIMIT 1`, "plan="+strconv.FormatInt(kopyaID, 10)).Scan(&olay); err != nil {
		return 0
	}
	var not sql.NullString
	if err := tx.QueryRowContext(ctx,
		`SELECT JSON_VALUE(detail,'$.not') FROM audit_log
		  WHERE id<? AND target=? AND action IN ('hosting.plan','hosting.olustur')
		  ORDER BY id DESC LIMIT 1`, olay, alanAdi).Scan(&not); err != nil || !not.Valid {
		return 0
	}
	i := strings.LastIndex(not.String, "plan=")
	if i < 0 {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(not.String[i+len("plan="):]), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// satirJSON — planın tüm sütunları (arşiv için).
func satirJSON(ctx context.Context, tx *sql.Tx, id int64) (string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT * FROM service_plans WHERE id=?`, id)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return "", err
	}
	if !rows.Next() {
		return "", sql.ErrNoRows
	}
	ham := make([]sql.RawBytes, len(cols))
	ptr := make([]any, len(cols))
	for i := range ham {
		ptr[i] = &ham[i]
	}
	if err := rows.Scan(ptr...); err != nil {
		return "", err
	}
	m := map[string]*string{}
	for i, c := range cols {
		if ham[i] == nil {
			m[c] = nil
			continue
		}
		v := string(ham[i])
		m[c] = &v
	}
	b, err := json.Marshal(m)
	return string(b), err
}

func arsivle(ctx context.Context, tx *sql.Tx, planID int64, did, taban sql.NullInt64, rapor string) error {
	js, err := satirJSON(ctx, tx, planID)
	if err != nil {
		return fmt.Errorf("arşiv satırı okunamadı: %w", err)
	}
	if len(rapor) > 1000 {
		rapor = rapor[:1000]
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO service_plans_ozel_arsiv (kaynak_plan_id, domain_id, taban_plan_id, satir_json, rapor) VALUES (?,?,?,?,?)`,
		planID, did, taban, js, rapor); err != nil {
		return fmt.Errorf("arşive yazılamadı: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM service_plans WHERE id=?`, planID); err != nil {
		return fmt.Errorf("kopya silinemedi: %w", err)
	}
	return nil
}

// wafEfektif — provisioner.WAFEfektif ile aynı çözüm (işlem içinde okunur).
func wafEfektif(ctx context.Context, tx *sql.Tx, did int64) (string, error) {
	var en int
	var mode string
	var pl int
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(d.waf_enabled, p.waf_enabled, 0),
		  LOWER(TRIM(COALESCE(NULLIF(TRIM(d.waf_mode),''), p.waf_mode, 'on'))),
		  COALESCE(NULLIF(d.waf_paranoia,0), p.waf_paranoia, 1)
		FROM domains d LEFT JOIN service_plans p ON p.id=d.plan_id WHERE d.id=?`, did).Scan(&en, &mode, &pl)
	return fmt.Sprintf("%d/%s/%d", en, mode, pl), err
}

// ustYazimaTasi — hostingi kaynak plandan taban plana (0 = plansız) taşır; efektif
// değeri değişecek her alanı üst yazıma, her WAF farkını hostingin WAF alanına
// yazar. postaMirasKurali: kopya hatası kuralı (yalnız açılış dönüşümünde).
// kokKilit: yeni yazılan alanlar kökün kilidi altına girer.
// Değişmez denetimi BURADA yapılır; ihlalde hata döner (çağıran işlemi geri alır).
func ustYazimaTasi(ctx context.Context, tx *sql.Tx, did int64, kaynak planSatiri, taban *planSatiri,
	postaMirasKurali, kokKilit bool, kaynakPlanID *int64) (alanlar, postaMiras, waf []string, err error) {
	once, err := efektiflimit.Oku(ctx, tx, did)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("efektif limit okunamadı: %w", err)
	}
	wafOnce, err := wafEfektif(ctx, tx, did)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("WAF okunamadı: %w", err)
	}
	ust := once.UstHarita()
	kok := map[string]bool{}
	for k := range once.KokAlanlar {
		kok[k] = true
	}
	haric := map[string]bool{}
	for _, a := range efektiflimit.Alanlar {
		d := once.Degerler[a.Sutun]
		if d.Ust != nil || d.Alan != nil {
			continue // efektif değer zaten plandan bağımsız (üst yazım / 0037)
		}
		kv := kaynak.limit[a.Sutun]
		tv, tabanVar := 0, taban != nil
		if tabanVar {
			tv = taban.limit[a.Sutun]
		}
		if tabanVar && kv == tv {
			continue
		}
		if postaMirasKurali && tabanVar && (a.Sutun == "saatlik_mail_limiti" || a.Sutun == "mail_kutu_kota_mb") && kv == 0 && tv != 0 {
			postaMiras = append(postaMiras, a.Sutun)
			haric[a.Sutun] = true
			continue
		}
		v := kv
		ust[a.Sutun] = &v
		if kokKilit {
			kok[a.Sutun] = true
		}
		alanlar = append(alanlar, a.Sutun)
	}
	// WAF: hosting alanı boşsa (plandan miras) ve kaynak ≠ taban → kaynağın değeri hostinge.
	var dEn, dPL sql.NullInt64
	var dMode sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT waf_enabled, waf_mode, waf_paranoia FROM domains WHERE id=?`, did).
		Scan(&dEn, &dMode, &dPL); err != nil {
		return nil, nil, nil, fmt.Errorf("hosting WAF okunamadı: %w", err)
	}
	tEn, tMode, tPL := 0, "on", 1
	if taban != nil {
		tEn, tMode, tPL = taban.wafEn, taban.wafMode, taban.wafPL
	}
	if !dEn.Valid && kaynak.wafEn != tEn {
		if _, err := tx.ExecContext(ctx, `UPDATE domains SET waf_enabled=? WHERE id=?`, kaynak.wafEn, did); err != nil {
			return nil, nil, nil, err
		}
		waf = append(waf, "waf_enabled")
	}
	if (!dMode.Valid || strings.TrimSpace(dMode.String) == "") && !strings.EqualFold(strings.TrimSpace(kaynak.wafMode), strings.TrimSpace(tMode)) {
		if _, err := tx.ExecContext(ctx, `UPDATE domains SET waf_mode=? WHERE id=?`, kaynak.wafMode, did); err != nil {
			return nil, nil, nil, err
		}
		waf = append(waf, "waf_mode")
	}
	if (!dPL.Valid || dPL.Int64 <= 0) && kaynak.wafPL != tPL {
		if _, err := tx.ExecContext(ctx, `UPDATE domains SET waf_paranoia=? WHERE id=?`, kaynak.wafPL, did); err != nil {
			return nil, nil, nil, err
		}
		waf = append(waf, "waf_paranoia")
	}
	if len(alanlar) > 0 {
		if err := efektiflimit.Kaydet(ctx, tx, did, ust, kok, 0, kaynakPlanID); err != nil {
			return nil, nil, nil, fmt.Errorf("üst yazım kaydedilemedi: %w", err)
		}
	}
	var tabanID any
	if taban != nil {
		tabanID = taban.id
	}
	if _, err := tx.ExecContext(ctx, `UPDATE domains SET plan_id=? WHERE id=?`, tabanID, did); err != nil {
		return nil, nil, nil, fmt.Errorf("hosting tabana bağlanamadı: %w", err)
	}
	sonra, err := efektiflimit.Oku(ctx, tx, did)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("efektif limit okunamadı: %w", err)
	}
	if fark := efektiflimit.Esit(once, sonra, haric); len(fark) > 0 {
		return nil, nil, nil, fmt.Errorf("değişmez bozuldu — efektif değer değişirdi: %s", strings.Join(fark, ","))
	}
	if wafSonra, err := wafEfektif(ctx, tx, did); err != nil || wafSonra != wafOnce {
		return nil, nil, nil, fmt.Errorf("değişmez bozuldu — efektif WAF değişirdi (%s → %s)", wafOnce, wafSonra)
	}
	return alanlar, postaMiras, waf, nil
}

// OzelPlanlariDonustur — açılış dönüşümü (bkz. dosya başı). Hata açılışı DÜŞÜRMEZ:
// dönüşemeyen kopya yerinde kalır ve raporlanır.
func OzelPlanlariDonustur(ctx context.Context, db *sql.DB) (DonusumRaporu, error) {
	var rapor DonusumRaporu
	var err error
	// FK'si kurulamamış eski şemada silinmiş hostingin üst yazımı kalmasın.
	if _, err = db.ExecContext(ctx,
		`DELETE u FROM hosting_limit_ust u LEFT JOIN domains d ON d.id=u.domain_id WHERE d.id IS NULL`); err != nil {
		return rapor, fmt.Errorf("hosting_limit_ust okunamadı (göç 0120 uygulandı mı?): %w", err)
	}
	if rapor.BayatUst, rapor.BayatArsiv, err = bayatKalintiTemizle(ctx, db); err != nil {
		return rapor, fmt.Errorf("geri yükleme kalıntısı temizlenemedi: %w", err)
	}
	rows, err := db.QueryContext(ctx, `SELECT id, domain_id FROM service_plans WHERE domain_id IS NOT NULL ORDER BY id`)
	if err != nil {
		return rapor, err
	}
	type aday struct{ plan, dom int64 }
	var adaylar []aday
	for rows.Next() {
		var a aday
		if rows.Scan(&a.plan, &a.dom) == nil {
			adaylar = append(adaylar, a)
		}
	}
	rows.Close()
	for _, a := range adaylar {
		k, temiz, err := tekKopyaDonustur(ctx, db, a.plan, a.dom)
		switch {
		case temiz:
			rapor.Temizlenen = append(rapor.Temizlenen, a.plan)
			log.Printf("ozel plan donusum: kullanilmayan eski kopya #%d arsivlendi", a.plan)
		case err != nil:
			k.PlanID, k.DomainID = a.plan, a.dom
			k.Neden = err.Error()
			rapor.Atlanan = append(rapor.Atlanan, k)
			log.Printf("🔴 ozel plan donusturulmedi: kopya #%d hosting %d — %s (limitler DEGISMEDI)", a.plan, a.dom, k.Neden)
		default:
			rapor.Donusen = append(rapor.Donusen, k)
			log.Printf("ozel plan donusum: kopya #%d → taban #%d (%s), %d alan ust yazim %v, posta miras %v, waf %v",
				k.PlanID, k.TabanID, k.Yontem, len(k.Alanlar), k.Alanlar, k.PostaMiras, k.WAF)
		}
	}
	return rapor, nil
}

// bayatKalintiTemizle — GERİ DÖNÜŞ KALINTISI. DB dönüşümden ÖNCE alınmış bir yedeğe
// geri yüklenirse (girginospanel-update/-restore sağlıksız açılışta güncelleme öncesi
// dump'ı geri yükler; operatör elle `zcat dump | mysql` yapabilir) dump yalnız KENDİ
// tablolarını (service_plans, domains, audit_log…) geri koyar: 0120'nin tabloları
// (hosting_limit_ust, service_plans_ozel_arsiv) geri alınmış zaman çizgisinin
// satırlarıyla KALIR. Yeni ikiliye tekrar geçişte bunlar canlı sanılırsa:
//   - üst yazım satırı, kopya hâlâ planken alanı "zaten plandan bağımsız" saydırır →
//     eski ikilinin kopyada yaptığı değişiklik sessizce ezilir;
//   - arşivdeki UNIQUE(kaynak_plan_id) kopyanın yeniden arşivlenmesini reddeder →
//     o hosting HER açılışta "dönüştürülmedi" kalır (çift dönüşüm / takılı dönüşüm).
//
// Yalnız KANITLANABİLİR bayat satır silinir: kaynak kopyası service_plans'ta AYNI
// hostinge ait (domain_id) olarak yeniden VAR olan arşiv ve üst yazım satırı. Dönüşüm
// kopyayı, üst yazımı yazdığı/arşivlediği AYNI işlemde siler; kopyanın geri gelmesi o
// işlemin geri alındığının kanıtıdır (kimlik yeniden kullanılsa bile yeni plan domain_id
// taşımaz — kopya artık üretilmiyor). Veri kaybı yok: silinen satırın bilgisi geri gelen
// kopyanın kendisidir; dönüşüm hemen ardından onu yeniden, aynı sonuçla dönüştürür.
// Kanıtlanamayan kalıntıyı (ör. elle üst yazım, devir) güncelleme/onarım betiği geri
// yüklemede zaten kaldırır (assets/ops yeni_tablolari_kenara_al: yedekle + DROP).
func bayatKalintiTemizle(ctx context.Context, db *sql.DB) (ust, arsiv int64, err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var kayit []string
	rows, err := tx.QueryContext(ctx,
		`SELECT u.domain_id, u.kaynak_ozel_plan_id FROM hosting_limit_ust u
		   JOIN service_plans p ON p.id=u.kaynak_ozel_plan_id AND p.domain_id=u.domain_id FOR UPDATE`)
	if err != nil {
		return 0, 0, err
	}
	for rows.Next() {
		var d, k int64
		if rows.Scan(&d, &k) == nil {
			kayit = append(kayit, fmt.Sprintf("hosting %d (kopya #%d)", d, k))
		}
	}
	rows.Close()
	r, err := tx.ExecContext(ctx,
		`DELETE u FROM hosting_limit_ust u JOIN service_plans p ON p.id=u.kaynak_ozel_plan_id AND p.domain_id=u.domain_id`)
	if err != nil {
		return 0, 0, err
	}
	ust, _ = r.RowsAffected()
	r, err = tx.ExecContext(ctx,
		`DELETE a FROM service_plans_ozel_arsiv a JOIN service_plans p ON p.id=a.kaynak_plan_id AND p.domain_id=a.domain_id`)
	if err != nil {
		return 0, 0, err
	}
	arsiv, _ = r.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	if ust+arsiv > 0 {
		log.Printf("🔴 ozel plan: DB donusum oncesi yedege geri yuklenmis — bayat %d ust yazim %v + %d arsiv satiri silindi (kopyalar geri geldi, yeniden donusturulecek)",
			ust, kayit, arsiv)
	}
	return ust, arsiv, nil
}

var errBelirsiz = errors.New("taban plan belirlenemedi (denetim kaydı yok ve birebir eşleşen tek katalog planı yok)")

func tekKopyaDonustur(ctx context.Context, db *sql.DB, planID, domID int64) (DonusumKalemi, bool, error) {
	k := DonusumKalemi{PlanID: planID, DomainID: domID}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return k, false, err
	}
	defer func() { _ = tx.Rollback() }()
	kopya, err := planSatirOku(ctx, tx, planID, true)
	if err != nil {
		return k, false, fmt.Errorf("kopya okunamadı: %w", err)
	}
	var kullanan []int64
	urows, err := tx.QueryContext(ctx, `SELECT id FROM domains WHERE plan_id=? FOR UPDATE`, planID)
	if err != nil {
		return k, false, err
	}
	for urows.Next() {
		var id int64
		if urows.Scan(&id) == nil {
			kullanan = append(kullanan, id)
		}
	}
	urows.Close()
	if len(kullanan) == 0 {
		if err := arsivle(ctx, tx, planID, sql.NullInt64{Int64: domID, Valid: true}, sql.NullInt64{}, "kullanilmayan eski ozel kopya"); err != nil {
			return k, false, err
		}
		return k, true, tx.Commit()
	}
	if len(kullanan) > 1 || kullanan[0] != domID {
		return k, false, fmt.Errorf("kopya başka/birden çok hostingte kullanılıyor (%d hosting)", len(kullanan))
	}
	var rid int64
	var alanAdi string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(reseller_id,0), alan_adi FROM domains WHERE id=?`, domID).
		Scan(&rid, &alanAdi); err != nil {
		return k, false, fmt.Errorf("hosting okunamadı: %w", err)
	}
	katalog, err := gorunurKatalog(ctx, tx, rid)
	if err != nil {
		return k, false, err
	}
	var taban *planSatiri
	if n := denetimTabani(ctx, tx, planID, alanAdi); n > 0 {
		for i := range katalog {
			if katalog[i].id == n {
				taban, k.Yontem = &katalog[i], "denetim"
			}
		}
	}
	if taban == nil {
		var esler []int
		for i := range katalog {
			if len(limitFark(kopya, katalog[i])) == 0 && wafAyni(kopya, katalog[i]) {
				esler = append(esler, i)
			}
		}
		if len(esler) == 1 {
			taban, k.Yontem = &katalog[esler[0]], "birebir"
		}
	}
	if taban == nil {
		return k, false, errBelirsiz
	}
	k.TabanID = taban.id
	kokKilit := kopya.sahip == 0 && rid > 0 // kökün açtığı kopya bayi hostingindeyse kilitli
	pid := planID
	k.Alanlar, k.PostaMiras, k.WAF, err = ustYazimaTasi(ctx, tx, domID, kopya, taban, true, kokKilit, &pid)
	if err != nil {
		return k, false, err
	}
	not := fmt.Sprintf("plan #%d → taban #%d (%s), %d alan üst yazım", planID, taban.id, k.Yontem, len(k.Alanlar))
	if len(k.Alanlar) > 0 {
		not += " [" + strings.Join(k.Alanlar, ",") + "]"
	}
	if len(k.PostaMiras) > 0 {
		not += ", posta tabandan miras [" + strings.Join(k.PostaMiras, ",") + "]"
	}
	if len(k.WAF) > 0 {
		not += ", WAF hostinge [" + strings.Join(k.WAF, ",") + "]"
	}
	if err := arsivle(ctx, tx, planID, sql.NullInt64{Int64: domID, Valid: true}, sql.NullInt64{Int64: taban.id, Valid: true}, not); err != nil {
		return k, false, err
	}
	if err := tx.Commit(); err != nil {
		return k, false, err
	}
	// Kayıt yalnız kökte görünür (kapsam 0).
	httpx.DenetimSistem(db, 0, "sistem", "sistem.ozel_plan_donusum", alanAdi, not, 0, true)
	return k, false, nil
}
