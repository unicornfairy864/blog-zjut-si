package service

import (
	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
)

// TrackAttachmentClick 给附件下载计数 +1（公开端 /go 用）。
//
// 计数失败只记日志：访客要的是文件，统计丢了不该让跳转失败。
func (s ContentService) TrackAttachmentClick(id int64) {
	if err := model.IncrementAttachmentClicks(global.DB, id); err != nil {
		global.Log.Warn("累加附件下载数失败", "attachment_id", id, "err", err)
	}
}
