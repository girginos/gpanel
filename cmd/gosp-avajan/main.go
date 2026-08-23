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
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/sys/unix"

	"girginospanel/internal/avayar"
	"girginospanel/internal/avmotor"
	"girginospanel/internal/wpsaglama"
)

func main() {
	var (
		tara  = flag.Bool("tara", false, "tek seferlik tarama")
		izle  = flag.Bool("izle", false, "gerçek zamanlı izleme (fanotify)")
		jsn   = flag.Bool("json", false, "makine okunur çıktı")
		surec = flag.Bool("surec", false, "süreç davranış izleme (netlink proc connector)")
	)
	flag.Parse()
	if !*tara && !*izle && !*surec {
		fmt.Fprintln(os.Stderr, "kullanım: girginospanel-avajan --tara [yol...] | --izle | --surec")
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

	// 🔴 Kural seti: uzak İMZALI paket (varsa + doğrulanırsa) → disk → gömülü
	// taban. GuncelSet imza doğrulamadan hiçbir kural yüklemez; en kötü durumda
	// gömülü tabana düşer. "kural yok, sessizce hiçbir şey yapma" durumu OLMAZ.
	// Ağ maliyeti: --tara her koşuda bir kez uzak dener; --izle başlangıçta.
	set := avmotor.GuncelSet(avmotor.TabanSet().Surum, false)
	motor, bozuk := avmotor.Yeni(set, 0)
	if bozuk > 0 {
		log.Printf("uyarı: %d kural derlenemedi (atlandı)", bozuk)
	}

	a := &ajan{db: db, ayar: ayar, motor: motor, json: *jsn, domCache: yeniDomainCache(db)}
	// 🔴 WP butunluk katmanini CANLANDIR: onceden nil geciliyordu ve motorun
	// en guclu katmani (cekirdek md5 dogrulamasi) hic calismiyordu (adversaryel
	// denetim C-1). Kaynak: disk onbellek -> kendi ayna -> WP.org.
	// Kosullu ATAMA (typed-nil kacinmak icin arayuz alani ancak burada atanir).
	if ayar.WPButunluk {
		a.saglama = wpsaglama.Yeni()
	}

	switch {
	case *izle:
		a.kaynak = "ajan-izle"
		a.izleyiciCalistir()
	case *surec:
		a.kaynak = "ajan-surec"
		a.surecIzle()
	default:
		a.kaynak = "ajan-zamanli"
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
	// 🔴 ARAYUZ tipi, concrete pointer DEGIL. `*wpsaglama.Kaynak` olsaydi
	// WPButunluk kapaliyken arayuze cevrilince TYPED-NIL olur ve
	// motor.go'daki `wpSaglama != nil` yanlislikla true doner -> nil
	// pointer'da RLock panik. Arayuz alani atanmazsa gercek nil kalir.
	saglama avmotor.SaglamaKaynagi

	// DB entegrasyonu: tarama kaydı + domain önbelleği.
	taramaID int64
	yuk      int64 // atomik: sistem load1 × 100 (dinamik throttle)
	domCache *domainCache
	kaynak   string // panel | ajan-zamanli | ajan-izle

	mu           sync.Mutex
	atlanan      int // RapidScan: degismedigi icin atlanan dosya
	eskiOnb      map[string]string
	yeniOnb      map[string]string
	onbMu        sync.Mutex
	taranan      int
	analizEdilen int
	bulunan      int
	bulgular     []avmotor.Bulgu

	// FAZ1 süreç izleme (tek-goroutine: surecIzle döngüsü sahibi, kilit yok).
	surecThrottle map[string]time.Time // domID:kuralKodu → son bildirim
	pidTablo      map[int]*pidKayit    // FORK-anı ebeveyn/web soyağacı (reparent-güvenli)
	uidAd         map[int]string       // uid → kullanıcı adı önbelleği
	oranKova      map[int]*kova        // uid → exec işleme token-bucket
}

// ── tek seferlik tarama ────────────────────────────────────────────────────

func (a *ajan) tamTarama(kokler []string) {
	// 🔴 CLAMAV MODU: "Kural Motoru" KAPALI iken kendi motorumuz (avmotor) yerine
	// clamav imzalari kullanilir. avmotor cok agresifti ve mesru framework
	// dosyalarinda (Compiler.php, StorageServiceProvider.php, ActionScheduler...)
	// yanlis-pozitif firtinasi uretiyordu. Bu toggle "sunucu motoru == clamav"
	// saglar. Geri donus: Kural Motoru tekrar acilinca avmotor devreye girer.
	if !a.ayar.KuralMotoru {
		a.clamavTarama(kokler)
		return
	}
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
	// 🔴 DİNAMİK YÜK THROTTLE: cgroup mutlak tavanı garanti eder ama tarama
	// yine boştaki çekirdeği doldurur. yuk_esigi>0 ise sistem 1-dk yükü eşiği
	// aşınca tarayıcı kendini duraklatır → gerçek trafik/işler önceliklenir.
	yukDur := make(chan struct{})
	if a.ayar.YukEsigi > 0 {
		go a.yukIzle(yukDur)
	}
	var wg sync.WaitGroup
	for i := 0; i < isSayisi; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for yol := range is {
				if tik != nil {
					<-tik
				}
				a.yukBekle()
				a.dosyaIsle(yol)
			}
		}()
	}

	// 🔴 RapidScan: onceki taramada TEMIZ bulunan ve o zamandan beri boyut+
	// mtime+ctime'i degismeyen dosyalar atlanir (Imunify RapidScan muadili).
	// ctime forge edilemez (her metadata/icerik degisiminde cekirdek gunceller),
	// bu yuzden timestomping ile gizlenmis zararli yine yeniden taranir.
	a.eskiOnb = a.rapidYukle()
	a.yeniOnb = map[string]string{}

	// 🔴 Tarama kaydı aç: bulgular buna bağlanır, panel görsün.
	// DURUMSUZ (--json) mod: panel kendi tarama satırını sahiplenir ve bulguları
	// JSON'dan yazar. Ajan burada İKİNCİ bir satır açmamalı — yoksa panel scan
	// başına çift av_taramalar satırı oluşur ve panelin boş satırı "temiz" diye
	// render edilir ("başarısızlık güven olarak render"). Bkz. bildirim/oto-karantina.
	if !a.json {
		a.taramaID = taramaBasla(a.db, a.kaynak, a.ayar.Kapsam)
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
			// RapidScan atlama
			if fi, e := d.Info(); e == nil {
				if ak := dosyaAnahtar(fi); ak != "" && a.eskiOnb[yol] == ak {
					a.mu.Lock()
					a.taranan++
					a.atlanan++
					a.mu.Unlock()
					a.onbMu.Lock()
					a.yeniOnb[yol] = ak
					a.onbMu.Unlock()
					return nil
				}
			}
			is <- yol
			return nil
		})
	}
	close(is)
	wg.Wait()
	close(yukDur)

	if !a.json {
		taramaBitir(a.db, a.taramaID, a.taranan, a.bulunan)
		a.rapidYaz()
	}
	a.ozetYaz(basla, kokler)
}

const rapidYol = "/var/lib/girginospanel/av/rapidscan.json"

// dosyaAnahtar — boyut:mtime:ctime (ns). ctime forge edilemez.
func dosyaAnahtar(fi os.FileInfo) string {
	var ct int64
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		ct = st.Ctim.Sec*1_000_000_000 + st.Ctim.Nsec
	}
	return fmt.Sprintf("%d:%d:%d", fi.Size(), fi.ModTime().UnixNano(), ct)
}

func (a *ajan) rapidYukle() map[string]string {
	m := map[string]string{}
	if b, err := os.ReadFile(rapidYol); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	// 🔴 Kural seti degistiyse onbellek GECERSIZ: yeni imzalar degismemis
	// dosyalara da uygulanmali; yoksa yeni kural eski "temiz" dosyayi sonsuza
	// dek atlar (nobetci bayatlamasi). Tam yeniden tarama zorla.
	if m["__kural_surum__"] != strconv.Itoa(a.motor.Surum()) {
		return map[string]string{}
	}
	delete(m, "__kural_surum__")
	return m
}

func (a *ajan) rapidYaz() {
	a.onbMu.Lock()
	a.yeniOnb["__kural_surum__"] = strconv.Itoa(a.motor.Surum())
	b, _ := json.Marshal(a.yeniOnb)
	a.onbMu.Unlock()
	if len(b) == 0 {
		return
	}
	_ = os.MkdirAll(filepath.Dir(rapidYol), 0o700)
	tmp := rapidYol + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, rapidYol) // atomik
	}
}

// yukIzle — /proc/loadavg'i 2sn'de bir okur, load1×100'u atomik saklar.
func (a *ajan) yukIzle(dur chan struct{}) {
	for {
		if b, err := os.ReadFile("/proc/loadavg"); err == nil {
			var l float64
			_, _ = fmt.Sscanf(string(b), "%f", &l)
			atomic.StoreInt64(&a.yuk, int64(l*100))
		}
		select {
		case <-dur:
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// yukBekle — sistem yükü eşiği (yuk_esigi% × çekirdek) aşarken duraklar.
// En fazla ~5 sn bekler sonra ilerler: kalıcı yüklü sunucuda tarama durmaz.
func (a *ajan) yukBekle() {
	if a.ayar.YukEsigi <= 0 {
		return
	}
	esik := int64(a.ayar.YukEsigi) * int64(runtime.NumCPU()) // load1×100 birimi
	for i := 0; i < 50; i++ {
		if atomic.LoadInt64(&a.yuk) <= esik {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (a *ajan) dosyaIsle(yol string) {
	a.mu.Lock()
	a.taranan++
	a.mu.Unlock()

	// 🔴 ilgiliUzanti gecidi taranan++'tan SONRA: "taranan" = kuyruga giren,
	// "analizEdilen" = gercekten motora sokulan. Ikisini ayirmazsak ozet
	// incelenmeyen dosyalari da "tarandi" diye sayar.
	if !ilgiliUzanti(yol) {
		return
	}
	a.mu.Lock()
	a.analizEdilen++
	a.mu.Unlock()
	wpKok := ""
	if a.ayar.WPButunluk {
		wpKok = wpKokBul(yol)
	}
	b, tespit := a.motor.TaraDosya(yol, wpKok, a.saglama)
	// YALNIZ KRITIK raporlanir. SUPHELI (50-99) raporlamak gercek WordPress'te
	// FP firtinasi uretti (ID3 medya kutuphanesinde hex, php-cs-fixer gizli
	// dosya). Bu kurallar IKINCIL sinyal (tek baslarina degil). version.php
	// korleme (SAGLAMA-KOR) 100=KRITIK oldugu icin gorunur.
	if !tespit || b.Seviye != avmotor.SeviyeKritik || b.Puan < a.ayar.EsikKritik {
		// RapidScan: analiz edilip TEMIZ cikan dosyayi onbellege al.
		if a.yeniOnb != nil {
			if fi, e := os.Stat(yol); e == nil {
				if ak := dosyaAnahtar(fi); ak != "" {
					a.onbMu.Lock()
					a.yeniOnb[yol] = ak
					a.onbMu.Unlock()
				}
			}
		}
		return
	}
	a.bulguKaydet(yol, b)
}

// bulguKaydet — tespit edilen bulguyu kaydeder: panel listesi (+json) ve otonom
// modda (--izle / zamanli --tara) oto-karantina + DB + bildirim + attack-chain
// olayi. Hem avmotor hem clamav yolu bunu ortak kullanir.
func (a *ajan) bulguKaydet(yol string, b avmotor.Bulgu) {
	a.mu.Lock()
	a.bulunan++
	a.bulgular = append(a.bulgular, b)
	a.mu.Unlock()

	// 🔴 DURUMSUZ (--json / panel) mod: yalnız tespit + JSON çıktısı. Kalıcı DB
	// kaydı, bildirim ve OTO-KARANTINA burada YAPILMAZ — panel "Tara" bir operatör
	// eylemidir; bulguları operatör inceler ve karantinayı KENDİ tetikler. Otonom
	// yollar (--izle, zamanlı --tara) aşağıdaki tam entegrasyonu çalıştırır.
	if a.json {
		return
	}

	// OTO-KARANTINA: cekirdek dosyalari (wp-includes/wp-admin) ASLA karantinaya
	// alinmaz -- silmek/tasimak siteyi bozar. Cekirdek tespiti RAPORLANIR;
	// operator `wp core verify-checksums` ile onarir.
	karantinaYol := ""
	if a.ayar.OtoKarantina && !cekirdekDosya(yol) {
		if h, err := karantinaAl(yol); err != nil {
			log.Printf("karantina basarisiz %s: %v", yol, err)
		} else {
			karantinaYol = h
		}
	}

	// 🔴 DB'YE YAZ: panel görsün. Tarama kaydı yoksa (DB yok) sessizce atlanır.
	var domID int64
	if a.domCache != nil {
		domID = a.domCache.domainID(yol)
	}
	bulguID := bulguYaz(a.db, a.taramaID, domID, b, karantinaYol)

	// 🔴 BİLDİRİM: müşteriye/panele akıt. domID>0 → müşteri, 0 → panel-geneli.
	seviye := "kritik"
	eylem := "tespit edildi"
	if karantinaYol != "" {
		eylem = "karantinaya alındı"
	}
	// 🔴 GENEL icerik: tam sunucu yolu SIZDIRILMAZ (yol yerine yalniz alan
	// adi + dosya adi). Panelde tiklaninca domain_id -> o domainin Antivirus
	// sayfasina gider (TopBar). Gorunurluk yalniz domain sahibi (bildirimYaz).
	alan := domainAdi(a.db, domID)
	baslik := "Zararlı dosya " + eylem
	if alan != "" {
		baslik = alan + " alanında zararlı dosya " + eylem
	}
	// SEL KORUMASI: domain başına okunmamış TEK toplu bildirim (bkz. db.go).
	bildirimYazAVToplu(a.db, seviye, baslik, filepath.Base(yol), domID, bulguID)
	// FAZ2 attack-chain: zararlı dosya = File Write aşaması.
	olayYaz(a.db, domID, "dosya", "dosya_yazma", "kritik", filepath.Base(yol), yol, 0, "av_bulgu", bulguID)
}

// clamBin — clamscan yolu (clamav modu).
const clamBin = "/usr/bin/clamscan"

// clamavTarama — "Kural Motoru" KAPALI iken clamscan ile tarar; bulgulari mevcut
// kayit/karantina/bildirim plumbing'i (bulguKaydet) uzerinden isler. avmotor
// devre disi; "sunucu motoru == clamav" davranisi.
func (a *ajan) clamavTarama(kokler []string) {
	basla := time.Now()
	if !a.json {
		a.taramaID = taramaBasla(a.db, a.kaynak, a.ayar.Kapsam)
	}
	if _, err := os.Stat(clamBin); err != nil {
		log.Printf("clamav modu: %s yok — tarama atlandi", clamBin)
		if !a.json {
			taramaBitir(a.db, a.taramaID, a.taranan, a.bulunan)
		}
		a.ozetYaz(basla, kokler)
		return
	}
	haric := a.ayar.HaricListesi()
	for _, kok := range kokler {
		a.clamKok(kok, haric)
	}
	if !a.json {
		taramaBitir(a.db, a.taramaID, a.taranan, a.bulunan)
	}
	a.ozetYaz(basla, kokler)
}

// clamKok — tek kok icin clamscan -r calistirir; FOUND satirlarini bulguKaydet'e
// verir, "Scanned files:" ozetinden taranan sayisini toplar.
func (a *ajan) clamKok(kok string, haric []string) {
	args := []string{"-r", "-i", "--stdout", "--max-filesize=25M", "--max-scansize=1024M"}
	for _, h := range haric {
		if h != "" {
			args = append(args, "--exclude-dir="+h)
		}
	}
	args = append(args, kok)
	out, _ := exec.Command(clamBin, args...).CombinedOutput()
	for _, satir := range strings.Split(string(out), "\n") {
		satir = strings.TrimSpace(satir)
		if strings.HasPrefix(satir, "Scanned files:") {
			if n, e := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(satir, "Scanned files:"))); e == nil {
				a.mu.Lock()
				a.taranan += n
				a.analizEdilen += n
				a.mu.Unlock()
			}
			continue
		}
		if !strings.HasSuffix(satir, " FOUND") {
			continue
		}
		i := strings.LastIndex(satir, ": ")
		if i <= 0 {
			continue
		}
		yol := satir[:i]
		imza := strings.TrimSuffix(satir[i+2:], " FOUND")
		if haricMi(yol, haric) {
			continue
		}
		a.bulguKaydet(yol, avmotor.Bulgu{
			Dosya:    yol,
			Puan:     100,
			Seviye:   avmotor.SeviyeKritik,
			Aciklama: imza,
			Kurallar: []string{"clamav"},
		})
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// cekirdekDosya — WordPress cekirdek agacinda mi (karantina istisnasi).
func cekirdekDosya(yol string) bool {
	y := filepath.ToSlash(yol)
	return strings.Contains(y, "/wp-includes/") || strings.Contains(y, "/wp-admin/")
}

func (a *ajan) ozetYaz(basla time.Time, kokler []string) {
	sure := time.Since(basla)
	if a.json {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"kokler": kokler, "taranan": a.taranan, "analiz_edilen": a.analizEdilen, "atlanan": a.atlanan, "bulunan": a.bulunan,
			"sure_ms": sure.Milliseconds(), "bulgular": a.bulgular,
		})
		return
	}
	fmt.Printf("tarandı: %d dosya (analiz: %d · rapidscan atlanan: %d) · bulgu: %d · süre: %s · kapsam: %s\n",
		a.taranan, a.analizEdilen, a.atlanan, a.bulunan, sure.Round(time.Millisecond), strings.Join(kokler, " "))
	for _, b := range a.bulgular {
		fmt.Printf("  [%s puan=%d] %s\n      %s\n", b.Seviye, b.Puan, b.Dosya, b.Aciklama)
	}
	if a.bulunan == 0 {
		fmt.Println("  temiz — eşiği aşan dosya yok")
	}
	// Kural seti kaynagi + surum: operator gomulu taban mi guncel mi gorsun.
	kaynak := "gomulu taban"
	if u := a.motor.Uretim(); u != "" && u != "gomulu" && u != "gömülü" {
		kaynak = "imzali set uretim " + u
	}
	fmt.Printf("kural seti: surum %d · %s · %d kural\n", a.motor.Surum(), kaynak, a.motor.KuralSayisi())
}

// ── gerçek zamanlı izleme (fanotify) ───────────────────────────────────────

// altKokMu — yol taranan koklerden birinin altinda mi (FILESYSTEM kapsam filtresi).
func altKokMu(yol string, kokler []string) bool {
	for _, k := range kokler {
		if k == "/" || yol == k || strings.HasPrefix(yol, strings.TrimRight(k, "/")+"/") {
			return true
		}
	}
	return false
}

func (a *ajan) izleyiciCalistir() {
	if !a.ayar.GercekZamanli {
		log.Fatal("gerçek zamanlı izleme ayarlarda KAPALI (panel → Antivirüs → Modüller)")
	}
	fd, err := unix.FanotifyInit(unix.FAN_CLASS_NOTIF|unix.FAN_CLOEXEC, unix.O_RDONLY|unix.O_LARGEFILE)
	if err != nil {
		log.Fatalf("fanotify_init: %v (CAP_SYS_ADMIN gerekli)", err)
	}
	defer unix.Close(fd)

	// 🔴 FAN_MARK_FILESYSTEM (FAN_MARK_MOUNT DEGIL): superblock/dosya-sistemi
	// seviyesinde isaret koyar → NAMESPACE-BAGIMSIZ. Unit ProtectSystem=strict ile
	// OZEL mount namespace'te calisir; FAN_MARK_MOUNT ajanin KENDI namespace'inin
	// vfsmount'unu izler ve host'ta (shell/PHP) yazilan dosyalari GORMEZ (canli
	// olculdu: sandbox'li unit yakalamadi, host-ns dogrudan ajan 2sn'de yakaladi).
	// FILESYSTEM superblock'u paylasilan oldugundan host yazimlari da olay uretir.
	kokler := a.ayar.TaramaKokleri()
	for _, kok := range kokler {
		if err := unix.FanotifyMark(fd, unix.FAN_MARK_ADD|unix.FAN_MARK_FILESYSTEM,
			unix.FAN_CLOSE_WRITE, unix.AT_FDCWD, kok); err != nil {
			log.Fatalf("fanotify_mark %s: %v", kok, err)
		}
		log.Printf("izleniyor: %s (filesystem)", kok)
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
			// FAN_MARK_FILESYSTEM tum superblock'u kapsar; kapsam=host ise yalniz
			// /home altini isle (kapsam=sunucu ise kok=/ → hepsi gecer).
			if !altKokMu(yol, kokler) || haricMi(yol, haric) || !ilgiliUzanti(yol) {
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
	// 🔴 .pht/.php3/.php4/.phtm/.phps — cogu Apache/mod_php yapilandirmasinda
	// PHP olarak YURUTULUR ve klasik bypass uzantilaridir. Uzanti gecidi
	// taramanin ON KOSULU oldugu icin listede olmayan uzanti = tam kacis.
	case ".php", ".phar", ".phtml", ".phtm", ".pht", ".php3", ".php4", ".php5",
		".php7", ".php8", ".phps", ".inc", ".shtml",
		".js", ".mjs", ".cgi", ".pl", ".py", ".sh", ".htaccess":
		return true
	}
	return filepath.Base(yol) == ".htaccess" || filepath.Base(yol) == ".user.ini"
}

func haricMi(yol string, haric []string) bool {
	// 🔴 SUBSTRING DEGIL segment/prefix. `strings.Contains(yol,"node_modules/")`
	// olsaydi saldirgan shell'i uploads/node_modules/ altina koyup taramadan
	// KACARDI (adversaryel denetimde kanitlandi: `mkdir node_modules` yeterli).
	y := filepath.ToSlash(yol)
	for _, h := range haric {
		h = filepath.ToSlash(h)
		if strings.HasPrefix(h, "/") {
			hp := strings.TrimRight(h, "/")
			if y == hp || strings.HasPrefix(y, hp+"/") {
				return true
			}
		} else {
			seg := strings.Trim(h, "/")
			if seg == "" {
				continue
			}
			for _, part := range strings.Split(y, "/") {
				if part == seg {
					return true
				}
			}
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
// karantinaAl — dosyayı .karantina'ya taşır, HEDEF YOLU döner (geri yükleme
// için DB'ye yazılır). Boş string = karantina yapılamadı.
func karantinaAl(yol string) (string, error) {
	sk, home := kiraciBul(yol)
	if sk == "" {
		return "", fmt.Errorf("kiracı bulunamadı: %s", yol)
	}
	qdir := filepath.Join(home, ".karantina")
	if err := os.MkdirAll(qdir, 0o700); err != nil {
		return "", err
	}
	hedef := filepath.Join(qdir, fmt.Sprintf("%d-%s", time.Now().Unix(), filepath.Base(yol)))
	if err := os.Rename(yol, hedef); err != nil {
		return "", err
	}
	if err := os.Chmod(hedef, 0o000); err != nil {
		return hedef, err
	}
	return hedef, nil
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
