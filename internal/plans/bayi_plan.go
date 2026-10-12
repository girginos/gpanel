package plans

// bayi_plan.go — bayinin KENDİ hosting planları (reseller_id = bayi):
//
//   - Varsayılan işareti: bayinin kendi planı VARSA tam bir "varsayılan" işaretli
//     planı olur (yeni hosting formunda ön seçili; site taşımada plansız hesaplara
//     uygulanır) — BayiVarsayilanIsaretle / BayiVarsayilanUzlastir. 🔴 Bayiye plan
//     OTOMATİK AÇILMAZ: kendi planı olmayan bayi kökün paylaşılan katalogunu kullanır.
//   - Paket sınırı: bayinin yazdığı plan değerleri bayi paketinin havuz ve fazla
//     satış kurallarına uyar (bayiPaketKontrol).
//   - Kök alanları: efektiflimit.Alanlar'da Ozette=false olan iç ayarlar (inode,
//     process, IO ağırlığı/bant genişliği/IOPS, FPM, MySQL bağlantı + governor)
//     bayi tarafından YAZILAMAZ: bayi gövdesinde bu alanlardan biri varsa istek
//     403 ile reddedilir (sessizce yok sayılmaz); yeni bayi planında panel
//     varsayılanı, düzenlemede mevcut değer korunur. Yönetici bu alanları her
//     planda düzenler.
//   - Varsayılan yapma ucu (VarsayilanYap) ve paket sınırı ipuçları (BayiSinir).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/efektiflimit"
	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

// planAlanlari — efektiflimit.Alanlar sütun adı → Plan alanı (22 alan; eksiksizlik
// testi bayi_plan_test.go'da).
func planAlanlari(p *Plan) map[string]*int {
	return map[string]*int{
		"disk_kota_mb": &p.DiskKotaMB, "trafik_kota_mb": &p.TrafikKotaMB, "inode_kota": &p.InodeKota,
		"cpu_yuzde": &p.CPUYuzde, "ram_mb": &p.RAMMB, "max_process": &p.MaxProcess,
		"io_agirlik": &p.IOAgirlik, "pm_max_children": &p.PMMaxChildren,
		"io_read_mbps": &p.IOReadMBps, "io_write_mbps": &p.IOWriteMBps,
		"io_read_iops": &p.IOReadIOPS, "io_write_iops": &p.IOWriteIOPS,
		"mysql_max_baglanti": &p.MySQLMaxBaglanti, "db_max_queries_per_hour": &p.DBMaxQueriesPerHr,
		"db_max_updates_per_hour": &p.DBMaxUpdatesPerHr, "db_max_query_seconds": &p.DBMaxQuerySeconds,
		"max_domain": &p.MaxDomain, "max_db": &p.MaxDB, "max_ftp": &p.MaxFTP,
		"max_email": &p.MaxEmail, "saatlik_mail_limiti": &p.SaatlikMailLimiti, "mail_kutu_kota_mb": &p.MailKutuKotaMB,
	}
}

// KokAlanlari — bayinin plan formunda OLMAYAN, yalnız kökün belirlediği alanlar
// (efektiflimit.Alanlar'da Ozette=false: GL-06 sınıfı iç ayarlar). Tek kaynak.
func KokAlanlari() []string {
	return efektiflimit.IcAlanlar()
}

// kokAlanlariniAl — hedefin kök alanlarını kaynaktan kopyalar (bayi isteği bu
// alanları değiştiremez).
func kokAlanlariniAl(hedef *Plan, kaynak Plan) {
	h, k := planAlanlari(hedef), planAlanlari(&kaynak)
	for _, s := range KokAlanlari() {
		*h[s] = *k[s]
	}
}

// planGovdesiOku — plan oluştur/düzenle gövdesini çözer. Bayide (rid>0) gövdede
// kökün iç alanlarından (efektiflimit.IcAlan) biri VARSA — değeri ne olursa olsun —
// istek 403 ile reddedilir (sessizce yok sayılmaz; kullanıcı kararı 2026-10-03).
// Yanıt yazıldıysa ok=false.
func planGovdesiOku(w http.ResponseWriter, r *http.Request, rid int64) (Plan, bool) {
	var p Plan
	ham, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil || json.Unmarshal(ham, &p) != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return p, false
	}
	if rid > 0 {
		var anahtar map[string]json.RawMessage
		if json.Unmarshal(ham, &anahtar) != nil {
			httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
			return p, false
		}
		// encoding/json struct alanlarını harf duyarsız (Unicode katlamalı: "IO_READ_MBPS",
		// "max_proceſs") eşler — denetim de strings.EqualFold ile aynı katlamayı kullanır.
		gelen := map[string]bool{}
		for k := range anahtar {
			for _, a := range efektiflimit.Alanlar {
				if strings.EqualFold(k, a.Sutun) {
					gelen[a.Sutun] = true
				}
			}
		}
		if ic := efektiflimit.IcAlanlarIcinde(gelen); len(ic) > 0 {
			httpx.WriteError(w, http.StatusForbidden, efektiflimit.IcAlanRedMesaji(ic))
			return p, false
		}
	}
	return p, true
}

// panelVarsayilanPlan — sütun varsayılanlarıyla (göç DDL'i) aynı boş plan.
func panelVarsayilanPlan() Plan {
	var p Plan
	varsayilanDoldur(&p)
	return p
}

// limitDegisti — iki plan arasında efektif limit alanı farkı var mı?
func limitDegisti(a, b Plan) bool {
	pa, pb := planAlanlari(&a), planAlanlari(&b)
	for _, al := range efektiflimit.Alanlar {
		if *pa[al.Sutun] != *pb[al.Sutun] {
			return true
		}
	}
	return false
}

// planHata — kullanıcıya dönen doğrulama hatası (yabancı veri içermez).
type planHata struct {
	kod   int
	mesaj string
}

func (e *planHata) Error() string { return e.mesaj }

// hataYaz — planHata ise kod+mesaj; değilse bayiHata (bayiye ham SQL yok).
func hataYaz(w http.ResponseWriter, rid int64, err error) {
	var ph *planHata
	if errors.As(err, &ph) {
		httpx.WriteError(w, ph.kod, ph.mesaj)
		return
	}
	bayiHata(w, rid, err)
}

type sorgulayici interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// bayiHavuzOku — bayinin havuz anlık görüntüsü (users; 0045 deseni) + fazla satış.
func bayiHavuzOku(ctx context.Context, q sorgulayici, rid int64) (maxDisk, maxTrafik int64, fazla bool, err error) {
	var f int
	err = q.QueryRowContext(ctx,
		`SELECT max_disk_mb, max_trafik_mb, fazla_satis FROM users WHERE id=? AND role='reseller'`, rid).
		Scan(&maxDisk, &maxTrafik, &f)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false, &planHata{http.StatusForbidden, "bayi kaydı bulunamadı"}
	}
	if maxDisk < 0 {
		maxDisk = 0
	}
	if maxTrafik < 0 {
		maxTrafik = 0
	}
	return maxDisk, maxTrafik, f == 1, err
}

// planTaahhutDegisim — bayinin hostinglerinin EFEKTİF disk/trafik taahhüdü: şimdiki
// ve planID'nin değerleri (disk, trafik) olsaydı. Hostinge özel üst yazım korunur
// (efektiflimit.IfadePlanla — plan değişimiyle aynı kural).
func planTaahhutDegisim(ctx context.Context, q sorgulayici, rid, planID int64, disk, trafik int) (eskiD, yeniD, eskiT, yeniT int64, err error) {
	ed := "IFNULL(" + efektiflimit.Ifade("disk_kota_mb") + ",0)"
	yd := "IFNULL(" + efektiflimit.IfadePlanla("disk_kota_mb", "np") + ",0)"
	et := "IFNULL(" + efektiflimit.Ifade("trafik_kota_mb") + ",0)"
	yt := "IFNULL(" + efektiflimit.IfadePlanla("trafik_kota_mb", "np") + ",0)"
	err = q.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(`+ed+`),0), COALESCE(SUM(IF(d.plan_id=?, `+yd+`, `+ed+`)),0),
		        COALESCE(SUM(`+et+`),0), COALESCE(SUM(IF(d.plan_id=?, `+yt+`, `+et+`)),0)
		   FROM domains d`+efektiflimit.Join+
			`CROSS JOIN (SELECT CAST(? AS SIGNED) AS disk_kota_mb, CAST(? AS SIGNED) AS trafik_kota_mb) np
		  WHERE d.reseller_id=?`,
		planID, planID, disk, trafik, rid).Scan(&eskiD, &yeniD, &eskiT, &yeniT)
	return
}

// bayiPaketKontrol — bayinin (rid>0) kendi planına yazdığı disk/trafik bayi paketine
// uyar. planID>0: düzenleme (bağlı hostinglerin taahhüdü de denetlenir). Yönetici
// (rid<=0) kısıtsız. Kurallar domains havuz kapılarıyla AYNI:
//
//	disk/trafik 0 (sınırsız) olamaz (D1 BayiSifirYasak);
//	fazla satış KAPALI ve havuz > 0 iken: plan değeri < havuz (sınıra ulaşmak doludur —
//	  havuza eşit plan ile tek hosting bile açılamaz); düzenlemede bağlı hostinglerle
//	  toplam taahhüt havuza ulaşırsa ARTIŞ reddedilir (azaltma her zaman geçer);
//	fazla satış AÇIK: taahhüt kapısı yok (domains ile aynı; fiili kullanım kapısı kalır).
//
// Posta tavanı ayrıca bayiTavanKontrol'de.
func (h *Handlers) bayiPaketKontrol(ctx context.Context, rid, planID int64, p *Plan) error {
	if rid <= 0 {
		return nil
	}
	if p.DiskKotaMB <= 0 || p.TrafikKotaMB <= 0 {
		return &planHata{http.StatusBadRequest, "bayi planlarında disk ve trafik kotası sıfır (sınırsız) olamaz"}
	}
	maxDisk, maxTrafik, fazla, err := bayiHavuzOku(ctx, h.DB, rid)
	if err != nil {
		return err
	}
	if fazla {
		return nil
	}
	if maxDisk > 0 && int64(p.DiskKotaMB) >= maxDisk {
		return &planHata{http.StatusBadRequest, fmt.Sprintf(
			"plan disk kotası (%d MB) bayi paketinizin disk havuzundan (%d MB) küçük olmalı", p.DiskKotaMB, maxDisk)}
	}
	if maxTrafik > 0 && int64(p.TrafikKotaMB) >= maxTrafik {
		return &planHata{http.StatusBadRequest, fmt.Sprintf(
			"plan trafik kotası (%d MB) bayi paketinizin trafik havuzundan (%d MB) küçük olmalı", p.TrafikKotaMB, maxTrafik)}
	}
	if planID <= 0 || (maxDisk == 0 && maxTrafik == 0) {
		return nil
	}
	eskiD, yeniD, eskiT, yeniT, err := planTaahhutDegisim(ctx, h.DB, rid, planID, p.DiskKotaMB, p.TrafikKotaMB)
	if err != nil {
		return err
	}
	if maxDisk > 0 && yeniD > eskiD && yeniD >= maxDisk {
		return &planHata{http.StatusConflict, fmt.Sprintf(
			"bu plana bağlı hostinglerle disk taahhüdünüz %d MB → %d MB olur, paket havuzunuz %d MB — kotayı düşürün ya da hostingleri başka plana alın",
			eskiD, yeniD, maxDisk)}
	}
	if maxTrafik > 0 && yeniT > eskiT && yeniT >= maxTrafik {
		return &planHata{http.StatusConflict, fmt.Sprintf(
			"bu plana bağlı hostinglerle trafik taahhüdünüz %d MB → %d MB olur, paket havuzunuz %d MB — kotayı düşürün ya da hostingleri başka plana alın",
			eskiT, yeniT, maxTrafik)}
	}
	return nil
}

// ── Varsayılan bayi planı ───────────────────────────────────────────────────
//
// 🔴 Bayiye hosting planı OTOMATİK AÇILMAZ (kullanıcı bildirimi 2026-10-12: "100 bayi
// = 100 hosting planı"). Eski BayiVarsayilanSagla, kendi planı olmayan HER bayiye
// "<bayi> Varsayılan" planı yazıyordu: bayi oluşturmada (UI, WHMCS ve API anahtarı —
// hepsi POST /resellers), HER açılışta tüm bayiler için ve site taşımada.
//
// Bayinin kotası planda DEĞİL bayi kaydındadır: users.max_domain / max_disk_mb /
// max_trafik_mb (paket anlık görüntüsü, 0045) + reseller_plans posta tavanları; hosting
// açılışında domains.resellerKotaKontrol, plan yazımında bayiPaketKontrol uygular.
// Kendi planı olmayan bayi kökün paylaşılan (global) katalogunu kullanır: yeni hostingde
// kökün varsayılanı seçilir (domains.Create); paket kapısı da bunu varsayar
// (reseller.enKucukTaahhut). Bayinin İLK kendi planı varsayılanı olur (Create); burada
// yalnız varsayılan İŞARETİ tutarlı tutulur.

// ErrBayiYok — kimlik bir bayiye ait değil.
var ErrBayiYok = errors.New("bayi bulunamadı")

// İşaretleme sonucu.
const (
	VarsayilanVardi       = "vardi"
	VarsayilanIsaretlendi = "isaretlendi"
	VarsayilanYok         = "yok" // bayinin kendi planı yok — plan OLUŞTURULMAZ
)

// BayiVarsayilanIsaretle — bayinin (rid) kendi katalog planları arasında TEK bir
// varsayılan olmasını sağlar (idempotent). Plan OLUŞTURMAZ:
//
//	kendi katalog planı + varsayılan işareti var → dokunmaz                (vardi)
//	kendi planı var, hiçbiri varsayılan değil  → en eskisini işaretler      (isaretlendi)
//	kendi planı yok                             → hiçbir şey yapmaz, id 0   (yok)
//
// Planın satır kilidi altında (eşzamanlı iki işaretleme tek varsayılan bırakır).
// Hostinglerin planına/limitine DOKUNMAZ. uid/kul: denetim kaydının aktörü
// (açılışta 0/"sistem").
func BayiVarsayilanIsaretle(ctx context.Context, db *sql.DB, rid, uid int64, kul string) (int64, string, error) {
	if rid <= 0 {
		return 0, "", ErrBayiYok
	}
	var bayi int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id=? AND role='reseller'`, rid).Scan(&bayi); err != nil {
		return 0, "", err
	}
	if bayi == 0 {
		return 0, "", ErrBayiYok
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = tx.Rollback() }()
	var mevcut int64
	var vars int
	err = tx.QueryRowContext(ctx,
		`SELECT id, varsayilan FROM service_plans WHERE reseller_id=? AND domain_id IS NULL
		  ORDER BY varsayilan DESC, id ASC LIMIT 1 FOR UPDATE`, rid).Scan(&mevcut, &vars)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, VarsayilanYok, nil
	case err != nil:
		return 0, "", err
	case vars == 1:
		return mevcut, VarsayilanVardi, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE service_plans SET varsayilan=IF(id=?,1,0) WHERE reseller_id=?`, mevcut, rid); err != nil {
		return 0, "", err
	}
	if err := tx.Commit(); err != nil {
		return 0, "", err
	}
	var pad string
	_ = db.QueryRowContext(ctx, `SELECT ad FROM service_plans WHERE id=?`, mevcut).Scan(&pad)
	httpx.DenetimSistem(db, uid, kul, "plan.varsayilan", pad, "otomatik: bayinin varsayılan planı yoktu", rid, true)
	return mevcut, VarsayilanIsaretlendi, nil
}

// BayiVarsayilanUzlastir — açılışta, kendi planı olup hiçbiri varsayılan olmayan
// bayiler için BayiVarsayilanIsaretle (idempotent). 🔴 Plan OLUŞTURMAZ: kendi planı
// olmayan bayi listeye hiç girmez. Bir bayinin hatası diğerlerini durdurmaz; ilk hata döner.
func BayiVarsayilanUzlastir(ctx context.Context, db *sql.DB) (isaretlenen int, err error) {
	rows, err := db.QueryContext(ctx,
		`SELECT p.reseller_id FROM service_plans p
		   JOIN users u ON u.id=p.reseller_id AND u.role='reseller'
		  WHERE p.domain_id IS NULL
		  GROUP BY p.reseller_id HAVING MAX(p.varsayilan)=0 ORDER BY p.reseller_id`)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	var ilk error
	for _, rid := range ids {
		_, durum, e := BayiVarsayilanIsaretle(ctx, db, rid, 0, "sistem")
		switch {
		case e != nil:
			log.Printf("bayi varsayılan plan: bayi %d işaretlenemedi: %v", rid, e)
			if ilk == nil {
				ilk = e
			}
		case durum == VarsayilanIsaretlendi:
			isaretlenen++
		}
	}
	return isaretlenen, ilk
}

// ── Eski otomatik bayi planlarının temizliği (göç tasarımı) ─────────────────

// otomatikPlanAciklama — eski BayiVarsayilanSagla'nın yazdığı açıklama: otomatik
// planın imzasının yarısı (öteki yarısı ad: varsayilanPlanAdi).
const otomatikPlanAciklama = "Otomatik oluşturulan varsayılan plan"

// varsayilanPlanAdi — eski otomatik planın adı: "<bayi> Varsayılan"; service_plans.ad
// sunucu genelinde UNIQUE olduğundan çakışmada bayi kimliği eklenirdi (deneme 1..3).
// 64 rune sınırı korunur. Yalnız temizlik imzası olarak kalır — yeni plan ÜRETMEZ.
func varsayilanPlanAdi(kul string, rid int64, deneme int) string {
	k := strings.TrimSpace(kul)
	if k == "" {
		k = "bayi-" + strconv.FormatInt(rid, 10)
	}
	ek := " Varsayılan"
	switch {
	case deneme == 1:
		ek += " #" + strconv.FormatInt(rid, 10)
	case deneme > 1:
		ek += fmt.Sprintf(" #%d-%d", rid, deneme)
	}
	r := []rune(k)
	if sinir := 64 - len([]rune(ek)); len(r) > sinir {
		r = r[:sinir]
	}
	return string(r) + ek
}

// otomatikAdMi — ad, bayinin (kul, rid) eski otomatik plan adlarından biri mi?
// (eski kod en çok 4 deneme yapıyordu: 0..3)
func otomatikAdMi(ad, kul string, rid int64) bool {
	for d := 0; d < 4; d++ {
		if ad == varsayilanPlanAdi(kul, rid, d) {
			return true
		}
	}
	return false
}

// OtomatikBayiPlanlariniTemizle — 2026-10-03..2026-10-12 sürümlerinin bayilere
// otomatik açtığı ve HİÇ KULLANILMAYAN "<bayi> Varsayılan" planlarını siler.
//
// 🔴 AÇILIŞA BAĞLI DEĞİL: mevcut sunuculardaki planların silinmesi kullanıcı kararı
// bekliyor. Bağlanacaksa açılışta, HTTP sunucusu dinlemeye başlamadan ÖNCE (eşzamanlı
// hosting oluşturma/plan değişimi yokken) ve BayiVarsayilanUzlastir'dan ÖNCE çağrılmalı.
//
//	SİLİNİR    reseller_id>0, katalog planı (domain_id NULL), açıklama otomatik imzayla
//	           BİREBİR, ad bayinin kullanıcı adından üretilen otomatik adla BİREBİR
//	           (bayi adı ya da açıklamayı değiştirdiyse plan benimsenmiştir), hiçbir
//	           hosting bağlı değil (domains.plan_id) ve bayinin bitmemiş taşıma işi yok
//	           (iş ayarlarında plan kimliği tutulur; kesilen iş sürdürülebilir)
//	DOKUNULMAZ kullanılan ya da benimsenmiş planlar, kökün planları, bayinin elle
//	           açtığı planlar, sahibi bayi olmayan satırlar
//
// Silinen plan bayinin varsayılanıysa en eski kalan kendi planı varsayılan olur
// (planSil ile aynı kural); kalmadıysa bayi kökün paylaşılan varsayılanını kullanır.
// İdempotent (ikinci çağrı 0 döner). Her plan AYRI işlemde, sahibin planları
// kilitlenip koşullar yeniden doğrulanarak silinir. Denetim: plan.sil (aktör sistem).
// Bir planın hatası diğerlerini durdurmaz; ilk hata döner.
func OtomatikBayiPlanlariniTemizle(ctx context.Context, db *sql.DB) (int, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT p.id, p.reseller_id, p.ad, u.username FROM service_plans p
		   JOIN users u ON u.id=p.reseller_id AND u.role='reseller'
		  WHERE p.reseller_id>0 AND p.domain_id IS NULL AND p.aciklama=?
		  ORDER BY p.id`, otomatikPlanAciklama)
	if err != nil {
		return 0, err
	}
	type aday struct {
		id, rid int64
		ad, kul string
	}
	var adaylar []aday
	for rows.Next() {
		var a aday
		if rows.Scan(&a.id, &a.rid, &a.ad, &a.kul) == nil && otomatikAdMi(a.ad, a.kul, a.rid) {
			adaylar = append(adaylar, a)
		}
	}
	rows.Close()
	silinen := 0
	var ilk error
	for _, a := range adaylar {
		sil, err := otomatikPlanSil(ctx, db, a.id, a.rid, a.ad, a.kul)
		if err != nil {
			log.Printf("otomatik bayi planı temizliği: plan %d (bayi %d): %v", a.id, a.rid, err)
			if ilk == nil {
				ilk = err
			}
			continue
		}
		if sil {
			silinen++
		}
	}
	return silinen, ilk
}

// otomatikPlanSil — tek adayı, sahibinin planları kilitliyken koşulları YENİDEN
// doğrulayarak siler. false,nil = koşul artık tutmuyor (dokunulmadı).
func otomatikPlanSil(ctx context.Context, db *sql.DB, id, rid int64, ad, kul string) (bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx,
		`SELECT id, ad, aciklama, varsayilan FROM service_plans
		  WHERE reseller_id=? AND domain_id IS NULL ORDER BY id FOR UPDATE`, rid)
	if err != nil {
		return false, err
	}
	var digerleri []int64
	bulundu, vars := false, 0
	for rows.Next() {
		var x int64
		var xad, xac string
		var xv int
		if err := rows.Scan(&x, &xad, &xac, &xv); err != nil {
			rows.Close()
			return false, err
		}
		if x != id {
			digerleri = append(digerleri, x)
			continue
		}
		bulundu = xad == ad && xac == otomatikPlanAciklama && otomatikAdMi(xad, kul, rid)
		vars = xv
	}
	rows.Close()
	if !bulundu {
		return false, nil
	}
	var kullanim, tasima int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM domains WHERE plan_id=?`, id).Scan(&kullanim); err != nil {
		return false, err
	}
	// Bitmemiş taşıma işi (kuyrukta/çalışıyor/kesildi …) planı ayarlarında taşır.
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasima_isleri WHERE sahip_rid=? AND durum NOT IN ('tamam','hata','iptal')`, rid).Scan(&tasima); err != nil {
		return false, err
	}
	if kullanim > 0 || tasima > 0 {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM service_plans WHERE id=? AND reseller_id=? AND domain_id IS NULL`, id, rid); err != nil {
		return false, err
	}
	if vars == 1 && len(digerleri) > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE service_plans SET varsayilan=IF(id=?,1,0) WHERE reseller_id=?`, digerleri[0], rid); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	httpx.DenetimSistem(db, 0, "sistem", "plan.sil", ad, "otomatik bayi planı temizliği: hiçbir hostingde kullanılmıyordu", rid, true)
	return true, nil
}

// ── Uçlar ───────────────────────────────────────────────────────────────────

// VarsayilanYap — POST /plans/{id}/varsayilan: planı SAHİBİNİN kapsamında varsayılan
// yapar (diğerleri sıfırlanır). Bayi yalnız kendi katalog planını; yönetici her
// katalog planını (kapsam = planın sahibi). Hostinglerin limitine dokunmaz.
func (h *Handlers) VarsayilanYap(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	rid := middleware.ResellerIDFrom(r)
	ctx := r.Context()
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		bayiHata(w, rid, err)
		return
	}
	defer func() { _ = tx.Rollback() }()
	var sahip int64
	var dom sql.NullInt64
	var ad string
	err = tx.QueryRowContext(ctx, `SELECT reseller_id, domain_id, ad FROM service_plans WHERE id=? FOR UPDATE`, id).
		Scan(&sahip, &dom, &ad)
	if errors.Is(err, sql.ErrNoRows) && rid <= 0 {
		httpx.WriteError(w, http.StatusNotFound, "plan bulunamadı")
		return
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		bayiHata(w, rid, err)
		return
	}
	// Bayi: YALNIZ sahibi olduğu plan (satır kilidi altında — devirle eşzamanlı
	// sahiplik değişimi de yakalanır). Olmayan plan ile yabancı plan AYIRT EDİLMEZ.
	if rid > 0 && (err != nil || sahip != rid) {
		httpx.WriteError(w, http.StatusForbidden, "bu planı varsayılan yapamazsınız")
		return
	}
	if dom.Valid {
		httpx.WriteError(w, http.StatusBadRequest, "hostinge özel plan varsayılan yapılamaz")
		return
	}
	if _, err := tx.ExecContext(ctx, `UPDATE service_plans SET varsayilan=IF(id=?,1,0) WHERE reseller_id=?`, id, sahip); err != nil {
		bayiHata(w, rid, err)
		return
	}
	if err := tx.Commit(); err != nil {
		bayiHata(w, rid, err)
		return
	}
	uid, kul := middleware.Aktor(r)
	httpx.Denetim(h.DB, r, uid, kul, "plan.varsayilan", ad, "", sahip, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}

// BayiSinir — GET /plans/bayi-sinir: plan formunun paket ipuçları. Bayi: havuz,
// fazla satış, şu anki taahhüt, posta tavanları; herkes: bayiye kapalı kök alanları.
func (h *Handlers) BayiSinir(w http.ResponseWriter, r *http.Request) {
	rid := middleware.ResellerIDFrom(r)
	out := map[string]any{"bayi": rid > 0, "kok_alanlar": KokAlanlari()}
	if rid <= 0 {
		httpx.WriteJSON(w, http.StatusOK, out)
		return
	}
	ctx := r.Context()
	var maxDomain, fazla int
	var maxDisk, maxTrafik, paketID int64
	err := h.DB.QueryRowContext(ctx,
		`SELECT max_domain, max_disk_mb, max_trafik_mb, fazla_satis, reseller_plan_id FROM users WHERE id=? AND role='reseller'`, rid).
		Scan(&maxDomain, &maxDisk, &maxTrafik, &fazla, &paketID)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusForbidden, "bayi kaydı bulunamadı")
		return
	}
	if err != nil {
		bayiHata(w, rid, err)
		return
	}
	var tDisk, tTrafik int64
	var hosting int
	if err := h.DB.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(`+efektiflimit.IfadeVarsayilan("disk_kota_mb", 0)+`),0),
		        COALESCE(SUM(`+efektiflimit.IfadeVarsayilan("trafik_kota_mb", 0)+`),0)
		   FROM domains d`+efektiflimit.Join+`WHERE d.reseller_id=?`, rid).Scan(&hosting, &tDisk, &tTrafik); err != nil {
		bayiHata(w, rid, err)
		return
	}
	var mE, mS, mK int
	if paketID > 0 {
		_ = h.DB.QueryRowContext(ctx,
			`SELECT COALESCE(mail_max_email,0), COALESCE(mail_saatlik_limit,0), COALESCE(mail_kutu_kota_mb,0)
			   FROM reseller_plans WHERE id=?`, paketID).Scan(&mE, &mS, &mK)
	}
	out["max_domain"], out["max_disk_mb"], out["max_trafik_mb"] = maxDomain, maxDisk, maxTrafik
	out["fazla_satis"] = fazla == 1
	out["hosting_sayisi"], out["taahhut_disk_mb"], out["taahhut_trafik_mb"] = hosting, tDisk, tTrafik
	out["mail_max_email"], out["mail_saatlik_limit"], out["mail_kutu_kota_mb"] = mE, mS, mK
	httpx.WriteJSON(w, http.StatusOK, out)
}

// HostingUygula — plan değişince bir hostinge limitleri yeniden uygulayan yol:
// domains "Yeniden Uygula" (PUT /domains/{id}/plan) ile AYNI işlev (kaynaklimit +
// WAF + posta). domains plans'ı içe aktardığı için main bağlar; nil ise yalnız WAF
// yeniden çizilir (eski davranış).
var HostingUygula func(db *sql.DB, did int64, alanAdi string)
