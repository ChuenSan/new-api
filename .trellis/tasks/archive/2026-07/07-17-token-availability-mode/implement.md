# Implement — 令牌级可用模式

## 前置

- 用户已审 `prd.md` + `design.md`
- `task.py start` 之后再改代码

## 清单

### A. 数据与鉴权

1. [x] `model/token.go`：`AvailabilityMode bool` + `Update()` 白名单
2. [x] `constant/context_key.go`：`ContextKeyTokenAvailabilityMode`
3. [x] `middleware/auth.go`：鉴权成功写入 context
4. [x] `controller/token.go`：Create/Update 赋值

### B. Relay 重放环

5. [x] `controller/relay.go`：读 availability mode
6. [x] 关模式完整保留现网循环；开模式实现“外层重试轮次 + 内层现有候选切换”
7. [x] 每轮重置选路 retry/used 状态；跨轮允许再次选择历史失败渠道
8. [x] 单轮无候选不结束外层请求；按退避策略开启下一轮
9. [x] 失败后若 `HasSendResponse()` 立即停止透明重放
10. [x] 保持 `processChannelError` + model-route slot release / notify

### C. 选路（不改排序）

11. [x] model-priority：used 集合限定为本轮，轮次结束后重置
12. [x] channel-priority：保持现有优先级/权重逻辑，仅在新轮次重置 retry
13. [x] 无候选进入下一轮，不作为外层最终失败
14. [x] 重试日志继续使用本轮 `use_channel`，不让累计日志污染选路排除状态

### D. 前端

15. [x] `web/default` keys types/schema
16. [x] form default + submit 映射
17. [x] mutate-drawer Switch（始终可见，不必依赖 auto 组）
18. [x] i18n 文案：「可用模式」/ 简短说明

### E. 验证

19. [x] 单测：无限轮次不受计数截断
20. [x] 单测：新轮次 retry/used/auto-group/model-route 状态重置
21. [x] 单测：无候选等待可被客户端取消
22. [x] 单测：token migration/update/context 字段
23. [x] `go test ./...`；`go build ./...`；前端 typecheck/build
24. [ ] 手工：开/关令牌各打一失败后恢复场景（若环境允许）

## 验证命令

```bash
go test ./controller/ -count=1 -run 'Token|Retry|Availability'
go test ./service/ -count=1 -run 'Channel|ModelPriority|Used'
go test ./model/ -count=1 -run 'Token'
go build ./...
```

## 风险点 / 回滚

| 点 | 动作 |
|----|------|
| relay CPU 忙循环 | 单轮 used-skip + 轮次间可取消退避 |
| 误伤关模式令牌 | 默认 false；关路径不改条件顺序 |
| 回滚 | 关令牌开关或回退 commit；DB 列可留 |

## 不做

- 改 model-route 排序
- 全局 Option 开关（本需求仅令牌）
- classic 前端（除非 default 共用 API 已足够）
- Task/MJ 路径

## start 前检查

- [x] prd 极简模型已定
- [x] 明确无限重放覆盖进入内部转发后、成功响应前的失败
- [x] 入口副作用只执行一次；内部轮次只重做选路与上游转发
- [x] 无候选时可取消短暂等待；客户端取消后停止重放
- [x] design 无新选路算法
- [x] implement.jsonl / check.jsonl 已填 spec
- [x] 用户批准 start
