// Backup off-site destinations: FTP/SFTP üzerinden uzak depolama yükleme.
// lftp tek araç olarak hem FTP hem SFTP'yi tek komutla destekler.
package backups

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"girginospanel/internal/gizli"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Destination struct {
	ID         int64  `json:"id"`
	DomainID   int64  `json:"domain_id"`
	Tip        string `json:"tip"` // "ftp" | "sftp"
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Kullanici  string `json:"kullanici"`
	Parola     string `json:"parola,omitempty"` // write-only: GET'te boş döner
	UzakDizin  string `json:"uzak_dizin"`
	Aktif      bool   `json:"aktif"`
	SonYukleme string `json:"son_yukleme,omitempty"`
	SonDurum   string `json:"son_durum,omitempty"`
	SonHata    string `json:"son_hata,omitempty"`
}

func gecerliTip(t string) bool { return t == "ftp" || t == "sftp" }

// readDestination: bir domain'in destinasyon kaydını döner (yoksa nil, nil).
func readDestination(ctx context.Context, db *sql.DB, domainID int64) (*Destination, error) {
	d := &Destination{DomainID: domainID}
	var aktif int
	var sonYuk sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT id, tip, host, port, kullanici, parola, uzak_dizin, aktif,
		        DATE_FORMAT(son_yukleme,'%Y-%m-%d %H:%i'), son_durum, son_hata
		 FROM backup_destinations WHERE domain_id=?`, domainID).
		Scan(&d.ID, &d.Tip, &d.Host, &d.Port, &d.Kullanici, &d.Parola, &d.UzakDizin,
			&aktif, &sonYuk, &d.SonDurum, &d.SonHata)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.Aktif = aktif == 1
	if sonYuk.Valid {
		d.SonYukleme = sonYuk.String
	}
	d.Parola = gizli.CozBagli(d.Parola, "yedek") // at-rest sifreli (graceful: eski duz-metin oldugu gibi doner)
	return d, nil
}

// lftpURL: tip + host + port'tan lftp URL'i kurar.
func lftpURL(d *Destination) string {
	// 🔴 Savunma derinligi (CWE-78, Corgea): Host lftp betigine TIRNAKSIZ
	// girdigi icin gecersiz bir host `open ... sftp://<host>` satirinda `;`+`!` ile
	// kabuk-kacisi (RCE) acardi. Girdi katmani (gecerliDestGirdi + genelAyarDogrula)
	// zaten allowlist'liyor; burada CAGIRANDAN BAGIMSIZ fail-closed: allowlist
	// gecmeyen host icin BOS url don — cagiranlar bos url'i guvenlik hatasi sayar.
	if !yedekHostRe.MatchString(d.Host) {
		return ""
	}
	if d.Tip == "sftp" {
		return fmt.Sprintf("sftp://%s:%d", d.Host, d.Port)
	}
	return fmt.Sprintf("ftp://%s:%d", d.Host, d.Port)
}

// uploadToRemote: lokal tar.gz'yi uzak hedefe yükler.
// lftp ile: connect → cd → put. SFTP için auto-confirm host key.
func uploadToRemote(ctx context.Context, d *Destination, localPath, dosyaAdi string) error {
	if !d.Aktif {
		return nil // disable: sessizce skip
	}
	url := lftpURL(d)
	if url == "" {
		return fmt.Errorf("güvenlik: geçersiz yedek host %q", d.Host)
	}
	// cmd:fail-exit ile herhangi bir komut başarısız olursa lftp non-zero exit eder
	script := fmt.Sprintf(
		`set cmd:fail-exit yes; `+
			`set sftp:auto-confirm yes; `+
			`set ssl:verify-certificate no; `+
			`set ftp:ssl-allow no; `+
			`set net:max-retries 1; `+
			`set net:timeout 15; `+
			`set net:reconnect-interval-base 2; `+
			`open -u "%s","%s" %s; `+
			`mkdir -p -f "%s"; `+
			`cd "%s"; `+
			`put -O . "%s"; `+
			`bye`,
		lftpEscape(d.Kullanici), lftpEscape(d.Parola), url,
		// 🔴 localPath sunucu üretimidir ama betikte çift tırnak içine gömülüyor;
		// lftpEscape olası bir tırnak/ters-bölü kaçışını (lftp betik enjeksiyonu →
		// `!komut` kabuk kaçışı) baştan kapatır — dosyadaki diğer alanlarla aynı kural.
		lftpEscape(d.UzakDizin), lftpEscape(d.UzakDizin), lftpEscape(localPath))

	cmd, temizle, err := lftpKomutu(ctx, script)
	if err != nil {
		return err
	}
	defer temizle()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("lftp: %s: %w", strings.TrimSpace(string(out)), err)
	}
	// Output'ta dahi hata izi varsa fail say (defense in depth)
	bad := []string{"Login failed", "Access failed", "Connection refused", "Permission denied",
		"Could not resolve", "Host key verification failed", "No route to host"}
	for _, p := range bad {
		if strings.Contains(string(out), p) {
			return fmt.Errorf("lftp: %s", strings.TrimSpace(string(out)))
		}
	}
	_ = dosyaAdi
	return nil
}

// deleteFromRemote: uzak hedefteki yedek dosyasini siler.
//
// 🔴 BU FONKSIYON YOKTU. Yedek silme yalnizca YEREL dosyayi ve DB kaydini
// kaldiriyordu; FTP/SFTP'ye yuklenmis kopya orada KALICI olarak duruyordu.
// Kullanici panelde "silindi" gorup uzak depoda dosyayi bulmaya devam
// ediyordu — ve depo sinirsiz buyuyordu (saklama/retention yolu da ayni
// eksigi tasiyordu).
//
// `rm -f`: dosya uzakta yoksa hata DEGILDIR. Silme, "bu dosya orada
// olmasin" demektir; zaten yoksa amac gerceklesmistir. Baglanti/kimlik
// hatasi ise GERCEK hatadir ve cagirana bildirilir.
// BETIK: `cd <TABAN dizin>` + `rm -f <TAM yol>`.
//
// 🔴 IKISI DE GEREKLI, ve nedeni olculerek ogrenildi:
//   - `rm -f` TAM YOL alir cunku hedef dosya TARIHLI alt dizinde olabilir
//     (genel hedef) ve o dizine `cd` etmek, dizin yoksa cmd:fail-exit ile
//     butun betigi dusururdu — oysa "dizin yok" = "dosya zaten orada yok".
//   - `cd <TABAN>` ise BAGLANTIYI KANITLAR. Yalniz `open` + `rm -f` yazan
//     ilk surumde erisilemeyen bir host'a silme 3 saniyede `{"ok":true}`
//     donuyordu: lftp baglanamiyor, ama `-f` hatayi yutuyor ve exit 0
//     veriyordu. Yani "uzak kopya silindi" diye rapor edilen sey hic
//     denenmemisti. Taban dizin her zaman vardir (yukleme onu `mkdir -p`
//     ile yaratiyor), bu yuzden `cd` yalnizca gercek baglanti/kimlik
//     hatalarinda patlar.
//
// ErrLftpYok — lftp ikilisi kurulu degil.
//
// 🔴 AYRI BIR HATA TIPI OLMASININ SEBEBI: lftp yoklugu bir UZAK DEPO arizasi
// degil, SUNUCU EKSIGIDIR. Ikisini ayni sepete koymak, lftp'siz eski
// kurulumlarda (paket listesine 2026-08-17'de eklendi) her silmeyi 409 ile
// bloke ederdi — duzeltmeden ONCE calisan bir islevi bozmak olurdu.
// Cagiran bunu "silmeyi engelleme, ama kullaniciya ACIKCA soyle" diye isler.
var ErrLftpYok = errors.New("lftp kurulu değil: uzak depodaki kopya silinemez")

// deleteFromRemote: uzak hedefteki yedek dosyasini siler VE silindigini
// DOGRULAR.
//
// 🔴🔴 `rm -f`'IN DONUS DEGERINE GUVENILEMEZ — olculdu:
//
//	yanlis parola   -> exit 0, cikti BOS
//	erisilemez host -> exit 0, cikti BOS
//	izin yok        -> exit 0, cikti BOS, dosya DURUYOR
//
// `-f` "hata verme" demektir ve `cmd:fail-exit` bu yuzden hic tetiklenmez;
// cikti bos oldugu icin desen taramasi da bosa calisir. Onceki surum tam da
// bu yuzden "silindi" deyip dosyayi FTP'de birakiyordu — yani duzeltilmek
// istenen hatanin kendisini uretiyordu.
//
// Bu yuzden IKI GECIS yapilir:
//  1. SILME: `cd <TABAN dizin>` (baglanti+kimlik KANITI; taban dizin
//     yukleme tarafinda `mkdir -p` ile yaratildigi icin vardir, bu yuzden
//     yalniz gercek arizada patlar) + `rm -f <TAM yol>` (dosya tarihli alt
//     dizinde olabilir; `cd` etmek dizin yoksa betigi dusururdu).
//  2. DOGRULAMA: `cls -1 <TAM yol>` — dosya HALA listeleniyorsa silme
//     BASARISIZDIR. Nobetci uretimi olcmeli, niyeti degil.
func deleteFromRemote(ctx context.Context, d *Destination, uzakDizin, dosyaAdi string) error {
	// 🔴 `Aktif` YUKLEME kapisidir, SILME kapisi DEGIL: dun aktif olan bir
	// hedefte bugun de dosyalar durur. Silerken kimlik bilgisi varsa DENERIZ.
	if d == nil || strings.TrimSpace(d.Host) == "" {
		return nil // hedef hic tanimli degil: silinecek uzak kopya da yok
	}
	if strings.TrimSpace(dosyaAdi) == "" {
		return fmt.Errorf("uzak silme: dosya adi bos")
	}
	// 🔴 YOL GECISI KORUMASI (savunma derinligi).
	//
	// `dosyaAdi` bugun yalniz sunucunun urettigi adlardan geliyor
	// (`c_<sk>-<damga>.tar.gz`) ve olculdugunde 324 kayitta tehlikeli ad
	// YOK. Ama koruma OLMADIGI icin mekanik olarak calisiyordu: bir ajan
	// `../KURBAN.tar.gz` ile hedef dizinin DISINDAKI dosyayi sildirdi.
	// Ileride bir ice-aktarma/restore yolu `backups.dosya`'ya `/` iceren
	// ad yazarsa, GENEL (admin ortak) hedefte capraz-dosya silme mumkun
	// olurdu. Ad, tek bir dosya adi olmak ZORUNDA.
	if dosyaAdi != filepath.Base(dosyaAdi) || strings.Contains(dosyaAdi, "..") {
		return fmt.Errorf("uzak silme: gecersiz dosya adi (%q)", dosyaAdi)
	}
	if _, err := exec.LookPath("lftp"); err != nil {
		return ErrLftpYok
	}

	url := lftpURL(d)
	if url == "" {
		return fmt.Errorf("güvenlik: geçersiz yedek host %q", d.Host)
	}
	tamYol := birlestirYol(uzakDizin, "") + "/" + dosyaAdi
	// Cift slash: `birlestirYol` "/" dondugunde "//dosya" olusuyordu.
	tamYol = strings.ReplaceAll(tamYol, "//", "/")
	ortak := `set sftp:auto-confirm yes; ` +
		`set ssl:verify-certificate no; ` +
		`set ftp:ssl-allow no; ` +
		`set net:max-retries 1; ` +
		`set net:timeout 15; ` +
		`set net:reconnect-interval-base 2; `

	// ── 1) SILME ────────────────────────────────────────────────────────
	silBetik := fmt.Sprintf(
		`set cmd:fail-exit yes; `+ortak+
			`open -u "%s","%s" %s; `+
			`cd "%s"; `+
			`rm -f "%s"; `+
			`bye`,
		lftpEscape(d.Kullanici), lftpEscape(d.Parola), url,
		lftpEscape(birlestirYol(d.UzakDizin, "")), lftpEscape(tamYol))
	if out, err := lftpKos(ctx, silBetik); err != nil {
		// 🔴 "DIZIN YOK" HATA DEGIL. `cd "<taban>"` dizin yoksa 550 verir ve
		// cmd:fail-exit betigi dusurur. Ama silinecek uzak kopya zaten yoksa
		// amac gerceklesmistir. Onceki hali, `uzak_dizin` yanlis/silinmis bir
		// hedefte HER dosyayi hata sayip domaini silinemez kiliyordu.
		if dizinYok(out) {
			return nil
		}
		return fmt.Errorf("lftp: %s: %w", kisaCikti(out), err)
	} else if p := lftpHataDeseni(out); p != "" {
		return fmt.Errorf("lftp: %s", p)
	}

	// ── 2) DOGRULAMA ────────────────────────────────────────────────────
	// fail-exit YOK: dizin/dosya yoksa `cls` non-zero doner ve bu BASARIDIR.
	//
	// 🔴 DOSYA YOLUNU DEGIL DIZINI LISTELE, ve TAM SATIR esle.
	// Ilk surum `cls -1 "<tam yol>"` yapip ciktida dosya adini ariyordu.
	// Ama lftp dosya YOKKEN de hata metninde YOLU yaziyor
	// ("...<yol>: No such file...") — yani basarili silmeden sonra bile
	// eslesme oluyor ve silme "basarisiz" raporlaniyordu (olculdu: dosya
	// gercekten silinmisken 409 donuyordu). Dizin listesi bu tuzagi
	// tasimaz: ya ad bir satir olarak vardir ya yoktur.
	dizin := birlestirYol(uzakDizin, "")
	dogBetik := fmt.Sprintf(
		ortak+`open -u "%s","%s" %s; `+`cls -1 "%s/"; `+`bye`,
		lftpEscape(d.Kullanici), lftpEscape(d.Parola), url, lftpEscape(dizin))
	out, _ := lftpKos(ctx, dogBetik)
	for _, satir := range strings.Split(out, "\n") {
		ad := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(satir), "/"))
		// `cls -1` yol da yazabilir; son bileseni karsilastir.
		if i := strings.LastIndexByte(ad, '/'); i >= 0 {
			ad = ad[i+1:]
		}
		if ad == dosyaAdi {
			return fmt.Errorf("uzak kopya silme komutundan SONRA hâlâ listeleniyor (%s) — hedefteki izinleri kontrol edin", tamYol)
		}
	}
	return nil
}

// lftpKos — betigi calistirir, birlesik ciktiyi doner.
func lftpKos(ctx context.Context, betik string) (string, error) {
	cmd, temizle, err := lftpKomutu(ctx, betik)
	if err != nil {
		return "", err
	}
	defer temizle()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// lftpHataDeseni — exit 0 olsa bile ciktida hata izi var mi.
// "No such file" BILEREK yok: silmede o bir basaridir.
func lftpHataDeseni(out string) string {
	bad := []string{"Login failed", "Access failed", "Connection refused", "Permission denied",
		"Could not resolve", "Host key verification failed", "No route to host",
		"Connection timed out", "cannot connect", "Name or service not known",
		"Network is unreachable", "Fatal error"}
	for _, p := range bad {
		if strings.Contains(out, p) {
			return strings.TrimSpace(out)
		}
	}
	return ""
}

// dizinYok — cikti "dizin/dosya yok" mu diyor.
func dizinYok(out string) bool {
	l := strings.ToLower(out)
	return strings.Contains(l, "no such file") || strings.Contains(l, "550")
}

func kisaCikti(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

// YedekUzaktanSil — yedegin UZAK kopyalarini siler.
//
// 🔴 IKI AYRI UZAK HEDEF VAR ve ikisi de temizlenmeli:
//  1. DOMAIN hedefi  (backup_destinations)  — duz `UzakDizin`
//  2. GENEL hedef    (backup_genel_ayar)    — Tools > Yedek Yoneticisi'nin
//     kendi FTP'si; dosyayi TARIHLI alt dizine (YYYY-AA-GG) yukluyor.
//
// Yalniz birincisini silmek, panelde "Yedek Yoneticisi"nden yonetilen
// kurulumlarda hicbir sey temizlemezdi — kullanicinin bildirdigi durum
// tam olarak buydu.
//
// Genel hedefte hem tarihli alt dizin hem taban dizin denenir (tarih dizini
// gelmeden once yuklenmis yedekler tabandadir; indirme yolu da ayni iki
// adayi deniyor).
func YedekUzaktanSil(ctx context.Context, db *sql.DB, domainID int64, dosyaAdi string) error {
	var hatalar []string
	lftpEksik := false

	if d, err := readDestination(ctx, db, domainID); err == nil && d != nil {
		if e := deleteFromRemote(ctx, d, d.UzakDizin, dosyaAdi); e != nil {
			if errors.Is(e, ErrLftpYok) {
				lftpEksik = true
			} else {
				hatalar = append(hatalar, "domain hedefi: "+e.Error())
			}
		}
	}

	// `UzakAktif` de burada aranmaz (bkz. deleteFromRemote): kapali bir
	// hedefte dosyalar durmaya devam eder.
	if g := genelAyarOku(ctx, db); g != nil && strings.TrimSpace(g.UzakHost) != "" {
		h := g.hedef()
		adaylar := []string{birlestirYol(g.UzakDizin, uzakTarihDizini(dosyaAdi))}
		if taban := birlestirYol(g.UzakDizin, ""); taban != adaylar[0] {
			adaylar = append(adaylar, taban)
		}
		for _, dz := range adaylar {
			if e := deleteFromRemote(ctx, h, dz, dosyaAdi); e != nil {
				if errors.Is(e, ErrLftpYok) {
					lftpEksik = true
					break
				}
				hatalar = append(hatalar, "genel hedef ("+dz+"): "+e.Error())
			}
		}
	}

	if lftpEksik {
		// Tek sebep lftp yoklugu ise cagiran bunu "engelleme, uyar" diye
		// isler; gercek depo arizalariyla karistirilmaz.
		if len(hatalar) == 0 {
			return ErrLftpYok
		}
	}
	if len(hatalar) > 0 {
		return fmt.Errorf("%s", strings.Join(hatalar, " | "))
	}
	return nil
}

// lftpKomutu: lftp betigini 0600 GECICI DOSYAYA yazip `lftp -f <dosya>` ile calistirir.
//
// 🔴 Neden: `lftp -c "<betik>"` betigi ARGV'ye koyar; betikte `open -u kullanici,PAROLA`
// oldugu icin parola `ps aux` ile herkese gorunur. Sunucuda /proc hidepid'siz bagli
// oldugundan bunu HERHANGI bir kiraci okuyabiliyordu. Dosya root'a ait 0600 olunca
// icerik kiracilara kapali; argv'de yalniz dosya yolu kalir.
func lftpKomutu(ctx context.Context, betik string) (*exec.Cmd, func(), error) {
	f, err := os.CreateTemp("", "gosp-lftp-*.cmd")
	if err != nil {
		return nil, func() {}, fmt.Errorf("lftp betigi: %w", err)
	}
	yol := f.Name()
	temizle := func() { _ = os.Remove(yol) }
	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		temizle()
		return nil, func() {}, fmt.Errorf("lftp betigi izin: %w", err)
	}
	// lftp -f dosyayi SATIR SATIR isler; tek satirlik ";" ayrilmis betik de gecerlidir.
	if _, err := f.WriteString(betik + "\n"); err != nil {
		_ = f.Close()
		temizle()
		return nil, func() {}, fmt.Errorf("lftp betigi yazma: %w", err)
	}
	_ = f.Close()
	return exec.CommandContext(ctx, "lftp", "-f", yol), temizle, nil
}

// lftpEscape: lftp komut satırı içinde çift tırnak içine konacak değerleri escape eder.
func lftpEscape(s string) string {
	// Kontrol karakterleri (satir enjeksiyonu) — girdi katmani da reddeder, burada da temizle.
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// testConnection: kimlik bilgilerini test eder.
// SFTP için sshpass+ssh, FTP için curl — her ikisi de auth-specific exit kodu döner.
func testConnection(ctx context.Context, d *Destination) error {
	if d.Tip == "sftp" {
		// 🔴 SFTP ALTSISTEMI ile test et, kabuk komutu ile DEGIL. Eskiden
		// `ssh ... true` calistiriliyordu; Hetzner Storage Box gibi SFTP-ONLY
		// hedefler kabuk exec'ine izin VERMEZ → kimlik dogrulama BASARILI olsa
		// bile "exec request failed on channel 0" hatasi donuyor ve kullanici
		// dogru parolayi girdigi halde "baglanti kurulamadi" goruyordu.
		// `sftp -b` toplu-mod: pwd + quit → yalniz altsistem gerekir.
		// sshpass parola passwd, PreferredAuthentications=password + publickey
		// kapali → parolanin gercekten gecerli oldugu garanti edilir.
		// KRITIK: kullaniciyi `-o User=` ile ver, host `--`'den SONRA gelsin →
		// ikisi de ssh opsiyonu olarak yorumlanamaz (ProxyCommand arg-injection kapali).
		args := []string{
			"-e", // parola SSHPASS ortam degiskeninden okunur; argv'de GORUNMEZ
			"sftp",
			"-P", fmt.Sprintf("%d", d.Port),
			"-o", "User=" + d.Kullanici,
			"-o", "ConnectTimeout=10",
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
			"-o", "PreferredAuthentications=password",
			"-o", "PubkeyAuthentication=no",
			"-o", "BatchMode=no",
			"-b", "-",
			"--", d.Host,
		}
		cmd := exec.CommandContext(ctx, "sshpass", args...)
		// Ortam degiskeni: /proc/<pid>/environ yalniz sahibine (root) okunur,
		// kiracilar goremez — argv ise herkese aciktir.
		cmd.Env = append(os.Environ(), "SSHPASS="+d.Parola)
		// 🔴 Test, YUKLEYICININ yaptigi isin aynisini yapmali: yukleyici
		// (uploadToRemote) `mkdir -p` ile dizini kendisi yaratir. Test yalnizca
		// `cd` deneseydi, HENUZ olusturulmamis hedef dizinde "No such file"
		// dondurup calisan bir yapilandirmayi bozukmus gibi gosterirdi.
		// Bu yuzden: dizin agacini olustur (varsa hatayi yut), ic, gercek bir
		// dosya YAZ ve sil → yazma yetkisi de kanitlanir.
		gecici, err := os.CreateTemp("", "gosp-erisim-*.tmp")
		if err != nil {
			return fmt.Errorf("gecici dosya: %w", err)
		}
		defer os.Remove(gecici.Name())
		_, _ = gecici.WriteString("girginospanel erisim testi\n")
		_ = gecici.Close()

		var b strings.Builder
		hedef := strings.TrimSpace(d.UzakDizin)
		// "-" oneki: sftp toplu-modda o satirin hatasini yut (dizin zaten varsa).
		for _, seviye := range dizinSeviyeleri(hedef) {
			b.WriteString("-mkdir " + seviye + "\n")
		}
		if hedef != "" && hedef != "/" {
			b.WriteString("cd " + hedef + "\n")
		}
		b.WriteString("put " + gecici.Name() + " .girginospanel-erisim-testi\n")
		b.WriteString("-rm .girginospanel-erisim-testi\n")
		b.WriteString("pwd\n")
		cmd.Stdin = strings.NewReader(b.String())
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s", sshGurultuTemizle(string(out), err))
		}
		return nil
	}
	// FTP — curl --user u:p ftp://host:port/  (NLST root)
	url := fmt.Sprintf("ftp://%s:%d/", d.Host, d.Port)
	args := []string{
		"-sS",
		"--connect-timeout", "10",
		"--max-time", "15",
		"-K", "-", // kimlik STDIN config'ten; argv'de GORUNMEZ
		"--ftp-skip-pasv-ip",
		url,
	}
	cmd := exec.CommandContext(ctx, "curl", args...)
	cmd.Stdin = strings.NewReader("user = \"" + curlConfEscape(d.Kullanici+":"+d.Parola) + "\"\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", sshGurultuTemizle(string(out), err))
	}
	return nil
}

// dizinSeviyeleri: "/a/b/c" -> ["/a", "/a/b", "/a/b/c"]. sftp toplu-modunda
// `mkdir -p` yoktur; her seviye tek tek yaratilmalidir.
func dizinSeviyeleri(yol string) []string {
	yol = strings.TrimSpace(yol)
	if yol == "" || yol == "/" {
		return nil
	}
	mutlak := strings.HasPrefix(yol, "/")
	var birikim string
	var res []string
	for _, parca := range strings.Split(strings.Trim(yol, "/"), "/") {
		if parca == "" || parca == "." || parca == ".." {
			continue
		}
		if birikim == "" {
			if mutlak {
				birikim = "/" + parca
			} else {
				birikim = parca
			}
		} else {
			birikim += "/" + parca
		}
		res = append(res, birikim)
	}
	return res
}

// curlConfEscape: curl -K config dosyasinda cift tirnak icine konacak degeri kacisla.
func curlConfEscape(v string) string {
	v = strings.ReplaceAll(v, "\\", "\\\\")
	v = strings.ReplaceAll(v, "\"", "\\\"")
	v = strings.ReplaceAll(v, "\r", "")
	v = strings.ReplaceAll(v, "\n", "")
	return v
}

// sshGurultuTemizle: ssh/sftp/curl ciktisindan ZARARSIZ uyarilari atar ve geriye
// gercek hata satirini birakir. Ozellikle "Warning: Permanently added '<host>'
// (RSA) to the list of known hosts." satiri her ILK baglantida cikar; kullaniciya
// hata gibi gosterilmesi yanlis alarm uretiyordu.
func sshGurultuTemizle(cikti string, err error) string {
	var kalan []string
	for _, satir := range strings.Split(cikti, "\n") {
		t := strings.TrimSpace(satir)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "Warning: Permanently added") ||
			strings.HasPrefix(t, "Uploading ") ||
			strings.HasPrefix(t, "Removing ") ||
			strings.HasPrefix(t, "Changing to:") ||
			strings.Contains(t, "to the list of known hosts") ||
			strings.HasPrefix(t, "Connected to ") ||
			strings.HasPrefix(t, "sftp> ") ||
			t == "Remote working directory: /" {
			continue
		}
		kalan = append(kalan, t)
	}
	if len(kalan) == 0 {
		if err != nil {
			return err.Error()
		}
		return "bilinmeyen hata"
	}
	return strings.Join(kalan, "; ")
}

// pushToDestinationAsync: yedek başarıyla oluştuktan sonra arkaplanda upload tetikler.
// Hata olsa bile API cevabını bloke etmez; son_durum/son_hata DB'ye yazılır.
func pushToDestinationAsync(db *sql.DB, domainID int64, localPath, dosyaAdi string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		d, err := readDestination(ctx, db, domainID)
		if err != nil || d == nil || !d.Aktif {
			return
		}
		if err := uploadToRemote(ctx, d, localPath, dosyaAdi); err != nil {
			short := err.Error()
			if len(short) > 500 {
				short = short[:500]
			}
			_, _ = db.Exec(`UPDATE backup_destinations
				SET son_durum='hata', son_hata=?, son_yukleme=NOW() WHERE domain_id=?`,
				short, domainID)
			log.Printf("backup destination upload domain=%d: %v", domainID, err)
			return
		}
		_, _ = db.Exec(`UPDATE backup_destinations
			SET son_durum='basarili', son_hata='', son_yukleme=NOW() WHERE domain_id=?`,
			domainID)
		log.Printf("backup destination upload domain=%d başarılı: %s", domainID, dosyaAdi)
	}()
}
