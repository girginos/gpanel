package arsiv

// sql.go — veritabanı dökümü (mysqldump çıktısı) GÜVENLİK TARAYICISI.
//
// Döküm güvenilmez girdidir: kiracının MySQL kullanıcısı olmayan, root'un (ya da
// ayrıcalıklı bir istemcinin) çalıştıracağı bir akış olabilir. Bu tarayıcı dökümü
// DİSKE YAZMAZ ve çalıştırmaz; tek geçişte, sabit bellekle sözcüksel çözümleyip
// (tırnak/yorum/sürümlü yorum farkındalığıyla) kurallara uymayanı reddeder.
//
// Strateji ALLOWLIST: bir ifade yalnız ilk sözcüğü meşru mysqldump/phpMyAdmin
// ifadelerinden biriyse geçer (SET, CREATE, DROP, INSERT, REPLACE, LOCK, UNLOCK,
// ALTER, START, BEGIN, COMMIT, ROLLBACK, DELIMITER). Ayrıca (her yerde):
//   - GRANT/REVOKE/PREPARE sözcüğü, CREATE/DROP/ALTER penceresinde USER/ROLE/
//     DATABASE/SCHEMA/SERVER/TABLESPACE/LOGFILE, INTO OUTFILE/DUMPFILE, LOAD_FILE,
//     SONAME (UDF), SLEEP/BENCHMARK → ret.
//   - Satır başındaki mysql İSTEMCİ komutları (system, source, use, connect, tee,
//     pager, edit …) ve kod bölgesindeki her ters bölü (\! \. \u …) → ret.
//   - SET: GLOBAL/PERSIST/PASSWORD/SELECT, işlev çağrısı, onaltılık sabit ve
//     NO_BACKSLASH_ESCAPES / ANSI içeren (birleşik) dizgi → ret. 🔴 Çözümleyici fark
//     (differential) kapatılır: NO_BACKSLASH_ESCAPES kipinde '\'' dizgiyi bitirir; biz
//     bitirmeyiz → o kipin açılması engellenir, iki ayrıştırıcı hep aynı sözcükleri görür.
//   - DEFINER=... yan tümcesi reddedilmez (mysqldump her görünüme/rutine yazar) ama
//     SAYILIR: faz 2 içe aktarmada süzer (aktar_db.go reDefiner) ve --sandbox şarttır.
//
// Bilinen sınır: çapraz-veritabanı adları (`baska`.`tablo`) sözcüksel olarak
// engellenmez — mysqldump görünüm gövdelerini kaynak veritabanı adıyla niteler.
// Koruma, içe aktaran kullanıcının yalnız hedef veritabanında yetkili olmasıdır.

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
)

// ErrSQL — dökümde güvenli olmayan / bozuk ifade. Ayrıntı için *SQLHata.
var ErrSQL = errors.New("arşiv: veritabanı dökümü reddedildi")

// SQLHata — reddin kuralı. Kural SABİT bir koddur; dökümden gelen metin taşımaz.
type SQLHata struct {
	Kural string
}

func (e *SQLHata) Error() string { return "arşiv: veritabanı dökümü reddedildi (" + e.Kural + ")" }
func (e *SQLHata) Unwrap() error { return ErrSQL }

// Kural kodları.
const (
	KuralIzinsizIfade = "izinsiz-ifade"
	KuralYonetim      = "kullanici-ya-da-veritabani-yonetimi"
	KuralDosya        = "dosya-erisimi"
	KuralIstemci      = "istemci-komutu"
	KuralSQLKipi      = "sql-kipi"
	KuralKaynak       = "kaynak-tuketimi"
	KuralYetki        = "yetki-ifadesi"
	KuralBozuk        = "bozuk-ya-da-kesik"
	KuralBoyut        = "boyut-siniri"
	KuralOlmayanIfade = "dinamik-sql"
)

// SQLSinir — sıfır varsayılanla doldurulur.
type SQLSinir struct {
	ToplamBayt int64 // taranan döküm baytı üst sınırı (varsayılan 64 GiB)
}

// SQLRapor — tarama özeti (yalnız sayılar; içerik yok).
type SQLRapor struct {
	Bayt    int64
	Ifade   int
	Tablo   int  // yaklaşık: CREATE ... TABLE sayısı
	Rutin   int  // PROCEDURE/FUNCTION/TRIGGER/EVENT/VIEW tanımı sayısı
	Definer int  // DEFINER= görülen ifade sayısı (faz 2 süzer)
	Tamam   bool // "-- Dump completed" damgası görüldü
}

var sqlIzinli = map[string]bool{
	"SET": true, "CREATE": true, "DROP": true, "INSERT": true, "REPLACE": true,
	"LOCK": true, "UNLOCK": true, "ALTER": true, "START": true, "BEGIN": true,
	"COMMIT": true, "ROLLBACK": true, "DELIMITER": true,
}

var sqlYonetimSozcuk = map[string]bool{
	"USER": true, "ROLE": true, "DATABASE": true, "SCHEMA": true, "SERVER": true,
	"TABLESPACE": true, "LOGFILE": true,
}

// Her yerde yasak sözcükler (tırnak/yorum dışında).
var sqlYasakSozcuk = map[string]string{
	"GRANT": KuralYetki, "REVOKE": KuralYetki,
	"PREPARE": KuralOlmayanIfade, "LOAD_FILE": KuralDosya, "SONAME": KuralDosya,
	"OUTFILE": KuralDosya, "DUMPFILE": KuralDosya,
	"SLEEP": KuralKaynak, "BENCHMARK": KuralKaynak,
}

// Satır başında etkili mysql istemci komutları.
var sqlIstemciKomut = map[string]bool{
	"SYSTEM": true, "SOURCE": true, "USE": true, "CONNECT": true, "TEE": true,
	"PAGER": true, "EDIT": true, "DELIMITER": true,
}

const (
	sqlSozcukPencere = 12   // ifade başındaki saklanan sözcük sayısı
	sqlSetDizgiSinir = 4096 // SET içindeki toplam dizgi baytı
	sqlSozcukUzun    = 128  // tek sözcükte tutulan en çok bayt
)

type sqlTok byte

const (
	tokYok sqlTok = iota
	tokSozcuk
	tokSembol
	tokDizgi
	tokIdent
	tokSayi
)

type sqlCoz struct {
	br    *bufio.Reader
	ctx   context.Context
	max   int64
	delim string
	rap   SQLRapor

	pos        int64
	satirBasi  bool
	vderin     int // açık sürümlü yorum sayısı (/*! ... */)
	sonTok     sqlTok
	sonSozcuk  string
	sonBitis   int64
	pencereKap bool

	// ifade durumu
	ilk        string
	n          int
	kelime     [sqlSozcukPencere]string
	setDizgi   []byte
	definerGor bool
	hata       *SQLHata
}

// SQLTara — dökümü okuyup güvenlik kurallarından geçirir. Kural ihlalinde *SQLHata
// (errors.Is(err, ErrSQL)), okuma hatasında o hata döner.
func SQLTara(ctx context.Context, r io.Reader, s SQLSinir) (SQLRapor, error) {
	if s.ToplamBayt <= 0 {
		s.ToplamBayt = 64 << 30
	}
	c := &sqlCoz{br: bufio.NewReaderSize(r, 64<<10), ctx: ctx, max: s.ToplamBayt, delim: ";", satirBasi: true}
	err := c.calis()
	if err == nil && c.hata != nil {
		err = c.hata
	}
	c.rap.Bayt = c.pos
	return c.rap, err
}

func (c *sqlCoz) ret(kural string) error {
	if c.hata == nil {
		c.hata = &SQLHata{Kural: kural}
	}
	return c.hata
}

func (c *sqlCoz) oku() (byte, error) {
	b, err := c.br.ReadByte()
	if err != nil {
		return 0, err
	}
	c.pos++
	if c.pos > c.max {
		return 0, c.ret(KuralBoyut)
	}
	if c.pos&(1<<20-1) == 0 {
		if err := c.ctx.Err(); err != nil {
			return 0, err
		}
	}
	return b, nil
}

// bak — sonraki n baytı tüketmeden gösterir (kısa kalırsa olduğu kadar).
func (c *sqlCoz) bak(n int) []byte {
	b, _ := c.br.Peek(n)
	return b
}

func sqlBosluk(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\f' || b == '\v'
}

func sqlSozcukBasi(b byte) bool {
	return b == '_' || b == '$' || b >= 0x80 || (b|0x20 >= 'a' && b|0x20 <= 'z')
}

func sqlSozcukGovde(b byte) bool { return sqlSozcukBasi(b) || (b >= '0' && b <= '9') }

func (c *sqlCoz) calis() error {
	for {
		b, err := c.oku()
		if err == io.EOF {
			return c.bitisEOF()
		}
		if err != nil {
			return err
		}
		if b == '\n' {
			c.satirBasi = true
			continue
		}
		if sqlBosluk(b) {
			continue
		}
		if c.delimMi(b) {
			c.ifadeBitir()
			c.satirBasi = false
			continue
		}
		switch {
		case b == '\\':
			return c.ret(KuralIstemci)
		case b == '#':
			if err := c.satirYorumu(); err != nil {
				return err
			}
		case b == '-' && c.satirYorumuMu():
			if err := c.satirYorumu(); err != nil {
				return err
			}
		case b == '/' && len(c.bak(1)) == 1 && c.bak(1)[0] == '*':
			if err := c.blokYorumu(); err != nil {
				return err
			}
		case b == '*' && c.vderin > 0 && len(c.bak(1)) == 1 && c.bak(1)[0] == '/':
			c.oku()
			c.vderin--
		case b == '\'' || b == '"':
			if err := c.dizgi(b); err != nil {
				return err
			}
		case b == '`':
			if err := c.ident(); err != nil {
				return err
			}
		case b >= '0' && b <= '9':
			if err := c.sayi(b); err != nil {
				return err
			}
		case sqlSozcukBasi(b):
			if err := c.sozcuk(b); err != nil {
				return err
			}
		default:
			if err := c.sembol(b); err != nil {
				return err
			}
		}
		if c.hata != nil {
			return c.hata
		}
	}
}

func (c *sqlCoz) delimMi(b byte) bool {
	d := c.delim
	if b != d[0] {
		return false
	}
	if len(d) == 1 {
		return true
	}
	p := c.bak(len(d) - 1)
	if len(p) != len(d)-1 || string(p) != d[1:] {
		return false
	}
	c.br.Discard(len(d) - 1)
	c.pos += int64(len(d) - 1)
	return true
}

// satirYorumuMu — '-' görüldü: "-- " (ikinci '-' + boşluk/denetim/EOF) yorumdur.
func (c *sqlCoz) satirYorumuMu() bool {
	p := c.bak(2)
	if len(p) < 1 || p[0] != '-' {
		return false
	}
	return len(p) == 1 || sqlBosluk(p[1]) || p[1] == '\n' || p[1] < 0x20
}

func (c *sqlCoz) satirYorumu() error {
	var bas []byte
	for {
		b, err := c.oku()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if b == '\n' {
			c.satirBasi = true
			break
		}
		if len(bas) < 32 {
			bas = append(bas, b)
		}
	}
	if strings.Contains(string(bas), "Dump completed") {
		c.rap.Tamam = true
	}
	return nil
}

func (c *sqlCoz) blokYorumu() error {
	c.oku() // '*'
	p := c.bak(2)
	// /*! ve /*M! → yürütülen (sürümlü) yorum: içerik KOD sayılır.
	if len(p) >= 1 && (p[0] == '!' || (len(p) == 2 && p[0] == 'M' && p[1] == '!')) {
		if p[0] == 'M' {
			c.oku()
		}
		c.oku()
		for i := 0; i < 8; i++ { // sürüm rakamları
			q := c.bak(1)
			if len(q) == 0 || q[0] < '0' || q[0] > '9' {
				break
			}
			c.oku()
		}
		c.vderin++
		return nil
	}
	var onceki byte
	for {
		b, err := c.oku()
		if err == io.EOF {
			return c.ret(KuralBozuk)
		}
		if err != nil {
			return err
		}
		if b == '\n' {
			c.satirBasi = false // yorum içi satır başı komut sayılmaz
		}
		if onceki == '*' && b == '/' {
			return nil
		}
		onceki = b
	}
}

func (c *sqlCoz) dizgi(q byte) error {
	adjacent := c.sonTok == tokSozcuk && c.sonBitis == c.pos-1 && (c.sonSozcuk == "X" || c.sonSozcuk == "B")
	setMi := c.ilk == "SET"
	toplam := 0
	for {
		b, err := c.oku()
		if err == io.EOF {
			return c.ret(KuralBozuk)
		}
		if err != nil {
			return err
		}
		if b == '\\' {
			e, err := c.oku()
			if err == io.EOF {
				return c.ret(KuralBozuk)
			}
			if err != nil {
				return err
			}
			b = e
			if setMi {
				c.setDizgi = append(c.setDizgi, '\\')
			}
		} else if b == q {
			break
		}
		toplam++
		if setMi {
			if toplam > sqlSetDizgiSinir || len(c.setDizgi) > sqlSetDizgiSinir {
				return c.ret(KuralSQLKipi)
			}
			c.setDizgi = append(c.setDizgi, b)
		}
	}
	if setMi {
		u := strings.ToUpper(string(c.setDizgi))
		if adjacent || strings.Contains(u, "NO_BACKSLASH_ESCAPES") || strings.Contains(u, "ANSI") {
			return c.ret(KuralSQLKipi)
		}
	}
	c.belirtec(tokDizgi, "")
	return nil
}

func (c *sqlCoz) ident() error {
	for {
		b, err := c.oku()
		if err == io.EOF {
			return c.ret(KuralBozuk)
		}
		if err != nil {
			return err
		}
		if b == '`' {
			break
		}
	}
	c.belirtec(tokIdent, "")
	return nil
}

func (c *sqlCoz) sayi(ilk byte) error {
	hex := false
	if ilk == '0' {
		if p := c.bak(1); len(p) == 1 && (p[0] == 'x' || p[0] == 'X' || p[0] == 'b' || p[0] == 'B') {
			hex = true
		}
	}
	for {
		p := c.bak(1)
		if len(p) == 0 || !(sqlSozcukGovde(p[0]) || p[0] == '.') {
			break
		}
		c.oku()
	}
	if hex && c.ilk == "SET" {
		return c.ret(KuralSQLKipi)
	}
	c.belirtec(tokSayi, "")
	return nil
}

func (c *sqlCoz) sozcuk(ilk byte) error {
	buf := make([]byte, 0, 16)
	buf = append(buf, ilk)
	for {
		p := c.bak(1)
		if len(p) == 0 || !sqlSozcukGovde(p[0]) {
			break
		}
		c.oku()
		if len(buf) < sqlSozcukUzun {
			buf = append(buf, p[0])
		}
	}
	w := strings.ToUpper(string(buf))
	return c.sozcukIsle(w)
}

func (c *sqlCoz) sembol(b byte) error {
	if b == '(' {
		if c.ilk == "SET" && c.sonTok == tokSozcuk {
			return c.ret(KuralSQLKipi) // SET içinde işlev çağrısı
		}
		c.pencereKap = true
	}
	c.belirtec(tokSembol, "")
	return nil
}

func (c *sqlCoz) belirtec(t sqlTok, w string) {
	if c.ilk == "" && t != tokSozcuk {
		c.ilk = "?"
		c.hata = &SQLHata{Kural: KuralIzinsizIfade}
	}
	c.n++
	c.sonTok, c.sonSozcuk, c.sonBitis = t, w, c.pos
	c.satirBasi = false
}

func (c *sqlCoz) sozcukIsle(w string) error {
	satirBasindaydi := c.satirBasi
	if satirBasindaydi && sqlIstemciKomut[w] && !(w == "DELIMITER" && c.ilk == "") {
		return c.ret(KuralIstemci)
	}
	if k, kotu := sqlYasakSozcuk[w]; kotu {
		return c.ret(k)
	}
	if w == "DEFINER" {
		c.definerGor = true
	}
	if c.ilk == "" {
		if !sqlIzinli[w] {
			return c.ret(KuralIzinsizIfade)
		}
		c.ilk = w
		if w == "DELIMITER" {
			return c.delimKomutu()
		}
	} else {
		switch c.ilk {
		case "CREATE", "DROP", "ALTER":
			if !c.pencereKap && c.n < sqlSozcukPencere && sqlYonetimSozcuk[w] {
				return c.ret(KuralYonetim)
			}
		case "SET":
			switch w {
			case "GLOBAL", "PERSIST", "PERSIST_ONLY", "PASSWORD", "SELECT", "ROLE":
				return c.ret(KuralSQLKipi)
			}
		}
	}
	if c.n < len(c.kelime) {
		c.kelime[c.n] = w
	}
	c.belirtec(tokSozcuk, w)
	return nil
}

// delimKomutu — "DELIMITER x": satırın kalanı yeni ayırıcıdır (mysql istemcisi gibi).
func (c *sqlCoz) delimKomutu() error {
	var d []byte
	for {
		b, err := c.oku()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if b == '\n' {
			c.satirBasi = true
			break
		}
		if len(d) > 16 {
			return c.ret(KuralIstemci)
		}
		d = append(d, b)
	}
	s := strings.TrimSpace(string(d))
	if s == "" || strings.ContainsAny(s, "\\'\"`") {
		return c.ret(KuralIstemci)
	}
	c.delim = s
	c.sifirla()
	return nil
}

func (c *sqlCoz) sifirla() {
	c.ilk, c.n, c.pencereKap, c.definerGor = "", 0, false, false
	c.kelime = [sqlSozcukPencere]string{}
	c.setDizgi = c.setDizgi[:0]
	c.sonTok, c.sonSozcuk = tokYok, ""
}

func (c *sqlCoz) ifadeBitir() {
	if c.ilk != "" {
		c.rap.Ifade++
		if c.definerGor {
			c.rap.Definer++
		}
		if c.ilk == "CREATE" || c.ilk == "ALTER" {
			for _, w := range c.kelime {
				if w == "TABLE" {
					if c.ilk == "CREATE" {
						c.rap.Tablo++
					}
					break
				}
				if w == "PROCEDURE" || w == "FUNCTION" || w == "TRIGGER" || w == "EVENT" || w == "VIEW" {
					c.rap.Rutin++
					break
				}
			}
		}
	}
	c.sifirla()
}

func (c *sqlCoz) bitisEOF() error {
	if c.hata != nil {
		return c.hata
	}
	if c.vderin > 0 {
		return c.ret(KuralBozuk)
	}
	c.ifadeBitir() // sonlandırıcısız son ifade (istemci de çalıştırır)
	return nil
}
