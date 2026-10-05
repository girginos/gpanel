package gdrive

// ortak.go — ORTAK (GirginOS'a ait) OAuth istemcisi.
//
// Sunucu sahibi kendi Google Cloud istemcisini açmak zorunda değildir: panel,
// ikilisine gömülü (ya da root'a ait dosyadaki) ortak istemci kimliğiyle cihaz
// akışını yürütür ("Google ile bağlan"). Desen, destek ajanının gömülü merkez
// anahtarıyla aynıdır.
//
// Kaynak öncelik sırası:
//  1. ldflags ile gömülü GomuluClientID + GomuluClientSecret
//     (-X girginospanel/internal/backups/hedef/gdrive.GomuluClientID=… ; yayın
//     derlemesinde scripts/build-assets.sh GOSP_GDRIVE_CLIENT_ID/SECRET'ten);
//  2. OrtakDosyaYolu (/etc/girginospanel/gdrive-istemci.json, root 0600).
//     Sahibi çalışan kullanıcı değilse ya da grup/diğer okuyabiliyorsa REDDEDİLİR
//     (açık hata); dosya yoksa sessizce yok sayılır.
//
// 🔴 Gizli anahtar hiçbir yanıtta, günlükte, denetimde ya da hata metninde
// görünmez; bu dosyadaki hata metinleri SABİTTİR (değer içermez). Cihaz akışı
// istemcilerinin gizli anahtarını Google "gizli" saymaz, yine de ikiliye gömülü
// değer sızdırılmaz.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"girginospanel/internal/sirdosya"
)

// IstemciOrtak — Ayar.Istemci değeri: hedef ortak istemciyi kullanır.
const IstemciOrtak = "ortak"

// Gömülü ortak istemci (boş = gömülü değil). ldflags -X ile doldurulur.
var (
	GomuluClientID     = ""
	GomuluClientSecret = ""
)

// OrtakDosyaYolu — ortak istemci dosyası (testlerde değiştirilir).
var OrtakDosyaYolu = "/etc/girginospanel/gdrive-istemci.json"

// Ortak istemci hataları (metinler sabit; ErrOrtakYok dışındakiler yapılandırma
// sorunudur ve kullanıcıya olduğu gibi gösterilebilir).
var (
	ErrOrtakYok      = errors.New("Ortak Google istemcisi bu sürümde yapılandırılmamış; Gelişmiş'ten kendi istemcinizi girin")
	ErrOrtakGuvensiz = errors.New("Ortak Google istemci dosyasının sahibi/izni güvensiz (yalnız root okuyabilmeli: chmod 600); dosya yok sayıldı")
	ErrOrtakBozuk    = errors.New("Ortak Google istemci yapılandırması geçersiz (client_id/client_secret biçimi)")
)

// ortakDosya — iki biçim kabul edilir:
//  1. düz:   {"client_id":"…","client_secret":"…"}
//  2. Google Cloud Console'dan "JSON'u indir" ile gelen dosya (cihaz akışı
//     istemcisi = "TV'ler ve Sınırlı Girişli Cihazlar" türü):
//     {"installed":{"client_id":"…","client_secret":"…","project_id":"…","auth_uri":"…","token_uri":"…",…}}
//
// 🔴 Google biçimindeki auth_uri/token_uri/project_id/… alanları YOK SAYILIR:
// uç adresleri koda sabittir (agkapi izin listesi); dosyadan uç adresi alınmaz.
type ortakDosya struct {
	ClientID     string          `json:"client_id"`
	ClientSecret string          `json:"client_secret"`
	Installed    json.RawMessage `json:"installed"`
}

type ortakIstemi struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// OrtakIstemci — geçerli ortak istemciyi çözer. Her çağrıda dosya yeniden
// okunur (sürüm/dosya değişikliği yeniden başlatma istemez).
func OrtakIstemci() (Kimlik, error) {
	gomuluVar := strings.TrimSpace(GomuluClientID) != "" || strings.TrimSpace(GomuluClientSecret) != ""
	if gomuluVar {
		k := Kimlik{ClientID: strings.TrimSpace(GomuluClientID), ClientSecret: strings.TrimSpace(GomuluClientSecret)}
		if ClientIDGecerli(k.ClientID) && ClientSecretGecerli(k.ClientSecret) {
			return k, nil
		}
	}
	b, st, err := sirdosya.Oku(OrtakDosyaYolu, 8<<10)
	if err != nil {
		if gomuluVar {
			return Kimlik{}, ErrOrtakBozuk
		}
		if errors.Is(err, os.ErrNotExist) {
			return Kimlik{}, ErrOrtakYok
		}
		return Kimlik{}, ErrOrtakGuvensiz
	}
	if uid, _, ok := sirdosya.Sahip(st); !ok || int(uid) != os.Getuid() || st.Mode().Perm()&0o077 != 0 {
		return Kimlik{}, ErrOrtakGuvensiz
	}
	// Üst düzey katı: yalnız client_id/client_secret/installed (yazım hatası ve
	// sahte uç adresi alanları reddedilir). "installed" İÇİ Google'ın kendi
	// alanlarını taşır (project_id, auth_uri, token_uri…): yok sayılır.
	var d ortakDosya
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if dec.Decode(&d) != nil {
		return Kimlik{}, ErrOrtakBozuk
	}
	k := Kimlik{ClientID: strings.TrimSpace(d.ClientID), ClientSecret: strings.TrimSpace(d.ClientSecret)}
	if len(d.Installed) > 0 {
		// İki biçim birlikte verilmişse belirsizlik: reddet (hangisinin geçerli
		// olduğu tahmin edilmez).
		if k.ClientID != "" || k.ClientSecret != "" {
			return Kimlik{}, ErrOrtakBozuk
		}
		var ic ortakIstemi
		if json.Unmarshal(d.Installed, &ic) != nil {
			return Kimlik{}, ErrOrtakBozuk
		}
		k = Kimlik{ClientID: strings.TrimSpace(ic.ClientID), ClientSecret: strings.TrimSpace(ic.ClientSecret)}
	}
	if !ClientIDGecerli(k.ClientID) || !ClientSecretGecerli(k.ClientSecret) {
		return Kimlik{}, ErrOrtakBozuk
	}
	return k, nil
}

// OrtakHazir — kullanılabilir ortak istemci var mı?
func OrtakHazir() bool {
	_, err := OrtakIstemci()
	return err == nil
}

// MaskeID — istemci kimliğini arayüze/yanıta güvenle gösterir (ilk 8 karakter + …).
func MaskeID(id string) string {
	r := []rune(id)
	if len(r) <= 8 {
		return string(r)
	}
	return string(r[:8]) + "…"
}
