package hostuyg

// Recipe-özel yönetim eylemleri — TS3 ServerQuery, Headscale CLI vb.
// Handler: POST /admin/hostuyg/{id}/eylem  {"fn": "user_list", "args": {...}}

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// EylemIstek — generic gövde.
type EylemIstek struct {
	Fn   string          `json:"fn"`
	Args json.RawMessage `json:"args,omitempty"`
}

// metaOku — UygulamaKayit.MetaJSON içinden string anahtar okur.
func metaOku(k *UygulamaKayit, anahtar string) string {
	if k.MetaJSON == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(k.MetaJSON), &m); err != nil {
		return ""
	}
	if v, ok := m[anahtar].(string); ok {
		return v
	}
	return ""
}

// EylemYurut — kod'a göre dispatch.
func (h *Handler) Eylem(w http.ResponseWriter, r *http.Request) {
	id, err := idAyikla(r.URL.Path, "eylem")
	if err != nil {
		yazHata(w, 400, err.Error())
		return
	}
	k, err := UygulamaGetir(r.Context(), h.DB, id)
	if err != nil {
		yazHata(w, 404, "bulunamadı")
		return
	}
	var g EylemIstek
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		yazHata(w, 400, "geçersiz gövde")
		return
	}
	ctx, iptal := context.WithTimeout(r.Context(), 30*time.Second)
	defer iptal()

	var sonuc any
	switch k.Kod {
	case "teamspeak3":
		sonuc, err = ts3Eylem(ctx, k, g)
	case "headscale":
		sonuc, err = headscaleEylem(ctx, k, g)
	default:
		yazHata(w, 400, "bu recipe için özel eylem yok")
		return
	}
	if err != nil {
		yazHata(w, 500, err.Error())
		return
	}
	yaz(w, 200, sonuc)
}

/* ---------- TeamSpeak 3 ServerQuery ---------- */

// ts3Query — TCP 10011'e bağlan, komutu gönder, cevap oku (banner+response).
// Basit protokol: "\r\n" satır bazlı. `error id=0 msg=ok\r\n` = başarı.
func ts3Query(ctx context.Context, k *UygulamaKayit, komutlar ...string) ([]string, error) {
	// Port 10011 çak varsa - portlar map'inden çek
	port := 10011
	for _, p := range k.Portlar {
		if p.Aciklama == "query" && p.Protokol == "tcp" {
			port = p.Port
			break
		}
	}
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("TS3 bağlantı: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	rd := bufio.NewReader(conn)
	// Banner: "TS3\r\n" + welcome message
	_, _ = rd.ReadString('\n') // "TS3"
	_, _ = rd.ReadString('\n') // welcome

	// İlk komut olarak login gönder (varsa)
	if pwd := metaOku(k, "ts3_admin_password"); pwd != "" {
		if _, err := conn.Write([]byte("login serveradmin " + ts3Escape(pwd) + "\n")); err != nil {
			return nil, err
		}
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				return nil, fmt.Errorf("login yanıt: %w", err)
			}
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "error ") {
				if !strings.Contains(line, "id=0") {
					return nil, fmt.Errorf("TS3 login: %s", line)
				}
				break
			}
		}
	}

	sonuclar := []string{}
	for _, c := range komutlar {
		if _, err := conn.Write([]byte(c + "\n")); err != nil {
			return sonuclar, err
		}
		// Response: satırlar + "error id=0 msg=ok"
		var sb strings.Builder
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				return sonuclar, err
			}
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "error ") {
				if !strings.Contains(line, "id=0") {
					return sonuclar, fmt.Errorf("TS3 error: %s", line)
				}
				break
			}
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(line)
		}
		sonuclar = append(sonuclar, sb.String())
	}
	return sonuclar, nil
}

func ts3Eylem(ctx context.Context, k *UygulamaKayit, g EylemIstek) (any, error) {
	switch g.Fn {
	case "kimlik":
		// MetaJSON'dan serveradmin bilgilerini oku
		return map[string]any{
			"query_kullanici": "serveradmin",
			"query_sifre":     metaOku(k, "ts3_admin_password"),
			"admin_token":     metaOku(k, "ts3_admin_token"),
		}, nil
	case "token_yenile":
		// Yeni ServerAdmin privilege key üret (login gerektirir)
		out, err := ts3Query(ctx, k, "use sid=1", "privilegekeyadd tokentype=0 tokenid1=6 tokenid2=0")
		if err != nil {
			return nil, err
		}
		if len(out) < 2 {
			return nil, errors.New("token yanıt eksik")
		}
		// "token=xyz..." formatında
		parsed := ts3Parse(out[1])
		return map[string]any{"token": parsed["token"]}, nil
	case "sunucu_bilgi":
		out, err := ts3Query(ctx, k, "use sid=1", "serverinfo")
		if err != nil {
			return nil, err
		}
		if len(out) < 2 {
			return nil, errors.New("TS3 yanıt eksik")
		}
		return map[string]any{"raw": out[1], "parsed": ts3Parse(out[1])}, nil
	case "client_liste":
		out, err := ts3Query(ctx, k, "use sid=1", "clientlist -uid -info")
		if err != nil {
			return nil, err
		}
		if len(out) < 2 {
			return nil, errors.New("TS3 yanıt eksik")
		}
		// clientlist birden fazla client döner, "|" ile ayrılmış
		clients := []map[string]string{}
		for _, cl := range strings.Split(out[1], "|") {
			clients = append(clients, ts3Parse(cl))
		}
		return map[string]any{"clients": clients}, nil
	case "client_kick":
		var args struct {
			CLID  string
			Sebep string
		}
		_ = json.Unmarshal(g.Args, &args)
		if args.CLID == "" {
			return nil, errors.New("clid gerekli")
		}
		if args.Sebep == "" {
			args.Sebep = "kicked by panel"
		}
		out, err := ts3Query(ctx, k, "use sid=1",
			fmt.Sprintf("clientkick clid=%s reasonid=5 reasonmsg=%s", args.CLID, ts3Escape(args.Sebep)))
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "raw": out}, nil
	default:
		return nil, fmt.Errorf("bilinmeyen TS3 fn: %s", g.Fn)
	}
}

// ts3Parse — "clid=1 client_nickname=Test\stest cid=1" → map
// Öncesinde baştaki CR/space temizlenir (TS3 satır başlarına \r ekler).
func ts3Parse(s string) map[string]string {
	out := map[string]string{}
	s = strings.TrimSpace(s)
	for _, kv := range strings.Fields(s) {
		if i := strings.Index(kv, "="); i > 0 {
			out[kv[:i]] = ts3Unescape(kv[i+1:])
		} else if kv != "" {
			// key-only field (ör. "virtualserver_password" bayrağı)
			out[kv] = ""
		}
	}
	return out
}

// TS3 escape: space→\s, \→\\, /→\/, |→\p
var ts3EscapeMap = map[string]string{" ": "\\s", "\\": "\\\\", "/": "\\/", "|": "\\p"}

func ts3Escape(s string) string {
	for a, b := range ts3EscapeMap {
		s = strings.ReplaceAll(s, a, b)
	}
	return s
}
func ts3Unescape(s string) string {
	r := strings.NewReplacer("\\s", " ", "\\p", "|", "\\/", "/", "\\\\", "\\")
	return r.Replace(s)
}

/* ---------- Headscale CLI wrapper ---------- */

// headscaleCLI — headscale binary'sini çalıştır, JSON output parse et.
// Binary: {kurulum_yolu}/headscale, config: {kurulum_yolu}/config.yaml
func headscaleCLI(ctx context.Context, k *UygulamaKayit, args ...string) ([]byte, error) {
	binary := k.KurulumYolu + "/headscale"
	config := k.KurulumYolu + "/config.yaml"
	fullArgs := append([]string{"-c", config, "-o", "json"}, args...)
	cmd := exec.CommandContext(ctx, binary, fullArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("headscale %v: %s (%w)", args, strings.TrimSpace(string(out)), err)
	}
	return out, nil
}

func headscaleEylem(ctx context.Context, k *UygulamaKayit, g EylemIstek) (any, error) {
	switch g.Fn {
	case "user_list":
		out, err := headscaleCLI(ctx, k, "users", "list")
		if err != nil {
			return nil, err
		}
		var users []map[string]any
		_ = json.Unmarshal(out, &users)
		return map[string]any{"users": users}, nil
	case "user_create":
		var args struct{ Ad string }
		_ = json.Unmarshal(g.Args, &args)
		if args.Ad == "" {
			return nil, errors.New("ad gerekli")
		}
		out, err := headscaleCLI(ctx, k, "users", "create", args.Ad)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "raw": string(out)}, nil
	case "user_delete":
		var args struct{ Ad string }
		_ = json.Unmarshal(g.Args, &args)
		if args.Ad == "" {
			return nil, errors.New("ad gerekli")
		}
		out, err := headscaleCLI(ctx, k, "users", "destroy", "--force", args.Ad)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "raw": string(out)}, nil
	case "node_list":
		out, err := headscaleCLI(ctx, k, "nodes", "list")
		if err != nil {
			return nil, err
		}
		var nodes []map[string]any
		_ = json.Unmarshal(out, &nodes)
		return map[string]any{"nodes": nodes}, nil
	case "node_delete":
		var args struct{ ID int64 }
		_ = json.Unmarshal(g.Args, &args)
		if args.ID <= 0 {
			return nil, errors.New("node id gerekli")
		}
		out, err := headscaleCLI(ctx, k, "nodes", "delete", "-i", strconv.FormatInt(args.ID, 10), "--force")
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "raw": string(out)}, nil
	case "preauthkey_create":
		var args struct {
			User      string
			Reusable  bool
			Ephemeral bool
			Expiry    string // "24h"
		}
		_ = json.Unmarshal(g.Args, &args)
		if args.User == "" {
			return nil, errors.New("user gerekli")
		}
		if args.Expiry == "" {
			args.Expiry = "24h"
		}
		cliArgs := []string{"preauthkeys", "create", "--user", args.User, "--expiration", args.Expiry}
		if args.Reusable {
			cliArgs = append(cliArgs, "--reusable")
		}
		if args.Ephemeral {
			cliArgs = append(cliArgs, "--ephemeral")
		}
		out, err := headscaleCLI(ctx, k, cliArgs...)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "key": strings.TrimSpace(string(out))}, nil
	default:
		return nil, fmt.Errorf("bilinmeyen headscale fn: %s", g.Fn)
	}
}
