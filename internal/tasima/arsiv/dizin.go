package arsiv

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"girginospanel/internal/kiraci"
)

// DizinSecenek: CikarDizin ayarları.
type DizinSecenek struct {
	// Butce: seçimin kaplayabileceği en çok bayt (ButceSinirsiz = yalnız sunucu boş alanı denetlenir).
	Butce int64
	// Izin: nil değilse yalnız Izin(rel)=true üyeler yazılır (rel: seçime göre göreli ad).
	Izin func(rel string) bool
	// BaglarYok: true → symlink ve hardlink üyeler HİÇ yazılmaz (posta kutusu: bir kutudan
	// başka kutuya bağ, kutular arası okumaya dönüşürdü).
	BaglarYok bool
}

// CikarDizin: onek altındaki üyeleri (adlar oneke göre göreli) KİRACI OLMAYAN bir
// hedefe (posta kutusu: /var/vmail/<alan>/<kutu>) `kul` kimliğinde çıkarır. Süzme,
// bağ/hardlink/aygıt/suid kuralları Cikar ile AYNI geçiştir (aynı akit); fark yalnız
// hedefin hazırlığıdır:
//   - hedef mutlak, temiz (filepath.Clean ile birebir), VAR olan ve sembolik bağ OLMAYAN
//     bir dizin olmalı (çağıran önceden yaratıp sahipliğini vermiştir; burada yaratılmaz);
//   - kul boş/"root" olamaz; kiraci.Komut uid 0'a çözülen kullanıcıyı da reddeder;
//   - butce (ButceSinirsiz dışında) seçimin boyutunu aşarsa ErrKota; hedefin dosya
//     sisteminde boş alan + inode denetlenir (ErrDisk).
func (a *Arsiv) CikarDizin(ctx context.Context, onek, hedef, kul string, s DizinSecenek) (*Rapor, error) {
	onek, err := adTemizle(onek)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(hedef) || filepath.Clean(hedef) != hedef || hedef == "/" || strings.ContainsRune(hedef, 0) {
		return nil, fmt.Errorf("%w: hedef dizin", ErrGecersiz)
	}
	if kul == "" || kul == "root" || strings.HasPrefix(kul, "-") {
		return nil, fmt.Errorf("%w: kullanıcı", ErrGecersiz)
	}
	st, err := os.Lstat(hedef)
	if err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%w: hedef dizin yok ya da dizin değil", ErrGecersiz)
	}
	bayt, uye := a.secim(onek)
	if s.Butce != ButceSinirsiz && bayt > s.Butce {
		return nil, ErrKota
	}
	if err := diskYeterli(hedef, bayt+a.s.DiskPayi, uye); err != nil {
		return nil, err
	}
	return a.cikarCalistir(ctx, onek, kiraci.Kiraci{Kul: kul, Ev: hedef}, hedef, s.Izin, s.BaglarYok)
}
