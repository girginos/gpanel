package optimize

// HTTP endpoint'ler:
//   GET  /optimize/analiz    → sistem + tüm servisler analizi
//   POST /optimize/uygula    → {servis, oneri_id[]} → yedek + uygula + reload
//   GET  /optimize/yedekler  → son yedekler
//   POST /optimize/rollback  → {yedek_id} → hedefi yedeğe geri döndür

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Handler struct {
	DB    *sql.DB
	Aktor func(r *http.Request) (int64, bool)
}

// suzVeNormalize — normalize + dusurme korumasi.
// Suzgec normalize'dan SONRA calisir: normalize nil dilimleri bos dilime
// cevirir, boylece suzgec nil uzerinde gezinmez.
func suzVeNormalize(a *ServisAnaliz) *ServisAnaliz {
	a = normalize(a)
	AnaliziSuz(a)
	return a
}

func (h *Handler) Analiz(w http.ResponseWriter, r *http.Request) {
	sistem := SistemOku()
	rapor := AnalizRaporu{
		Sistem: sistem,
		Zaman:  time.Now(),
		Servisler: []ServisAnaliz{
			*h.enrichSonUygulama(suzVeNormalize(&[]ServisAnaliz{mariadbAnaliz(sistem)}[0])),
			*h.enrichSonUygulama(suzVeNormalize(&[]ServisAnaliz{nginxAnaliz(sistem)}[0])),
			*h.enrichSonUygulama(suzVeNormalize(&[]ServisAnaliz{apacheAnaliz(sistem)}[0])),
			*h.enrichSonUygulama(suzVeNormalize(&[]ServisAnaliz{phpfpmAnaliz(sistem)}[0])),
			*h.enrichSonUygulama(suzVeNormalize(&[]ServisAnaliz{sysctlAnaliz(sistem)}[0])),
		},
	}
	yazJSON(w, 200, rapor)
}

func (h *Handler) Yedekler(w http.ResponseWriter, r *http.Request) {
	items, err := YedekListele(h.DB, 50)
	if err != nil {
		yazHata(w, 500, err.Error())
		return
	}
	yazJSON(w, 200, map[string]any{"items": items})
}

func (h *Handler) Uygula(w http.ResponseWriter, r *http.Request) {
	var g UygulaIstek
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		yazHata(w, 400, "geçersiz gövde")
		return
	}
	if g.Servis == "" || len(g.OneriID) == 0 {
		yazHata(w, 400, "servis ve oneri_id gerekli")
		return
	}
	var uid *int64
	if h.Aktor != nil {
		if u, ok := h.Aktor(r); ok {
			uid = &u
		}
	}

	// Analizi yeniden çalıştır — öneri ID'leri güncel olsun
	sistem := SistemOku()
	var analiz ServisAnaliz
	switch g.Servis {
	case "mariadb":
		analiz = mariadbAnaliz(sistem)
	case "nginx":
		analiz = nginxAnaliz(sistem)
	case "apache":
		analiz = apacheAnaliz(sistem)
	case "phpfpm":
		analiz = phpfpmAnaliz(sistem)
	case "sysctl":
		analiz = sysctlAnaliz(sistem)
	default:
		yazHata(w, 400, "bilinmeyen servis")
		return
	}
	// İstenen öneriler
	istenenler := map[string]bool{}
	for _, id := range g.OneriID {
		istenenler[id] = true
	}
	hedefler := []Oneri{}
	for _, o := range analiz.Oneriler {
		if istenenler[o.ID] {
			hedefler = append(hedefler, o)
		}
	}
	if len(hedefler) == 0 {
		yazHata(w, 400, "eşleşen öneri yok — analizi yenile")
		return
	}

	// Uygulanacak dosya bazlı grupla (aynı dosyayı bir kez yedekle)
	yedeklenen := map[string]*YedekKayit{}
	for _, o := range hedefler {
		if _, ok := yedeklenen[o.Dosya]; ok {
			continue
		}
		aciklama := fmt.Sprintf("%s: %d öneri uygulanmadan önce", g.Servis, len(hedefler))
		k, err := DosyaYedekle(h.DB, g.Servis, o.Dosya, aciklama, uid)
		if err != nil {
			yazHata(w, 500, "yedek: "+err.Error())
			return
		}
		yedeklenen[o.Dosya] = k
	}

	// Uygulananlar ozeti - her yedek icin (param=deger,...)
	uygulamaOzet := make(map[string][]string) // dosya -> []"param=deger"
	for _, o := range hedefler {
		uygulamaOzet[o.Dosya] = append(uygulamaOzet[o.Dosya], fmt.Sprintf("%s=%s", o.Param, o.Onerilen))
	}
	for dosya, kayit := range yedeklenen {
		if ozet, ok := uygulamaOzet[dosya]; ok {
			YedekUygulamalariYaz(h.DB, kayit.ID, strings.Join(ozet, ", "))
		}
	}

	// Uygula
	uygulandi := []string{}
	for _, o := range hedefler {
		if err := oneriUygula(o); err != nil {
			// Rollback: yedeklenen dosyaları geri döndür (servise henüz
			// dokunulmadı, o yüzden ayağa kaldırmaya gerek yok — ama emin
			// olmak için durumu raporlayalım).
			for _, k := range yedeklenen {
				_ = YedekGeriYukle(h.DB, k.ID)
			}
			yazHata(w, 500, fmt.Sprintf("uygulama başarısız (%s): %s — değişiklikler geri alındı", o.Param, err.Error()))
			return
		}
		uygulandi = append(uygulandi, o.ID)
	}

	// Servis reload/restart (nginx için -t sınama önce)
	if err := servisSaglikVeReload(g.Servis); err != nil {
		// Rollback: dosyaları geri yükle...
		for _, k := range yedeklenen {
			_ = YedekGeriYukle(h.DB, k.ID)
		}
		// ...VE SERVİSİ AYAĞA KALDIR. 🔴 Bu adım eskiden YOKTU: dosya geri
		// yükleniyor ama servis çalıştırılmıyordu. MariaDB'de etki "restart"
		// olduğu için başarısız bir restart veritabanını KAPALI bırakıyordu.
		ayagaKalkti := servisAyagaKaldir(g.Servis)
		mesaj := "servis reload: " + err.Error()
		if ayagaKalkti {
			mesaj += " — değişiklikler geri alındı, servis eski ayarlarla ÇALIŞIYOR"
		} else {
			mesaj += " — değişiklikler geri alındı ama SERVİS AYAĞA KALKMADI, elle müdahale gerekli"
		}
		yazHata(w, 500, mesaj)
		return
	}

	// 🔴 NEGATİF KONTROL (yalnız sysctl): `sysctl --system` GEÇERSİZ parametrede
	// bile EXIT=0 döner ve BAŞKA bir sysctl.d dosyası (ör. 99-girginosvm-perf)
	// panelin değerini EZEBİLİR. İkisinde de reload "başarılı" görünür ama runtime
	// önerilenle EŞLEŞMEZ. Uygulanan her paramı geri okuyup doğrula; eşleşmeyeni
	// kullanıcıya bildir ("başarısızlık güven olarak render" — sessiz geçme).
	ozetMesaj := fmt.Sprintf("%d öneri uygulandı, %d yedek alındı", len(uygulandi), len(yedeklenen))
	if g.Servis == "sysctl" {
		var sapan []string
		for _, o := range hedefler {
			bek := sysctlNormalize(o.Onerilen)
			got := sysctlNormalize(execOku("sysctl", "-n", o.Param))
			if bek != "" && got != "" && bek != got {
				sapan = append(sapan, fmt.Sprintf("%s (istenen %s, çalışan %s)", o.Param, bek, got))
			}
		}
		if len(sapan) > 0 {
			ozetMesaj += fmt.Sprintf(" — ⚠ %d parametre dosyaya yazıldı ama runtime'da farklı: %s. Başka bir /etc/sysctl.d dosyası eziyor olabilir.",
				len(sapan), strings.Join(sapan, "; "))
		}
	}

	yazJSON(w, 200, UygulaSonuc{
		YedekID:   yedekIDlerStr(yedeklenen),
		Yol:       yedekYollarStr(yedeklenen),
		Uygulanan: uygulandi,
		Etki:      etkiTur(g.Servis),
		Basarili:  true,
		Mesaj:     ozetMesaj,
	})
}

func (h *Handler) Rollback(w http.ResponseWriter, r *http.Request) {
	var g struct {
		YedekID int64 `json:"yedek_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil || g.YedekID <= 0 {
		yazHata(w, 400, "yedek_id gerekli")
		return
	}
	if err := YedekGeriYukle(h.DB, g.YedekID); err != nil {
		yazHata(w, 500, err.Error())
		return
	}
	yazJSON(w, 200, map[string]any{"ok": true})
}

func oneriUygula(o Oneri) error {
	switch o.Servis {
	case "mariadb":
		return SatirYazVeyaGuncelle(o.Dosya, o.Param, o.Onerilen)
	case "nginx":
		return nginxAnaConfGuncelle(o.Dosya, o.Param, o.Onerilen)
	case "apache":
		return apacheDirGuncelle(o.Dosya, o.Param, o.Onerilen)
	case "phpfpm":
		// param formu "[<pool>] pm.max_children" — pool bilgisini kullanmıyoruz,
		// çünkü Dosya alanı zaten pool dosyasını gösteriyor
		anahtar := o.Param
		if i := strings.Index(anahtar, "] "); i >= 0 {
			anahtar = anahtar[i+2:]
		}
		if err := SatirYazVeyaGuncelle(o.Dosya, anahtar, o.Onerilen); err != nil {
			return err
		}
		// 🔴 pm.* degerleri BIRBIRINE BAGLI. Kullanici tek bir oneriyi
		// (ornegin sadece pm.max_children) uygulayabildigi icin sonuc
		// GECERSIZ bir kombinasyon olabiliyordu ve php-fpm konfigi
		// reddediyordu ("failed to post process the configuration") ->
		// bir sonraki restartta servis ACILMIYORDU. Yazimdan sonra kalan
		// degerleri gecerli araliga cek.
		return phpfpmTutarliKil(o.Dosya)
	case "sysctl":
		return SatirYazVeyaGuncelle(o.Dosya, o.Param, o.Onerilen)
	}
	return errors.New("bilinmeyen servis")
}

// nginxAnaConfGuncelle — main directive'i regex ile değiştir/ekle.
// nginxBaglam — her direktifin AIT OLDUGU nginx baglami.
//
// 🔴 Bu tablo olmadan yazim yeri tahmin ediliyordu ve yanlisti: direktif
// nginx.conf'ta bulunamayinca `events {}` icine yaziliyordu. Olculdu —
// 28 direktifin 16'si http baglamina ait, events icinde nginx
// "directive is not allowed here" verip ACILMIYOR. `nginx -t` bunu
// yakalayip geri aliyordu, yani ozellik cokmuyordu ama 16 direktif
// HIC uygulanamiyordu (kullaniciya "uygulama basarisiz" olarak donuyordu).
var nginxBaglam = map[string]string{
	"worker_processes":     "main",
	"worker_rlimit_nofile": "main",
	"worker_connections":   "events",
}

// nginxHTTPConfYolu — http baglamindaki ayarlarin KANONIK yeri.
// nginx.conf'un http blogu yerine panelin kendi dosyasi kullanilir: bu
// dosya zaten bu direktiflerin cogunu tasiyor, dolayisiyla nginx.conf'a
// yazmak `duplicate` uretirdi.
const nginxHTTPConfYolu = "/etc/nginx/conf.d/00-girginospanel-perf.conf"

// NginxHedefDosya — bu direktifin GERCEKTEN yazilacagi dosya.
// Analiz asamasinda Oneri.Dosya'ya konur; yedekleme ve geri alma bu
// alani kullandigi icin yanlis olursa degistirilen dosya YEDEKLENMEZ.
//
// Kural: direktif nerede TANIMLIYSA orada degistirilir (ikinci tanim
// uretmemek icin). Hicbir yerde yoksa baglamin kanonik dosyasina yazilir.
func NginxHedefDosya(param string) string {
	baglam := nginxDirektifBaglami(param)
	aday := []string{nginxHTTPConfYolu, NginxAnaConfYolu}
	if baglam != "http" {
		aday = []string{NginxAnaConfYolu, nginxHTTPConfYolu}
	}
	for _, y := range aday {
		b, err := readAll(y)
		if err != nil {
			continue
		}
		for _, ln := range strings.Split(string(b), "\n") {
			t := strings.TrimSpace(ln)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			if strings.HasPrefix(t, param+" ") || strings.HasPrefix(t, param+"\t") {
				return y
			}
		}
	}
	if baglam == "http" {
		return nginxHTTPConfYolu
	}
	return NginxAnaConfYolu
}

// nginxDirektifBaglami — tabloda yoksa http varsay (direktiflerin ezici
// cogunlugu http baglaminda).
func nginxDirektifBaglami(param string) string {
	if b, ok := nginxBaglam[param]; ok {
		return b
	}
	return "http"
}

// nginxSatirDegistir — dosyada direktif varsa satiri degistirir.
// Degistirdiyse true doner. Yorum satirlarina dokunmaz.
func nginxSatirDegistir(dosya, param, deger string) (bool, error) {
	b, err := readAll(dosya)
	if err != nil {
		return false, err
	}
	lines := strings.Split(string(b), "\n")
	bulundu := false
	for i, ln := range lines {
		trim := strings.TrimSpace(ln)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		if strings.HasPrefix(trim, param+" ") || strings.HasPrefix(trim, param+"\t") {
			lead := ln[:len(ln)-len(strings.TrimLeft(ln, " \t"))]
			lines[i] = lead + param + " " + deger + ";"
			bulundu = true
		}
	}
	if !bulundu {
		return false, nil
	}
	return true, atomikYaz(dosya, []byte(strings.Join(lines, "\n")), 0o644)
}

func nginxAnaConfGuncelle(dosya, param, onerilen string) error {
	baglam := nginxDirektifBaglami(param)
	yeniSatir := param + " " + onerilen + ";"

	// 1) Direktif NEREDE tanimliysa ORADA degistir — boylece asla ikinci bir
	//    tanim uretmeyiz. Once kanonik http dosyasi, sonra ana konf.
	aday := []string{nginxHTTPConfYolu, dosya}
	if baglam != "http" {
		aday = []string{dosya, nginxHTTPConfYolu}
	}
	for _, y := range aday {
		if y == "" {
			continue
		}
		if _, err := readAll(y); err != nil {
			continue // dosya yok, sonrakine bak
		}
		degisti, err := nginxSatirDegistir(y, param, onerilen)
		if err != nil {
			return err
		}
		if degisti {
			return nil
		}
	}

	// 2) Hicbir yerde yok — DOGRU baglama ekle.
	switch baglam {
	case "http":
		// Panelin kendi http dosyasina ekle (yoksa olustur).
		b, _ := readAll(nginxHTTPConfYolu)
		icerik := string(b)
		if strings.TrimSpace(icerik) == "" {
			icerik = "# GirginOSPanel — performans ayarlari (http baglami)\n"
		}
		if !strings.HasSuffix(icerik, "\n") {
			icerik += "\n"
		}
		icerik += yeniSatir + "\n"
		return atomikYaz(nginxHTTPConfYolu, []byte(icerik), 0o644)

	case "events":
		b, err := readAll(dosya)
		if err != nil {
			return err
		}
		lines := strings.Split(string(b), "\n")
		for i, ln := range lines {
			t := strings.TrimSpace(ln)
			if strings.HasPrefix(t, "events") && strings.Contains(ln, "{") {
				lines = append(lines[:i+1], append([]string{"    " + yeniSatir}, lines[i+1:]...)...)
				return atomikYaz(dosya, []byte(strings.Join(lines, "\n")), 0o644)
			}
		}
		return errors.New("nginx.conf events blogu bulunamadi — elle ekleyin")

	default: // main
		b, err := readAll(dosya)
		if err != nil {
			return err
		}
		lines := strings.Split(string(b), "\n")
		// main baglam: ilk `events`/`http` blogundan ONCE olmali
		ekle := 0
		for i, ln := range lines {
			t := strings.TrimSpace(ln)
			if strings.HasPrefix(t, "events") || strings.HasPrefix(t, "http") {
				ekle = i
				break
			}
		}
		lines = append(lines[:ekle], append([]string{yeniSatir}, lines[ekle:]...)...)
		return atomikYaz(dosya, []byte(strings.Join(lines, "\n")), 0o644)
	}
}

// apacheDirGuncelle — <IfModule> içinde direktifi güncelle/ekle. Dosya yoksa oluştur.
func apacheDirGuncelle(dosya, param, onerilen string) error {
	b, _ := readAll(dosya)
	s := string(b)
	yeniSatir := fmt.Sprintf("    %s %s", param, onerilen)
	if s == "" {
		// Yeni tuning dosyası
		s = "<IfModule mpm_event_module>\n" + yeniSatir + "\n</IfModule>\n"
		return atomikYaz(dosya, []byte(s), 0o644)
	}
	lines := strings.Split(s, "\n")
	found := false
	for i, ln := range lines {
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "#") || trim == "" {
			continue
		}
		if strings.HasPrefix(trim, param+" ") || strings.HasPrefix(trim, param+"\t") {
			lead := ln[:len(ln)-len(strings.TrimLeft(ln, " \t"))]
			lines[i] = lead + param + " " + onerilen
			found = true
		}
	}
	if !found {
		// </IfModule>'den önce ekle
		for i := len(lines) - 1; i >= 0; i-- {
			if strings.Contains(lines[i], "</IfModule>") {
				lines = append(lines[:i], append([]string{yeniSatir}, lines[i:]...)...)
				found = true
				break
			}
		}
		if !found {
			lines = append(lines, yeniSatir)
		}
	}
	return atomikYaz(dosya, []byte(strings.Join(lines, "\n")), 0o644)
}

func servisSaglikVeReload(servis string) error {
	switch servis {
	case "nginx":
		if out, err := execOutput("nginx", "-t"); err != nil {
			return fmt.Errorf("nginx -t: %s", strings.TrimSpace(out))
		}
		return execRun("nginx", "-s", "reload")
	case "apache":
		if out, err := execOutput("httpd", "-t"); err != nil {
			return fmt.Errorf("httpd -t: %s", strings.TrimSpace(out))
		}
		return execRun("systemctl", "reload", "httpd")
	case "phpfpm":
		if out, err := execOutput("php-fpm", "-t"); err != nil {
			return fmt.Errorf("php-fpm -t: %s", strings.TrimSpace(out))
		}
		return execRun("systemctl", "reload", "php-fpm")
	case "mariadb":
		// 1) Config validate: mysqld --help --verbose ile syntax check (defaults dahil)
		if out, err := execOutput("mysqld", "--help", "--verbose", "--log-warnings=0"); err != nil {
			return fmt.Errorf("mysqld config validate: %s", strings.TrimSpace(kirp(out, 400)))
		}
		// 2) Restart
		if err := execRun("systemctl", "restart", "mariadb"); err != nil {
			return fmt.Errorf("systemctl restart mariadb: %w", err)
		}
		// 3) 15sn içinde active + SELECT 1 canlılık
		for i := 0; i < 15; i++ {
			time.Sleep(1 * time.Second)
			if !ServisAktif("mariadb") {
				continue
			}
			if out, err := execOutput("mysql", "-N", "-e", "SELECT 1"); err == nil && strings.TrimSpace(out) == "1" {
				return nil
			}
		}
		return fmt.Errorf("mariadb 15sn içinde canlı yanıt vermedi — rollback gerekli")
	case "sysctl":
		// Eski ad (override-öncesi) hâlâ diskteyse SİL: aynı parametreler iki
		// dosyada kalırsa çakışma sürer. Yeni dosya (99-zz-) kanonik.
		if SysctlEskiYolu != SysctlYolu {
			_ = os.Remove(SysctlEskiYolu)
		}
		return execRun("sysctl", "--system")
	}
	return nil
}

// execOku — komutu çalıştırır, birleşik çıktının trim'lenmiş halini döner
// (hata olsa da boş döner; negatif kontrol için yeterli).
func execOku(name string, args ...string) string {
	out, _ := execOutput(name, args...)
	return strings.TrimSpace(out)
}

// sysctlNormalize — sysctl değerlerini karşılaştırmak için normalize eder:
// TAB/çoklu boşlukları tek boşluğa indirir (sysctl -n çıktısı TAB kullanır,
// öneri değerleri de). Böylece "1024\t65535" == "1024 65535".
func sysctlNormalize(v string) string {
	return strings.Join(strings.Fields(v), " ")
}

// servisAyagaKaldir — geri alma SONRASI servisi eski (saglam) konfigle
// tekrar calistirir ve gercekten ayakta oldugunu DOGRULAR.
// Donus: servis calisiyor mu.
//
// 🔴 "restart komutu hata vermedi" YETMEZ — systemd basarili doner ama
// servis saniyeler icinde olebilir. MariaDB icin ayrica SELECT 1 ile
// canlilik sinanir.
// phpfpmTutarliKil — bir havuz dosyasindaki pm.* degerlerini php-fpm'in
// kabul edecegi araliga ceker.
//
// php-fpm (pm=dynamic) su kisitlari ZORUNLU tutar:
//
//	pm.max_children     >= pm.max_spare_servers
//	pm.max_spare_servers >= pm.min_spare_servers >= 1
//	pm.min_spare_servers <= pm.start_servers <= pm.max_spare_servers
//
// Ihlal edilirse konfig gecersiz olur ve php-fpm BASLAMAZ.
// pm=ondemand / pm=static havuzlarinda bu alanlar zaten yok sayilir,
// dolayisiyla dokunmayiz.
func phpfpmTutarliKil(dosya string) error {
	b, err := readAll(dosya)
	if err != nil {
		return err
	}
	metin := string(b)

	mod := fpmDeger(metin, "pm")
	if mod != "dynamic" {
		return nil // ondemand/static: spare degerleri baglayici degil
	}

	mc := fpmSayi(metin, "pm.max_children", 5)
	mxs := fpmSayi(metin, "pm.max_spare_servers", mc/2)
	mns := fpmSayi(metin, "pm.min_spare_servers", 1)
	ss := fpmSayi(metin, "pm.start_servers", mns)

	if mc < 1 {
		mc = 1
	}
	if mxs > mc {
		mxs = mc
	}
	if mxs < 1 {
		mxs = 1
	}
	if mns > mxs {
		mns = mxs
	}
	if mns < 1 {
		mns = 1
	}
	if ss < mns {
		ss = mns
	}
	if ss > mxs {
		ss = mxs
	}

	for _, ck := range []struct {
		k string
		v int
	}{
		{"pm.max_children", mc},
		{"pm.max_spare_servers", mxs},
		{"pm.min_spare_servers", mns},
		{"pm.start_servers", ss},
	} {
		if fpmSayi(metin, ck.k, -1) == -1 {
			continue // dosyada tanimli degil — biz eklemeyelim
		}
		if err := SatirYazVeyaGuncelle(dosya, ck.k, strconv.Itoa(ck.v)); err != nil {
			return err
		}
	}
	return nil
}

var fpmDegerRe = regexp.MustCompile(`(?m)^[ \t]*([A-Za-z_][A-Za-z0-9_.]*)[ \t]*=[ \t]*(\S+)`)

func fpmDeger(metin, anahtar string) string {
	for _, m := range fpmDegerRe.FindAllStringSubmatch(metin, -1) {
		if m[1] == anahtar {
			return m[2]
		}
	}
	return ""
}

func fpmSayi(metin, anahtar string, varsayilan int) int {
	v := fpmDeger(metin, anahtar)
	if v == "" {
		return varsayilan
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return varsayilan
	}
	return n
}

func servisAyagaKaldir(servis string) bool {
	unit := map[string]string{
		"nginx": "nginx", "apache": "httpd",
		"phpfpm": "php-fpm", "mariadb": "mariadb",
	}[servis]
	if unit == "" {
		return true // sysctl gibi servissiz hedefler
	}
	_ = execRun("systemctl", "restart", unit)
	for i := 0; i < 20; i++ {
		time.Sleep(1 * time.Second)
		if !ServisAktif(unit) {
			continue
		}
		if servis == "mariadb" {
			out, err := execOutput("mysql", "-N", "-e", "SELECT 1")
			if err != nil || strings.TrimSpace(out) != "1" {
				continue // aktif gorunuyor ama sorgu almiyor
			}
		}
		return true
	}
	return false
}

func etkiTur(servis string) string {
	switch servis {
	case "mariadb":
		return "restart"
	default:
		return "reload"
	}
}

func yedekIDlerStr(m map[string]*YedekKayit) string {
	ids := []string{}
	for _, k := range m {
		ids = append(ids, strconv.FormatInt(k.ID, 10))
	}
	return strings.Join(ids, ",")
}
func yedekYollarStr(m map[string]*YedekKayit) string {
	yollar := []string{}
	for _, k := range m {
		yollar = append(yollar, k.Yedek)
	}
	return strings.Join(yollar, "\n")
}

func yazJSON(w http.ResponseWriter, k int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(k)
	_ = json.NewEncoder(w).Encode(v)
}
func yazHata(w http.ResponseWriter, k int, m string) {
	yazJSON(w, k, map[string]string{"hata": m})
}

// normalize — nil slice'lari bos slice yap (JSON null yerine [] donsun).
func normalize(a *ServisAnaliz) *ServisAnaliz {
	if a.Oneriler == nil {
		a.Oneriler = []Oneri{}
	}
	if a.LogSinyal == nil {
		a.LogSinyal = []string{}
	}
	return a
}

// enrichSonUygulama — ServisAnaliz'e o servisin son 5 uygulama gecmisini ekler.
func (h *Handler) enrichSonUygulama(a *ServisAnaliz) *ServisAnaliz {
	if h.DB == nil {
		return a
	}
	a.SonUygulama = ServisSonUygulamalar(h.DB, a.Kod, 5)
	return a
}
