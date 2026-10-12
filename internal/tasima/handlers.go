package tasima

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"girginospanel/internal/domains"
	"girginospanel/internal/httpx"
	"girginospanel/internal/plans"
)

// logDizin — iş günlükleri (testler geçici dizine çevirir).
var logDizin = "/opt/girginospanel/logs"

// Ayni anda yalnizca TEK tasima isi calisir: rsync/mysqldump agir islerdir,
// paralel calisirsa sunucuyu bogar ve ayni sistem kullanicisi uzerinde yaris
// olusur. Yeni isler KUYRUGA yazilir (kuyruk.go); tek dagitici sirayla calistirir.
var (
	kilit      sync.Mutex
	aktifIsID  int64
	aktifIptal context.CancelFunc
)

func logYolu(isID int64) string {
	return filepath.Join(logDizin, fmt.Sprintf("tasima-%d.log", isID))
}

// isBulunamadi — kapsam dışı ve var olmayan iş için BAYT BAYT aynı yanıt (IDOR:
// başka kiracının iş kimliği varlığını sızdırmaz).
func isBulunamadi(w http.ResponseWriter) {
	httpx.WriteError(w, http.StatusNotFound, "iş bulunamadı")
}

// kapsamYaz — kapsam/hız/anahtar reddini yazar; true = devam edilebilir.
func kapsamHatasi(w http.ResponseWriter, kod int, msj string) bool {
	if kod != 0 {
		httpx.WriteError(w, kod, msj)
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Baglanti testi + kesif
// ---------------------------------------------------------------------------

type kaynakGirdi struct {
	Tip       string `json:"tip"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Kullanici string `json:"kullanici"`
	Parola    string `json:"parola"`
	Anahtar   string `json:"anahtar"`
	// PanelAnahtari: true ise panelin taşıma anahtarı kullanılır (parola/kullanıcı
	// anahtarı yerine). Anahtar KİRACIYA ÖZELDİR: kök kendi çiftini, her bayi kendi
	// çiftini kullanır (bkz. panelTasimaAnahtar) — bayi, kökün anahtarını eklediği
	// sunuculara bu yolla bağlanamaz.
	PanelAnahtari bool `json:"panel_anahtari"`
}

func (g kaynakGirdi) kaynak(kp Kapsam) *Kaynak {
	p := g.Port
	if p == 0 {
		p = 22
	}
	parola, anahtar := g.Parola, g.Anahtar
	if g.PanelAnahtari {
		if priv, _, err := panelTasimaAnahtar(kp); err == nil {
			anahtar = priv // kiracının özel anahtarı → key-auth (parola devre dışı)
			parola = ""
		}
	}
	return &Kaynak{
		Tip: strings.ToLower(strings.TrimSpace(g.Tip)), Host: g.Host, Port: p,
		Kullanici: g.Kullanici, Parola: parola, Anahtar: anahtar,
	}
}

// ── Panel taşıma SSH anahtarı (parola çalışmadığında otomatik yol) ──────────
// Kök: <tasimaKeyDir>/id_ed25519 (eski yol — kökün kaynaklarındaki yetki bozulmaz).
// Bayi: <tasimaKeyDir>/<rid>/id_ed25519.
var tasimaKeyDir = "/var/lib/girginospanel/tasima"

var tasimaKeyMu sync.Mutex

// sshKeygen — anahtar üretici (testler değiştirir).
var sshKeygen = func(yol, yorum string) ([]byte, error) {
	return exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-C", yorum, "-f", yol).CombinedOutput()
}

// panelTasimaAnahtar — kapsamın taşıma ed25519 anahtar çiftini garanti eder (yoksa
// üretir) ve (özel-anahtar-içeriği, public-anahtar-satırı) döner.
func panelTasimaAnahtar(kp Kapsam) (priv string, pub string, err error) {
	tasimaKeyMu.Lock()
	defer tasimaKeyMu.Unlock()
	dizin := kiraciDizin(kp)
	if err = os.MkdirAll(dizin, 0o700); err != nil {
		return "", "", err
	}
	yol := filepath.Join(dizin, "id_ed25519")
	yorum := "girginospanel-tasima"
	if kp.Bayi() {
		yorum = fmt.Sprintf("girginospanel-tasima-r%d", kp.RID)
	}
	if _, e := os.Lstat(yol); e != nil {
		out, ge := sshKeygen(yol, yorum)
		if ge != nil {
			return "", "", fmt.Errorf("anahtar üretilemedi: %s", strings.TrimSpace(string(out)))
		}
	}
	pb, err := dosyaOkuBagsiz(yol)
	if err != nil {
		return "", "", err
	}
	pubB, err := dosyaOkuBagsiz(yol + ".pub")
	if err != nil {
		return "", "", err
	}
	return string(pb), strings.TrimSpace(string(pubB)), nil
}

// dosyaOkuBagsiz — sembolik bağ izlemeden düzenli dosya okur (anahtar dizini).
func dosyaOkuBagsiz(yol string) ([]byte, error) {
	f, err := os.OpenFile(yol, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 64<<10 {
		return nil, fmt.Errorf("anahtar dosyası geçersiz")
	}
	b := make([]byte, st.Size())
	n, err := f.Read(b)
	if err != nil && n == 0 {
		return nil, err
	}
	return b[:n], nil
}

// PanelAnahtar: GET /tasima/anahtar → {pubkey, komut}. Kiracıya özel çift.
func (h *Handlers) PanelAnahtar(w http.ResponseWriter, r *http.Request) {
	kp, kod, msj := kapsamAl(r)
	if !kapsamHatasi(w, kod, msj) {
		return
	}
	if kp.Bayi() && !h.bayiAcikMi(r.Context()) {
		httpx.WriteError(w, http.StatusForbidden, errTasimaKapali.Error())
		return
	}
	_, pub, err := panelTasimaAnahtar(kp.Kapsam)
	if err != nil {
		if kp.Bayi() {
			log.Printf("tasima: bayi %d anahtari hazirlanamadi: %v", kp.RID, err)
			httpx.WriteError(w, http.StatusInternalServerError, "taşıma anahtarı hazırlanamadı")
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "panel taşıma anahtarı hazırlanamadı: "+err.Error())
		return
	}
	// Kaynakta çalıştırılacak tek komut (idempotent): dizin + izin + anahtar ekle.
	komut := "mkdir -p ~/.ssh && chmod 700 ~/.ssh && grep -qxF '" + pub + "' ~/.ssh/authorized_keys 2>/dev/null || echo '" + pub + "' >> ~/.ssh/authorized_keys; chmod 600 ~/.ssh/authorized_keys"
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"pubkey": pub, "komut": komut})
}

// kaynakHazirla — test/keşif/başlat ortak girişi: kapsam, hız sınırı, kaynak
// anahtarı, girdi doğrulaması ve hedef denetimi. Döner: kaynak, HTTP kodu, mesaj.
func (h *Handlers) kaynakHazirla(ctx context.Context, kp Kapsam, k *Kaynak) (int, string) {
	if err := h.kaynakKullanilabilir(ctx, kp, k.Tip); err != nil {
		if errors.Is(err, errTasimaKapali) {
			return http.StatusForbidden, err.Error()
		}
		return http.StatusBadRequest, err.Error()
	}
	if err := k.Dogrula(); err != nil {
		return http.StatusBadRequest, err.Error()
	}
	if err := k.hedefHazirla(ctx, kp); err != nil {
		if kp.Bayi() && !errors.Is(err, errHedefIzinsiz) {
			log.Printf("tasima: bayi %d hedef hazirlanamadi: %v", kp.RID, err)
			return http.StatusInternalServerError, tasimaHataGenel
		}
		return http.StatusBadRequest, err.Error()
	}
	return 0, ""
}

// Test — POST /tasima/test
func (h *Handlers) Test(w http.ResponseWriter, r *http.Request) {
	kp, kod, msj := kapsamAl(r)
	if !kapsamHatasi(w, kod, msj) {
		return
	}
	if !bayiHizIzni(kp.Kapsam) {
		httpx.WriteError(w, http.StatusTooManyRequests, "çok sık deneme — bir dakika sonra yeniden deneyin")
		return
	}
	var g kaynakGirdi
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&g); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz istek")
		return
	}
	k := g.kaynak(kp.Kapsam)
	if kod, msj := h.kaynakHazirla(r.Context(), kp.Kapsam, k); kod != 0 {
		httpx.WriteError(w, kod, msj)
		return
	}
	ad, err := k.BaglantiTesti(r.Context())
	if err != nil {
		httpx.WriteError(w, http.StatusBadGateway, "baglanti kurulamadi: "+err.Error())
		return
	}
	tespit, _ := k.PanelTespit(r.Context())
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "sunucu_adi": kisalt(ad, 128), "tespit_edilen": kisalt(tespit, 32),
		"uyusuyor": tespit == k.Tip,
	})
}

// Kesif — POST /tasima/kesif
func (h *Handlers) Kesif(w http.ResponseWriter, r *http.Request) {
	kp, kod, msj := kapsamAl(r)
	if !kapsamHatasi(w, kod, msj) {
		return
	}
	if !bayiHizIzni(kp.Kapsam) {
		httpx.WriteError(w, http.StatusTooManyRequests, "çok sık deneme — bir dakika sonra yeniden deneyin")
		return
	}
	var g kaynakGirdi
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&g); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz istek")
		return
	}
	k := g.kaynak(kp.Kapsam)
	if kod, msj := h.kaynakHazirla(r.Context(), kp.Kapsam, k); kod != 0 {
		httpx.WriteError(w, kod, msj)
		return
	}
	hesaplar, err := k.Kesfet(r.Context())
	if err != nil {
		httpx.WriteError(w, http.StatusBadGateway, "kesif basarisiz: "+err.Error())
		return
	}
	out := h.kesifIsaretle(r.Context(), kp.Kapsam, hesaplar)
	// 🔴 Oturumu kalıcılaştır: kaynak bilgileri + keşif sonucu şifreli saklanır ki
	// sayfa yenilenince kullanıcı her şeyi yeniden girmesin (bkz. oturum.go). Bayide
	// başlatma YALNIZ bu sunucu kopyasıyla yapılır (Secilen kesişimi).
	oturumID := h.oturumKaydet(kp, g, hesaplar)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"hesaplar": out, "toplam": len(out), "oturum_id": oturumID})
}

// kesifCikti — keşif satırı + hedefteki durum.
//   - Kök: mevcut = alan adı hedefte (herhangi bir sahipte) var.
//   - Bayi: durum = yeni | hesabinizda | kullanilamaz (+ neden). Başka kiracıdaki
//     alan adıyla ad-alanı reddi AYNI "kullanilamaz" olur — varlık sızmaz;
//     mevcut yalnız KENDİ hesabındaysa true.
type kesifCikti struct {
	Hesap
	Mevcut bool   `json:"mevcut"`
	Durum  string `json:"durum,omitempty"`
	Neden  string `json:"neden,omitempty"`
}

func (h *Handlers) kesifIsaretle(ctx context.Context, kp Kapsam, hesaplar []Hesap) []kesifCikti {
	out := make([]kesifCikti, 0, len(hesaplar))
	// N+1 yerine tek sorgu: hedefte var olan alan adlarini topla. Bayrak advisory
	// oldugundan DB hatasinda kesfi dusurmeyiz — log + eksik bayrak.
	sahipler := map[string]int64{}
	if len(hesaplar) > 0 {
		ph := make([]string, len(hesaplar))
		args := make([]any, len(hesaplar))
		for i, hs := range hesaplar {
			ph[i] = "?"
			args[i] = hs.AlanAdi
		}
		if rows, err := h.DB.QueryContext(ctx, `SELECT alan_adi, reseller_id FROM domains WHERE alan_adi IN (`+strings.Join(ph, ",")+`)`, args...); err != nil { //nolint:gosec // G202: ph = []string{"?",...} (yalnız placeholder); alan adları args ile ? bağlanır.
			log.Printf("tasima.Kesif: mevcut-domain sorgusu basarisiz: %v — 'mevcut' bayragi eksik olabilir", err)
		} else {
			defer rows.Close()
			for rows.Next() {
				var a string
				var rid int64
				if err := rows.Scan(&a, &rid); err == nil {
					sahipler[a] = rid
				}
			}
		}
	}
	for _, hs := range hesaplar {
		rid, var_ := sahipler[hs.AlanAdi]
		if !kp.Bayi() {
			out = append(out, kesifCikti{Hesap: hs, Mevcut: var_})
			continue
		}
		c := kesifCikti{Hesap: hs, Durum: "yeni"}
		switch {
		case var_ && rid == kp.RID:
			c.Durum, c.Mevcut = "hesabinizda", true
		case var_:
			c.Durum, c.Neden = "kullanilamaz", "bu alan adı bu panelde kullanılamaz"
		default:
			if err := domains.AdAlaniDenetle(ctx, h.DB, hs.AlanAdi, kp.RID); err != nil {
				var ah *domains.AdAlaniHatasi
				c.Durum = "kullanilamaz"
				if errors.As(err, &ah) {
					c.Neden = ah.Mesaj(false)
				} else {
					c.Neden = "bu alan adı bu panelde kullanılamaz"
				}
			}
		}
		out = append(out, c)
	}
	return out
}

// ---------------------------------------------------------------------------
// Is baslatma
// ---------------------------------------------------------------------------

type baslatGirdi struct {
	kaynakGirdi
	Mod      string  `json:"mod"` // tekil | toplu
	Ayarlar  Ayarlar `json:"ayarlar"`
	Secilen  []Hesap `json:"secilen"`   // frontend'in kesiften sectigi hesaplar
	OturumID int64   `json:"oturum_id"` // kaydedilmis oturum — parola yeniden girilmezse buradan cozulur
}

// isAyarKayit — tasima_isleri.ayarlar sütununa yazılan biçim: Ayarlar + SUNUCUNUN
// hazırladığı sahip eşlemesi (kuyruktaki kök işi panel yeniden başlasa da doğru
// sahibe açılsın). Ayarlar.SahipEsle json:"-" olduğundan istemci bunu gövdeden
// VEREMEZ; yalnız Baslat yazar, dağıtıcı okur.
type isAyarKayit struct {
	Ayarlar
	SahipEsleK map[string]sahipHedefJSON `json:"sahip_esle,omitempty"`
	// Arsiv: arşiv kaynaklı işte dosya kaynağı (yalnız sunucu yazar; istemci gövdesinden gelmez).
	Arsiv *isArsivKaynak `json:"arsiv,omitempty"`
}

// isArsivKaynak — işin arşiv dosyası: kök için yol, yükleme için kayıt kimliği; parmak izi
// keşiften sonra dosyanın değişmediğini iş başlarken doğrular.
type isArsivKaynak struct {
	Yol       string         `json:"yol,omitempty"`
	YuklemeID int64          `json:"yukleme_id,omitempty"`
	ParmakIzi arsivParmakIzi `json:"parmak_izi"`
}

type sahipHedefJSON struct {
	ResellerID int64 `json:"r"`
	CustomerID int64 `json:"c"`
}

var baslatMu sync.Mutex // aynı kiracının eşzamanlı iki başlatması tek iş açsın

// Baslat — POST /tasima/baslat
func (h *Handlers) Baslat(w http.ResponseWriter, r *http.Request) {
	kp, kod, msj := kapsamAl(r)
	if !kapsamHatasi(w, kod, msj) {
		return
	}
	if !bayiHizIzni(kp.Kapsam) {
		httpx.WriteError(w, http.StatusTooManyRequests, "çok sık deneme — bir dakika sonra yeniden deneyin")
		return
	}
	var g baslatGirdi
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&g); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "gecersiz istek")
		return
	}
	ctx := r.Context()
	k := g.kaynak(kp.Kapsam)

	// 🔴 KİMLİK + KEŞİF OTURUMDAN: yalnız AYNI kapsamın oturumu okunur (sahip_rid)
	// ve kimlik o kapsamın bağlamıyla çözülür. Başka kiracının oturum_id'si "yok"
	// sayılır (parola istenir) — şifreli kimlik başka kapsamda çözülmez.
	var oturumHesaplari []Hesap
	oturumVar := false
	if g.OturumID > 0 {
		var host, kesifJSON string
		var pSif, aSif sql.NullString
		if h.DB.QueryRowContext(ctx,
			`SELECT kaynak_host, COALESCE(kesif_json,''), kaynak_parola, kaynak_anahtar FROM tasima_isleri
			 WHERE id=? AND durum='oturum' AND sahip_rid=? AND (gecerlilik IS NULL OR gecerlilik > NOW())`,
			g.OturumID, kp.RID).Scan(&host, &kesifJSON, &pSif, &aSif) == nil && host == strings.TrimSpace(k.Host) {
			oturumVar = true
			if kesifJSON != "" {
				_ = json.Unmarshal([]byte(kesifJSON), &oturumHesaplari)
			}
			if k.Parola == "" && k.Anahtar == "" {
				if pSif.Valid && pSif.String != "" {
					k.Parola = kimlikCoz(pSif.String, kimlikBaglam(kp.Kapsam, host))
				}
				if aSif.Valid && aSif.String != "" {
					k.Anahtar = kimlikCoz(aSif.String, kimlikBaglam(kp.Kapsam, host))
				}
			}
		}
	}
	if kod, msj := h.kaynakHazirla(ctx, kp.Kapsam, k); kod != 0 {
		httpx.WriteError(w, kod, msj)
		return
	}
	if len(g.Secilen) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "tasinacak hesap secilmedi")
		return
	}
	if len(g.Secilen) > 500 {
		httpx.WriteError(w, http.StatusBadRequest, "tek seferde en fazla 500 site tasinabilir")
		return
	}
	mod := "toplu"
	if g.Mod == "tekil" || len(g.Secilen) == 1 {
		mod = "tekil"
	}

	// 🔴 BAYİ: seçilen hesaplar istemciden DEĞİL, oturumdaki SUNUCU keşfinden alınır
	// (alan adı kesişimi). Gövdedeki WebRoot/DBler/Sahip alanları yok sayılır →
	// bayi keşifte olmayan bir docroot/DB adı enjekte edemez.
	secilen := g.Secilen
	if kp.Bayi() {
		if !oturumVar {
			httpx.WriteError(w, http.StatusBadRequest, "önce kaynak sunucuyu keşfedin (oturum bulunamadı ya da süresi doldu)")
			return
		}
		sunucu := map[string]Hesap{}
		for _, hs := range oturumHesaplari {
			sunucu[strings.ToLower(hs.AlanAdi)] = hs
		}
		secilen = nil
		gorulen := map[string]bool{}
		for _, hs := range g.Secilen {
			a := strings.ToLower(strings.TrimSpace(hs.AlanAdi))
			if s, ok := sunucu[a]; ok && !gorulen[a] {
				gorulen[a] = true
				secilen = append(secilen, s)
			}
		}
	}
	// Gecerli hesaplari suz (uzaktan gelen veri — yeniden dogrula).
	var gecerli []Hesap
	for _, hs := range secilen {
		hs.AlanAdi = strings.ToLower(strings.TrimSpace(hs.AlanAdi))
		if !reAlanAdi.MatchString(hs.AlanAdi) || !strings.Contains(hs.AlanAdi, ".") {
			continue
		}
		if hs.KaynakHesap != "" && !reHesap.MatchString(hs.KaynakHesap) {
			continue
		}
		gecerli = append(gecerli, hs)
	}
	if len(gecerli) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "gecerli hesap bulunamadi")
		return
	}

	ay := g.Ayarlar
	ay.SahipEsle = nil
	var uretilenKimlikler []UretilenKimlik
	if kp.Bayi() {
		// 🔴 BAYİ İÇİN ZORLANAN ALANLAR (istemci değeri yok sayılır): site YALNIZ
		// bayinin kendi hesabına açılır, sahip/müşteri taşınmaz, var olan site ezilmez
		// (v1 kararı), plan kapsamlı ve zorunlu.
		ay.ResellerID, ay.CustomerID = kp.RID, 0
		ay.SahipleriTasi = false
		ay.Ustune = false
		ay.Hesaplar = nil
		planID, kod, msj := h.bayiPlanCoz(ctx, kp.RID, ay.PlanID)
		if kod != 0 {
			httpx.WriteError(w, kod, msj)
			return
		}
		ay.PlanID = planID
		if kod, msj := h.bayiKalanDenetle(ctx, kp.RID, len(gecerli)); kod != 0 {
			httpx.WriteError(w, kod, msj)
			return
		}
	} else if ay.SahipleriTasi {
		// 🔴 SAHİPLİK TAŞIMA (yalnız kök, senkron, domainlerden ÖNCE): kaynaktaki
		// reseller/müşteri hesaplarını oluştur; login→hedef eşlemesini işe yaz.
		// Üretilen reseller parolaları yanıtta bir kez raporlanır.
		sahipler, serr := k.KesfetSahipler(ctx)
		if serr != nil {
			httpx.WriteError(w, http.StatusBadGateway, "sahip keşfi başarısız: "+serr.Error())
			return
		}
		esle, kimlikler, herr := sahipleriHazirla(ctx, h.DB, sahipler, kp.Kapsam)
		if herr != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "sahip hesapları oluşturulamadı: "+herr.Error())
			return
		}
		ay.SahipEsle = esle
		uretilenKimlikler = kimlikler
	}

	kayit := isAyarKayit{Ayarlar: ay}
	if len(ay.SahipEsle) > 0 {
		kayit.SahipEsleK = map[string]sahipHedefJSON{}
		for lg, s := range ay.SahipEsle {
			kayit.SahipEsleK[lg] = sahipHedefJSON{ResellerID: s.ResellerID, CustomerID: s.CustomerID}
		}
	}
	ayarJSON, _ := json.Marshal(kayit)

	// Kimlik bilgileri at-rest sifreli (baglam = kapsam + host, capraz kullanim engeli).
	baglam := kimlikBaglam(kp.Kapsam, k.Host)
	parolaSifreli := ""
	if k.Parola != "" {
		parolaSifreli = kimlikSifrele(k.Parola, baglam)
	}
	anahtarSifreli := ""
	if k.Anahtar != "" {
		anahtarSifreli = kimlikSifrele(k.Anahtar, baglam)
	}

	baslatMu.Lock()
	var acik int
	if err := h.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasima_isleri WHERE sahip_rid=? AND durum IN ('kuyrukta','calisiyor')`, kp.RID).Scan(&acik); err != nil {
		baslatMu.Unlock()
		httpx.WriteError(w, http.StatusInternalServerError, "is kaydi olusturulamadi")
		return
	}
	if acik > 0 {
		// Yalnız KENDİ verisi: başka kiracının (kökün) işi bu sayıma girmez.
		baslatMu.Unlock()
		httpx.WriteError(w, http.StatusConflict, "devam eden ya da sırada bekleyen bir taşımanız var")
		return
	}
	res, err := h.DB.ExecContext(ctx,
		`INSERT INTO tasima_isleri (kaynak_tip, kaynak_host, kaynak_port, kaynak_kullanici,
		   kaynak_parola, kaynak_anahtar, tasima_modu, durum, toplam, ayarlar, baslatan,
		   sahip_rid, baslatan_uid)
		 VALUES (?,?,?,?,?,?,?, 'kuyrukta', ?, ?, ?, ?, NULLIF(?,0))`,
		k.Tip, k.Host, k.Port, k.Kullanici, parolaSifreli, anahtarSifreli,
		mod, len(gecerli), string(ayarJSON), kp.Kul, kp.RID, kp.UID)
	if err != nil {
		baslatMu.Unlock()
		log.Printf("tasima: is kaydi olusturulamadi: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "is kaydi olusturulamadi")
		return
	}
	isID, _ := res.LastInsertId()
	for _, hs := range gecerli {
		hj, _ := json.Marshal(hs)
		if _, err := h.DB.ExecContext(ctx,
			`INSERT INTO tasima_kalemleri (is_id, kaynak_hesap, alan_adi, durum, hesap_json)
			 VALUES (?,?,?, 'bekliyor', ?)`, isID, hs.KaynakHesap, hs.AlanAdi, string(hj)); err != nil {
			log.Printf("tasima: kalem yazilamadi (is=%d): %v", isID, err)
		}
	}
	baslatMu.Unlock()

	httpx.Denetim(h.DB, r, kp.UID, kp.Kul, "tasima_baslat",
		fmt.Sprintf("%s@%s", k.Tip, k.Host),
		fmt.Sprintf("%d site", len(gecerli)), kp.RID, true)

	h.dagit()
	durum := "kuyrukta"
	_ = h.DB.QueryRowContext(ctx, `SELECT durum FROM tasima_isleri WHERE id=?`, isID).Scan(&durum)
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{
		"is_id": isID, "toplam": len(gecerli), "durum": durum, "uretilen_kimlikler": uretilenKimlikler,
	})
}

// bayiPlanCoz — bayi işinin planı: istenen plan BaglanabilirMi'den geçmeli;
// istenmediyse bayinin (yoksa genel) VARSAYILAN katalog planı. Plan bulunamazsa
// ret: plansız hosting kaynak limitsiz açılır (kaynaklimit: plansız = sınırsız),
// taşınan hesap bayinin plan/havuz sınırının dışına çıkmasın. Bayiye plan
// OLUŞTURULMAZ (plans.BayiVarsayilanIsaretle yalnız işaretler).
func (h *Handlers) bayiPlanCoz(ctx context.Context, rid, planID int64) (int64, int, string) {
	if planID > 0 {
		if err := plans.BaglanabilirMi(ctx, h.DB, rid, planID, 0); err != nil {
			return 0, http.StatusForbidden, err.Error()
		}
		return planID, 0, ""
	}
	// Bayinin KENDİ varsayılan planı (planı var ama hiçbiri varsayılan değilse en
	// eskisi işaretlenir). Kendi planı yoksa genel (kök) varsayılan katalog planı.
	if defID, _, err := plans.BayiVarsayilanIsaretle(ctx, h.DB, rid, 0, "sistem"); err == nil && defID > 0 {
		return defID, 0, ""
	}
	var defID int64
	if err := h.DB.QueryRowContext(ctx,
		`SELECT id FROM service_plans WHERE varsayilan=1 AND domain_id IS NULL AND reseller_id IN (?,0)
		 ORDER BY reseller_id DESC, id LIMIT 1`, rid).Scan(&defID); err != nil || defID <= 0 {
		return 0, http.StatusBadRequest, errBayiPlanGerekli.Error()
	}
	return defID, 0, ""
}

// bayiKalanDenetle — başlatmada ön denetim: bayi etkin mi, seçilen site sayısı
// kalan hosting hakkını aşıyor mu? Kesin denetim her kalemde (kilit altında).
func (h *Handlers) bayiKalanDenetle(ctx context.Context, rid int64, adet int) (int, string) {
	var maxDomain int
	var durum string
	if err := h.DB.QueryRowContext(ctx,
		`SELECT max_domain, status FROM users WHERE id=? AND role='reseller'`, rid).Scan(&maxDomain, &durum); err != nil {
		return http.StatusForbidden, "yetkiniz yok"
	}
	if durum != "active" {
		return http.StatusForbidden, "bayi hesabı askıda — taşıma başlatılamaz"
	}
	if maxDomain <= 0 {
		return 0, ""
	}
	var mevcut int
	if err := h.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM domains WHERE reseller_id=?`, rid).Scan(&mevcut); err != nil {
		return http.StatusInternalServerError, "kota okunamadı"
	}
	if kalan := maxDomain - mevcut; adet > kalan {
		if kalan < 0 {
			kalan = 0
		}
		return http.StatusForbidden, fmt.Sprintf("hosting hesabı limitiniz yetersiz: %d site seçildi, kalan hak %d (%d/%d)", adet, kalan, mevcut, maxDomain)
	}
	return 0, ""
}

// ---------------------------------------------------------------------------
// Durum / liste / log / iptal
// ---------------------------------------------------------------------------

// isKapsami — işin sahibi (oturum satırları iş değildir). ok=false: yok.
func (h *Handlers) isKapsami(ctx context.Context, isID int64) (int64, bool) {
	var rid int64
	if err := h.DB.QueryRowContext(ctx,
		`SELECT sahip_rid FROM tasima_isleri WHERE id=? AND durum<>'oturum'`, isID).Scan(&rid); err != nil {
		return 0, false
	}
	return rid, true
}

// isErisimi — URL'deki iş bu kapsamda mı? Değilse (yok ya da başkasının) false
// ve AYNI 404 yazılmış olur.
func (h *Handlers) isErisimi(w http.ResponseWriter, r *http.Request, kp Kapsam) (int64, bool) {
	isID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || isID <= 0 {
		isBulunamadi(w)
		return 0, false
	}
	rid, ok := h.isKapsami(r.Context(), isID)
	if !ok || (kp.Bayi() && rid != kp.RID) {
		isBulunamadi(w)
		return 0, false
	}
	return isID, true
}

// Durum — GET /tasima  (son isler + aktif is; bayide YALNIZ kendi isleri)
func (h *Handlers) Durum(w http.ResponseWriter, r *http.Request) {
	kp, kod, msj := kapsamAl(r)
	if !kapsamHatasi(w, kod, msj) {
		return
	}
	q := `SELECT t.id, t.kaynak_tip, t.kaynak_host, t.tasima_modu, t.durum, t.toplam, t.tamamlanan, t.basarisiz,
	        COALESCE(t.hata,''), COALESCE(t.baslatan,''), t.baslangic, t.bitis, t.olusturma, t.sahip_rid,
	        COALESCE((SELECT u.username FROM users u WHERE u.id=t.sahip_rid AND t.sahip_rid>0),'')
	      FROM tasima_isleri t WHERE t.durum <> 'oturum'`
	var args []any
	if kp.Bayi() {
		q += ` AND t.sahip_rid=?`
		args = append(args, kp.RID)
	}
	q += ` ORDER BY t.id DESC LIMIT 25`
	rows, err := h.DB.QueryContext(r.Context(), q, args...)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "isler okunamadi")
		return
	}
	defer rows.Close()
	type is struct {
		ID         int64      `json:"id"`
		Tip        string     `json:"tip"`
		Host       string     `json:"host"`
		Mod        string     `json:"mod"`
		Durum      string     `json:"durum"`
		Toplam     int        `json:"toplam"`
		Tamamlanan int        `json:"tamamlanan"`
		Basarisiz  int        `json:"basarisiz"`
		Hata       string     `json:"hata"`
		Baslatan   string     `json:"baslatan"`
		Baslangic  *time.Time `json:"baslangic"`
		Bitis      *time.Time `json:"bitis"`
		Olusturma  time.Time  `json:"olusturma"`
		SahipRID   int64      `json:"sahip_rid,omitempty"`
		Sahip      string     `json:"sahip,omitempty"`
	}
	out := []is{}
	for rows.Next() {
		var v is
		if err := rows.Scan(&v.ID, &v.Tip, &v.Host, &v.Mod, &v.Durum, &v.Toplam,
			&v.Tamamlanan, &v.Basarisiz, &v.Hata, &v.Baslatan, &v.Baslangic, &v.Bitis, &v.Olusturma,
			&v.SahipRID, &v.Sahip); err != nil {
			continue
		}
		if kp.Bayi() {
			v.SahipRID, v.Sahip = 0, ""
		}
		out = append(out, v)
	}
	kilit.Lock()
	aktif := aktifIsID
	kilit.Unlock()
	// aktif_is yalnız KAPSAM İÇİNDEYSE: bayi başkasının (kökün) çalışan işini öğrenmez.
	if aktif != 0 && kp.Bayi() {
		if rid, ok := h.isKapsami(r.Context(), aktif); !ok || rid != kp.RID {
			aktif = 0
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"isler": out, "aktif_is": aktif})
}

// Detay — GET /tasima/{id}
func (h *Handlers) Detay(w http.ResponseWriter, r *http.Request) {
	kp, kod, msj := kapsamAl(r)
	if !kapsamHatasi(w, kod, msj) {
		return
	}
	isID, ok := h.isErisimi(w, r, kp.Kapsam)
	if !ok {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, kaynak_hesap, alan_adi, durum, COALESCE(domain_id,0),
		        dosya_bayt, db_sayisi, dns_sayisi, COALESCE(hata,''), basladi, bitti
		 FROM tasima_kalemleri WHERE is_id=? ORDER BY id`, isID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kalemler okunamadi")
		return
	}
	defer rows.Close()
	type kalem struct {
		ID        int64      `json:"id"`
		Hesap     string     `json:"kaynak_hesap"`
		AlanAdi   string     `json:"alan_adi"`
		Durum     string     `json:"durum"`
		DomainID  int64      `json:"domain_id"`
		DosyaBayt int64      `json:"dosya_bayt"`
		DBSayisi  int        `json:"db_sayisi"`
		DNSSayisi int        `json:"dns_sayisi"`
		Hata      string     `json:"hata"`
		Basladi   *time.Time `json:"basladi"`
		Bitti     *time.Time `json:"bitti"`
	}
	out := []kalem{}
	for rows.Next() {
		var v kalem
		if err := rows.Scan(&v.ID, &v.Hesap, &v.AlanAdi, &v.Durum, &v.DomainID,
			&v.DosyaBayt, &v.DBSayisi, &v.DNSSayisi, &v.Hata, &v.Basladi, &v.Bitti); err != nil {
			continue
		}
		out = append(out, v)
	}
	var durum, hata string
	var toplam, tamamlanan, basarisiz int
	_ = h.DB.QueryRowContext(r.Context(), `SELECT durum, toplam, tamamlanan, basarisiz, COALESCE(hata,'') FROM tasima_isleri WHERE id=?`, isID).
		Scan(&durum, &toplam, &tamamlanan, &basarisiz, &hata)
	kilit.Lock()
	calisiyor := aktifIsID == isID
	kilit.Unlock()
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"kalemler": out, "durum": durum, "toplam": toplam, "hata": hata,
		"tamamlanan": tamamlanan, "basarisiz": basarisiz, "calisiyor": calisiyor,
	})
}

// Log — GET /tasima/{id}/log
func (h *Handlers) Log(w http.ResponseWriter, r *http.Request) {
	kp, kod, msj := kapsamAl(r)
	if !kapsamHatasi(w, kod, msj) {
		return
	}
	isID, ok := h.isErisimi(w, r, kp.Kapsam)
	if !ok {
		return
	}
	metin := dosyaSonu(logYolu(isID), 120<<10)
	kilit.Lock()
	calisiyor := aktifIsID == isID
	kilit.Unlock()
	var durum string
	_ = h.DB.QueryRowContext(r.Context(), `SELECT durum FROM tasima_isleri WHERE id=?`, isID).Scan(&durum)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"log": metin, "calisiyor": calisiyor, "durum": durum,
	})
}

// Iptal — POST /tasima/{id}/iptal  (çalışan iş durdurulur; sıradaki iş düşer)
func (h *Handlers) Iptal(w http.ResponseWriter, r *http.Request) {
	kp, kod, msj := kapsamAl(r)
	if !kapsamHatasi(w, kod, msj) {
		return
	}
	isID, ok := h.isErisimi(w, r, kp.Kapsam)
	if !ok {
		return
	}
	kilit.Lock()
	eslesti := aktifIsID == isID && aktifIptal != nil
	if eslesti {
		aktifIptal()
	}
	kilit.Unlock()
	if !eslesti {
		res, err := h.DB.ExecContext(r.Context(),
			`UPDATE tasima_isleri SET durum='iptal', hata='kullanici iptal etti', bitis=NOW(),
			   kaynak_parola=NULL, kaynak_anahtar=NULL, kimlik_temiz=1
			 WHERE id=? AND durum='kuyrukta'`, isID)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "iptal edilemedi")
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			httpx.WriteError(w, http.StatusConflict, "bu iş çalışmıyor")
			return
		}
		_, _ = h.DB.ExecContext(r.Context(),
			`UPDATE tasima_kalemleri SET durum='atlandi', hata='kullanici iptal etti', bitti=NOW()
			 WHERE is_id=? AND durum='bekliyor'`, isID)
	}
	httpx.Denetim(h.DB, r, kp.UID, kp.Kul, "tasima_iptal",
		fmt.Sprintf("is=%d", isID), "", kp.RID, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// AcilistaTemizle — main.go acilista cagirir.
func (h *Handlers) AcilistaTemizle() {
	res, err := h.DB.Exec(
		`UPDATE tasima_isleri SET durum='kesildi', hata='panel yeniden baslatildi', bitis=NOW()
		 WHERE durum IN ('calisiyor','kesif')`)
	if err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("tasima: %d yarim kalan is 'kesildi' olarak isaretlendi", n)
			_, _ = h.DB.Exec(
				`UPDATE tasima_kalemleri SET durum='hata', hata='panel yeniden baslatildi', bitti=NOW()
				 WHERE durum='calisiyor'`)
		}
	}
	h.kuyrukTemizle()
	h.arsivYuklemeTemizle()
	// Guvenlik: biten islerin kimlik bilgilerini de temizle.
	_, _ = h.DB.Exec(
		`UPDATE tasima_isleri SET kaynak_parola=NULL, kaynak_anahtar=NULL, kimlik_temiz=1
		 WHERE durum IN ('tamam','hata','iptal','kesildi') AND kimlik_temiz=0`)
	// 🔴 Suresi dolan kaydedilmis oturumlari SIL (kimlik dahil). 'oturum' durumu
	// yeniden baslatmayi ATLATIR (yukaridaki UPDATE onu OLDURMEZ); yalniz TTL siler.
	_, _ = h.DB.Exec(`DELETE FROM tasima_isleri WHERE durum='oturum' AND gecerlilik IS NOT NULL AND gecerlilik < NOW()`)
	// Sıradaki işler panel yeniden başlasa da sürer — açılış tamamlansın diye gecikmeli.
	if acilisDagitGecikme > 0 {
		time.AfterFunc(acilisDagitGecikme, h.dagit)
	}
}

// acilisDagitGecikme — açılışta kuyruğun devralınmasından önceki bekleme (0 = yok).
var acilisDagitGecikme = 20 * time.Second

// dosyaSonu — dosyanin son n baytini guvenli okur (symlink/ozel dosya reddi).
func dosyaSonu(yol string, n int64) string {
	f, err := os.OpenFile(yol, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return ""
	}
	boyut := st.Size()
	if boyut > n {
		if _, err := f.Seek(boyut-n, 0); err != nil {
			return ""
		}
	}
	buf := make([]byte, n)
	okunan, _ := f.Read(buf)
	if okunan <= 0 {
		return ""
	}
	return string(buf[:okunan])
}
