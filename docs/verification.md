# 验证入口

普通构建/单元测试不会调用 OrbStack、安装服务或修改系统配置。以下三类运行验证均为显式入口，不由 `go test` 自动触发。

## 1. 单元、HTTP fixture、模型与静态检查（macOS 或 Linux）

```sh
make check
make race
make build
python3 -m unittest discover -s scripts -p 'test_*.py'
```

覆盖原子写和锁取消、下载校验及失败清理、控制 API HTTP fixture、配置生成、服务归属和命令失败、安装与应用回滚、恢复日志和用户修改保护。TUI 模型测试独立验证异步操作、取消、过期结果与显示状态；模型测试不等于真实终端验证。

## 2. Linux VM 内的测试

先检查已安装的 OrbStack，不创建或启动未知环境：

```sh
orb list
VM=你的专用测试虚拟机
orb -m "$VM" sh -c 'id; uname -m; ps -p 1 -o comm=; systemctl --user is-system-running; loginctl show-user "$(id -un)" -p Linger -p RuntimePath'
./scripts/verify-linux.sh "$VM"
```

要求普通用户、systemd 用户会话、Python 3。脚本根据 VM 实际架构，在宿主编译 Linux 测试二进制，复制至 Linux 的 `mktemp` 目录后**逐包执行**，不依赖 VM 内 Go 的版本。它并非仅交叉编译检查。使用 `CGO_ENABLED=0`，Linux 此入口不声称运行 race；race 单独在宿主运行。

## 3. 真实终端 PTY

`scripts/pty-smoke.py` 使用 Python 标准库 `pty.fork` 创建有 controlling terminal 的进程，发送按键、改变窗口大小并检查渲染和退出。它不调用模型的 `Update` 方法来模拟终端。

```sh
python3 scripts/pty-smoke.py --help
```

`empty` 模式仅操作专用的未安装数据目录，检查默认首页、网络草稿的隐藏输入与放弃、安装前导入、宽屏 Modal/紧凑布局、安装阶段取消和 SIGTERM。`install-local` 模式通过完整 TUI 向导安装明确的本地 Mihomo 二进制并确认未启动服务；`installed` 模式使用已启动的专用服务，以鼠标和键盘切换 PROXY 节点，并通过实际 API 回读确认。`runtime` 模式使用专用流量连接，验证实时卡片、按 UUID 搜索/详情/确认关闭、缩放和停止/启动后的自动恢复。完整的目录、服务、前置检查和清理由下一入口安排，建议通过它执行全部模式。

## 4. 真实 Mihomo + systemd 端到端

```sh
./scripts/e2e.sh "$VM"
# 若明确使用该 VM 已有的标准代理环境变量：
./scripts/e2e.sh "$VM" --proxy-mode environment
```

此脚本显式修改所选 Linux VM 中的**临时资源**：

- 动态检测 CPU 架构、普通用户与用户 systemd，不硬编码用户名或工程目录。
- `mktemp` 数据目录、随机 `lazyclash-e2e-*` unit；运行前检查同名 unit 与 `9097`/`7897` 端口，冲突即失败，不停止已有资源。
- 由产品按明确标签下载官方 Mihomo **v1.19.32** 和 **v1.19.33**，从官方 API 获取预期 SHA256 后校验；另建独立 unit，经 TUI 本地安装和 CLI `--bin` 验证。
- 验证真实默认配置、API 认证和组、PTY 显示与节点切换、本地 HTTP YAML 保存/校验/应用，以及 start/stop/restart 生命周期；运行旧版时安装新版、同版本复用和显式重装均检查 PID、旧运行版本和配置字节保持不变。
- `trap` 仅清理本次确认所属的 unit、进程和文件，失败也执行；不读取旧代理配置，不修改 macOS 代理、DNS 或路由。

日志保存在忽略的 `.test-artifacts/`，只包含脱敏场景输出、PTY 文本、测试结果与清理状态，不保存 secret、原始配置或未过滤 journal。失败时先查看这些诊断，不直接贴出原始配置。

## 已执行环境

2026-10-28：

- 宿主 macOS/arm64，Go 1.27.1：构建、全项目 `go test`、`go vet`、`go test -race` 通过。
- 复用现有专用 OrbStack Debian 12/arm64 测试 VM（本次选择 `proxyctl-e2e`），普通用户、systemd 252、用户 manager 可用且 Linger=yes。脚本仍要求调用者显式选择 VM，不依赖此名称。
- `verify-linux.sh` 将各包测试二进制放入 VM 后实际执行，包括 TUI 模型与 HTTP fixture。
- `e2e.sh` 实际运行官方下载/校验与本地安装、有效默认配置、HTTP 订阅、API、PTY 和服务生命周期；SIGTERM/按键取消等待清理。
- 清理检查确认专用 unit `LoadState=not-found`，7897/9097 释放，临时文件及测试进程清除；脱敏记录在 `.test-artifacts/`。

该日期使用当时的依赖版本；当前版本验证见下文。未实际验证 Linux/amd64、其他发行版或真实注销/重启后的服务持续运行。没有把交叉编译视为这些环境的运行证明；macOS 服务管理明确不支持。


## 响应式首页、网络设置与向导（2026-10-29）

当前依赖已统一为稳定 Charm v2：Bubble Tea 2.1.1、Bubbles 2.2.1、Lip Gloss 2.0.6、Fang 2.0.1，最低构建声明为 Go 1.26。

测试分类与可重复入口保持独立：

- `make check`：全项目测试与 vet。网络 fixture 包含 official/prefix/mirror、None/环境/自定义代理、来源与代理组合、HTTP/HTTPS CONNECT、认证 SOCKS5/SOCKS5H、API 限流和 HTML、TLS/DNS/超时分类、文件 Range 限量和摘要不匹配。全部使用本地 fixture，不测试开发者真实代理。
- `make race`：在宿主 macOS/arm64、Go 1.27.1 运行全项目 race。
- TUI 模型：默认进入首页、单一 Core/Version/Confirm/Installing/Result 状态机、网络草稿和保存、测试身份、重试、迟到消息、取消清理、键鼠/Paste/KeyRelease、focus及背景隔离。布局覆盖 160×45、120×32、100×28、80×24、60×20、120×18 和极小窗口；长中文/路径不会越界。
- `verify-linux.sh VM`：按 VM 的实际架构构建并在 Debian 12/arm64 内执行所有测试二进制，包含新网络和 TUI 测试。不是只做交叉编译。
- `e2e.sh VM`：真实 controlling PTY 和真实 Mihomo/systemd 生命周期。PTY runner 使用纯 Python 标准库还原当前 VT 屏幕，以适配 v2 按单元格更新的 renderer；不能把删除 ANSI 后拼接的历史输出误当当前屏幕。

模型 View 的 ANSI 渲染另做颜色/宽度图像检查，诊断图保存在忽略的 `.test-artifacts/`。这些是模型渲染检查，与真实 PTY 运行证据分开。

实测平台仍为宿主 macOS/arm64 和专用 OrbStack Debian 12/arm64。尚未在 Linux/amd64、其他发行版或最低 Go 1.26 工具链实跑；没有 macOS 内核安装或服务管理后端。真实网络可能遇到官方 API 限流或文件传输超时，此时记录失败阶段并清理，不切换未知镜像或跳过摘要。


最终本轮结果：以上宿主 check/race/build 与 Linux 全包测试均通过。v2 完整端到端以 `e2e.sh VM --proxy-mode environment` 通过，仍访问官方 API 和官方资产；没有 cache 或镜像 fallback。VM 的 No proxy 直连在文件下载出现超时，产品正确报告并清理；显式环境模式使用测试 VM 已有的标准代理变量，不读取 proxyctl 配置，不修改代理、DNS 或路由。日志只记录所选模式，不输出原始环境地址或凭据。

真实 PTY 包含默认 Overview、网络隐藏输入/草稿放弃、预安装导入、宽窄切换、Core/Version/Confirm/Installing/Result、本地安装、安装阶段 Esc 和 SIGTERM 清理，以及鼠标导航和 PROXY→REJECT 后实际 API 回读。真实更新从已运行 v1.19.32 安装 v1.19.33，断言 PID、API 报告的旧运行版本及配置字节不变；同版本复用与显式重装也保持原进程。最终 `cleanup.txt` 显示 resources released=1、exit=0，unit 为 not-found，7897/9097 释放，专用进程和临时目录不存在。

## 入口与业务边界整理（2026-10-29）

入口已移动到根目录 `main.go`，构建与 E2E 脚本统一使用模块根目录。CLI 在 `internal/cli` 内按命令职责拆文件；应用层在 `internal/app` 内按安装、安装记录、恢复、配置、运行及快照拆文件。首次安装的暂存、发布和服务注册分别执行，继续由同一锁和恢复日志保护。

本轮实际通过：

- macOS/arm64、Go 1.27.1：`make check`（全包测试与 vet）、`go test -race ./...`、`make build` 和根入口 `--help`。
- 新增安装边界测试：暂存不依赖服务，文件和配置验证后才注册最终路径，注册期间 journal 仍存在；暂存/注册失败或取消会清理，已有配置保持不变。
- `scripts/verify-linux.sh proxyctl-e2e`：Debian 12/arm64 内实际执行所有包测试，包含新增测试。
- `scripts/e2e.sh proxyctl-e2e --proxy-mode environment`：真实 PTY 三种模式、官方 Mihomo v1.19.32/v1.19.33、用户 systemd 生命周期、配置应用、节点切换、运行中更新/重装及取消恢复通过。
- 清理返回 `resources released=1`、`exit=0`，专用 unit 为 `not-found`；额外检查确认临时目录、测试进程已清除，7897/9097 可重新绑定。

服务范围仍为 Linux/systemd 用户服务。未新增 unisvc 依赖或其他平台后端，未扩大已实测平台范围。

## 运行监控与 Connections（2026-10-29）

本轮实际通过：

- macOS/arm64、Go 1.27.1：`make check`、`make race`、`make build`。业务测试使用临时目录和本地 HTTP fixture，验证真实累计来源、指标独立失败、取消关闭请求、采样不阻塞停止、停止/PID 变化/归属变化后丢弃旧样本及精确 ID 关闭。TUI 模型覆盖单任务刷新、迟到消息、搜索输入、列表重排和焦点、关闭确认、故障恢复以及既有尺寸矩阵。
- `python3 -m unittest discover -s scripts -p 'test_*.py'`：4 个 VT 协议回归测试，不启动子进程、服务或 VM。
- `scripts/verify-linux.sh lazyclash-e2e`：在本次显式选择的 Debian 12/arm64 专用 VM 内实际执行所有 Go 测试。普通用户、用户 systemd 可用，Linger=yes。
- `scripts/e2e.sh lazyclash-e2e --proxy-mode environment`：官方 Mihomo v1.19.32/v1.19.33 的下载/校验/安装/更新，以及 empty、install-local、installed、runtime 四类真实 PTY 场景通过。仍使用显式所选 VM 的既有标准代理变量，不读取旧代理配置或修改网络设置。
- 专用本地 HTTP fixture 接收经 Mihomo mixed 端口的两条独立连接，每条上传 256 KiB 并持续下载；断言实际正速率、累计增长和 RSS。CLI 关闭第一条时第二条仍存活，TUI 按完整 UUID 搜索、查看和确认关闭第二条，再从真实 API 回读；关闭后累计保留，内核停止后指标不可用，重新启动后累计为零且 TUI 自动恢复。
- 最终 `cleanup.txt` 为 `resources released=1`、`exit=0`，专用 unit 为 `not-found`，测试进程和目录清除，监听端口释放。诊断保存在忽略的 `.test-artifacts/`。

初次回归发现两处测试工具问题并已修复：端口探测使用与 Go 监听一致的 `SO_REUSEADDR`，容许已关闭连接的 TIME_WAIT，另行验证仍拒绝现存 listener；VT 屏幕解析补齐渲染器使用的滚动区域、插入/删除行、滚动和反向制表指令，保留当前屏幕断言。修复后完整端到端复跑通过，没有改用历史输出匹配掩盖显示问题。

本轮未实测 Linux/amd64、其他发行版、真实注销/重启后的持久运行或最低 Go 1.26 工具链。仅支持 Linux/systemd 用户服务，累计流量不跨内核重启持久化。

## 只读规则查看（2026-10-29）

已运行并通过：

- 宿主 macOS/arm64、Go 1.27.1：`make check`、`make race`、`make build`，以及 4 个 Python VT 协议测试和脚本语法检查。
- API fixture：真实规则 schema、原始顺序与不同 index 的重复规则、空 MATCH 条件、`size=-1/0`、可选统计、有效空数组、HTML/无效字段/HTTP 错误/超限/取消和脱敏。
- 应用用例：服务不可用时不访问 API，查询不持锁阻塞停止，跨停止/进程/安装身份/归属变化的旧规则被拒绝；CLI 输出顺序一致，查询不改配置或服务。
- TUI 模型：独立查询/五秒复查、重试及迟到消息、搜索文本、规则详情身份、索引复用、键鼠和窗口尺寸矩阵；额外检查 100×20 时第六导航不会被 Core 摘要覆盖。
- `scripts/verify-linux.sh lazyclash-e2e`：在显式选定的专用 Debian 12/arm64 VM 内实际执行全部 Go 测试。
- `scripts/e2e.sh lazyclash-e2e --proxy-mode environment`：真实官方 Mihomo v1.19.32/v1.19.33、systemd 和全部 PTY 模式通过。新增 rules 模式实际验证规则先后顺序、原 index、空 MATCH、搜索/详情/60×20 缩放/刷新、停止后隐藏旧列表与重启恢复；配置 hash 前后相同，无订阅默认配置的单条 Match 也可读取。

新增脱敏终端记录为 `.test-artifacts/pty-rules.txt`。本轮完整端到端首次执行通过，`cleanup.txt` 为 `resources released=1`、`exit=0`，专用 unit 为 `not-found`；额外检查确认临时目录、测试进程和监听端口已清理。

未扩大平台支持和实测范围。规则编辑、启用/禁用、提供器展开与配置覆盖仍未实现；列表显示内核已加载的规则，匹配顺序仅在 Rule 模式适用。


## Mariana + Adaptive 默认主题（2026-10-29）

来源、计算和颜色回退见 [主题映射](theme-mariana.md)。使用本机 Sublime Text 4 Build 4215 随附的 Mariana 与 Adaptive 资源；没有读取 Sublime 用户偏好。

本轮实际通过：

- macOS/arm64、Go 1.27.1：`make check`、`make race`、`make build`，Python VT 四项协议回归及新增预览/PTY 脚本语法检查。
- 模型：全部向导阶段、两种首页、网络结果、输入错误、禁用/选中/悬停/焦点；True Color、ANSI256、ANSI16、无色，指定尺寸及极小窗口。真实颜色配对检查正文、说明、选区、主按钮、错误和焦点的对比度。
- `scripts/theme-pty.py`：macOS/arm64 与显式选择的 OrbStack `lazyclash-e2e`（Debian 12/arm64，普通用户）的真实 controlling PTY。真彩、256 色、NO_COLOR 三种模式均完成五种尺寸、网络隐藏草稿、Tab 焦点、缩放保持、Esc 放弃与返回。输出检查无 OSC10/11 设置、256 色无 RGB、无色无实际 SGR 样式。
- `scripts/verify-linux.sh lazyclash-e2e`：Linux 全包测试二进制实际执行通过，暂存目录清理完成。
- `scripts/theme-preview.py`：导出真实模型 ANSI View，以 Menlo 实际字体生成独立 PNG，逐页检查首页、宽窄向导、网络结果、输入错误、下载、取消、成功/失败及256色回退。它们是模型排版预览，不能作为真实内核运行或服务验证证据。

证据位于忽略目录 `.test-artifacts/mariana/`；PTY 使用独立临时 home 和随机服务名，没有查询版本、安装内核或创建服务，临时进程/文件已清理。本轮主题修改未重跑真实 Mihomo/systemd 安装生命周期 E2E；此前记录单独保留。未逐一实测 Termius、iTerm2 等原生终端前端，16色仍受终端基础调色板影响。
