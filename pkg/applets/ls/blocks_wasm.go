//go:build js || wasm || wasip1

package ls

import "io/fs"

func getBlocksFromStat(info fs.FileInfo) int64 {
	return (info.Size() + 4095) / 4096 * 4
}

func getStatInfo(info fs.FileInfo) (nlink uint64, owner, group string) {
	return 1, "agent", "agent"
}
