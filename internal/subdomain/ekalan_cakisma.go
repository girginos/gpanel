package subdomain

// ekalan_cakisma.go — ek alan adı için HESAP düzeyi ad-alanı denetimi.
//
// domains.AdAlaniDenetle (AdAlaniEkle sarmalayıcısı) kiracıyı BAYİ (reseller) düzeyinde
// tanır: aynı bayinin farklı müşterileri "aynı kiracı" sayılır. Ek alan adı için bu
// YETMEZ: bir müşteri, aynı bayinin başka müşterisinin alan adının alt adını
// (mal.kurban.com) kendi hesabına ek alan adı olarak açabilirse o adın DNS bölgesini
// (BIND'de en özgül bölge kazanır), HTTP trafiğini ve sertifikasını ele geçirir.
// Bu nedenle burada sahip = HESAP (domains.id): ad, BAŞKA bir hesaba ait bir alan adı /
// ek alan adının kendisi, alt adı ya da üst adı ise RED. Kendi hesabının ana alan
// adının alt adı da reddedilir (Alt alan adı özelliği vardır; iç içe iki bölge
// belirsizlik üretir).

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

type ekSorgucu interface {
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
}

// ekCakismaHatasi — 409. Genel metin başka hesabın adını/sahibini İÇERMEZ.
type ekCakismaHatasi struct{ Genel, Ayrinti string }

func (e *ekCakismaHatasi) Error() string { return e.Genel }
func (e *ekCakismaHatasi) Mesaj(yonetici bool) string {
	if yonetici && e.Ayrinti != "" {
		return e.Genel + " (" + e.Ayrinti + ")"
	}
	return e.Genel
}

const (
	ekMsjKayitli = "bu alan adı sunucuda zaten kayıtlı"
	ekMsjCapraz  = "bu alan adı, sunucuda başka bir hesaba ait bir alan adıyla aynı ad alanında (alt ya da üst alan adı); sunucu yöneticinize başvurun"
	ekMsjKendi   = "bu ad, hesabınızdaki bir alan adının alt ya da üst adı; alt alan adı eklemek için \"Alt alan adı ekle\"yi kullanın"
)

// ekUstAdlar: "a.b.c.com" → ["b.c.com", "c.com", "com"].
func ekUstAdlar(ad string) []string {
	var out []string
	for {
		i := strings.IndexByte(ad, '.')
		if i < 0 || i == len(ad)-1 {
			return out
		}
		ad = ad[i+1:]
		out = append(out, ad)
	}
}

// ekHesapAdAlaniDenetle — did hesabının ek alan adı olarak `ad`'ı açması ad-alanı
// bakımından güvenli mi? Sorgu hatası düz hata (çağıran reddeder: fail-closed).
func ekHesapAdAlaniDenetle(ctx context.Context, q ekSorgucu, did int64, ad string) error {
	adaylar := append([]string{ad}, ekUstAdlar(ad)...)
	yer := strings.TrimSuffix(strings.Repeat("?,", len(adaylar)), ",")
	adArgs := make([]any, len(adaylar))
	for i, a := range adaylar {
		adArgs[i] = a
	}
	oku := func(sorgu string, args ...any) (map[string]int64, error) {
		rows, err := q.QueryContext(ctx, sorgu, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := map[string]int64{}
		for rows.Next() {
			var a string
			var id int64
			if err := rows.Scan(&a, &id); err != nil {
				return nil, err
			}
			out[strings.ToLower(a)] = id
		}
		return out, rows.Err()
	}
	// (tablo, sahip hesap sütunu): domains.id ve ek_alan_adlari.domain_id.
	tablolar := []struct{ ust, alt string }{
		{`SELECT alan_adi, id FROM domains WHERE alan_adi IN (` + yer + `)`,
			`SELECT alan_adi, id FROM domains WHERE CHAR_LENGTH(alan_adi) > ? AND RIGHT(alan_adi, ?) = ?`},
		{`SELECT alan_adi, domain_id FROM ek_alan_adlari WHERE alan_adi IN (` + yer + `)`,
			`SELECT alan_adi, domain_id FROM ek_alan_adlari WHERE CHAR_LENGTH(alan_adi) > ? AND RIGHT(alan_adi, ?) = ?`},
	}
	for _, t := range tablolar {
		ustler, err := oku(t.ust, adArgs...)
		if err != nil {
			return err
		}
		altlar, err := oku(t.alt, len(ad)+1, len(ad)+1, "."+ad)
		if err != nil {
			return err
		}
		for k, sahip := range ustler {
			if k == ad {
				return &ekCakismaHatasi{ekMsjKayitli, fmt.Sprintf("hesap #%d", sahip)}
			}
		}
		// Önce BAŞKA hesap (çapraz; deterministik), sonra kendi hesabı.
		for _, k := range sirali(ustler) {
			if ustler[k] != did {
				return &ekCakismaHatasi{ekMsjCapraz, fmt.Sprintf("%s alan adının alt adı; hesap #%d", k, ustler[k])}
			}
		}
		for _, k := range sirali(altlar) {
			if altlar[k] != did {
				return &ekCakismaHatasi{ekMsjCapraz, fmt.Sprintf("%s alan adının üst adı; hesap #%d", k, altlar[k])}
			}
		}
		for _, k := range sirali(ustler) {
			return &ekCakismaHatasi{ekMsjKendi, k + " alan adının alt adı"}
		}
		for _, k := range sirali(altlar) {
			return &ekCakismaHatasi{ekMsjKendi, k + " alan adının üst adı"}
		}
	}
	// Alt alan adı olarak kullanımda mı (subdomanlar.tam_ad)?
	var n int
	rows, err := q.QueryContext(ctx, `SELECT COUNT(*) FROM subdomanlar WHERE tam_ad=?`, ad)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		_ = rows.Scan(&n)
	}
	if n > 0 {
		return &ekCakismaHatasi{Genel: "bu ad sunucuda alt alan adı olarak kullanımda"}
	}
	return rows.Err()
}

// sirali — harita anahtarları (deterministik hata metni için).
func sirali(m map[string]int64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
