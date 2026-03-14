//go:build !windows

package update

import "syscall"

func diskFreeBytesImpl(path string) int64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return -1
	}
	// Bavail and Bsize types differ between Linux (uint64/int64) and
	// Darwin (uint32/int32), but both convert safely to int64.
	return int64(st.Bavail) * int64(st.Bsize) //nolint:unconvert
}
