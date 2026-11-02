# lazyclash

以 TUI 为主要交互的 Mihomo 终端代理管理工具。Go module：`github.com/nelvko/lazyclash`。

项目目标同时支持 root 和普通用户：各自拥有独立实例，普通用户默认使用自己的 user service，经 sudo 授权后可为本实例启用特权执行后端和 TUN。普通代理可并存，同一网络命名空间内的 lazyclash TUN 管理权独占，流量覆盖范围单独配置。当前实现尚限于非 root 用户服务；目标设计见 [服务 scope 设计](docs/service-scopes.md)。项目处于开发阶段，不承诺旧数据格式或服务定义兼容。

## 构建与使用

需要 **Go 1.26+**。固定稳定依赖：Bubble Tea **2.1.1**、Bubbles **2.2.1**、Lip Gloss **2.0.6**，统一使用 `charm.land/*/v2`；Cobra **1.10.2**。完整版本与校验记录在 go.mod/go.sum。

```sh
make build
./bin/lazyclash
# 或在源码根目录直接运行
go run .
```

服务与内核安装当前支持 **Linux/systemd 用户服务、arm64 与 amd64**。macOS 可构建、运行 TUI、配置下载网络及查询 Linux 发布信息，但预检查会明确阻止安装和服务管理，没有 macOS 空后端。

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

默认进入带侧边导航的 Overview。未安装 Mihomo 时，首页突出单项安装引导，默认聚焦 **安装 Mihomo / Install Mihomo**；按一次 Enter 打开已有安装向导，启动 TUI 不自动下载或安装。选择 **稍后设置 / Set up later** 后，本次会话显示正常概览，可继续浏览所有页面，并从 Settings 打开内核管理和网络设置；下次启动未安装实例时重新显示引导。旧的持久关闭引导偏好仍然有效。

安装会自动准备可运行的**内置默认配置**，并明确显示“内置默认配置已就绪”；启动仍由用户在 Overview 明确操作。订阅和用户导入的本地 YAML 均为可选，不作为完成设置或启动的前置条件。内置配置的 `PROXY → DIRECT` 是正常策略，支持 `DIRECT`、`REJECT`；可随时在 Profiles 添加订阅或配置。安装后及正常停止时不再显示未完成引导。

- `i` 或首页 **安装 Mihomo / Install Mihomo**：打开 Core → Version → Confirm 向导。
- `b`：打开本地可执行文件安装选项，同样经过版本检查与确认。
- `n` 或 Settings → **Network settings**：在查询版本前配置下载网络。
- `a` 或 **Import config**：添加订阅 URL 或本地 Mihomo YAML；可在安装内核前保存，标记等待内核校验。
- 侧栏 `↑↓` / `j/k` 立即切换右侧页面，焦点留在导航；`Home/End` 到第一／最后一页，首尾不循环。`Enter`、`→` 或 `l` 进入当前页上次有效的内容位置，首次进入则落到主要列表、卡片或内容视图；这次按键只移动焦点。常驻侧栏中的 `←` / `h` 不移动焦点。
- `1–7` 直接打开 Overview / Proxies / Profiles / Connections / Rules / Logs / Settings 并进入内容，与侧栏顺序一致。`c` 仍可打开 Connections，窄窗口使用 Menu。
- `Tab / Shift+Tab` 按稳定顺序移动到下一个／上一个控件；导航和每个列表整体各占一站，内部使用方向键。内容区 `Enter` 激活当前控件，表单使用 Tab 逐字段移动。`?` 打开可滚动的上下文帮助。
- `Esc` 每次返回一层：关闭最上层菜单或确认框、退出编辑或详情，最后回到当前页导航。普通内容和纵向列表也可用 `←` / `h` 执行同层返回；横向控件优先使用自身方向键，Profiles 卡片网格仍在网格内移动。普通提示消息不抢占返回操作。后台任务可通过进度通知中的 Cancel 取消；关闭通知不取消任务。`q` 或 `Ctrl+C` 退出时会安全取消任务并等待清理。
- `Ctrl+T` 显示／隐藏整条底部快捷键提示，包括 `q Quit`；默认显示，选择跨启动保存。也可在 Settings → Interface → Show shortcut hints 或帮助中的按钮切换；表单与弹窗保留必要操作提示。
- 输入、编辑、弹窗和菜单保留自身按键。输入框拥有正常字符、编辑组合键与粘贴事件，`q` 和数字不会被全局快捷键抢走。内核 `s/x/r` 快捷操作只在内容区生效。鼠标可点击按钮、选项、输入框和导航；悬浮不切页、不抢焦点，Modal 不向背景传递操作。

页面切换在本次 TUI 会话中保留滚动、搜索、列表选择、展开项及未提交文本；再次选择当前页不重置。返回时恢复有效位置，数据变化后失效的选择会回退到合理内容位置。页面框架和缓存立即显示，旧页读取可以取消，迟到结果不会抢焦点；已提交的更新、启停等任务不会因浏览其他页而取消。日志保留暂停或跟随状态，离页期间的事件不补回。

窄屏 Menu 抽屉打开时定位当前页，`↑↓` 同步浏览并保持抽屉打开；`Enter`、`→`、`l` 或 `Tab` 关闭抽屉并进入内容，`Esc`、`←` 或 `h` 关闭后回到 Menu，已浏览到的页面继续保留。鼠标点击菜单后切页并关闭抽屉。

Overview 自适应撑满内容区，展示 Core、TUN、System proxy 控制卡、Rule / Global / Direct 模式选择，以及当前配置、代理组、流量、内核信息和系统信息。内核启停、重启和模式切换调用现有应用用例，完成后重新读取状态；运行模式与保存模式分别显示。TUN 和系统代理目前没有切换后端，显示不可用原因，系统代理明确标为 Unmanaged，不显示虚假的关闭状态。宽窗口使用多列卡片，窄窗口逐步收为单列；运行指标按空间分两列或四列。运行后自动展示实际上传/下载速率、活动连接数、本次内核运行累计流量及内核 RSS；未安装、停止、正在连接、部分数据不可用和 API 不可达分别反馈，不把缺失数据显示为零。

## 运行监控与连接

Overview、Connections 或常驻侧栏可见时自动采样；每轮并行读取真实 API，完成后间隔一秒再采样，不堆积并发请求。Mihomo 的 `/memory` 首帧为人工零，跳过首帧取得真实内存，正常一轮约两秒，因此页面通常约三秒更新一次。没有可见采样区域、打开向导、执行操作和退出时会取消采样；旧结果不覆盖当前任务，返回页面可先显示有效缓存。采样前后检查服务归属、状态与进程 ID，停止或重启期间的旧数据会丢弃并重新连接。

`Upload / download` 来自 `/traffic` 最近一秒的字节样本，按 B/s 展示。`Since core start` 直接读取 `/connections` 的 `uploadTotal`、`downloadTotal`，包含已关闭连接的已记录流量；只统计 Mihomo 纳入计数的流量，不是整台机器的网络用量。内核重启或统计被清零时归零，关闭 TUI 不清零。当前不持久化跨重启、每日或每月流量，也不通过速率积分推算累计值。

Connections 显示当前活动连接，可搜索目标域名/IP/端口、进程、规则或连接 ID：

- `/` 或 Search：输入搜索，`Enter` 完成，输入中的 `q`、数字不触发全局操作。
- `↑/↓` 选择连接，`Enter` 查看来源、目标、规则、代理链、进程和字节详情；长内容可滚动。
- `x` 请求关闭选中连接，明确确认后 `y` 执行，默认选择取消；`Esc` 返回。关闭绑定显示的连接 ID，刷新重排不会切换关闭目标。
- `g` / `Ctrl+R` 或 Refresh / Retry：重新采样；暂不可用时持续重试。关闭后重新读取 API，不根据本地列表推测成功。

连接关闭仅提供单条操作。连接已消失时会明确反馈，不关闭其他连接。

Rules 只读展示内核加载的规则并保留匹配顺序，支持搜索、详情、独立异步刷新与重试。Rule 模式才按此顺序匹配；不会编辑或启用/禁用规则。操作与数据含义见 [规则查看](docs/rules.md)。

响应式规则以字符列/行数为准：至少 **110×30** 使用最大 **86×33** 的居中 Modal；否则使用当前终端区域中的同一单列向导。至少 **100×20** 显示 22 列宽的常驻侧栏，否则使用 Menu。常驻侧栏 Logo 区使用深色背景，右侧标题区与底栏使用相同的正文背景色，标题分隔线与 Logo 区底边对齐；Logo 使用加粗强调色，与页面标题保持同一基线，终端至少 24 行时顶栏增加对称的上下留白。常驻侧栏第一项菜单紧接顶栏底边，右侧正文在标题分隔线下保留一行留白。菜单连续等高排列，项间不留空行。常驻侧栏与 Menu 弹出菜单均固定每项高 3 行，文字上下各留 1 行；窗口变矮时只缩小菜单可见区域，以独立滚动访问其余页面，并显示上下溢出提示。鼠标在菜单区滚动时不影响正文或切换页面；↑↓、Home/End 同步切页并自动滚动到完整菜单项，Enter 进入内容，1–7 直接切页并进入内容。Tab 将导航作为一个整体，重新进入时定位当前页。极小弹出菜单无法同时容纳三行菜单项与溢出提示时才降为单行。常驻侧栏底部在空间充足时仅显示实时上传、下载速率，切换页面时持续更新；无可用样本时显示 `--`，真实零速率显示 `0 B/s`。空间不足时隐藏，不挤占菜单。Overview 使用侧栏右侧的全部可用宽度，其他页面正文最大 112 列；顶栏对齐正文右缘并展示页面专属内容：Overview 显示真实内核状态；Profiles 提供 Update all，Proxies、Connections、Rules 提供刷新与可用数据计数，Logs 提供采集控制，Settings 提供网络设置入口。标题栏操作保持可见，窄窗口使用短标签或单独一行。底栏显示时根据焦点提示可用快捷键，不常驻刷新成功消息；隐藏后释放一行给正文和侧栏，同时重算滚动与鼠标区域，保留当前操作位置。Overview 控制区优先展示内核和模式，按可用宽度显示一、二或四列；配置、代理组及信息区在正文至少 76 列且终端至少 24 行时双列，否则单列。标题、正文滚动区和底部操作分开。极小窗口显示扩大终端提示，仍可返回或取消。缩放不重置选项或重启任务。

首次进入 TUI、界面就绪约 200ms 后，常驻侧栏的 `la💤yClash` 会播放一次“翻个身，接着睡”：约 800ms 内，中间两格由小 `z` 变成大 `Z` 再回到 💤，配合青色渐亮再恢复。每次启动仅一次，切页、缩放或关闭弹窗不会重播；首次以窄窗口或无色/16 色模式启动时跳过。点击 Logo 可播放约 1.6 秒的彩蛋：惊醒 `!!`、发呆 `..`、重新睡着，再从 💤 上方冒出两口 `z/Z` 梦泡，第二口更淡。字标位置固定，20～23 行的短窗口只播放中间两格的变化。点击优先播放完整剧情，再次点击从头播放；弹窗、忙碌或侧栏隐藏时结束动画，平时保持静止。

## Profiles 与实时日志

Proxies 包含代理组及其选项，DIRECT、REJECT 也属于有效选择；Profiles 统一管理订阅下载、本地 YAML 和默认配置。每个 Profile 自己保存地址与内容版本，不需要另行管理来源。现有 CLI `nodes`、`profile` 等业务命令保留原有职责。

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

## 版本、确认与安装

Latest stable 每次从官方 GitHub API 解析为具体版本；Specific version 可输入稳定标签，例如 `v1.19.33`。尚未支持 pre-release 和其他内核，因此不展示可进入的占位流程。

确认页展示实际版本、Linux 平台/资产、安装路径、旧版与运行版本、操作、磁盘空间、下载来源及代理。预检查验证目标可写、unit 所属关系、磁盘、平台和已有状态；提交时在进程锁内复检，拒绝过期或修改过的计划。

安装显示真实下载字节、已知总量/百分比、速率，以及校验、解压、可执行文件检查和部署阶段。总量未知时使用不确定状态；详情可展开。失败保留选择，可修改网络后重试，仍需重新确认后才安装。

安装、更新和显式重装均**不会自动启动或重启服务、修改系统代理、启用 TUN 或设置自启动**。同版本默认 Use existing，只有明确选择 Reinstall 才替换。更新先暂存并验证，再原子替换路径；正在运行的进程继续持有旧 inode。配置、API secret 与 unit 不因更新而覆盖。结果和首页分别显示已安装版本与实际运行版本，切换运行版本需要显式重启。

没有订阅也可启动：默认 `PROXY` 选择组包含 `DIRECT` 和 `REJECT`，不依赖 Geo 数据，也不会凭空提供远程节点。添加配置只校验保存，不自动应用；重复显示名称会去重，新导入不会覆盖已有配置。应用前执行内核校验，运行状态下应用失败恢复旧配置和原服务，停止状态下应用不隐式启动。

## 下载网络：来源与代理独立

默认 **Official / No proxy**，不自动切换第三方服务，也不修改操作系统网络设置。来源与代理可以组合，例如 prefix + custom HTTP proxy。

| 下载来源 | 发布文件 | 版本及预期 SHA256 |
| --- | --- | --- |
| Official | 官方 GitHub release 资产地址 | 官方 GitHub API |
| URL prefix accelerator | 去掉前缀末尾 `/`，再拼 `/` 和完整官方 URL | 仍为官方 GitHub API，不添加前缀 |
| Custom mirror | 精确模板替换 `{version}`、`{asset}` | 仍为官方 GitHub API，不使用镜像摘要 |

前缀示例：`https://accelerator.example.invalid/https://github.com/MetaCubeX/mihomo/releases/download/...`。这里是完整字符串拼接，不使用路径拼接破坏内层 `https://`，不整体编码官方 URL。

镜像模板示例：`https://mirror.example.invalid/mihomo/{version}/{asset}`。两个占位符必须存在；未知占位符报错。文件扩展名、gzip 格式、平台资产和官方预期摘要仍严格校验。这些是占位域名，不代表推荐或信任第三方。界面预览会明确遮盖凭据、未知静态路径及查询值，实际请求保留 URL 编码语义。

连接代理支持：

- **No proxy**：明确忽略所有环境变量代理。
- **Environment proxy**：读取 `HTTP_PROXY/http_proxy`、`HTTPS_PROXY/https_proxy`、`ALL_PROXY/all_proxy`、`NO_PROXY/no_proxy`。每个客户端重新读取；localhost/loopback 和 NO_PROXY 按规则直连。不是操作系统原生代理检测。
- **Custom proxy**：`http://`、`https://`、`socks5://`、`socks5h://`，可使用认证；SOCKS 名称解析经代理。

所有下载相关请求使用所选连接代理。不会假设尚未安装或尚未启动的 Mihomo 可作下载代理。

Test connection 分开显示 Version query 与 File download。版本测试验证真实 release JSON；文件测试读取实际资产的 Range、最多 4 KiB，并检查 gzip 魔数，不下载整个内核、不代替完整性校验。尚无有效资产时标记 Not applicable。版本失败但已有有效资产时仍可独立测试文件；不同版本的文件结果不会混用。修改草稿会使测试结果失效，不会每输入一个字符就请求网络。

Back 放弃草稿；Apply 仅应用到当前会话；从失败状态进入时提供 Apply & retry。勾选 Remember 才保存允许的非敏感偏好。认证、查询参数、opaque URL 和不在公开路径规则内的地址只能用于本次会话，保存会明确失败；可取消 Remember 后继续。不会默认明文保存密码。

TLS 验证始终开启。元数据必须来自官方 API，不接受 HTML、挑战页或其他重定向作为官方摘要；GitHub API 受限时明确报错，不因镜像文件可达而声称网络全部正常。下载必须有明确来源的预期 SHA256；缺失或不匹配时拒绝安装。摘要来自官方 API 的信任边界有明确标注，不把镜像文件自身 hash 描述成官方真实性证明。

## 持久运行与权限

当前交付 Linux + systemd 用户服务 + Mihomo 本机 HTTP/SOCKS 代理。启动要求已明确启用 linger；TUI 退出或终端挂断不发送 Stop。运行意图、模式、节点缓存和显式订阅来源持久保存，状态展示实际 systemd/API 及差异。`service enable/disable` 独立控制自启动，`mode`、`profile refresh`、`uninstall` 复用共享用例。详细行为和验收见 [持久运行](docs/runtime-lifecycle.md)。

本版不提供特权 TUN、系统级服务或系统代理接管，开启请求会明确失败；不要求整个 TUI 以 root 运行，也不对用户可写 Mihomo 设置 capabilities。方案比较与后续安全条件见 [权限管理](docs/permissions.md)。

## CLI 与 Shell

CLI 和 TUI 共享 `ResolveRelease → PrepareInstall → InstallCore`，以及网络客户端、预检查、配置验证、原子部署与恢复。TUI 不启动自身 CLI，安装业务不在 shell 脚本中。

```sh
lazyclash install --core-version latest
lazyclash install --core-version v1.19.33 --dry-run
lazyclash install --bin /absolute/mihomo
lazyclash install --core-version v1.19.33 --reinstall
lazyclash install --download-source prefix --download-prefix https://accelerator.example.invalid/ \
  --proxy-mode custom --proxy-url http://127.0.0.1:7890
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

其余网络 flags：`--download-mirror TEMPLATE`、`--proxy-mode environment`、`--remember-download-settings`。未指定的设置复用已保存偏好。CLI 凭据参数可能进入用户自己的 Shell 历史，需要保密时优先使用隐藏输入的 TUI。

子进程和 TUI 无法修改父 Shell；显式求值：

```sh
eval "$(lazyclash env)"
eval "$(lazyclash env --unset)" # 配置损坏或 namespace 无效仍可清理

eval "$(lazyclash init --shell zsh)" # bash 同样支持；只输出函数，不编辑启动文件
lazyclash_on   # 启动成功后设置当前 Shell 环境
lazyclash_off  # 清除环境，不停止服务
```

服务名为用户级 `lazyclash-mihomo.service`，不提权、不自动 enable 或修改 linger；Start/Restart 要求 linger 已启用，显式 `service enable/disable` 管理自启动。API `127.0.0.1:9097`、mixed `127.0.0.1:7897`；安装和启动前检查冲突，不停止已有服务腾位置。API 密钥自动生成和注入，状态不输出密钥。服务 stdout/stderr 丢弃，避免 provider URL 凭据进入 journal；退出原因通过 systemd Result、退出状态和重启计数查询。

默认数据目录 `$XDG_DATA_HOME/lazyclash`（或 `~/.local/share/lazyclash`）。`--home ABS` / `LAZYCLASH_HOME` 隔离数据；`--service-name lazyclash-NAME` / `LAZYCLASH_SERVICE` 隔离 unit，仍需避免端口冲突。私有目录和 `0600` 文件保护配置、原始订阅、偏好与恢复日志；进程锁等待可取消，锁文件不删除。检测到用户修改的文件或 unit 时保留现场并报错。

## 开发与验证

```sh
make check
make race
make i18n-check
make build
python3 -m unittest discover -s scripts -p 'test_*.py'
orb list
./scripts/verify-linux.sh "$VM"
./scripts/e2e.sh "$VM"
```

普通测试仅用临时目录、fixture 与 fake 服务，不创建 VM 或修改真实代理。真实 Linux、PTY 与 Mihomo/systemd 测试显式执行，使用临时目录、随机专用 unit、冲突检查和失败清理，详情见 [验证说明](docs/verification.md)。未实测平台不得由交叉编译推断为已通过。

程序入口为根目录 `main.go`。`internal/cli` 保留 Cobra 子命令与 Shell 集成，`internal/tui` 负责交互与响应式渲染；二者直接调用 `internal/app` 的共用用例，包括运行采样和单连接关闭。`internal/downloadnet` 负责来源与连接，`internal/mihomo` 负责发布、下载、校验和控制 API，`internal/service` 负责 systemd 用户服务，`internal/store` 负责原子文件与进程锁。具体边界见 [代码结构](docs/architecture.md)。

[路线图](docs/roadmap.md)。参考旧项目仅用于只读研究，不读取旧运行配置，不继承历史、提供旧别名或实现迁移层。
