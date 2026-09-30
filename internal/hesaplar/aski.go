package hesaplar

// Hosting HESABI askı durumu — TEK kaynak — ve hesabın MariaDB kullanıcı kilidi.
//
// 🔴 Askı hesap düzeyindedir (kullanıcı kararı 2026-09-30): hesabın 1 alan adı
// da olsa 10 da olsa hesap askıya alınınca ona bağlı HER ŞEY kapanır. Bu yüzden
// hiçbir render/servis yolu kendi askı sorgusunu yazmaz; HesapAskida'yı çağırır.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
)

// ErrHesapAskida: hesap askıdayken hesabı yeniden çalışır hâle getirebilecek bir
// işlem (PHP havuzunu başlatma, PHP sürümü/ayarı uygulama …) istendi. Çağıran
// bunu errors.Is ile tanıyıp "hesap askıda" diye yanıtlar; hiçbir yan etki yoktur.
var ErrHesapAskida = errors.New("hesap askıda")

// HesapAskida — hosting hesabı (sistem kullanıcısı, c_…) askıda mı?
//
// Hesap = sistem kullanıcısı. Bugün hesabın alan adları, domains satırı (ana alan
// adı) ve ona bağlı subdomanlar satırlarıdır; hepsi AYNI sistem kullanıcısıyla
// render edilir. İleride eklenecek ek (addon) alan adları da hesaba (sistem
// kullanıcısına) bağlanacağı için bu fonksiyon onları DEĞİŞİKLİKSİZ kapsar: ek
// alan adının render yolu yalnız `HesapAskida(ctx, db, sk)` çağırır (tek satır).
// Hesaba bağlı domains satırlarından biri askıdaysa hesap askıdadır.
//
// Satır yoksa (hesap yok ya da askıda değil) false. Sorgu hatası çağırana döner:
// render yolları bu durumda yapılandırmaya DOKUNMAZ (ne açar ne kapatır).
func HesapAskida(ctx context.Context, db *sql.DB, sk string) (bool, error) {
	if db == nil || sk == "" {
		return false, nil
	}
	var bir int
	err := db.QueryRowContext(ctx,
		`SELECT 1 FROM domains WHERE sistem_kullanici=? AND COALESCE(askida,0)=1 LIMIT 1`, sk).Scan(&bir)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("hesap askı durumu okunamadı: %w", err)
	}
	return true, nil
}

// DBKullanicilariKilit — hesabın (domainID) MariaDB kullanıcılarını askıda
// kilitler (ALTER USER … ACCOUNT LOCK; MariaDB ≥ 10.4.2) ve açık oturumlarını
// düşürür; kilit=false iken kilidi açar. İdempotenttir.
//
// Kapsam yalnız db_accounts'ta BU domaine kayıtlı kullanıcılardır. Sistem/servis
// hesabı (SistemDBHesabiMi) ya da başka bir domaine de kayıtlı kullanıcı
// (kullaniciYalnizBuDomainde) ASLA kilitlenmez/açılmaz: komşu kiracının ya da
// sunucu servisinin veritabanı erişimi askıdan etkilenmemeli. Böyle bir satır
// hata olarak raporlanır (sessiz atlama yok).
func DBKullanicilariKilit(db *sql.DB, domainID int64, kilit bool) error {
	rows, err := db.Query(
		`SELECT DISTINCT db_user, COALESCE(NULLIF(db_host,''),'localhost') FROM db_accounts WHERE domain_id=?`, domainID)
	if err != nil {
		return fmt.Errorf("veritabanı kullanıcıları okunamadı: %w", err)
	}
	type hesap struct{ kul, host string }
	var liste []hesap
	for rows.Next() {
		var h hesap
		if err := rows.Scan(&h.kul, &h.host); err != nil {
			_ = rows.Close()
			return fmt.Errorf("veritabanı kullanıcıları okunamadı: %w", err)
		}
		liste = append(liste, h)
	}
	okumaHatasi := rows.Err()
	_ = rows.Close()
	if okumaHatasi != nil {
		// Yarım liste "hepsi kilitlendi" yanılgısı üretir.
		return fmt.Errorf("veritabanı kullanıcıları eksik okundu: %w", okumaHatasi)
	}

	eylem := "UNLOCK"
	if kilit {
		eylem = "LOCK"
	}
	var hatalar []error
	for _, h := range liste {
		if !GecerliDBKimlik(h.kul) || !reDBHost.MatchString(h.host) || SistemDBHesabiMi(h.kul) {
			hatalar = append(hatalar, fmt.Errorf("veritabanı kullanıcısı %q: güvenlik: işlenemez ad", h.kul))
			continue
		}
		if err := kullaniciYalnizBuDomainde(db, domainID, h.kul); err != nil {
			hatalar = append(hatalar, fmt.Errorf("veritabanı kullanıcısı %q: %w", h.kul, err))
			continue
		}
		// Ad GecerliDBKimlik'ten, host reDBHost'tan geçti: tırnak/ters bölü yok → SQLi yok.
		hesapAdi := fmt.Sprintf("'%s'@'%s'", h.kul, h.host)
		if err := mysqlCalistir(fmt.Sprintf("ALTER USER IF EXISTS %s ACCOUNT %s;", hesapAdi, eylem)); err != nil {
			hatalar = append(hatalar, fmt.Errorf("veritabanı kullanıcısı %q: %w", h.kul, err))
			continue
		}
		if kilit {
			// Kilit yalnız YENİ bağlantıyı keser; açık oturumu düşür. Oturum
			// yoksa hata; bu durumda kilit zaten uygulanmıştır → yalnız günlük.
			if err := mysqlCalistir(fmt.Sprintf("KILL USER %s;", hesapAdi)); err != nil {
				log.Printf("askı: veritabanı kullanıcısı %q oturumları düşürülemedi: %v", h.kul, err)
			}
		}
	}
	return errors.Join(hatalar...)
}
