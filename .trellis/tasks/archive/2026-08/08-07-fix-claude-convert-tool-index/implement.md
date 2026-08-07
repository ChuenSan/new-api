# 修复 OpenAI→Claude 流式转换 tool_calls index 错位 — Implementation Plan

上下文阅读顺序:`prd.md` → `design.md` → 本文件。本文件只列执行步骤、验证命令、回滚点。

## 0. 前置确认

- [ ] 确认当前分支为 `fix/claude-convert-tool-index`(`git branch --show-current`)。
- [ ] 确认 `git status` 中 `service/convert.go`、`relay/common/relay_info.go`、`relay/channel/openai/helper.go` 无未提交改动。
- [ ] 确认 `service/convert.go:255` `StreamResponseOpenAI2Claude` 的函数签名与 prd 引用一致(若上游 main 已重构此函数,回到 planning 更新事实)。
- [ ] 确认 `relay/common/relay_info.go:44-45` 仍存在 `ToolCallBaseIndex` / `ToolCallMaxIndexOffset`(设计重构的对象)。
- [ ] 通读 CC Switch 剖析文档第 6.2 节(`docs/协议转换实现剖析.md`)的 `tool_blocks_by_index` 段,作为映射模型的参照。

## 1. 重构状态结构体

文件:`relay/common/relay_info.go`

- [ ] 在 `ClaudeConvertInfo`(`:37`)中删除 `ToolCallBaseIndex` 与 `ToolCallMaxIndexOffset`。
- [ ] 新增 `ToolBlockIndexByOpenAIIndex map[int]int` 与 `ToolBlockStarted map[int]bool`(命名可对齐房内 Go 风格,但语义必须是「OpenAI index → Claude index 映射」+「已 start 集合」)。
- [ ] 在 `ClaudeConvertInfo` 的初始化处(grep `ClaudeConvertInfo{` 或 `ClaudeConvertInfo =` 找初始化点;若无显式初始化,在使用前 `make`)确保两个 map 非 nil。
- [ ] 验证:`go build ./relay/...` 通过;若有他处因字段删除编译失败,逐一核对——按 prd「代码边界」表,除 `service/convert.go` 外不应有第二处引用这两个字段,若出现说明事实有变,停下来回 planning。

## 2. 重写工具块 index 分配(核心)

文件:`service/convert.go` `StreamResponseOpenAI2Claude`

- [ ] 主工具循环(`:480-529`)按 design 决策 1 的三条分配规则改写:
  - 新 OpenAI index → 分配 `claudeIdx = Index`,`Index++`,记录两个 map,发 `content_block_start(claudeIdx)`;
  - 已有 OpenAI index → 取 `claudeIdx`,只发 `content_block_delta(claudeIdx, input_json_delta)`。
- [ ] `stopOpenBlocks` 的 `LastMessageTypeTools` 分支(`:272-276`)改为:收集 `ToolBlockStarted` 的 key、排序、逐个 `content_block_stop`,然后清空两个 map。text/thinking 分支(`:270-271`)不动。
- [ ] `stopOpenBlocksAndAdvance` 的 tools 分支(`:289-296`)按 design 决策 3 简化:关块 + 清 map + `LastMessagesType=none`,**删除** `Index` 的手动重算。
- [ ] 首 chunk 工具分支(`:318-355`)按 design 决策 2 改为走同一分配逻辑,`SetIndex(0)` 替换为分配返回值。
- [ ] 自查 INV-1..INV-4(见 design):尤其确认「text/thinking 与 tool 共享 `Index` 计数器」没有被某处 `Index = 0` 或局部重算破坏。
- [ ] 验证:`go build ./...`、`go vet ./service/`。

## 3. 测试

文件:`service/convert_test.go`(不存在则新建,存在则追加)

- [ ] **AC1 fixture 测试**:合成流 = `role chunk` → `1×reasoning delta` → `tool index=1 start(id+name)` → `2×tool index=1 args 增量` → `finish_reason=tool_calls` → `usage-only` → 结束。逐 chunk 调 `StreamResponseOpenAI2Claude`,收集所有产出事件,断言:
  - 存在 `content_block_start(index=0, thinking)` 与 `content_block_start(index=1, tool_use)`;
  - 不存在任何 `index=2` 的事件;
  - 每个 start 的 index 都有且仅有一次对应 stop;
  - stop 的 index 都曾在 start 中出现(R4)。
- [ ] **AC2 并行多 tool**:text delta 在前 + 3 个 tool(index 0,1,2)→ 断言 index 序列 0(text),1,2,3(tool)且配对。
- [ ] **AC3 乱序**:tool index 2 的 start 先于 index 0 → 断言同一 OpenAI index 的 args 落同一 Claude index,且 Claude index 按分配顺序连续。
- [ ] **AC4 首 chunk 即 tool**:`SendResponseCount==1` 且 `IsToolCall()` → 断言 tool_use 块 index 为 0 且后续一致。
- [ ] 辅助断言函数:从 `[]*dto.ClaudeResponse` 提取 `(eventType, index)` 序列,供各用例复用。
- [ ] 运行:`go test ./service/ -run 'TestStreamResponseOpenAI2Claude' -v`。
- [ ] 回归:`go test ./service/ ./relay/channel/openai/ ./relay/channel/claude/ ./relay/channel/gemini/`(覆盖所有 `ClaudeConvertInfo` 消费方所在包)。

## 4. 全量验证

- [ ] `go build ./...`
- [ ] `go test ./...`(全量,确认无其他包因结构体字段删除而挂)
- [ ] 若房内对转换函数有 fuzz / 属性测试惯例,补一个「随机 OpenAI index 序列 → Claude index 永远从 0 连续」的属性断言。

## 5. 部署验证(AC6,上线后,非本任务的代码步骤)

- [ ] 构建镜像、推到 cpa(走既有发布流程,不在本任务执行)。
- [ ] 用同一 Claude Code 客户端对渠道 50(DeepSeek-V4-Flash)重放触发请求(带 tool 定义的 `/v1/messages?beta=true`)。
- [ ] 确认客户端不再报 `Content block not found`,且服务端 `stream_status.status == "ok"`。

## 回滚点

- 全部改动收敛在 2 个文件 + 1 个测试文件,单 commit。`git revert <commit>` 即回滚到代数分配逻辑。无 DB/配置/API 变更,无数据迁移。
