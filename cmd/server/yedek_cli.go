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
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"girginospanel/internal/backups"
	"girginospanel/internal/backups/uzak"
	"girginospanel/internal/config"
	"girginospanel/internal/db"
)

const yedekCozKullanim = `Usage:
  girginospanel-server yedek-coz <file.age> --anahtar <key-file> [--cikti <path>]
  girginospanel-server yedek-coz --indir <job-id> --anahtar <key-file> [--cikti <path>]
Decrypts encrypted (age) remote backup copies. The first form needs no panel or DB
(it works on another machine after a disaster); --indir downloads the remote copy
from its destination first and uses the panel DB.
  --anahtar  path of the age private key file (the path, never the key itself)
  --cikti    output file (default: the name without .age); an existing file is never overwritten
  --indir    remote copy job number
Hybrid (post-quantum) keys need age >= 1.3.0 (this tool included).
Exit: 0 ok · 1 failed · 2 usage`

func yedekCozCLI(args []string, out, errw io.Writer) int {
	fs := flag.NewFlagSet("yedek-coz", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	anahtar := fs.String("anahtar", "", "age private key file")
	cikti := fs.String("cikti", "", "output file (default: the name without .age)")
	indir := fs.Int64("indir", 0, "remote copy job number (downloads from the destination and decrypts)")
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
			fmt.Fprintln(errw, "could not read the configuration:", err)
			return 1
		}
		d, err := db.Open(cfg.DBDsn)
		if err != nil {
			fmt.Fprintln(errw, "could not open the database")
			return 1
		}
		i := backups.UzakIsciKur(d)
		yol, err := i.IndirVeCoz(context.Background(), *indir, *anahtar, *cikti)
		if err != nil {
			fmt.Fprintln(errw, "failed:", yedekCozHata(err))
			return 1
		}
		fmt.Fprintln(out, "decrypted:", yol, "(job "+strconv.FormatInt(*indir, 10)+")")
		return 0
	}
	if err := uzak.DosyaCoz(dosya, *anahtar, *cikti); err != nil {
		fmt.Fprintln(errw, "failed:", yedekCozHata(err))
		return 1
	}
	c := *cikti
	if c == "" {
		c = uzak.VarsayilanCikti(dosya)
	}
	fmt.Fprintln(out, "decrypted:", c)
	return 0
}

// yedekCozHata — kullanıcıya gösterilecek hata. Hata değerleri anahtar içeriği taşımaz
// (uzak paketindeki sabit metinler ya da dosya yolu hataları).
func yedekCozHata(err error) string {
	// Bu iki hata panelin uzak-kopya arayüzüyle paylaşılır (Türkçe kalır) → CLI'da çevrilir.
	switch {
	case errors.Is(err, uzak.ErrIsYok):
		return "job not found"
	case errors.Is(err, uzak.ErrDurumUygunDegil):
		return "the job's state does not allow this operation" +
			strings.Replace(strings.TrimPrefix(err.Error(), uzak.ErrDurumUygunDegil.Error()), "(durum:", "(state:", 1)
	}
	return err.Error()
}
