package hesapaski

// bildirim.go — hesap askısı/açılışı bildirimi (D3b). Tek nokta: Adimlar.Uygula
// (tekil uç, toplu işlem ve bayi zinciri hepsi buradan geçer). Bildirim YALNIZ
// DB'deki askı durumu gerçekten değiştiyse yazılır (hesap kilidi altında önce/sonra
// okunur): reddedilen (kök askısı, demo), geri alınan (vhost hatası) ya da
// idempotent tekrar işlem bildirim üretmez; kısmi hata durum değiştirdiyse üretir.
//
// Alıcılar bildirim.YazOlay domain hedefi: domain sahibi (bayi ya da yönetici) +
// müşterisi. Olayı tetikleyen rolün kendi kopyası okunmuş yazılır (kendi eylemi
// için rozet yok): kök → yönetici, bayi elle → bayi, bayi zinciri → bayi (bayinin
// kendisi askıda; açıldığında N hesabı için N okunmamış bildirim görmesin).

import (
	"context"
	"database/sql"
	"time"

	"girginospanel/internal/bildirim"
)

// askidaOku — domainin güncel askı durumu (-1: okunamadı → bildirim yok).
func askidaOku(ctx context.Context, db *sql.DB, id int64) int {
	var a int
	if db.QueryRowContext(ctx, `SELECT COALESCE(askida,0) FROM domains WHERE id=?`, id).Scan(&a) != nil {
		return -1
	}
	return a
}

// askiBildirimi — önceki durumdan farklıysa bildirim yazar.
func askiBildirimi(ctx context.Context, db *sql.DB, id int64, alanAdi string, onceki int, kaynak string) {
	if onceki < 0 || alanAdi == "" {
		return
	}
	simdi := askidaOku(ctx, db, id)
	if simdi < 0 || simdi == onceki {
		return
	}
	aktor, kim := bildirim.AktorBayi, "bayi tarafından"
	switch kaynak {
	case KaynakKok:
		aktor, kim = bildirim.AktorYonetici, "yönetici tarafından"
	case KaynakBayi:
		kim = "bayi hesabının durumu nedeniyle"
	}
	// ref_id = olay anı (ns): her askı/açılış AYRI olaydır. Aynı ref'le, müşterinin hâlâ
	// okumadığı eski "askıya alındı" yeni askıyı tekilleştirip yutardı (askı → açılış
	// → askı dizisinde son durum hiç bildirilmezdi).
	an := time.Now().UnixNano()
	o := bildirim.Olay{Seviye: "uyari", Kategori: "aski", Baslik: "Hesap askıya alındı",
		Mesaj: alanAdi + " hosting hesabı " + kim + " askıya alındı.", RefTur: "aski", RefID: an}
	if simdi == 0 {
		o = bildirim.Olay{Seviye: "bilgi", Kategori: "aski", Baslik: "Hesap yeniden etkinleştirildi",
			Mesaj: alanAdi + " hosting hesabının askısı " + kim + " kaldırıldı.", RefTur: "aski", RefID: an}
	}
	bildirim.YazOlay(db, o, id, aktor, 0)
}
