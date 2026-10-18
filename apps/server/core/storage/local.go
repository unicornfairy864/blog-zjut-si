// Package storage 提供本地磁盘存储的路径约束与原子写入。
//
// 契约（agent.md §10 / C10）：图片只落盘，数据库只存 storage_key 与元数据。
package storage

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrInvalidPath 表示相对路径越出存储根目录。
var ErrInvalidPath = errors.New("非法存储路径")

// Local 是以 Root 为根的本地存储。
type Local struct {
	Root string
}

// Path 把 storage 相对路径解析为绝对路径，并阻止 ../ 越界。
func (l Local) Path(rel string) (string, error) {
	root, err := filepath.Abs(l.Root)
	if err != nil {
		return "", err
	}
	joined := filepath.Join(root, filepath.FromSlash(strings.TrimLeft(rel, "/")))
	if joined != root && !strings.HasPrefix(joined, root+string(os.PathSeparator)) {
		return "", ErrInvalidPath
	}
	return joined, nil
}

// WriteAtomic 先写临时文件再 rename，避免半截文件被读到。
func WriteAtomic(path string, write func(io.Writer) error, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}

	temp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer func() { _ = os.Remove(name) }()

	if err := write(temp); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Chmod(perm); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
