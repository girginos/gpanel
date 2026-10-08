package backups

// silinen_tuketici_kontrat_dogrulama_test.go — DÜŞMANCA DOĞRULAMA (yetim yedek tasarımı, commit e449f18).
//
//	D  'kurtarma' tipinin tüketicileri (pruneOld / pruneManuelYedek, bozulma taraması, kaynak sayacı)
//	I  ön yüz ↔ arka uç JSON kontratı (YedekSilinenler.tsx, DomainBackupsPage.tsx) + rota/fiil eşleşmesi
//	K  0217 göçünün ÖNCEKİ (181'de uygulanmış) sürümü üzerine yeniden koşması
//
// Gerçek BackupRoot'a YAZILMAZ, AĞA ÇIKILMAZ. Geçici MariaDB gerekir.
// Adında "Bulgu" geçen testler ÜRETİM KODUNDAKİ GERÇEK BİR HATAYI kanıtlar: bugün KIRMIZIDIR.

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// ================================================================ D — 'kurtarma' tüketicileri

// pruneOld + pruneManuelYedek, rastgele karışık tip/kimlik dizilimlerinde ('kurtarma' kimlikleri oto/tam'dan büyük ya da küçük olabilir):
// kurtarma ve planli satırlarına (satır + dosya) ASLA dokunmaz; oto → en yeni N, tam → en yeni 10 kalır ve kalanlar KENDİ tiplerinin en yenileridir.
func TestDG_D1_Prune_KurtarmaVePlanliRastgeleKarisimdaAsloSilinmez(t *testing.T) {
	db := kDB(t)
	kok := silinenOrtam(t)
	logYakala(t)
	for seed := int64(1); seed <= 25; seed++ {
		rng := rand.New(rand.NewSource(seed))
		id := 500 + seed
		sk := fmt.Sprintf("c_p%d", seed)
		dgDomain(t, db, id, fmt.Sprintf("p%d.test", seed), sk, 0)
		nOto, nTam, nKur, nPlanli := rng.Intn(9), rng.Intn(26), 1+rng.Intn(8), rng.Intn(4)
		var tipler []string
		for _, g := range []struct {
			tip string
			n   int
		}{{"oto", nOto}, {"tam", nTam}, {"kurtarma", nKur}, {"planli", nPlanli}} {
			for i := 0; i < g.n; i++ {
				tipler = append(tipler, g.tip)
			}
		}
		rng.Shuffle(len(tipler), func(i, j int) { tipler[i], tipler[j] = tipler[j], tipler[i] })
		ret := 1 + rng.Intn(5)
		type satir struct {
			id         int64
			tip, dosya string
		}
		var satirlar []satir
		for i, tip := range tipler {
			d := dgAd(sk, i)
			silinenDosya(t, kok, sk, d)
			r, err := db.Exec(`INSERT INTO backups (domain_id, tip, dosya) VALUES (?,?,?)`, id, tip, d)
			if err != nil {
				t.Fatal(err)
			}
			bid, _ := r.LastInsertId()
			satirlar = append(satirlar, satir{bid, tip, d})
		}
		if err := pruneOld(db, id, sk, ret); err != nil {
			t.Fatal(err)
		}
		pruneManuelYedek(db, id, sk)

		// beklenen: tip başına en yeni (en büyük id) limit kadar kalır
		limit := map[string]int{"oto": ret, "tam": 10, "kurtarma": 1 << 30, "planli": 1 << 30}
		sayac := map[string]int{}
		kalacak := map[int64]bool{}
		sirali := append([]satir(nil), satirlar...)
		sort.Slice(sirali, func(i, j int) bool { return sirali[i].id > sirali[j].id })
		for _, s := range sirali {
			if sayac[s.tip] < limit[s.tip] {
				kalacak[s.id] = true
			}
			sayac[s.tip]++
		}
		for _, s := range satirlar {
			var n int
			_ = db.QueryRow(`SELECT COUNT(*) FROM backups WHERE id=?`, s.id).Scan(&n)
			disk := varMi(filepath.Join(kok, sk, s.dosya))
			if kalacak[s.id] != (n == 1) || kalacak[s.id] != disk {
				t.Errorf("seed=%d ret=%d (oto=%d tam=%d kurtarma=%d planli=%d): %s id=%d kalmalı=%v satır=%d dosya=%v",
					seed, ret, nOto, nTam, nKur, nPlanli, s.tip, s.id, kalacak[s.id], n, disk)
			}
		}
	}
}

// Bozulma taraması (verifyYedekBozulma): yetimler ASLA taranmaz/işaretlenmez; bağlanmış 'kurtarma' satırı, domainin en yeni
// sha256'lı satırıysa taranır (dosya yok + uzak kopya yok → 'bozuk'; doğrulanmış uzak kopya varsa → 'uzakta').
func TestDG_D2_BozulmaTaramasi_YetimleriTaramaz_BagliKurtarmayiTarar(t *testing.T) {
	o := dgYeni(t)
	g113_exec(t, o.db, `ALTER TABLE backups ADD COLUMN sha256 CHAR(64) NOT NULL DEFAULT '', ADD COLUMN dogrulama VARCHAR(16) NOT NULL DEFAULT ''`)
	logYakala(t)
	sha := strings.Repeat("a", 64)
	yetim := silinenYetim(t, o.db, 960, "y.test", "c_vy", dgAd("c_vy", 0), 0, "NULL")
	g113_exec(t, o.db, `UPDATE backups SET sha256=? WHERE id=?`, sha, yetim)
	dgDomain(t, o.db, 961, "v.test", "c_vb", 0)
	g113_exec(t, o.db, `INSERT INTO backups (domain_id, tip, dosya, sha256) VALUES (961,'oto',?,?), (961,'kurtarma',?,?)`, dgAd("c_vb", 0), sha, dgAd("c_vb", 1), sha)
	var kurtarma int64
	_ = o.db.QueryRow(`SELECT id FROM backups WHERE tip='kurtarma'`).Scan(&kurtarma)

	taranan, bozuk := verifyYedekBozulma(o.db)
	if taranan != 1 || bozuk != 1 {
		t.Errorf("yalnız canlı domainin en yeni satırı taranmalı: taranan=%d bozuk=%d", taranan, bozuk)
	}
	var d string
	_ = o.db.QueryRow(`SELECT dogrulama FROM backups WHERE id=?`, yetim).Scan(&d)
	if d != "" {
		t.Errorf("🔴 yetim satır taranıp işaretlendi: dogrulama=%q", d)
	}
	_ = o.db.QueryRow(`SELECT dogrulama FROM backups WHERE id=?`, kurtarma).Scan(&d)
	if d != "bozuk" {
		t.Errorf("bağlanmış kurtarma satırı (dosyası yok, uzak kopyası yok) 'bozuk' işaretlenmeli: %q", d)
	}
	// doğrulanmış uzak kopyası varsa 'bozuk' DEĞİL 'uzakta'
	g113_exec(t, o.db, `UPDATE backups SET dogrulama='' WHERE id=?`, kurtarma)
	g113_exec(t, o.db, `INSERT INTO backup_uzak_kopyalar (backup_id, kaynak, durum) VALUES (?,'hedef','tamam')`, kurtarma)
	taranan, bozuk = verifyYedekBozulma(o.db)
	_ = o.db.QueryRow(`SELECT dogrulama FROM backups WHERE id=?`, kurtarma).Scan(&d)
	if taranan != 1 || bozuk != 0 || d != "uzakta" {
		t.Errorf("uzak kopyası olan kurtarma satırı 'uzakta' olmalı: taranan=%d bozuk=%d dogrulama=%q", taranan, bozuk, d)
	}
}

// Pano yedek sayacı (internal/kaynak/kaynak.go): sorgu metni kaynakta DEĞİŞMEDİ ve davranışı: bağlı 'kurtarma' satırları domainin
// sayacına/boyutuna GİRER, yetimler (domain_id=0) hiçbir domainin sayacına girmez.
func TestDG_D3_KaynakYedekSayaci_KurtarmaSayilir_YetimSayilmaz(t *testing.T) {
	b, err := os.ReadFile("../kaynak/kaynak.go")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("`(SELECT COUNT\\(\\*\\), COALESCE\\(SUM\\(boyut_b\\),0\\) FROM backups WHERE domain_id=\\?)`").FindSubmatch(b)
	if m == nil {
		t.Fatal("kaynak.go yedek sayacı sorgusu değişmiş: bu testi güncelleyin")
	}
	o := dgYeni(t)
	dgDomain(t, o.db, 970, "k.test", "c_k7", 0)
	g113_exec(t, o.db, `INSERT INTO backups (domain_id, tip, dosya, boyut_b) VALUES (970,'oto',?,100), (970,'kurtarma',?,1000)`, dgAd("c_k7", 0), dgAd("c_k7", 1))
	silinenYetim(t, o.db, 971, "y.test", "c_k7", dgAd("c_k7", 2), 0, "NULL")
	g113_exec(t, o.db, `UPDATE backups SET boyut_b=5000 WHERE domain_id=0`)
	var n, toplam int64
	if err := o.db.QueryRow(string(m[1]), 970).Scan(&n, &toplam); err != nil {
		t.Fatal(err)
	}
	if n != 2 || toplam != 1100 {
		t.Errorf("domain 970: 2 satır / 1100 B beklenirdi (oto + bağlı kurtarma; yetim hariç): %d / %d", n, toplam)
	}
	if err := o.db.QueryRow(string(m[1]), 0).Scan(&n, &toplam); err == nil && n > 0 {
		// domain_id=0 hiçbir canlı domainin kimliği olamaz (domains'te 0 id'li satır yok): yetimler hiçbir domainin sayacında değil.
		if g113_say(t, o.db, `SELECT COUNT(*) FROM domains WHERE id=0`) != 0 {
			t.Error("domains'te id=0 satır olmamalı")
		}
	}
}

// ================================================================ I — ön yüz ↔ arka uç kontratı

type dgTSAlan struct {
	ad, tip   string
	opsiyonel bool
}

// dgSusluBlok: kaynak[i:] içindeki ilk '{' ile eşleşen '}' arasındaki metin (iç içe süslüleri sayar, // yorumlarını atar).
func dgSusluBlok(kaynak string, i int) (string, bool) {
	a := strings.IndexByte(kaynak[i:], '{')
	if a < 0 {
		return "", false
	}
	a += i + 1
	derin, j := 1, a
	for ; j < len(kaynak) && derin > 0; j++ {
		switch kaynak[j] {
		case '{':
			derin++
		case '}':
			derin--
		}
	}
	if derin != 0 {
		return "", false
	}
	var sb strings.Builder
	for _, satir := range strings.Split(kaynak[a:j-1], "\n") {
		if k := strings.Index(satir, "//"); k >= 0 {
			satir = satir[:k]
		}
		sb.WriteString(satir + "\n")
	}
	return sb.String(), true
}

// dgTSAlanlari: bir TS nesne gövdesindeki `ad?: tür` alanları.
func dgTSAlanlari(gövde string) []dgTSAlan {
	var out []dgTSAlan
	for _, m := range regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)(\?)?:\s*([^;\n]+)`).FindAllStringSubmatch(gövde, -1) {
		out = append(out, dgTSAlan{ad: m[1], tip: strings.TrimSpace(m[3]), opsiyonel: m[2] == "?"})
	}
	return out
}

// dgTSTip: `type <ad> = { ... }` bloğunun alanları.
func dgTSTip(t *testing.T, kaynak, ad string) []dgTSAlan {
	t.Helper()
	i := strings.Index(kaynak, "type "+ad+" = {")
	if i < 0 {
		t.Fatalf("TS tipi %q bulunamadı", ad)
	}
	g, ok := dgSusluBlok(kaynak, i)
	if !ok {
		t.Fatalf("TS tipi %q gövdesi ayrıştırılamadı", ad)
	}
	out := dgTSAlanlari(g)
	if len(out) == 0 {
		t.Fatalf("TS tipi %q boş ayrıştırıldı", ad)
	}
	return out
}

// dgTSIcTip: `type <ad> = …` içinde isaretten (ör. "data?: {") sonraki süslü bloğun alanları.
func dgTSIcTip(t *testing.T, kaynak, ad, isaret string) []dgTSAlan {
	t.Helper()
	i := strings.Index(kaynak, "type "+ad+" = ")
	if i < 0 {
		t.Fatalf("TS tipi %q bulunamadı", ad)
	}
	k := strings.Index(kaynak[i:], isaret)
	if k < 0 {
		t.Fatalf("TS tipi %q içinde %q yok", ad, isaret)
	}
	g, ok := dgSusluBlok(kaynak, i+k)
	if !ok {
		t.Fatalf("TS tipi %q içindeki %q ayrıştırılamadı", ad, isaret)
	}
	out := dgTSAlanlari(g)
	if len(out) == 0 {
		t.Fatalf("TS tipi %q içindeki %q boş ayrıştırıldı", ad, isaret)
	}
	return out
}

func dgJSONTur(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}

// dgKontrat: TS alanları ↔ JSON nesnesi: zorunlu alan eksik / tür uyuşmazlığı hata; TS'in bilmediği alan hata (kontrat kilidi).
// fazla: TS tipinin bilerek okumadığı ama gönderilmesinde sakınca olmayan anahtarlar (ör. 409 gövdesinde "ok", "hata").
func dgKontrat(t *testing.T, baglam string, ts []dgTSAlan, nesne map[string]any, fazla ...string) {
	t.Helper()
	bilinen := map[string]bool{}
	for _, f := range fazla {
		bilinen[f] = true
	}
	for _, a := range ts {
		bilinen[a.ad] = true
		v, var_ := nesne[a.ad]
		if !var_ {
			if !a.opsiyonel {
				t.Errorf("🔴 %s: TS zorunlu alan %q arka uç yanıtında YOK: %v", baglam, a.ad, nesne)
			}
			continue
		}
		tur := dgJSONTur(v)
		izinli := false
		sabitler := map[string]bool{}
		for _, alt := range strings.Split(a.tip, "|") {
			alt = strings.TrimSpace(alt)
			switch {
			case len(alt) >= 2 && (alt[0] == '\'' || alt[0] == '"') && alt[len(alt)-1] == alt[0]:
				sabitler[alt[1:len(alt)-1]] = true
			case alt == tur:
				izinli = true
			case strings.HasSuffix(alt, "[]") && tur == "array":
				izinli = true
			case alt != "string" && alt != "number" && alt != "boolean" && alt != "null" && alt != "undefined" && !strings.HasSuffix(alt, "[]") && tur == "object":
				izinli = true
			}
		}
		if len(sabitler) > 0 && tur == "string" && sabitler[v.(string)] {
			izinli = true
		}
		if !izinli {
			t.Errorf("🔴 %s: alan %q TS tipi %q, arka uç değeri %s (%v)", baglam, a.ad, a.tip, tur, v)
		}
	}
	for k := range nesne {
		if !bilinen[k] {
			t.Errorf("🔴 %s: arka uç %q alanını gönderiyor ama TS tipinde YOK (kontrat dışı alan)", baglam, k)
		}
	}
}

func dgAnahtarlar(m map[string]any) string {
	var k []string
	for a := range m {
		k = append(k, a)
	}
	sort.Strings(k)
	return strings.Join(k, ",")
}

// dgSahiplerKontrol: JSON sahipler[]/farklilar[] elemanlarını lib/silinenYedek.ts `Sahip` tipiyle alan alan karşılaştırır.
func dgSahiplerKontrol(t *testing.T, baglam string, sahipTS []dgTSAlan, v any) []map[string]any {
	t.Helper()
	dizi, ok := v.([]any)
	if !ok {
		t.Fatalf("🔴 %s: dizi bekleniyordu: %v", baglam, v)
	}
	var out []map[string]any
	for i, e := range dizi {
		m, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("🔴 %s[%d]: nesne bekleniyordu: %v", baglam, i, e)
		}
		dgKontrat(t, fmt.Sprintf("%s[%d]", baglam, i), sahipTS, m)
		if dgAnahtarlar(m) != "alan_adi,dosya_sayisi,reseller_id" {
			t.Errorf("%s[%d] anahtar kümesi değişti: %s", baglam, i, dgAnahtarlar(m))
		}
		out = append(out, m)
	}
	return out
}

// YedekSilinenler.tsx (+ lib/silinenYedek.ts) tipleri ↔ gerçek yanıtlar + JSON şeklinin kilidi.
// Ön yüz commit 28396f9: sahipler[], farklilar[] (Sahip), sayı alanları (reseller_id, dosya_sayisi, hedef/yedek_reseller_id).
func TestDG_I1_FrontendKontrati_YedekSilinenler_AlanAlanKarsilastirma(t *testing.T) {
	src, err := os.ReadFile("../../frontend/src/components/YedekSilinenler.tsx")
	if err != nil {
		t.Fatal(err)
	}
	libSrc, err := os.ReadFile("../../frontend/src/lib/silinenYedek.ts")
	if err != nil {
		t.Fatal(err)
	}
	ts := string(src)
	grupTS, yedekTS := dgTSTip(t, ts, "Grup"), dgTSTip(t, ts, "Yedek")
	sahipTS := dgTSTip(t, string(libSrc), "Sahip")
	hataTS := dgTSIcTip(t, ts, "BaglaHata", "data?: {")
	silHataTS := dgTSIcTip(t, ts, "SilHata", "data?: {")

	o := dgYeni(t)
	yetimUzakSahte(t)
	// grup 1: iki ayrı sahipli yetim + bağlanabilir (farklı) canlı domain + yerel dosya; grup 2: bağlanabilir domain YOK
	o.hesap(980, "kontrat.com", "c_kon", 5, "ftp.k.invalid", 2)
	kSil(t, o.db, 980, "kontrat.com", "c_kon")
	silinenYetim(t, o.db, 981, "baska.com", "c_kon", dgAd("c_kon", 7), 6, "DATE_ADD(NOW(), INTERVAL 3 DAY)")
	dgDomain(t, o.db, 982, "hedef.com", "c_kon", 5)
	silinenYetim(t, o.db, 983, "yalniz.com", "c_yal", dgAd("c_yal", 0), 0, "NULL")
	silinenDosya(t, o.kok, "c_kon", dgAd("c_kon", 0))

	kod, g := advIstek(o.r, "GET", "/s", "")
	if kod != 200 {
		t.Fatalf("%d %s", kod, g)
	}
	var gruplar []map[string]any
	if err := json.Unmarshal([]byte(g), &gruplar); err != nil || len(gruplar) != 2 {
		t.Fatalf("2 grup beklenirdi: %v %s", err, g)
	}
	for _, grup := range gruplar {
		dgKontrat(t, "grup "+fmt.Sprint(grup["sk"]), grupTS, grup)
		sahipler := dgSahiplerKontrol(t, "grup "+fmt.Sprint(grup["sk"])+" sahipler", sahipTS, grup["sahipler"])
		yedekler, _ := grup["yedekler"].([]any)
		if len(yedekler) == 0 {
			t.Errorf("grup %v yedeksiz", grup["sk"])
		}
		for _, y := range yedekler {
			dgKontrat(t, "yedek "+fmt.Sprint(grup["sk"]), yedekTS, y.(map[string]any))
		}
		// kilit: tam anahtar kümesi
		if grup["sk"] == "c_kon" {
			if dgAnahtarlar(grup) != "alan_adi,baglanabilir_alan_adi,baglanabilir_domain_id,baglanabilir_farkli,boyut_b,dosya_sayisi,reseller_id,sahipler,silinme,sk,son_kullanma,yedekler,yerel_var" {
				t.Errorf("grup JSON anahtar kümesi değişti (ön yüzle birlikte gözden geçirin): %s", dgAnahtarlar(grup))
			}
			if grup["baglanabilir_farkli"] != true || grup["baglanabilir_alan_adi"] != "hedef.com" {
				t.Errorf("karma sahipli grup: farkli/alan adı bayrakları: %v", grup)
			}
			// sahipler[]: TÜM ayrık (alan adı, bayi) çiftleri, dosya sayısı çoktan aza
			if len(sahipler) != 2 || sahipler[0]["alan_adi"] != "kontrat.com" || sahipler[0]["reseller_id"] != float64(5) || sahipler[0]["dosya_sayisi"] != float64(2) ||
				sahipler[1]["alan_adi"] != "baska.com" || sahipler[1]["reseller_id"] != float64(6) || sahipler[1]["dosya_sayisi"] != float64(1) {
				t.Errorf("sahipler[] beklenen çiftler/sayılar değil: %v", sahipler)
			}
		} else {
			if _, var_ := grup["baglanabilir_alan_adi"]; var_ || grup["baglanabilir_domain_id"] != nil {
				t.Errorf("bağlanabilir hedefi olmayan grup: domain_id null, alan adı yok olmalı: %v", grup)
			}
			if len(sahipler) != 1 || sahipler[0]["alan_adi"] != "yalniz.com" || sahipler[0]["reseller_id"] != float64(0) || sahipler[0]["dosya_sayisi"] != float64(1) {
				t.Errorf("tek sahipli grupta sahipler[] tek eleman olmalı (yönetici = reseller_id 0): %v", sahipler)
			}
		}
		if dgAnahtarlar(yedekler[0].(map[string]any)) != "boyut_b,dosya,id,olusturma,yerel" {
			t.Errorf("yedek JSON anahtar kümesi değişti: %s", dgAnahtarlar(yedekler[0].(map[string]any)))
		}
	}

	// 409 bağla gövdesi (BaglaHata.data): ön yüzün okuduğu TÜM alanlar (sayılar dahil) gerçek yanıtla alan alan eşleşir
	kod, g = advIstek(o.r, "POST", "/s/c_kon/bagla", `{"domain_id":982}`)
	if kod != 409 {
		t.Fatalf("farklı sahip → 409: %d %s", kod, g)
	}
	var y409 map[string]any
	_ = json.Unmarshal([]byte(g), &y409)
	dgKontrat(t, "409 bağla", hataTS, y409, "ok", "hata")
	for _, zorunlu := range []string{"alan_adi_farkli", "farklilar", "hedef_alan_adi", "hedef_reseller_id", "yedek_alan_adi", "yedek_reseller_id"} {
		if _, var_ := y409[zorunlu]; !var_ {
			t.Errorf("🔴 409 gövdesinde ön yüzün okuduğu %q yok: %s", zorunlu, g)
		}
	}
	farklilar := dgSahiplerKontrol(t, "409 farklilar", sahipTS, y409["farklilar"])
	if len(farklilar) != 2 || farklilar[0]["alan_adi"] != "kontrat.com" || y409["yedek_alan_adi"] != "kontrat.com" || y409["yedek_reseller_id"] != float64(5) ||
		y409["hedef_alan_adi"] != "hedef.com" || y409["hedef_reseller_id"] != float64(5) || y409["alan_adi_farkli"] != true || y409["ok"] != false {
		t.Errorf("409 gövde değerleri: farklilar=%v yedek=%v/%v hedef=%v/%v", farklilar, y409["yedek_alan_adi"], y409["yedek_reseller_id"], y409["hedef_alan_adi"], y409["hedef_reseller_id"])
	}
	if dgAnahtarlar(y409) != "alan_adi_farkli,farklilar,hata,hedef_alan_adi,hedef_reseller_id,ok,yedek_alan_adi,yedek_reseller_id" {
		t.Errorf("409 bağla anahtar kümesi değişti: %s", dgAnahtarlar(y409))
	}

	// bağla 200, sil 409 / 200, ayar
	kod, g = advIstek(o.r, "POST", "/s/c_kon/bagla", `{"domain_id":982,"zorla":true}`)
	var ok200 map[string]any
	_ = json.Unmarshal([]byte(g), &ok200)
	if kod != 200 || dgAnahtarlar(ok200) != "baglanan,ok" || dgJSONTur(ok200["baglanan"]) != "number" || ok200["ok"] != true {
		t.Errorf("bağla 200 gövdesi {ok,baglanan:number} olmalı: %d %s", kod, g)
	}
	sahte := yetimUzakSahte(t)
	sahte.hata = func(*Destination, string) error { return fmt.Errorf("uzak kapalı") }
	g113_exec(t, o.db, `UPDATE backups SET son_kullanma=NULL WHERE domain_id=0`)
	kod, g = advIstek(o.r, "DELETE", "/s/c_yal", "")
	var sil409 map[string]any
	_ = json.Unmarshal([]byte(g), &sil409)
	if kod != 409 || dgAnahtarlar(sil409) != "hata,ok,silinen,uzagi_yoksay" || sil409["uzagi_yoksay"] != true || dgJSONTur(sil409["hata"]) != "string" {
		t.Errorf("sil 409 gövdesi {ok:false,hata,silinen,uzagi_yoksay:true} olmalı (ön yüz uzagi_yoksay ve hata'yı okur): %d %s", kod, g)
	}
	dgKontrat(t, "sil 409", silHataTS, sil409, "ok", "hata", "silinen")
	sahte.hata = nil
	kod, g = advIstek(o.r, "DELETE", "/s/c_yal", "")
	var sil200 map[string]any
	_ = json.Unmarshal([]byte(g), &sil200)
	if kod != 200 || dgAnahtarlar(sil200) != "ok,silinen" {
		t.Errorf("sil 200 gövdesi {ok,silinen}: %d %s", kod, g)
	}
	ar := advRouter(o.db, &Handlers{DB: o.db})
	_, g = advIstek(ar, "GET", "/s/ayar", "")
	var ayar map[string]any
	_ = json.Unmarshal([]byte(g), &ayar)
	if dgAnahtarlar(ayar) != "en_az_slot_gun,gun" || dgJSONTur(ayar["en_az_slot_gun"]) != "number" {
		t.Errorf("ayar GET gövdesi {gun:number|null, en_az_slot_gun}: %s", g)
	}
	// ön yüz gun için null / sayı gönderir; sunucu ikisini de kabul eder (ayrıntı: TestSilinenAyar_SinirlarVeKalicilik)
	if kod, _ := advIstek(ar, "PUT", "/s/ayar", `{"gun":null}`); kod != 200 {
		t.Errorf("ayar PUT null: %d", kod)
	}
	if kod, _ := advIstek(ar, "PUT", "/s/ayar", `{"gun":9999}`); kod != 400 {
		t.Errorf("ön yüzün regex'i 4 haneye izin verir (9999): sunucu 400 ile reddetmeli: %d", kod)
	}
}

// I4 — BackupYonetimiPage.tsx `Ozet` tipi ↔ GET /admin/backups/ozet: silinen_sayi + silinen_boyut_b (O8) ve domainler[] satırları.
func TestDG_I4_FrontendKontrati_BackupOzeti_SilinenKPI(t *testing.T) {
	src, err := os.ReadFile("../../frontend/src/pages/BackupYonetimiPage.tsx")
	if err != nil {
		t.Fatal(err)
	}
	ozetTS, satirTS := dgTSTip(t, string(src), "Ozet"), dgTSTip(t, string(src), "OzetSatir")
	o := dgYeni(t)
	h := &Handlers{DB: o.db}
	dgDomain(t, o.db, 1700, "canli.test", "c_ozet", 0)
	silinenYetim(t, o.db, 1701, "silinmis.test", "c_silo", dgAd("c_silo", 0), 0, "NULL")
	silinenYetim(t, o.db, 1701, "silinmis.test", "c_silo", dgAd("c_silo", 1), 0, "NULL")
	g113_exec(t, o.db, `UPDATE backups SET boyut_b=1500 WHERE domain_id=0`)
	g113_exec(t, o.db, `INSERT INTO backups (domain_id, dosya, boyut_b) VALUES (1700, ?, 700)`, dgAd("c_ozet", 0)) // canlı: silinen sayısına girmez
	rt := chi.NewRouter()
	rt.Get("/ozet", h.Ozet)
	kod, g := advIstek(rt, "GET", "/ozet", "")
	if kod != 200 {
		t.Fatalf("%d %s", kod, g)
	}
	var y map[string]any
	if err := json.Unmarshal([]byte(g), &y); err != nil {
		t.Fatal(err)
	}
	dgKontrat(t, "özet", ozetTS, y)
	if dgAnahtarlar(y) != "domainler,hedef_sayisi,silinen_boyut_b,silinen_sayi,toplam_boyut_b,toplam_yedek,zamanlama" {
		t.Errorf("özet anahtar kümesi değişti: %s", dgAnahtarlar(y))
	}
	if y["silinen_sayi"] != float64(2) || y["silinen_boyut_b"] != float64(3000) {
		t.Errorf("silinen_sayi=2 / silinen_boyut_b=3000 (yalnız domain_id=0 AND silinme IS NOT NULL) beklenirdi: %v / %v", y["silinen_sayi"], y["silinen_boyut_b"])
	}
	satirlar, _ := y["domainler"].([]any)
	if len(satirlar) != 1 {
		t.Fatalf("1 canlı domain satırı beklenirdi: %v", y["domainler"])
	}
	dgKontrat(t, "özet satırı", satirTS, satirlar[0].(map[string]any))
}

type dgIstek struct{ fiil, yol string }

// Ön yüzün çağırdığı (fiil, yol) çiftleri ↔ main.go'daki silinen-yedekler rotaları: birebir eşleşmeli.
func TestDG_I2_FrontendRotalari_MainGoRotalariylaBirebir(t *testing.T) {
	src, err := os.ReadFile("../../frontend/src/components/YedekSilinenler.tsx")
	if err != nil {
		t.Fatal(err)
	}
	norm := func(y string) string {
		y = regexp.MustCompile(`\$\{[^}]*\}`).ReplaceAllString(y, "{x}")
		y = regexp.MustCompile(`\{[a-z]+\}`).ReplaceAllString(y, "{x}")
		y = strings.ReplaceAll(y, "{x}{x}", "{x}") // ikinci ${...} sorgu dizesi ekidir (?uzagi_yoksay=1)
		return strings.TrimPrefix(y, "/api/v1")
	}
	fe := map[dgIstek]bool{}
	apiRe := regexp.MustCompile("api\\.(get|post|put|delete)(?:<[^()]*>)?\\(\\s*(?:'([^']*)'|\"([^\"]*)\"|`([^`]*)`)")
	for _, m := range apiRe.FindAllStringSubmatch(string(src), -1) {
		yol := m[2] + m[3] + m[4]
		if !strings.Contains(yol, "silinen-yedekler") {
			continue
		}
		fe[dgIstek{strings.ToUpper(m[1]), norm(yol)}] = true
	}
	for _, m := range regexp.MustCompile("fetch\\(\\s*`([^`]*)`").FindAllStringSubmatch(string(src), -1) {
		if strings.Contains(m[1], "silinen-yedekler") {
			fe[dgIstek{"GET", norm(m[1])}] = true
		}
	}
	mb, err := os.ReadFile("../../cmd/server/main.go")
	if err != nil {
		t.Fatal(err)
	}
	go_ := map[dgIstek]bool{}
	rotaRe := regexp.MustCompile(`\.(Get|Post|Put|Delete)\("(/admin/silinen-yedekler[^"]*)"`)
	for _, m := range rotaRe.FindAllStringSubmatch(string(mb), -1) {
		go_[dgIstek{strings.ToUpper(m[1]), norm(m[2])}] = true
	}
	if len(fe) != 6 || len(go_) != 6 {
		t.Errorf("6 rota beklenirdi: ön yüz %d, arka uç %d\nön yüz: %v\narka uç: %v", len(fe), len(go_), fe, go_)
	}
	for k := range fe {
		if !go_[k] {
			t.Errorf("🔴 ön yüz %s %s çağırıyor, arka uçta bu rota YOK", k.fiil, k.yol)
		}
	}
	for k := range go_ {
		if !fe[k] {
			t.Errorf("arka uç rotası %s %s ön yüzde kullanılmıyor", k.fiil, k.yol)
		}
	}
}

// DomainBackupsPage.tsx `Yedek` tipi ↔ GET /domains/{id}/backups (kaynak: 'kurtarma' dahil).
func TestDG_I3_FrontendKontrati_DomainYedekListesi_KurtarmaKaynagi(t *testing.T) {
	src, err := os.ReadFile("../../frontend/src/pages/DomainBackupsPage.tsx")
	if err != nil {
		t.Fatal(err)
	}
	ts := string(src)
	yedekTS := dgTSTip(t, ts, "Yedek")
	kaynak := ""
	for _, a := range yedekTS {
		if a.ad == "kaynak" {
			kaynak = a.tip
		}
	}
	if !strings.Contains(kaynak, "'kurtarma'") || !strings.Contains(kaynak, "'sistem'") || !strings.Contains(kaynak, "'manuel'") {
		t.Errorf("TS kaynak birleşimi sistem|manuel|kurtarma olmalı: %q", kaynak)
	}
	o := dgYeni(t)
	yetimUzakSahte(t)
	o.hesap(990, "dom.com", "c_dom", 0, "", 1)
	kSil(t, o.db, 990, "dom.com", "c_dom")
	dgDomain(t, o.db, 991, "dom.com", "c_dom", 0)
	dgBagla(t, o.r, "c_dom", 991, false)
	g113_exec(t, o.db, `INSERT INTO backups (domain_id, tip, dosya, job_id) VALUES (991,'oto',?,NULL),(991,'tam',?,NULL),(991,'tam',?,5)`, dgAd("c_dom", 5), dgAd("c_dom", 6), dgAd("c_dom", 7))
	kod, g := advIstek(o.r, "GET", "/d/991/backups", "")
	if kod != 200 {
		t.Fatalf("%d %s", kod, g)
	}
	var liste []map[string]any
	if err := json.Unmarshal([]byte(g), &liste); err != nil || len(liste) != 4 {
		t.Fatalf("4 satır beklenirdi (1 bağlanan + 3): %v %s", err, g)
	}
	say := map[string]int{}
	for _, y := range liste {
		dgKontrat(t, "yedek listesi", yedekTS, y)
		say[fmt.Sprint(y["kaynak"])]++
		if y["tip"] == "kurtarma" && y["kaynak"] != "kurtarma" {
			t.Errorf("tip=kurtarma → kaynak=kurtarma olmalı: %v", y)
		}
	}
	if say["kurtarma"] != 1 || say["sistem"] != 2 || say["manuel"] != 1 {
		t.Errorf("kaynak dağılımı: %v", say)
	}
}

// ================================================================ K — göç

// 181'de (ve ilk sürümü uygulayan her sunucuda) 0217'nin ÖNCEKİ sürümü (5b27194, goc0217V1) uygulanmış durumda; runMigrations değişmiş
// dosyayı YENİDEN KOŞMAZ ("GOC DEGISMIS ... YENIDEN KOSULMADI"): yeni dosya, defter (schema_migrations) sıfırlanınca koşar. Bu test, önceki
// sürümün şeması üzerine yeni dosyanın hatasız, idempotent uygulandığını ve eski biçimli yetimleri dönüştürdüğünü kanıtlar
// (V1/V2 ayrıntılı testleri: silinen_test.go TestGoc0217_V1_*, V2_*).
func TestDG_K1_Goc0217_OncekiSurumUzerineYenidenKosunca_TamamlanirVeDonusturur(t *testing.T) {
	db := silinenDBHam(t)
	gocMetniUygula(t, db, goc0217V1)
	if g113_say(t, db, `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='backups' AND COLUMN_NAME='eski_domain_id'`) != 0 {
		t.Fatal("kurulum: önceki sürümde eski_domain_id kolonu olmamalı")
	}
	// önceki sürümün ürettiği biçim: yetim = domain_id>0 + silinme dolu + domains'te karşılığı yok (FK'siz)
	g113_exec(t, db, `INSERT INTO domains (id, alan_adi, sistem_kullanici, reseller_id) VALUES (9501,'canli.test','c_canli',5)`)
	g113_exec(t, db, `INSERT INTO backups (domain_id, dosya, alan_adi, sistem_kullanici, reseller_id, silinme, son_kullanma) VALUES
		(9500,'c_eski-20261001-000001.tar.gz','eski.test','c_eski',5,NOW(),DATE_ADD(NOW(), INTERVAL 7 DAY)),
		(9500,'c_eski-20261002-000001.tar.gz','eski.test','c_eski',5,NOW(),NULL),
		(9501,'c_canli-20261001-000001.tar.gz','canli.test','c_canli',5,NULL,NULL)`)

	for i := 0; i < 3; i++ { // yeni sürüm: üst üste koşar, hata vermez
		silinenGocUygula(t, db)
	}
	for _, c := range []string{"eski_domain_id", "alan_adi", "sistem_kullanici", "reseller_id", "silinme", "son_kullanma", "iz_id"} {
		if g113_say(t, db, `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='backups' AND COLUMN_NAME=?`, c) != 1 {
			t.Errorf("kolon %s yok", c)
		}
	}
	sekilDogrula(t, db)
	if g113_say(t, db, `SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='backup_yetim_hedefler'`) != 1 {
		t.Error("backup_yetim_hedefler yok")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=0 AND eski_domain_id=9500 AND silinme IS NOT NULL`) != 2 {
		t.Error("önceki sürümün yetimleri (domain_id=9500, domains'te yok) yeni biçime (domain_id=0, eski_domain_id=9500) çevrilmeli")
	}
	if g113_say(t, db, `SELECT COUNT(*) FROM backups WHERE domain_id=9501 AND eski_domain_id=0 AND silinme IS NULL`) != 1 {
		t.Error("canlı satıra dokunulmamalı")
	}
	// ve dönüştürülen satırlar yeni kodla normal yaşam döngüsüne girer
	h := &Handlers{DB: db}
	r := advRouter(db, h)
	if _, g := advIstek(r, "GET", "/s", ""); !strings.Contains(g, `"sk":"c_eski"`) {
		t.Errorf("dönüştürülen yetimler yönetici listesinde görünmeli: %s", g)
	}
}
