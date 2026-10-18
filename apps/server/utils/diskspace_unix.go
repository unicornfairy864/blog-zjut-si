//go:build !windows

package utils

import (
	"fmt"
	"syscall"
)

// DiskUsagePercent 返回 dir 所在文件系统的已用百分比；负数表示当前平台无法统计。
func DiskUsagePercent(dir string) (int, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return -1, fmt.Errorf("统计磁盘占用失败: %w", err)
	}
	if stat.Blocks == 0 {
		return -1, nil
	}
	used := stat.Blocks - stat.Bavail
	return int(used * 100 / stat.Blocks), nil
}
