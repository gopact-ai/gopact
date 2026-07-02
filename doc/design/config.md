# gopact 配置注入设计

<!-- gopact:doc-language: zh,en -->

## 中文

本文档是 gopact 开源文档集的一部分，中文内容用于说明当前仓库约束、能力或维护流程。

## English

This document is part of the gopact open-source documentation set. The English section gives an entry point for readers who prefer English, while the remaining sections preserve the maintained technical details.


日期：2026-06-23

设计入口：[index.md](index.md)

配置注入系统负责把 provider、tool、sandbox、memory、skill、MCP、A2A、plugin、transfer、channel adapter 组合进 Runner。`gopact` 是 Go SDK，不拥有任何配置文件格式，不读取配置文件，也不从环境变量自动装配运行时。

配置文件、环境变量、远程配置中心、命令行参数、Kubernetes ConfigMap、Lark/内部平台配置等都属于应用层。应用层可以用任何格式解析配置，然后把结果转换成 `gopact` 的 Go options、typed structs、interfaces 或 adapter factories。

## 配置边界

配置注入回答：

- 启用哪些模块和 adapter；
- 每个模块使用什么默认实现；
- provider route 如何选择；
- sandbox、MCP、A2A、artifact export 有哪些权限；
- plugin/exporter/transfer/channel adapter 如何接入；
- secret provider 如何传入；
- 配置快照版本是什么。

配置注入不回答：

- 外部配置文件用 TOML、YAML、JSON、HCL 还是数据库；
- 外部配置文件路径、监听、合并和优先级；
- 环境变量命名规则；
- graph 业务分支；
- node 内部业务状态；
- prompt 内容；
- 单个工具的业务参数；
- checkpoint state。

## SDK 边界

`gopact` 必须坚持这些边界：

- 不提供 `LoadConfig(path)`、`ReadConfigFile`、`FromEnv` 这类入口；
- 不内置 TOML/YAML/JSON 配置文件解析语义；
- 不扫描全局环境变量；
- 不让 adapter 私自读取文件或环境变量；
- 不把 provider adapter 的私有配置格式泄露成 SDK 公共 API；
- 不把配置热加载实现成文件 watcher。

SDK 只接收已经解析好的 Go 值。SDK 级默认值和 setup 入口见 [sdk.md](sdk.md)：

- runner-level options：应用初始化 Runner 时传入；
- run-scoped options：单次 run 传入，只能收窄权限，默认不能扩大 runner 级 policy；
- injected interfaces：provider、sandbox、memory、MCP、A2A、channel 等后端以接口或 factory 注入；
- typed snapshots：用于审计、replay、热替换和事件记录。

## 注入结构草案

```go
type ConfigSnapshot struct {
	Version   ConfigVersion
	App       AppOptions
	Provider  provider.Options
	Sandbox   sandbox.Options
	Memory    memory.Options
	Skills    skill.Options
	MCP       mcp.Options
	A2A       a2a.Options
	Plugins   []Plugin
	Transfers []TransferBinding
	Channels  []ChannelBinding
	Policy    policy.Policy
	Secrets   SecretProvider
}

runner, err := gopact.NewRunner(
	graph,
	gopact.WithConfigSnapshot(snapshot),
	gopact.WithProviderRegistry(providerRegistry),
	gopact.WithProviderRouter(providerRouter),
	gopact.WithSandbox(sandboxBackend),
	gopact.WithMemory(memoryStore),
	gopact.WithPlugins(plugins...),
)
```

上面的类型是设计形态，不要求所有字段都放在一个巨大的 struct 里。实现时可以拆成 `Option`、`Registry`、`Router`、`Policy`、`SecretProvider` 等接口，但公共语义必须是“外部传入 Go 值”，不是“SDK 读取配置文件”。

## 外部配置映射

应用层可以这样接入自己的配置系统：

```text
TOML/YAML/JSON/env/remote config
  -> app-owned loader
  -> app-owned validation/defaulting
  -> gopact typed options/interfaces
  -> Runner / TurnLoop
```

SDK 侧只负责校验 typed options 的运行时约束：

- adapter 名称或 factory 存在；
- provider/model candidate 非空；
- fallback candidate 满足 required capability；
- sandbox command allowlist 合法；
- skill root 在允许路径内；
- MCP/A2A endpoint 有明确 trust boundary；
- plugin strict/failure policy 明确；
- transfer 不要求访问 Runner 内部状态；
- channel action 有明确的 policy 和身份映射。

## Secret Provider

secret 也由外部注入。SDK 不提供默认 env loader；当前 root package 已提供最小原子契约：

```go
type SecretProvider interface {
	ResolveSecret(ctx context.Context, ref SecretRef) (SecretValue, error)
}
```

规则：

- secret value 不写入事件、checkpoint、artifact、trace；
- `SecretValue` 的 `String`、`fmt` 和 JSON 表示必须始终 redacted，只有显式调用 `Bytes()` 的 adapter 能取得拷贝；
- secret provider 可以由应用实现为 env、file、OS keychain、remote secret service 或内部密钥系统；
- secret ref 可以进入 config snapshot，secret 明文不能进入；
- sandbox 默认不继承 secret，除非 policy 明确授权。

## 热替换

SDK 不监听配置文件。热替换由应用层触发：应用层解析并校验新的外部配置后，生成新的 `ConfigSnapshot` 或新的 module instances，再原子替换 Runner 持有的 typed snapshot。

```text
app loads external config
  -> app converts to gopact typed options
  -> gopact validates runtime constraints
  -> compute or accept ConfigVersion
  -> atomically swap snapshot
  -> emit ConfigSnapshotReloaded
```

规则：

- 正在运行的 `RoutePlan` 使用创建时的 `ConfigVersion`；
- 新 run 使用最新配置；
- 热替换不能改变正在执行中的 sandbox mount、MCP session 权限或 A2A delegation policy；
- 如果配置变更影响 resume，必须产生 config drift 事件。

当前代码第一片不计算配置版本，也不读取配置文件；应用可以通过 `graph.WithConfigVersion` 或 `checkpoint.WithConfigVersion` 注入版本。checkpoint 解码时默认拒绝 stored/current version 不一致，显式允许时会把 drift 写入 `checkpoint_config_drift` metadata，并由 `checkpoint_loaded` 事件透传。

## 默认配置

默认值必须保守：

- provider registry 只包含 fake provider；
- sandbox 默认拒绝网络和 workspace 外文件；
- memory 默认使用 noop 或 in-memory；
- skill 默认只加载显式 root；
- MCP/A2A 默认不连接外部 endpoint；
- plugin/exporter 默认关闭外部网络；
- TUI channel 可以作为本地开发默认 channel；
- 外部 channel 默认关闭。

## 测试要求

- typed options 校验和默认值；
- SDK 不提供配置文件 loader；
- SDK 不扫描全局环境变量；
- secret provider 返回值不泄漏；
- route snapshot version 写入事件；
- typed snapshot 原子替换；
- run-scoped option 不能扩大 policy；
- sandbox allowlist 校验；
- channel secret 引用不泄漏；
- transfer/channel adapter 名称校验；
- unknown adapter 返回结构化错误。
