// Package avmotor — GirginOSPanel'in KENDİ zararlı yazılım tespit motoru.
//
// NEDEN KENDİ MOTORUMUZ
// ────────────────────
// ClamAV bu iş için yanlış araç: (a) `clamscan` tek dosya için ~10 sn sürüyor
// (168 MB imzayı her seferinde diskten yüklüyor — ölçüldü), (b) resmî imza seti
// PHP webshell'lerinde zayıf, (c) motoru biz kontrol etmiyoruz.
//
// TASARIM: ÜÇ KATMAN, PUANLI KARAR
// ────────────────────────────────
// Tek desenli imza eşleşmesi barındırmada işe yaramaz — hem kaçırır hem yanlış
// pozitif üretir. Meşru bir eklenti de `base64_decode` kullanır. Bu yüzden
// KANIT TOPLUYORUZ ve eşiğe göre karar veriyoruz:
//
//  1. Kural motoru      — ağırlıklı desenler (obfuscation, tehlikeli çağrı,
//     süper-global → yürütme zinciri)
//  2. Konum sezgileri   — uploads/ altında .php, cache/ içinde yürütülebilir…
//     Bunlar TEK BAŞINA çok güçlü sinyaldir.
//  3. Bütünlük denetimi — WordPress çekirdek dosyası resmî sağlamayla
//     uyuşmuyorsa: arka kapı. Yanlış pozitif ~sıfır ve
//     HİÇBİR imza setinin bilmediği yeni backdoor'u da
//     yakalar. Barındırmada en güçlü tek silah budur.
//
// 🔴 EŞİK MANTIĞI: bir dosya "zararlı" ilan edilmeden önce puanı `EsikKritik`
// değerini aşmalı. Tek kural asla tek başına karantinaya yol açmaz — yanlış
// pozitifin bedeli müşterinin çalışan sitesidir.
package avmotor

import (
	"bufio"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Seviye — bulgunun ciddiyeti.
type Seviye string

const (
	SeviyeBilgi   Seviye = "bilgi"
	SeviyeSupheli Seviye = "supheli"
	SeviyeKritik  Seviye = "kritik"
)

// Eşikler. Puan toplamı bu değerleri aşınca ilgili seviye verilir.
//
// 🔴 Bu sayılar keyfi değil, kural puanlarıyla birlikte ayarlandı: tek bir
// "orta" kural (40) asla tek başına karantinaya yol açmaz; iki bağımsız kanıt
// ya da bir "kesin" kural (100) gerekir.
const (
	EsikSupheli = 50
	EsikKritik  = 100
)

// Kural — tek bir tespit kuralı.
type Kural struct {
	ID    string `json:"id"`    // GOSP-PHP-EVALB64-001
	Ad    string `json:"ad"`    // insan okur açıklama
	Desen string `json:"desen"` // Go regexp
	Puan  int    `json:"puan"`  // kanıt ağırlığı
	// Uzanti: boşsa her dosyada, doluysa yalnız bu uzantılarda çalışır.
	// Performans için önemli: 500 kuralı her dosyada denemek pahalıdır.
	Uzanti []string `json:"uzanti,omitempty"`
	re     *regexp.Regexp
}

// KuralSeti — imza paketinin çözülmüş hâli.
type KuralSeti struct {
	Surum    int     `json:"surum"`
	Uretim   string  `json:"uretim"` // RFC3339, imza paketi üretim zamanı
	Kurallar []Kural `json:"kurallar"`
}

// Bulgu — bir dosyada tespit edilen şey.
type Bulgu struct {
	Dosya    string   `json:"dosya"`
	Puan     int      `json:"puan"`
	Seviye   Seviye   `json:"seviye"`
	Kurallar []string `json:"kurallar"` // eşleşen kural ID'leri
	Aciklama string   `json:"aciklama"`
}

// Motor — derlenmiş kural seti + yapılandırma.
type Motor struct {
	set KuralSeti
	// maxBoyut: bundan büyük dosyalar okunmaz. Bir webshell megabaytlarca
	// olmaz; büyük dosyaları taramak yalnızca I/O yakar. Medya/yedek
	// dosyalarında boşuna dolaşmayı da engeller.
	maxBoyut int64
}

// Yeni — kural setini derler. Bozuk regex'ler ATLANIR ve sayısı döner;
// tek bozuk kural tüm motoru düşürmemeli.
func Yeni(set KuralSeti, maxBoyut int64) (*Motor, int) {
	if maxBoyut <= 0 {
		maxBoyut = 2 << 20 // 2 MiB
	}
	bozuk := 0
	derli := make([]Kural, 0, len(set.Kurallar))
	for _, k := range set.Kurallar {
		re, err := regexp.Compile(k.Desen)
		if err != nil {
			bozuk++
			continue
		}
		k.re = re
		derli = append(derli, k)
	}
	set.Kurallar = derli
	return &Motor{set: set, maxBoyut: maxBoyut}, bozuk
}

func (m *Motor) KuralSayisi() int { return len(m.set.Kurallar) }
func (m *Motor) Surum() int       { return m.set.Surum }

// TaraDosya — tek dosyayı üç katmanla değerlendirir.
//
// 🔴 wpKok boşsa bütünlük katmanı ÇALIŞMAZ (atlanmaz — yoktur). Çağıran,
// dosyanın hangi WordPress kurulumuna ait olduğunu biliyorsa vermeli.
func (m *Motor) TaraDosya(yol string, wpKok string, wpSaglama SaglamaKaynagi) (Bulgu, bool) {
	b := Bulgu{Dosya: yol}

	fi, err := os.Stat(yol)
	if err != nil || fi.IsDir() {
		return b, false
	}

	// ── Katman 2: konum sezgileri (dosyayı OKUMADAN karar verebilir) ──
	// Bunlar önce çalışır çünkü ucuz ve çok güçlüler.
	for _, s := range konumSezgileri(yol) {
		b.Puan += s.puan
		b.Kurallar = append(b.Kurallar, s.id)
	}

	// ── Katman 3: WordPress çekirdek bütünlüğü ──
	if wpKok != "" && wpSaglama != nil {
		if id, puan, ok := wpButunlukKontrol(yol, wpKok, wpSaglama); ok {
			b.Puan += puan
			b.Kurallar = append(b.Kurallar, id)
		}
	}

	// ── Katman 1: kural motoru (dosya içeriği) ──
	// 🔴 C-5: BOYUT GECIDI KALDIRILDI. Onceden `fi.Size() <= maxBoyut` sarti
	// vardi; saldirgan shell'i 2 MiB'in uzerine doldurunca icerik katmani
	// KOMPLE atlaniyordu (adversaryel denetimde kanitlandi). okuSinirli zaten
	// LimitReader ile ilk maxBoyut bayti okur — buyuk dosyada da ucuz. Boyuta
	// bakmadan hep tarariz; cok buyuk dosyada sona eklenen yuk icin kuyruk da okunur.
	{
		icerik, err := okuSinirli(yol, m.maxBoyut)
		if err == nil {
			// Buyuk dosyada son 256 KiB'i da tara (append tipi enjeksiyon).
			if fi.Size() > m.maxBoyut {
				if kuyruk, e := okuKuyruk(yol, 256<<10); e == nil {
					icerik = append(icerik, kuyruk...)
				}
			}
			ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(yol), "."))
			for _, k := range m.set.Kurallar {
				if len(k.Uzanti) > 0 && !icerirStr(k.Uzanti, ext) {
					continue
				}
				if k.re.Match(icerik) {
					b.Puan += k.Puan
					b.Kurallar = append(b.Kurallar, k.ID)
				}
			}
			// Entropi sezgisi — uzun, yüksek entropili tek satır obfuscation
			// göstergesidir. Tek başına yeterli DEĞİL (minified JS de öyledir),
			// bu yüzden düşük puan.
			if e, uzun := enYuksekSatirEntropisi(icerik); uzun && e > 5.2 {
				b.Puan += 25
				b.Kurallar = append(b.Kurallar, "GOSP-HEUR-ENTROPI")
			}
		}
	}

	switch {
	case b.Puan >= EsikKritik:
		b.Seviye = SeviyeKritik
	case b.Puan >= EsikSupheli:
		b.Seviye = SeviyeSupheli
	default:
		return b, false
	}
	b.Aciklama = strings.Join(b.Kurallar, ", ")
	return b, true
}

// ── konum sezgileri ────────────────────────────────────────────────────────

type sezgi struct {
	id   string
	puan int
}

// konumSezgileri — dosyanın NEREDE olduğuna bakar.
//
// 🔴 `uploads/` altında bir .php dosyası neredeyse her zaman ihlal demektir:
// WordPress oraya asla PHP yazmaz, meşru eklentiler de yazmaz. Gerçek dünyada
// görülen webshell'lerin büyük kısmı tam olarak buraya düşer. Bu tek kural,
// hiçbir imza seti gerektirmeden en yaygın saldırıyı yakalar.
func konumSezgileri(yol string) []sezgi {
	y := filepath.ToSlash(yol)
	ext := strings.ToLower(filepath.Ext(y))
	var out []sezgi

	yurutulebilir := ext == ".php" || ext == ".phar" || ext == ".phtml" ||
		ext == ".php5" || ext == ".php7" || ext == ".php8"

	if yurutulebilir {
		switch {
		case strings.Contains(y, "/wp-content/uploads/"):
			out = append(out, sezgi{"GOSP-KONUM-UPLOADS-PHP", 100})
		case strings.Contains(y, "/wp-content/cache/"):
			out = append(out, sezgi{"GOSP-KONUM-CACHE-PHP", 60})
		case strings.Contains(y, "/.well-known/"):
			out = append(out, sezgi{"GOSP-KONUM-WELLKNOWN-PHP", 100})
		}
		// Çift uzantı: shell.php.jpg / shell.jpg.php
		taban := strings.ToLower(filepath.Base(y))
		if n := strings.Count(taban, "."); n >= 2 {
			for _, sahte := range []string{".jpg.", ".jpeg.", ".png.", ".gif.", ".pdf.", ".zip."} {
				if strings.Contains(taban, sahte) {
					out = append(out, sezgi{"GOSP-KONUM-CIFT-UZANTI", 80})
					break
				}
			}
		}
	}

	// Gizli dizinde yürütülebilir dosya (".well-known" hariç, o yukarıda)
	if yurutulebilir && strings.Contains(y, "/.") && !strings.Contains(y, "/.well-known/") {
		out = append(out, sezgi{"GOSP-KONUM-GIZLI-DIZIN", 50})
	}
	return out
}

// ── yardımcılar ────────────────────────────────────────────────────────────

func icerirStr(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// okuKuyruk — dosyanın SON n baytını okur (append tipi enjeksiyon için).
func okuKuyruk(yol string, n int64) ([]byte, error) {
	f, err := os.Open(yol)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	off := fi.Size() - n
	if off < 0 {
		off = 0
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(f)
}

// okuSinirli — en fazla n bayt okur. Büyük dosyada belleği patlatmamak için.
func okuSinirli(yol string, n int64) ([]byte, error) {
	f, err := os.Open(yol)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, n))
}

// enYuksekSatirEntropisi — en uzun satırın Shannon entropisi.
//
// Obfuscated payload'lar tek satırda çok uzun ve yüksek entropili olur.
// `uzun` false ise entropi anlamsızdır (kısa satırda entropi yanıltıcıdır).
func enYuksekSatirEntropisi(b []byte) (float64, bool) {
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	enIyi := 0.0
	uzunVar := false
	for sc.Scan() {
		satir := sc.Bytes()
		if len(satir) < 200 {
			continue
		}
		uzunVar = true
		var sayac [256]int
		for _, c := range satir {
			sayac[c]++
		}
		n := float64(len(satir))
		e := 0.0
		for _, c := range sayac {
			if c == 0 {
				continue
			}
			p := float64(c) / n
			e -= p * math.Log2(p)
		}
		if e > enIyi {
			enIyi = e
		}
	}
	return enIyi, uzunVar
}
