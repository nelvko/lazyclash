# 验证入口

普通构建/单元测试不会调用 OrbStack、安装服务或修改系统配置。以下三类运行验证均为显式入口，不由 `go test` 自动触发。

## 新建配置表单（2026-11-02）

本轮第二个按钮使用 New profile / 新建配置，统一创建远程 URL 或本地文件配置，并接受可选显示名称、描述。已验证：

- 宿主 macOS/arm64：全包 `go test ./...`、`go vet ./...`、构建，以及新增 HTTPS 创建/取消/失败原子性、描述清空业务测试通过。TLS 使用本地 fixture 的受信客户端，不关闭产品证书校验。
- `./scripts/verify-linux.sh lazyclash-e2e` 在现有专用 Debian 12/arm64 VM 内实际运行全部测试二进制通过，包含新增配置创建业务测试和表单模型测试。
- `scripts/profiles-pty.py` 通过真实 controlling PTY 验证本地路径匹配/Tab 补全、已完成路径到创建按钮的焦点切换、远程名称与描述、忙碌期间防重复提交、一次下载、保留来源更新、退出重开元数据持久化，以及现有卡片菜单和命名回归。
- 表单模型覆盖 120×30、80×24、60×20、40×16、40×14，操作按钮命中区与错误保持可见。中英文实际 PTY 屏幕另检查 120×32、80×24、60×20、40×16、40×14，保存屏幕文字及按 VT 单元格重绘的 PNG；PNG 为终端状态预览，不是原生终端应用截图。
- Python 终端屏幕解析测试通过。脱敏结果位于忽略的 `.test-artifacts/profile-create-20261102-final/`，表单预览在 `.test-artifacts/profile-create-20261102/previews/`。

本轮 PTY 使用临时未安装目录和 loopback HTTP fixture，不安装或启动内核；没有重跑真实 Mihomo/systemd 的完整 E2E，也不把测试中的假服务视为真实服务验证。

分批提交复核：每个代码批次均单独导出 Git 暂存树，以 `go test -mod=readonly ./...` 验证其不依赖后续未提交文件；完整代码树另通过 vet 与构建。主题 PTY 已按当前首次引导、导航层级和通知行为更新，并在 Truecolor、256 色、NO_COLOR 的 160×45、120×32、80×24、60×20、120×18 下通过，结果位于 `.test-artifacts/commit-theme-20261102/`。

`scripts/i18n-pty.py` 同时通过实际语言切换、退出重开后的偏好持久化、中文鼠标命中、帮助、网络表单和缩放检查，记录在 `.test-artifacts/commit-i18n-20261102/`；Python 屏幕解析单元测试、全部 Python 脚本语法及 E2E Shell 语法检查通过。

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

`empty` 模式仅操作专用的未安装数据目录，检查默认首页、网络草稿的隐藏输入与放弃、安装前导入、宽屏 Modal/紧凑布局、安装阶段取消和 SIGTERM。`install-local` 模式通过完整 TUI 向导安装明确的本地 Mihomo 二进制并确认未启动服务；`installed` 模式使用已启动的专用服务，以鼠标和键盘切换 PROXY 节点，并通过实际 API 回读确认。`runtime` 模式使用专用流量连接，验证实时卡片、按 UUID 搜索/详情/确认关闭、缩放和停止/启动后的自动恢复。`runtime-settings` 模式验证端口输入、取消草稿、占用后的纠正、确认重启、宽窄切换及各控制 API 页面的重新连接。完整的目录、服务、前置检查和清理由下一入口安排，建议通过它执行全部模式。

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


## 七页导航、紧凑布局与实时日志（2026-10-29）

导航与实际单键快捷键统一为 `1 Overview`、`2 Proxies`、`3 Profiles`、`4 Connections`、`5 Rules`、`6 Logs`、`7 Settings`。侧栏数字为右侧提示，不属于页面名称；输入、搜索和帮助层中的数字按当前控件语义处理。当前页与键盘焦点保持独立，侧栏/列表各自作为焦点组。

本轮实际运行通过：

- `scripts/verify-linux.sh lazyclash-e2e`：在已有专用 OrbStack Debian 12/arm64 VM 中实际执行全部 Linux 测试二进制，普通 uid 501，systemd 252 用户 manager 正常；包含新日志协议、脱敏、取消、身份变化及布局/焦点测试。
- `scripts/e2e.sh lazyclash-e2e --proxy-mode environment`：完整 Mihomo/systemd E2E 通过（包含服务停止及恢复后的顶栏状态断言），仍由产品下载并验证官方 v1.19.32/v1.19.33；保留安装、更新、重装、配置、规则、连接及六种真实 PTY 场景。
- 新 Logs 实测：私有 fixture 配置显式启用 info 事件，经自有 loopback HTTP 流量产生真实 Mihomo TCP 日志；CLI 在条数/时间边界内输出日志，TUI 验证暂停、数字与 q 搜索、帮助层隔离、缩放、重连，以及停止/启动后收集包含新 source port 的真实事件。读取前后配置 SHA256 不变；没有启用系统 journal 或修改产品默认日志级别。
- `scripts/theme-pty.py`：macOS/arm64 和所选 Debian 12/arm64 的真实 controlling PTY，True Color、ANSI256、NO_COLOR 各覆盖 160×45、120×32、80×24、60×20、120×18，确认新布局不破坏网络草稿、焦点和返回。
- 现有 Python VT 协议四项测试、Python 语法检查、Bash 语法检查和差异空白检查通过。

证据单独保存到忽略目录 `.test-artifacts/ui-layout/`，包括 `linux-tests.log`、`e2e.log`、`pty-logs.txt`、`cleanup.txt` 和主机/Linux 颜色 PTY 记录。E2E 清理确认 dedicated unit 为 not-found、两个监听端口释放、`resources released=1`、exit=0；主机/Linux 测试与 PTY 暂存文件、进程已清理。没有读取 proxyctl 运行配置，没有修改 macOS 代理、DNS、路由或 Shell 文件。

日志行为与边界见 [实时日志查看](logs.md)。这些日志仅为当前会话实时观察，暂停或离页期间没有历史补采。未实际验证 Linux/amd64、其他发行版或其他 init 系统；没有把 macOS PTY 或交叉编译当作 macOS 服务支持证明。

## 运行端口设置（2026-10-29）

共享读取、预检查与事务应用用例，以及 TUI/CLI 操作见 [运行端口设置](runtime-settings.md)。普通测试使用临时目录、替代服务和本地 HTTP fixture；真实服务验证仍是显式入口。

本轮实际运行：

- 宿主 macOS/arm64、Go 1.27.1：最终源代码的 `make check`（全包测试与 vet）、`make race`、`make build` 通过。
- `scripts/verify-linux.sh lazyclash-e2e` 在最终源代码上重新构建并实际执行全部 Linux/arm64 测试，通过；包含普通 Start 的 UDP-only 冲突、占用默认端口安装提示、冲突后纠正和迟到消息不能提前退出清理等新增回归。
- `scripts/e2e.sh lazyclash-e2e --proxy-mode environment` 完整通过。在专用 OrbStack Debian 12/arm64、普通 uid 501、systemd 252 用户 manager 中，使用官方下载并校验的 Mihomo v1.19.32/v1.19.33；未读取已有代理配置，也未修改网络环境。
- 安装向导在自有 TCP/UDP fixture 占用默认端口时仍可安装，服务保持停止；明确 Start 因冲突失败，原 fixture 仍存活。
- CLI 分别验证 mixed TCP/UDP 和 controller TCP 冲突不修改文件或停止进程、dry-run、运行态重启确认、无变更不重启、运行态移动与自有端口互换、停止态保持停止。端口动态分配并记录供失败清理，不停止任何外部占用者。
- 端口变更以当前已应用 YAML 为基础：刻意修改已保存但未重新应用的 profile，证明配置其余字节、原始 profile、安装身份、API key 和 unit 保持不变；之后明确 Apply profile 仍保留新端口。实际请求验证新代理端口、新认证 API、`env` 输出和旧端口释放。
- `runtime-settings` 真实 controlling PTY 验证取消草稿、数字/q 输入、端口冲突后纠正、审阅不提前提交、明确 Apply & restart、120×32 / 80×24 / 60×20 / 120×18 / 120×40 缩放保留草稿，并在 Proxies、Rules、Logs、Overview 重新连接新端口；实际 HTTP 流量生成日志，未使用假数据。
- Python 四项 VT 回归、Python/Bash 语法检查通过。首次端口 PTY 运行暴露测试脚本的错误焦点假设：预检查失败已返回首个输入框，脚本仍发送 Shift+Tab；修复后增加新输入可见及错误清除的当前屏幕断言，完整端到端复跑通过。

证据在忽略的 `.test-artifacts/`：`e2e.log`、`pty-runtime-settings.txt`、`cleanup.txt`。最终清理为 `resources released=1`、`exit=0`，专用 unit 为 not-found，默认和动态 TCP/UDP 测试端口释放；额外检查专用测试目录及进程已消失。应用失败、取消、恢复失败和用户修改保护由可控模型/应用测试验证，未声称真实 VM 中注入过全部故障。实测平台未扩大到 Linux/amd64、其他发行版或其他 init。

完整 E2E 的二进制构建早于最后两项小修：将普通 Start 的端口检查统一扩展到 UDP、清除已结束预检查的底栏消息。两项都包含在随后最终源代码的 Linux 全包测试中；未因此重复声称又跑了一轮完整 E2E。

## 持久生命周期与权限边界（2026-10-29）

本轮实现范围为 Linux/systemd 用户服务/Mihomo 的普通本机代理，不包含特权 TUN 或系统级服务。设计和可重复步骤见 [持久运行](runtime-lifecycle.md) 与 [权限方案](permissions.md)。

最终源代码验证：

- 宿主 macOS/arm64：`make check`（全包测试及 vet）、`make race`、`make build` 全部通过；Python 四项 VT 回归、Python/Bash 语法检查与 `git diff --check` 通过。
- 复用已运行的专用 `lazyclash-e2e` Debian 12/arm64 VM：uid 501 普通用户，systemd 252，用户 manager running、Linger=yes。`verify-linux.sh lazyclash-e2e` 重新构建并实际执行全部 Linux 测试二进制，通过。
- `e2e.sh lazyclash-e2e --proxy-mode environment` 完整通过。使用官方 SHA256 校验的 Mihomo v1.19.32/v1.19.33；保留安装、更新、重装、订阅、配置、节点、连接、规则、日志、端口和真实 PTY 场景，增加记住来源后的 Refresh、不改变活动配置的刷新、停止态卸载和保留用户数据。
- 新 `lifecycle-e2e.py` 实测：真实 controlling PTY 发送 SIGHUP 后内核 PID 不变；模式通过动态 API 应用不重启；配置应用和显式重启恢复模式/节点；对已验证自有 PID 发送 SIGKILL 后 systemd 自动恢复，新 PID、恢复计数、模式与节点均实际读回；Enable/Disable 不改变当前 PID；明确 Stop 超过 RestartSec 后仍为 PID=0 和 inactive。
- Linux 文件 capabilities 用例因普通测试用户没有设置 `security.capability` 的权限明确 skip。setuid/setgid 拒绝路径实测通过，不能声称已对带真实 file capabilities 的二进制完成本轮运行验证。

真实 E2E 首轮暴露“API 可达但代理监听尚未准备好”的启动窗口，已在应用就绪检查增加 mixed TCP 监听探测并增加回归，最终端口互换与全套 E2E 通过。Mihomo 恢复时节点缓存加载可能晚于 API 首次可读，恢复验收在 15 秒内等待实际期望节点出现，未将首次快照中的默认选择直接视作持久化成功或缓存丢失。

记录保存在忽略目录 `.test-artifacts/`：`e2e.log`、`linux-lifecycle-tests.log`、各 `pty-*.txt` 和 `cleanup.txt`。最终清理确认专用 unit 为 not-found、默认及动态 TCP/UDP 监听均释放、`resources released=1`、`exit=0`。未修改主机系统代理、DNS、路由、Shell 配置或 VM 的 linger 策略。

未实测最后一个真实 SSH 登录会话退出、机器重启和 Linux/amd64 运行；单元测试、PTY SIGHUP、linger 检查和交叉编译都不能替代这些验收。文档提供专用机器上的 SSH/logout/boot 步骤。TUN、路由/DNS 接管、系统级部署及其停止清理尚未实现，不存在“模拟测试通过所以真实 TUN 已通过”的结论。

## 顶部通知条（2026-10-31）

- 宿主 macOS/arm64：`make check`、`make race`、`make build` 和四项 Python VT 回归通过。模型测试覆盖新旧通知计时隔离、同文案重复反馈、任务取消、弹窗去重、表单校验取消、Esc 优先级与关闭后的焦点恢复。
- `scripts/verify-linux.sh proxyctl-e2e`：先检查已有专用 Debian 12/arm64 VM 的普通用户、systemd 用户 manager 和 Linger 状态，再在 VM 内实际执行全部包测试，通过。
- `scripts/theme-pty.py`：在隔离数据目录和随机服务名下，使用真实 controlling PTY 验证错误超过五秒仍保留、鼠标/Esc 关闭、相同错误再次出现、输入取消提示自动收起，以及既有向导和隐藏输入。真彩、256 色和 NO_COLOR 三种模式均覆盖 160×45、120×32、80×24、60×20、120×18；当前屏幕记录保存在忽略目录 `.test-artifacts/notifications-pty/`。
- 通知模型另覆盖中文长文案、三行裁切及 20×7、10×4、1×1 极小窗口；`scripts/theme-preview.py` 导出的宽窄通知 PNG 用于检查字体排版，属于模型预览。

本轮未重跑真实 Mihomo 安装及 systemd 生命周期 E2E；PTY 使用未安装实例，不创建服务、不查询发布版本或修改系统网络。

## Toast 浮层（2026-10-31）

全局通知改为右上角 Toast，不再挤占正文高度。宿主 `make check`、`make build`、四项 Python VT 回归，以及 `scripts/verify-linux.sh proxyctl-e2e` 的 Linux 全包运行测试通过。新增回归验证正文几何、总行数和滚动位置稳定，覆盖区域不接收底层点击，订阅 URL 和搜索输入的避让，以及输入框部分滚出视口、短页面越界滚动裁切后的反馈。

更新后的 `scripts/theme-pty.py` 在真彩、256 色和 NO_COLOR 三种模式下通过 160×45、120×32、80×24、60×20、120×18 的真实终端验证，覆盖持久错误、键鼠关闭、重复反馈和自动收起；记录保存在 `.test-artifacts/toast-pty/`。宽窄 PNG 仍为模型预览。本次未重跑真实 Mihomo/systemd 生命周期 E2E。

## 多条 Toast 与底部空间回收（2026-11-01）

通知固定从页头右侧展开，宽屏最多两条、紧凑窗口最多一条；溢出进入有边界的列表，详情使用 Charm Bubbles viewport。正文回收原底部状态栏预留的一行，按键提示固定在终端最后一行，侧栏、紧凑导航遮挡区域和滚动边界同步。

本轮最终源代码验证：

- 宿主 macOS/arm64：`make check`（全部 Go 测试与 vet）、`make race`、`make build` 通过；Python 四项 VT 协议回归、脚本语法与 `git diff --check` 通过。
- 模型覆盖独立操作身份、同操作进度/结果/重试、相同错误不合并、按条目关闭、旧定时器隔离、成功 3 秒与信息 4 秒、隐藏/溢出/弹窗暂停计时、错误跨页保留、输入校验留在局部，以及通知列表、完整详情、键盘焦点和鼠标遮挡。受控故障测试覆盖恢复失败、最终状态回读失败时显示未知、弹窗关闭后保留严重错误，以及端口已提交但回读失败时仍保留已应用结果。
- 新底部布局回归覆盖 160×45、120×32、100×20、80×24、60×20、120×18、30×12、20×7，断言正文/滚动指示/鼠标命中到达倒数第二行、末行按键提示独立、侧栏填满正文高度、紧凑导航末行不能点击穿透；另检查 120×25 的侧栏边界。
- `scripts/verify-linux.sh proxyctl-e2e`：复用已有专用 Debian 12/arm64 VM，普通 uid 501、systemd 用户 manager running、Linger=yes；实际执行全部 Linux Go 测试通过，包含最终两项底部布局回归的全部尺寸。最终记录为 `.test-artifacts/notification-stack/footer-linux-tests.log`，此前通知集合验证为同目录 `linux-tests.log`。
- `scripts/theme-pty.py`：真彩、ANSI256、NO_COLOR × 160×45、120×32、80×24、60×20、120×18 的真实 controlling PTY 通过，覆盖独立同文案操作、顺序与溢出、Ctrl+N/鼠标展开、指定关闭、跨页保留、局部校验修改与离页、弹窗后计时及既有焦点和草稿行为。最终布局复跑逐一检查 15 份当前屏幕，确认 `q Quit` 固定末行，滚动标记位于倒数第二行。证据分别为 `.test-artifacts/notification-stack/pty/` 与 `footer-pty/`，隔离暂存资源已清理。
- 以最终实际 `Model.View()` 导出 ANSI，使用 Menlo 重新渲染并检查 120×32 的多条 Toast、局部输入校验和 60×20 的紧凑 Toast PNG。它们是模型排版预览，不作为真实服务运行证据。

本轮未重跑真实 Mihomo/systemd 生命周期 E2E；PTY 使用隔离的未安装实例，不安装内核或创建服务。恢复和状态回读故障来自受控模型测试，没有声称在 VM 内注入所有真实故障。实测平台范围未扩大。

## 侧栏 Logo 首版呼吸与点击彩蛋（2026-11-02）

初始状态、实际终端尺寸与色彩能力就绪约 200ms 后，常驻侧栏 Logo 以青色轻呼吸一次，约 800ms 内渐亮再恢复，不显示梦泡。每次启动仅一次，不因切页、缩放或关闭弹窗而重播；初始紧凑布局、加载失败、遮挡、无色及 16 色模式跳过。点击仍播放约 1.6 秒的原位变化与上方梦泡，字标宽度及菜单位置固定，20～23 行省略梦泡。没有适合此类品牌帧动画的 Bubbles 组件，使用封装的 Bubble Tea 定时消息；播放结束或被打断后不再安排动画消息。

本轮实际通过：

- 宿主 macOS/arm64：`make check`（全包测试与 vet）、`make build`、4 项 Python VT 协议回归及 Logo 验证脚本语法检查。
- 模型回归：首次尺寸、颜色和加载结果的六种到达顺序，200ms 等待与 800ms 呼吸、颜色升降、自然结束及各类跳过/打断后的不补播，等待和呼吸中的点击抢占、过期帧丢弃；另保留点击剧情的固定字标与梦泡坐标、总时长、重复点击重播、宽窄缩放，以及忙碌、退出、帮助、表单、通知列表和操作菜单打断。Logo 可点击但不增加 Tab 停靠点。
- `scripts/verify-linux.sh lazyclash-e2e`：在已有专用 Debian 12/arm64 VM 内实际执行全部可运行的 Go 测试，未启用需显式环境变量的真实 Mihomo 集成测试。
- `scripts/logo-demo.py`：macOS/arm64 与所选 Linux/arm64 的真实 controlling PTY 均验证 160×45、120×24、100×20，分别捕获 14、14、7 帧；检查 `la` 与 `yClash` 的字符格位置不变、没有尾部追加文字、梦泡逐格移动及最终恢复静态。
- `scripts/logo-intro-pty.py`：最终实现分别在 macOS/arm64 与所选 Linux/arm64 真实 controlling PTY 通过 160×45、120×24、100×20；实际采样 SGR 前景色，观察到基色与 20 帧变化，约 213～231ms 首次变亮、约 979～1021ms 恢复基色，峰值为 `#b7e5f7`，字符始终不变且没有进场梦泡。切页、缩放、关闭帮助后不重播，随后点击剧情完整通过；另验证 80×24 启动的静态标题 Logo 扩大到 160×45 后不补播。

可重复的宿主 PTY 入口：

```sh
make build
python3 scripts/logo-demo.py --binary bin/lazyclash \
  --output .test-artifacts/logo-wake-dream-pty \
  --size 160x45 --size 120x24 --size 100x20
python3 scripts/logo-intro-pty.py --binary bin/lazyclash \
  --output .test-artifacts/logo-intro/macos
```

最终进场验证的主机检查与 Linux 全包测试日志保存在 `.test-artifacts/logo-intro/`，其中 `macos/` 与 `linux/` 保存各尺寸的颜色观察、当前屏幕和原始 ANSI；每次脚本运行生成独立目录。此前点击剧情记录保留在 `.test-artifacts/logo-wake-dream/` 与 `.test-artifacts/logo-wake-dream-pty/`。PTY 使用隔离的未安装数据目录，VM 临时目录已清理；本轮未重跑 Mihomo/systemd 安装生命周期 E2E。

## Logo 原位翻身进场（2026-11-02）

进场在界面就绪约 200ms 后播放一次约 800ms 的“翻个身，接着睡”：原 emoji 的两格依次显示 `z `、` Z`、💤，两侧文字固定，同时青色渐亮至 `#c5efff` 再恢复。使用 40ms 帧间隔，总时长保持 800ms；进场不冒梦泡，点击仍播放完整的 1.6 秒惊醒与梦泡剧情。首次显示条件、取消与只播放一次的规则沿用首版。

本轮实际通过：

- 宿主 `make check`、`make build`、4 项 Python VT 协议回归及脚本语法检查。
- 模型验证完整的进场字形序列、中心两格与字标宽度固定、无进场梦泡、青色升降及恢复；保留初始化乱序、首次跳过、取消、点击抢占、旧消息隔离和点击剧情回归。
- `scripts/verify-linux.sh lazyclash-e2e`：在已有专用 Debian/arm64 VM 内实际执行全部可运行的 Go 测试，包含更新后的进场与生命周期回归。
- 更新后的 `scripts/logo-intro-pty.py` 分别在 macOS/arm64 和 Linux/arm64 的真实 controlling PTY 验证 160×45、120×24、100×20。各尺寸均捕获 `💤 → z → Z → 💤`，实际青色峰值为 `#c5efff`，随后恢复 `#85b3e0`；没有进场梦泡或字标偏移。切页、缩放与关闭帮助后不重播，原点击剧情完整通过；80×24 启动后扩大到 160×45 不补播。

记录位于 `.test-artifacts/logo-sleepy-entry/`：主机检查和 Linux 全包测试日志，以及 `macos/`、`linux-pty/` 下逐尺寸的中心字形、颜色、当前屏幕和原始 ANSI。仍可通过上一节的 `scripts/logo-intro-pty.py` 命令复验，使用新的输出目录即可。PTY 使用隔离的未安装实例，VM 暂存目录已清理；本轮未重跑真实 Mihomo/systemd 安装生命周期 E2E。

## 首次安装入口（2026-11-02）

未安装时，首页只突出安装 Mihomo，默认聚焦主按钮，按一次 Enter 打开现有安装向导；启动 TUI 不自动下载或安装。“稍后设置”只跳过当前会话的准备页，不写入永久关闭引导偏好。安装后的内置默认配置属于正常可用状态，可由用户明确启动；订阅和本地 YAML 均为可选能力，不构成必经步骤。

本轮实际验证：

- 宿主 macOS/arm64：最终源代码的 `make check`（全部 Go 测试及 vet）通过；本轮较早版本的 `make race` 通过。新增模型测试覆盖初始化消息乱序、一次 Enter 仅打开向导、迟到结果不抢焦点、会话跳过、无订阅默认配置就绪与明确启动，以及安装完成后保持停止。
- `scripts/verify-linux.sh lazyclash-e2e`：在已有专用 Debian/arm64 VM 内实际执行最终源代码的 Linux 全包测试，通过；不是仅做交叉编译。
- `scripts/onboarding-pty.py`：宿主中英文 × True Color、ANSI256、NO_COLOR × 160×45、120×32、80×24、60×20、120×18 的真实 controlling PTY 通过；Linux 中英文 True Color 也通过上述五种尺寸。最终界面另在中英文 True Color 下复验 160×45、120×32、80×24、60×20，检查首屏、安装向导入口、临时跳过和配置页。PNG 根据实际 PTY 字符格和颜色重绘，用于排版检查，不是原生 Ghostty 截图。
- `scripts/pty-smoke.py` 已同步现有页脚、单来源导入、默认配置名称和导航交互，并修正等待初始快照的竞态；E2E 的预导入检查按配置内部 ID 验证，不再把自动生成的显示名当作 ID。独立 Linux `empty` 模式通过，保留凭据隐藏、预安装导入、缩放、Esc 取消和 SIGTERM 清理场景。

最新完整 `e2e.sh lazyclash-e2e --proxy-mode environment` 尝试通过了 `empty` PTY，但在官方 Mihomo 发布元数据查询阶段因网络连接失败中止；后续只读直连 `curl` 诊断返回 HTTP 403。本轮没有完成该版本的真实安装、启动及后续 systemd 生命周期验证，不把模型测试和入口 PTY 视为这些场景的通过证据。清理记录为 `resources released=1`、`exit=1`，保留了本次 E2E 失败状态。

记录保存在忽略目录 `.test-artifacts/onboarding/`，包括最终宿主与 Linux 检查日志、较早的 race 日志、中英文各颜色/尺寸的当前屏幕与 ANSI、重绘 PNG，以及 E2E 输出和清理记录。验证使用隔离数据目录和随机服务名，临时资源已清理；实测平台范围未扩大。
