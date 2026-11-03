# Linux 权限边界

首发提供独立的普通用户 user 实例和 root system 实例。普通用户只使用自己的本机 HTTP/SOCKS 代理；root 的系统实例可明确授权整台主机的 TUN。`sudo lazyclash` 操作 root 的独立数据和服务，不复制用户安装，也不保持原用户 owner。没有普通用户提权流程、backend 切换或独立 helper；完整约定见 [服务 scope](service-scopes.md)。

## 用户实例

TUI、CLI 和 Mihomo 使用当前用户权限，数据保存在 XDG 用户目录，unit 位于用户 systemd 配置目录。服务设置 `NoNewPrivileges=true`，不额外授予网络 capabilities，不调用 sudo、pkexec 或 setcap。Start/Restart/Enable 检查用户 manager 与 linger，缺失时明确报告，由用户或管理员自行授权配置；lazyclash 不修改 linger。

文件摘要、精确 unit 内容、加载路径和 drop-in 检查用于确认安装归属及避免误操作，不构成对同 UID 恶意进程的隔离。loopback 代理端口本身不限制客户端 UID，不能把 user scope 宣传为用户流量隔离。

## 系统实例

root 默认数据目录为 `/var/lib/lazyclash/<service-name>`，忽略用户的 `LAZYCLASH_HOME`、`LAZYCLASH_SERVICE` 和 XDG 数据目录。显式 `--home` 仍须通过 root 所有权、真实目录、可写权限和服务账号遍历检查，祖先目录也属于校验范围。系统数据禁止位于 `/tmp`、`/var/tmp`、`/home`、`/root`、`/run/user` 及其子目录，因为 unit 文件系统沙箱会隐藏这些位置；隔离测试也应使用 `/var/lib/lazyclash` 下受保护的专用目录。

每个系统服务有专用的非登录、非 root 账号。root 持有二进制、配置代际、manifest、journal 和受保护的 `installation/lazyclash` 生命周期副本；服务账号只能读取配置、执行内核并写入独立 runtime 缓存。root 管理命令的版本探测和配置校验同样降至该账号，清空附加组并使用最小环境，不以 root 执行候选 Mihomo。

system unit 使用 `NoNewPrivileges=true`、只读系统视图、隐藏 home、私有临时目录及限定 runtime 写入路径。普通代理的 capability bounding/ambient 集合为空；明确开启 TUN 后只加入 `CAP_NET_ADMIN CAP_NET_RAW`。不开启完整 root Mihomo，不授予 `CAP_SYS_ADMIN`，不把 capabilities 写到可执行文件上。所有版本检查和校验路径拒绝 setuid/setgid 内核；Linux 无法确认文件 capabilities 时也拒绝执行。[systemd.exec](https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html)，[Linux capabilities](https://man7.org/linux/man-pages/man7/capabilities.7.html)

root 运行的 lazyclash 本身承担下载、配置和部署管理的 root 信任边界，管理员应从可信发行包安装到受保护的位置。内核降权、摘要和 unit 校验不能证明第三方二进制或订阅可信。系统实例不读取普通用户目录来继承配置，不使用用户可写 runtime 中的程序作为 root 生命周期入口。

## host TUN 与资源归属

CLI 开启要求 root 与 `--scope host --confirm-host`；TUI 使用默认取消的确认弹窗，说明整台主机、其他用户和服务的流量及 DNS 影响。TUN 控制权不从实例归属推断，订阅也不能授权开启。首版不提供 UID 范围、任意 namespace、普通用户 TUN 或自动实例交接。

TUN 配置启用受管 IPv4/IPv6 自动路由和 TUN 内 DNS 劫持，显式禁用对系统 resolver 的修改。`CAP_NET_ADMIN` 和 `CAP_NET_RAW` 具有主机级权限，不能视为某个设备的细粒度沙箱。设备可读写也不等于能够创建 TUN，真实建链、路由和 DNS 必须运行验证。[Linux TUN/TAP](https://www.kernel.org/doc/html/latest/networking/tuntap.html)

systemd 每次启动通过有限钩子取得本 namespace 的独占记录，检查已有 TUN、保留路由表和规则冲突；就绪后记录本次 invocation 的接口身份与网络资源。退出时仅清理已确认归属的资源，外部改动和未知归属会保留并报告。冲突不自动停止其他实例或 VPN。未完成清理的记录会阻止下一次接管，需要管理员检查，不能通过删除记录宣称网络已经恢复。

## 状态、凭据与验证

API secret、原始订阅与私有状态不出现在公开状态输出中。Mihomo 原始 stdout/stderr 指向 `null`，避免凭据进入 journal；退出诊断来自 systemd 状态，实时日志通过认证 API 读取并脱敏，不提供历史启动日志。

应用管理事务使用持久 journal，可处理失败立即回滚，中断后的恢复在下一次调用时触发。systemd 负责进程与退出清理，当前没有后台 daemon 持续修复管理事务。恢复失败保留证据；显式 Stop 优先于恢复，不能为回滚而复活已被要求停止的服务。

权限验收入口包括普通单测、Linux 全包、root 身份/数据测试和真实双作用域/TUN 端到端，实际结果见 [验证说明](verification.md)。缺少设置 `security.capability` 权限的测试必须明确 skip，macOS 单测和交叉编译不能代替 Linux capabilities、路由或清理实测。项目未发行，不提供旧安装兼容或自动迁移。
