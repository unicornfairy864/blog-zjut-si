package render

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// imageAttrPattern 匹配紧跟在图片后的 `{width=480}` / `{width=480 height=270}`。
//
// 为什么自己解析：goldmark 的 `parser.WithAttribute()` 只覆盖标题一类块级元素，
// 行内属性（图片宽度）不在核心里；这里在 AST 变换阶段把它摘下来挂到图片节点上，
// 由默认渲染器的 ImageAttributeFilter（白名单含 width/height）输出（§11）。
var imageAttrPattern = regexp.MustCompile(`^\{([^}\n]*)\}`)

// imageAttrNamePattern 匹配 `width=480` / `height="270"` 这类键值对。
var imageAttrNamePattern = regexp.MustCompile(`^(width|height)\s*=\s*"?(\d{1,5})"?$`)

// imageAttrMaxPixels 是允许写进 Markdown 的最大边长，避免离谱数值撑破版式。
const imageAttrMaxPixels = 20000

// imageAttrTransformer 把图片后面的 `{width=…}` 属性挂到图片节点上。
type imageAttrTransformer struct{}

// Transform 实现 parser.ASTTransformer。
func (imageAttrTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if image, ok := node.(*ast.Image); ok {
			applyImageAttributes(image, source)
		}
		return ast.WalkContinue, nil
	})
}

// applyImageAttributes 读取图片紧随其后的文本节点；命中属性语法就挂到图片上，
// 并把已经消费掉的 `{…}` 从文本里剪掉（剪空则删除该文本节点）。
func applyImageAttributes(image *ast.Image, source []byte) {
	parent := image.Parent()
	if parent == nil {
		return
	}
	next, ok := image.NextSibling().(*ast.Text)
	if !ok {
		return
	}

	value := next.Segment.Value(source)
	match := imageAttrPattern.FindStringSubmatch(string(value))
	if match == nil {
		return
	}
	attrs := parseImageAttributes(match[1])
	if len(attrs) == 0 {
		return
	}
	for name, raw := range attrs {
		image.SetAttribute([]byte(name), raw)
	}

	// 整段都是属性时直接删掉文本节点，避免输出多余的 `{…}` 原文。
	rest := value[len(match[0]):]
	if len(rest) == 0 {
		parent.RemoveChild(parent, next)
		return
	}
	next.Segment = text.NewSegment(next.Segment.Start+len(match[0]), next.Segment.Stop)
}

// parseImageAttributes 解析 `width=480 height=270`；非法项直接丢弃（不报错，正文照常渲染）。
//
// 值统一用字符串：goldmark 的 RenderAttributes 只认识 string / []byte / bool 一类值。
func parseImageAttributes(raw string) map[string]string {
	attrs := make(map[string]string, 2)
	for _, field := range strings.Fields(raw) {
		parts := imageAttrNamePattern.FindStringSubmatch(field)
		if parts == nil {
			continue
		}
		size, err := strconv.Atoi(parts[2])
		if err != nil || size <= 0 || size > imageAttrMaxPixels {
			continue
		}
		attrs[parts[1]] = strconv.Itoa(size)
	}
	return attrs
}
