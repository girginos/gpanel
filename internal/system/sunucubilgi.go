package system

// sunucubilgi.go — pano "Sunucu bilgileri" kartının veri yardımcıları.
// Kart yeni bir uç AÇMAZ: kök panosu zaten AdminOnly GET /system/usage'ı okur ve
// SystemInfo (hostname/ip/os_adi/kernel/mimari/panel_surum) orada döner. Burada yalnız
// ayrıştırma/normalleştirme saf fonksiyonları vardır (kabuk YOK, sır/yol/ortam YOK).

import "strings"

// osAdiAyikla: /etc/os-release içeriğinden PRETTY_NAME (yoksa NAME [+ VERSION_ID]).
// Tırnaksız/tek/çift tırnaklı, CRLF, boş değer, yorum satırı ve yinelenen anahtar
// (ilk geçerli kazanır) güvenle ele alınır.
func osAdiAyikla(icerik string) string {
	var ad, surum string
	for _, ln := range strings.Split(icerik, "\n") {
		ln = strings.TrimSpace(strings.TrimSuffix(ln, "\r"))
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		k, v, ok := strings.Cut(ln, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		switch strings.TrimSpace(k) {
		case "PRETTY_NAME":
			return v
		case "NAME":
			if ad == "" {
				ad = v
			}
		case "VERSION_ID":
			if surum == "" {
				surum = v
			}
		}
	}
	if ad != "" && surum != "" {
		return ad + " " + surum
	}
	return ad
}

// mimariAdi: uname -m / GOARCH → uname biçimi (x86_64, aarch64 …).
func mimariAdi(m string) string {
	m = strings.ToLower(strings.TrimSpace(m))
	switch m {
	case "amd64", "x86_64", "x64":
		return "x86_64"
	case "arm64", "aarch64":
		return "aarch64"
	case "386", "i386", "i486", "i586", "i686", "x86":
		return "i686"
	}
	return m
}
