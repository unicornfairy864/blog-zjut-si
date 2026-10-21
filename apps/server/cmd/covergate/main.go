// Command covergate 按包检查 Go 覆盖率剖面的语句覆盖率，不达标即退出 1。
//
// 为什么用 Go 而不是 shell：本地（Windows）与 CI / 容器（Linux）要跑同一条门禁，
// 覆盖率口径必须一致，而 `go run ./cmd/covergate` 两边都能执行。
//
// 用法：
//
//	go test ./... -covermode=atomic -coverprofile=coverage.out
//	go run ./cmd/covergate -profile=coverage.out -pkg=service -min=70
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

// packageCoverage 是一个包的语句覆盖率汇总。
type packageCoverage struct {
	dir        string
	statements int
	covered    int
}

// percent 返回语句覆盖率百分比；没有语句的包按 100 处理（不惩罚空包）。
func (c packageCoverage) percent() float64 {
	if c.statements == 0 {
		return 100
	}
	return float64(c.covered) * 100 / float64(c.statements)
}

// parseProfile 汇总 `go test -coverprofile` 的产物。
//
// 每行形如 `github.com/x/y/service/post.go:12.3,14.5 2 1`：末两列是语句数与命中次数，
// 文件名里**最后一个冒号**之前才是文件路径（Windows 上盘符也带冒号，所以不能取第一个）。
func parseProfile(r io.Reader) (map[string]*packageCoverage, error) {
	packages := make(map[string]*packageCoverage)

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("无法解析覆盖率行：%q", line)
		}
		colon := strings.LastIndex(fields[0], ":")
		if colon <= 0 {
			return nil, fmt.Errorf("无法解析覆盖率行的文件名：%q", line)
		}
		statements, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("无法解析语句数：%q", line)
		}
		hits, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("无法解析命中次数：%q", line)
		}

		dir := path.Dir(fields[0][:colon])
		entry, ok := packages[dir]
		if !ok {
			entry = &packageCoverage{dir: dir}
			packages[dir] = entry
		}
		entry.statements += statements
		if hits > 0 {
			entry.covered += statements
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取覆盖率剖面失败: %w", err)
	}
	if len(packages) == 0 {
		return nil, errors.New("覆盖率剖面里没有任何数据")
	}
	return packages, nil
}

// lookup 按包名（目录最后一段）找包；同名多个时取覆盖率最低的那个（保守取值）。
func lookup(packages map[string]*packageCoverage, name string) (*packageCoverage, bool) {
	var found *packageCoverage
	for _, entry := range packages {
		if path.Base(entry.dir) != name {
			continue
		}
		if found == nil || entry.percent() < found.percent() {
			found = entry
		}
	}
	return found, found != nil
}

func main() {
	profilePath := flag.String("profile", "coverage.out", "覆盖率剖面文件（go test -coverprofile 的产物）")
	pkgName := flag.String("pkg", "service", "要检查的包（按目录最后一段匹配）")
	minPercent := flag.Float64("min", 70, "最低语句覆盖率（%）")
	verbose := flag.Bool("v", false, "打印所有包的覆盖率")

	flag.Parse()

	file, err := os.Open(*profilePath)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "打开覆盖率剖面失败: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = file.Close() }()

	packages, err := parseProfile(file)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	if *verbose {
		entries := make([]*packageCoverage, 0, len(packages))
		for _, entry := range packages {
			entries = append(entries, entry)
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].dir < entries[j].dir })
		for _, entry := range entries {
			_, _ = fmt.Fprintf(os.Stdout, "%6.1f%%  %s\n", entry.percent(), entry.dir)
		}
	}

	target, ok := lookup(packages, *pkgName)
	if !ok {
		_, _ = fmt.Fprintf(os.Stderr, "覆盖率剖面里没有包 %q\n", *pkgName)
		os.Exit(1)
	}

	_, _ = fmt.Fprintf(os.Stdout, "%s 覆盖率：%.1f%%（%d/%d 语句，门禁 %.1f%%）\n",
		*pkgName, target.percent(), target.covered, target.statements, *minPercent)
	if target.percent() < *minPercent {
		_, _ = fmt.Fprintf(os.Stderr, "覆盖率不达标：%s 需要 ≥%.1f%%，实际 %.1f%%\n",
			*pkgName, *minPercent, target.percent())
		os.Exit(1)
	}
}
