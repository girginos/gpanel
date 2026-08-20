package avmotor

// TİCARİ PHP KODLAYICILARI (ionCube / SourceGuardian / Zend Guard) —
// ŞİFRELİ GÖVDEDE İMZA ARAMAK YALNIZCA YANLIŞ POZİTİF ÜRETİR.
//
// 🔴 GERÇEK OLAY (2026-08-20, versilo.net): WHMCS ionCube ile kodlanmıştır.
// 115 KB'lik şifreli blob içinde "c99" dizisi RASTGELE 2 kez geçtiği için
// GOSP-SHELL-C99-R57 (100) + GOSP-HEUR-ENTROPI-BLOB (25) = 125 puan → kritik →
// oto-karantina. Panel müşterinin WHMCS kurulumundan **347 dosyayı** yedi;
// init.php gidince site tamamen çöktü. Hiçbiri zararlı değildi.
//
// 🔴 NEDEN KAÇINILMAZ: şifreli/sıkıştırılmış veri ~rastgele baytlardır. 3-4
// karakterlik bir imza dizisi, 100 KB rastgele baytta neredeyse KESİN olarak
// geçer. Yani şifreli gövdede imza eşleşmesi SİNYAL DEĞİL, gürültüdür. Üstelik
// gövdeyi çözemeyiz (çözmek için kodlayıcının lisanslı loader'ı gerekir), yani
// orada arama yapmanın tespit değeri de YOKTUR. Ölçemediğin şeyi "kirli"
// saymak, "temiz" saymak kadar yanlıştır.
//
// ÇÖZÜM: kodlayıcı damgası varsa DÜZ METİN kısmı tam güçle taranır, şifreli
// gövde OPAK kabul edilir. Kaçış koruması aşağıda (govdedeDuzMetinKod).

import "bytes"

// kodlayiciDamgalari — dosya BAŞINDA aranan ticari kodlayıcı imzaları.
// Yalnız ilk kodlayiciBasBakis bayta bakılır: gerçek kodlayıcı damgası dosyanın
// EN BAŞINDADIR; rastgele yerde geçen "ioncube" kelimesi (ör. bir blog yazısı)
// taramayı zayıflatmamalı.
var kodlayiciDamgalari = []struct {
	damga []byte
	adi   string
}{
	{[]byte("//ICB0"), "ionCube"},         // ionCube başlık yorumu: `<?php //ICB0 82:0 83:e7bc`
	{[]byte("ionCube Loader"), "ionCube"}, // loader-yok geri düşüş metni (her kodlanmış dosyada)
	{[]byte("SourceGuardian"), "SourceGuardian"},
	{[]byte("sg_load"), "SourceGuardian"},
	{[]byte("@Zend;"), "ZendGuard"},
}

const (
	kodlayiciBasBakis = 1024 // damga yalnız bu ilk baytlarda aranır
	blobIkiliEsik     = 48   // bu kadar ardışık "ikili" bayt → şifreli gövde başladı
	blobPencere       = 256  // base64-yoğunluk ölçüm penceresi
	blobB64Oran       = 96   // pencerede yüzde kaç base64 karakteri → kodlanmış gövde
	blobKodIpucu      = 5    // enjekte kod penceresinde aranan PHP sözdizim karakteri sayısı
	blobKodPencere    = 160  // PHP açılışından sonra incelenen bayt
	blobNonB64Esik    = 2    // base64 gövdede bu kadar ardışık base64-olmayan bayt → düz kod başladı
	// (saf base64 gövde HİÇ non-b64 içermez; 2 ardışık non-b64 = düz kod. `';eval`
	//  dizisindeki `';` bunu tetikler — `eval` harfleri base64 alfabesinde olsa da.)
)

// ikiliBayt — bayt "metin dışı" mı. Yazdırılabilir ASCII ve olağan boşluklar
// metin sayılır; kalanı ikili.
func ikiliBayt(c byte) bool {
	if c == '\t' || c == '\n' || c == '\r' {
		return false
	}
	return c < 32 || c > 126
}

// b64Karakter — base64 alfabesi (+ satır sarma boşlukları hariç tutulur).
func b64Karakter(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
		c == '+' || c == '/' || c == '='
}

// phpSozdizimKarakteri — GERÇEK PHP kodunda bol, base64 alfabesinde YOK.
// Kodlanmış gövdede tesadüfen oluşan `<?php` dizisini gerçek enjekte koddan
// ayırmanın anahtarı budur (base64 bu karakterlerin HİÇBİRİNİ üretmez).
func phpSozdizimKarakteri(c byte) bool {
	return bytes.IndexByte(phpSozdizimKume, c) >= 0
}

// phpSozdizimKume — base64 alfabesinde BULUNMAYAN, gercek PHP kodunda bol
// gecen karakterler (bosluk $ ( ) ; tirnak cift-tirnak NL TAB [ ] { } < > . , ! -).
// Kodlanmis govdede tesadufen olusan PHP acilisini gercek enjekte koddan
// ayirmanin anahtari budur: base64 bunlarin HICBIRINI uretemez.
var phpSozdizimKume = []byte{0x20, 0x24, 0x28, 0x29, 0x3b, 0x27, 0x22, 0x0a, 0x09, 0x5b, 0x5d, 0x7b, 0x7d, 0x3c, 0x3e, 0x2e, 0x2c, 0x21, 0x2d}

// kodlayiciBlobBaslangici — dosya ticari kodlayıcıyla paketlenmişse şifreli
// gövdenin başladığı ofseti döner. (adi, ofset, true) / ("", 0, false).
//
// Gövde başlangıcı = damgadan sonraki ilk `blobIkiliEsik` uzunluğunda ardışık
// ikili bayt dizisi. Böylece düz metin önsöz (telif başlığı, loader-yok mesajı)
// tarama KAPSAMINDA kalır; yalnız şifreli kısım dışlanır.
// Dönüş: (kodlayıcı adı, blob başlangıç ofseti, bulundu, base64mi). base64mi=false
// ise blob ham-ikili gövdedir (tümü opak; tail taranmaz — FP kaynağı). base64mi=true
// ise blob base64 dizisidir ve dizinin BİTİMİNDEN sonrası (sink tail) taranabilir.
func kodlayiciBlobBaslangici(icerik []byte) (string, int, bool, bool) {
	bas := icerik
	if len(bas) > kodlayiciBasBakis {
		bas = bas[:kodlayiciBasBakis]
	}
	adi := ""
	for _, d := range kodlayiciDamgalari {
		if bytes.Contains(bas, d.damga) {
			adi = d.adi
			break
		}
	}
	if adi == "" {
		return "", 0, false, false
	}
	// (a) İKİLİ gövde (bazı paketleyiciler ham ikili yazar)
	ardisik := 0
	for i := 0; i < len(icerik); i++ {
		if ikiliBayt(icerik[i]) {
			ardisik++
			if ardisik >= blobIkiliEsik {
				return adi, i - ardisik + 1, true, false
			}
			continue
		}
		ardisik = 0
	}
	// (b) BASE64 gövde — ionCube'un asıl biçimi: gövde %100 yazdırılabilirdir
	// (76 karakterlik base64 satırları), yani "ikili bayt" sezgisi GÖRMEZ.
	// Ayırt edici: base64 alfabesi yoğunluğu. Gerçek PHP kaynağı bu oranı
	// sürdüremez (boşluk, $, (, ; ... base64'te yoktur).
	for i := 0; i+blobPencere <= len(icerik); i += blobPencere {
		p := icerik[i : i+blobPencere]
		say := 0
		for _, c := range p {
			if b64Karakter(c) {
				say++
			}
		}
		if say*100/len(p) >= blobB64Oran {
			// Pencere içinde geriye giderek gerçek başlangıcı bul.
			bas := i
			for bas > 0 && b64Karakter(icerik[bas-1]) {
				bas--
			}
			return adi, bas, true, true
		}
	}
	// Damga var ama kodlanmış gövde YOK → kodlanmış değil (ör. ionCube'dan
	// BAHSEDEN düz PHP dosyası, hatta damgayı taklit eden webshell). Tam tara.
	return "", 0, false, false
}

// govdedeDuzMetinKod — KAÇIŞ KORUMASI. Şifreli gövdeye eklenmiş GERÇEK PHP kodu
// var mı? Saldırgan sahte bir kodlayıcı başlığı + kısa ikili dolgu koyup
// arkasına webshell yazarak taramayı atlatmayı deneyebilir.
//
// Ayrım şu: rastgele şifreli baytlarda `<?php` dizisi TESADÜFEN geçebilir, ama
// ardından `blobKodDuzMetin` kadar KESİNTİSİZ düz metin gelmesi olasılık dışıdır
// (şifreli veride yazdırılabilir oran ~%37, 120 bayt için ~10^-52). Gerçek
// enjekte kod ise doğası gereği uzun düz metindir.
//
// Bulunursa o noktadan itibaren içerik döner (tam güçle taranır), yoksa nil.
func govdedeDuzMetinKod(govde []byte) []byte {
	for _, ac := range [][]byte{[]byte("<?php"), []byte("<?=")} {
		ofs := 0
		for {
			i := bytes.Index(govde[ofs:], ac)
			if i < 0 {
				break
			}
			p := ofs + i
			son := p + len(ac) + blobKodPencere
			if son > len(govde) {
				son = len(govde)
			}
			// 🔴 base64 gövdede "yazdırılabilirlik" AYIRT EDİCİ DEĞİLDİR
			// (gövdenin tamamı yazdırılabilir). Gerçek kodu ayıran şey PHP
			// SÖZDİZİMİ karakterleridir: base64 alfabesi boşluk/$/(/;/tırnak
			// ÜRETEMEZ. Tesadüfi `<?php` dizisinin ardından bunlar gelmez.
			ipucu := 0
			for _, c := range govde[p+len(ac) : son] {
				if phpSozdizimKarakteri(c) {
					ipucu++
				}
			}
			if ipucu >= blobKodIpucu {
				return govde[p:]
			}
			ofs = p + len(ac)
		}
	}
	return nil
}

// blobSonuB64 — base64 şifreli dizinin BİTTİĞİ ofset. blobBas'tan ileri base64
// karakterleri (+ satır sarma / boşluk) tüketir; blobNonB64Esik kadar ARDIŞIK
// base64-olmayan bayt görülünce (ör. `';eval(...` başlangıcı) düz kod bölgesi
// başladı sayar. YALNIZ base64 gövdede çağrılır (ham-ikili gövdede blobSon=len,
// tail taranmaz — rastgele ikili tail imza-çakışması FP'si üretirdi).
//
// 🔴 GÜVENLİK: eski kodlayiciAyikla yalnız önsözü ve blobBas SONRASINDA YENİ
// `<?php` etiketiyle açılan kodu tarardı. `$c='<base64>'; eval(base64_decode($c));`
// içinde eval AYNI context'te (yeni açılış yok) blob'dan sonra kalıyor → HİÇ
// taranmıyordu (kanıtlı puan=0). base64 dizisinin bitiminden sonraki tail'i
// taramak bu tam-context sink'i geri yakalar.
func blobSonuB64(icerik []byte, blobBas int) int {
	n := len(icerik)
	nonB64 := 0
	for i := blobBas; i < n; i++ {
		c := icerik[i]
		if b64Karakter(c) || c == '\n' || c == '\r' || c == ' ' || c == '\t' {
			nonB64 = 0
			continue
		}
		nonB64++
		if nonB64 >= blobNonB64Esik {
			return i - nonB64 + 1 // düz kod bu noktada başlıyor
		}
	}
	return n
}

// kodlayiciAyikla — içerik ticari kodlayıcıyla paketlenmişse taranacak kısmı
// döner: düz metin önsöz + (base64 gövde ise) blob-SONRASI tail + (varsa) blob
// İÇİNE yeni-<?php ile enjekte kod. Yalnız opak gövde (FP kaynağı) çıkarılır.
// Paketli değilse (nil, "", false) döner ve çağıran normal yolu izler.
func kodlayiciAyikla(icerik []byte) ([]byte, string, bool) {
	adi, blobBas, ok, b64 := kodlayiciBlobBaslangici(icerik)
	if !ok {
		return nil, "", false
	}
	blobSon := len(icerik) // ham-ikili gövde: tümü opak, tail yok
	if b64 {
		blobSon = blobSonuB64(icerik, blobBas)
	}
	tara := make([]byte, 0, len(icerik))
	tara = append(tara, icerik[:blobBas]...) // önsöz (telif başlığı, açılış)
	// blob-SONRASI tail (yalnız base64 gövdede): aynı-context eval/system sink'leri burada.
	if blobSon < len(icerik) {
		tara = append(tara, '\n')
		tara = append(tara, icerik[blobSon:]...)
	}
	// blob İÇİNE yeni `<?php` ile gömülmüş kod (nadir ama mümkün): gerçek ionCube
	// dosyasında yoktur (govdedeDuzMetinKod false döner → FP eklenmez).
	if enjekte := govdedeDuzMetinKod(icerik[blobBas:blobSon]); enjekte != nil {
		tara = append(tara, '\n')
		tara = append(tara, enjekte...)
	}
	return tara, adi, true
}
