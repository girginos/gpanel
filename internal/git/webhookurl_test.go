package git

import "testing"

// Payload URL sunucuda üretilir: HTTPS + panelin genel portu. Arayüzün eski sabiti
// (`http://<ip>:8443`) HTTPS portuna düz HTTP'ydi — nginx 400 döndürüyordu.
func TestWebhookURL(t *testing.T) {
	h := &Handlers{WebhookBase: "https://203.0.113.10:23003/"}
	if got := h.webhookURL("abc"); got != "https://203.0.113.10:23003/api/v1/git-webhook/abc" {
		t.Fatalf("got %q", got)
	}
	if (&Handlers{}).webhookURL("abc") != "" || h.webhookURL("") != "" {
		t.Fatal("taban ya da gizli yoksa adres üretilmemeli")
	}
}
