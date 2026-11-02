# Linux 权限边界

## 项目目标与当前实现

目标是同时支持 root 和普通用户，各自拥有独立实例：root 默认使用 system backend，普通用户无需提权即可使用自己的 user service，也能经 sudo 授权为同一实例部署 system backend 并开启 TUN。逻辑 owner、执行 scope、操作者授权、内核权限与流量覆盖分别建模；普通用户不自动操作 root 的实例。具体契约见 [服务 scope 与权限模型](service-scopes.md)。

以下描述的是**当前代码边界**，不是长期目标。项目未发行，后续直接重构 unit、manifest、存储和权限入口，不保留旧格式或旧行为兼容层。

本版只实现普通用户的 systemd user service。HTTP/SOCKS 监听与认证控制 API 均限定本机；TUI、CLI、Mihomo 运行在同一普通用户权限下。TUN、系统路由、DNS 接管、系统代理修改和系统级服务仍未实现，不能把以下未来方案视作已交付能力。

## 当前策略

- 不调用 sudo、pkexec、setcap，不安装特权 helper，不自动授权或切换到系统级作用域。Linux TUI 与实际 service backend 拒绝 root 运行；只读诊断及 Shell 变量输出仍可执行；版本检查与配置校验前拒绝 setuid/setgid 内核文件，并在 Linux 检查、拒绝文件 capabilities（无法确认时失败）。开启 TUN 返回明确的不支持错误，不保存虚假的开启状态。
- 服务使用 `NoNewPrivileges=true`，阻止该进程及其后代通过 exec 获得 setuid 或文件 capabilities。它不限制一个已经以 root 启动的 TUI，也不覆盖独立执行的版本检查/配置校验进程，因此不能代替应用入口与内核执行路径的权限检查。[systemd.exec](https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html#NoNewPrivileges=)
- 安装目录、配置、Profile、订阅与用户 unit 都由当前用户管理。文件摘要、精确 unit 内容、加载路径和 drop-in 检查用于避免误操作其他安装；它们不是对抗同 UID 恶意进程的隔离机制。
- 启动前检查用户 manager 与 linger；未确认 linger 时拒绝承诺持久运行，并提示用户明确执行 `loginctl enable-linger <UID>`。lazyclash 不修改此全局用户设置。linger 让 manager 在注销后继续存在，也允许其随开机启动；它本身不等于启用某个具体服务。[loginctl](https://www.freedesktop.org/software/systemd/man/latest/loginctl.html)
- 原始 Mihomo stdout/stderr 仍指向 `null`，避免 provider URL、订阅令牌或控制凭据写入 journal。失败通过 systemd 的 `Result`、`ExecMainStatus`、服务子状态与重启次数诊断；这不能提供完整启动错误正文。运行日志由现有认证 API 读取并在进入界面前脱敏。

## 提权方式比较

以下结论依据当前文件布局推导，并非这些机制本身必然不安全。

| 方式 | 当前布局中的攻击面 | 采用条件 |
| --- | --- | --- |
| 整个 TUI 长期 root 运行 | 下载器、YAML/订阅解析、终端交互、HTTP 客户端和本地内核校验都进入 root 信任边界；用户可写可执行文件会被 root 执行 | 当前拒绝此运行方式 |
| 每次操作 sudo | 限定的受保护服务控制命令可以缩小授权范围；但 sudo 执行用户可写 lazyclash/Mihomo、任意命令或用户提供的 unit 路径仍允许扩大权限 | 将来仅允许固定操作与固定、受保护的目标；不能直接 sudo 当前用户安装 |
| 对 Mihomo setcap | 权限附着在可执行文件上，直接运行及校验路径也可能获得能力；配置仍由用户控制，更新替换 inode 还可能丢失能力 | 当前不采用；不能把它当作普通用户 TUN 开关 |
| 专用非 root 用户的 system service | 能把长期权限集中到内核，但被授予的网络能力仍具有系统影响；root-owned unit 引用用户可写二进制、配置、current 链接或任一祖先目录，会破坏授权边界 | 未来须新增独立受保护部署、明确发布事务和能力约束 |
| 最小 helper + 按操作授权 | 可以把安装/发布/网络修改限制为固定请求；其 IPC、路径处理、竞态检查和参数校验成为新的特权入口 | 确有系统级需求后再实现，拒绝任意 shell/路径/命令参数，按请求验证身份与权限 |

`CAP_NET_ADMIN` 涉及接口、路由与防火墙操作，`CAP_NET_RAW` 涉及原始套接字；它们不是只允许“本程序使用 TUN”的细粒度权限。不得习惯性授予 `CAP_SYS_ADMIN` 或完整 root。未来所需集合必须根据实际 Mihomo 版本、TUN stack 与网络功能实测决定。[Linux capabilities](https://man7.org/linux/man-pages/man7/capabilities.7.html)

## system scope 与 TUN 的实现前提

后续允许 root 运行 TUI/CLI，但不会因此让 Mihomo 常驻完整 root。普通用户为自己的实例使用 system backend 时，通过固定、受保护的 helper 经 sudo 授权，不能把整个用户 TUI 或用户可写二进制直接 sudo 后当作实现。执行 scope 变化不改变 owner；system 普通代理和 system TUN 使用不同权限策略，关闭 TUN 不自动改变 backend。

新增系统级部署时，应将 unit、可执行文件和生效配置发布到普通用户不能替换的目录；包括其祖先目录、符号链接目标以及内核会继续读取的 provider 配置。服务使用专用非 root 用户，只有必要的运行数据目录可写。当前用户空间中的下载与 Profile 必须作为不可信输入，经过校验和明确授权后复制发布，不能让特权服务跟随用户 `current` 链接。只对配置文件执行 chown 不足以保护可被用户替换的父目录。

系统级控制授权应绑定逻辑 owner、固定实例、执行部署和动作。同一逻辑实例的 user/system backend 只允许一个活动；不同用户的普通代理可以并存。同一网络命名空间内只允许一个 lazyclash TUN 管理者，占用时提示用户和实例，管理员明确授权后才能转移；独占必须覆盖自启动、自动恢复和停止清理，不能只检查 TUI 状态。该协议不约束其他 VPN 客户端，外部资源冲突仍须检测并拒绝破坏性接管。

流量覆盖策略必须独立配置和授权，不能根据 owner 推导；UID 筛选不等于严格网络隔离。授权通过也不意味着输入安全；即使使用 polkit，特权机制仍须校验调用者与每次请求。SSH 场景可能没有图形认证 agent，需单独设计交互流程。[polkit](https://www.freedesktop.org/software/polkit/docs/latest/polkit.8.html)

验收必须包含 `/dev/net/tun`、实际设备创建权限、路由/DNS 的应用和失败回滚、停止后的清理，以及内核恢复后的重新应用。仅存在设备文件或可读写它并不代表具备创建网络设备的权限。Linux TUN 文档区分设备访问与 `CAP_NET_ADMIN`；资源的清理也需按持久设备及内核实际创建的副作用验证。[Linux TUN/TAP](https://www.kernel.org/doc/html/latest/networking/tuntap.html)

权限测试分别验证提权模式文件被拒绝且未执行。Linux 文件 capabilities 测试需要可设置 `security.capability` 的文件系统和测试身份，否则明确 skip；在 macOS 通过其余单测不能证明该 Linux 检查已经实测。
