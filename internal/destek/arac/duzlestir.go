package arac

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"girginospanel/internal/maske"
)

// hassasAnahtar — adı kimlik bilgisi çağrıştıran JSON alanları değerleriyle
// birlikte ATILIR (maske desenine güvenmeden; ör. {"ftp_parola":"x"}).
var hassasParcalar = []string{"parola", "password", "passwd", "pass", "secret", "sifre", "şifre",
	"token", "jeton", "anahtar", "key", "hash", "salt", "cookie", "cerez", "çerez", "otp",
	"credential", "kimlik_bilgi", "dsn", "env"}

func hassasAnahtar(ad string) bool {
	k := strings.ToLower(ad)
	for _, p := range hassasParcalar {
		if strings.Contains(k, p) {
			return true
		}
	}
	return false
}

// duzlestirSiniri — tek yanıttan üretilecek en çok satır (sonra KesSatirlar 300'e iner).
const duzlestirSiniri = 2000

// duzlestir — JSON değerini "yol: değer" satırlarına açar (anahtarlar sıralı,
// hassas anahtarlar atılır, çok satırlı değer tek satıra iner).
func duzlestir(v any, onek string) []string {
	var out []string
	duzlestirIc(v, onek, &out, 0)
	return out
}

func duzlestirIc(v any, yol string, out *[]string, derinlik int) {
	if len(*out) >= duzlestirSiniri || derinlik > 8 {
		return
	}
	switch t := v.(type) {
	case map[string]any:
		anahtarlar := make([]string, 0, len(t))
		for k := range t {
			if !hassasAnahtar(k) {
				anahtarlar = append(anahtarlar, k)
			}
		}
		sort.Strings(anahtarlar)
		for _, k := range anahtarlar {
			duzlestirIc(t[k], yolEkle(yol, k), out, derinlik+1)
		}
	case []any:
		if len(t) == 0 {
			*out = append(*out, yol+": []")
			return
		}
		for i, e := range t {
			duzlestirIc(e, yol+"["+strconv.Itoa(i)+"]", out, derinlik+1)
		}
	default:
		*out = append(*out, yol+": "+deger(t))
	}
}

func yolEkle(yol, k string) string {
	if yol == "" {
		return k
	}
	return yol + "." + k
}

// deger — skaler JSON değerinin tek satırlık metni.
func deger(v any) string {
	switch t := v.(type) {
	case nil:
		return "-"
	case string:
		return maske.TekSatir(t)
	case json.Number:
		return t.String()
	case bool:
		if t {
			return "evet"
		}
		return "hayır"
	}
	return maske.TekSatir(fmt.Sprint(v))
}

// nesne — v bir JSON nesnesiyse onu, değilse boş harita.
func nesne(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// dizi — v bir JSON dizisiyse onu, değilse nil.
func dizi(v any) []any {
	a, _ := v.([]any)
	return a
}

// sec — nesnenin yalnız izinli üst düzey alanlarını (verilen sırayla) düzleştirir.
func sec(m map[string]any, onek string, alanlar ...string) []string {
	var out []string
	for _, a := range alanlar {
		if v, ok := m[a]; ok && !hassasAnahtar(a) {
			out = append(out, duzlestir(v, yolEkle(onek, a))...)
		}
	}
	return out
}

// metin — nesne alanının düz metni ("" yoksa).
func metin(m map[string]any, a string) string {
	v, ok := m[a]
	if !ok || v == nil {
		return ""
	}
	return deger(v)
}

// satirlarAl — {"satirlar": [...]} yanıtındaki dizgeler.
func satirlarAl(v any) []string {
	var out []string
	for _, e := range dizi(nesne(v)["satirlar"]) {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
