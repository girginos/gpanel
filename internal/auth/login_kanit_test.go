package auth

// login_kanit_test.go — girişte kapı sırası, kapı kanıtı ve token kapısı (ADR-GP-001).
// Parola doğrulaması ve DB sahte (sahtedb): hangi sorguya İNİLDİĞİ ölçülür.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"girginospanel/internal/kapi"
	"girginospanel/internal/sahtedb"
)

// girisOrtami: kapı + root parola sonucu + kanıt kaydedici dikişleri ve sahte DB.
func girisOrtami(t *testing.T, kapiAdi string, parolaDogru bool) (*sahtedb.DB, *Handlers, *[]string) {
	t.Helper()
	kapidanGelsin(t, kapiAdi)
	eskiParola, eskiKanit := rootParolaKontrol, kapiGirisKaydet
	t.Cleanup(func() { rootParolaKontrol, kapiGirisKaydet = eskiParola, eskiKanit })
	rootParolaKontrol = func(string) bool { return parolaDogru }
	kanitlar := &[]string{}
	kapiGirisKaydet = func(_, rol string, _ *http.Request, _ int64) error {
		*kanitlar = append(*kanitlar, rol)
		return nil
	}
	s, db := sahtedb.Yeni()
	t.Cleanup(func() { db.Close() })
	return s, &Handlers{DB: db, Secret: ct_secret, LifetimeSec: 60}, kanitlar
}

func girisYap(h *Handlers, govde string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.Login(rec, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(govde)))
	return rec
}

func tokenKapisi(t *testing.T, rec *httptest.ResponseRecorder) *Claims {
	t.Helper()
	var v loginResp
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil || v.Token == "" {
		t.Fatalf("token dönmedi: %d %s", rec.Code, rec.Body)
	}
	c, err := Parse(ct_secret, v.Token)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Bayi kapısında DOĞRU root parolası, yanlış parolayla BİREBİR aynı yanıtı alır ve 2FA
// durumuna hiç inilmez: kapıya 2FA'dan sonra bakılsaydı 2FA açıkken {"iki_fa_gerekli":true}
// (200) dönüp root parolasının doğru olduğunu sızdırırdı.
func TestLoginYanlisKapidaParolaSizmaz(t *testing.T) {
	_, h, _ := girisOrtami(t, kapi.Bayi, false)
	ref := girisYap(h, `{"kullanici":"root","parola":"yanlis"}`)

	s, h, kanit := girisOrtami(t, kapi.Bayi, true)
	s.Satirlar("totp_enabled", []any{int64(1), "GIZLI", int64(0)}) // 2FA açık
	rec := girisYap(h, `{"kullanici":"root","parola":"dogru"}`)
	if rec.Code != ref.Code || rec.Body.String() != ref.Body.String() {
		t.Fatalf("yanlış kapıda doğru parola ayırt edilebilir: %d %q ≠ %d %q", rec.Code, rec.Body, ref.Code, ref.Body)
	}
	if s.Sorgulandi("totp_enabled") {
		t.Fatal("yanlış kapıda 2FA durumuna inilmemeli (parola doğruluğu sızar)")
	}
	if !s.Yurutuldu("auth.login.kapi") {
		t.Fatal("yanlış kapıdaki geçerli parola ayrı denetim kaydı bırakmalı (ele geçirilme sinyali)")
	}
	if len(*kanit) != 0 {
		t.Fatalf("reddedilen giriş kapı kanıtı yazmamalı: %v", *kanit)
	}
}

// Başarılı girişte token girilen kapıya bağlanır ve kanıt denenir (kararı kapi verir).
// Zorlama kapalıyken (kapı "") de kanıt denenir: başlık kanıtlıysa yeni kapı doğrulanabilsin.
func TestLoginAdminTokenKapisiVeKanit(t *testing.T) {
	for _, k := range []string{kapi.Admin, "", kapi.Eski} {
		t.Run("kapi="+k, func(t *testing.T) {
			s, h, kanit := girisOrtami(t, k, true)
			s.Satirlar("totp_enabled", []any{int64(0), "", int64(0)})
			rec := girisYap(h, `{"kullanici":"root","parola":"dogru"}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("giriş başarılı olmalı: %d %s", rec.Code, rec.Body)
			}
			if c := tokenKapisi(t, rec); c.Kapi != k || c.Role != "admin" {
				t.Fatalf("token girilen kapıya bağlanmalı: kapı=%q rol=%q", c.Kapi, c.Role)
			}
			if strings.Join(*kanit, ",") != kapi.Admin {
				t.Fatalf("admin kanıtı bir kez denenmeli: %v", *kanit)
			}
		})
	}
}

// 2FA kodu bekleyen giriş henüz giriş değildir: kanıt yazılmaz.
func TestLogin2FABekleyenKanitYazmaz(t *testing.T) {
	s, h, kanit := girisOrtami(t, kapi.Admin, true)
	s.Satirlar("totp_enabled", []any{int64(1), "GIZLI", int64(0)})
	rec := girisYap(h, `{"kullanici":"root","parola":"dogru"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "iki_fa_gerekli") {
		t.Fatalf("2FA istenmeli: %d %s", rec.Code, rec.Body)
	}
	if len(*kanit) != 0 {
		t.Fatalf("2FA tamamlanmadan kanıt yazılmamalı: %v", *kanit)
	}
}

// Bayi girişi: yönetici kapısında doğru bayi parolası = yanlış parola (askı ve 2FA
// yanıtlarına da inilmez); bayi kapısında token bayi kapısına bağlanır, bayi kanıtı denenir.
func TestResellerLoginKapi(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("dogru"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	bayi := []any{int64(5), string(hash), "Bayi Bir", "askida"}

	s, h, _ := girisOrtami(t, kapi.Admin, false)
	s.Satirlar("role='reseller'", bayi)
	ref := girisYap(h, `{"kullanici":"bayi1","parola":"yanlis"}`)

	s, h, kanit := girisOrtami(t, kapi.Admin, false)
	s.Satirlar("role='reseller'", bayi) // askıda: kapı denetimi askı yanıtından ÖNCE
	rec := girisYap(h, `{"kullanici":"bayi1","parola":"dogru"}`)
	if rec.Code != ref.Code || rec.Body.String() != ref.Body.String() {
		t.Fatalf("yönetici kapısında doğru bayi parolası ayırt edilebilir: %d %q ≠ %d %q", rec.Code, rec.Body, ref.Code, ref.Body)
	}
	if s.Sorgulandi("totp_enabled") || len(*kanit) != 0 {
		t.Fatal("yanlış kapıda 2FA'ya inilmemeli, kanıt yazılmamalı")
	}

	bayi[3] = "active"
	s, h, kanit = girisOrtami(t, kapi.Bayi, false)
	s.Satirlar("role='reseller'", bayi)
	s.Satirlar("totp_enabled", []any{int64(0), "", int64(0)})
	rec = girisYap(h, `{"kullanici":"bayi1","parola":"dogru"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("bayi kapısında giriş başarılı olmalı: %d %s", rec.Code, rec.Body)
	}
	if c := tokenKapisi(t, rec); c.Kapi != kapi.Bayi || c.Role != "reseller" || c.ResellerID != 5 {
		t.Fatalf("bayi token'ı: %+v", c)
	}
	if strings.Join(*kanit, ",") != kapi.Bayi {
		t.Fatalf("bayi kanıtı bir kez denenmeli: %v", *kanit)
	}
}
