package files

// arsiv.go — Asenkron arşivleme (zip / tar.gz) + ilerleme takibi.
//
// Eskiden Archive senkrondu: 10 GB'lık bir public_html'i zip'lerken HTTP isteği
// dakikalarca askıda kalır, kullanıcı hiçbir geri bildirim görmez, araya giren
// proxy timeout'u isteği düşürünce iş yarıda kalmış gibi görünürdü (aslında zip
// arka planda sürüyordu). Artık iş goroutine'de yürür; yanıt hemen is_id döner,
// UI /files/archive-progress ile ilerlemeyi poll eder.
//
// İkinci düzeltme: eski sürüm zip/tar'a MUTLAK yol veriyordu → zip "removing
// leading /" deyip arşivin içine home/c_xxx/public_html/... derin ağacını
// gömüyordu. Artık ortak dizinden (cmd.Dir) göreli isimlerle arşivlenir.
//
// 🔴 GÜVENLİK — DOSYA ADLARI ARGV'YE KONMAZ:
// Göreli isimler argv'ye eklenirse "-" ile başlayan bir DOSYA ADI zip/tar
// tarafından SEÇENEK olarak yorumlanır. Panel root çalıştığı için bu, paylaşımlı
// düğümde root komut çalıştırma demekti:
//     tenant "--checkpoint=1" ve "--checkpoint-action=exec=curl x|sh" adlı iki
//     dosya oluşturur, ikisini seçip tar.gz ister → tar komutu çalıştırır.
// Bu yüzden adlar argv'ye HİÇ konmaz; listeler STDIN'den verilir:
//     zip -@            → satır satır ad okur, seçenek ayrıştırmaz
//     tar --files-from=- --verbatim-files-from → satırı olduğu gibi ad sayar
// (--verbatim-files-from şarttır: onsuz "-" ile başlayan satır yine seçenektir.)
//
// 🔴 GÜVENLİK — SYMLINK: zip varsayılanı bağı İZLEYİP hedefin içeriğini arşive
// koymaktır. Root çalışırken tenant'ın public_html'ine koyduğu "ln -s /etc/shadow"
// arşive gerçek içerikle girerdi. "-y" ile bağlar bağ olarak saklanır. GNU tar
// zaten "-h" verilmedikçe bağı izlemez.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"girginospanel/internal/httpx"
)

// arsivIs: asenkron arşivleme işinin ilerleme durumu.
type arsivIs struct {
	mu      sync.Mutex
	Durum   string // "calisiyor" | "tamam" | "hata"
	Toplam  int    // arşivlenecek üye sayısı (0 = sayılıyor / sayım atlandı)
	Eklenen int    // şu ana dek arşive eklenen üye
	Boyut   int64  // çıktı arşivinin o anki boyutu (akıcı gösterge)
	Cikti   string // panel-göreli çıktı yolu
	Hata    string
	sahip   string // tenant sistem kullanıcısı — ilerlemeyi yalnız sahibi görsün
	bitti   time.Time
}

var (
	arsivIslerMu sync.Mutex
	arsivIsler   = map[string]*arsivIs{}
)

// Eşzamanlılık tavanı: her POST koşulsuz bir root zip/tar doğursaydı, tek bir
// tenant 50 istekle düğümün CPU/IO'sunu tüketip TÜM müşterileri düşürebilirdi.
// (Senkron sürümde HTTP bağlantı sınırı doğal fren görevi görüyordu.)
const (
	arsivGlobalTavan = 4 // aynı anda tüm panelde
	arsivTenantTavan = 1 // aynı anda tek tenant
	arsivIsTavan     = 500
)

var (
	arsivGlobalSem = make(chan struct{}, arsivGlobalTavan)
	arsivTenantSem sync.Map // sk -> chan struct{}
)

func arsivTenantKanal(sk string) chan struct{} {
	v, _ := arsivTenantSem.LoadOrStore(sk, make(chan struct{}, arsivTenantTavan))
	return v.(chan struct{})
}

// arsivIsTemizle: bitmiş işleri 10 dk sonra haritadan düşür.
func arsivIsTemizle() {
	arsivIslerMu.Lock()
	defer arsivIslerMu.Unlock()
	for id, is := range arsivIsler {
		is.mu.Lock()
		// "calisiyor"da takılı kalmış kayıtlar da (panik yolu) sonunda düşsün:
		// aksi halde harita kalıcı sızar.
		eski := !is.bitti.IsZero() && time.Since(is.bitti) > 10*time.Minute
		is.mu.Unlock()
		if eski {
			delete(arsivIsler, id)
		}
	}
}

// ArchiveProgress: GET .../files/archive-progress?id=<is_id>
func (h *Handlers) ArchiveProgress(w http.ResponseWriter, r *http.Request) {
	_, sk, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	id := r.URL.Query().Get("id")
	arsivIslerMu.Lock()
	is := arsivIsler[id]
	arsivIslerMu.Unlock()
	// Sahip kontrolü: is_id sızarsa (log, proxy kaydı, tarayıcı geçmişi) başka
	// bir tenant yabancı işin yolunu ve ilerlemesini okuyamasın.
	if is == nil || is.sahip != sk {
		httpx.WriteError(w, http.StatusNotFound, "iş bulunamadı (bitmiş olabilir)")
		return
	}
	is.mu.Lock()
	resp := map[string]any{
		"durum":   is.Durum,
		"toplam":  is.Toplam,
		"eklenen": is.Eklenen,
		"boyut":   is.Boyut,
		"cikti":   is.Cikti,
		"hata":    is.Hata,
	}
	is.mu.Unlock()
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// ----- Arşivle (seçili dosyaları zip/tar.gz yap) -----

type archiveReq struct {
	Kaynaklar []string `json:"kaynaklar"`
	CiktiYol  string   `json:"cikti_yol"` // örn /public_html/yedek.zip
	Format    string   `json:"format"`    // zip | tar.gz
}

func (h *Handlers) Archive(w http.ResponseWriter, r *http.Request) {
	home, sk, err := h.home(r)
	if err != nil {
		httpx.WriteError(w, statusFromErr(err), err.Error())
		return
	}
	var req archiveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if len(req.Kaynaklar) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "kaynak yok")
		return
	}
	if req.Format == "" {
		req.Format = "zip"
	}
	if req.Format != "zip" && req.Format != "tar.gz" {
		httpx.WriteError(w, http.StatusBadRequest, "desteklenmeyen format (zip, tar.gz)")
		return
	}
	// Uzantı ↔ format uyumu: aksi halde {"cikti_yol":"/public_html/index.php"}
	// ile mevcut bir PHP dosyasının üzerine arşiv yazılabilirdi.
	bekUzanti := ".zip"
	if req.Format == "tar.gz" {
		bekUzanti = ".tar.gz"
	}
	if !strings.HasSuffix(strings.ToLower(req.CiktiYol), bekUzanti) {
		httpx.WriteError(w, http.StatusBadRequest, "çıktı adı "+bekUzanti+" ile bitmeli")
		return
	}
	ciktiAbs, err := jailJoinStrict(home, req.CiktiYol)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "cikti: "+err.Error())
		return
	}
	// Var olan dosyayı sessizce yok etme (zip'te "update" semantiği ayrıca
	// kafa karıştırıcı sonuç üretir: eski üyeler arşivde kalır).
	if _, serr := os.Lstat(ciktiAbs); serr == nil {
		httpx.WriteError(w, http.StatusConflict, "bu adda bir dosya zaten var")
		return
	}
	if err := os.MkdirAll(filepath.Dir(ciktiAbs), 0755); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "hedef dizin: "+err.Error())
		return
	}

	var abslar []string
	for _, k := range req.Kaynaklar {
		kAbs, jerr := jailJoinStrict(home, k)
		if jerr != nil {
			continue
		}
		if kAbs == ciktiAbs {
			continue // kendini arşivleme
		}
		abslar = append(abslar, kAbs)
	}
	if len(abslar) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "geçerli kaynak yok")
		return
	}

	// Ortak dizin: hepsi aynı dizindeyse oradan göreli arşivle (arşivin içinde
	// gereksiz derin yol olmaz). Karışıksa home kökünden.
	calisma := filepath.Dir(abslar[0])
	for _, a := range abslar {
		if filepath.Dir(a) != calisma {
			calisma = home
			break
		}
	}
	// calisma HER ZAMAN home altında kalmalı: tek kaynak ev kökünün kendisiyse
	// filepath.Dir onu /home'a çıkarırdı (bugün zararsız ama jail dışı bir
	// çalışma dizini bırakmanın hiçbir gerekçesi yok).
	if calisma != home && !strings.HasPrefix(calisma, home+string(os.PathSeparator)) {
		calisma = home
	}
	adlar := make([]string, 0, len(abslar))
	for _, a := range abslar {
		rel, rerr := filepath.Rel(calisma, a)
		if rerr != nil || rel == "." || strings.HasPrefix(rel, "..") {
			adlar = append(adlar, a) // güvenli geri düşüş: mutlak yol
			continue
		}
		adlar = append(adlar, rel)
	}

	arsivIsTemizle()
	arsivIslerMu.Lock()
	dolu := len(arsivIsler) >= arsivIsTavan
	arsivIslerMu.Unlock()
	if dolu {
		httpx.WriteError(w, http.StatusServiceUnavailable, "çok fazla arşiv işi var, birazdan tekrar deneyin")
		return
	}

	isID := extractIsID() // aynı crypto/rand kimlik üreteci (tahmin edilemez)
	is := &arsivIs{Durum: "calisiyor", Cikti: req.CiktiYol, sahip: sk}
	arsivIslerMu.Lock()
	arsivIsler[isID] = is
	arsivIslerMu.Unlock()

	basarisiz := func(msg string) {
		is.mu.Lock()
		is.Durum = "hata"
		is.Hata = msg
		is.bitti = time.Now()
		is.mu.Unlock()
	}

	go func() {
		// 🔴 PANIC KORUMASI: bare `go func()` içindeki panik tüm çok-tenant
		// paneli çökertirdi; paniği "hata" işine indir.
		defer func() {
			if p := recover(); p != nil {
				basarisiz(fmt.Sprintf("arşivleme iç hatası: %v", p))
			}
		}()
		// Kuyruk: tenant başına 1, panel genelinde arsivGlobalTavan.
		ts := arsivTenantKanal(sk)
		ts <- struct{}{}
		defer func() { <-ts }()
		arsivGlobalSem <- struct{}{}
		defer func() { <-arsivGlobalSem }()

		arsivYurut(is, basarisiz, req.Format, ciktiAbs, calisma, adlar, abslar, sk)
	}()

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "is_id": isID, "cikti_yol": req.CiktiYol,
	})
}

// arsivSayimTavan: bu kadar üyeden sonra saymayı bırak. Sayım tüm ağacı bir kez
// daha yürür; 500k dosyalı bir docroot'ta işi iki katına çıkarır ve UI o süre
// boyunca "sayılıyor…" der. Tavan aşılırsa payda bilinmez (belirsiz gösterge).
const arsivSayimTavan = 200_000

// arsivDosyaSay: arşive girecek ÜYE sayısı (ilerleme paydası).
// Dizinler de sayılır: hem zip hem tar her dizin için ayrı bir üye satırı basar
// ("adding: alt1/"), yalnız dosya sayılsaydı ilerleme paydayı aşardı (803/800).
// 0 dönerse payda bilinmiyor demektir.
func arsivDosyaSay(abslar []string) int {
	n := 0
	for _, a := range abslar {
		st, err := os.Lstat(a)
		if err != nil {
			continue
		}
		if !st.IsDir() {
			n++
			continue
		}
		asildi := false
		_ = filepath.WalkDir(a, func(_ string, _ fs.DirEntry, werr error) error {
			if werr != nil {
				return nil // okunamayan alt ağaç sayımı bozmasın
			}
			n++ // kökün kendisi dahil her üye
			if n > arsivSayimTavan {
				asildi = true
				return filepath.SkipAll
			}
			return nil
		})
		if asildi {
			return 0
		}
	}
	return n
}

// arsivYurut: zip/tar'ı çalıştırır, çıktısını satır satır okuyup ilerlemeyi işler.
func arsivYurut(is *arsivIs, basarisiz func(string), format, ciktiAbs, calisma string, adlar, abslar []string, sk string) {
	toplam := arsivDosyaSay(abslar)
	is.mu.Lock()
	is.Toplam = toplam
	is.mu.Unlock()

	var cmd *exec.Cmd
	if format == "zip" {
		// -@ : dosya adlarını STDIN'den oku (argv'ye konmaz → seçenek enjeksiyonu yok)
		// -y : sembolik bağı İZLEME, bağ olarak sakla (jail dışı içerik sızmasın)
		// -q YOK: "adding: <yol>" satırlarını sayarak ilerleme çıkarıyoruz.
		cmd = exec.Command("zip", "-r", "-y", ciktiAbs, "-@")
	} else {
		// --verbatim-files-from: listedeki satır "-" ile başlasa bile ad sayılır.
		// (Bu bayrak olmadan --files-from girdisi seçenek olarak yorumlanır.)
		cmd = exec.Command("tar", "-czvf", ciktiAbs, "--verbatim-files-from", "--files-from=-")
	}
	cmd.Dir = calisma
	cmd.Stdin = strings.NewReader(strings.Join(adlar, "\n") + "\n")

	// stdout+stderr tek boruya: zip ilerlemeyi stdout'a, tar -v listeyi stdout'a,
	// uyarıları stderr'e yazar. Tek okuyucu ile ikisini de tarıyoruz.
	pr, pw, perr := os.Pipe()
	if perr != nil {
		basarisiz("boru açılamadı: " + perr.Error())
		return
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		basarisiz(format + ": başlatılamadı: " + err.Error())
		return
	}
	pw.Close() // yazan uç sadece çocukta kalsın, yoksa Scanner hiç EOF görmez

	// Panik yolu dahil her çıkışta: boru kapansın, çocuk süreç reap edilsin,
	// ticker dursun. Aksi halde zombi süreç + sonsuz ticker goroutine'i kalırdı.
	bittiSinyal := make(chan struct{})
	var kapatBir sync.Once
	kapat := func() {
		kapatBir.Do(func() {
			pr.Close()
			close(bittiSinyal)
		})
	}
	defer kapat()
	var beklendi sync.Once
	var waitErr error
	bekle := func() error {
		beklendi.Do(func() { waitErr = cmd.Wait() })
		return waitErr
	}
	defer func() { _ = bekle() }()

	// Boyut izleyici: satır tamponlaması yüzünden "eklenen" sıçramalı ilerler;
	// çıktı dosyasının boyutu ise her saniye kesin ölçülür → akıcı gösterge.
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-bittiSinyal:
				return
			case <-t.C:
				if st, err := os.Stat(ciktiAbs); err == nil {
					is.mu.Lock()
					is.Boyut = st.Size()
					is.mu.Unlock()
				}
			}
		}
	}()

	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024) // uzun dosya adları satırı taşırmasın
	var sonSatirlar []string
	eklenen := 0
	for sc.Scan() {
		s := strings.TrimRight(sc.Text(), "\r\n")
		if strings.TrimSpace(s) == "" {
			continue
		}
		sayilir := false
		if format == "zip" {
			sayilir = strings.Contains(s, "adding:") || strings.Contains(s, "updating:")
		} else {
			// tar -v her üye için bir satır basar; tanılama satırları "tar:" ile başlar.
			sayilir = !strings.HasPrefix(s, "tar:")
		}
		if sayilir {
			eklenen++
			// İlk 25 üyede de çubuk kıpırdasın (küçük arşivlerde donuk durmasın).
			if eklenen < 25 || eklenen%25 == 0 || eklenen == toplam {
				is.mu.Lock()
				is.Eklenen = eklenen
				is.mu.Unlock()
			}
		} else {
			// tanılama satırı: hata mesajı için son 12 satırı sakla
			sonSatirlar = append(sonSatirlar, s)
			if len(sonSatirlar) > 12 {
				sonSatirlar = sonSatirlar[1:]
			}
		}
	}
	// Tarayıcı hatası (ör. 1 MiB'ı aşan satır) sessizce "bitti" gibi görünürdü;
	// üstelik boruyu kapatmak çocuğa EPIPE verip neredeyse biten arşivi
	// SİLDİRİRDİ. Böyle bir durumda arşivi silme, dürüst hata ver.
	tarayiciHatasi := sc.Err()
	kapat()
	err := bekle()

	is.mu.Lock()
	is.Eklenen = eklenen
	is.mu.Unlock()

	if tarayiciHatasi != nil && !errors.Is(tarayiciHatasi, os.ErrClosed) {
		basarisiz("arşiv çıktısı okunamadı: " + tarayiciHatasi.Error() + " (arşiv diskte bırakıldı)")
		return
	}
	if err != nil {
		msg := strings.TrimSpace(strings.Join(sonSatirlar, "; "))
		if msg == "" {
			msg = err.Error()
		}
		// Yarım kalan arşiv bırakma: bozuk dosya "yedeğim var" yanılgısı yaratır.
		_ = os.Remove(ciktiAbs)
		basarisiz(format + ": " + msg)
		return
	}

	_, _ = exec.Command("chown", sk+":"+sk, ciktiAbs).CombinedOutput()
	_, _ = exec.Command("restorecon", ciktiAbs).CombinedOutput()

	var boyut int64
	if st, serr := os.Stat(ciktiAbs); serr == nil {
		boyut = st.Size()
	}
	is.mu.Lock()
	is.Durum = "tamam"
	is.Boyut = boyut
	is.bitti = time.Now()
	is.mu.Unlock()
}
