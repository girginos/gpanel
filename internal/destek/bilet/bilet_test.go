package bilet

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

var (
	t0 = time.Unix(1_790_000_000, 0)
	fp = strings.Repeat("ab", 32)
)

func anahtar(t *testing.T, tohum byte) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	sk := ed25519.NewKeyFromSeed([]byte(strings.Repeat(string([]byte{tohum}), 32)))
	return sk.Public().(ed25519.PublicKey), sk
}

// imzala: lisans sunucusunun biletUret'iyle aynı biçim (gövde JSON baytları üzerinde Ed25519).
func imzala(t *testing.T, sk ed25519.PrivateKey, g Govde) []byte {
	t.Helper()
	b, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	e := base64.RawURLEncoding
	return []byte(e.EncodeToString(b) + "." + e.EncodeToString(ed25519.Sign(sk, b)))
}

func gecerli() Govde {
	return Govde{FP: fp, Urun: "gpanel-suite", Durum: DurumAktif, Bitis: t0.Add(24 * time.Hour).Unix(), Iat: t0.Add(-time.Hour).Unix(), Pkt: "0"}
}

func TestBiletDogrulama(t *testing.T) {
	pub, sk := anahtar(t, 1)
	pub2, sk2 := anahtar(t, 2)
	d := Yeni([]ed25519.PublicKey{pub})

	if s, err := d.Dogrula(imzala(t, sk, gecerli()), fp, t0); err != nil || !s.Bitis.Equal(time.Unix(gecerli().Bitis, 0)) {
		t.Fatalf("geçerli bilet: %v %+v", err, s)
	}
	// anahtar döndürme: ikinci anahtar da kabul edilir
	if _, err := Yeni([]ed25519.PublicKey{pub, pub2}).Dogrula(imzala(t, sk2, gecerli()), fp, t0); err != nil {
		t.Fatalf("ikinci anahtar: %v", err)
	}

	odenmedi := gecerli()
	odenmedi.Durum = DurumOdenmedi
	askili := gecerli()
	askili.Askiya = t0.Add(2 * time.Hour).Unix()
	gecmisAski := gecerli()
	gecmisAski.Askiya = t0.Add(-time.Minute).Unix()
	askida := gecerli()
	askida.Durum = DurumAskida
	suresiz := gecerli()
	suresiz.Bitis = 0
	suresiDoldu := gecerli()
	suresiDoldu.Bitis = t0.Unix()
	iatYok := gecerli()
	iatYok.Iat = 0
	gelecek := gecerli()
	gelecek.Iat = t0.Add(time.Hour).Unix()
	urunYok := gecerli()
	urunYok.Urun = ""
	fpBos := gecerli()
	fpBos.FP = ""
	bilinmeyen := gecerli()
	bilinmeyen.Durum = "iptal"

	for ad, c := range map[string]struct {
		bilet []byte
		fp    string
		hata  error
	}{
		"yanlış anahtar":    {imzala(t, sk2, gecerli()), fp, ErrImza},
		"süresi dolmuş":     {imzala(t, sk, suresiDoldu), fp, ErrSuresiDoldu},
		"bitis yok":         {imzala(t, sk, suresiz), fp, ErrBicim},
		"başka parmak izi":  {imzala(t, sk, gecerli()), strings.Repeat("cd", 32), ErrParmakIzi},
		"bilet fp boş":      {imzala(t, sk, fpBos), fp, ErrParmakIzi},
		"beklenen fp kısa":  {imzala(t, sk, gecerli()), "ab", ErrParmakIzi},
		"askıda":            {imzala(t, sk, askida), fp, ErrDurum},
		"bilinmeyen durum":  {imzala(t, sk, bilinmeyen), fp, ErrDurum},
		"askı anı geçmiş":   {imzala(t, sk, gecmisAski), fp, ErrAskida},
		"iat yok":           {imzala(t, sk, iatYok), fp, ErrBicim},
		"iat gelecekte":     {imzala(t, sk, gelecek), fp, ErrBicim},
		"ürün yok":          {imzala(t, sk, urunYok), fp, ErrBicim},
		"biçim: nokta yok":  {[]byte("abc"), fp, ErrBicim},
		"biçim: üç parça":   {[]byte("a.b.c"), fp, ErrBicim},
		"biçim: boş":        {nil, fp, ErrBicim},
		"biçim: imza kısa":  {[]byte("e30." + base64.RawURLEncoding.EncodeToString([]byte("kisa"))), fp, ErrBicim},
		"bilet-yok işareti": {[]byte("bilet-yok"), fp, ErrBicim},
		"çok büyük":         {[]byte(strings.Repeat("a", EnBuyukBoyut+1)), fp, ErrBicim},
	} {
		if _, err := d.Dogrula(c.bilet, c.fp, t0); !errors.Is(err, c.hata) {
			t.Fatalf("%s: hata %v (beklenen %v)", ad, err, c.hata)
		}
	}
	// gövde kurcalanırsa imza düşer
	b := string(imzala(t, sk, gecerli()))
	govde, _ := base64.RawURLEncoding.DecodeString(strings.Split(b, ".")[0])
	kurcali := strings.Replace(string(govde), `"aktif"`, `"askida"`, 1)
	k := base64.RawURLEncoding.EncodeToString([]byte(kurcali)) + "." + strings.Split(b, ".")[1]
	if _, err := d.Dogrula([]byte(k), fp, t0); !errors.Is(err, ErrImza) {
		t.Fatalf("kurcalanmış gövde: %v", err)
	}
	// askıya alma anı bitişten önceyse doğrulama o anda düşer
	if s, err := d.Dogrula(imzala(t, sk, askili), fp, t0); err != nil || !s.Bitis.Equal(time.Unix(askili.Askiya, 0)) {
		t.Fatalf("askı anı bitiş olmalı: %v %+v", err, s)
	}
	// ödenmedi (7 gün pay) kabul
	if _, err := d.Dogrula(imzala(t, sk, odenmedi), fp, t0); err != nil {
		t.Fatalf("ödenmedi payı: %v", err)
	}
	// anahtarsız doğrulayıcı hiçbir şeyi kabul etmez
	if _, err := Yeni(nil).Dogrula(imzala(t, sk, gecerli()), fp, t0); !errors.Is(err, ErrAnahtarYok) {
		t.Fatalf("anahtarsız: %v", err)
	}
}
