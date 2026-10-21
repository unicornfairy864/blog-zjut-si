package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleProfile = `mode: atomic
github.com/x/y/apps/server/service/post.go:10.1,12.5 3 1
github.com/x/y/apps/server/service/post.go:20.1,22.5 2 0
github.com/x/y/apps/server/service/tag.go:5.1,6.5 5 4
github.com/x/y/apps/server/core/render/render.go:1.1,2.5 4 0
`

func TestParseProfile(t *testing.T) {
	packages, err := parseProfile(strings.NewReader(sampleProfile))
	require.NoError(t, err)
	require.Len(t, packages, 2)

	service := packages["github.com/x/y/apps/server/service"]
	require.NotNil(t, service)
	// 3 条命中 + 2 条未命中 + 5 条命中 = 10 条语句，覆盖 8 条。
	assert.Equal(t, 10, service.statements)
	assert.Equal(t, 8, service.covered)
	assert.InDelta(t, 80, service.percent(), 0.01)

	render := packages["github.com/x/y/apps/server/core/render"]
	require.NotNil(t, render)
	assert.Equal(t, 0.0, render.percent())
}

func TestParseProfile_RejectsMalformedInput(t *testing.T) {
	cases := []struct {
		name    string
		profile string
	}{
		{name: "列数不对", profile: "mode: atomic\na.go:1.1,2.2 3\n"},
		{name: "没有冒号", profile: "mode: atomic\na.go 3 1\n"},
		{name: "语句数不是数字", profile: "mode: atomic\na.go:1.1,2.2 x 1\n"},
		{name: "命中次数不是数字", profile: "mode: atomic\na.go:1.1,2.2 3 x\n"},
		{name: "空剖面", profile: "mode: atomic\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseProfile(strings.NewReader(tc.profile))
			require.Error(t, err)
		})
	}
}

func TestParseProfile_IgnoresBlankLines(t *testing.T) {
	packages, err := parseProfile(strings.NewReader(
		"\nmode: atomic\n\n" +
			"github.com/x/y/apps/server/service/post.go:10.1,12.5 3 1\n" +
			"\n" +
			"github.com/x/y/apps/server/core/render/render.go:1.1,2.5 4 0\n",
	))
	require.NoError(t, err)
	assert.Len(t, packages, 2)
}

func TestLookup_PicksLowestWhenPackageNameRepeats(t *testing.T) {
	packages, err := parseProfile(strings.NewReader(
		"mode: atomic\n" +
			"github.com/x/y/a/service/p.go:1.1,2.2 4 4\n" +
			"github.com/x/y/b/service/q.go:1.1,2.2 4 0\n",
	))
	require.NoError(t, err)

	got, ok := lookup(packages, "service")
	require.True(t, ok)
	assert.Equal(t, "github.com/x/y/b/service", got.dir)
	assert.Equal(t, 0.0, got.percent())
}

func TestLookup_Missing(t *testing.T) {
	packages, err := parseProfile(strings.NewReader(sampleProfile))
	require.NoError(t, err)

	_, ok := lookup(packages, "nope")
	assert.False(t, ok)
}

func TestPackageCoverage_EmptyPackageIsFull(t *testing.T) {
	assert.Equal(t, 100.0, packageCoverage{}.percent())
}
