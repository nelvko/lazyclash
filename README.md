# lazyclash

以 TUI 为主要交互的 Mihomo 终端代理管理工具。Go module：`github.com/nelvko/lazyclash`。

## 构建与使用

需要 **Go 1.26+**。固定稳定依赖：Bubble Tea **2.1.1**、Bubbles **2.2.1**、Lip Gloss **2.0.6**、Fang **2.0.1**，统一使用 `charm.land/*/v2`；Cobra **1.10.2**。完整版本与校验记录在 go.mod/go.sum。

```sh
make build
./bin/lazyclash
```

服务与内核安装当前支持 **Linux/systemd 用户服务、arm64 与 amd64**。macOS 可构建、运行 TUI、配置下载网络及查询 Linux 发布信息，但预检查会明确阻止安装和服务管理，没有 macOS 空后端。

## 首页与安装向导

默认进入带侧边导航的 Overview，不强制打开安装向导。顶部 Get started 按真实状态提供 Install、Import 或首次 Start；可以 Dismiss，随后仍可从 Settings 打开内核管理和网络设置。成功运行后主动停止不会重新显示首次启动引导。

- `i` 或首页 **Install core**：打开 Core → Version → Confirm 向导。
- `b`：打开本地可执行文件安装选项，同样经过版本检查与确认。
- `n` 或 Settings → **Network settings**：在查询版本前配置下载网络。
- `a` 或 **Import config**：添加订阅 URL 或本地 Mihomo YAML；可在安装内核前保存，标记等待内核校验。
- `1/2/3/4`：Overview / Nodes / Subscriptions / Settings；窄窗口可用 Menu。
- `Tab / Shift+Tab` 切换焦点，方向键选择选项，`Enter` 激活，`Space` 切换选项；`PgUp/PgDn` 和滚轮滚动。
- `Esc` 返回或取消；任务取消会显示清理状态，等待安全边界后才结束。`Ctrl+C` 同样等待清理后退出。
- 输入框拥有正常字符与粘贴事件，`q` 和数字不会被全局快捷键抢走。鼠标可点击按钮、选项、输入框和导航；Modal 不向背景传递操作。

首页保留 Current subscription、Selected proxy group、Traffic statistics、Kernel and environment 四张卡片。未实现的流量采集明确显示 Unavailable，不模拟数字，也不提供无效业务入口。

响应式规则以字符列/行数为准：至少 **110×30** 使用最大 **86×33** 的居中 Modal；否则使用当前终端区域中的同一单列向导。至少 **100×20** 显示常驻侧栏，否则使用 Menu。首页正文最大 112 列；正文至少 76 列且终端至少 24 行时卡片双列，否则单列。标题、正文滚动区和底部操作分开。极小窗口显示扩大终端提示，仍可返回或取消。缩放不重置选项或重启任务。

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
orb list
./scripts/verify-linux.sh "$VM"
./scripts/e2e.sh "$VM"
```

普通测试仅用临时目录、fixture 与 fake 服务，不创建 VM 或修改真实代理。真实 Linux、PTY 与 Mihomo/systemd 测试显式执行，使用临时目录、随机专用 unit、冲突检查和失败清理，详情见 [验证说明](docs/verification.md)。未实测平台不得由交叉编译推断为已通过。

主要代码：`internal/tui` 交互与响应式渲染；`internal/app` 共用用例与存储事务；`internal/downloadnet` 来源与连接；`internal/mihomo` 发布、下载、校验和控制 API；`internal/service` 用户 systemd；`internal/cli` 自动化及 Shell。

[路线图](docs/roadmap.md)。参考旧项目仅用于只读研究，不读取旧运行配置，不继承历史、提供旧别名或实现迁移层。
