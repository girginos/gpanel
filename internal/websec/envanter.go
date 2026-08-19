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
	// Durum: taraniyor | acik | temiz | desteklenmiyor | beklemede.
	// Her domain'e bir durum verir; frontend rozet + yönlendirme buradan.
	Durum string `json:"durum"`
}

type envanterYanit struct {
	Toplam int             `json:"toplam"`
	Items  []envanterSatir `json:"items"`
	// Desteklenmeyen: uygulaması bulunamayan domain sayısı (özet sayaç).
	Desteklenmeyen int `json:"desteklenmeyen"`
}

// Apps: GET /api/v1/websec/apps — PANELDEKİ HER domain'in güvenlik durumu.
//
// 🔴 SÜRÜCÜ TABLO = domains (cp_websec_apps DEĞİL). Önceden yalnız app'i tespit
// edilmiş domain'ler listeleniyordu → "panelde 9 domain var, monitörde 3 görünüyor"
// tutarsızlığı. Artık her domain bir satır alır; app'i olmayanlar da durum'la görünür
// (taranıyor / desteklenmiyor / beklemede). Bir domain'in birden çok app'i varsa
// (nadiren) app başına satır döner — "Taranan uygulamalar" semantiği korunur.
func (h *Handler) Apps(w http.ResponseWriter, r *http.Request) {
	y := envanterYanit{Items: []envanterSatir{}}

	// Domain başına "taranıyor" için canlı tarama durumu (bellek).
	kosuyor, taranSet := TaramaDurumu()
	// Hiç tam tarama tamamlandı mı? — "desteklenmiyor" (görüldü, app yok) ile
	// "beklemede" (henüz taranmadı) ayrımı için.
	var taramaOldu bool
	{
		var ls sql.NullString
		_ = h.DB.QueryRowContext(r.Context(),
			`SELECT last_success FROM cp_websec_status WHERE id=1`).Scan(&ls)
		taramaOldu = ls.Valid && ls.String != ""
	}

	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT d.id, COALESCE(d.alan_adi,''),
		       COALESCE(a.app_type,''), COALESCE(a.install_path,''),
		       COALESCE(a.app_version,''), COALESCE(a.paket_sayisi,0),
		       COALESCE(a.bulgu_sayisi,0),
		       COALESCE(DATE_FORMAT(a.son_tarama,'%Y-%m-%d %H:%i:%s'),'')
		  FROM domains d
		  LEFT JOIN cp_websec_apps a ON a.domain_id = d.id
		 ORDER BY (a.app_type IS NULL) ASC, a.bulgu_sayisi DESC, d.alan_adi ASC`)
	if err != nil {
		hataMesaji(w, 500, err.Error())
		return
	}
	defer rows.Close()
	desteklenmeyen := 0
	for rows.Next() {
		var s envanterSatir
		if err := rows.Scan(&s.DomainID, &s.AlanAdi, &s.AppType, &s.InstallPath,
			&s.AppVersion, &s.PaketSayisi, &s.BulguSayisi, &s.SonTarama); err != nil {
			hataMesaji(w, 500, err.Error())
			return
		}
		switch {
		case kosuyor && (len(taranSet) == 0 || taranSet[s.DomainID]):
			s.Durum = "taraniyor"
		case s.AppType != "" && s.BulguSayisi > 0:
			s.Durum = "acik"
		case s.AppType != "":
			s.Durum = "temiz"
		case taramaOldu:
			s.Durum = "desteklenmiyor"
			desteklenmeyen++
		default:
			s.Durum = "beklemede"
		}
		y.Items = append(y.Items, s)
	}
	// 🔴 rows.Err() KONTROL EDILMELI: kopan sorgu sessizce KISMİ liste döndürür.
	if err := rows.Err(); err != nil {
		hataMesaji(w, 500, err.Error())
		return
	}
	y.Toplam = len(y.Items)
	y.Desteklenmeyen = desteklenmeyen
	jsonYaz(w, 200, y)
}
