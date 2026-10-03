package toplu

// toplu_ip_test.go — toplu "ip" işi: hedef IP istemciden gelir; sunucunun sahip
// olmadığı (ya da geçersiz) IP iş kaydı AÇILMADAN 400 ile reddedilir. DB nil:
// red yolunda DB'ye dokunulmadığı da böylece kanıtlanır (dokunulsa panik).

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBaslat_IP_SunucudaOlmayanVeGecersiz_400(t *testing.T) {
	h := &Handlers{DB: nil}
	for _, govde := range []string{
		`{"tip":"ip","ids":[1,2],"ipv4":"192.0.2.77"}`, // TEST-NET-1: sunucuda yok
		`{"tip":"ip","ids":[1],"ipv4":"abc"}`,
		`{"tip":"ip","ids":[1],"ipv4":"2001:db8::1"}`, // v6 desteklenmez
		`{"tip":"ip","ids":[1]}`,
	} {
		rec := httptest.NewRecorder()
		h.Baslat(rec, httptest.NewRequest(http.MethodPost, "/domains/toplu/is", bytes.NewBufferString(govde)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s → %d beklenen 400 (%s)", govde, rec.Code, rec.Body.String())
		}
	}
	if aktifIs != 0 {
		t.Fatalf("reddedilen istek iş kilidini tutmamalı: %d", aktifIs)
	}
}
