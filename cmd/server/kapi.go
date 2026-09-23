package main

// Panel kapıları (ADR-GP-001) — `girginospanel-server kapi <komut>` CLI'ı ve
// "yeni adresin" ucu.
//
// 🔴 CLI config.Load()'dan ÖNCE çalışır: kurulum/güncelleme betikleri onu düz
// kabuktan çağırır; systemd'nin EnvironmentFile'ı (PANEL_JWT_SECRET vb.) orada
// yüklü değildir. Kapı işlemleri yalnız dosya düzeyindedir (kapi.json +
// _panel.conf) — DB ve sır gerekmez. nginx'e dokunmaz: uygulama, panel açılışındaki
// Heal'in işidir (tek uygulayıcı, tek doğrulama yolu).

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"

	"girginospanel/internal/httpx"
	"girginospanel/internal/kapi"
	"girginospanel/internal/middleware"
	"girginospanel/internal/portyonetim"
)

const kapiKullanim = `Kullanım: girginospanel-server kapi <komut>
  kur              taze kurulum: rol portlarını ata (ayar varsa portlar KORUNUR)
  gecis [gün]      mevcut kurulumdan geçiş: bugünkü port(lar) eski kapı olur
  durum [--json]   kapı ayarını göster
  port <rol>       yalnız port numarası (admin|bayi|musteri) — betikler için
  uzat <gün>       eski kapının açık kalma süresini uzat
  eski-kapat       eski kapıyı şimdi kapat (admin yeni kapıdan girmiş olmalı)
  eski-kapat --zorla   admin doğrulaması olmadan kapat (kilitlenme riski SİZDE)
Çıkış: 0 tamam · 1 hata · 2 kullanım · 3 kapı modu yapılandırılmamış`

// kapiPanelConf: geçişte "bugünkü genel portlar" buradan okunur.
const kapiPanelConf = "/etc/nginx/conf.d/_panel.conf"

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
				fmt.Fprintln(os.Stderr, "gün pozitif bir tam sayı olmalı")
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
	case "uzat":
		n := 0
		if len(args) > 1 {
			n, _ = strconv.Atoi(args[1])
		}
		if n < 1 {
			fmt.Fprintln(os.Stderr, "uzat <gün>: gün pozitif bir tam sayı olmalı")
			return 2
		}
		return kapiUzat(n)
	case "eski-kapat":
		return kapiEskiKapat(len(args) > 1 && args[1] == "--zorla")
	}
	fmt.Fprintln(os.Stderr, kapiKullanim)
	return 2
}

// kapiPortUygun: yasaklı değil ve sistemde dinlenmiyor (`ss` bir kez çalışır).
func kapiPortUygun() func(int) bool {
	mesgul := portyonetim.SistemdePortlar()
	return func(p int) bool {
		if yasak, _ := portyonetim.PortYasakMi(p); yasak {
			return false
		}
		return !mesgul[p]
	}
}

// kapiMevcut: ayar varsa (true, 0); yoksa (false, 0); okunamıyorsa (false, 1).
func kapiMevcut() (*kapi.Ayar, bool, int) {
	a, err := kapi.Oku(kapi.AyarYolu)
	if err == nil {
		return a, true, 0
	}
	if errors.Is(err, kapi.ErrYok) {
		return nil, false, 0
	}
	fmt.Fprintf(os.Stderr, "kapi.json okunamadı: %v\n", err)
	return nil, false, 1
}

func kapiKur() int {
	a, mevcut, kod := kapiMevcut()
	if kod != 0 {
		return kod
	}
	if mevcut {
		fmt.Println("kapı ayarı zaten var — portlar KORUNUYOR (yeniden kurulum):")
		kapiYazdir(a)
		return 0
	}
	a, err := kapi.KurulumAyari(kapiPortUygun(), time.Now().Unix())
	if err == nil {
		err = kapi.Yaz(kapi.AyarYolu, a)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "kapı portları atanamadı: %v\n", err)
		return 1
	}
	fmt.Println("kapı portları atandı (panel açılışında uygulanır):")
	kapiYazdir(a)
	return 0
}

func kapiGecis(gun int) int {
	a, mevcut, kod := kapiMevcut()
	if kod != 0 {
		return kod
	}
	if mevcut {
		fmt.Println("kapı geçişi zaten yapılmış:")
		kapiYazdir(a)
		return 0
	}
	conf, err := os.ReadFile(kapiPanelConf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "panel vhost okunamadı (%s): %v\n", kapiPanelConf, err)
		return 1
	}
	a, err = kapi.GecisAyari(string(conf), kapiPortUygun(), time.Now().Unix(), gun)
	if errors.Is(err, kapi.ErrDisariyaKapali) {
		fmt.Println(err.Error() + " — web moduna geçince `girginospanel-server kapi gecis` ile tekrar deneyin")
		return 0 // hata değil: operatörün bilinçli seçimi korundu
	}
	if err == nil {
		err = kapi.Yaz(kapi.AyarYolu, a)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "kapı geçişi yapılamadı: %v\n", err)
		return 1
	}
	fmt.Println("kapı geçişi hazırlandı (panel açılışında uygulanır):")
	kapiYazdir(a)
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
			fmt.Println("kapı modu yapılandırılmamış (tek ortak panel portu)")
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
		fmt.Fprintf(os.Stderr, "bilinmeyen rol: %q (admin|bayi|musteri)\n", rol)
		return 2
	}
	fmt.Println(p)
	return 0
}

func kapiUzat(gun int) int {
	err := kapi.Guncelle(kapi.AyarYolu, func(a *kapi.Ayar) error {
		if len(a.EskiPortlar) == 0 {
			return errors.New("açık eski kapı yok")
		}
		taban := time.Now().Unix()
		if a.EskiBitis > taban {
			taban = a.EskiBitis
		}
		a.EskiBitis = taban + int64(gun)*86400
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "uzatılamadı: %v\n", err)
		return 1
	}
	fmt.Printf("eski kapı süresi %d gün uzatıldı\n", gun)
	panelYenidenBaslat()
	return 0
}

func kapiEskiKapat(zorla bool) int {
	err := kapi.Guncelle(kapi.AyarYolu, func(a *kapi.Ayar) error {
		if len(a.EskiPortlar) == 0 {
			return errors.New("açık eski kapı yok")
		}
		if a.AdminDogrulandi == 0 && !zorla {
			return fmt.Errorf("admin YENİ admin kapısından (:%d) henüz hiç giriş yapmadı. Sağlayıcı "+
				"firewall'u o portu kesiyorsa eski kapıyı kapatmak herkesi kilitler. Önce yeni adresten "+
				"giriş yapın ya da riski üstlenip --zorla kullanın", a.Admin)
		}
		if zorla {
			// Eski portlar ayardan düşer → Heal onları "beklenmeyen genel listen" olarak
			// kaldırır (yine de admin kapısının dinlediği doğrulanmadan KALDIRMAZ).
			a.EskiPortlar, a.EskiBitis = nil, 0
			return nil
		}
		a.EskiBitis = time.Now().Unix()
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "eski kapı kapatılamadı: %v\n", err)
		return 1
	}
	fmt.Println("eski kapı kapatılıyor")
	panelYenidenBaslat()
	return 0
}

// panelYenidenBaslat: değişikliği hemen uygulamak için (Heal açılışta koşar).
func panelYenidenBaslat() {
	if exec.Command("systemctl", "is-active", "--quiet", "girginospanel").Run() != nil {
		fmt.Println("panel çalışmıyor — değişiklik bir sonraki açılışta uygulanır")
		return
	}
	if out, err := exec.Command("systemctl", "restart", "girginospanel").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "panel yeniden başlatılamadı: %v %s\n", err, out)
		return
	}
	fmt.Println("panel yeniden başlatıldı — değişiklik uygulandı")
}

func kapiYazdir(a *kapi.Ayar) {
	fmt.Printf("  admin   : %d\n  bayi    : %d\n  müşteri : %d\n", a.Admin, a.Bayi, a.Musteri)
	if len(a.EskiPortlar) > 0 {
		fmt.Printf("  eski    : %v — en erken %s, ve admin yeni kapıdan girene dek açık\n",
			a.EskiPortlar, time.Unix(a.EskiBitis, 0).Format("2006-01-02"))
	}
	if a.AdminDogrulandi > 0 {
		fmt.Printf("  admin yeni kapıdan girdi: %s\n", time.Unix(a.AdminDogrulandi, 0).Format("2006-01-02 15:04"))
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
// IPv6 host köşeli parantezle yazılır.
func kapiAdresURL(host string, port int) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return "https://" + net.JoinHostPort(host, strconv.Itoa(port))
}
