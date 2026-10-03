package php

// kok_sinir.go — PHP ayar uçlarında (PUT /domains/{id}/php-settings ve alt alan eşi
// PUT /domains/{id}/subdomain/{sid}/php-settings) kök/bayi/plan sınırının sunucu
// tarafı kapısı. Uçlar MusteriScope arkasında: müşteri, bayi ve müşteri-adına
// oturumu çağırabilir.
//
// 🔴 NEDEN: bayi kökün iç alanlarını /limitler'de yazamaz oldu (bayi-ic-alan), ama
// aynı iç alan (pm_max_children) ve türevleri PHP ayar gövdesinde serbestti; GET
// de değerini gösteriyordu. Paylaşılan / alt alan havuzu bu değeri AYNEN yazdığı
// için müşteri planın işçi sayısını (ve slice dışında koştuğundan cgroup sınırını)
// aşabiliyordu; alt alanda disable_functions / open_basedir de korumasızdı.
//
// Kural (yönetici dışı her oturum):
//   - İç alan anahtarları (efektiflimit.PHPAyarIcAnahtarlari): GÖRÜNMEZ; gövdede
//     anahtar VARSA (değeri ne olursa olsun — null/aynı değer dahil, kâhin olmasın)
//     403, hiçbir şey yazılmaz. Değer her zaman mevcut kayıttan korunur.
//   - Kök korumalı alanlar (kokKorumaliAnahtarlar: izolasyon sınırı): görünür ama
//     DEĞİŞTİRİLEMEZ; mevcuttan farklı değer → 403, aynı değer serbest (görünen
//     değeri geri göndermek bilgi sızdırmaz). Değer her zaman mevcuttan korunur.
//   - Boyut/süre ayarları yazılabilir; havuz render'ında plana kırpılır
//     (provisioner.PlanHavuzTavani — üç havuz türü de aynı uygulayıcı).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"girginospanel/internal/efektiflimit"
	"girginospanel/internal/middleware"
	"girginospanel/internal/provisioner"
)

// kokKorumaliAnahtarlar — havuzun izolasyon sınırı (per-tenant havuzda zaten sabit
// yazılır; paylaşılan/alt alan havuzunda değer aynen kullanılır). Yalnız yönetici
// değiştirir.
var kokKorumaliAnahtarlar = []string{
	"disable_functions", "open_basedir", "session_save_path", "mail_force_extra_parameters",
}

// ayarGovdeSiniri — PHP ayar gövdesi üst sınırı (ek_direktifler dahil bol).
const ayarGovdeSiniri = 1 << 20

// ayarIstegi — çözülmüş PUT gövdesi + ayarlar'da gelen ham anahtarlar.
type ayarIstegi struct {
	PHPSurum string
	Ayarlar  Settings
	anahtar  []string
}

// gelenVar — gövdede (encoding/json'un harf katlamasıyla eşleşen) anahtar var mı.
// json struct çözümü anahtarları büyük/küçük harf duyarsız eşler; "PM_MAX_CHILDREN"
// da alanı doldurur — kapı aynı katlamayı (strings.EqualFold) kullanır.
func (a ayarIstegi) gelenVar(ad string) bool {
	for _, k := range a.anahtar {
		if strings.EqualFold(k, ad) {
			return true
		}
	}
	return false
}

var errAyarGovde = errors.New("geçersiz gövde")

// ayarIstegiOku — {php_surum?, ayarlar} gövdesini okur.
func ayarIstegiOku(r *http.Request) (ayarIstegi, error) {
	var ham struct {
		PHPSurum string          `json:"php_surum"`
		Ayarlar  json.RawMessage `json:"ayarlar"`
	}
	var a ayarIstegi
	if err := json.NewDecoder(io.LimitReader(r.Body, ayarGovdeSiniri)).Decode(&ham); err != nil {
		return a, err
	}
	a.PHPSurum = ham.PHPSurum
	if len(ham.Ayarlar) == 0 || string(ham.Ayarlar) == "null" {
		return a, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(ham.Ayarlar, &m); err != nil {
		return a, errAyarGovde
	}
	for k := range m {
		a.anahtar = append(a.anahtar, k)
	}
	if err := json.Unmarshal(ham.Ayarlar, &a.Ayarlar); err != nil {
		return a, err
	}
	return a, nil
}

// kokAlaniDeger — korumalı alanın Settings'teki adresi.
func kokAlaniDeger(s *Settings, ad string) *string {
	switch ad {
	case "disable_functions":
		return &s.DisableFunctions
	case "open_basedir":
		return &s.OpenBasedir
	case "session_save_path":
		return &s.SessionSavePath
	case "mail_force_extra_parameters":
		return &s.MailForceExtraParameters
	}
	return nil
}

// kokSiniriUygula — yönetici dışı istekte kök/plan alanlarını denetler. Red listesi
// boş değilse çağıran 403 döner (efektiflimit.IcAlanRedMesaji) ve HİÇBİR ŞEY
// yazmaz. Boşsa a.Ayarlar'daki iç ve korumalı alanlar mevcut değerle doldurulmuştur.
func kokSiniriUygula(a *ayarIstegi, mevcut Settings) []string {
	var red []string
	for _, k := range efektiflimit.PHPAyarIcAnahtarlari() {
		if a.gelenVar(k) {
			red = append(red, k)
		}
	}
	for _, k := range kokKorumaliAnahtarlar {
		if a.gelenVar(k) && *kokAlaniDeger(&a.Ayarlar, k) != *kokAlaniDeger(&mevcut, k) {
			red = append(red, k)
		}
	}
	if len(red) > 0 {
		return red
	}
	a.Ayarlar.PMMaxChildren = mevcut.PMMaxChildren
	a.Ayarlar.PMStartServers = mevcut.PMStartServers
	a.Ayarlar.PMMinSpareServers = mevcut.PMMinSpareServers
	a.Ayarlar.PMMaxSpareServers = mevcut.PMMaxSpareServers
	for _, k := range kokKorumaliAnahtarlar {
		*kokAlaniDeger(&a.Ayarlar, k) = *kokAlaniDeger(&mevcut, k)
	}
	return nil
}

// kokYetkili — kök/plan alanlarını yazıp görebilen oturum (yalnız yönetici).
func kokYetkili(r *http.Request) bool { return middleware.RolFrom(r) == "admin" }

// IstegiCoz — PUT gövdesini okur ve yönetici dışı oturumda kök sınırını uygular.
// kod=0 → başarılı; aksi halde çağıran kod/mesajı döner, hiçbir şey yazmaz.
// mevcut: alanın (ya da alt alanın) KAYITLI ayarı (satır yoksa Defaults()).
func IstegiCoz(r *http.Request, mevcut Settings) (surum string, s Settings, kod int, mesaj string) {
	a, err := ayarIstegiOku(r)
	if err != nil {
		return "", Settings{}, http.StatusBadRequest, "geçersiz gövde: " + err.Error()
	}
	if !kokYetkili(r) {
		if red := kokSiniriUygula(&a, mevcut); len(red) > 0 {
			return "", Settings{}, http.StatusForbidden, efektiflimit.IcAlanRedMesaji(red)
		}
	}
	return a.PHPSurum, a.Ayarlar, 0, ""
}

// IstemciAyarlari — GET yanıtındaki ayarlar: yönetici dışı oturumda kökün iç alan
// anahtarları ÇIKARILIR (bayi/müşteri kökün iç değerini görmez — limitGorunum ile
// aynı kural). Korumalı alanlar görünür kalır (salt okunur gösterilir).
func IstemciAyarlari(r *http.Request, s Settings) any {
	if kokYetkili(r) {
		return s
	}
	b, err := json.Marshal(s)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{}
	}
	for _, k := range efektiflimit.PHPAyarIcAnahtarlari() {
		delete(m, k)
	}
	return m
}

// havuzTavanla — paylaşılan / alt alan havuzuna YAZILACAK değerler: plan tavanı
// (provisioner.PlanHavuzTavani — per-tenant havuzla aynı uygulayıcı) + asla düşmeyen
// disable_functions tabanı. DB'deki kayıt değişmez; yalnız render edilen havuz.
func havuzTavanla(db *sql.DB, domainID int64, sk string, s Settings) Settings {
	hv := provisioner.PlanHavuzTavani(db, domainID, sk, provisioner.HavuzDegerleri{
		PMStrategy:        s.PMStrategy,
		UploadMaxFilesize: s.UploadMaxFilesize,
		PostMaxSize:       s.PostMaxSize,
		MaxExecutionTime:  s.MaxExecutionTime,
		MaxInputTime:      s.MaxInputTime,
	})
	s.PMStrategy = hv.PMStrategy
	s.PMMaxChildren, s.PMStartServers = hv.MaxChildren, hv.StartServers
	s.PMMinSpareServers, s.PMMaxSpareServers = hv.MinSpareServers, hv.MaxSpareServers
	s.UploadMaxFilesize, s.PostMaxSize = hv.UploadMaxFilesize, hv.PostMaxSize
	s.MaxExecutionTime, s.MaxInputTime = hv.MaxExecutionTime, hv.MaxInputTime
	s.DisableFunctions = provisioner.DisableFunctionsTabanli(s.DisableFunctions)
	return s
}

// altHavuzAyarlari — alt alan havuzunun render edilecek ayarları: kayıt + üst
// alan adının planına göre tavan.
func altHavuzAyarlari(ctx context.Context, db *sql.DB, sk string, subID int64) (Settings, error) {
	s, err := GetSub(ctx, db, subID)
	if err != nil {
		return s, err
	}
	var domainID int64
	// Okunamazsa domainID=0 → plan yok varsayılanı (8 işçi; süre tavanı yine uygulanır).
	_ = db.QueryRowContext(ctx, `SELECT domain_id FROM subdomanlar WHERE id=?`, subID).Scan(&domainID)
	return havuzTavanla(db, domainID, sk, s), nil
}
