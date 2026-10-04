package ajan

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"girginospanel/internal/destek/arac"
	"girginospanel/internal/destek/baglam"
	p "girginospanel/internal/destek/protokol"

	"github.com/go-sql-driver/mysql"
)

// DB'deki bağlantı durumları (protokoldeki karşılıkları durumProtokol'de).
const (
	durumBekliyor = "bekliyor"
	durumAktif    = "aktif"
	durumIptal    = "iptal"
)

// baglanti: destek_baglantilari satırı. Zamanlar unix sn (0 = yok).
type baglanti struct {
	ID               int64
	Kimlik           string
	KodOzet          string
	TgUserID         int64
	TgKullaniciAdi   string
	Rol              p.Rol
	DomainID         int64
	FTPHesapID       int64
	ResellerID       int64
	PanelUserID      int64
	Duzey            p.Duzey
	SureGun          int
	Bitis            int64
	Durum            string
	Olusturan        string
	Onaylayan        string
	OnayAt           int64
	AydinlatmaSurum  string
	AydinlatmaZamani int64
	DurumIzi         string
	IkiFA            bool
	SonKullanim      int64
	IptalNedeni      string
	IptalEden        string
	IptalAt          int64
	OlusturmaAt      int64
}

const bagKolonlar = `id, kimlik, kod_ozet, tg_user_id, tg_kullanici_adi, rol, COALESCE(domain_id,0), COALESCE(ftp_hesap_id,0),
	reseller_id, panel_user_id, duzey, sure_gun, COALESCE(UNIX_TIMESTAMP(bitis),0), durum, olusturan, onaylayan,
	COALESCE(UNIX_TIMESTAMP(onay_at),0), aydinlatma_surum, COALESCE(UNIX_TIMESTAMP(aydinlatma_zamani),0), durum_izi, iki_fa,
	COALESCE(UNIX_TIMESTAMP(son_kullanim),0), iptal_nedeni, iptal_eden, COALESCE(UNIX_TIMESTAMP(iptal_at),0),
	COALESCE(UNIX_TIMESTAMP(olusturma_at),0)`

type tarayici interface{ Scan(dest ...any) error }

func baglantiTara(s tarayici) (*baglanti, error) {
	var b baglanti
	var rol, duzey string
	var bitis, onay, ayd, son, ipt, olus sql.NullFloat64
	if err := s.Scan(&b.ID, &b.Kimlik, &b.KodOzet, &b.TgUserID, &b.TgKullaniciAdi, &rol, &b.DomainID, &b.FTPHesapID,
		&b.ResellerID, &b.PanelUserID, &duzey, &b.SureGun, &bitis, &b.Durum, &b.Olusturan, &b.Onaylayan,
		&onay, &b.AydinlatmaSurum, &ayd, &b.DurumIzi, &b.IkiFA, &son, &b.IptalNedeni, &b.IptalEden, &ipt, &olus); err != nil {
		return nil, err
	}
	b.Rol, b.Duzey = p.Rol(rol), p.Duzey(duzey)
	b.Bitis, b.OnayAt, b.AydinlatmaZamani = int64(bitis.Float64), int64(onay.Float64), int64(ayd.Float64)
	b.SonKullanim, b.IptalAt, b.OlusturmaAt = int64(son.Float64), int64(ipt.Float64), int64(olus.Float64)
	return &b, nil
}

func baglantiKimlikle(ctx context.Context, db *sql.DB, kimlik string) (*baglanti, error) {
	return baglantiTara(db.QueryRowContext(ctx, `SELECT `+bagKolonlar+` FROM destek_baglantilari WHERE kimlik=?`, kimlik))
}

func baglantilarSorgu(ctx context.Context, db *sql.DB, kosul string, args ...any) ([]*baglanti, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+bagKolonlar+` FROM destek_baglantilari WHERE `+kosul, args...) //nolint:gosec // G202: kosul bu paketteki SABİT parçalar; değerler ? ile bağlanır.
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*baglanti
	for rows.Next() {
		b, err := baglantiTara(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// yeniKimlik: 16 bayt rastgele → 22 karakter base64url (protokol kimlik deseni).
func yeniKimlik() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// durumProtokol: DB durumu → protokol durumu.
func durumProtokol(d string) p.BaglantiDurumu {
	switch d {
	case durumAktif:
		return p.BaglantiEtkin
	case durumBekliyor:
		return p.BaglantiOnayBekliyor
	}
	return p.BaglantiIptalEdildi
}

// tgKisa: denetim aktörü için kısa Telegram kimliği (kullanıcı adı ya da id'nin son 6 hanesi).
func tgKisa(ad string, id int64) string {
	if ad != "" {
		return "@" + ad
	}
	s := strconv.FormatInt(id, 10)
	if len(s) > 6 {
		s = "…" + s[len(s)-6:]
	}
	return s
}

// botAktor: denetim aktörü "destek-bot (tg:<kısa>)".
func botAktor(b *baglanti) string {
	return kesRune("destek-bot (tg:"+tgKisa(b.TgKullaniciAdi, b.TgUserID)+")", 64)
}

// ---- kimlik izi (otomatik iptal) ----

type kimlikDurumu struct {
	iz         string
	ikiFA      bool
	resellerID int64
	neden      p.IptalNedeni // "" → kimlik geçerli
}

func izOzet(on, deger string) string {
	h := sha256.Sum256([]byte("destek-iz-v1|" + on + "|" + deger))
	return hex.EncodeToString(h[:])
}

// kimlikIzi: bağlantı sahibinin ŞU ANKİ kimlik durumu. Satır yok / askı / bayi
// askısı → neden dolu. DB hatası → err (çağıran kapalı başarısız davranır).
func kimlikIzi(ctx context.Context, db *sql.DB, rol p.Rol, domainID, ftpID, panelUserID int64) (kimlikDurumu, error) {
	switch rol {
	case p.RolMusteri:
		var askida, rid int64
		var parola, durum string
		err := db.QueryRowContext(ctx, `SELECT COALESCE(d.askida,0), COALESCE(d.reseller_id,0), f.password_md5, COALESCE(f.status,'active')
			FROM domains d JOIN ftp_accounts f ON f.domain_id=d.id WHERE d.id=? AND f.id=?`, domainID, ftpID).
			Scan(&askida, &rid, &parola, &durum)
		if errors.Is(err, sql.ErrNoRows) {
			return kimlikDurumu{neden: p.IptalAlanSilme}, nil
		}
		if err != nil {
			return kimlikDurumu{}, err
		}
		k := kimlikDurumu{iz: izOzet("musteri", parola), resellerID: rid}
		if askida != 0 || durum != "active" {
			k.neden = p.IptalAlanSilme
			return k, nil
		}
		if rid > 0 {
			var bd string
			err := db.QueryRowContext(ctx, `SELECT status FROM users WHERE id=? AND role='reseller'`, rid).Scan(&bd)
			if errors.Is(err, sql.ErrNoRows) || (err == nil && bd != "active") {
				k.neden = p.IptalBayi
				return k, nil
			}
			if err != nil {
				return kimlikDurumu{}, err
			}
		}
		return k, nil
	case p.RolBayi, p.RolKok:
		var r, durum, parola string
		var totp int
		err := db.QueryRowContext(ctx, `SELECT role, COALESCE(status,'active'), COALESCE(password_hash,''), COALESCE(totp_enabled,0)
			FROM users WHERE id=?`, panelUserID).Scan(&r, &durum, &parola, &totp)
		neden := p.IptalBayi
		beklenen := "reseller"
		if rol == p.RolKok {
			neden, beklenen = p.IptalKullanici, "admin"
		}
		if errors.Is(err, sql.ErrNoRows) {
			return kimlikDurumu{neden: neden}, nil
		}
		if err != nil {
			return kimlikDurumu{}, err
		}
		k := kimlikDurumu{iz: izOzet(string(rol), parola), ikiFA: totp == 1}
		if rol == p.RolBayi {
			k.resellerID = panelUserID
		}
		if r != beklenen || durum != "active" {
			k.neden = neden
		}
		return k, nil
	}
	return kimlikDurumu{neden: p.IptalKullanici}, nil
}

// izDenetle: kaydedilmiş iz ile şimdiki durumu karşılaştırır; iptal gerekiyorsa nedeni döner.
func izDenetle(b *baglanti, k kimlikDurumu) p.IptalNedeni {
	if k.neden != "" {
		return k.neden
	}
	if b.Rol == p.RolMusteri && k.resellerID != b.ResellerID {
		return p.IptalAlanSilme // hosting başka bayiye devredildi
	}
	if b.DurumIzi != "" && k.iz != b.DurumIzi {
		switch b.Rol {
		case p.RolMusteri:
			return p.IptalFTPParola
		case p.RolBayi:
			return p.IptalBayi
		default:
			return p.IptalKullanici
		}
	}
	if b.IkiFA && !k.ikiFA {
		return p.IptalIkiFA
	}
	return ""
}

// ---- hesap listesi (oturum kapsamı) ----

// hesaplarOku: bağlantının kapsamındaki hesaplar. Liste arac.HesapListesi'nden
// gelir (yürütücünün beklediği sıra ve süzgeçle BİREBİR aynı: domains.id artan,
// etiket desenine uymayan ad yok; müşteride askıdaki hosting listeye girmez).
func hesaplarOku(ctx context.Context, db *sql.DB, b *baglanti) ([]baglam.Hesap, error) {
	kid, did := b.PanelUserID, int64(0)
	if b.Rol == p.RolMusteri {
		kid, did = 0, b.DomainID
	}
	et, idler, err := arac.HesapListesi(ctx, db, b.Rol, kid, did)
	if err != nil {
		return nil, err
	}
	if len(et) != len(idler) {
		return nil, errors.New("hesap listesi tutarsız")
	}
	out := make([]baglam.Hesap, len(et))
	for i := range et {
		out[i] = baglam.Hesap{Sira: et[i].Sira, Etiket: et[i].Etiket, DomainID: idler[i]}
	}
	return out, nil
}

func hesapListesiOzeti(hs []baglam.Hesap) string {
	h := sha256.New()
	for _, x := range hs {
		h.Write([]byte(strconv.FormatInt(x.DomainID, 10) + ":" + x.Etiket + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func protokolHesaplari(hs []baglam.Hesap) []p.HesapEtiketi {
	out := make([]p.HesapEtiketi, len(hs))
	for i, h := range hs {
		out[i] = p.HesapEtiketi{Sira: h.Sira, Etiket: h.Etiket}
	}
	return out
}

// ---- iş kayıtları ----

var errTekrarIs = errors.New("iş kodu daha önce görüldü")

func isBaslat(ctx context.Context, db *sql.DB, isKodu string, bagID int64, arac string, risk p.Risk, param string) (int64, error) {
	res, err := db.ExecContext(ctx, `INSERT INTO destek_islemleri (is_kodu, baglanti_id, arac, risk, param_maskeli) VALUES (?,?,?,?,?)`,
		isKodu, bagID, kesRune(arac, 40), string(risk), kesRune(param, 1024))
	if err != nil {
		var me *mysql.MySQLError
		if errors.As(err, &me) && me.Number == 1062 {
			return 0, errTekrarIs
		}
		return 0, err
	}
	return res.LastInsertId()
}

func isBitir(ctx context.Context, db *sql.DB, id int64, sonuc string, sure time.Duration) {
	ms := sure.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	_, _ = db.ExecContext(ctx, `UPDATE destek_islemleri SET sonuc=?, sure_ms=? WHERE id=?`, kesRune(sonuc, 32), ms, id)
}

func kesRune(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
