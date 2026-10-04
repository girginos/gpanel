// destek-hub — GirginOSPanel Telegram destek botu MERKEZ servisi (bot.girginos.io).
//
//	destek-hub [calistir] --yapilandirma /etc/girginos-destek/destek-hub.json [--kimlik-dizini DIR]
//	destek-hub webhook-kur --url https://bot.girginos.io/v1/telegram/webhook [--bekleyenleri-sil]
//	destek-hub anahtar-uret --cikti /etc/girginos-destek/merkez-imza-anahtari
//	destek-hub acik-anahtar [--kimlik-dizini DIR]
//
// Sırlar YALNIZ systemd LoadCredential dizininden (CREDENTIALS_DIRECTORY) okunur;
// ortam değişkeninden sır okunmaz. Ayrıntı: deploy/destek-hub/KURULUM.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"girginospanel/internal/destek/merkez"
	"girginospanel/internal/destek/protokol"
)

func main() {
	alt := "calistir"
	args := os.Args[1:]
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		alt, args = args[0], args[1:]
	}
	var err error
	switch alt {
	case "calistir":
		err = calistir(args)
	case "webhook-kur":
		err = webhookKur(args)
	case "anahtar-uret":
		err = anahtarUret(args)
	case "acik-anahtar":
		err = acikAnahtar(args)
	default:
		err = fmt.Errorf("bilinmeyen alt komut %q (calistir | webhook-kur | anahtar-uret | acik-anahtar)", alt)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "destek-hub:", err)
		os.Exit(1)
	}
}

func calistir(args []string) error {
	fs := flag.NewFlagSet("calistir", flag.ExitOnError)
	yapYol := fs.String("yapilandirma", "/etc/girginos-destek/destek-hub.json", "yapılandırma dosyası")
	kimlik := fs.String("kimlik-dizini", "", "sır dizini (varsayılan: $CREDENTIALS_DIRECTORY)")
	_ = fs.Parse(args)

	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	yap, err := merkez.YapilandirmaYukle(*yapYol)
	if err != nil {
		return err
	}
	dizin, err := merkez.KimlikDizini(*kimlik)
	if err != nil {
		return err
	}
	sir, err := merkez.SirlariYukle(dizin)
	if err != nil {
		return err
	}
	if yap.VeriDizini == "" {
		return errors.New("veri_dizini zorunlu")
	}
	depo, err := merkez.DosyaDepoAc(yap.VeriDizini)
	if err != nil {
		return err
	}
	m, err := merkez.Yeni(merkez.Secenekler{
		Yap: yap, Sirlar: sir, Depo: depo, Log: log,
		Claude:   merkez.YeniSDKIstemci(sir.AnthropicAnahtar, yap.AnthropicTabanURL),
		Telegram: merkez.YeniTelegramHTTP(sir.TelegramToken, yap.TelegramTabanURL),
	})
	if err != nil {
		return err
	}
	ctx, dur := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer dur()
	go m.Calistir(ctx)

	srv := &http.Server{
		Addr: yap.Dinle, Handler: m.Handler(),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: protokol.YoklamaBekleme + 15*time.Second, IdleTimeout: 90 * time.Second,
		MaxHeaderBytes: 32 << 10,
	}
	hatalar := make(chan error, 2)
	go func() { hatalar <- srv.ListenAndServe() }()
	var ic *http.Server
	if yap.IcDinle != "" {
		ic = &http.Server{
			Addr: yap.IcDinle, Handler: m.IcHandler(),
			ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second,
			IdleTimeout: 60 * time.Second, MaxHeaderBytes: 8 << 10,
		}
		go func() { hatalar <- ic.ListenAndServe() }()
	}
	log.Info("destek-hub başladı", "dinle", yap.Dinle, "ic", yap.IcDinle, "merkez_anahtar_kimligi", sir.MerkezAnahtar.Kimlik)
	select {
	case <-ctx.Done():
	case err := <-hatalar:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	kapat, iptal := context.WithTimeout(context.Background(), 20*time.Second)
	defer iptal()
	_ = srv.Shutdown(kapat)
	if ic != nil {
		_ = ic.Shutdown(kapat)
	}
	m.Bekle()
	log.Info("destek-hub durdu")
	return nil
}

func webhookKur(args []string) error {
	fs := flag.NewFlagSet("webhook-kur", flag.ExitOnError)
	hedef := fs.String("url", "https://"+protokol.MerkezHost+protokol.TelegramWebhookYolu, "webhook URL'si")
	kimlik := fs.String("kimlik-dizini", "", "sır dizini (varsayılan: $CREDENTIALS_DIRECTORY)")
	sil := fs.Bool("bekleyenleri-sil", false, "bekleyen güncellemeleri at")
	taban := fs.String("telegram-taban", "", "Telegram API tabanı (test)")
	_ = fs.Parse(args)
	dizin, err := merkez.KimlikDizini(*kimlik)
	if err != nil {
		return err
	}
	sir, err := merkez.SirlariYukle(dizin)
	if err != nil {
		return err
	}
	ctx, iptal := context.WithTimeout(context.Background(), 30*time.Second)
	defer iptal()
	if err := merkez.YeniTelegramHTTP(sir.TelegramToken, *taban).WebhookKur(ctx, *hedef, sir.WebhookGizli, *sil); err != nil {
		return err
	}
	fmt.Println("webhook kuruldu:", *hedef)
	return nil
}

func anahtarUret(args []string) error {
	fs := flag.NewFlagSet("anahtar-uret", flag.ExitOnError)
	cikti := fs.String("cikti", "", "32 baytlık Ed25519 tohumunun yazılacağı dosya (0600, üzerine yazmaz)")
	_ = fs.Parse(args)
	if *cikti == "" {
		return errors.New("--cikti zorunlu")
	}
	c, err := protokol.AnahtarCiftiUret(nil)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(*cikti, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(c.Ozel.Seed()); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Printf("açık anahtar (panele gömülecek): %s\nanahtar kimliği: %s\n", protokol.AcikAnahtarKodla(c.Acik), c.Kimlik)
	return nil
}

func acikAnahtar(args []string) error {
	fs := flag.NewFlagSet("acik-anahtar", flag.ExitOnError)
	kimlik := fs.String("kimlik-dizini", "", "sır dizini (varsayılan: $CREDENTIALS_DIRECTORY)")
	_ = fs.Parse(args)
	dizin, err := merkez.KimlikDizini(*kimlik)
	if err != nil {
		return err
	}
	sir, err := merkez.SirlariYukle(dizin)
	if err != nil {
		return err
	}
	fmt.Printf("%s %s\n", protokol.AcikAnahtarKodla(sir.MerkezAnahtar.Acik), sir.MerkezAnahtar.Kimlik)
	return nil
}
