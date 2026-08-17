package panelhost

// Hostname değişimi: girginospanel-panelhost bash betiğini çağırır.
// Betik kilitlenme koruması + DNS check + auto-rollback dahil her şeyi
// yapıyor; biz sadece süreci çalıştırıp çıktısını iş adımlarına yazarız.

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"runtime/debug"
	"strings"
	"time"
)

// AyarlaAsync — arka planda hostname değişimi. is.Durum "kosuyor" ile başlar.
// Aynı anda ikinci bir Ayarla çağrılırsa nil döner (handler 409 döner).
func AyarlaAsync(hostname string) *Is {
	if !TipKilitle("ayarla") {
		return nil
	}
	is := IsBaslat("ayarla", hostname)
	go func() {
		defer TipSerbest("ayarla")
		ayarlaKos(is, hostname)
	}()
	return is
}

func ayarlaKos(is *Is, hostname string) {
	defer func() {
		if r := recover(); r != nil {
			// Suggestion #4: panik stack log'a düşsün — sadece "panik: xxx"
			// mesajı yetmez, kök nedeni bulmak için stack şart.
			log.Printf("panelhost.ayarlaKos PANIC: %v\n%s", r, debug.Stack())
			is.Bitir(fmt.Sprintf("panik: %v", r))
		}
	}()

	// 1) Ön koşul: betik yüklü mü?
	if !BetikVarMi() {
		is.AdimEkle("girginospanel-panelhost betiği bulunamadı", false)
		is.Bitir("betik yok: /usr/local/bin/girginospanel-panelhost")
		return
	}
	is.AdimEkle("girginospanel-panelhost betiği bulundu", true)

	// 2) Betik zaten dry-run DNS check yapıyor — yanlış hostname'de kaydetmez.
	// Tek çalıştırma: `girginospanel-panelhost ayarla <hostname>`
	ctx, iptal := context.WithTimeout(context.Background(), 60*time.Second)
	defer iptal()

	cmd := exec.CommandContext(ctx, PanelHostBetik, "ayarla", hostname)
	out, err := cmd.CombinedOutput()

	// Çıktıyı satır satır adım kayıtlarına ekle — kullanıcı ne olduğunu görsün.
	for _, ln := range strings.Split(string(out), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		// Betik ANSI renk kodu kullanıyor — çıktıda kalabilir; UI'da yalın
		// görünmesi için basit strip.
		ln = ansiTemizle(ln)
		basari := !strings.HasPrefix(ln, "✗") && !strings.Contains(strings.ToLower(ln), "başarısız") && !strings.Contains(strings.ToLower(ln), "hata")
		is.AdimEkle(ln, basari)
	}

	if err != nil {
		is.Bitir("betik hata verdi: " + err.Error())
		return
	}
	is.Bitir("")
}

// ansiTemizle — ESC[...m renk dizilerini kaldır. Kaba ama yeter.
func ansiTemizle(s string) string {
	out := make([]byte, 0, len(s))
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			// ESC[ ile başladı, 'm'e kadar atla
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			i = j + 1
			continue
		}
		out = append(out, s[i])
		i++
	}
	return string(out)
}
