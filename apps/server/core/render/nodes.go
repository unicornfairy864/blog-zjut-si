package render

import (
	"fmt"
	"html"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// blogNodeRenderer 覆盖标题、围栏代码块与数学节点；其余节点沿用 goldmark 默认渲染。
type blogNodeRenderer struct{}

// RegisterFuncs 实现 renderer.NodeRenderer。
func (r *blogNodeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindHeading, r.renderHeading)
	reg.Register(ast.KindFencedCodeBlock, r.renderFencedCodeBlock)
	reg.Register(kindMath, r.renderMath)
}

// renderHeading 输出带稳定锚点的标题（§11：正文标题自动加 id）。
func (r *blogNodeRenderer) renderHeading(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	heading := node.(*ast.Heading)
	if !entering {
		_, err := fmt.Fprintf(w, "</h%d>\n", heading.Level)
		return ast.WalkContinue, err
	}
	_, err := fmt.Fprintf(w, `<h%d id="%s">`, heading.Level, html.EscapeString(headingID(heading, source)))
	return ast.WalkContinue, err
}

// renderFencedCodeBlock 用 chroma 输出带 class 的高亮 HTML；失败时退化为转义文本。
func (r *blogNodeRenderer) renderFencedCodeBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	block := node.(*ast.FencedCodeBlock)
	code := block.Lines().Value(source)

	if err := writeHighlighted(w, string(block.Language(source)), code); err != nil {
		_, _ = fmt.Fprintf(w, "<pre><code>%s</code></pre>\n", html.EscapeString(string(code)))
	}
	// 代码块的行由 chroma 完整输出，跳过子节点（每行一个 Text）。
	return ast.WalkSkipChildren, nil
}

// renderMath 只输出占位 span，KaTeX 由前端懒加载替换（§11）。
func (r *blogNodeRenderer) renderMath(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	math := node.(*mathNode)
	tex := html.EscapeString(string(math.tex))

	class := "math"
	delimiter := "$"
	if math.block {
		class = "math math-display"
		delimiter = "$$"
	}
	_, err := fmt.Fprintf(w, `<span class="%s" data-tex="%s">%s%s%s</span>`, class, tex, delimiter, tex, delimiter)
	return ast.WalkContinue, err
}
