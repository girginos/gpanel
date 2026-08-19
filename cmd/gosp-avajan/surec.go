package main

// FAZ 1 — Process Behavior Engine (netlink proc connector). 3-AJAN DOĞRULAMA
// SONRASI YENİDEN TASARIM.
//
// 🔴 NEDEN netlink, eBPF DEĞİL: garble/toolchain + eski çekirdek + statik binary.
//
// 🔴 BİLDİRİM MODU: asla süreç öldürmez — skorlar + bildirir.
//
// 🔴 TASARIM (comm-denylist DEĞİL): eski "web-ebeveyn comm → kabuk-çocuk comm"
// modeli hem gürültü basıyordu (mail()/yedek php-fpm→sh her seferinde) hem
// önemsizce atlatılıyordu (kabuğu yeniden adlandır, webroot'tan ELF çalıştır).
// Yeni model: WEB-ATA + (paket-DIŞI binary köken VEYA şüpheli CMDLINE):
//   - exe kökeni güvenilmez (webroot/tmp/shm/memfd/deleted) → yüksek (comm'a bakmaz)
//   - web-ata + kabuk/yorumlayıcı + şüpheli cmdline (curl|bash, base64 -d, /dev/tcp)
//     → yüksek. "sh -c sendmail/mysqldump/convert" (mail/yedek) ŞÜPHESİZ → 0.
// Ebeveyn FORK-anında izlenir (reparent yarışını yener). comm YALNIZ ipucu;
// karar exe-realpath + cmdline'a dayanır.

import (
	"bytes"
	"encoding/binary"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	cnIdxProc         = 0x1
	cnValProc         = 0x1
	procCnMcastListen = 1
	procEventFork     = 0x00000001
	procEventExec     = 0x00000002
	procEventExit     = 0x80000000
)

// Geçici teşhis.
var surecDebug = os.Getenv("GOSP_SUREC_DEBUG") == "1"

// pidKayit — FORK-anında kurulan bilgi. reparent yarışını yener: ebeveyn kimliği
// olay-anında saklanır, /proc'tan sonradan (ppid=1 reparent riskiyle) okunmaz.
type pidKayit struct {
	ebeveyn int
	web     bool // bu süreç ya da bir atası web-sunucu mu (FORK-anında yayılır)
	dogdu   time.Time
}

// olay — netlink'ten ayrıştırılmış tek proc olayı.
type olay struct {
	tur     uint32
	pid     int
	ebeveyn int // yalnız FORK'ta anlamlı (parent_pid)
}

// kova — kiracı-uid başına kaba token-bucket (exec seli oran sınırı).
type kova struct {
	jeton    int
	sonDoldu time.Time
}

// surecIzle — FORK/EXEC/EXIT olaylarını dinler; EXEC'i puanlar.
func (a *ajan) surecIzle() {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, unix.NETLINK_CONNECTOR)
	if err != nil {
		log.Printf("süreç izleme kapalı — netlink socket: %v (CAP_NET_ADMIN gerekli)", err)
		return
	}
	defer unix.Close(fd)
	// Tamponu büyüt: yoğun fork/exec'te ENOBUFS'u azaltır.
	_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, 8<<20)
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: cnIdxProc}); err != nil {
		log.Printf("süreç izleme kapalı — netlink bind: %v", err)
		return
	}
	if err := procDinle(fd); err != nil {
		log.Printf("süreç izleme kapalı — PROC_CN_MCAST_LISTEN: %v", err)
		return
	}
	log.Printf("süreç izleniyor (netlink proc connector)")

	a.surecThrottle = map[string]time.Time{}
	a.pidTablo = map[int]*pidKayit{}
	a.uidAd = map[int]string{}
	sonSupurme := time.Now()

	buf := make([]byte, 16384)
	for {
		n, from, err := unix.Recvfrom(fd, buf, 0)
		if err != nil {
			// EINTR/ENOBUFS/EAGAIN GEÇİCİ — motor ASLA tek hatayla ölmez.
			if err == unix.EINTR || err == unix.ENOBUFS || err == unix.EAGAIN {
				if err == unix.ENOBUFS {
					log.Printf("süreç izleme: netlink tampon taştı (ENOBUFS) — olaylar atlandı, devam")
				}
				continue
			}
			log.Printf("süreç izleme durdu — netlink read: %v", err)
			return
		}
		// Yalnız ÇEKİRDEK kaynaklı olaylar (yerel sahte-olay enjeksiyonunu ele).
		if nl, ok := from.(*unix.SockaddrNetlink); !ok || nl.Pid != 0 {
			continue
		}
		for _, ev := range olaylarAyristir(buf[:n]) {
			a.olayIsle(ev)
		}
		// Periyodik süpürme: eski throttle + pidTablo girdilerini at (sınırsız
		// büyüme = bellek DoS'unu keser).
		if time.Since(sonSupurme) > 30*time.Second {
			a.tablolariSupur()
			sonSupurme = time.Now()
		}
	}
}

// olayIsle — FORK ebeveyn soyağacını kurar, EXIT temizler, EXEC'i değerlendirir.
func (a *ajan) olayIsle(ev olay) {
	switch ev.tur {
	case procEventFork:
		// Çocuğun web-ata durumunu FORK-anında hesapla ve yay (reparent-güvenli).
		ata := a.pidTablo[ev.ebeveyn]
		web := false
		if ata != nil {
			web = ata.web
		} else {
			web = a.procWebMi(ev.ebeveyn) // önbellekte yoksa /proc'tan sınıflandır
		}
		if len(a.pidTablo) < 200000 { // güvenlik üst sınırı
			a.pidTablo[ev.pid] = &pidKayit{ebeveyn: ev.ebeveyn, web: web, dogdu: time.Now()}
		}
	case procEventExit:
		delete(a.pidTablo, ev.pid)
	case procEventExec:
		a.execDegerlendir(ev.pid)
	}
}

// execDegerlendir — exe kökeni + cmdline + web-ata → puan → (throttle'lı) bildirim.
func (a *ajan) execDegerlendir(pid int) {
	exe := procExe(pid) // realpath (symlink çözülü; deleted/memfd işaretli)
	cmdline := procCmdline(pid)
	uid := procUid(pid)

	// Bu süreç bir web sunucusuna EXEC ettiyse pidTablo'da web=true işaretle →
	// bundan SONRA fork edeceği çocuklar web-ata durumunu doğru miras alır
	// (php-fpm worker'ı oturum ortasında yeniden doğsa bile izlenir).
	if _, comm := procStat(pid); webSunucuMu(exe, comm) {
		if k := a.pidTablo[pid]; k != nil {
			k.web = true
		} else if len(a.pidTablo) < 200000 {
			a.pidTablo[pid] = &pidKayit{web: true, dogdu: time.Now()}
		}
	}

	// Web-ata: FORK-anı önbelleğinden (reparent-güvenli); yoksa /proc zinciri.
	web := false
	if k := a.pidTablo[pid]; k != nil {
		web = k.web
	} else {
		web = a.atadaWebVar(pid)
	}

	puan, kod, aciklama := surecPuanla(web, exe, cmdline, uid)
	if surecDebug {
		log.Printf("DBG exec pid=%d web=%v exe=%q uid=%d puan=%d", pid, web, exe, uid, puan)
	}
	if puan < 30 {
		return
	}
	// Oran sınırı: kiracı-uid başına saniyede en çok ~5 değerlendirme bildirimi
	// (exec seli root ajanı NSS/DB'ye boğmasın). Sınıra takılırsa sessiz atla.
	if !a.oranIzin(uid) {
		return
	}
	// Kiracı bağlamı: uid → kullanıcı (önbellekli) → domain_id.
	kadi := a.uidKullaniciOnb(uid)
	domID := int64(0)
	if strings.HasPrefix(kadi, "c_") && a.domCache != nil {
		domID = a.domCache.domainIDSysUser(kadi)
	}
	if domID == 0 {
		return // kiracı değil → bildirimin sahibi yok (log'da DBG ile görülür)
	}
	// Throttle: SABİT kural-kodu anahtarı (saldırgan-kontrollü yol/comm DEĞİL) →
	// hem sınırsız-büyüme hem throttle-bypass kapanır.
	anahtar := strconv.FormatInt(domID, 10) + ":" + kod
	simdi := time.Now()
	if son, ok := a.surecThrottle[anahtar]; ok && simdi.Sub(son) < 60*time.Second {
		return
	}
	a.surecThrottle[anahtar] = simdi

	alan := domainAdi(a.db, domID)
	baslik := "Şüpheli süreç etkinliği"
	if alan != "" {
		baslik = alan + " — şüpheli süreç etkinliği"
	}
	seviye := "uyari"
	if puan >= 40 {
		seviye = "kritik"
	}
	log.Printf("SÜREÇ [%s puan=%d kod=%s] pid=%d uid=%s: %s", seviye, puan, kod, pid, kadi, aciklama)
	bildirimYaz(a.db, seviye, baslik, aciklama, domID, "av_surec", 0)
	// FAZ2 attack-chain: süreç kuralına göre aşama (indirici→c2, diğer→çalıştırma).
	olayYaz(a.db, domID, "surec", surecAsama(kod), seviye, aciklama, "av_surec", 0)
}

// surecAsama — süreç kural-kodunu kill-chain aşamasına eşler.
func surecAsama(kod string) string {
	if kod == "web_indirici" {
		return "c2"
	}
	return "calistirma" // guvenilmez_koken, sanal_kabuk_cmd
}

// ── Saf puanlama (test edilebilir) ──────────────────────────────────────────

// surecPuanla — web-ata + exe-realpath + cmdline. comm KULLANMAZ (sahtelenebilir).
// Dönüş: (puan, kuralKodu, aciklama). puan<30 → önemsiz.
func surecPuanla(web bool, exe, cmdline string, uid int) (int, string, string) {
	temizExe := exeTemiz(exe) // " (deleted)" ekini ayır

	// R1 — GÜVENİLMEZ KÖKEN: paket-dışı/dünya-yazılabilir/silinmiş binary. comm'a
	// bakmaz → kabuğu yeniden adlandırma + webroot-ELF + memfd fileless kapanır.
	if kokenSupheli(exe) && (web || uid >= 1000) {
		return 40, "guvenilmez_koken", "güvenilmez konumdan çalıştırma: " + kisaExe(exe)
	}

	if web {
		// R2 — web-ata + KABUK/YORUMLAYICI + şüpheli cmdline. "sh -c sendmail/
		// mysqldump/convert" (mail/yedek/görsel) ŞÜPHESİZ → düşer (mail() FP'si YOK).
		if kabukVeyaYorumlayiciMi(temizExe) && cmdlineSupheli(cmdline) {
			return 40, "sanal_kabuk_cmd", "web sürecinden şüpheli komut: " + kisaCmd(cmdline)
		}
		// R3 — web-ata + indirici + uzak URL. Gerçek indirme-borusu (eski ölü
		// +20 kuralının yerine; artık bildirir).
		if indiriciMi(temizExe) && cmdlineUzakURL(cmdline) {
			return 35, "web_indirici", "web sürecinden uzak indirme: " + kisaCmd(cmdline)
		}
	}
	return 0, "", ""
}

// kokenSupheli — çalıştırılan binary güvenilmez bir konumda mı: dünya-yazılabilir
// (/tmp,/dev/shm,/var/tmp), kiracı webroot'u (/home/*/public_html veya /home/*/),
// memfd (diskisiz), ya da silinmiş (" (deleted)").
func kokenSupheli(exe string) bool {
	if exe == "" {
		return false
	}
	if strings.HasSuffix(exe, " (deleted)") || strings.HasPrefix(exe, "/memfd:") {
		return true
	}
	y := filepath.ToSlash(exe)
	if strings.HasPrefix(y, "/tmp/") || strings.HasPrefix(y, "/dev/shm/") || strings.HasPrefix(y, "/var/tmp/") {
		return true
	}
	// Kiracı ev dizininden çalıştırılan binary (meşru binary /usr,/bin,/opt'ta olur).
	if strings.HasPrefix(y, "/home/") {
		return true
	}
	return false
}

func kabukVeyaYorumlayiciMi(exe string) bool {
	b := filepath.Base(exe)
	switch b {
	case "bash", "sh", "dash", "zsh", "ksh", "ash", "busybox",
		"php", "php-cgi", "python", "python2", "python3", "perl", "ruby", "node", "lua":
		return true
	}
	return false
}

func indiriciMi(exe string) bool {
	switch filepath.Base(exe) {
	case "curl", "wget", "fetch", "aria2c", "nc", "ncat", "socat":
		return true
	}
	return false
}

// cmdlineSupheli — komut satırında indir-çalıştır / ters-kabuk / obfuscation
// göstergesi var mı. MEŞRU shell-out'lar (sendmail, mysqldump, tar, convert)
// bu göstergeleri TAŞIMAZ → 0 (yanlış-pozitif selini keser).
var supheliTokenlar = []string{
	"curl ", "wget ", "|sh", "| sh", "|bash", "| bash", "bash -i", "sh -i",
	"/dev/tcp/", "/dev/udp/", "nc -e", "ncat -e", "-e /bin/", "mkfifo",
	"base64 -d", "base64 --decode", "base64 -D", "gzip -d", "xxd -r",
	"python -c", "python3 -c", "perl -e", "perl -MIO", "ruby -e", "php -r",
	"chmod +x", "chmod 777", "wget -o", "wget -O", "curl -o", "curl -O",
	"setsid", "0<&", ">&/dev/tcp", "eval(", "$(curl", "$(wget", "`curl", "`wget",
}

func cmdlineSupheli(cmdline string) bool {
	l := strings.ToLower(cmdline)
	for _, t := range supheliTokenlar {
		if strings.Contains(l, t) {
			return true
		}
	}
	return false
}

func cmdlineUzakURL(cmdline string) bool {
	l := strings.ToLower(cmdline)
	return strings.Contains(l, "http://") || strings.Contains(l, "https://") ||
		strings.Contains(l, "ftp://")
}

func exeTemiz(exe string) string { return strings.TrimSuffix(exe, " (deleted)") }

func kisaExe(exe string) string {
	e := exeTemiz(exe)
	if len(e) > 96 {
		return e[:96] + "…"
	}
	return e
}
func kisaCmd(c string) string {
	c = strings.TrimSpace(c)
	if len(c) > 120 {
		return c[:120] + "…"
	}
	return c
}

// ── Web-ata sınıflandırma ───────────────────────────────────────────────────

// webSunucuMu — verilen exe yolu / comm bir web sunucusu/PHP işleyicisi mi.
func webSunucuMu(exe, comm string) bool {
	b := filepath.Base(exeTemiz(exe))
	for _, w := range []string{"php-fpm", "php-cgi", "lsphp", "httpd", "apache2", "nginx", "litespeed"} {
		if b == w || comm == w {
			return true
		}
	}
	// "php-fpm: pool x" comm 15-karakter kırpık "php-fpm" olur; base "php-fpm".
	return strings.HasPrefix(comm, "php-fpm") || strings.HasPrefix(b, "php-fpm")
}

// procWebMi — pid /proc'tan web sunucusu mu (FORK-anı önbellek dolumu için).
func (a *ajan) procWebMi(pid int) bool {
	_, comm := procStat(pid)
	return webSunucuMu(procExe(pid), comm)
}

// atadaWebVar — pid'in ata zincirinde (FORK önbelleği + /proc yedeği) web
// sunucusu var mı. En çok 8 seviye; reparent'te önbellek fork-anı ebeveyni verir.
func (a *ajan) atadaWebVar(pid int) bool {
	for i := 0; i < 8 && pid > 1; i++ {
		if k := a.pidTablo[pid]; k != nil {
			if k.web {
				return true
			}
			pid = k.ebeveyn
			continue
		}
		ppid, comm := procStat(pid)
		if webSunucuMu(procExe(pid), comm) {
			return true
		}
		if ppid <= 1 {
			break
		}
		pid = ppid
	}
	return false
}

// ── Oran sınırı + önbellekler ───────────────────────────────────────────────

// oranIzin — kiracı-uid başına kaba token-bucket (saniyede ~5). Süpürme ile
// eski kovalar temizlenir.
func (a *ajan) oranIzin(uid int) bool {
	if a.oranKova == nil {
		a.oranKova = map[int]*kova{}
	}
	k := a.oranKova[uid]
	simdi := time.Now()
	if k == nil {
		k = &kova{jeton: 5, sonDoldu: simdi}
		a.oranKova[uid] = k
	}
	// Saniyede 5 jeton yeniden dolar (üst sınır 5).
	yeni := int(simdi.Sub(k.sonDoldu).Seconds()) * 5
	if yeni > 0 {
		k.jeton += yeni
		if k.jeton > 5 {
			k.jeton = 5
		}
		k.sonDoldu = simdi
	}
	if k.jeton <= 0 {
		return false
	}
	k.jeton--
	return true
}

func (a *ajan) uidKullaniciOnb(uid int) string {
	if uid < 0 {
		return ""
	}
	if a.uidAd == nil {
		a.uidAd = map[int]string{}
	}
	if ad, ok := a.uidAd[uid]; ok {
		return ad
	}
	ad := ""
	if u, err := user.LookupId(strconv.Itoa(uid)); err == nil {
		ad = u.Username
	}
	a.uidAd[uid] = ad
	return ad
}

// tablolariSupur — throttle (60sn) + pidTablo (5dk) eski girdilerini at.
func (a *ajan) tablolariSupur() {
	simdi := time.Now()
	for k, t := range a.surecThrottle {
		if simdi.Sub(t) > 60*time.Second {
			delete(a.surecThrottle, k)
		}
	}
	for pid, k := range a.pidTablo {
		if simdi.Sub(k.dogdu) > 5*time.Minute {
			delete(a.pidTablo, pid)
		}
	}
}

// ── netlink ─────────────────────────────────────────────────────────────────

func procDinle(fd int) error {
	body := make([]byte, 24)
	binary.LittleEndian.PutUint32(body[0:], cnIdxProc)
	binary.LittleEndian.PutUint32(body[4:], cnValProc)
	binary.LittleEndian.PutUint16(body[16:], 4)
	binary.LittleEndian.PutUint32(body[20:], procCnMcastListen)
	msg := make([]byte, 16+len(body))
	binary.LittleEndian.PutUint32(msg[0:], uint32(len(msg)))
	binary.LittleEndian.PutUint16(msg[4:], uint16(unix.NLMSG_DONE))
	binary.LittleEndian.PutUint32(msg[12:], uint32(os.Getpid()))
	copy(msg[16:], body)
	return unix.Sendto(fd, msg, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK})
}

// olaylarAyristir — netlink tamponundaki FORK/EXEC/EXIT olaylarını çıkarır.
// Yapı: nlmsghdr(16) + cn_msg(20) + proc_event{what(4)cpu(4)ts(8)=16 + veri}.
// EXEC veri: process_pid(4) process_tgid(4). FORK veri: parent_pid(4) parent_tgid
// (4) child_pid(4) child_tgid(4). EXIT veri: process_pid(4)...
func olaylarAyristir(buf []byte) []olay {
	var out []olay
	for len(buf) >= 16 {
		nlLen := binary.LittleEndian.Uint32(buf[0:])
		if nlLen < 16 || int(nlLen) > len(buf) {
			break
		}
		nlType := binary.LittleEndian.Uint16(buf[4:])
		p := buf[16:nlLen]
		// Yalnız connector veri mesajları (kontrol/hata mesajlarını atla) + doğru
		// connector kimliği.
		if nlType != unix.NLMSG_ERROR && nlType != unix.NLMSG_NOOP && len(p) >= 20+16+8 &&
			binary.LittleEndian.Uint32(p[0:]) == cnIdxProc && binary.LittleEndian.Uint32(p[4:]) == cnValProc {
			what := binary.LittleEndian.Uint32(p[20:])
			veri := p[20+16:] // event_data
			switch what {
			case procEventExec:
				pid := int(int32(binary.LittleEndian.Uint32(veri[0:])))
				if pid > 0 {
					out = append(out, olay{tur: procEventExec, pid: pid})
				}
			case procEventFork:
				if len(veri) >= 16 {
					ppid := int(int32(binary.LittleEndian.Uint32(veri[0:])))  // parent_pid
					cpid := int(int32(binary.LittleEndian.Uint32(veri[8:])))  // child_pid
					if cpid > 0 {
						out = append(out, olay{tur: procEventFork, pid: cpid, ebeveyn: ppid})
					}
				}
			case procEventExit:
				pid := int(int32(binary.LittleEndian.Uint32(veri[0:])))
				if pid > 0 {
					out = append(out, olay{tur: procEventExit, pid: pid})
				}
			}
		}
		adv := (nlLen + 3) &^ 3
		if int(adv) >= len(buf) {
			break
		}
		buf = buf[adv:]
	}
	return out
}

// ── /proc ayrıştırıcılar ────────────────────────────────────────────────────

func procStat(pid int) (int, string) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, ""
	}
	return statAyristir(string(b))
}

// statAyristir — comm parantez/boşluk içerebilir → ilk '(' ile SON ')' arası.
func statAyristir(s string) (int, string) {
	a := strings.IndexByte(s, '(')
	z := strings.LastIndexByte(s, ')')
	if a < 0 || z < 0 || z <= a {
		return 0, ""
	}
	comm := s[a+1 : z]
	alanlar := strings.Fields(s[z+1:])
	ppid := 0
	if len(alanlar) >= 2 {
		ppid, _ = strconv.Atoi(alanlar[1])
	}
	return ppid, comm
}

func procExe(pid int) string {
	y, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	if err != nil {
		return ""
	}
	return y
}

// procCmdline — /proc/pid/cmdline (NUL ayraçlı) → boşlukla birleştir.
func procCmdline(pid int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil || len(b) == 0 {
		return ""
	}
	return string(bytes.ReplaceAll(bytes.TrimRight(b, "\x00"), []byte{0}, []byte{' '}))
}

func procUid(pid int) int {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return -1
	}
	for _, satir := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(satir, "Uid:") {
			alan := strings.Fields(satir)
			if len(alan) >= 2 {
				if u, e := strconv.Atoi(alan[1]); e == nil {
					return u
				}
			}
		}
	}
	return -1
}

// domainIDSysUser — sistem_kullanici (c_X) → domain_id (önbellekli).
func (c *domainCache) domainIDSysUser(sk string) int64 {
	if c.db == nil || sk == "" {
		return 0
	}
	if id, ok := c.skID[sk]; ok {
		return id
	}
	var id int64
	_ = c.db.QueryRow(`SELECT id FROM domains WHERE sistem_kullanici=? LIMIT 1`, sk).Scan(&id)
	c.skID[sk] = id
	return id
}
