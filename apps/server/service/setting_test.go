package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
)

// putRawSetting 直接改写 settings.value_json，用来构造"存了非预期 JSON"的脏数据。
func putRawSetting(t *testing.T, key, raw string) {
	t.Helper()

	require.NoError(t, global.DB.Exec(
		`UPDATE settings SET value_json = ? WHERE key = ?`, raw, key,
	).Error)
}

func TestSettingService_StringRoundTrip(t *testing.T) {
	setupEnv(t)

	settings := SettingService{}

	// 键缺失时返回空串而不是错误（契约：settings 表是兜底，不是强约束）。
	got, err := settings.String("not.exists")
	require.NoError(t, err)
	assert.Empty(t, got)

	require.NoError(t, settings.PutString("test.key", "值"))
	got, err = settings.String("test.key")
	require.NoError(t, err)
	assert.Equal(t, "值", got)
}

func TestSettingService_StringRejectsNonStringJSON(t *testing.T) {
	setupEnv(t)

	putRawSetting(t, model.SettingContentVersion, "1")

	// 存的是 JSON 数字却按字符串读：必须报错，不能静默返回空串。
	_, err := SettingService{}.String(model.SettingContentVersion)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不是 JSON 字符串")
}

func TestSettingService_Int64(t *testing.T) {
	setupEnv(t)

	settings := SettingService{}

	// 键缺失走 fallback。
	got, err := settings.Int64("not.exists", 42)
	require.NoError(t, err)
	assert.Equal(t, int64(42), got)

	// JSON 数字与数字字符串都认（content_version 历史上两种都出现过）。
	putRawSetting(t, model.SettingContentVersion, "7")
	got, err = settings.Int64(model.SettingContentVersion, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(7), got)

	putRawSetting(t, model.SettingContentVersion, `"15"`)
	got, err = settings.Int64(model.SettingContentVersion, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(15), got)

	// 既不是数字也不是数字字符串 → 报错。
	putRawSetting(t, model.SettingContentVersion, `"abc"`)
	_, err = settings.Int64(model.SettingContentVersion, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不是 JSON 整数")
}

func TestSettingService_BumpContentVersion(t *testing.T) {
	setupEnv(t)

	settings := SettingService{}

	// 迁移 000002 已种下 content_version；连续递增两次应当是 +2。
	before, err := settings.Int64(model.SettingContentVersion, 0)
	require.NoError(t, err)

	require.NoError(t, settings.BumpContentVersion())
	require.NoError(t, settings.BumpContentVersion())

	after, err := settings.Int64(model.SettingContentVersion, 0)
	require.NoError(t, err)
	assert.Equal(t, before+2, after)
}

func TestSettingService_BumpContentVersionCreatesMissingKey(t *testing.T) {
	setupEnv(t)

	settings := SettingService{}
	require.NoError(t, global.DB.Exec(
		`DELETE FROM settings WHERE key = ?`, model.SettingContentVersion,
	).Error)

	require.NoError(t, settings.BumpContentVersion())

	got, err := settings.Int64(model.SettingContentVersion, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), got)
}

func TestSettingService_BumpContentVersionRejectsNonNumber(t *testing.T) {
	setupEnv(t)

	putRawSetting(t, model.SettingContentVersion, `"abc"`)

	err := SettingService{}.BumpContentVersion()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不是 JSON 数字")
}
