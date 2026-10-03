package protokol

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"testing"
)

// Bilinen-yanıt vektörleri: kanonik biçim başka dilde/ikilide yeniden yazılırsa
// bu baytlar birebir üretilmeli. Beklenen değer kanonik yazıcıdan BAĞIMSIZ kurulur.

func kvDizge(b *bytes.Buffer, s string) {
	_ = binary.Write(b, binary.BigEndian, uint32(len(s)))
	b.WriteString(s)
}

func kvSayi(b *bytes.Buffer, n int64) { _ = binary.Write(b, binary.BigEndian, n) }

func TestKanonikVektorIsEmri(t *testing.T) {
	c := testCifti(t, 1)
	e := ornekIsEmri()
	if err := testImzalayici(t, c, t0).imzalaZaman(e, "AAAAAAAAAAAAAAAAAAAAAA", t0.Unix(), t0.Unix()+60); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	kvDizge(&b, "girginos-destek/v1/is_emri")
	kvSayi(&b, 1)
	kvDizge(&b, c.Kimlik)
	kvDizge(&b, "AAAAAAAAAAAAAAAAAAAAAA")
	kvSayi(&b, 1_790_000_000)
	kvSayi(&b, 1_790_000_060)
	kvDizge(&b, "AB23CD45")
	kvDizge(&b, "oturum_0001")
	kvDizge(&b, "baglanti_0001")
	kvDizge(&b, "log_oku")
	kvDizge(&b, "R0")
	kvDizge(&b, `{"hesap":1,"tur":"nginx_error"}`)
	k, err := KanonikBaytlar(e)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(k, b.Bytes()) {
		t.Fatalf("kanonik baytlar:\n%x\nbeklenen\n%x", k, b.Bytes())
	}
	imza, _ := b64.DecodeString(e.Imza)
	if !ed25519.Verify(c.Acik, b.Bytes(), imza) {
		t.Fatal("imza bağımsız kurulan baytlar üzerinde doğrulanmıyor")
	}
}

func TestKanonikVektorEslesme(t *testing.T) {
	c := testCifti(t, 1)
	m := &EslesmeOnayla{KodOzet: h64("a"), TgUserID: 987654321, TgKullaniciAdi: "musteri_1"}
	if err := testImzalayici(t, c, t0).imzalaZaman(m, "AAAAAAAAAAAAAAAAAAAAAA", t0.Unix(), t0.Unix()+30); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	kvDizge(&b, "girginos-destek/v1/eslesme_onayla")
	kvSayi(&b, 1)
	kvDizge(&b, c.Kimlik)
	kvDizge(&b, "AAAAAAAAAAAAAAAAAAAAAA")
	kvSayi(&b, 1_790_000_000)
	kvSayi(&b, 1_790_000_030)
	kvDizge(&b, h64("a"))
	kvSayi(&b, 987654321)
	kvDizge(&b, "musteri_1")
	imza, _ := b64.DecodeString(m.Imza)
	if !ed25519.Verify(c.Acik, b.Bytes(), imza) {
		t.Fatal("eslesme_onayla kanonik vektörü")
	}
}

func TestKanonikVektorMeydan(t *testing.T) {
	merkez := testCifti(t, 1)
	cihaz := testCifti(t, 7)
	kay := ornekKayit(t, cihaz)
	m, err := testImzalayici(t, merkez, t0).MeydanOlustur(kay)
	if err != nil {
		t.Fatal(err)
	}
	y, err := MeydanYanitla(cihaz.Ozel, m, kay)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []struct {
		tur  string
		acik ed25519.PublicKey
		imza string
	}{
		{"meydan_okuma", merkez.Acik, m.MerkezImza},
		{"meydan_yanit", cihaz.Acik, y.Imza},
	} {
		var b bytes.Buffer
		kvDizge(&b, "girginos-destek/v1/"+d.tur)
		kvSayi(&b, 1)
		kvDizge(&b, m.Meydan)
		kvSayi(&b, t0.Unix()+30)
		kvDizge(&b, merkez.Kimlik)
		kvDizge(&b, kay.CihazAnahtari)
		kvDizge(&b, kay.LisansParmakIzi)
		imza, _ := b64.DecodeString(d.imza)
		if !ed25519.Verify(d.acik, b.Bytes(), imza) {
			t.Fatalf("%s kanonik vektörü", d.tur)
		}
	}
}
