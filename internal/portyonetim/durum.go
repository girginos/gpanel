package portyonetim

// Panelin mevcut backend + dış portlarını okur.
//
// Backend port: /etc/girginospanel/env içinde PANEL_LISTEN=127.0.0.1:8080
// Dış port: /etc/nginx/conf.d/_panel.conf içinde "listen X ssl"

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"girginospanel/internal/kapi"
)

const (
	EnvYolu           = "/etc/girginospanel/env"
	VhostYolu         = "/etc/nginx/conf.d/_panel.conf"
	VhostCatchallYolu = "/etc/nginx/conf.d/_panel_catchall.conf"
)

type Durum struct {
	BackendPort int    `json:"backend_port"` // 8080
	DisPort     int    `json:"dis_port"`     // 8443 (kapı modunda: genel/müşteri kapısı)
	Uygulama    string `json:"uygulama"`     // panel binary path
	AktifIs     *Is    `json:"aktif_is,omitempty"`
	// Kapi: rol kapıları (ADR-GP-001) yapılandırılmışsa dolu. Kapı modunda dış port
	// tek başına değiştirilemez; rol portları ayrı yönetilir.
	Kapi *KapiDurum `json:"kapi,omitempty"`
}

// BackendPortOku — /etc/girginospanel/env içinden PANEL_LISTEN'i parse et.
// 127.0.0.1:8080 → 8080, :8080 → 8080. Bulamazsa 8080 (varsayılan).
func BackendPortOku() int {
	f, err := os.Open(EnvYolu)
	if err != nil {
		return 8080
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(l, "PANEL_LISTEN=") {
			continue
		}
		v := strings.TrimPrefix(l, "PANEL_LISTEN=")
		v = strings.TrimSpace(v)
		if i := strings.LastIndex(v, ":"); i >= 0 {
			if p, e := strconv.Atoi(v[i+1:]); e == nil && p > 0 {
				return p
			}
		}
	}
	return 8080
}

// DisPortOku — panelin herkese açık dış portu (vhost'un genel listen'ı; kapı modunda
// müşteri kapısı — admin/bayi portu ASLA). Uygulama subdomain vhost'ları ve {panelport}
// şablonları bunu kullanır. Bulunamazsa 8443.
func DisPortOku() int {
	if p := kapi.GenelPanelPortuOku(VhostYolu); p > 0 {
		return p
	}
	return 8443
}

// DurumGetir — snapshot.
func DurumGetir() Durum {
	return Durum{
		BackendPort: BackendPortOku(),
		DisPort:     DisPortOku(),
		Uygulama:    "/opt/girginospanel/bin/girginospanel-server",
		AktifIs:     isSnapshot(),
		Kapi:        KapiDurumGetir(),
	}
}
