package tasima

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	stdlog "log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"girginospanel/internal/hesaplar"
)

type dbHedef struct{ Ad, Kul string }

// configtenDBKimlikOku / orijinalKimlikDene — orijinal DB kimliği yolunun girişleri
// (testler değiştirir: bayide bu yolun HİÇ denenmediği canlı MariaDB'ye dokunmadan
// sınanır).
var (
	configtenDBKimlikOku = configtenDBKimlik
	orijinalKimlikDene   func(h *Handlers, ctx context.Context, domainID int64, kaynakDB string, orij dbKimlik) bool
)

func init() { orijinalKimlikDene = (*Handlers).orijinalKimlikKullanilir }

// veritabanlariniAktar — tum kaynak DB'lerini tasir.
//
// 🔴 Parola domain basina BIR KEZ uretilir ve kullanici YOKSA atanir. Orijinal
// kimlik yolunda mevcut kullanicinin parolasi DEGISTIRILMEZ (ayni kullaniciyi
// paylasan CANLI siteler kopardi); hesaplar.MySQLCreateDB artik yalniz-yeni
// semantikle calisir ve var olan kullaniciya hic dokunmaz.
func (h *Handlers) veritabanlariniAktar(ctx context.Context, k *Kaynak, hs Hesap, sk, webRoot string,
	kapsam Kapsam, sonuc *AktarSonuc, log func(string, ...any)) (map[string]dbHedef, string, error) {

	// 🔴 BAYİ + KUM HAVUZU ZORUNLU: bayi taşıması ancak içe aktarma istemcisi
	// --sandbox destekliyorsa yürütülür. Desteklemeyen bir istemcide düşman dökümün
	// dosya sistemine erişme (\!, system, LOAD DATA LOCAL, source) yolu yalnız satır
	// süzgeci + --binary-mode ile kapatılır; bu, kök için kabul edilir ama bir bayinin
	// kiracı sınırını aşma riskini taşıyamayız → bayi işi kapalı.
	if kapsam.Bayi() && !sandboxKontrol() {
		return nil, "", errBayiSandboxYok
	}

	// hedefKul: FALLBACK yolunun panel kullanicisi (<sk>_db; bkz. tasimaKullanicisiSec).
	// Ilk ihtiyacta secilir. Parolasini bilinen yeni bir degere ayarlamak ZORUNLU: aksi
	// halde wp-config'e yazacak parolamiz olmaz ve site "Access denied" verir. Kullanici
	// zaten varsa (ustune tasima) bu YALNIZ panelde bu domaine kayitliysa yapilir — bkz.
	// panelKullanicisiylaOlustur.
	hedefKul := ""
	dbPw := hesaplar.RandomParola(24)

	// 🔴 ORIJINAL kimlik tercihi: DB adi+kullanici+parola AYNI kalirsa wp-config
	// HIC degismez (kullanici config guncellemek zorunda kalmaz) ve ad uzamaz.
	// Yapilandirmadan okunur; yalniz hedefte ad+kullanici MUSAITSE (cakisma yok)
	// kullanilir, aksi halde <sk> onekli benzersiz ada + config rewrite'a düsülür.
	kimlikler := configtenDBKimlikOku(webRoot)

	// 🔴 ORİJİNAL DB KİMLİĞİ YOLU BAYİDE KAPALI: bu yol, kaynağın wp-config'inden
	// okunan (DÜŞMAN) ad/kullanıcı/parolayı hedefte doğrudan kullanır. Kök için
	// (kendi sunucusu, kendi kiracıları) kabul edilir; bir bayide kaynaktan gelen
	// serbest ad başka kiracının ad-alanına düşme riskini artırır → her zaman
	// <sk> önekli benzersiz ad + config yeniden yazımı.
	orijIzin := !kapsam.Bayi()

	esleme := map[string]dbHedef{}
	kuruldu := false
	var basarisiz []string

	for _, kaynakDB := range hs.DBler {
		if !reDBAd.MatchString(kaynakDB) {
			continue
		}
		// --- ORIJINAL kimlikle taşıma (config degismez) ---
		if orij, ok := kimlikler[kaynakDB]; orijIzin && ok && orijinalKimlikDene(h, ctx, sonuc.DomainID, kaynakDB, orij) {
			if err := hesaplar.MySQLCreateDB(h.DB, sonuc.DomainID, kaynakDB, orij.Kul, orij.Pw); err != nil {
				log("uyari: orijinal DB %s olusturulamadi, benzersize düşülüyor: %v", kaynakDB, err)
			} else if err := h.dbAktar(ctx, k, kaynakDB, kaynakDB, log); err != nil {
				log("HATA: %s aktarilamadi: %v", kaynakDB, err)
				basarisiz = append(basarisiz, kaynakDB)
				continue
			} else {
				sonuc.DBSayisi++
				log("veritabani (ORIJINAL ad/kullanici/parola — config degismedi): %s", kaynakDB)
				continue // esleme'ye EKLENMEZ → configYenidenYaz bu DB'yi atlar
			}
		}
		// --- FALLBACK: benzersiz ad + panel kullanicisi + config rewrite ---
		hedefAd, err := h.benzersizHedefDB(ctx, sonuc.DomainID, sk, kaynakDB, hs.KaynakHesap)
		if err != nil {
			log("uyari: %s icin hedef ad uretilemedi: %v", kaynakDB, err)
			basarisiz = append(basarisiz, kaynakDB)
			continue
		}
		log("veritabani: %s → %s", kaynakDB, hedefAd)

		if !kuruldu {
			if hedefKul == "" {
				kul, err := h.tasimaKullanicisiSec(ctx, sonuc.DomainID, sk)
				if err != nil {
					log("uyari: %s icin panel kullanicisi secilemedi: %v", kaynakDB, err)
					basarisiz = append(basarisiz, kaynakDB)
					continue
				}
				hedefKul = kul
			}
			if err := h.panelKullanicisiylaOlustur(sonuc.DomainID, hedefAd, hedefKul, dbPw); err != nil {
				log("uyari: %s olusturulamadi: %v", hedefAd, err)
				basarisiz = append(basarisiz, kaynakDB)
				continue
			}
			kuruldu = true
		} else if err := hesaplar.MySQLCreateDBForUser(h.DB, sonuc.DomainID, hedefAd, hedefKul); err != nil {
			log("uyari: %s olusturulamadi: %v", hedefAd, err)
			basarisiz = append(basarisiz, kaynakDB)
			continue
		}

		if err := h.dbAktar(ctx, k, kaynakDB, hedefAd, log); err != nil {
			log("HATA: %s aktarilamadi: %v", kaynakDB, err)
			basarisiz = append(basarisiz, kaynakDB)
			continue
		}
		sonuc.DBSayisi++
		esleme[kaynakDB] = dbHedef{Ad: hedefAd, Kul: hedefKul}
	}

	if len(basarisiz) > 0 {
		return esleme, dbPw, fmt.Errorf("veritabani aktarilamadi: %s", strings.Join(basarisiz, ", "))
	}
	return esleme, dbPw, nil
}

// panelKullanicisiylaOlustur: FALLBACK yolunun ilk DB'si + <sk>_db panel kullanicisi.
//   - Kullanici YOKSA: hesaplar.MySQLCreateDB (yalniz-yeni) ile olusur.
//   - VARSA ve panelde YALNIZ bu domaine kayitliysa (ustune tasima / yeniden deneme):
//     parolasi sahiplik dogrulanarak (MySQLChangePassword) dbPw'ye cekilir, DB ona
//     baglanir (MySQLCreateDBForUser).
//   - VARSA ama bu domaine ait DEGILSE (onek cakismasiyla baska kiracinin adi, ya da
//     kayitsiz artik): REDDEDILIR. Eskiden MySQLCreateDB'nin kosulsuz ALTER USER'i
//     bu kullaniciyi sessizce devraliyordu.
func (h *Handlers) panelKullanicisiylaOlustur(domainID int64, hedefAd, hedefKul, dbPw string) error {
	err := hesaplar.MySQLCreateDB(h.DB, domainID, hedefAd, hedefKul, dbPw)
	if !errors.Is(err, hesaplar.ErrDBKimlikMevcut) {
		return err
	}
	if perr := hesaplar.MySQLChangePassword(h.DB, domainID, hedefKul, dbPw); perr != nil {
		return fmt.Errorf("%w (%v)", err, perr)
	}
	return hesaplar.MySQLCreateDBForUser(h.DB, domainID, hedefAd, hedefKul)
}

// tasimaKullanicisiSec: FALLBACK yolunun panel kullanicisi. Temel ad <sk>_db.
//
// 🔴 ONEK CAKISMASI (DoS): en uzun kok kurali shop.com'un (c_shop) c_shop_db'sini
// shop-db.com'a (tam sk'si c_shop_db) verir. Eskiden <sk>_db sabitti: shop-db.com'un
// sahibi adi onceden acinca kurbanin tasimasi panelKullanicisiylaOlustur'da
// (MySQLCreateDB 1396 → MySQLChangePassword → "bu domaine kayitli degil") duruyor, hic
// DB aktarilamiyordu. Artik ad kurbanin KENDI ad-alaninda secilir (c_shop_db2 …). Ad
// panelde YALNIZ bu domaine kayitliysa (ustune tasima) yeniden kullanilir; degilse
// MariaDB'de ve panelde hic kullanilmayan bir ad gerekir.
func (h *Handlers) tasimaKullanicisiSec(ctx context.Context, domainID int64, sk string) (string, error) {
	return hesaplar.KendiAdAlanindaAdSec(ctx, h.DB, domainID, sk+"_db", func(ad string) (bool, error) {
		if hesaplar.KullaniciYalnizBuDomainde(h.DB, domainID, ad) == nil {
			return true, nil
		}
		return hesaplar.AdSerbestMi(ctx, h.DB, ad)
	})
}

// kendiAdAlaniUygun: ad BASKA bir kiracinin ad-alanina dusmuyor mu (bkz.
// hesaplar.DBAdiBaskaKiracininMi)? Olcum hatasi = uygun degil (fail-closed).
func (h *Handlers) kendiAdAlaniUygun(ctx context.Context, domainID int64, ad string) bool {
	baska, err := hesaplar.DBAdiBaskaKiracininMi(ctx, h.DB, domainID, ad)
	return err == nil && !baska
}

// orijinalKimlikKullanilir: kaynak DB ORIJINAL ad/kullanici/parolasiyla (wp-config
// degismeden) tasinabilir mi? Ad ve kullanici hedefte hic yok olmali.
//
// 🔴 Orijinal ad/kullanici BASKA bir kiracinin ad-alanina duserse (kaynak sunucuda
// secilmis serbest adlar: "c_shop_db", "shop_joomla_…") kullanilmaz — o kiracinin
// turettigi adlari onceden tutardi; benzersiz (kendi ad-alani) yola dusulur.
func (h *Handlers) orijinalKimlikKullanilir(ctx context.Context, domainID int64, kaynakDB string, orij dbKimlik) bool {
	return orij.Kul != "" && orij.Pw != "" && reDBAd.MatchString(orij.Kul) &&
		h.dbAdiMusaitMi(ctx, kaynakDB) && !h.dbKullanicisiVarMi(ctx, orij.Kul) &&
		h.kendiAdAlaniUygun(ctx, domainID, kaynakDB) && h.kendiAdAlaniUygun(ctx, domainID, orij.Kul)
}

func (h *Handlers) dbKullanicisiVarMi(ctx context.Context, kul string) bool {
	// 🔴 kul, taşınan sitenin wp-config.php'sinden okunur (saldırgan etkisine açık
	// dosya). mysql CLI (root socket auth — panel DB kullanıcısı mysql.user'ı
	// okuyamayabilir, bkz. kaynaklimit.mysqlUserHosts) parametreli sorgu
	// desteklemez; bu yüzden SQL tamamen SABİT tutulur — kullanıcı girdisi
	// sorguya hiçbir biçimde girmez, karşılaştırma Go tarafında yapılır
	// (parametrize sorgunun yapısal eşdeğeri). reDBAd allowlist'i ilk kapı
	// olarak kalır; geçersiz değere "var" denir → çağıran orijinal-kimlik
	// yolunu atlar (fail-closed, benzersiz panel adına düşer).
	if !reDBAd.MatchString(kul) {
		return true
	}
	out, err := exec.CommandContext(ctx, "mysql", "-N", "-B", "-e",
		"SELECT user FROM mysql.user WHERE host='localhost'").Output()
	if err != nil {
		// mevcut davranış korunur: belirsizlikte çağıran orijinal yolu dener,
		// MySQLCreateDB hatası zaten "benzersize düşülüyor" loguyla zarifçe düşer.
		return false
	}
	for _, satir := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(satir) == kul {
			return true
		}
	}
	return false
}

// dbAdiMusaitMi — hedef sunucuda bu DB adi HIC yok mu (sema + panel kaydi).
func (h *Handlers) dbAdiMusaitMi(ctx context.Context, ad string) bool {
	var n int
	_ = h.DB.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name=?)
		      + (SELECT COUNT(*) FROM db_accounts WHERE db_name=?)`, ad, ad).Scan(&n)
	return n == 0
}

// benzersizHedefDB — "olduser_wp" → "<sk>_wp". 64 karakter sinirinda KIRPMAK
// yerine cakisma denetler ve sayac eki verir; kirpma iki farkli kaynak DB'yi
// ayni hedefe indirip birini sessizce dusuruyordu.
//
// 🔴 ONEK CAKISMASI: "<sk>_<sonek>" baska bir kiracinin DAHA UZUN kokune dusebilir
// (shop.com'a tasinan "joomla_x" → c_shop_joomla_x = shop-joomla.com'un ad-alani).
// Boyle aday atlanir; temel baska kiracidaysa "<temel>_<n>" adaylarinin hepsi de
// oradadir → kendi ad-alaninda kompakt ad (hesaplar.KendiAdAlanindaAdSec).
func (h *Handlers) benzersizHedefDB(ctx context.Context, domainID int64, sk, kaynakDB, kaynakHesap string) (string, error) {
	sonek := kaynakDB
	if kaynakHesap != "" && strings.HasPrefix(kaynakDB, kaynakHesap+"_") {
		sonek = strings.TrimPrefix(kaynakDB, kaynakHesap+"_")
	}
	sonek = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			return r
		}
		return '_'
	}, sonek)
	if sonek == "" {
		sonek = "db"
	}
	temel := sk + "_" + sonek
	// Benzersizlik HEM gercek sema HEM panel kaydindan (db_accounts) kontrol
	// edilmeli: MySQLCreateDB, db_accounts.db_name UNIQUE'e carpar; yalniz
	// information_schema bakmak yeniden-tasimada 1062 Duplicate verir.
	musait := func(aday string) (bool, error) {
		var n int
		if err := h.DB.QueryRowContext(ctx,
			`SELECT (SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name=?)
			      + (SELECT COUNT(*) FROM db_accounts WHERE db_name=?)`, aday, aday).Scan(&n); err != nil {
			return false, err
		}
		return n == 0, nil
	}
	for i := 0; i < 50; i++ {
		aday := temel
		if i > 0 {
			aday = fmt.Sprintf("%s_%d", temel, i+1)
		}
		if len(aday) > 64 {
			kes := 64 - len(aday) + len(temel)
			if kes < 1 {
				return "", fmt.Errorf("ad cok uzun")
			}
			aday = temel[:kes]
			if i > 0 {
				aday = fmt.Sprintf("%s_%d", temel[:kes-2], i+1)
			}
		}
		if baska, err := hesaplar.DBAdiBaskaKiracininMi(ctx, h.DB, domainID, aday); err != nil {
			return "", err
		} else if baska {
			if i == 0 {
				break // temel baska kiracida → "<temel>_<n>" de onda
			}
			continue
		}
		if ok, err := musait(aday); err != nil {
			return "", err
		} else if ok {
			return aday, nil
		}
	}
	ad, err := hesaplar.KendiAdAlanindaAdSec(ctx, h.DB, domainID, temel, musait)
	if err != nil {
		return "", fmt.Errorf("benzersiz veritabani adi uretilemedi: %w", err)
	}
	return ad, nil
}

// ---------------------------------------------------------------------------
// Veritabani aktarimi
// ---------------------------------------------------------------------------

const (
	maxDumpBayt   int64 = 8 << 30
	maxAcilimBayt int64 = 64 << 30
	dumpBitisIz         = "Dump completed"
)

// dbAktar — uzak dump'i indirir ve KISITLI bir MySQL kullanicisiyla import eder.
//
// 🔴 Iki tuzak:
//  1. "mysqldump | gzip" boru hattinda kabuk varsayilan olarak SON komutun
//     cikis kodunu dondurur → mysqldump cokse bile exit 0, sonuc "basarili"
//     ama veritabani BOS. Bu yuzden pipefail ZORUNLU + dump sonu damgasi
//     dogrulanir.
//  2. Import ROOT ile yapilirsa dusman dump hedef DB sinirini asar.
func (h *Handlers) dbAktar(ctx context.Context, k *Kaynak, kaynakDB, hedefDB string, log func(string, ...any)) error {
	if !reDBAd.MatchString(kaynakDB) || !reDBAd.MatchString(hedefDB) {
		return fmt.Errorf("gecersiz veritabani adi")
	}
	if k.arsiv != nil {
		return h.arsivDBAktar(ctx, k, kaynakDB, hedefDB, log)
	}
	tmp, err := os.CreateTemp("/var/tmp", "gosp_tasima_*.sql.gz")
	if err != nil {
		return err
	}
	tmpAd := tmp.Name()
	defer os.Remove(tmpAd)

	// 🔴 KAYNAK MySQL KİMLİĞİ — panele göre. cPanel'de root'un ~/.my.cnf'i vardır
	// (ek kimlik gerekmez, eski davranış). Plesk/DirectAdmin'de MySQL root/admin
	// PAROLALIDIR → kimliksiz mysqldump "Access denied (using password: NO)" verir
	// (kullanıcının gerçek Plesk'inde CANLI yakalandı). Panelin admin MySQL kimliğini
	// kullan. 🔴 Parola $(cat/sed ...) ile ALINIR: `ps`'te parolanın KENDİSİ görünmez,
	// yalnız onu okuyan komut görünür.
	// Kimlik kaynak sürücüsünden (kaynak_kayit.go: Plesk .psa.shadow, DA mysql.conf).
	kimlikOnek := ""
	dumpKul := ""
	if s, ok := surucuBul(k.Tip); ok {
		kimlikOnek, dumpKul = s.dumpKimlikOnek, s.dumpKul
	}
	ic := kimlikOnek + "mysqldump " + dumpKul + "--single-transaction --quick --routines --triggers " +
		"--no-tablespaces --default-character-set=utf8mb4 " + shQuote(kaynakDB) + " | gzip -c"
	// pipefail icin bash zorlanir; yoksa sh'e duser (o zaman dump-sonu damgasi
	// tek koruma olarak kalir).
	uzak := "if command -v bash >/dev/null 2>&1; then bash -o pipefail -c " + shQuote(ic) +
		"; else " + ic + "; fi"

	sayan := &sinirliYazici{alt: tmp, kalan: maxDumpBayt}
	eb, runErr := k.AkisCalistir(ctx, uzak, sayan)
	tmp.Close()
	var hazirlik *akisHazirlikHatasi
	if errors.As(runErr, &hazirlik) {
		return hazirlik.err
	}
	if sayan.asildi {
		return fmt.Errorf("dump boyut sinirini asti (%d GB)", maxDumpBayt>>30)
	}
	if runErr != nil {
		return fmt.Errorf("dump: %s", kisalt(temizHata(eb, k.Parola), 200))
	}
	st, err := os.Stat(tmpAd)
	if err != nil || st.Size() == 0 {
		return fmt.Errorf("dump bos dondu")
	}

	f, err := os.Open(tmpAd)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("dump okunamadi (gzip): %w", err)
	}
	defer gz.Close()
	return h.dbIceAktar(ctx, io.LimitReader(gz, maxAcilimBayt), hedefDB)
}

// dbIceAktar — düz SQL dökümünü KISITLI geçici MySQL kullanıcısıyla hedef veritabanına
// içe aktarır (canlı SSH dökümü ve arşiv dökümü ORTAK yol). Döküm süzgeçten (dumpFiltre:
// DEFINER sökme, istemci komutu reddi, bitiş damgası) geçer; `mysql --sandbox` varsa
// kullanılır (bayide zorunlu: veritabanlariniAktar). Boyut sınırını çağıran verir.
func (h *Handlers) dbIceAktar(ctx context.Context, kaynak io.Reader, hedefDB string) error {
	impKul, impPw, err := h.geciciImportKullanicisi(ctx, hedefDB)
	if err != nil {
		return fmt.Errorf("import kullanicisi: %w", err)
	}
	defer h.importKullanicisiDusur(impKul)

	cnfAd, err := cnfYaz(impKul, impPw)
	if err != nil {
		return err
	}
	defer os.Remove(cnfAd)

	// 🔴 KISITLI İÇE AKTARMA İSTEMCİSİ: --sandbox dosya sistemine erişen istemci
	// komutlarını, --binary-mode piped girdide TÜM istemci komutlarını (\!, system,
	// source, \u) devre dışı bırakır, --local-infile=0 LOAD DATA LOCAL'i kapatır.
	// Bunlar --sandbox'sız istemcilerde (root yolu) tek başına yetmez; satır süzgeci
	// (dumpFiltre) ikinci kat savunmadır.
	imp := exec.CommandContext(ctx, "mysql", importArgs(cnfAd, hedefDB)...)
	filtre := &dumpFiltre{}
	imp.Stdin = filtre.Sar(kaynak)
	var ieb strings.Builder
	imp.Stderr = &ieb
	runHatasi := imp.Run()
	filtre.Bekle() // okuyucuyu kapatır: mysql erken çıktıysa süzgeç takılı kalmaz
	// 🔴 GÜVENLİK REDDİ ÖNCE: düşman döküm güvenli olmayan bir istemci komutu (\!,
	// system, source, \u, LOAD DATA LOCAL) içeriyorsa akış kesilmiştir. Genel neden
	// döndür; döküm içeriği / import stderr'i çağırana SIZMAZ.
	if filtre.Reddedildi {
		stdlog.Printf("tasima: içe aktarma reddedildi (hedef=%s): güvenli olmayan istemci komutu", hedefDB)
		return fmt.Errorf("içe aktarma reddedildi: kaynak döküm güvenli olmayan komut içeriyor")
	}
	if runHatasi != nil {
		// 🔴 IMPORT STDERR HAM DÖNMEZ: mysql import hatası düşman dökümden SQL/veri
		// parçaları taşır (satır numarası + ham satır). Ayrıntı sunucu günlüğüne;
		// çağırana yalnız genel metin (nginxset.SunucuHatasiGenel ile aynı ilke).
		stdlog.Printf("tasima: DB içe aktarma hatası (hedef=%s): %s",
			hedefDB, kisalt(temizHata(ieb.String(), ""), 500))
		return fmt.Errorf("içe aktarma başarısız (ayrıntı sunucu günlüğüne yazıldı)")
	}
	// 🔴 mysqldump ciktisini "-- Dump completed" ile bitirir. Damga yoksa dump
	// yarim kalmistir (uzak hata, kopan baglanti, kilitli tablo…).
	if !filtre.Tamam {
		return fmt.Errorf("dump yarim kaldi (kaynak sunucuda mysqldump hata verdi)")
	}
	return nil
}

// importArgs — kısıtlı içe aktarma istemcisinin argümanları. --sandbox yalnız
// istemci destekliyorsa eklenir (kök yolu desteklemeyen istemcide çalışmaya devam
// eder; bayi yolu veritabanlariniAktar'da zaten reddedilmiştir).
//
// 🔴 --defaults-file (İLK argüman olmak ZORUNDA) DIŞLAYICIDIR: yalnız kısıtlı
// kullanıcının cnf'i okunur. --defaults-extra-file ise genel dosyalardan SONRA ama
// ~/.my.cnf'ten ÖNCE okunur; panel root olarak çalıştığından yöneticinin
// /root/.my.cnf'indeki [client] user/password kısıtlı kimliği EZER ve düşman döküm
// ROOT yetkisiyle içe aktarılırdı (başka kiracının DB'sine, mysql.user'a yazma).
func importArgs(cnfAd, hedefDB string) []string {
	a := []string{
		"--defaults-file=" + cnfAd,
		"--default-character-set=utf8mb4",
		"--binary-mode",
		"--local-infile=0",
	}
	if sandboxKontrol() {
		a = append(a, "--sandbox")
	}
	return append(a, hedefDB)
}

// sandboxKontrol — içe aktarma istemcisi --sandbox destekliyor mu (testler değiştirir).
var sandboxKontrol = mysqlSandboxDestekli

// errBayiSandboxYok — bayi işi, istemci --sandbox desteklemediği için kapalı. Metin
// sabittir ve kiracı verisi içermez → bayiye aynen gösterilir (bayiyeAcikHatalar).
var errBayiSandboxYok = errors.New("bu sunucudaki içe aktarma istemcisi güvenli kum havuzunu (--sandbox) desteklemiyor; bayi taşıması kapalı")

var (
	sandboxBirKez    sync.Once
	sandboxDestekli  bool
	reSandboxSecenek = regexp.MustCompile(`(?m)^\s*--sandbox\b`)
)

// mysqlSandboxDestekli — yerel `mysql` istemcisi --sandbox seçeneğini tanıyor mu?
// `mysql --help --verbose` seçenek listesini tarar (ağa/DB'ye bağlanmaz). Sonuç
// süreç ömrü boyunca önbelleklenir.
func mysqlSandboxDestekli() bool {
	sandboxBirKez.Do(func() {
		out, _ := exec.Command("mysql", "--no-defaults", "--help", "--verbose").CombinedOutput()
		sandboxDestekli = reSandboxSecenek.Match(out)
	})
	return sandboxDestekli
}

func cnfYaz(kul, pw string) (string, error) {
	c, err := os.CreateTemp("/var/tmp", "gosp_imp_*.cnf")
	if err != nil {
		return "", err
	}
	ad := c.Name()
	_ = c.Chmod(0o600)
	_, err = c.WriteString("[client]\nuser=" + kul + "\npassword=" + pw +
		"\nsocket=/var/lib/mysql/mysql.sock\n")
	c.Close()
	if err != nil {
		os.Remove(ad)
		return "", err
	}
	return ad, nil
}

type sinirliYazici struct {
	alt    *os.File
	kalan  int64
	asildi bool
}

func (w *sinirliYazici) Write(p []byte) (int, error) {
	if w.asildi {
		return 0, fmt.Errorf("boyut siniri asildi")
	}
	if int64(len(p)) > w.kalan {
		w.asildi = true
		return 0, fmt.Errorf("boyut siniri asildi")
	}
	n, err := w.alt.Write(p)
	w.kalan -= int64(n)
	return n, err
}

var reDefiner = regexp.MustCompile("(?i)/\\*![0-9]* *DEFINER *= *[^*]*\\*/|DEFINER *= *`[^`]*`@`[^`]*`")

var errDumpReddi = errors.New("dump güvenli olmayan istemci komutu içeriyor")

// dumpFiltre — DEFINER yan tumcelerini soker (kisitli import kullanicisi baskasi
// adina DEFINER atayamaz), dump-sonu damgasini izler ve düşman bir dökümün
// dosya sistemine erişen mysql İSTEMCİ komutlarını (satır başında etkili)
// yakalarsa akışı KESİP reddeder (Reddedildi=true).
type dumpFiltre struct {
	Tamam      bool
	Reddedildi bool
	okur       *io.PipeReader
	bitti      chan struct{}
}

func (d *dumpFiltre) Sar(r io.Reader) io.Reader {
	d.bitti = make(chan struct{})
	pr, pw := io.Pipe()
	d.okur = pr
	go func() {
		defer close(d.bitti)
		defer pw.Close()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 1<<20), 32<<20)
		for sc.Scan() {
			satir := sc.Text()
			if tehlikeliDumpSatiri(satir) {
				d.Reddedildi = true
				_ = pw.CloseWithError(errDumpReddi)
				return
			}
			if strings.Contains(satir, dumpBitisIz) {
				d.Tamam = true
			}
			if _, err := pw.Write([]byte(reDefiner.ReplaceAllString(satir, "") + "\n")); err != nil {
				return
			}
		}
		if err := sc.Err(); err != nil {
			_ = pw.CloseWithError(err)
		}
	}()
	return pr
}

// Bekle — tüketici (imp.Run) döndükten SONRA çağrılır: okuyucu ucunu kapatır ve
// süzme goroutine'i bitene kadar bekler; ardından Reddedildi/Tamam alanları yarış
// olmadan okunur. (exec.Cmd.Wait stdin kopyalayıcısını bekler → Run döndükten sonra
// okuyucuyu okuyan kalmaz, kapatmak güvenlidir.)
//
// 🔴 Okuyucuyu kapatmak ZORUNLU: mysql girdinin tamamını okumadan çıkarsa (ilk SQL
// hatası — ör. MySQL 8 dökümündeki utf8mb4_0900_ai_ci —, iptal, hesapTimeout) io.Pipe'ın
// okuyanı kalmaz ve goroutine pw.Write'ta SONSUZA DEK takılırdı → HesapAktar dönmez,
// tek global iş yuvası (aktifIsID) panel yeniden başlatılana kadar kilitlenirdi.
// Kapalı okuyucuda pw.Write io.ErrClosedPipe alır, goroutine döner.
func (d *dumpFiltre) Bekle() {
	if d.bitti == nil {
		return
	}
	_ = d.okur.Close()
	<-d.bitti
}

// tehlikeliDumpSatiri — satır (baştaki boşluk atılınca) dosya sistemine erişen
// bir mysql İSTEMCİ komutuyla mı başlıyor? Bu komutlar yalnız satır başında etkili
// olur; gerçek `mysqldump` çıktısı bunları HİÇ üretmez (yalnız SQL + `--`/`/*!*/`
// yorumları) → meşru dökümde yanlış-pozitif yok, düşman dökümde yakalanır.
//
//	\!  → kabuk komutu · \.  → dosya kaynak · \u  → veritabanı seç
//	system … → kabuk komutu · source … → dosya kaynak
//	LOAD DATA … LOCAL … → yerel dosya okuma (--local-infile=0 zaten kapatır)
func tehlikeliDumpSatiri(satir string) bool {
	t := strings.TrimLeft(satir, " \t")
	if strings.HasPrefix(t, `\!`) || strings.HasPrefix(t, `\.`) || strings.HasPrefix(t, `\u`) {
		return true
	}
	lt := strings.ToLower(t)
	if istemciKomutBasi(lt, "system") || istemciKomutBasi(lt, "source") {
		return true
	}
	if strings.HasPrefix(lt, "load data") && strings.Contains(lt, "local") {
		return true
	}
	return false
}

// istemciKomutBasi — lt, kelime ile başlayıp ardından sınır (boşluk/tab/satır sonu)
// mu geliyor? "sourced_table" gibi meşru tanımlayıcıları yanlış yakalamamak için.
func istemciKomutBasi(lt, kelime string) bool {
	if !strings.HasPrefix(lt, kelime) {
		return false
	}
	rest := lt[len(kelime):]
	return rest == "" || rest[0] == ' ' || rest[0] == '\t'
}

// geciciImportKullanicisi — YALNIZ hedefDB uzerinde yetkili gecici kullanici.
func (h *Handlers) geciciImportKullanicisi(ctx context.Context, hedefDB string) (string, string, error) {
	if !reDBAd.MatchString(hedefDB) {
		return "", "", fmt.Errorf("gecersiz hedef veritabani")
	}
	ham := make([]byte, 9)
	if _, err := rand.Read(ham); err != nil {
		return "", "", err
	}
	kul := "gosp_imp_" + hex.EncodeToString(ham)[:12]
	pw := hesaplar.RandomParola(28)
	for _, q := range []string{
		"CREATE USER '" + kul + "'@'localhost' IDENTIFIED BY '" + pw + "'",
		"GRANT ALL PRIVILEGES ON `" + hesaplar.GrantDBKac(hedefDB) + "`.* TO '" + kul + "'@'localhost'",
		"FLUSH PRIVILEGES",
	} {
		c := exec.CommandContext(ctx, "mysql", "-e", q)
		var eb strings.Builder
		c.Stderr = &eb
		if err := c.Run(); err != nil {
			_ = exec.Command("mysql", "-e", "DROP USER IF EXISTS '"+kul+"'@'localhost'").Run()
			return "", "", fmt.Errorf("%s", kisalt(temizHata(eb.String(), ""), 150))
		}
	}
	return kul, pw, nil
}

func (h *Handlers) importKullanicisiDusur(kul string) {
	if kul == "" {
		return
	}
	_ = exec.Command("mysql", "-e", "DROP USER IF EXISTS '"+kul+"'@'localhost'").Run()
}
