package domains

// dbsil_test.go — DELETE /databases/{dbid}: satırdaki DB kullanıcısı (yedekten kurtarılan
// wp-config ile yazdırılmış olabilir) DROP USER ile YALNIZ güvenle bu domainin hesabıysa
// düşer. Eskiden "başka satırda yok" (paylasim=0) yetiyordu: servis hesabı (roundcube/pma
// benzeri — kendi şemasında yetkili) ya da komşunun kayıtsız kullanıcısı düşürülüyordu.
// Harness: dbonek_test.go (dbo_ortam — geçici MariaDB + mysql sarmalayıcısı).

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/hesaplar"
	"girginospanel/internal/middleware"
)

func (o *dboOrtam) dbSil(t *testing.T, dbid int64) (int, string) {
	t.Helper()
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(aa_sir))
		r.Delete("/databases/{dbid}", o.h.DeleteDatabase)
	})
	req := httptest.NewRequest(http.MethodDelete, "/databases/"+strconv.FormatInt(dbid, 10), nil)
	req.Header.Set("Authorization", "Bearer "+aa_yonetici(t))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func (o *dboOrtam) satirID(t *testing.T, dbAdi string) int64 {
	t.Helper()
	var id int64
	if err := o.h.DB.QueryRow(`SELECT id FROM db_accounts WHERE BINARY db_name=?`, dbAdi).Scan(&id); err != nil {
		t.Fatalf("satır %q: %v", dbAdi, err)
	}
	return id
}

func TestDeleteDatabase_YabanciKullaniciyiDusurmez(t *testing.T) {
	o := dbo_ortam(t)
	on := o.on
	// Servis hesabı benzeri: panelde kayıtsız, kendi (var olan) şemasında yetkili.
	svc, svcPw := on+"svc", "SvcParola12345"
	o.kokExec(t, "CREATE USER '"+svc+"'@'localhost' IDENTIFIED BY '"+svcPw+"'")
	o.kokExec(t, "CREATE DATABASE `"+on+"svcsema`")
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+on+"svcsema`.* TO '"+svc+"'@'localhost'")
	// Saldırganın (domain 1) satırı: kendi DB'si + satıra yazdırılmış servis hesabı.
	ad := on + "_wp"
	o.kokExec(t, "CREATE DATABASE `"+ad+"`")
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+hesaplar.GrantDBKac(ad)+"`.* TO '"+svc+"'@'localhost'")
	if _, err := o.h.DB.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,'x')`, ad, svc); err != nil {
		t.Fatal(err)
	}
	if kod, govde := o.dbSil(t, o.satirID(t, ad)); kod != http.StatusOK {
		t.Fatalf("DB silme %d: %s", kod, govde)
	}
	if !o.kullaniciVar(t, svc) || !o.baglanir(svc, svcPw, on+"svcsema") {
		t.Errorf("servis hesabı DROP USER ile düşürüldü (sunucu geneli kesinti)")
	}
	if o.dbVar(t, ad) {
		t.Errorf("DB düşürülmedi")
	}

	// Meşru: tek DB'lik kendi kullanıcısı düşer.
	ad2, kul2 := on+"_site", on+"_siteu"
	if err := hesaplar.MySQLCreateDB(o.h.DB, 1, ad2, kul2, "SiteParola12345"); err != nil {
		t.Fatal(err)
	}
	if kod, govde := o.dbSil(t, o.satirID(t, ad2)); kod != http.StatusOK {
		t.Fatalf("meşru DB silme %d: %s", kod, govde)
	}
	if o.kullaniciVar(t, kul2) || o.dbVar(t, ad2) {
		t.Errorf("meşru silmede kullanıcı/DB kaldı")
	}
}

// Mevcut-kullanıcı modu: yanıttaki parola kendi hesabının satırından okunmalı. Harf
// duyarsız "db_user=? LIMIT 1" komşunun "<on>_x_db" satırını (saldırganın ayrı hesabı
// "<ON>_X_DB" için) seçip komşunun parolasını yanıtta döndürüyordu.
func TestCreateDatabase_MevcutKullaniciHarfVaryanti(t *testing.T) {
	o := dbo_ortam(t)
	on := o.on
	kurbanPw := "KurbanParola123"
	kurban, _ := o.kurbanKur(t, kurbanPw)
	buyuk, buyukPw := strings.ToUpper(kurban), "BuyukParola123"
	t.Cleanup(func() { _, _ = o.kok.Exec("DROP USER IF EXISTS '" + buyuk + "'@'localhost'") })
	v1 := on + "_v1"
	o.kokExec(t, "CREATE USER '"+buyuk+"'@'localhost' IDENTIFIED BY '"+buyukPw+"'")
	o.kokExec(t, "CREATE DATABASE `"+v1+"`")
	o.kokExec(t, "GRANT ALL PRIVILEGES ON `"+hesaplar.GrantDBKac(v1)+"`.* TO '"+buyuk+"'@'localhost'")
	if _, err := o.h.DB.Exec(`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain) VALUES (1,?,?,?)`, v1, buyuk, buyukPw); err != nil {
		t.Fatal(err)
	}
	kod, govde := o.istek(t, http.MethodPost, "/domains/1/databases",
		map[string]any{"kullanici_tipi": "mevcut", "mevcut_kullanici": buyuk, "db_sonek": "v3"})
	if kod != http.StatusCreated {
		t.Fatalf("mevcut kullanıcıyla DB oluşturma %d: %s", kod, govde)
	}
	if strings.Contains(govde, kurbanPw) {
		t.Errorf("yanıtta komşunun parolası döndü: %s", govde)
	}
	if !strings.Contains(govde, buyukPw) {
		t.Errorf("yanıtta kendi hesabının parolası yok: %s", govde)
	}
}
