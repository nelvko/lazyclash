# 验证入口

普通构建/单元测试不会调用 OrbStack、安装服务或修改系统配置。以下三类运行验证均为显式入口，不由 `go test` 自动触发。

## 1. 单元、HTTP fixture、模型与静态检查（macOS 或 Linux）

```sh
make check
make race
make build
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

`empty` 模式仅操作专用的未安装数据目录，检查默认首页、网络草稿的隐藏输入与放弃、安装前导入、宽屏 Modal/紧凑布局、安装阶段取消和 SIGTERM。`install-local` 模式通过完整 TUI 向导安装明确的本地 Mihomo 二进制并确认未启动服务；`installed` 模式使用已启动的专用服务，以鼠标和键盘切换 PROXY 节点，并通过实际 API 回读确认。完整的目录、服务、前置检查和清理由下一入口安排，建议通过它执行两种模式。

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
