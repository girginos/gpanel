package tasima

// kuyruk.go — taşıma KUYRUĞU ve tek dağıtıcı.
//
// Baslat artık sunucu genelindeki meşguliyet için 409 DÖNMEZ (bayiye kökün çalışan
// işi hakkında bilgi sızardı): iş 'kuyrukta' yazılır, dağıtıcı TEK genel yuvayı
// (eşzamanlılık 1 — sunucuyu koruyan eski gerekçe) sırayla doldurur. Aynı kiracının
// zaten sırada/çalışan işi varsa 409 yalnız KENDİ verisidir (Baslat).
//
// İşin her şeyi satırdadır: kimlik (kapsam bağlamıyla şifreli), ayarlar (isAyarKayit),
// seçilen hesaplar (tasima_kalemleri.hesap_json). Panel yeniden başlarsa 'kuyrukta'
// işler sürer, 'calisiyor' işler 'kesildi' olur (AcilistaTemizle).
//
// Başlamadan önce dağıtıcı kapsamın iznini YENİDEN okur: kök bayi taşımasını ya da
// kaynağı kapattıysa sıradaki iş başlamadan iptal edilir.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// kuyrukOmru — bu süreden uzun sırada bekleyen iş iptal edilir (kimliği silinir).
const kuyrukOmru = 24 * time.Hour

// hesapAktarici — calistir'in kalem başına çağırdığı taşıma (testler değiştirir:
// günlük/kalem süzgeci gerçek taşıma olmadan sınanır).
var hesapAktarici = (*Handlers).HesapAktar

// isYurutucu — dağıtıcının çalıştırdığı fonksiyon (testler değiştirir: gerçek
// taşıma yerine yuva/kapsam davranışı sınanır).
var isYurutucu func(h *Handlers, ctx context.Context, isID int64)

func init() { isYurutucu = (*Handlers).isYurut }

// dagit — yuva boşsa en eski 'kuyrukta' işi alıp başlatır. Eşzamanlı çağrılar
// güvenlidir (yuva kilit altında, iş durumu koşullu UPDATE ile alınır).
func (h *Handlers) dagit() {
	kilit.Lock()
	defer kilit.Unlock()
	if aktifIsID != 0 {
		return
	}
	for deneme := 0; deneme < 3; deneme++ {
		var id int64
		if err := h.DB.QueryRow(`SELECT id FROM tasima_isleri WHERE durum='kuyrukta' ORDER BY id LIMIT 1`).Scan(&id); err != nil {
			if err != sql.ErrNoRows {
				log.Printf("tasima: kuyruk okunamadi: %v", err)
			}
			return
		}
		res, err := h.DB.Exec(`UPDATE tasima_isleri SET durum='calisiyor', baslangic=NOW() WHERE id=? AND durum='kuyrukta'`, id)
		if err != nil {
			log.Printf("tasima: is %d alinamadi: %v", id, err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // başka biri aldı / iptal edildi
		}
		ctx, iptal := context.WithCancel(context.Background())
		aktifIsID, aktifIptal = id, iptal
		go isYurutucu(h, ctx, id)
		return
	}
}

// yuvaBirak — iş bitti: yuva boşalır, kimlik silinir, sıradaki iş başlatılır.
func (h *Handlers) yuvaBirak(isID int64) {
	kilit.Lock()
	if aktifIsID == isID {
		if aktifIptal != nil {
			aktifIptal()
		}
		aktifIsID, aktifIptal = 0, nil
	}
	kilit.Unlock()
	// Kimlik bilgilerini DB'den temizle — is bitti, artik gerekmiyor.
	_, _ = h.DB.Exec(
		`UPDATE tasima_isleri SET kaynak_parola=NULL, kaynak_anahtar=NULL, kimlik_temiz=1 WHERE id=?`, isID)
	h.dagit()
}

// isBitir — işi hata/iptal ile kapatır (başlamadan düşen iş).
func (h *Handlers) isBitir(isID int64, durum, neden string) {
	_, _ = h.DB.Exec(`UPDATE tasima_isleri SET durum=?, hata=?, bitis=NOW() WHERE id=?`, durum, neden, isID)
	_, _ = h.DB.Exec(`UPDATE tasima_kalemleri SET durum='atlandi', hata=?, bitti=NOW() WHERE is_id=? AND durum='bekliyor'`, neden, isID)
}

// isYukle — kuyruktaki işi satırdan kurar.
func (h *Handlers) isYukle(isID int64) (*Kaynak, Ayarlar, Kapsam, error) {
	var tip, host, kul, ayarJSON string
	var port int
	var rid int64
	var pSif, aSif sql.NullString
	if err := h.DB.QueryRow(
		`SELECT kaynak_tip, kaynak_host, kaynak_port, kaynak_kullanici, kaynak_parola, kaynak_anahtar,
		        COALESCE(ayarlar,''), sahip_rid
		   FROM tasima_isleri WHERE id=?`, isID).
		Scan(&tip, &host, &port, &kul, &pSif, &aSif, &ayarJSON, &rid); err != nil {
		return nil, Ayarlar{}, Kapsam{}, fmt.Errorf("is okunamadi: %w", err)
	}
	kp := Kapsam{RID: rid}
	k := &Kaynak{Tip: tip, Host: host, Port: port, Kullanici: kul}
	baglam := kimlikBaglam(kp, host)
	if pSif.Valid && pSif.String != "" {
		k.Parola = kimlikCoz(pSif.String, baglam)
	}
	if aSif.Valid && aSif.String != "" {
		k.Anahtar = kimlikCoz(aSif.String, baglam)
	}
	var kayit isAyarKayit
	if ayarJSON != "" {
		if err := json.Unmarshal([]byte(ayarJSON), &kayit); err != nil {
			return nil, Ayarlar{}, kp, fmt.Errorf("is ayarlari okunamadi")
		}
	}
	ay := kayit.Ayarlar
	ay.SahipEsle = nil
	// Sahip eşlemesi YALNIZ kök işinde (sunucunun Baslat'ta yazdığı kayıttan).
	if !kp.Bayi() && len(kayit.SahipEsleK) > 0 {
		ay.SahipEsle = map[string]sahipHedef{}
		for lg, s := range kayit.SahipEsleK {
			ay.SahipEsle[lg] = sahipHedef{ResellerID: s.ResellerID, CustomerID: s.CustomerID}
		}
	}
	if kp.Bayi() {
		// Savunma derinliği: satır bozulsa da bayi işi bayinin kendi hesabına.
		ay.ResellerID, ay.CustomerID, ay.SahipleriTasi, ay.Ustune = kp.RID, 0, false, false
	}
	return k, ay, kp, nil
}

// isHesaplari — işin bekleyen kalemleri (sunucuda saklanan hesap_json).
func (h *Handlers) isHesaplari(isID int64) ([]Hesap, error) {
	rows, err := h.DB.Query(
		`SELECT alan_adi, kaynak_hesap, COALESCE(hesap_json,'') FROM tasima_kalemleri
		  WHERE is_id=? AND durum='bekliyor' ORDER BY id`, isID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hesap
	for rows.Next() {
		var alan, hesap, hj string
		if rows.Scan(&alan, &hesap, &hj) != nil {
			continue
		}
		var hs Hesap
		if hj == "" || json.Unmarshal([]byte(hj), &hs) != nil {
			hs = Hesap{KaynakHesap: hesap}
		}
		hs.AlanAdi = alan // satırın kendi alan adı esastır
		out = append(out, hs)
	}
	return out, nil
}

// isYurut — dağıtıcının başlattığı iş (goroutine).
func (h *Handlers) isYurut(ctx context.Context, isID int64) {
	defer h.yuvaBirak(isID)
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("tasima: panik (is=%d): %v", isID, rec)
			_, _ = h.DB.Exec(`UPDATE tasima_isleri SET durum='hata', hata='beklenmeyen hata', bitis=NOW() WHERE id=?`, isID)
		}
	}()
	k, ay, kp, err := h.isYukle(isID)
	if err != nil {
		log.Printf("tasima: is %d yuklenemedi: %v", isID, err)
		h.isBitir(isID, "hata", "iş bilgileri okunamadı")
		return
	}
	defer func() { k.Parola, k.Anahtar = "", "" }()
	// Başlamadan önce izin YENİDEN: kök bayi taşımasını ya da kaynağı kapatmış olabilir.
	if err := h.kaynakKullanilabilir(ctx, kp, k.Tip); err != nil {
		h.isBitir(isID, "iptal", err.Error())
		return
	}
	if err := k.Dogrula(); err != nil {
		h.isBitir(isID, "hata", hataMetni(kp.Yonetici(), err).Error())
		return
	}
	// Bayide hedef çalıştırma anında YENİDEN çözülüp denetlenir (DNS değişmiş olabilir).
	if err := k.hedefHazirla(ctx, kp); err != nil {
		neden := errHedefIzinsiz.Error()
		if !errors.Is(err, errHedefIzinsiz) {
			neden = hataMetni(kp.Yonetici(), err).Error()
		}
		h.isBitir(isID, "hata", neden)
		return
	}
	hesaplar, err := h.isHesaplari(isID)
	if err != nil || len(hesaplar) == 0 {
		h.isBitir(isID, "hata", "taşınacak kalem bulunamadı")
		return
	}
	h.calistir(ctx, isID, k, hesaplar, ay, kp)
}

// calistir — kalemleri sırayla taşır. Bayi işinde günlük ve kalem notları
// süzülür (gunluk.go); ham satır sunucu günlüğüne gider.
func (h *Handlers) calistir(ctx context.Context, isID int64, k *Kaynak, hesaplar []Hesap, ay Ayarlar, kp Kapsam) {
	_ = os.MkdirAll(logDizin, 0o750)
	lf, err := os.OpenFile(logYolu(isID), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		log.Printf("tasima: log acilamadi: %v", err)
	}
	if lf != nil {
		defer lf.Close()
	}
	yaz := func(f string, a ...any) {
		ham := fmt.Sprintf(f, a...)
		satir := temizHata(ham, k.Parola) // kök de olsa kaynak parolası günlüğe düşmez
		if kp.Bayi() {
			satir = bayiSatirSuz(ham, k.Parola)
			if satir != ham {
				log.Printf("tasima[is=%d bayi=%d]: %s", isID, kp.RID, kisalt(temizHata(ham, k.Parola), 2000))
			}
		}
		if lf != nil {
			_, _ = lf.WriteString(fmt.Sprintf("[%s] %s\n", time.Now().Format("15:04:05"), satir))
			_ = lf.Sync()
		}
	}

	yaz("Tasima basladi — kaynak %s (%s), %d site", k.Host, k.Tip, len(hesaplar))

	var tamam, hata int
	for i, hs := range hesaplar {
		select {
		case <-ctx.Done():
			yaz("IPTAL EDILDI — kalan %d site atlandi", len(hesaplar)-i)
			_, _ = h.DB.Exec(`UPDATE tasima_isleri SET durum='iptal', bitis=NOW() WHERE id=?`, isID)
			_, _ = h.DB.Exec(`UPDATE tasima_kalemleri SET durum='atlandi', hata='kullanici iptal etti', bitti=NOW() WHERE is_id=? AND durum='bekliyor'`, isID)
			return
		default:
		}

		yaz("──────── [%d/%d] %s ────────", i+1, len(hesaplar), hs.AlanAdi)
		_, _ = h.DB.Exec(
			`UPDATE tasima_kalemleri SET durum='calisiyor', basladi=NOW() WHERE is_id=? AND alan_adi=?`,
			isID, hs.AlanAdi)

		sonuc, err := hesapAktarici(h, ctx, k, hs, ay, kp, yaz)
		if err != nil && ctx.Err() != nil {
			// Iptal son hesap sirasinda gelirse dongunun basindaki ctx kontrolune
			// hic donulmez; is 'hata' olarak ve BOS mesajla kapaniyordu.
			yaz("IPTAL EDILDI — %s yarida kesildi", hs.AlanAdi)
			_, _ = h.DB.Exec(
				`UPDATE tasima_kalemleri SET durum='atlandi', hata='kullanici iptal etti', bitti=NOW() WHERE is_id=? AND alan_adi=?`,
				isID, hs.AlanAdi)
			_, _ = h.DB.Exec(`UPDATE tasima_kalemleri SET durum='atlandi', hata='kullanici iptal etti', bitti=NOW() WHERE is_id=? AND durum='bekliyor'`, isID)
			_, _ = h.DB.Exec(`UPDATE tasima_isleri SET durum='iptal', bitis=NOW() WHERE id=?`, isID)
			return
		}
		if err != nil {
			durum := kalemDurumu(err)
			if durum == "hata" {
				hata++
			}
			yaz("%s: %s → %v", strings.ToUpper(durum), hs.AlanAdi, err)
			_, _ = h.DB.Exec(
				`UPDATE tasima_kalemleri SET durum=?, hata=?, bitti=NOW() WHERE is_id=? AND alan_adi=?`,
				durum, kisalt(err.Error(), 500), isID, hs.AlanAdi)
		} else {
			tamam++
			uyarilar := sonuc.Uyarilar
			if kp.Bayi() {
				uyarilar = bayiUyarilariSuz(uyarilar, k.Parola)
			}
			uy := ""
			if len(uyarilar) > 0 {
				uy = " | uyari: " + strings.Join(uyarilar, "; ")
			}
			yaz("TAMAM: %s (%.1f MB, %d DB, %d DNS)%s", hs.AlanAdi,
				float64(sonuc.DosyaBayt)/(1024*1024), sonuc.DBSayisi, sonuc.DNSSayisi, uy)
			_, _ = h.DB.Exec(
				`UPDATE tasima_kalemleri SET durum='tamam', domain_id=?, dosya_bayt=?, db_sayisi=?,
				   dns_sayisi=?, hata=NULLIF(?,''), bitti=NOW() WHERE is_id=? AND alan_adi=?`,
				sonuc.DomainID, sonuc.DosyaBayt, sonuc.DBSayisi, sonuc.DNSSayisi,
				kisalt(strings.Join(uyarilar, "; "), 4000), isID, hs.AlanAdi)
		}
		_, _ = h.DB.Exec(`UPDATE tasima_isleri SET tamamlanan=?, basarisiz=? WHERE id=?`, tamam, hata, isID)
	}

	sonDurum := "tamam"
	if hata > 0 && tamam == 0 {
		sonDurum = "hata"
	}
	yaz("Tasima bitti — %d basarili, %d basarisiz", tamam, hata)
	_, _ = h.DB.Exec(`UPDATE tasima_isleri SET durum=?, tamamlanan=?, basarisiz=?, bitis=NOW() WHERE id=?`,
		sonDurum, tamam, hata, isID)
}

// kalemDurumu — başarısız kalemin durumu: kota/askı → 'atlandi' (site açılmadı,
// sonraki kalemler kendi kota denetiminden geçer), diğerleri 'hata'.
func kalemDurumu(err error) string {
	var kh *kotaHatasi
	if errors.As(err, &kh) {
		return "atlandi"
	}
	return "hata"
}

// kuyrukTemizle — açılışta: çok bekleyen işler iptal; silinmiş bayilerin taşıma
// satırları (oturum + iş + kalemler CASCADE) ve anahtar dizinleri silinir.
func (h *Handlers) kuyrukTemizle() {
	_, _ = h.DB.Exec(
		`UPDATE tasima_isleri SET durum='iptal', hata='sırada çok uzun bekledi', bitis=NOW(),
		   kaynak_parola=NULL, kaynak_anahtar=NULL, kimlik_temiz=1
		 WHERE durum='kuyrukta' AND olusturma < (NOW() - INTERVAL ? SECOND)`, int(kuyrukOmru.Seconds()))
	h.artakalanBayileriTemizle()
}

// artakalanBayileriTemizle — sahibi (bayi) artık olmayan taşıma satırları ve
// kiracı anahtar dizinleri. Bayi silinince kimliği/oturumu panelde kalmasın.
func (h *Handlers) artakalanBayileriTemizle() {
	res, err := h.DB.Exec(
		`DELETE FROM tasima_isleri WHERE sahip_rid>0 AND durum<>'calisiyor'
		   AND sahip_rid NOT IN (SELECT id FROM users WHERE role='reseller')`)
	if err != nil {
		log.Printf("tasima: artakalan bayi satirlari temizlenemedi: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("tasima: silinmis bayilere ait %d tasima satiri temizlendi", n)
	}
	girdiler, err := os.ReadDir(tasimaKeyDir)
	if err != nil {
		return
	}
	for _, g := range girdiler {
		if !g.IsDir() {
			continue
		}
		rid, err := strconv.ParseInt(g.Name(), 10, 64)
		if err != nil || rid <= 0 || strconv.FormatInt(rid, 10) != g.Name() {
			continue
		}
		var n int
		if err := h.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE id=? AND role='reseller'`, rid).Scan(&n); err != nil || n > 0 {
			continue
		}
		if err := os.RemoveAll(filepath.Join(tasimaKeyDir, g.Name())); err != nil {
			log.Printf("tasima: bayi %d anahtar dizini silinemedi: %v", rid, err)
		}
	}
}
