package avmotor

// FAZ 0 — obfuscation'a karşı sertleşme: bölgesel (kayan pencere) entropi +
// SINIRLI deobfuscation-yeniden-tarama. İkisi de mevcut kural motorunun üstüne
// biner; yeni altyapı gerektirmez.
//
// 🔴 Tasarım ilkesi: ucuz katman önce (regex/konum/entropi), pahalı katman
// (decode) yalnız içerik gerçekten kodlanmış blob içeriyorsa iş yapar. Tüm
// decode işi KATI bir bütçeyle sınırlıdır (derinlik + bayt + blob sayısı) —
// sonsuz/özyineli decode bir DoS vektörüdür.

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"encoding/hex"
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
// "normal kod içine GÖMÜLÜ yüksek-entropili blok" arar. Bu ayrım kritiktir:
// minified JS / base64 varlık dosya BOYUNCA yüksek entropilidir (yanlış poz);
// gizlenmiş payload ise LOKAL yüksek, çevresi normal koddur (gerçek sinyal).
//
// Dönüş: (skor, kuralID, tespit).
func bolgeselEntropi(b []byte) (int, string, bool) {
	const pencere = 1024
	const adim = 512
	if len(b) < pencere {
		if len(b) >= 256 && shannon(b) > 5.3 {
			return 15, "GOSP-HEUR-ENTROPI", true
		}
		return 0, "", false
	}
	maxE, minE := 0.0, 8.0
	yuksek, toplam := 0, 0
	for off := 0; off+pencere <= len(b); off += adim {
		e := shannon(b[off : off+pencere])
		toplam++
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
	// ayırır. Ayırt edici: dosyada belirgin bir NORMAL-KOD bölgesi (düşük min)
	// de var mı? Varsa → kod içine gömülü blob (gerçek sinyal). Yoksa (dosya
	// boyunca yüksek) → minified/base64 varlık olabilir (zayıf).
	if maxE <= 5.5 || yuksek == 0 {
		return 0, "", false
	}
	if minE < 4.7 {
		return 35, "GOSP-HEUR-ENTROPI-BLOB", true
	}
	return 15, "GOSP-HEUR-ENTROPI", true
}

// ── Sınırlı deobfuscation ───────────────────────────────────────────────────

// Bütçe — sonsuz/DoS decode'a karşı sandbox.
const (
	cozDerinlik = 4       // en fazla iç içe decode katmanı
	cozMaxBayt  = 3 << 20 // toplam çözülmüş bayt tavanı (3 MiB)
	cozMaxBlob  = 12      // katman başına en fazla blob
	cozTavanPu  = 120     // decode katmanının verebileceği en fazla puan
)

var (
	reB64 = regexp.MustCompile(`[A-Za-z0-9+/]{24,}={0,2}`)
	reHex = regexp.MustCompile(`(?:\\x[0-9A-Fa-f]{2}){16,}`) // "\x41\x42..." dizisi
)

type cozButce struct{ kalanBayt int }

// deobfuskeTara — içerikteki kodlanmış blobları SINIRLI derinlikte çözer ve
// çözülmüş her katmanı MEVCUT kural setiyle yeniden tarar. Amaç:
//
//	eval(gzinflate(base64_decode('...')))  →  içteki $_POST/system/... yakala.
//
// Regex düz metni göremez; bu katman görebilir. Bütçe aşılırsa DURUR. Bulgular
// "GOSP-DECODE:" ön ekiyle işaretlenir — panelde obfuscate edildiği görünür.
func deobfuskeTara(icerik []byte, kurallar []Kural, ext string) (int, []string) {
	but := &cozButce{kalanBayt: cozMaxBayt}
	puan := 0
	var idler []string
	gorulen := map[string]bool{}

	var kaz func(data []byte, derinlik int)
	kaz = func(data []byte, derinlik int) {
		if derinlik > cozDerinlik || but.kalanBayt <= 0 {
			return
		}
		for _, blob := range cozBloblar(data, but) {
			if len(blob) < 8 {
				continue
			}
			// Aynı payload'ı iki kez çözme (rot13(rot13(x))==x döngüsü dahil).
			bas := blob
			if len(bas) > 24 {
				bas = bas[:24]
			}
			h := string(bas) + string(rune(len(blob)%251))
			if gorulen[h] {
				continue
			}
			gorulen[h] = true
			for _, k := range kurallar {
				if len(k.Uzanti) > 0 && !icerirStr(k.Uzanti, ext) {
					continue
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

// cozBloblar — data içindeki kodlanmış blobları BİR kat çözer (base64 [+ iç
// sıkıştırma], \x hex, str_rot13). Her çözülen bayt bütçeden düşer.
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

	// base64 blobları (+ base64 içi gzinflate/gzuncompress/gzdecode)
	for _, m := range reB64.FindAll(data, cozMaxBlob) {
		dec, err := base64.StdEncoding.DecodeString(string(m))
		if err != nil {
			dec, err = base64.RawStdEncoding.DecodeString(string(m))
		}
		if err == nil && len(dec) > 0 {
			ekle(dec)
			if inf := acKompres(dec); inf != nil {
				ekle(inf)
			}
		}
	}
	// ham "\x41\x42..." hex dizisi
	for _, m := range reHex.FindAll(data, cozMaxBlob) {
		temiz := bytes.ReplaceAll(m, []byte(`\x`), nil)
		if dec, err := hex.DecodeString(string(temiz)); err == nil {
			ekle(dec)
		}
	}
	// str_rot13 — ucuz, tüm içeriğe uygula
	if r := rot13(data); r != nil {
		ekle(r)
	}
	return out
}

// acKompres — baytları gzip/zlib/ham-deflate olarak açmayı dener. PHP eşlemesi:
// gzinflate = ham DEFLATE, gzuncompress = zlib, gzdecode = gzip.
func acKompres(b []byte) []byte {
	if len(b) < 4 {
		return nil
	}
	// gzip (magic 1f 8b)
	if b[0] == 0x1f && b[1] == 0x8b {
		if r, err := gzip.NewReader(bytes.NewReader(b)); err == nil {
			if o, e := io.ReadAll(io.LimitReader(r, cozMaxBayt)); e == nil && len(o) > 0 {
				return o
			}
		}
	}
	// zlib (gzuncompress)
	if r, err := zlib.NewReader(bytes.NewReader(b)); err == nil {
		if o, e := io.ReadAll(io.LimitReader(r, cozMaxBayt)); e == nil && len(o) > 0 {
			return o
		}
	}
	// ham deflate (gzinflate)
	fr := flate.NewReader(bytes.NewReader(b))
	if o, e := io.ReadAll(io.LimitReader(fr, cozMaxBayt)); e == nil && len(o) > 0 && yazdirilabilir(o) {
		return o
	}
	return nil
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
// yoksa çöp/ikili mi). Ham deflate her baytı "açar"; sonuç anlamsızsa atla.
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
