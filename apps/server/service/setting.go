package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
)

// SettingService 读写 settings 表。
type SettingService struct{}

// String 读取字符串型设置（value_json 必须是 JSON 字符串）。
func (SettingService) String(key string) (string, error) {
	setting, err := model.GetSetting(global.DB, key)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("读取设置 %s 失败: %w", key, err)
	}
	var value string
	if err := json.Unmarshal([]byte(setting.ValueJSON), &value); err != nil {
		return "", fmt.Errorf("设置 %s 不是 JSON 字符串: %w", key, err)
	}
	return value, nil
}

// PutString 写入字符串型设置。
func (SettingService) PutString(key, value string) error {
	buf, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("序列化设置 %s 失败: %w", key, err)
	}
	if err := model.PutSetting(global.DB, key, string(buf), dbtime.Now()); err != nil {
		return fmt.Errorf("写入设置 %s 失败: %w", key, err)
	}
	return nil
}

// Int64 读取整数型设置；键缺失时返回 fallback。
//
// value_json 允许 JSON 数字（如 content_version 的 1）或数字字符串，两种都兼容。
func (SettingService) Int64(key string, fallback int64) (int64, error) {
	setting, err := model.GetSetting(global.DB, key)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return fallback, nil
		}
		return 0, fmt.Errorf("读取设置 %s 失败: %w", key, err)
	}

	var number int64
	if err := json.Unmarshal([]byte(setting.ValueJSON), &number); err == nil {
		return number, nil
	}
	var text string
	if err := json.Unmarshal([]byte(setting.ValueJSON), &text); err == nil {
		if parsed, parseErr := strconv.ParseInt(strings.TrimSpace(text), 10, 64); parseErr == nil {
			return parsed, nil
		}
	}
	return 0, fmt.Errorf("设置 %s 不是 JSON 整数", key)
}

// BumpContentVersion 递增 content_version（公开读接口的 ETag 依赖它）。
func (SettingService) BumpContentVersion() error {
	setting, err := model.GetSetting(global.DB, model.SettingContentVersion)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return model.PutSetting(global.DB, model.SettingContentVersion, "1", dbtime.Now())
		}
		return fmt.Errorf("读取 content_version 失败: %w", err)
	}
	var version int64
	if err := json.Unmarshal([]byte(setting.ValueJSON), &version); err != nil {
		return fmt.Errorf("content_version 不是 JSON 数字: %w", err)
	}
	version++
	buf, err := json.Marshal(version)
	if err != nil {
		return fmt.Errorf("序列化 content_version 失败: %w", err)
	}
	return model.PutSetting(global.DB, model.SettingContentVersion, string(buf), dbtime.Now())
}
