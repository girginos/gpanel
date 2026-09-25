package portyonetim

// Rol kapıları (ADR-GP-001) — Port Yönetimi'nden yönetim:
//   GET  /admin/portlar                  — Durum.Kapi (rol portları, eski kapı, zorlama)
//   POST /admin/portlar/kapi/rol         — {rol, yeni_port (0 = rastgele)}
//   POST /admin/portlar/kapi/uzat        — {gun}
//   POST /admin/portlar/kapi/eski-kapat  — {zorla}
//
// Ayar kapi.json'dadır (tek doğruluk kaynağı); nginx'i provisioner uygular (KapiUygula).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"girginospanel/internal/guvenlikduvari"
	"girginospanel/internal/kapi"
)

// KapiUygula: kapı ayarı değişince nginx'i hemen uyumlar ve zinciri ölçer. main bağlar
// (provisioner.KapiSimdiUygula) — provisioner bu paketi kullandığı için doğrudan
// çağrılamaz (import döngüsü).
var KapiUygula func(ctx context.Context) error

// GenelHost: sunucunun genel adresi (main bağlar). Arayüz, tarayıcı SSH tünelinden
// (127.0.0.1) bağlıyken bayi/müşteri adreslerini bununla kurar. nil → "".
var GenelHost func() string

// Dikişler (testler).
var (
	kapiAyarYolu     = kapi.AyarYolu
	kapiSistemPortu  = SistemdePortlar
	kapiFwKapaliPort = guvenlikduvari.KapatilanPortlar
	kapiSELinux      = selinuxDenetci
	kapiFtpPasif     = ftpPasifAraligi
	kapiMasaustu     = kapi.MasaustuModu
	kapiPanelConf    = func() string {
		b, _ := os.ReadFile(VhostYolu)
		return string(b)
	}
)

// ftpConfYolu: pure-ftpd yapılandırması (girginospanel-ftp-setup yazar). Dikiş (testler).
var ftpConfYolu = "/etc/pure-ftpd/pure-ftpd.conf"

// ftpPasifAraligi: pure-ftpd'nin pasif veri portları. Bu portlar yalnız aktarım sırasında
// dinlendiği için `ss` anlık görüntüsünde görünmez; kapı portu bu aralıktan seçilirse nginx
// ile pure-ftpd aynı portu tutmaya çalışır (nginx restart'ında EADDRINUSE → nginx açılmaz).
// Yapılandırma yoksa ftp-setup'ın varsayılanı: FTP kapı portundan SONRA da kurulabilir.
func ftpPasifAraligi() (int, int) {
	if b, err := os.ReadFile(ftpConfYolu); err == nil {
		for _, ln := range strings.Split(string(b), "\n") {
			if f := strings.Fields(ln); len(f) == 3 && f[0] == "PassivePortRange" {
				if alt, ust, ok := portAraligi(f[1] + "-" + f[2]); ok {
					return alt, ust
				}
			}
		}
	}
	return 30000, 30100
}

var errKapiModu = errors.New("kapı modunda dış port tek başına değiştirilemez — rol portlarını Kapılar bölümünden yönetin")

// kapiModu: kapı ayarı VAR mı (bozuk olsa bile — o durumda da tek-port işlemleri güvenli değil).
func kapiModu() bool {
	_, err := os.Stat(kapiAyarYolu)
	return err == nil
}

// KapiDurum: arayüz için kapı özeti. Yalnız admin görür (GET /portlar AdminOnly).
type KapiDurum struct {
	Admin           int   `json:"admin"`
	Bayi            int   `json:"bayi"`
	Musteri         int   `json:"musteri"`
	EskiPortlar     []int `json:"eski_portlar"`
	EskiBitis       int64 `json:"eski_bitis"`
	EskiAcik        bool  `json:"eski_acik"`
	AdminDogrulandi int64 `json:"admin_dogrulandi"`
	// IlkGiris: bayi/müşteri kapısından ilk dış giriş (rol → unix sn; yoksa 0). Eski kapı
	// kapanmadan önce hangi rolün yeni kapısının gerçekten kullanıldığını gösterir.
	IlkGiris map[string]int64 `json:"ilk_giris"`
	// Masaustu: erisim masaüstü modu — yönetici kapısı yalnız SSH tünelinden.
	Masaustu bool `json:"masaustu"`
	// GenelHost: sunucunun genel adresi (bkz. GenelHost); boş olabilir.
	GenelHost string `json:"genel_host"`
	Zorlama   bool   `json:"zorlama"`
	Hata      string `json:"hata,omitempty"`
}

// KapiDurumGetir: kapı modu kapalıysa nil. Ayar okunamıyorsa yalnız Hata dolu döner
// (arayüz "kapı modu bozuk" gösterir; tek-port işlemleri yine kapalıdır).
func KapiDurumGetir() *KapiDurum {
	a, err := kapi.Oku(kapiAyarYolu)
	if errors.Is(err, kapi.ErrYok) {
		return nil
	}
	if err != nil {
		return &KapiDurum{Hata: err.Error(), EskiPortlar: []int{}, IlkGiris: map[string]int64{}}
	}
	simdi := time.Now().Unix()
	eski := a.EskiPortlar
	if eski == nil {
		eski = []int{}
	}
	ilk := map[string]int64{kapi.Bayi: a.IlkGiris[kapi.Bayi], kapi.Musteri: a.IlkGiris[kapi.Musteri]}
	return &KapiDurum{
		Admin: a.Admin, Bayi: a.Bayi, Musteri: a.Musteri,
		EskiPortlar: eski, EskiBitis: a.EskiBitis, EskiAcik: a.EskiAcik(simdi),
		AdminDogrulandi: a.AdminDogrulandi,
		IlkGiris:        ilk,
		Masaustu:        kapiMasaustu(),
		GenelHost:       genelHost(),
		Zorlama:         kapi.ZorlamaAktif(),
	}
}

func genelHost() string {
	if GenelHost == nil {
		return ""
	}
	return GenelHost()
}

// KapiPortDenetleyici: bir portun kapı portu OLAMAMA nedenini döner ("" = uygun).
// `ss`, firewall ve SELinux anlık görüntüsü BİR KEZ alınır (rastgele seçimde her aday
// için yeniden çalışmasın). kendi: panelin zaten dinlediği ve devralınabilecek portlar
// (açık eski kapı, yalnız-loopback tünel portu) — "meşgul" sayılmaz.
func KapiPortDenetleyici(kendi ...int) func(int) string {
	mesgul := kapiSistemPortu()
	fw := kapiFwKapaliPort()
	selinux := kapiSELinux()
	ftpAlt, ftpUst := kapiFtpPasif()
	for _, p := range kendi {
		delete(mesgul, p)
	}
	return func(p int) string {
		if yasak, aciklama := PortYasakMi(p); yasak {
			return fmt.Sprintf("port %d kullanılamaz: %s", p, aciklama)
		}
		if p >= ftpAlt && p <= ftpUst {
			return fmt.Sprintf("port %d FTP pasif veri aralığında (%d-%d) — pure-ftpd aktarım sırasında kullanır", p, ftpAlt, ftpUst)
		}
		if mesgul[p] {
			return fmt.Sprintf("port %d başka bir servis tarafından kullanılıyor", p)
		}
		if fw[p] {
			return fmt.Sprintf("port %d güvenlik duvarında kapatılmış/kısıtlanmış — önce o kuralı kaldırın", p)
		}
		if tur := selinux(p); tur != "" {
			return fmt.Sprintf("port %d SELinux'ta başka bir servise ayrılmış (%s) — nginx bu portu kullanamaz", p, tur)
		}
		return ""
	}
}

// KapiRolSonuc: rol portu değişikliğinin sonucu.
type KapiRolSonuc struct {
	Rol      string `json:"rol"`
	EskiPort int    `json:"eski_port"`
	YeniPort int    `json:"yeni_port"`
	Zorlama  bool   `json:"zorlama"`
	// Uygulandi: değişiklik nginx'e uygulandı mı. Uyari doluyken ayrım önemlidir:
	// true → port değişti ama kapı zinciri doğrulanamadı (bayi/müşteri için önceki port
	// kapanmış olabilir); false → yalnız kaydedildi, nginx eski hâlinde.
	Uygulandi bool `json:"uygulandi"`
	// Uyari: ayar kaydedildi ama nginx'e uygulanamadı/ölçülemedi (bakım döngüsü yeniden
	// dener; bu sırada kapı kuralları devre dışıdır, kimse kilitlenmez).
	Uyari string `json:"uyari,omitempty"`
}

// KapiRolPortuAta: portu denetler (yeni == 0 → kapı aralığından rastgele seçer) ve
// ayara yazar. nginx'e DOKUNMAZ: uygulamayı çağıran yapar (API: KapiUygula; CLI: panel
// yeniden başlatma → Heal). CLI de bunu kullanır — denetim tek yerde.
func KapiRolPortuAta(rol string, yeni int) (eski, atanan int, err error) {
	a, err := kapi.Oku(kapiAyarYolu)
	if err != nil {
		return 0, 0, err
	}
	if a.RolPortu(rol) == 0 {
		return 0, 0, fmt.Errorf("bilinmeyen rol: %q (admin|bayi|musteri)", rol)
	}
	for _, r := range kapi.Roller {
		if yeni != 0 && a.RolPortu(r) == yeni {
			if r == rol {
				return 0, 0, kapi.ErrAyniPort
			}
			return 0, 0, fmt.Errorf("port %d zaten %s kapısı", yeni, r)
		}
	}
	// Panelin kendi portları (nginx onları dinlediği için `ss` "meşgul" der): açık eski kapı
	// elle devralınabilir. DIŞARIDA DİNLENMEYEN port (yalnız loopback / erisim-yorumlu izi
	// var — SSH tüneli yolu) ise YALNIZ masaüstü modunda yönetici kapısı olabilir: Heal izi
	// olan porta genel listen eklemez — bayi/müşteri (ya da web modunda yönetici) kapısı
	// oraya taşınsa dışarıdan erişilemez, eski portu ise kaldırılırdı.
	izli, disKapali := panelPortIzleri(kapiPanelConf())
	kendi := append([]int{}, a.EskiPortlar...)
	tunelIzinli := rol == kapi.Admin && kapiMasaustu()
	if tunelIzinli {
		for p := range disKapali {
			kendi = append(kendi, p)
		}
	}
	sorun := KapiPortDenetleyici(kendi...)
	if yeni == 0 {
		// Rastgele seçim izi olan HİÇBİR porta düşmez (kapi.GecisAyari ile aynı kural).
		haric := map[int]bool{a.Admin: true, a.Bayi: true, a.Musteri: true}
		for _, p := range kendi {
			haric[p] = true
		}
		for p := range izli {
			haric[p] = true
		}
		if yeni, err = kapi.RastgelePortSec(func(p int) bool { return sorun(p) == "" }, haric); err != nil {
			return 0, 0, err
		}
	} else if !tunelIzinli && disKapali[yeni] {
		return 0, 0, fmt.Errorf("port %d dışarıda dinlenmiyor (yalnız 127.0.0.1 / SSH tüneli yolu) — %s kapısı dışarıdan erişilemezdi", yeni, rol)
	} else if s := sorun(yeni); s != "" {
		return 0, 0, errors.New(s)
	}
	eski, err = kapi.RolPortuDegistir(kapiAyarYolu, rol, yeni, time.Now().Unix())
	return eski, yeni, err
}

// panelPortIzleri: panel bloğundaki portlar — izli: HERHANGİ bir listen izi olan (genel,
// loopback ya da erisim'in yoruma aldığı); disKapali: izi olup GENEL (etkin, loopback
// olmayan) satırı OLMAYAN, yani dışarıdan dinlenmeyen.
//
// 🔴 KAPI HARİTASINDAN BAĞIMSIZ: kapi.YerelPortlar haritadaki portları dışlar; açık eski
// kapı portu da ("eski") yalnız loopback'te dinleniyor olabilir (masaüstü modunda
// 127.0.0.1'e çekilmiş önceki yönetici portu). Harita üzerinden bakmak bu portu "genel"
// sanıp bayi/müşteri kapısının oraya taşınmasına izin veriyordu (KP-1).
func panelPortIzleri(conf string) (izli, disKapali map[int]bool) {
	izli, genel := map[int]bool{}, map[int]bool{}
	for _, d := range kapi.Dinlemeler(strings.Split(conf, "\n")) {
		if d.Port == 0 {
			continue
		}
		izli[d.Port] = true
		if d.Genel() {
			genel[d.Port] = true
		}
	}
	disKapali = map[int]bool{}
	for p := range izli {
		if !genel[p] {
			disKapali[p] = true
		}
	}
	return izli, disKapali
}

// KapiRolDegistir: bir rolün kapı portunu değiştirir ve hemen uygular. yeni == 0 →
// kapı aralığından rastgele.
func KapiRolDegistir(ctx context.Context, db *sql.DB, rol string, yeni int, aktor *int64) (*KapiRolSonuc, error) {
	if !TipKilitle("kapi") {
		return nil, errDegisiklikSuruyor
	}
	defer TipSerbest("kapi")
	eski, yeni, err := KapiRolPortuAta(rol, yeni)
	if err != nil {
		return nil, err
	}
	sonuc := &KapiRolSonuc{Rol: rol, EskiPort: eski, YeniPort: yeni}
	uygErr := kapiUygula(ctx)
	if uygErr != nil {
		sonuc.Uyari = uygErr.Error()
	}
	sonuc.Uygulandi = uygulandiMi(uygErr)
	sonuc.Zorlama = kapi.ZorlamaAktif()
	gecmisSatirYaz(db, "kapi-"+rol, eski, yeni, uygErr == nil, false, sonuc.Uyari, aktor)
	return sonuc, nil
}

// uygulandiMi: uygulama hatası değişikliğin nginx'e uygulandığını mı söylüyor?
func uygulandiMi(err error) bool {
	if err == nil {
		return true
	}
	var uh *kapi.UygulamaHatasi
	return errors.As(err, &uh) && uh.Uygulandi
}

func kapiUygula(ctx context.Context) error {
	if KapiUygula == nil {
		return errors.New("kapı uygulayıcısı bağlı değil — değişiklik panel yeniden başlayınca uygulanır")
	}
	return KapiUygula(ctx)
}

// ── HTTP ─────────────────────────────────────────────────────────────────

type kapiRolGovde struct {
	Rol      string `json:"rol"`
	YeniPort int    `json:"yeni_port"`
}

func (h *Handler) KapiRol(w http.ResponseWriter, r *http.Request) {
	var g kapiRolGovde
	if !govdeCoz(w, r, &g) {
		return
	}
	s, err := KapiRolDegistir(r.Context(), h.DB, g.Rol, g.YeniPort, h.aktorUID(r))
	if err != nil {
		h.denetle(r, "kapi.rol_port", g.Rol, err.Error(), false)
		kapiHataYaz(w, err)
		return
	}
	h.denetle(r, "kapi.rol_port", s.Rol, fmt.Sprintf("%d → %d", s.EskiPort, s.YeniPort), s.Uyari == "")
	jsonYaz(w, 200, s)
}

type kapiUzatGovde struct {
	Gun int `json:"gun"`
}

func (h *Handler) KapiUzat(w http.ResponseWriter, r *http.Request) {
	var g kapiUzatGovde
	if !govdeCoz(w, r, &g) {
		return
	}
	if !TipKilitle("kapi") {
		kapiHataYaz(w, errDegisiklikSuruyor)
		return
	}
	defer TipSerbest("kapi")
	if err := kapi.EskiUzat(kapiAyarYolu, g.Gun, time.Now().Unix()); err != nil {
		kapiHataYaz(w, err)
		return
	}
	h.denetle(r, "kapi.eski_uzat", "eski", fmt.Sprintf("%d gün", g.Gun), true)
	kapiUygulaYanit(w, r)
}

type kapiEskiKapatGovde struct {
	Zorla bool `json:"zorla"`
}

func (h *Handler) KapiEskiKapat(w http.ResponseWriter, r *http.Request) {
	var g kapiEskiKapatGovde
	if !govdeCoz(w, r, &g) {
		return
	}
	if !TipKilitle("kapi") {
		kapiHataYaz(w, errDegisiklikSuruyor)
		return
	}
	defer TipSerbest("kapi")
	if err := kapi.EskiKapat(kapiAyarYolu, g.Zorla, time.Now().Unix()); err != nil {
		kapiHataYaz(w, err)
		return
	}
	detay := ""
	if g.Zorla {
		detay = "zorla (admin yeni kapıyı doğrulamadan)"
	}
	h.denetle(r, "kapi.eski_kapat", "eski", detay, true)
	kapiUygulaYanit(w, r)
}

// kapiUygulaYanit: ayar yazıldıktan sonra uygular; uygulama hatası isteği BOZMAZ
// (ayar kaydedildi, bakım döngüsü yeniden dener) ama uyarı olarak döner.
// Yanıt: {ok, uyari?, uygulandi, kapi}. uygulandi HER ZAMAN yazılır (KapiRolSonuc ile
// aynı anlam): uyari doluyken true → değişiklik nginx'e uygulandı ama kapı zinciri
// doğrulanamadı (eski-kapat'ta eski adres gerçekten kapanmış olabilir); false → yalnız
// kaydedildi, nginx eski hâlinde.
func kapiUygulaYanit(w http.ResponseWriter, r *http.Request) {
	yanit := map[string]any{"ok": true}
	err := kapiUygula(r.Context())
	if err != nil {
		yanit["uyari"] = err.Error()
	}
	yanit["uygulandi"] = uygulandiMi(err)
	yanit["kapi"] = KapiDurumGetir()
	jsonYaz(w, 200, yanit)
}

func kapiHataYaz(w http.ResponseWriter, err error) {
	var adh *kapi.AdminDogrulanmadiHatasi
	switch {
	case errors.Is(err, errDegisiklikSuruyor):
		hataYaz(w, http.StatusConflict, err.Error())
	case errors.Is(err, kapi.ErrYok), errors.Is(err, kapi.ErrEskiYok), errors.Is(err, kapi.ErrEskiDevralma):
		hataYaz(w, http.StatusConflict, err.Error())
	case errors.As(err, &adh):
		hataYaz(w, http.StatusConflict, err.Error())
	default:
		hataYaz(w, http.StatusBadRequest, err.Error())
	}
}

// govdeCoz: katı JSON çözümü (bilinmeyen alan → 400).
func govdeCoz(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		hataYaz(w, 400, "geçersiz gövde: "+err.Error())
		return false
	}
	return true
}
