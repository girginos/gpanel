package arsiv

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
)

// DumpAc: envanterde TEK KEZ geçen düzenli dosya üyesinin (veritabanı dökümü) içeriğini
// akış olarak döner; diske yazılmaz. Bildirilen boyut max'ı aşarsa ErrSinir (çağıran
// kendi döküm sınırını verir). Okuyucu, üyenin bildirilen boyutundan fazlasını vermez;
// Close ile kapatılmalıdır.
func (a *Arsiv) DumpAc(ctx context.Context, ad string, max int64) (io.ReadCloser, error) {
	ad, err := adTemizle(ad)
	i, ok := a.indeks[ad]
	if err != nil || !ok || a.Uyeler[i].Tur != tar.TypeReg {
		return nil, ErrYok
	}
	if a.Uyeler[i].Boyut > max {
		return nil, fmt.Errorf("%w: döküm boyutu", ErrSinir)
	}
	g, err := a.gecisAc(ctx, a.Uyeler)
	if err != nil {
		return nil, err
	}
	// i. üyeye kadar ilerle; arşiv erken biterse sonraki() envanterle karşılaştırıp
	// ErrDegisti döner.
	for g.n <= i {
		if _, _, err := g.sonraki(); err != nil {
			g.kapat()
			return nil, err
		}
	}
	return &dumpOkuyucu{g: g}, nil
}

type dumpOkuyucu struct{ g *gecis }

func (d *dumpOkuyucu) Read(p []byte) (int, error) {
	n, err := d.g.tr.Read(p)
	if err != nil && err != io.EOF {
		err = d.g.hata(err)
	}
	return n, err
}

func (d *dumpOkuyucu) Close() error {
	d.g.kapat()
	return nil
}
