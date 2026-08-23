package provisioner

// oomguard.go — Sistem belleği tükenince ÖNCE veritabanının ölmesini engelle.
//
// 🔴 GERÇEK OLAY (2026-08-22, üretim): yoğun NSS sorgusu (özyinelemeli
// find/getfacl taraması) altında systemd-userdbd worker'ları birikti —
// 4105 süreç / 3.9 GB. Sunucuda SWAP YOKTU, dolayısıyla kernel doğrudan
// OOM-killer'a gitti ve en büyük RSS'li süreci, yani MariaDB'yi öldürdü.
// Veritabanı ölünce TÜM siteler 500/503 verdi; ikinci kez de öldü.
//
// Buradaki üç önlem bu zincirin her halkasını kırar:
//   1. swap yoksa KRİTİK bildirim — swap, OOM-killer ile aramızdaki tek tampon.
//      (Otomatik swap dosyası OLUŞTURULMAZ: disk tüketen, geri alınması
//      operatöre ait bir karar. Kurulum betiği bunu bilinçli yapar.)
//   2. systemd-userdbd'ye bellek/task tavanı — sızıntı olursa siteler değil
//      o servis ölür ve systemd onu yeniden başlatır.
//   3. MariaDB'ye OOMScoreAdjust=-700 + Restart=always — DB tüm sitelerin
//      ortak bağımlılığıdır; OOM adayı listesinde EN SONA konur, yine de
//      ölürse kendi kalkar.

import (
	"os"
	"os/exec"
	"strings"
)

const (
	userdbDropIn = "/etc/systemd/system/systemd-userdbd.service.d/gosp-limits.conf"
	mariaDropIn  = "/etc/systemd/system/mariadb.service.d/gosp-oom.conf"
)

// SwapVarMi — /proc/swaps'ta en az bir etkin swap alanı var mı?
func SwapVarMi() bool {
	b, err := os.ReadFile("/proc/swaps")
	if err != nil {
		return true // okuyamıyorsak yanlış alarm verme
	}
	satirlar := strings.Split(strings.TrimSpace(string(b)), "\n")
	return len(satirlar) > 1 // ilk satır başlık
}

// oomGuardEnsure — systemd drop-in'lerini idempotent yazar; değişiklik varsa
// daemon-reload eder. Servis yeniden BAŞLATILMAZ (çalışan DB'yi kesmemek için;
// drop-in bir sonraki başlatmada da geçerli olur, OOMScoreAdjust hariç).
func oomGuardEnsure() {
	degisti := false

	userdb := `[Service]
# GirginOSPanel: yogun NSS sorgusu altinda systemd-userdbd worker'lari birikip
# 3.9 GB RAM yiyerek OOM-killer'a MariaDB'yi oldurttu (2026-08-22 olayi).
# Tavan: sizinti olursa siteler degil bu servis olur ve yeniden baslar.
MemoryMax=384M
TasksMax=128
`
	if dropInYaz(userdbDropIn, userdb, "MemoryMax=384M") {
		degisti = true
	}

	maria := `[Service]
# GirginOSPanel: veritabani tum sitelerin ortak bagimliligi — OOM adaylarinda
# EN SONA. Yine de olurse kendi kalksin (siteler DB'siz 500 verir).
OOMScoreAdjust=-700
Restart=always
RestartSec=5
`
	if dropInYaz(mariaDropIn, maria, "OOMScoreAdjust=-700") {
		degisti = true
	}

	if degisti {
		_ = exec.Command("systemctl", "daemon-reload").Run()
	}
}

// dropInYaz — dosya yoksa veya beklenen anahtarı içermiyorsa yazar.
// Döndürdüğü değer: yazıldı mı.
func dropInYaz(yol, icerik, anahtar string) bool {
	if b, err := os.ReadFile(yol); err == nil && strings.Contains(string(b), anahtar) {
		return false
	}
	dizin := yol[:strings.LastIndex(yol, "/")]
	if err := os.MkdirAll(dizin, 0o755); err != nil {
		return false
	}
	if err := os.WriteFile(yol, []byte(icerik), 0o644); err != nil {
		return false
	}
	return true
}
