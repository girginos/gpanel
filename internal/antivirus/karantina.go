package antivirus

// Karantina yönetimi — liste, geri yükle, sil, incele.
//
// 🔴 NEDEN: karantina tek yön değildir. Yanlış pozitif GERÇEK: obfuscated
// meşru bir eklenti webshell'e benzer. Geri yükleme olmadan karantina, kalıcı
// veri kaybı riskidir ve operatör özelliği kapatır. Bu dosya karantinayı
// GERİ ALINABİLİR bir işleme çevirir.

import (
	"errors"
	"log"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/sys/unix"

	"girginospanel/internal/httpx"
)

type karantinaKayit struct {
	ID           int64  `json:"id"`
	OrijinalYol  string `json:"orijinal_yol"`
	KarantinaYol string `json:"karantina_yol"`
	Imza         string `json:"imza"`
	Seviye       string `json:"seviye"`
	Puan         int    `json:"puan"`
	Durum        string `json:"durum"`
	Tarih        string `json:"tarih"`
	Boyut        int64  `json:"boyut"`
	Mevcut       bool   `json:"mevcut"` // karantina dosyası hâlâ diskte mi
}

// KarantinaListe — GET /domains/{id}/antivirus/karantina/liste
func (h *Handlers) KarantinaListe(w http.ResponseWriter, r *http.Request) {
	id, sk, _, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	// Kiracı çözümlenemezse hiçbir kayıt "mevcut" gösterilmez (fail-closed).
	uid, _, uok := kiraciUID(sk)
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, orijinal_yol, karantina_yol, imza, seviye, puan, durum,
		        DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s')
		   FROM av_bulgular
		  WHERE domain_id=? AND durum IN ('karantina','geri_yuklendi','silindi')
		  ORDER BY created_at DESC LIMIT 500`, id)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []karantinaKayit{}
	for rows.Next() {
		var k karantinaKayit
		if rows.Scan(&k.ID, &k.OrijinalYol, &k.KarantinaYol, &k.Imza,
			&k.Seviye, &k.Puan, &k.Durum, &k.Tarih) != nil {
			continue
		}
		// 🔴 os.Stat YOK: karantina yolu kiracının evindedir ve kiracı girdiyi
		// (ya da .karantina'nın kendisini) symlink yapabilir; root'un izleyen
		// stat'ı ev dışındaki herhangi bir yol için var/boyut kâhini olurdu.
		if uok && k.KarantinaYol != "" {
			k.Mevcut, k.Boyut = karantinaBilgi("/home/"+sk, k.KarantinaYol, uid)
		}
		out = append(out, k)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"kayitlar": out})
}

// bulguSahipMi — bid bu domaine ait mi + karantina yollarını döner.
// 🔴 Cross-user koruması: bulgu domain_id'si isteğin domain'iyle eşleşmeli VE
// yollar /home/<sk>/ içinde olmalı (path-traversal + başka müşteriye erişim).
func (h *Handlers) bulguSahipMi(r *http.Request, sk string, id int64) (bid int64, orij, kar, durum string, ok bool) {
	bid, _ = strconv.ParseInt(chi.URLParam(r, "bid"), 10, 64)
	if bid == 0 {
		return 0, "", "", "", false
	}
	var domID int64
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT domain_id, orijinal_yol, karantina_yol, durum FROM av_bulgular WHERE id=?`, bid).
		Scan(&domID, &orij, &kar, &durum)
	if err != nil || domID != id {
		return 0, "", "", "", false
	}
	home := "/home/" + sk + "/"
	// Her iki yol da bu kiracının evinde olmalı.
	if orij != "" && !strings.HasPrefix(filepath.Clean(orij)+"/", home) &&
		filepath.Clean(orij) != "/home/"+sk {
		return 0, "", "", "", false
	}
	if kar != "" && !strings.HasPrefix(filepath.Clean(kar)+"/", home) {
		return 0, "", "", "", false
	}
	return bid, orij, kar, durum, true
}

// KarantinaGeriYukle — POST .../karantina/{bid}/geri-yukle
// 🔴 Yanlış pozitif kurtarma: karantinadaki dosyayı orijinal yerine taşır.
func (h *Handlers) KarantinaGeriYukle(w http.ResponseWriter, r *http.Request) {
	id, sk, demo, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kullanılamaz")
		return
	}
	bid, orij, kar, durum, ok := h.bulguSahipMi(r, sk, id)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "bulgu bu domaine ait değil")
		return
	}
	if durum != "karantina" {
		httpx.WriteError(w, http.StatusBadRequest, "bu bulgu karantinada değil (durum: "+durum+")")
		return
	}
	if kar == "" || orij == "" {
		httpx.WriteError(w, http.StatusBadRequest, "karantina/orijinal yol kayıtlı değil — geri yüklenemez")
		return
	}
	// Varlık denetimi guvenliGeriTasi içinde, pinlenmiş dir-fd'ye göre yapılır
	// (ENOENT → aşağıda 400). Burada os.Lstat(kar) ara bileşenleri izlerdi.
	uid, gid, uok := kiraciUID(sk)
	if !uok {
		httpx.WriteError(w, http.StatusInternalServerError, "kiracı kullanıcı çözümlenemedi")
		return
	}
	// 🔴 SYMLINK-GÜVENLİ geri yükleme. Panel-server ROOT çalışır ve orij yolu
	// TAMAMEN kiracının mülkündedir (/home/<sk>/public_html/...). os.Rename ara
	// dizin bileşenlerindeki symlink'leri İZLER (CWE-59) ve os.Stat↔os.Rename
	// arası TOCTOU (CWE-367) vardır: kiracı bir ara bileşeni başka kiracının
	// public_html'ine symlink yapıp root'a ev DIŞINA yazdırabilir → cross-tenant
	// RCE. Çözüm: openat(O_NOFOLLOW) zinciriyle hedefi kiracı evine ÇAPALA, tüm
	// FS işlemlerini pinlenmiş dir-fd'lere göre (renameat/fchmodat/fchownat) yap.
	home := "/home/" + sk
	if err := guvenliGeriTasi(home, kar, orij, uid, gid); err != nil {
		switch {
		case errors.Is(err, os.ErrExist):
			httpx.WriteError(w, http.StatusConflict, "orijinal konumda zaten bir dosya var — elle çözün")
		case errors.Is(err, os.ErrNotExist):
			httpx.WriteError(w, http.StatusBadRequest, "karantina dosyası bulunamadı (elle silinmiş olabilir)")
		case errors.Is(err, errGuvenliYol):
			httpx.WriteError(w, http.StatusBadRequest, "yol güvenli değil (symlink/ev dışı/kiracıya ait olmayan dosya) — geri yüklenmedi")
		default:
			httpx.WriteError(w, http.StatusInternalServerError, "geri yüklenemedi: "+err.Error())
		}
		return
	}
	if _, err := h.DB.Exec(`UPDATE av_bulgular SET durum='geri_yuklendi', karantina=0 WHERE id=?`, bid); err != nil {
		log.Printf("antivirus KarantinaGeriYukle: durum güncellenemedi (bid=%d): %v — dosya geri yüklendi ama panel karantinada gösterebilir", bid, err)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "geri_yuklendi": orij})
}

// KarantinaSil — POST .../karantina/{bid}/sil
// 🔴 Kalıcı silme: karantinadaki dosyayı diskten kaldırır (temizlik).
func (h *Handlers) KarantinaSil(w http.ResponseWriter, r *http.Request) {
	id, sk, demo, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	if demo {
		httpx.WriteError(w, http.StatusForbidden, "demo aboneliğinde kullanılamaz")
		return
	}
	bid, _, kar, _, ok := h.bulguSahipMi(r, sk, id)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "bulgu bu domaine ait değil")
		return
	}
	if kar != "" {
		if err := guvenliSil("/home/"+sk, kar); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "silinemedi: "+err.Error())
			return
		}
	}
	if _, err := h.DB.Exec(`UPDATE av_bulgular SET durum='silindi', karantina=0 WHERE id=?`, bid); err != nil {
		log.Printf("antivirus KarantinaSil: durum güncellenemedi (bid=%d): %v — dosya silindi ama panel karantinada gösterebilir", bid, err)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "silindi": true})
}

// KarantinaIncele — GET .../karantina/{bid}/incele
// 🔴 GÜVENLİ İNCELEME: dosyayı ASLA ÇALIŞTIRMADAN, ilk 64 KB'ını düz metin
// döner. Operatör yanlış pozitif mi gerçek mi kendi görür. İçerik JSON string
// olarak döner (tarayıcı çalıştırmaz).
func (h *Handlers) KarantinaIncele(w http.ResponseWriter, r *http.Request) {
	id, sk, _, ok := h.domain(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "domain bulunamadı")
		return
	}
	_, _, kar, _, ok := h.bulguSahipMi(r, sk, id)
	if !ok || kar == "" {
		httpx.WriteError(w, http.StatusForbidden, "bulgu bu domaine ait değil")
		return
	}
	uid, _, uok := kiraciUID(sk)
	if !uok {
		httpx.WriteError(w, http.StatusInternalServerError, "kiracı kullanıcı çözümlenemedi")
		return
	}
	f, err := guvenliAc("/home/"+sk, kar, uid)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "dosya açılamadı (silinmiş olabilir)")
		return
	}
	defer f.Close()
	buf := make([]byte, 64<<10)
	n, _ := f.Read(buf)
	icerik := buf[:n]
	// İkili dosya mı? NUL varsa hex özet ver, ham değil.
	if hasNUL(icerik) {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"ikili":  true,
			"boyut":  n,
			"icerik": "[ikili dosya — metin gösterilemiyor]",
		})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ikili":  false,
		"boyut":  n,
		"kesik":  n == len(buf),
		"icerik": string(icerik),
	})
}

func hasNUL(b []byte) bool {
	for _, c := range b {
		if c == 0 {
			return true
		}
	}
	return false
}

var errGuvenliYol = errors.New("guvenli-olmayan yol (symlink/ev disi)")

// kiraciUID — sk kullanıcısının uid/gid'i (fail-closed).
func kiraciUID(sk string) (int, int, bool) {
	u, err := user.Lookup(sk)
	if err != nil {
		return 0, 0, false
	}
	uid, e1 := strconv.Atoi(u.Uid)
	gid, e2 := strconv.Atoi(u.Gid)
	if e1 != nil || e2 != nil {
		return 0, 0, false
	}
	return uid, gid, true
}

// evAltDirAc — home kökünden hedefDir'e bileşen bileşen O_NOFOLLOW ile iner.
// Bir bileşen symlink ise (ELOOP) ya da hedef home dışına çıkıyorsa errGuvenliYol
// döner. eksikOlustur=true ise olmayan ara dizinleri kiracı uid'iyle üretir.
// Dönen fd hedefDir'in GERÇEK (symlink'siz) halidir; çağıran KAPATMALI.
func evAltDirAc(home, hedefDir string, uid, gid int, eksikOlustur bool) (int, error) {
	home = filepath.Clean(home)
	hedefDir = filepath.Clean(hedefDir)
	if hedefDir != home && !strings.HasPrefix(hedefDir+"/", home+"/") {
		return -1, errGuvenliYol
	}
	// home güven çapasıdır (root-yönetimli); NOFOLLOW olmadan açılır.
	dirFd, err := unix.Open(home, unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	if hedefDir == home {
		return dirFd, nil
	}
	rel, err := filepath.Rel(home, hedefDir)
	if err != nil {
		unix.Close(dirFd)
		return -1, errGuvenliYol
	}
	for _, parca := range strings.Split(rel, "/") {
		if parca == "" || parca == "." || parca == ".." {
			unix.Close(dirFd)
			return -1, errGuvenliYol
		}
		next, err := unix.Openat(dirFd, parca, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			if eksikOlustur && errors.Is(err, unix.ENOENT) {
				if e := unix.Mkdirat(dirFd, parca, 0o755); e != nil {
					unix.Close(dirFd)
					return -1, e
				}
				next, err = unix.Openat(dirFd, parca, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
				// Sahiplik ADA göre değil AÇILMIŞ fd'ye göre verilir: mkdirat ile
				// fchownat arasında kiracı adı başka bir nesneyle (ör. hard-link)
				// değiştirse bile chown yalnız O_DIRECTORY|O_NOFOLLOW ile açılan
				// dizine uygulanır.
				if err == nil {
					_ = unix.Fchown(next, uid, gid)
				}
			}
			if err != nil {
				unix.Close(dirFd)
				// ELOOP: bileşen symlink (O_NOFOLLOW). ENOTDIR: bileşen dizin değil
				// ya da symlink-to-dir O_DIRECTORY ile reddedildi. İkisi de
				// symlink/kötü-yol saldırısı işaretidir → güvenli-değil.
				if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
					return -1, errGuvenliYol
				}
				return -1, err
			}
		}
		unix.Close(dirFd)
		dirFd = next
	}
	return dirFd, nil
}

// yarisKancasi — YALNIZ testler için: rename ile izin/sahiplik işlemi arasındaki
// yarış penceresini (kiracının hedef adı symlink'le değiştirmesi) belirlenimci
// olarak yeniden üretmeye yarar. Üretimde nil'dir.
var yarisKancasi func(asama string)

// kiraciDosyaAc — dirFd altındaki ad'ı SYMLINK İZLEMEDEN açar ve yalnız
// KİRACIYA AİT (st_uid == uid) DÜZENLİ dosyayı kabul eder; aksi errGuvenliYol.
//
// 🔴 NEDEN sahiplik: .karantina'nın üst dizini (/home/<sk>) kiracıya aittir;
// kiracı .karantina'yı yeniden adlandırıp kendi dizinini koyabilir ve içine
// evindeki root'a ait bir dosyayı (ör. root:root 0640 yapılandırma) taşıyabilir.
// Panel root olarak böyle bir inode'u okur/chown'lar/chmod'larsa kiracıya ait
// olmayanı ona verir. Yalnız kiracının kendi inode'u üzerinde işlem yapmak, root
// işleminin kiracının zaten yapabileceğinden fazlasını yapamamasını sağlar.
// O_NONBLOCK: ad bir FIFO ise açılış sonsuza dek beklemesin (S_ISREG ile reddedilir).
func kiraciDosyaAc(dirFd int, ad string, uid int) (int, unix.Stat_t, error) {
	var st unix.Stat_t
	fd, err := unix.Openat(dirFd, ad, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		// ELOOP: son bileşen symlink. ENXIO: soket. İkisi de kötü-yol işaretidir.
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) || errors.Is(err, unix.ENXIO) {
			return -1, st, errGuvenliYol
		}
		return -1, st, err
	}
	if err := unix.Fstat(fd, &st); err != nil {
		unix.Close(fd)
		return -1, st, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || int(st.Uid) != uid {
		unix.Close(fd)
		return -1, st, errGuvenliYol
	}
	return fd, st, nil
}

// guvenliGeriTasi — kar dosyasını orij'e symlink-güvenli taşır. Root ayrıcalığı
// altında bile kiracı-denetimli yol üzerinden ev DIŞINA yazımı engeller: hem
// kaynak hem hedef dizin openat(O_NOFOLLOW) ile kiracı evine çapalanır, taşıma
// pinlenmiş dir-fd'lere göre yapılır.
//
// 🔴 İzin/sahiplik YOLA göre DEĞİL, taşımadan ÖNCE açılıp doğrulanmış dosya
// fd'sine göre verilir. Eski kod Fchmodat(destFd, destBase, 0644, 0) kullanıyordu:
// flags=0 düz fchmodat son bileşendeki symlink'i İZLER. Karantina girdisi symlink
// ise (kiracı .karantina'yı denetler) ya da rename ile chmod arasında kiracı
// hedef adı symlink'le değiştirirse root, ev dışındaki bir dosyayı 0644 yapıyordu.
func guvenliGeriTasi(home, kar, orij string, uid, gid int) error {
	destDir := filepath.Dir(orij)
	destBase := filepath.Base(orij)
	karDir := filepath.Dir(kar)
	karBase := filepath.Base(kar)

	// 1) Kaynağı sabitle: kiracının .karantina'sındaki GERÇEK, kiracıya ait
	// düzenli dosya (symlink/FIFO/dizin/başkasının dosyası reddedilir).
	karFd, err := evAltDirAc(home, karDir, uid, gid, false)
	if err != nil {
		return err
	}
	defer unix.Close(karFd)
	srcFd, _, err := kiraciDosyaAc(karFd, karBase, uid)
	if err != nil {
		return err
	}
	defer unix.Close(srcFd)

	// 2) Hedef dizin (eksik ara dizinler kiracı adına oluşturulur).
	destFd, err := evAltDirAc(home, destDir, uid, gid, true)
	if err != nil {
		return err
	}
	defer unix.Close(destFd)

	// Çakışma: hedef zaten var mı (symlink izlemeden, pinlenmiş fd'ye göre).
	var st unix.Stat_t
	if unix.Fstatat(destFd, destBase, &st, unix.AT_SYMLINK_NOFOLLOW) == nil {
		return os.ErrExist
	}

	// 3) Taşı (renameat son bileşeni izlemez).
	if err := unix.Renameat(karFd, karBase, destFd, destBase); err != nil {
		return err
	}
	if yarisKancasi != nil {
		yarisKancasi("geri-tasi")
	}
	// 4) Sahiplik + izin: doğrulanmış inode'un fd'si üzerinden. Hedef adda şu an
	// ne olduğu (kiracının koyduğu bir symlink dahil) önemsizdir. chown, setuid/
	// setgid bitlerini temizler; ardından 0644.
	if err := unix.Fchown(srcFd, uid, gid); err != nil {
		log.Printf("antivirus guvenliGeriTasi: sahiplik verilemedi (%s): %v", orij, err)
	}
	if err := unix.Fchmod(srcFd, 0o644); err != nil {
		log.Printf("antivirus guvenliGeriTasi: izin verilemedi (%s): %v", orij, err)
	}
	return nil
}

// karantinaDizinAc — <home>/.karantina'yı SYMLINK İZLEMEDEN açar; yoksa root'a
// ait 0700 olarak oluşturur. .karantina symlink ise (kiracı değiştirmiş) ELOOP/
// ENOTDIR → errGuvenliYol. Dönen fd'yi çağıran KAPATMALI.
func karantinaDizinAc(home string) (int, error) {
	homeFd, err := evAltDirAc(home, home, 0, 0, false)
	if err != nil {
		return -1, err
	}
	defer unix.Close(homeFd)
	const ad = ".karantina"
	fd, err := unix.Openat(homeFd, ad, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		if e := unix.Mkdirat(homeFd, ad, 0o700); e != nil && !errors.Is(e, unix.EEXIST) {
			return -1, e
		}
		fd, err = unix.Openat(homeFd, ad, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	}
	if err != nil {
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			return -1, errGuvenliYol
		}
		return -1, err
	}
	return fd, nil
}

// karantinayaTasi — kaynak dosyayı (kiracı evinde) <home>/.karantina'ya taşır ve
// izinlerini 0000 yapar; karantina yolunu döner.
//
// 🔴 Eski el ile karantina yolu root olarak os.Lstat → os.MkdirAll → os.Rename →
// os.Chmod çağırıyordu: Lstat yalnız son bileşeni denetler (symlink'i REDDETMEZ,
// yalnız dizini), MkdirAll/Rename ara bileşenleri ve .karantina'nın kendisini
// izler, os.Chmod ise taşınan symlink'i İZLER. Kiracı public_html'inde ev dışını
// gösteren bir symlink'i karantinaya aldırarak root'a herhangi bir dosyayı
// (ör. /etc/passwd) chmod 0 yaptırabiliyordu. Artık: dizinler O_NOFOLLOW zinciriyle
// pinlenir, kaynak kiracıya ait düzenli dosya olarak AÇILIP doğrulanır, taşıma
// renameat ile, izin açık fd üzerinden fchmod ile verilir.
func karantinayaTasi(home, kaynak string, uid int, an time.Time) (string, error) {
	home = filepath.Clean(home)
	kaynak = filepath.Clean(kaynak)
	ad := filepath.Base(kaynak)

	srcDirFd, err := evAltDirAc(home, filepath.Dir(kaynak), 0, 0, false)
	if err != nil {
		return "", err
	}
	defer unix.Close(srcDirFd)
	fd, _, err := kiraciDosyaAc(srcDirFd, ad, uid)
	if err != nil {
		return "", err
	}
	defer unix.Close(fd)

	qFd, err := karantinaDizinAc(home)
	if err != nil {
		return "", err
	}
	defer unix.Close(qFd)

	hedefAd := an.Format("20060102_150405") + "_" + ad
	if err := unix.Renameat(srcDirFd, ad, qFd, hedefAd); err != nil { // aynı FS → atomik
		return "", err
	}
	if yarisKancasi != nil {
		yarisKancasi("karantina")
	}
	// Çalıştırılamaz/okunamaz: yol değil, doğrulanmış inode'un fd'si.
	if err := unix.Fchmod(fd, 0); err != nil {
		log.Printf("antivirus karantinayaTasi: izin 0000 yapılamadı (%s): %v", kaynak, err)
	}
	return filepath.Join(home, ".karantina", hedefAd), nil
}

// karantinaBilgi — kayıttaki karantina yolunu SYMLINK İZLEMEDEN yoklar. Yalnız
// kiracı evinde (O_NOFOLLOW dizin zinciri) ve kiracıya ait DÜZENLİ bir dosya ise
// (true, boyut) döner; symlink/başkasının dosyası/ev dışı → (false, 0).
func karantinaBilgi(home, yol string, uid int) (bool, int64) {
	dirFd, err := evAltDirAc(home, filepath.Dir(filepath.Clean(yol)), 0, 0, false)
	if err != nil {
		return false, 0
	}
	defer unix.Close(dirFd)
	var st unix.Stat_t
	if unix.Fstatat(dirFd, filepath.Base(yol), &st, unix.AT_SYMLINK_NOFOLLOW) != nil {
		return false, 0
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || int(st.Uid) != uid {
		return false, 0
	}
	return true, st.Size
}

// guvenliAc — kar dosyasini SYMLINK-GUVENLI acar. .karantina'nin ust dizini
// (/home/<sk>) kiraciya aittir; kiraci .karantina'yi silip yerine symlink
// koyabilir (CWE-59). evAltDirAc ile hedef kiraci evine capalanir + son bilesen
// kiraciDosyaAc ile O_NOFOLLOW acilir → symlink/FIFO/baskasinin dosyasi reddedilir
// (root'un kiraciya ait olmayan bir dosyayi ona okutmasi engellenir).
func guvenliAc(home, yol string, uid int) (*os.File, error) {
	dirFd, err := evAltDirAc(home, filepath.Dir(yol), 0, 0, false)
	if err != nil {
		return nil, err
	}
	defer unix.Close(dirFd)
	fd, _, err := kiraciDosyaAc(dirFd, filepath.Base(yol), uid)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), yol), nil
}

// guvenliSil — kar dosyasini SYMLINK-GUVENLI siler (pinlenmis dir-fd + Unlinkat;
// Unlinkat son bileseni ASLA izlemez → symlink'in kendisi silinir, hedefi degil).
func guvenliSil(home, yol string) error {
	dirFd, err := evAltDirAc(home, filepath.Dir(yol), 0, 0, false)
	if err != nil {
		return err
	}
	defer unix.Close(dirFd)
	if err := unix.Unlinkat(dirFd, filepath.Base(yol), 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return err
	}
	return nil
}
