package merkez

import (
	"context"
	"testing"
)

// panikTG: ilk gönderimde panik eden Telegram istemcisi.
type panikTG struct {
	*sahteTG
	patladi bool
}

func (p *panikTG) MesajGonder(ctx context.Context, m GidenMesaj) (int64, error) {
	if !p.patladi {
		p.patladi = true
		panic("beklenmeyen nil")
	}
	return p.sahteTG.MesajGonder(ctx, m)
}

// Webhook goroutine'indeki panik tüm süreci düşürmemeli; sonraki güncellemeler işlenmeli.
func TestWebhook_GuncellemeIslemePanigiSurecleriDusurmez(t *testing.T) {
	o := yeniOrtam(t, nil)
	p := &panikTG{sahteTG: o.tg}
	o.m.tg = p
	o.mesaj(t, 4242, "/durum") // panik → kurtarılmalı
	o.mesaj(t, 4242, "/durum") // sonraki güncelleme normal yanıtlanır
	if len(o.tg.metinler(4242)) == 0 {
		t.Fatal("panikten sonra güncelleme işlenmedi")
	}
}
