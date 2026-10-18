//go:build windows

package utils

// DiskUsagePercent 在 Windows 上不做统计（生产运行在 Linux 容器里）。
func DiskUsagePercent(string) (int, error) {
	return -1, nil
}
