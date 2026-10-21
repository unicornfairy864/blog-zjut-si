package render

import (
	"regexp"
	"strings"
	"unicode"
)

// 代码块的文件名标签（agent.md §11「代码块」）。
//
// 只在围栏 info string 里认两种**显式**写法，语言名照旧是第一个词：
//
//	```go title="main.go"
//	```go [main.go]
//
// 不认 ```go main.go 这种裸词：info string 里除了语言还可能有别的渲染选项，
// 猜错会把选项当文件名显示出来。
var (
	codeTitleAttrPattern    = regexp.MustCompile(`(?i)(?:^|\s)title\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"']+))`)
	codeTitleBracketPattern = regexp.MustCompile(`^\[([^\]\n]+)\]$`)
)

// codeTitleMaxRunes 是文件名标签的长度上限；超长直接截断，不让一行标题撑破版式。
const codeTitleMaxRunes = 120

// codeBlockTitle 从围栏 info string 里取文件名标签；没有则返回空串。
func codeBlockTitle(info string) string {
	if strings.TrimSpace(info) == "" {
		return ""
	}

	// 属性写法在整串里找：`title=…` 也可能就是第一个词（即没写语言）。
	if match := codeTitleAttrPattern.FindStringSubmatch(info); match != nil {
		for _, group := range match[1:] {
			if group != "" {
				return cleanCodeTitle(group)
			}
		}
	}

	// 方括号写法只认语言之后的那个词；整串再看一次是为了兼容 ```[main.go]。
	for _, candidate := range []string{infoWithoutLanguage(info), info} {
		if match := codeTitleBracketPattern.FindStringSubmatch(strings.TrimSpace(candidate)); match != nil {
			return cleanCodeTitle(match[1])
		}
	}
	return ""
}

// infoWithoutLanguage 去掉 info string 开头的语言名，返回其余部分。
func infoWithoutLanguage(info string) string {
	trimmed := strings.TrimSpace(info)
	if i := strings.IndexFunc(trimmed, unicode.IsSpace); i >= 0 {
		return trimmed[i+1:]
	}
	return ""
}

// cleanCodeTitle 把换行与制表符压成空格、丢掉其余控制字符，再按 rune 截断。
func cleanCodeTitle(raw string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		default:
			return r
		}
	}, raw)
	cleaned = strings.TrimSpace(cleaned)

	runes := []rune(cleaned)
	if len(runes) > codeTitleMaxRunes {
		return string(runes[:codeTitleMaxRunes])
	}
	return cleaned
}
