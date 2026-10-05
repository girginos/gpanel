package websec

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strconv"
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

// domainTaramaYaz: bu turda taranan domainlere son_tarama damgası (0112). Envanterin
// "desteklenmiyor" (tarandı, uygulama yok) ile "beklemede" (henüz taranmadı) ayrımı
// buradan gelir — eskiden sunucu geneli last_success'ten geliyordu ve son taramadan
// SONRA açılan domain de "desteklenmiyor" görünüyordu. Kısmi taramada listedeki
// domainler (keşif tam onları sorguladı); tam taramada (domainIDs boş) tarama
// başladıktan (gecenSn önce) SONRA açılan domainler damgalanmaz: keşif onları
// görmedi. Eşik DB saatiyle kurulur (NOW() - INTERVAL): Go/MariaDB saat dilimi karışmaz.
func domainTaramaYaz(ctx context.Context, db *sql.DB, domainIDs []int64, gecenSn int64) {
	sorgu := `INSERT INTO cp_websec_domain_tarama (domain_id, son_tarama)
		SELECT d.id, NOW() FROM domains d WHERE `
	var args []any
	if len(domainIDs) > 0 {
		sorgu += `d.id IN (` + yerTutucu(len(domainIDs)) + `)` //nolint:gosec // G202: yerTutucu yalnız "?" üretir; id'ler args ile bağlanır.
		for _, id := range domainIDs {
			args = append(args, id)
		}
	} else {
		sorgu += `(d.olusturulma IS NULL OR d.olusturulma <= NOW() - INTERVAL ? SECOND)`
		args = append(args, gecenSn)
	}
	sorgu += ` ON DUPLICATE KEY UPDATE son_tarama = VALUES(son_tarama)`
	if _, err := db.ExecContext(ctx, sorgu, args...); err != nil { //nolint:gosec // G701: sorgu SABİT parçalar + yalnız "?"; değerler args ile bağlanır.
		log.Printf("websec: domain tarama kaydı yazılamadı: %v", err)
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
	// DomainSonTarama — domainin kendi son tarama zamanı (cp_websec_domain_tarama);
	// uygulaması olmayan domain için de dolu. Arayüz "tarama bitti" bunu izler.
	DomainSonTarama string `json:"domain_son_tarama"`
	// SonSonuc — domainin son tarama sonucu (taranmışsa): bulunan uygulama + bulgu sayısı.
	SonSonuc *sonSonuc `json:"son_sonuc,omitempty"`
	// Durum: taraniyor | sirada | acik | temiz | desteklenmiyor | beklemede.
	// Her domain'e bir durum verir; frontend rozet + yönlendirme buradan.
	Durum string `json:"durum"`
}

type sonSonuc struct {
	UygulamaSayisi int `json:"uygulama_sayisi"`
	BulguSayisi    int `json:"bulgu_sayisi"`
}

// sonSonuclariYaz — taranmış domainlerin her satırına domain düzeyi son sonuç
// (uygulama sayısı + toplam bulgu) yazar; uygulaması olmayan domain 0/0 alır:
// arayüz "taranacak uygulama bulunamadı" bunu gösterir. Taranmamış (DomainSonTarama
// boş) domain SonSonuc taşımaz.
func sonSonuclariYaz(items []envanterSatir) {
	topl := map[int64]*sonSonuc{}
	for _, it := range items {
		if it.DomainSonTarama == "" {
			continue
		}
		t := topl[it.DomainID]
		if t == nil {
			t = &sonSonuc{}
			topl[it.DomainID] = t
		}
		if it.AppType != "" {
			t.UygulamaSayisi++
			t.BulguSayisi += it.BulguSayisi
		}
	}
	for i := range items {
		items[i].SonSonuc = topl[items[i].DomainID]
	}
}

type envanterYanit struct {
	Toplam int             `json:"toplam"`
	Items  []envanterSatir `json:"items"`
	// Desteklenmeyen: uygulaması bulunamayan domain sayısı (özet sayaç).
	Desteklenmeyen int `json:"desteklenmeyen"`
}

// Apps: GET /api/v1/websec/apps[?domain_id=] — görünür HER domain'in güvenlik durumu.
//
// 🔴 SÜRÜCÜ TABLO = domains (cp_websec_apps DEĞİL). Önceden yalnız app'i tespit
// edilmiş domain'ler listeleniyordu → "panelde 9 domain var, monitörde 3 görünüyor"
// tutarsızlığı. Artık her domain bir satır alır; app'i olmayanlar da durum'la görünür
// (taranıyor / sırada / desteklenmiyor / beklemede). Bir domain'in birden çok app'i varsa
// (nadiren) app başına satır döner — "Taranan uygulamalar" semantiği korunur.
//
// Kapsam (kapsam.go): kök tüm domainleri, bayi YALNIZ kendi domainlerini görür.
// domain_id süzgeci domain sayfası içindir; geçersiz değer hiçbir satır döndürmez,
// yabancı domain ile olmayan domain aynı boş yanıtı alır.
func (h *Handler) Apps(w http.ResponseWriter, r *http.Request) {
	y := envanterYanit{Items: []envanterSatir{}}
	k := kapsamAl(r)
	where, args := k.kosul()
	if dom := r.URL.Query().Get("domain_id"); dom != "" {
		if n, e := strconv.ParseInt(dom, 10, 64); e == nil && n > 0 {
			where += " AND d.id=?"
			args = append(args, n)
		} else {
			where += " AND 1=0"
		}
	}

	// Domain başına "taranıyor"/"sirada" için canlı tarama durumu ve kuyruk (bellek).
	kosuyor, taranSet := TaramaDurumu()
	bekleyen := kuy.bekleyenKume()

	// taranmis: domainin kendi tarama kaydı var mı (0112) — "desteklenmiyor"
	// (görüldü, app yok) ile "beklemede" (henüz taranmadı) ayrımı için.
	//nolint:gosec // G202/G701: where yalnız sabit parçalar + "?" (kapsam.kosul, d.id=?); değerler args ile bağlanır.
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT d.id, COALESCE(d.alan_adi,''),
		       COALESCE(a.app_type,''), COALESCE(a.install_path,''),
		       COALESCE(a.app_version,''), COALESCE(a.paket_sayisi,0),
		       COALESCE(a.bulgu_sayisi,0),
		       COALESCE(DATE_FORMAT(a.son_tarama,'%Y-%m-%d %H:%i:%s'), DATE_FORMAT(t.son_tarama,'%Y-%m-%d %H:%i:%s'), ''),
		       COALESCE(DATE_FORMAT(t.son_tarama,'%Y-%m-%d %H:%i:%s'),''),
		       (t.domain_id IS NOT NULL)
		  FROM domains d
		  LEFT JOIN cp_websec_apps a ON a.domain_id = d.id
		  LEFT JOIN cp_websec_domain_tarama t ON t.domain_id = d.id
		 WHERE 1=1`+where+`
		 ORDER BY (a.app_type IS NULL) ASC, a.bulgu_sayisi DESC, d.alan_adi ASC`, args...)
	if err != nil {
		k.hata(w, err)
		return
	}
	defer rows.Close()
	desteklenmeyen := 0
	for rows.Next() {
		var s envanterSatir
		var taranmis bool
		if err := rows.Scan(&s.DomainID, &s.AlanAdi, &s.AppType, &s.InstallPath,
			&s.AppVersion, &s.PaketSayisi, &s.BulguSayisi, &s.SonTarama, &s.DomainSonTarama, &taranmis); err != nil {
			k.hata(w, err)
			return
		}
		switch {
		case kosuyor && (len(taranSet) == 0 || taranSet[s.DomainID]):
			s.Durum = "taraniyor"
		case bekleyen[s.DomainID]:
			s.Durum = "sirada"
		case s.AppType != "" && s.BulguSayisi > 0:
			s.Durum = "acik"
		case s.AppType != "":
			s.Durum = "temiz"
		case taranmis:
			s.Durum = "desteklenmiyor"
			desteklenmeyen++
		default:
			s.Durum = "beklemede"
		}
		y.Items = append(y.Items, s)
	}
	// 🔴 rows.Err() KONTROL EDILMELI: kopan sorgu sessizce KISMİ liste döndürür.
	if err := rows.Err(); err != nil {
		k.hata(w, err)
		return
	}
	sonSonuclariYaz(y.Items)
	y.Toplam = len(y.Items)
	y.Desteklenmeyen = desteklenmeyen
	jsonYaz(w, 200, y)
}
