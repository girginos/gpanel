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
//     Hesabın PHP kodunu FPM dışında çalıştıran Laravel kuyruk işleyicisi
//     (systemd, Restart=always) ve zamanlanmış görevi (cron.d) de kapanır
//     (laravel.HesapAski); askıda açılamazlar.
//   - uygulamalar: hesabın kendi systemd birimleri (User=<sk>: Uygulama
//     Çalıştırıcı eklentisinin Node/Python/konteyner birimleri dahil) durur ve
//     askıda hiçbir yoldan başlayamaz (provisioner.HesapBirimleriAski).
//   - cron: crontab spool'dan çıkarılır + hesabın süreçleri öldürülür (mevcut);
//     askıda panelden cron yazılamaz.
//   - girişler: FTP ve müşteri paneli (ftp_accounts.status), SSH/SFTP (hesap
//     süresi; ssh_erisim ayarı korunur).
//   - veritabanı: hesabın MariaDB kullanıcıları (TÜM host'ları) ACCOUNT LOCK.
//   - posta: GİRİŞ kapanır, GELEN POSTA AÇIK KALIR (kullanıcı kararı
//     2026-10-03). Hesabın ana + alt alan adlarında IMAP/POP3, SMTP AUTH
//     gönderim ve webmail tek tık reddedilir, açık oturumlar düşürülür; teslim
//     sürer (ret yok, veri kaybı yok). Bayrak mail eklentisinin veritabanında
//     (virtual_domains.askida, eklenti soketi üzerinden — PostaAski), dovecot
//     password_query'si ona bakar (provisioner.PostaAskiAltyapisi). Kutu bazlı
//     askı (virtual_users.aktif) ayrıdır, dokunulmaz.
//
// Eşzamanlılık: Uygula hesap başına kilitlidir (kilit.Hesap); tekil/toplu yol
// bayi durum denetimini ve uygulamayı bayi kilidi (kilit.Bayi — bayi zinciri,
// hosting oluşturma ve silme ile aynı) altında yapar. Uygula istek bağlamından
// bağımsız koşar (istemci kopması / HTTP zaman aşımı işlemi yarıda kesmez);
// yalnız kendi üst süresi vardır. Açılış süpürmesi
// (AskidakileriUygula) askıdaki her hesaba adımları yeniden uygular: bu dal
// yayına girmeden askıya alınmış hesaplar ve yarım kalmış işlemler uzlaşır.
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
	"time"

	"girginospanel/internal/cron"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/kilit"
	"girginospanel/internal/laravel"
	"girginospanel/internal/provisioner"
	"girginospanel/internal/sshaccess"
	"girginospanel/internal/subdomain"
)

// Askı kaynakları (domains.askida_kaynak, VARCHAR(8)) — askıyı KİM aldı:
//   - KaynakKok: kök (panel yöneticisi) elle. DB değeri 'manuel': 0048 göçünden
//     beri elle alınan HER askı bu değerle yazıldı, kimin aldığı ayırt edilemez →
//     kökün sayılır (kullanıcı kararı 2026-09-29). Değer bu yüzden korunur, göç
//     gerekmez. Bayi AÇAMAZ (ErrKokAskisi).
//   - KaynakBayiElle: bayi kendi hostingini elle; bayi de kök de açabilir.
//   - KaynakBayi: bayi zinciri (bayinin kendisi askıya alındı). Zincir geri
//     açarken YALNIZ bunları açar; elle alınanlar (kök/bayi) kalır.
const (
	KaynakKok      = "manuel"
	KaynakBayiElle = "bayi_el"
	KaynakBayi     = "bayi"
)

// bayiAcabilir — bu kaynaktaki askıyı bayi kaldırabilir mi? Tanınmayan / boş
// kaynak kökün sayılır.
func bayiAcabilir(kaynak string) bool { return kaynak == KaynakBayiElle || kaynak == KaynakBayi }

// Üst süreler. Uygula istek bağlamından BAĞIMSIZ koşar (context.WithoutCancel):
// istemci kopunca ya da HTTP zaman aşımı (chimw.Timeout) dolunca iptal olan
// istek bağlamı adımların ortasında kesip DB ile gerçek durumu ayrıştırırdı.
// Güvenli tarafa dönüş yazımları (DB'yi geri alma, yeniden askıya alma) işlemin
// kendi süresi dolmuş olsa bile ayrı bir bağlamla (kurtarmaSuresi) yapılır.
// Üretimde değerler SABİTTİR; testler kısaltır.
var (
	islemSuresi    = 10 * time.Minute
	kurtarmaSuresi = 2 * time.Minute
)

// kurtarmaBaglami — güvenli tarafa dönüş için işlem bağlamının iptalinden /
// süresinden bağımsız yeni bağlam.
func kurtarmaBaglami(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), kurtarmaSuresi)
}

var (
	// ErrKokAskisi: bayi, kökün (ya da kaynağı ayırt edilemeyen eski) askısını
	// kaldıramaz.
	ErrKokAskisi = errors.New("hesap yönetici tarafından askıya alındı — yalnız yönetici askıdan alabilir")
	// ErrKaynak: tanınmayan askı kaynağı (programlama hatası; hiçbir şey yapılmaz).
	ErrKaynak = errors.New("geçersiz askı kaynağı")
	// ErrDemo: demo abonelik askıya alınamaz / askı işlemine girmez.
	ErrDemo = errors.New("demo abonelik askıya alınamaz")
	// ErrBayiAskida: bayi askıdayken hosting TEK TEK askıya alınamaz — kaynak
	// elle askıya dönerdi ve bayi geri açılınca zincir onu atlardı (site sessizce
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
	Birimler      func(ctx context.Context, db *sql.DB, domainID int64, askida bool) error
	Uygulamalar   func(sk string, askida bool) error
	CalismaZamani func(sk string, askida bool)
	Posta         func(ctx context.Context, db *sql.DB, domainID int64, askida bool) error
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
		Birimler:      laravel.HesapAski,
		Uygulamalar:   provisioner.HesapBirimleriAski,
		CalismaZamani: calismaZamani,
		Posta:         PostaAski,
	}
}

// calismaZamani — askının çalışma zamanı adımı: önce cron (zamanlayıcı dosyası
// silinir, depo askı deposuna taşınır — yeni görev başlamasın), sonra süreçler
// (pkill). Açarken cron deposu geri gelir, zamanlayıcı yeniden üretilir.
func calismaZamani(sk string, askida bool) {
	if err := cron.AskiUygula(sk, askida); err != nil {
		log.Printf("hesap askısı: %s: cron: %v", sk, err)
	}
	provisioner.SuspendUserRuntime(sk, askida)
}

// Uygula — Varsayilan().Uygula.
func Uygula(ctx context.Context, db *sql.DB, id int64, askida bool, kaynak string) (string, error) {
	return Varsayilan().Uygula(ctx, db, id, askida, kaynak)
}

// BayiUygula — Varsayilan().BayiUygula.
func BayiUygula(ctx context.Context, db *sql.DB, resellerID int64, askida bool) (int, int, error) {
	return Varsayilan().BayiUygula(ctx, db, resellerID, askida)
}

// AskidakileriUygula — Varsayilan().AskidakileriUygula.
func AskidakileriUygula(ctx context.Context, db *sql.DB) (int, int, error) {
	return Varsayilan().AskidakileriUygula(ctx, db)
}

// hesapSatiri — domains satırının askı işlemine giren alanları.
type hesapSatiri struct {
	alanAdi, sk, surum, kaynak string
	isDemo, askida             int
	bayi                       int64
}

func satirOku(ctx context.Context, db *sql.DB, id int64) (hesapSatiri, error) {
	var h hesapSatiri
	err := db.QueryRowContext(ctx,
		`SELECT alan_adi, sistem_kullanici, is_demo, COALESCE(php_surum,'8.3'), COALESCE(askida,0), COALESCE(askida_kaynak,''), COALESCE(reseller_id,0)
		   FROM domains WHERE id=?`, id).
		Scan(&h.alanAdi, &h.sk, &h.isDemo, &h.surum, &h.askida, &h.kaynak, &h.bayi)
	return h, err
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
// kaynak: KaynakKok / KaynakBayiElle (tekil/toplu; bayi durum kuralları
// uygulanır, bayi kökün askısını açamaz) ya da KaynakBayi (bayi zinciri).
// İdempotenttir: iki kez askıya alma/askıdan alma her adımı yeniden uygular,
// zarar vermez.
//
// Sıra: DB durumu → ana vhost (ilk adım; düşerse DB eski hâline döner, hiçbir
// şey değişmemiştir — bayi zincirinin askıya alması hariç, bkz. aşağısı) →
// kalan adımlar. Kalan adımlardan biri düşerse ötekiler yine uygulanır ve
// KismiHata döner (güvenli taraf: hesap askıda sayılır).
//
// ctx'in YALNIZ değerleri kullanılır; iptali/süresi değil (bkz. islemSuresi).
//
// Kilitler (sıra: bayi → hesap): tekil/toplu yol (elle kaynaklar) bayi durumunu
// ve uygulamayı kilit.Bayi altında yapar — bayi zinciri (reseller, kilit.Bayi'yi
// tutarak BayiUygula'yı çağırır) araya girip "bayi aktif" okumasını bayatlatamaz.
// Hesap kilidi (kilit.Hesap) aynı hesaba eşzamanlı askı işlemlerini serileştirir:
// askıya almanın adımları askıdan almanın durumYaz'ından SONRA koşup DB "açık"
// derken havuzları/girişleri kapalı bırakamaz.
func (a Adimlar) Uygula(ctx context.Context, db *sql.DB, id int64, askida bool, kaynak string) (string, error) {
	if kaynak != KaynakKok && kaynak != KaynakBayiElle && kaynak != KaynakBayi {
		return "", ErrKaynak
	}
	ctx, iptal := context.WithTimeout(context.WithoutCancel(ctx), islemSuresi)
	defer iptal()
	on, err := satirOku(ctx, db, id)
	if err != nil {
		return "", err
	}
	if kaynak != KaynakBayi {
		bk := kilit.Bayi(on.bayi)
		bk.Lock()
		defer bk.Unlock()
	}
	hk := kilit.Hesap(on.sk)
	hk.Lock()
	defer hk.Unlock()
	onceki := askidaOku(ctx, db, id)
	alanAdi, err := a.uygulaKilitli(ctx, db, id, askida, kaynak)
	askiBildirimi(ctx, db, id, alanAdi, onceki, kaynak) // durum GERÇEKTEN değiştiyse (bildirim.go)
	return alanAdi, err
}

func (a Adimlar) uygulaKilitli(ctx context.Context, db *sql.DB, id int64, askida bool, kaynak string) (string, error) {
	// Kilit altında YENİDEN oku: bekleme sırasında başka bir işlem durumu değiştirmiş olabilir.
	h, err := satirOku(ctx, db, id)
	if err != nil {
		return "", err
	}
	alanAdi, sk, surum, eskiKaynak, eskiAskida := h.alanAdi, h.sk, h.surum, h.kaynak, h.askida
	if h.isDemo == 1 {
		return alanAdi, ErrDemo
	}
	if kaynak != KaynakBayi {
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
	if kaynak == KaynakBayiElle && eskiAskida == 1 && !bayiAcabilir(eskiKaynak) {
		// 🔴 Kökün askısını bayi AÇAMAZ (kullanıcı kararı 2026-09-29); kilit
		// altında okunan kaynağa göre. Yeniden askıya alma (idempotent) ise
		// kaynağı bayininkine İNDİRMEZ — yoksa bayi ardından açabilirdi.
		if !askida {
			return alanAdi, ErrKokAskisi
		}
		kaynak = eskiKaynak
	}

	if err := durumYaz(ctx, db, id, askida, kaynak); err != nil {
		return alanAdi, err
	}
	// 1) Ana vhost — askı durumunu DB'den okur (tek kaynak).
	if err := a.AnaVhost(db, id); err != nil {
		kctx, kiptal := kurtarmaBaglami(ctx)
		defer kiptal()
		if askida && kaynak == KaynakBayi {
			// 🔴 Bayi zinciri GERİ ALMAZ (zincirin önceki davranışı): bayi
			// askıdayken hosting tek tek askıya alınamaz (ErrBayiAskida), geri
			// alınsaydı askıdaki bayinin altında yayında site kalırdı. Güvenli
			// taraf: hesap askıda sayılır, kalan adımlar uygulanır; ana vhost bir
			// sonraki başarılı render'da 503 olur.
			hatalar := append([]error{fmt.Errorf("ana vhost: %w", err)}, a.kapat(kctx, db, id, sk, surum)...)
			return alanAdi, &KismiHata{Askida: true, Hatalar: hatalar}
		}
		// DB ile nginx ayrışmasın: render eski dosyayı geri koydu, DB de dönsün.
		if gerr := durumYaz(kctx, db, id, eskiAskida == 1, eskiKaynak); gerr != nil {
			log.Printf("🔴 hesap askısı: domain %d ana vhost uygulanamadı VE DB durumu geri alınamadı (DB askida=%v, vhost eski hâlinde): %v", id, askida, gerr)
			return alanAdi, &KismiHata{Askida: askida, Hatalar: []error{
				fmt.Errorf("ana vhost: %w", err), fmt.Errorf("askı durumu geri alınamadı: %w", gerr)}}
		}
		return alanAdi, &VhostHatasi{Err: err}
	}
	if askida {
		hatalar := a.kapat(ctx, db, id, sk, surum)
		if len(hatalar) > 0 && ctx.Err() != nil {
			// Üst süre adımların ortasında doldu: DB "askıda" derken açık adım
			// kalmasın — kapatma adımları (idempotent) kurtarma bağlamıyla bir kez
			// daha uygulanır.
			kctx, kiptal := kurtarmaBaglami(ctx)
			defer kiptal()
			hatalar = a.kapat(kctx, db, id, sk, surum)
		}
		if len(hatalar) > 0 {
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
	// bütün adımlar askı hâline (idempotent) döndürülür — işlemin süresi dolmuş
	// olsa da (kurtarma bağlamı).
	log.Printf("hesap askısı: domain %d askıdan alınamadı, yeniden askıya alınıyor: %v", id, errors.Join(hatalar...))
	kctx, kiptal := kurtarmaBaglami(ctx)
	defer kiptal()
	if err := durumYaz(kctx, db, id, true, eskiKaynak); err != nil {
		// DB "açık" diyor ve geri yazılamıyor: adımları kapatmak gerçeği DB'den
		// AYRIŞTIRIRDI (DB açık der, site PHP'siz, girişler kilitli). Olduğu gibi
		// bırakılır ve hesap AÇIK raporlanır; yönetici yeniden dener.
		log.Printf("🔴 hesap askısı: domain %d askıdan alma yarım kaldı VE askı durumu geri yazılamadı — DB açık, bazı adımlar kapalı: %v", id, err)
		hatalar = append(hatalar, fmt.Errorf("askı durumu geri yazılamadı: %w", err))
		return alanAdi, &KismiHata{Askida: false, Hatalar: hatalar}
	}
	if err := a.AnaVhost(db, id); err != nil {
		hatalar = append(hatalar, fmt.Errorf("ana vhost yeniden askıya alınamadı: %w", err))
	}
	hatalar = append(hatalar, a.kapat(kctx, db, id, sk, surum)...)
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
	ekle("posta girişi", a.Posta(ctx, db, id, true))
	// İşleyiciler ve uygulamalar DB kilidinden ÖNCE durur: kilitli DB'ye
	// bağlanamayıp çökme döngüsüne girmesinler.
	ekle("kuyruk işleyicisi / zamanlanmış görev", a.Birimler(ctx, db, id, true))
	if hesapKullanicisi(sk) {
		ekle("uygulamalar", a.Uygulamalar(sk, true))
	}
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
	ekle("posta girişi", a.Posta(ctx, db, id, false))
	ekle("veritabanı kullanıcıları", a.DBKullanici(db, id, false))
	// Uygulamalar ve işleyiciler DB kilidi açıldıktan SONRA başlar. Uygulama
	// adımı kuyruk işleyicisinden ÖNCE: işleyici birimi de User=<sk>'dir; askı
	// koruması kalkmadan başlatılırsa systemd koşulu yüzünden başlamazdı.
	if hesapKullanicisi(sk) {
		ekle("uygulamalar", a.Uygulamalar(sk, false))
	}
	ekle("kuyruk işleyicisi / zamanlanmış görev", a.Birimler(ctx, db, id, false))
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
// 🔴 GERİ AÇARKEN yalnız askida_kaynak='bayi' olanlar açılır: tek tek (elle —
// kök ya da bayi) askıya alınmış hesaplar bayi geri açılınca yanlışlıkla yayına dönmez. Demo
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

// AskidakileriUygula — askıdaki (askida=1) HER hesaba askı adımlarını yeniden
// uygular; DB'ye (askida/durum/kaynak) dokunmaz, bayi kuralına bakmaz (askıyı
// kaldırmaz, yalnız uygular). İdempotenttir; panel her açılışta arka planda
// çağırır:
//   - bu dal yayına girmeden askıya alınmış hesaplar (eski kod yalnız ana
//     vhost'u kapatıyordu: alt alan adları, PHP havuzu, SSH, DB kullanıcıları
//     açık) yeni kapsama geçer — kendiliğinden düzelmezdi (alt alan sapma
//     nöbetçisi askıdakileri bilerek atlar);
//   - yarım kalmış bir askı işlemi (KismiHata, çökme) uzlaşır.
//
// Döner: (uygulanan, hatalı, liste hatası).
func (a Adimlar) AskidakileriUygula(ctx context.Context, db *sql.DB) (int, int, error) {
	if db == nil {
		return 0, 0, nil
	}
	rows, err := db.QueryContext(ctx, `SELECT id FROM domains WHERE COALESCE(askida,0)=1 ORDER BY id`)
	if err != nil {
		return 0, 0, err
	}
	var liste []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, 0, err
		}
		liste = append(liste, id)
	}
	okumaHatasi := rows.Err()
	_ = rows.Close()
	if okumaHatasi != nil {
		return 0, 0, okumaHatasi
	}
	uygulanan, hatali := 0, 0
	for _, id := range liste {
		switch err := a.yenidenAski(ctx, db, id); {
		case err == nil:
			uygulanan++
		case errors.Is(err, sql.ErrNoRows):
			// bu arada silinmiş — sayma
		default:
			log.Printf("hesap askısı (açılış): domain %d: %v", id, err)
			hatali++
		}
	}
	return uygulanan, hatali, nil
}

// yenidenAski — askıdaki tek hesaba adımları (ana vhost + kapat) hesap kilidi
// altında yeniden uygular. Bu arada askıdan alınmışsa dokunmaz.
func (a Adimlar) yenidenAski(ctx context.Context, db *sql.DB, id int64) error {
	on, err := satirOku(ctx, db, id)
	if err != nil {
		return err
	}
	hk := kilit.Hesap(on.sk)
	hk.Lock()
	defer hk.Unlock()
	h, err := satirOku(ctx, db, id)
	if err != nil {
		return err
	}
	if h.askida != 1 {
		return nil
	}
	var hatalar []error
	if err := a.AnaVhost(db, id); err != nil {
		hatalar = append(hatalar, fmt.Errorf("ana vhost: %w", err))
	}
	hatalar = append(hatalar, a.kapat(ctx, db, id, h.sk, h.surum)...)
	if len(hatalar) > 0 {
		return &KismiHata{Askida: true, Hatalar: hatalar}
	}
	return nil
}
