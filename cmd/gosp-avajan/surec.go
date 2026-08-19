package main

// FAZ 1 — Process Behavior Engine (netlink proc connector).
//
// 🔴 NEDEN netlink, eBPF DEĞİL: eBPF toolchain garble ile çakışıyor + çekirdek-
// sürümüne duyarlı. netlink CN_IDX_PROC saf Go (golang.org/x/sys/unix), eski
// çekirdeklerde çalışır, statik binary'ye sığar, CAP_NET_ADMIN yeter.
//
// 🔴 BİLDİRİM MODU: asla süreç öldürmez — yalnız exec zincirini SKORLAR ve
// şüpheliyi (php-fpm→bash gibi) bildirir. "Kill process" ileri fazda, yüksek
// güven + operatör politikasıyla.

import (
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
	procEventExec     = 0x00000002
)

// Geçici teşhis: GOSP_SUREC_DEBUG=1 → her exec olayını logla.
var surecDebug = os.Getenv("GOSP_SUREC_DEBUG") == "1"

// surecIzle — exec olaylarını dinler, /proc'tan zenginleştirir, şüpheli
// ebeveyn→çocuk zincirlerini bildirir. Bloke eder (goroutine olarak çağrılır).
func (a *ajan) surecIzle() {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, unix.NETLINK_CONNECTOR)
	if err != nil {
		log.Printf("süreç izleme kapalı — netlink socket: %v (CAP_NET_ADMIN gerekli)", err)
		return
	}
	defer unix.Close(fd)
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
	buf := make([]byte, 8192)
	for {
		n, err := unix.Read(fd, buf)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			log.Printf("süreç izleme durdu — netlink read: %v", err)
			return
		}
		pids := execPidleri(buf[:n])
		if surecDebug {
			log.Printf("DBG netlink %d bayt → %d exec pid: %v", n, len(pids), pids)
		}
		for _, pid := range pids {
			a.surecDegerlendir(pid)
		}
	}
}

// procDinle — PROC_CN_MCAST_LISTEN gönderip olay akışına abone olur.
func procDinle(fd int) error {
	body := make([]byte, 24)
	binary.LittleEndian.PutUint32(body[0:], cnIdxProc)  // id.idx
	binary.LittleEndian.PutUint32(body[4:], cnValProc)  // id.val
	binary.LittleEndian.PutUint32(body[8:], 0)          // seq
	binary.LittleEndian.PutUint32(body[12:], 0)         // ack
	binary.LittleEndian.PutUint16(body[16:], 4)         // len
	binary.LittleEndian.PutUint16(body[18:], 0)         // flags
	binary.LittleEndian.PutUint32(body[20:], procCnMcastListen)
	msg := make([]byte, 16+len(body))
	binary.LittleEndian.PutUint32(msg[0:], uint32(len(msg)))
	binary.LittleEndian.PutUint16(msg[4:], uint16(unix.NLMSG_DONE))
	binary.LittleEndian.PutUint16(msg[6:], 0)
	binary.LittleEndian.PutUint32(msg[8:], 0)
	binary.LittleEndian.PutUint32(msg[12:], uint32(os.Getpid()))
	copy(msg[16:], body)
	return unix.Sendto(fd, msg, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK})
}

// execPidleri — netlink tamponundaki EXEC olaylarının PID'lerini çıkarır.
// Yapı: nlmsghdr(16) + cn_msg(20) + proc_event{what(4)cpu(4)ts(8)=16 + veri}.
func execPidleri(buf []byte) []int {
	var out []int
	for len(buf) >= 16 {
		nlLen := binary.LittleEndian.Uint32(buf[0:])
		if nlLen < 16 || int(nlLen) > len(buf) {
			break
		}
		p := buf[16:nlLen] // cn_msg + proc_event
		if len(p) >= 20+16+4 {
			what := binary.LittleEndian.Uint32(p[20:]) // proc_event.what
			if what == procEventExec {
				pid := int(int32(binary.LittleEndian.Uint32(p[20+16:]))) // process_pid
				if pid > 0 {
					out = append(out, pid)
				}
			}
		}
		adv := (nlLen + 3) &^ 3
		if int(adv) >= len(buf) || adv == 0 {
			break
		}
		buf = buf[adv:]
	}
	return out
}

// surecDegerlendir — pid'i /proc'tan zenginleştir, puanla, gerekirse bildir.
func (a *ajan) surecDegerlendir(pid int) {
	ppid, comm := procStat(pid)
	if comm == "" {
		return
	}
	yol := procExe(pid)
	uid := procUid(pid)
	ebeveynComm := ""
	if ppid > 0 {
		_, ebeveynComm = procStat(ppid)
	}
	puan, aciklama := surecPuanla(ebeveynComm, comm, yol, uid)
	if surecDebug {
		log.Printf("DBG exec pid=%d %q→%q uid=%d yol=%q puan=%d", pid, ebeveynComm, comm, uid, yol, puan)
	}
	if puan < 30 {
		return
	}
	// Kiracı bağlamı: uid → kullanıcı adı → domains.sistem_kullanici.
	kadi := uidKullanici(uid)
	domID := int64(0)
	if strings.HasPrefix(kadi, "c_") && a.domCache != nil {
		domID = a.domCache.domainIDSysUser(kadi)
	}
	// Yalnız kiracı bağlamı ilgilendirir: root/sistem süreçlerinin kabuk açması
	// normaldir (cron, panel). Kiracı değilse ATLA.
	if domID == 0 {
		return
	}
	// Throttle: kiracı+kural başına 60 sn'de bir bildirim (exec seli spam yapmasın).
	anahtar := strconv.FormatInt(domID, 10) + ":" + aciklama
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
	log.Printf("SÜREÇ [şüpheli puan=%d] pid=%d %s → %s (uid=%s): %s", puan, pid, ebeveynComm, comm, kadi, aciklama)
	bildirimYaz(a.db, "kritik", baslik, aciklama, domID, "av_surec", 0)
}

// ── Saf puanlama (test edilebilir) ──────────────────────────────────────────

var (
	webEbeveyn = map[string]bool{"php-fpm": true, "php": true, "php-cgi": true, "lsphp": true,
		"httpd": true, "apache2": true, "apache": true, "nginx": true, "litespeed": true}
	kabuklar = map[string]bool{"bash": true, "sh": true, "dash": true, "zsh": true, "ksh": true, "ash": true}
	indirici = map[string]bool{"curl": true, "wget": true, "fetch": true, "python": true, "python3": true,
		"perl": true, "ruby": true, "nc": true, "ncat": true, "socat": true}
)

// surecPuanla — ebeveyn→çocuk exec zincirini puanlar. comm değerleri /proc'tan
// (15 karakter kırpık olabilir; php-fpm bu sınırın altında).
func surecPuanla(ebeveyn, cocuk, yol string, uid int) (int, string) {
	web := webEbeveyn[ebeveyn]
	if web && kabuklar[cocuk] {
		return 30, "web sunucusu kabuk başlattı: " + ebeveyn + " → " + cocuk
	}
	if web && indirici[cocuk] {
		return 20, "web sunucusu indirici başlattı: " + ebeveyn + " → " + cocuk
	}
	if dunyaYazilir(yol) && (web || uid >= 1000) {
		return 30, "dünya-yazılabilir dizinden çalıştırma: " + yol
	}
	return 0, ""
}

// dunyaYazilir — çalıştırılan ikili dünya-yazılabilir/geçici bir dizinde mi.
func dunyaYazilir(yol string) bool {
	y := filepath.ToSlash(yol)
	return strings.HasPrefix(y, "/tmp/") || strings.HasPrefix(y, "/dev/shm/") ||
		strings.HasPrefix(y, "/var/tmp/")
}

// ── /proc ayrıştırıcılar ────────────────────────────────────────────────────

func procStat(pid int) (int, string) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, ""
	}
	return statAyristir(string(b))
}

// statAyristir — /proc/<pid>/stat satırından (ppid, comm). comm parantez içinde
// ve BOŞLUK/PARANTEZ içerebilir → ilk '(' ile SON ')' arası alınır (klasik tuzak).
func statAyristir(s string) (int, string) {
	a := strings.IndexByte(s, '(')
	z := strings.LastIndexByte(s, ')')
	if a < 0 || z < 0 || z <= a {
		return 0, ""
	}
	comm := s[a+1 : z]
	alanlar := strings.Fields(s[z+1:]) // state ppid pgrp ...
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

func uidKullanici(uid int) string {
	if uid < 0 {
		return ""
	}
	if u, err := user.LookupId(strconv.Itoa(uid)); err == nil {
		return u.Username
	}
	return ""
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
