# 终端代理 Shell Integration

普通启动方式始终为 `lazyclash`，没有同名函数、alias 或启动包装。TUI 保存当前 Shell 的请求；Shell 内的私有 Prompt Hook 在命令返回后设置环境变量。系统代理、TUN 和终端代理彼此独立。

## 首次使用

在概览的「网络设置」卡片中选中「终端代理」，点击「启用」并确认安装。待激活时可点「查看激活命令」，按 Esc 返回。程序验证当前 Linux Shell 的身份、实际 Mihomo Mixed Port 及监听状态后，安装对应脚本与启动引用。安装失败不保存开启成功状态。

退出 TUI 后，在同一个终端执行提示的 `source` 命令。脚本会注册 Hook、领取该 Shell 的待激活请求并**立即同步**。无须重新打开 TUI；可用 `env | grep -i proxy` 和 `lazyclash shell status` 检查。以后的开关和端口变更在返回提示符时自动同步。

未执行 source 时，请求继续保留，允许再次进入 TUI 修改。自动激活提示只尝试展示一次；卡片中的“查看激活命令”可随时查看。提示在 Bubble Tea 完全退出并恢复终端后输出。SIGKILL 无法输出提示，但不会清除已保存请求或已应用环境。

## 会话与状态

配置默认为 `~/.config/lazyclash/shell`，运行状态默认为 `~/.cache/lazyclash/shell`，遵循相应 XDG 变量。目录 0700、文件 0600；配置和状态不随内核卸载而删除。数据目录与服务名用于区分端口来源，但开启意图属于当前 Shell 会话。

首次 pending 请求绑定 UID、Linux boot ID、Shell PID、进程启动时间、TTY、实际 Shell 类型和 PID namespace。source 仅领取自己所属的请求，创建随机正式 session ID，并通过环境变量导出会话标识。子 Shell 的继承标识不能通过父进程身份校验；正常交互式子 Shell 重新初始化自己的会话。Pending 请求七天过期，死进程状态在后续管理操作中清理。

每会话请求与应用回执分开存放。`pending`、`enabling`、`disabling` 不等于 `on`；只有匹配的成功回执与当前代理环境才能确认已开启。缺失或损坏的状态不作为关闭指令。TUI 异常退出也不发送关闭请求。

Hook 用内建读取检查小型固定格式记录；未变更立即返回，不执行 Go 子进程、不解析主配置、不扫描目录、不执行状态文件内容。source 阶段允许有限的 attach/finalize 子进程。端口发布只发生在已提交设置或可信实际配置观测之后，按实例 namespace 分发给相关会话，不改变其他会话的开启意图。

## 环境变量与恢复

接管前，Shell 私有内存分别记录八个变量的存在性、值及 export 属性：`http_proxy`、`https_proxy`、`all_proxy`、`HTTP_PROXY`、`HTTPS_PROXY`、`ALL_PROXY`、`no_proxy`、`NO_PROXY`。重复开启及端口变更不会覆盖快照；快照不导出，也不写入磁盘。

HTTP/HTTPS 变量设为 `http://127.0.0.1:<实际端口>`，ALL_PROXY 两种大小写均使用 `socks5h://127.0.0.1:<实际端口>`。两个原始绕过列表与 `localhost,127.0.0.1,::1` 合并去重。关闭时分别恢复各变量：原本未定义的 unset、空值保留为空、非导出值恢复非导出。Fish 使用当前进程的 global shadow，不更改 universal 变量。

遇到 readonly 或不支持的变量类型时不进行部分接管，显示同步失败。恢复失败时保留原始快照，便于修复后重试。

## 启动文件与 Hook

- Bash：`.bashrc` 加初始化引用；同时处理实际优先加载的 `.bash_profile`、`.bash_login`、`.profile`，不创建更高优先级文件遮蔽已有配置。保留 PROMPT_COMMAND 字符串或数组及已有条目，重复 source 不重复注册。
- Zsh：使用 `$ZDOTDIR/.zshrc`（未设置时为 `$HOME/.zshrc`），通过 `add-zsh-hook precmd`、`zshexit` 追加 Hook。
- Fish：使用 `$XDG_CONFIG_HOME/fish/conf.d/lazyclash.fish`，通过 `fish_prompt`、`fish_exit` 事件注册，不覆盖用户 prompt 函数。

启动文件只添加标记明确的引用块，修改前备份，使用受控原子写入和安装日志恢复中断。卸载只删除自己记录的内容，不用整份备份覆盖用户后续编辑。

## 管理与限制

`lazyclash shell status [--json]` 与 `lazyclash shell uninstall` 不依赖可用的内核安装或 systemd 服务。当前 Shell 已接管时，卸载先保存恢复请求并返回，让 Prompt Hook 恢复；一个最多等待 30 秒的独立清理进程收到回执后移除自己的初始化引用。超时仍是待完成卸载，不能宣称完成；`shell status` 显示持久化票据与失败原因，重复 `shell uninstall` 可以重试。若其他终端尚有有效的开启待激活请求，保留脚本和引用，待其 source、退出或请求过期后再重试。其他已加载终端保留自己的状态与内存 Hook，仍可独立开关。

第一阶段只支持可验证的 Linux Bash、Zsh、Fish 交互式 TTY。非交互进程、未知 Shell、无法读取进程身份或没有 TTY 时明确不可用，不猜测会话。自定义 `--norc`、`--noprofile`、启动文件提前 return/exec、用户在初始化后手动移除 Hook，不能保证自动加载或后续 Hook 持续存在；状态中的 Hook 回执代表最近一次确认。

Bash 没有通用的可追加 EXIT Hook API：已有用户 EXIT trap 时保留它，退出遗留文件由身份失效清理。`exec` 替换 Shell 会丢失内存快照，无法跨进程替换恢复；重新 source 会建立新会话。经 Shell 优化为直接 exec 的匿名子 Shell `(lazyclash)` 无法在所有情况下与直接调用区分，不承诺该场景隔离。普通交互式子 Shell 和独立终端按真实进程身份隔离。
