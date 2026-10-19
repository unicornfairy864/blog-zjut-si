package render

import (
	"bytes"
	"regexp"
	"strconv"

	"github.com/yuin/goldmark/ast"
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

// mathTransformer 在解析完成后把段落里的 $...$ 换成 mathNode。
type mathTransformer struct{}

// Transform 实现 parser.ASTTransformer。
func (mathTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if paragraph, ok := node.(*ast.Paragraph); ok {
			replaceParagraphMath(paragraph, source)
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
	replaceMathInParagraph(parent, source)
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

// replaceMathInParagraph 只处理段落的直接 Text 子节点；代码块与代码段不参与。
func replaceMathInParagraph(parent *ast.Paragraph, source []byte) {
	for child := parent.FirstChild(); child != nil; {
		next := child.NextSibling()
		if textNode, ok := child.(*ast.Text); ok {
			replaceTextWithMath(parent, textNode, source)
		}
		child = next
	}
}

// replaceTextWithMath 把一段文本按公式切分，并保留原节点的换行语义。
func replaceTextWithMath(parent *ast.Paragraph, node *ast.Text, source []byte) {
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
			nodes = append(nodes, ast.NewTextSegment(text.NewSegment(base+cursor, base+match[0])))
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
		nodes = append(nodes, ast.NewTextSegment(text.NewSegment(base+cursor, base+len(value))))
	}

	// 原节点末尾的软/硬换行必须留在最后一个节点上，否则段落会被粘成一行。
	if node.SoftLineBreak() || node.HardLineBreak() {
		if last, ok := nodes[len(nodes)-1].(*ast.Text); ok {
			last.SetSoftLineBreak(node.SoftLineBreak())
			last.SetHardLineBreak(node.HardLineBreak())
		} else {
			tail := ast.NewTextSegment(text.NewSegment(base+len(value), base+len(value)))
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
