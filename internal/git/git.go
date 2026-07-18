// Package git: per-domain Git deploy (deploy key + repo + webhook auto-pull)
package git

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

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
}

type Handlers struct {
	DB *sql.DB
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

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func deployKeyDir(sk string) string {
	return "/home/" + sk + "/.ssh"
}

// generateDeployKey: ssh-keygen -t ed25519 ile no-passphrase key uretir, /home/<sk>/.ssh/'a yazar
func generateDeployKey(sk string) (pubKey string, err error) {
	dir := deployKeyDir(sk)
	_ = os.MkdirAll(dir, 0700)
	priv := filepath.Join(dir, "gospanel_deploy")
	pub := priv + ".pub"

	if _, err := os.Stat(pub); err == nil {
		// Mevcut key kullan
		b, _ := os.ReadFile(pub)
		return strings.TrimSpace(string(b)), nil
	}
	_, _ = exec.Command("rm", "-f", priv, pub).CombinedOutput()
	out, err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-C", "deploy@girginospanel/"+sk, "-f", priv).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ssh-keygen: %s: %w", strings.TrimSpace(string(out)), err)
	}
	// chown + perms
	_, _ = exec.Command("chown", "-R", sk+":"+sk, dir).CombinedOutput()
	_ = os.Chmod(priv, 0600)
	_ = os.Chmod(pub, 0644)

	// ssh config'e github.com için bu key'i bağla (per-user, ~/.ssh/config)
	cfg := filepath.Join(dir, "config")
	cfgBody := `Host github.com
    HostName github.com
    User git
    IdentityFile ~/.ssh/gospanel_deploy
    StrictHostKeyChecking no
    UserKnownHostsFile=/dev/null
`
	_ = os.WriteFile(cfg, []byte(cfgBody), 0600)
	_, _ = exec.Command("chown", sk+":"+sk, cfg).CombinedOutput()
	_, _ = exec.Command("restorecon", "-R", dir).CombinedOutput()

	b, _ := os.ReadFile(pub)
	return strings.TrimSpace(string(b)), nil
}

var (
	reTargetDir = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	reBranch    = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
)

// gecerliTargetDir: komut-enjeksiyon + path-traversal engelle
func gecerliTargetDir(td string) bool {
	td = strings.TrimSpace(td)
	if td == "" || len(td) > 128 {
		return false
	}
	if strings.HasPrefix(td, "/") || strings.Contains(td, "..") {
		return false
	}
	return reTargetDir.MatchString(td)
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

// temizleDizinIcerigi: dizin icerigini SHELL OLMADAN sil (dotfile dahil)
func temizleDizinIcerigi(dst string) {
	entries, err := os.ReadDir(dst)
	if err != nil {
		return
	}
	for _, e := range entries {
		_ = os.RemoveAll(filepath.Join(dst, e.Name()))
	}
}

// runAsUserArgs: komutu sk kullanicisi olarak SHELL OLMADAN (argv) calistir; panel env verilmez
func runAsUserArgs(sk, cwd string, argv ...string) (string, error) {
	sudoArgs := append([]string{"-u", sk, "-H", "--"}, argv...)
	cmd := exec.Command("sudo", sudoArgs...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		ruArgs := append([]string{"-u", sk, "--"}, argv...)
		cmd2 := exec.Command("runuser", ruArgs...)
		if cwd != "" {
			cmd2.Dir = cwd
		}
		cmd2.Env = []string{
			"HOME=/home/" + sk,
			"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		}
		out, err = cmd2.CombinedOutput()
	}
	return string(out), err
}

// gitClone: ilk kez klonla (target_dir varsa silinir)
func gitClone(sk, repoURL, branch, targetDir string) (sha string, log string, err error) {
	if !gecerliTargetDir(targetDir) {
		return "", "", errors.New("geçersiz hedef dizin")
	}
	if !gecerliBranch(branch) {
		return "", "", errors.New("geçersiz branch adı")
	}
	if !gecerliRepoURL(repoURL) {
		return "", "", errors.New("geçersiz repo URL")
	}
	home := "/home/" + sk
	dst := filepath.Join(home, targetDir)
	// hedef temizle (ama public_html varsa içerik kaybolur — uyarı UI'da)
	temizleDizinIcerigi(dst)
	_ = os.MkdirAll(dst, 0755)
	_, _ = exec.Command("chown", sk+":"+sk, dst).CombinedOutput()

	out, err := runAsUserArgs(sk, home, "git", "clone", "--depth", "1", "--branch", branch, "--", repoURL, dst)
	log = out
	if err != nil {
		return "", out, err
	}
	shaOut, _ := runAsUserArgs(sk, dst, "git", "-C", dst, "rev-parse", "HEAD")
	sha = strings.TrimSpace(shaOut)
	_, _ = exec.Command("restorecon", "-R", dst).CombinedOutput()
	return sha, log, nil
}

// gitPull: mevcut repo'da pull yap
func gitPull(sk, targetDir, branch string) (sha string, log string, err error) {
	if !gecerliTargetDir(targetDir) {
		return "", "", errors.New("geçersiz hedef dizin")
	}
	if !gecerliBranch(branch) {
		return "", "", errors.New("geçersiz branch adı")
	}
	home := "/home/" + sk
	dst := filepath.Join(home, targetDir)
	if _, err := os.Stat(filepath.Join(dst, ".git")); err != nil {
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
	_, _ = exec.Command("restorecon", "-R", dst).CombinedOutput()
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
		`INSERT INTO git_repos(domain_id, repo_url, branch, target_dir, deploy_key_pub, webhook_secret, son_durum)
		 VALUES(?,?,?,?,?,?, 'beklemede')
		 ON DUPLICATE KEY UPDATE repo_url=VALUES(repo_url), branch=VALUES(branch),
		   target_dir=VALUES(target_dir), deploy_key_pub=VALUES(deploy_key_pub)`,
		id, req.RepoURL, req.Branch, req.TargetDir, pub, secret)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gid, _ := res.LastInsertId()
	row := h.DB.QueryRowContext(r.Context(), selectAll+" WHERE id=?", gid)
	repo, _ := scan(row)
	httpx.WriteJSON(w, http.StatusCreated, repo)
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
	sha, log, err := gitClone(sk, repoURL, branch, targetDir)
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
	_, _ = h.DB.ExecContext(r.Context(), `DELETE FROM git_repos WHERE domain_id=?`, id)
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
		 WHERE g.webhook_secret=? LIMIT 1`, secret).Scan(&gid, &domainID, &sk, &branch, &targetDir)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "secret eşleşmedi", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
