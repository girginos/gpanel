package ajan

// unutma.go — KVKK silme/anonimleştirme (Telegram /unutbeni ve merkez/kullanıcı iptali).
//
// 🔴 NEDEN: /unutbeni "bağlantılarınız ve kayıtlarınız silinir" der. Hub tarafı gerçekten
// siler (merkez/depo.go KullaniciUnut); sunucudaki izler (destek_baglantilari.tg_user_id /
// tg_kullanici_adi ve audit_log aktör/hedef dizgelerindeki "tg:<kısa>") ise yalnız
// 'iptal' damgası alıyordu. Bu dosya o izleri ANONİMLEŞTİRİR.
//
// Anonimleştirme, silme DEĞİLDİR — bilinçli ödünleşim: güvenlik denetim izi "NE yapıldı /
// hangi bağlantı iptal edildi / hangi araç çalıştı" bilgisini korur, "KİM (hangi Telegram
// hesabı) yaptı" bilgisini yitirir. Denetim satırının kendisi (zaman, eylem, ok, kapsam)
// kalır; yalnız Telegram kimliği "tg:silindi" olur. Bağlantı satırında tg_user_id=0 ve
// tg_kullanici_adi='' olur (şema: tg_user_id BIGINT NOT NULL, tekillik YOK → 0 geçerli,
// yeni göç gerekmez).
//
// Çevrimdışı ajan: merkez /unutbeni anında hub kaydını siler; ajan çevrimdışıysa ya da iptal
// iletilemezse, ajan bir sonraki bağlanışta bağlantıyı bildirir, merkez kaydı olmadığı için
// BaglantiIptal{neden=merkez} döner (merkez/ajan.go baglantiDurumIsle) ve bu dosya o iptalde
// de çalışır → silme "bir sonraki bağlanışta" TAMAMLANIR.
//
// Bilinen sınır: hub, zaten 'iptal' olmuş (hub'ın artık tanımadığı) bağlantı kimliklerini
// bilemez; aynı kullanıcının başka bir bağlantısı için unutbeni gelirse o satırlar da
// anonimleşir, aksi halde 30 gün sonra süpürge (anonimEskiIptaller) anonimleştirir.

import (
	"context"
	"database/sql"
	"log"

	p "girginospanel/internal/destek/protokol"
)

const (
	// AnonimDeger — denetim dizgelerinde Telegram kimliğinin yerine yazılan değer.
	AnonimDeger = "silindi"
	// anonimSaklamaGun — 'iptal' bağlantı satırındaki tg kimliğinin tutulma süresi (gün).
	anonimSaklamaGun = 30
)

// anonimlestirNeden — hangi iptal nedenleri kullanıcı/merkez isteğidir (Telegram kimliği silinir).
// Otomatik nedenler (kimlik izi, süre dolumu, hosting/bayi olayı) kimliği HEMEN silmez:
// panelde sahip "kim bağlanmıştı" görebilmeli; 30 gün sonra süpürge siler.
func anonimlestirNeden(n p.IptalNedeni) bool {
	switch n {
	case p.IptalUnutBeni, p.IptalMerkez, p.IptalKullanici:
		return true
	}
	return false
}

// tgEtiketleri — bir (kullanıcı adı, id) için audit_log'da görünebilen "tg:<kısa>" kısa
// biçimleri: botAktor/hedefTg ("@ad" ya da "…son6"), araç yolu (aracTgKisa: ad ya da son6).
func tgEtiketleri(ad string, id int64) []string {
	var out []string
	ekle := func(s string) {
		if s == "" {
			return
		}
		for _, x := range out {
			if x == s {
				return
			}
		}
		out = append(out, s)
	}
	if ad != "" {
		ekle(tgKisa(ad, id))
		ekle(aracTgKisa(ad, id))
	}
	if id != 0 {
		ekle(tgKisa("", id))
		ekle(aracTgKisa("", id))
	}
	return out
}

type tgKimlik struct {
	ad string
	id int64
}

// anonimlestir — b bağlantısının sahibi Telegram kullanıcısının sunucudaki izlerini siler:
// aynı kullanıcının TÜM 'iptal' satırları + b + audit_log aktör/hedef dizgeleri. Tek işlem.
func (a *Ajan) anonimlestir(ctx context.Context, b *baglanti) error {
	return anonimlestirDB(ctx, a.DB, b)
}

func anonimlestirDB(ctx context.Context, db *sql.DB, b *baglanti) error {
	if b == nil || db == nil {
		return nil
	}
	ctx = context.WithoutCancel(ctx)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	kimlikler := map[tgKimlik]bool{{b.TgKullaniciAdi, b.TgUserID}: true}
	satirlar := []int64{b.ID}
	if b.TgUserID != 0 {
		rows, err := tx.QueryContext(ctx, `SELECT id, tg_user_id, tg_kullanici_adi FROM destek_baglantilari WHERE tg_user_id=? AND durum='iptal'`, b.TgUserID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, tg int64
			var ad string
			if err := rows.Scan(&id, &tg, &ad); err != nil {
				_ = rows.Close()
				return err
			}
			satirlar = append(satirlar, id)
			kimlikler[tgKimlik{ad, tg}] = true
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()
	}
	for _, id := range satirlar {
		if _, err := tx.ExecContext(ctx, `UPDATE destek_baglantilari SET tg_user_id=0, tg_kullanici_adi='' WHERE id=?`, id); err != nil {
			return err
		}
	}
	for k := range kimlikler {
		for _, l := range tgEtiketleri(k.ad, k.id) {
			if err := auditAnonimle(ctx, tx, l); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// auditAnonimle — audit_log.actor_username "…(tg:<l>)…" ve target "tg:<l>" / "…(tg:<l>)"
// biçimlerinde <l> yerine AnonimDeger yazar. Yalnız TAM kısa-kimlik eşleşir ("(tg:" + l + ")"
// ve hedefte tam "tg:" + l): "@abcde" "@abcdef"in önekiyse ikincisine dokunulmaz.
func auditAnonimle(ctx context.Context, tx *sql.Tx, l string) error {
	eski, yeni := "(tg:"+l+")", "(tg:"+AnonimDeger+")"
	if _, err := tx.ExecContext(ctx, `UPDATE audit_log SET actor_username = REPLACE(actor_username, ?, ?) WHERE INSTR(actor_username, ?) > 0`, eski, yeni, eski); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE audit_log SET target = REPLACE(target, ?, ?) WHERE INSTR(target, ?) > 0`, eski, yeni, eski); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE audit_log SET target = ? WHERE target = ?`, "tg:"+AnonimDeger, "tg:"+l)
	return err
}

// anonimEskiIptaller — süpürge: 'iptal' olalı anonimSaklamaGun günü geçen satırların Telegram
// kimliğini (yalnız bağlantı satırı; audit_log'a DOKUNMAZ — denetim izi, ürün sahibi kararı
// olmadan sessizce değişmez) siler. Süre ve kapsam: dosya başlığı + KURULUM.md.
func (a *Ajan) anonimEskiIptaller(ctx context.Context) {
	_, err := a.DB.ExecContext(ctx, `UPDATE destek_baglantilari SET tg_user_id=0, tg_kullanici_adi=''
		WHERE durum='iptal' AND (tg_user_id<>0 OR tg_kullanici_adi<>'') AND iptal_at IS NOT NULL
		AND iptal_at < NOW() - INTERVAL ? DAY LIMIT 1000`, anonimSaklamaGun)
	if err != nil {
		log.Printf("destek süpürge: eski iptal anonimleştirme: %v", err)
	}
}
