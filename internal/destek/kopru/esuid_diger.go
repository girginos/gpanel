//go:build !linux

package kopru

import (
	"errors"
	"net"
)

// esUIDOku — Linux dışında eş kimliği doğrulanamaz → HER bağlantı reddedilir
// (kapalı başarısız). Köprü yalnız Linux sunucuda çalışır.
func esUIDOku(*net.UnixConn) (uint32, error) { return 0, errors.New("SO_PEERCRED yalnız Linux") }
