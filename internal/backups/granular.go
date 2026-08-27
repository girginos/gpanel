package backups

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/archivex"
	"girginospanel/internal/hesaplar"
	"girginospanel/internal/httpx"

	"github.com/go-chi/chi/v5"
)

// shq: tek-tirnak shell-quote (mysql/mysqldump dosya yollari + db adlari icin).
func shq(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sistemDBmi: asla dokunulmamasi gereken sistem/panel veritabanlari.
func sistemDBmi(n string) bool {
	switch strings.ToLower(strings.TrimSpace(n)) {
	case "mysql", "information_schema", "performance_schema", "sys", "panel":
		return true
	}
	return false
}

// domainDBleri: domaine ait TUM veritabani adlari (ana <sk>_main + db_accounts satirlari).
// Sadece gecerli-kimlik + sistem-disi adlar doner (whitelist kaynagi).
func domainDBleri(db *sql.DB, domainID int64, sk string) []string {
	set := map[string]bool{}
	out := []string{}
	add := func(n string) {
		n = strings.TrimSpace(n)
		if n != "" && hesaplar.GecerliDBKimlik(n) && !sistemDBmi(n) && !set[n] {
			set[n] = true
			out = append(out, n)
		}
	}
	add(sk + "_main")
	if rows, err := db.Query(`SELECT db_name FROM db_accounts WHERE domain_id=?`, domainID); err == nil {
		defer rows.Close()
		for rows.Next() {
			var n string
			if rows.Scan(&n) == nil {
				add(n)
			}
		}
	}
	return out
}

// tenantAnaKullanici: domainin ana DB kullanicisi (yeni-hedef DB'ye GRANT icin).
func tenantAnaKullanici(db *sql.DB, domainID int64, sk string) string {
	var u string
	_ = db.QueryRow(`SELECT db_user FROM db_accounts WHERE domain_id=? AND db_name=? LIMIT 1`,
		domainID, sk+"_main").Scan(&u)
	if u == "" {
		_ = db.QueryRow(`SELECT db_user FROM db_accounts WHERE domain_id=? LIMIT 1`, domainID).Scan(&u)
	}
	if u == "" {
		u = sk + "_db"
	}
	return u
}

// arsivManifest: arsivin __db__/manifest.json icerigi (bilgilendirme + DB listesi).
type arsivManifest struct {
	Olusturma     string   `json:"olusturma"`
	Home          string   `json:"home"`
	AnaDB         string   `json:"ana_db"`
	Veritabanlari []string `json:"veritabanlari"`
}

// dosyaSha256: bir dosyanin sha256 checksum'i (hex). At-rest bit-rot / uzak-kopya
// dogrulamasi + olusturma-ani butunluk kaydi icin. Buyuk arsivi akiskan okur (streaming).
func dosyaSha256(yol string) (string, error) {
	f, err := os.Open(yol)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// dumpTamamlandiMi: mysqldump ciktisinin son 512 baytinda "-- Dump completed"
// damgasini arar. Yarim kalan dump exit 0 + boyut>0 ile "basarili" gorunebilir.
func dumpTamamlandiMi(yol string) bool {
	f, err := os.Open(yol)
	if err != nil {
		return false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	n := int64(512)
	if fi.Size() < n {
		n = fi.Size()
	}
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, fi.Size()-n); err != nil {
		return false
	}
	return strings.Contains(string(buf), "-- Dump completed")
}

// arsivOlustur: /home/<sk> + TUM domain DB'leri (__db__/<ad>.sql) + manifest'i tek
// .tar.gz'ye paketler. Hem manuel (Create) hem otomatik (scheduler) tarafindan kullanilir.
// Eski surum yalniz <sk>_main aliyordu; artik wp_* gibi ek DB'ler de yedeklenir.
//
// 🔴 BUTUNLUK: tar'in cikis kodu YALNIZ kaba hatalari yakalar; sessiz sikistirma
// bozulmasini (truncation, disk hatasi) yakalamaz. Arsiv olusturulur olusturulmaz
// `gzip -t` ile dogrulanir ve sha256 hesaplanir. Bozuk arsiv SILINIR ve hata doner
// (cagiran KRITIK bildirim gonderir). Doner: (boyut, sha256, basarisiz_db_listesi, hata).
func arsivOlustur(ctx context.Context, db *sql.DB, domainID int64, sk, dir, dosya, olusturmaTS string) (int64, string, []string, error) {
	abs := filepath.Join(dir, dosya)
	IlerlemeAsama(domainID, "veritabanları alınıyor", 0)
	dbDir := filepath.Join(dir, "__db__")
	_ = os.RemoveAll(dbDir)
	if err := os.MkdirAll(dbDir, 0700); err != nil {
		return 0, "", nil, fmt.Errorf("db staging: %w", err)
	}
	defer os.RemoveAll(dbDir)

	yazilan := []string{}
	basarisizDB := []string{}
	for _, dbName := range domainDBleri(db, domainID, sk) {
		// 🔴 EKSIK-VERI vs DB'SIZ SITE AYRIMI: domainDBleri her zaman speculatif <sk>_main
		// ekler. DB'siz statik site'de bu DB HIC OLMAZ → dump'i basarisiz olur ama bu bir
		// HATA DEGILDIR (yedeklenecek veri yok). Yalniz GERCEKTEN VAR OLAN bir DB'nin dump'i
		// basarisizsa "eksik veri" sayilir (kilit/izin/bozulma = gercek veri kaybi riski).
		// mysqldump ile AYNI auth yolu (root socket) kullanilir ki gorunurluk tutarli olsun.
		chk := exec.CommandContext(ctx, "bash", "-c",
			fmt.Sprintf("mysql -N -e \"SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=%s\"", shq(dbName)))
		chkOut, chkErr := chk.Output()
		if chkErr != nil {
			// 🔴 "DB yok" ile "kontrol EDILEMEDI" ayni sey DEGILDIR. Eskiden hata
			// yutuluyordu: MySQL kisa sure cevap veremezse (upgrade, max_connections,
			// socket doluluğu) var olan DB "yok" sayilip SESSIZCE atlaniyor, yedek
			// yine 'ok' isaretleniyordu. Artik eksik-veri olarak raporlanir.
			basarisizDB = append(basarisizDB, dbName+" (varlık kontrolü başarısız)")
			continue
		}
		if strings.TrimSpace(string(chkOut)) == "" {
			continue // DB gercekten yok (DB'siz statik site) — beklenen durum
		}
		hedef := filepath.Join(dbDir, dbName+".sql")
		// 🔴 --routines --events: MariaDB'de triggers VARSAYILAN acik ama routines ve
		// events KAPALI. Eskiden eksiktiler → stored procedure/function/event yedege
		// GIRMIYORDU (site tasima aktar.go ve sistem-yedek betigi bunlari zaten
		// kullaniyor; yalniz per-domain yedek geride kalmisti).
		//: sunucuya MySQL-8 istemcisi kurulursa varsayilan
		// MariaDB mysqldump 'unknown option' ile exit 2 verir ve TUM dumplar patlar.
		hataDosya := hedef + ".hata"
		cmd := exec.CommandContext(ctx, "bash", "-c",
			fmt.Sprintf("mysqldump --single-transaction --skip-lock-tables --routines --events --triggers --default-character-set=utf8mb4 --hex-blob %s > %s 2> %s",
				shq(dbName), shq(hedef), shq(hataDosya)))
		dumpHata := func() string {
			b, _ := os.ReadFile(hataDosya)
			m := strings.TrimSpace(string(b))
			if len(m) > 200 {
				m = m[:200]
			}
			return m
		}
		if err := cmd.Run(); err != nil {
			// 🔴 Sebep eskiden 2>/dev/null ile yok ediliyordu → bildirimde yalniz DB adi
			// vardi, teshis imkansizdi. Artik stderr'in ilk 200 karakteri tasinir.
			sebep := dumpHata()
			_ = os.Remove(hedef)
			_ = os.Remove(hataDosya)
			basarisizDB = append(basarisizDB, dbName+" ("+sebep+")")
			continue
		}
		_ = os.Remove(hataDosya)
		if fi, e := os.Stat(hedef); e != nil || fi.Size() == 0 {
			_ = os.Remove(hedef)
			basarisizDB = append(basarisizDB, dbName+" (boş dump)")
			continue
		}
		// 🔴 KAPANIS DAMGASI: mysqldump ciktisini "-- Dump completed" ile bitirir.
		// Damga yoksa dump YARIM kalmistir (disk dolmasi, kesilen baglanti).
		// exit kodu + boyut kontrolu bunu YAKALAMAZ. Site tasima (aktar.go) bu
		// korumaya sahipti, yedekleme yolu degildi.
		if !dumpTamamlandiMi(hedef) {
			_ = os.Remove(hedef)
			basarisizDB = append(basarisizDB, dbName+" (dump yarım kaldı: kapanış damgası yok)")
			continue
		}
		yazilan = append(yazilan, dbName)
	}

	// 🔴 KULLANICI + GRANT: mysqldump bunlari ALMAZ (mysql semasinda dururlar).
	// Bu satir olmadan geri yukleme semayi ve veriyi getiriyor ama siteyi ayaga
	// kaldiran kullaniciyi getirmiyordu — site 500 donmeye devam ediyordu.
	if n := dbKullanicilariYaz(dbDir, yazilan); n > 0 {
		log.Printf("backup %s: %d veritabani kullanicisi arsive eklendi", sk, n)
	}

	man := arsivManifest{Olusturma: olusturmaTS, Home: sk, AnaDB: sk + "_main", Veritabanlari: yazilan}
	if b, err := json.MarshalIndent(man, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(dbDir, "manifest.json"), b, 0600)
	}

	args := []string{"czf", abs, "-C", "/home", sk, "-C", dir, "__db__"}
	// 🔴 GNU tar CIKIS KODLARI: 0 = tam basarili, 1 = "bazi dosyalar farkli"
	// (ornegin "file changed as we read it" — canli oturum/cache/log dosyasi
	// yedeklenirken degisti) = UYARI, arsiv GECERLIDIR; 2 = olumcul hata.
	// Eskiden her sifir-olmayan kod olumcul sayilip arsiv SILINIYORDU: en cok
	// trafik alan (= en degerli) siteler, Laravel storage/framework/sessions veya
	// WP wp-content/cache yuzunden rastgele YEDEKSIZ kaliyordu. Uretimde
	// gerceklesti (c_kuran_islam_tr_org, 2026-08-26) ve kontrollu deneyle
	// dogrulandi: exit=1 iken gzip -t saglam, tum uyeler listelendi, cikarim basarili.
	// Arsiv olusurken hedef dosyanin buyumesini ornekle → musteri ilerlemeyi gorur.
	IlerlemeAsama(domainID, "dosyalar arşivleniyor", oncekiYedekBoyutu(db, domainID))
	IlerlemeDosyaIzle(domainID, abs)
	tarUyarisi := ""
	if out, err := exec.CommandContext(ctx, "tar", args...).CombinedOutput(); err != nil {
		kod := -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			kod = ee.ExitCode()
		}
		if kod != 1 {
			_ = os.Remove(abs)
			return 0, "", basarisizDB, fmt.Errorf("tar: %s: %w", strings.TrimSpace(string(out)), err)
		}
		// exit 1 → arsivi TUT; asagidaki gzip -t + sha256 zaten gecerliligi olcer.
		tarUyarisi = strings.TrimSpace(string(out))
		if len(tarUyarisi) > 300 {
			tarUyarisi = tarUyarisi[:300]
		}
		log.Printf("backup %s: tar uyarisi (arsiv korundu): %s", sk, tarUyarisi)
	}

	IlerlemeDosyaDur(domainID)
	IlerlemeAsama(domainID, "bütünlük doğrulanıyor", 0)
	// 🔴 BUTUNLUK DOGRULAMASI: gzip stream'i saglam mi (bozulma/truncation tespiti).
	if out, err := exec.CommandContext(ctx, "gzip", "-t", abs).CombinedOutput(); err != nil {
		_ = os.Remove(abs) // bozuk arsivi tut-ma: yaniltici "gecerli yedek" olusmasin
		return 0, "", basarisizDB, fmt.Errorf("yedek butunluk dogrulamasi basarisiz — BOZUK arsiv: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// sha256: at-rest bit-rot / uzak-hedef kopya dogrulamasi icin saklanir.
	sum, err := dosyaSha256(abs)
	if err != nil {
		return 0, "", basarisizDB, fmt.Errorf("checksum hesaplanamadi: %w", err)
	}

	var boyut int64
	if st, _ := os.Stat(abs); st != nil {
		boyut = st.Size()
	}
	return boyut, sum, basarisizDB, nil
}

// IcerikDosya / IcerikDB: arsiv listeleme cikti tipleri.
type IcerikDosya struct {
	Yol   string `json:"yol"`
	Boyut int64  `json:"boyut"`
	Dizin bool   `json:"dizin"`
}
type IcerikDB struct {
	Ad    string `json:"ad"`
	Boyut int64  `json:"boyut"`
}

// Icerik: GET /domains/{id}/backups/{bid}/icerik
// Arsivi (root, kendi yedegi) salt-okunur tarayip dosya agaci + DB listesini doner.
// Dosya-bazli ve SQL-bazli granuler geri yukleme ekrani bunu kullanir.
func (h *Handlers) Icerik(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	bid, _ := strconv.ParseInt(chi.URLParam(r, "bid"), 10, 64)

	var sk, dosya string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT d.sistem_kullanici, b.dosya FROM backups b
		 JOIN domains d ON d.id=b.domain_id
		 WHERE b.id=? AND b.domain_id=?`, bid, id).Scan(&sk, &dosya)
	if errors.Is(err, sql.ErrNoRows) {
		httpx.WriteError(w, http.StatusNotFound, "yedek bulunamadı")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !strings.HasPrefix(sk, "c_") {
		httpx.WriteError(w, http.StatusBadRequest, "güvenlik")
		return
	}
	abs, hazirErr := YerelDosyaHazirla(r.Context(), h.DB, sk, dosya)
	if hazirErr != nil {
		httpx.WriteError(w, http.StatusNotFound, hazirErr.Error())
		return
	}

	f, err := os.Open(abs)
	if err != nil {
		httpx.WriteError(w, http.StatusNotFound, "yedek dosyası diskte bulunamadı")
		return
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "arşiv okunamadı: "+err.Error())
		return
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	const limit = 6000
	dosyalar := []IcerikDosya{}
	dbler := []IcerikDB{}
	kesildi := false
	for {
		hd, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			break
		}
		name := strings.TrimPrefix(hd.Name, "./")
		if name == "" {
			continue
		}
		// DB dump'lari: __db__/<ad>.sql
		if strings.HasPrefix(name, "__db__/") {
			base := strings.TrimPrefix(name, "__db__/")
			if strings.HasSuffix(base, ".sql") {
				dbler = append(dbler, IcerikDB{Ad: strings.TrimSuffix(base, ".sql"), Boyut: hd.Size})
			}
			continue
		}
		// Eski format: kok seviyesinde <dosya>.tar.gz.sql / dump.sql -> ana DB
		if hd.Typeflag == tar.TypeReg && strings.HasSuffix(name, ".sql") && !strings.Contains(strings.TrimSuffix(name, "/"), "/") {
			dbler = append(dbler, IcerikDB{Ad: sk + "_main", Boyut: hd.Size})
			continue
		}
		// Home dosyalari: <sk>/...
		if name == sk || name == sk+"/" {
			continue
		}
		disp := name
		if strings.HasPrefix(name, sk+"/") {
			disp = strings.TrimPrefix(name, sk+"/")
		}
		disp = strings.TrimSuffix(disp, "/")
		if disp == "" {
			continue
		}
		if len(dosyalar) >= limit {
			kesildi = true
			continue
		}
		dosyalar = append(dosyalar, IcerikDosya{
			Yol:   disp,
			Boyut: hd.Size,
			Dizin: hd.Typeflag == tar.TypeDir,
		})
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"dosyalar":      dosyalar,
		"veritabanlari": dbler,
		"kesildi":       kesildi,
	})
}

// ---- granuler geri yukleme yardimcilari ----

// guvenliUyeYolu: arsiv-ici goreli yol dogrulamasi (jail-escape / mutlak yol reddi).
func guvenliUyeYolu(p string) (string, bool) {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "./")
	if p == "" || strings.HasPrefix(p, "/") {
		return "", false
	}
	c := filepath.Clean(p)
	if c == ".." || strings.HasPrefix(c, "../") || strings.Contains(c, "/../") || strings.HasPrefix(c, "/") {
		return "", false
	}
	return c, true
}

// arsivDBDosyalari: cikarilmis tmp icindeki DB dump'larini (ad -> sql yolu) haritalar.
// Yeni format: tmp/__db__/<ad>.sql. Eski format: tmp/<herhangi>.sql -> <sk>_main.
func arsivDBDosyalari(tmp, sk string) map[string]string {
	out := map[string]string{}
	dbDir := filepath.Join(tmp, "__db__")
	if fi, err := os.Stat(dbDir); err == nil && fi.IsDir() {
		ents, _ := os.ReadDir(dbDir)
		for _, e := range ents {
			// kullanicilar.sql bir DB dump'i DEGILDIR; "kullanicilar" adinda bir
			// veritabani olusturmaya calismak geri yuklemeyi kirardi.
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") || e.Name() == kullaniciDosyaAdi {
				continue
			}
			out[strings.TrimSuffix(e.Name(), ".sql")] = filepath.Join(dbDir, e.Name())
		}
		if len(out) > 0 {
			return out
		}
	}
	// eski format: kok seviyesinde tek .sql -> ana DB
	ents, _ := os.ReadDir(tmp)
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		out[sk+"_main"] = filepath.Join(tmp, e.Name())
		break
	}
	return out
}

// dbImport: bir .sql dosyasini verilen (whitelist'ten gecmis) DB'ye import eder.
func dbImport(dbName, sqlPath string) error {
	// 🔴 Hedef DB yoksa OLUSTUR. Eskiden dogrudan `mysql <ad> < dump` calisiyordu;
	// veritabani silinmisse "Unknown database" ile patliyordu — yani tam da geri
	// yuklemeye EN COK ihtiyac duyulan durumda (DB kaybi) restore imkansizdi.
	// Ad zaten GecerliDBKimlik + sistemDBmi + sahiplik kapisindan gecmis olur.
	if out, err := exec.Command("bash", "-c",
		fmt.Sprintf("mysql -e %s", shq("CREATE DATABASE IF NOT EXISTS `"+dbName+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"))).CombinedOutput(); err != nil {
		return fmt.Errorf("veritabanı oluşturulamadı: %s", strings.TrimSpace(string(out)))
	}
	cmd := exec.Command("bash", "-c", fmt.Sprintf("mysql %s < %s 2>&1", shq(dbName), shq(sqlPath)))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// tumDBGeriYukle: arsivdeki DB'lerden domaine ait olanlari import eder (mod=tam / veritabani).
// filter != "" ise yalniz o DB. Domaine ait olmayan / sistem DB'leri atlanir.
func tumDBGeriYukle(db *sql.DB, domainID int64, tmp, sk, filter string) []map[string]string {
	files := arsivDBDosyalari(tmp, sk)
	sahip := map[string]bool{}
	for _, n := range domainDBleri(db, domainID, sk) {
		sahip[n] = true
	}
	res := []map[string]string{}
	idx := map[string]int{} // db adi -> res icindeki konum (sonradan not eklemek icin)
	yuklenen := []string{}  // gercekten import edilen veritabanlari
	for name, p := range files {
		if filter != "" && name != filter {
			continue
		}
		if sistemDBmi(name) {
			res = append(res, map[string]string{"db": name, "durum": "reddedildi (sistem veritabanı)"})
			continue
		}
		yenidenKayit := false
		if !sahip[name] {
			// 🔴 KURTARMA YOLU: panel kaydi silinmis DB'yi de geri yukleyebilmeliyiz.
			// Operator veritabanini panelden silince db_accounts satiri da gider;
			// whitelist bosalir ve "kendi yedeginden kendi DB'sini" geri yuklemek
			// IMKANSIZ hale gelirdi (uretimde yasandi: is "tamam" dedi ama sifir DB
			// geri yuklendi). Arsiv zaten BU domainin yedegi oldugu icin icindeki DB
			// adi tanim geregi bu domaine aittir. Tek gercek risk, adin BASKA bir
			// domaine kayitli olmasi — o durumda REDDEDILIR.
			if baskaDomaininMi(db, name, domainID) {
				res = append(res, map[string]string{"db": name, "durum": "reddedildi (başka bir domaine kayıtlı)"})
				continue
			}
			yenidenKayit = true
		}
		if err := dbImport(name, p); err != nil {
			res = append(res, map[string]string{"db": name, "durum": "hata: " + err.Error()})
			continue
		}
		durum := "geri yüklendi"
		if yenidenKayit {
			// Panele geri kaydet: aksi halde bu DB bir daha YEDEKLENMEZ
			// (domainDBleri yalniz db_accounts + <sk>_main bakar).
			// db_user ve db_pass_plain NOT NULL ve DEFAULT'suz; sql_mode
			// STRICT_TRANS_TABLES oldugu icin bu sutunlari atlayan INSERT DUSER
			// (olculdu). Bos string veriyoruz: DB kullanicisi/parolasi yedekte
			// BULUNMAZ, operator panelden tanimlar.
			if _, e := db.Exec(
				`INSERT INTO db_accounts (domain_id, db_name, db_user, db_pass_plain, notlar) VALUES (?,?,'','',?)`,
				domainID, name, "geri yüklemede yeniden kaydedildi"); e == nil {
				durum = "geri yüklendi (panel kaydı yeniden oluşturuldu — kullanıcı/parola tanımlayın)"
			} else {
				durum = "geri yüklendi (panelde kayıtlı değil — Veritabanları bölümünden ekleyin)"
			}
		}
		idx[name] = len(res)
		yuklenen = append(yuklenen, name)
		res = append(res, map[string]string{"db": name, "durum": durum})
	}

	// 🔴 KIMLIK GERI YUKLEME. Semayi ve veriyi geri getirmek siteyi ayaga
	// KALDIRMAZ; baglanacak MySQL kullanicisi da gerekir. Iki kaynak denenir:
	//  (A) arsivdeki kullanicilar.sql (yeni yedekler) — parola hash'iyle birebir,
	//  (B) yoksa sitenin kendi yapilandirmasi (wp-config.php/.env) — eski
	//      arsivler icin. Ikisi de yoksa durum degismez, operator panelden
	//      "Kullanici Olustur" ile tanimlar.
	if len(yuklenen) > 0 {
		izin := map[string]bool{}
		for _, n := range yuklenen {
			izin[n] = true
		}
		if n, err := dbKullanicilariUygula(filepath.Join(tmp, "__db__"), izin); err == nil && n > 0 {
			log.Printf("restore %s: %d kullanici/yetki ifadesi uygulandi", sk, n)
		}
		for _, name := range yuklenen {
			if ek := kimlikTamamla(db, domainID, sk, name); ek != "" {
				i := idx[name]
				res[i]["durum"] = res[i]["durum"] + " — " + ek
			}
		}
	}
	return res
}

// baskaDomaininMi: db_name BASKA bir domaine kayitli mi (cross-tenant koruma).
func baskaDomaininMi(db *sql.DB, dbName string, domainID int64) bool {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM db_accounts WHERE db_name=? AND domain_id<>?`,
		dbName, domainID).Scan(&n); err != nil {
		return true // olcemiyorsak REDDET (fail-closed)
	}
	return n > 0
}

// DBSonucOzeti: tumDBGeriYukle sonucunu sayilara ve okunur mesaja cevirir.
// 🔴 Bu ozet OLMADAN cagiran katman sonucu atiyordu ve "veritabanları geri
// yüklendi" diyordu — SIFIR DB geri yuklenmis olsa bile. Basarisizligin guven
// olarak render edilmesi tam olarak buydu.
func DBSonucOzeti(res []map[string]string) (yuklenen, atlanan, hatali int, mesaj string) {
	var p []string
	for _, r := range res {
		d := r["durum"]
		switch {
		case strings.HasPrefix(d, "geri yüklendi"):
			yuklenen++
		case strings.HasPrefix(d, "hata"):
			hatali++
		default:
			atlanan++
		}
		p = append(p, r["db"]+": "+d)
	}
	return yuklenen, atlanan, hatali, strings.Join(p, " | ")
}

// birDBGeriYukle: tek DB'yi ya orijinal ustune (hedefDB boş) ya da YENİ ada geri yükler (mod=db).
func birDBGeriYukle(db *sql.DB, domainID int64, tmp, sk, srcDB, hedefDB string) (string, error) {
	files := arsivDBDosyalari(tmp, sk)
	sqlPath, ok := files[srcDB]
	if !ok {
		return "", fmt.Errorf("yedekte ‘%s’ veritabanı yok", srcDB)
	}
	sahip := map[string]bool{}
	for _, n := range domainDBleri(db, domainID, sk) {
		sahip[n] = true
	}
	if hedefDB == "" || hedefDB == srcDB {
		if sistemDBmi(srcDB) || !sahip[srcDB] {
			return "", fmt.Errorf("‘%s’ bu domaine ait değil", srcDB)
		}
		if err := dbImport(srcDB, sqlPath); err != nil {
			return "", err
		}
		return "‘" + srcDB + "’ üzerine geri yüklendi", nil
	}
	// YENİ hedef DB: tenant önekli, geçerli, sistem-dışı olmalı (non-destructive).
	if !hesaplar.GecerliDBKimlik(hedefDB) || sistemDBmi(hedefDB) || !strings.HasPrefix(hedefDB, sk+"_") {
		return "", fmt.Errorf("geçersiz hedef adı — ‘%s_’ ile başlamalı", sk)
	}
	if sahip[hedefDB] {
		return "", fmt.Errorf("‘%s’ zaten var; üzerine yazmak için hedefi boş bırakın", hedefDB)
	}
	dbUser := tenantAnaKullanici(db, domainID, sk)
	if err := hesaplar.MySQLCreateDBForUser(db, domainID, hedefDB, dbUser); err != nil {
		return "", fmt.Errorf("hedef DB oluşturulamadı: %w", err)
	}
	if err := dbImport(hedefDB, sqlPath); err != nil {
		return "", err
	}
	return "yeni ‘" + hedefDB + "’ veritabanına geri yüklendi", nil
}

// secilenDosyalariGeriYukle: arsivden yalniz secilen yollari kopyalar.
// hedef=klasor → /home/<sk>/geri-yukleme-<stamp>/ (hicbir sey ezilmez, önerilen).
// hedef=yerinde → orijinal konuma (yalniz secilenler; tum home ezilmez).
func secilenDosyalariGeriYukle(tmp, sk string, yollar []string, hedef string) (int, string, error) {
	src := filepath.Join(tmp, sk)
	kokHedef := "/home/" + sk
	altKlasor := ""
	if hedef != "yerinde" {
		altKlasor = "geri-yukleme-" + time.Now().Format("20060102-150405")
		kokHedef = filepath.Join("/home/"+sk, altKlasor)
		if err := os.MkdirAll(kokHedef, 0755); err != nil {
			return 0, "", fmt.Errorf("hedef klasör: %w", err)
		}
	}
	n := 0
	for _, y := range yollar {
		rel, ok := guvenliUyeYolu(y)
		if !ok {
			continue
		}
		s := filepath.Join(src, rel)
		if s != src && !strings.HasPrefix(s, src+string(os.PathSeparator)) {
			continue
		}
		if _, err := os.Lstat(s); err != nil {
			continue
		}
		d := filepath.Join(kokHedef, rel)
		if err := os.MkdirAll(filepath.Dir(d), 0755); err != nil {
			continue
		}
		if _, err := exec.Command("cp", "-a", s, d).CombinedOutput(); err != nil {
			continue
		}
		n++
	}
	_, _ = exec.Command("chown", "-R", sk+":"+sk, kokHedef).CombinedOutput()
	_, _ = exec.Command("restorecon", "-R", kokHedef).CombinedOutput()
	return n, altKlasor, nil
}

// yolKacis: göreli yol arşiv kökünü aşıyor mu? (Clean sonrası ".." ile başlıyorsa).
func yolKacis(p string) bool {
	c := filepath.Clean(p)
	return c == ".." || strings.HasPrefix(c, "../")
}

// restoreArsivTara: restore-özel güvenlik ön-taraması. archivex.Tara'nın aksine iç
// göreli symlink/hardlink'e İZİN verir (npm/composer projeleri); YALNIZ jail-kaçışını
// (mutlak veya ..-taşan symlink/hardlink hedefi, mutlak/.. üye yolu) + aygıt/fifo
// üyelerini reddeder. Arşivler root-üretimi güvenilir yedeklerdir; bu ek savunmadır.
func restoreArsivTara(abs string) error {
	f, err := os.Open(abs)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hd, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return fmt.Errorf("arşiv okunamadı: %w", e)
		}
		name := strings.TrimPrefix(hd.Name, "./")
		if filepath.IsAbs(hd.Name) || yolKacis(name) {
			return fmt.Errorf("güvenlik: geçersiz üye yolu: %s", hd.Name)
		}
		switch hd.Typeflag {
		case tar.TypeSymlink:
			if filepath.IsAbs(hd.Linkname) {
				return fmt.Errorf("güvenlik: mutlak symlink hedefi reddedildi: %s -> %s", hd.Name, hd.Linkname)
			}
			// hedefi symlink'in bulunduğu dizine göre çöz; arşiv kökünü aşamaz.
			if yolKacis(filepath.Join(filepath.Dir(name), hd.Linkname)) {
				return fmt.Errorf("güvenlik: arşiv dışına symlink reddedildi: %s -> %s", hd.Name, hd.Linkname)
			}
		case tar.TypeLink:
			if filepath.IsAbs(hd.Linkname) || yolKacis(strings.TrimPrefix(hd.Linkname, "./")) {
				return fmt.Errorf("güvenlik: geçersiz hardlink reddedildi: %s -> %s", hd.Name, hd.Linkname)
			}
		case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
			return fmt.Errorf("güvenlik: aygıt/fifo üyesi reddedildi: %s", hd.Name)
		}
	}
	return nil
}

// arsivUyeListesi: arsivin tum uye adlarini (salt-okunur) doner.
func arsivUyeListesi(abs string) ([]string, error) {
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var uyeler []string
	for {
		hd, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return uyeler, nil
		}
		uyeler = append(uyeler, strings.TrimPrefix(hd.Name, "./"))
	}
	return uyeler, nil
}

// cikarUyeleri: moda gore arsivden cikarilacak ust-duzey uyeleri hesaplar.
// hasHome → "<sk>", DB icin "__db__" (veya eski format kok .sql'leri).
func cikarUyeleri(mod, sk string, tumUyeler, yollar []string) []string {
	hasHome, hasDbDir := false, false
	legacySql := []string{}
	for _, m := range tumUyeler {
		if m == sk || strings.HasPrefix(m, sk+"/") {
			hasHome = true
		}
		if strings.HasPrefix(m, "__db__/") {
			hasDbDir = true
		}
		if strings.HasSuffix(m, ".sql") && !strings.Contains(strings.TrimSuffix(m, "/"), "/") {
			legacySql = append(legacySql, m)
		}
	}
	dbUye := []string{}
	if hasDbDir {
		dbUye = []string{"__db__"}
	} else {
		dbUye = legacySql
	}
	switch mod {
	case "dosyalar":
		if hasHome {
			return []string{sk}
		}
		return nil
	case "veritabani", "db":
		return dbUye
	case "tam":
		r := []string{}
		if hasHome {
			r = append(r, sk)
		}
		return append(r, dbUye...)
	case "dosya":
		r := []string{}
		for _, y := range yollar {
			if rel, ok := guvenliUyeYolu(y); ok {
				r = append(r, sk+"/"+rel)
			}
		}
		return r
	}
	return nil
}

// arsivUyeCikarRoot: SADECE verilen uyeleri ROOT olarak destDir'e cikarir.
// Kota-dostu (root uid kotasiz) — tenant home'unun 2. kopyasi tenant kotasini
// asmadan sahnelenebilir. GUVENLIK: once archivex.Tara ile uye on-taramasi
// (symlink/hardlink/jail-disi uye reddi); arsivler zaten root-uretimi guvenilir
// yedeklerdir (BackupRoot 0700).
func arsivUyeCikarRoot(abs, destDir string, uyeler []string) (string, error) {
	if len(uyeler) == 0 {
		return "", fmt.Errorf("çıkarılacak üye yok")
	}
	tur := archivex.TuruBelirle(abs)
	if tur == archivex.TurBilinmeyen {
		return "", fmt.Errorf("desteklenmeyen arşiv")
	}
	// archivex.Tara TÜM symlink/hardlink'leri reddeder → node_modules/vendor gibi
	// MEŞRU iç symlink'leri olan siteler restore edilemezdi. Restore-özel tarama:
	// yalnız ARŞİV DIŞINA kaçan (mutlak veya ..-taşan) symlink/hardlink + aygıt/fifo
	// üyeleri reddedilir; iç göreli symlink'lere izin verilir (tenant kendi jail'ine geri yüklenir).
	if err := restoreArsivTara(abs); err != nil {
		return "", err
	}
	args := append([]string{"-xz", "-f", abs, "-C", destDir}, uyeler...)
	out, err := exec.Command("tar", args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("üye çıkarma: %w", err)
	}
	return string(out), nil
}

// homeGeriYukle: tum home'u geri yukler. temiz=false → EZMEZ (yedekte olmayan aktif
// dosyalar korunur, yalniz yedektekiler ustune yazilir). temiz=true → rsync --delete
// (yedekteki tam durum; ESKI davranis, tehlikeli).
func homeGeriYukle(tmp, sk string, temiz bool) {
	extractedHome := filepath.Join(tmp, sk)
	if _, err := os.Stat(extractedHome); err != nil {
		return
	}
	args := []string{"-a"}
	if temiz {
		args = append(args, "--delete")
	}
	args = append(args, extractedHome+"/", "/home/"+sk+"/")
	if out, err := exec.Command("rsync", args...).CombinedOutput(); err != nil {
		_ = out
		_, _ = exec.Command("cp", "-af", extractedHome+"/.", "/home/"+sk+"/").CombinedOutput()
	}
	_, _ = exec.Command("chown", "-R", sk+":"+sk, "/home/"+sk).CombinedOutput()
	_, _ = exec.Command("restorecon", "-R", "/home/"+sk).CombinedOutput()
}
