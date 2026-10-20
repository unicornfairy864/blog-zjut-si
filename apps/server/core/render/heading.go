package render

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
)

const (
	// headingIDMaxLen 是自动锚点的最大长度，避免超长标题产出过长 id。
	headingIDMaxLen = 64
	// headingFallbackPrefix 给纯中文标题兜底；拼音库不在 §17.2 白名单。
	headingFallbackPrefix = "h-"
)

// headingID 计算标题锚点；同名标题按出现顺序追加 -2、-3。
//
// 遍历文档只统计当前标题之前的同名标题，因此同一份文档多次渲染结果一致，
// 渲染器本身不需要保存状态（goldmark 的 renderer 是并发共享的）。
func headingID(heading *ast.Heading, source []byte) string {
	base := headingBaseID(headingText(heading, source))

	count := 0
	_ = ast.Walk(heading.OwnerDocument(), func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if node == heading {
			return ast.WalkStop, nil
		}
		if other, ok := node.(*ast.Heading); ok && headingBaseID(headingText(other, source)) == base {
			count++
		}
		return ast.WalkContinue, nil
	})

	if count > 0 {
		return base + "-" + strconv.Itoa(count+1)
	}
	return base
}

// headingText 收集标题内所有文本节点（含强调、行内代码与公式原文），不依赖已弃用的 Node.Text。
func headingText(node ast.Node, source []byte) string {
	var sb strings.Builder
	_ = ast.Walk(node, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch typed := child.(type) {
		case *ast.Text:
			sb.Write(typed.Segment.Value(source))
		case *mathNode:
			// 公式已经被换成占位节点，锚点仍按 TeX 原文算，保证 id 稳定。
			sb.Write(typed.tex)
		}
		return ast.WalkContinue, nil
	})
	return sb.String()
}

// headingBaseID 优先用 ASCII slug；中文标题退化为内容哈希。
func headingBaseID(text string) string {
	var sb strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '-' || r == '_':
			if sb.Len() > 0 && !lastDash {
				sb.WriteByte('-')
				lastDash = true
			}
		}
	}

	slug := strings.Trim(sb.String(), "-")
	if len(slug) >= 2 {
		if len(slug) > headingIDMaxLen {
			slug = strings.TrimRight(slug[:headingIDMaxLen], "-")
		}
		return slug
	}

	sum := sha256.Sum256([]byte(text))
	return headingFallbackPrefix + hex.EncodeToString(sum[:4])
}
