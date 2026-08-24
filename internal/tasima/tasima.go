// Package tasima — cPanel / Plesk / DirectAdmin kurulumlarindan GirginOSPanel'e
// uctan uca site tasima (migration).
//
// GUVENLIK NOTU (bu dosya saldiri yuzeyinin merkezi):
// Bu paket operatorun verdigi kimlik bilgileriyle UZAK bir sunucuya baglanir ve
// oradan veri ceker. Uzak taraftan donen HER SEY (hesap adi, domain, DB adi,
// zone icerigi) DUSMANDIR — dogrudan komuta gomulmez.
//
//  1. Kabuk YOK: her yerel calistirma exec.CommandContext(argv...) — "sh -c" asla.
//  2. Uzak komutlar SABIT sablon; degisken deger girerse shQuote() ile tek-tirnak
//     icine alinir (uzak tarafta kabuk kacinilmaz oldugu icin).
//  3. Host/kullanici allowlist regex'ten gecer; bastaki '-' REDDEDILIR (aksi halde
//     ssh/rsync bunu BAYRAK sanar → arg enjeksiyonu).
//  4. ssh daima "-l <kullanici> -- <host>" ile cagrilir (user@host ayristirma yok).
//  5. Parola argv'ye YAZILMAZ (ps ile gorunur) — sshpass -e + ortam degiskeni.
//  6. Kimlik bilgileri at-rest AES-256-GCM (internal/gizli) ile saklanir, is
//     bitince silinir.
package tasima

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Handlers — HTTP katmani icin DB tasiyici.
type Handlers struct{ DB *sql.DB }

// Kaynak — uzak (tasinacak) sunucunun baglanti bilgileri.
type Kaynak struct {
	Tip       string // cpanel | plesk | directadmin
	Host      string
	Port      int
	Kullanici string
	Parola    string // duz metin YALNIZ bellekte; DB'ye sifreli yazilir
	Anahtar   string // SSH ozel anahtari (opsiyonel, paroleye alternatif)
}

// Ayarlar — tasima davranisi (JSON olarak tasima_isleri.ayarlar icinde).
type Ayarlar struct {
	Dosyalar   bool   `json:"dosyalar"`
	Veritabani bool   `json:"veritabani"`
	DNS        bool   `json:"dns"`
	SSL        bool   `json:"ssl"`
	Posta      bool   `json:"posta"` // kutular + mail verisi (Plesk maildir)
	Ustune     bool   `json:"ustune"` // hedefte domain varsa uzerine yaz
	HedefPHP   string `json:"hedef_php"`
	PlanID     int64  `json:"plan_id"`
	// Sahiplik: taşınan site ana hesap yerine bir bayiye/müşteriye atanabilir.
	// reseller_id=0 → ana admin · customer_id=0 → müşteri yok.
	ResellerID int64    `json:"reseller_id"`
	CustomerID int64    `json:"customer_id"`
	Hesaplar   []string `json:"hesaplar"` // toplu modda secilen hesaplar (bos = hepsi)
	// SahipleriTasi: kaynaktaki reseller/müşteri hesapları GVM'de oluşturulup her
	// domain KENDİ sahibine atanır. Bu durumda ResellerID/CustomerID yalnız
	// admin-sahipli (sahibi keşfedilemeyen) domainler için varsayılan olur.
	SahipleriTasi bool                  `json:"sahipleri_tasi"`
	SahipEsle     map[string]sahipHedef `json:"-"` // runtime: kaynak login → GVM hedef (Baslat doldurur)
}

const (
	sshBaglantiSn = 15
	kesifTimeout  = 90 * time.Second
	// Tek hesabin tasinmasi icin ust sinir — takilan rsync/mysqldump isi
	// sonsuza kadar tutmasin.
	hesapTimeout = 3 * time.Hour
)

// ---------------------------------------------------------------------------
// Dogrulama
// ---------------------------------------------------------------------------

var (
	// RFC1123 host adi; bastaki/sondaki tire yok, '-' ile BASLAYAMAZ.
	reHost = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]{0,251}[A-Za-z0-9])?$`)
	// Unix kullanici adi; '-' ile baslayamaz.
	reKullanici = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,63}$`)
	// Uzaktan donen hesap adlari (cpanel/plesk/DA kullanicilari).
	reHesap = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,63}$`)
	// Alan adi.
	reAlanAdi = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
	// MySQL tanimlayici.
	reDBAd = regexp.MustCompile(`^[A-Za-z0-9_$-]{1,64}$`)
)

var gecerliTipler = map[string]bool{"cpanel": true, "plesk": true, "directadmin": true}

// Dogrula — Kaynak girdisini kabul etmeden once tam dogrular.
// Bastaki '-' kontrolu ozellikle onemli: "-oProxyCommand=..." bir ssh BAYRAGIDIR.
// kendineMiCozuluyor — host bu sunucunun kendisine (loopback: 127.0.0.0/8, ::1) mi
// çözülüyor? /etc/hosts'ta "127.0.0.1 <hostname>" olduğunda ve kaynak adı bu sunucunun
// adıyla aynıysa true döner. IP girildiyse LookupHost onu aynen döndürür → gerçek uzak
// IP loopback değildir, geçer. Kısa timeout: istek yolunda takılmasın.
func kendineMiCozuluyor(host string) bool {
	if p := net.ParseIP(host); p != nil {
		return p.IsLoopback()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return false // çözülemiyorsa burada karar verme; bağlantı denemesi hata verir
	}
	for _, ip := range ips {
		if p := net.ParseIP(ip); p != nil && p.IsLoopback() {
			return true
		}
	}
	return false
}

func (k *Kaynak) Dogrula() error {
	if !gecerliTipler[k.Tip] {
		return fmt.Errorf("gecersiz kaynak panel tipi")
	}
	h := strings.TrimSpace(k.Host)
	if h == "" || len(h) > 253 {
		return fmt.Errorf("kaynak sunucu adresi gecersiz")
	}
	if strings.HasPrefix(h, "-") {
		return fmt.Errorf("kaynak sunucu adresi gecersiz")
	}
	// IP ya da host adi olmali.
	if net.ParseIP(h) == nil && !reHost.MatchString(h) {
		return fmt.Errorf("kaynak sunucu adresi gecersiz")
	}
	k.Host = h

	// 🔴 LOOPBACK GUARD: kaynak host bu sunucunun KENDISINE (localhost) çözülüyorsa
	// reddet. SIK TUZAK: panel sunucusunun hostname'i kaynağınkiyle AYNI ise
	// (/etc/hosts'ta "127.0.0.1 <hostname>") host adı localhost'a döner → taşıma
	// GERÇEK uzak sunucu yerine KENDİNE bağlanır ve kaynağın parolası "yanlış"
	// görünür. Çözüm net: gerçek uzak sunucunun IP adresini girmek. (Canlı olay
	// 2026-08-22: plesk.lto.com.tr hem kaynak hem panel adıydı → ::1'e çözülüyordu.)
	if kendineMiCozuluyor(h) {
		return fmt.Errorf("kaynak adresi '%s' bu sunucunun KENDİSİNE (localhost) çözülüyor — taşıma gerçek uzak sunucuya ulaşamaz. Bu genellikle bu sunucunun adı kaynakla AYNI olduğunda olur (/etc/hosts). ÇÖZÜM: kaynak alanına hostname yerine uzak sunucunun IP ADRESİNİ girin", h)
	}

	if k.Port <= 0 || k.Port > 65535 {
		return fmt.Errorf("SSH portu gecersiz")
	}
	u := strings.TrimSpace(k.Kullanici)
	if u == "" {
		u = "root"
	}
	if strings.HasPrefix(u, "-") || !reKullanici.MatchString(u) {
		return fmt.Errorf("SSH kullanici adi gecersiz")
	}
	k.Kullanici = u

	if k.Parola == "" && strings.TrimSpace(k.Anahtar) == "" {
		return fmt.Errorf("parola veya SSH anahtari gerekli")
	}
	// Parolada satir sonu/NUL olmasin (sshpass ortam degiskenini bozar).
	if strings.ContainsAny(k.Parola, "\x00\r\n") {
		return fmt.Errorf("parola gecersiz karakter iceriyor")
	}
	return nil
}

// shQuote — uzak kabukta kullanilacak degeri tek tirnak icine alir.
// Tek tirnak icinde HICBIR sey yorumlanmaz; tek tirnagin kendisi kapatilip
// kacirilir. Uzaktan donen (dusman) degerler komuta boyle gomulur.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ---------------------------------------------------------------------------
// Uzak calistirma
// ---------------------------------------------------------------------------

// sshOrtakArgs — her ssh/rsync cagrisinda kullanilan sertlestirilmis secenekler.
func (k *Kaynak) sshOrtakArgs(anahtarDosya string) []string {
	a := []string{
		"-p", fmt.Sprintf("%d", k.Port),
		"-o", fmt.Sprintf("ConnectTimeout=%d", sshBaglantiSn),
		// Ilk baglantida host anahtarini kabul et ama SONRADAN degisirse reddet
		// (MITM koruma). Tasima tek seferlik oldugu icin dogru denge.
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=/root/.ssh/known_hosts_tasima",
		// Kaynak sunucunun bize komut/agent sizdirmasini engelle.
		"-o", "ForwardAgent=no",
		"-o", "ForwardX11=no",
		"-o", "ClearAllForwardings=yes",
		"-o", "PermitLocalCommand=no",
	}
	if anahtarDosya != "" {
		// Anahtar yolu: interaktif prompt olmasin (hang engelle).
		a = append(a, "-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes", "-i", anahtarDosya)
	} else {
		// 🔴 Parola yolu: BatchMode=yes KULLANMA — parola prompt'unu kapatir ve
		// sshpass parolayi enjekte EDEMEZ (butun parola-tabanli tasima kirilir).
		// Yerine parola auth'a zorla + tek prompt (yanlis parolada retry/hang yok).
		a = append(a, "-o", "PubkeyAuthentication=no",
			"-o", "PreferredAuthentications=password,keyboard-interactive",
			"-o", "NumberOfPasswordPrompts=1")
	}
	return a
}

// anahtarYaz — SSH ozel anahtarini 0600 gecici dosyaya yazar. temizle() cagrilmali.
func (k *Kaynak) anahtarYaz() (yol string, temizle func(), err error) {
	if strings.TrimSpace(k.Anahtar) == "" {
		return "", func() {}, nil
	}
	f, err := os.CreateTemp("/root", ".tasima_key_*")
	if err != nil {
		return "", func() {}, err
	}
	ad := f.Name()
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		os.Remove(ad)
		return "", func() {}, err
	}
	icerik := k.Anahtar
	if !strings.HasSuffix(icerik, "\n") {
		icerik += "\n"
	}
	if _, err := f.WriteString(icerik); err != nil {
		f.Close()
		os.Remove(ad)
		return "", func() {}, err
	}
	f.Close()
	return ad, func() { os.Remove(ad) }, nil
}

// Calistir — uzak sunucuda komut calistirir, stdout dondurur.
// uzakKomut bu paket tarafindan uretilen SABIT sablondur; degisken deger
// iceriyorsa shQuote()'tan gecmis olmalidir.
func (k *Kaynak) Calistir(ctx context.Context, uzakKomut string) (string, error) {
	anahtar, temizle, err := k.anahtarYaz()
	if err != nil {
		return "", fmt.Errorf("anahtar yazilamadi: %w", err)
	}
	defer temizle()

	args := k.sshOrtakArgs(anahtar)
	// -l <kullanici> -- <host>: user@host ayristirmasi yok, host bayrak sanilamaz.
	args = append(args, "-l", k.Kullanici, "--", k.Host, uzakKomut)

	var cmd *exec.Cmd
	if anahtar == "" {
		// Parola argv'de GORUNMEZ: sshpass -e ortam degiskeninden okur.
		cmd = exec.CommandContext(ctx, "sshpass", append([]string{"-e", "ssh"}, args...)...)
		cmd.Env = append(os.Environ(), "SSHPASS="+k.Parola)
	} else {
		cmd = exec.CommandContext(ctx, "ssh", args...)
		cmd.Env = os.Environ()
	}
	var sb, eb strings.Builder
	cmd.Stdout = &sb
	cmd.Stderr = &eb
	if err := cmd.Run(); err != nil {
		return sb.String(), fmt.Errorf("%s", k.sshHataYorumla(eb.String()))
	}
	return sb.String(), nil
}

// RsyncCek — uzak dizini yerele kopyalar. uzakYol/yerelYol cagiran tarafindan
// dogrulanmis olmalidir.
func (k *Kaynak) RsyncCek(ctx context.Context, uzakYol, yerelYol string, ekstra ...string) (string, error) {
	anahtar, temizle, err := k.anahtarYaz()
	if err != nil {
		return "", err
	}
	defer temizle()

	// rsync -e icin ssh komut dizesi. Degerler dogrulanmis (allowlist) oldugu
	// icin burada bosluk/tirnak riski yok; yine de sabit sablon kullaniyoruz.
	sshArgs := append([]string{"ssh"}, k.sshOrtakArgs(anahtar)...)
	rshDize := strings.Join(sshArgs, " ")

	args := []string{
		// 🔴 SILME YOK. --delete-excluded, --delete'i IMA EDER: hedefte olup
		// kaynakta olmayan her sey silinir. 'ustune' modunda hedef MUSTERININ
		// CANLI dizinidir — uploads/, public/ gibi klasorler geri donusu
		// olmadan yok oluyordu (kanary testiyle dogrulandi).
		"-a", "--numeric-ids",
		"--timeout=120", "--partial",
		// Tenant izolasyonu: uzaktaki setuid/setgid bitleri TASINMAZ.
		"--no-perms", "--chmod=Du=rwx,Dgo=rx,Fu=rw,Fgo=r",
		// Sembolik baglar hedef disina cikamasin.
		"--safe-links",
		"-e", rshDize,
	}
	args = append(args, ekstra...)
	// Kaynak: kullanici@host:yol — kullanici/host allowlist'ten gecti.
	args = append(args, k.Kullanici+"@"+k.Host+":"+uzakYol, yerelYol)

	var cmd *exec.Cmd
	if anahtar == "" {
		cmd = exec.CommandContext(ctx, "sshpass", append([]string{"-e", "rsync"}, args...)...)
		cmd.Env = append(os.Environ(), "SSHPASS="+k.Parola)
	} else {
		cmd = exec.CommandContext(ctx, "rsync", args...)
		cmd.Env = os.Environ()
	}
	var sb, eb strings.Builder
	cmd.Stdout = &sb
	cmd.Stderr = &eb
	if err := cmd.Run(); err != nil {
		return sb.String(), fmt.Errorf("dosya kopyalama (rsync) hatasi: %s", k.sshHataYorumla(eb.String()))
	}
	return sb.String(), nil
}

// BaglantiTesti — kimlik dogru mu, uzak panel tipi beklenen mi?
func (k *Kaynak) BaglantiTesti(ctx context.Context) (string, error) {
	ctx, iptal := context.WithTimeout(ctx, kesifTimeout)
	defer iptal()
	cikti, err := k.Calistir(ctx, "echo GOSP_OK; uname -n")
	if err != nil {
		return "", err
	}
	if !strings.Contains(cikti, "GOSP_OK") {
		return "", fmt.Errorf("uzak sunucudan beklenen yanit alinamadi")
	}
	satirlar := strings.Fields(cikti)
	ad := ""
	if len(satirlar) > 1 {
		ad = satirlar[len(satirlar)-1]
	}
	return ad, nil
}

// ---------------------------------------------------------------------------
// Yardimcilar
// ---------------------------------------------------------------------------

// sshHataYorumla — ham ssh/rsync stderr'ini KULLANICININ ANLAYACAGI Turkce
// yonlendirmeye cevirir. Amac: "Permission denied (publickey,...,password)" gibi
// yalniz sistem yoneticilerinin cozdugu hatalar yerine, ne yapilacagini soylemek.
// (Kullanici SSH detayi bilmek zorunda kalmasin — kaynak-baglanti sorunlari
// tasimanin en sik takilma noktasi.) Sir icermez (temizHata ile maskelenir).
func (k *Kaynak) sshHataYorumla(stderr string) string {
	low := strings.ToLower(stderr)
	switch {
	case strings.Contains(low, "permission denied") || strings.Contains(low, "authentication failed"):
		if strings.TrimSpace(k.Anahtar) != "" {
			return "SSH anahtari kabul edilmedi. Girdiginiz ozel anahtarin karsiligi (public key), kaynak sunucuda '" +
				k.Kullanici + "' kullanicisinin ~/.ssh/authorized_keys dosyasinda ekli olmali. Alternatif olarak parola ile baglanabilirsiniz."
		}
		return "Kaynak sunucu SSH parolasini reddetti. En sik iki neden:\n" +
			"1) Yanlis parola — buraya kaynak sunucunun LINUX '" + k.Kullanici +
			"' (SSH) parolasi girilmeli; Plesk/cPanel PANEL parolasi DEGIL (ikisi farklidir).\n" +
			"2) Parola dogru ama kaynak sunucu root'un parola ile girisini kapatmis olabilir. Bu durumda ya SSH ANAHTARI kullanin (asagidaki 'SSH anahtari' alanina yetkili bir ozel anahtar girin), ya da kaynakta gecici olarak acin:\n" +
			"   /etc/ssh/sshd_config -> 'PermitRootLogin yes' + 'PasswordAuthentication yes', sonra 'systemctl restart sshd' (tasima bitince eski haline getirin)."
	case strings.Contains(low, "connection refused"):
		return fmt.Sprintf("Kaynak sunucuya SSH baglantisi reddedildi (port %d). SSH portu dogru mu? Kaynagin guvenlik duvari bu sunucunun IP'sine izin veriyor mu?", k.Port)
	case strings.Contains(low, "timed out") || strings.Contains(low, "no route to host") || strings.Contains(low, "network is unreachable"):
		return "Kaynak sunucuya ulasilamadi (baglanti zaman asimi). Host adresi ve SSH portu dogru mu? Kaynagin guvenlik duvari bu sunucuyu engelliyor olabilir."
	case strings.Contains(low, "could not resolve") || strings.Contains(low, "name or service not known") || strings.Contains(low, "nodename nor servname"):
		return fmt.Sprintf("Kaynak host adi cozulemedi ('%s'). Yazimi kontrol edin ya da dogrudan IP adresi girin.", k.Host)
	case strings.Contains(low, "host key") && (strings.Contains(low, "changed") || strings.Contains(low, "verification failed")):
		return "Kaynagin SSH host anahtari degismis gorunuyor (yeniden kurulum ya da guvenlik uyarisi). Kaynaga guveniyorsaniz /root/.ssh/known_hosts_tasima icindeki ilgili satiri silip tekrar deneyin."
	}
	// Taninmayan hata: ham (ama sir-maskeli + kisa).
	return kisalt(temizHata(stderr, k.Parola), 300)
}

// temizHata — uzak stderr'i tek satira indirger VE bilinen sirlari maskeler.
// Bu metin DB'ye (tasima_kalemleri.hata) ve API yanitina gidiyor; ssh/sshpass/
// rsync stderr'ine parola yansirsa disari sizmamali.
func temizHata(s string, sirlar ...string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	for _, sir := range sirlar {
		if len(sir) >= 4 {
			s = strings.ReplaceAll(s, sir, "••••••")
		}
	}
	if v := os.Getenv("SSHPASS"); len(v) >= 4 {
		s = strings.ReplaceAll(s, v, "••••••")
	}
	return strings.TrimSpace(s)
}

func kisalt(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Maskele — kimlik bilgisini API yanitinda gostermek icin.
func Maskele(s string) string {
	if s == "" {
		return ""
	}
	return "••••••"
}
