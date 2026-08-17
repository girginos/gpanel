package optimize

// Ortak tipler + yedek/uygula/rollback altyapısı.

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const YedekDizini = "/var/backups/gpanel-optimize"

type Seviye string

const (
	SeviyeBilgi  Seviye = "bilgi"
	SeviyeOnemli Seviye = "onemli"
	SeviyeKritik Seviye = "kritik"
)

// Oneri — tek bir parametre için değişiklik önerisi.
type Oneri struct {
	ID       string `json:"id"`     // stabil: <servis>:<param>
	Servis   string `json:"servis"` // mariadb|nginx|apache|phpfpm|sysctl
	Param    string `json:"param"`  // örn. innodb_buffer_pool_size
	Etiket   string `json:"etiket"` // İnsan-okur ad
	Mevcut   string `json:"mevcut"`
	Onerilen string `json:"onerilen"`
	Gerekce  string `json:"gerekce"`
	Seviye   Seviye `json:"seviye"`
	Dosya    string `json:"dosya"` // uygulama hedef yolu
	Etki     string `json:"etki"`  // "reload" | "restart" | "yok"
}

// ServisAnaliz — tek bir servis için tam analiz raporu.
type ServisAnaliz struct {
	Kod         string              `json:"kod"`
	Ad          string              `json:"ad"`
	Ikon        string              `json:"ikon"`
	Durum       *ServisDurum        `json:"durum"`
	LogSinyal   []string            `json:"log_sinyal"` // log'lardan çıkarılan uyarılar
	Oneriler    []Oneri             `json:"oneriler"`
	NotYok      string              `json:"not_yok,omitempty"` // servis yoksa açıklama
	SonUygulama []ServisSonUygulama `json:"son_uygulama,omitempty"`
}

type AnalizRaporu struct {
	Sistem    *Sistem        `json:"sistem"`
	Servisler []ServisAnaliz `json:"servisler"`
	Zaman     time.Time      `json:"zaman"`
}

// UygulaIstek — 1+ öneri ID'sini bir bütün olarak uygular (aynı servis).
type UygulaIstek struct {
	Servis  string   `json:"servis"`
	OneriID []string `json:"oneri_id"`
}

// UygulaSonuc — dönen özet.
type UygulaSonuc struct {
	YedekID   string   `json:"yedek_id"`
	Yol       string   `json:"yol"`
	Uygulanan []string `json:"uygulanan"`
	Etki      string   `json:"etki"`
	Basarili  bool     `json:"basarili"`
	Mesaj     string   `json:"mesaj"`
}

// YedekKayit — DB satırı.
type YedekKayit struct {
	ID           int64  `json:"id"`
	Yedek        string `json:"yedek"` // yedek dosya yolu
	Hedef        string `json:"hedef"` // orijinal yol
	Servis       string `json:"servis"`
	Aciklama     string `json:"aciklama"`
	AktorUID     *int64 `json:"aktor_uid,omitempty"`
	CreatedAt    string `json:"created_at"`
	RolledBack   bool   `json:"rolled_back"`
	Uygulananlar string `json:"uygulananlar,omitempty"`
}

// DosyaYedekle — hedef dosyayı zaman damgalı yedeğe kopyala + DB kaydı.
// Hedef dosya yoksa boş yedek oluşturur (rollback = dosyayı sil).
func DosyaYedekle(db *sql.DB, servis, hedef, aciklama string, aktorUID *int64) (*YedekKayit, error) {
	if err := os.MkdirAll(filepath.Join(YedekDizini, servis), 0o750); err != nil {
		return nil, err
	}
	ts := time.Now().Format("20060102-150405")
	yedekAd := fmt.Sprintf("%s.%s.yedek", filepath.Base(hedef), ts)
	yedekYol := filepath.Join(YedekDizini, servis, yedekAd)
	// Hedef varsa kopyala
	if _, err := os.Stat(hedef); err == nil {
		src, err := os.Open(hedef)
		if err != nil {
			return nil, err
		}
		defer src.Close()
		dst, err := os.OpenFile(yedekYol, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
		if err != nil {
			return nil, err
		}
		defer dst.Close()
		if _, err := io.Copy(dst, src); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else {
		// Hedef yoktu — sentinel
		_ = os.WriteFile(yedekYol+".yoktu", []byte("orijinal dosya yoktu"), 0o640)
	}

	if db == nil {
		return &YedekKayit{Yedek: yedekYol, Hedef: hedef, Servis: servis, Aciklama: aciklama}, nil
	}

	res, err := db.Exec(
		`INSERT INTO cp_optimize_yedekler (servis, yedek_yol, hedef_yol, aciklama, aktor_uid)
		 VALUES (?, ?, ?, ?, ?)`,
		servis, yedekYol, hedef, aciklama, aktorUID)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &YedekKayit{ID: id, Yedek: yedekYol, Hedef: hedef, Servis: servis, Aciklama: aciklama}, nil
}

// YedekListele — son N yedek.
func YedekListele(db *sql.DB, limit int) ([]YedekKayit, error) {
	if db == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := db.Query(
		`SELECT id, servis, yedek_yol, hedef_yol, aciklama, IFNULL(uygulananlar,""), aktor_uid, geri_alindi, created_at
		 FROM cp_optimize_yedekler ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []YedekKayit{}
	for rows.Next() {
		var k YedekKayit
		if err := rows.Scan(&k.ID, &k.Servis, &k.Yedek, &k.Hedef, &k.Aciklama, &k.Uygulananlar, &k.AktorUID, &k.RolledBack, &k.CreatedAt); err == nil {
			out = append(out, k)
		}
	}
	return out, nil
}

// YedekGeriYukle — yedeği hedefe geri kopyalar (rollback).
func YedekGeriYukle(db *sql.DB, yedekID int64) error {
	if db == nil {
		return errors.New("db yok")
	}
	var yedek, hedef, servis string
	err := db.QueryRow(
		`SELECT yedek_yol, hedef_yol, servis FROM cp_optimize_yedekler
		 WHERE id=? AND geri_alindi=0`, yedekID).Scan(&yedek, &hedef, &servis)
	if err != nil {
		return err
	}
	// "yoktu" sentinel varsa hedefi sil
	if _, err := os.Stat(yedek + ".yoktu"); err == nil {
		_ = os.Remove(hedef)
	} else {
		src, err := os.Open(yedek)
		if err != nil {
			return err
		}
		defer src.Close()
		tmp := hedef + ".rollback.tmp"
		dst, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		if _, err := io.Copy(dst, src); err != nil {
			dst.Close()
			_ = os.Remove(tmp)
			return err
		}
		dst.Close()
		if err := os.Rename(tmp, hedef); err != nil {
			return err
		}
	}
	_, _ = db.Exec(`UPDATE cp_optimize_yedekler SET geri_alindi=1 WHERE id=?`, yedekID)
	// Servis reload/restart
	_ = ServisReload(servis)
	return nil
}

// ServisReload — servisin doğal reload/restart komutunu çalıştır.
func ServisReload(servis string) error {
	switch servis {
	case "nginx":
		return execRun("nginx", "-s", "reload")
	case "apache", "httpd":
		return execRun("systemctl", "reload", "httpd")
	case "mariadb":
		return execRun("systemctl", "restart", "mariadb")
	case "phpfpm", "php-fpm":
		return execRun("systemctl", "reload", "php-fpm")
	case "sysctl":
		return execRun("sysctl", "--system")
	}
	return nil
}

// SatirYazVeyaGuncelle — .ini/.cnf tarzı dosyada `anahtar = deger` satırı ekle/güncelle.
// Section-aware değil (nginx için değil; my.cnf, php-fpm.d/*.conf için).
// Anahtar birden fazla kere varsa hepsini günceller. Yoksa dosyanın sonuna ekler.
func SatirYazVeyaGuncelle(dosya, anahtar, deger string) error {
	b, err := os.ReadFile(dosya)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s := string(b)
	yeniSatir := fmt.Sprintf("%s = %s", anahtar, deger)
	bulundu := false
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		trim := strings.TrimSpace(ln)
		if trim == "" || strings.HasPrefix(trim, "#") || strings.HasPrefix(trim, ";") {
			continue
		}
		k, _, ok := strings.Cut(trim, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(k) == anahtar {
			// yorumları koru (baştaki whitespace)
			lead := ln[:len(ln)-len(strings.TrimLeft(ln, " \t"))]
			lines[i] = lead + yeniSatir
			bulundu = true
		}
	}
	if !bulundu {
		if s != "" && !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		s += yeniSatir + "\n"
		return atomikYaz(dosya, []byte(s), 0o644)
	}
	return atomikYaz(dosya, []byte(strings.Join(lines, "\n")), 0o644)
}

func atomikYaz(yol string, b []byte, mode os.FileMode) error {
	dir := filepath.Dir(yol)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := yol + ".tmp"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	return os.Rename(tmp, yol)
}

// execRun — hata varsa döner, çıktıyı bastırır.
func execRun(bin string, args ...string) error {
	return exec.Command(bin, args...).Run()
}

// execOutput — combined output (stdout+stderr).
func execOutput(bin string, args ...string) (string, error) {
	out, err := exec.Command(bin, args...).CombinedOutput()
	return string(out), err
}

// readAll — dosyayı oku, yoksa boş.
func readAll(yol string) ([]byte, error) {
	b, err := os.ReadFile(yol)
	if errors.Is(err, os.ErrNotExist) {
		return []byte{}, nil
	}
	return b, err
}

// kirp — string'in maks n karakter kismini alir (uzun ise "..." ekler).
func kirp(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// YedekUygulamalariYaz — bir yedege ait uygulananlari (param=deger,...) kaydeder.
func YedekUygulamalariYaz(db *sql.DB, yedekID int64, uygulananlar string) {
	if db == nil {
		return
	}
	_, _ = db.Exec(`UPDATE cp_optimize_yedekler SET uygulananlar=? WHERE id=?`, uygulananlar, yedekID)
}

// ServisSonUygulama — bir servisin son N uygulamasini ozet olarak dondurur.
type ServisSonUygulama struct {
	YedekID      int64  `json:"yedek_id"`
	Tarih        string `json:"tarih"`
	Uygulananlar string `json:"uygulananlar"`
	GeriAlindi   bool   `json:"geri_alindi"`
}

func ServisSonUygulamalar(db *sql.DB, servis string, limit int) []ServisSonUygulama {
	if db == nil {
		return nil
	}
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	rows, err := db.Query(
		`SELECT id, created_at, IFNULL(uygulananlar,""), geri_alindi FROM cp_optimize_yedekler
		 WHERE servis=? AND uygulananlar IS NOT NULL AND uygulananlar != ""
		 ORDER BY id DESC LIMIT ?`, servis, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []ServisSonUygulama{}
	for rows.Next() {
		var u ServisSonUygulama
		if rows.Scan(&u.YedekID, &u.Tarih, &u.Uygulananlar, &u.GeriAlindi) == nil {
			out = append(out, u)
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────
// DUSURME KORUMASI
//
// 🔴 Analiz, onerilen degeri RAM/CPU'dan hesapliyor ve mevcut degerin
// zaten daha IYI olup olmadigina bakmiyordu. Olculen ornekler:
//
//	fs.file-max              2097152 -> 500000   (4x DUSUS)
//	net.core.netdev_max_backlog 16384 -> 5000    (3x DUSUS)
//	client_max_body_size      10240m -> 128M     (yukleme limiti duser)
//
// Bunlar "optimizasyon" degil regresyon: operator butonu tiklayinca
// sunucusu kotulesiyordu. Kapasite/tavan niteligindeki parametrelerde
// mevcut deger onerilenden buyukse oneri HIC URETILMEZ.
//
// Iki yonlu olanlar (swappiness, dirty_ratio, timeout'lar, gzip seviyesi)
// bu listede DEGIL — orada dusurmek mesru bir tercih olabilir.
var SadeceArtir = map[string]bool{
	// kernel / kaynak tavanlari
	"fs.file-max": true, "fs.nr_open": true, "fs.aio-max-nr": true,
	"fs.inotify.max_user_watches": true, "kernel.pid_max": true,
	"net.core.somaxconn": true, "net.core.netdev_max_backlog": true,
	"net.core.rmem_max": true, "net.core.wmem_max": true,
	"net.ipv4.tcp_max_syn_backlog": true, "net.ipv4.tcp_max_tw_buckets": true,
	"net.netfilter.nf_conntrack_max": true, "vm.max_map_count": true,
	// nginx
	"worker_connections": true, "worker_rlimit_nofile": true,
	"types_hash_max_size": true, "server_names_hash_bucket_size": true,
	"client_max_body_size": true, "keepalive_requests": true,
	// apache
	"MaxRequestWorkers": true, "ServerLimit": true,
	// php-fpm
	"pm.max_children": true,
	// mariadb
	"innodb_buffer_pool_size": true, "max_connections": true,
	"tmp_table_size": true, "max_heap_table_size": true,
	"thread_cache_size": true,
}

var boyutEkRe = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)\s*([kKmMgGtT]?)[bB]?$`)

// ByteCevir — "512M", "1g", "2097152" gibi degerleri sayiya cevirir.
// Cevrilemezse (auto, on/off, "1024 65535" gibi aralik) ok=false.
func ByteCevir(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	m := boyutEkRe.FindStringSubmatch(v)
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	switch strings.ToLower(m[2]) {
	case "k":
		n *= 1024
	case "m":
		n *= 1024 * 1024
	case "g":
		n *= 1024 * 1024 * 1024
	case "t":
		n *= 1024 * 1024 * 1024 * 1024
	}
	return n, true
}

// OneriGecerli — bu oneri kullaniciya gosterilmeli mi?
// Mevcut deger onerilenle ayniysa veya "sadece artir" sinifinda mevcut
// zaten daha buyukse: HAYIR.
func OneriGecerli(param, mevcut, onerilen string) bool {
	if mevcut == "" || mevcut == onerilen {
		return false
	}
	// "[havuz] pm.max_children" gibi onekleri temizle
	sade := param
	if i := strings.Index(sade, "] "); i >= 0 {
		sade = sade[i+2:]
	}
	if !SadeceArtir[sade] {
		return true
	}
	mv, ok1 := ByteCevir(mevcut)
	ov, ok2 := ByteCevir(onerilen)
	if !ok1 || !ok2 {
		return true // sayisal karsilastirilamiyor — karar verme, goster
	}
	return ov > mv
}

// AnaliziSuz — bir servis analizindeki onerileri dusurme korumasindan gecirir.
func AnaliziSuz(a *ServisAnaliz) {
	if a == nil {
		return
	}
	kalan := a.Oneriler[:0]
	for _, o := range a.Oneriler {
		if OneriGecerli(o.Param, o.Mevcut, o.Onerilen) {
			kalan = append(kalan, o)
		}
	}
	a.Oneriler = kalan
}
