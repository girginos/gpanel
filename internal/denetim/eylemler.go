package denetim

import (
	"sort"
	"strings"
	"unicode"
)

// EylemEtiket: bir denetim eylem kodunun insan dilindeki adı.
//
// Eylem kodları (ör. "musteri.adina_cikis") kalıcı kayıt anahtarıdır, kullanıcıya
// GÖSTERİLMEZ; ön yüz yalnız bu etiketleri gösterir, kod yalnız ipucunda (title)
// durur. Kaynakta yazılan her kodun burada etiketi olmak ZORUNDADIR —
// TestEylemKataloguKaynaktakiTumKodlariKapsar kaynak kodu tarayarak denetler.
type EylemEtiket struct {
	TR string `json:"tr"`
	EN string `json:"en"`

	// teknik: başarılı kaydı varsayılan listede gizlenir (aynı olayı daha anlamlı
	// bir başka kayıt zaten anlatır). Eylem süzgecinde açıkça seçilince görünür;
	// başarısız kayıtları HER ZAMAN görünür.
	teknik bool
	// gereksizNot: etiketi tekrarlayan sabit açıklamalar — listede gösterilmez.
	// "*" ile biten girdi önek eşleşmesidir.
	gereksizNot []string
}

// eylemKatalogu: kod → etiket. Kaynakta artık yazılmayan eski kodlar da (eski
// kayıtlar düzgün görünsün diye) burada kalır.
var eylemKatalogu = map[string]EylemEtiket{
	// Oturum / hesap
	"auth.login":       {TR: "Giriş", EN: "Login"},
	"auth.login.kapi":  {TR: "Yanlış porttan giriş denemesi", EN: "Login attempt on wrong port"},
	"auth.2fa":         {TR: "2FA doğrulama", EN: "2FA verification"},
	"auth.2fa.enable":  {TR: "2FA açıldı", EN: "2FA enabled"},
	"auth.2fa.disable": {TR: "2FA kapatıldı", EN: "2FA disabled"},
	"auth.parola":      {TR: "Parola değiştirildi", EN: "Password changed"},

	// Müşteri adına giriş (bayi/yönetici müşteri paneline girer)
	"musteri.adina_kod": {TR: "Müşteri paneline giriş", EN: "Customer panel access", teknik: true,
		gereksizNot: []string{"müşteri paneline giriş kodu üretildi"}},
	"musteri.adina_oturum": {TR: "Müşteri paneline girildi", EN: "Entered customer panel",
		gereksizNot: []string{"müşteri adına oturum açıldı"}},
	"musteri.adina_cikis": {TR: "Müşteri panelinden çıkıldı", EN: "Left customer panel",
		gereksizNot: []string{"müşteri adına oturum kapatıldı"}},

	// Hosting
	"hosting.olustur":                  {TR: "Hosting oluşturuldu", EN: "Hosting created"},
	"hosting.sil":                      {TR: "Hosting silindi", EN: "Hosting deleted"},
	"hosting.sil.uzak-yedek-birakildi": {TR: "Hosting silindi, uzak yedekler bırakıldı", EN: "Hosting deleted, remote backups kept"},
	"hosting.askiya_al":                {TR: "Hosting askıya alındı", EN: "Hosting suspended"},
	"hosting.askidan_al":               {TR: "Hosting askıdan alındı", EN: "Hosting unsuspended"},
	"hosting.plan":                     {TR: "Plan değiştirildi", EN: "Plan changed"},
	"domain.ip_degistir":               {TR: "IP adresi değiştirildi", EN: "IP address changed"},
	"tasima_bayi_ayar":                 {TR: "Bayi taşıma izni değiştirildi", EN: "Reseller migration access changed"},
	"tasima_kaynak_ayar":               {TR: "Taşıma kaynağı ayarı değiştirildi", EN: "Migration source setting changed"},
	"hosting.limit":                    {TR: "Hostinge özel limitler değiştirildi", EN: "Hosting custom limits changed"},
	"sistem.ozel_plan_donusum":         {TR: "Özel plan kopyası dönüştürüldü", EN: "Custom plan copy converted"},
	"marka.guncelle":                   {TR: "Marka güncellendi", EN: "Branding updated"},
	"marka.sifirla":                    {TR: "Marka sıfırlandı", EN: "Branding reset"},
	"marka.gorsel.yukle":               {TR: "Marka görseli yüklendi", EN: "Branding image uploaded"},
	"marka.gorsel.sil":                 {TR: "Marka görseli silindi", EN: "Branding image deleted"},
	"marka.izin":                       {TR: "Bayi marka izni değiştirildi", EN: "Reseller branding permission changed"},
	"marka.ayar":                       {TR: "Marka ayarları değiştirildi", EN: "Branding settings changed"},
	"marka.alanadi.ekle":               {TR: "Özel panel adresi eklendi", EN: "Custom panel address added"},
	"marka.alanadi.dogrula":            {TR: "Özel panel adresi doğrulandı", EN: "Custom panel address verified"},
	"marka.alanadi.sil":                {TR: "Özel panel adresi kaldırıldı", EN: "Custom panel address removed"},
	"auth.login.marka_host":            {TR: "Yanlış panel adresinden giriş reddi", EN: "Login rejected: wrong panel address"},
	"api_anahtar.olustur":              {TR: "API anahtarı oluşturuldu", EN: "API key created"},
	"api_anahtar.yenile":               {TR: "API anahtarı yenilendi", EN: "API key rotated"},
	"api_anahtar.iptal":                {TR: "API anahtarı iptal edildi", EN: "API key revoked"},
	"api_anahtar.sil":                  {TR: "API anahtarı silindi", EN: "API key deleted"},
	"api_anahtar.toplu_iptal":          {TR: "Bayinin API anahtarları iptal edildi", EN: "Reseller API keys revoked"},
	"api_anahtar.red":                  {TR: "API anahtarı reddedildi", EN: "API key rejected"},
	"api_anahtar.yeniden_dogrulama":    {TR: "API anahtarı için kimlik doğrulandı", EN: "Re-authenticated for API key"},
	"hosting.ozel_plan":                {TR: "Özel plan uygulandı", EN: "Custom plan applied"},
	"hosting.toplu_durum":              {TR: "Toplu durum değişikliği", EN: "Bulk status change"},
	"hosting.sahip_devir":              {TR: "Hosting sahibi değişti", EN: "Hosting owner changed"},

	// Veritabanı
	"db.olustur":           {TR: "Veritabanı oluşturuldu", EN: "Database created"},
	"db.sil":               {TR: "Veritabanı silindi", EN: "Database deleted"},
	"db.parola":            {TR: "Veritabanı parolası değiştirildi", EN: "Database password changed"},
	"db.kullanici-olustur": {TR: "Veritabanı kullanıcısı oluşturuldu", EN: "Database user created"},
	"db.optimize":          {TR: "Veritabanı iyileştirildi", EN: "Database optimized"},

	// Bayi
	"bayi.olustur":    {TR: "Bayi oluşturuldu", EN: "Reseller created"},
	"bayi.guncelle":   {TR: "Bayi güncellendi", EN: "Reseller updated"},
	"bayi.sil":        {TR: "Bayi silindi", EN: "Reseller deleted"},
	"bayi.askiya_al":  {TR: "Bayi askıya alındı", EN: "Reseller suspended"},
	"bayi.askidan_al": {TR: "Bayi askıdan alındı", EN: "Reseller unsuspended"},
	"bayi.parola":     {TR: "Bayi parolası değiştirildi", EN: "Reseller password changed"},

	// Plan
	"plan.olustur":  {TR: "Plan oluşturuldu", EN: "Plan created"},
	"plan.guncelle": {TR: "Plan güncellendi", EN: "Plan updated"},

	// Yedek
	"yedek.geriyukle":           {TR: "Yedek geri yüklendi", EN: "Backup restored"},
	"yedek.sil.lftp-yok":        {TR: "Yedek silindi, uzak kopya kaldı", EN: "Backup deleted, remote copy kept", gereksizNot: []string{"uzak kopya kaldı"}},
	"yedek.sil.uzak-yoksayildi": {TR: "Yedek silindi, uzak kopya bırakıldı", EN: "Backup deleted, remote copy kept"},

	// Taşıma
	"tasima_baslat":     {TR: "Taşıma başlatıldı", EN: "Migration started"},
	"tasima_iptal":      {TR: "Taşıma iptal edildi", EN: "Migration cancelled"},
	"tasima_oturum_sil": {TR: "Taşıma oturumu silindi", EN: "Migration session deleted"},

	// Erişim politikası
	"erisim.ayar":               {TR: "Erişim ayarı değişti", EN: "Access settings changed"},
	"erisim.kural.ekle":         {TR: "Erişim kuralı eklendi", EN: "Access rule added"},
	"erisim.kural.sil":          {TR: "Erişim kuralı silindi", EN: "Access rule removed"},
	"altalan.erisim.ayar":       {TR: "Alt alan erişim ayarı değişti", EN: "Subdomain access settings changed"},
	"altalan.erisim.kural.ekle": {TR: "Alt alan erişim kuralı eklendi", EN: "Subdomain access rule added"},
	"altalan.erisim.kural.sil":  {TR: "Alt alan erişim kuralı silindi", EN: "Subdomain access rule removed"},
	"guvenilir_vekil.ekle":      {TR: "Güvenilir vekil eklendi", EN: "Trusted proxy added"},
	"guvenilir_vekil.sil":       {TR: "Güvenilir vekil silindi", EN: "Trusted proxy removed"},

	// Panel portları
	"kapi.rol_port":   {TR: "Panel portu değiştirildi", EN: "Panel port changed"},
	"kapi.eski_uzat":  {TR: "Eski panel portunun süresi uzatıldı", EN: "Old panel port extended"},
	"kapi.eski_kapat": {TR: "Eski panel portu kapatıldı", EN: "Old panel port closed"},

	// Güvenlik / diğer
	"websec.tara":              {TR: "Güvenlik taraması başlatıldı", EN: "Security scan started"},
	"guvenlik.izolasyon_kaybi": {TR: "İzolasyon uyarısı", EN: "Isolation warning"},
	"winajan.ekle":             {TR: "Windows ajanı eklendi", EN: "Windows agent added"},
	"winajan.sil":              {TR: "Windows ajanı silindi", EN: "Windows agent removed", gereksizNot: []string{"windows ajanı silindi"}},
}

// eylemEtiketi: katalogdaki etiket; bilinmeyen kod için okunur bir geri dönüş
// ("musteri.adina_cikis" → "Musteri adina cikis") — ham noktalı kod gösterilmez.
func eylemEtiketi(kod string) EylemEtiket {
	if e, ok := eylemKatalogu[kod]; ok {
		return e
	}
	o := okunurKod(kod)
	return EylemEtiket{TR: o, EN: o}
}

// okunurKod: ayraçları (. _ -) boşluğa çevirir, ilk harfi büyütür.
func okunurKod(kod string) string {
	s := strings.Join(strings.FieldsFunc(kod, func(r rune) bool { return r == '.' || r == '_' || r == '-' }), " ")
	if s == "" {
		return kod
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// notSade: etiketi tekrarlayan açıklamayı düşürür (eski kayıtlar dahil — okuma anında).
func notSade(kod, not string) string {
	not = strings.TrimSpace(not)
	if not == "" {
		return ""
	}
	e := eylemEtiketi(kod)
	if strings.EqualFold(not, e.TR) {
		return ""
	}
	for _, g := range e.gereksizNot {
		if on, ok := strings.CutSuffix(g, "*"); ok {
			if strings.HasPrefix(not, on) {
				return ""
			}
		} else if strings.EqualFold(not, g) {
			return ""
		}
	}
	return not
}

// teknikEylemler: varsayılan listede (eylem süzgeci boşken) başarılı kaydı gizlenen kodlar.
func teknikEylemler() []string {
	var out []string
	for k, e := range eylemKatalogu {
		if e.teknik {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
