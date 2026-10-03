package pano

// uretici.go — bildirim ÜRETİCİLERİ (örnekleyiciden): kota/havuz/SSL eşikleri
// (kenar-tetikli, cp_esik_durum), Website Security yeni yüksek bulgu, biten
// taşıma/toplu işler. Alıcı kapsamı bildirim paketinin hedefleriyle:
//
//	domain olayı → YazOlay(domainID): domain sahibi (bayi/yönetici) + müşterisi
//	bayi havuzu  → YazBayi(rid): yalnız o bayi (+ %100'de asim_bildirim=1 ise yönetici)
//	taşıma       → işin sahibi (sahip_rid>0 → bayi, 0 → yönetici)
//	toplu işlem  → yönetici (cp_toplu_isler sahipsiz; bayi toplu işlem başlatamaz)

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"girginospanel/internal/bildirim"
)

// Bastırma pencereleri (aynı olay bu süre içinde ikinci kez yazılmaz).
const (
	esikBastir   = 6 * time.Hour
	websecBastir = time.Hour
)

func boyutYaz(mb float64) string {
	if mb >= 1024 {
		return fmt.Sprintf("%.1f GB", mb/1024)
	}
	return fmt.Sprintf("%.0f MB", mb)
}

func kotaBaslik(metrik string, kova int) (string, string) {
	ad := map[string]string{"disk": "Disk kotası", "trafik": "Aylık trafik kotası", "hesap": "Hesap sayısı"}[metrik]
	if kova >= 100 {
		return "kritik", ad + " doldu"
	}
	return "uyari", fmt.Sprintf("%s %%%d'i aştı", ad, kova)
}

// kotaEsikTara — domain başına disk ve aylık trafik kovası.
func kotaEsikTara(ctx context.Context, db *sql.DB) error {
	satirlar, err := kotaSatirlari(ctx, db, Kapsam{Tum: true}, 0)
	if err != nil {
		return err
	}
	sahip := map[int64]int64{}
	rows, err := db.QueryContext(ctx, `SELECT id, reseller_id FROM domains`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, r int64
		if rows.Scan(&id, &r) == nil {
			sahip[id] = r
		}
	}
	rows.Close()
	eski, err := durumlariYukle(ctx, db, "domain")
	if err != nil {
		return err
	}
	gorulen := map[anahtar]bool{}
	for _, s := range satirlar {
		a := anahtar{s.ID, s.Metrik}
		gorulen[a] = true
		simdiSahip := sahip[s.ID]
		e, bulundu := eski[a]
		onceki := int(e.seviye)
		if bulundu && e.sahip != simdiSahip {
			onceki = 0 // devredildi: yeni sahip yukarı geçişi bir kez görür
		}
		yeni := kotaKovasi(onceki, s.Yuzde)
		if !bulundu && yeni == 0 {
			continue // normal ve kayıt yok: tablo büyümesin
		}
		if bulundu && int64(yeni) == e.seviye && e.sahip == simdiSahip {
			continue
		}
		if err := durumYaz(ctx, db, "domain", s.ID, s.Metrik, int64(yeni), s.Yuzde, simdiSahip); err != nil {
			return err
		}
		if yukariGecti(onceki, yeni) {
			sev, baslik := kotaBaslik(s.Metrik, yeni)
			bildirim.YazOlay(db, bildirim.Olay{
				Seviye: sev, Kategori: "kota", Baslik: baslik,
				Mesaj:  fmt.Sprintf("%s: %%%.0f kullanımda (%s / %s)", s.AlanAdi, s.Yuzde, boyutYaz(s.KullanimMB), boyutYaz(float64(s.LimitMB))),
				RefTur: "esik_" + s.Metrik, RefID: int64(yeni),
			}, s.ID, bildirim.AktorYok, esikBastir)
		}
	}
	// Limiti kaldırılan / ölçümü düşen domainin durumu sıfırlanır.
	for a, e := range eski {
		if (a.metrik == "disk" || a.metrik == "trafik") && !gorulen[a] && e.seviye > 0 {
			if err := durumYaz(ctx, db, "domain", a.id, a.metrik, 0, 0, sahip[a.id]); err != nil {
				return err
			}
		}
	}
	return nil
}

// havuzEsikTara — bayi kaynak havuzu (users.max_*; 0 = sınırsız → atlanır).
func havuzEsikTara(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx,
		`SELECT u.id, u.username, u.max_domain, u.max_disk_mb, u.max_trafik_mb, u.asim_bildirim,
		        COUNT(d.id), COALESCE(SUM(d.boyut_kb),0), COALESCE(SUM(d.trafik_kb),0)
		   FROM users u LEFT JOIN domains d ON d.reseller_id=u.id
		  WHERE u.role='reseller' GROUP BY u.id`)
	if err != nil {
		return err
	}
	type havuz struct {
		rid                int64
		ad                 string
		maxHesap           int
		maxDisk, maxTrafik int64
		asim               bool
		hesap              int
		diskKB, trafikKB   int64
	}
	var liste []havuz
	for rows.Next() {
		var h havuz
		if err := rows.Scan(&h.rid, &h.ad, &h.maxHesap, &h.maxDisk, &h.maxTrafik, &h.asim, &h.hesap, &h.diskKB, &h.trafikKB); err != nil {
			rows.Close()
			return err
		}
		liste = append(liste, h)
	}
	rows.Close()
	eski, err := durumlariYukle(ctx, db, "bayi")
	if err != nil {
		return err
	}
	for _, h := range liste {
		type olcu struct {
			metrik      string
			kull, limit float64
			metin       string
		}
		var olculer []olcu
		if h.maxHesap > 0 {
			olculer = append(olculer, olcu{"hesap", float64(h.hesap), float64(h.maxHesap), fmt.Sprintf("%d / %d hesap", h.hesap, h.maxHesap)})
		}
		if h.maxDisk > 0 {
			mb := float64(h.diskKB) / 1024
			olculer = append(olculer, olcu{"disk", mb, float64(h.maxDisk), boyutYaz(mb) + " / " + boyutYaz(float64(h.maxDisk))})
		}
		if h.maxTrafik > 0 {
			mb := float64(h.trafikKB) / 1024
			olculer = append(olculer, olcu{"trafik", mb, float64(h.maxTrafik), boyutYaz(mb) + " / " + boyutYaz(float64(h.maxTrafik))})
		}
		gorulen := map[string]bool{}
		for _, o := range olculer {
			gorulen[o.metrik] = true
			y := yuvarla(o.kull * 100 / o.limit)
			a := anahtar{h.rid, o.metrik}
			e, bulundu := eski[a]
			onceki := int(e.seviye)
			yeni := kotaKovasi(onceki, y)
			if (!bulundu && yeni == 0) || (bulundu && int64(yeni) == e.seviye) {
				continue
			}
			if err := durumYaz(ctx, db, "bayi", h.rid, o.metrik, int64(yeni), y, h.rid); err != nil {
				return err
			}
			if !yukariGecti(onceki, yeni) {
				continue
			}
			sev, baslik := kotaBaslik(o.metrik, yeni)
			olay := bildirim.Olay{Seviye: sev, Kategori: "kota", Baslik: "Kaynak havuzu: " + baslik,
				Mesaj: fmt.Sprintf("%%%.0f kullanımda (%s)", y, o.metin), RefTur: "havuz_" + o.metrik, RefID: int64(yeni)}
			bildirim.YazBayi(db, h.rid, olay, esikBastir)
			// users.asim_bildirim (göç 0047, "aşımda yöneticiye bildir") ilk kez işlevsel:
			// havuz doldu → yönetici de bilgilendirilir (panel geneli).
			if yeni >= 100 && h.asim {
				olay.Baslik = "Bayi kaynak havuzu doldu"
				olay.Mesaj = fmt.Sprintf("Bayi %s: %s %%%.0f (%s)", h.ad, map[string]string{"hesap": "hesap sayısı", "disk": "disk", "trafik": "aylık trafik"}[o.metrik], y, o.metin)
				olay.RefTur, olay.RefID = "esik_bayi_"+o.metrik, h.rid
				bildirim.YazOlay(db, olay, 0, bildirim.AktorYok, esikBastir)
			}
		}
		for _, m := range []string{"hesap", "disk", "trafik"} {
			if e, bulundu := eski[anahtar{h.rid, m}]; bulundu && !gorulen[m] && e.seviye > 0 {
				if err := durumYaz(ctx, db, "bayi", h.rid, m, 0, 0, h.rid); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// sslEsikTara — SSL kalan gün seviyesi (ssl_bitis, SSL taramasıyla diskteki
// sertifikadan tazelenir). Askıdaki/demo hesap ve SSL'i kapatılan domain atlanır.
func sslEsikTara(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx,
		`SELECT id, alan_adi, reseller_id, DATEDIFF(ssl_bitis, CURDATE()) FROM domains
		  WHERE ssl_aktif=1 AND ssl_bitis IS NOT NULL AND COALESCE(askida,0)=0 AND COALESCE(is_demo,0)=0`)
	if err != nil {
		return err
	}
	type s struct {
		id, sahip int64
		alan      string
		kalan     int
	}
	var liste []s
	for rows.Next() {
		var x s
		if rows.Scan(&x.id, &x.alan, &x.sahip, &x.kalan) == nil {
			liste = append(liste, x)
		}
	}
	rows.Close()
	eski, err := durumlariYukle(ctx, db, "domain")
	if err != nil {
		return err
	}
	gorulen := map[int64]bool{}
	for _, x := range liste {
		gorulen[x.id] = true
		a := anahtar{x.id, "ssl"}
		e, bulundu := eski[a]
		onceki := int(e.seviye)
		if bulundu && e.sahip != x.sahip {
			onceki = sslNormal
		}
		yeni := sslSeviyesi(onceki, x.kalan)
		if (!bulundu && yeni == sslNormal) || (bulundu && int64(yeni) == e.seviye && e.sahip == x.sahip) {
			continue
		}
		if err := durumYaz(ctx, db, "domain", x.id, "ssl", int64(yeni), float64(x.kalan), x.sahip); err != nil {
			return err
		}
		if !yukariGecti(onceki, yeni) {
			continue
		}
		sev, baslik, mesaj := "uyari", "SSL sertifikası yenilenemedi", fmt.Sprintf("%s: sertifikanın bitmesine %d gün kaldı; otomatik yenileme başarısız görünüyor.", x.alan, x.kalan)
		switch yeni {
		case sslKritik:
			sev, baslik = "kritik", "SSL sertifikasının süresi doluyor"
		case sslDoldu:
			sev, baslik, mesaj = "kritik", "SSL sertifikasının süresi doldu", fmt.Sprintf("%s: sertifikanın süresi %d gün önce doldu; ziyaretçiler güvenlik uyarısı görüyor.", x.alan, -x.kalan)
		}
		bildirim.YazOlay(db, bildirim.Olay{Seviye: sev, Kategori: "ssl", Baslik: baslik, Mesaj: mesaj,
			RefTur: "esik_ssl", RefID: int64(yeni)}, x.id, bildirim.AktorYok, esikBastir)
	}
	for a, e := range eski {
		if a.metrik == "ssl" && !gorulen[a.id] && e.seviye > 0 {
			if err := durumYaz(ctx, db, "domain", a.id, "ssl", 0, 0, e.sahip); err != nil {
				return err
			}
		}
	}
	return nil
}

// websecTara — domain başına yeni kritik/yüksek Website Security bulgusu. İmleç =
// o domainin bildirilen en büyük bulgu kimliği. İlk kurulumda mevcut bulgular
// taban çizgisidir (bildirim yağmuru yok); sonrasında kaydı olmayan domainin ilk
// bulgusu da bildirilir.
func websecTara(ctx context.Context, db *sql.DB) error {
	ilk := !imlecVar(ctx, db, "websec_taban")
	rows, err := db.QueryContext(ctx,
		`SELECT f.domain_id, d.reseller_id, d.alan_adi, MAX(f.id) FROM cp_websec_findings f JOIN domains d ON d.id=f.domain_id
		  WHERE LOWER(f.severity) IN ('critical','high') GROUP BY f.domain_id, d.reseller_id, d.alan_adi`)
	if err != nil {
		return err
	}
	type s struct {
		id, sahip, maxID int64
		alan             string
	}
	var liste []s
	for rows.Next() {
		var x s
		if rows.Scan(&x.id, &x.sahip, &x.alan, &x.maxID) == nil {
			liste = append(liste, x)
		}
	}
	rows.Close()
	eski, err := durumlariYukle(ctx, db, "domain")
	if err != nil {
		return err
	}
	for _, x := range liste {
		e := eski[anahtar{x.id, "websec"}]
		if x.maxID <= e.seviye {
			continue
		}
		if err := durumYaz(ctx, db, "domain", x.id, "websec", x.maxID, 0, x.sahip); err != nil {
			return err
		}
		if ilk {
			continue
		}
		var yeni, kritik int
		var ornek sql.NullString
		_ = db.QueryRowContext(ctx,
			`SELECT COUNT(*), COALESCE(SUM(LOWER(severity)='critical'),0),
			        SUBSTRING_INDEX(GROUP_CONCAT(CONCAT(package_name,' ',cve_id) ORDER BY id DESC SEPARATOR '|'),'|',1)
			   FROM cp_websec_findings WHERE domain_id=? AND id>? AND LOWER(severity) IN ('critical','high')`,
			x.id, e.seviye).Scan(&yeni, &kritik, &ornek)
		if yeni == 0 {
			continue
		}
		sev := "uyari"
		if kritik > 0 {
			sev = "kritik"
		}
		mesaj := fmt.Sprintf("%s: %d yeni kritik/yüksek önemli güvenlik açığı", x.alan, yeni)
		if ornek.Valid && ornek.String != "" {
			mesaj += " (ör. " + ornek.String + ")"
		}
		bildirim.YazOlay(db, bildirim.Olay{Seviye: sev, Kategori: "websec", Baslik: "Website Security: yeni yüksek önemli bulgu",
			Mesaj: mesaj, RefTur: "websec", RefID: x.maxID}, x.id, bildirim.AktorYok, websecBastir)
	}
	if ilk {
		imlecYaz(ctx, db, "websec_taban")
	}
	return nil
}

// IsTur — biten taşıma ve toplu işlemleri bildirir. İş başına "bildirildi"
// işareti (konu_tur='is'); ilk kurulumda mevcut işler taban çizgisidir.
func IsTur(ctx context.Context, db *sql.DB) {
	ilk := !imlecVar(ctx, db, "is_taban")
	if err := tasimaBitti(ctx, db, ilk); err != nil {
		log.Printf("pano: taşıma bildirimleri: %v", err)
	}
	if err := topluBitti(ctx, db, ilk); err != nil {
		log.Printf("pano: toplu işlem bildirimleri: %v", err)
	}
	if ilk {
		imlecYaz(ctx, db, "is_taban")
	}
}

func tasimaBitti(ctx context.Context, db *sql.DB, ilk bool) error {
	rows, err := db.QueryContext(ctx,
		`SELECT t.id, t.durum, t.toplam, t.tamamlanan, t.basarisiz, t.sahip_rid, t.kaynak_tip
		   FROM tasima_isleri t
		   LEFT JOIN cp_esik_durum e ON e.konu_tur='is' AND e.konu_id=t.id AND e.metrik='tasima'
		  WHERE t.durum IN ('tamam','hata','kesildi') AND e.konu_id IS NULL
		  ORDER BY t.id LIMIT 100`)
	if err != nil {
		return err
	}
	type s struct {
		id, rid                  int64
		durum, tip               string
		toplam, tamam, basarisiz int
	}
	var liste []s
	for rows.Next() {
		var x s
		if rows.Scan(&x.id, &x.durum, &x.toplam, &x.tamam, &x.basarisiz, &x.rid, &x.tip) == nil {
			liste = append(liste, x)
		}
	}
	rows.Close()
	for _, x := range liste {
		if err := durumYaz(ctx, db, "is", x.id, "tasima", 1, 0, x.rid); err != nil {
			return err
		}
		if ilk {
			continue
		}
		sev, baslik := "bilgi", "Taşıma tamamlandı"
		switch {
		case x.durum == "hata":
			sev, baslik = "kritik", "Taşıma hatayla bitti"
		case x.durum == "kesildi":
			sev, baslik = "uyari", "Taşıma yarıda kesildi"
		case x.basarisiz > 0:
			sev, baslik = "uyari", "Taşıma kısmen tamamlandı"
		}
		olay := bildirim.Olay{Seviye: sev, Kategori: "tasima", Baslik: baslik,
			Mesaj:  fmt.Sprintf("İş #%d (%s): %d/%d hesap aktarıldı, %d başarısız", x.id, x.tip, x.tamam, x.toplam, x.basarisiz),
			RefTur: "tasima", RefID: x.id}
		if x.rid > 0 {
			bildirim.YazBayi(db, x.rid, olay, 0)
		} else {
			bildirim.YazOlay(db, olay, 0, bildirim.AktorYok, 0)
		}
	}
	return nil
}

func topluBitti(ctx context.Context, db *sql.DB, ilk bool) error {
	rows, err := db.QueryContext(ctx,
		`SELECT t.id, t.tip, t.durum, t.toplam, t.basari, t.hata
		   FROM cp_toplu_isler t
		   LEFT JOIN cp_esik_durum e ON e.konu_tur='is' AND e.konu_id=t.id AND e.metrik='toplu'
		  WHERE t.durum IN ('tamam','kismi','hata') AND e.konu_id IS NULL
		  ORDER BY t.id LIMIT 100`)
	if err != nil {
		return err
	}
	type s struct {
		id                   int64
		tip, durum           string
		toplam, basari, hata int
	}
	var liste []s
	for rows.Next() {
		var x s
		if rows.Scan(&x.id, &x.tip, &x.durum, &x.toplam, &x.basari, &x.hata) == nil {
			liste = append(liste, x)
		}
	}
	rows.Close()
	for _, x := range liste {
		if err := durumYaz(ctx, db, "is", x.id, "toplu", 1, 0, 0); err != nil {
			return err
		}
		if ilk {
			continue
		}
		sev, baslik := "bilgi", "Toplu işlem tamamlandı"
		switch x.durum {
		case "hata":
			sev, baslik = "kritik", "Toplu işlem başarısız oldu"
		case "kismi":
			sev, baslik = "uyari", "Toplu işlem kısmen tamamlandı"
		}
		bildirim.YazOlay(db, bildirim.Olay{Seviye: sev, Kategori: "toplu", Baslik: baslik,
			Mesaj:  fmt.Sprintf("İş #%d (%s): %d başarılı, %d hatalı / %d hesap", x.id, x.tip, x.basari, x.hata, x.toplam),
			RefTur: "toplu", RefID: x.id}, 0, bildirim.AktorYok, 0)
	}
	return nil
}
