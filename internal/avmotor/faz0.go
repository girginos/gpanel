package avmotor

// FAZ 0 — obfuscation'a karşı sertleşme: bölgesel (kayan pencere) entropi +
// SINIRLI deobfuscation-yeniden-tarama. İkisi de mevcut kural motorunun üstüne
// biner; yeni altyapı gerektirmez.
//
// 🔴 GÜVENLİK MODELİ: girdi DÜŞMANDIR (müşteri public_html yüklemesi). Decode
// işi KATI bir bütçeyle sınırlıdır — üç ayrı fren:
//   1. cozMaxBayt      : toplam SAKLANAN çözülmüş bayt tavanı
//   2. cozMaxDecompres : toplam decompress SAYISI (nil dönse bile harcanır) —
//      "yazdırılamaz bombaya açılıp bütçe harcamadan kaçma" DoS'unu keser
//   3. cozBombaOran    : sıkıştırma-oranı bombası (küçük girdi → devasa çıktı)
// canlı slice cgroup limitleri UYGULANMAYABİLİR (ölçüldü: infinity), o yüzden
// in-process bütçenin doğruluğu tek gerçek savunmadır.

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"encoding/hex"
	"hash/fnv"
	"io"
	"math"
	"regexp"
)

// ── Bölgesel entropi ────────────────────────────────────────────────────────

// shannon — bir bayt diliminin Shannon entropisi (0..8).
func shannon(b []byte) float64 {
	if len(b) == 0 {
		return 0
	}
	var c [256]int
	for _, x := range b {
		c[x]++
	}
	n := float64(len(b))
	e := 0.0
	for _, k := range c {
		if k == 0 {
			continue
		}
		p := float64(k) / n
		e -= p * math.Log2(p)
	}
	return e
}

// bolgeselEntropi — dosyayı kayan pencerelerle tarar. Tek dosya-eşiği yerine
// "normal kod içine GÖMÜLÜ yüksek-entropili blok" arar. Ayrım kritiktir:
// minified JS / base64 varlık dosya BOYUNCA yüksek entropilidir (yanlış poz);
// gizlenmiş payload ise LOKAL yüksek, çevresi normal koddur (gerçek sinyal).
//
// 🔴 Puanlar bilinçli DÜŞÜK: entropi TEK BAŞINA asla eşik aşmamalı — yalnız
// başka kanıtı doğrular. (BLOB 25: COK-UZUN-B64 kuralı +20 ile toplansa bile
// 45 < EsikSupheli(50) → base64 varlık gömen meşru PHP şüpheli işaretlenmez.)
func bolgeselEntropi(b []byte) (int, string, bool) {
	const pencere = 1024
	const adim = 512
	if len(b) < pencere {
		if len(b) >= 256 && shannon(b) > 5.5 {
			return 15, "GOSP-HEUR-ENTROPI", true
		}
		return 0, "", false
	}
	maxE, minE := 0.0, 8.0
	yuksek := 0
	for off := 0; off+pencere <= len(b); off += adim {
		e := shannon(b[off : off+pencere])
		if e > maxE {
			maxE = e
		}
		if e < minE {
			minE = e
		}
		if e > 5.5 {
			yuksek++
		}
	}
	// 5.5 eşiği base64/sıkıştırılmış bloğu (≈5.7-6.0) normal koddan (≈4.3-5.2)
	// ayırır. Ayırt edici: dosyada belirgin NORMAL-KOD bölgesi (düşük min) de
	// var mı? Varsa → gömülü blob (gerçek sinyal). Yoksa (dosya boyunca yüksek)
	// → minified/base64 varlık olabilir (zayıf).
	if maxE <= 5.5 || yuksek == 0 {
		return 0, "", false
	}
	if minE < 4.7 {
		return 25, "GOSP-HEUR-ENTROPI-BLOB", true
	}
	return 15, "GOSP-HEUR-ENTROPI", true
}

// ── Sınırlı deobfuscation ───────────────────────────────────────────────────

const (
	cozDerinlik     = 4       // en fazla iç içe decode katmanı
	cozMaxBayt      = 3 << 20 // toplam SAKLANAN çözülmüş bayt tavanı (3 MiB)
	cozMaxBlob      = 12      // katman başına en fazla blob
	cozMaxDecompres = 16      // tüm ağaç için toplam decompress SAYISI (bomba backstop)
	cozBombaOran    = 200     // açılan/girdi oranı bu üstündeyse sıkıştırma bombası
	cozTavanPu      = 60      // decode katmanının en fazla puanı — EsikKritik(100) ALTINDA:
	//                          decode tek başına asla KRİTİK (karantina) üretemez.
)

var (
	// -_ dahil: URL-safe base64'ü de yakala.
	reB64 = regexp.MustCompile(`[A-Za-z0-9+/\-_]{24,}={0,2}`)
	reHex = regexp.MustCompile(`(?:\\x[0-9A-Fa-f]{2}){16,}`) // "\x41\x42..." dizisi
)

type cozButce struct {
	kalanBayt  int // saklanan çözülmüş bayt bütçesi
	decompresK int // kalan decompress hakkı (nil dönse bile harcanır)
}

// deobfuskeTara — içerikteki kodlanmış blobları SINIRLI derinlikte çözer ve her
// katmanı MEVCUT kural setiyle yeniden tarar. Amaç: eval(gzinflate(base64_decode
// ('...'))) içindeki $_POST/system'i yakalamak — regex'in göremediği düz metni.
//
// 🔴 disGoruldu: DIŞTA (düz metin taramasında) zaten eşleşen kural ID'leri.
// Bunları YENİDEN saymayız — yoksa "dosya obfuscate" sinyali çift sayılıp
// (dış + decode) meşru packer/lisans-stub'ı yapay olarak KRİTİK'e iterdi.
// Decode katmanı YALNIZ çözüldükten sonra görünen YENİ sinyali ödüllendirir.
//
// Bulgular "GOSP-DECODE:" ön ekli; puan cozTavanPu ile tavanlı.
func deobfuskeTara(icerik []byte, kurallar []Kural, ext string, disGoruldu map[string]bool) (int, []string) {
	// Ucuz ön-gate: kodlanmış gösterge yoksa hiç çalışma (rot13 tam-kopyası dahil
	// her metin dosyasında boşuna maliyet çıkarmasın).
	if !reB64.Match(icerik) && !reHex.Match(icerik) && !bytes.Contains(icerik, []byte("rot13")) {
		return 0, nil
	}
	but := &cozButce{kalanBayt: cozMaxBayt, decompresK: cozMaxDecompres}
	puan := 0
	var idler []string
	gorulen := map[uint64]bool{}

	var kaz func(data []byte, derinlik int)
	kaz = func(data []byte, derinlik int) {
		if derinlik > cozDerinlik || but.kalanBayt <= 0 {
			return
		}
		for _, blob := range cozBloblar(data, but) {
			if len(blob) < 8 {
				continue
			}
			hh := fnvHash(blob)
			if gorulen[hh] {
				continue
			}
			gorulen[hh] = true
			// Bilinen ikili varlık (görsel/font/pdf) → benign veri; tarama da
			// özyineleme de yapma (FP + boşuna iş).
			if varlikMi(blob) {
				continue
			}
			for _, k := range kurallar {
				if len(k.Uzanti) > 0 && !icerirStr(k.Uzanti, ext) {
					continue
				}
				if disGoruldu[k.ID] {
					continue // dışta zaten sayıldı → çift sayma
				}
				if k.re != nil && k.re.Match(blob) {
					puan += k.Puan
					idler = append(idler, "GOSP-DECODE:"+k.ID)
				}
			}
			kaz(blob, derinlik+1)
		}
	}
	kaz(icerik, 1)

	if puan > cozTavanPu {
		puan = cozTavanPu
	}
	return puan, dedupStr(idler)
}

// cozBloblar — data içindeki kodlanmış blobları BİR kat çözer. Her çözülen bayt
// bütçeden düşer; bütçe biterse DURUR (mevcut çağrı dahil).
func cozBloblar(data []byte, but *cozButce) [][]byte {
	var out [][]byte
	ekle := func(b []byte) {
		if len(b) == 0 || but.kalanBayt <= 0 {
			return
		}
		if len(b) > but.kalanBayt {
			b = b[:but.kalanBayt]
		}
		but.kalanBayt -= len(b)
		out = append(out, b)
	}

	// base64 (+ URL-safe) [+ iç gzinflate/gzuncompress/gzdecode]
	for _, m := range reB64.FindAll(data, cozMaxBlob) {
		if but.kalanBayt <= 0 {
			break
		}
		dec := b64Coz(m)
		if len(dec) == 0 {
			continue
		}
		ekle(dec)
		if inf := acKompres(dec, but); inf != nil {
			ekle(inf)
		}
	}
	// ham "\x41\x42..." hex dizisi [+ iç sıkıştırma] — hex koluna da acKompres
	// (yoksa gzinflate(hex2bin('...')) decode katmanını atlardı).
	for _, m := range reHex.FindAll(data, cozMaxBlob) {
		if but.kalanBayt <= 0 {
			break
		}
		temiz := bytes.ReplaceAll(m, []byte(`\x`), nil)
		if dec, err := hex.DecodeString(string(temiz)); err == nil {
			ekle(dec)
			if inf := acKompres(dec, but); inf != nil {
				ekle(inf)
			}
		}
	}
	// str_rot13 — YALNIZ içerik str_rot13 içeriyorsa (her dosyada boşuna kopya
	// alma). Regex/hex zaten kendi kanıtlarıyla gate'li.
	if bytes.Contains(data, []byte("rot13")) {
		if r := rot13(data); r != nil {
			ekle(r)
		}
	}
	return out
}

// b64Coz — std → raw-std → URL → raw-URL sırayla dener.
func b64Coz(m []byte) []byte {
	s := string(m)
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding,
	} {
		if d, err := enc.DecodeString(s); err == nil && len(d) > 0 {
			return d
		}
	}
	return nil
}

// acKompres — baytları gzip/zlib/ham-deflate olarak açmayı dener. PHP eşlemesi:
// gzinflate = ham DEFLATE, gzuncompress = zlib, gzdecode = gzip.
//
// 🔴 BÜTÇEYE BAĞLI: LimitReader kalan bütçeyle sınırlı (sabit 3MiB DEĞİL) ve
// decompresK her çağrıda (SONUÇ nil OLSA BİLE) düşürülür → yazdırılamaz bombaya
// açılıp bütçe harcamadan sonsuz koşan DoS'u keser. Oran-bombası da işaretlenir.
func acKompres(b []byte, but *cozButce) []byte {
	if len(b) < 4 || but.decompresK <= 0 || but.kalanBayt <= 0 {
		return nil
	}
	but.decompresK-- // hakkı SONUÇTAN bağımsız harca (bomba backstop)

	tavan := int64(cozMaxBayt)
	if int64(but.kalanBayt) < tavan {
		tavan = int64(but.kalanBayt)
	}

	var r io.Reader
	if b[0] == 0x1f && b[1] == 0x8b { // gzip
		if g, err := gzip.NewReader(bytes.NewReader(b)); err == nil {
			r = g
		}
	} else if z, err := zlib.NewReader(bytes.NewReader(b)); err == nil { // zlib
		r = z
	} else { // ham deflate
		r = flate.NewReader(bytes.NewReader(b))
	}
	if r == nil {
		return nil
	}
	o, e := io.ReadAll(io.LimitReader(r, tavan+1))
	if e != nil || len(o) == 0 {
		return nil
	}
	// Sıkıştırma-oranı bombası: küçük girdi devasa çıktı → tüm decode'u durdur.
	if len(o) > 4096 && len(o) > len(b)*cozBombaOran {
		but.decompresK = 0
		return nil
	}
	// Çöp decompress (özellikle ham-deflate'in yanlış eşleşmesi) sonucu ele.
	if !yazdirilabilir(o) {
		return nil
	}
	return o
}

func rot13(b []byte) []byte {
	out := make([]byte, len(b))
	degisti := false
	for i, c := range b {
		switch {
		case c >= 'a' && c <= 'z':
			out[i] = 'a' + (c-'a'+13)%26
			degisti = true
		case c >= 'A' && c <= 'Z':
			out[i] = 'A' + (c-'A'+13)%26
			degisti = true
		default:
			out[i] = c
		}
	}
	if !degisti {
		return nil
	}
	return out
}

// yazdirilabilir — çözülmüş baytların çoğu yazdırılabilir mi (metin payload mı,
// yoksa çöp/ikili mi).
func yazdirilabilir(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	n := len(b)
	if n > 512 {
		n = 512
	}
	bas := 0
	for _, c := range b[:n] {
		if c == 0 {
			return false
		}
		if (c >= 32 && c < 127) || c == '\n' || c == '\r' || c == '\t' {
			bas++
		}
	}
	return float64(bas)/float64(n) > 0.75
}

// varlikMi — blob bilinen bir ikili varlık mı (görsel/font/pdf/medya). Bunlar
// gömülü base64 varlıklarda (data-URI, PDF kütüphaneleri) sık; taramaya sokmak
// yanlış pozitif + boşuna iştir.
func varlikMi(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	switch {
	case b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G': // PNG
		return true
	case b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff: // JPEG
		return true
	case b[0] == 'G' && b[1] == 'I' && b[2] == 'F' && b[3] == '8': // GIF
		return true
	case bytes.HasPrefix(b, []byte("%PDF")): // PDF
		return true
	case bytes.HasPrefix(b, []byte("RIFF")): // WEBP/WAV
		return true
	case bytes.HasPrefix(b, []byte("wOFF")) || bytes.HasPrefix(b, []byte("wOF2")): // WOFF/WOFF2
		return true
	case bytes.HasPrefix(b, []byte("OTTO")) || (b[0] == 0x00 && b[1] == 0x01 && b[2] == 0x00 && b[3] == 0x00): // OpenType/TrueType
		return true
	}
	return false
}

func fnvHash(b []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return h.Sum64()
}

func dedupStr(l []string) []string {
	if len(l) == 0 {
		return l
	}
	g := make(map[string]bool, len(l))
	out := l[:0]
	for _, s := range l {
		if !g[s] {
			g[s] = true
			out = append(out, s)
		}
	}
	return out
}
