package merkez

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Telegram Bot API istemcisi (yalnız stdlib).

// Dugme: satır içi klavye düğmesi (callback_data ≤ 64 bayt).
type Dugme struct {
	Metin string
	Veri  string
}

// GidenMesaj: sendMessage.
type GidenMesaj struct {
	ChatID   int64
	Metin    string
	HTML     bool // parse_mode=HTML; dinamik parçalar htmlKacis ile kaçırılmış OLMALI
	Dugmeler [][]Dugme
}

// TelegramIstemci: testte sahte.
type TelegramIstemci interface {
	MesajGonder(ctx context.Context, m GidenMesaj) (int64, error)
	MesajDuzenle(ctx context.Context, chatID, mesajID int64, metin string, html bool) error
	EylemGonder(ctx context.Context, chatID int64, eylem string) error
	CallbackYanitla(ctx context.Context, id, metin string) error
}

// TelegramHTTP: gerçek istemci. Token yalnız URL yolunda; hata metinlerine URL KONMAZ.
type TelegramHTTP struct {
	token string
	taban string
	hc    *http.Client
}

func YeniTelegramHTTP(token, taban string) *TelegramHTTP {
	if taban == "" {
		taban = "https://api.telegram.org"
	}
	return &TelegramHTTP{token: token, taban: strings.TrimRight(taban, "/"), hc: &http.Client{Timeout: 20 * time.Second}}
}

type tgYanit struct {
	Ok          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
	ErrorCode   int             `json:"error_code"`
}

// TelegramHatasi: API hatası (açıklama Telegram'dan; token içermez).
type TelegramHatasi struct {
	Metot    string
	Kod      int
	Aciklama string
}

func (e *TelegramHatasi) Error() string {
	return fmt.Sprintf("telegram %s: %d %s", e.Metot, e.Kod, e.Aciklama)
}

func (t *TelegramHTTP) cagir(ctx context.Context, metot string, govde any, sonuc any) error {
	b, err := json.Marshal(govde)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.taban+"/bot"+t.token+"/"+metot, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("telegram %s: istek kurulamadı", metot)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.hc.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // url.Error URL'yi (token'ı) taşır — yalnız iç hata
		}
		return fmt.Errorf("telegram %s: %v", metot, err)
	}
	defer resp.Body.Close()
	rb, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("telegram %s: yanıt okunamadı", metot)
	}
	var y tgYanit
	if err := json.Unmarshal(rb, &y); err != nil {
		return fmt.Errorf("telegram %s: HTTP %d, yanıt çözülemedi", metot, resp.StatusCode)
	}
	if !y.Ok {
		return &TelegramHatasi{Metot: metot, Kod: y.ErrorCode, Aciklama: strings.ReplaceAll(y.Description, t.token, "***")}
	}
	if sonuc != nil {
		return json.Unmarshal(y.Result, sonuc)
	}
	return nil
}

type tgDugme struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

func klavye(d [][]Dugme) map[string]any {
	satirlar := make([][]tgDugme, 0, len(d))
	for _, s := range d {
		var r []tgDugme
		for _, x := range s {
			r = append(r, tgDugme{Text: x.Metin, CallbackData: x.Veri})
		}
		satirlar = append(satirlar, r)
	}
	return map[string]any{"inline_keyboard": satirlar}
}

func (t *TelegramHTTP) MesajGonder(ctx context.Context, m GidenMesaj) (int64, error) {
	g := map[string]any{
		"chat_id":              m.ChatID,
		"text":                 m.Metin,
		"link_preview_options": map[string]any{"is_disabled": true},
	}
	if m.HTML {
		g["parse_mode"] = "HTML"
	}
	if len(m.Dugmeler) > 0 {
		g["reply_markup"] = klavye(m.Dugmeler)
	}
	var r struct {
		MessageID int64 `json:"message_id"`
	}
	if err := t.cagir(ctx, "sendMessage", g, &r); err != nil {
		return 0, err
	}
	return r.MessageID, nil
}

func (t *TelegramHTTP) MesajDuzenle(ctx context.Context, chatID, mesajID int64, metin string, html bool) error {
	g := map[string]any{
		"chat_id": chatID, "message_id": mesajID, "text": metin,
		"link_preview_options": map[string]any{"is_disabled": true},
	}
	if html {
		g["parse_mode"] = "HTML"
	}
	return t.cagir(ctx, "editMessageText", g, nil)
}

func (t *TelegramHTTP) EylemGonder(ctx context.Context, chatID int64, eylem string) error {
	return t.cagir(ctx, "sendChatAction", map[string]any{"chat_id": chatID, "action": eylem}, nil)
}

func (t *TelegramHTTP) CallbackYanitla(ctx context.Context, id, metin string) error {
	g := map[string]any{"callback_query_id": id}
	if metin != "" {
		g["text"] = metin
	}
	return t.cagir(ctx, "answerCallbackQuery", g, nil)
}

// WebhookKur: setWebhook (yalnız özel sohbet mesajları ve düğmeler; gizli başlık zorunlu).
func (t *TelegramHTTP) WebhookKur(ctx context.Context, hedef, gizli string, bekleyenleriSil bool) error {
	u, err := url.Parse(hedef)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("webhook URL'si https olmalı")
	}
	return t.cagir(ctx, "setWebhook", map[string]any{
		"url":                  hedef,
		"secret_token":         gizli,
		"allowed_updates":      []string{"message", "callback_query"},
		"drop_pending_updates": bekleyenleriSil,
		"max_connections":      20,
	}, nil)
}

// ---- gelen güncellemeler ----

type TgKullanici struct {
	ID           int64  `json:"id"`
	IsBot        bool   `json:"is_bot"`
	Username     string `json:"username"`
	LanguageCode string `json:"language_code"`
}

type TgSohbet struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type TgMesaj struct {
	MessageID int64        `json:"message_id"`
	From      *TgKullanici `json:"from"`
	Chat      TgSohbet     `json:"chat"`
	Text      string       `json:"text"`
}

type TgCallback struct {
	ID      string      `json:"id"`
	From    TgKullanici `json:"from"`
	Message *TgMesaj    `json:"message"`
	Data    string      `json:"data"`
}

type TgGuncelleme struct {
	UpdateID      int64       `json:"update_id"`
	Message       *TgMesaj    `json:"message"`
	CallbackQuery *TgCallback `json:"callback_query"`
}

// htmlKacis: parse_mode=HTML için dinamik metin kaçışı.
func htmlKacis(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// parcala: Telegram 4096 karakter sınırı için rune sınırında böler (satır sonunu tercih eder).
func parcala(s string, n int) []string {
	r := []rune(s)
	var out []string
	for len(r) > n {
		k := n
		for i := n; i > n/2; i-- {
			if r[i-1] == '\n' {
				k = i
				break
			}
		}
		out = append(out, string(r[:k]))
		r = r[k:]
	}
	if len(r) > 0 || len(out) == 0 {
		out = append(out, string(r))
	}
	return out
}
