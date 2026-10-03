package arac

import (
	"context"
	"database/sql"
	"errors"
	"regexp"

	"girginospanel/internal/destek/protokol"
	"girginospanel/internal/kiraci"
	"girginospanel/internal/middleware"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\x1b[()][A-Za-z0-9]|\x1b[=>]`)

// hesap — kapsam denetiminden geçmiş hosting hesabı.
type hesap struct {
	ID         int64
	AlanAdi    string
	SK         string
	ResellerID int64
	Askida     bool
	Demo       bool
}

// cagri — tek araç çağrısının bağlamı.
type cagri struct {
	y *yurutucu
	k *Kimlik
	b protokol.OturumBaglami
	g *protokol.AracGirdisi
	h *hesap // hesap parametresi olmayan araçta nil
}

// cikti — aracın ham (maskelenmemiş olabilir) çıktısı; sonucKur maskeler ve keser.
type cikti struct {
	ozet     string
	satirlar []string
	kesildi  bool
}

// kullaniciDogrula — bayi/kök hesabı HER iş emrinde DB'den: rol uyuşmalı, etkin
// olmalı. (Middleware aynı denetimi süreç içi istekte bir kez daha yapar.)
func (y *yurutucu) kullaniciDogrula(ctx context.Context, k *Kimlik) error {
	if k.Rol == protokol.RolMusteri {
		if k.KullaniciID != 0 {
			return hataf(protokol.HataYetki, "müşteri kimliğinde kullanıcı numarası olamaz")
		}
		return nil
	}
	beklenen := "reseller"
	if k.Rol == protokol.RolKok {
		beklenen = "admin"
	}
	if k.KullaniciID <= 0 {
		return hataf(protokol.HataYetki, "kullanıcı numarası yok")
	}
	var rol, durum string
	err := y.db.QueryRowContext(ctx, `SELECT role, status FROM users WHERE id=?`, k.KullaniciID).Scan(&rol, &durum)
	if errors.Is(err, sql.ErrNoRows) {
		return hataf(protokol.HataYetki, "panel hesabı yok")
	}
	if err != nil {
		return hataf(protokol.HataMesgul, "hesap durumu doğrulanamadı")
	}
	if rol != beklenen {
		return hataf(protokol.HataYetki, "panel hesabının rolü uyuşmuyor")
	}
	if durum != "active" {
		return hataf(protokol.HataYetki, "panel hesabı askıda")
	}
	return nil
}

// hesapKapsam — sıra numarasının işaret ettiği domain_id için AÇIK kapsam denetimi
// (DomainSahibiMi / YonetimSahibi eşdeğeri, DB'den):
//   - kayıt var ve alan adı oturum etiketiyle AYNI (silinip yeniden kullanılan
//     kimlik ya da merkezin uydurduğu etiket → kapsam_disi)
//   - müşteri: hesap askıda değil
//   - bayi: domains.reseller_id == kendi users.id
//   - kök: her hesap
func (y *yurutucu) hesapKapsam(ctx context.Context, k *Kimlik, id int64, etiket string) (*hesap, error) {
	if id <= 0 {
		return nil, hataf(protokol.HataKapsam, "hesap yok")
	}
	h := &hesap{ID: id}
	var askida, demo int
	err := y.db.QueryRowContext(ctx,
		`SELECT alan_adi, sistem_kullanici, reseller_id, COALESCE(askida,0), COALESCE(is_demo,0) FROM domains WHERE id=?`, id).
		Scan(&h.AlanAdi, &h.SK, &h.ResellerID, &askida, &demo)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, hataf(protokol.HataKapsam, "hesap bulunamadı")
	}
	if err != nil {
		return nil, hataf(protokol.HataMesgul, "hesap okunamadı")
	}
	h.Askida, h.Demo = askida == 1, demo == 1
	if h.AlanAdi != etiket {
		return nil, hataf(protokol.HataKapsam, "hesap kaydı oturumdakiyle uyuşmuyor")
	}
	if !kiraci.GecerliSK(h.SK) {
		return nil, hataf(protokol.HataKapsam, "hesabın sistem kullanıcısı geçersiz")
	}
	switch k.Rol {
	case protokol.RolMusteri:
		if h.Askida {
			return nil, hataf(protokol.HataKapsam, "hesap askıda")
		}
	case protokol.RolBayi:
		if k.KullaniciID <= 0 || h.ResellerID != k.KullaniciID {
			return nil, hataf(protokol.HataKapsam, "hesap bu bayiye ait değil")
		}
	case protokol.RolKok:
	default:
		return nil, hataf(protokol.HataYetki, "rol")
	}
	return h, nil
}

// kokZorunlu — yalnız kök araçlarının gövdesindeki İKİNCİ denetim (katalog
// RolIzinli'ye ek; katalog yanlışlıkla genişlerse de kök dışı geçemez).
func (c *cagri) kokZorunlu() error {
	if c.k.Rol != protokol.RolKok || c.b.Rol != protokol.RolKok {
		return hataf(protokol.HataYetki, "yalnız kök oturumu")
	}
	return nil
}

// destekKimligi — süreç içi istek kimliği. Müşteride hesap zorunludur
// (MusteriClaims.DomainID = hesap).
func (c *cagri) destekKimligi(h *hesap) (middleware.DestekKimlik, error) {
	dk := middleware.DestekKimlik{TgKisa: c.k.TgKisa}
	switch c.k.Rol {
	case protokol.RolMusteri:
		if h == nil {
			return dk, hataf(protokol.HataKapsam, "hesap gerekli")
		}
		dk.Rol, dk.DomainID, dk.AlanAdi = middleware.DestekMusteri, h.ID, h.AlanAdi
	case protokol.RolBayi:
		dk.Rol, dk.KullaniciID, dk.Kullanici = middleware.DestekBayi, c.k.KullaniciID, c.k.Kullanici
	case protokol.RolKok:
		dk.Rol, dk.KullaniciID, dk.Kullanici = middleware.DestekKok, c.k.KullaniciID, c.k.Kullanici
	default:
		return dk, hataf(protokol.HataYetki, "rol")
	}
	return dk, nil
}

// HesapListesi — ajanın bağlantı kaydından OturumBaglami.Hesaplar + Kimlik.HesapIDleri
// kurması için yardımcı (aynı sıra). Müşteri: yalnız musteriDomainID; bayi: kendi
// hesapları; kök: tüm hesaplar. Sıra domains.id artan; en çok protokol.EnCokHesap.
func HesapListesi(ctx context.Context, db *sql.DB, rol protokol.Rol, kullaniciID, musteriDomainID int64) ([]protokol.HesapEtiketi, []int64, error) {
	var (
		rows *sql.Rows
		err  error
	)
	const sec = `SELECT id, alan_adi FROM domains `
	switch rol {
	case protokol.RolMusteri:
		rows, err = db.QueryContext(ctx, sec+`WHERE id=? AND COALESCE(askida,0)=0`, musteriDomainID)
	case protokol.RolBayi:
		if kullaniciID <= 0 {
			return nil, nil, hataf(protokol.HataYetki, "bayi numarası yok")
		}
		rows, err = db.QueryContext(ctx, sec+`WHERE reseller_id=? ORDER BY id LIMIT ?`, kullaniciID, protokol.EnCokHesap)
	case protokol.RolKok:
		rows, err = db.QueryContext(ctx, sec+`ORDER BY id LIMIT ?`, protokol.EnCokHesap)
	default:
		return nil, nil, hataf(protokol.HataYetki, "rol")
	}
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var et []protokol.HesapEtiketi
	var idler []int64
	for rows.Next() {
		var id int64
		var ad string
		if err := rows.Scan(&id, &ad); err != nil {
			return nil, nil, err
		}
		if !etiketRe.MatchString(ad) {
			continue // protokol etiket desenine uymayan ad oturuma girmez
		}
		idler = append(idler, id)
		et = append(et, protokol.HesapEtiketi{Sira: len(idler), Etiket: ad})
	}
	return et, idler, rows.Err()
}

var etiketRe = regexp.MustCompile(`^[a-z0-9.-]{1,253}$`)
