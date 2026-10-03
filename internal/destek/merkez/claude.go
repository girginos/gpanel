package merkez

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"girginospanel/internal/destek/protokol"
)

// ClaudeIstek: Messages API isteği. Govde, istekKur'un ürettiği BAYT-BAYT
// gövdedir; istemci onu yeniden serileştirmeden gönderir (prompt cache öneki).
type ClaudeIstek struct {
	Model     string
	MaxTokens int64
	Govde     []byte
}

// Kullanim: yanıttaki usage alanları.
type Kullanim struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreation            *struct {
		Ephemeral5m int64 `json:"ephemeral_5m_input_tokens"`
		Ephemeral1h int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
}

// ClaudeYanit: yanıtın merkezin kullandığı alanları; içerik blokları HAM tutulur.
type ClaudeYanit struct {
	ID          string            `json:"id"`
	Model       string            `json:"model"`
	Content     []json.RawMessage `json:"content"`
	StopReason  string            `json:"stop_reason"`
	StopDetails *struct {
		Type        string `json:"type"`
		Category    string `json:"category"`
		Explanation string `json:"explanation"`
	} `json:"stop_details"`
	Usage Kullanim `json:"usage"`
}

// ClaudeIstemci: testte SAHTE ile değiştirilir; üretimde SDKIstemci.
type ClaudeIstemci interface {
	Gonder(ctx context.Context, ist ClaudeIstek) (*ClaudeYanit, error)
}

// SDKIstemci: resmi Go SDK'sı (anthropic-sdk-go). Gövde option.WithRequestBody ile
// OLDUĞU GİBİ gönderilir: tools = protokol.ToolsJSON() baytları, system = SistemSabit()
// — SDK tipleriyle yeniden serileştirme önek baytlarını değiştirebilirdi.
// Kimlik bilgisi YALNIZ verilen anahtardır: WithoutEnvironmentDefaults ortamdan
// (ANTHROPIC_API_KEY vb.) hiçbir şey okumaz.
type SDKIstemci struct {
	c anthropic.Client
}

// YeniSDKIstemci: taban boşsa SDK varsayılanı (api.anthropic.com).
func YeniSDKIstemci(anahtar, taban string) *SDKIstemci {
	opts := []option.RequestOption{
		option.WithoutEnvironmentDefaults(),
		option.WithAPIKey(anahtar),
		option.WithMaxRetries(2),
	}
	if taban != "" {
		opts = append(opts, option.WithBaseURL(taban))
	}
	return &SDKIstemci{c: anthropic.NewClient(opts...)}
}

func (s *SDKIstemci) Gonder(ctx context.Context, ist ClaudeIstek) (*ClaudeYanit, error) {
	msg, err := s.c.Messages.New(ctx,
		anthropic.MessageNewParams{Model: anthropic.Model(ist.Model), MaxTokens: ist.MaxTokens},
		option.WithRequestBody("application/json", ist.Govde),
		option.WithRequestTimeout(120*time.Second),
	)
	if err != nil {
		var apierr *anthropic.Error
		if errors.As(err, &apierr) {
			// Gövde/anahtar log'a gitmez: yalnız durum kodu.
			return nil, &claudeHatasi{Durum: apierr.StatusCode}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &claudeHatasi{Durum: 0}
	}
	var y ClaudeYanit
	if err := json.Unmarshal([]byte(msg.RawJSON()), &y); err != nil {
		return nil, fmt.Errorf("claude yanıtı çözülemedi: %w", err)
	}
	return &y, nil
}

type claudeHatasi struct{ Durum int }

func (e *claudeHatasi) Error() string {
	if e.Durum == 0 {
		return "claude: bağlantı hatası"
	}
	return fmt.Sprintf("claude: HTTP %d", e.Durum)
}

// ---- istek gövdesi ----

// apiMesaj: konuşma geçmişindeki tek mesaj; içerik blokları kanonik (deterministik) JSON.
type apiMesaj struct {
	Rol    string
	Icerik []json.RawMessage
}

// jsonDizge: HTML kaçışsız, deterministik JSON dizgesi.
func jsonDizge(s string) []byte {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(s)
	return bytes.TrimRight(b.Bytes(), "\n")
}

func metinBlok(s string) json.RawMessage {
	var b bytes.Buffer
	b.WriteString(`{"type":"text","text":`)
	b.Write(jsonDizge(s))
	b.WriteByte('}')
	return b.Bytes()
}

func aracSonucBlok(id, icerik string, hata bool) json.RawMessage {
	var b bytes.Buffer
	b.WriteString(`{"type":"tool_result","tool_use_id":`)
	b.Write(jsonDizge(id))
	b.WriteString(`,"content":`)
	b.Write(jsonDizge(icerik))
	if hata {
		b.WriteString(`,"is_error":true`)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// hamBlok: yanıt içerik bloğunun merkezin okuduğu alanları.
type hamBlok struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Thinking  string          `json:"thinking"`
	Signature string          `json:"signature"`
	Data      string          `json:"data"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// asistanBloklari: yanıt bloklarını girdi biçimine normalleştirir (yalnız API'nin
// girdi olarak kabul ettiği alanlar, sabit sırayla). Boş metin blokları atılır.
func asistanBloklari(icerik []json.RawMessage) []json.RawMessage {
	var out []json.RawMessage
	for _, ham := range icerik {
		var h hamBlok
		if err := json.Unmarshal(ham, &h); err != nil {
			continue
		}
		var b bytes.Buffer
		switch h.Type {
		case "text":
			if strings.TrimSpace(h.Text) == "" {
				continue
			}
			out = append(out, metinBlok(h.Text))
			continue
		case "tool_use":
			girdi := bytes.TrimSpace(h.Input)
			var c bytes.Buffer
			if len(girdi) == 0 || json.Compact(&c, girdi) != nil {
				c.Reset()
				c.WriteString("{}")
			}
			b.WriteString(`{"type":"tool_use","id":`)
			b.Write(jsonDizge(h.ID))
			b.WriteString(`,"name":`)
			b.Write(jsonDizge(h.Name))
			b.WriteString(`,"input":`)
			b.Write(c.Bytes())
			b.WriteByte('}')
		case "thinking":
			b.WriteString(`{"type":"thinking","thinking":`)
			b.Write(jsonDizge(h.Thinking))
			b.WriteString(`,"signature":`)
			b.Write(jsonDizge(h.Signature))
			b.WriteByte('}')
		case "redacted_thinking":
			b.WriteString(`{"type":"redacted_thinking","data":`)
			b.Write(jsonDizge(h.Data))
			b.WriteByte('}')
		default:
			continue
		}
		out = append(out, b.Bytes())
	}
	return out
}

// metinTopla: yanıttaki metin bloklarının birleşimi.
func metinTopla(icerik []json.RawMessage) string {
	var parca []string
	for _, ham := range icerik {
		var h hamBlok
		if json.Unmarshal(ham, &h) == nil && h.Type == "text" && strings.TrimSpace(h.Text) != "" {
			parca = append(parca, strings.TrimSpace(h.Text))
		}
	}
	return strings.Join(parca, "\n\n")
}

// cacheIsaretiEkle: nesne bloğunun sonuna cache_control ekler (saklanan geçmiş DEĞİŞMEZ).
func cacheIsaretiEkle(blok json.RawMessage, ttl string) []byte {
	b := bytes.TrimSpace(blok)
	if len(b) < 2 || b[len(b)-1] != '}' {
		return b
	}
	out := make([]byte, 0, len(b)+48)
	out = append(out, b[:len(b)-1]...)
	out = append(out, `,"cache_control":`...)
	out = append(out, cacheKontrol(ttl)...)
	out = append(out, '}')
	return out
}

func cacheKontrol(ttl string) string {
	if ttl == "1h" {
		return `{"type":"ephemeral","ttl":"1h"}`
	}
	return `{"type":"ephemeral"}`
}

// istekSecenek: model çağrısı başına değişen alanlar.
type istekSecenek struct {
	Model      string
	MaxTokens  int64
	ToolChoice string // ham JSON; boş = auto
	Dusunme    bool
	Efor       string
	CacheTTL   string
}

// anaIstekKur: ana/yükseltme modeli gövdesi.
//
// Prompt cache yerleşimi (sıra tools → system → messages):
//
//	[1] tools  = protokol.ToolsJSON() BAYT-AYNEN (tüm oturum ve rollerde aynı)
//	[2] system = tek blok SistemSabit() + cache_control  → kırılım 1: tools+system
//	[3] messages: ilk user mesajının İLK bloğu OturumBlogu (oturuma özgü), sonra
//	    konuşma; SON mesajın SON bloğu cache_control → kırılım 2 (her istekte
//	    ileri kayar; önceki istek(ler)in yazdığı önek okunur).
//
// tools ve system'den önce/ arasında oturuma özgü HİÇBİR bayt yoktur; model,
// max_tokens, tool_choice gibi alanlar önek bloklarının içeriğine girmez.
func anaIstekKur(s istekSecenek, mesajlar []apiMesaj) []byte {
	var b bytes.Buffer
	b.WriteString(`{"tools":`)
	b.Write(protokol.ToolsJSON())
	b.WriteString(`,"system":[{"type":"text","text":`)
	b.Write(jsonDizge(protokol.SistemSabit()))
	b.WriteString(`,"cache_control":`)
	b.WriteString(cacheKontrol(s.CacheTTL))
	b.WriteString(`}]`)
	ortakAlanlar(&b, s)
	mesajlariYaz(&b, mesajlar, s.CacheTTL, true)
	b.WriteByte('}')
	return b.Bytes()
}

func ortakAlanlar(b *bytes.Buffer, s istekSecenek) {
	b.WriteString(`,"model":`)
	b.Write(jsonDizge(s.Model))
	fmt.Fprintf(b, `,"max_tokens":%d`, s.MaxTokens)
	if s.ToolChoice != "" {
		b.WriteString(`,"tool_choice":`)
		b.WriteString(s.ToolChoice)
	}
	if s.Dusunme {
		b.WriteString(`,"thinking":{"type":"adaptive"}`)
	}
	if s.Efor != "" {
		b.WriteString(`,"output_config":{"effort":`)
		b.Write(jsonDizge(s.Efor))
		b.WriteString(`}`)
	}
}

func mesajlariYaz(b *bytes.Buffer, mesajlar []apiMesaj, ttl string, isaret bool) {
	b.WriteString(`,"messages":[`)
	for i, m := range mesajlar {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"role":`)
		b.Write(jsonDizge(m.Rol))
		b.WriteString(`,"content":[`)
		for j, blok := range m.Icerik {
			if j > 0 {
				b.WriteByte(',')
			}
			if isaret && i == len(mesajlar)-1 && j == len(m.Icerik)-1 {
				b.Write(cacheIsaretiEkle(blok, ttl))
			} else {
				b.Write(blok)
			}
		}
		b.WriteString(`]}`)
	}
	b.WriteByte(']')
}

// tekAracIstekKur: triyaj / çıkış denetimi — tek zorunlu araç, kısa sabit system.
// (Haiku'nun en küçük önbelleklenebilir öneki bu istemlerden büyük olduğundan
// cache_control konmaz: yazma primi ödenmez, okuma da olmaz.)
func tekAracIstekKur(model string, maxTok int64, sistem, aracJSON, aracAdi string, mesajlar []apiMesaj) []byte {
	var b bytes.Buffer
	b.WriteString(`{"tools":[`)
	b.WriteString(aracJSON)
	b.WriteString(`],"system":[{"type":"text","text":`)
	b.Write(jsonDizge(sistem))
	b.WriteString(`}]`)
	ortakAlanlar(&b, istekSecenek{Model: model, MaxTokens: maxTok, ToolChoice: `{"type":"tool","name":` + string(jsonDizge(aracAdi)) + `}`})
	mesajlariYaz(&b, mesajlar, "", false)
	b.WriteByte('}')
	return b.Bytes()
}
