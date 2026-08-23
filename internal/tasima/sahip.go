// Reseller/müşteri (sahiplik) taşıma: kaynaktaki reseller + client hesaplarını
// GVM'de oluşturur ve domainleri doğru sahibe atar. Kesif KesfetSahipler ile
// yakalar; burada idempotent oluşturulur (login/eposta varsa yeniden kullanılır).
package tasima

import (
	"context"
	"database/sql"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"girginospanel/internal/hesaplar"
)

// sahipHedef — bir kaynak sahip login'inin GVM'deki karşılığı.
type sahipHedef struct {
	ResellerID int64 // users.id (reseller); 0 = yok
	CustomerID int64 // customers.id; 0 = yok
}

// UretilenKimlik — taşımada OLUŞTURULAN hesabın admin'e raporlanacak bilgisi.
// Parola yalnız yeni oluşturulan reseller'lar için doldurulur (Plesk parolaları
// düz-metin alınamaz → yeni üretilir).
type UretilenKimlik struct {
	Tip    string `json:"tip"` // reseller | müşteri
	Ad     string `json:"ad"`
	Login  string `json:"login"`
	Eposta string `json:"eposta"`
	Parola string `json:"parola,omitempty"`
}

// sahipleriHazirla — kaynak sahipleri (reseller önce, sonra client) GVM'de
// idempotent oluşturur. Döner: login→hedef eşlemesi + üretilen kimlik listesi.
// Hata halinde o ana kadar oluşturulanlar kalır (idempotent → tekrar çalıştırılabilir).
func sahipleriHazirla(ctx context.Context, db *sql.DB, sahipler []KaynakSahip) (map[string]sahipHedef, []UretilenKimlik, error) {
	esle := map[string]sahipHedef{}
	var kimlikler []UretilenKimlik

	// 1) RESELLER'lar önce (client'ların üst reseller'ı olabilir).
	for _, s := range sahipler {
		if s.Tip != "reseller" || !reHesap.MatchString(s.Login) {
			continue
		}
		// Zaten var mı? (username)
		var uid int64
		if db.QueryRowContext(ctx, `SELECT id FROM users WHERE username=? AND role='reseller'`, s.Login).Scan(&uid) == nil && uid > 0 {
			esle[s.Login] = sahipHedef{ResellerID: uid}
			continue
		}
		planID, err := resellerPlanBulVeyaOlustur(ctx, db, s)
		if err != nil {
			return esle, kimlikler, fmt.Errorf("reseller planı (%s): %w", s.Login, err)
		}
		parola := hesaplar.RandomParola(16)
		hash, err := bcrypt.GenerateFromPassword([]byte(parola), 12)
		if err != nil {
			return esle, kimlikler, err
		}
		ad := s.Ad
		if ad == "" {
			ad = s.Login
		}
		res, err := db.ExecContext(ctx,
			`INSERT INTO users(username, password_hash, role, full_name, status, max_domain, max_disk_mb, max_trafik_mb,
			   asim_ilkesi, asim_bildirim, fazla_satis, reseller_plan_id)
			 VALUES(?,?, 'reseller', ?, 'active', ?, ?, ?, 'disk_trafik', 1, 0, ?)`,
			s.Login, string(hash), ad, s.MaxDomain, s.DiskMB, s.TrafikMB, planID)
		if err != nil {
			return esle, kimlikler, fmt.Errorf("reseller (%s): %w", s.Login, err)
		}
		rid, _ := res.LastInsertId()
		esle[s.Login] = sahipHedef{ResellerID: rid}
		kimlikler = append(kimlikler, UretilenKimlik{Tip: "reseller", Ad: ad, Login: s.Login, Eposta: s.Eposta, Parola: parola})
	}

	// 2) CLIENT'lar → müşteri (customers). Üst reseller'ı varsa reseller_id ile eşle.
	for _, s := range sahipler {
		if s.Tip != "client" || !reHesap.MatchString(s.Login) {
			continue
		}
		var resellerID int64
		if s.Reseller != "" {
			resellerID = esle[s.Reseller].ResellerID
		}
		// Zaten var mı? (eposta ile — customers'ta login yok)
		var cid int64
		if s.Eposta != "" {
			_ = db.QueryRowContext(ctx, `SELECT id FROM customers WHERE eposta=? LIMIT 1`, s.Eposta).Scan(&cid)
		}
		if cid == 0 {
			ad := s.Ad
			if ad == "" {
				ad = s.Login
			}
			res, err := db.ExecContext(ctx,
				`INSERT INTO customers(ad, eposta, durum, notlar) VALUES(?,?, 'aktif', ?)`,
				ad, s.Eposta, "Site taşımadan: "+s.Login)
			if err != nil {
				return esle, kimlikler, fmt.Errorf("müşteri (%s): %w", s.Login, err)
			}
			cid, _ = res.LastInsertId()
			kimlikler = append(kimlikler, UretilenKimlik{Tip: "müşteri", Ad: ad, Login: s.Login, Eposta: s.Eposta})
		}
		esle[s.Login] = sahipHedef{CustomerID: cid, ResellerID: resellerID}
	}
	return esle, kimlikler, nil
}

// resellerPlanBulVeyaOlustur — kaynak reseller için GVM reseller planı (idempotent
// ad "Taşınan-<login>"). Limitler kaynaktan (0 = sınırsız).
func resellerPlanBulVeyaOlustur(ctx context.Context, db *sql.DB, s KaynakSahip) (int64, error) {
	ad := "Taşınan-" + s.Login
	var id int64
	if db.QueryRowContext(ctx, `SELECT id FROM reseller_plans WHERE ad=?`, ad).Scan(&id) == nil && id > 0 {
		return id, nil
	}
	res, err := db.ExecContext(ctx,
		`INSERT INTO reseller_plans(ad, aciklama, max_domain, max_disk_mb, max_trafik_mb,
		   mail_max_email, mail_saatlik_limit, mail_kutu_kota_mb, fiyat_kurus,
		   asim_ilkesi, asim_bildirim, fazla_satis, varsayilan)
		 VALUES(?,?,?,?,?, 0, 0, 0, 0, 'disk_trafik', 1, 0, 0)`,
		ad, "Site taşımadan otomatik oluşturuldu ("+s.Login+")", s.MaxDomain, s.DiskMB, s.TrafikMB)
	if err != nil {
		return 0, err
	}
	id, _ = res.LastInsertId()
	return id, nil
}
