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
	"strconv"
	"strings"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

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

// runAsUser: komutu sk kullanıcısı olarak çalıştır
func runAsUser(sk, cwd, cmd string) (string, error) {
	args := []string{"-u", sk, "-H", "bash", "-lc", "cd " + cwd + " && " + cmd}
	out, err := exec.Command("sudo", args...).CombinedOutput()
	if err != nil {
		// sudo yoksa runuser dene
		args2 := []string{"-u", sk, "--", "bash", "-lc", "cd " + cwd + " && " + cmd}
		out, err = exec.Command("runuser", args2...).CombinedOutput()
	}
	return string(out), err
}

// gitClone: ilk kez klonla (target_dir varsa silinir)
func gitClone(sk, repoURL, branch, targetDir string) (sha string, log string, err error) {
	home := "/home/" + sk
	dst := filepath.Join(home, targetDir)
	// hedef temizle (ama public_html varsa içerik kaybolur — uyarı UI'da)
	_, _ = exec.Command("bash", "-c",
		fmt.Sprintf("rm -rf %q/{*,.*} 2>/dev/null; true", dst)).CombinedOutput()
	_ = os.MkdirAll(dst, 0755)
	_, _ = exec.Command("chown", sk+":"+sk, dst).CombinedOutput()

	out, err := runAsUser(sk, home,
		fmt.Sprintf("git clone --depth 1 --branch %s %s %s 2>&1", branch, repoURL, dst))
	log = out
	if err != nil {
		return "", out, err
	}
	shaOut, _ := runAsUser(sk, dst, "git rev-parse HEAD 2>&1")
	sha = strings.TrimSpace(shaOut)
	_, _ = exec.Command("restorecon", "-R", dst).CombinedOutput()
	return sha, log, nil
}

// gitPull: mevcut repo'da pull yap
func gitPull(sk, targetDir, branch string) (sha string, log string, err error) {
	home := "/home/" + sk
	dst := filepath.Join(home, targetDir)
	if _, err := os.Stat(filepath.Join(dst, ".git")); err != nil {
		return "", "", errors.New("hedef dizin git deposu değil; önce 'klonla' kullanın")
	}
	out, err := runAsUser(sk, dst,
		fmt.Sprintf("git fetch origin %s && git reset --hard origin/%s 2>&1", branch, branch))
	log = out
	if err != nil {
		return "", out, err
	}
	shaOut, _ := runAsUser(sk, dst, "git rev-parse HEAD 2>&1")
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
