// Package arsiv: taşıma kaynağından çekilen hesap arşivlerini (cPanel cpmove / Full
// Backup, DirectAdmin user.*.tar.gz|.tar.zst) GÜVENİLMEZ girdi olarak okur.
//
// Güvenlik modeli:
//   - Arşiv yalnız Go archive/tar ile okunur. Çıkarma aracı (kiracı kimliğinde GNU tar)
//     ham arşivi GÖRMEZ; bu paketin üye üye denetleyip YENİDEN yazdığı süzülmüş akışı
//     stdin'den alır. Denetim ve yazım aynı geçişte yapılır → denetleyen ile çıkaran
//     arasında ayrıştırıcı farkı (GNU uzun ad, PAX, xattr, sahiplik, aygıt) kalmaz.
//   - Üye adı mutlak / ".." / ters bölü / 4096+ bayt / 64+ bileşen → arşiv reddedilir.
//   - Aynı ad iki kez (ikisi de dizin değilse) → ret: tar'da sonraki öncekini ezer;
//     envanterin okuduğu üst veri ile çıkarılan içerik ayrışamasın.
//   - Çıkarmada dizin + düzenli dosya yazılır; symlink yalnız göreli, seçili kökün
//     içinde kalan ve seçimdeki hiçbir bağdan geçmeyen hedefle (sıradan bağımsız; bağ
//     zinciri sözcüksel denetimi aşamaz), hardlink yalnız seçimde daha önce yazılmış
//     düzenli dosyaya; seçimdeki bir bağın altından geçen yol, aygıt, fifo ve tanınmayan
//     tür ATLANIR ve raporlanır. setuid/setgid/sticky silinir; uid/gid/kullanıcı adı/
//     xattr yok sayılır. Çıkarılan bağlar kiracıya aittir ve ev içinde kalabilir: bu
//     ağaçta root olarak çalışan tüketici yolu evalti.Ac ile (RESOLVE_NO_SYMLINKS) açar;
//     os.OpenRoot ev içindeki bağları izlediği için yetmez.
//   - Sınırlar: bildirilen toplam boyut, üye sayısı, ad baytı, belleğe alınan üst veri
//     (≤ 4 MiB), döküm (çağıranın sınırı), arşiv dosyasına göre açılmış akış VE üye
//     verisi oranı (seyrek üye), kiracı bütçesi (Hedef.Butce), hedefte boş disk + inode.
//   - gzip/bzip2 süreç içinde (Go); xz/zstd alt süreçte, panel root ise nobody
//     kimliğinde, bellek sınırlı, yalnız stdin → stdout.
//   - Yazma: `runuser -u <sk> -- tar -x --no-same-owner --no-same-permissions -f - -C
//     <kök>`; kök ev dizinine çapalı, symlink izlenmeden hazırlanır (evalti).
//
// Paket tasima'yı import etmez (tasima → arsiv tek yönlü).
package arsiv

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
)

var (
	ErrUyeAdi   = errors.New("arşiv: geçersiz üye adı")
	ErrTekrar   = errors.New("arşiv: aynı üye birden çok kez geçiyor")
	ErrSinir    = errors.New("arşiv: boyut ya da üye sınırı aşıldı")
	ErrBomba    = errors.New("arşiv: sıkıştırma oranı sınırı aşıldı (olası sıkıştırma bombası)")
	ErrUstVeri  = errors.New("arşiv: üst veri sınırı aşıldı")
	ErrBicim    = errors.New("arşiv: tanınmayan ya da bozuk arşiv")
	ErrAcici    = errors.New("arşiv: açıcı sunucuda kurulu değil")
	ErrDegisti  = errors.New("arşiv: okuma sırasında içerik değişti")
	ErrYok      = errors.New("arşiv: üye bulunamadı ya da düzenli dosya değil")
	ErrDisk     = errors.New("arşiv: hedefte yeterli boş alan yok")
	ErrKota     = errors.New("arşiv: seçim kiracının kalan disk hakkını aşıyor")
	ErrCikarma  = errors.New("arşiv: çıkarma başarısız")
	ErrGecersiz = errors.New("arşiv: geçersiz hedef")
)

// Sinirlar: sıfır alanlar varsayılanla doldurulur.
type Sinirlar struct {
	ToplamBayt  int64 // tüm üyelerin bildirilen açılmış boyutu toplamı
	UyeSayisi   int   // üye sayısı
	AdBayt      int64 // envanterde tutulan ad + bağ hedefi baytı (başlık bombası)
	UstVeriBayt int64 // belleğe alınan üst veri toplamı (en çok 4 MiB)
	BombaOrani  int64 // (açılmış akış ya da üye verisi) / arşiv dosyası bayt oranı üst sınırı
	BombaEsik   int64 // oran denetiminin başladığı okunan bayt
	DiskPayi    int64 // Cikar: seçimin üstüne hedefte boş kalması gereken bayt
}

const maxUstVeri = 4 << 20

func (s Sinirlar) tamamla() Sinirlar {
	if s.ToplamBayt <= 0 {
		s.ToplamBayt = 64 << 30
	}
	if s.UyeSayisi <= 0 {
		s.UyeSayisi = 500_000
	}
	if s.AdBayt <= 0 {
		s.AdBayt = 64 << 20
	}
	if s.UstVeriBayt <= 0 || s.UstVeriBayt > maxUstVeri {
		s.UstVeriBayt = maxUstVeri
	}
	if s.BombaOrani <= 0 {
		s.BombaOrani = 500
	}
	if s.BombaEsik <= 0 {
		s.BombaEsik = 512 << 20
	}
	if s.DiskPayi <= 0 {
		s.DiskPayi = 512 << 20
	}
	return s
}

// Uye: envanterdeki bir arşiv üyesi.
type Uye struct {
	Ad    string // normalize ad ("./" ve sondaki "/" yok); "" = arşiv kökü
	Tur   byte   // tar Typeflag (tar.TypeReg, tar.TypeDir, tar.TypeSymlink, ...)
	Boyut int64  // bildirilen açılmış boyut
	Hedef string // symlink/hardlink hedefi (ham)
}

// Arsiv: envanteri çıkarılmış arşiv. Sonraki her okuma (Cikar, DumpAc) arşivi yeniden
// baştan okur ve üyelerin envanterle birebir aynı olduğunu doğrular.
type Arsiv struct {
	yol     string
	s       Sinirlar
	indeks  map[string]int // ad → Uyeler sırası (kök hariç)
	Uyeler  []Uye
	UstVeri map[string][]byte // ustVeriMi'nin seçtiği düzenli dosyaların içeriği
}

// Ac: arşivi tek geçişte okuyup envanterini çıkarır. ustVeriMi(ad) true dönen düzenli
// dosyaların içeriği belleğe alınır (toplam en çok Sinirlar.UstVeriBayt). Hiçbir şey
// diske yazılmaz.
func Ac(ctx context.Context, yol string, s Sinirlar, ustVeriMi func(ad string) bool) (*Arsiv, error) {
	a := &Arsiv{yol: yol, s: s.tamamla(), indeks: map[string]int{}, UstVeri: map[string][]byte{}}
	g, err := a.gecisAc(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer g.kapat()
	var adBayt, ustBayt int64
	for {
		u, _, err := g.sonraki()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if u.Ad == "" {
			if u.Tur != tar.TypeDir {
				return nil, adHatasi(".")
			}
			a.Uyeler = append(a.Uyeler, u)
			continue
		}
		adBayt += int64(len(u.Ad) + len(u.Hedef))
		if adBayt > a.s.AdBayt {
			return nil, fmt.Errorf("%w: üye adları", ErrSinir)
		}
		if j, onceden := a.indeks[u.Ad]; onceden {
			if u.Tur != tar.TypeDir || a.Uyeler[j].Tur != tar.TypeDir {
				return nil, fmt.Errorf("%w: %q", ErrTekrar, kisalt(u.Ad))
			}
		} else {
			a.indeks[u.Ad] = len(a.Uyeler)
		}
		a.Uyeler = append(a.Uyeler, u)
		if u.Tur == tar.TypeReg && ustVeriMi != nil && ustVeriMi(u.Ad) {
			ustBayt += u.Boyut
			if ustBayt > a.s.UstVeriBayt {
				return nil, ErrUstVeri
			}
			b := make([]byte, u.Boyut)
			if _, err := io.ReadFull(g.veri, b); err != nil {
				return nil, g.hata(err)
			}
			a.UstVeri[u.Ad] = b
		}
	}
	if err := g.bitir(); err != nil {
		return nil, err
	}
	return a, nil
}
