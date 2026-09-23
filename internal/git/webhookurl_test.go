package git

import "testing"

// Payload URL sunucuda üretilir: HTTPS + panelin genel portu. Arayüzün eski sabiti
// (`http://<ip>:8443`) HTTPS portuna düz HTTP'ydi — nginx 400 döndürüyordu.
func TestWebhookURL(t *testing.T) {
	taban := "https://203.0.113.10:23003/"
	h := &Handlers{WebhookBase: func() string { return taban }}
	if got := h.webhookURL("abc"); got != "https://203.0.113.10:23003/api/v1/git-webhook/abc" {
		t.Fatalf("got %q", got)
	}
	// Müşteri kapısı panelden değişti (yeniden başlatma yok): adres yeni portu izlemeli.
	taban = "https://203.0.113.10:24004"
	if got := h.webhookURL("abc"); got != "https://203.0.113.10:24004/api/v1/git-webhook/abc" {
		t.Fatalf("taban her çağrıda okunmalı, got %q", got)
	}
	if (&Handlers{}).webhookURL("abc") != "" || h.webhookURL("") != "" {
		t.Fatal("taban ya da gizli yoksa adres üretilmemeli")
	}
	if (&Handlers{WebhookBase: func() string { return "" }}).webhookURL("abc") != "" {
		t.Fatal("boş taban adres üretmemeli")
	}
}
