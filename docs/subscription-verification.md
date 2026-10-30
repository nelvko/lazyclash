# Profile 与运行配置验证

当前实现与测试日期：2026-10-30。模型和流程见 [设计](subscription-design.md)，早期方案调研见 [调研记录](subscription-research.md)。

## 当前模型与删除范围

配置库 v3 仅保存 Profile，每条直接拥有地址、名称、更新时间、原文 revision 与用户覆写。已删除独立来源表、来源注册/刷新/删除接口、来源 DTO、共享引用、subscription CLI、profile create/Managed 配方和 Builder 节点抽取路径。没有把该功能移到隐藏入口，也没有旧格式兼容或迁移代码。

## 本轮验证

| 检查 | 结果与边界 |
| --- | --- |
| go test ./... | 全包通过，真实内核集成默认跳过 |
| go vet ./... | 全包通过 |
| go test -race ./... | 全包通过，包含当前存储模型、完整配置构建、名称、更新与删除事务及 TUI |
| TUI 单元/交互/画布 | 直接 Profile 元数据、刷新、重命名、路径补全、首次失败/重试与窄屏通过 |
| 真实 PTY | 重新编译后 21 场景通过，10 次 loopback HTTP 请求，未安装内核 |
| Python 辅助检查 | 4 项终端屏幕模型测试；所有脚本 AST、e2e.sh Shell 语法检查通过 |
| Linux/systemd 配置 E2E | 脚本已适配两个完整 Profile，本轮未重新执行真实 VM 链路 |

普通业务测试只用临时目录、HTTP fixture、fake 服务。监听 fixture 的测试在允许本机临时端口的执行环境运行；没有访问真实用户订阅、安装内核或修改系统代理。

## 业务与失败恢复覆盖

- 完整订阅/本地文件导入只保存 Profile，不选择、不启动；一次性导入不保存原地址，记源是显式操作。
- 原文快照先写、索引后提交；中断和过期 CAS 不会发布部分 Profile。错误版本、缺失字段、已删除的来源表或配方字段均严格拒绝，损坏原文件不被重写。
- 名称 UTF-8/Base64、RFC 5987 filename*、传统 filename、中文/特殊字符、URL 回退、无效头部、控制字符清理、NFC/大小写/空白比较与长度限制。
- 自动名称随有效响应同步，缺失名称头保留原名，手动名称受到保护；重复更新不累加后缀，改名不改变 ID、快照路径、运行代或 Pending。
- 同 URL 两次导入的 Profile 独立更新、改地址和删除；不再存在共享来源的分离或引用维护。
- 删除 Profile 后索引不残留地址、来源记录或后续下载任务；HTTP 请求计数确认 Update all 不下载已删项。已应用 Profile 删除受保护，历史快照保留。
- 批量刷新只处理可刷新 Profile；一个失败不丢弃其他成功，取消停止后续任务，一次性导入不被刷新。
- HTTP 失败、错误 JSON/YAML、其他内核格式、引用错误、内核校验拒绝和取消，均不部分更新名称、地址、原文 revision 或覆写。
- 原文与覆写分开；整体字段替换、顺序规则、受管字段拒绝、组/节点/Provider/dialer 引用及循环检查继续覆盖。
- 下载更新、改地址、编辑覆写不改变已应用配置；运行代固定 revision 与覆写，显式 Apply 才加载。Apply 失败恢复旧代，保存的草稿仍可重试。
- 停止时 Apply 不启动；端口/mode 调整使用已应用配置；原有 journal、Stop 优先、进程变化、外部修改保护回归保留。

故障恢复由确定性 fixture 注入重启失败、恢复失败、取消和文件/进程变化。这些测试不等于已经模拟真实系统任意掉电。

## 终端路径导入

`scripts/profiles-pty.py` 使用当前二进制、临时私有 home 和 loopback fixture，覆盖 URL 可见输入和自动命名、服务端更名、手动名称保护、地址修改、批量刷新、重复导入、窄屏标题栏、退出重进持久化。

本地导入覆盖实时目录/文件匹配、两级目录补全、方向键选择、Tab 不提交、Enter 才导入、中文和空格路径及无扩展名配置。保存前后检查没有 installation 状态，不把卡片打开或 Tab 补全当成 Apply。

```sh
go build -o /tmp/lazyclash-profiles-simple .
python3 -B scripts/profiles-pty.py --binary /tmp/lazyclash-profiles-simple \
  --output /absolute/path/to/isolated-output
```

本轮结果保存在忽略目录 `.test-artifacts/profiles-simple/result.json`。TUI 模型另外验证首次状态读取失败显示不可用与 Retry，不渲染虚假的 0 saved、空白 default 卡片或“未安装”；成功重试恢复，后续刷新失败保留已成功快照。

路径组件不递归，只读取当前目录项，最多扫描 4096 项、保留 200 候选，过大时提示缩小路径。URL 不扫描，过期消息和关闭后的消息不会覆盖输入；隐藏文件需点前缀，符号链接/特殊文件不列为候选。绝对/相对/~/ 路径提交前归一化，SSH 中对应远程文件系统。

画布检查涵盖 120×32 至 8×4，24×7 保留输入及取消提示。可用 `LAZYCLASH_PROFILES_PREVIEW_DIR=/absolute/path go test ./internal/tui -run '^TestExportProfilesViews$' -count=1` 导出 Profile 和导入窗口的合成预览；它们不是真实订阅或服务截图。

## 真实内核与 Linux 验证边界

完整配置 Builder 的 opt-in 入口仍保留：

```sh
LAZYCLASH_TEST_MIHOMO=/absolute/path/to/mihomo \
  go test -race ./internal/mihomo -run 'TestMihomo.*Integration$' -count=1 -v
```

使用显式提供的二进制，在临时目录与端口运行内核并清理，不自动下载或安装服务。当前覆盖 Full、覆写、Default、Provider 缓存隔离、原生规则加载与选择持久化；不再含 Managed 场景。本轮未重跑该真实内核入口。

此前 2026-10-29 的真实验证使用官方 Mihomo v1.19.33 / Darwin arm64，发布物 SHA256 核对为 `060b64b0f2c9a310831db0fe4adb2878de67bfa606c311813c1f13966aa504b6`。真实 Linux 测试在已有 Debian 12/arm64 专用 VM、普通 uid 501、systemd 252、Linger=yes 下执行。历史完整 E2E 验证过安装、更新、生命周期、SIGHUP 后 PID 保持、SIGKILL 恢复、mode/selector 持久化和清理；不能据此声称新 v3 Profile 全链路已经实跑 systemd。

`scripts/configuration-e2e.py` 本轮已改为两个完整 Profile，核对原文 SHA、运行 manifest revision、最终配置摘要及 live rules；包含覆写、下载更新、显式切换、选择跨重启和删除保护。损坏未启用快照的场景在 finally 恢复，验证 Apply 的输入完整性拒绝，不冒充加载后回滚。复现完整安装与生命周期仍需明确指定已有专用 VM：

```sh
./scripts/e2e.sh VM_NAME --proxy-mode environment
```

历史端口测试曾有一次本地代理请求失败，后续两次完整重跑通过，原次缺少 HTTP 诊断，根因仍未确定。基本就绪检查不保证后续每次请求成功，该观察保留为请求诊断限制。

## 尚未验证或未实现

- 本轮未运行真实 Linux/systemd 安装与完整生命周期 E2E，未覆盖 Linux/amd64 或其他发行版。
- 没有产品热加载、后台定时刷新、历史回退 UI、自动 GC、多订阅组合或 TUN。
- 顶层配置版本不能表示 Mihomo 自行刷新的全部远程 Provider 内容；本地资产不会随导入自动复制。
- 普通测试不证明真实网络、远程节点、DNS 或任意断电场景可靠。
