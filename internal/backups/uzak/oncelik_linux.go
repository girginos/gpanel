//go:build linux

package uzak

import (
	"runtime"
	"syscall"
)

// ioprioSet — SYS_IOPRIO_SET (linux): sınıf IDLE(3), who=thread.
const (
	ioprioWhoProcess = 1
	ioprioClassIdle  = 3
	ioprioShift      = 13
)

// DusukOncelikThread — çağıran goroutine'i bir OS iş parçacığına SABİTLER ve o
// parçacığı nice 19 + ioprio idle yapar (yalnız bu parçacık; süreç etkilenmez).
// 🔴 Sabitleme ASLA bırakılmaz: goroutine biterken parçacık Go tarafından yok edilir;
// bırakılsaydı düşük öncelikli parçacık başka goroutine'lere kalırdı. En iyi çaba.
// Şifreleme ve dosya okuma çağıran goroutine'de koştuğu için yedeğin disk/CPU
// önceliğini bozmaz (ağ gönderimi hedefin kendi goroutine'lerinde kalır).
func DusukOncelikThread() {
	runtime.LockOSThread()
	tid := syscall.Gettid()
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, tid, 19)
	_, _, _ = syscall.Syscall(syscall.SYS_IOPRIO_SET, ioprioWhoProcess, uintptr(tid), uintptr(ioprioClassIdle<<ioprioShift))
}

// BosAlan — yolun bulunduğu dosya sisteminde yazılabilir boş bayt.
func BosAlan(yol string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(yol, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil //nolint:gosec // G115: Bsize pozitif
}
