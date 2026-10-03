package tasima

// komut_altin_test.go — ALTIN TEST: kaynak sunucuya giden uzak komut dizgelerinin
// sha256'sı sabitlenir (Plesk / cPanel / DirectAdmin / gPanel × keşif, sahipler,
// dump, DNS, sertifika, posta + ortak tespit/bağlantı testi).
//
// Kaydedici sahte uzakKabuk Kaynak.kabuk'a takılır: Calistir / RsyncCek /
// AkisCalistir ağa çıkmadan kaydedilir. Ağa, canlı sisteme ve DB'ye dokunulmaz
// (dump senaryosu yalnız dbAktar'ın kendi /var/tmp geçici dosyasını açıp siler).
//
// Amaç: taşıma kodunun yeniden düzenlenmesi (aktar.go bölünmesi, bağdaştırıcılar)
// uzak komutları BAYT BAYT aynı bırakmalı. Bilinçli bir komut değişikliğinde bu
// tablo AYNI committe güncellenir ve nedeni commit mesajına yazılır; bulunan yeni
// değer hata iletisinde "ALTIN" satırıyla basılır.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// kaydedici — uzakKabuk'un kaydeden sahtesi.
type kaydedici struct {
	cagrilar []string
	yanit    func(komut string) string
	// AkisCalistir'in döneceği değerler (boşsa sahte uzak hata).
	akisStderr string
	akisHata   error
}

var errAltinAkis = errors.New("altin: sahte akis hatasi")

func (r *kaydedici) kaydet(parca ...string) {
	r.cagrilar = append(r.cagrilar, strings.Join(parca, "\x1f"))
}

func (r *kaydedici) Calistir(_ context.Context, uzakKomut string) (string, error) {
	r.kaydet("calistir", uzakKomut)
	if r.yanit != nil {
		return r.yanit(uzakKomut), nil
	}
	return "", nil
}

func (r *kaydedici) RsyncCek(_ context.Context, uzakYol, yerelYol string, ekstra ...string) (string, error) {
	r.kaydet(append([]string{"rsync", uzakYol, yerelYol}, ekstra...)...)
	return "", nil
}

func (r *kaydedici) AkisCalistir(_ context.Context, uzakKomut string, _ io.Writer) (string, error) {
	r.kaydet("akis", uzakKomut)
	if r.akisHata != nil {
		return r.akisStderr, r.akisHata
	}
	return "sahte stderr", errAltinAkis
}

// ozet — kaydedilen çağrı dizisinin sha256'sı (tür + argümanlar, sıra dahil).
func (r *kaydedici) ozet() string {
	s := sha256.Sum256([]byte(strings.Join(r.cagrilar, "\x1e")))
	return hex.EncodeToString(s[:])
}

const (
	altinAlan    = "ornek-alan.com.tr"
	altinDB      = "ornekhesap_wp"
	altinHedefDB = "c_ornek_wp"
	altinParola  = "altin-parola-1234"
)

var altinTipler = []string{"plesk", "cpanel", "directadmin", "gpanel"}

// altinYanit — komutların HEPSİ gönderilsin diye gereken asgari sahte yanıtlar.
func altinYanit(komut string) string {
	switch {
	case strings.HasPrefix(komut, "echo GOSP_OK"):
		return "GOSP_OK\nkaynak-sunucu\n"
	case strings.Contains(komut, "SELECT m.mail_name"):
		// Kutu bulunursa ikinci komut (mail_auth_view) da gönderilir.
		return "info\n"
	}
	return ""
}

func altinKaynak(tip string, kay *kaydedici) *Kaynak {
	return &Kaynak{Tip: tip, Host: "192.0.2.10", Port: 22, Kullanici: "root", Parola: altinParola, kabuk: kay}
}

type altinSenaryo struct {
	ad    string
	tip   string
	calis func(ctx context.Context, k *Kaynak) error
	// bos: bu senaryoda HİÇ uzak komut gitmemeli (ör. cPanel/DA sahip keşfi).
	bos bool
	// hata: beklenen hata metni ("" = hata yok).
	hata string
}

func altinSenaryolari() []altinSenaryo {
	s := []altinSenaryo{
		{ad: "ortak/tespit", tip: "plesk", calis: func(ctx context.Context, k *Kaynak) error {
			_, err := k.PanelTespit(ctx)
			return err
		}},
		{ad: "ortak/baglanti", tip: "plesk", calis: func(ctx context.Context, k *Kaynak) error {
			_, err := k.BaglantiTesti(ctx)
			return err
		}},
	}
	for _, tip := range altinTipler {
		s = append(s,
			altinSenaryo{ad: tip + "/kesif", tip: tip, calis: func(ctx context.Context, k *Kaynak) error {
				_, err := k.Kesfet(ctx)
				return err
			}},
			altinSenaryo{ad: tip + "/sahipler", tip: tip, bos: tip == "cpanel" || tip == "directadmin",
				calis: func(ctx context.Context, k *Kaynak) error {
					_, err := k.KesfetSahipler(ctx)
					return err
				}},
			// dbAktar sahte akış hatasında import'a (DB/mysql) varmadan döner.
			altinSenaryo{ad: tip + "/dump", tip: tip, hata: "dump: sahte stderr",
				calis: func(ctx context.Context, k *Kaynak) error {
					return (&Handlers{}).dbAktar(ctx, k, altinDB, altinHedefDB, func(string, ...any) {})
				}},
			altinSenaryo{ad: tip + "/dns", tip: tip, calis: func(ctx context.Context, k *Kaynak) error {
				kaynakZoneOku(ctx, k, altinAlan)
				return nil
			}},
			altinSenaryo{ad: tip + "/sertifika", tip: tip, calis: func(ctx context.Context, k *Kaynak) error {
				_, _, err := kaynakCertOku(ctx, k, altinAlan)
				return err
			}},
			altinSenaryo{ad: tip + "/posta", tip: tip, calis: func(ctx context.Context, k *Kaynak) error {
				_, err := pleskMailKesfet(ctx, k, altinAlan)
				return err
			}},
		)
	}
	return s
}

// komutAltin — senaryo → kaydedilen çağrı dizisinin sha256'sı (taban aef5088).
// Aynı değerler bilinçlidir: sertifika/posta/dns komutları bugün tipten bağımsız
// (posta her tipte Plesk komutunu gönderir), cPanel ile gPanel dump'ı kimliksiz;
// e3b0c442… boş dizgenin sha'sıdır (uzak komut YOK).
var komutAltin = map[string]string{
	"ortak/tespit":          "660f481e7c2e37f51be0379fa040df3529011a5e3ef509c98834aeff2ddcb4d5",
	"ortak/baglanti":        "992d9d1ac6630a7e11070d3f6e86bd92e08e2116d0dfa7357089b49fb54b1ddc",
	"plesk/kesif":           "b6bee2bc4f1037cbc5ad65b1bfeba288fc8ea2aab2ced721e10e72698c2a817c",
	"plesk/sahipler":        "1494de69ea74aac5b65c333c2ae3423aaa07e1bce476e57c0baba2e081896c03",
	"plesk/dump":            "4594b08ba38f4f4b76d9e97a900cd21dc9cb6e258e595acc5f0921a8f82f19cb",
	"plesk/dns":             "d0161e91c90434253c70bf8c60a1a592220d4dd47b67358dbbcfe512a95ecd47",
	"plesk/sertifika":       "d40ca371e3134654cc2fc4be0bd8bc588ae6521606d920736d8f0adaaf410779",
	"plesk/posta":           "bf1ebe8bf9f570cb7a7df018d080664eb87abb8cd2f7541f7d4b35e21d10b6b0",
	"cpanel/kesif":          "b5597bc3464a1ecb389941fdcc16936db841da2b1856496ba8d7b3d2ee1cd893",
	"cpanel/sahipler":       "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	"cpanel/dump":           "6f1199f11a0adf808c36b05fe5228ac9139d237981d553e8c92e644e7f322d48",
	"cpanel/dns":            "58d9451a3f19848a5bb0325b2ee2e5fcb67f7d3e77594a09a9140d559f2abd71",
	"cpanel/sertifika":      "d40ca371e3134654cc2fc4be0bd8bc588ae6521606d920736d8f0adaaf410779",
	"cpanel/posta":          "bf1ebe8bf9f570cb7a7df018d080664eb87abb8cd2f7541f7d4b35e21d10b6b0",
	"directadmin/kesif":     "cccdee8fde2b14767cbe3d5e31a492f5c5712fe098349f0dba1b6563483dc839",
	"directadmin/sahipler":  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	"directadmin/dump":      "8eafcd0d3ed63a9bdbaf9c5b9bd60f43b66a0a9b178fdbe2e55fd39d690c3a7e",
	"directadmin/dns":       "58d9451a3f19848a5bb0325b2ee2e5fcb67f7d3e77594a09a9140d559f2abd71",
	"directadmin/sertifika": "d40ca371e3134654cc2fc4be0bd8bc588ae6521606d920736d8f0adaaf410779",
	"directadmin/posta":     "bf1ebe8bf9f570cb7a7df018d080664eb87abb8cd2f7541f7d4b35e21d10b6b0",
	"gpanel/kesif":          "df5366a18e71aff91f64eb3de58d6748659b8a27e01a966d0c97b2c011546645",
	"gpanel/sahipler":       "3ef8cbb4d4fe5358067a5779783ff3924cd7ff48f789fa3533500bad7f61291f",
	"gpanel/dump":           "6f1199f11a0adf808c36b05fe5228ac9139d237981d553e8c92e644e7f322d48",
	"gpanel/dns":            "58d9451a3f19848a5bb0325b2ee2e5fcb67f7d3e77594a09a9140d559f2abd71",
	"gpanel/sertifika":      "d40ca371e3134654cc2fc4be0bd8bc588ae6521606d920736d8f0adaaf410779",
	"gpanel/posta":          "bf1ebe8bf9f570cb7a7df018d080664eb87abb8cd2f7541f7d4b35e21d10b6b0",
}

func TestKomutAltin(t *testing.T) {
	gorulen := map[string]bool{}
	for _, sn := range altinSenaryolari() {
		sn := sn
		gorulen[sn.ad] = true
		t.Run(sn.ad, func(t *testing.T) {
			kay := &kaydedici{yanit: altinYanit}
			err := sn.calis(context.Background(), altinKaynak(sn.tip, kay))
			hataMetni := ""
			if err != nil {
				hataMetni = err.Error()
			}
			if hataMetni != sn.hata {
				t.Errorf("hata = %q, beklenen %q", hataMetni, sn.hata)
			}
			if sn.bos != (len(kay.cagrilar) == 0) {
				t.Errorf("uzak çağrı sayısı %d (boş beklenen=%v)", len(kay.cagrilar), sn.bos)
			}
			bulunan := kay.ozet()
			// GOSP_ALTIN_DOKUM=<dizin>: ham çağrılar incelemek için dosyaya yazılır
			// (bir değişikliğin hangi komutu etkilediğini görmek için).
			if dizin := os.Getenv("GOSP_ALTIN_DOKUM"); dizin != "" {
				yol := filepath.Join(dizin, strings.ReplaceAll(sn.ad, "/", "_")+".txt")
				if err := os.WriteFile(yol, []byte(strings.Join(kay.cagrilar, "\n\x1e\n")), 0o600); err != nil {
					t.Logf("döküm yazılamadı: %v", err)
				}
			}
			if beklenen, ok := komutAltin[sn.ad]; !ok || beklenen != bulunan {
				ilk := ""
				if len(kay.cagrilar) > 0 {
					ilk = kay.cagrilar[0]
				}
				t.Errorf("uzak komut dizgesi DEĞİŞTİ (%d çağrı; ilk: %.200q)\nALTIN %q: %q,",
					len(kay.cagrilar), ilk, sn.ad, bulunan)
			}
		})
	}
	for ad := range komutAltin {
		if !gorulen[ad] {
			t.Errorf("altın tabloda senaryosu olmayan anahtar: %s", ad)
		}
	}
}

// Taşınan akış bloğunun hata sözleşmesi: yerel hazırlık hatası (anahtar dosyası)
// "dump: …" diye SARILMADAN aynen döner (eski dbAktar davranışı); uzak hata ise
// maskelenip kısaltılmış stderr ile "dump: " önekiyle döner.
func TestDbAktarAkisHataSozlesmesi(t *testing.T) {
	ic := errors.New("anahtar yazilamadi: sahte")
	kay := &kaydedici{akisHata: &akisHazirlikHatasi{err: ic}}
	err := (&Handlers{}).dbAktar(context.Background(), altinKaynak("plesk", kay), altinDB, altinHedefDB, func(string, ...any) {})
	if err != ic {
		t.Fatalf("hazırlık hatası aynen dönmeli: %v", err)
	}

	uzun := "Access denied " + altinParola + " " + strings.Repeat("x", 300)
	kay = &kaydedici{akisStderr: uzun, akisHata: fmt.Errorf("exit status 2")}
	err = (&Handlers{}).dbAktar(context.Background(), altinKaynak("plesk", kay), altinDB, altinHedefDB, func(string, ...any) {})
	beklenen := "dump: " + kisalt(temizHata(uzun, altinParola), 200)
	if err == nil || err.Error() != beklenen {
		t.Fatalf("uzak hata = %v, beklenen %q", err, beklenen)
	}
	if strings.Contains(err.Error(), altinParola) {
		t.Fatalf("parola hata metnine sızdı: %q", err.Error())
	}
}
