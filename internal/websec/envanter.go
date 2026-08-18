package websec

import (
	"context"
	"database/sql"
	"log"
	"net/http"
)

// ── UYGULAMA ENVANTERI ──────────────────────────────────────────────────────
// 🔴 NEDEN VAR: Monitör bugüne kadar YALNIZCA zafiyet bulgularını gösteriyordu.
// Bilinen açığı olmayan bir domain hiçbir yerde görünmüyordu; yani ekranda
//
//	"her şey güvenli"   ile   "tarayıcı hiç çalışmadı"   AYNI görünüyordu.
//
// Müşteri "panelde 2 domain var ama Website Security Monitor'de listelenmemiş"
// dedi. Ölçtük: tarayıcı ÇALIŞIYORDU (scanned_apps=2, last_success dolu), sadece
// envanter görünmüyordu. Boş bulgu listesi ancak "şunlar tarandı" listesiyle
// birlikte anlam kazanır — aksi halde sessiz bir arıza güvence gibi okunur.
//
// Bu dosya taranan her uygulamayı kaydeder. Bulgu sayısı 0 olsa bile satır YAZILIR;
// asıl değeri zaten budur.

// envanterYaz: bir uygulamayı envantere yaz/güncelle (idempotent, UPSERT).
//
// Hata YUTULMAZ ama tarama da DURDURULMAZ: envanter ikincil bir kayıttır,
// yazılamaması zafiyet taramasını iptal etmeyi haklı çıkarmaz. Yine de log'a
// düşer, çünkü sessizce kaybolan kayıt tam da bu dosyanın çözmeye çalıştığı
// problemin ta kendisidir.
func envanterYaz(ctx context.Context, db *sql.DB, domainID int64, appType, yol, surum string, paketSayisi, bulguSayisi int) {
	_, err := db.ExecContext(ctx, `
		INSERT INTO cp_websec_apps
		    (domain_id, app_type, install_path, app_version, paket_sayisi, bulgu_sayisi, son_tarama)
		VALUES (?, ?, ?, ?, ?, ?, NOW())
		ON DUPLICATE KEY UPDATE
		    app_version  = VALUES(app_version),
		    paket_sayisi = VALUES(paket_sayisi),
		    bulgu_sayisi = VALUES(bulgu_sayisi),
		    son_tarama   = NOW()`,
		domainID, appType, yol, surum, paketSayisi, bulguSayisi)
	if err != nil {
		log.Printf("websec envanter: %s %s yazilamadi: %v", appType, yol, err)
	}
}

// envanterSolmaTemizle: bu turda GÖRÜLMEYEN kurulumları düşür.
//
// Site silinmiş/WordPress kaldırılmışsa envanterde kalmamalı — aksi halde panel
// var olmayan bir kurulumu "tarandı" diye gösterir. Bulgu tablosundaki solma
// temizliğiyle aynı mantık: yalnızca BAŞARILI ekosistemler için, ve kısmi
// taramada yalnızca ilgili domain'ler için.
func envanterSolmaTemizle(ctx context.Context, db *sql.DB, esik any, appTypes []string, domainIDs []int64) {
	if len(appTypes) == 0 {
		return
	}
	sorgu := `DELETE FROM cp_websec_apps WHERE son_tarama < ? AND app_type IN (`
	args := []any{esik}
	for i, a := range appTypes {
		if i > 0 {
			sorgu += ","
		}
		sorgu += "?"
		args = append(args, a)
	}
	sorgu += ")"
	if len(domainIDs) > 0 {
		sorgu += " AND domain_id IN ("
		for i, id := range domainIDs {
			if i > 0 {
				sorgu += ","
			}
			sorgu += "?"
			args = append(args, id)
		}
		sorgu += ")"
	}
	if _, err := db.ExecContext(ctx, sorgu, args...); err != nil {
		log.Printf("websec envanter: solma temizligi: %v", err)
	}
}

type envanterSatir struct {
	DomainID    int64  `json:"domain_id"`
	AlanAdi     string `json:"alan_adi"`
	AppType     string `json:"app_type"`
	InstallPath string `json:"install_path"`
	AppVersion  string `json:"app_version"`
	PaketSayisi int    `json:"paket_sayisi"`
	BulguSayisi int    `json:"bulgu_sayisi"`
	SonTarama   string `json:"son_tarama"`
}

type envanterYanit struct {
	Toplam int             `json:"toplam"`
	Items  []envanterSatir `json:"items"`
	// TaranmayanDomainler: panelde kayıtlı ama envanterde HİÇ görünmeyen
	// domain'ler. "Bulgu yok" ile "hiç bakılmadı" ayrımını kullanıcıya
	// AÇIKÇA gösteren alan budur — bu listenin dolu olması bir uyarıdır.
	TaranmayanDomainler []string `json:"taranmayan_domainler"`
}

// Apps: GET /api/v1/websec/apps — taranan uygulama envanteri.
func (h *Handler) Apps(w http.ResponseWriter, r *http.Request) {
	y := envanterYanit{Items: []envanterSatir{}, TaranmayanDomainler: []string{}}

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT a.domain_id, COALESCE(d.alan_adi,''), a.app_type, a.install_path,
		       a.app_version, a.paket_sayisi, a.bulgu_sayisi,
		       DATE_FORMAT(a.son_tarama,'%Y-%m-%d %H:%i:%s')
		  FROM cp_websec_apps a
		  LEFT JOIN domains d ON d.id = a.domain_id
		 ORDER BY a.bulgu_sayisi DESC, d.alan_adi ASC, a.app_type ASC`)
	if err != nil {
		hataMesaji(w, 500, err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var s envanterSatir
		if err := rows.Scan(&s.DomainID, &s.AlanAdi, &s.AppType, &s.InstallPath,
			&s.AppVersion, &s.PaketSayisi, &s.BulguSayisi, &s.SonTarama); err != nil {
			hataMesaji(w, 500, err.Error())
			return
		}
		y.Items = append(y.Items, s)
	}
	// 🔴 rows.Err() KONTROL EDILMELI: satır döngüsü ortasında kopan bir sorgu
	// sessizce KISMİ liste döndürür ve "az uygulama var" gibi görünür.
	if err := rows.Err(); err != nil {
		hataMesaji(w, 500, err.Error())
		return
	}
	y.Toplam = len(y.Items)

	// Envanterde hiç görünmeyen domain'ler
	drows, err := h.DB.QueryContext(r.Context(), `
		SELECT d.alan_adi FROM domains d
		 WHERE NOT EXISTS (SELECT 1 FROM cp_websec_apps a WHERE a.domain_id = d.id)
		 ORDER BY d.alan_adi`)
	if err == nil {
		defer drows.Close()
		for drows.Next() {
			var ad string
			if drows.Scan(&ad) == nil && ad != "" {
				y.TaranmayanDomainler = append(y.TaranmayanDomainler, ad)
			}
		}
	}
	jsonYaz(w, 200, y)
}
