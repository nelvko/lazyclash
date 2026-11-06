# lazyclash

以 TUI 为主要交互的 Mihomo 终端代理管理工具。Go module：`github.com/nelvko/lazyclash`。

Linux/systemd 同时支持普通用户的 user 实例和 root 的独立 system 实例；两者均可运行本机 HTTP/SOCKS 代理，root 可明确确认后启用整台主机的 TUN。`sudo lazyclash` 操作 root 自己的配置和服务，不复制用户数据或切换原用户实例。普通代理可在端口不冲突时并存，同一网络命名空间内的 TUN 管理权独占。首发范围见 [服务 scope](docs/service-scopes.md)；项目未发行，不提供旧数据格式、unit 或命令行为兼容层。

## 构建与使用

需要 **Go 1.26+**。固定稳定依赖：Bubble Tea **2.1.1**、Bubbles **2.2.1**、Lip Gloss **2.0.6**，统一使用 `charm.land/*/v2`；Cobra **1.10.2**。完整版本与校验记录在 go.mod/go.sum。

```sh
make build
./bin/lazyclash
# 或在源码根目录直接运行
go run .
```

服务与内核安装支持 **Linux/systemd 的 user/system 服务、arm64 与 amd64**。macOS 可构建、运行 TUI、配置下载网络及查询 Linux 发布信息，但预检查会明确阻止安装和服务管理，没有 macOS 空后端。

发行打包使用 `make package VERSION=dev`：在 `bin/packages/` 生成 `lazyclash_dev_linux_amd64.tar.gz`、`lazyclash_dev_linux_arm64.tar.gz` 和 `lazyclash_dev_SHA256SUMS`。每个归档包含一个 lazyclash 可执行程序及 README；Mihomo 在使用时下载或从明确选择的本地文件安装。管理员使用 root 实例前，应把可信发行程序安装到受保护的位置，例如 `/usr/local/bin/lazyclash`。

在 macOS 终端执行 `./scripts/dev.sh` 可构建并进入现有的专用 OrbStack 虚拟机；进入后直接输入 `lazyclash`。脚本自动选择唯一的 `lazyclash` / `lazyclash-*` 虚拟机并检测其架构，构建产物为 `bin/linux-架构/lazyclash`，仅向当前虚拟机 Shell 的 PATH 添加该目录。程序使用正常默认数据目录和服务名，安装及启动由你在 TUI 中操作。多个专用虚拟机时可用 `./scripts/dev.sh VM_NAME` 选择；`exit` 返回 macOS。

## 界面语言

支持 **English / 简体中文**。在 **Settings / 设置 → Language / 语言** 选择后立即更新界面，并将偏好私密保存到当前数据目录的 `preferences.json`；保存失败时保留原语言。语言切换使用已有按钮交互，支持 Tab、Enter 与鼠标，未安装内核时也可使用。

启动语言优先级为 `--lang` → `LAZYCLASH_LANG` → 已保存偏好 → `LC_ALL` / `LC_MESSAGES` / `LANG` → 英文。支持 `en`、`zh-Hans`、`auto`，兼容 `zh-CN` 和 `zh_CN.UTF-8` 等常见语言标签；`C` / `POSIX` 使用英文。

```sh
lazyclash --lang zh-Hans
lazyclash --lang en --help
LAZYCLASH_LANG=zh-Hans lazyclash
```

CLI 帮助与面向用户的提示随语言切换。JSON 字段、状态值、命令和参数名、Shell 导出、路径、用户配置名称和内核原始日志保持原文。

## 首页与安装向导

默认进入带侧边导航的 Overview。未安装 Mihomo 时，概览正文直接呈现紧凑安装区，后台只读检查最新稳定版，默认聚焦 **下载并安装 / Download and install**。按一次 Enter 后开始安装，没有内核选择或第二次确认。启动 TUI 不自动下载或安装内核。选择 **稍后设置 / Set up later** 后，本次会话显示正常概览，可继续浏览所有页面，并从 Settings 打开内核管理和下载方式设置；下次启动未安装实例时重新显示引导。

安装会自动准备可运行的**内置默认配置**，并明确显示“内置默认配置已就绪”；启动仍由用户在 Overview 明确操作。订阅和用户导入的本地 YAML 均为可选，不作为完成设置或启动的前置条件。内置配置的 `PROXY → DIRECT` 是正常策略，支持 `DIRECT`、`REJECT`；可随时在 Profiles 添加订阅或配置。安装后及正常停止时不再显示未完成引导。

- `i` 或首页 **下载并安装 / Download and install**：未安装时聚焦概览安装区，共用版本检查功能查询最新稳定版；点击 **下载并安装** 后执行预检和安装，无需再选择唯一支持的内核或重复确认首次安装。已安装时进入管理页。
- `b`：直接打开本地可执行文件安装选项，点击安装后检查版本并执行预检。
- `n` 或 Settings → **Download method / 下载方式**：选择直连、使用环境代理或 GitHub 加速。
- `a` 或 **Import config**：添加订阅 URL 或本地 Mihomo YAML；可在安装内核前保存，标记等待内核校验。
- 侧栏 `↑↓` / `j/k` 立即切换右侧页面，焦点留在导航；`Home/End` 到第一／最后一页，首尾不循环。`Enter`、`→` 或 `l` 进入当前页上次有效的内容位置，首次进入则落到主要列表、卡片或内容视图；这次按键只移动焦点。常驻侧栏中的 `←` / `h` 不移动焦点。
- `1–7` 直接打开 Overview / Proxies / Profiles / Connections / Rules / Logs / Settings 并进入内容，与侧栏顺序一致。`c` 仍可打开 Connections，窄窗口使用 Menu。
- `Tab / Shift+Tab` 按稳定顺序移动到下一个／上一个控件；导航和每个列表整体各占一站，内部使用方向键。内容区 `Enter` 激活当前控件，表单使用 Tab 逐字段移动。`?` 打开可滚动的上下文帮助。
- `Esc` 每次返回一层：关闭最上层菜单或确认框、退出编辑或详情，最后回到当前页导航。普通内容和纵向列表也可用 `←` / `h` 执行同层返回；横向控件优先使用自身方向键，Profiles 卡片网格仍在网格内移动。普通提示消息不抢占返回操作。内联安装区域聚焦时 `Esc` 取消安装并等待清理；切到其他页面后 `Esc` 继续普通导航。后台任务可通过进度通知中的 Cancel 取消；关闭通知不取消任务。`q` 或 `Ctrl+C` 退出时会安全取消任务并等待清理。
- `Ctrl+T` 显示／隐藏整条底部快捷键提示，包括 `q Quit`；默认显示，选择跨启动保存。也可在 Settings → Interface → Show shortcut hints 或帮助中的按钮切换；表单与弹窗保留必要操作提示。
- 输入、编辑、弹窗和菜单保留自身按键。输入框拥有正常字符、编辑组合键与粘贴事件，`q` 和数字不会被全局快捷键抢走。内核 `s/x/r` 快捷操作只在内容区生效。鼠标可点击按钮、选项、输入框和导航；悬浮不切页、不抢焦点，Modal 不向背景传递操作。

页面切换在本次 TUI 会话中保留滚动、搜索、列表选择、展开项及未提交文本；再次选择当前页不重置。返回时恢复有效位置，数据变化后失效的选择会回退到合理内容位置。页面框架和缓存立即显示，旧页读取可以取消，迟到结果不会抢焦点；已提交的更新、启停等任务不会因浏览其他页而取消。日志保留暂停或跟随状态，离页期间的事件不补回。

窄屏 Menu 抽屉打开时定位当前页，`↑↓` 同步浏览并保持抽屉打开；`Enter`、`→`、`l` 或 `Tab` 关闭抽屉并进入内容，`Esc`、`←` 或 `h` 关闭后回到 Menu，已浏览到的页面继续保留。鼠标点击菜单后切页并关闭抽屉。

Overview 自适应撑满内容区，展示 Core、Rule / Global / Direct 模式、当前配置、代理组、流量、内核与系统信息。启停、重启、模式和 TUN 操作调用共享应用用例，完成后重新读取状态；运行值与保存值分别显示。环境卡明确显示 user/system scope；普通用户 TUN 显示不支持，root 系统实例提供整台主机 TUN 的确认入口。系统代理标为 Unmanaged，不显示虚假的关闭状态。宽窗口使用多列卡片，窄窗口逐步收为单列；运行指标按空间分两列或四列。运行后自动展示实际上传/下载速率、活动连接数、本次内核运行累计流量及内核 RSS；未安装、停止、正在连接、部分数据不可用和 API 不可达分别反馈，不把缺失数据显示为零。

## 运行监控与连接

Overview、Connections 或常驻侧栏可见时自动采样；每轮并行读取真实 API，完成后间隔一秒再采样，不堆积并发请求。Mihomo 的 `/memory` 首帧为人工零，跳过首帧取得真实内存，正常一轮约两秒，因此页面通常约三秒更新一次。没有可见采样区域、打开向导、执行操作和退出时会取消采样；旧结果不覆盖当前任务，返回页面可先显示有效缓存。采样前后检查服务归属、状态与进程 ID，停止或重启期间的旧数据会丢弃并重新连接。

`Upload / download` 来自 `/traffic` 最近一秒的字节样本，按 B/s 展示。`Since core start` 直接读取 `/connections` 的 `uploadTotal`、`downloadTotal`，包含已关闭连接的已记录流量；只统计 Mihomo 纳入计数的流量，不是整台机器的网络用量。内核重启或统计被清零时归零，关闭 TUI 不清零。当前不持久化跨重启、每日或每月流量，也不通过速率积分推算累计值。

Connections 显示当前活动连接，可搜索目标域名/IP/端口、进程、规则或连接 ID：

- `/` 或 Search：输入搜索，`Enter` 完成，输入中的 `q`、数字不触发全局操作。
- `↑/↓` 选择连接，`Enter` 查看来源、目标、规则、代理链、进程和字节详情；长内容可滚动。
- `x` 请求关闭选中连接，明确确认后 `y` 执行，默认选择取消；`Esc` 返回。关闭绑定显示的连接 ID，刷新重排不会切换关闭目标。
- `g` / `Ctrl+R` 或 Refresh / Retry：重新采样；暂不可用时持续重试。关闭后重新读取 API，不根据本地列表推测成功。

连接关闭仅提供单条操作。连接已消失时会明确反馈，不关闭其他连接。

Rules 主列表只读展示内核加载的规则并保留匹配顺序，支持搜索、原位展开、异步读取与失败重试。页头可查看并更新规则集合，以及增改删、排序当前 Profile 的自定义规则；自定义规则独立持久化，订阅更新后保留，保存与应用分开。Rule 模式才按规则顺序匹配。操作与数据含义见 [规则管理](docs/rules.md)。

首次安装在概览正文中完成，保留侧栏和标题，面板高度随内容变化；更多选项折叠收纳下载方式、指定版本和本地可执行文件。查询失败就地提供重试与下载方式设置，安装按钮在没有可用版本时不可用。检查、下载、校验和完成状态在同一区域更新，完成后主动选择 **启动内核** 才会启动。管理已安装内核、网络与其他表单仍按字符列/行数适配：至少 **110×30** 使用最大宽度 **86** 的居中 Modal，内容较少时收紧高度；否则使用当前终端区域中的同一单列表单。至少 **100×20** 显示 22 列宽的常驻侧栏，否则使用 Menu。常驻侧栏 Logo 区使用深色背景，右侧标题区与底栏使用相同的正文背景色，标题分隔线与 Logo 区底边对齐；Logo 使用加粗强调色，与页面标题保持同一基线，终端至少 24 行时顶栏增加对称的上下留白。常驻侧栏第一项菜单紧接顶栏底边，右侧正文在标题分隔线下保留一行留白。菜单连续等高排列，项间不留空行。常驻侧栏与 Menu 弹出菜单均固定每项高 3 行，文字上下各留 1 行；窗口变矮时只缩小菜单可见区域，以独立滚动访问其余页面，并显示上下溢出提示。鼠标在菜单区滚动时不影响正文或切换页面；↑↓、Home/End 同步切页并自动滚动到完整菜单项，Enter 进入内容，1–7 直接切页并进入内容。Tab 将导航作为一个整体，重新进入时定位当前页。极小弹出菜单无法同时容纳三行菜单项与溢出提示时才降为单行。常驻侧栏底部在空间充足时仅显示实时上传、下载速率，切换页面时持续更新；无可用样本时显示 `--`，真实零速率显示 `0 B/s`。空间不足时隐藏，不挤占菜单。Overview 与 Proxies 使用侧栏右侧的全部可用宽度，其他页面正文最大 112 列；顶栏对齐正文右缘并展示页面专属内容：Overview 显示真实内核状态；Profiles 提供 Update all，Proxies、Connections、Rules 提供刷新与可用数据计数，Logs 提供采集控制，Settings 提供下载方式入口。标题栏操作保持可见，窄窗口使用短标签或单独一行。底栏显示时根据焦点提示可用快捷键，不常驻刷新成功消息；隐藏后释放一行给正文和侧栏，同时重算滚动与鼠标区域，保留当前操作位置。Overview 控制区优先展示内核和模式，按可用宽度显示一、二或四列；配置、代理组及信息区在正文至少 76 列且终端至少 24 行时双列，否则单列。标题、正文滚动区和底部操作分开。极小窗口显示扩大终端提示，仍可返回或取消。缩放不重置选项或重启任务。

首次进入 TUI、界面就绪约 200ms 后，常驻侧栏的 `la💤yClash` 会播放一次“翻个身，接着睡”：约 800ms 内，中间两格由小 `z` 变成大 `Z` 再回到 💤，配合青色渐亮再恢复。每次启动仅一次，切页、缩放或关闭弹窗不会重播；首次以窄窗口或无色/16 色模式启动时跳过。点击 Logo 可播放约 1.6 秒的彩蛋：惊醒 `!!`、发呆 `..`、重新睡着，再从 💤 上方冒出两口 `z/Z` 梦泡，第二口更淡。字标位置固定，20～23 行的短窗口只播放中间两格的变化。点击优先播放完整剧情，再次点击从头播放；弹窗、忙碌或侧栏隐藏时结束动画，平时保持静止。

## Profiles 与实时日志

Proxies 使用全部可用正文宽度，正常宽窗口中分组之间不额外留空行，不绘制横线；窄屏和短窗口保留一行分隔。组头展示组名、当前最终节点、必要的「自动」标记和右对齐的成员数徽标；折叠箭头位于右侧，整块组头连续高亮，正常宽窗口上下各留一行，短窗口保持紧凑。宽屏支持多组独立展开，并可全部收起；完整类型与引用路径在详情中查看。正常宽窗口的组头与首排卡片之间不额外留空行，窄屏和短窗口保留一行间隔；节点卡片按实际正文宽度和最小 36 列卡片展示一至四列，窄屏成员页也可多列，少量成员仍保持对应列宽；缩放时直接重排并保留节点焦点。卡片以顶部一行留白和两行内容组成，相邻节点排之间空一行，右侧保留两格留白，不重复显示当前选择或默认排序行；卡片保留协议与延迟，左侧竖条贯穿全高并标识内核已确认的选择。延迟数值与状态在右侧固定区域内居中，点击可单独重测该节点并更新结果，不切换节点。模式与搜索在宽屏同行，测速与非默认排序入口位于展开组的标题行，进度按需出现。组内连续滚动并显示可见成员范围，不分页；窄窗口采用组目录、成员两级浏览。方向键优先访问同排相邻节点，右边界的引用组可继续进入，也可直接使用卡片的「进入 →」；宽屏就地跳转，返回恢复搜索、焦点与滚动位置。Selector 可手动改选，自动组只读查看成员，DIRECT、REJECT 也属于有效选择；支持搜索、各组独立排序和可取消的当前组测速，刷新状态不会触发测速。操作与数据边界见 [代理组与节点](docs/proxies.md)。Profiles 统一管理订阅下载、本地 YAML 和默认配置。每个 Profile 自己保存地址与内容版本，不需要另行管理来源。现有 CLI `nodes`、`profile` 等业务命令保留原有职责。

Profiles 顶部提供来源输入、Add subscription、New profile 与 Update all。Add 继续使用自动命名；New profile 可为远程 HTTPS 或本地文件填写显示名称、描述，并保存来源供后续更新。来源和本地路径在编辑时正常显示，方便检查和修改；保存后的卡片仅显示来源标签。配置以响应式卡片展示来源、更新时间、覆写和待应用状态；正文至少 86 列时双列，否则单列。左键单击卡片直接应用该配置；点击当前配置时，有待应用更改才重新加载，没有更改则保持现状。未安装内核时打开安装提示，不执行应用；内核停止时切换配置仍保持停止。`a` 聚焦来源输入，方向键或 `hjkl` 按卡片位置移动，`Enter` 打开详情，也可通过 Use profile / Apply changes 加载配置。`Tab` 可访问并滚动到所有操作，`Esc` 关闭详情。更新按钮和导入覆写仅保存草稿，点击卡片的更新按钮不会同时切换配置；所有配置操作直接针对 Profile。页面展示已保存的描述等元数据，不显示未经获取的流量配额或到期时间。

New profile 支持 HTTPS URL、绝对路径、`~/` 和相对路径。输入本地路径时实时列出当前目录中匹配的文件与文件夹，`↑↓` 选择、`Tab` 补全、`Enter` 提交；目录只继续浏览，不提交为配置。普通文件不限制扩展名，创建配置会保存来源副本，不修改原文件或自动应用。路径属于 lazyclash 进程运行环境，SSH 中指远程主机的文件。

Profiles 卡片用底色与间距分组，已应用配置显示蓝色左侧条，键盘焦点使用 `›` 与底色变化，待应用更改使用橙色文字。

右键卡片、点击卡片右上角 `⋯`，或聚焦卡片后按 `m` / `Shift+F10`，可打开同一个操作菜单：应用、更新、重命名、修改地址、导入覆写、查看详情与删除。`↑↓` 选择、`Enter` 执行，`Esc` / `Tab` 或点击外部关闭；关闭菜单的点击不会切换背景配置。不可用操作显示原因，当前配置禁止删除；更新仍仅保存草稿。小窗口中的菜单自动避让边缘并滚动显示。

输入 URL 后选择 Add 即下载并保存，不再要求填写名称。名称依次采用用户明确指定的名称、`profile-title`、`Content-Disposition` 文件名、URL 路径、Host 或固定兜底值；重复名称添加 ` (1)`、` (2)`。自动名称随有效的服务端名称更新，缺失名称头时保留原名；详情中的 Rename 标记为自定义，后续刷新不覆盖。Edit URL 会下载并校验新来源，自动名称重新推导，自定义名称保留。显示名称与稳定 ID、磁盘文件路径分离，改名不重新生成或应用配置。完整规则见 [配置设计](docs/subscription-design.md#订阅显示名称)。

Logs 通过当前 Mihomo 的认证本机控制 API 获取实时事件，默认只在内存保留最近 200 条脱敏记录，不启用原始 journal、不落盘保存日志。`f` 暂停/继续，向上滚动或搜索会暂停采集，`End` 恢复跟随，`g` 重连。暂停、离页期间的事件不会补回，重新采集显示间隔提示。需要内核运行且 API 可用；没有历史启动日志。

CLI 使用同一日志用例，例如 `lazyclash logs --lines 50 --duration 5s`，在有界时间内采集最多指定条数并输出 JSON。日志来源、容量、取消和脱敏边界见 [日志查看](docs/logs.md)。

## 运行端口设置

已安装 Mihomo 后，在 **Settings → Runtime settings / Ports** 修改本机 HTTP/SOCKS 代理端口和控制 API 端口。先检查并审阅，再确认应用；运行中显示 **Apply & restart** 并说明连接中断，停止状态提交后仍保持停止。两个端口必须不同，范围为 1024–65535；检查代理 TCP/UDP 与控制 API TCP 冲突，不停止占用端口的其他进程。

CLI 使用同一事务用例：`lazyclash settings show` 查看，`lazyclash settings ports --mixed-port 17897 --controller-port 19097 --dry-run` 预检查；运行中应用需显式加 `--restart`。失败或取消执行回滚；恢复失败则保留恢复记录并报告。API 密钥、原始 profile 和其他配置保留。父 Shell 已导出的代理变量需重新求值 `lazyclash env`。详细操作与恢复边界见 [运行端口设置](docs/runtime-settings.md)。

## 版本与安装

首次安装区复用升级功能的只读 `CheckUpdate`，概览就绪后从官方 `version.txt` 查询最新稳定版；查询不下载内核。点击 **下载并安装** 后固定这次看到的版本，按设备构建命名规则生成官方 Release 文件地址，不请求 GitHub Release API。Specific version 可输入稳定标签，例如 `v1.19.33`；也可直接使用本地文件。尚未支持 pre-release 安装和其他内核。

发布构建按设备自动适配：arm64 使用对应资产；Linux amd64 结合所有可见 CPU 的指令集与操作系统 AVX 支持选择 v3、v2 或 v1。信息不足时选 v1，旧版本缺少推荐资产时向兼容等级回退；不会给低等级 CPU 选择旧的无标签 v3 资产。使用不超过 4 KiB 的 Range 请求确认候选文件，仅 HTTP 404/410 时继续尝试兼容构建；网络、权限或无效文件响应直接报错。规则依据 [Mihomo FAQ](https://github.com/MetaCubeX/mihomo/wiki/FAQ)，当前仍只支持 Linux arm64/amd64。

首次安装按一次 **下载并安装** 即串联资产解析、预检和现有安装管线。预检验证目标可写、unit 所属关系、磁盘、平台和已有状态；阻断原因会展示，提交仍在进程锁内复检。已有实例的手动版本更换或重装保留实际计划审阅，显示当前/运行版本、目标资产、路径、操作和警告。

安装显示真实下载字节、已知总量/百分比、速率，以及解压、可执行文件检查和部署阶段。总量来自 HTTP 响应，未知时使用不确定状态；详情可展开。失败保留选择，可重试或修改网络；修改网络只重新检查，仍须主动点击安装。取消、改选版本或本地文件会清除待安装意图，迟到结果不会继续安装。

普通 `install`、手动换版和显式重装均**不会自动启动或重启服务、修改系统代理、启用 TUN 或设置自启动**。同版本默认 Use existing，只有明确选择 Reinstall 才替换。这些流程先暂存并验证，再原子替换路径；正在运行的进程继续持有旧 inode。配置、API secret 与 unit 不因换版而覆盖。结果和首页分别显示已安装版本与实际运行版本，普通安装后切换运行版本需要显式重启。

已安装实例的 Settings → Manage / 管理内核提供**手动检查更新**与确认升级。检查只读地获取实际安装版本及官方 `version.txt`，区分已最新、有更新、本地更高和无法比较；确认升级固定目标，原服务运行时重启并验证，失败恢复旧文件及运行状态，停止时保持停止。Alpha 支持识别与检查，不比较 hash 先后，本期不提供 Alpha 一键升级。进入已安装实例的管理页不自动请求网络。架构、校验与恢复限制见 [内核版本管理](docs/core-updates.md)。

没有订阅也可启动：默认 `PROXY` 选择组包含 `DIRECT` 和 `REJECT`，不依赖 Geo 数据，也不会凭空提供远程节点。添加配置只校验保存，不自动应用；重复显示名称会去重，新导入不会覆盖已有配置。应用前执行内核校验，运行状态下应用失败恢复旧配置和原服务，停止状态下应用不隐式启动。

## 下载方式

默认直连。下载方式互斥三选一，安装和升级共用设置：

| 方式 | 连接行为 |
| --- | --- |
| 直连 | 请求原始 GitHub 地址，忽略代理环境变量 |
| 使用环境代理 | 读取 HTTP(S)_PROXY、ALL_PROXY、NO_PROXY 等标准环境变量；不是操作系统原生代理检测 |
| GitHub 加速 | 在完整原始 URL 前添加用户填写的 HTTP(S) 前缀，忽略代理环境变量 |

只有 GitHub 加速显示可编辑的“加速地址”。前缀去除首尾空格、统一末尾一个 `/`，再直接拼接完整 URL，例如 `https://accelerator.example.invalid/https://github.com/MetaCubeX/mihomo/releases/download/...`；保留内层 `https://`，不整体编码 URL。前缀不接受凭据、查询参数、片段或模板占位符。界面可展开预览，未知私有路径脱敏。不会自动选择公共加速站或修改操作系统网络设置。

版本文本、安装包探测及下载全部遵循同一方式，没有静默直连回退。环境方式显示是否检测到代理及是否被 NO_PROXY 绕过，诊断不展示代理凭据；每个客户端重新读取环境，localhost/loopback 按规则直连。

连接测试分别显示“版本信息”和“安装包连接”。版本测试复用 `CheckUpdate` 并固定版本解析设备资源；安装包探测最多读取前 4 KiB，检查响应状态、长度和 gzip 头，不代替完整下载、解压及执行版本检查。修改草稿会使测试结果失效，不会逐字符联网。

返回放弃草稿；应用默认只影响当前会话，勾选“记住下载偏好”才保存可安全持久化的设置。修改下载方式后只重新检查版本，仍需主动点击安装。

安装与升级不依赖 `api.github.com`，也不获取上游预期 SHA256 或签名。HTTPS 请求保持 TLS 证书验证；明确配置 HTTP 加速前缀时，该段连接没有 TLS 保护。下载限制为 64 MiB，解压后的可执行文件限制为 128 MiB，完整读取 gzip 检查格式与 CRC，并执行 `-v` 精确核对所选版本，再由既有事务验证配置并发布。这些检查可以发现传输损坏、格式及版本错误，不能独立证明上游真实性；安装记录中的本地 SHA256 用于后续文件变化检测和恢复，不是官方校验值。信任范围包含所选下载站或加速站。

## 持久运行与权限

普通用户通过 systemd user service 运行，Start/Restart/Enable 要求已明确启用 linger；root 通过 systemd system service 运行，不依赖登录会话或 linger。安装不启动、不 enable、不修改 linger，TUI 退出或终端挂断不发送 Stop。运行意图、模式、TUN 策略、节点缓存和显式订阅来源持久保存。`service enable/disable` 独立控制自启动；多步管理事务中断后，遗留 journal 在下一次应用调用时恢复，没有后台 daemon 或独立事务执行器代跑。详细行为见 [持久运行](docs/runtime-lifecycle.md)。

system Mihomo 使用专用非 root 账号；普通代理无额外 capabilities，明确开启 TUN 才授予 `CAP_NET_ADMIN CAP_NET_RAW`。TUI 默认取消的弹窗和 CLI `tun on --scope host --confirm-host` 均说明整台主机、其他用户和服务的流量及 DNS 影响。停止时开启仅保存策略；运行时切换会重启内核。启动及退出由同一受保护 lazyclash 程序的有限 unit 钩子检查冲突、记录并清理自有接口/路由，不自动抢占其他实例或 VPN。TUN 的 DNS 劫持不修改系统 resolver；系统代理自动设置、普通用户 TUN 和 UID 筛选不在首发范围。权限细节与限制见 [权限管理](docs/permissions.md)。

```sh
# 已安装到受保护路径；始终操作独立 root 实例
sudo /usr/local/bin/lazyclash
sudo /usr/local/bin/lazyclash tun on --scope host --confirm-host
sudo /usr/local/bin/lazyclash tun off
```

## CLI 与 Shell

CLI 和 TUI 共享 `ResolveRelease → PrepareInstall → InstallCore`，以及网络客户端、预检查、配置验证、原子部署与恢复。TUI 不启动自身 CLI，安装业务不在 shell 脚本中。

```sh
lazyclash install --core-version latest
lazyclash install --core-version v1.19.33 --dry-run
lazyclash install --bin /absolute/mihomo
lazyclash install --core-version v1.19.33 --reinstall
lazyclash install check                         # 只读检查，不下载内核或重启
lazyclash install check --channel alpha         # Alpha hash 不同显示无法比较
lazyclash install upgrade --yes                 # 检查并确认正式版升级，必要时重启
lazyclash install --download-method accelerated \
  --acceleration-prefix https://accelerator.example.invalid/
lazyclash profile add /absolute/config.yaml --name local
lazyclash profile apply local
lazyclash profile add https://example.invalid/sub --remember-source  # 自动命名
lazyclash profile add https://example.invalid/sub --name '我的机场' --remember-source
lazyclash profile list                 # 查看 id、name、name_source
lazyclash profile rename PROFILE_ID '新名称'
lazyclash profile url PROFILE_ID https://example.invalid/new-sub
lazyclash service start
lazyclash service stop
lazyclash service restart
lazyclash nodes
lazyclash select PROXY DIRECT
lazyclash stats                  # 单次采样；停止/未安装时输出明确 State 和 null 指标
lazyclash connections            # 运行中的活动连接 JSON
lazyclash connections close ID   # 明确请求关闭一条连接
lazyclash rules                  # 当前内核规则，JSON 按原顺序输出
lazyclash status
lazyclash doctor
```

网络 flags：`--download-method direct|environment|accelerated`、`--acceleration-prefix URL`、`--remember-download-settings`。未指定的设置复用已保存偏好。

子进程和 TUI 无法修改父 Shell；显式求值：

```sh
eval "$(lazyclash env)"
eval "$(lazyclash env --unset)" # 配置损坏或 namespace 无效仍可清理

eval "$(lazyclash init --shell zsh)" # bash 同样支持；只输出函数，不编辑启动文件
lazyclash_on   # 启动成功后设置当前 Shell 环境
lazyclash_off  # 清除环境，不停止服务
```

默认服务名为 `lazyclash-mihomo.service`，user/system manager 各自管理，不共享配置或安装记录。API 默认 `127.0.0.1:9097`、mixed 默认 `127.0.0.1:7897`；并存实例须调整端口，冲突时不会停止其他服务腾位置。API 密钥自动生成和注入，状态不输出密钥。服务 stdout/stderr 丢弃，避免 provider URL 凭据进入 journal；退出原因通过 systemd Result、退出状态和重启计数查询。

普通用户默认数据目录为 `$XDG_DATA_HOME/lazyclash`（或 `~/.local/share/lazyclash`），可用 `--home ABS` / `LAZYCLASH_HOME` 和 `--service-name lazyclash-NAME` / `LAZYCLASH_SERVICE` 隔离实例。root 默认 `/var/lib/lazyclash/<service-name>`，忽略这两个用户环境变量及 XDG 数据目录，按显式 flags 选择独立路径和服务。系统数据及祖先必须受 root 保护，且不能位于 unit 沙箱隐藏的 `/tmp`、`/var/tmp`、`/home`、`/root`、`/run/user` 下；仅运行缓存由专用服务账号写入。配置、原始订阅、偏好与恢复记录按必要权限私密保存。进程锁等待可取消，锁文件不删除；检测到外来文件、unit/drop-in 或归属变化时保留现场并报错。

## 开发与验证

```sh
make check
make race
make i18n-check
make build
make package VERSION=dev
python3 -m unittest discover -s scripts -p 'test_*.py'
orb list
./scripts/verify-linux.sh "$VM"
./scripts/e2e.sh "$VM"
# 双作用域测试另传打包后的可执行程序及 VM 内明确选择的 Mihomo
./scripts/service-scopes-e2e.sh "$VM" /absolute/packaged/lazyclash /absolute/vm/mihomo
```

普通测试仅用临时目录、fixture 与 fake 服务，不创建 VM 或修改真实代理。真实 Linux、PTY 与 Mihomo/systemd 测试显式执行，使用临时目录、随机专用 unit、冲突检查和失败清理，详情见 [验证说明](docs/verification.md)。未实测平台不得由交叉编译推断为已通过。

程序入口为根目录 `main.go`。`internal/cli` 保留 Cobra 子命令与 Shell 集成，`internal/tui` 负责交互与响应式渲染；二者直接调用 `internal/app` 的共用用例，包括运行采样和单连接关闭。`internal/downloadnet` 负责来源与连接，`internal/mihomo` 负责发布、下载、降权校验、TUN 配置和控制 API，`internal/service` 负责 systemd user/system 服务及有限 TUN 生命周期，`internal/store` 负责作用域数据路径、原子文件与进程锁。具体边界见 [代码结构](docs/architecture.md)。

[路线图](docs/roadmap.md)。参考旧项目仅用于只读研究，不读取旧运行配置，不继承历史、提供旧别名或实现迁移层。
