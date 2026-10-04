package merkez

// kvkk_test.go — KVKK: kullanıcı metni maskesi (Anthropic'e gitmeden), araç sonucu hub maskesi,
// /unutbeni gerçek silme, bot KVKK metni sürüm eşitliği.

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"girginospanel/internal/destek/protokol"
)

// kvTckn — 9 haneden geçerli T.C. kimlik no (resmi sağlama algoritması).
func kvTckn(d9 string) string {
	var r [11]int
	for i := 0; i < 9; i++ {
		r[i] = int(d9[i] - '0')
	}
	r[9] = (((r[0]+r[2]+r[4]+r[6]+r[8])*7-(r[1]+r[3]+r[5]+r[7]))%10 + 10) % 10
	for i := 0; i < 10; i++ {
		r[10] += r[i]
	}
	r[10] %= 10
	var b strings.Builder
	for _, v := range r {
		b.WriteString(strconv.Itoa(v))
	}
	return b.String()
}

// kvGizliler — kullanıcı mesajına gömülen kişisel/gizli değerler (kaynakta sır biçimli dizge durmasın diye birleştirilir).
func kvGizliler() (tc, iban, kart, parola, tel, eposta string) {
	return kvTckn("123456789"), "TR33 0006 1005 1978 6457 8413 26", "4111 " + "1111 " + "1111 " + "1111",
		"GizliParola" + "9!x", "0532 123 45 67", "ali.veli@ornek.com.tr"
}

func istekGovdeleri(o *ortam) string {
	var sb strings.Builder
	o.c.mu.Lock()
	defer o.c.mu.Unlock()
	for _, i := range o.c.istekler {
		sb.Write(i.Govde)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func TestKVKKKullaniciMetniModeleMaskeliGider(t *testing.T) {
	o := yeniOrtam(t, nil)
	sahteAjanBagla(t, o, nil)
	o.baglantiEkle(t, tTg, "s_0000000000000001", protokol.RolKok, "ornek.com.tr")
	tc, iban, kart, parola, tel, eposta := kvGizliler()
	o.c.ana = func(n int, ist ClaudeIstek) (*ClaudeYanit, error) {
		return yanit("end_turn", bMetin("Anladım, siteyi inceliyorum.")), nil
	}
	o.mesaj(t, tTg, "site açılmıyor. T.C. no "+tc+", iban "+iban+", kartım "+kart+", şifrem: "+parola+", password="+parola+", tel "+tel+", mail "+eposta+". Sunucu 203.0.113.77")

	govdeler := istekGovdeleri(o)
	if o.c.toplam() < 2 {
		t.Fatalf("triyaj + ana model çağrılmadı (%d)", o.c.toplam())
	}
	for ad, deger := range map[string]string{"tckn": tc, "iban": "6457 8413", "kart": "1111 1111", "parola": parola, "telefon": "123 45 67", "eposta-yerel": "ali.veli", "ip": "203.0.113.77"} {
		if strings.Contains(govdeler, deger) {
			t.Errorf("model isteğinde %s sızdı: %q", ad, deger)
		}
	}
	// teşhis için anlam korunur: sorun metni, e-posta alan adı, /24'e kırpılmış IP
	for _, olmali := range []string{"site açılmıyor", "ornek.com.tr", "203.0.113.0"} {
		if !strings.Contains(govdeler, olmali) {
			t.Errorf("model isteğinde beklenen bağlam yok: %q", olmali)
		}
	}
	// cevap yolu çalışır
	if !strings.Contains(o.tg.son(tTg), "siteyi inceliyorum") {
		t.Fatalf("cevap kullanıcıya ulaşmadı: %q", o.tg.son(tTg))
	}
	// hub konuşma kaydı da maskeli (kalıcı depo)
	for _, k := range o.depo.konusmaBellek() {
		for _, deger := range []string{tc, "6457 8413", "1111 1111", parola, "123 45 67", "ali.veli"} {
			if strings.Contains(k.Metin, deger) {
				t.Errorf("konuşma kaydında sızıntı: %q (%s)", deger, k.Yon)
			}
		}
	}
}

// Savunma derinliği: bot.go zaten maskelese de modele giden HER yol kendi başına maskeler
// (triyaj ve ana model ayrı ayrı; çağıran bu fonksiyonlara ham metin verirse bile).
func TestKVKKTriyajVeAnaModelHamMetniKendisiMaskeler(t *testing.T) {
	tc, iban, kart, parola, tel, eposta := kvGizliler()
	ham := "site açılmıyor tc " + tc + " iban " + iban + " kart " + kart + " şifrem: " + parola + " tel " + tel + " mail " + eposta
	yasak := map[string]string{"tckn": tc, "iban": "6457 8413", "kart": "1111 1111", "parola": parola, "telefon": "123 45 67", "eposta": "ali.veli"}

	t.Run("triyaj", func(t *testing.T) {
		o := yeniOrtam(t, nil)
		if _, err := o.m.triyaj(t.Context(), "s_0000000000000001", ham); err != nil {
			t.Fatal(err)
		}
		g := istekGovdeleri(o)
		if !strings.Contains(g, "site açılmıyor") {
			t.Fatal("triyaj isteği yok (test geçersiz)")
		}
		for ad, d := range yasak {
			if strings.Contains(g, d) {
				t.Errorf("triyaj isteğine %s sızdı", ad)
			}
		}
	})
	t.Run("ana", func(t *testing.T) {
		o := yeniOrtam(t, nil)
		sahteAjanBagla(t, o, nil)
		bg := o.baglantiEkle(t, tTg, "s_0000000000000001", protokol.RolKok, "ornek.com.tr")
		oturum, err := o.m.oturumlar.Al(bg, "tr")
		if err != nil {
			t.Fatal(err)
		}
		s := o.m.destekYanitla(t.Context(), oturum, ham, "tr", func(string) {})
		if s.Metin == "" {
			t.Fatal("cevap yolu çalışmadı")
		}
		var ana string
		for _, i := range o.c.anaIstekler() {
			ana += string(i.Govde)
		}
		if !strings.Contains(ana, "site açılmıyor") {
			t.Fatal("ana model isteği yok (test geçersiz)")
		}
		for ad, d := range yasak {
			if strings.Contains(ana, d) {
				t.Errorf("ana model isteğine %s sızdı", ad)
			}
		}
	})
}

func TestKVKKInsanDevriMesajiMaskeli(t *testing.T) {
	o := yeniOrtam(t, nil)
	sahteAjanBagla(t, o, nil)
	o.baglantiEkle(t, tTg, "s_0000000000000001", protokol.RolKok, "ornek.com.tr")
	o.c.niyet = "insan"
	tc, _, _, _, tel, _ := kvGizliler()
	o.mesaj(t, tTg, "insanla görüşmek istiyorum, tc "+tc+" tel "+tel)
	var grup string
	for _, k := range o.tg.hepsi() {
		if k.Chat == tDestekGrubu {
			grup = k.Metin
		}
	}
	if grup == "" {
		t.Fatal("devir mesajı destek grubuna gitmedi")
	}
	if strings.Contains(grup, tc) || strings.Contains(grup, "123 45 67") || strings.Contains(istekGovdeleri(o), tc) {
		t.Fatalf("devir/model yolunda sızıntı: %q", grup)
	}
}

// Komutlar (eşleştirme kodu dahil) maskeden ÖNCE ayrılır: /start KOD akışı etkilenmez.
func TestKVKKKomutlarMaskelenmez(t *testing.T) {
	o := yeniOrtam(t, nil)
	ajan := sahteAjanBagla(t, o, nil)
	kod, _ := o.kodAl(t, ajan.kayit.LisansParmakIzi, "okuma", 7)
	o.mesaj(t, tTg, "/start "+protokol.EslesmeKoduGorunen(kod))
	bekle(t, func() bool { ajan.mu.Lock(); defer ajan.mu.Unlock(); return len(ajan.eslesmeler) == 1 })
	if o.c.toplam() != 0 {
		t.Fatal("komut modele gitti")
	}
}

func TestKVKKAracSonucuHubdaIkinciMaske(t *testing.T) {
	o := yeniOrtam(t, nil)
	tc, iban, kart, parola, _, _ := kvGizliler()
	kanarya := "hub-bilinen-" + "sir-0123456789"
	o.sir.PortalIcAnahtar = kanarya // bilinen sır (Sirlar.Bilinenler)
	ajan := sahteAjanBagla(t, o, func(e *protokol.IsEmri) *protokol.IsSonucu {
		// eski/ele geçirilmiş ajan: maskesiz çıktı gönderir
		return &protokol.IsSonucu{Ok: true, Ozet: "kayıt sahibi kart " + kart + " " + kanarya, Satirlar: []string{
			"owner tc " + tc, "iban " + iban, "password=" + parola, "pid 12345 port 3306 worker 203.0.113.7 sürüm 8.2.12",
			"2026/10/04 10:00:01 [error] 1234#1234: *5678 connect() failed",
		}}
	})
	b := o.baglantiEkle(t, tTg, ajan.Sunucu, protokol.RolMusteri, "ornek.com.tr")
	ajan.baglam = protokol.OturumBaglami{Rol: b.Rol, Duzey: b.Duzey, Hesaplar: b.Hesaplar}
	o.c.ana = func(n int, ist ClaudeIstek) (*ClaudeYanit, error) {
		if n == 0 {
			return yanit("tool_use", bArac("tu_1", "site_yokla", `{"hesap":1}`)), nil
		}
		return yanit("end_turn", bMetin("Teşhis tamam.")), nil
	}
	o.mesaj(t, tTg, "site 502 veriyor")
	g := istekGovdeleri(o)
	if !strings.Contains(g, "veri_guvenilmez") {
		t.Fatal("araç sonucu modele gitmedi (test geçersiz)")
	}
	for ad, deger := range map[string]string{"tckn": tc, "iban": "6457 8413", "kart": "1111 1111", "parola": parola, "bilinen sır": kanarya} {
		if strings.Contains(g, deger) {
			t.Errorf("araç sonucu hub maskesinden geçmedi, %s sızdı: %q", ad, deger)
		}
	}
	// teşhis çıktısı bozulmadı: PID, port, IP (araç yolunda IP kırpılmaz — ajan zaten kırpar), sürüm, zaman
	for _, olmali := range []string{"pid 12345", "port 3306", "203.0.113.7", "8.2.12", "2026/10/04 10:00:01", "1234#1234"} {
		if !strings.Contains(g, olmali) {
			t.Errorf("araç çıktısı bozuldu: %q yok", olmali)
		}
	}
	if !bytes.Contains([]byte(o.tg.son(tTg)), []byte("Teşhis tamam")) {
		t.Fatalf("cevap yolu: %q", o.tg.son(tTg))
	}
}

// ---- /unutbeni ----

func unutOrtam(t *testing.T, ajanBagli bool) (*ortam, *sahteAjan) {
	o := yeniOrtam(t, nil)
	var ajan *sahteAjan
	sunucu := "s_0000000000000001"
	if ajanBagli {
		ajan = sahteAjanBagla(t, o, nil)
		sunucu = ajan.Sunucu
	}
	o.baglantiEkle(t, tTg, sunucu, protokol.RolKok, "ornek.com.tr")
	o.mesaj(t, tTg, "/dil en")
	o.mesaj(t, tTg, "site açılmıyor, tel 0532 123 45 67")
	// başka kullanıcı: silinmemeli
	o.baglantiEkle(t, tTg+1, sunucu, protokol.RolKok, "diger.com")
	o.mesaj(t, tTg+1, "benim sitem de açılmıyor")
	return o, ajan
}

func TestUnutBeniHubdaGercekSilme_AjanBagli(t *testing.T) {
	o, ajan := unutOrtam(t, true)
	if len(o.depo.konusmaBellek()) == 0 || o.m.oturumlar.Sayi() != 2 {
		t.Fatalf("ön koşul: konuşma/oturum yok (oturum %d)", o.m.oturumlar.Sayi())
	}
	o.m.bot.startHiz.Izin(tgKisa(tTg), time.Now()) // /start denemesi sayacı tohumla
	if !o.m.bot.hiz.olaylarVar(tgKisa(tTg)) || !o.m.bot.startHiz.olaylarVar(tgKisa(tTg)) {
		t.Fatal("ön koşul: hız sayaçları yok")
	}
	o.mesaj(t, tTg, "/unutbeni")
	o.callback(t, tTg, "unut:evet")
	bekle(t, func() bool {
		ajan.mu.Lock()
		defer ajan.mu.Unlock()
		return len(ajan.iptaller) >= 1 && ajan.iptaller[len(ajan.iptaller)-1].Neden == protokol.IptalUnutBeni
	})
	if n := len(o.depo.KullaniciBaglantilari(tTg)); n != 0 {
		t.Fatalf("bağlantı kaldı: %d", n)
	}
	if _, var_ := o.depo.KullaniciAl(tTg); var_ {
		t.Fatal("kullanıcı tercihi kaldı")
	}
	for _, k := range o.depo.konusmaBellek() {
		if k.Tg == tgKisa(tTg) {
			t.Fatalf("konuşma satırı kaldı: %+v", k)
		}
	}
	if len(o.depo.KullaniciBaglantilari(tTg+1)) != 1 {
		t.Fatal("başka kullanıcının bağlantısı silindi")
	}
	var diger bool
	for _, k := range o.depo.konusmaBellek() {
		diger = diger || k.Tg == tgKisa(tTg+1)
	}
	if !diger {
		t.Fatal("başka kullanıcının konuşması silindi")
	}
	if o.m.bot.hiz.olaylarVar(tgKisa(tTg)) || o.m.bot.startHiz.olaylarVar(tgKisa(tTg)) {
		t.Fatal("hız sayacı kaldı")
	}
	if n := o.m.oturumlar.Sayi(); n != 1 {
		t.Fatalf("bellek oturumu kapanmadı: %d (beklenen: yalnız başka kullanıcının 1 oturumu)", n)
	}
	son := o.tg.son(tTg)
	for _, s := range []string{"Deleted", "Telegram ID", "tg:silindi", "Not deletable"} {
		if !strings.Contains(son, s) {
			t.Errorf("bot yanıtı kapsamı açıkça söylemiyor (%q yok): %q", s, son)
		}
	}
	if strings.Contains(son, "offline") {
		t.Errorf("ajan bağlıyken çevrimdışı mesajı: %q", son)
	}
}

func TestUnutBeniAjanCevrimdisi_MetinVeHubSilmesi(t *testing.T) {
	o, _ := unutOrtam(t, false) // sunucu için bağlı ajan YOK
	o.mesaj(t, tTg, "/unutbeni")
	o.callback(t, tTg, "unut:evet")
	if len(o.depo.KullaniciBaglantilari(tTg)) != 0 {
		t.Fatal("ajan çevrimdışıyken hub kaydı silinmedi")
	}
	son := o.tg.son(tTg)
	if !strings.Contains(son, "offline") || !strings.Contains(son, "reconnects") {
		t.Fatalf("çevrimdışı durumda yanıt: %q", son)
	}
	// çevrimdışı ajan yeniden bağlanıp bağlantıyı bildirince merkez kaydı olmadığı için iptal döner
	// (ajan tarafı: TestUnutBeni_CevrimdisiYenidenBaglanma_VeZatenIptal) — merkez davranışı:
	// TestEslesmeAkisi "merkezde karşılığı olmayan bağlantıyı ajan da kapatsın".
}

func TestUnutBeniUcusTakiIslemKayitEklemez(t *testing.T) {
	o, _ := unutOrtam(t, true)
	o.mesaj(t, tTg, "/unutbeni")
	o.callback(t, tTg, "unut:evet")
	o.m.konusmaKaydet(tTg, "s_0000000000000001", "bot", "uçuştaki yanıt")
	for _, k := range o.depo.konusmaBellek() {
		if k.Tg == tgKisa(tTg) {
			t.Fatalf("silme sonrası satır eklendi: %+v", k)
		}
	}
	o.m.unutulduKaldir(tTg) // yeniden eşleştirme
	o.m.konusmaKaydet(tTg, "s_0000000000000001", "bot", "yeni oturum")
	var n int
	for _, k := range o.depo.konusmaBellek() {
		if k.Tg == tgKisa(tTg) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("yeniden eşleştirme sonrası kayıt: %d", n)
	}
	// mezar taşı süresi dolunca kalkar
	o.m.unutuldu(tTg)
	if !o.m.unutulduMu(tTg) {
		t.Fatal("mezar taşı yok")
	}
	o.m.unutMu.Lock()
	o.m.unutulanlar[tTg] = time.Now().Add(-2 * unutmaBekleme)
	o.m.unutMu.Unlock()
	if o.m.unutulduMu(tTg) {
		t.Fatal("mezar taşı süresi dolmadı")
	}
}

func TestUnutBeniBekleyenEslesmeSilinir(t *testing.T) {
	o := yeniOrtam(t, nil)
	ajan := sahteAjanBagla(t, o, nil)
	kod, _ := o.kodAl(t, ajan.kayit.LisansParmakIzi, "okuma", 7)
	o.mesaj(t, tTg, "/start "+protokol.EslesmeKoduGorunen(kod))
	bekle(t, func() bool { o.m.ajan.mu.Lock(); defer o.m.ajan.mu.Unlock(); return len(o.m.ajan.eslesmeler) == 1 })
	o.mesaj(t, tTg, "/unutbeni")
	o.callback(t, tTg, "unut:evet")
	o.m.ajan.mu.Lock()
	n := len(o.m.ajan.eslesmeler)
	o.m.ajan.mu.Unlock()
	if n != 0 {
		t.Fatalf("bekleyen eşleştirme kaldı: %d", n)
	}
}

// Dosya deposunda: durum.json ve tüm konuşma dosyalarında kullanıcıya ait HİÇBİR iz kalmaz.
func TestKullaniciUnut_DosyaDeposuIzKalmaz(t *testing.T) {
	dizin := t.TempDir()
	dd, err := DosyaDepoAc(dizin)
	if err != nil {
		t.Fatal(err)
	}
	const tg, diger = int64(909090123456), int64(808080654321)
	simdi := time.Now().UTC()
	for _, id := range []int64{tg, diger} {
		_ = dd.KullaniciKaydet(Kullanici{TgUserID: id, Dil: "en", SeciliBaglanti: "bag_" + strconv.FormatInt(id, 10)})
		_ = dd.BaglantiKaydet(Baglanti{BaglantiID: "bag_" + strconv.FormatInt(id, 10), SunucuKimligi: "s_0000000000000001", TgUserID: id,
			Durum: protokol.BaglantiEtkin, Rol: protokol.RolKok, Duzey: protokol.DuzeyOkuma, KVKKKabul: simdi})
		for _, gun := range []int{0, 1, 2} {
			_ = dd.KonusmaEkle(KonusmaKaydi{Zaman: simdi.AddDate(0, 0, -gun), Tg: tgKisa(id), Sunucu: "s_0000000000000001", Yon: "kullanici", Metin: "ileti-" + strconv.FormatInt(id, 10)})
		}
	}
	if err := dd.KullaniciUnut(tg); err != nil {
		t.Fatal(err)
	}
	var tum strings.Builder
	err = filepath.Walk(dizin, func(yol string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		b, _ := os.ReadFile(yol)
		tum.WriteString(yol + "\n" + string(b) + "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s := tum.String()
	for ad, iz := range map[string]string{"tg id": strconv.FormatInt(tg, 10), "tg özeti": tgKisa(tg), "mesaj": "ileti-" + strconv.FormatInt(tg, 10), "bağlantı": "bag_" + strconv.FormatInt(tg, 10)} {
		if strings.Contains(s, iz) {
			t.Errorf("hub durum/konuşma dosyalarında iz kaldı (%s): %q", ad, iz)
		}
	}
	for _, kal := range []string{strconv.FormatInt(diger, 10), tgKisa(diger), "ileti-" + strconv.FormatInt(diger, 10)} {
		if !strings.Contains(s, kal) {
			t.Errorf("başka kullanıcının verisi silindi: %q", kal)
		}
	}
	if strings.Count(s, "ileti-"+strconv.FormatInt(diger, 10)) != 3 {
		t.Error("başka kullanıcının konuşma satırı sayısı değişti")
	}
}

// ---- bot KVKK metni sürümü ----

var kvkkSurumRe = regexp.MustCompile(`(?:sürüm|version): (\S+)`)

func TestKvkkMetinSurumuMetinleEsit(t *testing.T) {
	for i, dil := range []string{"tr", "en"} {
		m := kvkkSurumRe.FindStringSubmatch(metinler["kvkk"][i])
		if m == nil {
			t.Fatalf("[%s] bot KVKK metni sürüm etiketi taşımıyor", dil)
		}
		if m[1] != KvkkMetinSurumu {
			t.Errorf("[%s] bot KVKK metni sürümü %q, KvkkMetinSurumu %q — metin değişti mi? İkisini birlikte güncelleyin", dil, m[1], KvkkMetinSurumu)
		}
	}
}

// olaylarVar: anahtar için kayıt var mı (test/tanı).
func (k *kayanPencere) olaylarVar(anahtar string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	_, a := k.olaylar[anahtar]
	_, b := k.olaylar["~"+anahtar]
	return a || b
}

// Önce maske, sonra kesme: sınır bir tanımlayıcıyı ikiye bölüp maskeyi kaçıramaz;
// hub'ın bilinen sırları kullanıcı metninde de birebir maskelenir.
func TestKVKKMaskeSonraKesme_VeBilinenSirlar(t *testing.T) {
	o := yeniOrtam(t, nil)
	tc, _, _, _, _, _ := kvGizliler()
	got := o.m.maskeliKullaniciMetni(strings.Repeat("a", 3995) + " " + tc + " son")
	if strings.Contains(got, tc[:4]) {
		t.Fatalf("kesme sınırı T.C. no'yu böldü, ön ek sızdı: ...%q", got[len(got)-12:])
	}
	if n := len([]rune(got)); n > 4001 { // KesRune kesilince "…" ekler
		t.Fatalf("4000 rune (+ellipsis) sınırı aşıldı: %d", n)
	}
	// bilinen sır (portal iç anahtarı) kullanıcı mesajına yapıştırılırsa
	got = o.m.maskeliKullaniciMetni("şunu kullan: " + tPortalIc + " tamam")
	if strings.Contains(got, tPortalIc) {
		t.Fatalf("bilinen sır kullanıcı metninde maskelenmedi: %q", got)
	}
}
