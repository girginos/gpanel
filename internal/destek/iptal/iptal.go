// Package iptal — destek bağlantılarının OTOMATİK iptal tetikleri.
//
// Panelin başka paketleri (bayi parola sıfırlama/askı/silme, hosting silme/askı,
// 2FA kapatma) bu paketi çağırır; paket yalnız SQL yapar (ağır ajan paketini ve
// onun bağımlılıklarını içe aktarmaz → döngü yok). İptal edilen kayıtlar Kanca'ya
// verilir: ajan (internal/destek/ajan) kancayı kurar, denetime yazar ve merkeze
// baglanti_iptal bildirir. Kanca kurulmamışsa (modül hiç başlamadı, test) yalnız
// DB güncellenir; ajan açılışta son iptalleri merkeze yeniden bildirir.
//
// Asıl işlemi BOZMAZ: hata yalnız günlüğe yazılır. Tablo yoksa (göç 0129 henüz
// koşmadı) sessizce geçer. Ajan ayrıca her kullanımda ve periyodik süpürgede
// kimlik izini (FTP parolası, bayi parolası, 2FA, askı) yeniden doğrular; bu
// paket iptali ANINDA yapar, iz denetimi kaçan yolları yakalar (savunma derinliği).
package iptal

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"strings"

	"girginospanel/internal/destek/protokol"

	"github.com/go-sql-driver/mysql"
)

// Kayit: iptal edilen bağlantı.
type Kayit struct {
	ID             int64
	Kimlik         string
	Rol            string
	ResellerID     int64
	DomainID       int64
	TgUserID       int64
	TgKullaniciAdi string
	Neden          protokol.IptalNedeni
}

// Kanca: iptal sonrası bildirim (ajan kurar). nil → yalnız DB.
var Kanca func(ctx context.Context, aktorUID int64, aktorAd string, kayitlar []Kayit)

// Domain: domainin müşteri bağlantılarını iptal eder (hosting silme/askı → alan_silme;
// FTP parolası değişimi → ftp_parola).
func Domain(ctx context.Context, db *sql.DB, domainID int64, neden protokol.IptalNedeni, aktorUID int64, aktorAd string) int {
	if domainID <= 0 {
		return 0
	}
	return uygula(ctx, db, `rol='musteri' AND domain_id=?`, []any{domainID}, neden, aktorUID, aktorAd)
}

// Bayi: bayinin KENDİ bağlantılarını; musterilerDahil ise bayinin müşterilerinin
// bağlantılarını da iptal eder (askı/silme: evet; parola sıfırlama: yalnız bayi).
func Bayi(ctx context.Context, db *sql.DB, bayiID int64, musterilerDahil bool, aktorUID int64, aktorAd string) int {
	if bayiID <= 0 {
		return 0
	}
	if musterilerDahil {
		return uygula(ctx, db, `reseller_id=? AND rol IN ('bayi','musteri')`, []any{bayiID}, protokol.IptalBayi, aktorUID, aktorAd)
	}
	return uygula(ctx, db, `rol='bayi' AND panel_user_id=?`, []any{bayiID}, protokol.IptalBayi, aktorUID, aktorAd)
}

// Kullanici: kök/bayi kullanıcısının KENDİ bağlantıları (2FA kapatma → iki_fa).
func Kullanici(ctx context.Context, db *sql.DB, userID int64, neden protokol.IptalNedeni, aktorUID int64, aktorAd string) int {
	if userID <= 0 {
		return 0
	}
	return uygula(ctx, db, `rol IN ('kok','bayi') AND panel_user_id=?`, []any{userID}, neden, aktorUID, aktorAd)
}

// TabloYok: göç 0129 koşmamış (MySQL 1146).
func TabloYok(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1146
}

func uygula(ctx context.Context, db *sql.DB, kosul string, args []any, neden protokol.IptalNedeni, aktorUID int64, aktorAd string) int {
	if db == nil {
		return 0
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// Panel isteği iptal edilse de (istemci koptu) iptal tamamlanmalı.
	ctx = context.WithoutCancel(ctx)
	rows, err := db.QueryContext(ctx, `SELECT id, kimlik, rol, reseller_id, COALESCE(domain_id,0), tg_user_id, tg_kullanici_adi
		FROM destek_baglantilari WHERE durum<>'iptal' AND `+kosul, args...) //nolint:gosec // G202: kosul bu paketteki SABİT parçalar; değerler ? ile bağlanır.
	if err != nil {
		if !TabloYok(err) {
			log.Printf("destek iptal (%s): bağlantılar okunamadı: %v", neden, err)
		}
		return 0
	}
	var adaylar []Kayit
	for rows.Next() {
		var k Kayit
		if err := rows.Scan(&k.ID, &k.Kimlik, &k.Rol, &k.ResellerID, &k.DomainID, &k.TgUserID, &k.TgKullaniciAdi); err != nil {
			log.Printf("destek iptal: satır okunamadı: %v", err)
			continue
		}
		k.Neden = neden
		adaylar = append(adaylar, k)
	}
	_ = rows.Close()
	var iptal []Kayit
	for _, k := range adaylar {
		if Tek(ctx, db, k.ID, neden, aktorAd) {
			iptal = append(iptal, k)
		}
	}
	if len(iptal) > 0 && Kanca != nil {
		Kanca(ctx, aktorUID, aktorAd, iptal)
	}
	return len(iptal)
}

// Tek: tek bağlantıyı iptal eder; zaten iptalse false.
func Tek(ctx context.Context, db *sql.DB, id int64, neden protokol.IptalNedeni, eden string) bool {
	res, err := db.ExecContext(ctx, `UPDATE destek_baglantilari SET durum='iptal', iptal_nedeni=?, iptal_eden=?,
		iptal_at=CURRENT_TIMESTAMP WHERE id=? AND durum<>'iptal'`, string(neden), kes(eden, 64), id)
	if err != nil {
		if !TabloYok(err) {
			log.Printf("destek iptal: bağlantı %d iptal edilemedi: %v", id, err)
		}
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}

func kes(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
