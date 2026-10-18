// Package render 把 Markdown 渲染为可直接入库的 HTML 与纯文本。
//
// 契约（agent.md §11）：goldmark + bluemonday，content_html 落库，读路径不重复渲染；
// 代码块用 chroma 高亮（输出 class，CSS 在 apps/web/styles/chroma.css）；
// 标题自动生成稳定锚点；数学只输出 span.math[data-tex] 占位，不在服务端渲染 KaTeX。
package render

import (
	"bytes"
	"html"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// WordsPerMinute 是阅读时长的换算基准（agent.md §11：每 300 字 1 分钟）。
const WordsPerMinute = 300

// Result 是一次渲染的产物，四个字段都直接落库。
type Result struct {
	HTML        string
	Text        string
	WordCount   int
	ReadingTime int
}

var (
	rendererOnce sync.Once
	markdown     goldmark.Markdown
	sanitizer    *bluemonday.Policy
)

// Markdown 渲染 Markdown 源码；输入为空时返回全零结果。
func Markdown(src string) Result {
	rendererOnce.Do(initRenderer)

	var buf bytes.Buffer
	if err := markdown.Convert([]byte(src), &buf); err != nil {
		// 渲染失败时返回空结果，让调用方落库空内容而不是 500。
		return Result{}
	}

	markup := string(sanitizer.SanitizeBytes(buf.Bytes()))
	text := toText(markup)
	words := countWords(text)
	return Result{
		HTML:        markup,
		Text:        text,
		WordCount:   words,
		ReadingTime: readingTime(words),
	}
}

// initRenderer 构建一次 goldmark 与 bluemonday 实例；两者并发只读，可安全复用。
func initRenderer() {
	markdown = goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(
			// 数学占位在解析后做 AST 变换，只影响普通段落。
			parser.WithASTTransformers(util.Prioritized(mathTransformer{}, 100)),
		),
		goldmark.WithRenderer(renderer.NewRenderer(
			renderer.WithNodeRenderers(
				util.Prioritized(gmhtml.NewRenderer(), 1000),
				// 数字更小的先注册、后覆盖：用 400 覆盖标题、代码块与数学节点。
				util.Prioritized(&blogNodeRenderer{}, 400),
			),
		)),
	)

	policy := bluemonday.NewPolicy()
	policy.AllowStandardURLs()

	// 链接与图片：只允许 http/https/mailto 与相对路径，禁止 javascript: 等伪协议。
	policy.AllowAttrs("href").OnElements("a")
	policy.AllowAttrs("alt", "src", "title", "width", "height", "loading").OnElements("img")

	// class/id 供标题锚点与 chroma 高亮使用；data-tex 是 KaTeX 的占位属性（§11）。
	policy.AllowAttrs("class", "id", "data-tex", "data-lang").Globally()

	// GFM 任务列表会输出 input[type=checkbox]。
	policy.AllowAttrs("type", "checked", "disabled").OnElements("input")
	policy.AllowAttrs("colspan", "rowspan", "align").OnElements("td", "th")

	policy.AllowElements(
		"p", "br", "hr",
		"h1", "h2", "h3", "h4", "h5", "h6",
		"ul", "ol", "li", "dl", "dt", "dd",
		"blockquote", "pre", "code",
		"strong", "em", "del", "s", "mark", "kbd", "sup", "sub", "small", "abbr",
		"span", "div",
		"table", "thead", "tbody", "tr", "th", "td",
		"figure", "figcaption",
		"a", "img", "input",
		"details", "summary",
	)

	// 正文链接不加 rel="nofollow"：站内链接不应被降权（§11 SEO）。
	policy.RequireNoFollowOnLinks(false)
	policy.RequireNoFollowOnFullyQualifiedLinks(false)
	policy.RequireNoReferrerOnFullyQualifiedLinks(false)

	sanitizer = policy
}

var (
	blockTagPattern = regexp.MustCompile(`(?i)</(?:p|div|h[1-6]|li|blockquote|pre|tr|table|thead|tbody|ul|ol|figure|figcaption|dd|dt)>|<br\s*/?>`)
	tagPattern      = regexp.MustCompile(`<[^>]*>`)
	spacePattern    = regexp.MustCompile(`[ \t\f\v]+`)
	newlinePattern  = regexp.MustCompile(`\n{3,}`)
)

// toText 从已消毒的 HTML 提取纯文本，用于全文检索兜底、摘要与字数统计。
func toText(markup string) string {
	s := blockTagPattern.ReplaceAllString(markup, "\n")
	s = tagPattern.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = spacePattern.ReplaceAllString(s, " ")

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	s = strings.Join(lines, "\n")
	s = newlinePattern.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// countWords 按 §11 统计字数：中文字符逐个计数，英文按单词计数。
func countWords(text string) int {
	count := 0
	inWord := false
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			count++
			inWord = false
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if !inWord {
				count++
				inWord = true
			}
		default:
			inWord = false
		}
	}
	return count
}

// readingTime 返回向上取整的阅读分钟数；空正文为 0。
func readingTime(words int) int {
	if words <= 0 {
		return 0
	}
	return (words + WordsPerMinute - 1) / WordsPerMinute
}
