//go:build linux

package kopru

import (
	"errors"
	"net"
	"syscall"
)

// esUIDOku — SO_PEERCRED ile bağlanan sürecin uid'si (çekirdek bildirir; taklit edilemez).
func esUIDOku(c *net.UnixConn) (uint32, error) {
	rc, err := c.SyscallConn()
	if err != nil {
		return 0, err
	}
	var uc *syscall.Ucred
	var serr error
	if err := rc.Control(func(fd uintptr) {
		uc, serr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if serr != nil {
		return 0, serr
	}
	if uc == nil {
		return 0, errors.New("eş kimliği alınamadı")
	}
	return uc.Uid, nil
}
