// Package guvenlikduvari: panel-yönetimli nftables güvenlik duvarı (inet girginos_fw).
//
// Güvenlik tasarımı:
//   - Tablo policy ACCEPT: kural yoksa hiçbir şey engellenmez (varsayılan bozmaz).
//   - "ct state established,related accept" en üstte: CANLI oturumlar (SSH dahil) asla kopmaz;
//     kurallar yalnızca YENİ bağlantıları etkiler → yanlış kural = anında kilitlenme DEĞİL.
//   - Kritik portlar (SSH/web/panel/DNS) KAPATILAMAZ (self-lockout + site-down koruması).
//   - Uygulamadan önce "nft -c" (check) → hata varsa uygulanmaz (nginx -t deseni).
//   - Kalıcılık: /etc/nftables/girginos_fw.nft + panel başlangıcında Reapply.
package guvenlikduvari

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

const (
	tabloAdi   = "girginos_fw"
	kuralDosya = "/etc/nftables/girginos_fw.nft"
)

// KAPATILAMAZ portlar: bunları "kapat" ile engellemek sunucuyu/paneli/siteleri düşürür.
var korumaliPortlar = map[int]bool{
	22:   true, // SSH — yönetim erişimi
	80:   true, // HTTP — müşteri siteleri
	443:  true, // HTTPS — müşteri siteleri
	8080: true, // panel API
	8443: true, // panel UI
	53:   true, // DNS (named)
}

// firewallSablonlari: tek-tıkla uygulanan hazır kural paketleri (yaygın açık portları kapat).
// Kritik portlar (korumaliPortlar) şablonlarda ASLA yer almaz.
type sablonKural struct {
	Tip, Protokol, Aciklama string
	Port                    int
}

var firewallSablonlari = map[string][]sablonKural{
	"mysql_kapat": {
		{"kapat", "tcp", "Şablon: MySQL dışa kapalı", 3306},
	},
	"ftp_kapat": {
		{"kapat", "tcp", "Şablon: FTP dışa kapalı", 21},
	},
	"mail_kapat": {
		{"kapat", "tcp", "Şablon: SMTP kapalı", 25},
		{"kapat", "tcp", "Şablon: SMTPS kapalı", 465},
		{"kapat", "tcp", "Şablon: Submission kapalı", 587},
		{"kapat", "tcp", "Şablon: POP3 kapalı", 110},
		{"kapat", "tcp", "Şablon: IMAP kapalı", 143},
	},
	"rpc_kapat": {
		{"kapat", "tcp", "Şablon: rpcbind kapalı", 111},
		{"kapat", "tcp", "Şablon: NFS kapalı", 2049},
	},
}

type Handlers struct{ DB *sql.DB }

type Kural struct {
	ID        int64  `json:"id"`
	Tip       string `json:"tip"`
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Protokol  string `json:"protokol"`
	Aciklama  string `json:"aciklama"`
	Aktif     bool   `json:"aktif"`
	CreatedAt string `json:"created_at"`
}

// GET /firewall — kural listesi + kapatılamaz portlar
func (h *Handlers) Liste(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, tip, ip, port, protokol, aciklama, aktif, DATE_FORMAT(created_at,'%Y-%m-%d %H:%i')
		 FROM firewall_kurallari ORDER BY id DESC`)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "listelenemedi")
		return
	}
	defer rows.Close()
	out := []Kural{}
	for rows.Next() {
		var k Kural
		var ak int
		if err := rows.Scan(&k.ID, &k.Tip, &k.IP, &k.Port, &k.Protokol, &k.Aciklama, &ak, &k.CreatedAt); err == nil {
			k.Aktif = ak == 1
			out = append(out, k)
		}
	}
	_ = rows.Err()
	korumali := make([]int, 0, len(korumaliPortlar))
	for p := range korumaliPortlar {
		korumali = append(korumali, p)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"kurallar": out, "korumali_portlar": korumali})
}

// POST /firewall  {tip, ip, port, protokol, aciklama}
func (h *Handlers) Ekle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tip      string `json:"tip"`
		IP       string `json:"ip"`
		Port     int    `json:"port"`
		Protokol string `json:"protokol"`
		Aciklama string `json:"aciklama"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	req.Tip = strings.ToLower(strings.TrimSpace(req.Tip))
	req.IP = strings.TrimSpace(req.IP)
	req.Protokol = strings.ToLower(strings.TrimSpace(req.Protokol))
	if req.Protokol == "" {
		req.Protokol = "tcp"
	}
	if req.Protokol != "tcp" && req.Protokol != "udp" {
		httpx.WriteError(w, http.StatusBadRequest, "protokol tcp veya udp olmalı")
		return
	}
	if req.Port < 0 || req.Port > 65535 {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz port (0-65535)")
		return
	}

	switch req.Tip {
	case "ban", "whitelist":
		if !gecerliIP(req.IP) {
			httpx.WriteError(w, http.StatusBadRequest, "geçerli bir IP veya CIDR girin (örn. 1.2.3.4 veya 1.2.3.0/24)")
			return
		}
	case "kapat":
		if req.Port == 0 {
			httpx.WriteError(w, http.StatusBadRequest, "kapatılacak port belirtin")
			return
		}
		if korumaliPortlar[req.Port] {
			httpx.WriteError(w, http.StatusBadRequest,
				fmt.Sprintf("port %d kritik (SSH/web/panel/DNS) — kapatılamaz, aksi halde sunucuya erişim kaybolur", req.Port))
			return
		}
		req.IP = "" // kapat = herkesten
	default:
		httpx.WriteError(w, http.StatusBadRequest, "tip: ban | whitelist | kapat")
		return
	}
	// ban: kritik portu tek IP'ye yasaklamak serbest (saldırgan engelleme) — canlı oturum
	// established-accept ile korunur; ama tüm-portlar (0) + kritik yönetim IP kombinasyonu
	// riskli; UI uyarı gösterir. Kural: ban port 0 whitelist'i ezmez (whitelist önce gelir).

	res, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO firewall_kurallari (tip, ip, port, protokol, aciklama, aktif) VALUES (?,?,?,?,?,1)`,
		req.Tip, req.IP, req.Port, req.Protokol, req.Aciklama)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "kayıt eklenemedi")
		return
	}
	nid, _ := res.LastInsertId()

	if err := h.rebuild(); err != nil {
		// uygulama başarısız → kaydı geri al (tutarlılık)
		_, _ = h.DB.Exec(`DELETE FROM firewall_kurallari WHERE id=?`, nid)
		_ = h.rebuild()
		httpx.WriteError(w, http.StatusInternalServerError, "firewall uygulanamadı: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"ok": true, "id": nid})
}

// POST /firewall/sablon  {"sablon":"mysql_kapat"} — hazır kural paketini uygular (idempotent).
func (h *Handlers) Sablon(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Sablon string `json:"sablon"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "geçersiz gövde")
		return
	}
	kurallar, ok := firewallSablonlari[req.Sablon]
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "bilinmeyen şablon")
		return
	}
	eklenen := 0
	for _, k := range kurallar {
		if korumaliPortlar[k.Port] { // güvenlik: şablonda kritik port olmamalı ama yine de atla
			continue
		}
		var n int
		_ = h.DB.QueryRow(`SELECT COUNT(*) FROM firewall_kurallari WHERE tip=? AND port=? AND protokol=? AND ip=''`,
			k.Tip, k.Port, k.Protokol).Scan(&n)
		if n > 0 { // zaten var → atla (idempotent)
			continue
		}
		if _, err := h.DB.ExecContext(r.Context(),
			`INSERT INTO firewall_kurallari (tip, ip, port, protokol, aciklama, aktif) VALUES (?,'',?,?,?,1)`,
			k.Tip, k.Port, k.Protokol, k.Aciklama); err == nil {
			eklenen++
		}
	}
	if err := h.rebuild(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "firewall uygulanamadı: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "eklenen": eklenen})
}

// DELETE /firewall/{id}
func (h *Handlers) Sil(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM firewall_kurallari WHERE id=?`, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "silinemedi")
		return
	}
	if err := h.rebuild(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "firewall güncellenemedi: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /firewall/{id}/durum  {aktif}
func (h *Handlers) Durum(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req struct {
		Aktif bool `json:"aktif"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	ak := 0
	if req.Aktif {
		ak = 1
	}
	if _, err := h.DB.ExecContext(r.Context(), `UPDATE firewall_kurallari SET aktif=? WHERE id=?`, ak, id); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "güncellenemedi")
		return
	}
	if err := h.rebuild(); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "firewall güncellenemedi: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// rebuild: aktif kurallardan nft ruleset üret, önce "nft -c" ile doğrula, sonra uygula + kalıcı yaz.
func (h *Handlers) rebuild() error {
	rows, err := h.DB.Query(`SELECT tip, ip, port, protokol FROM firewall_kurallari WHERE aktif=1 ORDER BY
		FIELD(tip,'whitelist','kapat','ban'), id`)
	if err != nil {
		return err
	}
	var wl, kapat, ban []string
	// kisitla: port-belirtili whitelist kuralı o portu ALLOWLIST moduna alır.
	// Yani "ip saddr X dport P accept" satırlarının ardına "proto dport P drop"
	// eklenir → o porta SADECE izinli IP'ler erişir, diğer herkes engellenir.
	// Anahtar "proto/port" → deterministik çıktı için sıralanır.
	kisitla := map[string]bool{}
	for rows.Next() {
		var tip, ip, proto string
		var port int
		if err := rows.Scan(&tip, &ip, &port, &proto); err != nil {
			continue
		}
		switch tip {
		case "whitelist":
			wl = append(wl, "\t\t"+saddr(ip)+dport(proto, port)+"accept")
			if port > 0 { // porta özel izin → o port allowlist'e geçer
				kisitla[proto+"/"+strconv.Itoa(port)] = true
			}
		case "kapat":
			kapat = append(kapat, "\t\t"+proto+" dport "+strconv.Itoa(port)+" drop")
		case "ban":
			ban = append(ban, "\t\t"+saddr(ip)+dport(proto, port)+"drop")
		}
	}
	_ = rows.Err()
	rows.Close()

	// allowlist drop'ları: izinli IP accept'lerinden SONRA, kapat/ban'dan ÖNCE gelir.
	// (established,related + lo en üstte olduğu için canlı oturum/SSH asla kopmaz.)
	kisitAnahtar := make([]string, 0, len(kisitla))
	for key := range kisitla {
		kisitAnahtar = append(kisitAnahtar, key)
	}
	sort.Strings(kisitAnahtar)
	var kisit []string
	for _, key := range kisitAnahtar {
		if i := strings.IndexByte(key, '/'); i > 0 {
			kisit = append(kisit, "\t\t"+key[:i]+" dport "+key[i+1:]+" drop")
		}
	}

	var b bytes.Buffer
	// idempotent atomik değiştirme: tabloyu garanti et → sil → yeniden kur
	b.WriteString("table inet " + tabloAdi + " {}\n")
	b.WriteString("delete table inet " + tabloAdi + "\n")
	b.WriteString("table inet " + tabloAdi + " {\n")
	b.WriteString("\tchain input {\n")
	b.WriteString("\t\ttype filter hook input priority filter; policy accept;\n")
	b.WriteString("\t\tct state established,related accept\n")
	b.WriteString("\t\tiif \"lo\" accept\n")
	// sıra önemli: whitelist (accept) → allowlist-kısıt (drop) → kapat (drop) → ban (drop)
	for _, r := range wl {
		b.WriteString(r + "\n")
	}
	for _, r := range kisit {
		b.WriteString(r + "\n")
	}
	for _, r := range kapat {
		b.WriteString(r + "\n")
	}
	for _, r := range ban {
		b.WriteString(r + "\n")
	}
	b.WriteString("\t}\n}\n")

	ruleset := b.Bytes()
	// 1) doğrula (check) — bozuk ruleset uygulanmaz
	if out, err := nftCheck(ruleset); err != nil {
		return fmt.Errorf("nft doğrulama: %s", strings.TrimSpace(out))
	}
	// 2) uygula
	if out, err := nftApply(ruleset); err != nil {
		return fmt.Errorf("nft uygulama: %s", strings.TrimSpace(out))
	}
	// 3) kalıcı yaz (reboot sonrası panel başlangıcı yeniden yükler)
	_ = os.MkdirAll("/etc/nftables", 0o755)
	_ = os.WriteFile(kuralDosya, ruleset, 0o600)
	return nil
}

// Reapply: panel başlangıcında çağrılır — reboot sonrası kuralları DB'den yeniden uygular.
func Reapply(db *sql.DB) error {
	h := &Handlers{DB: db}
	return h.rebuild()
}

// FirewalldDevral: AlmaLinux/RHEL varsayılan firewalld'sini devre dışı bırakır.
//
// 🔴 NEDEN: GirginOSPanel firewall'ı kendi nftables tablosunu (girginos_fw)
// yönetir. firewalld ile AYNI ANDA çalışırsa çakışır — nftables'ta bir tablonun
// base chain'inde 'drop' KESİNDİR (terminal) → firewalld panelin portlarını
// (8443/80/443/53/21) düşürür ve panelin 'accept' politikası bunu EZEMEZ.
// Sonuç: panel UI erişilemez, siteler açılmaz, Cloudflare "origin unreachable".
// Bu yüzden tek firewall otoritesi panel olsun diye firewalld'yi durdur+kapat+
// mask ediyoruz. firewalld durunca kendi nftables tablosunu ANINDA temizler →
// reboot GEREKMEZ. Açılışta her seferinde çağrılır; idempotent (kurulu değilse
// veya zaten mask'liyse hiçbir şey yapmaz), dolayısıyla self-healing.
func FirewalldDevral() {
	// Kurulu mu? systemctl cat sıfır dönerse unit vardır.
	if exec.Command("systemctl", "cat", "firewalld.service").Run() != nil {
		return // firewalld yok — panel zaten tek firewall
	}
	// Zaten devralınmış mı? is-enabled "masked" döner.
	if out, _ := exec.Command("systemctl", "is-enabled", "firewalld").Output(); strings.TrimSpace(string(out)) == "masked" {
		return
	}
	_ = exec.Command("systemctl", "disable", "--now", "firewalld").Run()
	_ = exec.Command("systemctl", "mask", "firewalld").Run()
	log.Printf("firewall: firewalld durduruldu+mask edildi — tek firewall = panel nftables (girginos_fw)")
}

// --- yardımcılar ---

func gecerliIP(s string) bool {
	if s == "" {
		return false
	}
	if net.ParseIP(s) != nil {
		return true
	}
	_, _, err := net.ParseCIDR(s)
	return err == nil
}

// saddr: IP/CIDR ailesine göre "ip saddr X " veya "ip6 saddr X " döner.
func saddr(ip string) string {
	fam := "ip"
	host := ip
	if i := strings.IndexByte(ip, '/'); i >= 0 {
		host = ip[:i]
	}
	if p := net.ParseIP(host); p != nil && p.To4() == nil {
		fam = "ip6"
	}
	return fam + " saddr " + ip + " "
}

// dport: port>0 ise "proto dport N " döner, değilse boş (tüm portlar).
func dport(proto string, port int) string {
	if port <= 0 {
		return ""
	}
	return proto + " dport " + strconv.Itoa(port) + " "
}

func nftCheck(ruleset []byte) (string, error) {
	cmd := exec.Command("nft", "-c", "-f", "-")
	cmd.Stdin = bytes.NewReader(ruleset)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func nftApply(ruleset []byte) (string, error) {
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = bytes.NewReader(ruleset)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
