package backups

// hedef_lftp.go — mevcut FTP/SFTP yolunu (lftp/sshpass/curl) hedef.Hedef
// arayüzüne saran ADAPTÖR. Sarılan fonksiyonların (uploadToRemote,
// deleteFromRemote, fetchGenelDizinden, testConnection, uzakBoyut) GÖVDELERİ
// DEĞİŞMEDİ; adaptör yalnız ad eşlemesi (taban dizin + göreli ad), yerel dosya
// adlandırması, atomik indirme ve hata sınıflama/maskeleme yapar.
// backup_destinations ve backup_genel_ayar mevcut okuyucularla (readDestination,
// genelAyarOku) aynen okunur; adaptör şemaya dokunmaz.
//
// Bilinen sınırlar (Y1 "eski yol sertleştirme" kapatacak):
//   - Yükleme doğrudan nihai ada yazılır (lftp put) → YetenekAtomikYukle YOK;
//     hedeftest.Uyum'dan bu yüzden geçmez. Orkestratör manifesti EN SON
//     yükleyerek "tamam"ı işaretler.
//   - Listele yalnız TAM ad sorgusudur (uzakBoyut): boyut okunamazsa ya da 0
//     ise boş liste döner — "bulunamadı" ile "okunamadı" ayırt edilemez.
//   - Ilerleme ve bant sınırı uygulanmaz; FTP TLS / SFTP host anahtarı
//     doğrulaması sarılan betiklerdeki gibidir.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"girginospanel/internal/backups/hedef"
)

// lftpHedef — tek bir FTP/SFTP hedefi (domain ya da sistem geneli).
type lftpHedef struct {
	d Destination // kopya; UzakDizin = TABAN dizin
}

var _ hedef.Hedef = (*lftpHedef)(nil)

// yeniLftpHedef — Destination'dan adaptör.
//
// 🔴 Aktif bayrağı YÜKLEME kapısıdır ve orkestratörün işidir: uploadToRemote
// Aktif=false iken SESSİZCE nil döner. Adaptör bunu kopyada true yapar; aksi
// halde kapalı bir hedefe "yükle" çağrısı hiçbir şey yüklemeden BAŞARI döner
// ve orkestratör olmayan bir uzak kopyayı "tamam" sayardı.
func yeniLftpHedef(d *Destination) (*lftpHedef, error) {
	if d == nil || !gecerliTip(d.Tip) || strings.TrimSpace(d.Host) == "" {
		return nil, hedef.YeniHata(hedef.KategoriBilinmeyen, "kur", errors.New("geçersiz ftp/sftp hedefi"))
	}
	c := *d
	c.Aktif = true
	return &lftpHedef{d: c}, nil
}

// genelLftpHedefi — sistem geneli hedef (backup_genel_ayar id=1) için adaptör.
func genelLftpHedefi(g *GenelAyar) (*lftpHedef, error) {
	if g == nil {
		return nil, hedef.YeniHata(hedef.KategoriBilinmeyen, "kur", errors.New("genel hedef yok"))
	}
	return yeniLftpHedef(g.hedef())
}

// lftpGenelAd — sistem geneli hedefteki mevcut yerleşimin göreli adı:
// "c_x-20260825-233538.tar.gz" → "2026-08-25/c_x-20260825-233538.tar.gz"
// (damga yoksa tabanda; pushGenelAsync/fetchGenelUzaktan ile aynı kural).
func lftpGenelAd(dosyaAdi string) string {
	if t := uzakTarihDizini(dosyaAdi); t != "" {
		return t + "/" + dosyaAdi
	}
	return dosyaAdi
}

func (h *lftpHedef) Tur() string               { return h.d.Tip }
func (h *lftpHedef) Yetenekler() hedef.Yetenek { return 0 }

// genel — fetchGenelDizinden/uzakBoyut GenelAyar bekler; aynı kimlik + taban.
func (h *lftpHedef) genel() *GenelAyar {
	return &GenelAyar{
		UzakAktif: true, UzakTip: h.d.Tip, UzakHost: h.d.Host, UzakPort: h.d.Port,
		UzakKullanici: h.d.Kullanici, UzakParola: h.d.Parola, UzakDizin: h.d.UzakDizin,
	}
}

// yol — göreli adı (dizin, dosya) olarak taban dizine katlar.
func (h *lftpHedef) yol(islem, ad string) (uzakDizin, dosya string, err error) {
	if err := hedef.AdDogrula(ad); err != nil {
		return "", "", hedef.YeniHata(hedef.KategoriBilinmeyen, islem, err)
	}
	dizin, dosya := hedef.AdBol(ad)
	return birlestirYol(h.d.UzakDizin, dizin), dosya, nil
}

// Test — testConnection (SFTP: dizin oluştur + yaz + sil; FTP: kök listesi).
func (h *lftpHedef) Test(ctx context.Context) error {
	d := h.d
	return hedef.Sarmala("test", testConnection(ctx, &d), h.d.Parola)
}

// Yukle — uploadToRemote dosyayı YEREL adıyla koyar (`put -O .`); yerel ad uzak
// addan farklıysa, uzak adı taşıyan bir sembolik bağ 0700 geçici dizinde verilir.
func (h *lftpHedef) Yukle(ctx context.Context, yerelYol, uzakAd string, _ hedef.YukleSecenek) error {
	uzakDizin, dosya, err := h.yol("yukle", uzakAd)
	if err != nil {
		return err
	}
	if _, err := os.Stat(yerelYol); err != nil {
		return hedef.Sarmala("yukle", err)
	}
	kaynak := yerelYol
	if filepath.Base(yerelYol) != dosya {
		mutlak, err := filepath.Abs(yerelYol)
		if err != nil {
			return hedef.Sarmala("yukle", err)
		}
		gd, err := os.MkdirTemp("", "gosp-hedef-*")
		if err != nil {
			return hedef.Sarmala("yukle", err)
		}
		defer os.RemoveAll(gd)
		kaynak = filepath.Join(gd, dosya)
		if err := os.Symlink(mutlak, kaynak); err != nil {
			return hedef.Sarmala("yukle", err)
		}
	}
	d := h.d
	d.UzakDizin = uzakDizin
	if err := uploadToRemote(ctx, &d, kaynak, dosya); err != nil {
		return hedef.Sarmala("yukle", err, h.d.Parola)
	}
	return hedef.Sarmala("yukle", ctx.Err())
}

// Listele — YALNIZ tam ad (uzakBoyut); dizin/taban önekleri desteklenmez.
func (h *lftpHedef) Listele(ctx context.Context, onek string) ([]hedef.Nesne, error) {
	if hedef.AdDogrula(onek) != nil {
		return nil, hedef.YeniHata(hedef.KategoriBilinmeyen, "listele", hedef.ErrDesteklenmiyor)
	}
	uzakDizin, dosya, err := h.yol("listele", onek)
	if err != nil {
		return nil, err
	}
	n := uzakBoyut(ctx, h.genel(), uzakDizin, dosya)
	if err := ctx.Err(); err != nil {
		return nil, hedef.Sarmala("listele", err)
	}
	if n <= 0 {
		return nil, nil
	}
	return []hedef.Nesne{{Ad: onek, Boyut: n}}, nil
}

// Indir — fetchGenelDizinden GEÇİCİ ada indirir; yalnız başarıda yerelYol'a
// taşınır (kesilen indirme nihai adı almaz).
func (h *lftpHedef) Indir(ctx context.Context, uzakAd, yerelYol string) error {
	uzakDizin, dosya, err := h.yol("indir", uzakAd)
	if err != nil {
		return err
	}
	gecici := hedef.GeciciAd(yerelYol)
	defer os.Remove(gecici)
	if err := fetchGenelDizinden(ctx, h.genel(), uzakDizin, dosya, gecici); err != nil {
		return hedef.Sarmala("indir", err, h.d.Parola)
	}
	if err := ctx.Err(); err != nil {
		return hedef.Sarmala("indir", err)
	}
	if err := os.Rename(gecici, yerelYol); err != nil {
		return hedef.Sarmala("indir", err)
	}
	return nil
}

// Sil — deleteFromRemote (yoksa nil; silindiğini listeleyerek doğrular).
// lftp kurulu değilse dönen hata errors.Is(err, ErrLftpYok) ile tanınır.
func (h *lftpHedef) Sil(ctx context.Context, uzakAd string) error {
	uzakDizin, dosya, err := h.yol("sil", uzakAd)
	if err != nil {
		return err
	}
	d := h.d
	return hedef.Sarmala("sil", deleteFromRemote(ctx, &d, uzakDizin, dosya), h.d.Parola)
}
