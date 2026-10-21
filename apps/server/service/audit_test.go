package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/utils"
)

// auditLogByAction 读某个 action 最近一条审计记录（按 id 倒序），用于断言落库字段。
func auditLogByAction(t *testing.T, action string) model.AuditLog {
	t.Helper()

	var entry model.AuditLog
	require.NoError(t, global.DB.Where("action = ?", action).Order("id DESC").Take(&entry).Error)
	return entry
}

func TestAuditService_WriteTxStoresMetaAndHashedIP(t *testing.T) {
	setupEnv(t)

	const plainIP = "203.0.113.9"

	err := ServiceGroupApp.Audit.WriteTx(global.DB, AuditInput{
		AdminID:    42,
		Action:     "test.action",
		TargetType: "test",
		TargetID:   "7",
		Meta:       map[string]any{"changed": []string{"title", "slug"}, "count": 2},
		IP:         plainIP,
	})
	require.NoError(t, err)

	entry := auditLogByAction(t, "test.action")
	assert.Equal(t, int64(42), entry.AdminID)
	assert.Equal(t, "test", entry.TargetType)
	assert.Equal(t, "7", entry.TargetID)
	assert.Greater(t, int64(entry.CreatedAt), int64(0))

	// meta 必须是合法 JSON，且内容原样保留。
	assert.True(t, json.Valid([]byte(entry.MetaJSON)), "meta 必须是合法 JSON：%s", entry.MetaJSON)
	var meta map[string]any
	require.NoError(t, json.Unmarshal([]byte(entry.MetaJSON), &meta))
	assert.Equal(t, float64(2), meta["count"])
	assert.Equal(t, []any{"title", "slug"}, meta["changed"])

	// ip 列存的是加盐哈希而不是明文（§9）。
	assert.Equal(t, utils.HashIP(plainIP), entry.IPHash)
	assert.Len(t, entry.IPHash, 32)

	// 整行都不能出现明文 IP。
	var raw string
	require.NoError(t, global.DB.Raw(
		`SELECT meta_json || '|' || ip_hash || '|' || target_id || '|' || target_type FROM audit_logs WHERE id = ?`,
		entry.ID,
	).Scan(&raw).Error)
	assert.NotContains(t, raw, plainIP, "审计表里不能出现明文 IP")
}

func TestAuditService_WriteTxEmptyMetaIsJSONObject(t *testing.T) {
	setupEnv(t)

	// meta 为空时落 "{}"，保证列里永远是合法 JSON（前端不用做 null 判断）。
	require.NoError(t, ServiceGroupApp.Audit.WriteTx(global.DB, AuditInput{Action: "test.empty"}))

	entry := auditLogByAction(t, "test.empty")
	assert.Equal(t, "{}", entry.MetaJSON)
	assert.True(t, json.Valid([]byte(entry.MetaJSON)))
}

func TestAuditService_WriteHappyPath(t *testing.T) {
	setupEnv(t)

	// Write 自带事务：正常路径下应当落库。
	ServiceGroupApp.Audit.Write(AuditInput{
		AdminID: 1,
		Action:  "test.write",
		Meta:    map[string]any{"ok": true},
		IP:      "203.0.113.10",
	})

	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", "test.write"))
	assert.Equal(t, utils.HashIP("203.0.113.10"), auditLogByAction(t, "test.write").IPHash)
}

func TestAuditService_WriteTxRejectsUnmarshalableMeta(t *testing.T) {
	setupEnv(t)

	// 函数无法序列化成 JSON：必须返回错误，不能 panic，也不能写半条记录。
	err := ServiceGroupApp.Audit.WriteTx(global.DB, AuditInput{
		Action: "test.bad-meta",
		Meta:   map[string]any{"fn": func() {}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "序列化审计元数据失败")
	assert.Zero(t, countRows(t, "audit_logs", ""))

	// Write 走同一条序列化路径，只是把错误降级成日志。
	assert.NotPanics(t, func() {
		ServiceGroupApp.Audit.Write(AuditInput{
			Action: "test.bad-meta",
			Meta:   map[string]any{"fn": func() {}},
		})
	})
	assert.Zero(t, countRows(t, "audit_logs", ""))
}

func TestAuditService_WriteSwallowsTransactionFailure(t *testing.T) {
	setupEnv(t)

	// 造一个"必然写不进去"的状态：直接删掉审计表。
	require.NoError(t, global.DB.Exec("DROP TABLE audit_logs").Error)

	// 契约：Write 尽力而为，事务失败只记日志，绝不 panic（它被登录失败、登出等主流程调用）。
	assert.NotPanics(t, func() {
		ServiceGroupApp.Audit.Write(AuditInput{Action: "test.closed", IP: "203.0.113.11"})
	})

	// 需要与业务同生共死时用 WriteTx，它会如实把错误交回调用方。
	err := ServiceGroupApp.Audit.WriteTx(global.DB, AuditInput{Action: "test.closed"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "写入审计日志失败")
}
