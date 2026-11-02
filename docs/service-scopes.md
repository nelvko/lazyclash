# 服务 scope 与权限模型（目标设计）

本文定义后续重构的目标，不代表当前运行时已经支持。当前实现仍只有非 root 的 systemd 用户服务，root 入口与 TUN 开启检查尚未调整。当前行为见 [持久运行](runtime-lifecycle.md)，当前权限边界见 [权限管理](permissions.md)。

项目目标是：**root 与普通用户都能使用，各自拥有独立实例；没有管理员权限也能使用本机 HTTP/SOCKS；普通用户经 sudo 授权后能够为自己的实例开启 TUN。** 普通代理允许多实例并存；同一网络命名空间内，lazyclash 只允许一个实例管理 TUN；流量覆盖范围单独配置。Linux/systemd 是本轮范围，其他平台不因此获得服务支持。

项目尚未发行，直接重构现有接口、unit、manifest 和目录结构。不实现旧格式读取、旧 unit 匹配、自动迁移、双写或旧命令别名。开发环境中的旧安装显式清理后重装；遇到旧数据或同名外部 unit 时明确拒绝，不猜测、不接管。

## 1. 分开实例归属、执行与流量范围

| 维度 | 含义 | 不应推导出的结论 |
| --- | --- | --- |
| 逻辑实例归属 | 配置、订阅与操作目标属于哪个用户的哪个实例 | 切换执行 backend 不等于把实例转交给 root |
| 服务 scope：`user` / `system` | 服务注册到哪个 systemd manager，由谁管理其生命周期 | system 服务不等于 Mihomo 以 root 运行 |
| 操作者与授权 | 当前调用者能否对指定实例执行指定动作 | 普通用户不等于永远不能开启 TUN；属于 sudo 组也不等于当前操作已获授权 |
| 内核权限配置：`proxy` / `tun` | Mihomo 实际获得的设备访问与 capabilities | root 启动界面不等于当前内核已经具备可用 TUN |
| 流量覆盖范围 | TUN 接管哪些 UID、地址或转发流量，以及 DNS 策略 | 实例属于 Alice 不等于只接管 Alice 的流量 |

`proxy` 是无网络管理能力的 HTTP/SOCKS 运行配置；`tun` 是经过明确授权的网络管理配置。两者不是服务 scope 的别名，也不与 Mihomo 的 rule/global/direct 运行模式混用。首版只通过 system scope 提供 TUN，不给用户安装的二进制直接 setcap。

## 2. scope 选择与实例身份

公开入口仍是 `lazyclash`，默认绑定当前调用者拥有的逻辑实例。首次安装时普通用户使用 user backend，root 使用 system backend；已有实例使用已提交的执行部署，不因为再次打开 TUI 就新建另一份服务。scope 是执行属性，不能同时充当“选择 root 的实例”的参数。

| 调用场景 | 目标与行为 |
| --- | --- |
| 普通用户首次安装 | 自己的 user service、配置与订阅；普通代理无需 sudo |
| root 首次安装 | root 自己的 system 实例；不依赖 root 的用户 manager、会话 bus 或 linger |
| 普通用户，开启自己实例的 TUN | 引导确认并授权切换到该实例专属的 system backend，不操作 root 已有实例，不对原 user unit 临时加权 |
| 普通用户，重新打开已切换 backend 的实例 | 继续绑定原 owner UID 和 installation ID；受保护的读取与操作经 sudo 授权，不要求重启整个 TUI |
| 普通用户，无 sudo 或授权被拒绝 | 原有 user 代理继续可用；特权操作报告需要授权，不改配置或服务 |
| 管理员，处理其他实例的 TUN 占用 | 明确选择并授权转移 TUN 管理权；不把管理员身份当作自动抢占许可 |

`sudo lazyclash` 按有效 UID 选择 root 自己的实例，不能因为 `SUDO_USER`、`HOME` 或 `--home` 恰好指向普通用户目录就接管该用户安装。普通用户应从原界面为自己的实例请求授权。TUI 和 CLI 状态必须分别展示实例所属用户、执行 scope、TUN 占用和流量覆盖范围。

逻辑实例引用绑定 `owner UID + installation ID`；system backend 同样保留普通用户的 owner UID。执行部署另绑定 scope、service name、配置代际和受保护路径。切换 backend 保留逻辑实例身份，不能生成一个独立的 root 实例；同名 unit 也不能作为归属依据。查询、锁、确认计划、事务、恢复和 API 访问都校验这两层绑定。`--home` 只选择用户数据位置，不能给系统操作指定任意特权读写目录。

Alice、Bob、root 的普通代理可以同时运行，只要监听地址和端口不冲突；发现冲突时报告，不停止其他实例腾位置。user service 隔离管理与配置归属，但 loopback TCP 监听本身并不限制客户端 UID，不能据此宣称代理只允许该用户连接。

## 3. 服务生命周期

| 项目 | user scope | system scope |
| --- | --- | --- |
| 服务管理器 | 当前普通用户的 `systemctl --user` | 系统 `systemctl` |
| unit | 用户配置目录中的 `systemd/user/` | root 管理的 `/etc/systemd/system/` |
| Mihomo 身份 | 当前普通用户 | 实例专用的非登录、非 root 服务用户 |
| SSH 注销后的持久性 | 显式配置并检查 linger | 不依赖登录会话或 linger |
| 自启动 | 本 unit 的 enable 状态，另需 user manager 生命周期条件 | 本 unit 的 enable 状态 |
| TUN | 不直接支持；需要将本实例的执行切换到 system backend | 授权、取得管理权并通过环境检查后支持 |

安装、查询或打开 TUI 不自动 start、enable 或修改 linger。root 也遵守相同的操作意图语义。切换 scope 不意味着启用 TUN；切换 TUN 不意味着启动一个原本停止的内核。

root 可以运行 TUI/CLI，不再全局拒绝 UID 0。普通用户使用自己实例的 system backend 时保持界面非 root，只将必要用例交给受保护的权限入口。Mihomo 的权限来自受管 unit，而不是调用者的 UID。同一逻辑实例只允许一个活动执行 backend，停止的旧 user unit 不能与 system backend 同时自启动。

### 内核托管与管理事务

当前阶段采用 **每个活动内核独立 systemd unit + 按需 lazyclash 事务执行器**。进程托管与领域协调是两种职责，是否需要常驻 daemon 不决定内核是否应拥有独立 unit。

| 职责 | 执行位置 |
| --- | --- |
| 计划审阅、授权、请求操作与观察结果 | TUI/CLI，通过共享应用用例调用 |
| 配置构建、归属校验、发布、API 就绪和回滚 | lazyclash 应用用例及独立事务执行器 |
| 配置应用、backend 切换和 TUN 交接等已接受的多步操作 | systemd 托管的有限任务，执行完毕即退出；普通用户任务与特权任务保持各自权限边界 |
| 内核进程启动、异常重启、限速、停止超时、cgroup 与自启动 | 每个内核自己的 user/system unit |
| 每次启动的 TUN 授权检查、退出后的自有资源清理 | 受管 unit 的生命周期入口，使用同一套受保护协调规则 |
| owner、部署代际、TUN 选择、操作结果与 Stop 意图 | 相应权限范围内的持久记录，不能只放在客户端或执行器内存中 |

systemd 已提供进程监督和生命周期钩子；Mihomo 官方运行服务示例同样直接用 unit 启动内核。lazyclash 仍须实现配置有效性、API/代理就绪及网络清理等领域行为，不能把 systemd active 当作业务成功。[systemd service](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html)，[Mihomo 服务示例](https://wiki.metacubex.one/startup/service/)

当前代码的恢复主要由下次应用用例触发，这不是目标架构所需的离线恢复保证。多步变更要先持久保存计划与恢复点，再交由对应 manager 的独立任务执行；只有计划和任务均已接受才向客户端确认受理。客户端通过 operation ID 观察结果，断开终端不取消已接受任务；明确取消或 Stop 另写持久意图。独立执行入口复用应用用例，界面层不编排 shell 脚本或业务 CLI。

systemd 的 transient service 能使任务脱离发起客户端运行，但其运行定义不会自动变成跨重启的业务事务日志。执行器崩溃和机器重启后的恢复仍需持久记录、恢复入口与真实 unit/network 状态核对；不能承诺仅凭一个 transient unit 就能自动回滚。[systemd-run](https://www.freedesktop.org/software/systemd/man/latest/systemd-run.html)

启动门禁和清理必须覆盖直接启动 unit、自动重启、开机启动及部分启动失败。`ExecStartPre` 可执行有限检查，不能靠在那里启动后台持锁进程维持整个服务的独占；TUN grant 必须在内核运行和清理期间持续有效。`ExecStopPost` 可用于异常退出及部分启动失败后的清理，但仍须处理清理自身失败和整机宕机。[生命周期钩子](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html#ExecStopPost=)

当前不新增一个全局 root daemon 来自行启动、等待和重启所有内核：普通用户的无特权代理因此会增加系统部署依赖，每实例的 cgroup、退出状态和重试策略也需要重新安排。若 daemon 与全部子内核位于同一个 service cgroup，停止或重启该 daemon 默认会波及这些内核；不通过降低 `KillMode` 保留失去托管的子进程。[systemd kill](https://www.freedesktop.org/software/systemd/man/latest/systemd.kill.html#KillMode=)

也不将整个 TUN 内核固定在全机唯一的共享 executor unit 中。多个逻辑实例独立存在时，原 owner 关闭或让出 TUN 后仍可能继续普通代理；共享 executor 会要求迁移整个内核或长期占住这个位置。首版保留每实例 unit，让独占只约束 TUN 管理权。转移后旧实例的运行/停止与自启动意图需要明确列入计划；继续代理时清除其 TUN 策略和权限，不强制迁回 user manager。

不建常驻 daemon 是当前需求下的决策，不是永久架构禁令。若后续确定需要持续策略协调、后台订阅更新或多客户端事件服务，可以引入一个专做控制面的 broker；内核仍由独立 systemd units 托管。broker 自己的升级/重启应与既有内核生命周期分离，并继续使用持久事务和 unit 启动门禁。systemd socket activation 可作为按需启动 broker 的方式，但不代替请求身份与操作授权。[systemd socket](https://www.freedesktop.org/software/systemd/man/latest/systemd.socket.html)

## 4. 系统存储与特权入口

普通用户的 Profile、订阅和编辑状态仍由该用户管理。该用户的特权 backend 与 root 自己的实例分别使用独立受保护部署，目录中的实例标识由校验过的 owner UID 和 installation ID 派生：

- unit、受保护权限入口和策略位于 root 管理的系统目录。
- `/etc/lazyclash/instances/<instance-key>/` 保存受管配置与权限策略；按实例服务身份授予必要的读取权。
- `/var/lib/lazyclash/instances/<instance-key>/` 中的二进制发布、配置代际、manifest 和事务记录由 root 管理；只有单独的运行缓存目录可由该实例 Mihomo 写入。
- `/run/lazyclash/` 保存系统级锁与运行期资源归属。秘密和原始订阅不作为公共实例发现信息。

特权服务不得引用普通用户可替换的二进制、配置、`current` 链接、provider 输入或其祖先目录。root 创建文件也不够：实例内核运行身份同样不能改写受保护的发布目录。路径校验、受限文件打开与实际发布必须抗符号链接替换和检查/使用竞态，不能把当前同 UID 的防误操作检查当作权限隔离。

普通用户可以准备下载与配置候选，但授权侧必须接收有大小限制的内容/文件描述符，复制到受保护暂存区，独立检查后原子发布。不能让 root 跟随客户端提供的任意路径，也不能让授予网络能力的内核访问任意本地 provider 路径。provider、规则及其他文件引用需要纳入受管路径和发布策略；自治下载的缓存与可信输入分开。

配置校验、版本探测和下载候选的执行使用无网络管理能力的隔离身份，不因为 helper 是 root 就以 root 执行未发布的 Mihomo。自定义本地内核发布到 system 实例需要单独审阅授权；文件摘要不等于二进制可信。

系统管理采用 root 管理、按需运行的 helper 入口与受托管事务执行器，复用应用用例；当前不新增常驻 lazyclash daemon。不能简单在当前 `exec.Command` 前拼 `sudo`：系统用例同时涉及文件发布、unit、控制 API 和失败恢复，而不只是 `systemctl`。

特权入口遵守以下约束：

- 首次安装或更新 helper 是明确的管理员部署动作。不能静默 sudo 当前工作目录或普通用户可写的 lazyclash；不自动写宽泛 sudoers 或授予任意命令执行权。
- 普通用户通过 sudo 调用固定的受保护入口，使用有版本、有长度限制的结构化请求。入口校验实际调用者、逻辑 owner、执行部署、动作、期望代际及输入；owner 不能只取自请求中自报的 UID，也不能把 sudo 后的有效 UID 0 当作实例 owner。不接受任意 shell、unit 路径、环境变量或 systemctl 参数。
- root 直接执行同一套系统用例的验证与事务，不需要 sudo。授权和业务预检查分离；sudo 存在、组成员身份或一次成功认证都不能代替每次请求的授权结果。
- sudo 密码由 sudo 在真实终端读取，TUI 暂时交还终端；不在 Bubble Tea 输入框收集密码，不经 stdin、参数、日志或文件转交。非交互环境无法完成授权时立即返回需要授权，不无限等待。
- helper 控制 API 访问和流式日志也必须绑定实例及操作。Mihomo 的 API secret 不是只读权限令牌，不能为了读取状态就发给未授权用户，否则其可绕过 TUN 的操作授权。
- 取消、授权过期和拒绝不能留下已启动内核、半发布配置或 root-owned 用户文件。已提交的系统事务由受保护执行侧完成有界恢复，不能依赖 TUI 仍然在线。

第一版只做 sudo 授权。polkit 可在有明确需求时单独评估，不并列维护两套尚未验证的授权流程。

## 5. TUN 的权限与状态

system scope 的 `proxy` 配置不授予网络管理能力。`tun` 配置优先让同一实例的专用非 root 用户通过 systemd 获得必要的设备访问、`CapabilityBoundingSet` 和 ambient capabilities；不常驻完整 root，不使用 `CAP_SYS_ADMIN`，不把能力附着到用户可写文件上。[systemd capabilities](https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html#Capabilities)

权限集合按实际 Mihomo 版本、TUN stack、自动路由和 DNS 行为在 Linux 验证后确定。`CAP_NET_ADMIN` 涉及主机网络管理，不是仅允许某个 TUN 设备的沙箱；`CAP_NET_RAW` 等只在实测需要时加入。`NoNewPrivileges=true` 不能代替已有能力的约束，也不能阻止进程使用启动时已被授予的 ambient capabilities。

开启前至少检查 `/dev/net/tun`、实际创建设备权限、内核/容器限制、接口与路由冲突、已有 TUN 管理者和 DNS 策略。root 或 sudo 授权通过不保证这些检查通过。

### TUN 管理权

同一网络命名空间内只允许一个 lazyclash TUN 管理者。这是本项目的协调策略，不是 Linux 只允许一个 TUN 设备。首版只处理系统 backend 所在的主机网络命名空间，不增加任意 namespace 创建、切换或跨容器协调框架。

- 系统侧协调同时校验网络命名空间、owner UID、installation ID、unit 和本次执行身份，覆盖启动、自动重启、开机启动、停止清理与交接。锁不能由 TUI 持有，也不能只在点击开启或 helper 请求期间持有；用户绕过 TUI 直接启动受管 unit 时同样受约束。
- 可有多个已安装的特权 backend，但同一 namespace 只有当前管理者能修改 TUN 网络资源。管理权持续到内核退出且自有资源清理完成；崩溃导致锁释放，不等于残留路由、防火墙或 DNS 已被清理。新启动必须先核对归属记录、真实服务和网络状态，无法确认时拒绝接管并报告恢复需求。
- 冲突只公开必要信息，例如“root（UID 0）的实例 <id> 正在运行 TUN”，以及 unit、当前状态和流量覆盖范围；不公开订阅、原始配置或 API secret。root 和其他管理员也不能自动抢占。
- 转移需要管理员明确授权并绑定原占用者和目标实例；重新校验后，停止原执行、阻止其自动重启/开机再竞争、完成自有资源清理，再将管理权交给目标并验证就绪。期间状态显示为交接中；失败保留恢复记录，禁止两个实例同时管理，显式 Stop 仍优先于恢复。
- 运行期锁不会跨机器重启保存选择。管理权选择和交接结果须持久化；重启后仅被选定的实例按其 enable/运行意图尝试恢复，其他已安装 backend 不通过启动顺序竞争管理权。
- 协调协议只能约束本项目的受管 backend。其他 VPN、Clash 或手动网络设置仍需冲突检测；不把“本项目锁空闲”当作网络空闲，不删除无归属的外部资源。

其他独立 HTTP/SOCKS 实例可在端口不冲突时继续存在；它们的出站连接是否经过活动 TUN，取决于流量覆盖策略，不保证网络行为互不影响。

### 流量覆盖范围

TUN 的覆盖范围是显式运行策略，与实例 owner 和 service scope 分开保存、授权与展示。开启确认必须列出接管哪些流量及 DNS 影响；未选择范围时不能从 owner UID 猜测，更不能静默接管整机。订阅不能扩大已授权的范围。

Linux 可以通过 Mihomo 的 `include-uid` / `exclude-uid` 配合 `auto-route` 表达用户筛选，但不把 UID 筛选宣传为严格的网络隔离。由系统 resolver 代发的 DNS、转发流量和 IPv6 需要分别定义并实测。首版优先实现经过验证的明确覆盖选项，不为尚未验证的组合生成成功状态。[Mihomo TUN 配置](https://wiki.metacubex.one/config/inbound/tun/#include-uid)

TUN enable、受管接口、自动路由和 DNS 接管策略属于应用运行设置，不由订阅中的 `tun.enable` 自动授权。导入可以保存原始 TUN 配置作为输入；普通 user 构建不能因此开启 TUN，system 构建也必须依据明确授权的运行策略生成最终配置。Mihomo 的 DNS 劫持与修改操作系统 resolver 是不同动作，不能将前者成功描述成后者已经实现；对于提供 `dns-mode` 的内核版本也必须显式生成所选策略，不依赖版本默认值。[Mihomo DNS 策略](https://wiki.metacubex.one/config/inbound/tun/#dns-mode)

TUN 状态区分期望、实际和操作结果：

- 内核停止时，开启 TUN 只发布下次启动所需配置与权限，不隐式 start 或 enable，也不提前修改网络；这不表示已取得管理权。若已有其他管理者，不能借“下次启动”安排隐式抢占。
- 内核运行时，明确确认可能的连接中断和主机网络影响，再发布配置与 unit 权限、执行重启并验证；不能只 PATCH `tun.enable` 后显示成功。
- 就绪包含实际 TUN 设备、预期路由/DNS 行为和代理可用性；systemd active、API 返回成功或 `mihomo -t` 均不足以单独证明 TUN 可用。
- 关闭 TUN 后仍保持 system scope；重启/停止流程清理本实例的设备、路由和 DNS 副作用，移除不再需要的能力。不自动回到 user scope。
- 失败恢复旧配置、权限与运行意图，只回滚明确属于本实例的网络资源；遇到外部改动保留证据并报告，不能覆盖整机路由/DNS 快照。恢复记录由系统侧维护。
- 显式 Stop 优先于 pending 的开启或恢复，不能因回滚重新开启用户已要求关闭的 TUN。

## 6. 同一实例从 user backend 切换到 system backend

普通用户在 user scope 点击开启 TUN，不应只收到“换成 root 运行”的错误，也不应偷偷操作 root 的实例。交互必须说明本实例的目标 system backend、配置发布范围、停止/自启动调整、TUN 的实际覆盖范围及所需 sudo 授权。若已有其他 TUN 管理者，先报告占用；管理员转移管理权是单独明确的决定。

backend 切换是同一逻辑实例内的显式操作，与不同实例之间转移 TUN 管理权分开，不是旧版本兼容迁移：

1. 只读准备计划，绑定 owner UID、installation ID、两端执行部署、代际、端口、运行和 enable 状态。目标部署归属不符或来源发生变化时拒绝覆盖，重新审阅。
2. 完成授权，将二进制、有效配置与所需资源复制校验到独立受保护部署；保留逻辑 installation ID，重新生成执行部署的 API secret，不引用用户可写文件。系统侧记录已授权发布的代际，用户编辑不会直接改变运行配置。
3. 受保护事务记录恢复点；切换前停止来源 backend，来源若已启用自启动则在确认范围内 disable，防止重启后抢占端口。必须操作原用户的 manager，不能用 root 的 `systemctl --user` 冒充。
4. 发布并应用目标的 TUN 策略。来源正在运行才在取得管理权后启动目标；来源停止则目标保持停止。目标 enable 状态按确认计划设置，不能隐式启用服务或 linger。
5. 运行中的目标通过完整 TUN 就绪检查后提交执行 backend 的选择；停止目标则确认发布完成且未修改网络。界面仍显示原逻辑实例与 owner；用户 Profile 数据保留，不建立共享可写目录。
6. 失败先停止并清理目标网络资源，再恢复来源的原配置、运行与 enable 状态。跨 manager 的步骤不是原子操作，必须通过持久事务与恢复点处理；中断后不允许同一实例的两个 backend 无主运行。恢复失败明确报告，不伪造回到原状。

日后如需回到 user backend，另做显式切换；单纯关闭 TUN 不触发反向复制。Alice、Bob、root 的独立代理实例仍可共存，单一活动 backend 约束只针对同一逻辑实例；TUN 独占则跨所有这些实例。

## 7. 重构边界与实施顺序

不先制作通用跨平台框架。围绕 Linux 的两种 scope 重构现有共享用例：

1. 定义逻辑 owner/实例身份、执行 scope、部署路径、内核权限和流量覆盖策略，替换 `app.New(home, name)` 中的隐式用户语义以及写死的 capability scope。manifest、事务和锁直接采用新格式，删除旧 unit/格式兼容分支。
2. `service` 实现明确的 user/system manager 操作与 unit 验证；`store` 分开用户数据与系统受保护发布。先验证 root 的 system 普通代理和普通用户的 user 普通代理。
3. 在应用用例边界接入授权和受保护 helper，覆盖系统安装、更新、配置发布、启停、API、诊断和恢复。把已接受的多步事务交给 systemd 托管的执行器，补上客户端离线、执行器崩溃与机器重启的恢复入口。CLI/TUI 调用相同用例，不各自拼 sudo 或启动自身 CLI。
4. 实现系统侧 TUN 管理权协调、占用者查询与显式转移；将 Mihomo builder 从“永远禁用 TUN”改为接受已验证的权限和覆盖策略，覆盖检查、实际状态、网络清理和恢复。
5. 加入同一实例 user → system backend 的确认与切换流程，更新 TUI/CLI 的 owner、scope、授权、TUN 占用与覆盖状态；界面控件优先复用合适的 Charm Bubbles。

scope 与策略类型是本项目内部契约，不把 sudo/systemd 的第三方类型泄漏到界面。不得仅凭有效 UID 填写 `TUN.Supported=true`；能力展示至少区分未实现、需要系统实例、需要授权、环境不支持、可执行及运行失败。

## 8. 验收要求

- root 无用户会话运行：能安装、启动、停止和重启 system 实例；普通用户无 sudo 时完整使用 user 代理；两者互不误操作。
- 普通用户具有 sudo 时：在原 TUI 中完成授权，为自己的实例使用 system backend/TUN，owner 和逻辑实例身份不变且不接管 root 的实例；无 sudo、权限不足、密码错误、取消、授权过期、无 TTY 都给出准确结果，失败前后无未授权副作用。
- 验证目录和任一祖先可写、符号链接/竞态替换、provider 越界、伪造实例/代际、unit/drop-in 修改、秘密泄露及恶意 helper 请求会被拒绝。
- 验证 Alice、Bob、root 的代理并存、端口冲突、跨 UID/同名 unit 的归属校验，以及同一逻辑实例不能双 backend 活动。
- 验证并发开启 TUN、占用者提示、管理员显式转移、交接中止、进程崩溃及清理未完成；直接启动 unit、自动重启和机器重启也不能绕过单管理者约束。不接管外部服务，不依赖旧格式兼容。
- 验证任务受理边界：客户端断线、SIGKILL、任务重复提交、执行器崩溃和机器重启后，已受理操作按持久记录完成或恢复；未受理操作不能被描述为已提交。观察取消与业务取消分开，Stop 优先级覆盖独立任务执行。
- 独立验证 owner 与流量范围的不同组合，尤其是整机覆盖、UID 筛选、系统 resolver 代发 DNS，以及其他普通代理的出站流量是否符合所选策略。
- 在真实 Linux/Mihomo 中验证缺失 TUN 设备、capabilities/容器限制、真实建链、IPv4/IPv6 路由、DNS、停止清理、崩溃恢复、重启和显式 Stop 优先级。
- 验证 SSH 注销、TUI 退出和机器重启后的预期状态；system scope 不依赖 linger，user scope 不暗改 linger，自启动只按确认意图生效。

模型测试、helper/权限测试和真实 Linux 端到端分别报告；macOS 单测、配置校验或交叉编译不能代替 TUN/systemd 的运行验收。
