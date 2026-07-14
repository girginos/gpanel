package files

import (
	"os"
	"os/user"
	"strconv"
)

type usrInfo struct{ UID, GID int }

func userLookup(name string) (usrInfo, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return usrInfo{}, err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return usrInfo{UID: uid, GID: gid}, nil
}

func osChown(path string, uid, gid int) error {
	return os.Chown(path, uid, gid)
}
