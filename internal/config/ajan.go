package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/go-sql-driver/mysql"
)

// AjanYapilandirma — girginospanel-destek-ajan hizmetinin ASGARİ yapılandırması:
// yalnız veritabanı bağlantısı ve çıktı maskesi için bilinen panel sırları. JWT sırrı,
// Redis parolası vb. ajanda kullanılmaz; yalnız araç çıktılarında BİREBİR maskelensin
// diye Sirlar'a konur (bellekte; hiçbir yere yazılmaz/loglanmaz).
type AjanYapilandirma struct {
	DBDsn string
	// Sirlar — maskelenecek panel sırları (JWT sırrı, DB DSN'i ve parolası, Redis yönetici
	// parolası, diğer sır-adlı değişkenler).
	Sirlar []string
	// SirKaynagi — "kimlik" (systemd LoadCredential) ya da "ortam" (geliştirme).
	SirKaynagi string
}

// AjanYukle — panel ile AYNI kimlik dosyasını (LoadCredential=panel-env:/etc/girginospanel/env)
// okur; sır yoksa hata verir. Load gibi sırları süreç ortamından siler.
func AjanYukle() (*AjanYapilandirma, error) {
	defer SirlariOrtamdanSil()
	kim, err := kimlikOku()
	if err != nil {
		return nil, err
	}
	kaynak := "ortam"
	sir := func(ad string) string { return strings.TrimSpace(os.Getenv(ad)) }
	var tum []string
	if kim != nil {
		kaynak = "kimlik"
		sir = func(ad string) string { return strings.TrimSpace(kim[ad]) }
		for k, v := range kim {
			if SirDegiskeniMi(k) && len(strings.TrimSpace(v)) >= 8 {
				tum = append(tum, strings.TrimSpace(v))
			}
		}
	} else {
		for _, ad := range SirAdlari {
			if v := strings.TrimSpace(os.Getenv(ad)); len(v) >= 8 {
				tum = append(tum, v)
			}
		}
	}
	dsn := sir("PANEL_DB_DSN")
	if dsn == "" {
		return nil, fmt.Errorf("PANEL_DB_DSN yok (kaynak: %s)", kaynak)
	}
	c := &AjanYapilandirma{DBDsn: dsn, SirKaynagi: kaynak}
	seen := map[string]bool{}
	ekle := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			c.Sirlar = append(c.Sirlar, s)
		}
	}
	for _, s := range tum {
		ekle(s)
	}
	ekle(dsn)
	if dc, err := mysql.ParseDSN(dsn); err == nil {
		ekle(dc.Passwd)
	}
	return c, nil
}
