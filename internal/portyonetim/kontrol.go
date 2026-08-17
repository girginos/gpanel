package portyonetim

// Port kullanılabilirlik kontrolü — `ss -tln` + kendi panel portumuzu
// bilerek göz ardı et.

import (
	"bufio"
	"os/exec"
	"strconv"
	"strings"
)

// PortMesgul — sunucuda başka bir servis bu portu dinliyor mu?
// gozardi parametresi: kendi backend/dış portumuz olabilir (değişiklik
// senaryosu için).
func PortMesgul(port int, gozardi ...int) bool {
	if port < 1 || port > 65535 {
		return false // aralık dışı → meşgul değil; validasyon PortYasakMi'de yapılır
	}
	set := SistemdePortlar()
	for _, g := range gozardi {
		delete(set, g)
	}
	return set[port]
}

// SistemdePortlar — `ss -tln` çıktısından tüm dinlenen portlar.
func SistemdePortlar() map[int]bool {
	out := map[int]bool{}
	c, err := exec.Command("ss", "-tlnH").Output()
	if err != nil {
		return out
	}
	sc := bufio.NewScanner(strings.NewReader(string(c)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 {
			continue
		}
		// f[3] = "127.0.0.1:8080" veya "*:8443" veya "[::]:22"
		local := f[3]
		if i := strings.LastIndex(local, ":"); i > 0 {
			if p, e := strconv.Atoi(local[i+1:]); e == nil {
				out[p] = true
			}
		}
	}
	return out
}

// PortAcikTest — belirli bir porta TCP connect edebilir miyiz? (curl'a
// başvurmadan önce hızlı sağlık kontrolü).
func PortAcikTest(host string, port int) bool {
	c, err := exec.Command("bash", "-c",
		"timeout 2 bash -c 'echo > /dev/tcp/"+host+"/"+strconv.Itoa(port)+"' && echo OK").Output()
	return err == nil && strings.TrimSpace(string(c)) == "OK"
}
