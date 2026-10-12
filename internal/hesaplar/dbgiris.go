package hesaplar

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"girginospanel/internal/gizli"

	"github.com/go-sql-driver/mysql"
)

// ErrDBParolaUyusmuyor: MariaDB, verilen kullanıcı/parola ile girişi KESİN olarak
// reddetti (ER_ACCESS_DENIED_ERROR 1045). Ölçülemeyen durumlardan (soket yok, zaman
// aşımı, max_user_connections…) ayrı tutulur: çağıran yalnız bu hatada "parola yanlış"
// diyebilir, diğerlerinde karar veremez.
var ErrDBParolaUyusmuyor = errors.New("veritabanı parolası MariaDB'deki parolayla uyuşmuyor")

// erAccessDenied: MariaDB/MySQL "Access denied for user …" hata numarası.
const erAccessDenied = 1045

// dbGirisSoketi: kiracı DB kullanıcılarının bağlandığı MariaDB unix soketi. phpMyAdmin
// (PMA_single_signon_host='localhost') ve PHP uygulamaları da 'localhost' = SOKET ile
// bağlanır; hesaplar '<kul>'@'localhost' olarak tanımlıdır (127.0.0.1/TCP eşleşmez).
// Testte geçici sunucunun soketine çevrilir.
var dbGirisSoketi = func() string {
	for _, p := range []string{"/var/lib/mysql/mysql.sock", "/run/mysqld/mysqld.sock", "/var/run/mysqld/mysqld.sock", "/tmp/mysql.sock"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// DBGirisDene: kul/parola ile MariaDB'ye GERÇEKTEN giriş dener — phpMyAdmin SSO'nun
// yapacağıyla aynı yol (unix soket, '<kul>'@'localhost'). Veritabanı SEÇİLMEZ: yalnız
// kimlik ölçülür (DB yetkisi ayrı konu).
//
//	nil                   → giriş başarılı
//	ErrDBParolaUyusmuyor  → MariaDB kesin reddetti (1045)
//	diğer hata            → ölçülemedi (karar verilemez)
//
// 🔴 NEDEN (2026-10-12, bir müşteri sunucusu): panelin db_accounts kaydındaki parola,
// hesabın MariaDB parolası panel dışından değiştirildiği için bayatlamıştı. Panel bunu
// hiç ölçmüyordu; phpMyAdmin girişi "Access denied" ile düşüyor, signon betiği de
// kullanıcıya yalnızca "Token eksik" gösteriyordu.
func DBGirisDene(ctx context.Context, kul, parola string) error {
	sk := dbGirisSoketi()
	if sk == "" {
		return errors.New("MariaDB soketi bulunamadı")
	}
	c := mysql.NewConfig()
	c.User, c.Passwd = kul, parola
	c.Net, c.Addr = "unix", sk
	c.Timeout, c.ReadTimeout, c.WriteTimeout = 3*time.Second, 3*time.Second, 3*time.Second
	baglayici, err := mysql.NewConnector(c)
	if err != nil {
		return err
	}
	db := sql.OpenDB(baglayici)
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, iptal := context.WithTimeout(ctx, 5*time.Second)
	defer iptal()
	err = db.PingContext(ctx)
	var me *mysql.MySQLError
	if errors.As(err, &me) && me.Number == erAccessDenied {
		return ErrDBParolaUyusmuyor
	}
	return err
}

// dbGirisDene: DBGirisDene dikişi (birim testleri canlı MariaDB'ye gitmeden sahteler).
var dbGirisDene = DBGirisDene

// kayitParolaAzami: db_pass_plain VARCHAR(255) içine at-rest şifreli biçim ("gos1:" +
// base64(12 nonce + parola + 16 etiket)) sığmalı → en çok ~159 bayt; pay bırakılır.
const kayitParolaAzami = 128

// MySQLKayitliParolaEsitle: MariaDB'ye DOKUNMADAN panel kaydını (db_accounts.db_pass_plain)
// verilen parolaya eşitler — ancak parola MariaDB'de GERÇEKTEN giriş yapabiliyorsa.
//
// Ne zaman: hesabın parolası panel dışından değişmiş (uygulama kurulumu, elle ALTER USER,
// eski taşıma) ve uygulamanın yapılandırma dosyasındaki parola DOĞRU, panel kaydı BAYAT.
// Bu durumda "Parola Sıfırla" (ALTER USER) ÇALIŞAN siteyi kırardı; burada site etkilenmez.
//
// Güvenlik: ALTER USER yolunun sahiplik kapılarının aynısı uygulanır (sistem hesabı asla;
// kullanıcı yalnız bu domaine kayıtlı; hesabın yetkileri yalnız bu domainin DB'lerinde).
// Parola SQL'e gömülmez (sürücü yapılandırması + at-rest şifreli kayıt) → ParolaGecerli
// karakter kısıtı gerekmez; mevcut uygulama parolası tırnak vb. içerebilir.
func MySQLKayitliParolaEsitle(ctx context.Context, panelDB *sql.DB, domainID int64, dbUser, parola string) error {
	if SistemDBHesabiMi(dbUser) {
		return ErrSistemDBHesabi
	}
	if !GecerliDBKimlik(dbUser) {
		return fmt.Errorf("güvenlik: geçersiz kullanıcı adı")
	}
	if parola == "" || len(parola) > kayitParolaAzami || gizli.SifreliMi(parola) {
		return fmt.Errorf("geçersiz parola")
	}
	if err := kullaniciYalnizBuDomainde(panelDB, domainID, dbUser); err != nil {
		return err
	}
	if err := hesapYetkileriBuDomainde(panelDB, domainID, dbUser); err != nil {
		return err
	}
	if err := dbGirisDene(ctx, dbUser, parola); err != nil {
		if errors.Is(err, ErrDBParolaUyusmuyor) {
			return ErrDBParolaUyusmuyor
		}
		return fmt.Errorf("parola MariaDB'de doğrulanamadı: %w", err)
	}
	// BINARY: bkz. MySQLChangePassword (harf varyantlı komşu satırı ezilmez).
	if _, err := panelDB.ExecContext(ctx,
		`UPDATE db_accounts SET db_pass_plain=? WHERE domain_id=? AND BINARY db_user=?`,
		gizli.SaklaBagli(parola, dbUser), domainID, dbUser); err != nil {
		return fmt.Errorf("metadata: %w", err)
	}
	return nil
}
