package provisioner

// posta_aski.go — HESAP ASKISININ POSTA ALTYAPISI (mevcut sunucular için uzlaştırma).
//
// Kullanıcı kararı (2026-10-03): hosting hesabı askıdayken posta GİRİŞİ kapanır
// (IMAP/POP3, SMTP AUTH gönderim, webmail tek tık), GELEN POSTA AÇIK KALIR.
// Askı alan adı düzeyinde mail eklentisinin veritabanında tutulur
// (mailserver.virtual_domains.askida; panel hesapaski.PostaAski ile yazar) ve
// dovecot'ta uygulanır:
//   - passdb password_query: virtual_domains JOIN ile `d.askida=0` → IMAP/POP3
//     girişi ve Dovecot SASL üzerinden SMTP AUTH (postfix smtpd_sasl_type=dovecot)
//     reddedilir.
//   - webmail master passdb'si (99-gosp-webmail.conf) `pass = yes` → tek tık
//     girişinde hedef kutu password_query'den yine aranır; yoksa master girişi
//     askıyı atlardı. (Roundcube SMTP'yi localhost:25'ten kimliksiz gönderir;
//     IMAP oturumu açılamayınca gönderemez.)
//   - userdb user_query'ye ve postfix virtual_mailbox_maps'e DOKUNULMAZ: LMTP
//     teslimi userdb'ye bakar; koşul oraya girerse gelen posta kırılır.
//
// Taze kurulumda mail eklentisi kurucusu (internal/lisans/kurulum_mail*) bunları
// zaten böyle yazar. Bu dosya, kurucu güncellenmeden önce kurulmuş sunucuları
// panel AÇILIŞINDA uzlaştırır (PostaAskiAltyapisi; idempotent): kolon eklenir
// (mailadmin'in ALTER yetkisi yok → root unix soketiyle mysql), sorgu ve master
// passdb düzeltilir, `doveconf -n` doğrular (düşerse dosyalar geri yazılır),
// dovecot reload edilir (açık oturumlar düşmez).
//
// 🔴 Sıra: kolon ÖNCE. password_query var olmayan kolona bakarsa sorgu hata
// verir ve HERKESİN girişi düşer.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PostaPasswordQuery — askıyı uygulayan passdb sorgusu (kurucudaki
// dovecotPasswordQuery ile AYNI metin).
const PostaPasswordQuery = "SELECT u.email AS user, u.password FROM virtual_users u " +
	"JOIN virtual_domains d ON d.id=u.domain_id " +
	"WHERE u.email='%u' AND u.aktif=1 AND d.askida=0"

// postaEskiPasswordQuery — kurucunun önceki sürümlerinin yazdığı sorgu. Yalnız
// bu (ya da yeni) sorgu tanınır; elle değiştirilmiş bir sorguya DOKUNULMAZ.
var postaEskiPasswordQuery = []string{
	"SELECT email as user, password FROM virtual_users WHERE email='%u' AND aktif=1",
}

var (
	postaDovecotSQLYol = "/etc/dovecot/dovecot-sql.conf.ext"
	postaWebmailYol    = "/etc/dovecot/conf.d/99-gosp-webmail.conf"

	// postaKomut — dış komut (mysql, doveconf, systemctl). Testler sahteyle
	// değiştirir: ana makinenin MariaDB'sine/dovecot'una dokunulmaz.
	postaKomut = func(ctx context.Context, stdin, ad string, args ...string) (string, error) {
		c, iptal := context.WithTimeout(ctx, 2*time.Minute)
		defer iptal()
		cmd := exec.CommandContext(c, ad, args...)
		if stdin != "" {
			cmd.Stdin = strings.NewReader(stdin)
		}
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}

	postaAltyapiMu sync.Mutex
)

// ErrPostaTaninmayan — dovecot yapılandırması kurucunun yazdığı biçimde değil
// (elle kurulmuş / değiştirilmiş): otomatik düzeltilmez.
var ErrPostaTaninmayan = errors.New("dovecot posta yapılandırması tanınmıyor — elle düzenlenmiş olabilir, dokunulmadı")

var (
	rePasswordQuery = regexp.MustCompile(`^\s*password_query\s*=\s*(.*?)\s*$`)
	reUserQuery     = regexp.MustCompile(`^\s*user_query\s*=`)
	reMasterYes     = regexp.MustCompile(`^(\s*)master\s*=\s*yes\s*$`)
	rePassAyar      = regexp.MustCompile(`^(\s*)pass\s*=\s*(\S+)\s*$`)
)

// postaSQLConfDonustur — dovecot-sql.conf.ext'te YALNIZ password_query satırını
// askı koşullu sorguya çevirir; user_query ve öteki satırlar (parola dahil)
// aynen kalır.
func postaSQLConfDonustur(icerik string) (string, bool, error) {
	satirlar := strings.Split(icerik, "\n")
	idx := -1
	for i, s := range satirlar {
		if rePasswordQuery.MatchString(s) {
			if idx >= 0 {
				return "", false, fmt.Errorf("%w: birden çok password_query", ErrPostaTaninmayan)
			}
			idx = i
		}
	}
	if idx < 0 {
		return "", false, fmt.Errorf("%w: password_query yok", ErrPostaTaninmayan)
	}
	deger := rePasswordQuery.FindStringSubmatch(satirlar[idx])[1]
	if deger == PostaPasswordQuery {
		return icerik, false, nil
	}
	tanindi := false
	for _, e := range postaEskiPasswordQuery {
		if deger == e {
			tanindi = true
		}
	}
	if !tanindi {
		return "", false, fmt.Errorf("%w: password_query beklenmeyen biçimde", ErrPostaTaninmayan)
	}
	satirlar[idx] = "password_query = " + PostaPasswordQuery
	return strings.Join(satirlar, "\n"), true, nil
}

// postaWebmailDonustur — 99-gosp-webmail.conf master passdb bloğuna
// `pass = yes` ekler (`pass = no` ise yes yapar).
func postaWebmailDonustur(icerik string) (string, bool, error) {
	satirlar := strings.Split(icerik, "\n")
	mi := -1
	for i, s := range satirlar {
		if reMasterYes.MatchString(s) {
			if mi >= 0 {
				return "", false, fmt.Errorf("%w: birden çok master passdb", ErrPostaTaninmayan)
			}
			mi = i
		}
	}
	if mi < 0 {
		return "", false, fmt.Errorf("%w: master passdb yok", ErrPostaTaninmayan)
	}
	// Bloğun sınırı: master satırının geriye/ileriye doğru ilk '{' / '}' satırı.
	bas, son := -1, -1
	for i := mi; i >= 0; i-- {
		if strings.HasSuffix(strings.TrimSpace(satirlar[i]), "{") {
			bas = i
			break
		}
	}
	for i := mi; i < len(satirlar); i++ {
		if strings.TrimSpace(satirlar[i]) == "}" {
			son = i
			break
		}
	}
	if bas < 0 || son < 0 || !strings.HasPrefix(strings.TrimSpace(satirlar[bas]), "passdb") {
		return "", false, fmt.Errorf("%w: master passdb bloğu ayrıştırılamadı", ErrPostaTaninmayan)
	}
	girinti := reMasterYes.FindStringSubmatch(satirlar[mi])[1]
	for i := bas + 1; i < son; i++ {
		if m := rePassAyar.FindStringSubmatch(satirlar[i]); m != nil {
			if strings.EqualFold(m[2], "yes") {
				return icerik, false, nil
			}
			satirlar[i] = m[1] + "pass = yes"
			return strings.Join(satirlar, "\n"), true, nil
		}
	}
	yeni := append([]string{}, satirlar[:mi+1]...)
	yeni = append(yeni, girinti+"pass = yes")
	yeni = append(yeni, satirlar[mi+1:]...)
	return strings.Join(yeni, "\n"), true, nil
}

// postaAskiMetinEtkin — yapılandırma metinleri askıyı uyguluyor mu.
// webmail == nil: webmail (master passdb) kurulu değil.
func postaAskiMetinEtkin(sqlConf string, webmail *string) bool {
	if _, degisir, err := postaSQLConfDonustur(sqlConf); err != nil || degisir {
		return false
	}
	for _, s := range strings.Split(sqlConf, "\n") {
		if reUserQuery.MatchString(s) && strings.Contains(s, "askida") {
			return false // user_query'de askı gelen postayı keser — "etkin" sayılmaz
		}
	}
	if webmail != nil {
		if _, degisir, err := postaWebmailDonustur(*webmail); err != nil || degisir {
			return false
		}
	}
	return true
}

// PostaAskiEtkin — bu sunucuda dovecot posta askısını uyguluyor mu (password_query
// askı koşullu + varsa webmail master passdb'si pass=yes). hesapaski, askıya
// alınan alan adında posta varsa bunu doğrular: bayrak yazılıp dovecot ona
// bakmıyorsa "posta kapandı" demek yanlış olurdu.
func PostaAskiEtkin() bool {
	b, err := os.ReadFile(postaDovecotSQLYol)
	if err != nil {
		return false
	}
	var wm *string
	if w, err := os.ReadFile(postaWebmailYol); err == nil {
		s := string(w)
		wm = &s
	} else if !errors.Is(err, os.ErrNotExist) {
		return false
	}
	return postaAskiMetinEtkin(string(b), wm)
}

// PostaAskiAltyapisi — mail eklentisi kurulu sunucuda posta askısı altyapısını
// idempotent kurar (bkz. dosya başı). Mail kurulu değilse (dovecot-sql.conf.ext
// yok) hiçbir şey yapmaz. Döner: bir şey değişti mi.
func PostaAskiAltyapisi(ctx context.Context) (bool, error) {
	postaAltyapiMu.Lock()
	defer postaAltyapiMu.Unlock()

	sqlOrj, err := os.ReadFile(postaDovecotSQLYol)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%s okunamadı: %w", postaDovecotSQLYol, err)
	}

	// 1) Şema — kolon password_query'den ÖNCE.
	degisti, err := postaKolonGuvence(ctx)
	if err != nil {
		return false, err
	}

	// 2) Yapılandırma metinleri.
	sqlYeni, sqlDegisir, err := postaSQLConfDonustur(string(sqlOrj))
	if err != nil {
		return degisti, fmt.Errorf("%s: %w", postaDovecotSQLYol, err)
	}
	var wmOrj []byte
	wmYeni, wmDegisir := "", false
	if b, err := os.ReadFile(postaWebmailYol); err == nil {
		wmOrj = b
		if wmYeni, wmDegisir, err = postaWebmailDonustur(string(b)); err != nil {
			return degisti, fmt.Errorf("%s: %w", postaWebmailYol, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return degisti, fmt.Errorf("%s okunamadı: %w", postaWebmailYol, err)
	}
	if !sqlDegisir && !wmDegisir {
		return degisti, nil
	}

	// 3) Yaz (yerinde: sahip/izin/SELinux etiketi korunur) + yedek.
	geriAl := func() {
		_ = os.WriteFile(postaDovecotSQLYol, sqlOrj, 0o600)
		if wmOrj != nil {
			_ = os.WriteFile(postaWebmailYol, wmOrj, 0o644)
		}
	}
	damga := time.Now().UTC().Format("20060102-150405")
	if sqlDegisir {
		if err := os.WriteFile(postaDovecotSQLYol+".gosp."+damga, sqlOrj, 0o600); err != nil {
			return degisti, fmt.Errorf("%s yedeklenemedi: %w", postaDovecotSQLYol, err)
		}
		if err := os.WriteFile(postaDovecotSQLYol, []byte(sqlYeni), 0o600); err != nil {
			geriAl()
			return degisti, fmt.Errorf("%s yazılamadı: %w", postaDovecotSQLYol, err)
		}
	}
	if wmDegisir {
		if err := os.WriteFile(postaWebmailYol+".gosp."+damga, wmOrj, 0o644); err != nil {
			geriAl()
			return degisti, fmt.Errorf("%s yedeklenemedi: %w", postaWebmailYol, err)
		}
		if err := os.WriteFile(postaWebmailYol, []byte(wmYeni), 0o644); err != nil {
			geriAl()
			return degisti, fmt.Errorf("%s yazılamadı: %w", postaWebmailYol, err)
		}
	}

	// 4) Doğrula + uygula; düşerse eski hâline.
	kctx := context.WithoutCancel(ctx)
	if out, err := postaKomut(kctx, "", "doveconf", "-n"); err != nil {
		geriAl()
		return degisti, fmt.Errorf("`doveconf -n` başarısız, posta askısı yapılandırması GERİ ALINDI: %v — %s", err, kirpPosta(out))
	}
	if out, err := postaKomut(kctx, "", "systemctl", "reload", "dovecot"); err != nil {
		geriAl()
		_, _ = postaKomut(kctx, "", "systemctl", "reload", "dovecot")
		return degisti, fmt.Errorf("dovecot reload başarısız, posta askısı yapılandırması GERİ ALINDI: %v — %s", err, kirpPosta(out))
	}
	return true, nil
}

// postaKolonGuvence — mailserver.virtual_domains.askida yoksa ekler.
func postaKolonGuvence(ctx context.Context) (bool, error) {
	out, err := postaKomut(ctx,
		"SELECT (SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='mailserver' AND table_name='virtual_domains'),"+
			" (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='mailserver' AND table_name='virtual_domains' AND column_name='askida');\n",
		"mysql", "--protocol=socket", "-N", "-B")
	if err != nil {
		return false, fmt.Errorf("posta şeması okunamadı: %v — %s", err, kirpPosta(out))
	}
	alan := strings.Fields(out)
	if len(alan) != 2 {
		return false, fmt.Errorf("posta şeması okunamadı: beklenmeyen çıktı %q", kirpPosta(out))
	}
	tablo, e1 := strconv.Atoi(alan[0])
	kolon, e2 := strconv.Atoi(alan[1])
	if e1 != nil || e2 != nil {
		return false, fmt.Errorf("posta şeması okunamadı: beklenmeyen çıktı %q", kirpPosta(out))
	}
	if tablo == 0 {
		return false, fmt.Errorf("%w: mailserver.virtual_domains yok", ErrPostaTaninmayan)
	}
	if kolon > 0 {
		return false, nil
	}
	if out, err := postaKomut(ctx,
		"ALTER TABLE mailserver.virtual_domains ADD COLUMN askida TINYINT(4) NOT NULL DEFAULT 0;\n",
		"mysql", "--protocol=socket", "-N", "-B"); err != nil {
		return false, fmt.Errorf("virtual_domains.askida eklenemedi: %v — %s", err, kirpPosta(out))
	}
	return true, nil
}

func kirpPosta(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}
