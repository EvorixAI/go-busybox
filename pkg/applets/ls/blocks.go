//go:build !js && !wasm && !wasip1

package ls

import (
	"fmt"
	"io/fs"
	"os/user"
	"syscall"
)

func getBlocksFromStat(info fs.FileInfo) int64 {
	if sys := info.Sys(); sys != nil {
		if stat, ok := sys.(*syscall.Stat_t); ok {
			return stat.Blocks / 2
		}
	}
	return (info.Size() + 4095) / 4096 * 4
}

func getStatInfo(info fs.FileInfo) (nlink uint64, owner, group string) {
	nlink = 1
	owner = "?"
	group = "?"
	if sys := info.Sys(); sys != nil {
		if stat, ok := sys.(*syscall.Stat_t); ok {
			nlink = stat.Nlink
			if u, err := user.LookupId(fmt.Sprintf("%d", stat.Uid)); err == nil {
				owner = u.Username
			} else {
				owner = fmt.Sprintf("%d", stat.Uid)
			}
			if g, err := user.LookupGroupId(fmt.Sprintf("%d", stat.Gid)); err == nil {
				group = g.Name
			} else {
				group = fmt.Sprintf("%d", stat.Gid)
			}
		}
	}
	return
}
