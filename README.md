# lazyclash

以 TUI 为主要交互的 Mihomo 终端代理管理工具。Go module：`github.com/nelvko/lazyclash`。

## 构建与使用

需要 **Go 1.26+**。固定稳定依赖：Bubble Tea **2.1.1**、Bubbles **2.2.1**、Lip Gloss **2.0.6**、Fang **2.0.1**，统一使用 `charm.land/*/v2`；Cobra **1.10.2**。完整版本与校验记录在 go.mod/go.sum。

```sh
make build
./bin/lazyclash
# 或在源码根目录直接运行
go run .
```

服务与内核安装当前支持 **Linux/systemd 用户服务、arm64 与 amd64**。macOS 可构建、运行 TUI、配置下载网络及查询 Linux 发布信息，但预检查会明确阻止安装和服务管理，没有 macOS 空后端。

## 首页与安装向导

默认进入带侧边导航的 Overview，不强制打开安装向导。顶部 Get started 按真实状态提供 Install、Import 或首次 Start；可以 Dismiss，随后仍可从 Settings 打开内核管理和网络设置。成功运行后主动停止不会重新显示首次启动引导。

- `i` 或首页 **Install core**：打开 Core → Version → Confirm 向导。
- `b`：打开本地可执行文件安装选项，同样经过版本检查与确认。
- `n` 或 Settings → **Network settings**：在查询版本前配置下载网络。
- `a` 或 **Import config**：添加订阅 URL 或本地 Mihomo YAML；可在安装内核前保存，标记等待内核校验。
- `1–7`：Overview / Proxies / Profiles / Connections / Rules / Logs / Settings，与侧栏显示顺序一致；数字在菜单右侧显示。`c` 仍可打开 Connections，窄窗口使用 Menu。
- `Tab / Shift+Tab` 按视觉顺序切换导航组、列表和可见控件；方向键在侧栏选择页面、在列表选择条目，`Enter` 激活。表单使用 Tab 逐字段移动。`?` 打开可滚动的上下文帮助。
- `Esc` 返回或取消；任务取消会显示清理状态，等待安全边界后才结束。`Ctrl+C` 同样等待清理后退出。
- 输入框拥有正常字符与粘贴事件，`q` 和数字不会被全局快捷键抢走。鼠标可点击按钮、选项、输入框和导航；Modal 不向背景传递操作。

首页保留 Current subscription、Selected proxy group、Traffic statistics、Kernel and environment 四张卡片。运行后自动展示实际上传/下载速率、活动连接数、本次内核运行累计流量及内核 RSS；未安装、停止、正在连接、部分数据不可用和 API 不可达分别反馈，不把缺失数据显示为零。

## 运行监控与连接

Overview 和 Connections 可见时自动采样；每轮并行读取真实 API，完成后间隔一秒再采样，不堆积并发请求。Mihomo 的 `/memory` 首帧为人工零，跳过首帧取得真实内存，正常一轮约两秒，因此页面通常约三秒更新一次。切换至其他页面、打开向导、执行操作和退出会取消采样；旧结果不覆盖当前任务。采样前后检查服务归属、状态与进程 ID，停止或重启期间的旧数据会丢弃并重新连接。

`Upload / download` 来自 `/traffic` 最近一秒的字节样本，按 B/s 展示。`Since core start` 直接读取 `/connections` 的 `uploadTotal`、`downloadTotal`，包含已关闭连接的已记录流量；只统计 Mihomo 纳入计数的流量，不是整台机器的网络用量。内核重启或统计被清零时归零，关闭 TUI 不清零。当前不持久化跨重启、每日或每月流量，也不通过速率积分推算累计值。

Connections 显示当前活动连接，可搜索目标域名/IP/端口、进程、规则或连接 ID：

- `/` 或 Search：输入搜索，`Enter` 完成，输入中的 `q`、数字不触发全局操作。
- `↑/↓` 选择连接，`Enter` 查看来源、目标、规则、代理链、进程和字节详情；长内容可滚动。
- `x` 请求关闭选中连接，明确确认后 `y` 执行，默认选择取消；`Esc` 返回。关闭绑定显示的连接 ID，刷新重排不会切换关闭目标。
- `g` / `Ctrl+R` 或 Refresh / Retry：重新采样；暂不可用时持续重试。关闭后重新读取 API，不根据本地列表推测成功。

连接关闭仅提供单条操作。连接已消失时会明确反馈，不关闭其他连接。

Rules 只读展示内核加载的规则并保留匹配顺序，支持搜索、详情、独立异步刷新与重试。Rule 模式才按此顺序匹配；不会编辑或启用/禁用规则。操作与数据含义见 [规则查看](docs/rules.md)。

响应式规则以字符列/行数为准：至少 **110×30** 使用最大 **86×33** 的居中 Modal；否则使用当前终端区域中的同一单列向导。至少 **100×20** 显示常驻侧栏，否则使用 Menu。首页正文靠近侧栏左对齐，最大 112 列；顶栏显示页面与真实内核状态，底栏根据焦点显示可用快捷键，不常驻刷新成功消息。正文至少 76 列且终端至少 24 行时卡片双列，否则单列。标题、正文滚动区和底部操作分开。极小窗口显示扩大终端提示，仍可返回或取消。缩放不重置选项或重启任务。

## Profiles 与实时日志

Proxies 包含代理组及其选项，DIRECT、REJECT 也属于有效选择；Profiles 包含订阅 URL、本地 YAML 和默认配置，订阅只是来源之一。现有 CLI `nodes`、`profile` 等业务命令保留原有职责。

Logs 通过当前 Mihomo 的认证本机控制 API 获取实时事件，默认只在内存保留最近 200 条脱敏记录，不启用原始 journal、不落盘保存日志。`f` 暂停/继续，向上滚动或搜索会暂停采集，`End` 恢复跟随，`g` 重连。暂停、离页期间的事件不会补回，重新采集显示间隔提示。需要内核运行且 API 可用；没有历史启动日志。

CLI 使用同一日志用例，例如 `lazyclash logs --lines 50 --duration 5s`，在有界时间内采集最多指定条数并输出 JSON。日志来源、容量、取消和脱敏边界见 [日志查看](docs/logs.md)。

## 版本、确认与安装

Latest stable 每次从官方 GitHub API 解析为具体版本；Specific version 可输入稳定标签，例如 `v1.19.33`。尚未支持 pre-release 和其他内核，因此不展示可进入的占位流程。

确认页展示实际版本、Linux 平台/资产、安装路径、旧版与运行版本、操作、磁盘空间、下载来源及代理。预检查验证目标可写、unit 所属关系、磁盘、平台和已有状态；提交时在进程锁内复检，拒绝过期或修改过的计划。

安装显示真实下载字节、已知总量/百分比、速率，以及校验、解压、可执行文件检查和部署阶段。总量未知时使用不确定状态；详情可展开。失败保留选择，可修改网络后重试，仍需重新确认后才安装。

安装、更新和显式重装均**不会自动启动或重启服务、修改系统代理、启用 TUN 或设置自启动**。同版本默认 Use existing，只有明确选择 Reinstall 才替换。更新先暂存并验证，再原子替换路径；正在运行的进程继续持有旧 inode。配置、API secret 与 unit 不因更新而覆盖。结果和首页分别显示已安装版本与实际运行版本，切换运行版本需要显式重启。

没有订阅也可启动：默认 `PROXY` 选择组包含 `DIRECT` 和 `REJECT`，不依赖 Geo 数据，也不会凭空提供远程节点。添加配置只校验保存，不自动应用；同名文件拒绝覆盖。应用前执行内核校验，运行状态下应用失败恢复旧配置和原服务，停止状态下应用不隐式启动。

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

## CLI 与 Shell

CLI 和 TUI 共享 `ResolveRelease → PrepareInstall → InstallCore`，以及网络客户端、预检查、配置验证、原子部署与恢复。TUI 不启动自身 CLI，安装业务不在 shell 脚本中。

```sh
lazyclash install --core-version latest
lazyclash install --core-version v1.19.33 --dry-run
lazyclash install --bin /absolute/mihomo
lazyclash install --core-version v1.19.33 --reinstall
lazyclash install --download-source prefix --download-prefix https://accelerator.example.invalid/ \
  --proxy-mode custom --proxy-url http://127.0.0.1:7890
lazyclash profile add local /absolute/config.yaml
lazyclash profile apply local
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

服务名为用户级 `lazyclash-mihomo.service`，不提权、不自动 enable 或修改 linger。API `127.0.0.1:9097`、mixed `127.0.0.1:7897`；安装和启动前检查冲突，不停止已有服务腾位置。API 密钥自动生成和注入，状态不输出密钥。服务 stdout/stderr 暂时丢弃，避免 provider URL 凭据进入 journal。

默认数据目录 `$XDG_DATA_HOME/lazyclash`（或 `~/.local/share/lazyclash`）。`--home ABS` / `LAZYCLASH_HOME` 隔离数据；`--service-name lazyclash-NAME` / `LAZYCLASH_SERVICE` 隔离 unit，仍需避免端口冲突。私有目录和 `0600` 文件保护配置、原始订阅、偏好与恢复日志；进程锁等待可取消，锁文件不删除。检测到用户修改的文件或 unit 时保留现场并报错。

## 开发与验证

```sh
make check
make race
make build
python3 -m unittest discover -s scripts -p 'test_*.py'
orb list
./scripts/verify-linux.sh "$VM"
./scripts/e2e.sh "$VM"
```

普通测试仅用临时目录、fixture 与 fake 服务，不创建 VM 或修改真实代理。真实 Linux、PTY 与 Mihomo/systemd 测试显式执行，使用临时目录、随机专用 unit、冲突检查和失败清理，详情见 [验证说明](docs/verification.md)。未实测平台不得由交叉编译推断为已通过。

程序入口为根目录 `main.go`。`internal/cli` 保留 Cobra 子命令与 Shell 集成，`internal/tui` 负责交互与响应式渲染；二者直接调用 `internal/app` 的共用用例，包括运行采样和单连接关闭。`internal/downloadnet` 负责来源与连接，`internal/mihomo` 负责发布、下载、校验和控制 API，`internal/service` 负责 systemd 用户服务，`internal/store` 负责原子文件与进程锁。具体边界见 [代码结构](docs/architecture.md)。

[路线图](docs/roadmap.md)。参考旧项目仅用于只读研究，不读取旧运行配置，不继承历史、提供旧别名或实现迁移层。
