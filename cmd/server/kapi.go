package main

// Panel kapıları (ADR-GP-001) — `girginospanel-server kapi <komut>` CLI'ı ve
// "yeni adresin" ucu.
//
// 🔴 CLI config.Load()'dan ÖNCE çalışır: kurulum/güncelleme betikleri onu düz
// kabuktan çağırır; systemd'nin EnvironmentFile'ı (PANEL_JWT_SECRET vb.) orada
// yüklü değildir. Kapı işlemleri yalnız dosya düzeyindedir (kapi.json +
// _panel.conf) — DB ve sır gerekmez. nginx'e dokunmaz: uygulama, panel açılışındaki
// Heal'in işidir (tek uygulayıcı, tek doğrulama yolu). Yazılan kapı portları SELinux'ta
// burada etiketlenir (semanage saniyeler sürer; panel açılışı beklemesin).

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/httpx"
	"girginospanel/internal/kapi"
	"girginospanel/internal/middleware"
	"girginospanel/internal/panelhost"
	"girginospanel/internal/portyonetim"
	"girginospanel/internal/provisioner"
)

const kapiKullanim = `Usage: girginospanel-server kapi <command>
  kur              fresh install: assign the role ports (existing settings are KEPT)
  gecis [days]     transition an existing install: today's port(s) become the old gate
  durum [--json]   show the gate settings
  port <role>      port number only (admin|bayi|musteri) — for scripts
  rol-port <role> [port]  change a role's gate port (random if no port is given); if the
                   admin port changes, the previous port stays open until someone
                   signs in on the new one
  uzat <days>      extend how long the old gate stays open
  eski-kapat       close the old gate now (the admin must have signed in on the new gate)
  eski-kapat --zorla   close it without the admin check (the lock-out risk is YOURS)
  selinux          label the gate ports http_port_t in SELinux (kur/gecis/rol-port do
                   this themselves; for servers switched over earlier, and manual use)
Exit: 0 ok · 1 error · 2 usage · 3 gate mode not configured`

// Dikişler (testler): CLI gerçek kapi.json'a, _panel.conf'a ve systemctl'e dokunmasın.
var (
	kapiCLIAyarYolu = kapi.AyarYolu
	// kapiPanelConf: geçişte "bugünkü genel portlar" buradan okunur.
	kapiPanelConf    = "/etc/nginx/conf.d/_panel.conf"
	kapiPortUygunluk = kapiPortUygun
	kapiRolPortuAta  = portyonetim.KapiRolPortuAta
	// kapiSELinuxHazirla: yazılan kapı portlarını panel başlamadan etiketler (0/1/3).
	kapiSELinuxHazirla = provisioner.KapiSELinuxHazirla
	// panelYenidenBaslat: değişikliği hemen uygulamak için (Heal açılışta koşar).
	panelYenidenBaslat = func() {
		if exec.Command("systemctl", "is-active", "--quiet", "girginospanel").Run() != nil {
			fmt.Println("panel is not running — the change is applied at the next start")
			return
		}
		if out, err := exec.Command("systemctl", "restart", "girginospanel").CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "could not restart the panel: %v %s\n", err, out)
			return
		}
		fmt.Println("panel restarted — change applied")
	}
)

func kapiCLI(args []string) int {
	komut := ""
	if len(args) > 0 {
		komut = args[0]
	}
	switch komut {
	case "kur":
		return kapiKur()
	case "gecis":
		gun := kapi.VarsayilanGecisGun
		if len(args) > 1 {
			n, err := strconv.Atoi(args[1])
			if err != nil || n < 1 {
				fmt.Fprintln(os.Stderr, "days must be a positive integer")
				return 2
			}
			gun = n
		}
		return kapiGecis(gun)
	case "durum":
		return kapiDurum(len(args) > 1 && args[1] == "--json")
	case "port":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, kapiKullanim)
			return 2
		}
		return kapiPort(args[1])
	case "rol-port":
		if len(args) < 2 || !kapiRolMu(args[1]) {
			fmt.Fprintln(os.Stderr, kapiKullanim)
			return 2
		}
		port := 0
		if len(args) > 2 {
			n, err := strconv.Atoi(args[2])
			if err != nil || n < 1 {
				fmt.Fprintln(os.Stderr, "port must be a positive integer")
				return 2
			}
			port = n
		}
		return kapiRolPort(args[1], port)
	case "uzat":
		n := 0
		if len(args) > 1 {
			n, _ = strconv.Atoi(args[1])
		}
		if n < 1 {
			fmt.Fprintln(os.Stderr, "uzat <days>: days must be a positive integer")
			return 2
		}
		return kapiUzat(n)
	case "eski-kapat":
		return kapiEskiKapat(len(args) > 1 && args[1] == "--zorla")
	case "selinux":
		return kapiSELinux()
	}
	fmt.Fprintln(os.Stderr, kapiKullanim)
	return 2
}

// kapiPortUygun: yasaklı değil, sistemde dinlenmiyor, firewall'da kapalı/kısıtlı değil
// (anlık görüntüler bir kez alınır).
func kapiPortUygun() func(int) bool {
	sorun := portyonetim.KapiPortDenetleyici()
	return func(p int) bool { return sorun(p) == "" }
}

// kapiHata: CLI okuyucusuna İngilizce hata metni. internal/kapi hataları panel API'siyle
// paylaşılır (arayüzde Türkçe gösterilir) → kütüphane metnine dokunmadan bilinen sentinel'ler
// burada çevrilir; bilinmeyenler olduğu gibi basılır.
func kapiHata(err error) string {
	var adh *kapi.AdminDogrulanmadiHatasi
	switch {
	case errors.Is(err, kapi.ErrYok):
		return "gate mode is not configured"
	case errors.Is(err, kapi.ErrEskiYok):
		return "there is no open old gate"
	case errors.Is(err, kapi.ErrDisariyaKapali):
		return "the panel is closed to the outside (erisim desktop mode) — gate transition skipped"
	case errors.Is(err, kapi.ErrAdreseBagli):
		return "the panel is bound to a specific address (internal network/VPN IP) — gate transition skipped: opening the role gates on all interfaces would remove this restriction"
	case errors.Is(err, kapi.ErrAyniPort):
		return "the role is already on this port — no change"
	case errors.Is(err, kapi.ErrEskiDevralma):
		return "an open old gate port cannot be handed to another role until the admin has signed in once on the NEW admin gate: handing it over closes the transition gate at that address, and if the provider firewall blocks the new admin port the admin is locked out. Sign in on the new admin address first"
	case errors.As(err, &adh):
		return fmt.Sprintf("the admin has not signed in on the NEW admin gate (:%d) yet. If the provider firewall blocks that port, closing the old gate locks everyone out. Sign in on the new address first, or take the risk and force the close", adh.AdminPort)
	}
	return err.Error()
}

func kapiRolMu(rol string) bool {
	for _, r := range kapi.Roller {
		if r == rol {
			return true
		}
	}
	return false
}

// kapiMevcut: ayar varsa (true, 0); yoksa (false, 0); okunamıyorsa (false, 1).
func kapiMevcut() (*kapi.Ayar, bool, int) {
	a, err := kapi.Oku(kapiCLIAyarYolu)
	if err == nil {
		return a, true, 0
	}
	if errors.Is(err, kapi.ErrYok) {
		return nil, false, 0
	}
	fmt.Fprintf(os.Stderr, "could not read kapi.json: %v\n", err)
	return nil, false, 1
}

func kapiKur() int {
	a, mevcut, kod := kapiMevcut()
	if kod != 0 {
		return kod
	}
	if mevcut {
		fmt.Println("gate settings already exist — ports are KEPT (reinstall):")
		kapiYazdir(a)
		return 0
	}
	a, err := kapi.KurulumAyari(kapiPortUygunluk(), time.Now().Unix())
	if err == nil {
		err = kapi.Yaz(kapiCLIAyarYolu, a)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not assign the gate ports: %v\n", kapiHata(err))
		return 1
	}
	fmt.Println("gate ports assigned (applied when the panel starts):")
	kapiYazdir(a)
	kapiEtiketle()
	return 0
}

func kapiGecis(gun int) int {
	a, mevcut, kod := kapiMevcut()
	if kod != 0 {
		return kod
	}
	if mevcut {
		fmt.Println("gate transition already done:")
		kapiYazdir(a)
		return 0
	}
	conf, err := os.ReadFile(kapiPanelConf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not read the panel vhost (%s): %v\n", kapiPanelConf, err)
		return 1
	}
	a, err = kapi.GecisAyari(string(conf), kapiPortUygunluk(), time.Now().Unix(), gun)
	if errors.Is(err, kapi.ErrDisariyaKapali) {
		fmt.Println(kapiHata(err) + " — once the panel is back in web mode, run `girginospanel-server kapi gecis` again")
		return 0 // hata değil: operatörün bilinçli seçimi korundu
	}
	if errors.Is(err, kapi.ErrAdreseBagli) {
		fmt.Println(kapiHata(err))
		return 0 // hata değil: iç ağ/VPN adresine bağlı panel olduğu gibi kalır
	}
	if err == nil {
		err = kapi.Yaz(kapiCLIAyarYolu, a)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not complete the gate transition: %v\n", kapiHata(err))
		return 1
	}
	fmt.Println("gate transition prepared (applied when the panel starts):")
	kapiYazdir(a)
	kapiEtiketle()
	return 0
}

func kapiDurum(jsonCikti bool) int {
	a, mevcut, kod := kapiMevcut()
	if kod != 0 {
		return kod
	}
	if !mevcut {
		if jsonCikti {
			fmt.Println("{}")
		} else {
			fmt.Println("gate mode is not configured (single shared panel port)")
		}
		return 3
	}
	if jsonCikti {
		e := json.NewEncoder(os.Stdout)
		e.SetIndent("", "  ")
		if err := e.Encode(a); err != nil {
			return 1
		}
		return 0
	}
	kapiYazdir(a)
	return 0
}

func kapiPort(rol string) int {
	a, mevcut, kod := kapiMevcut()
	if kod != 0 {
		return kod
	}
	if !mevcut {
		return 3
	}
	p := a.RolPortu(rol)
	if p == 0 {
		fmt.Fprintf(os.Stderr, "unknown role: %q (admin|bayi|musteri)\n", rol)
		return 2
	}
	fmt.Println(p)
	return 0
}

func kapiRolPort(rol string, yeni int) int {
	eski, atanan, err := kapiRolPortuAta(rol, yeni)
	switch {
	case errors.Is(err, kapi.ErrYok):
		fmt.Fprintln(os.Stderr, "gate mode is not configured")
		return 3
	case errors.Is(err, kapi.ErrAyniPort):
		fmt.Println(kapiHata(err))
		return 0
	case err != nil:
		fmt.Fprintf(os.Stderr, "could not change the role port: %v\n", kapiHata(err))
		return 1
	}
	fmt.Printf("%s gate: %d → %d\n", rol, eski, atanan)
	if rol == kapi.Admin {
		fmt.Printf("previous admin port :%d stays OPEN until someone signs in on the new admin gate\n", eski)
	}
	kapiEtiketle()
	panelYenidenBaslat()
	return 0
}

// kapiSELinux: `kapi selinux` — önceden kapı moduna geçmiş sunucular, yeniden başlatma
// sonrası (/run önbelleği silinir) ve elle kullanım için.
func kapiSELinux() int {
	switch kod := kapiSELinuxHazirla(); kod {
	case 0:
		fmt.Println("gate ports are ready in SELinux (or SELinux is off)")
		return 0
	case 3:
		fmt.Fprintln(os.Stderr, "gate mode is not configured")
		return 3
	default:
		kapiEtiketUyarisi()
		return 1
	}
}

// kapiEtiketle: kur/gecis/rol-port yazdıktan sonra yeni portları etiketler. Başarısızlık
// komutu düşürmez: ayar yazıldı, panel etiketsiz porta listen YAZMAZ (eski davranış,
// kilitlenme yok) — operatör uyarılır.
func kapiEtiketle() {
	if kapiSELinuxHazirla() == 1 {
		kapiEtiketUyarisi()
	}
}

func kapiEtiketUyarisi() {
	fmt.Fprintln(os.Stderr, "🔴 SELinux Enforcing: the gate ports could not be labeled http_port_t — the panel will NOT "+
		"apply the gate layout (the old behavior continues). Fix the port named in the log, then run `girginospanel-server kapi selinux`.")
}

func kapiUzat(gun int) int {
	if err := kapi.EskiUzat(kapiCLIAyarYolu, gun, time.Now().Unix()); err != nil {
		fmt.Fprintf(os.Stderr, "could not extend: %v\n", kapiHata(err))
		return 1
	}
	fmt.Printf("old gate lifetime extended by %d days\n", gun)
	panelYenidenBaslat()
	return 0
}

func kapiEskiKapat(zorla bool) int {
	if err := kapi.EskiKapat(kapiCLIAyarYolu, zorla, time.Now().Unix()); err != nil {
		fmt.Fprintf(os.Stderr, "could not close the old gate: %v\n", kapiHata(err))
		var adh *kapi.AdminDogrulanmadiHatasi
		if errors.As(err, &adh) {
			fmt.Fprintln(os.Stderr, "(to force it: girginospanel-server kapi eski-kapat --zorla)")
		}
		return 1
	}
	fmt.Println("closing the old gate")
	panelYenidenBaslat()
	return 0
}

func kapiYazdir(a *kapi.Ayar) {
	fmt.Printf("  admin    : %d\n  reseller : %d\n  customer : %d\n", a.Admin, a.Bayi, a.Musteri)
	if len(a.EskiPortlar) > 0 {
		fmt.Printf("  old      : %v — open until at least %s, and until the admin signs in on the new gate\n",
			a.EskiPortlar, time.Unix(a.EskiBitis, 0).Format("2006-01-02"))
	}
	if a.AdminDogrulandi > 0 {
		fmt.Printf("  admin signed in on the new gate: %s\n", time.Unix(a.AdminDogrulandi, 0).Format("2006-01-02 15:04"))
	}
}

// kapiAdres: GET /api/v1/kapi/adres — eski kapıdan giriş yapmış kullanıcıya KENDİ
// rolünün yeni adresini söyler (arayüz banner'ı). Yalnız kimliği doğrulanmış
// kullanıcıya ve yalnız eski kapıda: anonim ziyaretçiye yeni port SIZDIRILMAZ
// (yönlendirme yapılsaydı tarayıcı botlar yeni portu eski porttan öğrenirdi).
func kapiAdres(w http.ResponseWriter, r *http.Request) {
	bos := map[string]any{}
	if kapi.Istek(r) != kapi.Eski {
		httpx.WriteJSON(w, http.StatusOK, bos)
		return
	}
	a, err := kapi.Oku(kapi.AyarYolu)
	if err != nil {
		httpx.WriteJSON(w, http.StatusOK, bos)
		return
	}
	rol := ""
	if c := middleware.ClaimsFrom(r); c != nil {
		switch c.Role {
		case "admin":
			rol = kapi.Admin
		case "reseller":
			rol = kapi.Bayi
		}
	} else if middleware.MusteriClaimsFrom(r) != nil {
		rol = kapi.Musteri
	}
	port := a.RolPortu(rol)
	if port == 0 {
		httpx.WriteJSON(w, http.StatusOK, bos)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"adres": kapiAdresURL(r.Host, port),
		"bitis": a.EskiBitis,
	})
}

// kapiAdresURL: isteğin host'unu (varsa portunu atarak) yeni kapı portuyla birleştirir.
// IPv6 host köşeli parantezle yazılır. nginx `Host $host` ile varsayılan portta portsuz
// ama parantezli IPv6 ("[2001:db8::1]") iletir: parantez atılmazsa JoinHostPort ikinci
// kez sarar → "https://[[2001:db8::1]]:P" (geçersiz URL).
func kapiAdresURL(host string, port int) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	} else if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	return "https://" + net.JoinHostPort(host, strconv.Itoa(port))
}

// musteriKapiAdresi: müşteri adına giriş (internal/adina) için müşteri kapısının kök
// adresi. Kapı zorlaması kapalıysa ("" kapı) ayrı kapı yoktur → ("", false): arayüz
// aynı kökende kalır. Zorlama açıksa adres GEREKLİDİR (bayi/yönetici kapısında müşteri
// girişi 404'tür); okunamazsa ("", true) → uç 503 döner.
func musteriKapiAdresi(r *http.Request) (string, bool) {
	if kapi.Istek(r) == "" {
		return "", false
	}
	a, err := kapi.Oku(kapi.AyarYolu)
	if err != nil {
		return "", true
	}
	port := a.RolPortu(kapi.Musteri)
	if port == 0 {
		return "", true
	}
	return kapiAdresURL(r.Host, port), true
}

// ssoMusteriAdresi: API anahtarıyla müşteri girişinde (POST /domains/{id}/sso) dönen
// URL'nin kök adresi. Port: kapı modunda müşteri kapısı, değilse panelin tek portu.
// Host: isteğin geldiği ad; IP/localhost ise panel ana bilgisayar adı (panelhost)
// ayarlıysa o (tarayıcıda geçerli sertifika için), yoksa isteğin host'u (sunucu IP'si).
func ssoMusteriAdresi(r *http.Request) (string, bool) { return ssoKapiAdresi(r, kapi.Musteri) }

// ssoBayiAdresi: bkz. ssoMusteriAdresi — bayi paneline tek tık giriş (POST
// /resellers/{id}/sso); kapı modunda bayi kapısı.
func ssoBayiAdresi(r *http.Request) (string, bool) { return ssoKapiAdresi(r, kapi.Bayi) }

func ssoKapiAdresi(r *http.Request, rol string) (string, bool) {
	host := ssoHostAdi(r.Host)
	if host == "" {
		return "", false
	}
	if ip := net.ParseIP(host); ip != nil || host == "localhost" {
		if ad := panelhost.DurumOku().Hostname; ad != "" {
			host = ad
		}
	}
	port := 0
	if kapi.Istek(r) != "" {
		a, err := kapi.Oku(kapi.AyarYolu)
		if err != nil {
			return "", false
		}
		if port = a.RolPortu(rol); port == 0 {
			return "", false
		}
	} else if p, err := strconv.Atoi(panelhost.PanelPort()); err == nil {
		port = p
	}
	if port <= 0 {
		return "", false
	}
	return kapiAdresURL(host, port), true
}

// ssoHostAdi: Host başlığından portsuz, köşeli parantezsiz host adı.
func ssoHostAdi(h string) string {
	if hh, _, err := net.SplitHostPort(h); err == nil {
		return hh
	}
	return strings.Trim(h, "[]")
}

// kapiPortlar: GET /api/v1/kapi/portlar — otomasyonun (WHMCS modülü) ürün tipine göre doğru
// panel adresini kurabilmesi için MÜŞTERİ ve BAYİ kapısı portları (hosting ürünü → müşteri
// kapısı, bayi ürünü → bayi kapısı). Yalnız kimliği doğrulanmış yönetici/bayi ve anahtarları;
// YÖNETİCİ portu DÖNMEZ (yönetici kapısı yalnız bilenlerde kalır). Ayar okunamazsa boş nesne.
func kapiPortlar(w http.ResponseWriter, r *http.Request) {
	a, err := kapi.Oku(kapiAyarYolu)
	if err != nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"musteri": a.Musteri, "bayi": a.Bayi})
}

// kapiAyarYolu — kapiPortlar'ın okuduğu ayar (dikiş: testler geçici dosya verir).
var kapiAyarYolu = kapi.AyarYolu
