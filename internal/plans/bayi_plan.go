package plans

// bayi_plan.go — bayinin KENDİ hosting planları (reseller_id = bayi):
//
//   - Varsayılan plan güvencesi: her bayinin en az bir katalog planı ve tam bir
//     "varsayılan" işaretli planı olur (yeni hosting formunda ön seçili; site
//     taşımada plansız hesaplara uygulanır). Bayi oluşturulunca ve açılışta
//     idempotent uzlaştırmayla (BayiVarsayilanSagla / BayiVarsayilanUzlastir).
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

// ErrBayiYok — kimlik bir bayiye ait değil.
var ErrBayiYok = errors.New("bayi bulunamadı")

// Sağlama sonucu.
const (
	VarsayilanVardi       = "vardi"
	VarsayilanIsaretlendi = "isaretlendi"
	VarsayilanOlusturuldu = "olusturuldu"
)

// bayiHavuz — varsayılan plan değer kuralının girdisi.
type bayiHavuz struct {
	MaxDomain               int
	MaxDiskMB, MaxTrafikMB  int64
	FazlaSatis              bool
	MailMaxEmail            int // 0 = tavan yok
	MailSaatlik, MailKutuMB int
}

const intUst = 1<<31 - 1

// havuzPayi — varsayılan plandaki disk/trafik değeri (MB):
//
//	havuz sınırsız (0): şablon değeri; şablon da sınırsızsa tohum ("Başlangıç") değeri
//	  (bayi planı 0 olamaz);
//	havuz > 0: hosting başına PAY = ⌊(havuz−1) / hak⌋ (hak = bayinin hosting hakkı,
//	  sınırsızsa 1) — hakkın TAMAMI bu planla açılsa toplam taahhüt havuzun ALTINDA
//	  kalır (domains kapısı "sınıra ulaşmak doludur"); şablon sınırsızsa pay; fazla
//	  satış kapalıyken şablon paydan büyükse pay; aksi halde şablon.
func havuzPayi(sablon int, havuz int64, hak int, fazla bool, yedek int) int {
	if havuz <= 0 {
		if sablon > 0 {
			return sablon
		}
		return yedek
	}
	if hak < 1 {
		hak = 1
	}
	pay := (havuz - 1) / int64(hak)
	if pay < 1 {
		pay = 1
	}
	if pay > intUst {
		pay = intUst
	}
	if sablon <= 0 || (!fazla && int64(sablon) > pay) {
		return int(pay)
	}
	return sablon
}

// tavanaSigdir — posta değeri bayi paketinin tavanını aşamaz (0 = sınırsız da aşım).
func tavanaSigdir(v, tavan int) int {
	if tavan > 0 && (v <= 0 || v > tavan) {
		return tavan
	}
	return v
}

// bayiVarsayilanDegerler — varsayılan bayi planının DEĞER KURALI (saf, testli):
//
//	ŞABLON = kökün varsayılan global katalog planı (yoksa en küçük diskli global plan,
//	o da yoksa tohum "Başlangıç") — kök şablonu kendi varsayılan planıyla ayarlar.
//	Şablondan yalnız bayiye zaten GÖRÜNEN özet alanlar alınır (CPU, RAM, PHP, domain/
//	DB/FTP sayısı, posta); kök iç ayarları (IO/DB governor/process/inode/FPM, nginx,
//	WAF) panel varsayılanında kalır (GL-06: bayi kökün iç değerini görmez).
//	Disk/trafik: havuzPayi. Posta: paket tavanına kırpılır.
func bayiVarsayilanDegerler(sablon Plan, hv bayiHavuz) Plan {
	p := panelVarsayilanPlan()
	p.CPUYuzde, p.RAMMB, p.PHPSurum = sablon.CPUYuzde, sablon.RAMMB, sablon.PHPSurum
	p.MaxDomain, p.MaxDB, p.MaxFTP = sablon.MaxDomain, sablon.MaxDB, sablon.MaxFTP
	varsayilanDoldur(&p) // şablonda 0/boş olan CPU/RAM/PHP panel varsayılanına
	tohum := seedPlanlari()[0]
	p.DiskKotaMB = havuzPayi(sablon.DiskKotaMB, hv.MaxDiskMB, hv.MaxDomain, hv.FazlaSatis, tohum.Disk)
	p.TrafikKotaMB = havuzPayi(sablon.TrafikKotaMB, hv.MaxTrafikMB, hv.MaxDomain, hv.FazlaSatis, tohum.Trafik)
	p.MaxEmail = tavanaSigdir(sablon.MaxEmail, hv.MailMaxEmail)
	p.SaatlikMailLimiti = tavanaSigdir(sablon.SaatlikMailLimiti, hv.MailSaatlik)
	p.MailKutuKotaMB = tavanaSigdir(sablon.MailKutuKotaMB, hv.MailKutuMB)
	return p
}

// varsayilanPlanAdi — "<bayi> Varsayılan"; service_plans.ad sunucu genelinde UNIQUE
// olduğundan çakışmada bayi kimliği eklenir. 64 rune sınırı korunur.
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

// varsayilanSablon — kökün şablon planı (bkz. bayiVarsayilanDegerler).
func varsayilanSablon(ctx context.Context, q sorgulayici) (Plan, string) {
	p, err := scan(q.QueryRowContext(ctx, selectAll+
		` WHERE reseller_id=0 AND domain_id IS NULL ORDER BY varsayilan DESC, (disk_kota_mb=0) ASC, disk_kota_mb ASC, id ASC LIMIT 1`))
	if err == nil {
		return p, p.Ad
	}
	t := seedPlanlari()[0]
	return Plan{DiskKotaMB: t.Disk, TrafikKotaMB: t.Trafik, MaxDomain: t.MaxDom, MaxDB: t.MaxDB,
		MaxEmail: t.MaxMail, MaxFTP: t.MaxFTP, CPUYuzde: t.CPU, RAMMB: t.RAM}, t.Ad + " (tohum)"
}

// BayiVarsayilanSagla — bayinin (rid) varsayılan planını GARANTİ eder (idempotent):
//
//	kendi katalog planı + varsayılan işareti var → dokunmaz            (vardi)
//	kendi planı var, hiçbiri varsayılan değil  → en eskisini işaretler  (isaretlendi)
//	kendi planı yok                             → "<bayi> Varsayılan" oluşturur (olusturuldu)
//
// Bayi satırı FOR UPDATE ile kilitlenir (eşzamanlı iki sağlama tek plan üretir).
// Hostinglerin planına/limitine DOKUNMAZ. uid/kul: denetim kaydının aktörü
// (açılışta 0/"sistem").
func BayiVarsayilanSagla(ctx context.Context, db *sql.DB, rid, uid int64, kul string) (int64, string, error) {
	if rid <= 0 {
		return 0, "", ErrBayiYok
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = tx.Rollback() }()
	var ad string
	var hv bayiHavuz
	var fazla int
	var paketID int64
	err = tx.QueryRowContext(ctx,
		`SELECT username, max_domain, max_disk_mb, max_trafik_mb, fazla_satis, reseller_plan_id
		   FROM users WHERE id=? AND role='reseller' FOR UPDATE`, rid).
		Scan(&ad, &hv.MaxDomain, &hv.MaxDiskMB, &hv.MaxTrafikMB, &fazla, &paketID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", ErrBayiYok
	}
	if err != nil {
		return 0, "", err
	}
	hv.FazlaSatis = fazla == 1
	var mevcut int64
	var vars int
	err = tx.QueryRowContext(ctx,
		`SELECT id, varsayilan FROM service_plans WHERE reseller_id=? AND domain_id IS NULL
		  ORDER BY varsayilan DESC, id ASC LIMIT 1 FOR UPDATE`, rid).Scan(&mevcut, &vars)
	switch {
	case err == nil && vars == 1:
		return mevcut, VarsayilanVardi, tx.Commit()
	case err == nil:
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
	case !errors.Is(err, sql.ErrNoRows):
		return 0, "", err
	}
	// Posta tavanı: bağlı bayi paketi (bayiTavanKontrol ile aynı kaynak). Paket
	// yoksa tavan yok.
	if paketID > 0 {
		_ = tx.QueryRowContext(ctx,
			`SELECT COALESCE(mail_max_email,0), COALESCE(mail_saatlik_limit,0), COALESCE(mail_kutu_kota_mb,0)
			   FROM reseller_plans WHERE id=?`, paketID).Scan(&hv.MailMaxEmail, &hv.MailSaatlik, &hv.MailKutuMB)
	}
	sablon, sablonAd := varsayilanSablon(ctx, tx)
	p := bayiVarsayilanDegerler(sablon, hv)
	p.Aciklama = "Otomatik oluşturulan varsayılan plan"
	var id int64
	for deneme := 0; deneme < 4; deneme++ {
		p.Ad = varsayilanPlanAdi(ad, rid, deneme)
		res, err := tx.ExecContext(ctx,
			`INSERT INTO service_plans(ad, aciklama, disk_kota_mb, trafik_kota_mb,
			   max_domain, max_db, max_email, saatlik_mail_limiti, mail_kutu_kota_mb, max_ftp,
			   cpu_yuzde, ram_mb, max_process, inode_kota, io_agirlik, mysql_max_baglanti,
			   pm_max_children, php_surum, fastcgi_cache, client_max_body_mb, nginx_ek_direktifler,
			   waf_enabled, waf_mode, waf_paranoia, varsayilan, reseller_id)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'',0,?,?,1,?)`,
			p.Ad, p.Aciklama, p.DiskKotaMB, p.TrafikKotaMB,
			p.MaxDomain, p.MaxDB, p.MaxEmail, p.SaatlikMailLimiti, p.MailKutuKotaMB, p.MaxFTP,
			p.CPUYuzde, p.RAMMB, p.MaxProcess, p.InodeKota, p.IOAgirlik, p.MySQLMaxBaglanti,
			p.PMMaxChildren, p.PHPSurum, b01(p.FastCgiCache), p.ClientMaxBodyMB,
			p.WafMode, p.WafParanoia, rid)
		if err != nil {
			if tekrarAnahtar(err) {
				continue
			}
			return 0, "", err
		}
		id, _ = res.LastInsertId()
		break
	}
	if id == 0 {
		return 0, "", fmt.Errorf("varsayılan plan adı üretilemedi (bayi %d)", rid)
	}
	if err := tx.Commit(); err != nil {
		return 0, "", err
	}
	httpx.DenetimSistem(db, uid, kul, "plan.varsayilan_olustur", p.Ad,
		fmt.Sprintf("disk=%dMB trafik=%dMB (şablon: %s)", p.DiskKotaMB, p.TrafikKotaMB, sablonAd), rid, true)
	return id, VarsayilanOlusturuldu, nil
}

// BayiVarsayilanUzlastir — açılışta TÜM bayiler için BayiVarsayilanSagla (idempotent).
// Bir bayinin hatası diğerlerini durdurmaz; ilk hata döner.
func BayiVarsayilanUzlastir(ctx context.Context, db *sql.DB) (olusan, isaretlenen int, err error) {
	rows, err := db.QueryContext(ctx, `SELECT id FROM users WHERE role='reseller' ORDER BY id`)
	if err != nil {
		return 0, 0, err
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
		_, durum, e := BayiVarsayilanSagla(ctx, db, rid, 0, "sistem")
		switch {
		case e != nil:
			log.Printf("bayi varsayılan plan: bayi %d sağlanamadı: %v", rid, e)
			if ilk == nil {
				ilk = e
			}
		case durum == VarsayilanOlusturuldu:
			olusan++
		case durum == VarsayilanIsaretlendi:
			isaretlenen++
		}
	}
	return olusan, isaretlenen, ilk
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
