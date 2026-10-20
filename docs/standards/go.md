# Go 编码细则

> 上位契约：[`../agent.md`](../agent.md) §3.2 / §7 / §9。本文只补充"怎么写"。

## 1. 分层与依赖

```
api/v1  ──▶ service ──▶ model
   │            │
   └────────────┴──▶ model/common/{errs,response,request,dbtime}
initialize ──▶ 全部（装配用）
core/*、utils ──▶ 不依赖业务包
```

| 层 | 只做 | 禁止 |
| --- | --- | --- |
| `api/v1/*` | 绑定 → `binding` 校验 → 调 service → `response.*` | SQL、事务、状态机、拼业务规则 |
| `service/*` | 全部业务规则；用 `global.DB.Transaction` 发起事务 | 接收 `*gin.Context`；返回 gorm 类型 |
| `model/*` | 数据访问（gorm/SQL 只在这里） | 权限判断、状态机判断、HTTP 概念 |
| `router/*` | 路由与中间件链 | 业务逻辑 |
| `utils/*`、`core/*` | 无状态基础设施 | 反向依赖业务包 |

`model` 对外只暴露 `model.ErrNotFound` 等哨兵错误，不让 `gorm.ErrRecordNotFound` 泄漏到上层。

## 2. 命名与注释

- 标识符英文，注释中文；导出符号必须有注释，且以符号名开头。
- 包注释写在 `enter.go` / 主文件顶部，说明职责与契约出处。
- 文件名：小写下划线（`login_attempt.go`），一个文件只放一类实体与其查询。
- 枚举常量集中在实体文件顶部（`JobStatusPending`、`AttemptKindLogin`），禁止魔法字符串。

## 3. 错误处理

```go
// 业务错误：只构造 errs.AppError，不返回裸 error 字符串
return nil, errs.New(errs.ErrorAuth, "用户名或密码错误")
return nil, errs.WithDetails(errs.ErrorRateLimited, "请求过于频繁", map[string]any{"retryAfter": 60})

// 基础设施错误：%w 包装，交给 response.FailWithError 归一成 1006
return nil, fmt.Errorf("查询会话失败: %w", err)
```

- `api` 层只调用 `response.FailWithError(c, err)`；业务失败一律 HTTP 200，状态语义由信封 `code` 表达
  （v1.14 起 `errs.HTTPStatus(code)` 不再决定状态码，只服务于健康检查等非业务场景）。
- 绝不把 `err.Error()` 直接返回前端，也不在前端可见的 `msg` 里出现库名、文件名、堆栈。
- 校验失败：`response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")`。

## 4. 事务模板

```go
if err := global.DB.Transaction(func(tx *gorm.DB) error {
    if err := model.CreateSession(tx, &session); err != nil {
        return err
    }
    return model.CreateAuditLog(tx, &entry)
}); err != nil {
    return fmt.Errorf("创建会话失败: %w", err)
}
```

- 事务由 **service** 发起；`tx` 以参数透传到 model 函数（`model.Xxx(tx, …)`）。
- 不在事务里做 HTTP、加锁、外部 IO。
- 写事务默认 `_txlock=immediate`（DSN 里已设），避免升级锁失败（SQLITE_BUSY）。

## 5. 时间与序列化

- 内存里用 `time.Time`；落库与出参统一 `model/common/dbtime.Unix`（秒，UTC）。
- 可空时间用 `*dbtime.Unix`（`posts.published_at`、`preview_tokens.used_at`）。
- 日粒度（`stats_daily.date`）是站点时区 `Asia/Shanghai` 的 `YYYY-MM-DD`，不要用 UTC 日期。

## 6. 日志

```go
global.Log.Info("已创建初始管理员", "username", username)
global.Log.Error("写审计日志失败", "action", in.Action, "err", err)
```

- 只用 `slog` 键值对；字段名见 agent.md §7。
- 禁止打印密码、口令哈希、邮箱、cookie、完整 IP（用 `utils.HashIP`）。
- 每个请求有 `request_id`；跨函数传递时显式带参数，不用全局变量。

## 7. 并发

- 共享可变状态加锁或用 `sync.Once` / `atomic`；参考 `utils.KeyedLimiter`、`utils.SetPepper`。
- 不写裸 `go func()`：用 `errgroup.Group` 管生命周期（见 `initialize/server.go`）。
- 后台任务单实例；调度器每 60s 扫描一次。

## 8. 测试模板（D18 补齐，当前不写）

```go
func TestLogin_Locked(t *testing.T) {
    t.Parallel()
    cases := []struct{ name string; ... }{...}
    for _, tc := range cases {
        tc := tc
        t.Run(tc.name, func(t *testing.T) {
            t.Parallel()
            // arrange: 临时 SQLite（t.TempDir）+ 迁移
            // act / assert
        })
    }
}
```

- 表驱动 + `t.Parallel()`；service 覆盖率 ≥70%；每个 API 至少 happy path + 错误路径。
- 状态机与任务调度必须有单测（§16）。

## 9. 质量门

```bash
gofmt -l .            # 必须为空
go vet ./...
golangci-lint run     # govet,errcheck,staticcheck,revive,gosec,bodyclose,unparam
go build -tags nodynamic ./...   # WebP 走 wazero，不依赖宿主 libwebp
```

提交前跑 `make check`（含前端 lint 与 typecheck）。
