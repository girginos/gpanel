// Package git: per-domain Git deploy (deploy key + repo + webhook auto-pull)
package git

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"girginospanel/internal/gizli"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

// badURLChars: repo URL'de yasak shell/enjeksiyon metakarakterleri
const badURLChars = "\"'`$();|&<>\\"

type Repo struct {
	ID            int64  `json:"id"`
	DomainID      int64  `json:"domain_id"`
	RepoURL       string `json:"repo_url"`
	Branch        string `json:"branch"`
	TargetDir     string `json:"target_dir"`
	DeployKeyPub  string `json:"deploy_key_pub"`
	WebhookSecret string `json:"webhook_secret"`
	SonSync       string `json:"son_sync,omitempty"`
	SonCommit     string `json:"son_commit,omitempty"`
	SonDurum      string `json:"son_durum"`
	Olusturulma   string `json:"olusturulma"`
	// WebhookURL: el ile webhook kurulumu için Payload URL (sunucu üretir).
	WebhookURL string `json:"webhook_url,omitempty"`
}

type Handlers struct {
	DB *sql.DB
	// WebhookBase: panelin GENEL adresi, örn. "https://203.0.113.10:23003" (kapı
	// modunda müşteri kapısı — admin portu sızmaz). HER ÇAĞRIDA okunur: müşteri kapısı
	// panelden değişince (yeniden başlatma yok) adres kapanmış portta kalmasın.
	// nil ya da "" → Payload URL üretilmez.
	WebhookBase func() string
}

// webhookURL: Payload URL. 🔴 Arayüz eskiden `http://<ip>:8443/...` kuruyordu: HTTPS
// portuna düz HTTP (nginx 400 döner) ve sabit port (dış port değişince / rol
// kapılarında ölü adres). Portu yalnız sunucu doğru bilir.
func (h *Handlers) webhookURL(secret string) string {
	if h.WebhookBase == nil || secret == "" {
		return ""
	}
	taban := strings.TrimRight(h.WebhookBase(), "/")
	if taban == "" {
		return ""
	}
	return taban + "/api/v1/git-webhook/" + secret
}

const selectAll = `SELECT id, domain_id, repo_url, branch, target_dir,
  deploy_key_pub, webhook_secret,
  COALESCE(DATE_FORMAT(son_sync,'%Y-%m-%d %H:%i'),''),
  son_commit, son_durum,
  DATE_FORMAT(created_at,'%Y-%m-%d %H:%i')
  FROM git_repos`

func scan(rs interface{ Scan(...any) error }) (Repo, error) {
	var r Repo
	err := rs.Scan(&r.ID, &r.DomainID, &r.RepoURL, &r.Branch, &r.TargetDir,
		&r.DeployKeyPub, &r.WebhookSecret, &r.SonSync, &r.SonCommit, &r.SonDurum, &r.Olusturulma)
	r.WebhookSecret = gizli.CozBagli(r.WebhookSecret, "webhook") // at-rest AEAD → gösterim için çöz
	return r, err
}

func (h *Handlers) lookupDomain(r *http.Request) (id int64, sk string, demo bool, err error) {
	id, _ = strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var dmo int
	err = h.DB.QueryRowContext(r.Context(),
		`SELECT sistem_kullanici, is_demo FROM domains WHERE id=?`, id).Scan(&sk, &dmo)
	demo = dmo == 1
	return
}

func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ----- Kiracı ev dizininde dosya işlemleri (CWE-59 link-following) -----
//
// 🔴 GÜVENLİK: /home/<sk> kiracının YAZABİLDİĞİ güvenilmez bir ağaçtır; kiracı orada
// FTP/SSH/PHP ile symlink kurabilir. Panel ROOT çalışır. Eskiden bu pakette hedef
// temizliği (ReadDir+RemoveAll), MkdirAll, `chown sk:sk dst`, deploy key okuma/üretme ve
// ~/.ssh/config yazımı ROOT olarak, sözcüksel (filepath.Join) yollar üzerinden
// yapılıyordu → çekirdek ara/son bileşen symlink'lerini root adına izliyordu:
//   - public_html → /home/<komşu>: komşunun sitesi root olarak silinir, sahipliği kiracıya geçer;
//   - ~/.ssh → /root/.ssh ya da .pub → herhangi bir dosya: içerik root olarak okunup
//     deploy_key_pub olarak kiracıya DÖNER;
//   - ~/.ssh/config → herhangi bir dosya: root üzerine yazar + chown ile kiracıya verir.
// Çözüm (kök neden): TÜM dosya işlemleri kiracı KİMLİĞİNDE (sudo -u / runuser) yapılır —
// kiracı kendi evinde zaten ne yapabiliyorsa panel onun adına yalnız onu yapar; komşunun
// 0710 evine ya da sistem dosyalarına çekirdek DAC erişim vermez (symlink'i ne zaman
// değiştirirse değiştirsin: TOCTOU bu bariyeri aşamaz). Ek olarak (savunma derinliği +
// açık hata) symlink bileşenli yollar önceden reddedilir; root'ta kalan tek adım
// (restorecon, etiketi politika varsayılanına döndürür) yalnız symlink'siz yolda çalışır.

// evKoku: kiracı ev dizinlerinin kökü. Yalnız testte t.TempDir'e çevrilir.
var evKoku = "/home"

func evDizini(sk string) string { return filepath.Join(evKoku, sk) }

// reSK: sistem kullanıcısı adı (c_<slug>[n]). "/" ve "." içeremez → ev yolu /home dışına
// taşınamaz.
var reSK = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

func gecerliSK(sk string) bool { return reSK.MatchString(sk) }

// kiraciCalistir: komutu kiracı (sk) kimliğiyle SHELL OLMADAN (argv) çalıştırır; panel
// ortamı verilmez. stdout ve stderr AYRI döner (deploy key okuması sudo uyarılarıyla
// kirlenmesin). stdin nil değilse komuta verilir. Testte kaydedici sahteyle değiştirilir.
var kiraciCalistir = kiraciCalistirGercek

func kiraciCalistirGercek(sk, cwd string, stdin []byte, argv ...string) (string, string, error) {
	calistir := func(ad string, args, env []string) (string, string, error) {
		cmd := exec.Command(ad, args...)
		if cwd != "" {
			cmd.Dir = cwd
		}
		if env != nil {
			cmd.Env = env
		}
		if stdin != nil {
			cmd.Stdin = bytes.NewReader(stdin)
		}
		var so, se bytes.Buffer
		cmd.Stdout, cmd.Stderr = &so, &se
		err := cmd.Run()
		return so.String(), se.String(), err
	}
	o, e, err := calistir("sudo", append([]string{"-u", sk, "-H", "--"}, argv...), nil)
	if err != nil {
		o, e, err = calistir("runuser", append([]string{"-u", sk, "--"}, argv...), []string{
			"HOME=" + evDizini(sk),
			"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		})
	}
	return o, e, err
}

// restoreconCalistir: SELinux etiketini politika varsayılanına döndürür (root). Testte
// kaydediciyle değiştirilir. Doğrudan değil restoreconKiraci üzerinden çağrılır.
var restoreconCalistir = func(yol string) {
	_, _ = exec.Command("restorecon", "-R", yol).CombinedOutput()
}

// restoreconKiraci: home/parcalar yolunda root restorecon — yol symlink bileşeni
// içeriyorsa ATLANIR (root aracı kiracı dışına yönlendirilmesin).
func restoreconKiraci(home string, parcalar ...string) {
	if rErr := yolSymlinksiz(home, parcalar...); rErr != nil {
		return
	}
	restoreconCalistir(filepath.Join(append([]string{home}, parcalar...)...))
}

// yolSymlinksiz: home ve home/parcalar[0]/…/parcalar[n] bileşenlerinden HİÇBİRİ symlink
// değil. Var olmayan ilk bileşende durur (kalanı kiracı kimliğiyle oluşturulacak).
// Yalnız Lstat kullanır (root hiçbir içerik okumaz). Tek başına TOCTOU'ya açıktır —
// asıl bariyer işlemlerin kiracı kimliğinde yapılmasıdır.
func yolSymlinksiz(home string, parcalar ...string) error {
	hfi, herr := os.Lstat(home)
	if herr != nil || !hfi.IsDir() { // Lstat: symlink hiçbir zaman IsDir değildir
		return errors.New("ev dizini güvenli değil (yok ya da symlink)")
	}
	yol := home
	for _, p := range parcalar {
		yol = filepath.Join(yol, p)
		fi, lerr := os.Lstat(yol)
		if errors.Is(lerr, os.ErrNotExist) {
			return nil
		}
		if lerr != nil {
			return fmt.Errorf("yol denetlenemedi: %s", p)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("yol symlink içeriyor, reddedildi: %s", p)
		}
	}
	return nil
}

// reDeployPub: yanıta/DB'ye yalnız TEK SATIR ed25519 ortak anahtarı döner.
var reDeployPub = regexp.MustCompile(`^ssh-ed25519 [A-Za-z0-9+/]+={0,3}( [ -~]*)?$`)

// pubOkuArgv: ortak anahtarı kiracı olarak, en çok 4 KiB okuyan komut (ed25519 .pub ~100 B).
func pubOkuArgv(pub string) []string { return []string{"head", "-c", "4096", "--", pub} }

const sshConfigGovde = `Host github.com
    HostName github.com
    User git
    IdentityFile ~/.ssh/gospanel_deploy
    StrictHostKeyChecking no
    UserKnownHostsFile=/dev/null
`

// generateDeployKey: ssh-keygen -t ed25519 ile no-passphrase key üretir, ~/.ssh/'a yazar.
// Tüm dosya işlemleri KİRACI kimliğinde (bkz. yukarıdaki güvenlik notu).
func generateDeployKey(sk string) (pubKey string, err error) {
	if !gecerliSK(sk) {
		return "", errors.New("geçersiz sistem kullanıcısı")
	}
	home := evDizini(sk)
	dir := filepath.Join(home, ".ssh")
	priv := filepath.Join(dir, "gospanel_deploy")
	pub := priv + ".pub"
	cfg := filepath.Join(dir, "config")

	// Açık ret (savunma derinliği): ~/.ssh, anahtar dosyaları ve config symlink olamaz.
	for _, ad := range []string{"gospanel_deploy", "gospanel_deploy.pub", "config"} {
		if kErr := yolSymlinksiz(home, ".ssh", ad); kErr != nil {
			return "", fmt.Errorf("~/.ssh/%s: %w", ad, kErr)
		}
	}
	if o, e, mErr := kiraciCalistir(sk, home, nil, "mkdir", "-p", "-m", "0700", "--", dir); mErr != nil {
		return "", fmt.Errorf("~/.ssh oluşturulamadı: %s: %w", strings.TrimSpace(o+e), mErr)
	}
	// Mevcut anahtar: kiracı olarak oku (boyut sınırlı: yol yarışla /dev/zero'ya çevrilse
	// bile panel belleği şişmez); yalnız geçerli ed25519 ortak anahtarı kabul et.
	if o, _, cErr := kiraciCalistir(sk, home, nil, pubOkuArgv(pub)...); cErr == nil {
		if mevcut := strings.TrimSpace(o); reDeployPub.MatchString(mevcut) {
			return mevcut, nil
		}
	}
	if o, e, rErr := kiraciCalistir(sk, home, nil, "rm", "-f", "--", priv, pub); rErr != nil {
		return "", fmt.Errorf("eski anahtar silinemedi: %s: %w", strings.TrimSpace(o+e), rErr)
	}
	if o, e, kgErr := kiraciCalistir(sk, home, nil, "ssh-keygen", "-q", "-t", "ed25519", "-N", "",
		"-C", "deploy@girginospanel/"+sk, "-f", priv); kgErr != nil {
		return "", fmt.Errorf("ssh-keygen: %s: %w", strings.TrimSpace(o+e), kgErr)
	}
	// ssh config'e github.com için bu key'i bağla (per-user, ~/.ssh/config) — kiracı yazar.
	// (tee içeriği stdout'a da yansıtır; kullanılmaz.)
	if _, e, tErr := kiraciCalistir(sk, home, []byte(sshConfigGovde), "tee", "--", cfg); tErr != nil {
		return "", fmt.Errorf("~/.ssh/config yazılamadı: %s: %w", strings.TrimSpace(e), tErr)
	}
	_, _, _ = kiraciCalistir(sk, home, nil, "chmod", "0600", "--", cfg)
	restoreconKiraci(home, ".ssh")

	o, _, err := kiraciCalistir(sk, home, nil, pubOkuArgv(pub)...)
	yeni := strings.TrimSpace(o)
	if err != nil || !reDeployPub.MatchString(yeni) {
		return "", errors.New("deploy key ortak anahtarı okunamadı")
	}
	return yeni, nil
}

var (
	reTargetDir = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	reBranch    = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
)

// hedefParcalari: target_dir'i doğrular ve ev dizinine göre bileşenlerine ayırır.
// Komut-enjeksiyon + path-traversal + 🔴 ev kökü/gizli dizin: boş, "." ya da ".." bileşen
// ("." , "a//b", "a/./b") ve "." ile başlayan (gizli: .ssh, .git, .config …) bileşen
// reddedilir → hedef ASLA ev kökü (tüm ev silinirdi) ya da gizli dizin olamaz. Sondaki
// "/" tolere edilir (eski kayıtlar "public_html/" olabilir).
func hedefParcalari(td string) ([]string, bool) {
	td = strings.TrimSpace(td)
	if len(td) > 128 || !reTargetDir.MatchString(td) {
		return nil, false
	}
	// Mutlak yol ("/etc" → ilk bileşen boş) ve ".." bileşeni de aşağıda reddedilir.
	parcalar := strings.Split(strings.TrimRight(td, "/"), "/")
	for _, p := range parcalar {
		if p == "" || strings.HasPrefix(p, ".") {
			return nil, false
		}
	}
	return parcalar, true
}

// gecerliTargetDir: komut-enjeksiyon + path-traversal + ev kökü/gizli dizin engelle
func gecerliTargetDir(td string) bool {
	_, ok := hedefParcalari(td)
	return ok
}

// gecerliBranch: git branch/ref adi dogrulama
func gecerliBranch(b string) bool {
	b = strings.TrimSpace(b)
	if b == "" || len(b) > 128 || strings.Contains(b, "..") {
		return false
	}
	return reBranch.MatchString(b)
}

// gecerliRepoURL: sema zorunlu + shell/enjeksiyon metakarakterleri reddet
func gecerliRepoURL(u string) bool {
	u = strings.TrimSpace(u)
	if u == "" || len(u) > 512 {
		return false
	}
	for _, c := range u {
		if c <= ' ' || strings.ContainsRune(badURLChars, c) {
			return false
		}
	}
	return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "git@") || strings.HasPrefix(u, "ssh://")
}

// runAsUserArgs: komutu sk kullanicisi olarak SHELL OLMADAN (argv) calistir; panel env
// verilmez. stdout+stderr birlikte döner (git günlüğü için).
func runAsUserArgs(sk, cwd string, argv ...string) (string, error) {
	o, e, err := kiraciCalistir(sk, cwd, nil, argv...)
	return o + e, err
}

// gitClone: ilk kez klonla (target_dir varsa İÇERİĞİ silinir — uyarı UI'da).
// Temizlik + oluşturma KİRACI kimliğinde (eskiden root: ReadDir+RemoveAll, MkdirAll, chown).
func gitClone(sk, repoURL, branch, targetDir string) (sha string, log string, err error) {
	parcalar, ok := hedefParcalari(targetDir)
	if !ok {
		return "", "", errors.New("geçersiz hedef dizin")
	}
	if !gecerliBranch(branch) {
		return "", "", errors.New("geçersiz branch adı")
	}
	if !gecerliRepoURL(repoURL) {
		return "", "", errors.New("geçersiz repo URL")
	}
	if !gecerliSK(sk) {
		return "", "", errors.New("geçersiz sistem kullanıcısı")
	}
	home := evDizini(sk)
	dst := filepath.Join(append([]string{home}, parcalar...)...)
	if hedefErr := yolSymlinksiz(home, parcalar...); hedefErr != nil {
		return "", "", hedefErr
	}
	if o, e, mkErr := kiraciCalistir(sk, home, nil, "mkdir", "-p", "--", dst); mkErr != nil {
		return "", strings.TrimSpace(o + e), fmt.Errorf("hedef dizin oluşturulamadı: %w", mkErr)
	}
	// İçeriği sil (dotfile dahil), dizinin kendisi (izin/ACL/setgid) korunur. find -P
	// (varsayılan) başlangıç noktası symlink ise içine inmez.
	if o, e, tmErr := kiraciCalistir(sk, home, nil, "find", dst, "-mindepth", "1", "-maxdepth", "1",
		"-exec", "rm", "-rf", "--", "{}", "+"); tmErr != nil {
		return "", strings.TrimSpace(o + e), fmt.Errorf("hedef dizin temizlenemedi: %w", tmErr)
	}

	out, err := runAsUserArgs(sk, home, "git", "clone", "--depth", "1", "--branch", branch, "--", repoURL, dst)
	log = out
	if err != nil {
		return "", out, err
	}
	shaOut, _ := runAsUserArgs(sk, dst, "git", "-C", dst, "rev-parse", "HEAD")
	sha = strings.TrimSpace(shaOut)
	restoreconKiraci(home, parcalar...)
	return sha, log, nil
}

// gitPull: mevcut repo'da pull yap (Pull + Webhook). .git varlığı kiracı kimliğinde denetlenir.
func gitPull(sk, targetDir, branch string) (sha string, log string, err error) {
	parcalar, ok := hedefParcalari(targetDir)
	if !ok {
		return "", "", errors.New("geçersiz hedef dizin")
	}
	if !gecerliBranch(branch) {
		return "", "", errors.New("geçersiz branch adı")
	}
	if !gecerliSK(sk) {
		return "", "", errors.New("geçersiz sistem kullanıcısı")
	}
	home := evDizini(sk)
	dst := filepath.Join(append([]string{home}, parcalar...)...)
	if depoErr := yolSymlinksiz(home, parcalar...); depoErr != nil {
		return "", "", depoErr
	}
	if _, _, gdErr := kiraciCalistir(sk, home, nil, "test", "-d", filepath.Join(dst, ".git")); gdErr != nil {
		return "", "", errors.New("hedef dizin git deposu değil; önce 'klonla' kullanın")
	}
	out, err := runAsUserArgs(sk, dst, "git", "-C", dst, "fetch", "origin", branch)
	if err == nil {
		o2, e2 := runAsUserArgs(sk, dst, "git", "-C", dst, "reset", "--hard", "origin/"+branch)
		out, err = out+o2, e2
	}
	log = out
	if err != nil {
		return "", out, err
	}
	shaOut, _ := runAsUserArgs(sk, dst, "git", "-C", dst, "rev-parse", "HEAD")
	sha = strings.TrimSpace(shaOut)
	restoreconKiraci(home, parcalar...)
	return sha, log, nil
}

// ----- HTTP handlers -----

type baglaReq struct {
	RepoURL   string `json:"repo_url"`
	Branch    string `json:"branch"`
	TargetDir string `json:"target_dir"`
}

func (h *Handlers) Get(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	row := h.DB.QueryRowContext(r.Context(), selectAll+" WHERE domain_id=? LIMIT 1", id)
	repo, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusOK, nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	repo.WebhookURL = h.webhookURL(repo.WebhookSecret)
	httpx.WriteJSON(w, http.StatusOK, repo)
}

// Bagla: deploy key olustur + repo URL kaydet (clone YAPMAZ; ayrica clone tetiklenir)
func (h *Handlers) Bagla(w http.ResponseWriter, r *http.Request) {
	id, sk, demo, err := h.lookupDomain(r)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğe git bağlanamaz")
		return
	}
	var req baglaReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	if req.RepoURL == "" {
		httpx.WriteError(w, http.StatusBadRequest, "repo_url zorunlu")
		return
	}
	if req.Branch == "" {
		req.Branch = "main"
	}
	if req.TargetDir == "" {
		req.TargetDir = "public_html"
	}
	// Guvenlik: repo_url / branch / target_dir dogrula (fail-fast; komut-enjeksiyon + traversal)
	if !gecerliRepoURL(req.RepoURL) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz repo URL (https://, git@ veya ssh:// olmalı)")
		return
	}
	if !gecerliBranch(req.Branch) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz branch adı")
		return
	}
	if !gecerliTargetDir(req.TargetDir) {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz hedef dizin")
		return
	}
	pub, err := generateDeployKey(sk)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "deploy key: "+err.Error())
		return
	}
	secret := randomHex(20)
	res, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO git_repos(domain_id, repo_url, branch, target_dir, deploy_key_pub, webhook_secret, webhook_secret_hash, son_durum)
		 VALUES(?,?,?,?,?,?,?, 'beklemede')
		 ON DUPLICATE KEY UPDATE repo_url=VALUES(repo_url), branch=VALUES(branch),
		   target_dir=VALUES(target_dir), deploy_key_pub=VALUES(deploy_key_pub)`,
		id, req.RepoURL, req.Branch, req.TargetDir, pub, gizli.SaklaBagli(secret, "webhook"), sha256hex(secret))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gid, _ := res.LastInsertId()
	row := h.DB.QueryRowContext(r.Context(), selectAll+" WHERE id=?", gid)
	repo, _ := scan(row)
	repo.WebhookURL = h.webhookURL(repo.WebhookSecret)
	httpx.WriteJSON(w, http.StatusCreated, repo)
}

// patliURL: github.com HTTPS repo_url'e domainin ŞİFRELİ PAT'ini (github_connections.pat)
// çözüp RUNTIME enjekte eder. PAT DB'de repo_url'e DÜZ METİN GÖMÜLMEZ (yalnız
// github_connections.pat'te AEAD). SSH/public/deploy-key repo'lar dokunulmadan döner.
func (h *Handlers) patliURL(domainID int64, repoURL string) string {
	if !strings.HasPrefix(repoURL, "https://github.com/") {
		return repoURL
	}
	var enc string
	_ = h.DB.QueryRow(`SELECT COALESCE(pat,'') FROM github_connections WHERE domain_id=?`, domainID).Scan(&enc)
	pat := gizli.CozBagli(enc, "github-pat")
	if pat == "" {
		return repoURL // token yok — public repo olabilir; olduğu gibi dene
	}
	return strings.Replace(repoURL, "https://", "https://"+pat+"@", 1)
}

// Klonla: ilk clone
func (h *Handlers) Klonla(w http.ResponseWriter, r *http.Request) {
	id, sk, demo, err := h.lookupDomain(r)
	if err != nil || demo {
		httpx.WriteError(w, http.StatusForbidden, "izin yok")
		return
	}
	var repoURL, branch, targetDir string
	var gid int64
	err = h.DB.QueryRowContext(r.Context(),
		`SELECT id, repo_url, branch, target_dir FROM git_repos WHERE domain_id=? LIMIT 1`, id).
		Scan(&gid, &repoURL, &branch, &targetDir)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusBadRequest, "önce repo bağlayın")
		return
	}
	sha, log, err := gitClone(sk, h.patliURL(id, repoURL), branch, targetDir)
	durum := "basarili"
	if err != nil {
		durum = "hata"
	}
	_, _ = h.DB.ExecContext(r.Context(),
		`UPDATE git_repos SET son_sync=NOW(), son_commit=?, son_durum=? WHERE id=?`,
		sha, durum, gid)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "klonlama: "+err.Error()+"\n"+log)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "commit": sha, "log": log,
	})
}

// Pull: var olan repo'da pull
func (h *Handlers) Pull(w http.ResponseWriter, r *http.Request) {
	id, sk, demo, err := h.lookupDomain(r)
	if err != nil || demo {
		httpx.WriteError(w, http.StatusForbidden, "izin yok")
		return
	}
	var branch, targetDir string
	var gid int64
	err = h.DB.QueryRowContext(r.Context(),
		`SELECT id, branch, target_dir FROM git_repos WHERE domain_id=? LIMIT 1`, id).
		Scan(&gid, &branch, &targetDir)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusBadRequest, "repo yok")
		return
	}
	sha, log, err := gitPull(sk, targetDir, branch)
	durum := "basarili"
	if err != nil {
		durum = "hata"
	}
	_, _ = h.DB.ExecContext(r.Context(),
		`UPDATE git_repos SET son_sync=NOW(), son_commit=?, son_durum=? WHERE id=?`,
		sha, durum, gid)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "pull: "+err.Error()+"\n"+log)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "commit": sha, "log": log,
	})
}

// Sil: repo kaydını sil (deploy key dosyada kalır)
func (h *Handlers) Sil(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM git_repos WHERE domain_id=?`, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "repo kaydı silinemedi: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Webhook: GitHub'tan gelen push event'i, secret dogrulanir + git pull
// URL: POST /api/v1/git-webhook/:secret
// Auth gerekmez (secret URL'de). GitHub kendisi imza da gonderir; biz sadece secret'i match ediyoruz.
func (h *Handlers) Webhook(w http.ResponseWriter, r *http.Request) {
	secret := chi.URLParam(r, "secret")
	if len(secret) < 16 {
		http.Error(w, "geçersiz secret", http.StatusBadRequest)
		return
	}
	var gid, domainID int64
	var sk, branch, targetDir string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT g.id, g.domain_id, d.sistem_kullanici, g.branch, g.target_dir
		 FROM git_repos g JOIN domains d ON d.id=g.domain_id
		 WHERE g.webhook_secret_hash=? LIMIT 1`, sha256hex(secret)).Scan(&gid, &domainID, &sk, &branch, &targetDir)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "secret eşleşmedi", http.StatusNotFound)
		return
	}
	if err != nil {
		// Kimlik doğrulamasız uç: ham DB hatası yanıta girmez.
		log.Printf("git webhook: repo okunamadı: %v", err)
		http.Error(w, "sunucu hatası", http.StatusInternalServerError)
		return
	}
	// 🔴 Askıdaki hesapta HİÇBİR komut çalışmaz: pull kiracı kimliğiyle git
	// çalıştırır ve kiracının .git/config / hooks'u kod yürütebilir (askıda
	// PHP/SSH/cron kapalıyken uzaktan tetiklenen tek yol olurdu). Yanıt eşleşmeyen
	// sırla AYNIDIR: kimlik doğrulamasız uçta hesabın askı durumu sızmaz. Durum
	// okunamazsa da çalışmaz.
	if askida, aerr := hesaplar.HesapAskida(r.Context(), h.DB, sk); aerr != nil {
		log.Printf("git webhook: domain %d: %v", domainID, aerr)
		http.Error(w, "sunucu hatası", http.StatusInternalServerError)
		return
	} else if askida {
		http.Error(w, "secret eşleşmedi", http.StatusNotFound)
		return
	}
	sha, log, perr := gitPull(sk, targetDir, branch)
	durum := "basarili"
	if perr != nil {
		durum = "hata-webhook"
	}
	_, _ = h.DB.ExecContext(r.Context(),
		`UPDATE git_repos SET son_sync=NOW(), son_commit=?, son_durum=? WHERE id=?`,
		sha, durum, gid)
	if perr != nil {
		http.Error(w, "pull başarısız: "+perr.Error()+"\n"+log, http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "commit": sha,
	})
}
