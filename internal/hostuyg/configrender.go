package hostuyg

// Config dosyaları render + yazma.
//
// Placeholder'lar (UnitRender ile aynı):
//   {kurulum}, {sistem_kullanici}, {panelhost}
//   {port_<ad>}   → ör. {port_web}, {port_ssh}
//   {secret16|32|64} → hex random
//
// Sahip:
//   "app"  → sistem_kullanici (uygulamanın kendisi)
//   "root" → root (uygulama sadece okuyabilir)

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfigYaz — Tarif.ConfigDosyalar'ı hedef dizine yaz.
// Dönen map: config template variable adı → gerçek değer (secret'ler dahil, unit
// render'da tekrar kullanılabilir).
func ConfigYaz(
	tarif *Tarif,
	kurulumYolu, sistemKullanici, panelhost string,
	portlar map[string]int,
	secretler map[string]string,
) error {
	if len(tarif.ConfigDosyalar) == 0 {
		return nil
	}

	uid, gid, uidErr := KullaniciUID(sistemKullanici)

	for _, c := range tarif.ConfigDosyalar {
		icerik := c.Icerik
		icerik = degiskenYerinekoy(icerik, kurulumYolu, sistemKullanici, panelhost, portlar, secretler)

		hedef := filepath.Join(kurulumYolu, c.Yol)
		if err := os.MkdirAll(filepath.Dir(hedef), 0755); err != nil {
			return fmt.Errorf("config dizini: %w", err)
		}
		izin := os.FileMode(c.Izin)
		if izin == 0 {
			izin = 0640
		}
		// Atomic yaz
		tmp := hedef + ".tmp"
		if err := os.WriteFile(tmp, []byte(icerik), izin); err != nil {
			return fmt.Errorf("config yaz: %w", err)
		}
		if err := os.Rename(tmp, hedef); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("config rename: %w", err)
		}
		// Sahip ayarla
		if c.Sahip == "app" && uidErr == nil {
			_ = os.Chown(hedef, uid, gid)
		}
	}
	return nil
}

// DizinSahipDegistir — kurulum dizinini recursive olarak app user'a ver.
// systemd unit User=app-... olduğu için app kendi dizinine yazabilmeli.
// WalkDir kullanır (Walk deprecated, WalkDir daha performanslı).
func DizinSahipDegistir(dizin, sistemKullanici string) error {
	uid, gid, err := KullaniciUID(sistemKullanici)
	if err != nil {
		return err
	}
	return filepath.WalkDir(dizin, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(path, uid, gid)
	})
}

func degiskenYerinekoy(s, kurulum, sistemKul, panelhost string, portlar map[string]int, secretler map[string]string) string {
	s = strings.ReplaceAll(s, "{kurulum}", kurulum)
	s = strings.ReplaceAll(s, "{sistem_kullanici}", sistemKul)
	s = strings.ReplaceAll(s, "{panelhost}", panelhost)
	for ad, p := range portlar {
		s = strings.ReplaceAll(s, "{port_"+ad+"}", fmt.Sprintf("%d", p))
	}
	for k, v := range secretler {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}
