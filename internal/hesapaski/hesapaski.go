// Package hesapaski — hosting HESABINI askıya alma / askıdan alma: TEK yol.
//
// Kullanıcı kararı (2026-09-30): "1 hosting hesabında 1 alan adı olsa da 10 olsa
// da doğrudan HESAP askıya alınmalı." Askı hesap düzeyindedir: hesaba bağlı her
// şey tek hamlede kapanır, askıdan alınınca her şey askı öncesi hâline döner.
// Tekil uç (domains.askiToggle), toplu işlem (domains.TopluDurum) ve bayi
// zinciri (BayiUygula) YALNIZ Uygula'yı çağırır.
//
// Askıdayken:
//   - web: ana vhost + TÜM alt alan adı vhost'ları (nginx; apache arka ucu
//     kaldırılır) 503 askı sayfası. Askı durumu tek kaynaktan
//     (hesaplar.HesapAskida) okunur ve her render yolu ona bakar; ACME HTTP-01
//     açık kalır (sertifika askıda da yenilenir).
//   - PHP: hesabın FPM havuzları durur (provisioner.PHPHavuzAski); havuzu
//     yeniden başlatabilecek yollar askıda kapalıdır (provisioner.HesapAskiKapisi).
//   - cron: crontab spool'dan çıkarılır + hesabın süreçleri öldürülür (mevcut).
//   - girişler: FTP ve müşteri paneli (ftp_accounts.status), SSH/SFTP (hesap
//     süresi; ssh_erisim ayarı korunur).
//   - veritabanı: hesabın MariaDB kullanıcıları ACCOUNT LOCK.
//   - posta: kutular mail eklentisinin veritabanındadır; bu yol dokunmaz.
//
// Ek (addon) alan adı arka ucu henüz yok. Tasarım hesap (sistem kullanıcısı)
// anahtarlıdır: ek alan adının vhost'u hesaplar.HesapAskida(sk) çağırınca askıya
// girer; burada yalnız onun vhost'larını yeniden kuran tek adım eklenir.
package hesapaski

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"girginospanel/internal/hesaplar"
	"girginospanel/internal/provisioner"
	"girginospanel/internal/sshaccess"
	"girginospanel/internal/subdomain"
)

// Askı kaynakları (domains.askida_kaynak). Bayi zinciri geri açarken YALNIZ
// kendi askıya aldıklarını ('bayi') açar; elle askıya alınan ('manuel') kalır.
const (
	KaynakManuel = "manuel"
	KaynakBayi   = "bayi"
)

var (
	// ErrDemo: demo abonelik askıya alınamaz / askı işlemine girmez.
	ErrDemo = errors.New("demo abonelik askıya alınamaz")
	// ErrBayiAskida: bayi askıdayken hosting TEK TEK askıya alınamaz — kaynak
	// 'manuel'e dönerdi ve bayi geri açılınca zincir onu atlardı (site sessizce
	// kapalı kalırdı).
	ErrBayiAskida = errors.New("bayi zaten askıda — hosting hâlihazırda kapalı")
	// ErrBayiAskidaAcilamaz: bayisi askıdaki hosting TEK TEK açılamaz — askıdaki
	// bayinin altında yayında site kalırdı.
	ErrBayiAskidaAcilamaz = errors.New("bayi hesabı askıda — önce bayiyi askıdan alın")
)

// VhostHatasi: ilk adım (ana vhost) uygulanamadı. Render eski yapılandırmayı
// geri koyar ve DB durumu eski hâline döndürülür → hiçbir şey değişmedi.
type VhostHatasi struct{ Err error }

func (e *VhostHatasi) Error() string { return e.Err.Error() }
func (e *VhostHatasi) Unwrap() error { return e.Err }

// KismiHata: ana vhost'tan sonraki adımlardan en az biri uygulanamadı.
// Güvenli taraf: askıya almada hesap askıda KALIR; askıdan almada askı öncesi
// askıdaysa yeniden askıya alınır (Askida=true). Hatalar raporlanır; işlem
// idempotent olduğundan yeniden denenebilir.
type KismiHata struct {
	Askida  bool // işlem sonunda hesap askıda mı
	Hatalar []error
}

func (e *KismiHata) Error() string {
	durum := "hesap askıda bırakıldı"
	if !e.Askida {
		durum = "hesap açık"
	}
	return "işlem kısmen uygulandı, " + durum + ": " + errors.Join(e.Hatalar...).Error()
}
func (e *KismiHata) Unwrap() []error { return e.Hatalar }

// Adimlar — askının dış etkili adımları. Üretimde Varsayilan(); testler sahte
// koyar (ana makinenin nginx/php-fpm/shadow/MariaDB'sine dokunmadan sınanır).
type Adimlar struct {
	AnaVhost      func(db *sql.DB, domainID int64) error
	AltVhostlar   func(ctx context.Context, db *sql.DB, domainID int64) error
	AltHavuzlar   func(ctx context.Context, db *sql.DB, domainID int64, sk string) ([]string, error)
	PHPHavuz      func(db *sql.DB, domainID int64, sk, surum string, ekHavuzlar []string, askida bool) error
	SSHGiris      func(sk string, kilit bool) error
	DBKullanici   func(db *sql.DB, domainID int64, kilit bool) error
	CalismaZamani func(sk string, askida bool)
}

// Varsayilan — üretim adımları.
func Varsayilan() Adimlar {
	return Adimlar{
		AnaVhost:      provisioner.RerenderVhost,
		AltVhostlar:   subdomain.AltAlanVhostlariniYenidenKur,
		AltHavuzlar:   subdomain.AltAlanHavuzAdlari,
		PHPHavuz:      provisioner.PHPHavuzAski,
		SSHGiris:      sshaccess.HesapGirisKilidi,
		DBKullanici:   hesaplar.DBKullanicilariKilit,
		CalismaZamani: provisioner.SuspendUserRuntime,
	}
}

// Uygula — Varsayilan().Uygula.
func Uygula(ctx context.Context, db *sql.DB, id int64, askida bool, kaynak string) (string, error) {
	return Varsayilan().Uygula(ctx, db, id, askida, kaynak)
}

// BayiUygula — Varsayilan().BayiUygula.
func BayiUygula(ctx context.Context, db *sql.DB, resellerID int64, askida bool) (int, int, error) {
	return Varsayilan().BayiUygula(ctx, db, resellerID, askida)
}

// durumYaz — domains askı kolonları (askida, durum, askida_kaynak).
func durumYaz(ctx context.Context, db *sql.DB, id int64, askida bool, kaynak string) error {
	ak, durum := 0, "aktif"
	if askida {
		ak, durum = 1, "pasif"
	} else {
		kaynak = ""
	}
	_, err := db.ExecContext(ctx,
		`UPDATE domains SET askida=?, durum=?, askida_kaynak=? WHERE id=?`, ak, durum, kaynak, id)
	return err
}

// ftpDurum — FTP + müşteri paneli girişi: Pure-FTPd auth sorgusu da musteri.Login
// da status='active' arar → askıdayken ikisi de reddedilir.
func ftpDurum(ctx context.Context, db *sql.DB, id int64, askida bool) error {
	durum := "active"
	if askida {
		durum = "suspended"
	}
	_, err := db.ExecContext(ctx, `UPDATE ftp_accounts SET status=? WHERE domain_id=?`, durum, id)
	return err
}

// Uygula — hosting hesabına askıyı uygular (askida=true) ya da kaldırır.
// kaynak: KaynakManuel (tekil/toplu; bayi durum kuralları uygulanır) ya da
// KaynakBayi (bayi zinciri). İdempotenttir: iki kez askıya alma/askıdan alma
// her adımı yeniden uygular, zarar vermez.
//
// Sıra: DB durumu → ana vhost (ilk adım; düşerse DB eski hâline döner, hiçbir
// şey değişmemiştir — bayi zincirinin askıya alması hariç, bkz. aşağısı) →
// kalan adımlar. Kalan adımlardan biri düşerse ötekiler yine uygulanır ve
// KismiHata döner (güvenli taraf: hesap askıda sayılır).
func (a Adimlar) Uygula(ctx context.Context, db *sql.DB, id int64, askida bool, kaynak string) (string, error) {
	var alanAdi, sk, surum, eskiKaynak string
	var isDemo, eskiAskida int
	if err := db.QueryRowContext(ctx,
		`SELECT alan_adi, sistem_kullanici, is_demo, COALESCE(php_surum,'8.3'), COALESCE(askida,0), COALESCE(askida_kaynak,'')
		   FROM domains WHERE id=?`, id).
		Scan(&alanAdi, &sk, &isDemo, &surum, &eskiAskida, &eskiKaynak); err != nil {
		return "", err
	}
	if isDemo == 1 {
		return alanAdi, ErrDemo
	}
	if kaynak == KaynakManuel {
		var bayiDurum string
		if e := db.QueryRowContext(ctx,
			`SELECT u.status FROM domains d JOIN users u ON u.id=d.reseller_id WHERE d.id=?`, id).
			Scan(&bayiDurum); e == nil && bayiDurum != "active" {
			if askida {
				return alanAdi, ErrBayiAskida
			}
			return alanAdi, ErrBayiAskidaAcilamaz
		}
	}

	if err := durumYaz(ctx, db, id, askida, kaynak); err != nil {
		return alanAdi, err
	}
	// 1) Ana vhost — askı durumunu DB'den okur (tek kaynak).
	if err := a.AnaVhost(db, id); err != nil {
		if askida && kaynak == KaynakBayi {
			// 🔴 Bayi zinciri GERİ ALMAZ (zincirin önceki davranışı): bayi
			// askıdayken hosting tek tek askıya alınamaz (ErrBayiAskida), geri
			// alınsaydı askıdaki bayinin altında yayında site kalırdı. Güvenli
			// taraf: hesap askıda sayılır, kalan adımlar uygulanır; ana vhost bir
			// sonraki başarılı render'da 503 olur.
			hatalar := append([]error{fmt.Errorf("ana vhost: %w", err)}, a.kapat(ctx, db, id, sk, surum)...)
			return alanAdi, &KismiHata{Askida: true, Hatalar: hatalar}
		}
		// DB ile nginx ayrışmasın: render eski dosyayı geri koydu, DB de dönsün.
		_ = durumYaz(ctx, db, id, eskiAskida == 1, eskiKaynak)
		return alanAdi, &VhostHatasi{Err: err}
	}
	if askida {
		if hatalar := a.kapat(ctx, db, id, sk, surum); len(hatalar) > 0 {
			return alanAdi, &KismiHata{Askida: true, Hatalar: hatalar}
		}
		return alanAdi, nil
	}
	hatalar := a.ac(ctx, db, id, sk, surum)
	if len(hatalar) == 0 {
		return alanAdi, nil
	}
	if eskiAskida != 1 {
		// Zaten açık bir hesapta yeniden deneme: askıya ALMA, yalnız raporla.
		return alanAdi, &KismiHata{Askida: false, Hatalar: hatalar}
	}
	// 🔴 Güvenli taraf: askıdan alma yarım kaldı → hesap askıda kalır. DB askıya,
	// bütün adımlar askı hâline (idempotent) döndürülür.
	log.Printf("hesap askısı: domain %d askıdan alınamadı, yeniden askıya alınıyor: %v", id, errors.Join(hatalar...))
	if err := durumYaz(ctx, db, id, true, eskiKaynak); err != nil {
		hatalar = append(hatalar, fmt.Errorf("askı durumu geri yazılamadı: %w", err))
	}
	if err := a.AnaVhost(db, id); err != nil {
		hatalar = append(hatalar, fmt.Errorf("ana vhost yeniden askıya alınamadı: %w", err))
	}
	hatalar = append(hatalar, a.kapat(ctx, db, id, sk, surum)...)
	return alanAdi, &KismiHata{Askida: true, Hatalar: hatalar}
}

// kapat — ana vhost dışındaki askı adımları (hepsi denenir).
func (a Adimlar) kapat(ctx context.Context, db *sql.DB, id int64, sk, surum string) []error {
	var hatalar []error
	ekle := func(adim string, err error) {
		if err != nil {
			hatalar = append(hatalar, fmt.Errorf("%s: %w", adim, err))
		}
	}
	ekle("alt alan adları", a.AltVhostlar(ctx, db, id))
	ekle("FTP/panel girişi", ftpDurum(ctx, db, id, true))
	ekle("veritabanı kullanıcıları", a.DBKullanici(db, id, true))
	if hesapKullanicisi(sk) {
		havuzlar, err := a.AltHavuzlar(ctx, db, id, sk)
		ekle("alt alan PHP havuzları", err)
		ekle("PHP", a.PHPHavuz(db, id, sk, surum, havuzlar, true))
		ekle("SSH/SFTP girişi", a.SSHGiris(sk, true))
		a.CalismaZamani(sk, true) // cron spool + süreçler (en sonda: havuz artık durdu)
	}
	return hatalar
}

// ac — ana vhost dışındaki askıdan alma adımları (hepsi denenir). PHP havuzu
// alt alan vhost'larından ÖNCE açılır: alt alan üreticisi havuzu ayarlarından
// yeniden yazar, eski kopya artık kalmaz.
func (a Adimlar) ac(ctx context.Context, db *sql.DB, id int64, sk, surum string) []error {
	var hatalar []error
	ekle := func(adim string, err error) {
		if err != nil {
			hatalar = append(hatalar, fmt.Errorf("%s: %w", adim, err))
		}
	}
	if hesapKullanicisi(sk) {
		havuzlar, err := a.AltHavuzlar(ctx, db, id, sk)
		ekle("alt alan PHP havuzları", err)
		ekle("PHP", a.PHPHavuz(db, id, sk, surum, havuzlar, false))
	}
	ekle("alt alan adları", a.AltVhostlar(ctx, db, id))
	ekle("FTP/panel girişi", ftpDurum(ctx, db, id, false))
	ekle("veritabanı kullanıcıları", a.DBKullanici(db, id, false))
	if hesapKullanicisi(sk) {
		ekle("SSH/SFTP girişi", a.SSHGiris(sk, false))
		a.CalismaZamani(sk, false)
	}
	return hatalar
}

// hesapKullanicisi — sistem kullanıcısı adımları (PHP/SSH/cron) yalnız panelin
// oluşturduğu c_ hesaplarına uygulanır (sistem/root kullanıcısına ASLA).
func hesapKullanicisi(sk string) bool { return strings.HasPrefix(sk, "c_") }

// BayiUygula — bir bayinin TÜM hosting hesaplarına askıyı zincirleme uygular ya
// da kaldırır; her hesap Uygula'dan (KaynakBayi) geçer.
//
// 🔴 GERİ AÇARKEN yalnız askida_kaynak='bayi' olanlar açılır: tek tek (manuel)
// askıya alınmış hesaplar bayi geri açılınca yanlışlıkla yayına dönmez. Demo
// abonelikler zincire GİRMEZ. Döner: (başarılı, başarısız, hata). Başarısız > 0
// ise çağıran bunu kullanıcıya ve denetim kaydına yansıtmalı.
func (a Adimlar) BayiUygula(ctx context.Context, db *sql.DB, resellerID int64, askida bool) (int, int, error) {
	if db == nil || resellerID <= 0 {
		return 0, 0, nil
	}
	sorgu := `SELECT id FROM domains
	           WHERE reseller_id=? AND COALESCE(is_demo,0)=0 AND COALESCE(askida,0)=0`
	if !askida {
		sorgu = `SELECT id FROM domains
		          WHERE reseller_id=? AND COALESCE(askida,0)=1 AND askida_kaynak='bayi'`
	}
	rows, err := db.QueryContext(ctx, sorgu, resellerID)
	if err != nil {
		return 0, 0, err
	}
	var liste []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			liste = append(liste, id)
		}
	}
	okumaHatasi := rows.Err()
	_ = rows.Close()
	if okumaHatasi != nil {
		// Liste yarım okunduysa devam etmek "yarısı askıya alındı, hepsi tamam"
		// yanılgısı üretir — hiç dokunmadan hata dön.
		return 0, 0, fmt.Errorf("bayi %d hosting listesi eksik okundu: %w", resellerID, okumaHatasi)
	}
	basarili, basarisiz := 0, 0
	for _, id := range liste {
		if _, err := a.Uygula(ctx, db, id, askida, KaynakBayi); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// Liste anlık görüntü: hesap bu arada silinmiş — sayma.
				log.Printf("bayi-aski: domain %d islem sirasinda silinmis — atlandi", id)
				continue
			}
			log.Printf("bayi-aski: domain %d: %v", id, err)
			basarisiz++
			continue
		}
		basarili++
	}
	return basarili, basarisiz, nil
}
