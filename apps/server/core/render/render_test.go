package render

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkdown_EmptyInput(t *testing.T) {
	t.Parallel()

	got := Markdown("")
	assert.Equal(t, Result{}, got)
}

func TestMarkdown_CodeBlockStructure(t *testing.T) {
	t.Parallel()

	got := Markdown("```go\npackage main\n\nfunc main() {}\n```\n")
	t.Logf("chroma 输出：\n%s", got.HTML)

	// 行号由 apps/web/styles/prose.css 的计数器生成，前提是 chroma 逐行输出 .line/.cl。
	assert.Equal(t, 3, strings.Count(got.HTML, `<span class="line">`))
	assert.Equal(t, 3, strings.Count(got.HTML, `<span class="cl">`))
	assert.Contains(t, got.HTML, `class="chroma"`)
	assert.Contains(t, got.HTML, `class="kn"`) // package 是 KeywordNamespace：确认真的高亮了
}

func TestMarkdown_CodeBlockFallsBackOnUnknownLanguage(t *testing.T) {
	t.Parallel()

	got := Markdown("```not-a-real-lexer\nhello\n```\n")
	assert.Contains(t, got.HTML, "hello")
	assert.Contains(t, got.HTML, `class="line"`)
}

// TestCodeBlockTitle 覆盖 info string 的解析口径（§11：文件名标签）。
func TestCodeBlockTitle(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		info string
		want string
	}{
		{name: "双引号", info: `go title="main.go"`, want: "main.go"},
		{name: "单引号", info: `go title='main.go'`, want: "main.go"},
		{name: "无引号", info: "go title=main.go", want: "main.go"},
		{name: "值里有空格", info: `go title="my file.go"`, want: "my file.go"},
		{name: "大小写不敏感", info: `go TITLE="main.go"`, want: "main.go"},
		{name: "方括号", info: "go [main.go]", want: "main.go"},
		{name: "方括号无语言", info: "[main.go]", want: "main.go"},
		{name: "没有语言只有属性", info: `title="main.go"`, want: "main.go"},
		{name: "只有语言", info: "go", want: ""},
		{name: "空 info", info: "", want: ""},
		{name: "裸词不认", info: "go main.go", want: ""},
		{name: "别的选项不认", info: "go linenos=true", want: ""},
		{name: "换行被压成空格", info: "go title=\"a\nb\"", want: "a b"},
		{name: "控制字符被丢掉", info: "go title=\"a\x00b\"", want: "ab"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, codeBlockTitle(tc.info))
		})
	}
}

func TestCodeBlockTitle_TruncatesLongName(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("文", 200)
	got := codeBlockTitle("go title=\"" + long + "\"")
	assert.Len(t, []rune(got), codeTitleMaxRunes)
}

func TestMarkdown_CodeBlockTitle(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		src   string
		title string // 期望的 figcaption 文本；空表示不包 figure
	}{
		{name: "title 属性", src: "```go title=\"main.go\"\npackage main\n```\n", title: "main.go"},
		{name: "方括号", src: "```go [main.go]\npackage main\n```\n", title: "main.go"},
		{name: "没有标签", src: "```go\npackage main\n```\n", title: ""},
		{name: "裸词不是标签", src: "```go main.go\npackage main\n```\n", title: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := Markdown(tc.src)
			if tc.title == "" {
				assert.NotContains(t, got.HTML, "<figure")
				assert.NotContains(t, got.HTML, "<figcaption")
				return
			}
			assert.Contains(t, got.HTML, `<figure class="code-block" data-lang="go">`)
			assert.Contains(t, got.HTML, "<figcaption>"+tc.title+"</figcaption>")
			assert.Contains(t, got.HTML, "</figure>")
			// 包了 figure 也不能挡住高亮与行号。
			assert.Contains(t, got.HTML, `<span class="line">`)
		})
	}
}

func TestMarkdown_CodeBlockTitleIsEscaped(t *testing.T) {
	t.Parallel()

	got := Markdown("```go title=\"<img src=x onerror=alert(1)>\"\ncode\n```\n")
	assert.NotContains(t, got.HTML, "<img src=x")
	assert.Contains(t, got.HTML, "&lt;img src=x onerror=alert(1)&gt;")
}

// TestMarkdown_RawHTMLIsOmitted 锁住第一道防线：goldmark 未开 WithUnsafe，裸 HTML 块根本不进正文。
func TestMarkdown_RawHTMLIsOmitted(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  string
	}{
		{name: "脚本标签", src: "<script>alert(1)</script>\n\n正文"},
		{name: "事件属性", src: `<p onclick="alert(1)">正文</p>`},
		{name: "iframe", src: `<iframe src="https://evil.example"></iframe>`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := Markdown(tc.src)
			assert.NotContains(t, got.HTML, "<script")
			assert.NotContains(t, got.HTML, "onclick")
			assert.NotContains(t, got.HTML, "<iframe")
			assert.NotContains(t, got.HTML, "alert(1)")
		})
	}

	// 裸 HTML 被丢弃，但它后面的正常段落照常渲染。
	assert.Contains(t, Markdown("<script>alert(1)</script>\n\n正文").HTML, "正文")
}

// TestSanitizer_StripsDangerousAttributes 直接打第二道防线（bluemonday），不经过 goldmark。
func TestSanitizer_StripsDangerousAttributes(t *testing.T) {
	t.Parallel()

	initRenderer()

	cases := []struct {
		name string
		raw  string
		want []string
		deny []string
	}{
		{
			name: "事件属性被剥掉、元素与文本保留",
			raw:  `<p onclick="alert(1)" style="color:red">正文</p>`,
			want: []string{"<p>正文</p>"},
			deny: []string{"onclick", "style="},
		},
		{
			name: "伪协议链接被剥掉",
			raw:  `<a href="javascript:alert(1)">点我</a>`,
			deny: []string{"javascript:"},
		},
		{
			name: "相对路径与 https 链接保留",
			raw:  `<a href="/posts/x">内链</a><a href="https://example.com">外链</a>`,
			want: []string{`href="/posts/x"`, `href="https://example.com"`},
		},
		{
			name: "class 与 data-tex 是白名单",
			raw:  `<span class="math" data-tex="a_i">$a_i$</span>`,
			want: []string{`class="math"`, `data-tex="a_i"`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := string(sanitizer.SanitizeBytes([]byte(tc.raw)))
			for _, want := range tc.want {
				assert.Contains(t, got, want)
			}
			for _, deny := range tc.deny {
				assert.NotContains(t, got, deny)
			}
		})
	}
}

func TestMarkdown_KeepsHeadingAnchorAndMathPlaceholder(t *testing.T) {
	t.Parallel()

	got := Markdown("## 二级标题\n\n公式 $a_i, b_i$ 在行内。\n")
	assert.Regexp(t, `<h2 id="[^"]+">二级标题</h2>`, got.HTML)
	assert.Contains(t, got.HTML, `<span class="math" data-tex="a_i, b_i">`)
	// 契约 §11：服务端不渲染 KaTeX，只留占位。
	assert.NotContains(t, got.HTML, "katex")
}

func TestMarkdown_ImageWidthAttribute(t *testing.T) {
	t.Parallel()

	got := Markdown("![图](/uploads/ab/x.png){width=480}\n")
	assert.Contains(t, got.HTML, `src="/uploads/ab/x.png"`)
	assert.Contains(t, got.HTML, `width="480"`)
	assert.Contains(t, got.HTML, `alt="图"`)
}

func TestMarkdown_GFMExtensions(t *testing.T) {
	t.Parallel()

	got := Markdown("| a | b |\n| - | - |\n| 1 | 2 |\n\n- [x] 已完成\n- [ ] 未完成\n\n~~删除~~\n")
	assert.Contains(t, got.HTML, "<table>")
	assert.Contains(t, got.HTML, `type="checkbox"`)
	assert.Contains(t, got.HTML, "<del>删除</del>")
}

func TestToText(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		markup string
		want   string
	}{
		{name: "去标签", markup: "<p>一段<b>粗体</b>文字</p>", want: "一段粗体文字"},
		{name: "块级转换行", markup: "<p>第一段</p><p>第二段</p>", want: "第一段\n第二段"},
		{name: "实体反转义", markup: "<p>a &amp; b &lt;c&gt;</p>", want: "a & b <c>"},
		{name: "折叠空白", markup: "<p>a    b</p>", want: "a b"},
		{name: "空输入", markup: "", want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, toText(tc.markup))
		})
	}
}

func TestCountWords(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		text string
		want int
	}{
		{name: "纯中文按字", text: "你好世界", want: 4},
		{name: "英文按词", text: "hello brave world", want: 3},
		{name: "中英混排", text: "Go 语言 hello", want: 4}, // Go(1) + 语言(2) + hello(1)
		{name: "标点不计", text: "a, b. c!", want: 3},
		{name: "数字算一个词", text: "version 2 released", want: 3},
		{name: "空串", text: "", want: 0},
		{name: "连字符分词", text: "well-known", want: 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, countWords(tc.text))
		})
	}
}

func TestReadingTime(t *testing.T) {
	t.Parallel()

	cases := []struct {
		words int
		want  int
	}{
		{words: 0, want: 0},
		{words: 1, want: 1},
		{words: 300, want: 1},
		{words: 301, want: 2},
		{words: 900, want: 3},
	}

	for _, tc := range cases {
		assert.Equal(t, tc.want, readingTime(tc.words), "words=%d", tc.words)
	}
}

func TestMarkdown_WordCountSkipsMarkup(t *testing.T) {
	t.Parallel()

	got := Markdown("# 标题\n\n正文三个词 here now\n")
	// 中文 7 字（标题 2 + 正文三个词 5）+ 英文 2 词（here、now）。
	assert.Equal(t, 9, got.WordCount)
	require.Equal(t, 1, got.ReadingTime)
	assert.NotContains(t, got.Text, "#")
}
