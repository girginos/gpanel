package main

// Yedek çözme CLI'ı — şifreli (age) uzak yedek kopyalarını çözer:
//
//	girginospanel-server yedek-coz <dosya.age> --anahtar <anahtar-dosyasi> [--cikti <yol>]
//	girginospanel-server yedek-coz --indir <is-no> --anahtar <anahtar-dosyasi> [--cikti <yol>]
//
// İlk biçim panel/DB gerektirmez (felaket sonrası başka makinede çalışır). İkinci
// biçim (uzak kopyayı hedeften indirip çözer) panel DB'sini ve hedef kimliğini kullanır.
// Anahtar dosyası argv'ye DEĞERİ olarak değil YOLU olarak verilir; özel anahtar hiçbir
// zaman stdout/stderr'e yazılmaz. Mevcut çıktı dosyası ezilmez; yanlış anahtarda çıktı kalmaz.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"

	"girginospanel/internal/backups"
	"girginospanel/internal/backups/uzak"
	"girginospanel/internal/config"
	"girginospanel/internal/db"
)

const yedekCozKullanim = `Kullanım:
  girginospanel-server yedek-coz <dosya.age> --anahtar <anahtar-dosyasi> [--cikti <yol>]
  girginospanel-server yedek-coz --indir <is-no> --anahtar <anahtar-dosyasi> [--cikti <yol>]
Hibrit (post-kuantum) anahtarlar için age >= 1.3.0 (bu araç dahil) gerekir.
Çıkış: 0 tamam · 1 başarısız · 2 kullanım`

func yedekCozCLI(args []string, out, errw io.Writer) int {
	fs := flag.NewFlagSet("yedek-coz", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	anahtar := fs.String("anahtar", "", "age özel anahtar dosyası")
	cikti := fs.String("cikti", "", "çıktı dosyası (varsayılan: .age uzantısız ad)")
	indir := fs.Int64("indir", 0, "uzak kopya iş numarası (hedeften indirip çözer)")
	// flag paketi ilk konumsal argümanda durur; dosya adı başta verilebilsin diye ayır.
	var dosya string
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		dosya, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil || *anahtar == "" || (dosya == "" && *indir <= 0) || (dosya != "" && *indir > 0) {
		fmt.Fprintln(errw, yedekCozKullanim)
		return 2
	}
	if *indir > 0 {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintln(errw, "yapılandırma okunamadı:", err)
			return 1
		}
		d, err := db.Open(cfg.DBDsn)
		if err != nil {
			fmt.Fprintln(errw, "veritabanı açılamadı")
			return 1
		}
		i := backups.UzakIsciKur(d)
		yol, err := i.IndirVeCoz(context.Background(), *indir, *anahtar, *cikti)
		if err != nil {
			fmt.Fprintln(errw, "başarısız:", yedekCozHata(err))
			return 1
		}
		fmt.Fprintln(out, "çözüldü:", yol, "(iş "+strconv.FormatInt(*indir, 10)+")")
		return 0
	}
	if err := uzak.DosyaCoz(dosya, *anahtar, *cikti); err != nil {
		fmt.Fprintln(errw, "başarısız:", yedekCozHata(err))
		return 1
	}
	c := *cikti
	if c == "" {
		c = uzak.VarsayilanCikti(dosya)
	}
	fmt.Fprintln(out, "çözüldü:", c)
	return 0
}

// yedekCozHata — kullanıcıya gösterilecek hata. Hata değerleri anahtar içeriği taşımaz
// (uzak paketindeki sabit metinler ya da dosya yolu hataları).
func yedekCozHata(err error) string { return err.Error() }
