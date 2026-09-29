package sshaccess

// Symlink / sabit bağ gerileme testleri — root SSH ele geçirme zinciri.
//
// Saldırı: kiracı kendi evinde ~/.ssh'i ya da ~/.ssh/authorized_keys'i ev dışını
// (üretimde /root/.ssh/authorized_keys) gösteren bir bağa çevirir; yönetici
// kiracının anahtarını kaydettiğinde panel (root) yazımı bağın hedefine yapıyordu.
// Burada "kurban" = ev dışındaki t.TempDir'de duran, root'un dosyasını temsil eden
// dosya. Her saldırıda istek reddedilmeli ve kurbanın içeriği, mtime'ı ve sahibi
// DEĞİŞMEMELİ.
//
// CANLI SİSTEME DOKUNMAZ: ev kökü (evKoku), uid/gid çözümü (kimlikCoz), restorecon
// (restoreconCalistir) dikişlerle sahtelenir; DB yerine bellek içi sahte sürücü
// kullanılır. TestMain dikişleri önce güvenli sahtelere çeker.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
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
)

const testSK = "c_sshdeneme"

func TestMain(m *testing.M) {
	evKoku = "/nonexistent-gosp-sshaccess-test"
	kimlikCoz = func(string) (int, int, error) { return -1, -1, errors.New("test: kimlikCoz sahtelenmedi") }
	restoreconCalistir = func(yol string) { panic("test: restoreconCalistir sahtelenmedi: " + yol) }
	os.Exit(m.Run())
}

type ortam struct {
	kok, ev, dis string // evKoku, evKoku/<sk>, kiracı evinin DIŞI (root'un alanı)
	uid, gid     int
	etiketlenen  []string
}

func ortamKur(t *testing.T) *ortam {
	t.Helper()
	o := &ortam{kok: t.TempDir(), dis: t.TempDir(), uid: os.Getuid(), gid: os.Getgid()}
	if os.Geteuid() == 0 {
		// root olarak koşuyorsa Fchown'ın gerçekten yapıldığını görmek için yabancı kimlik.
		o.uid, o.gid = 54321, 54321
	}
	o.ev = filepath.Join(o.kok, testSK)
	if err := os.Mkdir(o.ev, 0o710); err != nil {
		t.Fatal(err)
	}
	eskiKok, eskiKimlik, eskiEtiket, eskiAd := evKoku, kimlikCoz, restoreconCalistir, geciciAd
	evKoku = o.kok
	kimlikCoz = func(sk string) (int, int, error) {
		if sk != testSK {
			return -1, -1, errors.New("beklenmeyen sk: " + sk)
		}
		return o.uid, o.gid, nil
	}
	restoreconCalistir = func(yol string) { o.etiketlenen = append(o.etiketlenen, yol) }
	t.Cleanup(func() { evKoku, kimlikCoz, restoreconCalistir, geciciAd = eskiKok, eskiKimlik, eskiEtiket, eskiAd })
	return o
}

var kurbanZamani = time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)

const kurbanIcerik = "ssh-ed25519 AAAAYONETICI kok@sunucu\n"

const kiraciAnahtari = "ssh-ed25519 AAAAKIRACI kiraci@pc"

func kurbanYaz(t *testing.T, yol string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(yol), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(yol, []byte(kurbanIcerik), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(yol, kurbanZamani, kurbanZamani); err != nil {
		t.Fatal(err)
	}
}

func kurbanDegismedi(t *testing.T, yol string) {
	t.Helper()
	b, err := os.ReadFile(yol)
	if err != nil {
		t.Fatalf("kurban okunamadı: %v", err)
	}
	if string(b) != kurbanIcerik {
		t.Fatalf("KURBAN EZİLDİ: %q", b)
	}
	st, err := os.Lstat(yol)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Mode().IsRegular() || st.Mode().Perm() != 0o600 || !st.ModTime().Equal(kurbanZamani) {
		t.Fatalf("kurban değişti: mod=%v mtime=%v", st.Mode(), st.ModTime())
	}
	if s := st.Sys().(*syscall.Stat_t); int(s.Uid) != os.Getuid() || int(s.Gid) != os.Getgid() {
		t.Fatalf("KURBANIN SAHİBİ DEĞİŞTİ: %d:%d", s.Uid, s.Gid)
	}
}

func geciciKalmadi(t *testing.T, dizin string) {
	t.Helper()
	girdiler, _ := os.ReadDir(dizin)
	for _, g := range girdiler {
		if strings.HasPrefix(g.Name(), ".authorized_keys.gosp-") {
			t.Fatalf("geçici dosya kaldı: %s/%s", dizin, g.Name())
		}
	}
}

func sahipKontrol(t *testing.T, o *ortam, yol string) {
	t.Helper()
	st, err := os.Lstat(yol)
	if err != nil {
		t.Fatal(err)
	}
	if s := st.Sys().(*syscall.Stat_t); int(s.Uid) != o.uid || int(s.Gid) != o.gid {
		t.Fatalf("%s sahibi %d:%d, beklenen %d:%d", yol, s.Uid, s.Gid, o.uid, o.gid)
	}
}

// ---------------------------------------------------------------- sahte DB

type sahteBaglayici struct{ sk string }

func (c sahteBaglayici) Connect(context.Context) (driver.Conn, error) { return sahteBag(c), nil }
func (sahteBaglayici) Driver() driver.Driver                          { return sahteSurucu{} }

type sahteSurucu struct{}

func (sahteSurucu) Open(string) (driver.Conn, error) { return nil, errors.New("kullanılmaz") }

type sahteBag sahteBaglayici

func (b sahteBag) Prepare(q string) (driver.Stmt, error) { return sahteKomut{sk: b.sk, q: q}, nil }
func (sahteBag) Close() error                            { return nil }
func (sahteBag) Begin() (driver.Tx, error)               { return nil, errors.New("işlem yok") }

type sahteKomut struct{ sk, q string }

func (sahteKomut) Close() error  { return nil }
func (sahteKomut) NumInput() int { return -1 }
func (k sahteKomut) Exec([]driver.Value) (driver.Result, error) {
	return nil, errors.New("test: DB yazımı beklenmiyor: " + k.q)
}
func (k sahteKomut) Query([]driver.Value) (driver.Rows, error) {
	if !strings.Contains(k.q, "FROM domains") {
		return nil, errors.New("beklenmeyen sorgu: " + k.q)
	}
	return &sahteSatirlar{sk: k.sk}, nil
}

type sahteSatirlar struct {
	sk    string
	bitti bool
}

func (*sahteSatirlar) Columns() []string { return []string{"sistem_kullanici", "alan_adi", "is_demo"} }
func (*sahteSatirlar) Close() error      { return nil }
func (s *sahteSatirlar) Next(dest []driver.Value) error {
	if s.bitti {
		return io.EOF
	}
	s.bitti = true
	dest[0], dest[1], dest[2] = s.sk, "ornek.test", int64(0)
	return nil
}

// anahtarKaydet: yöneticinin PUT /domains/1/ssh/anahtar isteği (rota AdminOnly;
// handler burada doğrudan çağrılır).
func anahtarKaydet(t *testing.T, anahtar string) *httptest.ResponseRecorder {
	t.Helper()
	db := sql.OpenDB(sahteBaglayici{sk: testSK})
	t.Cleanup(func() { db.Close() })
	h := &Handlers{DB: db}
	govde := `{"anahtar":"` + anahtar + `"}`
	req := httptest.NewRequest(http.MethodPut, "/domains/1/ssh/anahtar", strings.NewReader(govde))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h.AnahtarKaydet(w, req)
	return w
}

// ---------------------------------------------------------------- meşru kullanım

func TestAnahtarKaydet_MesruYazimGuncellemeVeTemizleme(t *testing.T) {
	o := ortamKur(t)
	ssh := filepath.Join(o.ev, ".ssh")
	ak := filepath.Join(ssh, "authorized_keys")

	// 1) ~/.ssh yok → oluşturulur, anahtar yazılır.
	if w := anahtarKaydet(t, kiraciAnahtari); w.Code != http.StatusOK {
		t.Fatalf("meşru yazım %d: %s", w.Code, w.Body)
	}
	st, err := os.Lstat(ssh)
	if err != nil || !st.IsDir() || st.Mode().Perm() != 0o700 {
		t.Fatalf(".ssh: %v %v", st, err)
	}
	sahipKontrol(t, o, ssh)
	b, err := os.ReadFile(ak)
	if err != nil || string(b) != kiraciAnahtari+"\n" {
		t.Fatalf("authorized_keys içerik %q %v", b, err)
	}
	st, _ = os.Lstat(ak)
	if !st.Mode().IsRegular() || st.Mode().Perm() != 0o600 {
		t.Fatalf("authorized_keys mod %v", st.Mode())
	}
	sahipKontrol(t, o, ak)
	geciciKalmadi(t, ssh)
	gercekEv, _ := filepath.EvalSymlinks(o.ev)
	beklenen := []string{filepath.Join(gercekEv, ".ssh"), filepath.Join(gercekEv, ".ssh", "authorized_keys")}
	if strings.Join(o.etiketlenen, "|") != strings.Join(beklenen, "|") {
		t.Fatalf("restorecon yolları %v, beklenen %v", o.etiketlenen, beklenen)
	}
	if !anahtarVar(testSK) {
		t.Fatal("anahtarVar yazımdan sonra false")
	}

	// 2) Var olan normal dosya güncellenir.
	if w := anahtarKaydet(t, "ssh-rsa AAAAIKINCI kiraci@pc2"); w.Code != http.StatusOK {
		t.Fatalf("güncelleme %d: %s", w.Code, w.Body)
	}
	if b, _ := os.ReadFile(ak); string(b) != "ssh-rsa AAAAIKINCI kiraci@pc2\n" {
		t.Fatalf("güncelleme içerik %q", b)
	}
	sahipKontrol(t, o, ak)
	geciciKalmadi(t, ssh)

	// 3) Boş anahtar dosyayı boşaltır.
	if w := anahtarKaydet(t, ""); w.Code != http.StatusOK {
		t.Fatalf("temizleme %d: %s", w.Code, w.Body)
	}
	if st, _ := os.Lstat(ak); st == nil || st.Size() != 0 {
		t.Fatalf("temizleme sonrası boyut %v", st)
	}
	if anahtarVar(testSK) {
		t.Fatal("anahtarVar boş dosyada true")
	}
}

// ---------------------------------------------------------------- saldırılar

// ~/.ssh → root'un .ssh dizini (ara bileşen symlink).
func TestAnahtarKaydet_SshDiziniSymlinkKurbaniDegistirmez(t *testing.T) {
	o := ortamKur(t)
	kurban := filepath.Join(o.dis, "authorized_keys")
	kurbanYaz(t, kurban)
	disSt, _ := os.Lstat(o.dis)
	if err := os.Symlink(o.dis, filepath.Join(o.ev, ".ssh")); err != nil {
		t.Fatal(err)
	}
	w := anahtarKaydet(t, kiraciAnahtari)
	if w.Code != http.StatusConflict {
		t.Fatalf(".ssh symlink kabul edildi: %d %s", w.Code, w.Body)
	}
	kurbanDegismedi(t, kurban)
	geciciKalmadi(t, o.dis)
	if st, _ := os.Lstat(o.dis); st.Sys().(*syscall.Stat_t).Uid != disSt.Sys().(*syscall.Stat_t).Uid || st.Mode() != disSt.Mode() {
		t.Fatalf("ev dışı dizinin sahibi/modu değişti")
	}
	if len(o.etiketlenen) != 0 {
		t.Fatalf("reddedilen istekte restorecon: %v", o.etiketlenen)
	}
}

// ~/.ssh/authorized_keys → /root/.ssh/authorized_keys (son bileşen symlink).
func TestAnahtarKaydet_AuthorizedKeysSymlinkKurbaniDegistirmez(t *testing.T) {
	o := ortamKur(t)
	kurban := filepath.Join(o.dis, "authorized_keys")
	kurbanYaz(t, kurban)
	ssh := filepath.Join(o.ev, ".ssh")
	if err := os.Mkdir(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(ssh, "authorized_keys")
	if err := os.Symlink(kurban, link); err != nil {
		t.Fatal(err)
	}
	w := anahtarKaydet(t, kiraciAnahtari)
	if w.Code != http.StatusConflict {
		t.Fatalf("authorized_keys symlink kabul edildi: %d %s", w.Code, w.Body)
	}
	kurbanDegismedi(t, kurban)
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("bağ beklenmedik biçimde değişti: %v %v", st, err)
	}
	geciciKalmadi(t, ssh)
}

// ~/.ssh/authorized_keys = root dosyasına SABİT bağ (Nlink>1): O_TRUNC ile yerinde
// yazım inode'u, yani root'un dosyasını ezerdi.
func TestAnahtarKaydet_SabitBagKurbaniDegistirmez(t *testing.T) {
	o := ortamKur(t)
	kurban := filepath.Join(o.dis, "authorized_keys")
	kurbanYaz(t, kurban)
	ssh := filepath.Join(o.ev, ".ssh")
	if err := os.Mkdir(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(kurban, filepath.Join(ssh, "authorized_keys")); err != nil {
		t.Skipf("sabit bağ kurulamadı: %v", err)
	}
	w := anahtarKaydet(t, kiraciAnahtari)
	if w.Code != http.StatusConflict {
		t.Fatalf("sabit bağ kabul edildi: %d %s", w.Code, w.Body)
	}
	kurbanDegismedi(t, kurban)
	geciciKalmadi(t, ssh)
}

// /home/<sk> → ev dışı (ev dizininin kendisi symlink).
func TestAnahtarKaydet_EvDiziniSymlinkKurbaniDegistirmez(t *testing.T) {
	o := ortamKur(t)
	kurban := filepath.Join(o.dis, ".ssh", "authorized_keys")
	kurbanYaz(t, kurban)
	if err := os.Remove(o.ev); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(o.dis, o.ev); err != nil {
		t.Fatal(err)
	}
	w := anahtarKaydet(t, kiraciAnahtari)
	if w.Code != http.StatusConflict {
		t.Fatalf("ev symlink kabul edildi: %d %s", w.Code, w.Body)
	}
	kurbanDegismedi(t, kurban)
	geciciKalmadi(t, filepath.Join(o.dis, ".ssh"))
	if anahtarVar(testSK) {
		t.Fatal("anahtarVar ev symlink'ini izledi")
	}
}

// Geçici dosya adı önceden kiracının symlink'i ise O_EXCL izlemeden reddeder.
func TestAuthorizedKeysYaz_GeciciAdSymlinkIzlenmez(t *testing.T) {
	o := ortamKur(t)
	kurban := filepath.Join(o.dis, "authorized_keys")
	kurbanYaz(t, kurban)
	ssh := filepath.Join(o.ev, ".ssh")
	if err := os.Mkdir(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	const sabit = ".authorized_keys.gosp-sabit"
	geciciAd = func() string { return sabit }
	if err := os.Symlink(kurban, filepath.Join(ssh, sabit)); err != nil {
		t.Fatal(err)
	}
	if err := authorizedKeysYaz(testSK, []byte(kiraciAnahtari+"\n")); err == nil {
		t.Fatal("önceden var olan geçici ad (symlink) kabul edildi")
	}
	kurbanDegismedi(t, kurban)
	if _, err := os.Lstat(filepath.Join(ssh, "authorized_keys")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reddedilen yazımda authorized_keys oluştu: %v", err)
	}
}

// authorized_keys dizin ya da FIFO ise reddedilir (tür denetimi).
func TestAuthorizedKeysYaz_DizinVeFIFOReddedilir(t *testing.T) {
	for _, tur := range []string{"dizin", "fifo"} {
		t.Run(tur, func(t *testing.T) {
			o := ortamKur(t)
			ssh := filepath.Join(o.ev, ".ssh")
			if err := os.Mkdir(ssh, 0o700); err != nil {
				t.Fatal(err)
			}
			ak := filepath.Join(ssh, "authorized_keys")
			var err error
			if tur == "dizin" {
				err = os.Mkdir(ak, 0o700)
			} else {
				err = unix.Mkfifo(ak, 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := authorizedKeysYaz(testSK, []byte(kiraciAnahtari+"\n")); !errors.Is(err, errGuvensizYol) {
				t.Fatalf("%s authorized_keys: hata %v, beklenen errGuvensizYol", tur, err)
			}
			geciciKalmadi(t, ssh)
		})
	}
}

// TOCTOU: tür/bağ denetiminden SONRA kiracı authorized_keys'i ya da ~/.ssh'i
// root'un dosyasına bağlarsa yazım yine hedefe gitmemeli (Renameat girdiyi
// değiştirir; ~/.ssh fd ile pinli). Yarış geciciAd dikişinde, denetimle yazım
// arasında deterministik olarak kurulur.
func TestAuthorizedKeysYaz_DenetimSonrasiYarisKurbaniDegistirmez(t *testing.T) {
	for _, hedef := range []string{"authorized_keys", ".ssh"} {
		t.Run(hedef, func(t *testing.T) {
			o := ortamKur(t)
			kurban := filepath.Join(o.dis, "authorized_keys")
			kurbanYaz(t, kurban)
			ssh := filepath.Join(o.ev, ".ssh")
			if err := os.Mkdir(ssh, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(ssh, "authorized_keys"), []byte("ssh-ed25519 ESKI\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			geciciAd = func() string {
				if hedef == "authorized_keys" {
					_ = os.Remove(filepath.Join(ssh, "authorized_keys"))
					_ = os.Symlink(kurban, filepath.Join(ssh, "authorized_keys"))
				} else {
					_ = os.Rename(ssh, filepath.Join(o.ev, "tasindi"))
					_ = os.Symlink(o.dis, ssh)
				}
				return ".authorized_keys.gosp-yaris"
			}
			if err := authorizedKeysYaz(testSK, []byte(kiraciAnahtari+"\n")); err != nil {
				t.Fatalf("yazım: %v", err)
			}
			kurbanDegismedi(t, kurban)
			geciciKalmadi(t, o.dis)
		})
	}
}

// ---------------------------------------------------------------- Ayarla iskeleti

func TestSshDiziniHazirla_MesruVeSymlink(t *testing.T) {
	o := ortamKur(t)
	if err := sshDiziniHazirla(testSK); err != nil {
		t.Fatalf("meşru: %v", err)
	}
	ssh := filepath.Join(o.ev, ".ssh")
	st, err := os.Lstat(ssh)
	if err != nil || !st.IsDir() || st.Mode().Perm() != 0o700 {
		t.Fatalf(".ssh: %v %v", st, err)
	}
	sahipKontrol(t, o, ssh)
	if len(o.etiketlenen) != 1 {
		t.Fatalf("restorecon %v", o.etiketlenen)
	}

	o2 := ortamKur(t)
	kurban := filepath.Join(o2.dis, "authorized_keys")
	kurbanYaz(t, kurban)
	disSt, _ := os.Lstat(o2.dis)
	if err := os.Symlink(o2.dis, filepath.Join(o2.ev, ".ssh")); err != nil {
		t.Fatal(err)
	}
	if err := sshDiziniHazirla(testSK); !errors.Is(err, errGuvensizYol) {
		t.Fatalf(".ssh symlink: hata %v, beklenen errGuvensizYol", err)
	}
	st, _ = os.Lstat(o2.dis)
	if st.Sys().(*syscall.Stat_t).Uid != disSt.Sys().(*syscall.Stat_t).Uid || st.Mode() != disSt.Mode() {
		t.Fatal("ev dışı dizinin sahibi/modu değişti")
	}
	kurbanDegismedi(t, kurban)
	if len(o2.etiketlenen) != 0 {
		t.Fatalf("reddedilen istekte restorecon: %v", o2.etiketlenen)
	}
}

// ---------------------------------------------------------------- anahtarVar kahini

func TestAnahtarVar_SymlinkIzlemez(t *testing.T) {
	o := ortamKur(t)
	kurban := filepath.Join(o.dis, "authorized_keys")
	kurbanYaz(t, kurban)
	ssh := filepath.Join(o.ev, ".ssh")
	if err := os.Mkdir(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	ak := filepath.Join(ssh, "authorized_keys")

	if err := os.Symlink(kurban, ak); err != nil {
		t.Fatal(err)
	}
	if anahtarVar(testSK) {
		t.Fatal("authorized_keys symlink'i izlendi (ev dışı varlık kahini)")
	}
	_ = os.Remove(ak)

	// Ev İÇİNDE kalan göreli symlink de izlenmez (yazım yolu da symlink'i reddeder).
	if err := os.WriteFile(filepath.Join(o.ev, "anahtarlar"), []byte(kiraciAnahtari+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../anahtarlar", ak); err != nil {
		t.Fatal(err)
	}
	if anahtarVar(testSK) {
		t.Fatal("ev içi göreli symlink izlendi")
	}
	_ = os.Remove(ak)

	if err := os.Mkdir(ak, 0o700); err != nil {
		t.Fatal(err)
	}
	if anahtarVar(testSK) {
		t.Fatal("dizin anahtar sayıldı")
	}
	_ = os.Remove(ak)

	if err := os.WriteFile(ak, []byte(kiraciAnahtari+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !anahtarVar(testSK) {
		t.Fatal("normal dosya görülmedi")
	}

	o2 := ortamKur(t)
	kurbanYaz(t, filepath.Join(o2.dis, "authorized_keys"))
	if err := os.Symlink(o2.dis, filepath.Join(o2.ev, ".ssh")); err != nil {
		t.Fatal(err)
	}
	if anahtarVar(testSK) {
		t.Fatal(".ssh symlink'i izlendi (ev dışı varlık kahini)")
	}
}

// evAc: bozuk sk ("" / "." / ".." / "/" içeren) ev kökünün dışına ya da köke
// çözülmemeli (sk DB'den gelir; handler gecerliSK'ya ek kemer).
func TestEvAc_GecersizSKReddedilir(t *testing.T) {
	o := ortamKur(t)
	kurbanYaz(t, filepath.Join(o.kok, ".ssh", "authorized_keys"))
	for _, sk := range []string{"", ".", "..", "x/..", "../" + filepath.Base(o.kok)} {
		if fd, err := evAc(sk); err == nil {
			unix.Close(fd)
			t.Fatalf("evAc(%q) kabul edildi", sk)
		}
		if anahtarVar(sk) {
			t.Fatalf("anahtarVar(%q) ev dışını gördü", sk)
		}
	}
}

// ---------------------------------------------------------------- restorecon kemeri

func TestRestoreconFd_EvDisinaTasinanEtiketlenmez(t *testing.T) {
	o := ortamKur(t)
	ssh := filepath.Join(o.ev, ".ssh")
	if err := os.Mkdir(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	evFd, err := unix.Open(o.ev, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(evFd)
	sshFd, err := unix.Open(ssh, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(sshFd)

	restoreconFd(evFd, sshFd)
	if len(o.etiketlenen) != 1 {
		t.Fatalf("ev içindeki dizin etiketlenmedi: %v", o.etiketlenen)
	}
	if err := os.Rename(ssh, filepath.Join(o.dis, "tasindi")); err != nil {
		t.Fatal(err)
	}
	restoreconFd(evFd, sshFd)
	if len(o.etiketlenen) != 1 {
		t.Fatalf("ev dışına taşınan dizin etiketlendi: %v", o.etiketlenen)
	}
}
