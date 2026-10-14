package provisioner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"girginospanel/internal/httpx"

	"github.com/go-sql-driver/mysql"
)

// PHPSurumHealOnStartup — veritabanındaki GEÇERSİZ php_surum değerlerini, kaydın bugün
// FİİLEN çalıştığı sürüme çeker (açılışta bir kez; idempotent).
//
// NEDEN: php_surum eskiden doğrulanmadan yazılıyordu (API/WHMCS serbest metni, plan
// kopyası, alt alan ayarı). normalizePHP geçersiz değeri sessizce 8.3 havuzuna düşürüyor,
// DB'de ham değer kalıyordu: arayüz/API yanlış sürüm gösteriyor, aynı değerle yapılan
// sonraki kayıt artık 400 alıyor (PHPSurumDogrula).
//
// 🔴 DAVRANIŞ DEĞİŞMEZ: yazılan değer, o kaydın bugün kullandığı sürümdür — geçersiz →
// normalizePHP ("8.3"); boşluklu geçerli değer ("8.2 ") kırpılır. Boş değere dokunulmaz
// (varsayılan/devral anlamı). cp_laravel_apps'te boş = sistem PHP'si (laravel.phpBin) ve
// geçersiz değer bugün de sistem PHP'sine düşüyor → boşa çekilir.
// 🔴 İKİLİ KARŞILAŞTIRMA: tabloların harmanlaması PAD SPACE ("8.2 " = "8.2"); DISTINCT ve
// WHERE sondaki boşluğu yok sayardı.
func PHPSurumHealOnStartup() {
	if pkgDB == nil {
		return
	}
	ctx, iptal := context.WithTimeout(context.Background(), 2*time.Minute)
	defer iptal()
	if n, err := PHPSurumHeal(ctx, pkgDB); err != nil {
		log.Printf("php_surum onarımı: %v", err)
	} else if n > 0 {
		log.Printf("php_surum onarımı: %d kayıt fiilen çalışan sürüme çekildi", n)
	}
}

var phpSurumTablolari = []struct {
	tablo   string
	laravel bool // geçersiz → "" (sistem PHP'si)
}{
	{"domains", false}, {"subdomanlar", false}, {"ek_alan_adlari", false},
	{"service_plans", false}, {"cp_laravel_apps", true},
}

// PHPSurumHeal: düzeltilen kayıt sayısını döner. Olmayan tablo atlanır (eski şema).
func PHPSurumHeal(ctx context.Context, db *sql.DB) (int, error) {
	toplam := 0
	for _, t := range phpSurumTablolari {
		rows, err := db.QueryContext(ctx, `SELECT DISTINCT CAST(php_surum AS BINARY) FROM `+t.tablo+ //nolint:gosec // G202: tablo adı SABİT listeden
			` WHERE php_surum IS NOT NULL AND CAST(php_surum AS BINARY) <> ''`)
		if err != nil {
			var me *mysql.MySQLError
			if errors.As(err, &me) && me.Number == 1146 { // tablo yok
				continue
			}
			return toplam, fmt.Errorf("%s okunamadı: %w", t.tablo, err)
		}
		var degerler []string
		for rows.Next() {
			var b []byte
			if err := rows.Scan(&b); err == nil {
				degerler = append(degerler, string(b))
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return toplam, fmt.Errorf("%s okunamadı: %w", t.tablo, err)
		}
		for _, v := range degerler {
			hedef, degisir := phpSurumHedef(v, t.laravel)
			if !degisir {
				continue
			}
			res, err := db.ExecContext(ctx, `UPDATE `+t.tablo+` SET php_surum=? WHERE CAST(php_surum AS BINARY)=CAST(? AS BINARY)`, hedef, v) //nolint:gosec // G202: tablo adı SABİT listeden
			if err != nil {
				return toplam, fmt.Errorf("%s güncellenemedi: %w", t.tablo, err)
			}
			n, _ := res.RowsAffected()
			toplam += int(n)
			log.Printf("php_surum onarımı: %s %q → %q (%d kayıt)", t.tablo, v, hedef, n)
			httpx.DenetimSistem(db, 0, "sistem", "php_surum.duzeltildi", t.tablo,
				fmt.Sprintf("%q → %q (%d kayıt; bugün kullanılan sürüm)", v, hedef, n), 0, true)
		}
	}
	return toplam, nil
}

// phpSurumHedef: v'nin yazılması gereken değeri; degisir=false ise dokunulmaz.
func phpSurumHedef(v string, laravel bool) (string, bool) {
	k := strings.TrimSpace(v)
	if _, ok := phpMap[k]; ok {
		return k, k != v // geçerli: yalnız boşluk varsa kırp
	}
	if laravel {
		return "", true
	}
	return normalizePHP(k), true
}
