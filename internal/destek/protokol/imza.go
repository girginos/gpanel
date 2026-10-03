package protokol

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"
)

// Kanonik bayt biçimi (sürüm 1)
//
// İmza, JSON metni üzerinden DEĞİL aşağıdaki bayt dizisi üzerinden atılır.
// İlkel kodlamalar:
//
//	dizge(s) = uint32_be(len(s)) || s          (UTF-8 baytları, ham)
//	sayi(n)  = uint64_be(uint64(int64 n))      (ikiye tümleyen)
//
// İmzalı mesaj ([IsEmri], [EslesmeOnayla]) için alanlar SABİT sırayla:
//
//	dizge("girginos-destek/v1/" + tip)   // alan ayırıcı: tipler arası imza taşınamaz
//	sayi(Surum)
//	dizge(anahtar_kimligi) dizge(nonce) sayi(verilme) sayi(son_gecerlilik)
//	+ tipe özgü gövde:
//	  is_emri:        dizge(is_kodu) dizge(oturum) dizge(baglanti_id) dizge(arac)
//	                  dizge(risk) dizge(json.Compact(param))
//	  eslesme_onayla: dizge(kod_ozet) sayi(tg_user_id) dizge(tg_kullanici_adi)
//
// meydan_okuma (merkez imzalar) ve meydan_yanit (cihaz imzalar):
//
//	dizge("girginos-destek/v1/meydan_okuma" | ".../meydan_yanit") sayi(Surum)
//	dizge(meydan) sayi(son_gecerlilik) dizge(merkez_anahtar_kimligi)
//	dizge(cihaz_anahtari) dizge(lisans_parmak_izi)
//
// "imza" alanının kendisi kanonik baytlara GİRMEZ. param, imzalamadan önce
// json.Compact ile sıkıştırılır ve kabloya o haliyle konur; doğrulayıcı da
// sıkıştırarak hesaplar (yalnız boşluk farkı serbesttir, anahtar sırası değil).

const alanOneki = "girginos-destek/v1/"

type kanonik struct{ b []byte }

func (k *kanonik) dizge(s string) {
	k.b = binary.BigEndian.AppendUint32(k.b, uint32(len(s)))
	k.b = append(k.b, s...)
}

func (k *kanonik) sayi(n int64) { k.b = binary.BigEndian.AppendUint64(k.b, uint64(n)) }

// ImzaliMesaj: merkez anahtarıyla imzalanan gövdeler ([IsEmri], [EslesmeOnayla]).
type ImzaliMesaj interface {
	Govde
	imza() *ImzaAlanlari
	kanonikGovde(k *kanonik) error
}

func (m *IsEmri) kanonikGovde(k *kanonik) error {
	var p bytes.Buffer
	if err := json.Compact(&p, m.Param); err != nil {
		return hataf(HataBicim, "param: %v", err)
	}
	k.dizge(m.IsKodu)
	k.dizge(m.Oturum)
	k.dizge(m.BaglantiID)
	k.dizge(m.Arac)
	k.dizge(string(m.Risk))
	k.dizge(p.String())
	return nil
}

func (m *EslesmeOnayla) kanonikGovde(k *kanonik) error {
	k.dizge(m.KodOzet)
	k.sayi(m.TgUserID)
	k.dizge(m.TgKullaniciAdi)
	return nil
}

// KanonikBaytlar: m'nin imzalanan bayt dizisi (biçim yukarıda).
func KanonikBaytlar(m ImzaliMesaj) ([]byte, error) {
	a := m.imza()
	k := &kanonik{}
	k.dizge(alanOneki + string(m.Tip()))
	k.sayi(Surum)
	k.dizge(a.AnahtarKimligi)
	k.dizge(a.Nonce)
	k.sayi(a.Verilme)
	k.sayi(a.SonGecerlilik)
	if err := m.kanonikGovde(k); err != nil {
		return nil, err
	}
	return k.b, nil
}

func meydanKanonik(tur string, m *MeydanOkuma, kay *Kayit) []byte {
	k := &kanonik{}
	k.dizge(alanOneki + tur)
	k.sayi(Surum)
	k.dizge(m.Meydan)
	k.sayi(m.SonGecerlilik)
	k.dizge(m.MerkezAnahtarKimligi)
	k.dizge(kay.CihazAnahtari)
	k.dizge(kay.LisansParmakIzi)
	return k.b
}

// ---- anahtarlar ----

// AnahtarCifti: Ed25519 anahtar çifti ve kimliği.
type AnahtarCifti struct {
	Acik   ed25519.PublicKey
	Ozel   ed25519.PrivateKey
	Kimlik string
}

// AnahtarKimligi: sha256(açık anahtar) ilk 8 baytı, küçük hex (16 karakter).
func AnahtarKimligi(acik ed25519.PublicKey) string {
	h := sha256.Sum256(acik)
	return hex.EncodeToString(h[:8])
}

// AnahtarCiftiUret: yeni çift; r nil → crypto/rand. Merkez anahtarı ve cihaz anahtarı için.
func AnahtarCiftiUret(r io.Reader) (*AnahtarCifti, error) {
	if r == nil {
		r = rand.Reader
	}
	acik, ozel, err := ed25519.GenerateKey(r)
	if err != nil {
		return nil, err
	}
	return &AnahtarCifti{Acik: acik, Ozel: ozel, Kimlik: AnahtarKimligi(acik)}, nil
}

// AnahtarCiftiTohumdan: 32 baytlık tohumdan (diskte saklanan biçim) çift.
func AnahtarCiftiTohumdan(tohum []byte) (*AnahtarCifti, error) {
	if len(tohum) != ed25519.SeedSize {
		return nil, hataf(HataBicim, "tohum %d bayt", len(tohum))
	}
	ozel := ed25519.NewKeyFromSeed(tohum)
	acik := ozel.Public().(ed25519.PublicKey)
	return &AnahtarCifti{Acik: acik, Ozel: ozel, Kimlik: AnahtarKimligi(acik)}, nil
}

// AcikAnahtarKodla / AcikAnahtarCoz: kablodaki ve panele gömülü biçim (base64url, dolgusuz).
func AcikAnahtarKodla(acik ed25519.PublicKey) string { return b64.EncodeToString(acik) }

func AcikAnahtarCoz(s string) (ed25519.PublicKey, error) {
	b, err := b64Coz("acik_anahtar", s, ed25519.PublicKeySize, ed25519.PublicKeySize)
	if err != nil {
		return nil, err
	}
	return ed25519.PublicKey(b), nil
}

func rastgele(r io.Reader, n int) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return b64.EncodeToString(b), nil
}

// YeniIsKodu: 8 karakter, IsKoduAlfabesi'nden (r nil → crypto/rand).
func YeniIsKodu(r io.Reader) (string, error) {
	if r == nil {
		r = rand.Reader
	}
	out := make([]byte, 8)
	var tek [1]byte
	for i := 0; i < len(out); {
		if _, err := io.ReadFull(r, tek[:]); err != nil {
			return "", err
		}
		// 31 karakterlik alfabe: 248 = 8*31 üstü atılır (sapmasız)
		if int(tek[0]) >= 8*len(IsKoduAlfabesi) {
			continue
		}
		out[i] = IsKoduAlfabesi[int(tek[0])%len(IsKoduAlfabesi)]
		i++
	}
	return string(out), nil
}

// ---- merkez: imzalayıcı ----

// Imzalayici: merkezin özel anahtarıyla imzalı mesaj ve meydan okuma üretir.
type Imzalayici struct {
	cift     *AnahtarCifti
	Saat     func() time.Time
	Rastgele io.Reader
}

func YeniImzalayici(c *AnahtarCifti) *Imzalayici {
	return &Imzalayici{cift: c, Saat: time.Now, Rastgele: rand.Reader}
}

// Imzala: nonce/verilme/son_gecerlilik/anahtar_kimligi alanlarını doldurur ve imzalar.
// gecerlilik (0, EnUzunGecerlilik] olmalı. IsEmri.Param sıkıştırılarak yerinde değiştirilir.
func (s *Imzalayici) Imzala(m ImzaliMesaj, gecerlilik time.Duration) error {
	if gecerlilik <= 0 || gecerlilik > EnUzunGecerlilik {
		return hataf(HataGecerlilikUzun, "%v", gecerlilik)
	}
	nonce, err := rastgele(s.Rastgele, 16)
	if err != nil {
		return err
	}
	simdi := s.Saat()
	return s.imzalaZaman(m, nonce, simdi.Unix(), simdi.Add(gecerlilik).Unix())
}

func (s *Imzalayici) imzalaZaman(m ImzaliMesaj, nonce string, verilme, son int64) error {
	if e, ok := m.(*IsEmri); ok {
		var p bytes.Buffer
		if err := json.Compact(&p, e.Param); err != nil {
			return hataf(HataBicim, "param: %v", err)
		}
		e.Param = p.Bytes()
	}
	a := m.imza()
	a.AnahtarKimligi = s.cift.Kimlik
	a.Nonce = nonce
	a.Verilme = verilme
	a.SonGecerlilik = son
	kb, err := KanonikBaytlar(m)
	if err != nil {
		return err
	}
	a.Imza = b64.EncodeToString(ed25519.Sign(s.cift.Ozel, kb))
	return m.Dogrula()
}

// MeydanOlustur: kayit'a karşılık merkez imzalı meydan okuma. Merkez meydanı bağlantıya
// bağlı TEK KULLANIMLIK saklamalı (yanıt gelince ya da süre dolunca silinir).
func (s *Imzalayici) MeydanOlustur(kay *Kayit) (*MeydanOkuma, error) {
	if err := kay.Dogrula(); err != nil {
		return nil, err
	}
	meydan, err := rastgele(s.Rastgele, 32)
	if err != nil {
		return nil, err
	}
	m := &MeydanOkuma{
		Meydan:               meydan,
		SonGecerlilik:        s.Saat().Add(MeydanGecerlilik).Unix(),
		MerkezAnahtarKimligi: s.cift.Kimlik,
	}
	m.MerkezImza = b64.EncodeToString(ed25519.Sign(s.cift.Ozel, meydanKanonik(string(TipMeydanOkuma), m, kay)))
	return m, m.Dogrula()
}

// MeydanYanitla: ajan tarafı; cihaz özel anahtarıyla yanıt. Önce [Dogrulayici.MeydanDogrula] çağrılmalı.
func MeydanYanitla(cihazOzel ed25519.PrivateKey, m *MeydanOkuma, kay *Kayit) (*MeydanYanit, error) {
	if len(cihazOzel) != ed25519.PrivateKeySize {
		return nil, hataf(HataBicim, "cihaz anahtarı")
	}
	if subtle.ConstantTimeCompare(cihazOzel.Public().(ed25519.PublicKey), mustB64(kay.CihazAnahtari)) != 1 {
		return nil, hataf(HataMeydan, "cihaz anahtarı kayıttakiyle uyuşmuyor")
	}
	y := &MeydanYanit{
		Meydan: m.Meydan,
		Imza:   b64.EncodeToString(ed25519.Sign(cihazOzel, meydanKanonik(string(TipMeydanYanit), m, kay))),
	}
	return y, y.Dogrula()
}

func mustB64(s string) []byte {
	b, _ := b64.DecodeString(s)
	return b
}

// MeydanYanitDogrula: merkez tarafı; m merkezin gönderdiği (sakladığı) meydan okuma.
func MeydanYanitDogrula(m *MeydanOkuma, kay *Kayit, y *MeydanYanit, simdi time.Time) error {
	if err := kay.Dogrula(); err != nil {
		return err
	}
	if err := y.Dogrula(); err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(m.Meydan), []byte(y.Meydan)) != 1 {
		return hataf(HataMeydan, "meydan uyuşmuyor")
	}
	if simdi.Unix() > m.SonGecerlilik {
		return hataf(HataSuresiDoldu, "meydan")
	}
	acik, _ := b64Coz("cihaz_anahtari", kay.CihazAnahtari, 32, 32)
	imza, _ := b64Coz("imza", y.Imza, 64, 64)
	if !ed25519.Verify(ed25519.PublicKey(acik), meydanKanonik(string(TipMeydanYanit), m, kay), imza) {
		return hataf(HataImza, "meydan_yanit")
	}
	return nil
}

// ---- ajan: doğrulayıcı ----

// Dogrulayici: ajan tarafı. Panele gömülü merkez açık anahtar(lar)ı ile imzalı
// mesajları ve meydan okumayı doğrular; tekrar önbelleğine yazar.
type Dogrulayici struct {
	anahtarlar map[string]ed25519.PublicKey
	onbellek   TekrarOnbellegi
	Saat       func() time.Time
	Sapma      time.Duration
	baslangic  int64
}

// YeniDogrulayici: saat nil → time.Now. Başlangıç anı kaydedilir: önbellek bellekte
// olduğundan yeniden başlatma ÖNCESİ verilmiş mesajlar reddedilir (yeniden oynatma penceresi kapanır).
func YeniDogrulayici(saat func() time.Time, onbellek TekrarOnbellegi, acikAnahtarlar ...ed25519.PublicKey) (*Dogrulayici, error) {
	if saat == nil {
		saat = time.Now
	}
	if onbellek == nil || len(acikAnahtarlar) == 0 {
		return nil, hataf(HataIc, "önbellek ve en az bir anahtar gerekli")
	}
	d := &Dogrulayici{anahtarlar: map[string]ed25519.PublicKey{}, onbellek: onbellek, Saat: saat, Sapma: SaatSapmasi, baslangic: saat().Unix()}
	for _, a := range acikAnahtarlar {
		if len(a) != ed25519.PublicKeySize {
			return nil, hataf(HataBicim, "açık anahtar %d bayt", len(a))
		}
		d.anahtarlar[AnahtarKimligi(a)] = a
	}
	return d, nil
}

// Dogrula: biçim → anahtar → imza → zaman → tekrar. nil dönerse mesaj BİR KEZ yürütülebilir.
func (d *Dogrulayici) Dogrula(m ImzaliMesaj) error {
	if err := m.Dogrula(); err != nil {
		return err
	}
	a := m.imza()
	acik, ok := d.anahtarlar[a.AnahtarKimligi]
	if !ok {
		return hataf(HataAnahtarBilinmiyor, "%s", a.AnahtarKimligi)
	}
	imza, err := b64Coz("imza", a.Imza, ed25519.SignatureSize, ed25519.SignatureSize)
	if err != nil {
		return err
	}
	kb, err := KanonikBaytlar(m)
	if err != nil {
		return err
	}
	if !ed25519.Verify(acik, kb, imza) {
		return hataf(HataImza, "%s", m.Tip())
	}
	simdi := d.Saat()
	sapma := int64(d.Sapma / time.Second)
	if a.SonGecerlilik <= a.Verilme {
		return hataf(HataZaman, "son_gecerlilik ≤ verilme")
	}
	if a.SonGecerlilik-a.Verilme > int64(EnUzunGecerlilik/time.Second) {
		return hataf(HataGecerlilikUzun, "%d sn", a.SonGecerlilik-a.Verilme)
	}
	if a.Verilme > simdi.Unix()+sapma {
		return hataf(HataZaman, "verilme gelecekte")
	}
	if simdi.Unix() > a.SonGecerlilik+sapma {
		return hataf(HataSuresiDoldu, "%s", m.Tip())
	}
	if a.Verilme < d.baslangic-sapma {
		return hataf(HataSuresiDoldu, "yeniden başlatma öncesi verilmiş")
	}
	return d.onbellek.Isaretle(string(m.Tip())+"|"+a.AnahtarKimligi+"|"+a.Nonce, time.Unix(a.SonGecerlilik+sapma+1, 0), simdi)
}

// MeydanDogrula: ajan tarafı; meydan okumanın sabitlenmiş merkez anahtarıyla imzalı,
// bu kayda ait ve süresi içinde olduğunu doğrular.
func (d *Dogrulayici) MeydanDogrula(m *MeydanOkuma, kay *Kayit) error {
	if err := m.Dogrula(); err != nil {
		return err
	}
	acik, ok := d.anahtarlar[m.MerkezAnahtarKimligi]
	if !ok {
		return hataf(HataAnahtarBilinmiyor, "%s", m.MerkezAnahtarKimligi)
	}
	imza, _ := b64Coz("merkez_imza", m.MerkezImza, 64, 64)
	if !ed25519.Verify(acik, meydanKanonik(string(TipMeydanOkuma), m, kay), imza) {
		return hataf(HataImza, "meydan_okuma")
	}
	simdi := d.Saat().Unix()
	sapma := int64(d.Sapma / time.Second)
	if simdi > m.SonGecerlilik+sapma {
		return hataf(HataSuresiDoldu, "meydan")
	}
	if m.SonGecerlilik-simdi > int64(MeydanGecerlilik/time.Second)+sapma {
		return hataf(HataGecerlilikUzun, "meydan")
	}
	return nil
}
