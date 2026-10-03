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
	"strings"
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
// Kapsam db_accounts'ta BU domaine kayıtlı kullanıcılardır; her kullanıcının
// MariaDB'deki TÜM hesapları (host'ları) işlenir. db_accounts.db_host'a
// GÜVENİLMEZ: yedekten dönüş arşivdeki host'u aynen kullanır ('%', '127.0.0.1'
// …; bkz. kaynaklimit mysqlUserHosts) — yalnız kayıtlı host'u kilitlemek öteki
// host'tan bağlantıyı açık bırakırdı. Sistem/servis hesabı (SistemDBHesabiMi)
// ya da başka bir domaine de kayıtlı kullanıcı (kullaniciYalnizBuDomainde)
// ASLA kilitlenmez/açılmaz: komşu kiracının ya da sunucu servisinin veritabanı
// erişimi askıdan etkilenmemeli. Kilitlerken böyle bir satır hata olarak
// raporlanır (sessiz atlama yok: o kullanıcı açık kaldı). Açarken yalnız
// günlüğe yazılır: onu hiç kilitlemedik, hata askıdan almayı yarım bırakıp hesabı
// yeniden askıya alırdı (hesap hiç açılamazdı).
//
// Kullanıcısı boş satırlar (geri yüklemenin "kullanıcı/parola tanımlayın" diye
// yeniden kaydettiği DB) atlanır: kilitlenecek hesap yoktur. MariaDB'de hiçbir
// host'ta bulunmayan kullanıcı da bağlanamaz → kilitlenecek bir şey yoktur.
func DBKullanicilariKilit(db *sql.DB, domainID int64, kilit bool) error {
	rows, err := db.Query(
		`SELECT DISTINCT db_user FROM db_accounts WHERE domain_id=? AND db_user<>''`, domainID)
	if err != nil {
		return fmt.Errorf("veritabanı kullanıcıları okunamadı: %w", err)
	}
	var liste []string
	for rows.Next() {
		var kul string
		if err := rows.Scan(&kul); err != nil {
			_ = rows.Close()
			return fmt.Errorf("veritabanı kullanıcıları okunamadı: %w", err)
		}
		liste = append(liste, kul)
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
	reddet := func(kul string, err error) {
		if kilit {
			hatalar = append(hatalar, fmt.Errorf("veritabanı kullanıcısı %q: %w", kul, err))
			return
		}
		log.Printf("askıdan alma: veritabanı kullanıcısı %q atlandı (askıda kilitlenmemişti): %v", kul, err)
	}
	for _, kul := range liste {
		if !GecerliDBKimlik(kul) || SistemDBHesabiMi(kul) {
			reddet(kul, errors.New("güvenlik: işlenemez ad"))
			continue
		}
		if err := kullaniciYalnizBuDomainde(db, domainID, kul); err != nil {
			reddet(kul, err)
			continue
		}
		hostlar, err := dbHesapHostlari(kul)
		if err != nil {
			hatalar = append(hatalar, fmt.Errorf("veritabanı kullanıcısı %q: %w", kul, err))
			continue
		}
		for _, host := range hostlar {
			if !reDBHost.MatchString(host) {
				hatalar = append(hatalar, fmt.Errorf("veritabanı kullanıcısı %q: güvenlik: işlenemez host", kul))
				continue
			}
			// Ad GecerliDBKimlik'ten geçti; host reDBHost'tan geçti ve sqlKac ile
			// kaçışlı (kacisModu) → SQLi yok. IF EXISTS YOK: hesap az önce
			// mysql.user'dan okundu; ALTER başarısızsa hata döner (kilitlenmeyen
			// hesap "kilitlendi" sayılmaz).
			hesapAdi := fmt.Sprintf("'%s'@'%s'", kul, sqlKac(host))
			if err := mysqlCalistir(kacisModu, fmt.Sprintf("ALTER USER %s ACCOUNT %s;", hesapAdi, eylem)); err != nil {
				hatalar = append(hatalar, fmt.Errorf("veritabanı kullanıcısı %q: %w", kul, err))
				continue
			}
			if kilit {
				// Kilit yalnız YENİ bağlantıyı keser; açık oturumu düşür. Oturum
				// yoksa hata; bu durumda kilit zaten uygulanmıştır → yalnız günlük.
				if err := mysqlCalistir(kacisModu, fmt.Sprintf("KILL USER %s;", hesapAdi)); err != nil {
					log.Printf("askı: veritabanı kullanıcısı %q oturumları düşürülemedi: %v", kul, err)
				}
			}
		}
	}
	return errors.Join(hatalar...)
}

// dbHesapHostlari — kul adının MariaDB'deki TÜM hesaplarının host'ları.
// Ad GecerliDBKimlik'ten geçmiş olmalı; sqlKac + kacisModu ile kaçışlı.
func dbHesapHostlari(kul string) ([]string, error) {
	out, err := mysqlKomut("-N", "-B", "-r", "-e", kacisModu+
		" SELECT Host FROM mysql.user WHERE User='"+sqlKac(kul)+"'").Output()
	if err != nil {
		return nil, fmt.Errorf("MariaDB hesapları okunamadı: %w", err)
	}
	var hostlar []string
	for _, s := range strings.Split(string(out), "\n") {
		if s = strings.TrimSpace(s); s != "" {
			hostlar = append(hostlar, s)
		}
	}
	return hostlar, nil
}
