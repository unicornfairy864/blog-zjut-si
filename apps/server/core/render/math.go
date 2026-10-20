package render

import (
	"bytes"
	"regexp"
	"strconv"

	"github.com/yuin/goldmark/ast"
	extensionast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// kindMath 是自定义行内节点类型；只承载 TeX 原文，前端 KaTeX 负责渲染。
var kindMath = ast.NewNodeKind("Math")

// mathNode 表示一段 $...$ 或 $$...$$。
type mathNode struct {
	ast.BaseInline
	tex   []byte
	block bool
}

// Kind 实现 ast.Node。
func (n *mathNode) Kind() ast.NodeKind { return kindMath }

// Dump 实现 ast.Node，只在调试 AST 时使用。
func (n *mathNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{
		"Tex":   string(n.tex),
		"Block": strconv.FormatBool(n.block),
	}, nil)
}

var (
	// mathPattern 匹配 $$...$$（块级）与 $...$（行内）；内容不含换行与 $，
	// 行内公式要求首尾非空白，避免把 "$5 和 $10" 这类价格误判成公式。
	//
	// 跨行的 $$ 块（Typora 写法）由 wholeParagraphMath 在段落层面处理，不走这条正则。
	mathPattern = regexp.MustCompile(`\$\$([^$\n]+?)\$\$|\$([^$\n\s](?:[^$\n]*[^$\n\s])?)\$`)
)

// mathTransformer 在解析完成后把行内容器里的 $...$ 换成 mathNode。
type mathTransformer struct{}

// Transform 实现 parser.ASTTransformer。
//
// 公式可以出现在任何行内容器里：段落、紧凑列表项（goldmark 用 TextBlock 而不是 Paragraph）、
// 标题、表格单元格。少处理一种就会出现「列表里的公式不渲染」这类问题（§11）。
func (mathTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch typed := node.(type) {
		case *ast.Paragraph:
			replaceParagraphMath(typed, source)
		case *ast.TextBlock:
			replaceMathInInline(typed, source)
		case *ast.Heading:
			replaceMathInInline(typed, source)
		case *extensionast.TableCell:
			replaceMathInInline(typed, source)
		}
		return ast.WalkContinue, nil
	})
}

// replaceParagraphMath 先判断整段是不是一个块级公式，不是再逐个子节点替换行内公式。
//
// Typora 写的跨行块公式
//
//	$$
//	\int_0^1 x^2\,dx
//	$$
//
// 会被 goldmark 拆成多个带软换行的 Text 子节点，任何单节点正则都匹配不到，
// 所以只能在段落层面按源码区间判断（§11）。
func replaceParagraphMath(parent *ast.Paragraph, source []byte) {
	if tex, ok := wholeParagraphMath(parent, source); ok {
		parent.RemoveChildren(parent)
		parent.AppendChild(parent, &mathNode{tex: tex, block: true})
		return
	}
	replaceMathInInline(parent, source)
}

// wholeParagraphMath 判断段落是否整体就是 `$$…$$`；是则返回去掉定界符并修剪过的 TeX。
//
// 只接受纯 Text 子节点：段落里混了强调、代码段等行内节点时不做整段替换，
// 免得把正常正文吃掉。
func wholeParagraphMath(parent *ast.Paragraph, source []byte) ([]byte, bool) {
	var (
		start, stop int
		seen        bool
	)
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		textNode, ok := child.(*ast.Text)
		if !ok {
			return nil, false
		}
		if !seen {
			start, stop, seen = textNode.Segment.Start, textNode.Segment.Stop, true
			continue
		}
		if textNode.Segment.Start < start {
			start = textNode.Segment.Start
		}
		if textNode.Segment.Stop > stop {
			stop = textNode.Segment.Stop
		}
	}
	if !seen || stop <= start || stop > len(source) {
		return nil, false
	}

	raw := bytes.TrimSpace(source[start:stop])
	if len(raw) < 4 || !bytes.HasPrefix(raw, []byte("$$")) || !bytes.HasSuffix(raw, []byte("$$")) {
		return nil, false
	}
	tex := bytes.TrimSpace(raw[2 : len(raw)-2])
	if len(tex) == 0 || bytes.ContainsRune(tex, '$') {
		return nil, false
	}
	return tex, true
}

// replaceMathInInline 处理一个行内容器（段落 / 紧凑列表项 / 标题 / 表格单元格）。
//
// 匹配前必须先把相邻的 Text 兄弟合回去：goldmark 在强调定界符处会把一段文本切开
// （`$a_i, b_i$` 会被切成 `$a_` / `i, b_` / `i$` 三个节点），公式横跨多个节点时
// 单节点正则永远匹配不到，表现就是「带下标的公式不渲染」（§11）。
func replaceMathInInline(parent ast.Node, source []byte) {
	mergeAdjacentText(parent, source)
	for child := parent.FirstChild(); child != nil; {
		next := child.NextSibling()
		if textNode, ok := child.(*ast.Text); ok {
			replaceTextWithMath(parent, textNode, source)
		}
		child = next
	}
}

// mergeAdjacentText 合并相邻且源码连续的 Text 兄弟节点。
//
// 只合并紧挨着的 Text：中间夹了强调、行内代码、链接等节点时不动它们，
// 所以 `$x$ **粗** $y$` 这种混排不会把标记吃掉。换行（软换行）会让两个节点不再连续，
// 也就不会被合并。
func mergeAdjacentText(parent ast.Node, source []byte) {
	for child := parent.FirstChild(); child != nil; {
		textNode, ok := child.(*ast.Text)
		if !ok {
			child = child.NextSibling()
			continue
		}
		for {
			next, isText := textNode.NextSibling().(*ast.Text)
			if !isText || !textNode.Merge(next, source) {
				break
			}
			parent.RemoveChild(parent, next)
		}
		child = textNode.NextSibling()
	}
}

// replaceTextWithMath 把一段文本按公式切分，并保留原节点的换行语义。
func replaceTextWithMath(parent ast.Node, node *ast.Text, source []byte) {
	value := node.Segment.Value(source)
	matches := mathPattern.FindAllSubmatchIndex(value, -1)
	if len(matches) == 0 {
		return
	}

	base := node.Segment.Start
	nodes := make([]ast.Node, 0, len(matches)*2+1)
	cursor := 0
	for _, match := range matches {
		if match[0] > cursor {
			nodes = append(nodes, newTextSegment(base+cursor, base+match[0], node.IsRaw()))
		}

		block := match[2] >= 0
		start, end := match[4], match[5]
		if block {
			start, end = match[2], match[3]
		}
		nodes = append(nodes, &mathNode{tex: value[start:end], block: block})
		cursor = match[1]
	}
	if cursor < len(value) {
		nodes = append(nodes, newTextSegment(base+cursor, base+len(value), node.IsRaw()))
	}

	// 原节点末尾的软/硬换行必须留在最后一个节点上，否则段落会被粘成一行。
	if node.SoftLineBreak() || node.HardLineBreak() {
		if last, ok := nodes[len(nodes)-1].(*ast.Text); ok {
			last.SetSoftLineBreak(node.SoftLineBreak())
			last.SetHardLineBreak(node.HardLineBreak())
		} else {
			tail := newTextSegment(base+len(value), base+len(value), node.IsRaw())
			tail.SetSoftLineBreak(node.SoftLineBreak())
			tail.SetHardLineBreak(node.HardLineBreak())
			nodes = append(nodes, tail)
		}
	}

	parent.ReplaceChild(parent, node, nodes[0])
	previous := nodes[0]
	for _, replacement := range nodes[1:] {
		parent.InsertAfter(parent, previous, replacement)
		previous = replacement
	}
}

// newTextSegment 建一个文本节点，并沿用原节点的 raw 标记（如 `\&` 这类转义）。
func newTextSegment(start, stop int, raw bool) *ast.Text {
	segment := ast.NewTextSegment(text.NewSegment(start, stop))
	segment.SetRaw(raw)
	return segment
}
