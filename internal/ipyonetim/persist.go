package ipyonetim

// Kalıcılık — multi-verify sonrası revize.
//
// KRİTİK FİX'LER (multi-verify #4):
//   - Package-level mutex `yazKilit` (Ekle/Sil ile ortak) → concurrent
//     PersistYaz half-written script'ini engeller.
//   - Atomic yazım: `.tmp` + `os.Rename` (aynı filesystem'de atomic).
//   - `bash -n` ile syntax doğrulama → hatalı script kaydedilmez → boot'ta
//     tüm panel IP'lerin kaybolması durumu.
//   - `daemon-reload` ve `enable` `exec.CommandContext(ctx, …)` — stuck
//     systemctl handler'ı bloklamaz.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// yazKilit — Ekle/Sil/PersistYaz için ortak mutex. Check-then-act ve
// concurrent-write koruması.
var yazKilit sync.Mutex

const (
	unitIcerik = `[Unit]
Description=GirginOSPanel — yönetilen ek IP adreslerini geri yükle
After=network-online.target
Wants=network-online.target
Before=nginx.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=` + KurulumScriptYolu + `

[Install]
WantedBy=multi-user.target
`
	scriptBaslik = `#!/bin/bash
# OTOMATİK ÜRETİLDİ — ipyonetim.PersistYaz
# GirginOSPanel'in cp_server_ips tablosundaki IP'leri sunucuya geri yükler.
# Elle düzenleme; bir sonraki IP değişikliğinde üzerine yazılır.
set -u

`
)

// PersistYazIci — mutex sahibi olarak çağrılır (Ekle/Sil zaten kilitlemiş).
// Dışarıdan gelen çağrılar `PersistYaz` sarmalayıcısı üzerinden gelir.
func persistYazIci(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT ip, iface, cidr FROM cp_server_ips ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var sb strings.Builder
	sb.WriteString(scriptBaslik)
	i := 0
	for rows.Next() {
		var ip, iface string
		var cidr int
		if err := rows.Scan(&ip, &iface, &cidr); err != nil {
			continue
		}
		i++
		// Yeni label (persistte deterministik değil — kernel labelı sadece
		// insan tanıması için, panel tespit label prefix'e bakar).
		// Random olur ama script determinist olsun diye INDEX bazlı slug.
		label := LabelPrefix + fmt.Sprintf("i%04d", i%10000) // %04d = 10000 slot, panel-i0001 = 11 char <= 15 OK
		// Güvenlik: label 15 char'a sığmalı — panel-i999 = 10 char. OK.
		sb.WriteString(fmt.Sprintf(
			"ip addr add %s/%d dev %s label %q 2>/dev/null || true\n",
			ip, cidr, iface, label))
	}
	sb.WriteString("\nexit 0\n")

	// 1) TMP dosyaya yaz
	tmpScript := KurulumScriptYolu + ".tmp"
	if err := os.WriteFile(tmpScript, []byte(sb.String()), 0755); err != nil {
		return fmt.Errorf("tmp script yazılamadı: %w", err)
	}

	// 2) `bash -n` ile syntax doğrula — hatalıysa rename ETME
	if out, err := exec.CommandContext(ctx, "bash", "-n", tmpScript).CombinedOutput(); err != nil {
		_ = os.Remove(tmpScript)
		return fmt.Errorf("script syntax hatalı (rename edilmedi, mevcut script korundu): %s (%v)",
			strings.TrimSpace(string(out)), err)
	}

	// 3) Atomic rename
	if err := os.Rename(tmpScript, KurulumScriptYolu); err != nil {
		_ = os.Remove(tmpScript)
		return fmt.Errorf("script rename başarısız: %w", err)
	}

	// 4) Unit — sadece içerik değiştiyse yaz + reload
	yaz := true
	if mev, err := os.ReadFile(KurulumUnitYolu); err == nil && string(mev) == unitIcerik {
		yaz = false
	}
	if yaz {
		tmpUnit := KurulumUnitYolu + ".tmp"
		if err := os.WriteFile(tmpUnit, []byte(unitIcerik), 0644); err != nil {
			return fmt.Errorf("unit tmp yazılamadı: %w", err)
		}
		if err := os.Rename(tmpUnit, KurulumUnitYolu); err != nil {
			_ = os.Remove(tmpUnit)
			return fmt.Errorf("unit rename başarısız: %w", err)
		}
		if out, err := exec.CommandContext(ctx, "systemctl", "daemon-reload").CombinedOutput(); err != nil {
			log.Printf("ipyonetim.PersistYaz: daemon-reload uyarısı: %s (%v)",
				strings.TrimSpace(string(out)), err)
		}
		if out, err := exec.CommandContext(ctx, "systemctl", "enable", "girginospanel-ip.service").CombinedOutput(); err != nil {
			log.Printf("ipyonetim.PersistYaz: enable uyarısı: %s (%v)",
				strings.TrimSpace(string(out)), err)
		}
	}
	return nil
}

// PersistYaz — dışa açık sarmalayıcı. Kilidi kendisi alır (Ekle/Sil zaten
// kilitli çağırırsa `TryLock` başarısız olur; onlar `persistYazIci`'yi
// kilitli halde çağırmalı).
func PersistYaz(ctx context.Context, db *sql.DB) error {
	// Kilit zaten Ekle/Sil'de tutuluyorsa yeniden kilitleme deadlock olur.
	// Bu yüzden Ekle/Sil `persistYazIci`'yi çağırır. Bu wrapper harici
	// çağrılar (startup init, admin manuel refresh) içindir.
	if !yazKilit.TryLock() {
		// Zaten kilitli — kısa süre bekle veya "sırada" dön.
		// Startup için ilk çağrı: mutex boş.
		return errors.New("başka bir IP işlemi devam ediyor")
	}
	defer yazKilit.Unlock()
	return persistYazIci(ctx, db)
}
