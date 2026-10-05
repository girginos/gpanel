package uzak

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"

	"girginospanel/internal/backups/hedef"
)

// Aday — saklama politikasının sileceği (ya da sildiği) uzak kopya.
type Aday struct {
	ID         int64  `json:"id"`
	DomainID   int64  `json:"domain_id"`
	YedekDosya string `json:"yedek_dosya"`
	UzakAd     string `json:"uzak_ad"`
	Boyut      int64  `json:"boyut_b"`
	Neden      string `json:"neden"`
	// Sonuc — yalnız gerçek koşuda: "silindi" | "atlandi: <sebep>" | "hata: <sebep>".
	Sonuc string `json:"sonuc,omitempty"`

	manifestAd string
}

const silinmisKayitGun = 90

// SaklamaAdaylari — politikaya göre silinecek kopyalar (YEREL, uzağa dokunmaz):
// domain başına en yeni saklama_n kopya kalır; saklama_gun>0 ise ondan eski kopyalar
// da gider ama her domainin EN YENİ kopyası her durumda korunur. Yalnız bu
// orkestratörün kaydı olan ('tamam', ya da yarım kalmış 'siliniyor') kopyalar
// aday olabilir: klasördeki yabancı dosyalar kayıtta olmadığı için asla aday değildir.
func SaklamaAdaylari(ctx context.Context, db *sql.DB, a Ayar) ([]Aday, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, domain_id, yedek_dosya, uzak_ad, manifest_ad, boyut_b, durum,
		        UNIX_TIMESTAMP(NOW()) - UNIX_TIMESTAMP(created_at)
		 FROM backup_uzak_kopyalar
		 WHERE kaynak='hedef' AND hedef_id=? AND durum IN ('tamam','siliniyor')
		 ORDER BY domain_id, id DESC`, a.HedefID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Aday
	var sonDomain int64 = -1
	sira := 0
	for rows.Next() {
		var c Aday
		var durum string
		var yas int64
		if err := rows.Scan(&c.ID, &c.DomainID, &c.YedekDosya, &c.UzakAd, &c.manifestAd, &c.Boyut, &durum, &yas); err != nil {
			return nil, err
		}
		if c.DomainID != sonDomain {
			sonDomain, sira = c.DomainID, 0
		}
		if durum == DurumSiliniyor {
			c.Neden = "yarım kalan silme tamamlanıyor"
			out = append(out, c)
			continue
		}
		sira++
		switch {
		case sira > a.SaklamaN:
			c.Neden = fmt.Sprintf("son %d kopya sınırı aşıldı", a.SaklamaN)
			out = append(out, c)
		case a.SaklamaGun > 0 && sira > 1 && yas > int64(a.SaklamaGun)*86400:
			c.Neden = fmt.Sprintf("%d gün sınırı aşıldı", a.SaklamaGun)
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

// silinebilirAd — kaydın adları gerçekten BİZİM biçimimizde mi? (kayıt bozulsa da
// başkasının dosyasına dokunmayız.)
func silinebilirAd(c *Aday) bool {
	return c.UzakAd != "" && hedef.AdDogrula(c.UzakAd) == nil && !hedef.GeciciMi(c.UzakAd) &&
		strings.HasSuffix(c.UzakAd, ".age") && c.manifestAd == ManifestAdi(c.UzakAd)
}

// SaklamaUygula — politikayı uygular (dryRun=true: yalnız aday listesi, uzağa
// dokunulmaz). Silme YALNIZ kendi kaydımız olan kopyalar için ve YALNIZ uzakta
// manifesti görünüyorsa yapılır; önce veri, en son manifest silinir (yarım kalırsa
// 'siliniyor' durumu sonraki koşuda tamamlanır). Tek tek hata tüm koşuyu durdurmaz.
func SaklamaUygula(ctx context.Context, db *sql.DB, h hedef.Hedef, a Ayar, dryRun bool) ([]Aday, error) {
	adaylar, err := SaklamaAdaylari(ctx, db, a)
	if err != nil || dryRun {
		return adaylar, err
	}
	for k := range adaylar {
		c := &adaylar[k]
		if ctx.Err() != nil {
			return adaylar, ctx.Err()
		}
		if !silinebilirAd(c) {
			c.Sonuc = "atlandi: kayıt adı beklenen biçimde değil"
			continue
		}
		var ilkDurum string
		if err := db.QueryRowContext(ctx, `SELECT durum FROM backup_uzak_kopyalar WHERE id=?`, c.ID).Scan(&ilkDurum); err != nil {
			c.Sonuc = "hata: kayıt okunamadı"
			continue
		}
		nesneler, err := h.Listele(ctx, c.manifestAd)
		if err != nil {
			c.Sonuc = "hata: manifest sorgulanamadı (" + string(hedef.KategoriAl(err)) + ")"
			continue
		}
		var manifestVar bool
		for _, n := range nesneler {
			if n.Ad == c.manifestAd {
				manifestVar = true
			}
		}
		if !manifestVar && ilkDurum == DurumTamam {
			// Manifestsiz kopya bizim doğrulanmış kopyamız değil: DOKUNMA.
			c.Sonuc = "atlandi: uzakta manifest yok, silinmedi"
			continue
		}
		if _, err := db.ExecContext(ctx, `UPDATE backup_uzak_kopyalar SET durum='siliniyor' WHERE id=? AND durum IN ('tamam','siliniyor')`, c.ID); err != nil {
			c.Sonuc = "hata: durum yazılamadı"
			continue
		}
		if err := h.Sil(ctx, c.UzakAd); err != nil {
			c.Sonuc = "hata: silinemedi (" + string(hedef.KategoriAl(err)) + ")"
			continue
		}
		if err := h.Sil(ctx, c.manifestAd); err != nil {
			c.Sonuc = "hata: manifest silinemedi (" + string(hedef.KategoriAl(err)) + ")"
			continue
		}
		if _, err := db.ExecContext(ctx, `UPDATE backup_uzak_kopyalar SET durum='silindi', bitis_at=NOW() WHERE id=?`, c.ID); err != nil {
			c.Sonuc = "hata: durum yazılamadı"
			continue
		}
		c.Sonuc = "silindi"
	}
	if _, err := db.ExecContext(ctx,
		`DELETE FROM backup_uzak_kopyalar WHERE kaynak='hedef' AND durum='silindi' AND bitis_at < NOW() - INTERVAL ? DAY`, silinmisKayitGun); err != nil {
		log.Printf("uzak kopya: eski silinmiş kayıtlar temizlenemedi: %v", err)
	}
	return adaylar, nil
}
