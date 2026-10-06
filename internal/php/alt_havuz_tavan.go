package php

// alt_havuz_tavan.go — alt alan havuzlarının pm.max_children TOPLAMINA hesap bazlı tavan.
//
// 🔴 NEDEN: alt alan havuzları paylaşılan php-fpm master'ında, hesabın
// girginos-<sk>.slice'ı DIŞINDA koşar. havuzTavanla her havuzu TEK TEK plan tavanına
// (P = pm_max_children) kırpıyordu; ama bir hesap N alt alan havuzu açınca toplam
// işçi N×P olur — slice'ın RAM/süreç sınırı dışında, planın N katı. Her alt alan
// ayrı havuz olduğundan tek-havuz tavanı bunu görmez.
//
// KARAR / FORMÜL (hesap bütçesi):
//   - Bütçe B = hesabın alt alan havuzlarının plan tavanlarının (P_i, efektif plan:
//     pm_max_children ya da max(4, ram_mb/64) — slice MemoryMax ile tutarlı türetim)
//     en büyüğü. Tipik hesapta tüm alan adları tek plandadır → B = P: hesabın
//     slice DIŞINDAKİ alt alan işçilerinin toplamı bir planlık işçiyi aşmaz (slice
//     İÇİ ana havuzla birlikte hesap tavanı 2P).
//   - Dağıtım id sırasıyla (eski alt alan önce): her havuz min(P_i, kalan) alır,
//     en az 1. Yani YENİ alt alan havuzu kısılır, mevcutlar etkilenmez; sıra
//     havuzların uygulanma sırasından BAĞIMSIZDIR (her çağrı aynı dağıtımı yeniden
//     hesaplar → deterministik, yarışta tutarlı).
//   - Reddedilmez: bütçe bitince havuz yine açılır (1 işçi; ondemand ile boştayken 0
//     süreç). En kötü durum N alt alan → N işçi (B < N iken) — kabul edilen, bilinçli
//     taban: alt alanı açılamaz kılmak mevcut davranışı bozardı.
//   - Yalnız render edilen havuz değişir; DB'deki kayıt aynen kalır.
//   - Sorgu hatasında tek-havuz tavanı (eski davranış) korunur (fail-open + log): hesap
//     tavanı bir güvenlik duvarı değil kaynak adaleti; panel DB'si bozukken alt alan
//     havuzu yazılamaz hale getirilmez.

import (
	"context"
	"database/sql"
	"log"
	"sort"

	"girginospanel/internal/provisioner"
)

// hesapHavuzDagit — sıralı istekleri (plan tavanları, id sırasıyla) butce içinde
// dağıtır: her biri min(istek, kalan), en az 1. Saf işlev (testle kilitli).
func hesapHavuzDagit(butce int, istek []int) []int {
	out := make([]int, len(istek))
	kalan := butce
	for i, w := range istek {
		a := w
		if a > kalan {
			a = kalan
		}
		if a < 1 {
			a = 1
		}
		out[i] = a
		kalan -= a
		if kalan < 0 {
			kalan = 0
		}
	}
	return out
}

// hesapAltHavuzTavani — subID havuzunun hesap bazlı pm.max_children tavanı (ve
// bütçe, istek: log için). ok=false → hesaplanamadı (tek-havuz tavanı geçerli).
func hesapAltHavuzTavani(ctx context.Context, db *sql.DB, sk string, subID, domainID int64) (tavan, butce, istek int, ok bool) {
	rows, err := db.QueryContext(ctx,
		`SELECT s.id, s.domain_id FROM subdomanlar s
		   JOIN domains d ON d.id = s.domain_id
		   JOIN subdomain_php_settings p ON p.subdomain_id = s.id
		  WHERE d.sistem_kullanici = ? ORDER BY s.id`, sk)
	if err != nil {
		log.Printf("php: alt alan havuz bütçesi okunamadı (sk=%s): %v", sk, err)
		return 0, 0, 0, false
	}
	defer rows.Close()
	type alt struct{ id, dom int64 }
	var liste []alt
	var bulundu bool
	for rows.Next() {
		var a alt
		if err := rows.Scan(&a.id, &a.dom); err != nil {
			log.Printf("php: alt alan havuz bütçesi okunamadı (sk=%s): %v", sk, err)
			return 0, 0, 0, false
		}
		liste = append(liste, a)
		bulundu = bulundu || a.id == subID
	}
	rows.Close() // sonraki plan sorguları bağlantı beklemesin
	if rows.Err() != nil {
		log.Printf("php: alt alan havuz bütçesi okunamadı (sk=%s): %v", sk, rows.Err())
		return 0, 0, 0, false
	}
	if !bulundu { // henüz satırı olmayan (ilk kayıt) havuz da bütçeye girer
		liste = append(liste, alt{subID, domainID})
		sort.Slice(liste, func(i, j int) bool { return liste[i].id < liste[j].id })
	}
	istekler := make([]int, len(liste))
	idx := 0
	for i, a := range liste {
		istekler[i] = provisioner.PlanPMMaxChildren(db, a.dom)
		if istekler[i] > butce {
			butce = istekler[i]
		}
		if a.id == subID {
			idx = i
		}
	}
	return hesapHavuzDagit(butce, istekler)[idx], butce, istekler[idx], true
}

// hesapTavaniUygula — havuzTavanla çıktısına hesap bazlı toplam tavanı uygular:
// pm.max_children kısılırsa start/min/max_spare yeniden türetilir.
func hesapTavaniUygula(ctx context.Context, db *sql.DB, sk string, subID, domainID int64, s Settings) Settings {
	tavan, butce, istek, ok := hesapAltHavuzTavani(ctx, db, sk, subID, domainID)
	if !ok || tavan >= s.PMMaxChildren {
		return s
	}
	log.Printf("php: alt alan havuzu hesap tavanına kısıldı (sk=%s sub=%d): pm.max_children %d → %d (hesap bütçesi %d, plan %d)",
		sk, subID, s.PMMaxChildren, tavan, butce, istek)
	s.PMMaxChildren = tavan
	s.PMStartServers, s.PMMinSpareServers, s.PMMaxSpareServers = provisioner.HavuzYedekleri(tavan)
	return s
}
