# Linux 服务 scope 与首发范围

lazyclash 在 Linux/systemd 上提供两个独立作用域：普通用户管理自己的 `user` 实例，root 管理自己的 `system` 实例。两者都可运行本机 HTTP/SOCKS 代理；TUN 仅由 root 的系统实例明确启用，首版只有整台主机的 `host` 范围。

`sudo lazyclash` 选择 root 的独立实例，不复制、接管或切换原用户的配置与服务。普通用户界面不发起 sudo，也不提供同一实例的 user → system 切换。项目尚未发行，不维护旧 unit、manifest、路径或命令的兼容层，不自动迁移旧安装。

## 作用域、路径与身份

| 项目 | 普通用户 `user` | root `system` |
| --- | --- | --- |
| manager | 当前用户的 `systemctl --user` | 系统 `systemctl` |
| 默认数据目录 | `$XDG_DATA_HOME/lazyclash`，否则 `~/.local/share/lazyclash` | `/var/lib/lazyclash/<service-name>` |
| unit 目录 | 用户配置目录的 `systemd/user/` | `/etc/systemd/system/` |
| Mihomo 运行身份 | 当前普通用户 | 每个服务专用的非登录、非 root 账号 |
| 普通代理权限 | 当前用户权限，无额外 capabilities | 空的 capability bounding/ambient 集合 |
| TUN | 不支持 | 明确确认后授予 `CAP_NET_ADMIN`、`CAP_NET_RAW` |
| 注销后运行 | 需要明确启用 linger | 不依赖登录会话和 linger |
| 自启动 | 显式 enable 当前 user unit | 显式 enable 当前 system unit |

默认服务名均为 `lazyclash-mihomo`。同名 user/system unit 属于不同 manager，配置、密钥和安装记录互不共享。普通代理可并存，但监听端口必须不同；发现端口占用时报告，不停止其他实例。loopback TCP 监听本身没有用户隔离语义。

管理器预检只确认当前 scope 的 systemd 可访问，`SystemState=degraded` 不会阻止安装。属性查询读取带名称的输出，不依赖 `--value`；服务名必须明确返回 `LoadState=not-found` 才视为空闲。系统管理器连接失败不会提示用户会话或 `XDG_RUNTIME_DIR`。这些查询已修正旧版工具的选项问题，但 systemd 219 的完整安装、unit 与生命周期尚未实测，版本范围及实际验证见 [验证说明](verification.md)。

普通用户可用 `--home` / `LAZYCLASH_HOME` 和 `--service-name` / `LAZYCLASH_SERVICE` 隔离实例。root 忽略这两个用户环境变量，也不根据 `SUDO_USER` 选择数据；使用显式 `--home` 和 `--service-name`。root 的每个服务名有独立默认目录。自定义系统路径必须位于受保护、服务账号可遍历的目录下，祖先须 root 所有且不可由其他账号改写。`/tmp`、`/var/tmp`、`/home`、`/root`、`/run/user` 及其子目录会被 unit 文件系统沙箱隐藏，因此明确禁止；请使用 `/var/lib/lazyclash` 下的独立目录。符号链接或不安全的现存文件也会被拒绝。

## 日常操作

安装只发布内核、默认配置和 unit，不启动、不 enable、不修改 linger。普通用户先明确配置 linger，再使用 `service start`；root 的系统实例不做 linger 检查。`service enable/disable` 只改变自启动，`service stop` 只改变当前运行意图。退出 TUI、断开终端和 CLI 返回均不停止已托管的 Mihomo。

```sh
# 普通用户的本机代理
lazyclash install --core-version latest
loginctl enable-linger "$(id -un)"  # 由用户或管理员明确执行
lazyclash service start

# 管理员已将可信发行版安装到受保护的位置；这些命令操作 root 实例
sudo /usr/local/bin/lazyclash install --core-version latest
sudo /usr/local/bin/lazyclash status
sudo /usr/local/bin/lazyclash tun on --scope host --confirm-host
sudo /usr/local/bin/lazyclash service start
sudo /usr/local/bin/lazyclash tun off
```

TUI 的系统环境卡显示 system scope、已保存 TUN 状态和主机范围。开启弹窗说明路由、DNS 及其他用户和服务的影响，默认选择取消。CLI 的 `--scope host` 只确认流量范围，不用于选择服务 manager。普通用户请求开启会返回权限边界说明，不启动授权或复制流程。

## 系统部署与执行权限

系统安装在数据目录内分别保存 root 管理的内核、配置代际、manifest、事务记录和生命周期入口。安装时把当前 lazyclash 可执行程序复制为受保护的 `installation/lazyclash`，供 unit 调用同一个程序的有限内部生命周期命令；不是单独发行的 helper。摘要与所属关系检查会拒绝该副本被替换的安装。

发布目录由 root 拥有，服务账号只获得所需的遍历、配置读取和二进制执行权限。`installation/runtime` 单独归该服务账号所有，是运行缓存的可写边界。原始 Profile、订阅、密钥和事务日志不对其他用户开放。版本探测与配置校验也降至专用账号，清理附加组与 root 环境，不因管理命令由 root 发起就以 root 执行候选 Mihomo。setuid/setgid 和带文件 capabilities 的内核会被拒绝。

system unit 使用 `NoNewPrivileges=true`、`ProtectSystem=strict`、`ProtectHome=true` 和私有临时目录，写入路径限定为 runtime。普通代理不授予网络管理能力；TUN 模式才在 bounding/ambient 集合中加入 `CAP_NET_ADMIN CAP_NET_RAW`。不使用 `setcap`、`CAP_SYS_ADMIN` 或长期 root Mihomo。网络 capabilities 具有主机级影响，不是只允许访问一个接口的沙箱。

## TUN 策略、冲突与清理

首版 TUN 使用明确的 host 策略：受管接口、IPv4/IPv6 自动路由和 TUN 内的 DNS 劫持，不提供 UID 筛选或任意 namespace 切换。策略显式设置 `dns-mode: disabled`，不修改 `/etc/resolv.conf` 或操作系统 resolver；DNS 劫持不等于系统 resolver 配置变更。订阅不能通过 `tun.enable` 或 TUN listener 隐式授权，必须关闭这些输入并通过 lazyclash 的运行设置启用。

停止状态下开启只发布配置和 unit 权限，保持停止，不提前修改网络。运行中开启或停用会先停止旧进程，再发布配置与权限并重启，验证就绪；失败按事务记录恢复旧配置、权限和运行意图。Profile 应用、模式和端口更新保留已明确选择的 TUN 策略。

同一网络命名空间只允许一个 lazyclash TUN 管理者。`/run/lazyclash` 中的私有记录绑定实例 ID、数据目录、服务名和 systemd invocation ID；启动检查在协调锁内完成。已存在的 TUN 设备、保留路由表/规则区间或其他实例记录会阻止启动，root 也不会自动抢占。转移需要先显式停止原实例并确认清理完成；若不希望其开机再竞争，还须显式停用其自启动。没有一键交接或跨重启的管理者选择服务。

就绪钩子记录接口 ifindex/alias 与 IPv4/IPv6 规则、路由。退出钩子只清理属于本次 invocation 的资源，覆盖正常停止、异常退出和部分启动失败；归属变化或外部规则出现时保留记录并报告，不能清空全机网络配置。外部 VPN 不受本项目协调协议约束，不能把本项目记录空闲当作网络空闲。机器重启后运行期记录重新建立；多个已启用 TUN 的实例可能竞争，部署时应只为预期实例保留 TUN 自启动。

## 内核托管与管理事务

每个内核由独立 systemd unit 直接启动，systemd 负责进程、cgroup、异常重启、停止超时和自启动。system unit 用同一受保护 lazyclash 可执行程序的 `ExecStartPre` / `ExecStartPost` / `ExecStopPost` 有限钩子执行 TUN 检查、就绪和清理；这些调用结束即退出，不运行后台持锁进程。

CLI/TUI 直接调用共享应用用例，安装、更新、配置和 TUN 变更在应用锁及持久 journal 下进行。可处理的失败立即回滚；应用被强制终止后，遗留 journal 在下一次应用调用时核对并恢复。没有常驻 daemon、独立特权 helper 或脱离客户端的事务执行器，不能承诺客户端消失后未完成的管理事务会立即自动完成。systemd 的内核自动恢复和 unit 退出钩子仍独立于 TUI 存在。显式 Stop 的持久意图优先于事务恢复。

## 打包与验证

`make package VERSION=dev` 在 `bin/packages/` 生成 Linux/amd64、Linux/arm64 的发行归档与 SHA256SUMS；归档包含一个 lazyclash 程序和 README。Mihomo 由安装用例下载或从明确选择的本地文件安装，不随 lazyclash 归档附带。

本轮已通过宿主 check/race、Linux 全包测试、root 身份/数据权限回归，以及打包产物的双作用域、真实 TUN HTTP/DNS、崩溃恢复、损坏配置停止清理和显式交接端到端验收。root TUI 在 120×36、80×24 验证确认与默认取消，120×36 另实际确认启用 TUN。实际结果与限制统一记录在 [验证说明](verification.md)。执行入口为 `scripts/service-scopes-e2e.sh VM /absolute/packaged/lazyclash /absolute/vm/mihomo`，使用隔离目录、专用 unit 和受控网络 fixture。SSH 最后会话注销、机器重启及未实测平台分别验收，不能用编译或配置校验推断通过。
