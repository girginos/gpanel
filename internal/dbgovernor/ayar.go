package dbgovernor

// ayar.go — kısıcı ayarları. Kaynak: cp_ayarlar (anahtar/değer; 0055). Satır yoksa varsayılan
// geçerlidir; geçersiz/aralık dışı değer SESSİZCE varsayılana/sınıra çekilir (bozuk bir ayar
// kısıcıyı agresifleştirmesin). Ayarlar her turda yeniden okunur: değişiklik en çok bir tur
// (varsayılan 15 sn) sonra uygulanır, yeniden başlatma gerekmez.

import (
	"context"
	"database/sql"
	"math"
	"strconv"
	"strings"
	"time"
)

// Mod: kısıcının çalışma kipi.
type Mod string

const (
	// ModKapali: hiçbir şey ölçülmez/yazılmaz; açık kısıt varsa geri alınır.
	ModKapali Mod = "kapali"
	// ModGozlem (VARSAYILAN): ölçüm + kayıt + API + bildirim ("uygula modunda kısılırdı"); YENİ kısıt
	// için MariaDB'ye HİÇBİR değişiklik yapılmaz (ALTER/KILL yok). Önceki uygula döneminden kalan
	// kısıtlar normal soğumayla kalkar (geri yazma tek izinli ALTER'dir). Eşiği gerçek trafikle
	// sınamak için.
	ModGozlem Mod = "gozlem"
	// ModUygula: tam kısma.
	ModUygula Mod = "uygula"
)

// Ayar: çalışma ayarları.
type Ayar struct {
	Mod Mod
	// Poll: ölçüm aralığı.
	Poll time.Duration
	// Pencere: CPU hızının ortalandığı kayan pencere (en az 2 yoklama).
	Pencere time.Duration
	// EsikHosting: planı cpu_yuzde vermeyen hostinglerin DB CPU eşiği (çekirdek). Planı olanda
	// eşik = max(MinEsik, cpu_yuzde/100).
	EsikHosting float64
	// KisitBaglanti: kısıtta MAX_USER_CONNECTIONS değeri.
	KisitBaglanti int
	// Yukselme: kısıt bu süre sürüp hâlâ eşik üstündeyse 2. seviye (okuma sorgusu KILL).
	Yukselme time.Duration
	// KillEsigi: 2. seviyede bu süreden uzun çalışan OKUMA sorguları öldürülür.
	KillEsigi time.Duration
	// Soguma: eşik (x SogumaOrani) altında bu kadar kalınca kısıt kalkar.
	Soguma      time.Duration
	SogumaOrani float64
	// UzunSorgu: anlık CPU göstergesi (canlı) için sorgunun en az bu kadar sürmüş olması.
	UzunSorgu time.Duration
	// BaglantiKes: 2. seviyeden sonra aşım bu süre daha sürerse 3. seviye — o DB kullanıcısının
	// bağlantıları KILL CONNECTION ile kesilir (yeniden bağlanan istemci düşük sınıra takılır).
	BaglantiKes time.Duration
}

// MinEsik: hosting eşiğinin alt sınırı (çekirdek). Çok dar bir planın DB eşiği de bunun altına
// inmez: normal bir sitenin DB CPU'su çekirdeğin birkaç yüzdesidir.
const MinEsik = 0.5

// VarsayilanAyar: ayar satırı yokken geçerli değerler.
func VarsayilanAyar() Ayar {
	return Ayar{
		Mod:           ModGozlem,
		Poll:          15 * time.Second,
		Pencere:       60 * time.Second,
		EsikHosting:   1.0,
		KisitBaglanti: 2,
		Yukselme:      45 * time.Second,
		KillEsigi:     10 * time.Second,
		Soguma:        5 * time.Minute,
		SogumaOrani:   0.8,
		UzunSorgu:     5 * time.Second,
		BaglantiKes:   3 * time.Minute,
	}
}

// Ayar anahtarları (cp_ayarlar.anahtar).
const (
	anMod      = "db_kisici_mod"        // kapali | gozlem | uygula
	anEsik     = "db_kisici_esik"       // çekirdek (ondalık)
	anPoll     = "db_kisici_poll_sn"    // 5..120
	anPencere  = "db_kisici_pencere_sn" // 2*poll..900
	anBaglanti = "db_kisici_baglanti"   // 1..100
	anYukselme = "db_kisici_yukselme_sn"
	anKill     = "db_kisici_kill_sn"
	anSoguma   = "db_kisici_soguma_sn"
	anUzun     = "db_kisici_uzun_sn"
	anKes      = "db_kisici_baglanti_kes_dk" // 1..120 dakika
)

// ModAnahtari: kök ayarının cp_ayarlar anahtarı (API yazar).
const ModAnahtari = anMod

// ModCoz: "kapali|gozlem|uygula" (büyük/küçük harf ve boşluk duyarsız) → Mod.
func ModCoz(s string) (Mod, bool) {
	switch m := Mod(strings.ToLower(strings.TrimSpace(s))); m {
	case ModKapali, ModGozlem, ModUygula:
		return m, true
	}
	return "", false
}

// sayi: sonlu bir ondalık sayı ayrıştırır (NaN/Inf reddedilir: süreye/eşiğe sızmasın).
func sayi(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

func sinirla(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ayarUygula: anahtar→değer haritasını varsayılanın üzerine yazar (saf; test edilebilir).
func ayarUygula(m map[string]string) Ayar {
	a := VarsayilanAyar()
	sn := func(anahtar string, lo, hi float64, varsayilan time.Duration) time.Duration {
		s, ok := m[anahtar]
		if !ok {
			return varsayilan
		}
		v, ok := sayi(s)
		if !ok {
			return varsayilan
		}
		return time.Duration(sinirla(v, lo, hi) * float64(time.Second))
	}
	if s, ok := m[anMod]; ok {
		if md, ok := ModCoz(s); ok {
			a.Mod = md // bozuk değer varsayılanda (gozlem = güvenli yön) kalır
		}
	}
	if s, ok := m[anEsik]; ok {
		if v, ok := sayi(s); ok {
			a.EsikHosting = sinirla(v, 0.1, 64)
		}
	}
	if s, ok := m[anBaglanti]; ok {
		if v, ok := sayi(s); ok {
			a.KisitBaglanti = int(sinirla(v, 1, 100))
		}
	}
	a.Poll = sn(anPoll, 5, 120, a.Poll)
	a.Pencere = sn(anPencere, 10, 900, a.Pencere)
	if a.Pencere < 2*a.Poll {
		a.Pencere = 2 * a.Poll
	}
	a.Yukselme = sn(anYukselme, 10, 3600, a.Yukselme)
	a.KillEsigi = sn(anKill, 1, 3600, a.KillEsigi)
	a.Soguma = sn(anSoguma, 30, 86400, a.Soguma)
	a.UzunSorgu = sn(anUzun, 1, 600, a.UzunSorgu)
	if s, ok := m[anKes]; ok {
		if v, ok := sayi(s); ok {
			a.BaglantiKes = time.Duration(sinirla(v, 1, 120) * float64(time.Minute))
		}
	}
	return a
}

// ayarOku: cp_ayarlar'dan db_kisici_* satırlarını okur. HATA döner (çağıran o turda yeni kısıt
// koymaz — okunamayan ayarla agresif davranmaktan iyidir).
func ayarOku(ctx context.Context, db *sql.DB) (Ayar, error) {
	rows, err := db.QueryContext(ctx, `SELECT anahtar, deger FROM cp_ayarlar WHERE anahtar LIKE 'db\_kisici\_%'`)
	if err != nil {
		return Ayar{}, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return Ayar{}, err
		}
		m[k] = v
	}
	if err := rows.Err(); err != nil {
		return Ayar{}, err
	}
	return ayarUygula(m), nil
}
