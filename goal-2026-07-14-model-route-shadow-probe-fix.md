# 影子探测触发修复开发计划

生成时间：2026-07-14
分支：`feat/model-route-shadow-probe`
状态：待评审，未动手

## 1. 背景与问题

线上现象（cpa `snew-new-api`，`routing_priority_mode=model_priority`）：
GPT 系列（主力 `gpt-5.6-sol`）在使用时几乎不触发影子探测，管理页指标呈现「大量 PROBING + shadow_sample_count=0 + last_probe 为空」。

已定位的根因（按影响排序）：

- **根因 A（主因）**：GPT 主流量走 `/v1/responses`（`*dto.OpenAIResponsesRequest`）。
  `BuildProductionShadowCaptureFromRelay` 只识别 `GeneralOpenAIRequest / ClaudeRequest / GeminiChatRequest`，其余 `return nil`。
  → capture 为空 → `ScheduleShadowProbeAfterProduction` 开头 return
  → 即便探测队列里有 due 的 GPT PROBING 项，GPT 成功请求也弹不出来。
  （线上观察到的 GPT 影子，source 实为 grok/claude 的 `/v1/messages` 请求顺带 pop 出来的。）

- **根因 B（次因）**：`GlobalProbeQueue` 为进程内存 heap。
  入队只有两条路：生产失败进入 PROBING/OPEN/RATE_LIMITED、管理端 force_probe。
  启动/容器 recreate 后队列清空，DB 仍是 PROBING，无回灌逻辑 → PROBING 永久卡死不再探测。

- **根因 C（可选）**：`DefaultFirstStandbyShadowSampleRate=0.05` / `DefaultOtherStandbyShadowSampleRate=0.01`
  常量存在但从未接线，健康 standby 不会被主动采样。

指标校正：Primary（当前生产渠道）`shadow=0` 是**预期**（探测排除 primary），非缺陷。

## 2. 修复范围与优先级

| 编号 | 名称 | 优先级 | 复杂度 | 结论 |
|---|---|---|---|---|
| A | Responses 支持 capture | P0 必修 | 低 | 立即做 |
| B | PROBING 队列启动/周期回灌 | P1 建议 | 中 | 与 A 同批 |
| C | 健康 standby 采样接线 | P2 可选 | 中（决策为主）| 本期不做，待产品确认 |

本期落地：**A + B**。C 单列，需白子哥确认是否接受 standby 产生持续计费探测流量后再排期。

## 3. 详细设计

### 3.1 修复 A — Responses capture

**文件**：`controller/shadow_probe.go`

**改动点**：
1. `BuildProductionShadowCaptureFromRelay` 的 `switch` 增加分支：
   ```
   case *dto.OpenAIResponsesRequest:
       fillResponsesShadowView(&view, r)
       maxTokens = MaxOutputTokens（若有）
       relayFormat = types.RelayFormatOpenAIResponses
       requestPath = "/v1/responses"
   ```
2. 新增 `fillResponsesShadowView(view, r)`：
   - `r.Instructions` 非空 → 追加 `ShadowMessage{Role:"system"}`。
   - `r.ParseInput()`（已存在，返回 `[]MediaInput`）遍历：
     - `input_text` → 累积为 user text。
     - `input_image` / `input_file` → 置 `view.HasNonTextContent = true`。
   - 拼成至少一条 `ShadowMessage{Role:"user", Text:...}`。
3. 复用现有 `hasUserText` / `TextIndependentComplete` 校验，保证抽出非空 user text，否则维持 `return nil`。

**复用资产**（勿重造）：
- `dto.OpenAIResponsesRequest.ParseInput()`（`dto/openai_request.go:984`）已解析 string / 数组 / input_text / input_image / input_file。
- `types.RelayFormatOpenAIResponses` 常量已存在。

**边界与风险**：
- Responses role 结构与 chat 不同：只取 input 中的文本 turn，忽略 tool/函数调用项。
- 探测执行时的 body 由下游 executor 按 capture 的 relayFormat 生成；确认 Responses 格式能被 `buildShadowDTORequest` 正确处理，若不支持则回退（见 3.3 待确认项）。

**代码量估算**：约 30–40 行，单文件。

### 3.2 修复 B — PROBING 队列回灌

**文件**：
- `modelroute/reconcile.go`（已存在，追加函数）
- `service/system_task.go`（挂接周期调用）+ 启动流程接线

**改动点**：
1. 新增 `ReconcileProbeQueueFromDB()`：
   - 仅当 `IsModelPriorityMode()` 为真时执行。
   - `model.ListAllChannelModelMetrics()`（已存在）遍历。
   - `route_state ∈ {PROBING, OPEN, RATE_LIMITED}` 的行 → `EnqueueFromMetrics(m, 0)`。
   - `EnqueueFromMetrics` 内部 `Upsert` 幂等，重复调用安全。
2. 启动时调用一次（放在 modelroute 初始化 / migration 完成后的接线点）。
3. `service/system_task.go` 周期 ticker 内追加调用（复用现有 `systemTaskSchedulerInterval` 节流骨架，避免高频扫库）。

**边界与风险**：
- 多节点部署：队列进程本地，各节点各自回灌 → 各自跑影子。此为 PRD 既定的进程本地设计，不引入新语义。
- 回灌用 `manualPriority=0`：与现有失败入队路径一致；若需按 policy 优先级排序可后续增强，本期不做。
- `NextProbeAt` 由 `CooldownUntilTime()` 决定，冷却未到不会提前 pop，天然限流。

**代码量估算**：约 50–80 行，2–3 文件。

### 3.3 待白子哥确认的决策项

1. **周期回灌间隔**：建议复用 system_task 现有节流周期，不新增独立 ticker。确认可接受。
2. **Responses 影子 body 生成**：需确认 `buildShadowDTORequest` 对 `RelayFormatOpenAIResponses` 的支持；若不支持，方案二是「探测统一用目标渠道 native 格式」而非触发请求格式。此项影响 A 的 executor 侧，需在编码前验证。
3. **修复 C 是否本期做**：涉及 standby 主动计费探测，默认不做。

## 4. 测试计划

三块目标均标注「无覆盖测试」，需补 `testify` 表驱动测试（项目规范：`require` 用于致命断言，`assert` 用于值检查）。

- **A**：`controller/shadow_probe_test.go` 新增 Responses capture 用例：
  - input 为 string、input_text 数组、含 input_image 的混合、空 input（应 nil）、仅 instructions 无 user（应 nil）。
  - 断言 `view.Messages`、`HasNonTextContent`、`RelayFormat`、`MaxTokens` 精确值。
- **B**：`modelroute/reconcile_test.go` 新增回灌用例：
  - 构造 PROBING/OPEN/RATE_LIMITED/HEALTHY 混合 metrics → 调用 `ReconcileProbeQueueFromDB` → 断言仅前三态入队，队列 Len 与 due 顺序正确。
  - 需在 fixture 内显式初始化 DB / runtime 状态。

## 5. 线上验证（不改代码即可先做）

1. 管理端对某 PROBING 的 `gpt-5.6-sol × channel` 点 `force_probe`，再发一条能建 capture 的成功请求（`/v1/messages` 或 chat），观察是否出现影子日志。
2. 部署 A 后：用 `/v1/responses` 打 GPT，观察 `last_probe` 是否由空变有值、`shadow_sample_count` 是否 +1。
3. 部署 B 后：重启容器，确认 PROBING 行在无新失败的情况下也能被探测。

## 6. 交付与合规

- 分支：沿用 `feat/model-route-shadow-probe`。
- 提交拆分：A、B 独立 commit，便于回滚。
- PR：git user `baizige` 非核心历史作者，PR body 需注明 AI 辅助生成；使用 `.github/PULL_REQUEST_TEMPLATE.md` 模板。
- 保护项：不触碰 new-api / QuantumNous 标识。
- 数据库改动：无 schema 变更，仅读 `channel_model_metrics`；兼容 SQLite/MySQL/PG（走现有 GORM 方法）。

## 7. 未决 / 后续

- 修复 C（standby 采样）待产品决策。
- 探测 body 是否统一改为目标渠道 native 格式（避免 Claude body 探 OpenAI 渠道的语义错位），作为独立增强项评估。
