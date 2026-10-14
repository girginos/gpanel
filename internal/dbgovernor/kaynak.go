package dbgovernor

// kaynak.go — MariaDB ile konuşan katman (DİKİŞ). Motor yalnız Olcer ve Yonetici arayüzlerini
// bilir; üretimde ikisini de root soket kimliğiyle `mysql` CLI'ı karşılar (panelin geri kalanıyla
// aynı yol: kaynaklimit, hesaplar). Testler sahte Olcer/Yonetici verir ya da Calistir'i değiştirir.
//
// SQL güvenliği: komutlara yalnız (a) doğrulanmış kimlikler (kullaniciGecerli), (b) host allowlist'i
// (hostGecerli) ve (c) tamsayılar girer; serbest metin ASLA birleştirilmez.

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/hesaplar"
)

// Sorgu: çalışmakta olan bir deyim (information_schema.PROCESSLIST satırı).
type Sorgu struct {
	ID        int64
	QueryID   int64
	Kullanici string
	Sure      time.Duration
	Durum     string // STATE
	Metin     string // INFO'nun ilk karakterleri (sınıflandırma için)
}

// Baglanti: bir istemci bağlantısı (information_schema.PROCESSLIST satırı; Sleep dahil).
type Baglanti struct {
	ID        int64
	Kullanici string
}

// Hesap: mysql.user satırı (bir kullanıcının bir host'taki hesabı).
type Hesap struct {
	Host     string
	Baglanti int // Max_user_connections (0 = sınırsız)
}

// Olcer: ölçüm kaynağı.
type Olcer interface {
	// KullaniciCPU: kullanıcı → kümülatif CPU saniyesi (USER_STATISTICS.CPU_TIME). userstat kapalıysa
	// boş harita (hata değil).
	KullaniciCPU(ctx context.Context) (map[string]float64, error)
	// Calisanlar: o an çalışan (Sleep olmayan) deyimler.
	Calisanlar(ctx context.Context) ([]Sorgu, error)
}

// Yonetici: MariaDB üzerinde işlem yapan katman.
type Yonetici interface {
	// Hesaplar: kullanıcıların mysql.user hesapları.
	Hesaplar(ctx context.Context, kullanicilar []string) (map[string][]Hesap, error)
	// BaglantiAyarla: ALTER USER 'u'@'h' WITH MAX_USER_CONNECTIONS n (yalnız bu seçenek değişir).
	BaglantiAyarla(ctx context.Context, kullanici, host string, n int) error
	// SorguOldur: KILL QUERY ID <query_id> — bağlantıyı KESMEZ, yalnız o deyimi sonlandırır.
	SorguOldur(ctx context.Context, queryID int64) error
	// Baglantilar: kullanıcının AÇIK bağlantıları. Çağıranın (kısıcının) KENDİ bağlantısı dahil
	// edilmez; yalnız tam o kullanıcının bağlantıları döner.
	Baglantilar(ctx context.Context, kullanici string) ([]Baglanti, error)
	// BaglantiKes: KILL CONNECTION <id> — bağlantıyı koparır (3. seviye).
	BaglantiKes(ctx context.Context, id int64) error
	// UserstatAcikMi / UserstatAc: @@GLOBAL.userstat (dinamik; yeniden başlatma gerekmez).
	UserstatAcikMi(ctx context.Context) (bool, error)
	UserstatAc(ctx context.Context) error
}

var (
	// 1..64: panel kiracı DB kullanıcısını `<önek>_<sonek(<=32)>` ile üretir, toplam 64'e kadar çıkabilir
	// (hesaplar.GecerliDBKimlik); 32'de kesmek uzun adlı kullanıcıyı governor'dan tamamen muaf bırakırdı.
	reKullanici = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)
	reHost      = regexp.MustCompile(`^[A-Za-z0-9_.%\-]{1,64}$`)
)

// sistemHesaplari: kaynaklimit.dbGovernorSystemUsers ile aynı küme (kopya: kaynaklimit'i
// içe aktarmak döngü yaratırdı). Bu liste hesaplar.SistemDBHesabiMi'ye EK ikinci savunmadır.
var sistemHesaplari = map[string]bool{
	"root": true, "mysql": true, "mariadb.sys": true, "panel": true,
	"event_scheduler": true, "debian-sys-maint": true, "replication": true,
	"repl": true, "healthcheck": true, "": true,
}

// kullaniciGecerli: SQL'e girebilecek güvenli bir KİRACI kullanıcı adı mı? Sistem/servis
// hesapları (root, panel, pma, roundcube, mail…) ASLA kiracı sayılmaz.
func kullaniciGecerli(u string) bool {
	if !reKullanici.MatchString(u) || sistemHesaplari[strings.ToLower(u)] {
		return false
	}
	return !hesaplar.SistemDBHesabiMi(u)
}

func hostGecerli(h string) bool { return reHost.MatchString(h) }

// ---------------------------------------------------------------------------------------------
// CLI gerçekleştirimi

// cliKomut: tek SQL'i root soketiyle çalıştırır (-N başlıksız, -B sekmeli). Testler değiştirir.
var cliKomut = func(ctx context.Context, sorgu string) (string, error) {
	c, iptal := context.WithTimeout(ctx, 20*time.Second)
	defer iptal()
	cmd := exec.CommandContext(c, "mysql", "-uroot", "-N", "-B", "-e", sorgu)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("mysql: %w: %s", err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// CLI: Olcer + Yonetici'nin üretim gerçekleştirimi.
type CLI struct {
	// Calistir: nil ise cliKomut.
	Calistir func(ctx context.Context, sorgu string) (string, error)
}

func (c CLI) calistir(ctx context.Context, sorgu string) (string, error) {
	if c.Calistir != nil {
		return c.Calistir(ctx, sorgu)
	}
	return cliKomut(ctx, sorgu)
}

func (c CLI) KullaniciCPU(ctx context.Context) (map[string]float64, error) {
	out, err := c.calistir(ctx, "SELECT USER, CPU_TIME FROM information_schema.USER_STATISTICS")
	if err != nil {
		// userstat kapalıyken tablo boştur (hata vermez); gerçek hata = MariaDB okunamıyor.
		return nil, err
	}
	return kullaniciCPUCoz(out), nil
}

// kullaniciCPUCoz: "USER\tCPU_TIME" satırları. Bozuk satır atlanır.
func kullaniciCPUCoz(out string) map[string]float64 {
	m := map[string]float64{}
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimRight(ln, "\r"), "\t")
		if len(f) != 2 {
			continue
		}
		v, ok := sayi(f[1])
		if !ok || v < 0 {
			continue
		}
		m[f[0]] = v
	}
	return m
}

func (c CLI) Calisanlar(ctx context.Context) ([]Sorgu, error) {
	// -B kipi sekme/satırsonunu \t \n olarak kaçışlar; STATE/INFO yine de REPLACE ile temizlenir.
	out, err := c.calistir(ctx, "SELECT ID, USER, TIME_MS, QUERY_ID, "+
		"REPLACE(REPLACE(IFNULL(STATE,''),CHAR(9),' '),CHAR(10),' '), "+
		"REPLACE(REPLACE(LEFT(IFNULL(INFO,''),160),CHAR(9),' '),CHAR(10),' ') "+
		"FROM information_schema.PROCESSLIST WHERE COMMAND IN ('Query','Execute')")
	if err != nil {
		return nil, err
	}
	return calisanlarCoz(out), nil
}

// calisanlarCoz: "ID\tUSER\tTIME_MS\tQUERY_ID\tSTATE\tINFO" satırları.
func calisanlarCoz(out string) []Sorgu {
	var l []Sorgu
	for _, ln := range strings.Split(out, "\n") {
		f := strings.SplitN(strings.TrimRight(ln, "\r"), "\t", 6)
		if len(f) < 6 {
			continue
		}
		id, e1 := strconv.ParseInt(strings.TrimSpace(f[0]), 10, 64)
		ms, ok2 := sayi(f[2])
		qid, e3 := strconv.ParseInt(strings.TrimSpace(f[3]), 10, 64)
		if e1 != nil || !ok2 || e3 != nil || ms < 0 {
			continue
		}
		l = append(l, Sorgu{ID: id, QueryID: qid, Kullanici: f[1], Sure: time.Duration(ms * float64(time.Millisecond)),
			Durum: f[4], Metin: f[5]})
	}
	return l
}

func (c CLI) Hesaplar(ctx context.Context, kullanicilar []string) (map[string][]Hesap, error) {
	var q []string
	for _, u := range kullanicilar {
		if kullaniciGecerli(u) {
			q = append(q, "'"+u+"'")
		}
	}
	m := map[string][]Hesap{}
	if len(q) == 0 {
		return m, nil
	}
	out, err := c.calistir(ctx, "SELECT User, Host, Max_user_connections FROM mysql.user WHERE User IN ("+strings.Join(q, ",")+")")
	if err != nil {
		return nil, err
	}
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimRight(ln, "\r"), "\t")
		if len(f) != 3 || !kullaniciGecerli(f[0]) || !hostGecerli(f[1]) {
			continue // bozuk/güvensiz host biçimi: dokunma
		}
		n, err := strconv.Atoi(strings.TrimSpace(f[2]))
		if err != nil || n < 0 {
			continue
		}
		m[f[0]] = append(m[f[0]], Hesap{Host: f[1], Baglanti: n})
	}
	return m, nil
}

func (c CLI) BaglantiAyarla(ctx context.Context, kullanici, host string, n int) error {
	if !kullaniciGecerli(kullanici) || !hostGecerli(host) || n < 0 {
		return fmt.Errorf("güvenlik: geçersiz hesap %q@%q", kullanici, host)
	}
	_, err := c.calistir(ctx, fmt.Sprintf("ALTER USER '%s'@'%s' WITH MAX_USER_CONNECTIONS %d", kullanici, host, n))
	return err
}

func (c CLI) SorguOldur(ctx context.Context, queryID int64) error {
	if queryID <= 0 {
		return fmt.Errorf("geçersiz query id")
	}
	_, err := c.calistir(ctx, fmt.Sprintf("KILL QUERY ID %d", queryID))
	return err
}

func (c CLI) Baglantilar(ctx context.Context, kullanici string) ([]Baglanti, error) {
	if !kullaniciGecerli(kullanici) {
		return nil, fmt.Errorf("güvenlik: geçersiz kullanıcı %q", kullanici)
	}
	// BINARY: MariaDB hesap adları harf DUYARLI; kendi bağlantımız (CONNECTION_ID) hariç.
	out, err := c.calistir(ctx, fmt.Sprintf(
		"SELECT ID, USER FROM information_schema.PROCESSLIST WHERE BINARY USER='%s' AND ID<>CONNECTION_ID()", kullanici))
	if err != nil {
		return nil, err
	}
	var l []Baglanti
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimRight(ln, "\r"), "\t")
		if len(f) != 2 {
			continue
		}
		id, e := strconv.ParseInt(strings.TrimSpace(f[0]), 10, 64)
		if e != nil || id <= 0 {
			continue
		}
		l = append(l, Baglanti{ID: id, Kullanici: f[1]})
	}
	return l, nil
}

func (c CLI) BaglantiKes(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("geçersiz bağlantı id")
	}
	_, err := c.calistir(ctx, fmt.Sprintf("KILL CONNECTION %d", id))
	return err
}

func (c CLI) UserstatAcikMi(ctx context.Context) (bool, error) {
	out, err := c.calistir(ctx, "SELECT @@GLOBAL.userstat")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "1", nil
}

func (c CLI) UserstatAc(ctx context.Context) error {
	_, err := c.calistir(ctx, "SET GLOBAL userstat=1")
	return err
}

// ---------------------------------------------------------------------------------------------
// Sınıflandırma yardımcıları

// cpuDurumlari: CPU yakan (kilit/ağ/uyku beklemeyen) deyim durumları — anlık CPU göstergesi
// yalnız bunları sayar. Bilinmeyen durum SAYILMAZ (aşırı saymak haksız kısıt demektir).
var cpuDurumlari = []string{"executing", "sending data", "statistics", "sorting", "creating sort index",
	"copying to tmp", "creating tmp", "removing dup", "searching rows", "preparing", "optimizing", "filling schema"}

func cpuYakiyorMu(durum string) bool {
	d := strings.ToLower(strings.TrimSpace(durum))
	for _, p := range cpuDurumlari {
		if strings.HasPrefix(d, p) {
			return true
		}
	}
	return false
}

// okumaSorgusuMu: deyim güvenle öldürülebilecek bir OKUMA mı? Yalnız SELECT (başındaki boşluk,
// parantez ve /* */ -- # yorumları atılır). WITH (CTE) bilerek DIŞARIDA: "WITH … DELETE/UPDATE"
// yazabilir. Yazma deyimi (INSERT/UPDATE/DELETE/ALTER…) KILL edilmez: yarım kalan yazma MyISAM'de
// tutarsızlık, InnoDB'de pahalı geri alma demektir; yazmayı MAX_UPDATES_PER_HOUR ve bağlantı
// sınırı kısar.
func okumaSorgusuMu(metin string) bool {
	s := metin
	for {
		s = strings.TrimLeft(s, " \t\r\n(")
		switch {
		case strings.HasPrefix(s, "/*"):
			i := strings.Index(s, "*/")
			if i < 0 {
				return false
			}
			s = s[i+2:]
		case strings.HasPrefix(s, "--"), strings.HasPrefix(s, "#"):
			i := strings.IndexByte(s, '\n')
			if i < 0 {
				return false
			}
			s = s[i+1:]
		default:
			if len(s) < 6 {
				return false
			}
			if !strings.EqualFold(s[:6], "select") {
				return false
			}
			return len(s) == 6 || !(s[6] == '_' || s[6] >= '0' && s[6] <= '9' || s[6] >= 'a' && s[6] <= 'z' || s[6] >= 'A' && s[6] <= 'Z')
		}
	}
}
