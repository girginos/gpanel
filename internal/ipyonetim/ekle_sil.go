package ipyonetim

// IP ekleme/silme — multi-verify sonrası revize.
//
// KRİTİK FİX'LER:
//   - Label formatı: `panel-XXXX` (4 hex, 10 char). Iface prefix YOK →
//     kernel 15-char truncation bug'ı kapandı.
//   - CIDR kısıtı: sadece /24-/32. Daha geniş subnet on-link routing'i bozar.
//   - `ip addr add` çevresinde package-level mutex → concurrent check-then-act
//     race'i engeller (PersistYaz mutex'iyle ortak).
//   - Rollback ve sessiz DELETE artık log yapar (stderr).
//   - CAP_NET_ADMIN preflight (paket init'inde bir kez).

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"os/exec"
	"strings"
)

// Ekle — sunucuya yeni bir IP ekle + DB'ye kayıt + persist unit güncelle.
func Ekle(ctx context.Context, db *sql.DB, ip string, iface string, cidr int, not string, createdBy *int64) error {
	ip = normIP(ip)
	if ip == "" {
		return errors.New("geçersiz IPv4 (yalnız v4 desteklenir)")
	}
	if iface == "" {
		iface = varsayilanArayuz()
		if iface == "" {
			return errors.New("arayüz tespit edilemedi")
		}
	}
	// 🔴 CIDR sınırı: /24 - /32. Daha geniş mask on-link routing ezer
	// (multi-verify #2). Standart secondary IP kullanımında /32 idealdir.
	if cidr < 24 || cidr > 32 {
		return fmt.Errorf("CIDR /%d reddedildi — yalnız /24 ile /32 arası kabul edilir (secondary IP standardı /32'dir)", cidr)
	}

	// Iface adı sanity: harf/rakam/./_ /- + max 15 (IFNAMSIZ)
	if len(iface) == 0 || len(iface) > 15 {
		return errors.New("arayüz adı geçersiz (1-15 karakter)")
	}
	for _, c := range iface {
		if !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') &&
			!(c >= '0' && c <= '9') && c != '.' && c != '_' && c != '-' {
			return errors.New("arayüz adı geçersiz karakter içeriyor")
		}
	}

	// Mutex + check-then-act tek atom (multi-verify #4 için PersistYaz'la ortak)
	yazKilit.Lock()
	defer yazKilit.Unlock()

	if IPHostuncaVar(ip) {
		return errors.New("IP zaten sunucuda tanımlı (kullanıcı IP'si olabilir)")
	}
	var mev int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cp_server_ips WHERE ip=?`, ip).Scan(&mev)
	if mev > 0 {
		return errors.New("IP zaten DB kaydında var")
	}

	// Yeni label: `panel-` + 4 hex = 10 char, hep güvenli
	label, err := yeniLabel()
	if err != nil {
		return fmt.Errorf("label üretilemedi: %w", err)
	}

	cmd := exec.CommandContext(ctx, "ip", "addr", "add",
		fmt.Sprintf("%s/%d", ip, cidr), "dev", iface, "label", label)
	if out, e := cmd.CombinedOutput(); e != nil {
		return fmt.Errorf("ip addr add başarısız: %s (%v)", strings.TrimSpace(string(out)), e)
	}

	// DB kayıt
	_, err = db.ExecContext(ctx,
		`INSERT INTO cp_server_ips (ip, iface, cidr, note, created_by) VALUES (?, ?, ?, ?, ?)`,
		ip, iface, cidr, not, createdBy)
	if err != nil {
		// Rollback + LOG (sessiz değil)
		out, delErr := exec.Command("ip", "addr", "del",
			fmt.Sprintf("%s/%d", ip, cidr), "dev", iface).CombinedOutput()
		if delErr != nil {
			log.Printf("ipyonetim.Ekle: DB fail + rollback fail: ip=%s iface=%s out=%q err=%v",
				ip, iface, strings.TrimSpace(string(out)), delErr)
		}
		return fmt.Errorf("DB kayıt hatası (runtime IP geri alındı): %w", err)
	}

	// Persist unit güncelle (kilit hâlâ bizde — iç varyantı çağır)
	if err := persistYazIci(ctx, db); err != nil {
		log.Printf("ipyonetim.Ekle: persistYazIci başarısız (reboot restore güvenilmez): %v", err)
	}
	return nil
}

// Sil — panel-etiketli bir IP'yi kaldır.
func Sil(ctx context.Context, db *sql.DB, ip string) error {
	ip = normIP(ip)
	if ip == "" {
		return errors.New("geçersiz IP")
	}

	yazKilit.Lock()
	defer yazKilit.Unlock()

	var hedef *SunucuIP
	for _, s := range SunucuIPler() {
		if s.IP == ip {
			s2 := s
			hedef = &s2
			break
		}
	}
	if hedef == nil {
		// Sunucuda yok — DB kaydını temizle
		res, err := db.ExecContext(ctx, `DELETE FROM cp_server_ips WHERE ip=?`, ip)
		if err != nil {
			log.Printf("ipyonetim.Sil: DB DELETE fail ip=%s: %v", ip, err)
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			// PersistYaz'ı çağır — persist unit güncellensin
			if e := persistYazIci(ctx, db); e != nil {
				log.Printf("ipyonetim.Sil: persistYazIci fail after DB cleanup: %v", e)
			}
			return nil
		}
		return errors.New("IP sunucuda ve DB'de bulunamadı")
	}
	if !hedef.PanelIP {
		return errors.New("bu IP panel tarafından eklenmedi (label yok) — güvenlik gereği silinemez")
	}
	if hedef.PrimaryMi {
		return errors.New("primary IP silinemez (panel bu IP üzerinden erişilebilir kalmalı)")
	}
	if PanelBuIPUzerindeMi(ip) {
		return errors.New("panel şu an bu IP üzerinde çalışıyor — silmek erişimi keser")
	}

	// Runtime silme
	cmd := exec.CommandContext(ctx, "ip", "addr", "del",
		fmt.Sprintf("%s/%d", ip, hedef.CIDR), "dev", hedef.Iface)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ip addr del başarısız: %s (%v)", strings.TrimSpace(string(out)), err)
	}

	// DB'den kaldır
	if _, err := db.ExecContext(ctx, `DELETE FROM cp_server_ips WHERE ip=?`, ip); err != nil {
		log.Printf("ipyonetim.Sil: DB DELETE fail after runtime del ip=%s: %v", ip, err)
	}

	// Persist güncelle (kilit hâlâ bizde)
	if err := persistYazIci(ctx, db); err != nil {
		log.Printf("ipyonetim.Sil: persistYazIci fail: %v", err)
	}
	return nil
}

func yeniLabel() (string, error) {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	// "panel-" + 4 hex = 10 char, IFNAMSIZ (15) sınırında bol boşluk
	return LabelPrefix + hex.EncodeToString(b[:]), nil
}

// normIP — IPv4 normalize et; v6 reddet.
func normIP(s string) string {
	s = strings.TrimSpace(s)
	ip := net.ParseIP(s)
	if ip == nil {
		return ""
	}
	v4 := ip.To4()
	if v4 == nil {
		return "" // v6 reddedildi (multi-verify: v4-only)
	}
	return v4.String()
}

// varsayilanArayuz — default route iface'ini bul.
func varsayilanArayuz() string {
	out, err := exec.Command("ip", "-o", "-4", "route", "show", "default").Output()
	if err != nil {
		return ""
	}
	// "default via 148.251.169.177 dev pub0 proto ..."
	f := strings.Fields(string(out))
	for i := 0; i < len(f)-1; i++ {
		if f[i] == "dev" {
			return f[i+1]
		}
	}
	return ""
}
