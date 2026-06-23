# gopact Agent Template 设计

日期：2026-06-23

设计入口：[index.md](index.md)

Agent template 是基于 core runtime 的可复用 graph 组合。它不是特殊 runtime，也不是不透明 agent class。

## 原则

- template 使用 `Runner`、graph、provider router、tool registry、memory、artifact、policy；
- template 不直接依赖具体 provider adapter；
- template 的每一步必须产生事件；
- template state 应该可 checkpoint；
- template 可以提供默认 prompt，但 prompt library 不进入 core；
- template 的行为必须能用 trajectory test 断言。

## 第一批 template

| Template | 目标 | 阶段 |
| --- | --- | --- |
| ReAct | 模型-工具循环，完成单 agent 任务 | M5 |
| Plan-Execute | 先计划再执行，可插入人工审批 | M5 后 |
| Supervisor | 多 worker agent 路由和合并 | M6 后 |
| Agent-as-Tool | 把本地或 A2A agent 暴露为 tool | M4/M5 |
| Dev Agent | 用于自举的仓库开发 agent | M5 后 |

M5 前可以先写 template 规格和测试 fixture，不需要公开稳定 API。

## ReAct State

```go
type ReActState struct {
	Messages      []gopact.Message
	VisibleTools  []tools.ToolInfo
	DeferredTools []tools.ToolInfo
	Artifacts     []artifact.Ref
	Steps         []ReActStep
	Final         *gopact.Message
}

type ReActStep struct {
	CallID     string
	ModelRoute gopact.ModelRoute
	ToolCalls  []gopact.ToolCall
	ToolResults []gopact.ToolResult
	Error      string
}
```

规则：

- `Messages` 是模型上下文，不是完整事件轨迹；
- 完整轨迹来自 event stream；
- 大工具结果用 artifact ref；
- memory recall 结果必须以可审计 part 或 context block 注入；
- deferred tools 不直接暴露给模型。

## ReAct Graph

```text
Start
  -> load_context
  -> select_tools
  -> call_model
  -> maybe_call_tools
  -> decide_next
  -> End
```

节点说明：

- `load_context`：从 memory、checkpoint、resume payload 准备上下文；
- `select_tools`：从 tool registry 获取 visible tools，必要时搜索 deferred tools；
- `call_model`：通过 provider router 调用模型；
- `maybe_call_tools`：执行模型请求的工具调用；
- `decide_next`：判断 final、继续、interrupt、失败或达到最大步数。

## 终止条件

ReAct 必须有显式终止条件：

- 模型返回 final message；
- 没有 tool call 且没有继续信号；
- 达到 `MaxSteps`；
- context budget 不足且无法压缩；
- policy deny；
- interrupt raised；
- unrecoverable provider/tool error。

达到 `MaxSteps` 不是成功，必须产生结构化错误和事件。

## Tool loop 规则

- 工具调用前必须经过 tool middleware 和 policy；
- 工具结果必须产生事件；
- 非幂等工具不能被自动重试；
- tool result 不自动成为高优先级 instruction；
- 工具调用失败是否继续由 template policy 决定，并产生事件；
- visible tool set 是单次 model request 的快照。

## HITL

HITL 通过 interrupt/resume 表达：

- tool approval；
- 模型不确定时请求用户选择；
- 高风险 artifact export；
- sandbox 写入或命令执行；
- A2A delegation。

template 不能阻塞等待用户输入。

## Dev Agent 自举语义

Dev Agent 是用于维护 `gopact` 仓库的 template。

自举模式：

| 模式 | patch 生成 | patch apply |
| --- | --- | --- |
| plan mode | 可以生成 patch 建议 | 不 apply |
| write mode | 可以生成并 apply patch | 必须经过 sandbox、policy、event、diff |

Level 1 自举只读分析和计划；Level 2 可以受控写 docs/examples/tests/adapter 骨架；Level 3 可以处理低风险 core 变更，但 release、权限扩大和破坏性修改仍需人工 gate。

## 测试要求

ReAct template 至少有这些 trajectory tests：

- 一次模型直接 final；
- 一次模型调用工具后 final；
- visible/deferred tool 隔离；
- tool policy deny；
- tool approval interrupt/resume；
- max steps exceeded；
- provider fallback event；
- checkpoint 后 resume 不重复非幂等工具；
- memory recall 注入可观察；
- artifact ref 出现在 final 或事件中。
