# 验证入口

## Proxies 三列卡片与悬浮响应（2026-11-04）

节点网格按可用宽度分为一至三列，少量成员及搜索后剩余一、两个成员时继续保留列宽，不拉伸到整行。组头与首排卡片空一行；卡片的两行内容上下各加一行，右侧留两格，选中竖条贯穿四行。留白属于卡片的点击区域，引用入口仍独立；方向键按节点身份去重，避免卡片拆分的命中区域被误当成相邻节点。

普通代理页鼠标移动只更新悬浮：同一命中区不重复布局，切换命中区跳过无关的数据协调与焦点修复。弹窗和菜单保留原有事件流程。没有新增缓存或悬浮延时。

本轮实际通过：

- 最终源码宿主 macOS/arm64 `make check build`（全包 Go 测试、vet、产品构建），TUI 测试用时 22.586 秒；整个 `TestProxies` 与 `TestProxyHover` 系列的定向 race 通过，用时 18.789 秒。本轮未重跑全包 race。
- `./scripts/verify-linux.sh lazyclash-e2e` 对最终源码构建 11 个包，在 Debian 12/arm64、systemd 252 VM 内实际执行九个可运行测试包，通过。普通用户 uid 501，user manager 为 running，Linger=yes；`/tmp/lazyclash-linux.*` 前后均为空，没有私有测试目录残留。
- 模型回归覆盖最多三列、少量和筛选后单个成员不拉伸、四行选中竖条与左右内容对齐、卡片全部字符格的独立命中、上下留白的真实鼠标选择、标题间隔不捕获操作，以及部分可见 padding 仍计入成员范围。原有多列导航、引用往返、选择确认与取消、测速和过期结果保护继续通过。
- 悬浮回归确认普通节点、引用入口、侧栏与空白处的屏幕及命中区与完整布局一致，不改变焦点、滚动或选择，不启动后端命令；排序菜单继续响应移动。可复现本地基准包含 `Update` 与 `View`，每项 20 次，Apple M4 Pro：48 个节点由原完整流程的 16.17 ms 降为跨卡 7.23 ms、同卡 1.24 ms；300 个节点分别为 76.39、27.46、2.24 ms。对照保持相同布局与鼠标事件，仅比较事件处理路径，不代表 SSH 或终端显示延迟。
- 八项 Python 终端解析回归通过。
- 最终 `scripts/proxies-pty.py` 在真实 controlling PTY 完成中英文 × True Color、ANSI256、NO_COLOR 六组，各组记录 `selections=3`、`modes=3`、`tests=12`、`canceled=2`、`max_tests=4`，最终 `PROXY=HK 01`。覆盖 160×45、200×40、240×40、120×32、80×24、60×20、40×16、120×18，200 与 240 列均保持三列；验证标题间隔、上下 padding 点击选择与四行竖条、两成员和搜索后单成员保留列宽、引用入口、排序稳定、测速完成提示收起及连续滚动。已目视检查中文 200×40、240×40 的单卡及双卡、80×24 效果。PTY 使用内存后端，PNG 按实际 PTY 字符格与颜色重绘，不是原生终端截图，也不代表真实 Mihomo API 或远端节点测速。

证据保存在忽略目录 `.test-artifacts/proxies-card-spacing/`：`host-check-build.log`、`host-proxies-hover-race.log`、`hover-benchmark.log`、`navigation-fix.log`、`python-tests.log`、`linux-tests.log`、`linux-environment.txt`、`linux-cleanup.json`、`pty.log` 与 `pty/`。修复方向键回归前的完整测试及 Linux 失败记录独立保存在 `first-pass/`，早期 PTY 记录及脚本断言修正记录分别保存在 `pre-horizontal-fix/` 和 `script-*/`，最终源码与脚本已重新通过上述验证。本轮未访问真实节点或重跑 Mihomo/systemd/TUN E2E，也未尝试下载内核；Linux 实际运行平台为 arm64，未在 amd64 运行。

## Proxies 留白分组与展开信息精简（2026-11-04）

策略组之间留一行空白并去掉横向分隔线；组头显示最终节点、必要的「自动」标记和右对齐数量。展开后直接展示两行卡片，不重复当前选择与默认排序；非默认排序留在组头，引用卡片统一显示「策略组 · 最终节点」。测速进度只在实际有测试项时出现，完成或用户取消后保留两秒提示再收起，离页取消不延续提示。节点选择、独立引用入口、宽窄浏览和连续滚动范围沿用已有交互。

本轮实际通过：

- 最终源码的宿主 macOS/arm64 `make check build`（全部 Go 测试、vet 和产品构建），以及八项 Python 终端解析回归通过。最终 TUI 普通测试用时 22.053 秒。
- 本轮全包 `make race` 通过，TUI 用时 208.406 秒。最后一处排序焦点修复后，对最终源码另运行整个 `TestProxies` 系列的定向 race，通过，用时 15.353 秒；全包 race 与最后修复后的定向验证分别保留日志。
- `./scripts/verify-linux.sh lazyclash-e2e` 对最终源码构建 11 个包，在已有专用 Debian 12/arm64、systemd 252 VM 内实际执行全部九个可运行测试包，通过。普通用户 uid 501，user manager 为 running，Linger=yes；运行前后核对 `/tmp/lazyclash-linux.*`，没有本轮私有目录残留。
- 模型回归覆盖 160、80、40 列的组间留白不捕获选择、非默认排序与测速命中区、摘要精简但详情保留完整引用路径，以及完成和取消提示的到期、旧到期消息不影响新测速。最后补充鼠标事件回归：从非首组的排序入口恢复默认后，焦点与滚动位置仍返回该组，不跳到首组。既有多列导航、宽窄引用往返、自动组只读、选择确认、取消与迟到结果回归继续通过。
- 最终 `scripts/proxies-pty.py` 在真实 controlling PTY 完成中英文 × True Color、ANSI256、NO_COLOR 六组，每组记录 `selections=1`、`modes=3`、`tests=10`、`canceled=4`、`max_tests=4`，最终 `PROXY=HK 01`。覆盖 160×45、200×40、240×40、120×32、80×24、60×20、40×16、120×18，其中 200、240 列分别展示四列、五列卡片；继续验证连续滚动范围。新增检查确认测速完成提示自动收起后，卡片延迟结果与当前排序仍保留；已目视检查 200、160、80 列效果。PTY 使用明确的内存后端，PNG 按实际 PTY 字符格与颜色重绘，不是原生终端截图，也不表示真实 Mihomo API 或远端节点测速通过。
- `scripts/pty-smoke.py` 的显示断言同步为选中竖条与内置项 `—`，保留真实 API 回读和刷新 PID 断言。脚本语法、上述六组已捕获 PTY 字符屏幕及 51 个选择正反例的离线检查通过；这一步没有启动子 PTY、网络、Mihomo 或服务，不是重跑真实 E2E。修改脚本后八项 Python 回归再次通过。

本轮证据保存在忽略目录 `.test-artifacts/proxies-whitespace/`：`host-check-build.log`、`host-race.log`、`host-proxies-race-final.log`、`python-tests.log`、`linux-tests.log`、`linux-environment.txt`、`linux-cleanup.json`、`pty.log`、`pty-smoke-display-check.log` 和 `pty/`。最终焦点修复前的 check/build、Linux 与 PTY 记录另保存在 `first-pass/`。本轮未重跑真实 Mihomo/systemd 或 system scope/TUN 专项，也没有重新尝试官方内核下载；之前的真实运行与下载网络失败仍是独立历史记录，不能视为本轮源码的重新验收。Linux 实际运行平台仍为 arm64，未在 amd64 运行。

## Proxies 全宽布局与引用组就地浏览（2026-11-04）

代理页使用全部可用正文宽度，按实际空间和成员数量分配节点列数；卡片之间保留紧凑间隔，选中竖条与键盘焦点独立。宽屏模式与搜索同行，数量徽标右对齐，测速移入展开组标题；引用卡片提供独立入口。多列方向键先访问同排相邻成员，宽屏引用跳转保留组列表并将目标组标题定位到视口上部，返回恢复原搜索、焦点和滚动位置；窄屏继续采用两级浏览。成员连续滚动，可见范围保留在按键提示上方，不使用分页。

本轮实际通过：

- 最终源码的宿主 macOS/arm64 `make check build`（全部 Go 测试、vet 和产品构建）、全包 `make race`，以及八项 Python 终端解析回归。最终 TUI race 用时 209.219 秒，通过。
- `./scripts/verify-linux.sh lazyclash-e2e` 在已有专用 Debian 12/arm64、systemd 252 VM 内实际执行全部九个可运行测试包，通过。随后增加引用跳转锚点与切换等待期间的交互保护，最终源码另在同一 VM 重新执行整个 TUI 测试包，通过。两批日志分别保留，不将修复前的全包运行描述为最终源码的全包重跑。VM 普通用户 uid 501，user manager 为 running，Linger=yes；私有测试暂存目录已清理。
- 最终源码的模型回归覆盖三列、四列与不完整末行导航、少量成员铺满可用行、左右相邻成员优先、引用入口与选择互不混触、远处目标组定位和返回恢复、实时成员重排、模式切换、busy 期间禁用入口、右侧徽标与独立测速命中区，以及裁切到部分卡片时的可见范围和固定底部布局。
- 最终 `scripts/proxies-pty.py` 在真实 controlling PTY 完成中英文 × True Color、ANSI256、NO_COLOR 六组。每组实际记录 `selections=1`、`modes=3`、`tests=10`、`canceled=4`、`max_tests=4`，最终 `PROXY=HK 01`。覆盖独立引用入口与键盘往返、选择确认、自动组只读、排序稳定、取消与迟到结果、空组和负载均衡，以及 160×45、200×40、240×40、120×32、80×24、60×20、40×16、120×18 缩放；80×24 另以 PgDn 验证连续滚动后显示 `4–8 / 8`。该入口明确使用内存后端；PNG 按实际 PTY 字符格和颜色重绘，用于布局检查，不是原生终端截图，也不代表真实 Mihomo API 或远端节点网络测试通过。

真实网络验收未完成，以下三次尝试均保留失败状态：

1. 首次 `./scripts/e2e.sh lazyclash-e2e` 使用 direct 路线，通过 empty PTY 后，真实 download-network PTY 的官方版本查询报 `network connection`，未进入安装阶段。
2. 对同一官方版本地址进行独立 direct HEAD 查询返回 HTTP 200 后，沿同一路线进行一次完整 E2E 重试。此次通过 empty 与 download-network PTY、未安装的只读版本查询和 v1.19.32 pinned dry-run，但实际 release 文件下载在 `0/20809592` 字节时报 `network connection`，尚未部署内核或进入 Proxies。
3. 单独使用生产安装下载器，以明确的 environment 路线尝试获取官方 v1.19.33，供后续隔离的真实 Proxies 专项使用；release 文件连接探测同样报 `network connection`，未完成获取。既有 CONNECT observer 记录 `github=1`、`connected=1`，`rejected_api=0`、`other=0`、`upstream_failures=0`；这只证明经过明确选择的代理隧道，不能证明隧道后的 TLS 和下载已完成。全程保留 TLS 证书校验与生产下载大小限制，没有降低检查或在产品中增加路线回退。此次专项没有运行到真实 Proxies API/PTY。

两次完整尝试和单独获取尝试的清理均为 `resources released=1`、`exit=1`；没有遗留本轮专用服务，默认与 audit TCP/UDP 端口可重新绑定，VM 私有暂存目录已清理。本轮没有真实 Proxies API/PTY 运行通过的记录，也未重跑 system scope/TUN 专项；下方 2026-11-03 的完整 E2E 成功属于前一轮的独立历史证据。实际 Linux 运行平台仍为 arm64，未在 amd64 运行。

本轮脱敏证据位于忽略目录 `.test-artifacts/proxies-adaptive/`：`host-check-build.log`、`host-race.log`、`python-tests.log`、`linux-tests-pre-anchor.log`、`linux-tui-final.log`、`network-diagnostic.txt`，以及 `pty/`。两次完整尝试分别保存在 `first-attempt/`、`second-attempt/`，各自包含 E2E 日志、实际到达阶段的 PTY 记录和清理结果；`targeted-runtime/` 保存单独 environment 获取的生产下载日志、来源类别计数与清理记录。

## Proxies 分组与节点浏览重设计（2026-11-03）

代理页改为折叠组摘要与双列成员；窄屏使用组列表、成员列表两级浏览。选择确认与键盘焦点分别显示，自动策略组只供查看；搜索、排序、引用组往返、延迟测试与取消均保留稳定焦点和过期结果保护。引用组不复用核心的历史延迟，只显示绑定当前解析路径的本轮测试结果；叶子节点仍可使用核心历史。

本轮实际通过：

- 宿主 macOS/arm64 的 `make check`、`make build`、全包 `make race` 与完整 TUI/i18n race。最终引用组历史保护后重新通过 `make check build` 与 Proxies 定向 race；八项 Python 终端解析回归及变更脚本语法检查通过。
- `./scripts/verify-linux.sh lazyclash-e2e` 在已有专用 Debian 12/arm64、systemd 252 VM 内实际执行全部九个可运行测试包。最终产品提交另在 VM 内重跑整个 TUI 测试包，通过，包含引用组路径变化后旧历史失效的回归；私有暂存目录已清理。
- `scripts/proxies-pty.py` 在真实 controlling PTY 完成中英文 × True Color/ANSI256/NO_COLOR 六组，覆盖协议/UDP、双列键盘与鼠标选择、自动策略只读、组引用与搜索往返、排序、测试完成/取消/迟到结果、空组、负载均衡、模式切换，以及 160×45、120×32、80×24、60×20、40×16、120×18 缩放。该入口明确使用内存后端，调用报告与 ANSI/文本/PNG 位于 `.test-artifacts/proxies-redesign/pty/`；PNG 按实际 PTY 字符格与颜色重绘，不是原生终端截图，也不代表远端节点网络测试通过。
- `./scripts/e2e.sh lazyclash-e2e` 以 direct 下载方式完整通过真实 Mihomo/user systemd/PTY 验收。Proxies 使用真实内核的 DIRECT/REJECT，键盘和鼠标切换后均由 API 回读确认，`r` 刷新保持 PID；内置项显示 N/A。整轮还完成官方 v1.19.32 安装与 v1.19.33 升级、配置事务与回滚、规则、日志暂停/恢复、实时连接、端口修改及 SIGHUP/SIGKILL 恢复等既有场景。验证脚本同步新代理页交互与当前页面导航，日志停启状态改由实际服务 JSON 核对，同时保留捕获不可用、重连和新事件断言。

最终脱敏记录位于忽略目录 `.test-artifacts/proxies-redesign/`：`linux-tests.log`、`linux-tui-final.log`、`e2e.log`、`e2e-cleanup.txt` 及相关 `pty-*.txt`。完整 E2E 退出码为 0；随机专用 unit 已移除，默认与动态 TCP/UDP 监听均释放，清理记录为 `resources released=1`、`exit=0`。实际 Linux 运行平台为 arm64，未在 amd64 运行；本轮未重跑 system scope/TUN 专项，也不把内存后端延迟结果当作真实远端节点测速证据。

## systemd 属性查询与作用域诊断（2026-11-03）

管理器预检、服务名冲突检查和 linger 查询统一读取 `属性名=值`，移除旧版 systemctl/loginctl 不支持的 `--value`。可读的 `SystemState=degraded` 与 `running` 均表示管理器可访问；不要求清理全机失败服务。只有明确的 `LoadState=not-found` 才允许发布新 unit，查询失败、缺失属性、已有 vendor unit 或 masked unit 仍阻止安装。用户管理器失败保留会话与 `XDG_RUNTIME_DIR` 提示，系统管理器失败使用独立的中英文诊断。

核对了上游 systemd **v219** 的 [systemctl 属性选项与输出实现](https://github.com/systemd/systemd/blob/v219/src/systemctl/systemctl.c)、[Manager 的 SystemState 属性](https://github.com/systemd/systemd/blob/v219/src/core/dbus-manager.c) 和 [logind 用户的 Linger 属性](https://github.com/systemd/systemd/blob/v219/src/login/logind-user-dbus.c)。该版本支持所需属性及带名称的输出；systemctl 的多属性逗号列表也已存在。此核对不等同于在 v219 系统运行产品，也不代表当前生成的 unit、安全沙箱、capabilities 和完整 Mihomo 生命周期已适配 v219。

本轮实际通过：

- 宿主 `make check build`（全部 Go 测试、vet 和产品构建）、`go test -race ./internal/service ./internal/app ./internal/i18n`。
- 回归 fixture 在修改前复现 `--value` 导致的预检失败及 root 误报用户管理器；修改后覆盖 user/system 查询、running/degraded/starting 状态、带名属性解析、空/错属性、服务冲突、失败时不发布 unit、linger 的明确 yes/no、取消与错误链，以及中英文诊断。
- `./scripts/verify-linux.sh lazyclash-e2e` 在已有专用 Debian 12/arm64、systemd 252 VM 内实际执行全部可运行的 Linux Go 测试，通过。
- 显式启用 `LAZYCLASH_TEST_SYSTEMD=1`，分别以该 VM 的普通用户和 root 执行 `TestSystemdManagerIntegration`。真实 user/system 管理器预检、随机未占用服务名、未安装状态回读和 scope/linger 处理均通过；user 实际为 Linger=yes，system 不查询 linger。没有安装 unit、启停服务或改变 linger，测试二进制的私有暂存目录已清理。

证据保存在忽略目录 `.test-artifacts/systemd-properties/`：`before-fix.log`、`service-i18n.log`、`check-build.log`、`race.log`、`linux-tests.log`、`live-managers.log` 和最终 user/root 定向复验的 `linux-properties-final.log`；`upstream-v219/` 为只读获取的上述上游资源。普通测试默认跳过真实管理器检查；可在专用 Linux 测试机器上显式运行：

```sh
LAZYCLASH_TEST_SYSTEMD=1 go test ./internal/service -run '^TestSystemdManagerIntegration$' -count=1 -v
```

本轮未访问用户的服务器，没有真实 systemd 219 环境，也未重跑 Mihomo 安装/启动或 TUN 完整端到端测试。修复范围是已复现的命令选项、属性解析和作用域诊断，不能据此宣称整体兼容 systemd 219。

## master 合并验证（2026-11-03）

合并 `codex/service-scopes` 的四个提交，同时保留 master 的 API-free 下载实现及下载审计。修正两者的执行边界：安装用例把专用服务身份传入下载阶段，解压后的系统内核保持 root 所有、服务组只读和可执行，再以该非 root 身份运行版本检查；普通用户下载仍为 0700。

合并结果通过 `make check`、`make build`、`go test -race ./internal/app ./internal/mihomo`、八项 Python 回归及 `./scripts/verify-linux.sh lazyclash-e2e`。另在 Linux 以 root 实际执行八项身份、路径和文件权限测试，其中新增下载回归检查真实子进程 UID/GID、空附加组、最小环境、无法改写自身、最终文件归属及临时文件清理。`make package VERSION=dev` 重新生成两种 Linux 架构的归档并校验 SHA256SUMS。

用于重跑真实内核端到端的旧临时 Mihomo 文件已不存在；本轮获取官方 v1.19.33 测试文件时遇到 GitHub HTTPS TLS 连接错误，因此未重跑真实 Mihomo/systemd/TUN 完整端到端。下方两批实现各自的真实运行结果及限制分别保留，不能作为合并后重跑成功的记录。本轮 Linux 实测仍为 arm64，amd64 仅构建和归档验证。

## 双作用域、root TUN 与发行打包（2026-11-03）

本节记录服务 scope 分支的运行时验收结果；后续章节分别保留各批实现及验证范围。

本轮按首发范围实现运行时代码：root 使用独立 system service，普通用户各自使用 user service；`sudo lazyclash` 忽略原用户继承的实例环境。system 内核由专用非 root 账号运行，普通代理无网络 capabilities，明确授权 host TUN 后才授予 `CAP_NET_ADMIN CAP_NET_RAW`。不增加 daemon、提权 helper、普通用户 TUN 或旧安装兼容层。

实际通过的验证：

- macOS/arm64、Go 1.27.1：`make check`、完整 `make race`、`make build`、四项 Python 终端解析回归、Shell/Python 脚本语法及差异空白检查。
- `./scripts/verify-linux.sh lazyclash-e2e`：在已有专用 Debian 12/arm64、systemd 252 VM 内实际执行最终源码的全部可运行 Go 测试。另以 root 实际执行七项独立 scope 默认值、目录/文件归属、sandbox 路径、稳定清理身份和 Mihomo 降权回归；验证子进程 UID/GID 与补充组确实已下降。
- 第一批 scope 提交独立导出暂存源码，完成 `make check`、Linux 全包运行及真实 root/system、普通用户/user 的 proxy-only 验收；第二批 TUN 提交也从独立暂存树通过 check，避免依赖后续工作区文件。
- `make package VERSION=dev` 生成 Linux/amd64、Linux/arm64 归档与 SHA256SUMS，显式使用 amd64 v1 / arm64 v8.0。校验归档摘要和 ELF 架构，解包后的 arm64 产品用于下述完整实测；没有用 `go run` 或内存后端代替。
- root/system 与普通用户/user 实际安装、并发普通代理、TCP/UDP 端口冲突拒绝、独立数据/服务、root 核心非零 UID、普通代理 CapEff=0、`sudo` 继承用户环境不接管用户实例、安装后未自动启动及正常卸载。
- CLI 缺少 host 范围确认时拒绝启用 TUN；普通用户没有提权流程。已有外来 TUN 接口时拒绝启用；另一个 root 实例占用时错误包含占用实例，原服务不被停止。
- TUN 通过受控本地 HTTP CONNECT fixture 实际处理 `203.0.113.77:80` 流量，并将发往 `203.0.113.53:53` 的 DNS 查询劫持至受控 DNS fixture，验证响应与代理请求计数。TUN 期间专用内核只有 NET_ADMIN/NET_RAW，两种规则族、路由表和接口均核对。
- TUN 开启时重新应用 profile 保持策略；SIGKILL 内核后退出钩子清理，systemd 自动重启并再次达到真实 TUN 就绪；当前配置故意损坏时 Stop 报检查错误，但内核、接口、双栈规则/路由及 claim 仍可靠释放。关闭 TUN 后再次启动，网络 capabilities 归零；显式停止后由另一个 root 实例取得管理权。
- root 产品实际 controlling PTY 在 120×36、80×24 显示完整整机流量与 DNS 影响确认；默认 Enter 取消且网络不变。120×36 另实际确认开启 TUN，再经 CLI 关闭并核对资源释放。模型补充中英文 160×45、80×24、60×20、120×18、20×7，极小窗口无法完整阅读时禁止确认。

实测发现并修复了 iproute2 自定义路由表的字符串 JSON、设备消失后的 `iif_detached` 标记，以及 Mihomo 已创建网络设备但 TUN 栈尚未初始化完成的窗口。unit 就绪钩子与 app 就绪均需 API 确认 TUN 实际开启；单凭接口或 `systemd active` 不判定成功。清理使用独立受保护 instance-id，不依赖损坏的 current 配置；不删除无法确认归属的规则、路由或接口。

此 OrbStack VM 为所有 system service 注入会弱化 sandbox 的全局 drop-in，且 LXC generator 会在 daemon-reload 时重建它。测试 fixture 在专用 VM 临时保存这两份文件，结束后按原始 SHA256 恢复并 reload，没有重启其他服务。产品仍拒绝额外 drop-in，没有为测试环境放宽 unit 所有权或权限校验。各轮创建的专用 unit、账号、安装目录、网络 fixture 和 TUN 管理记录已清理。

本轮也显式执行既有 `./scripts/e2e.sh lazyclash-e2e`；默认 9097/7897 端口被该 VM 其他已有 Mihomo 实例占用，脚本按预期在资源创建前退出，没有停止已有进程。完整运行验收由新入口使用独立动态端口完成；本轮不将旧入口报告为通过。Mihomo 使用明确选择的本地 v1.19.33 可执行文件副本，不读取旧安装的订阅或凭据，也不把这次本地安装验收描述为官方下载安装链路验证。

入口：

```sh
make package VERSION=dev
# 解包对应 VM 架构的归档后；第三个参数是 VM 内明确选择的 Mihomo 可执行文件
./scripts/service-scopes-e2e.sh VM /absolute/packaged/lazyclash /absolute/vm/mihomo
./scripts/service-scopes-e2e.sh VM /absolute/packaged/lazyclash /absolute/vm/mihomo --proxy-only
./scripts/service-scopes-e2e.sh VM /absolute/packaged/lazyclash /absolute/vm/mihomo --ui-tun-only
```

脱敏证据位于忽略目录 `.test-artifacts/`：`scopes-check.log`、`scopes-race.log`、`scopes-linux.log`、`scopes-root-tests.log`、`package-verify.log`、`scopes-index-check.log`、`scopes-index-linux.log`、`tun-index-check.log`、`service-scopes-e2e.log`、`service-scopes-proxy-e2e.log`、`service-scopes-ui-e2e.log` 与 `pty-root-tun-{120x36,80x24}.txt`。

实际运行平台为 Linux/arm64；amd64 完成打包构建与归档验证，尚未在 amd64 机器运行。本轮没有重启 VM、注销最后一个真实 SSH 会话、同时运行 Alice/Bob 两个普通 UID，也未完成 IPv6 端到端流量测试；双栈规则/路由核对不等同于这些场景通过。自动交接、UID 流量筛选、普通用户提权与系统 resolver 改写不在首发范围。


普通构建/单元测试不会调用 OrbStack、安装服务或修改系统配置。以下三类运行验证均为显式入口，不由 `go test` 自动触发。

## 移除 GitHub Release API 依赖（2026-11-03）

安装、重装、指定版本、升级和下载方式连接测试共用 API-free 资源解析：最新版本来自既有 `version.txt` 检查，稳定标签与 CPU 能力规则生成固定的官方 Release 文件地址。最多读取 4 KiB 探测候选，只有 HTTP 404/410 会尝试兼容构建；所有请求遵循用户所选的 direct/environment/accelerated 路线。没有访问 `api.github.com` 的生产回退。

当前校验模型为响应状态与长度、64 MiB 压缩包上限、128 MiB 解压文件上限、完整 gzip/CRC、0700 权限与同步、执行 `-v` 精确版本，以及应用层配置验证、事务和回滚。HTTPS 证书验证保持开启；HTTP 加速前缀没有该段 TLS 保护。不存在上游预期摘要或独立签名验证，本地安装记录摘要用于后续变化和恢复身份检测。下方 2026-11-02 及之前的摘要/API 记录描述当时实现，不能视为当前校验承诺。

本轮已实际验证：

- 宿主 macOS/arm64：最终源码的 `make check`（全包测试及 vet）、全包 `make race`、`make build` 通过。独立导出的实现暂存树另通过 `go test -mod=readonly ./...`，不依赖未暂存的验证脚本。
- HTTP fixture 覆盖稳定版本文本与固定标签资源链、amd64 兼容候选、仅 404/410 降级、有限 Range/gzip 头/响应长度、未知总量、完整 gzip 损坏与截断、精确执行版本、取消和失败清理。CLI 实际使用生产 Resolver/下载器，以本地 HTTP 和 gzip 可执行文件 fixture 验证 latest dry-run 不完整下载，指定版本只安装一次且服务保持停止；应用层保留配置、事务和回滚回归。
- `scripts/verify-linux.sh lazyclash-e2e` 在已有专用 Debian 12/arm64 VM 内实际执行全部可运行的 Linux Go 测试，通过；不是仅交叉编译。普通 uid 501，systemd 用户 manager running，Linger=yes。
- `scripts/onboarding-pty.py --test-binary` 在真实 controlling PTY 完成中英文 × 120×32、60×20 × 更多选项/成功共八组，核对新连接测试名称、诚实的校验说明及成功后 `starts=0`。使用明确启用的内存后端；PNG 是实际 PTY 字符格与颜色重绘，不是 Ghostty 原生截图，也不替代真实下载安装验证。
- Python 八项回归、全部 Python 脚本语法、E2E Shell 语法及差异空白检查通过。新增 CONNECT observer 的本地协议测试验证不改写隧道负载、上游认证、API 拒绝、失败无回退、错误脱敏及关闭后的端口释放。

真实网络验收使用同一显式 environment 上游，由仅记录类别的 CONNECT observer 串联，保留端到端 TLS。首次完整 E2E 尝试已通过实际产品 empty/TUI 连接测试、未安装只读检查、指定版本 dry-run、官方 v1.19.32 下载与安装、运行中手动换为 v1.19.33、同版本复用与重装、旧版运行时只读检查；手动换版保持旧 PID、实际 API 版本和配置。所有已执行远程阶段的审计均为 GitHub API 尝试零。

`./scripts/e2e.sh lazyclash-e2e --download-method environment` 三次均未整轮通过：分别在确认升级的版本文本查询、指定版本 dry-run 的文件探测、TUI 连接测试的文件探测遇到网络/TLS 中断。无 observer 的同一路线只读对照也复现 `SSLZeroReturnError`，不能将其归因为 GitHub API 限流或据此声称整套生命周期 E2E 已通过。没有自动重试、切换路线或忽略失败。首轮清理曾因 observer 的 TIME_WAIT 被误判为监听残留而报失败，修正其监听复用设置后，本地协议测试和后两轮清理均通过；首轮已另行核验自有 unit、进程和端口全部释放。

随后沿同一路线定向执行真实安装、启动、只读检查与确认升级。四阶段均首次成功，原始退出码保存在 `targeted-upgrade/attempts.jsonl`。检查保持旧 PID；确认升级由官方 v1.19.32 升至 v1.19.33，实际 PID 改变，安装版本与实际 API 运行版本一致，配置摘要不变，`core-pending.json` 已清除。运行会话中断后恢复检查现有实例并完成清理，没有重新下载或重新执行升级。审计共 12 个官方 CONNECT 隧道，API/其他目标/上游 CONNECT 失败均为零。补验及清理记录明确区分于原始四阶段执行：专用 unit 为 not-found、自有进程归零、默认端口和 observer 端口均可绑定，`resources released=1`、`exit=0`。

证据位于忽略目录 `.test-artifacts/api-free/`：`check.log`、`race.log`、`build.log`、`staged-code-test.log`、`staged-script-test.log`、`linux-tests.log` 和 `pty/20261103-110314-254fab/`；失败整轮保留在 `first-attempt/`、`second-attempt/` 与 `e2e-run.log`，路线对照为 `network-diagnostics.jsonl`，成功定向升级及恢复清理证据在 `targeted-upgrade/`。实测平台仍为 macOS/arm64 与专用 Debian/arm64；没有扩大 Linux/amd64、其他发行版或真实掉电恢复的运行验证范围。

## 轻量卡片与侧栏布局（2026-11-02）

概览保留轻量卡片：内核、路由模式、配置、代理组形成主区域，流量统计继续通栏展示，环境及已安装内核的信息仍为独立卡片。TUN 和系统代理说明收进环境，跳过安装后仅有一个普通安装入口；管理配置直接进入配置页。首次安装正文上限为 96 列并与完整页头左边界对齐。配置卡片减少重复元数据，摘要明确区分自定义数量与内置数量。侧栏宽 20 列，最终按反馈恢复标签本身居中，焦点箭头及低色当前页圆点独立占位，不挤偏文字。

本轮实际验证：

- 宿主 macOS/arm64：概览基线的 `make check`、全包 `make race`、`make build` 通过；删除未引用翻译键后另通过 i18n 测试。最终居中侧栏重新通过 `make check`、`make build` 及 Sidebar/MarianaSidebar/DashboardSidebar 定向 race。Python 四项 VT 解析回归、变更脚本语法及差异空白检查通过。
- 模型覆盖中英文、单列/双列、首次安装/跳过/已安装、按钮命中区与滚动裁切、稳定卡片 ID、环境焦点保持及显式进入/激活内核管理。安装区覆盖 216×55 至 20×7，断言正文限宽不缩短页头分隔线。
- 实际产品安装/概览 PTY 完成中英文 × 216×55、160×45、80×24 六组。首次安装区实际宽度分别为 96、96、76 列；验证稍后设置、唯一普通安装按钮、管理配置直达配置页且无导入框、返回时保留会话跳过，以及窄屏滚动到流量和环境卡片。
- 产品 `profiles-pty.py` 的 loopback HTTP fixture 完整通过创建、刷新、菜单、缩放、删除与退出重开持久化，HTTP 请求计数为 14。`i18n-pty.py` 通过语言切换、重开持久化、中文鼠标命中、帮助、网络草稿及 120×40 ↔ 80×24 缩放。
- `scripts/verify-linux.sh lazyclash-e2e` 在已有专用 Debian/arm64 VM 内实际运行全部可运行的 Go 测试包，通过；最终居中侧栏也重新通过同一入口。Linux 产品 `theme-pty.py` 在 True Color、ANSI256、NO_COLOR 下均通过 160×45、120×32、80×24、60×20、120×18，覆盖焦点、通知、网络草稿及缩放；私有暂存目录已清理。
- 侧栏缩至 20 列后，Logo 点击与进场 PTY 分别通过 160×45、120×24、100×20，保持字标坐标及动画时序；另验证 80×24 启动后扩大不补播。
- 最终居中侧栏的产品 PTY 通过中英文 × True Color/ANSI256/NO_COLOR 六组，每组包含 160×45、100×20、80×24 弹出菜单，逐格核对标签中心及上下切页后的坐标。最终中文 160×45、80×24 另复验安装/跳过/管理配置/返回链路并检查 PNG；模型补充 ANSI16 与窄至 4 列时的装饰裁切及边框保持。

记录在忽略目录 `.test-artifacts/light-cards/`、`.test-artifacts/layout-profiles-pty/`、`.test-artifacts/sidebar-layout/` 及最终居中的 `.test-artifacts/sidebar-centered/`。PNG 为实际 PTY 字符格重绘或 Go View 模型预览，不是原生 Ghostty 截图。PTY 使用隔离的未安装目录；本轮未重跑真实 Mihomo/systemd 生命周期 E2E，需明确核心路径的集成测试仍按条件跳过。

## 紧凑色块按钮（2026-11-02）

普通页面、页头、概览、安装区、向导、配置卡片与表单、帮助及通知共用按钮渲染。主操作、当前选中、焦点、危险确认、禁用和执行中使用独立表现；固定两列焦点位置，颜色能力切换不改变按钮宽度。配置删除默认取消，仍需名称匹配与明确确认。页头的全部更新在最后一个可更新配置消失后会修复焦点，旧动作也不能启动任务。

本轮实际验证：

- 宿主 macOS/arm64：最终 `make check`、`make build` 通过；全包 `make race` 通过，随后页头禁用保护的最终改动另通过 Button/HeaderButton/ProfilesHeader 的定向 race。Python 四项 VT 解析回归、全部 Python 脚本语法与差异空白检查通过。
- 模型回归覆盖中英文、True Color/ANSI256/ANSI16/无色、焦点与悬停的标签和命中区不移动、通知控件不重叠、忙碌防重复与取消清理禁用、配置删除默认取消及名称校验。删除按钮尺寸覆盖 120×32 至 24×7，其他控件保留既有极小窗口回归。
- 安装引导实际 controlling PTY 完成中英文 × True Color/ANSI256/NO_COLOR × 160×45、80×24、60×20、120×18 × 取消、成功、查询重试、更多选项共 96 组；内核更新另完成中英文 × 三种颜色能力共 6 组，每组包含 160×45、120×32、80×24、60×20、120×18、40×16 缩放。明确启用内存后端，核对调用计数、取消清理和成功后未启动，不实际安装内核或服务。
- 另在实际 PTY 完成 24 组中英文按钮外观检查，逐格验证主按钮 `#5c99d6` 底色、`#1d2227` 文字、加粗、橙色 `#f9ae58` 焦点，以及 Tab/Shift+Tab、悬停、256 色与无色边界的固定位置。PNG 按 PTY 字符格重绘，不是 Ghostty 原生截图。
- 产品 `profiles-pty.py` 的 loopback HTTP fixture 完整通过创建、刷新、菜单、持久化、缩放和四种删除确认场景；请求计数为 14。产品 `theme-pty.py` 在宿主和 Linux/arm64 各完成三种颜色能力 × 五种尺寸，覆盖通知、焦点、网络草稿和缩放。
- 产品 `i18n-pty.py` 通过中英文切换、重开后语言持久化、中文鼠标命中、帮助、网络表单及缩放；脚本先显式取消首次只读版本查询，再操作语言按钮，避免在忙碌期间点击禁用按钮。
- `scripts/verify-linux.sh lazyclash-e2e` 在已有专用 Debian/arm64 VM 内实际执行全部可运行的 Linux Go 测试，通过。预检为普通 uid 501、systemd 用户 manager running、Linger=yes；私有 PTY 暂存目录已清理，没有修改 VM 配置。

记录位于忽略目录 `.test-artifacts/buttons/`：宿主检查及 race 日志、`linux-tests.log`、`theme-pty/`、`linux-pty/`、`profiles-pty/` 和 `wizard/`。本轮没有重跑真实 Mihomo/systemd 生命周期 E2E；需显式实际核心路径的集成测试仍按条件跳过，安装进度和成功场景来自内存后端。

## 概览内安装与统一下载方式（2026-11-02）

本节记录当时实现；下载与校验行为已由上方 2026-11-03 的 API-free 实现替代。下方同日的首次安装和单页安装记录保留其当时行为与验证结果。概览直接显示安装区，默认只读检查稳定版本；明确点击下载并安装后才解析资产、预检及安装，不再打开初装弹窗或要求第二次确认。下载方式统一为直连、使用环境代理、GitHub 加速，当时版本文本、发布 API、安装包探测和下载遵循同一设置。该轮没有移除安装阶段对 GitHub API 预期 SHA256 的依赖。

本轮已实际验证：

- 宿主 macOS/arm64 的全包 `make check`、`make race` 与构建通过；包含查询取消与迟到结果、单次安装链、失败重试、预检阻断、取消等待清理、明确启动、输入焦点和极小窗口回归。
- `scripts/verify-linux.sh lazyclash-e2e` 在已有专用 Debian/arm64 VM 中实际执行全部 Linux 测试包，通过。网络 fixture 验证 version.txt → 发布元数据 → Range 探测 → 完整 SHA256 下载的统一加速地址，没有静默直连回退。旧偏好迁移和拒绝不支持配置时保留原文件也有回归。
- `scripts/onboarding-pty.py --test-binary` 在宿主真实 controlling PTY 完成中英文 × 三种颜色能力 × 120×32、80×24、60×20 的 18 组取消场景；成功、查询失败重试、更多选项各完成中英文 × 三种尺寸，共 18 组。每组核对检查、解析、预检、安装调用次数，成功页明确验证 `starts=0`。
- Linux/arm64 实际 controlling PTY 完成取消、成功、更多选项 × 中英文 × 三种尺寸，共 18 组；临时目录已清理。`core-update-pty.py` 另完成中英文 × 三种颜色能力的六组更新场景，包括 120×32、80×24、60×20、40×16 缩放。
- 实际产品 `theme-pty.py` 完成三种颜色能力各五种尺寸；`i18n-pty.py` 完成语言切换、偏好持久化、中文鼠标命中、帮助、下载方式表单及缩放。Python 四项 VT 解析回归、全部 Python 脚本语法、E2E Shell 语法与差异空白检查通过。
- 收尾复核修正失败页展开更多选项的重复焦点，以及后台查询失败后从导航返回正文时恢复到禁用安装按钮的问题；实际 `Update` 按键回归验证可达选项及显式重试。最终源码重新完成全包 check/race、Linux 全包测试与构建，独立导出的实现暂存树通过 `go test -mod=readonly ./...`。日志分别为 `.test-artifacts/inline-check-final.log`、`inline-race-final.log`、`inline-install-linux-final.log` 与 `inline-staged-implementation-test.log`。

最终实际执行 `./scripts/e2e.sh lazyclash-e2e --download-method environment`：冲突预检及真实产品 `empty` PTY 通过，包括安装前导入、下载方式草稿、24×8 缩放、本地长路径输入、Esc/SIGTERM 取消和清理。随后官方固定版本 Release API 返回 HTTP 403，尚未下载内核，本轮真实安装、启动及后续 systemd 生命周期未完成。清理确认 `resources released=1`、`exit=1`。记录为 `.test-artifacts/inline-e2e-after-pty-sync.log` 和 `.test-artifacts/cleanup.txt`。

完整前置操作后的长路径输入暴露了验证脚本读取旧增量帧的问题：脚本连续发 Tab 时仍在消费输入期间积压的画面。`pty-smoke.py` 现在先等完整路径，再等同步渲染帧完成后判断每次焦点，保留准确的安装按钮断言。独立 Linux 实际产品 `empty` 也完整通过，记录为 `.test-artifacts/inline-install-pty/empty-real-final.txt`。

主要证据位于忽略的 `.test-artifacts/inline-install-pty/`：`final-matrix/20261102-170431-f9df1e`、`final-scenarios/20261102-170632-6e2b08`、`linux/evidence/20261102-171358-437da5`、`core-update/`、`theme-final/` 与 `i18n-final/`。稳定展示图为 `.test-artifacts/inline-install-preview-{initial,download,success,network,compact}.png`，已按 Linux 实际 PTY 字符格和颜色重绘并人工查看。

安装与升级 PTY 使用明确启用的内存后端 fixture；它们验证产品状态机和终端交互，不下载或执行真实 Mihomo，也不创建 systemd 服务。PNG 不是原生 Ghostty 截图，不以其代替真实下载安装或服务生命周期验证。

## Mihomo 版本检查与升级（2026-11-02）

本轮实现与边界见 [内核版本管理](core-updates.md)。已实际运行：

- 宿主 macOS/arm64：`make check`（全部 Go 测试与 vet）、`make race`、`make build`、四项 Python 终端解析测试及 `git diff --check` 通过。最终界面紧凑布局及诊断调整后再次运行全包 check/build，并对 app、CLI、TUI、service、store 的更新/恢复/锁相关用例运行 race，通过。
- `./scripts/verify-linux.sh lazyclash-e2e` 在现有专用 Debian 12/arm64 VM 内实际执行全部包测试，两次均通过，最终记录位于忽略的 `.test-artifacts/core-update-linux-run.log`。包括正式版比较、Alpha/未知格式、未安装/不可执行、HTTP/超时/无效响应、损坏下载、配置与版本验证失败、取消、重启/API就绪失败、回滚失败保留材料、中断边界、Stop 优先、systemd start-limit 清理、外部状态变化及并发升级。
- `scripts/core-update-pty.py` 使用 opt-in TUI 测试二进制，在真实 controlling PTY 内验证中英文 Manage 打开不查询、一次手动检查、版本审阅、一次明确确认升级、Esc 等待模拟恢复以及缩放；各自调用报告为 `checks=1, upgrades=1`。测试尺寸 120×32、80×24、60×20、40×16；40×16 初始屏同时可见本地/目标版本、状态和重启提示。记录位于 `.test-artifacts/core-update-20261102/pty-final/`。此 PTY 使用 fake backend，无真实网络、内核文件或系统服务，不能当作真实升级证据。
- 同目录 `previews/` 保存 24 组中英文检查/进度/结果的 ANSI 与 PNG（四种尺寸）；PNG 是模型终端渲染预览，已人工查看，非原生终端截图。
- 构建后的实际 CLI 经显式 environment 代理成功查询 stable `v1.19.33`、Alpha `alpha-69c1631`。两次以不存在的数据目录调用 `install check`，查询后均确认目录不存在；没有创建锁、下载内核或启动服务。

真实完整 E2E **本轮未通过**：显式执行 `./scripts/e2e.sh lazyclash-e2e --proxy-mode environment` 与无代理版本，均在第一个固定版本安装的官方 Releases API 查询返回 HTTP 403（permission denied or rate limit），尚未下载内核。两次均完成冲突预检和 empty PTY；清理确认 `resources released=1`，无专用 unit、进程或监听残留。产品没有绕过摘要验证；同一网络的 `version.txt` 查询可以成功。

E2E 脚本已新增旧版实际运行时只读检查保持 PID、确认升级改变 PID/实际版本且配置不变的断言，但因上述限流尚未执行到该场景。真实新内核启动失败、start-limit 后旧版本恢复尚无本轮 systemd 实测证据，仅有业务及服务 fixture 测试。未验证真实掉电持久性、Linux/amd64 CPU 或其他发行版；不能由 Linux/arm64 单元执行和文件同步顺序推断这些环境已通过。

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

`empty` 模式仅操作专用的未安装数据目录，检查概览内安装区、下载方式草稿的编辑与放弃、安装前导入、宽窄布局、安装阶段取消和 SIGTERM。`download-network` 模式按明确选择的 direct/environment 方式，在未安装实例中真实查询版本和探测安装包，再放弃草稿，断言没有创建安装或服务。`install-local` 模式通过 TUI 内联安装明确的本地 Mihomo 二进制并确认未启动服务；`installed` 模式使用已启动的专用服务，以鼠标和键盘切换 PROXY 节点，并通过实际 API 回读确认。`runtime` 模式使用专用流量连接，验证实时卡片、按 UUID 搜索/详情/确认关闭、缩放和停止/启动后的自动恢复。`runtime-settings` 模式验证端口输入、取消草稿、占用后的纠正、确认重启、宽窄切换及各控制 API 页面的重新连接。完整的目录、服务、前置检查和清理由下一入口安排，建议通过它执行全部模式。

## 4. 真实 Mihomo + systemd 端到端

```sh
./scripts/e2e.sh "$VM"
# 若明确使用该 VM 已有的标准代理环境变量：
./scripts/e2e.sh "$VM" --download-method environment
```

此脚本显式修改所选 Linux VM 中的**临时资源**：

- 动态检测 CPU 架构、普通用户与用户 systemd，不硬编码用户名或工程目录。
- `mktemp` 数据目录、随机 `lazyclash-e2e-*` unit；运行前检查同名 unit 与 `9097`/`7897` 端口，冲突即失败，不停止已有资源。
- 显式 environment 模式将已有 HTTP(S) `HTTPS_PROXY` 作为上游，为本轮进程设置临时本地 CONNECT observer；不解密 TLS，只记录目标类别与隧道计数，并拒绝 `api.github.com`。每个远程阶段断言经过该路线且 API 尝试为零。此验证工具不支持 SOCKS 上游；产品本身的环境代理支持范围不变。direct 模式不产生同等的 observer 审计证据。
- 由产品按明确标签与设备规则下载官方 Mihomo **v1.19.32** 和 **v1.19.33**，验证 HTTP 响应、大小限制、gzip 完整性与精确可执行版本；不查询 GitHub Release API 或校验上游预期摘要。另建独立 unit，经 TUI 本地安装和 CLI `--bin` 验证。
- 未安装时真实执行 TUI 连接测试、`install check` 和指定版本 dry-run，检查无安装或 unit；旧版运行时的只读检查保持 PID，确认升级后核对新 PID、已安装与运行版本、配置不变及 journal 清理。
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

## 单页安装与设备构建选择（2026-11-02）

首次打开安装窗口时直接显示版本与下载设置，复用 `CheckUpdate` 查询稳定版，不再要求确认唯一的 Mihomo 内核选项。用户明确点击安装后，使用已查到的固定版本依次解析官方资产、预检并安装；取消或修改选项会清除这次安装意图，迟到结果不能触发安装。已有实例通过手动入口重装、降级或更换本地文件时仍保留计划审阅，预检阻止项不能绕过。

设备构建选择继续限定为已有 Linux/arm64 与 Linux/amd64。arm64 使用原生构建；amd64 根据每个可见 CPU 的指令集交集及操作系统 AVX 状态支持，选择可用的最高安全 v3、v2 或 v1，信息不完整时保守选择 v1。旧版无标签 amd64 只在已确认 v3 能力时使用，`compatible` 作为 v1 回退；初装、指定版本和升级共用该解析器。参考 [Mihomo FAQ](https://github.com/MetaCubeX/mihomo/wiki/FAQ) 与 [Go 最低要求](https://go.dev/wiki/MinimumRequirements#amd64)，没有凭 CPU 型号推测能力，也没有扩大其他架构或旧 Linux 内核的支持范围。

本轮实际验证：

- 宿主 macOS/arm64：最终源代码的 `make check`（全部 Go 测试及 vet）通过，记录为 `.test-artifacts/install-flow/check-final.log`。
- `scripts/verify-linux.sh lazyclash-e2e`：在已有专用 Debian/arm64 VM 内实际执行最终源代码的 Linux 全包测试，通过；记录为 `.test-artifacts/install-flow/linux-final.log`。
- Mihomo 及 TUI 的 Install、CoreUpdate、Wizard、Resolve 相关 race 测试通过，记录为 `.test-artifacts/install-flow/race-final.log`；不是本轮全包 race 的结论。设备选择测试覆盖全部 v2/v3 必需位缺失、系统未启用 YMM 状态、多核能力不一致、CPU 信息缺失，以及资产缺少时降级和禁止向上误选；AMD64 测试二进制另通过交叉构建，未声称已在真实 AMD64 设备运行。
- Python 四项 VT 协议回归通过。真实产品二进制使用 environment 连接模式查询官方稳定版本，得到 `v1.19.33`，查询后隔离数据目录仍不存在，未创建安装或服务。
- 最终 `make build`、Python 脚本语法和 Shell 语法检查及差异空白检查通过。
- 更新后的 `scripts/onboarding-pty.py --test-binary` 在宿主完成中英文 × True Color、ANSI256、NO_COLOR × 160×45、120×32、80×24、60×20、120×18 共 30 场景；在专用 Linux/arm64 VM 实际完成中英文 × True Color × 120×32、80×24、60×20 共 6 场景。每场明确记录 `checks=1, resolves=1, prepares=1, installs=1`，验证一次安装操作串联、取消等待，以及返回后的会话跳过与浏览。后端是明确启用的内存 fixture，没有真实下载、执行 Mihomo 或创建服务。主机记录在 `.test-artifacts/install-flow-pty/final/20261102-140846-33c0eb/`，Linux 记录在 `.test-artifacts/install-flow-pty/linux/evidence/20261102-141332-16465f/`；已目视检查 Linux 120×32、60×20 安装页 PNG。VM 私有暂存目录已清理并复查不存在。

最新完整 `e2e.sh lazyclash-e2e --proxy-mode environment` 尝试通过 `empty` PTY 后，在官方固定版本 release API 查询阶段收到 HTTP 403；清理记录为 `resources released=1`、`exit=1`。本轮未完成真实内核下载安装、启动及后续 systemd 生命周期验证，不以版本查询、模型测试或界面预览代替这些证据。

证据保存在忽略目录 `.test-artifacts/install-flow/`。终端 PNG 按实际 PTY 字符格与颜色重绘，安装进度和结果的内存替代后端预览只用于界面检查；两者均不能作为实际下载或服务运行证据。测试使用隔离数据目录和专用资源，实测平台范围未扩大。
