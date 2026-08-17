package websec

// CVSS 3.x Base Score hesaplayıcı — OSV.dev `severity[].score` alanı CVSS
// vektör string'i olarak geliyor (örn. "CVSS:3.1/AV:N/AC:L/PR:N/UI:R/S:C/C:L/I:L/A:N").
// Bu vektörü FIRST.org resmi formülüne göre Base Score'a çeviririz.
//
// Referans: https://www.first.org/cvss/v3.1/specification-document (Section 7)
//
// 🔴 SADECE Base Score — Temporal ve Environmental metrikleri yok sayarız.
// OSV verisinde de zaten yalnız Base vector döner.

import (
	"math"
	"strings"
)

// CVSSBaseScore — CVSS 3.0 veya 3.1 vector'ünden Base Score (0.0-10.0).
// Geçersiz/eksik vector → 0.
func CVSSBaseScore(vector string) float64 {
	vector = strings.TrimSpace(vector)
	if vector == "" {
		return 0
	}
	// "CVSS:3.1/AV:N/..."  → parça parça oku
	parcalar := strings.Split(vector, "/")
	m := map[string]string{}
	surum := ""
	for _, p := range parcalar {
		kv := strings.SplitN(p, ":", 2)
		if len(kv) != 2 {
			continue
		}
		if kv[0] == "CVSS" {
			surum = kv[1]
			continue
		}
		m[kv[0]] = kv[1]
	}
	// Yalnız 3.x destekli — 2.x'in formülü farklı.
	if !strings.HasPrefix(surum, "3.") {
		return 0
	}

	av, ok1 := agirlikAV[m["AV"]]
	ac, ok2 := agirlikAC[m["AC"]]
	ui, ok3 := agirlikUI[m["UI"]]
	c, ok4 := agirlikCIA[m["C"]]
	i, ok5 := agirlikCIA[m["I"]]
	a, ok6 := agirlikCIA[m["A"]]
	scope := m["S"]
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 || (scope != "U" && scope != "C") {
		return 0
	}
	// PR ağırlığı Scope'a bağlı
	var pr float64
	if scope == "U" {
		v, ok := agirlikPRUnchanged[m["PR"]]
		if !ok {
			return 0
		}
		pr = v
	} else {
		v, ok := agirlikPRChanged[m["PR"]]
		if !ok {
			return 0
		}
		pr = v
	}

	iss := 1 - (1-c)*(1-i)*(1-a)
	var impact float64
	if scope == "U" {
		impact = 6.42 * iss
	} else {
		impact = 7.52*(iss-0.029) - 3.25*math.Pow(iss-0.02, 15)
	}
	explo := 8.22 * av * ac * pr * ui

	var base float64
	if impact <= 0 {
		base = 0
	} else if scope == "U" {
		base = math.Min(impact+explo, 10)
	} else {
		base = math.Min(1.08*(impact+explo), 10)
	}
	return yuvarlaYukari(base)
}

// yuvarlaYukari — CVSS spec: "roundup to nearest 0.1" — küsurları yukarı yuvarla
// (0.001 hassasiyet). math.Round yuvarla-en-yakın kullanır; CVSS özel algoritma
// kullanıyor (ör. 4.001 → 4.1). Bkz spec Appendix.
func yuvarlaYukari(x float64) float64 {
	// x * 100_000 → tam sayı; sonra Ceil(y/10_000) → 0.1 üstüne. Küçük
	// float hatalarını 100_000'e ölçekleyerek dengeleriz.
	kesir := math.Round(x * 100_000)
	if int64(kesir)%10_000 == 0 {
		return kesir / 100_000
	}
	return math.Floor(kesir/10_000)/10 + 0.1
}

var agirlikAV = map[string]float64{
	"N": 0.85, // Network
	"A": 0.62, // Adjacent
	"L": 0.55, // Local
	"P": 0.20, // Physical
}
var agirlikAC = map[string]float64{
	"L": 0.77, // Low
	"H": 0.44, // High
}
var agirlikPRUnchanged = map[string]float64{
	"N": 0.85, "L": 0.62, "H": 0.27,
}
var agirlikPRChanged = map[string]float64{
	"N": 0.85, "L": 0.68, "H": 0.50,
}
var agirlikUI = map[string]float64{
	"N": 0.85, // None
	"R": 0.62, // Required
}
var agirlikCIA = map[string]float64{
	"N": 0.00, "L": 0.22, "H": 0.56,
}
