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

在 macOS 终端执行 `./scripts/dev.sh` 可进入现有的专用 OrbStack 虚拟机；进入后直接输入 `lazyclash`，每次都会先在 macOS 上构建最新代码，再在虚拟机里运行。修改代码后只需退出 lazyclash 并重新执行 `lazyclash`，无需退出虚拟机或重新执行脚本；构建失败时显示错误，不启动旧版本。脚本自动选择唯一的 `lazyclash` / `lazyclash-*` 虚拟机并检测其架构，构建产物为 `bin/linux-架构/lazyclash`，开发启动器位于其 `dev/` 子目录，仅将启动器目录加入当前虚拟机 Shell 的 PATH。程序使用正常默认数据目录和服务名，安装及启动由你在 TUI 中操作。多个专用虚拟机时可用 `./scripts/dev.sh VM_NAME` 选择；`exit` 返回 macOS。

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

安装后可直接从概览启动内核。尚未选用订阅时，概览显示「未使用订阅」。代理端口、控制端口、已选择的代理模式和已授权的 TUN 设置属于本机实例，切换订阅后继续生效。

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

Overview 自适应撑满内容区，展示 Core、Rule / Global / Direct 模式、当前订阅、代理组、流量、内核与系统信息。启停、重启、模式和 TUN 操作调用共享应用用例，完成后重新读取状态；运行值与保存值分别显示。环境卡明确显示 user/system scope；普通用户 TUN 显示不支持，root 系统实例提供整台主机 TUN 的确认入口。系统代理标为 Unmanaged，不显示虚假的关闭状态。宽窗口使用多列卡片，窄窗口逐步收为单列；运行指标按空间分两列或四列。运行后自动展示实际上传/下载速率、活动连接数、本次内核运行累计流量及内核 RSS；未安装、停止、正在连接、部分数据不可用和 API 不可达分别反馈，不把缺失数据显示为零。

## 运行监控与连接

TUI 在后台通过 Mihomo 控制 API 持续接收流量、内存、连接和日志。切页和收起侧栏时，共享数据会话继续运行。内核停止或重启后，会话根据实际进程重新连接。

`Upload / download` 来自 `/traffic` 最近一秒的字节样本，按 B/s 展示。`Since core start` 直接读取 `/connections` 的 `uploadTotal`、`downloadTotal`，包含已关闭连接的已记录流量；只统计 Mihomo 纳入计数的流量，不是整台机器的网络用量。内核重启或统计被清零时归零，关闭 TUI 不清零。当前不持久化跨重启、每日或每月流量，也不通过速率积分推算累计值。

Connections 显示当前活动连接，可搜索目标域名/IP/端口、进程、规则或连接 ID：

- `/` 或 Search：输入搜索，`Enter` 完成，输入中的 `q`、数字不触发全局操作。
- `↑/↓` 选择连接，`Enter` 查看来源、目标、规则、代理链、进程和字节详情；长内容可滚动。
- 在活跃列表按 `x` 打开详情，核对目标后点「关闭此连接」立即断开。键盘可用 `Tab` 聚焦该按钮，再按 `Enter` 或空格执行。`Esc` 收起详情。
- 页头「关闭全部」会先询问确认，默认选中取消。确认后会关闭所有活跃连接，包括当前搜索和暂停视图之外的连接。

列表会自动更新。单条关闭请求会核对连接 ID 和开始时间，操作结果随内核的后续观测更新。

Rules 主列表只读展示内核加载的规则并保留匹配顺序，支持搜索、原位展开、异步读取与失败重试。页头可查看并更新规则集合，以及增改删、排序当前 Profile 的自定义规则；自定义规则独立持久化，订阅更新后保留，保存与应用分开。Rule 模式才按规则顺序匹配。操作与数据含义见 [规则管理](docs/rules.md)。

首次安装在概览正文中完成，保留侧栏和标题，面板高度随内容变化；更多选项折叠收纳下载方式、指定版本和本地可执行文件。查询失败就地提供重试与下载方式设置，安装按钮在没有可用版本时不可用。检查、下载、校验和完成状态在同一区域更新，完成后主动选择 **启动内核** 才会启动。管理已安装内核、网络与其他表单仍按字符列/行数适配：至少 **110×30** 使用最大宽度 **86** 的居中 Modal，内容较少时收紧高度；否则使用当前终端区域中的同一单列表单。终端至少 **115×9** 时显示 20 列宽的常驻侧栏，更小的窗口使用 Menu。常驻侧栏 Logo 区使用深色背景，右侧标题区与底栏使用相同的正文背景色。Logo 使用加粗强调色，与页面标题保持同一基线；页标题使用近白色粗体和蓝色短竖线。终端至少 24 行时顶栏为 3 行，上下各留 1 行，更矮窗口使用紧凑顶栏。常驻侧栏的第一项紧接 Logo 区底边。右侧正文紧接三行顶栏，使用紧凑顶栏时在正文前留一行。菜单连续等高排列，项间不留空行。常驻侧栏和 Menu 抽屉在空间足够时每项高 3 行。窗口变矮时菜单先收为每项 1 行，再通过独立滚动显示其余页面。鼠标在菜单区滚动时不影响正文或切换页面；↑↓、Home/End 同步切页并自动滚动到完整菜单项，Enter 进入内容，1–7 直接切页并进入内容。Tab 将导航作为一个整体，重新进入时定位当前页。

各页的常驻侧栏底部都居中显示内核状态，例如「● 运行中」。能确认当前进程的启动时间且高度足够时，状态上方显示精确到秒的运行时长，例如 `09:02:30`。窗口变矮时优先保留导航和状态。侧栏收起后，页头显示简短状态。上下行速率可在概览的「实时流量」卡查看。

各页正文使用扣除侧栏和页边距后的可用宽度；顶栏对齐正文右缘并展示页面专属内容：Profiles 提供 Update all，Proxies 提供模式切换、按需搜索与代理集合，不显示组总数和手动刷新，Connections 提供刷新与活动连接数，Rules 提供规则集合入口，Logs 提供采集控制，Settings 提供下载方式入口。标题栏操作保持可见，窄窗口使用短标签或单独一行。底栏显示时根据焦点提示可用快捷键，不常驻刷新成功消息；隐藏后释放一行给正文和侧栏，同时重算滚动与鼠标区域，保留当前操作位置。概览正文达到 90 列时，内核状态与当前订阅并排显示，较窄时依次堆叠。「内核状态」卡首行显示内核名称 Mihomo，下一行显示进程状态。侧栏容不下秒表时，卡内显示「已运行 1 小时 2 分钟」等时长，满一天后显示天和小时。安装版本与运行版本不一致时，卡内会列出两者。精确运行时长和版本可在「设置 → 内核管理 → 管理 / 更新」查看。点击标题旁的「启动」或「停止」，或聚焦按钮后按 Enter / 空格，即可启停内核。标题、正文滚动区和底部操作分开。极小窗口显示扩大终端提示，仍可返回或取消。缩放不重置选项或重启任务。

Logo 使用柔和的月光蓝到薰衣草紫渐变。首次进入 TUI、界面就绪约 200ms 后，常驻侧栏的 `la💤yClash` 会播放一次“翻个身，接着睡”：约 800ms 内，中间两格由小 `z` 变成大 `Z` 再回到 💤，配合整条渐变提亮再恢复。每次启动仅一次，切页、缩放或关闭弹窗不会重播；首次以窄窗口或无色/16 色模式启动时跳过。点击 Logo 可播放约 1.6 秒的彩蛋：惊醒 `!!`、发呆 `..`、重新睡着，再从 💤 上方冒出两口 `z/Z` 梦泡，第二口更淡。字标位置固定，20～23 行的短窗口只播放中间两格的变化。点击优先播放完整剧情，再次点击从头播放；弹窗、忙碌或侧栏隐藏时结束动画，平时保持静止。

## Profiles 与实时日志

Proxies 使用全部可用正文宽度。组头展示组名和当前最终节点，右侧按列显示 Mihomo 策略组类型、纯数字成员总数和折叠箭头。类型包括手动选择、自动选择、自动回退、负载均衡和链式代理。宽屏支持多个组原地展开，也可全部收起。正常高度的宽窗口保留三行完整的组头悬停高亮，窄屏和短窗口使用紧凑组头。节点卡片随正文宽度排成一至四列，多列时每张至少宽 29 格。150 列终端可显示四列，窄屏成员页也能按宽度分列。少量成员保持相同列宽，缩放时保留节点焦点。卡片首行显示名称和右对齐的延迟，次行显示协议、UDP 支持或引用摘要。宽且高的真彩色或 256 色窗口里，首排卡片紧接组头，两行文字上下各留半行，相邻两排卡片之间露出一行背景。低色和无色终端的宽窗口在排间空一行，窄屏和矮窗连续排列。已选节点以蓝色卡面和左侧竖条标识。延迟低于 150 ms 时显示绿色，达到 150 ms 时显示黄色。点击延迟数值或状态可单独重测该节点并就地更新结果。页头右侧将规则、全局、直连并排为三个连续等宽、可独立操作的分段，与搜索和代理集合入口相隔四格。切换模式时按钮保持原位，窄屏可分行并固定在列表上方。有多个匹配成员的展开组在组头提供「排序」，可测速的组就地显示「测速」、进度和取消操作。成员连续滚动，部分成员在屏幕外时显示可见范围。多个展开组的结果合并计数，显示「跨 2 组 · 1–29 / 49」等范围。窄窗口采用组目录、成员两级浏览。方向键优先访问同排相邻节点，右边界的引用组可继续进入，也可直接使用卡片的「进入 →」；宽屏就地跳转，返回恢复焦点与滚动位置。在 Selector 中可点击节点或按 Enter 改选，点击自动组的节点只移动焦点。DIRECT、REJECT 也属于有效选择。支持各组独立排序和可取消的当前组测速，应用配置或切换模式后自动同步代理组。点击「搜索」或按 `/` 展开单行输入框，可按组名、组类型（如「自动选择」或 `url-test`）、节点名和协议筛选。按 Esc 关闭搜索并恢复原来的浏览位置。内核确认 Direct 模式后隐藏策略组及节点操作，正文仅居中显示「所有流量均直接连接。」；切回原模式恢复展开、排序与浏览位置。代理集合与规则集合复用管理面板，支持查看来源、单项或批量更新；集合更新后自动同步主列表，Inline/Compatible 来源只读。操作与数据边界见 [代理组与节点](docs/proxies.md)。Profiles 统一管理远程订阅和本地 YAML。每个 Profile 自己保存地址与内容版本，不需要另行管理来源。现有 CLI `nodes`、`profile` 等业务命令保留原有职责。

概览的「当前订阅」显示所选条目的名称、描述或来源、更新时间，以及服务商提供的流量余额和到期信息。尚未应用、更新未生效或等待校验时，名称下方会显示相应提示。未选用订阅时显示「未使用订阅」。通过「订阅 ›」可进入订阅页管理条目。

订阅页（Profiles）只列出用户保存的远程订阅和本地配置。空库时，点击输入框或按 `a` 输入订阅链接或本地 YAML 路径。停用当前订阅会保留它的卡片和来源。删除当前订阅时，可在同一个确认框内选择接替配置，或选择不使用订阅。后者会移除运行中的订阅节点和规则，流量可能直连。内核已停止时，选择在下次启动生效。

订阅页输入链接或本地文件路径后，「导入」按钮点亮，导入的配置会自动命名。「新建」可填写来源、显示名称和描述。两种方式都会保存来源，之后可通过「全部更新」重新获取。来源和本地路径在编辑时正常显示，方便检查和修改；卡片第二行优先显示单行描述，未填写时显示域名或文件类型，右侧显示更新时间。配置以响应式卡片展示额度和校验状态；网格按正文宽度排一至四列，在 98、148、198 个字符格时依次增加一列。同一窗口下，增删订阅时卡宽保持一致。左键点击其他卡片会切换配置，点击当前项保持现状。未安装内核时打开该配置的设置；内核停止时切换配置仍保持停止。`a` 聚焦来源输入，方向键或 `hjkl` 按卡片位置移动，`Enter` 打开此订阅的设置。`Tab` 可访问并滚动到所有操作，`Esc` 关闭设置。点击更新会重新读取来源。当前订阅生成的运行配置有变化时自动热重载，失败时回退；刷新其他订阅只更新本地副本。内核已停止时，新配置留待下次启动。保存当前订阅的覆写也会自动加载有效变化。卡片将最近一次成功获取的时间靠右显示。订阅响应提供流量额度时，卡片显示剩余量和总量；进度条越短，剩余额度越少。到期日按服务商提供的时间单独展示。

「新建」表单支持 HTTPS URL、绝对路径、`~/` 和相对路径。输入本地路径时实时列出当前目录中匹配的文件与文件夹，`↑↓` 选择、`Tab` 补全、`Enter` 提交；目录只继续浏览，不提交为配置。普通文件不限制扩展名，创建配置会保存来源副本，不修改原文件或自动应用。路径属于 lazyclash 进程运行环境，SSH 中指远程主机的文件。

点击订阅地址框右侧的 `⎘`，或用 `Tab` 聚焦后按 `Enter`，可将剪贴板文字插入光标处。窗口过窄时，图标移到框下右侧。确认后再添加订阅。Ghostty 可能弹出读取授权，SSH 会话也可使用。终端拒绝或未返回内容时，输入保留，框下提示改用终端自带的粘贴快捷键。

Profiles 卡片用底色与间距分组，已选配置用左侧竖条标记，键盘焦点使用 `›` 与底色变化。

右键卡片、点击卡片右上角 `⋯`，或聚焦卡片后按 `m` / `Shift+F10`，可打开同一个操作菜单。「设置…」集中编辑名称、描述和来源，显示已有的更新时间、流量和到期日期，也可从文件导入覆写或停用当前订阅。本地来源显示「重新读取」，一次性导入的副本省略更新操作。`↑↓` 选择、`Enter` 执行，`Esc` / `Tab` 或点击外部关闭；关闭菜单的点击不会切换背景配置。删除当前订阅时，可在确认框中选择接替配置或停用订阅。小窗口中的菜单自动避让边缘并滚动显示。

「编辑配置…」打开本地来源的原文件，或打开远程订阅及一次性导入配置的私有副本。远程订阅下次更新会替换对完整配置的修改。需要长期保留的规则可写在「编辑覆写…」中。全局「设置 → 外部编辑器」可指定命令和参数，留空时依次尝试 $VISUAL、$EDITOR、nano、vi。在 SSH 中使用远端编辑器。退出编辑器后自动校验改动，失败时可继续编辑或重试。

输入订阅链接后选择「导入」即可下载并保存。名称依次采用用户明确指定的名称、`profile-title`、`Content-Disposition` 文件名、URL 路径、Host 或固定兜底值；重复名称添加 ` (1)`、` (2)`。自动名称随有效的服务端名称更新，缺失名称头时保留原名；在「设置…」中更改名称会标记为自定义，单改描述保留自动命名。更改来源会先读取并校验新内容，自动名称重新推导，自定义名称保留。显示名称与稳定 ID、磁盘文件路径分离，改名不重新生成或应用配置。完整规则见 [配置设计](docs/subscription-design.md#订阅显示名称)。

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

概览 → 网络设置 → **终端代理**控制当前交互式 Shell。首次开启时确认安装 Shell Integration，退出 TUI 后按提示执行一次 `source`；这次 source 会立即应用已保存请求，无须重新进入 TUI。以后直接运行 `lazyclash`，切换后退出即自动同步，关闭会恢复接管前的原始变量。

```sh
lazyclash shell status           # 当前 Shell、安装/加载/回执与代理状态
lazyclash shell status --json
lazyclash shell uninstall        # 当前 Shell 恢复后移除自己的初始化引用
```

第一阶段支持 Linux 的 Bash、Zsh、Fish 交互会话；每个终端独立，不包装 `lazyclash` 命令。安装脚本位于 `$XDG_CONFIG_HOME/lazyclash/shell`，状态位于 `$XDG_CACHE_HOME/lazyclash/shell`。当前 Shell 未加载时显示“待激活”，也可在卡片中查看激活命令。详见 [Shell Integration](docs/shell-integration.md)。

手动输出 POSIX exports 的 `env` 仍可用于自动化；它不会保存或恢复原始变量，也不属于上述会话集成。旧 `init`、`lazyclash_on`、`lazyclash_off` 已移除。

```sh
eval "$(lazyclash env)"
eval "$(lazyclash env --unset)" # 手动清理；配置损坏时仍可输出
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

`third_party/ultraviolet` 保存固定的上游源码与中文行重绘补丁，`go.mod` 指向仓库内副本，构建时需要保留该目录。上述 `make test/check/race` 和 Linux 验证入口包含该依赖的原有测试，补丁维护方式见 [说明](third_party/ultraviolet/LAZYCLASH.md)。

程序入口为根目录 `main.go`。`internal/cli` 保留 Cobra 子命令与 Shell 集成，`internal/tui` 负责交互与响应式渲染；二者直接调用 `internal/app` 的共用用例，包括运行采样和单连接关闭。`internal/downloadnet` 负责来源与连接，`internal/mihomo` 负责发布、下载、降权校验、TUN 配置和控制 API，`internal/service` 负责 systemd user/system 服务及有限 TUN 生命周期，`internal/store` 负责作用域数据路径、原子文件与进程锁。具体边界见 [代码结构](docs/architecture.md)。

[路线图](docs/roadmap.md)。参考旧项目仅用于只读研究，不读取旧运行配置，不继承历史、提供旧别名或实现迁移层。
