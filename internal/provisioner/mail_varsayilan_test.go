package provisioner

// mail_varsayilan_test.go — varsayılan mail sertifikası mantığının kanıtı:
//  - self-signed cert → geçersiz (değiştirilmeli / kaynak olarak reddedilir)
//  - CA-imzalı (LE) cert → geçerli
//  - süresi geçmiş → geçersiz
//  - 🔴 varsayılanın TEK kaynağı panel host sertifikası: kiracı sertifikası
//    reddedilir; panel sertifikası yenilenince / varsayılana kiracı sertifikası
//    yazılmışsa eşitleme panel sertifikasına döndürür.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// certUret — test için: self-signed VEYA "farklı issuer" (LE taklidi) cert PEM.
func certUret(t *testing.T, subjectCN, issuerCN string, notAfter time.Time) []byte {
	t.Helper()
	anahtar, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	// issuer != subject yapmak için ayrı bir "CA" ile imzala.
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: issuerCN},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: subjectCN},
		Issuer:       pkix.Name{CommonName: issuerCN},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}
	var der []byte
	if subjectCN == issuerCN {
		// self-signed: kendi anahtarıyla, kendi kendine imza
		der, _ = x509.CreateCertificate(rand.Reader, tmpl, tmpl, &anahtar.PublicKey, anahtar)
	} else {
		// CA imzalı (issuer != subject) — LE benzeri
		der, _ = x509.CreateCertificate(rand.Reader, tmpl, caTmpl, &anahtar.PublicKey, caKey)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestCertGecerliLE(t *testing.T) {
	gelecek := time.Now().Add(90 * 24 * time.Hour)
	gecmis := time.Now().Add(-24 * time.Hour)

	self := certUret(t, "gpanel.example", "gpanel.example", gelecek)  // self-signed
	le := certUret(t, "mail.example", "R3 Let's Encrypt", gelecek)    // CA imzalı
	suresi := certUret(t, "mail.example", "R3 Let's Encrypt", gecmis) // süresi geçmiş LE

	if certGecerliLE(self) {
		t.Error("self-signed cert GEÇERLİ sayıldı — kaynak olarak reddedilmeliydi")
	}
	if !certGecerliLE(le) {
		t.Error("CA-imzalı LE cert GEÇERSİZ sayıldı — kabul edilmeliydi")
	}
	if certGecerliLE(suresi) {
		t.Error("süresi geçmiş cert GEÇERLİ sayıldı")
	}
	t.Log("certGecerliLE: self-signed reddedildi, LE kabul edildi, süresi geçmiş reddedildi ✓")
}

// ── Varsayılanın tek kaynağı = panel host sertifikası (güvenlik regresyonu) ──
//
// 🔴 Bütün testler varsayılan/kaynak yollarını t.TempDir'e, dovecot/postfix
// reload'unu sayaca çevirir (mv_ortam): canlı /etc/pki/mail ve
// /etc/ssl/girginospanel'e ASLA dokunulmaz.

// mv_cift — test için cert+key PEM çifti. issuerCN == subjectCN → self-signed;
// değilse ayrı bir "CA" imzalar (LE taklidi).
func mv_cift(t *testing.T, subjectCN, issuerCN string, notAfter time.Time) (crt, key []byte) {
	t.Helper()
	anahtar, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: subjectCN},
		DNSNames:     []string{subjectCN},
		NotBefore:    time.Now().Add(-48 * time.Hour),
		NotAfter:     notAfter,
	}
	var der []byte
	if subjectCN == issuerCN {
		der, err = x509.CreateCertificate(rand.Reader, tmpl, tmpl, &anahtar.PublicKey, anahtar)
	} else {
		caKey, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			t.Fatal(e)
		}
		caTmpl := &x509.Certificate{
			SerialNumber:          big.NewInt(1),
			Subject:               pkix.Name{CommonName: issuerCN},
			NotBefore:             time.Now().Add(-72 * time.Hour),
			NotAfter:              time.Now().Add(365 * 24 * time.Hour),
			IsCA:                  true,
			BasicConstraintsValid: true,
			KeyUsage:              x509.KeyUsageCertSign,
		}
		der, err = x509.CreateCertificate(rand.Reader, tmpl, caTmpl, &anahtar.PublicKey, caKey)
	}
	if err != nil {
		t.Fatal(err)
	}
	kder, err := x509.MarshalECPrivateKey(anahtar)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder})
}

type mvOrtam struct {
	crt, key             string // varsayılan (SNI'sız) posta sertifikası
	kaynakCrt, kaynakKey string // panel host sertifikası
	kiraciCrt, kiraciKey string // bir kiracının mail.<alan> sertifikası
	yenile               int    // dovecot/postfix reload sayısı
}

func mv_ortam(t *testing.T) *mvOrtam {
	t.Helper()
	c, k, kc, kk, y := mailVarsayilanCrt, mailVarsayilanKey, mailVarsayilanKaynakCrt, mailVarsayilanKaynakKey, mailVarsayilanYenile
	t.Cleanup(func() {
		mailVarsayilanCrt, mailVarsayilanKey, mailVarsayilanKaynakCrt, mailVarsayilanKaynakKey, mailVarsayilanYenile = c, k, kc, kk, y
	})
	kok := t.TempDir()
	for _, d := range []string{"pki/mail", "ssl/girginospanel", "mail-certs/kiraci.test"} {
		if err := os.MkdirAll(filepath.Join(kok, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	o := &mvOrtam{}
	o.crt, o.key = filepath.Join(kok, "pki/mail/mail.crt"), filepath.Join(kok, "pki/mail/mail.key")
	o.kaynakCrt, o.kaynakKey = filepath.Join(kok, "ssl/girginospanel/panel.crt"), filepath.Join(kok, "ssl/girginospanel/panel.key")
	o.kiraciCrt, o.kiraciKey = filepath.Join(kok, "mail-certs/kiraci.test/fullchain.pem"), filepath.Join(kok, "mail-certs/kiraci.test/key.pem")
	mailVarsayilanCrt, mailVarsayilanKey = o.crt, o.key
	mailVarsayilanKaynakCrt, mailVarsayilanKaynakKey = o.kaynakCrt, o.kaynakKey
	mailVarsayilanYenile = func() { o.yenile++ }
	return o
}

func (o *mvOrtam) koy(t *testing.T, crtYol, keyYol string, crt, key []byte) {
	t.Helper()
	if err := os.WriteFile(crtYol, crt, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyYol, key, 0o600); err != nil {
		t.Fatal(err)
	}
}

func mv_oku(t *testing.T, yol string) []byte {
	t.Helper()
	b, err := os.ReadFile(yol)
	if err != nil {
		t.Fatalf("%s okunamadı: %v", yol, err)
	}
	return b
}

// Kiracının mail.<alan> LE sertifikası varsayılan (sunucu geneli) sertifikaya
// YAZILAMAZ — varsayılan self-signed ya da süresi dolmuş olsa bile (eski
// davranış: tam bu durumda kiracı sertifikası sunucunun kimliği oluyordu).
func TestMailVarsayilanKur_KiraciKaynagiReddedilir(t *testing.T) {
	gelecek := time.Now().Add(80 * 24 * time.Hour)
	for _, v := range []struct {
		ad       string
		varsayil func(t *testing.T) ([]byte, []byte)
	}{
		{"varsayilan-self-signed", func(t *testing.T) ([]byte, []byte) {
			return mv_cift(t, "sunucu.test", "sunucu.test", gelecek)
		}},
		{"varsayilan-suresi-dolmus-panel-kopyasi", func(t *testing.T) ([]byte, []byte) {
			return mv_cift(t, "panel.sunucu.test", "YE2 Lets Encrypt", time.Now().Add(-time.Hour))
		}},
	} {
		t.Run(v.ad, func(t *testing.T) {
			o := mv_ortam(t)
			vc, vk := v.varsayil(t)
			o.koy(t, o.crt, o.key, vc, vk)
			kc, kk := mv_cift(t, "mail.kiraci.test", "YE2 Lets Encrypt", gelecek)
			o.koy(t, o.kiraciCrt, o.kiraciKey, kc, kk)
			pc, pk := mv_cift(t, "panel.sunucu.test", "YE2 Lets Encrypt", gelecek)
			o.koy(t, o.kaynakCrt, o.kaynakKey, pc, pk)

			for _, yol := range [][2]string{
				{o.kiraciCrt, o.kiraciKey},
				// yol oyunu: Clean'den sonra panel yolu DEĞİL
				{filepath.Dir(o.kaynakCrt) + "/../../mail-certs/kiraci.test/fullchain.pem", o.kaynakKey},
				// yalnız anahtar yolu farklı
				{o.kaynakCrt, o.kiraciKey},
			} {
				degisti, err := MailVarsayilanKur(yol[0], yol[1])
				if !errors.Is(err, ErrMailVarsayilanKaynak) || degisti {
					t.Errorf("kaynak %v: ErrMailVarsayilanKaynak beklenirdi: degisti=%v err=%v", yol, degisti, err)
				}
			}
			if !bytes.Equal(mv_oku(t, o.crt), vc) || !bytes.Equal(mv_oku(t, o.key), vk) {
				t.Fatal("varsayılan posta sertifikası kiracı kaynağıyla DEĞİŞTİ — sunucu geneli kimlik kiracıya geçti")
			}
			if o.yenile != 0 {
				t.Fatalf("dovecot/postfix yenilenmemeliydi: %d", o.yenile)
			}
		})
	}
}

// Kurulum davranışı korunur: panel host LE'si self-signed / eksik varsayılanın
// yerine geçer; ilk seferde eski varsayılan yedeklenir.
func TestMailVarsayilanKur_PanelKaynagiSelfSignedVarsayilaniDoldurur(t *testing.T) {
	gelecek := time.Now().Add(80 * 24 * time.Hour)
	for _, eksik := range []bool{false, true} {
		o := mv_ortam(t)
		sc, sk := mv_cift(t, "sunucu.test", "sunucu.test", gelecek)
		if !eksik {
			o.koy(t, o.crt, o.key, sc, sk)
		}
		pc, pk := mv_cift(t, "panel.sunucu.test", "YE2 Lets Encrypt", gelecek)
		o.koy(t, o.kaynakCrt, o.kaynakKey, pc, pk)

		degisti, err := MailVarsayilanKur(o.kaynakCrt, o.kaynakKey)
		if err != nil || !degisti {
			t.Fatalf("eksik=%v: panel kaynağı varsayılanı doldurmalı: degisti=%v err=%v", eksik, degisti, err)
		}
		if !bytes.Equal(mv_oku(t, o.crt), pc) || !bytes.Equal(mv_oku(t, o.key), pk) {
			t.Fatalf("eksik=%v: varsayılan panel sertifikası olmalı", eksik)
		}
		if fi, err := os.Stat(o.key); err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("eksik=%v: anahtar izni 0600 olmalı: %v %v", eksik, fi, err)
		}
		if o.yenile != 1 {
			t.Fatalf("eksik=%v: dovecot/postfix bir kez yenilenmeli: %d", eksik, o.yenile)
		}
		if !eksik && !bytes.Equal(mv_oku(t, o.crt+".selfsigned.yedek"), sc) {
			t.Fatal("ilk seferde eski self-signed varsayılan yedeklenmeli")
		}
	}
}

// Kurulum akışı geçerli (CA imzalı, süresi dolmamış) bir varsayılana dokunmaz.
func TestMailVarsayilanKur_GecerliVarsayilanaDokunmaz(t *testing.T) {
	gelecek := time.Now().Add(80 * 24 * time.Hour)
	o := mv_ortam(t)
	vc, vk := mv_cift(t, "eski.sunucu.test", "YE1 Lets Encrypt", gelecek)
	o.koy(t, o.crt, o.key, vc, vk)
	pc, pk := mv_cift(t, "panel.sunucu.test", "YE2 Lets Encrypt", gelecek)
	o.koy(t, o.kaynakCrt, o.kaynakKey, pc, pk)
	if degisti, err := MailVarsayilanKur(o.kaynakCrt, o.kaynakKey); err != nil || degisti {
		t.Fatalf("geçerli varsayılan korunmalı: degisti=%v err=%v", degisti, err)
	}
	if !bytes.Equal(mv_oku(t, o.crt), vc) || o.yenile != 0 {
		t.Fatal("geçerli varsayılan değişti / servisler yenilendi")
	}
}

// Panel host SSL akışı (AdminOnly) + nöbetçi: varsayılan panel sertifikasıyla
// EŞİTLENİR — oraya yazılmış bir kiracı sertifikası ve süresi dolan/yenilenen
// eski panel kopyası değiştirilir; eşitse, panel LE'si yoksa ya da çift
// bozuksa hiçbir şey yazılmaz.
func TestMailVarsayilanPaneldenEsitle(t *testing.T) {
	gelecek := time.Now().Add(80 * 24 * time.Hour)
	durumlar := []struct {
		ad        string
		kur       func(t *testing.T, o *mvOrtam) (bc, bk []byte) // değişecekse beklenen içerik
		degismeli bool
		hataBekle bool
	}{
		{"kiraci-sertifikasi-geri-alinir", func(t *testing.T, o *mvOrtam) ([]byte, []byte) {
			kc, kk := mv_cift(t, "mail.kiraci.test", "YE2 Lets Encrypt", gelecek)
			o.koy(t, o.crt, o.key, kc, kk)
			pc, pk := mv_cift(t, "panel.sunucu.test", "YE2 Lets Encrypt", gelecek)
			o.koy(t, o.kaynakCrt, o.kaynakKey, pc, pk)
			return pc, pk
		}, true, false},
		{"suresi-dolan-panel-kopyasi-yenilenir", func(t *testing.T, o *mvOrtam) ([]byte, []byte) {
			ec, ek := mv_cift(t, "panel.sunucu.test", "YE2 Lets Encrypt", time.Now().Add(-time.Hour))
			o.koy(t, o.crt, o.key, ec, ek)
			pc, pk := mv_cift(t, "panel.sunucu.test", "YE2 Lets Encrypt", gelecek)
			o.koy(t, o.kaynakCrt, o.kaynakKey, pc, pk)
			return pc, pk
		}, true, false},
		{"gecerli-eski-panel-kopyasi-yenilenir", func(t *testing.T, o *mvOrtam) ([]byte, []byte) {
			ec, ek := mv_cift(t, "panel.sunucu.test", "YE2 Lets Encrypt", time.Now().Add(20*24*time.Hour))
			o.koy(t, o.crt, o.key, ec, ek)
			pc, pk := mv_cift(t, "panel.sunucu.test", "YE2 Lets Encrypt", gelecek)
			o.koy(t, o.kaynakCrt, o.kaynakKey, pc, pk)
			return pc, pk
		}, true, false},
		{"zaten-esit", func(t *testing.T, o *mvOrtam) ([]byte, []byte) {
			pc, pk := mv_cift(t, "panel.sunucu.test", "YE2 Lets Encrypt", gelecek)
			o.koy(t, o.crt, o.key, pc, pk)
			o.koy(t, o.kaynakCrt, o.kaynakKey, pc, pk)
			return nil, nil
		}, false, false},
		{"panel-self-signed", func(t *testing.T, o *mvOrtam) ([]byte, []byte) {
			sc, sk := mv_cift(t, "sunucu.test", "sunucu.test", gelecek)
			o.koy(t, o.crt, o.key, sc, sk)
			pc, pk := mv_cift(t, "panel.sunucu.test", "panel.sunucu.test", gelecek)
			o.koy(t, o.kaynakCrt, o.kaynakKey, pc, pk)
			return nil, nil
		}, false, false},
		{"anahtar-eslesmiyor", func(t *testing.T, o *mvOrtam) ([]byte, []byte) {
			sc, sk := mv_cift(t, "sunucu.test", "sunucu.test", gelecek)
			o.koy(t, o.crt, o.key, sc, sk)
			pc, _ := mv_cift(t, "panel.sunucu.test", "YE2 Lets Encrypt", gelecek)
			_, baskaKey := mv_cift(t, "panel.sunucu.test", "YE2 Lets Encrypt", gelecek)
			o.koy(t, o.kaynakCrt, o.kaynakKey, pc, baskaKey)
			return nil, nil
		}, false, true},
		{"panel-ssl-yok", func(t *testing.T, o *mvOrtam) ([]byte, []byte) {
			sc, sk := mv_cift(t, "sunucu.test", "sunucu.test", gelecek)
			o.koy(t, o.crt, o.key, sc, sk)
			return nil, nil
		}, false, false},
	}
	for _, d := range durumlar {
		t.Run(d.ad, func(t *testing.T) {
			o := mv_ortam(t)
			beklenenC, beklenenK := d.kur(t, o)
			onceC, onceK := mv_oku(t, o.crt), mv_oku(t, o.key)

			degisti, err := MailVarsayilanPaneldenEsitle()
			if (err != nil) != d.hataBekle || degisti != d.degismeli {
				t.Fatalf("degisti=%v err=%v (beklenen degisti=%v hata=%v)", degisti, err, d.degismeli, d.hataBekle)
			}
			sonC, sonK := mv_oku(t, o.crt), mv_oku(t, o.key)
			if d.degismeli {
				if !bytes.Equal(sonC, beklenenC) || !bytes.Equal(sonK, beklenenK) {
					t.Fatal("varsayılan panel host sertifikası/anahtarı olmalı")
				}
				if o.yenile != 1 {
					t.Fatalf("dovecot/postfix bir kez yenilenmeli: %d", o.yenile)
				}
			} else {
				if !bytes.Equal(sonC, onceC) || !bytes.Equal(sonK, onceK) {
					t.Fatal("varsayılan DEĞİŞMEMELİYDİ")
				}
				if o.yenile != 0 {
					t.Fatalf("servisler yenilenmemeliydi: %d", o.yenile)
				}
			}
			// Geçici dosya artığı kalmamalı.
			girdiler, _ := os.ReadDir(filepath.Dir(o.crt))
			for _, g := range girdiler {
				if strings.Contains(g.Name(), ".gecici-") {
					t.Errorf("geçici dosya artığı: %s", g.Name())
				}
			}
		})
	}
}

// Posta yığını kurulu değilse (varsayılan dizini yok) eşitleme dizin
// YARATMAZ, hiçbir şey yazmaz.
func TestMailVarsayilanPaneldenEsitle_PostaKuruluDegil(t *testing.T) {
	o := mv_ortam(t)
	pc, pk := mv_cift(t, "panel.sunucu.test", "YE2 Lets Encrypt", time.Now().Add(80*24*time.Hour))
	o.koy(t, o.kaynakCrt, o.kaynakKey, pc, pk)
	if err := os.RemoveAll(filepath.Dir(o.crt)); err != nil {
		t.Fatal(err)
	}
	degisti, err := MailVarsayilanPaneldenEsitle()
	if err != nil || degisti {
		t.Fatalf("posta kurulu değilken no-op beklenirdi: degisti=%v err=%v", degisti, err)
	}
	if _, err := os.Stat(filepath.Dir(o.crt)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("varsayılan dizini yaratılmamalıydı: %v", err)
	}
	if o.yenile != 0 {
		t.Fatalf("servisler yenilenmemeliydi: %d", o.yenile)
	}
}

// Nöbetçi açılışta bir kez eşitler ve ctx bitince döner.
func TestMailVarsayilanNobetci_AcilistaEsitler(t *testing.T) {
	o := mv_ortam(t)
	gelecek := time.Now().Add(80 * 24 * time.Hour)
	kc, kk := mv_cift(t, "mail.kiraci.test", "YE2 Lets Encrypt", gelecek)
	o.koy(t, o.crt, o.key, kc, kk)
	pc, pk := mv_cift(t, "panel.sunucu.test", "YE2 Lets Encrypt", gelecek)
	o.koy(t, o.kaynakCrt, o.kaynakKey, pc, pk)
	ctx, iptal := context.WithCancel(context.Background())
	iptal()
	bitti := make(chan struct{})
	go func() { MailVarsayilanNobetci(ctx); close(bitti) }()
	select {
	case <-bitti:
	case <-time.After(5 * time.Second):
		t.Fatal("nöbetçi ctx bitince dönmedi")
	}
	if !bytes.Equal(mv_oku(t, o.crt), pc) || !bytes.Equal(mv_oku(t, o.key), pk) {
		t.Fatal("nöbetçi açılışta varsayılanı panel sertifikasıyla eşitlemeliydi")
	}
}

// Atomik yazım hedefteki symlink'i İZLEMEZ (yerine koyar); dışarıdaki dosya
// değişmez.
func TestMailVarsayilanAtomikYaz_SymlinkIzlenmez(t *testing.T) {
	kok := t.TempDir()
	dis := filepath.Join(kok, "dis.txt")
	if err := os.WriteFile(dis, []byte("dokunulmaz"), 0o644); err != nil {
		t.Fatal(err)
	}
	hedef := filepath.Join(kok, "mail.crt")
	if err := os.Symlink(dis, hedef); err != nil {
		t.Fatal(err)
	}
	if err := mailVarsayilanAtomikYaz(hedef, []byte("yeni"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b := mv_oku(t, dis); string(b) != "dokunulmaz" {
		t.Fatalf("symlink izlendi, dış dosya değişti: %q", b)
	}
	if fi, err := os.Lstat(hedef); err != nil || fi.Mode()&os.ModeSymlink != 0 || string(mv_oku(t, hedef)) != "yeni" {
		t.Fatalf("hedef düz dosya olarak yazılmalıydı: %v %v", fi, err)
	}
}
