# 实施计划：统一渠道外部站点跳转

## 实施顺序

1. **建立共享外链规则**
   - 从模型路由的两个重复实现中抽取 `normalizeExternalUrl`。
   - 让模型路由策略与指标页面改用该单一实现，补齐 URL 规范化单测。

2. **补齐管理员日志的渠道展示数据**
   - 实现按页去重的渠道展示信息解析器，返回名称、当前 Base URL 与存在状态。
   - 扩展通用日志管理员查询结果，保留现有 `channel_name` 并新增 `channel_base_url`。
   - 在绘图、任务日志的管理员控制器响应装配中增加同字段；禁止修改自助和对外任务响应的数据暴露边界。
   - 为前端日志类型增加可选字段。

3. **统一渠道单元格渲染**
   - 以可复用链接徽标替代绘图和任务日志的 `createChannelColumn` 输出。
   - 将通用日志的 `ChannelCell` 中 `#id` 徽标改为同一链接徽标，并保留附加状态和弹层。
   - 确认移动端卡片通过列单元格自动获得行为；如存在额外渠道展示，改为调用同一组件。

4. **验证和回归**
   - 单测覆盖 URL 规则和批量映射。
   - 控制器/模型测试覆盖管理员字段补全、渠道不存在和自助接口不泄露。
   - 运行 default 前端类型检查、相关测试与后端 Go 测试。
   - 使用管理员账号手工验证三类日志的桌面和窄屏视图。

## 建议修改区域

| 层 | 主要位置 |
| --- | --- |
| default 共享工具 | `web/default/src/lib/` |
| 模型路由引用点 | `web/default/src/features/model-route/index.tsx`、`components/policy-sortable-group.tsx` |
| 使用日志前端 | `web/default/src/features/usage-logs/components/columns/`、`data/schema.ts`、`types.ts` |
| 通用日志 API | `model/log.go`、`controller/log.go` |
| 绘图日志 API | `model/midjourney.go`、`controller/midjourney.go` |
| 任务日志 API | `model/task.go`、`controller/task.go`、必要时管理员专用 DTO 装配处 |
| 渠道查询复用 | `model/channel.go`、`model/channel_cache.go` 或现有展示查询辅助位置 |

## 验证命令（实施时确认脚本名称）

```bash
cd web/default && bun test
cd web/default && bun run typecheck
go test ./model ./controller
```

若仓库脚本与上述命令不符，以 `web/default/package.json` 和当前 CI 配置声明的等价命令为准。

## 审查清单

- [ ] 不存在第三份 URL 规范化逻辑。
- [ ] `channel_base_url` 没有进入自助日志和通用对外任务 DTO。
- [ ] 任何一页日志的渠道查询次数与行数无关。
- [ ] 链接仅在可用 HTTP(S) 地址时渲染，且始终带 `noopener noreferrer`。
- [ ] 三种日志、桌面/移动布局，以及通用日志的附加控件均已回归。
