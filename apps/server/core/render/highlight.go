package render

import (
	"io"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

var (
	// codeFormatter 输出 class 而不是内联样式：CSP 与 bluemonday 都不允许 style。
	codeFormatter = chromahtml.New(chromahtml.WithClasses(true))
	// codeStyle 与 apps/web/styles/chroma.css 必须同源（github 风格）。
	codeStyle = styles.Get("github")
)

// writeHighlighted 用 chroma 渲染围栏代码块；未知语言退化为纯文本。
func writeHighlighted(w io.Writer, language string, code []byte) error {
	lexer := lexers.Get(language)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, string(code))
	if err != nil {
		return err
	}
	style := codeStyle
	if style == nil {
		style = styles.Fallback
	}
	return codeFormatter.Format(w, style, iterator)
}
