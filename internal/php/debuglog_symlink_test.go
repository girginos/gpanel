package php

// debuglog_symlink_test.go — GET/DELETE /domains/{id}/php/debug-log panel (root)
// olarak kiracinin evindeki .gpanel/php_debug.log'u acar. /home/<sk> kiraci-sahipli
// (c_X:c_X 0710) GUVENILMEZ ust dizindir: kiraci .gpanel ya da php_debug.log yerine
// symlink / hardlink / FIFO / kendi dizinini koyarak root'a keyfi dosya OKUTMAMALI
// (komsu kiraci / sistem sirri sizintisi), KESTIRMEMELI (butunluk kaybi / DoS) ve
// istek goroutine'ini ASMAMALI. Tum senaryolar t.TempDir icinde kurulur; kiraci
// sahipligi chown ile taklit edildiginden root gerekir (degilse atlanir).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/sys/unix"

	"girginospanel/internal/sahtedb"
)

// dlKiraciUID — test kiracisinin uid/gid'i (ev dizininin sahibi).
const dlKiraciUID = 4242

// dlGizli — kurban dosyanin icerigi; yanitta gorunmesi = root olarak sizinti.
const dlGizli = "KURBAN-GIZLI-ICERIK-7f3a"

func dlMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// dlEvKur: base/home/c_test (kiraci sahipli 0710) + meşru .gpanel (root:root 0755)
// kurar ve debugLogEvKok'u base/home'a cevirir.
func dlEvKur(t *testing.T) (base, ev, gp string) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root gerekli: kiraci sahipligi chown ile taklit edilir")
	}
	base = t.TempDir()
	kok := filepath.Join(base, "home")
	ev = filepath.Join(kok, "c_test")
	dlEvDizini(t, ev)
	gp = filepath.Join(ev, ".gpanel")
	dlMust(t, os.Mkdir(gp, 0o755))
	dlMust(t, os.Chmod(gp, 0o755))
	eski := debugLogEvKok
	debugLogEvKok = kok
	t.Cleanup(func() { debugLogEvKok = eski })
	return base, ev, gp
}

// dlEvDizini: kiraci-sahipli 0710 ev dizini (hardenHomePerms ile ayni sahiplik).
func dlEvDizini(t *testing.T, ev string) {
	t.Helper()
	dlMust(t, os.MkdirAll(ev, 0o755))
	dlMust(t, os.Chown(ev, dlKiraciUID, dlKiraciUID))
	dlMust(t, os.Chmod(ev, 0o710))
}

// dlYaz: p'ye icerik yaz ve uid:uid sahipli 0644 yap.
func dlYaz(t *testing.T, p, icerik string, uid int) {
	t.Helper()
	dlMust(t, os.WriteFile(p, []byte(icerik), 0o644))
	dlMust(t, os.Chown(p, uid, uid))
	dlMust(t, os.Chmod(p, 0o644))
}

// dlKurban: evin DISINDA, uid sahipli, gizli icerikli kurban dosya.
func dlKurban(t *testing.T, base string, uid int) string {
	t.Helper()
	p := filepath.Join(base, fmt.Sprintf("kurban-%d", uid))
	dlYaz(t, p, dlGizli+"\n", uid)
	return p
}

// dlIstek: GET/DELETE handler'ini (sahtedb: domain 42 → c_test) cagirir.
func dlIstek(t *testing.T, method string) (int, string) {
	t.Helper()
	sd, db := sahtedb.Yeni()
	sd.Satirlar("SELECT sistem_kullanici FROM domains", []any{"c_test"})
	h := &Handlers{DB: db}
	r := chi.NewRouter()
	r.Get("/domains/{id}/php/debug-log", h.GetDebugLog)
	r.Delete("/domains/{id}/php/debug-log", h.ClearDebugLog)
	req := httptest.NewRequest(method, "/domains/42/php/debug-log", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// dlGetSatirlar: GET yanitindaki satirlar (200 beklenir).
func dlGetSatirlar(t *testing.T) []string {
	t.Helper()
	kod, govde := dlIstek(t, http.MethodGet)
	if kod != http.StatusOK {
		t.Fatalf("GET 200 beklenirdi: %d %s", kod, govde)
	}
	var m struct {
		Satirlar []string `json:"satirlar"`
	}
	dlMust(t, json.Unmarshal([]byte(govde), &m))
	return m.Satirlar
}

// TestDebugLog_MesruOkuVeKes — meşru kullanim bozulmamali: root:root 0755 .gpanel
// icindeki kiraci-sahipli duzenli log okunur, kuyruk (64KB/200 satir) korunur,
// DELETE ayni dosyayi (sahibi/turu degismeden) sifira keser; dosya/dizin yoksa bos.
func TestDebugLog_MesruOkuVeKes(t *testing.T) {
	_, ev, gp := dlEvKur(t)
	log := filepath.Join(gp, "php_debug.log")

	// .gpanel hic yok (debug hic acilmamis) → bos, DELETE sessiz ok
	dlMust(t, os.Remove(gp))
	if s := dlGetSatirlar(t); len(s) != 0 {
		t.Fatalf(".gpanel yokken bos liste beklenirdi: %q", s)
	}
	if kod, govde := dlIstek(t, http.MethodDelete); kod != http.StatusOK || !strings.Contains(govde, `"ok":true`) {
		t.Fatalf(".gpanel yokken DELETE ok beklenirdi: %d %s", kod, govde)
	}
	dlMust(t, os.Mkdir(gp, 0o755))
	dlMust(t, os.Chmod(gp, 0o755))
	if s := dlGetSatirlar(t); len(s) != 0 {
		t.Fatalf("log yokken bos liste beklenirdi: %q", s)
	}

	dlYaz(t, log, "satir1\nsatir2\n", dlKiraciUID)
	if s := dlGetSatirlar(t); strings.Join(s, "|") != "satir1|satir2" {
		t.Fatalf("meşru log okunamadi: %q", s)
	}
	if s := debugLogSatirlar(ev); strings.Join(s, "|") != "satir1|satir2" {
		t.Fatalf("debugLogSatirlar meşru log: %q", s)
	}

	// kuyruk: 300 satir x ~300B (>64KB) → son 200 satir, ilk kismi satir atilir
	var sb strings.Builder
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&sb, "%03d-%s\n", i, strings.Repeat("x", 296))
	}
	dlYaz(t, log, sb.String(), dlKiraciUID)
	s := dlGetSatirlar(t)
	if len(s) != 200 {
		t.Fatalf("kuyruk 200 satir olmali: n=%d", len(s))
	}
	if !strings.HasPrefix(s[0], "100-") || !strings.HasPrefix(s[199], "299-") {
		t.Fatalf("kuyruk bozuldu: ilk=%.8q son=%.8q", s[0], s[199])
	}

	if kod, govde := dlIstek(t, http.MethodDelete); kod != http.StatusOK || !strings.Contains(govde, `"ok":true`) {
		t.Fatalf("DELETE ok beklenirdi: %d %s", kod, govde)
	}
	st, err := os.Lstat(log)
	dlMust(t, err)
	sys := st.Sys().(*syscall.Stat_t)
	if !st.Mode().IsRegular() || st.Size() != 0 || sys.Uid != dlKiraciUID {
		t.Fatalf("meşru log kesilmeli (duzenli, kiraci sahipli, 0 bayt): mode=%v size=%d uid=%d", st.Mode(), st.Size(), sys.Uid)
	}
	if s := dlGetSatirlar(t); len(s) != 0 {
		t.Fatalf("kesme sonrasi bos beklenirdi: %q", s)
	}
}

// TestDebugLog_KiraciKontrollu_RootOkumazKesmez — istismar senaryolari: kiraci
// kendi evindeki yolu root'un disindaki bir dosyaya yonlendirir. GET gizli icerigi
// DONDURMEMELI, DELETE kurban dosyayi KESMEMELI (handler + cekirdek fonksiyon).
func TestDebugLog_KiraciKontrollu_RootOkumazKesmez(t *testing.T) {
	senaryolar := []struct {
		ad  string
		kur func(t *testing.T, base, ev, gp string) (kurban string)
	}{
		{"php_debug.log → root sahipli ev-disi dosyaya symlink", func(t *testing.T, base, ev, gp string) string {
			k := dlKurban(t, base, 0)
			dlMust(t, os.Symlink(k, filepath.Join(gp, "php_debug.log")))
			return k
		}},
		{"php_debug.log → kiraci-sahipli ev-disi dosyaya symlink (yaprak O_NOFOLLOW)", func(t *testing.T, base, ev, gp string) string {
			k := dlKurban(t, base, dlKiraciUID)
			dlMust(t, os.Symlink(k, filepath.Join(gp, "php_debug.log")))
			return k
		}},
		{".gpanel → baska root:root 0755 dizine symlink (icinde kiraci-sahipli log)", func(t *testing.T, base, ev, gp string) string {
			hedef := filepath.Join(base, "hedefdizin")
			dlMust(t, os.Mkdir(hedef, 0o755))
			dlMust(t, os.Chmod(hedef, 0o755))
			k := filepath.Join(hedef, "php_debug.log")
			dlYaz(t, k, dlGizli+"\n", dlKiraciUID)
			dlMust(t, os.Remove(gp))
			dlMust(t, os.Symlink(hedef, gp))
			return k
		}},
		{".gpanel kiraci-sahipli dizin (yazici degismezi root:root)", func(t *testing.T, base, ev, gp string) string {
			dlMust(t, os.Chown(gp, dlKiraciUID, dlKiraciUID))
			k := filepath.Join(gp, "php_debug.log")
			dlYaz(t, k, dlGizli+"\n", dlKiraciUID)
			return k
		}},
		{".gpanel root ama grup-yazilabilir 0775", func(t *testing.T, base, ev, gp string) string {
			dlMust(t, os.Chmod(gp, 0o775))
			k := filepath.Join(gp, "php_debug.log")
			dlYaz(t, k, dlGizli+"\n", dlKiraciUID)
			return k
		}},
		{".gpanel root ama diger-yazilabilir 0757", func(t *testing.T, base, ev, gp string) string {
			dlMust(t, os.Chmod(gp, 0o757))
			k := filepath.Join(gp, "php_debug.log")
			dlYaz(t, k, dlGizli+"\n", dlKiraciUID)
			return k
		}},
		{"php_debug.log root sahipli duzenli dosya (sahip != kiraci)", func(t *testing.T, base, ev, gp string) string {
			k := filepath.Join(gp, "php_debug.log")
			dlYaz(t, k, dlGizli+"\n", 0)
			return k
		}},
		{"php_debug.log → kiraci-sahipli ev-disi dosyaya hardlink (Nlink)", func(t *testing.T, base, ev, gp string) string {
			k := dlKurban(t, base, dlKiraciUID)
			dlMust(t, os.Link(k, filepath.Join(gp, "php_debug.log")))
			return k
		}},
		{"php_debug.log → root sahipli ev-disi dosyaya hardlink", func(t *testing.T, base, ev, gp string) string {
			k := dlKurban(t, base, 0)
			dlMust(t, os.Link(k, filepath.Join(gp, "php_debug.log")))
			return k
		}},
		{"ev dizini symlink (gercek ev baska yerde, icinde gecerli yapi)", func(t *testing.T, base, ev, gp string) string {
			gercek := filepath.Join(base, "gercekev")
			dlEvDizini(t, gercek)
			ggp := filepath.Join(gercek, ".gpanel")
			dlMust(t, os.Mkdir(ggp, 0o755))
			dlMust(t, os.Chmod(ggp, 0o755))
			k := filepath.Join(ggp, "php_debug.log")
			dlYaz(t, k, dlGizli+"\n", dlKiraciUID)
			dlMust(t, os.RemoveAll(ev))
			dlMust(t, os.Symlink(gercek, ev))
			return k
		}},
	}
	for _, sn := range senaryolar {
		t.Run(sn.ad, func(t *testing.T) {
			base, ev, gp := dlEvKur(t)
			kurban := sn.kur(t, base, ev, gp)
			saglam := func(adim string) {
				t.Helper()
				b, err := os.ReadFile(kurban)
				if err != nil || string(b) != dlGizli+"\n" {
					t.Fatalf("%s: kurban dosya root olarak kesildi/degisti: %q err=%v", adim, b, err)
				}
			}
			for _, s := range debugLogSatirlar(ev) {
				if strings.Contains(s, dlGizli) {
					t.Errorf("debugLogSatirlar kurban icerigini root olarak okudu: %q", s)
				}
			}
			for _, s := range dlGetSatirlar(t) {
				if strings.Contains(s, dlGizli) {
					t.Errorf("GET yaniti kurban icerigini sizdirdi: %q", s)
				}
			}
			if kod, govde := dlIstek(t, http.MethodDelete); kod != http.StatusOK {
				t.Fatalf("DELETE 200 beklenirdi: %d %s", kod, govde)
			}
			saglam("DELETE")
			_ = debugLogKes(ev)
			saglam("debugLogKes")
		})
	}
}

// TestDebugLog_FIFOBloklamazReddedilir — php_debug.log FIFO ise acilis BLOKLAMAMALI
// (aksi halde her GET bir goroutine'i sonsuza dek asar → panel DoS) ve duzenli
// dosya olmadigi icin reddedilmeli.
func TestDebugLog_FIFOBloklamazReddedilir(t *testing.T) {
	_, ev, gp := dlEvKur(t)
	fifo := filepath.Join(gp, "php_debug.log")
	dlMust(t, unix.Mkfifo(fifo, 0o644))
	dlMust(t, os.Chown(fifo, dlKiraciUID, dlKiraciUID))
	sonuc := make(chan error, 1)
	go func() {
		f, err := debugLogAc(ev, unix.O_RDONLY)
		if f != nil {
			_ = f.Close()
		}
		sonuc <- err
	}()
	select {
	case err := <-sonuc:
		if err == nil {
			t.Fatal("FIFO debug log olarak kabul edildi (yalniz duzenli dosya kabul edilmeli)")
		}
	case <-time.After(3 * time.Second):
		// asili acilisi serbest birak (okuyucu bekliyorken nonblock yazici acilir)
		if w, e := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); e == nil {
			_ = w.Close()
		}
		t.Fatal("FIFO acilisi blokladi (O_NONBLOCK yok) — istek goroutine'i asili kalir")
	}
}

// TestDebugLogEv_TekBilesen — sk tek yol bileseni olmali; ev = kok/sk.
func TestDebugLogEv_TekBilesen(t *testing.T) {
	for _, sk := range []string{"", "root", "c_a/../../etc", "c_/x", "c_a/b"} {
		if ev, err := debugLogEv(sk); err == nil {
			t.Errorf("gecersiz sk %q kabul edildi: %q", sk, ev)
		}
	}
	if ev, err := debugLogEv("c_abc"); err != nil || ev != debugLogEvKok+"/c_abc" {
		t.Fatalf("gecerli sk: ev=%q err=%v", ev, err)
	}
}
