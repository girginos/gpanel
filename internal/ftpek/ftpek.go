// Package ftpek — hosting müşterisinin KENDİ alan adı altında BELİRLİ DİZİNLERE ÖZEL
// ek FTP hesapları (listele / oluştur / dizin-etkin değiştir / parola / sil).
//
// Mimari (ADR ftp-ek, 2026-10-05):
//   - Ek hesap, ana hesapla AYNI ftp_accounts tablosunda satırdır (tur='ek'). Pure-FTPd
//     sorguları (GetPW/GetUID/GetGID/GetDir, status='active') DEĞİŞMEZ: home_dir kısıtlı
//     dizindir, ChrootEveryone o dizine hapseder, uid/gid ana hesabınkidir (tenant).
//   - Parola ana hesapla AYNI yoldan saklanır: hesaplar.FTPParolaHash ($6$ SHA-512 crypt;
//     pure-ftpd MYSQLCrypt=crypt). Düz metin hiçbir yere yazılmaz.
//   - Kullanıcı adı = <sistem_kullanici> + "-" + <ek>. sistem_kullanici ^c_[a-z0-9_]+$
//     olduğundan "-" içeremez: bir ek hesap adı HİÇBİR hostingin ana hesap adıyla
//     (FTPCreate'in ON DUPLICATE KEY UPDATE'i ile üstüne yazılabilecek şekilde) çakışamaz.
//   - 🔴 ftp_accounts aynı zamanda MÜŞTERİ PANELİ girişinin kaynağıdır (musteri.Login):
//     ek hesap panele GİRİŞ YAPAMAZ (Login sorgusu tur='ana' ister). Tüm ana-hesap
//     tüketicileri tur='ana' ile ayrılmıştır.
//   - Salt-okuma izni ve hesap bazlı kota pure-ftpd'de (MySQL backend) hesap başına
//     desteklenmediği/yapılandırma değişikliği gerektirdiği için BU SÜRÜMDE YOKTUR
//     (izin her zaman "rw"); karar bekliyor.
package ftpek

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"girginospanel/internal/efektiflimit"
	"girginospanel/internal/files"
	"girginospanel/internal/hesaplar"

	"github.com/go-sql-driver/mysql"
)

const (
	Ayirac           = "-"
	MaksEk           = 20  // alan adı başına varsayılan üst sınır (plan sınırı bundan küçükse o)
	MaksEkAdUzunluk  = 24  // kullanıcı adının serbest kısmı
	MaksDizinUzunluk = 200 // home'a göreli yol (bayt)
	MaksDerinlik     = 12
)

// HomeKok: tenant ev dizinlerinin kökü (testler geçici dizine çevirir).
var HomeKok = "/home"

// Hata: kullanıcıya gösterilecek sade hata + HTTP kodu.
type Hata struct {
	HTTP int
	Msg  string
}

func (h *Hata) Error() string { return h.Msg }

func hata(kod int, msg string) *Hata { return &Hata{HTTP: kod, Msg: msg} }

// Hesap: arayüze dönen ek hesap (parola/hash ASLA yok).
type Hesap struct {
	Kullanici string `json:"kullanici"`
	Ek        string `json:"ek"`    // önek sonrası serbest kısım
	Dizin     string `json:"dizin"` // home'a göreli, "/" ile başlar
	Izin      string `json:"izin"`  // şimdilik hep "rw"
	Durum     string `json:"durum"` // aktif | askida
	Etkin     bool   `json:"etkin"` // müşterinin hesap anahtarı
	Olusturma string `json:"olusturma"`
}

type Liste struct {
	Onek       string  `json:"onek"`
	Hesaplar   []Hesap `json:"hesaplar"`
	Azami      int     `json:"azami"` // bu hostingte açılabilecek en çok ek hesap
	Kullanim   int     `json:"kullanim"`
	HostAskida bool    `json:"host_askida"`
}

var (
	reSK    = regexp.MustCompile(`^c_[a-z0-9_]{1,30}$`)
	reEkAd  = regexp.MustCompile(`^[a-z0-9][a-z0-9_]*$`)
	rezerve = map[string]bool{"root": true, "admin": true, "administrator": true, "ftp": true,
		"anonymous": true, "nobody": true, "www": true, "mail": true}
	// Mutlak yol olarak yazıldığında REDDEDİLEN sistem kökleri (home'a göreli sayılmaz).
	sistemKokleri = map[string]bool{"etc": true, "root": true, "proc": true, "sys": true, "dev": true,
		"var": true, "usr": true, "bin": true, "sbin": true, "boot": true, "lib": true, "lib64": true,
		"opt": true, "run": true, "tmp": true, "mnt": true, "srv": true, "media": true, "home": true}
)

// LimitOku: hostingin plan/üst-yazım max_ftp değeri (0 = sınırsız). Testler değiştirir.
var LimitOku = func(ctx context.Context, db *sql.DB, domainID int64) int {
	el, err := efektiflimit.Oku(ctx, db, domainID)
	if err != nil {
		return 0
	}
	return el.Efektif("max_ftp", 0)
}

type sorgulayici interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// ---- doğrulayıcılar (saf, FS'e dokunmaz) ----

// KullaniciAdi: sk + "-" + ek. ek küçük harfe indirilir, [a-z0-9_] doğrulanır.
func KullaniciAdi(sk, ek string) (kullanici, ekTemiz string, err error) {
	if !reSK.MatchString(sk) {
		return "", "", hata(http.StatusConflict, "hosting hesabı kullanıcısı geçersiz")
	}
	ek = strings.ToLower(strings.TrimSpace(ek))
	if ek == "" {
		return "", "", hata(http.StatusBadRequest, "kullanıcı adı gerekli")
	}
	if len(ek) > MaksEkAdUzunluk {
		return "", "", hata(http.StatusBadRequest, fmt.Sprintf("kullanıcı adı en çok %d karakter olmalı", MaksEkAdUzunluk))
	}
	if !reEkAd.MatchString(ek) {
		return "", "", hata(http.StatusBadRequest, "kullanıcı adı yalnız küçük harf, rakam ve alt çizgi içerebilir")
	}
	if rezerve[ek] {
		return "", "", hata(http.StatusBadRequest, "bu kullanıcı adı ayrılmış, başka bir ad seçin")
	}
	return sk + Ayirac + ek, ek, nil
}

// DizinCoz: kullanıcı girdisini home'a GÖRELİ, temiz, nokta-dizinsiz yola çevirir.
// "../", mutlak sistem yolu, NUL/kontrol/biçim karakteri, nokta ile başlayan bileşen
// (.ssh, .git…), aşırı uzun/derin yol ve ev dizininin kendisi REDDEDİLİR (sessizce
// düzeltilmez). Çözülmüş yol "a/b" biçimindedir (başında "/" yok).
func DizinCoz(girdi, sk string) (string, error) {
	g := strings.TrimSpace(girdi)
	if g == "" {
		return "", hata(http.StatusBadRequest, "dizin seçin")
	}
	if len(g) > MaksDizinUzunluk+len(HomeKok)+len(sk)+2 || !utf8.ValidString(g) {
		return "", hata(http.StatusBadRequest, "dizin yolu geçersiz ya da çok uzun")
	}
	for _, r := range g {
		if r == 0 || r == '\\' || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "", hata(http.StatusBadRequest, "dizin yolu geçersiz karakter içeriyor")
		}
	}
	home := HomeKok + "/" + sk
	switch {
	case g == home:
		g = ""
	case strings.HasPrefix(g, home+"/"):
		g = strings.TrimPrefix(g, home+"/")
	case strings.HasPrefix(g, "/"):
		ilk := strings.SplitN(strings.TrimLeft(g, "/"), "/", 2)[0]
		if sistemKokleri[strings.ToLower(ilk)] {
			return "", hata(http.StatusBadRequest, "dizin yalnız kendi hesabınızın altında olabilir")
		}
	}
	var parca []string
	for _, p := range strings.Split(g, "/") {
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, ".") {
			return "", hata(http.StatusBadRequest, "nokta ile başlayan ya da üst dizin (..) bileşenleri kullanılamaz")
		}
		if p != strings.TrimSpace(p) || strings.ContainsAny(p, "~*?\"<>|:") {
			return "", hata(http.StatusBadRequest, "dizin adı geçersiz karakter içeriyor")
		}
		parca = append(parca, p)
	}
	if len(parca) == 0 {
		return "", hata(http.StatusBadRequest, "ev dizininin kendisi seçilemez, bir alt klasör seçin")
	}
	if len(parca) > MaksDerinlik {
		return "", hata(http.StatusBadRequest, "dizin yolu çok derin")
	}
	rel := strings.Join(parca, "/")
	if len(rel) > MaksDizinUzunluk {
		return "", hata(http.StatusBadRequest, "dizin yolu çok uzun")
	}
	return rel, nil
}

// DizinHazirla: rel'i FS'te symlink'siz doğrular; yoksa ve olustur ise oluşturur
// (tenant sahipli, 0755). Mutlak yol döner.
func DizinHazirla(sk, rel string, olustur bool) (string, error) {
	home := HomeKok + "/" + sk
	var yok bool
	ok, err := files.DirBeneath(home, rel)
	switch {
	case err == nil && !ok:
		return "", hata(http.StatusBadRequest, "seçilen yol bir klasör değil")
	case err == nil:
	case os.IsNotExist(err):
		yok = true
	default:
		return "", hata(http.StatusBadRequest, "dizin güvenli değil (sembolik bağ içeriyor) ya da erişilemiyor")
	}
	if yok {
		if !olustur {
			return "", hata(http.StatusBadRequest, "klasör bulunamadı; oluşturma seçeneğini işaretleyin")
		}
		if err := files.MkdirAllBeneath(home, rel, sk); err != nil {
			return "", hata(http.StatusInternalServerError, "klasör oluşturulamadı")
		}
	}
	return home + "/" + rel, nil
}

// ---- hız sınırı (alan adı başına kayan pencere) ----

const (
	HizMaks    = 30
	HizPencere = 10 * time.Minute
)

var (
	hizMu  sync.Mutex
	hizLog = map[int64][]time.Time{}
	Simdi  = time.Now // testte sahte saat
)

// HizIzin: alan adı başına yazma işlemi sayacı. (izinli, beklenecek süre)
func HizIzin(domainID int64) (bool, time.Duration) {
	simdi := Simdi()
	hizMu.Lock()
	defer hizMu.Unlock()
	var tut []time.Time
	for _, t := range hizLog[domainID] {
		if simdi.Sub(t) < HizPencere {
			tut = append(tut, t)
		}
	}
	if len(tut) >= HizMaks {
		hizLog[domainID] = tut
		return false, HizPencere - simdi.Sub(tut[0])
	}
	hizLog[domainID] = append(tut, simdi)
	return true, 0
}

// HizSifirla: yalnız testler.
func HizSifirla() {
	hizMu.Lock()
	hizLog = map[int64][]time.Time{}
	hizMu.Unlock()
}

// ---- DB işlemleri ----

type domainBilgi struct {
	SK       string
	Demo     bool
	Askida   bool
	UID      int
	GID      int
	AnaDurum string
}

func domainOku(ctx context.Context, q sorgulayici, id int64, kilit bool) (domainBilgi, error) {
	var d domainBilgi
	var demo, askida int
	sorgu := `SELECT sistem_kullanici, COALESCE(is_demo,0), COALESCE(askida,0) FROM domains WHERE id=?`
	if kilit {
		sorgu += ` FOR UPDATE`
	}
	err := q.QueryRowContext(ctx, sorgu, id).Scan(&d.SK, &demo, &askida)
	if errors.Is(err, sql.ErrNoRows) {
		return d, hata(http.StatusNotFound, "domain bulunamadı")
	}
	if err != nil {
		return d, hata(http.StatusInternalServerError, "domain okunamadı")
	}
	d.Demo, d.Askida = demo == 1, askida == 1
	return d, nil
}

// anaOku: ana hesabın uid/gid ve durumu.
func anaOku(ctx context.Context, q sorgulayici, id int64, sk string, d *domainBilgi) error {
	err := q.QueryRowContext(ctx,
		`SELECT uid_n, gid_n, status FROM ftp_accounts
		  WHERE domain_id=? AND tur='ana' ORDER BY (username=?) DESC, id LIMIT 1`, id, sk).
		Scan(&d.UID, &d.GID, &d.AnaDurum)
	if errors.Is(err, sql.ErrNoRows) {
		return hata(http.StatusConflict, "bu hostingin ana FTP hesabı bulunamadı")
	}
	if err != nil {
		return hata(http.StatusInternalServerError, "ana FTP hesabı okunamadı")
	}
	if d.UID <= 0 || d.GID <= 0 {
		// uid/gid 0 (root) ile bir FTP hesabı ASLA açılmaz.
		return hata(http.StatusConflict, "ana FTP hesabının sistem kimliği geçersiz")
	}
	return nil
}

// durumHesapla: pure-ftpd'nin okuduğu status değeri. Etkin DEĞİL ya da hosting/ana hesap
// askıdaysa 'suspended' — askıdaki hosting'te hiçbir ek hesap açılamaz.
func durumHesapla(etkin bool, d domainBilgi) string {
	if etkin && !d.Askida && d.AnaDurum == "active" {
		return "active"
	}
	return "suspended"
}

func azami(ctx context.Context, db *sql.DB, domainID int64) int {
	a := MaksEk
	if m := LimitOku(ctx, db, domainID); m > 0 {
		// max_ftp ana hesabı da sayar (kaynak özeti: kullanım = tüm ftp_accounts satırları).
		if kalan := m - 1; kalan < a {
			a = kalan
		}
	}
	if a < 0 {
		a = 0
	}
	return a
}

func ekSay(ctx context.Context, q sorgulayici, id int64) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM ftp_accounts WHERE domain_id=? AND tur='ek'`, id).Scan(&n)
	return n, err
}

func limitHatasi(az int) *Hata {
	if az == 0 {
		return hata(http.StatusConflict, "paketinizde ek FTP hesabı hakkı yok")
	}
	return hata(http.StatusConflict, fmt.Sprintf("en çok %d ek FTP hesabı açabilirsiniz", az))
}

// Listele: hostingin ek hesapları (parola/hash YOK).
func Listele(ctx context.Context, db *sql.DB, domainID int64) (*Liste, error) {
	d, err := domainOku(ctx, db, domainID, false)
	if err != nil {
		return nil, err
	}
	l := &Liste{Onek: d.SK + Ayirac, Hesaplar: []Hesap{}, Azami: azami(ctx, db, domainID), HostAskida: d.Askida}
	rows, err := db.QueryContext(ctx,
		`SELECT username, home_dir, status, ek_etkin, COALESCE(DATE_FORMAT(created_at,'%Y-%m-%d %H:%i'),'')
		   FROM ftp_accounts WHERE domain_id=? AND tur='ek' ORDER BY id`, domainID)
	if err != nil {
		return nil, hata(http.StatusInternalServerError, "hesaplar okunamadı")
	}
	defer rows.Close()
	home := HomeKok + "/" + d.SK
	for rows.Next() {
		var h Hesap
		var dizin, durum string
		var etkin int
		if err := rows.Scan(&h.Kullanici, &dizin, &durum, &etkin, &h.Olusturma); err != nil {
			return nil, hata(http.StatusInternalServerError, "hesaplar okunamadı")
		}
		h.Ek = strings.TrimPrefix(h.Kullanici, d.SK+Ayirac)
		h.Dizin = "/" + strings.TrimPrefix(dizin, home+"/")
		h.Izin = "rw"
		h.Etkin = etkin == 1
		h.Durum = "askida"
		if durum == "active" {
			h.Durum = "aktif"
		}
		l.Hesaplar = append(l.Hesaplar, h)
	}
	if err := rows.Err(); err != nil {
		return nil, hata(http.StatusInternalServerError, "hesaplar okunamadı")
	}
	l.Kullanim = len(l.Hesaplar)
	return l, nil
}

type OlusturGirdi struct {
	DomainID     int64
	Ek           string
	Dizin        string
	DizinOlustur bool
	Parola       string // düz; yalnız hash'lenip yazılır
}

// Olustur: hesabı ekler. Eşzamanlılık: domains satırı FOR UPDATE ile serileşir (limit
// yarışı yok), UNIQUE(username) çakışmayı 409'a çevirir.
func Olustur(ctx context.Context, db *sql.DB, g OlusturGirdi) (*Hesap, error) {
	d, err := domainOku(ctx, db, g.DomainID, false)
	if err != nil {
		return nil, err
	}
	if d.Demo {
		return nil, hata(http.StatusForbidden, "demo abonelikte ek FTP hesabı açılamaz")
	}
	if d.Askida {
		return nil, hata(http.StatusConflict, "hesap askıda; ek FTP hesabı oluşturulamaz")
	}
	kullanici, ek, err := KullaniciAdi(d.SK, g.Ek)
	if err != nil {
		return nil, err
	}
	rel, err := DizinCoz(g.Dizin, d.SK)
	if err != nil {
		return nil, err
	}
	if !hesaplar.ParolaGecerli(g.Parola) {
		return nil, hata(http.StatusBadRequest, "parola geçersiz karakter (satır sonu) içeriyor")
	}
	az := azami(ctx, db, g.DomainID)
	if n, e := ekSay(ctx, db, g.DomainID); e != nil {
		return nil, hata(http.StatusInternalServerError, "hesaplar okunamadı")
	} else if n >= az {
		return nil, limitHatasi(az) // klasör oluşturmadan önce ucuz ön kontrol
	}
	hash := hesaplar.FTPParolaHash(g.Parola)
	if hash == "" {
		return nil, hata(http.StatusInternalServerError, "parola işlenemedi")
	}
	if err := anaOku(ctx, db, g.DomainID, d.SK, &d); err != nil {
		return nil, err
	}
	abs, err := DizinHazirla(d.SK, rel, g.DizinOlustur)
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, hata(http.StatusInternalServerError, "işlem başlatılamadı")
	}
	defer func() { _ = tx.Rollback() }()
	// Kilit altında yeniden oku: askı/limit kilit anındaki değerle karara bağlanır.
	d2, err := domainOku(ctx, tx, g.DomainID, true)
	if err != nil {
		return nil, err
	}
	if d2.Askida {
		return nil, hata(http.StatusConflict, "hesap askıda; ek FTP hesabı oluşturulamaz")
	}
	if err := anaOku(ctx, tx, g.DomainID, d.SK, &d2); err != nil {
		return nil, err
	}
	if n, e := ekSay(ctx, tx, g.DomainID); e != nil {
		return nil, hata(http.StatusInternalServerError, "hesaplar okunamadı")
	} else if n >= az {
		return nil, limitHatasi(az)
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO ftp_accounts (domain_id, username, password_md5, home_dir, uid_n, gid_n, status, tur, ek_etkin)
		 VALUES (?,?,?,?,?,?,?, 'ek', 1)`,
		g.DomainID, kullanici, hash, abs, d2.UID, d2.GID, durumHesapla(true, d2))
	if err != nil {
		var me *mysql.MySQLError
		if errors.As(err, &me) && me.Number == 1062 {
			return nil, hata(http.StatusConflict, "bu kullanıcı adı zaten kullanımda")
		}
		return nil, hata(http.StatusInternalServerError, "hesap oluşturulamadı")
	}
	if err := tx.Commit(); err != nil {
		return nil, hata(http.StatusInternalServerError, "hesap oluşturulamadı")
	}
	return &Hesap{Kullanici: kullanici, Ek: ek, Dizin: "/" + rel, Izin: "rw", Durum: "aktif", Etkin: true}, nil
}

type GuncelleGirdi struct {
	DomainID     int64
	Kullanici    string
	Dizin        *string
	DizinOlustur bool
	Etkin        *bool
}

// Guncelle: dizin ve/veya etkin.
func Guncelle(ctx context.Context, db *sql.DB, g GuncelleGirdi) (*Hesap, error) {
	if g.Dizin == nil && g.Etkin == nil {
		return nil, hata(http.StatusBadRequest, "değiştirilecek bir alan yok")
	}
	d, err := domainOku(ctx, db, g.DomainID, false)
	if err != nil {
		return nil, err
	}
	if d.Demo {
		return nil, hata(http.StatusForbidden, "demo abonelikte ek FTP hesabı değiştirilemez")
	}
	var abs string
	if g.Dizin != nil {
		rel, err := DizinCoz(*g.Dizin, d.SK)
		if err != nil {
			return nil, err
		}
		if abs, err = DizinHazirla(d.SK, rel, g.DizinOlustur); err != nil {
			return nil, err
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, hata(http.StatusInternalServerError, "işlem başlatılamadı")
	}
	defer func() { _ = tx.Rollback() }()
	d2, err := domainOku(ctx, tx, g.DomainID, true)
	if err != nil {
		return nil, err
	}
	var eski string
	var etkin int
	err = tx.QueryRowContext(ctx,
		`SELECT home_dir, ek_etkin FROM ftp_accounts WHERE domain_id=? AND tur='ek' AND BINARY username=? FOR UPDATE`,
		g.DomainID, g.Kullanici).Scan(&eski, &etkin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, hata(http.StatusNotFound, "FTP hesabı bulunamadı")
	}
	if err != nil {
		return nil, hata(http.StatusInternalServerError, "hesap okunamadı")
	}
	if g.Etkin != nil {
		etkin = 0
		if *g.Etkin {
			etkin = 1
		}
	}
	if abs == "" {
		abs = eski
	}
	if err := anaOku(ctx, tx, g.DomainID, d.SK, &d2); err != nil {
		return nil, err
	}
	durum := durumHesapla(etkin == 1, d2)
	if _, err := tx.ExecContext(ctx,
		`UPDATE ftp_accounts SET home_dir=?, ek_etkin=?, status=? WHERE domain_id=? AND tur='ek' AND BINARY username=?`,
		abs, etkin, durum, g.DomainID, g.Kullanici); err != nil {
		return nil, hata(http.StatusInternalServerError, "hesap güncellenemedi")
	}
	if err := tx.Commit(); err != nil {
		return nil, hata(http.StatusInternalServerError, "hesap güncellenemedi")
	}
	ad := "askida"
	if durum == "active" {
		ad = "aktif"
	}
	return &Hesap{Kullanici: g.Kullanici, Ek: strings.TrimPrefix(g.Kullanici, d.SK+Ayirac),
		Dizin: "/" + strings.TrimPrefix(abs, HomeKok+"/"+d.SK+"/"), Izin: "rw", Durum: ad, Etkin: etkin == 1}, nil
}

// ParolaDegistir: yalnız bu hostingin ek hesabı (başkasınınki/ana hesap → 404).
func ParolaDegistir(ctx context.Context, db *sql.DB, domainID int64, kullanici, parola string) error {
	if !hesaplar.ParolaGecerli(parola) {
		return hata(http.StatusBadRequest, "parola geçersiz karakter (satır sonu) içeriyor")
	}
	d, err := domainOku(ctx, db, domainID, false)
	if err != nil {
		return err
	}
	if d.Demo {
		return hata(http.StatusForbidden, "demo abonelikte ek FTP hesabı değiştirilemez")
	}
	hash := hesaplar.FTPParolaHash(parola)
	if hash == "" {
		return hata(http.StatusInternalServerError, "parola işlenemedi")
	}
	res, err := db.ExecContext(ctx,
		`UPDATE ftp_accounts SET password_md5=? WHERE domain_id=? AND tur='ek' AND BINARY username=?`,
		hash, domainID, kullanici)
	if err != nil {
		return hata(http.StatusInternalServerError, "parola değiştirilemedi")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return hata(http.StatusNotFound, "FTP hesabı bulunamadı")
	}
	return nil
}

// Sil: yalnız bu hostingin ek hesabı. Klasör ve içindeki dosyalara DOKUNULMAZ.
func Sil(ctx context.Context, db *sql.DB, domainID int64, kullanici string) error {
	res, err := db.ExecContext(ctx,
		`DELETE FROM ftp_accounts WHERE domain_id=? AND tur='ek' AND BINARY username=?`, domainID, kullanici)
	if err != nil {
		return hata(http.StatusInternalServerError, "hesap silinemedi")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return hata(http.StatusNotFound, "FTP hesabı bulunamadı")
	}
	return nil
}
