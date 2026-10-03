package arac

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"sort"
	"strings"
	"time"

	"girginospanel/internal/kiraci"
	"girginospanel/internal/monitor"
)

// ortam — dış dünya dikişleri (testler sahteyle değiştirir). Hepsi SABİT argv
// alır; çağıranlar yalnız katalog enum'larından ya da DB'den gelen, biçimi
// denetlenmiş değerleri koyar.
type ortam struct {
	// komut — kök kimliğinde argv (kabuk yok); stdout+stderr birleşik, sınırlı.
	komut func(ctx context.Context, argv ...string) ([]byte, error)
	// kiraciKomut — kiracı kimliğinde (kiraci.Komut: runuser, panel ortamı yok); stdout.
	kiraciKomut func(ctx context.Context, sk string, argv ...string) ([]byte, error)
	// yokla — SSRF kapılı HTTP sağlık yoklaması (monitor.Yokla).
	yokla func(hedefURL string) monitor.DomainHealth
	// dig — tek DNS sorgusu: @sunucu ad tur → +short satırları.
	dig func(ctx context.Context, sunucu, ad, tur string) ([]string, error)
}

// ciktiSiniri — tek komut çıktısının üst sınırı (bayt); fazlası atılır.
const ciktiSiniri = 4 << 20

// sinirliTampon — ciktiSiniri'nı aşan yazımı sessizce düşüren tampon (komut
// takılmasın diye hata dönmez).
type sinirliTampon struct {
	b   bytes.Buffer
	sin int
}

func (t *sinirliTampon) Write(p []byte) (int, error) {
	if kalan := t.sin - t.b.Len(); kalan > 0 {
		if len(p) > kalan {
			t.b.Write(p[:kalan])
		} else {
			t.b.Write(p)
		}
	}
	return len(p), nil
}

var kokOrtami = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}

func varsayilanOrtam() ortam {
	return ortam{
		komut: func(ctx context.Context, argv ...string) ([]byte, error) {
			if len(argv) == 0 || argv[0] == "" {
				return nil, errors.New("boş komut")
			}
			cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // G204: argv yalnız bu paketteki sabitlerden + biçim denetimli DB değerlerinden kurulur
			cmd.Env = kokOrtami
			cmd.Dir = "/"
			cmd.WaitDelay = 5 * time.Second
			t := &sinirliTampon{sin: ciktiSiniri}
			cmd.Stdout, cmd.Stderr = t, t
			err := cmd.Run()
			return t.b.Bytes(), err
		},
		kiraciKomut: func(ctx context.Context, sk string, argv ...string) ([]byte, error) {
			if !kiraci.GecerliSK(sk) {
				return nil, kiraci.ErrSK
			}
			cmd, err := kiraci.Kiraci{Kul: sk, Ev: kiraci.EvKoku + "/" + sk}.Komut(ctx, argv...)
			if err != nil {
				return nil, err
			}
			t := &sinirliTampon{sin: ciktiSiniri}
			cmd.Stdout = t
			cmd.Stderr = &sinirliTampon{sin: 4096}
			err = cmd.Run()
			return t.b.Bytes(), err
		},
		yokla: monitor.Yokla,
		dig: func(ctx context.Context, sunucu, ad, tur string) ([]string, error) {
			dctx, iptal := context.WithTimeout(ctx, 5*time.Second)
			defer iptal()
			cmd := exec.CommandContext(dctx, "dig", "+short", "+time=2", "+tries=1", "@"+sunucu, ad, tur) //nolint:gosec // G204: sunucu sabit listeden, ad biçim denetimli, tur katalog enum'u
			cmd.Env = kokOrtami
			t := &sinirliTampon{sin: 64 << 10}
			cmd.Stdout = t
			if err := cmd.Run(); err != nil {
				return nil, err
			}
			return digSatirlari(t.b.String()), nil
		},
	}
}

// digSatirlari — +short çıktısını karşılaştırılabilir kümeye çevirir: küçük harf,
// sondaki nokta yok, TXT tırnakları birleşik, sıralı, tekil. ";;" hata satırları atılır.
func digSatirlari(s string) []string {
	gor := map[string]bool{}
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, ";") {
			continue
		}
		ln = kayitNormal(ln)
		if !gor[ln] {
			gor[ln] = true
			out = append(out, ln)
		}
	}
	sort.Strings(out)
	return out
}

// kayitNormal — DNS kayıt değerinin karşılaştırma biçimi.
func kayitNormal(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, `"`) {
		// TXT: "a" "b" → ab
		var b strings.Builder
		ic := false
		for i := 0; i < len(s); i++ {
			ch := s[i]
			switch {
			case ch == '\\' && ic && i+1 < len(s):
				i++
				b.WriteByte(s[i])
			case ch == '"':
				ic = !ic
			case ic:
				b.WriteByte(ch)
			}
		}
		return b.String()
	}
	alan := strings.Fields(strings.ToLower(s))
	for i, a := range alan {
		alan[i] = strings.TrimSuffix(a, ".")
	}
	return strings.Join(alan, " ")
}
