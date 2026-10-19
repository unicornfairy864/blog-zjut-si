package service

import (
	"errors"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
)

// RedirectFor 是公开边缘用的跳转查询：命中返回目标与状态码，未命中返回 nil（§5 跳转）。
//
// 命中就顺手 hits+1；累加失败只记日志，不影响跳转本身。
func (s ContentService) RedirectFor(path string) (*model.Redirect, error) {
	redirect, err := model.FindRedirectByPath(global.DB, path)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	if err := model.IncrementRedirectHits(global.DB, redirect.ID); err != nil {
		global.Log.Warn("累加跳转命中失败", "redirect_id", redirect.ID, "err", err)
	}
	return redirect, nil
}
