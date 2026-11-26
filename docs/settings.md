# 设置页

设置页集中管理应用偏好、持久服务设置和维护操作。卡片左侧列出设置名称，右侧显示开关和下拉框。

- **常规**包含语言、启动页面、底部提示栏、主题和编辑器。浏览页面时按 `?` 可隐藏或显示底部提示栏，与此处的开关同步。
- **内核设置**按「端口与访问」「连接与运行」「资源与缓存」三个标签组织。每个实例独立保存这些设置。
- **内核管理**用于安装或更新 Mihomo、选择下载方式、设置开机自启和重启当前实例。

日常启停和 TUN 在概览，路由模式在概览和代理页，配置导入在订阅页。
概览「终端代理」的「配置」也可修改同一个混合端口。

「启动页面」默认是「概览」，也可选择「代理」「订阅」「连接」「规则」「日志」或「设置」。保存后会停留在设置页，下次打开 TUI 时进入所选页面。未安装 Mihomo 时也可使用这项偏好，安装入口仍在概览页。

## 内核设置

### 端口与访问

「代理端口」默认显示 `7897 ❯`。点击整行可设置混合、HTTP(S) 和 SOCKS 端口，可用范围为 1–65535。留空或填 0 即停用该监听。Linux（含 Android）内核另有 Redirect 和 TProxy 端口，macOS 内核另有 Redirect 端口。平台取自已校验的内核可执行文件；无法识别时展示全部端口，并在说明中列出平台限制。关闭「允许局域网访问」时，常规代理通过 `127.0.0.1` 接收连接。「绑定地址」可提前保存，重新开启后使用该值。默认的 `*` 表示所有接口。控制 API 使用自己的监听地址。[Mihomo 代理端口](https://wiki.metacubex.one/config/inbound/port/)

点开「绑定地址」可直接输入 IPv4 或 IPv6。确认运行中的 Mihomo 与 TUI 共享网卡时，下拉框会列出地址及网卡名。长地址在窄屏自动换行，点击任意一行都能填入该地址，也可用方向键浏览、Enter 填入、Esc 收起。内核已停止或无法确认网卡归属时，可手输或选择「所有接口（*）」。审阅并应用后保存。

「访问规则」集中编辑允许网段和禁止网段。默认只显示 `❯`，任一列表与默认值不同时显示 `自定义 ❯`。按客户端来源 IP 匹配，每行可填一个 IPv4、IPv6 或 CIDR。禁止优先。清空允许列表会拒绝所有来源，包括本机。「恢复默认」填入两个地址族的全网段并清空禁止列表，审阅并应用后保存。允许网段、禁止网段和免验证网段分别保存。未编辑的字段继续沿用订阅值。

「用户验证」显示 `未配置 ❯` 或 `N 个用户 ❯`。HTTP(S)、SOCKS 和混合代理共用账号。已有账号的密码留空即沿用。在同一弹窗内可展开「免验证网段」，为已经通过访问规则的来源跳过账号验证。代理地址平时以占位符表示凭据。点击「复制」可复制含凭据的地址，多个账号时先选择用户名。通配绑定地址会保留局域网 IP 占位符，使用时换成内核所在机器的地址。[Mihomo 通用配置](https://wiki.metacubex.one/config/general/)

Shell 集成仅发布本机的混合端口。若本机需要用户验证，可通过「复制」取得含凭据的代理地址，在客户端使用。

「外部控制」右侧显示 `❯`。点击整行可设置 HTTP 监听地址和 API 密钥，展开分组可配置 HTTPS 监听和证书、CORS、本地 Unix 套接字、Linux 路由标记及 DoH 路径。HTTP 控制地址用于管理当前实例，默认是 `127.0.0.1:9097`。密钥留空沿用现值，审阅时仅显示是否替换。Unix 套接字和 DoH 请求绕过 API 密钥验证。Unix 套接字的父目录须由内核运行账户独占，例如在实例的私有运行目录中使用相对路径 `control.sock`。HTTPS 证书和私钥也可放在该运行目录中。[Mihomo 外部控制](https://wiki.metacubex.one/config/general/#api)

在同一个弹窗内审阅变更，再选择应用或返回修改。端口输入有效且有变更时可进入审阅，仅列出改动的端口。应用到运行中的服务会重启内核并中断活动连接，随后从控制 API 读取变更端口的实际值。已停止的服务会在下次启动时使用新值。

### 连接与运行

| 设置项 | 新实例默认值 | 用途 |
| --- | --- | --- |
| IPv6 | 开 | 控制 Mihomo 的 IPv6 支持。启用主机 TUN 时内核临时开启 IPv6，关闭 TUN 后恢复保存的选择。 |
| 统一延迟 | 关 | 测试节点延迟时再发一次请求。若成功，采用这次请求的耗时，减少初次握手对结果的影响。 |
| 日志等级 | `info` | 控制内核输出的详细程度，可选 `debug`、`info`、`warning`、`error`、`silent`。 |
| TCP 并发连接 | 关 | 同一域名解析出多个 IP 时并发尝试 TCP 连接，采用最先连通的地址。对应 `tcp-concurrent`。 |
| TCP 保活 | 开 | 允许内核为 TCP 连接启用保活探测。开启对应 `disable-keep-alive: false`。首次探测前的空闲时间和探测间隔按秒设置，留空使用内核默认值。 |
| 进程匹配 | 按需（`strict`） | 按需在规则用到进程信息时查询。也可选始终查询（`always`）或关闭查询（`off`）。对应 `find-process-mode`。 |

统一延迟和 TCP 并发连接的说明已对照 Mihomo 官方文档与 v1.19.33 的实现。[全局配置](https://wiki.metacubex.one/config/general/) · [节点测速源码](https://github.com/MetaCubeX/mihomo/blob/v1.19.33/adapter/adapter.go#L237-L262) · [并发拨号源码](https://github.com/MetaCubeX/mihomo/blob/v1.19.33/component/dialer/dialer.go#L296-L327)

### 资源与缓存

| 设置项 | 新实例默认值 | 用途 |
| --- | --- | --- |
| 自动更新 GEO 数据 | 关 | 开启后可设置更新间隔，默认为 24 小时。运行中调整计划后，内核管理会提示在下次启动时生效。 |
| GEO 加载模式 | 节省内存 | 可选择 `memconservative` 或 `standard`。 |
| 资源下载 ETag | 开 | 使用服务器的 ETag 检查远程资源是否变化。 |
| 记住 Fake-IP 映射 | 关 | 将 `profile.store-fake-ip` 应用到当前实例。使用 Fake-IP 的订阅可跨重启复用映射，DNS 模式仍由订阅决定。 |

这些偏好优先于订阅原文和该订阅的覆写。刷新或切换订阅、重新打开 TUI 和重启内核都会保留选择。已有实例继续使用已经保存的偏好。首次出现的选项从当前已部署的配置读取，缺省时使用上表默认值。[Mihomo v1.19.33 默认配置](https://github.com/MetaCubeX/mihomo/blob/v1.19.33/config/config.go#L454-L500)

保存时会校验生成的配置。运行中的内核按需热重载，停止的内核在下次启动时读取新配置。订阅源文件保持原样。[Mihomo 通用配置文档](https://wiki.metacubex.one/config/general/)

## 内核管理

未安装时，卡片提供「安装内核」，点击后进入概览页的安装引导。安装后，可在这里管理版本和服务。当前运行状态、启停操作和运行时长可在概览页查看。

- **已安装版本**显示本机版本号。点击整行可查看版本详情。内核运行时，详情中会列出运行版本和可获取的运行时长。手动安装指定版本、使用本地可执行文件和 `lazyclash uninstall` 卸载说明也在这里。
- **检查更新**直接查询远端版本。审阅结果后，有新正式版时可选择「确认升级」。按 Esc 返回卡片。
- **下载设置**在未安装时位于「安装内核」下方，安装后位于「检查更新」下方。行末显示当前下载方式。点击或按 Enter 可选择直连、环境代理或 GitHub 加速，并测试版本信息和安装包连接。首次安装、检查更新和升级下载共用此设置。
- **开机自启**决定系统启动时是否启动 Mihomo。切换开关时，当前运行状态保持不变。用户实例需要先启用 linger，系统实例可直接设置。保存的偏好与 systemd 报告的自启状态不一致时，开关下方会提示重新应用。
- **用户服务 / 系统服务**标明「重启」作用于哪个实例。运行中调整 GEO 更新计划后，按钮下方会提示新计划在下次启动时生效。

服务查询错误、异常退出结果和退出码可在概览「内核状态」查看，出现故障时会提示运行 `lazyclash doctor`。保存的路由模式与实际模式不一致时，概览「路由模式」会显示两个值和重新应用的提示。

## 布局与交互

正文填满侧栏和外侧留白之外的可用宽度，不设最大列宽。
正文宽度达到 80 格时分两列，左侧从上到下是「常规」和「内核管理」，右侧是「内核设置」。窄屏按「常规」「内核设置」「内核管理」的顺序纵向排列。在同一终端宽度下，三个内核标签按最高页的内容预留空间，切换时下方卡片保持原位。

标签从左向右按文字宽度排列。蓝色文字和短下划线标出当前页。宽屏会拉开标签间距，窄屏会收紧间距，并在需要时使用短标签。窗口空间充足时，同组的设置项缩进两格，项目之间空一行，两组之间空两行。较矮或较窄的窗口使用紧凑布局。

鼠标经过设置项时保留原有配色。点击控件后，设置行显示普通底色。用鼠标打开菜单或弹窗，按 Esc 返回时也是如此。按 Tab 或 Shift+Tab 会从该项继续导航，并高亮新聚焦的控件。用键盘打开菜单或弹窗，关闭后会保留入口的焦点标记。

点击标签或在标签上使用左右方向键切换。当前值靠右显示。点击「代理端口」「代理身份验证」或「外部控制」整行可打开对应弹窗。数值行可直接输入，Enter 保存、Esc 取消。TCP 保活关闭时暂时禁用它的两个时间输入，保留原值。底部提示栏会列出当前控件可用的按键。

「编辑器」右侧默认显示 `❯`。指定命令后，这里显示命令和参数，例如 `nvim -f ❯`。较长的命令用省略号缩短。点击整行，或聚焦后按 Enter，可查看和修改完整命令。

设置弹窗的标题与正文左侧对齐，标题下方用细线分隔。代理端口分为「常规代理」和「透明代理」两组。每个端口独占一行，短输入框在右侧对齐。空间充足时，端口名称相对分组标题缩进两格，与输入框中的数值垂直居中。窄屏或矮屏使用单行输入框，底部保留操作按钮。悬停名称旁的 `ⓘ`，或在对应输入框按 `F1`，即可查看协议说明。宽屏下，每个代理账号的用户名、密码和移除操作位于同一行。外部控制优先显示监听地址和 API 密钥，其余选项可按组展开。设置弹窗内的输入框和按钮也仅在聚焦或操作时改变样式。窄屏下，字段随可用宽度换行，滚动区域会跟随键盘焦点。底部操作按钮固定，审阅时可返回继续编辑。

将鼠标停在配置项名称后的 `ⓘ` 上约 300 毫秒可查看说明，移开即关闭。键盘聚焦该配置项时，按 `F1` 查看说明，再按 `F1` 或 `Esc` 关闭。切换焦点或离开页面也会关闭说明。图标沿用配置项的焦点。

在设置正文中，按 `↑` 或 `k` 移到上一项，按 `↓` 或 `j` 移到下一项。上下移动沿当前列跨卡片进行，到列首或列尾时停在当前项。双栏中，普通设置项可以用 `→` / `l` 从左栏进入右栏，用 `←` / `h` 从右栏回到左栏。焦点优先落在对面同一行，否则落在垂直距离最近的可操作项，上下等距时选上方。移动会跳过只读和禁用项，新聚焦的控件自动滚入视口。

在左栏或单栏的普通设置项上按 `←` / `h` 返回导航，在右栏或单栏按 `→` / `l` 停在当前项。`Esc` 从任一栏返回导航。`Tab` 和 `Shift+Tab` 遍历整页控件，滚轮和 `PgUp` / `PgDn` 单独滚动页面。底栏显示当前项的操作，按 `Enter` 执行。

横向控件优先使用自己的方向键。在内核标签条上，`←` / `→` 或 `h` / `l` 切换分类。输入框中的左右键移动光标，`h`、`j`、`k`、`l` 按普通字符输入。
改变窗口尺寸会重新计算布局与命中区域。语言、启动页面、提示栏和主题选择会保存到下次启动。

语言、启动页面、主题、日志等级和进程匹配使用相同的下拉框。语言可选 English 和简体中文。中文界面的主题选项依次为「自动」「浅色」「深色」，英文界面对应 Auto、Light、Dark。每个控件按本组选项的最长显示宽度排版，切换选项时箭头保持原位。点击控件，或用 Tab 聚焦后按 Enter 打开，箭头由 `▾` 变为 `▴`。行尾 `✓` 标出当前值。用 ↑/↓ 或滚轮移动高亮行，鼠标经过其他选项时游标留在原位。无色终端用行首 `›` 表示游标。按 Enter 或点击选项保存，Esc 取消本次选择。

「深色」（默认）使用 Mariana 配色。「浅色」使用雾蓝灰配色。选择「自动」后，终端报告浅色背景时使用雾蓝灰，报告深色背景或尚未报告时使用 Mariana。

设置卡片布局见 `internal/tui/settings_view.go` 与 `settings_mihomo.go`。三个访问设置弹窗共用 `ports.go` 的审阅与应用流程。数值和端口字段复用 Charm Bubbles 的单行输入组件，CORS 来源使用多行输入组件。[Charm Bubbles](https://github.com/charmbracelet/bubbles)

设计借鉴用户提供的 Clash Verge、CFW 及工具设置截图中的分组、行对齐和当前值展示，
并参考 [Lazygit 配置](https://github.com/jesseduffield/lazygit/blob/master/docs/Config.md)
对自适应面板和键盘焦点滚动的处理。

## 验证入口

设置页在 160×45 终端中按双列显示。40×16 终端中可用键盘滚动到「下载设置」，按 Enter 打开弹窗、按 Esc 返回原行。 [160×45 设置页](../.test-artifacts/settings-management/final/settings-wide.png) · [40×16 下载设置行](../.test-artifacts/settings-management/final/settings-narrow.png)

```sh
go test ./internal/tui -run 'TestSettings|TestCoreSettings|TestCoreNumber|TestRuntimePorts|TestAccess|TestProxyAuth' -count=1
go test -c ./internal/tui -o bin/settings-tui.test
python3 scripts/settings-pty.py --binary bin/settings-tui.test \
  --output .test-artifacts/settings-redesign/pty \
  --profile truecolor --profile 256 --profile no-color
```

PTY 使用隔离的内存后端，不访问真实 Mihomo、订阅或系统服务。PNG 从实际 PTY 字符格与颜色重绘，
用于检查排版和命中，不代表真实代理网络或系统服务生命周期验证。

用 `--modals-only` 可单独验证三个访问设置弹窗。宽屏下检查输入框边框、标签对齐和分组留白，40×16 下检查 Tab 滚动和说明图标。[160×45 弹窗](../.test-artifacts/proxy-port-spacing/ports-160x45.png) · [40×24 弹窗](../.test-artifacts/proxy-port-spacing/ports-40x24.png) · [40×16 聚焦 TProxy](../.test-artifacts/proxy-port-spacing/ports-40x16.png)

给上面的 PTY 命令加上 `--tabs-only` 可单独检查鼠标移入标签、端口行和开关前后的字符与颜色，并验证键盘切页。[200×46 实际终端](../.test-artifacts/settings-no-hover/20261126-124244-741106/zh-Hans/truecolor/resources-focused-access-hover-200x46.png) · [40×24 实际终端](../.test-artifacts/settings-no-hover/20261126-124244-741106/zh-Hans/truecolor/resources-focused-access-hover-40x24.png)

三个内核设置标签已在中英文、宽屏和窄屏的 TUI 中验证。Linux/arm64 上另用真实 Mihomo v1.19.33 和独立 systemd 用户服务验证了新增 HTTP 代理端口、控制地址与密钥轮换、局域网开关、行内保活编辑，以及 GEO 更新计划在重启前后的提示。切换和刷新含相反设置的订阅后，再次读取了实例配置。[200×46 设置页](../.test-artifacts/settings-spacing/20261126-113645-a57553/zh-Hans/truecolor/ports-access-200x46.png) · [外部控制审阅](../.test-artifacts/settings-tab-style/20261126-112838-c53317/zh-Hans/truecolor/external-control-review.png) · [真实内核验证结果](../.test-artifacts/core-settings-runtime/verified.json)

2026-11-05 验证通过：`make check build`、八项 Python 终端解析测试，以及
`./scripts/verify-linux.sh lazyclash-e2e` 的 Linux/arm64 实际测试。
独立导出的设置页改动基于已提交源码通过构建、设置/语言/端口/TUN/提示栏与文案定向测试，
确认不依赖其他会话未提交的页面实现。

初版真实 PTY 完成中英文 × TrueColor、ANSI256、NO_COLOR 六组，每组均验证
160×45、120×32、80×24、60×20、40×16 并恢复宽屏；覆盖语言保存时重复点击阻断、
提示栏鼠标切换和 Ctrl+T 恢复、滚轮、Tab 揭示导入与端口，以及端口窗口取消后的焦点恢复。
每组语言保存 2 次、提示栏保存 2 次、端口读取 6 次，服务、路由模式和端口应用次数均为 0。
已目视检查中文宽屏、40×16 及英文无色布局。

证据在忽略目录 `.test-artifacts/settings-redesign/`；PTY 汇总和字符格截图在
`pty/20261105-214548-3cca79/`。本次未重跑真实 Mihomo/systemd/TUN 生命周期 E2E。

2026-11-06 移除设置页沿用通用布局的 112 列上限。回归从实际终端尺寸验证正文宽度和卡片最右边框，
覆盖侧栏出现及单双列断点，避免只直接测试 `settingsContent` 而遗漏外层限宽。
`make check build`、Linux/arm64 VM 测试与独立导出的设置页定向测试均通过。
中英文 TrueColor PTY 完成 240、200、160、120、105、104、100、99、84、83、80、60、40 列及恢复宽屏；
每组端口读取 14 次，语言与提示栏各保存 2 次，服务、模式和端口应用次数均为 0。
已目视检查宽屏和单列布局。证据在 `.test-artifacts/settings-width/`，最终 PTY 为
`pty/20261106-163755-cb3db0/`。本次未重跑完整颜色组合或真实内核/服务生命周期。

2026-11-21 将设置页重组为界面偏好、服务与端口、内核管理和故障排查。模型回归覆盖入口去重、
端口摘要脱敏、重启可用性、条件诊断和缩放后的焦点。中英文 TrueColor PTY 各完成 14 次尺寸切换，
验证 240 列双列至 40 列单列、语言和提示栏保存、端口窗口往返，以及从故障排查打开日志后返回设置。
每组读取 14 次端口与 1 次隔离日志流。截图和汇总位于
`.test-artifacts/settings-dedup/pty/20261121-120313-7ce5cf/`。

2026-11-25 初版曾提供「跟随配置」选项。在专用 Linux/arm64 虚拟机中通过设置页修改三项通用设置，并从 Mihomo v1.19.33 的控制 API 读回实际值。运行时修改保留原进程，服务停止时保存保留停止状态。[初版真实内核验证结果](../.test-artifacts/mihomo-general-settings/linux-real-core.json)

中英文 PTY 在真彩、256 色和无色模式下覆盖 240 至 40 列、键鼠选择、取消和恢复跟随配置。[160×45 中文设置页](../.test-artifacts/mihomo-general-settings/pty/20261125-163818-26756d/zh-Hans/truecolor/mihomo-overrides-wide.png) · [40×16 菜单](../.test-artifacts/mihomo-general-settings/pty/20261125-163818-26756d/zh-Hans/truecolor/mihomo-menu-compact.png)

设置行的布局回归按终端字符格检查中英文七个开关和下拉框，包括刚好能同行和少一格必须换行两种情况。真实 PTY 另验证了换行后点击语言下拉框以及用 Esc 收起菜单。[160×45 双列布局](../.test-artifacts/settings-row-alignment/pty/20261125-165055-37cbbc/zh-Hans/truecolor/settings-wide.png) · [28×24 换行布局](../.test-artifacts/settings-row-alignment/pty/20261125-165055-37cbbc/zh-Hans/truecolor/settings-wrapped-28x24.png)

实例设置固定后，Linux/arm64 实测验证了新安装的默认值和首次导入订阅后的值。通过设置页修改三项后，切换订阅、刷新、重启服务并重开 TUI，均与保存值一致。运行中的 Mihomo v1.19.33 热重载时保留原进程。[真实内核读回结果](../.test-artifacts/settings-fixed-switches/linux-real-core.json)

当时设置页的四个开关使用相同的六格滑轨。中文宽屏预览展示两种滑块位置，窄屏预览展示长名称下方的键盘焦点和完整滑轨。[160×45 双列设置页](../.test-artifacts/settings-fixed-switches/rail-only/20261125-170352-e741fa/zh-Hans/truecolor/settings-wide.png) · [28×24 开关换行](../.test-artifacts/settings-fixed-switches/rail-only/20261125-170352-e741fa/zh-Hans/truecolor/switch-wrapped-28x24.png)

扩充后的「内核偏好」在中英文、真彩、256 色和无色终端中验证了四个新增控件。Linux/arm64 专用实例从 Mihomo v1.19.33 的控制 API 读回全部七个字段，并在切换到含相反设置的订阅、刷新和重启服务后再次核对。[宽屏卡片](../.test-artifacts/core-preferences/final-pty/20261125-205212-fd8601/zh-Hans/truecolor/settings-wide.png) · [40×16 进程匹配菜单](../.test-artifacts/core-preferences/final-pty/20261125-205212-fd8601/zh-Hans/truecolor/process-menu-40x16.png) · [真实内核读回结果](../.test-artifacts/core-preferences/linux-real-core.json)
