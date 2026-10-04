package apianahtar

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"strings"
	"time"
)

// Kayit: api_anahtarlari satırı (+ sahibin users satırından okunanlar).
// 🔴 Ozet hiçbir yanıtta dönmez (uc paketi ayrı bir görünüm tipi kullanır).
type Kayit struct {
	ID            int64
	SahipID       int64
	SahipRol      string
	Ad            string
	Onek          string
	Ozet          string
	Son4          string
	Izinler       []string
	IPler         []string
	HizDk         int
	SonKullanma   int64 // unix; 0 = yok (v1'de her anahtarın bitişi vardır)
	Olusturma     int64
	SonKullanim   int64
	SonKullanimIP string
	IptalAt       int64 // unix; 0 = iptal yok. Gelecekte olabilir (yenileme geçişi).
	IptalEdenID   int64
	IptalNeden    string
	// users JOIN (yalnız OnekleGetir/Listele doldurur)
	SahipAd     string
	SahipDurum  string
	SahipUyeRol string // users.role — sahip_rol ile EŞLEŞMELİ
}

// Durum değerleri.
const (
	DurumEtkin  = "etkin"
	DurumGecis  = "gecis" // yenilendi; iptal_at gelecekte
	DurumSuresi = "suresi_doldu"
	DurumIptal  = "iptal"
	// DurumAskida: YALNIZ görünüm durumu — anahtar kendi başına geçerli ama sahibi
	// bayi askıda (users.status='suspended'). Kayıtta hiçbir şey değişmez; bayi
	// askıdan alınınca anahtar kendiliğinden yeniden çalışır (kullanıcı kararı
	// 2026-10-03). Doğrulama bu durumu middleware'de 403 bayi_askida ile reddeder.
	DurumAskida = "askida"
)

// KodBayiAskida: askıdaki bayinin anahtarıyla gelen isteğe dönen makine okunur kod.
// Oturum askısının "hesap_askida" kodundan AYRI: ön yüz o kodla paneli kapatır;
// anahtar istemcisi (WHMCS/betik) ise bunun geçici olduğunu bilmeli.
const (
	KodBayiAskida   = "bayi_askida"
	BayiAskidaMesaj = "bayi hesabı askıya alınmış; API anahtarı bayi askıdan alınınca yeniden çalışır"
)

// SahipAskida: anahtarın sahibi askıdaki bir bayi mi? Askının TEK kaynağı
// users.status (reseller.Guncelle yazar; hesapaski bayi zinciri de oradan okur).
func (k *Kayit) SahipAskida() bool {
	return k.SahipRol == RolBayi && k.SahipUyeRol == "reseller" && k.SahipDurum == "suspended"
}

// GorunenDurum: listede gösterilecek durum — geçerli bir anahtarın sahibi askıdaki
// bayiyse DurumAskida, değilse Durum. İptal/süresi dolmuş anahtar askıda görünmez
// (askıdan alınınca da geri gelmeyecek).
func (k *Kayit) GorunenDurum(simdi int64) string {
	d := k.Durum(simdi)
	if (d == DurumEtkin || d == DurumGecis) && k.SahipAskida() {
		return DurumAskida
	}
	return d
}

// Durum: anahtarın şu anki durumu.
func (k *Kayit) Durum(simdi int64) string {
	if k.IptalAt > 0 && k.IptalAt <= simdi {
		return DurumIptal
	}
	if k.SonKullanma > 0 && k.SonKullanma <= simdi {
		return DurumSuresi
	}
	if k.IptalAt > 0 {
		return DurumGecis
	}
	return DurumEtkin
}

// Gecerli: istek anında kabul edilebilir mi (iptal edilmemiş, süresi dolmamış)?
func (k *Kayit) Gecerli(simdi int64) bool {
	d := k.Durum(simdi)
	return d == DurumEtkin || d == DurumGecis
}

const secimKolonlari = `k.id, k.sahip_id, k.sahip_rol, k.ad, k.onek, k.ozet, k.son4, k.izinler, k.izinli_ipler,
	k.hiz_dk, COALESCE(UNIX_TIMESTAMP(k.son_kullanma_at),0), COALESCE(UNIX_TIMESTAMP(k.olusturma_at),0),
	COALESCE(UNIX_TIMESTAMP(k.son_kullanim_at),0), k.son_kullanim_ip, COALESCE(UNIX_TIMESTAMP(k.iptal_at),0),
	COALESCE(k.iptal_eden_id,0), k.iptal_neden,
	COALESCE(u.username,''), COALESCE(u.status,''), COALESCE(u.role,'')`

type tarayici interface{ Scan(...any) error }

func satirOku(s tarayici) (*Kayit, error) {
	var k Kayit
	var iz, ip string
	if err := s.Scan(&k.ID, &k.SahipID, &k.SahipRol, &k.Ad, &k.Onek, &k.Ozet, &k.Son4, &iz, &ip,
		&k.HizDk, &k.SonKullanma, &k.Olusturma, &k.SonKullanim, &k.SonKullanimIP, &k.IptalAt,
		&k.IptalEdenID, &k.IptalNeden, &k.SahipAd, &k.SahipDurum, &k.SahipUyeRol); err != nil {
		return nil, err
	}
	k.Izinler = IzinleriGoc(IzinleriCoz(iz))
	k.IPler = IzinleriCoz(ip)
	return &k, nil
}

// OnekleGetir: doğrulama yolu — önek ile TEK satır (+ sahibin durumu). Yoksa (nil, nil).
func OnekleGetir(ctx context.Context, db *sql.DB, onek string) (*Kayit, error) {
	row := db.QueryRowContext(ctx, `SELECT `+secimKolonlari+`
		FROM api_anahtarlari k LEFT JOIN users u ON u.id = k.sahip_id WHERE k.onek = ?`, onek)
	k, err := satirOku(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return k, err
}

// IDGetir: yönetim yolu. Yoksa (nil, nil).
func IDGetir(ctx context.Context, db *sql.DB, id int64) (*Kayit, error) {
	row := db.QueryRowContext(ctx, `SELECT `+secimKolonlari+`
		FROM api_anahtarlari k LEFT JOIN users u ON u.id = k.sahip_id WHERE k.id = ?`, id)
	k, err := satirOku(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return k, err
}

// Ekle: yeni satır; id döner.
func Ekle(ctx context.Context, db *sql.DB, k *Kayit) (int64, error) {
	var bitis any
	if k.SonKullanma > 0 {
		bitis = k.SonKullanma
	}
	res, err := db.ExecContext(ctx, `INSERT INTO api_anahtarlari
		(sahip_id, sahip_rol, ad, onek, ozet, son4, izinler, izinli_ipler, hiz_dk, son_kullanma_at)
		VALUES (?,?,?,?,?,?,?,?,?, FROM_UNIXTIME(?))`,
		k.SahipID, k.SahipRol, k.Ad, k.Onek, k.Ozet, k.Son4,
		strings.Join(k.Izinler, ","), strings.Join(k.IPler, ","), k.HizDk, bitis)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Listele: YALNIZ (sahipID, rol) çiftinin anahtarları — başka sahibi listeleyen
// bir yol YOKTUR. İptal edilmişler de döner (iz kalır), en yeniler önce, en çok 500.
func Listele(ctx context.Context, db *sql.DB, sahipID int64, rol string) ([]*Kayit, error) {
	if sahipID <= 0 || rol == "" {
		return nil, errors.New("geçersiz sahip")
	}
	rows, err := db.QueryContext(ctx, `SELECT `+secimKolonlari+` FROM api_anahtarlari k
		LEFT JOIN users u ON u.id = k.sahip_id
		WHERE k.sahip_id=? AND k.sahip_rol=? ORDER BY k.id DESC LIMIT 500`, sahipID, rol)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Kayit
	for rows.Next() {
		k, err := satirOku(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// Iptal: anahtarı iptal eder (zaman = iptal anı; gelecekte olabilir — yenileme
// geçişi). sahipKosul > 0 ise YALNIZ o sahibin anahtarı etkilenir (bayi IDOR
// kilidi: yabancı satır 0 satır = bulunamadı). Zaten (daha erken) iptal edilmiş
// anahtarın iptal zamanı İLERİ alınmaz. Etkilenen satır var mı döner.
func Iptal(ctx context.Context, db *sql.DB, id, sahipKosul, eden int64, neden string, zaman int64) (bool, error) {
	q := `UPDATE api_anahtarlari SET iptal_at=FROM_UNIXTIME(?), iptal_eden_id=?, iptal_neden=?
		WHERE id=? AND (iptal_at IS NULL OR iptal_at > FROM_UNIXTIME(?))`
	args := []any{zaman, eden, kisalt(neden, 32), id, zaman}
	if sahipKosul > 0 {
		q += ` AND sahip_id=?`
		args = append(args, sahipKosul)
	}
	res, err := db.ExecContext(ctx, q, args...)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Sil: anahtar satırını KALICI siler (denetim geçmişi audit_log'da kalır; FK yok).
// sahipKosul > 0 ise YALNIZ o sahibin satırı silinir (bayi IDOR kilidi: yabancı
// satır 0 satır = bulunamadı). yalnizGecersiz → yalnız iptal edilmiş ya da süresi
// dolmuş satır silinir (karar ile silme arasında durum değişse bile etkin anahtar
// onaysız silinmesin). Silindi mi döner.
func Sil(ctx context.Context, db *sql.DB, id, sahipKosul int64, yalnizGecersiz bool, simdi int64) (bool, error) {
	q := `DELETE FROM api_anahtarlari WHERE id=?`
	args := []any{id}
	if sahipKosul > 0 {
		q += ` AND sahip_id=?`
		args = append(args, sahipKosul)
	}
	if yalnizGecersiz {
		q += ` AND ((iptal_at IS NOT NULL AND iptal_at <= FROM_UNIXTIME(?))
			OR (son_kullanma_at IS NOT NULL AND son_kullanma_at <= FROM_UNIXTIME(?)))`
		args = append(args, simdi, simdi)
	}
	res, err := db.ExecContext(ctx, q, args...)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SahibinTumunuIptal: bir sahibin TÜM geçerli anahtarlarını şimdi iptal eder
// (bayi parolası sıfırlandı / bayi silindi). Bayi askısı İPTAL ETMEZ — askı
// doğrulama anında denetlenir (SahipAskida). Etkilenen sayı döner.
// Hata günlüğe yazılır ve döner; çağıran asıl işlemi BOZMAZ ama hatayı denetime
// yansıtabilir.
func SahibinTumunuIptal(ctx context.Context, db *sql.DB, sahipID, eden int64, neden string) (int64, error) {
	if db == nil || sahipID <= 0 {
		return 0, nil
	}
	res, err := db.ExecContext(ctx, `UPDATE api_anahtarlari
		SET iptal_at=NOW(), iptal_eden_id=?, iptal_neden=?
		WHERE sahip_id=? AND (iptal_at IS NULL OR iptal_at > NOW())`, eden, kisalt(neden, 32), sahipID)
	if err != nil {
		// Tablo yok (göç koşmadı) → anahtar da yok; gürültü yapma.
		if strings.Contains(strings.ToLower(err.Error()), "doesn't exist") {
			return 0, nil
		}
		log.Printf("GÜVENLİK: API anahtarları iptal edilemedi (sahip=%d neden=%s): %v", sahipID, neden, err)
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// SonKullanimYaz: son kullanım zamanı + IP (çağıran kısıtlar; hata yutulur).
func SonKullanimYaz(db *sql.DB, id int64, ip string) {
	if db == nil {
		return
	}
	ctx, iptal := context.WithTimeout(context.Background(), 3*time.Second)
	defer iptal()
	_, _ = db.ExecContext(ctx, `UPDATE api_anahtarlari SET son_kullanim_at=NOW(), son_kullanim_ip=? WHERE id=?`, kisalt(ip, 45), id)
}

// kisalt: rune sınırında en çok n karakter.
func kisalt(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
