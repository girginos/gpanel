// girginospanel-avajan — GirginOSPanel antivirüs ajanı.
//
//	girginospanel-avajan --tara            # ayarlardaki kapsamı tara (tek seferlik)
//	girginospanel-avajan --tara /home/c_x  # belirli yolu tara
//	girginospanel-avajan --izle            # gerçek zamanlı izleme (fanotify)
//	girginospanel-avajan --json            # makine okunur çıktı
//
// 🔴 NEDEN fanotify, inotify DEĞİL: inotify DİZİN BAŞINA watch ister. Bir
// barındırma sunucusunda yüz binlerce dizin var; her birine watch koymak hem
// `max_user_watches` sınırını patlatır hem de megabaytlarca çekirdek belleği
// yer. fanotify tek bir MOUNT'a işaret koyar ve altındaki her şeyi kapsar.
//
// 🔴 NEDEN FAN_CLOSE_WRITE, FAN_MODIFY DEĞİL: FAN_MODIFY her write() çağrısında
// tetiklenir — 10 MB'lık bir dosya yüzlerce olay üretir ve hepsinde tararız.
// FAN_CLOSE_WRITE yazma bittiğinde BİR KEZ gelir; taramak için doğru an da odur
// (yarım yazılmış dosyayı taramak anlamsızdır).
//
// 🔴 BİLDİRİM MODU (operatör kararı): dosya yazıldıktan SONRA taranır. İzin
// modunda (FAN_OPEN_PERM) her açılış tarama bitene kadar BEKLER; ajan takılırsa
// site erişimi kilitlenir. Milisaniyelik bir açık pencere karşılığında bu risk
// alınmıyor.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/sys/unix"

	"girginospanel/internal/avayar"
	"girginospanel/internal/avmotor"
)

func main() {
	var (
		tara = flag.Bool("tara", false, "tek seferlik tarama")
		izle = flag.Bool("izle", false, "gerçek zamanlı izleme (fanotify)")
		jsn  = flag.Bool("json", false, "makine okunur çıktı")
	)
	flag.Parse()
	if !*tara && !*izle {
		fmt.Fprintln(os.Stderr, "kullanım: girginospanel-avajan --tara [yol...] | --izle")
		os.Exit(2)
	}

	db, err := dbAc()
	if err != nil {
		log.Fatalf("veritabanı: %v", err)
	}
	defer db.Close()

	ayar, err := avayar.Oku(context.Background(), db)
	if err != nil {
		log.Fatalf("ayarlar okunamadı: %v", err)
	}

	// 🔴 Motor kuralları GÖMÜLÜ tabandan gelir. İmza paketi henüz yoksa bile
	// koruma çalışır; "kural yok, sessizce hiçbir şey yapma" durumu OLMAMALI.
	motor, bozuk := avmotor.Yeni(avmotor.TabanSet(), 0)
	if bozuk > 0 {
		log.Printf("uyarı: %d kural derlenemedi (atlandı)", bozuk)
	}

	a := &ajan{db: db, ayar: ayar, motor: motor, json: *jsn}

	switch {
	case *izle:
		a.izleyiciCalistir()
	default:
		kokler := flag.Args()
		if len(kokler) == 0 {
			kokler = ayar.TaramaKokleri()
		}
		a.tamTarama(kokler)
	}
}

type ajan struct {
	db    *sql.DB
	ayar  avayar.Ayarlar
	motor *avmotor.Motor
	json  bool

	mu       sync.Mutex
	taranan  int
	bulunan  int
	bulgular []avmotor.Bulgu
}

// ── tek seferlik tarama ────────────────────────────────────────────────────

func (a *ajan) tamTarama(kokler []string) {
	kap := avayar.SunucuKapasitesi()
	_, _, isSayisi := a.ayar.Etkin(kap)

	// Hız sınırı: dosya/sn. 0 = sınırsız.
	// 🔴 cgroup CPU limiti tek başına yetmez: tarama asıl olarak DİSK G/Ç
	// yakar ve IOWeight yalnız GÖRECELİ önceliktir, mutlak tavan değildir.
	// Dosya hızı tavanı, yoğun sunucuda taramanın diski doldurmasını engeller.
	var tik <-chan time.Time
	if a.ayar.DosyaHizSn > 0 {
		t := time.NewTicker(time.Second / time.Duration(a.ayar.DosyaHizSn))
		defer t.Stop()
		tik = t.C
	}

	is := make(chan string, 256)
	var wg sync.WaitGroup
	for i := 0; i < isSayisi; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for yol := range is {
				if tik != nil {
					<-tik
				}
				a.dosyaIsle(yol)
			}
		}()
	}

	basla := time.Now()
	haric := a.ayar.HaricListesi()
	for _, kok := range kokler {
		_ = filepath.WalkDir(kok, func(yol string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // okunamayan dizini atla, taramayı durdurma
			}
			if haricMi(yol, haric) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() || !d.Type().IsRegular() {
				return nil
			}
			is <- yol
			return nil
		})
	}
	close(is)
	wg.Wait()

	a.ozetYaz(basla, kokler)
}

func (a *ajan) dosyaIsle(yol string) {
	a.mu.Lock()
	a.taranan++
	a.mu.Unlock()

	if !ilgiliUzanti(yol) {
		return
	}
	wpKok := ""
	if a.ayar.WPButunluk {
		wpKok = wpKokBul(yol)
	}
	b, tespit := a.motor.TaraDosya(yol, wpKok, nil) // sağlama kaynağı: Faz 3
	if !tespit || b.Puan < a.ayar.EsikKritik {
		return
	}
	a.mu.Lock()
	a.bulunan++
	a.bulgular = append(a.bulgular, b)
	a.mu.Unlock()

	if a.ayar.OtoKarantina {
		if err := karantinaAl(yol); err != nil {
			log.Printf("karantina başarısız %s: %v", yol, err)
		}
	}
}

func (a *ajan) ozetYaz(basla time.Time, kokler []string) {
	sure := time.Since(basla)
	if a.json {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"kokler": kokler, "taranan": a.taranan, "bulunan": a.bulunan,
			"sure_ms": sure.Milliseconds(), "bulgular": a.bulgular,
		})
		return
	}
	fmt.Printf("tarandı: %d dosya · bulgu: %d · süre: %s · kapsam: %s\n",
		a.taranan, a.bulunan, sure.Round(time.Millisecond), strings.Join(kokler, " "))
	for _, b := range a.bulgular {
		fmt.Printf("  [%s puan=%d] %s\n      %s\n", b.Seviye, b.Puan, b.Dosya, b.Aciklama)
	}
	if a.bulunan == 0 {
		fmt.Println("  temiz — eşiği aşan dosya yok")
	}
}

// ── gerçek zamanlı izleme (fanotify) ───────────────────────────────────────

func (a *ajan) izleyiciCalistir() {
	if !a.ayar.GercekZamanli {
		log.Fatal("gerçek zamanlı izleme ayarlarda KAPALI (panel → Antivirüs → Modüller)")
	}
	fd, err := unix.FanotifyInit(unix.FAN_CLASS_NOTIF|unix.FAN_CLOEXEC, unix.O_RDONLY|unix.O_LARGEFILE)
	if err != nil {
		log.Fatalf("fanotify_init: %v (CAP_SYS_ADMIN gerekli)", err)
	}
	defer unix.Close(fd)

	// FAN_MARK_MOUNT: kök başına TEK işaret, altındaki her şeyi kapsar.
	for _, kok := range a.ayar.TaramaKokleri() {
		if err := unix.FanotifyMark(fd, unix.FAN_MARK_ADD|unix.FAN_MARK_MOUNT,
			unix.FAN_CLOSE_WRITE, unix.AT_FDCWD, kok); err != nil {
			log.Fatalf("fanotify_mark %s: %v", kok, err)
		}
		log.Printf("izleniyor: %s", kok)
	}

	ctx, iptal := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer iptal()

	haric := a.ayar.HaricListesi()
	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			log.Print("izleyici durduruldu")
			return
		default:
		}
		n, err := unix.Read(fd, buf)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			log.Printf("fanotify read: %v", err)
			return
		}
		for _, olayFD := range olayDosyalari(buf[:n]) {
			yol, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", olayFD))
			unix.Close(olayFD)
			if err != nil || yol == "" {
				continue
			}
			// 🔴 Silinen dosya "(deleted)" soneki alır — taramaya çalışma.
			if strings.HasSuffix(yol, " (deleted)") {
				continue
			}
			if haricMi(yol, haric) || !ilgiliUzanti(yol) {
				continue
			}
			a.dosyaIsle(yol)
			if a.bulunan > 0 {
				a.mu.Lock()
				son := a.bulgular[len(a.bulgular)-1]
				a.bulgular = nil
				a.bulunan = 0
				a.mu.Unlock()
				log.Printf("TESPİT [%s puan=%d] %s — %s", son.Seviye, son.Puan, son.Dosya, son.Aciklama)
			}
		}
	}
}

// olayDosyalari — fanotify olay tamponunu ayrıştırıp dosya tanıtıcılarını verir.
// metaBoyut — fanotify olay başlığının bayt boyutu.
var metaBoyut = uint32(unsafe.Sizeof(unix.FanotifyEventMetadata{}))

func olayDosyalari(b []byte) []int {
	var fds []int
	for len(b) >= int(metaBoyut) {
		// 🔴 unsafe.Pointer ŞART: çekirdek ham C struct yazıyor, encoding/binary
		// ile ayrıştırmak alan hizalamasını (padding) yanlış varsayardı.
		meta := (*unix.FanotifyEventMetadata)(unsafe.Pointer(&b[0]))
		if meta.Event_len < metaBoyut || int(meta.Event_len) > len(b) {
			break
		}
		if meta.Fd >= 0 {
			fds = append(fds, int(meta.Fd))
		}
		b = b[meta.Event_len:]
	}
	return fds
}

// ── yardımcılar ────────────────────────────────────────────────────────────

// ilgiliUzanti — yalnız YÜRÜTÜLEBİLİR/enjeksiyona açık türler taranır.
//
// 🔴 Her dosyayı taramak WordPress'in sürekli yazdığı önbellek/log dosyalarında
// olay fırtınası yaratır ve disk G/Ç'sini yakar. Bir webshell .jpg olamaz —
// sunucu onu yürütmez (çift uzantı zaten konum sezgisinde yakalanıyor).
func ilgiliUzanti(yol string) bool {
	switch strings.ToLower(filepath.Ext(yol)) {
	case ".php", ".phar", ".phtml", ".php5", ".php7", ".php8", ".inc",
		".js", ".mjs", ".cgi", ".pl", ".py", ".sh", ".htaccess":
		return true
	}
	return filepath.Base(yol) == ".htaccess" || filepath.Base(yol) == ".user.ini"
}

func haricMi(yol string, haric []string) bool {
	for _, h := range haric {
		if strings.Contains(yol, h) {
			return true
		}
	}
	return false
}

// wpKokBul — dosyanın ait olduğu WordPress kurulumunun kökünü bulur.
// wp-includes/version.php varlığı kanıttır.
func wpKokBul(yol string) string {
	d := filepath.Dir(yol)
	for i := 0; i < 8; i++ { // makul derinlik sınırı
		if _, err := os.Stat(filepath.Join(d, "wp-includes", "version.php")); err == nil {
			return d
		}
		ust := filepath.Dir(d)
		if ust == d {
			break
		}
		d = ust
	}
	return ""
}

// karantinaAl — dosyayı kiracının .karantina dizinine taşır.
//
// 🔴 AYNI dosya sisteminde rename: kopyala-sil DEĞİL. Kopyalama sırasında
// saldırgan dosyayı çalıştırmaya devam edebilir ve büyük dosyada yarış oluşur.
// rename atomiktir. Ayrıca izinler 0000'a çekilir — karantinadaki bir dosya
// yanlışlıkla servis edilmemeli.
func karantinaAl(yol string) error {
	sk, home := kiraciBul(yol)
	if sk == "" {
		return fmt.Errorf("kiracı bulunamadı: %s", yol)
	}
	qdir := filepath.Join(home, ".karantina")
	if err := os.MkdirAll(qdir, 0o700); err != nil {
		return err
	}
	hedef := filepath.Join(qdir, fmt.Sprintf("%d-%s", time.Now().Unix(), filepath.Base(yol)))
	if err := os.Rename(yol, hedef); err != nil {
		return err
	}
	return os.Chmod(hedef, 0o000)
}

func kiraciBul(yol string) (sk, home string) {
	p := filepath.ToSlash(yol)
	if !strings.HasPrefix(p, "/home/") {
		return "", ""
	}
	parca := strings.Split(strings.TrimPrefix(p, "/home/"), "/")
	if len(parca) < 2 || !strings.HasPrefix(parca[0], "c_") {
		return "", ""
	}
	return parca[0], "/home/" + parca[0]
}

func dbAc() (*sql.DB, error) {
	b, err := os.ReadFile("/etc/girginospanel/env")
	if err != nil {
		return nil, err
	}
	for _, satir := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(satir, "PANEL_DB_DSN=") {
			return sql.Open("mysql", strings.TrimPrefix(satir, "PANEL_DB_DSN="))
		}
	}
	return nil, fmt.Errorf("PANEL_DB_DSN bulunamadı")
}
