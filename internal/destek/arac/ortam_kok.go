package arac

// ortam_kok.go — kiracı DIŞI (kök) komutlar: nginx -t, dogrula, dig vb.
// Kiracı komutlarıyla AYNI dosyada durmaz: kiraci.Komut kullanan dosyalarda
// ortam yalnız append ile genişletilebilir (internal/kiraci ortam_koruma_test).
// Burada ortam sıfırdan, sırsız asgari değerle kurulur — panel ortamı miras alınmaz.

import (
	"context"
	"errors"
	"os/exec"
	"time"
)

var kokOrtami = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}

func kokKomut(ctx context.Context, argv ...string) ([]byte, error) {
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
}

func kokDig(ctx context.Context, sunucu, ad, tur string) ([]string, error) {
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
}
