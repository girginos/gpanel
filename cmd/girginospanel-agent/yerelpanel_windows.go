//go:build windows

// yerelpanel_windows.go — ajanin ikinci yuzu: YEREL WEB PANELI (ilk kesit).
//
// 🔴 MIMARI — IKI KAPI, TEK SUREC:
//
//	8460 — merkezi ajan API'si: gPanel X-Gosp-Jeton basligiyla konusur.
//	       Bu dosya o kapiya DOKUNMAZ; sozlesme main_windows.go'da AYNEN kalir.
//	8443 — yerel panel (bu dosya): gomulu statik UI + /api/yerel/* uclari,
//	       cerez oturumu (admin + bcrypt parola). Panele kayitli OLMAYAN,
//	       tek basina calisan Windows sunucunun tarayici arayuzu.
//
// Iki kapi ayni surecte ve AYNI self-signed TLS sertifikasiyla dinler (tek
// parmak izi: operator iki kapida da ayni kimligi gorur, tek istisna onaylar).
// Servis Execute'u durunca IKI sunucu da kapatilir (bkz. main_windows.go).
// Dinleme adresi GOSP_PANEL_ADRES ortam degiskeniyle ezilebilir; varsayilan
// 0.0.0.0:8443.
package main

import (
	"crypto/rand"
	"crypto/tls"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"girginospanel/internal/platform"
)

const (
	// varsayilanPanelKapi — yerel panelin dinleme adresi (GOSP_PANEL_ADRES ezer).
	varsayilanPanelKapi = "0.0.0.0:8443"
	// oturumCerezAdi — tarayici oturum cerezinin adi.
	oturumCerezAdi = "gosp_win_oturum"
	// oturumOmru — oturumun sunucu tarafindaki gecerlilik suresi (3 saat).
	oturumOmru = 3 * time.Hour
)

// webuiFS — yerel panelin statik arayuzu (ajan/webui dizini: index.html +
// varliklar; dosyalari ayri bir is uretir). go:embed dizini DERLEME aninda
// cozer — webui yoksa build BILEREK patlar: UI'siz bir "panel" artefakti
// sessizce uretilmesin.
//
//go:embed webui
var webuiFS embed.FS

// ── oturum deposu (KALICI + SLIDING) ─────────────────────────────────────────

// oturumlarYolu — oturum tablosunun diske yazilan kopyasi.
// 🔴 KALICI: servis yeniden baslasa (guncelle/cokme/reboot) da oturumlar
// DUSMESIN — operator surekli yeniden giris yapmasin (onceki "bellekte" tasarim
// her restart'ta atiyordu). Jetonlar C:\ProgramData\girginospanel altinda;
// dizin ACL'i SYSTEM+Administrators ile kilitli (E duzeltmesi) → dosya miras alir.
const oturumlarYolu = `C:\ProgramData\girginospanel\oturumlar.json`

// oturumDeposu — jeton -> SON ETKINLIK zamani. gecerliMi SLIDING'dir (her gecerli
// istekte tazelenir) → aktif kullanici ASLA atilmaz; yalniz oturumOmru kadar
// BOSTA kalinirsa duser. Degisiklikler diske yansir: ekle/sil aninda, sliding
// tazelemeler periyodik flush ile (her istekte disk yazmamak icin).
type oturumDeposu struct {
	mu sync.Mutex
	m  map[string]time.Time
}

var oturumlar = oturumDeposu{m: map[string]time.Time{}}

// yukle — acilista diskteki oturumlari belege alir; suresi gecenler ELENIR.
func (d *oturumDeposu) yukle() {
	d.mu.Lock()
	defer d.mu.Unlock()
	b, err := os.ReadFile(oturumlarYolu)
	if err != nil {
		return
	}
	var m map[string]time.Time
	if json.Unmarshal(b, &m) != nil {
		return
	}
	for j, z := range m {
		if time.Since(z) <= oturumOmru {
			d.m[j] = z
		}
	}
}

// kaydet — belegi diske ATOMIK yazar (temp + rename). Kilit CAGIRANDA olmalidir.
// Best-effort: hata giris/cikis akisini BOZMAZ (yalniz kaliciligi kaybeder).
func (d *oturumDeposu) kaydet() {
	b, err := json.Marshal(d.m)
	if err != nil {
		return
	}
	gecici := oturumlarYolu + ".yeni"
	if os.WriteFile(gecici, b, 0o600) == nil {
		_ = os.Rename(gecici, oturumlarYolu)
	}
}

// periyodikKaydet — sliding tazelemelerini periyodik diske yazar (restart'ta
// sliding kaybolmasin). Ayri goroutine olarak baslatilir (sunucuKos).
func (d *oturumDeposu) periyodikKaydet() {
	for range time.Tick(2 * time.Minute) {
		d.mu.Lock()
		d.ayikla()
		d.kaydet()
		d.mu.Unlock()
	}
}

// ayikla — omru dolan oturumlari siler. Kilit CAGIRANDA olmalidir.
func (d *oturumDeposu) ayikla() {
	for j, z := range d.m {
		if time.Since(z) > oturumOmru {
			delete(d.m, j)
		}
	}
}

// ekle — yeni oturumu kaydeder (+ diske). Suresi gecenler GIRISTE ayiklanir ki
// depo sinirsiz buyumesin (basarili giris olmadan depoya kayit dusmez).
func (d *oturumDeposu) ekle(jeton string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ayikla()
	d.m[jeton] = time.Now()
	d.kaydet()
}

func (d *oturumDeposu) gecerliMi(jeton string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	z, ok := d.m[jeton]
	if !ok {
		return false
	}
	if time.Since(z) > oturumOmru {
		delete(d.m, jeton)
		d.kaydet()
		return false
	}
	d.m[jeton] = time.Now() // 🔴 SLIDING: aktif kullanici atilmaz; disk yazimi periyodik (flush)
	return true
}

func (d *oturumDeposu) sil(jeton string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.m, jeton)
	d.kaydet()
}

// oturumJetonu — 32 bayt kriptografik rastgelelik, hex (64 karakter).
func oturumJetonu() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// sahteHash — surec basina uretilen, HICBIR girdiyle eslesmeyen bcrypt hash'i
// (parolasi 32 rastgele bayttir ve uretilir uretilmez atilir). Config'te henuz
// panel hash'i yokken bile bcrypt kiyasi bununla KOSULSUZ yapilir; yoksa
// "hash yok" dali bcrypt suresinden hizli doner, kurulum durumu yanit
// suresinden disaridan olculebilirdi.
var sahteHash = func() []byte {
	p := make([]byte, 32)
	_, _ = rand.Read(p)
	h, err := bcrypt.GenerateFromPassword(p, bcrypt.DefaultCost)
	if err != nil {
		// Olmamali; bozuk bicimli yedek de "asla eslesmez" sartini saglar
		// (CompareHashAndPassword cozumleme hatasiyla doner).
		return []byte("sahte-hash-uretilemedi")
	}
	return h
}()

// ── ortak yardimcilar ───────────────────────────────────────────────────────

// yerelJSON — JSON yanit yazici (merkezi taraftaki yazJSON kapanisinin
// paket duzeyindeki esi; iki kapi ayni govde bicimini konussun).
func yerelJSON(w http.ResponseWriter, kod int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(kod)
	_ = json.NewEncoder(w).Encode(v)
}

// oturumlu — gecerli oturum cerezi olmayan istegi 401 ile keser.
func oturumlu(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(oturumCerezAdi)
		if err != nil || !oturumlar.gecerliMi(c.Value) {
			yerelJSON(w, http.StatusUnauthorized, map[string]string{"hata": "oturum gerekli"})
			return
		}
		h(w, r)
	}
}

// ── kimlik uclari ───────────────────────────────────────────────────────────

// girisUcu — POST /api/yerel/giris {"kullanici","parola"} ucunun kurucusu.
// bootAyar acilista yuklenen config'tir; dosya o an okunamazsa hash icin
// yedek kaynak olur (giris tamamen kilitlenmesin).
func girisUcu(bootAyar ajanAyar) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			yontemHatasi(w, r)
			return
		}
		var ist struct {
			Kullanici string `json:"kullanici"`
			Parola    string `json:"parola"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&ist); err != nil {
			yerelJSON(w, http.StatusBadRequest, map[string]string{"hata": "govde okunamadi"})
			return
		}
		// 🔴 Hash HER DENEMEDE dosyadan taze okunur: `panel-parola` komutu
		// hash'i degistirince servis RESTART BEKLEMEDEN etkisini gosterir.
		guncel, err := ayarYukle()
		if err != nil {
			guncel = bootAyar
		}
		hash := []byte(guncel.PanelParolaHash)
		hashVar := len(hash) > 0
		if !hashVar {
			hash = sahteHash // asla eslesmez; yalniz kiyas SURESINI esitler
		}
		// bcrypt kiyasi KOSULSUZ kosar (kullanici adi yanlis olsa bile):
		// erken donus, yanit suresinden "kullanici mi parola mi" sizdirirdi.
		kiyas := bcrypt.CompareHashAndPassword(hash, []byte(ist.Parola))
		if !hashVar || ist.Kullanici != "admin" || kiyas != nil {
			// 🔴 SABIT 500ms gecikme — kaba kuvveti yavaslatir; sure her
			// basarisizlikta AYNI oldugundan basarisizligin nedeni zamanlamayla
			// da ele verilmez. Mesaj da bilerek TEK: kullanici adinin mi
			// parolanin mi hatali oldugu SOYLENMEZ.
			time.Sleep(500 * time.Millisecond)
			log.Printf("yerel panel: BASARISIZ giris denemesi (%s)", r.RemoteAddr)
			yerelJSON(w, http.StatusUnauthorized, map[string]string{"hata": "kullanici adi veya parola hatali"})
			return
		}
		jeton, err := oturumJetonu()
		if err != nil {
			yerelJSON(w, http.StatusInternalServerError, map[string]string{"hata": "oturum jetonu uretilemedi"})
			return
		}
		oturumlar.ekle(jeton)
		// Cerez: HttpOnly (XSS oturumu okuyamaz) + Secure (yalniz HTTPS; kapi
		// zaten TLS) + SameSite=Strict (baska origin'den gelen istek cerezi
		// TASIMAZ — CSRF savunmasi). MaxAge = oturumOmru (3 saat): cerez
		// tarayici/sekme kapansa da 3 saat KALIR — kisa aradan sonra tekrar
		// giris istenmez; sunucu tarafi TTL (gecerliMi) ile ayni omur.
		http.SetCookie(w, &http.Cookie{
			Name:     oturumCerezAdi,
			Value:    jeton,
			Path:     "/",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   int(oturumOmru / time.Second),
		})
		log.Printf("yerel panel: admin girisi yapildi (%s)", r.RemoteAddr)
		yerelJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// cikisUcu — POST /api/yerel/cikis: oturumu hem depodan hem tarayicidan
// dusurur. Gecerli oturum SART DEGIL (cikis idempotenttir; suresi dolmus
// cerezle bile cagrilabilmeli).
func cikisUcu(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		yontemHatasi(w, r)
		return
	}
	if c, err := r.Cookie(oturumCerezAdi); err == nil {
		oturumlar.sil(c.Value)
	}
	// MaxAge -1: tarayiciya cerezi HEMEN silmesini soyler.
	http.SetCookie(w, &http.Cookie{
		Name:     oturumCerezAdi,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	yerelJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── veri uclari (hepsi oturumlu) ────────────────────────────────────────────

// ozetUcu — GET /api/yerel/ozet: panel ust bilgisi icin makine ozeti.
// Alan adlari merkezi /saglik ile hizali (surum/kanal/yetenekler/sinanmis_mi).
func ozetUcu(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		yontemHatasi(w, r)
		return
	}
	ad, _ := os.Hostname() // hata durumunda bos kalir; ozet yine doner
	yet := platform.Aktif().Yetenekler()
	yerelJSON(w, http.StatusOK, map[string]any{
		"hostname":    ad,
		"surum":       platform.Surum,
		"kanal":       platform.Kanal,
		"yetenekler":  uint32(yet),
		"sinanmis_mi": yet.Var(platform.YetSite),
	})
}

// sitelerUcu — IIS site listesi / acma / silme.
//
//	GET                  -> {"siteler":[{"ad","durum","baglama"}]}
//	POST   {"alan_adi"}  -> platform.SiteOlustur; kendini-sina MUHUR KAPISI
//	                        platform icinde dogal isler: sinanmamis hostta 422
//	DELETE ?ad=<alan>    -> platform.SiteSil; SistemKullanici BOS gecilir,
//	                        platform alan adindan kendisi turetir
func sitelerUcu(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		siteler, err := platform.SiteListe()
		if err != nil {
			hataYaz(w, r, http.StatusInternalServerError, err)
			return
		}
		yerelJSON(w, http.StatusOK, map[string]any{"siteler": siteler})
	case http.MethodPost:
		var ist struct {
			AlanAdi string `json:"alan_adi"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&ist); err != nil {
			govdeHatasi(w, r)
			return
		}
		son, err := platform.Aktif().SiteOlustur(platform.SiteIstek{AlanAdi: ist.AlanAdi})
		if err != nil {
			hataYaz(w, r, http.StatusUnprocessableEntity, err)
			return
		}
		log.Printf("yerel panel: site acildi: %s (%s)", ist.AlanAdi, son.SistemKullanici)
		// Govde merkezi POST /site ile ayni sozlesme (SiteSonuc, etiketsiz).
		yerelJSON(w, http.StatusOK, son)
	case http.MethodDelete:
		ad := r.URL.Query().Get("ad")
		if ad == "" {
			zarfYaz(w, r, http.StatusBadRequest, KodGecersizIstek, "ad parametresi gerekli", false)
			return
		}
		if err := platform.Aktif().SiteSil(platform.SiteKimlik{AlanAdi: ad}); err != nil {
			hataYaz(w, r, http.StatusUnprocessableEntity, err)
			return
		}
		log.Printf("yerel panel: site silindi: %s", ad)
		_ = platform.PlanSiteTemizle(ad) // öksüz plan atamasini temizle (denetim bulgusu)
		yerelJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		yontemHatasi(w, r)
	}
}

// olaylarUcu — GET /api/yerel/olaylar?gunluk=&adet= — govde ve hata kodlari
// merkezi /olaylar ile BIREBIR ayni ({"olaylar":[...]}, gecersiz istek 400).
func olaylarUcu(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		yontemHatasi(w, r)
		return
	}
	adet := 50
	if v := r.URL.Query().Get("adet"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			zarfYaz(w, r, http.StatusBadRequest, KodGecersizIstek, "adet sayi olmali", false)
			return
		}
		adet = n
	}
	olaylar, err := platform.OlaylariOku(r.URL.Query().Get("gunluk"), adet)
	if err != nil {
		hataYaz(w, r, http.StatusInternalServerError, err)
		return
	}
	yerelJSON(w, http.StatusOK, map[string]any{"olaylar": olaylar})
}

// gorevlerUcu — GET /api/yerel/gorevler?tumu=0|1 — govde merkezi /gorevler
// ile BIREBIR ayni ({"gorevler":[...]}).
func gorevlerUcu(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		yontemHatasi(w, r)
		return
	}
	gorevler, err := platform.GorevleriOku(r.URL.Query().Get("tumu") == "1")
	if err != nil {
		hataYaz(w, r, http.StatusInternalServerError, err)
		return
	}
	yerelJSON(w, http.StatusOK, map[string]any{"gorevler": gorevler})
}

// ── kurulum katalogu uclari (hepsi oturumlu) ────────────────────────────────

// katalogUcu — GET /api/yerel/katalog: kurulabilir yazilim katalogu + her
// kalemin kurulu/kurulabilir durumu. Kurulum sihirbazinin ilk adimi bu
// listeyi cizer; kademe etiketi (guvenilir/deneysel/karar-bekliyor) UI'da
// AYNEN gosterilir.
func katalogUcu(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		yontemHatasi(w, r)
		return
	}
	yerelJSON(w, http.StatusOK, map[string]any{"kalemler": platform.KatalogDurumu()})
}

// katalogKurUcu — POST /api/yerel/katalog/kur {"anahtar":"ftp"}: kurulumu
// ASENKRON baslatir, 202 + is kimligi doner. 409 = baska kurulum suruyor
// (tek-ucus kurali platform katindadir, burada yalniz koda cevrilir);
// 422 = kalem kurulamaz (taninmiyor / zaten kurulu / karar-bekliyor).
func katalogKurUcu(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		yontemHatasi(w, r)
		return
	}
	var ist struct {
		Anahtar string `json:"anahtar"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&ist); err != nil {
		govdeHatasi(w, r)
		return
	}
	id, err := platform.Baslat(ist.Anahtar)
	if err != nil {
		hataYaz(w, r, http.StatusUnprocessableEntity, err)
		return
	}
	log.Printf("yerel panel: kurulum baslatildi: %s (is %s)", ist.Anahtar, id)
	yerelJSON(w, http.StatusAccepted, map[string]string{"is_id": id})
}

// katalogIsUcu — GET /api/yerel/katalog/is?id=X: is durumu + gunluk satirlari.
// Sihirbaz bitti=true gorene kadar bu ucu yoklar; gunluk her yanitla komple
// doner (tavan platform katinda 500 satir).
func katalogIsUcu(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		yontemHatasi(w, r)
		return
	}
	g, ok := platform.IsDurumu(r.URL.Query().Get("id"))
	if !ok {
		yerelJSON(w, http.StatusNotFound, map[string]string{"hata": "is bulunamadi"})
		return
	}
	yerelJSON(w, http.StatusOK, map[string]any{"durum": g.Durum, "bitti": g.Bitti, "log": g.Log, "ilerleme": g.Ilerleme})
}

// katalogIsAkisUcu — GET /api/yerel/katalog/is-akis?id=X: is durumunu GERCEK
// ZAMANLI akitir (Server-Sent Events). Sihirbaz bunu tercih eder; ~500 ms'de
// bir anlik goruntu (durum + gunluk + yapisal ilerleme) gonderir, is bitince
// son goruntuyu yollayip akisi kapatir. Yoklama ucu (katalog/is) FALLBACK olarak
// durur: EventSource desteklenmiyorsa ya da akis kurulamazsa sihirbaz ona doner.
//
// 🔴 Bu uc bir MUTASYON DEGIL (yalniz okur) — bu yuzden denetimli() ile
// sarilmaz; 30 dakikalik acik baglanti "sure=1800000ms" gibi yaniltici bir
// denetim satiri uretirdi. Sunucuda WriteTimeout yok, uzun akis guvenli.
func katalogIsAkisUcu(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		yontemHatasi(w, r)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		yerelJSON(w, http.StatusInternalServerError, map[string]string{"hata": "akis desteklenmiyor"})
		return
	}
	id := r.URL.Query().Get("id")
	if _, ok := platform.IsDurumu(id); !ok {
		yerelJSON(w, http.StatusNotFound, map[string]string{"hata": "is bulunamadi"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // araya giren proxy tamponlamasin
	w.WriteHeader(http.StatusOK)

	// yolla — anlik goruntuyu tek SSE olayi olarak yazar; is bittiyse true doner.
	yolla := func() bool {
		g, ok := platform.IsDurumu(id)
		if !ok {
			return true // is kayboldu (olmamali) — akisi kapat
		}
		b, err := json.Marshal(map[string]any{"durum": g.Durum, "bitti": g.Bitti, "log": g.Log, "ilerleme": g.Ilerleme})
		if err != nil {
			return true
		}
		if _, err := w.Write([]byte("data: " + string(b) + "\n\n")); err != nil {
			return true // istemci gitti
		}
		fl.Flush()
		return g.Bitti
	}
	if yolla() {
		return // zaten bitmis: tek olay + kapan
	}
	tik := time.NewTicker(500 * time.Millisecond)
	defer tik.Stop()
	for {
		select {
		case <-r.Context().Done():
			return // istemci baglantisini kapatti
		case <-tik.C:
			if yolla() {
				return
			}
		}
	}
}

// ── statik arayuz + sunucu kurucusu ─────────────────────────────────────────

// statikUc — gomulu webui'yi kok / altindan sunar (oturum SARTI YOK: giris
// sayfasinin kendisi de buradan yuklenir; veri uclarinin tamami oturumludur).
//
// 🔴 SPA YEDEGI YOK: bilinmeyen yol index.html'e DUSMEZ, 404 doner — tek
// sayfa zaten index.html; her yolu index'e cevirmek /api yazim hatalarini
// bile HTML sayfayla maskelerdi. Kok disinda dizin listelemesi de kapali.
func statikUc() http.Handler {
	alt, err := fs.Sub(webuiFS, "webui")
	if err != nil {
		// Olmamali ("webui" sabit ve gecerli ad) — panik yerine aciklayici yanit.
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "gomulu arayuz acilamadi", http.StatusInternalServerError)
		})
	}
	dosyalar := http.FileServer(http.FS(alt))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			yontemHatasi(w, r)
			return
		}
		if r.URL.Path != "/" && strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		// 🔴 NO-CACHE ZORUNLU: gomulu statik dosyalar (index.html/uygulama.js/
		// stil.css) surumsuz sabit adlarla sunuluyor. Cache basligi olmadan
		// tarayici bunlari kendi sezgisiyle saklar ve ajan guncellense bile ESKI
		// JS'i calistirir — "giris basarili ama panele gecmiyor" tam bu yuzden
		// yasandi (2026-09-04). Ajan bir sonraki surumde her aciliste TAZE gelsin.
		// 🔴 Guvenlik basliklari: clickjacking + MIME-sniff + <base> enjeksiyonu
		// savunmasi. NOT: default-src/script-src BILEREK yok — webui inline script
		// kullaniyor olabilir (dogrulanmadan strict CSP paneli kirar); framing/
		// object/base guvenli alt kume.
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'; object-src 'none'; base-uri 'none'")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		dosyalar.ServeHTTP(w, r)
	})
}

// yerelPanelSunucu — 8443 yerel panel sunucusunu KURAR; dinlemeyi baslatan
// sunucuKos'tur (main_windows.go). ayar acilistaki config'tir (giris ucunda
// yalniz dosya okunamazsa yedek hash kaynagi); sert 8460 ile AYNI TLS
// sertifikasidir.
func yerelPanelSunucu(ayar ajanAyar, sert tls.Certificate) *http.Server {
	adres := os.Getenv("GOSP_PANEL_ADRES")
	if adres == "" {
		adres = varsayilanPanelKapi
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/yerel/giris", girisUcu(ayar))
	mux.HandleFunc("/api/yerel/cikis", cikisUcu)
	mux.HandleFunc("/api/yerel/ozet", oturumlu(ozetUcu))
	mux.HandleFunc("/api/yerel/siteler", oturumlu(denetimli("siteler", sitelerUcu)))
	mux.HandleFunc("/api/yerel/olaylar", oturumlu(olaylarUcu))
	mux.HandleFunc("/api/yerel/gorevler", oturumlu(gorevlerUcu))
	mux.HandleFunc("/api/yerel/katalog", oturumlu(katalogUcu))
	mux.HandleFunc("/api/yerel/katalog/kur", oturumlu(denetimli("katalog-kur", katalogKurUcu)))
	mux.HandleFunc("/api/yerel/katalog/is", oturumlu(katalogIsUcu))
	mux.HandleFunc("/api/yerel/katalog/is-akis", oturumlu(katalogIsAkisUcu)) // SSE: gercek zamanli ilerleme
	// Ayri dosyalarda tanimli uc gruplari (site/servis + veritabani); imzalar
	// bilerek disaridan baglanir ki o dosyalar bu dosyaya dokunmadan eklensin.
	OpsUclariniKaydet(mux, oturumlu, yerelJSON)
	VeriUclariniKaydet(mux, oturumlu, yerelJSON)
	PlanUclariniKaydet(mux, oturumlu, yerelJSON)
	AyarUclariniKaydet(mux, oturumlu, yerelJSON)
	mux.Handle("/", statikUc())

	return &http.Server{
		Addr:    adres,
		Handler: mux,
		TLSConfig: &tls.Config{
			// 🔴 TLS 1.2 taban — merkezi kapiyla ayni cizgi.
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{sert},
		},
	}
}
