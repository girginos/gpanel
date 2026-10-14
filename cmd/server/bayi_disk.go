package main

// bayi-disk-kotasi: bayi disk havuzunun (XFS proje kotası) durumunu gösterir; --uygula ile
// kernel komut satırına (rootflags) pquota ekler. Yalnız YÖNETİCİNİN açık eylemidir — panel bunu
// kendiliğinden ASLA çalıştırmaz (grub/kernel değişikliği). Etkisi bir SONRAKİ yeniden
// başlatmada başlar. Kullanım:
//
//	girginospanel bayi-disk-kotasi            durum + kuru çalıştırma (hiçbir şey değişmez)
//	girginospanel bayi-disk-kotasi --uygula   grubby ile rootflags'e pquota ekle

import (
	"context"
	"fmt"
	"io"
	"time"

	"girginospanel/internal/bayikaynak"
)

func bayiDiskKotasiCLI(args []string, out, errw io.Writer) int {
	uygula := false
	for _, a := range args {
		switch a {
		case "--uygula":
			uygula = true
		case "-h", "--help", "yardim":
			fmt.Fprintln(out, "kullanım: girginospanel bayi-disk-kotasi [--uygula]")
			return 0
		default:
			fmt.Fprintf(errw, "bilinmeyen argüman: %q (kullanım: bayi-disk-kotasi [--uygula])\n", a)
			return 2
		}
	}
	ctx, iptal := context.WithTimeout(context.Background(), 2*time.Minute)
	defer iptal()
	d := bayikaynak.DiskDurumu(ctx)
	fmt.Fprintf(out, "dosya sistemi : %s (%s)\n", d.Mount, d.FS)
	fmt.Fprintf(out, "mod           : %s\n", d.Mod)
	fmt.Fprintf(out, "açıklama      : %s\n", d.Mesaj)
	fmt.Fprintf(out, "rootflags     : mevcut=%q önerilen=%q\n", d.MevcutRootflags, d.OnerilenRootflag)
	s, err := bayikaynak.Etkinlestir(ctx, uygula)
	if s.Komut != "" {
		fmt.Fprintf(out, "komut         : %s\n", s.Komut)
	}
	if err != nil {
		fmt.Fprintf(errw, "HATA: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "sonuç         : %s\n", s.Mesaj)
	if !uygula && s.Komut != "" {
		fmt.Fprintln(out, "uygulamak için: girginospanel bayi-disk-kotasi --uygula   (sonra sunucuyu yeniden başlatın; disk havuzu aşan bayiler için panel API'sindeki önizlemeye bakın)")
	}
	return 0
}
