# 进程内熔断器（circuitbreaker）

## 设计定位

**临时、轻量、进程内存。** 针对 `auto-ban` 机制"触发条件窄、禁用持久化写 DB、无时间窗口"的不足，提供一个只活在当前进程内存里的熔断器：连续非 2xx 失败达到阈值即熔断，冷却后放一个试探请求探活，成功即恢复。全程不写 DB、不写 Redis、不做多实例同步。

**互斥关系：** `auto_ban=1` → 走原 auto-ban 路径，永不触碰本包；`auto_ban=0` → 走熔断路径。两者不会同时对同一渠道生效。一个渠道要么是 auto-ban 渠道，要么是熔断渠道，由其 `auto_ban` 配置决定。

**核心 key：** `"<channel_id>:<model_name>"`，例如 `"42:gpt-4o"`。熔断粒度是"渠道 × 模型"——同一渠道的某个模型熔断不影响该渠道的其他模型。

## 状态机

```
        连续失败 >= 阈值            冷却到期(惰性)
CLOSED ──────────────────► OPEN ──────────────────► HALF_OPEN
  ▲                           ▲                         │
  │                           │                         │ 试探成功
  └───────────────────────────┘ ◄──── 试探失败 ──────────┘
   成功(2xx)                  重新计时冷却
```

- **CLOSED**：正常放行。每次非 2xx 失败计数 +1；达到阈值 → OPEN，记录 `openedAt` 与冷却窗口。任意一次 2xx 成功 → 计数归零（删除条目）。
- **OPEN**：阻塞。渠道选择阶段直接过滤掉，不进入候选。冷却到期后由 `IsOpen` 在下次查询时**惰性**转为 HALF_OPEN——不依赖定时器。
- **HALF_OPEN**：只允许**一个**试探请求通过。试探成功 → CLOSED；试探失败 → 重新 OPEN 并重置冷却窗口。

## 关键机制

### 惰性状态转换

不维护后台 goroutine 或 ticker。OPEN → HALF_OPEN 的转换发生在 `IsOpen` 查询时：若 `time.Since(openedAt) >= cooldown`，就地转为 HALF_OPEN 并返回 `false`（放行）。这意味着冷却到点后，直到下一次渠道选择查询才真正进入试探态。

### 试探请求互斥（单 probe 槽）

HALF_OPEN 必须保证同一时刻只有一个试探请求打到上游，否则试探结果不可信。通过 `probeInFlight` 标志实现：

- `AllowProbe`：若槽空闲（`!probeInFlight`）则占用并返回 `true`；否则返回 `false`，该渠道本轮不可选。
- `RecordSuccess` / `RecordFailure` 在试探结束时释放槽位。
- **泄漏兜底**：若试探请求因超时/panic 未回调记录函数，槽位会一直被占。`AllowProbe` 中检查占用时长，超过 `ProbeTimeout`（30s）即视为泄漏、回收槽位，避免熔断器被卡死在 HALF_OPEN。

### 计数语义

只有完全的 2xx（`statusCode ∈ [200,300)`）才算成功并重置计数。其余情况——包括无状态码的请求失败——一律记为失败。这样确保真正"健康"才会清零，健康度判定偏保守。

## 配置

### 全局默认（`setting/operation_setting/circuit_breaker_setting.go`）

```go
type CircuitBreakerSetting struct {
    FailureThreshold int  // 默认 3：连续非 2xx 失败多少次熔断
    CooldownSeconds int  // 默认 60：OPEN 持续多少秒后转 HALF_OPEN
}
```

注册到 `config.GlobalConfig`，键名 `circuit_breaker_setting`，可通过系统运行时配置加载/热更新。访问函数 `GetCircuitBreakerSetting()`。

### 渠道级覆盖（`dto/channel_settings.go`）

```go
type ChannelOtherSettings struct {
    // ...
    CircuitBreakerFailureThreshold *int `json:"circuit_breaker_failure_threshold,omitempty"`
    CircuitBreakerCooldownSeconds  *int `json:"circuit_breaker_cooldown_seconds,omitempty"`
}
```

- 使用指针类型以区分"未设置（nil）→ 用全局默认"与"显式设为某值"。
- **非正值（<=0）的覆盖被拒绝**，回退到全局默认，防止误配成 0 把渠道永久熔断或永不冷却。
- 优先级：渠道覆盖 > 全局默认。仅在 `auto_ban=0` 时被消费。

## 公开 API

| 函数 | 用途 | 调用点 |
|------|------|--------|
| `IsOpen(channelId, modelName)` | 查询是否 OPEN（含惰性转 HALF_OPEN） | 渠道选择过滤 |
| `IsHalfOpen(channelId, modelName)` | 纯查询是否 HALF_OPEN | 渠道选择 |
| `AllowProbe(channelId, modelName)` | 抢占单 probe 槽，返回是否可试探 | 渠道选择（HALF_OPEN 时） |
| `RecordSuccess(channelId, modelName)` | 记录 2xx 成功，HALF_OPEN→CLOSED | relay 成功分支 |
| `RecordFailure(channelId, modelName, statusCode, settings)` | 记录失败，驱动状态机 | relay 失败分支 |
| `GetFailureThreshold(settings)` / `GetCooldownSeconds(settings)` | 解析阈值/冷却（覆盖 > 默认） | 状态机内部 |

## 集成点

熔断只在三个位置接入，对现有 relay/retry 主流程侵入极小。

1. **`controller/relay.go` — 成功记录**：retry loop 内 `newAPIError == nil` 时，若 `!channel.GetAutoBan()`，调用 `RecordSuccess`。

2. **`controller/relay.go` — 失败记录**：`processChannelError(...)` 之后，若 `!channel.GetAutoBan()`，调用 `RecordFailure`，传入 `channel.GetOtherSettings()` 以读取渠道级覆盖。

3. **`model/channel_cache.go` — 渠道选择过滤**：
   - `filterOpenCircuitBreakerChannels`：在精确模型名与规范化模型名两次候选筛选后，剔除 OPEN 的 `auto_ban=0` 渠道。HALF_OPEN 渠道**保留**在候选里，留给后续抢占判定。
   - 单候选直取分支与加权选择循环中：对 HALF_OPEN 渠道调用 `AllowProbe` 抢 probe 槽，抢不到则跳过（不计入有效权重）。

当某 `channel:model` 的所有候选渠道都 OPEN 时，`GetRandomSatisfiedChannel` 自然返回 nil，relay 主流程按"无可用渠道"返回错误给客户端——无需特殊分支。

## 与现有机制的关系

- **auto-ban（`auto_ban=1`）**：完全不受影响。熔断代码路径全部由 `!channel.GetAutoBan()` 守卫，auto-ban 渠道既不记录、不被过滤，仍走原持久化禁用逻辑。
- **手动禁用（`Status=2`）**：不受影响。熔断只针对 `auto_ban=0` 渠道的瞬时健康度，与渠道是否被管理员手动停用正交。
- **计费/预扣费/日志**：不受影响。熔断只在渠道选择与 relay 成败记录处动作，不触碰任何计费、配额、日志写入路径。

## 不涉及（第一版边界）

- 前端可视化（无 UI 展示熔断状态）
- 修改现有 auto-ban 逻辑
- Redis/DB 持久化熔断状态
- 滑动窗口计数（当前为连续失败计数）
- 多实例同步（各进程熔断状态独立，进程重启即清空）
- 分布式 probe 协调

## 测试

`circuit_breaker_test.go` 覆盖状态机的全部转移与边界，时间通过 `now` 间接层注入，确定性推进、无 sleep：

- CLOSED → OPEN（连续失败达阈值）
- 成功重置 CLOSED 计数
- 2xx 被当作成功
- OPEN → HALF_OPEN（冷却到期，惰性）
- HALF_OPEN → CLOSED（试探成功）
- HALF_OPEN 仅允许一个试探请求
- HALF_OPEN → OPEN（试探失败，冷却窗口重置）
- 渠道级覆盖优先于全局默认
- 非正/nil 覆盖回退全局默认
- 泄漏 probe 超时回收
- key 按 channel:model 隔离

运行：

```bash
go test ./service/circuitbreaker/...
```

---

## 📜 License & Attribution

本项目为 [QuantumNous/new-api](https://github.com/QuantumNous/new-api) 的衍生，基于 [One API](https://github.com/songquanpeng/one-api)（MIT License）开发，遵循 [AGPLv3](./LICENSE) 协议。修改版本须保留作者署名与指向原项目的可见链接。
