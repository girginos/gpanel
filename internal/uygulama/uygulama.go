// Package uygulama — TENANT-level (per-domain) uygulama installer.
//
// Kullanıcı bir hosting hesabına PHP CMS (WordPress, Joomla, Nextcloud vb.)
// kurabilsin diye. Kurulum modeli:
//   1. Alt-dizin seç (public_html/ veya public_html/blog/)
//   2. Katalog'dan uygulama seç
//   3. indir → çıkar (runuser -u <sk>) → DB oluştur → web-sihirbaza devret
//
// Kurulum kayıtları cp_uygulama_kurulumlar tablosunda tutulur.
// UI: kurulan app'lerin listesi + yönetim URL + kaldır.
//
// GÜVENLİK — tenant-level KRİTİK:
//   - MusteriScope middleware zorunlu (cross-tenant erişim engellenir)
//   - sk (system kullanıcı) her yerde "c_" prefix'i garantili
//   - symlink-red guard (os.Lstat) — path traversal
//   - çıkarma runuser -u <sk> (root DEĞİL)
//   - alt-dizin normalize + validate (`../` yasak)

package uygulama

import (
	"context"
	crand "crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"girginospanel/internal/hesaplar"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Tarif struct {
	Kod         string
	Ad          string
	Aciklama    string
	Ikon        string // fallback emoji
	LogoURL     string // Simple Icons CDN veya kendi upstream
	Kategori    string
	Surum       string
	IndirmeURL  string
	SHA256      string
	IcerikTuru  string
	YonetimYolu string
	DBGerek     bool
	// NativeApp: PHP değil, sistemler bazlı çalışan uygulama.
	// Şu an tenant-level installer HAZIR DEĞİL — UI "yakında" gösterir.
	NativeApp bool
}

// Simple Icons CDN — https://simpleicons.org/ — brand SVG'leri
const simpleIcon = "https://cdn.simpleicons.org/"

var Katalog = []Tarif{
	// -------- CMS'ler (PHP) --------
	{
		Kod: "joomla", Ad: "Joomla!", Ikon: "🟠",
		LogoURL:  simpleIcon + "joomla/F44321",
		Aciklama: "Esnek CMS — çok dilli, çok kullanıcılı siteler için ideal.",
		Kategori: "cms", Surum: "5.2.3",
		IndirmeURL:  "https://downloads.joomla.org/cms/joomla5/5-2-3/Joomla_5-2-3-Stable-Full_Package.tar.gz",
		IcerikTuru:  "tarball_gz",
		YonetimYolu: "/administrator/", DBGerek: true,
	},
	{
		Kod: "drupal", Ad: "Drupal", Ikon: "💧",
		LogoURL:  simpleIcon + "drupal/0678BE",
		Aciklama: "Kurumsal seviye CMS — büyük siteler, çoklu içerik türü.",
		Kategori: "cms", Surum: "10.3.8",
		IndirmeURL:  "https://ftp.drupal.org/files/projects/drupal-10.3.8.tar.gz",
		IcerikTuru:  "tarball_gz",
		YonetimYolu: "/", DBGerek: true,
	},
	{
		Kod: "grav", Ad: "Grav CMS", Ikon: "⚡",
		LogoURL:  simpleIcon + "grav/221E1F",
		Aciklama: "Flat-file (DB'siz) modern CMS — hızlı, hafif, geliştirici dostu.",
		Kategori: "cms", Surum: "2.0.19",
		// 🔴 getgrav.org/download/core/... adresi 404 veriyor (site tarafi degisti).
		// GitHub release'i kalici: "grav-admin" paketi yonetim panelini de icerir.
		IndirmeURL:  "https://github.com/getgrav/grav/releases/download/2.0.19/grav-admin-v2.0.19.zip",
		IcerikTuru:  "zip",
		YonetimYolu: "/admin", DBGerek: false,
	},
	{
		Kod: "prestashop", Ad: "PrestaShop", Ikon: "🛒",
		LogoURL:  simpleIcon + "prestashop/DF0067",
		Aciklama: "E-ticaret platformu — 300K+ mağaza kullanır. Türkçe destekli.",
		Kategori: "cms", Surum: "8.2.0",
		IndirmeURL:  "https://github.com/PrestaShop/PrestaShop/releases/download/8.2.0/prestashop_8.2.0.zip",
		IcerikTuru:  "zip",
		YonetimYolu: "/admin", DBGerek: true,
	},
	{
		Kod: "mediawiki", Ad: "MediaWiki", Ikon: "📚",
		LogoURL:  simpleIcon + "wikipedia/000000",
		Aciklama: "Wikipedia'nın motoru — kurumsal wiki, döküman platformu.",
		Kategori: "cms", Surum: "1.42.3",
		IndirmeURL:  "https://releases.wikimedia.org/mediawiki/1.42/mediawiki-1.42.3.tar.gz",
		IcerikTuru:  "tarball_gz",
		YonetimYolu: "/", DBGerek: true,
	},
	// -------- Bulut / Depolama --------
	{
		Kod: "nextcloud", Ad: "Nextcloud", Ikon: "☁️",
		LogoURL:  simpleIcon + "nextcloud/0082C9",
		Aciklama: "Self-hosted bulut depolama + ofis — Google Drive alternatifi.",
		Kategori: "bulut", Surum: "30.0.2",
		IndirmeURL:  "https://download.nextcloud.com/server/releases/nextcloud-30.0.2.tar.bz2",
		IcerikTuru:  "tarball_bz2",
		YonetimYolu: "/", DBGerek: true,
	},
	{
		Kod: "matomo", Ad: "Matomo Analytics", Ikon: "📊",
		LogoURL:  simpleIcon + "matomo/3152A0",
		Aciklama: "Google Analytics alternatifi — kendi verilerin, GDPR uyumlu.",
		Kategori: "analitik", Surum: "5.2.1",
		IndirmeURL:  "https://builds.matomo.org/matomo-5.2.1.tar.gz",
		IcerikTuru:  "tarball_gz",
		YonetimYolu: "/", DBGerek: true,
	},
	// -------- Native app'ler (sunucu-level, "yakında") --------
	{
		Kod: "teamspeak3", Ad: "TeamSpeak 3", Ikon: "🎧",
		LogoURL:  simpleIcon + "teamspeak/2580C3",
		Aciklama: "Sesli iletişim sunucusu — 32 slot ücretsiz. UDP 9987 voice.",
		Kategori: "iletisim", Surum: "3.13.7",
		NativeApp: true,
	},
	{
		Kod: "wireguard", Ad: "WireGuard VPN", Ikon: "🔒",
		LogoURL:  simpleIcon + "wireguard/88171A",
		Aciklama: "Hızlı, modern VPN — kernel modülü + wg-quick.",
		Kategori: "guvenlik", Surum: "latest",
		NativeApp: true,
	},
	{
		Kod: "gitea", Ad: "Gitea", Ikon: "🌿",
		LogoURL:  simpleIcon + "gitea/609926",
		Aciklama: "Kendi git deponuz — GitHub benzeri, hafif.",
		Kategori: "gelistirme", Surum: "1.22.6",
		NativeApp: true,
	},
}

// Kayit — DB'deki kurulum satırı.
type Kayit struct {
	ID         int64     `json:"id"`
	DomainID   int64     `json:"domain_id"`
	Kod        string    `json:"kod"`
	Ad         string    `json:"ad"`
	Surum      string    `json:"surum"`
	AltDizin   string    `json:"alt_dizin"`
	YonetimURL string    `json:"yonetim_url"`
	DBAdi      string    `json:"db_adi,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// altDizinDeseni — path traversal'a karşı: sadece harf/rakam/tire/altçizgi/slash.
// Baştaki/ortadaki `..` blok. En fazla 32 char.
var altDizinDeseni = regexp.MustCompile(`^[a-z0-9_/-]{0,32}$`)

func normalizeAltDizin(s string) (string, error) {
	s = strings.Trim(s, "/")
	s = strings.ToLower(s)
	if strings.Contains(s, "..") {
		return "", errors.New("alt_dizin '..' içeremez")
	}
	if !altDizinDeseni.MatchString(s) {
		return "", errors.New("alt_dizin geçersiz karakter (a-z 0-9 _ - /)")
	}
	return s, nil
}

// Kur — tam pipeline: dizin hazır → indir → çıkar (runuser) → DB oluştur → kayıt.
// docroot = /home/<sk>/public_html/<altDizin>
type KurArgs struct {
	Domain   Domain
	Tarif    *Tarif
	AltDizin string
	DBAdi    string // opsiyonel; boş ise otomatik `<kod>_<domainID>`
	DBSifre  string // opsiyonel; boş ise otomatik üret
}

type Domain struct {
	ID      int64
	AlanAdi string
	SK      string // sistem user "c_xxxx"
	DocRoot string // /home/<sk>/public_html
}

func Kur(ctx context.Context, db *sql.DB, a KurArgs,
	mysqlOlustur func(domainID int64, dbAd, dbUser, dbSifre string) error,
	mysqlDrop func(domainID int64, dbAd, dbUser string) error,
) (*Kayit, error) {
	if a.Tarif == nil {
		return nil, errors.New("tarif nil")
	}
	if a.Domain.SK == "" || !strings.HasPrefix(a.Domain.SK, "c_") {
		return nil, errors.New("SK 'c_' prefix'i taşımalı (güvenlik)")
	}
	sub, err := normalizeAltDizin(a.AltDizin)
	if err != nil {
		return nil, err
	}
	// Basarisizlikta yan etkileri (DB + cikarilan dosyalar) geri almak icin.
	var kurulumTamam bool
	// Dosya temizligi: kurulum yarida kalirsa cikarilanlar kalmasin, yoksa
	// "hedef dizin bos degil" kontrolu ayni dizini KALICI olarak bloklar.
	var dosyaTemizle func()
	defer func() {
		if kurulumTamam || dosyaTemizle == nil {
			return
		}
		dosyaTemizle()
	}()
	hedef := filepath.Join(a.Domain.DocRoot, sub)

	// Symlink-red guard (path traversal)
	if st, err := os.Lstat(hedef); err == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("hedef bir symlink — güvenlik reddi")
		}
	}
	// Boş değil kontrolü
	if entries, err := os.ReadDir(hedef); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("hedef dizin boş değil: %s (temizle veya farklı alt_dizin)", hedef)
	}
	// 🔴 Symlink denetimi: hedef gercekten docroot altinda mi? (ara bilesenler
	// dahil). Aksi halde asagidaki islemler tenant'in kurdugu bir symlink
	// uzerinden sistem dizinine tasabilir.
	if err := guvenliHedef(a.Domain.DocRoot, hedef); err != nil {
		return nil, err
	}
	// Dizini TENANT olusturur -> bastan doğru sahiplikte olur, root'un
	// tenant yolunda chown yapmasina (ve symlink yarisina) gerek kalmaz.
	if err := tenantKomut(ctx, a.Domain.SK, "mkdir", "-p", hedef); err != nil {
		return nil, fmt.Errorf("dizin olusturulamadi: %w", err)
	}
	dosyaTemizle = func() {
		tCtx, tIptal := context.WithTimeout(context.Background(), 2*time.Minute)
		defer tIptal()
		_ = tenantKomut(tCtx, a.Domain.SK, "rm", "-rf", "--", hedef)
	}

	// İndirme — /tmp'ye
	tmp, err := os.CreateTemp("/var/tmp", "uyg-*.dat")
	if err != nil {
		return nil, err
	}
	tmpYol := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpYol)

	dlCtx, iptal := context.WithTimeout(ctx, 5*time.Minute)
	defer iptal()
	if out, err := exec.CommandContext(dlCtx, "curl", "-fsSL", "--proto", "=https", "--max-filesize", "2000000000", "-o", tmpYol, a.Tarif.IndirmeURL).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("indirme: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	// 🔴 OKUMA IZNI: os.CreateTemp dosyayi 0600 root:root olusturur; asagidaki
	// cikarma `runuser -u <sk>` ile kostugu icin tenant dosyayi ACAMIYORDU
	// ("cannot open zipfile ... Permission denied"). Icerik zaten herkese acik
	// bir dagitim arsivi (WordPress/Grav vb.) — sir degil.
	if err := os.Chmod(tmpYol, 0o644); err != nil {
		return nil, fmt.Errorf("gecici dosya izni: %w", err)
	}

	// Çıkarma — runuser -u <sk> (root DEĞİL — security)
	var cikarCmd *exec.Cmd
	switch a.Tarif.IcerikTuru {
	case "tarball_gz":
		cikarCmd = exec.CommandContext(ctx, "runuser", "-u", a.Domain.SK, "--",
			"tar", "-xzf", tmpYol, "-C", hedef, "--strip-components=1")
	case "tarball_bz2":
		cikarCmd = exec.CommandContext(ctx, "runuser", "-u", a.Domain.SK, "--",
			"tar", "-xjf", tmpYol, "-C", hedef, "--strip-components=1")
	case "zip":
		cikarCmd = exec.CommandContext(ctx, "runuser", "-u", a.Domain.SK, "--",
			"unzip", "-q", tmpYol, "-d", hedef)
	default:
		return nil, fmt.Errorf("icerik_turu: %s", a.Tarif.IcerikTuru)
	}
	if out, err := cikarCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("çıkarma: %s (%w)", strings.TrimSpace(string(out)), err)
	}

	// 🔴 ZIP KOK DIZINI: tar icin `--strip-components=1` var ama `unzip`'in
	// karsiligi YOK. Grav gibi paketler icerigi "grav-admin/" alt klasorunde
	// tutar -> site kokunde bos bir dizin, icinde asil uygulama kalirdi
	// (kullanici /gravtest yerine /gravtest/grav-admin'e gitmek zorunda).
	// Cikarma sonrasi TEK bir dizin olustuysa icerigini bir seviye yukari al.
	if a.Tarif.IcerikTuru == "zip" {
		// 🔴 Duzlestirme TENANT olarak yapilir. Root `os.Rename` kullansaydi,
		// cikarma bitmis (dizin tamamen tenant'in) oldugu icin tenant araya
		// symlink sokup root'a /etc/passwd tasitabilirdi (TOCTOU).
		if err := tekKokDiziniDuzlestirTenant(ctx, a.Domain.SK, hedef); err != nil {
			return nil, fmt.Errorf("arsiv kok dizini duzlestirme: %w", err)
		}
	}

	// DB — MVP: kullanıcı wizard'da manual girer. mysqlOlustur nil ise skip.
	dbAdi := ""
	if a.Tarif.DBGerek && mysqlOlustur != nil {
		// 🔴 AD-UZAYI ZORUNLU: DB adi tenant'in sistem kullanicisiyla
		// baslamak ZORUNDA. Aksi halde musteri baska bir tenant'in DB adini
		// verip `ALTER USER ... IDENTIFIED BY` ile onun parolasini sifirlayabilir
		// (CREATE ... IF NOT EXISTS sessizce no-op olur, sonraki adimlar calisir).
		dbAdi = a.DBAdi
		if dbAdi == "" {
			// Sunucu uretir: <sk>_<kod>_<domainID> — hem sahiplik hem benzersizlik.
			dbAdi = fmt.Sprintf("%s_%s_%d", a.Domain.SK, a.Tarif.Kod, a.Domain.ID)
			if len(dbAdi) > 64 {
				dbAdi = dbAdi[:64]
			}
		}
		if !hesaplar.MusteriDBKimlikGecerli(a.Domain.SK, dbAdi) {
			return nil, fmt.Errorf("db_adi '%s' bu hesaba ait degil — '%s' veya '%s_' ile baslamali",
				dbAdi, a.Domain.SK, a.Domain.SK)
		}
		// Zaten VAR olan bir DB'yi devralma: ayni ad-uzayinda bile olsa
		// mevcut bir DB'nin parolasini sessizce degistirmek yanlis olur.
		if hesaplar.DBVarMi(dbAdi) {
			return nil, fmt.Errorf("veritabani '%s' zaten var — farkli bir ad secin", dbAdi)
		}
		dbSifre := a.DBSifre
		if dbSifre == "" {
			dbSifre = randomStr(24)
			if dbSifre == "" {
				return nil, errors.New("guvenli parola uretilemedi (entropi okunamadi) — kurulum durduruldu")
			}
		}
		// 🔴 domainID SART: db_accounts.domain_id -> domains(id) FK'si var.
		// 0 gecilirse "foreign key constraint fails" ile patlar AMA MariaDB
		// tarafinda veritabani ZATEN OLUSMUS olur -> oksuz DB kalir.
		// Defer'i cagridan ONCE kur: mysqlOlustur yarida patlarsa (MariaDB
		// tarafi olustu ama db_accounts INSERT'i dustu gibi) DB oksuz kalmasin.
		defer func() {
			if kurulumTamam || mysqlDrop == nil {
				return
			}
			_ = mysqlDrop(a.Domain.ID, dbAdi, dbAdi)
		}()
		if err := mysqlOlustur(a.Domain.ID, dbAdi, dbAdi, dbSifre); err != nil {
			return nil, fmt.Errorf("DB: %w", err)
		}
	}

	// Yönetim URL üret
	yonetimURL := "https://" + a.Domain.AlanAdi
	if sub != "" {
		yonetimURL += "/" + sub
	}
	yonetimURL += a.Tarif.YonetimYolu

	// DB kayıt
	dbCtx, iptal2 := context.WithTimeout(ctx, 5*time.Second)
	defer iptal2()
	res, err := db.ExecContext(dbCtx,
		`INSERT INTO cp_uygulama_kurulumlar (domain_id, kod, ad, surum, alt_dizin, yonetim_url, db_adi)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.Domain.ID, a.Tarif.Kod, a.Tarif.Ad, a.Tarif.Surum, sub, yonetimURL, dbAdi)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	kurulumTamam = true

	return &Kayit{
		ID: id, DomainID: a.Domain.ID, Kod: a.Tarif.Kod, Ad: a.Tarif.Ad,
		Surum: a.Tarif.Surum, AltDizin: sub, YonetimURL: yonetimURL, DBAdi: dbAdi,
		CreatedAt: time.Now(),
	}, nil
}

// Liste — bir domain için tüm kurulu uygulamalar.
func Liste(ctx context.Context, db *sql.DB, domainID int64) ([]Kayit, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, domain_id, kod, ad, surum, alt_dizin, yonetim_url, db_adi, created_at
		 FROM cp_uygulama_kurulumlar WHERE domain_id=? ORDER BY id DESC`, domainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Kayit{}
	for rows.Next() {
		var k Kayit
		if err := rows.Scan(&k.ID, &k.DomainID, &k.Kod, &k.Ad, &k.Surum,
			&k.AltDizin, &k.YonetimURL, &k.DBAdi, &k.CreatedAt); err == nil {
			out = append(out, k)
		}
	}
	return out, nil
}

// Sil — dosyaları temizle + DB kayıt sil (opsiyonel DB'yi de drop et).
func Sil(ctx context.Context, db *sql.DB, kayitID int64, domain Domain, mysqlDrop func(domainID int64, dbAd, dbUser string) error) error {
	var sub, dbAdi string
	err := db.QueryRowContext(ctx,
		`SELECT alt_dizin, db_adi FROM cp_uygulama_kurulumlar WHERE id=? AND domain_id=?`,
		kayitID, domain.ID).Scan(&sub, &dbAdi)
	if err != nil {
		return err
	}
	hedef := filepath.Join(domain.DocRoot, sub)

	// 🔴🔴 GUVENLIK — eski kod uc ayri sekilde sömürülebiliyordu:
	//
	//  1) `strings.HasPrefix(hAbs, dcAbs)` AYIRICI DUYARSIZ:
	//     "/home/c_a/public_html_evil" -> "/home/c_a/public_html" ile eslesir.
	//  2) SYMLINK COZULMUYOR: tenant `public_html/srv -> /var/lib` yapar,
	//     alt_dizin="srv/mysql" ile kurulum kaydi olusturur; Sil cagrilinca
	//     `os.RemoveAll` yolu cozer ve ROOT olarak /var/lib/mysql'i siler.
	//  3) `sub == ""` dalinda TUM docroot icerigi siliniyordu — kullanicinin
	//     kendi dosyalari ve DIGER uygulama kurulumlari dahil.
	//
	// Savunma: yolu cozup docroot altinda kaldigini kanitla, ayirici duyarli
	// karsilastir, kok kurulumda TOPLU SILME YAPMA ve silmeyi TENANT olarak
	// calistir (root, tenant'in yazabildigi yolda rm -rf kosmasin).
	if err := guvenliHedef(domain.DocRoot, hedef); err != nil {
		return err
	}
	if sub == "" {
		// Kok kurulum: hangi dosyanin uygulamaya ait oldugunu bilmiyoruz.
		// Docroot'u supurmek kullanicinin kendi verisini de yok eder.
		return errors.New("kok dizine kurulan uygulama otomatik silinemez — " +
			"dosyalari dosya yoneticisinden temizleyip kaydi kaldirin")
	}
	silCtx, silIptal := context.WithTimeout(ctx, 2*time.Minute)
	defer silIptal()
	if err := tenantKomut(silCtx, domain.SK, "rm", "-rf", "--", hedef); err != nil {
		return fmt.Errorf("dosyalar silinemedi: %w", err)
	}

	// DB drop
	if dbAdi != "" && mysqlDrop != nil {
		_ = mysqlDrop(domain.ID, dbAdi, dbAdi)
	}

	// Kayıt sil
	_, err = db.ExecContext(ctx, `DELETE FROM cp_uygulama_kurulumlar WHERE id=?`, kayitID)
	return err
}

func KatalogAra(kod string) *Tarif {
	for i := range Katalog {
		if Katalog[i].Kod == kod {
			t := Katalog[i]
			return &t
		}
	}
	return nil
}

// randomStr — KRIPTO GUVENLI rastgele dize (DB parolasi icin).
//
// 🔴 Eskiden `math/rand` + `time.Now().UnixNano()` tohumu kullaniliyordu.
// Kurulum zamani panel loglarindan/DB kaydindan (created_at) bilindigi icin
// tohum daraltilabilir ve uretilen DB parolasi TAHMIN EDILEBILIR hale gelirdi.
// crypto/rand isletim sisteminin entropi havuzunu kullanir.
func randomStr(n int) string {
	const harfler = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, n)
	if _, err := crand.Read(b); err != nil {
		// Entropi okunamazsa ZAYIF parola uretmektense hata ver:
		// cagiran taraf kurulumu durdurur.
		return ""
	}
	for i := range b {
		b[i] = harfler[int(b[i])%len(harfler)]
	}
	return string(b)
}

// sistemUIDGID — "c_xxx" sistem kullanicisinin uid/gid'sini dondurur.
// os.Chown sayisal id ister; user.Lookup string dondurdugu icin cevrilir.
func sistemUIDGID(sk string) (int, int, error) {
	u, err := user.Lookup(sk)
	if err != nil {
		return 0, 0, err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, err
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return 0, 0, err
	}
	return uid, gid, nil
}

// tekKokDiziniDuzlestirTenant — arsivin tek kok klasoru varsa icerigini bir
// seviye yukari alir. TUM islem `runuser -u <sk>` altinda kosar: root, tenant'in
// yazabildigi bir dizinde asla mv/rm yapmaz (symlink yarisi kapanir).
// `-n` (no-clobber) ile ustune yazma engellenir; nokta ile baslayan dosyalar
// dotglob ile kapsanir.
func tekKokDiziniDuzlestirTenant(ctx context.Context, sk, dizin string) error {
	girisler, err := os.ReadDir(dizin)
	if err != nil {
		return err
	}
	if len(girisler) != 1 || !girisler[0].IsDir() {
		return nil // duzlestirilecek bir sey yok
	}
	ic := filepath.Join(dizin, girisler[0].Name())
	betik := `set -e
shopt -s dotglob nullglob
ic="$1"; dis="$2"
[ -L "$ic" ] && exit 0          # symlink ise DOKUNMA
[ -d "$ic" ] || exit 0
for f in "$ic"/*; do mv -n -- "$f" "$dis"/; done
rmdir -- "$ic" 2>/dev/null || true`
	return tenantKomut(ctx, sk, "bash", "-c", betik, "_", ic, dizin)
}

/* =====================================================================
   SYMLINK GUVENLIGI

   🔴 SALDIRI: `os.Lstat(hedef)` YALNIZ SON bileseni denetler; ara dizinler
   cozulur. Tenant `public_html/a -> /var/spool` symlink'i kurup
   alt_dizin="a/cron" gonderirse:
     - Lstat(son bilesen) symlink degil  -> gecer
     - ReadDir bos                        -> gecer
     - os.Chown symlink'i IZLER           -> /var/spool/cron tenant'a gecer
   Sonuc: root cron dizini tenant'in olur = kok ele gecirme.
   Ayni yolla /etc/sudoers.d, bos herhangi bir sistem dizini.

   SAVUNMA (iki kat):
     a) Yolu TAMAMEN cozup (EvalSymlinks) docroot altinda kaldigini kanitla.
        Ayirici duyarli karsilastirma: "public_html" ile "public_html_x"
        karismasin.
     b) Ayricalikli dosya islemlerini root olarak YAPMA — `runuser -u <sk>`
        altinda calistir. Tenant kendi ev dizini disina zaten yazamaz;
        boylece TOCTOU yarisi kazanilsa bile kazanc yok.
   ===================================================================== */

// yolIcerdeMi — `alt` yolu `kok` altinda mi? Ayirici duyarli.
func yolIcerdeMi(kok, alt string) bool {
	kok = filepath.Clean(kok)
	alt = filepath.Clean(alt)
	if alt == kok {
		return true
	}
	return strings.HasPrefix(alt, kok+string(filepath.Separator))
}

// guvenliHedef — hedefin (symlink'ler cozulmus haliyle) docroot altinda
// kaldigini dogrular. Var olmayan son bilesenler icin en yakin VAR OLAN
// atayi cozer; boylece "henuz olusturulmamis dizin" durumu da kapsanir.
func guvenliHedef(docRoot, hedef string) error {
	kokGercek, err := filepath.EvalSymlinks(docRoot)
	if err != nil {
		return fmt.Errorf("docroot cozulemedi: %w", err)
	}
	// Var olan en yakin atayi bul
	ata := filepath.Clean(hedef)
	for {
		if _, err := os.Lstat(ata); err == nil {
			break
		}
		ust := filepath.Dir(ata)
		if ust == ata {
			return errors.New("guvenlik: yol cozulemedi")
		}
		ata = ust
	}
	ataGercek, err := filepath.EvalSymlinks(ata)
	if err != nil {
		return fmt.Errorf("guvenlik: yol cozulemedi: %w", err)
	}
	if !yolIcerdeMi(kokGercek, ataGercek) {
		return errors.New("guvenlik: hedef docroot disina cikiyor (symlink)")
	}
	return nil
}

// tenantKomut — islemi TENANT olarak calistir. Root'un tenant'in yazabildigi
// bir yolda ayricalikli is yapmasi symlink yarisina acik oldugu icin
// mkdir/mv/rm gibi islemler bu sarmalayicidan gecer.
func tenantKomut(ctx context.Context, sk string, args ...string) error {
	tam := append([]string{"-u", sk, "--"}, args...)
	out, err := exec.CommandContext(ctx, "runuser", tam...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", args[0], strings.TrimSpace(string(out)))
	}
	return nil
}
