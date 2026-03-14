//go:build windows

package update

// diskFreeBytesImpl returns -1 on Windows; disk space checks are skipped.
func diskFreeBytesImpl(_ string) int64 { return -1 }
