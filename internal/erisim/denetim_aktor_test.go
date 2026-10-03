package erisim

import (
	"testing"

	"girginospanel/internal/auth"
)

// Müşteri ADINA oturumda erişim politikası değişikliği denetime GERÇEK aktörle
// (bayi) ve ayrı "adına" kolonuyla düşer. Önceki yerel aktör kopyası bu oturumu
// "musteri:<id>" (uid 0) diye yazıyor, işlemi yapan bayiyi kayıttan siliyordu.
func TestErisimDenetim_AdinaOturumGercekAktorle(t *testing.T) {
	adina, _, err := auth.GenerateMusteri(er_sir, auth.MusteriClaims{
		FTPHesapID: 420, DomainID: 42, Kullanici: "m", AlanAdi: "bayialan.test",
		AktorUID: 7, AktorAd: "bayi7", AktorRol: "reseller", AktorRID: 7}, auth.AdinaOmurSn)
	if err != nil {
		t.Fatal(err)
	}
	sd, _, govde := er_cagir(t, er_durumlar[0], adina)
	if !sd.Yurutuldu("adina_domain_id") || !sd.Yurutuldu("bayi7") {
		t.Fatalf("adına-oturum kaydı aktör (bayi7) + adina_domain_id ile yazılmalı (yanıt %s)", govde)
	}
	if sd.Yurutuldu("musteri:42") {
		t.Fatal("adına-oturumda aktör \"musteri:42\" yazılmamalı — işlemi yapan bayidir")
	}

	// Müşterinin KENDİ oturumu değişmez: aktör "musteri:<id>", adına kolonu yok.
	sd, _, _ = er_cagir(t, er_durumlar[0], er_tokenlar(t)["müşteri"])
	if !sd.Yurutuldu("musteri:42") || sd.Yurutuldu("adina_domain_id") {
		t.Fatal("müşteri oturumu \"musteri:42\" olarak, adına kolonu olmadan yazılmalı")
	}
}
