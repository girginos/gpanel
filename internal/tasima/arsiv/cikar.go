package arsiv

import (
	"archive/tar"
	"bufio"
	"context"
	"fmt"
	"girginospanel/internal/selinuxetiket"
	"io"
	"time"

	"golang.org/x/sys/unix"

	"girginospanel/internal/evalti"
	"girginospanel/internal/kiraci"
)

// Hedef: çıkarmanın yapılacağı kiracı dizini.
type Hedef struct {
	SK  string // kiracı sistem kullanıcısı (c_...)
	Kok string // ev dizinine göre göreli dizin; "" → ev dizininin kendisi
	// Butce: seçimin kiracıda kaplayabileceği en çok bayt (çağıran: kiracının kalan disk
	// hakkı). Sıfır değer boş olmayan hiçbir seçime izin vermez; sınırsız plan için
	// ButceSinirsiz verilir (o zaman yalnız sunucunun boş alanı denetlenir).
	Butce int64
}

// ButceSinirsiz: Hedef.Butce için "kiracı disk hakkı sınırsız".
const ButceSinirsiz int64 = -1

// Atlanan: yazılmayan üye ve nedeni.
type Atlanan struct {
	Ad    string
	Neden string
}

const maxAtlananListe = 200

// Rapor: bir Cikar çağrısının sonucu.
type Rapor struct {
	Dosya, Dizin, Bag, Sabit int   // düzenli dosya, dizin, symlink, hardlink
	Bayt                     int64 // yazılan düzenli dosya baytı
	Atlanan                  []Atlanan
	AtlananSayisi            int
	// TarCikti: çıkarıcının stderr kuyruğu. Yalnız yönetici günlüğü içindir; kiracı
	// yollarını içerebilir, ham hâliyle bayi yanıtına konmaz.
	TarCikti string
}

func (r *Rapor) atla(ad, neden string) {
	r.AtlananSayisi++
	if len(r.Atlanan) < maxAtlananListe {
		r.Atlanan = append(r.Atlanan, Atlanan{Ad: ad, Neden: neden})
	}
}

// Cikar: onek altındaki üyeleri (adlar oneke göre göreli) kiracının h.Kok dizinine
// kiracı kimliğinde çıkarır. onek "" → arşivin tamamı.
func (a *Arsiv) Cikar(ctx context.Context, onek string, h Hedef) (*Rapor, error) {
	onek, err := adTemizle(onek)
	if err != nil {
		return nil, err
	}
	kok, err := adTemizle(h.Kok)
	if err != nil {
		return nil, fmt.Errorf("%w: hedef dizin", ErrGecersiz)
	}
	home, err := evalti.EvDizini(h.SK)
	if err != nil {
		return nil, fmt.Errorf("%w: kiracı", ErrGecersiz)
	}
	// Kiracı bütçesi sunucu boş alanından ÖNCE denetlenir. secim bildirilen boyutlardan
	// (seyrek üyede mantıksal boyut) hesaplanır ve yazılacak baytın üst sınırıdır: akışa
	// giden her dosya tam bildirilen boyutta, seyrek olmayan biçimde yazılır. Bütçesi
	// yeten kiracı ErrDisk'e ancak sunucunun boş alanı kendi kalan hakkının altındaysa
	// ulaşır.
	bayt, uye := a.secim(onek)
	if h.Butce != ButceSinirsiz && bayt > h.Butce {
		return nil, ErrKota
	}
	uid, gid, err := evalti.Kimlik(h.SK)
	if err != nil {
		return nil, fmt.Errorf("%w: kiracı kimliği çözülemedi", ErrGecersiz)
	}
	if err := diskYeterli(home, bayt+a.s.DiskPayi, uye); err != nil {
		return nil, err
	}
	if err := evalti.MkdirHepsi(home, kok, uid, gid, 0o755); err != nil {
		return nil, fmt.Errorf("%w: hedef dizin hazırlanamadı (%v)", ErrGecersiz, err)
	}
	hedefYol := home
	if kok != "" {
		hedefYol += "/" + kok
	}
	// SK EvDizini'de doğrulandı; Kul testte ayrıcalıksız kimliğe çevrilebilir.
	r, err := a.cikarCalistir(ctx, onek, kiraci.Kiraci{Kul: evalti.OSKullanici(h.SK), Ev: home, Dilim: true, DilimSK: h.SK}, hedefYol, nil, false)
	// Çıkarma kiracı kimliğinde yapılır (yeni dosya ebeveynden etiket miras alır); yine de
	// hedef zaten yanlış etiketliyse (kalıntı) politika varsayılanına döndür.
	selinuxetiket.LoglaYol("arsiv cikar", hedefYol)
	return r, err
}

// cikarCalistir: süzülmüş akışı ki kimliğinde `tar -x -C hedefYol`a besler (Cikar ve
// CikarDizin ortak gövdesi). Hedefin hazırlığı ve denetimi çağıranındır.
func (a *Arsiv) cikarCalistir(ctx context.Context, onek string, ki kiraci.Kiraci, hedefYol string, izin func(rel string) bool, baglarYok bool) (*Rapor, error) {
	cmd, err := ki.Komut(ctx, "tar", "-x",
		"--no-same-owner", "--no-same-permissions", "-f", "-", "-C", hedefYol)
	if err != nil {
		return nil, err
	}
	girdi, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	kuyruk := &kuyrukYazici{}
	cmd.Stderr = kuyruk
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%w: çıkarıcı başlatılamadı", ErrCikarma)
	}
	r := &Rapor{}
	akErr := a.akit(ctx, onek, girdi, r, izin, baglarYok)
	_ = girdi.Close() // yarıda kesilen akışta tar eksik girdiyle hata verip çıkar
	werr := cmd.Wait()
	r.TarCikti = string(kuyruk.b)
	if akErr != nil {
		return r, akErr
	}
	if werr != nil {
		return r, fmt.Errorf("%w (%v)", ErrCikarma, werr)
	}
	return r, nil
}

// secim: onek altındaki üyelerin (onekin kendisi hariç) sayısı ve diskte kaplayacağı
// yaklaşık bayt (üye başına bir blok ek yük).
func (a *Arsiv) secim(onek string) (bayt int64, uye int) {
	const blok = 4096
	for _, u := range a.Uyeler {
		if rel, ok := altinda(u.Ad, onek); ok && rel != "" {
			uye++
			bayt += (u.Boyut/blok + 1) * blok
		}
	}
	return bayt, uye
}

// inodePayi: seçimin üye sayısının üstüne boş kalması gereken inode.
const inodePayi = 1000

// diskYeterli: hedef dosya sisteminde gerekli boş bayt ve inode var mı? fstatfs ev
// dizinine çapalı fd üzerinden yapılır. Hata metni sunucunun boş alanını içermez
// (sunucu geneli ölçü bayiye verilmez).
func diskYeterli(home string, gerekBayt int64, uye int) error {
	fd, err := evalti.Ac(home, "", unix.O_DIRECTORY|unix.O_RDONLY)
	if err != nil {
		return fmt.Errorf("%w: ev dizini açılamadı", ErrGecersiz)
	}
	defer unix.Close(fd)
	var st unix.Statfs_t
	if err := unix.Fstatfs(fd, &st); err != nil {
		return fmt.Errorf("%w: boş alan ölçülemedi", ErrDisk)
	}
	if bos := int64(st.Bavail) * st.Bsize; gerekBayt > bos {
		return ErrDisk
	}
	if st.Files > 0 && uint64(uye)+inodePayi > st.Ffree {
		return ErrDisk
	}
	return nil
}

// izliYazici: alttaki yazıcının ilk hatasını saklar (okuma hatası ile çıkarıcının
// kapanmasından doğan yazma hatasını ayırmak için).
type izliYazici struct {
	w   io.Writer
	err error
}

func (y *izliYazici) Write(p []byte) (int, error) {
	n, err := y.w.Write(p)
	if err != nil && y.err == nil {
		y.err = err
	}
	return n, err
}

// kuyrukYazici: son 4 KiB'ı tutar.
type kuyrukYazici struct{ b []byte }

func (k *kuyrukYazici) Write(p []byte) (int, error) {
	k.b = append(k.b, p...)
	if len(k.b) > 4096 {
		k.b = append([]byte(nil), k.b[len(k.b)-4096:]...)
	}
	return len(p), nil
}

// akit: arşivi baştan okur (üyeler envanterle birebir aynı olmalı) ve onek altındaki
// üyeleri süzüp w'ye YENİ bir tar akışı olarak yazar. Denetim ve yazım aynı geçiştedir.
func (a *Arsiv) akit(ctx context.Context, onek string, w io.Writer, r *Rapor, izin func(rel string) bool, baglarYok bool) error {
	g, err := a.gecisAc(ctx, a.Uyeler)
	if err != nil {
		return err
	}
	defer g.kapat()
	iy := &izliYazici{w: w}
	bw := bufio.NewWriterSize(iy, 256<<10)
	tw := tar.NewWriter(bw)
	s := &suzgec{onek: onek, baglar: bagAgaciKur(a.Uyeler, onek), dosya: map[string]bool{}, r: r, izin: izin, baglarYok: baglarYok}
	yazmaHatasi := func(err error) error {
		if iy.err != nil {
			return fmt.Errorf("%w (%v)", ErrCikarma, iy.err)
		}
		return g.hata(err)
	}
	for {
		u, hdr, err := g.sonraki()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		yeni := s.karar(u, hdr)
		if yeni == nil {
			continue
		}
		if err := tw.WriteHeader(yeni); err != nil {
			return yazmaHatasi(err)
		}
		if yeni.Typeflag == tar.TypeReg {
			if _, err := io.Copy(tw, g.veri); err != nil {
				return yazmaHatasi(err)
			}
		}
	}
	// Akış sonu (CRC, açıcı çıkış kodu) doğrulanmadan tar sonu yazılmaz: bozuk
	// arşivde çıkarıcı da eksik girdiyle hata verir.
	if err := g.bitir(); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return yazmaHatasi(err)
	}
	if err := bw.Flush(); err != nil {
		return yazmaHatasi(err)
	}
	return nil
}

// suzgec: tek bir çıkarma geçişinin yazma kararları.
type suzgec struct {
	onek   string
	baglar *bagAgaci       // seçimdeki TÜM symlink'ler (envanterden; sıradan bağımsız)
	dosya  map[string]bool // yazılan düzenli dosyalar (hardlink hedefi olabilir)
	r      *Rapor
	// izin: nil değilse yalnız izin(rel)=true üyeler yazılır (posta: yalnız bilinen kutular,
	// dovecot iç dosyaları hariç). false → "seçim dışı" olarak atlanır ve raporlanır.
	izin func(rel string) bool
	// baglarYok: symlink ve hardlink üyeler atlanır (bkz. DizinSecenek.BaglarYok).
	baglarYok bool
}

// karar: üye yazılacaksa çıkarıcıya gidecek temiz başlık, değilse nil (atlananlar
// rapora yazılır). Başlık sıfırdan kurulur: kip yalnız izin bitleri (setuid/setgid/
// sticky yok), sahiplik/ad/xattr/PAX kaydı taşınmaz; GNU biçimi ham bayt adları
// (UTF-8 olmayan eski dosya adları) olduğu gibi taşır.
func (s *suzgec) karar(u Uye, hdr *tar.Header) *tar.Header {
	rel, ok := altinda(u.Ad, s.onek)
	if !ok || rel == "" {
		return nil // seçim dışı ya da seçimin kök dizininin kendisi
	}
	if s.izin != nil && !s.izin(rel) {
		s.r.atla(rel, "izin verilmeyen yol (kutu/ad süzgeci)")
		return nil
	}
	if s.baglar.bagAltinda(rel) {
		s.r.atla(rel, "bağ üzerinden geçen yol")
		return nil
	}
	yeni := &tar.Header{
		Name:    rel,
		Mode:    hdr.Mode & 0o777,
		ModTime: time.Unix(hdr.ModTime.Unix(), 0),
		Format:  tar.FormatGNU,
	}
	if s.baglarYok && (u.Tur == tar.TypeSymlink || u.Tur == tar.TypeLink) {
		s.r.atla(rel, "bağ (bu hedefte bağ yazılmaz)")
		return nil
	}
	switch u.Tur {
	case tar.TypeDir:
		yeni.Typeflag = tar.TypeDir
		yeni.Name += "/"
		s.r.Dizin++
	case tar.TypeReg:
		yeni.Typeflag = tar.TypeReg
		yeni.Size = u.Boyut
		s.dosya[rel] = true
		s.r.Dosya++
		s.r.Bayt += u.Boyut
	case tar.TypeSymlink:
		if neden := s.baglar.bagNedeni(rel, u.Hedef); neden != "" {
			s.r.atla(rel, neden)
			return nil
		}
		yeni.Typeflag = tar.TypeSymlink
		yeni.Linkname = u.Hedef
		yeni.Mode = 0o777
		s.r.Bag++
	case tar.TypeLink:
		// Hedef normalize edilip seçime göre göreli ada çevrilir; yalnız bu geçişte daha
		// önce yazılmış düzenli dosya kabul edilir. Geçersiz ("..", mutlak) ya da seçim
		// dışı hedef "" olur ve hiçbir yazılmış dosyayla eşleşmez.
		h, _ := adTemizle(u.Hedef)
		hrel, _ := altinda(h, s.onek)
		if !s.dosya[hrel] {
			s.r.atla(rel, "seçim dışına ya da yazılmamış dosyaya sabit bağ")
			return nil
		}
		yeni.Typeflag = tar.TypeLink
		yeni.Linkname = hrel
		s.r.Sabit++
	case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
		s.r.atla(rel, "aygıt ya da fifo")
		return nil
	default:
		s.r.atla(rel, "desteklenmeyen üye türü")
		return nil
	}
	return yeni
}
