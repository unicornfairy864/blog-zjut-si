package task

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
)

// periodicSpec 描述一个按**站点时区**跑的周期任务（agent.md §13）。
//
// weekday 为 -1 表示每天；其余取值表示只在当天入队。
type periodicSpec struct {
	jobType string
	hour    int
	minute  int
	weekday time.Weekday
}

// periodicSpecs 是全部周期任务；时刻取 §13 的规定。
var periodicSpecs = []periodicSpec{
	{jobType: model.JobTypeBackup, hour: 3, minute: 0, weekday: -1},
	{jobType: model.JobTypeCleanup, hour: 4, minute: 0, weekday: -1},
	// integrity_check 排在 cleanup 之后、link_check 之前：先让当天的清理落定再校验库。
	{jobType: model.JobTypeIntegrityCheck, hour: 4, minute: 30, weekday: time.Monday},
	{jobType: model.JobTypeLinkCheck, hour: 5, minute: 0, weekday: time.Monday},
}

// defaultSiteLocation 是站点时区缺省值（agent.md §4）。
const defaultSiteLocation = "Asia/Shanghai"

// enqueuePeriodic 把"今天该跑但还没跑"的周期任务投递进队列。
//
// 判据是 jobs 表里当天该时刻之后是否已有同类型记录，所以重启不会重复跑，
// 也不会因为调度器停过一段而漏跑（只要当天还没过那个时刻点）。
func (s *Scheduler) enqueuePeriodic(now time.Time) {
	location := siteLocation()
	local := now.In(location)

	for _, spec := range periodicSpecs {
		if spec.weekday >= 0 && local.Weekday() != spec.weekday {
			continue
		}
		due := time.Date(local.Year(), local.Month(), local.Day(), spec.hour, spec.minute, 0, 0, location)
		if local.Before(due) {
			continue
		}

		dueUnix := dbtime.FromTime(due)
		count, err := model.CountJobsOfTypeSince(global.DB, spec.jobType, dueUnix)
		if err != nil {
			global.Log.Error("检查周期任务失败", "type", spec.jobType, "err", err)
			continue
		}
		if count > 0 {
			continue
		}

		if err := model.CreateJob(global.DB, &model.Job{
			Type:        spec.jobType,
			Payload:     "{}",
			RunAt:       dbtime.FromTime(local),
			Status:      model.JobStatusPending,
			MaxAttempts: model.DefaultJobMaxAttempts,
			CreatedAt:   dbtime.FromTime(local),
		}); err != nil {
			global.Log.Error("投递周期任务失败", "type", spec.jobType, "err", err)
			continue
		}
		global.Log.Info("投递周期任务", "type", spec.jobType, "due", due.Format(time.RFC3339))
	}
}

// siteLocation 读取 settings.site_timezone；缺键或非法时退回上海。
//
// 调度器只依赖 model 与 global（不引 service，避免循环依赖），所以这里自己解一次 JSON。
func siteLocation() *time.Location {
	fallback := func() *time.Location {
		if loc, err := time.LoadLocation(defaultSiteLocation); err == nil {
			return loc
		}
		return time.FixedZone("CST", 8*60*60)
	}

	setting, err := model.GetSetting(global.DB, model.SettingSiteTimezone)
	if err != nil {
		return fallback()
	}
	var name string
	if err := json.Unmarshal([]byte(setting.ValueJSON), &name); err != nil {
		return fallback()
	}
	loc, err := time.LoadLocation(strings.TrimSpace(name))
	if err != nil {
		return fallback()
	}
	return loc
}
