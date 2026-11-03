# 持久运行与首发交付范围

Linux/systemd 支持普通用户的 user 代理和 root 的独立 system 实例。system Mihomo 使用专用非 root 账号；明确确认后可开启整台主机的 host TUN。`sudo lazyclash` 选择 root 实例，不复制原用户配置、不切换原服务。路径与权限见 [服务 scope](service-scopes.md) 和 [权限边界](permissions.md)。

每个内核由自己的 systemd unit 直接启动。TUI 关闭、终端挂断或 CLI 返回均不发送 Stop；没有常驻 lazyclash daemon 或独立 helper。系统代理自动设置、普通用户 TUN、UID 流量筛选和其他平台/init 不在首发范围。项目未发行，不读取或迁移旧 unit、manifest 和部署布局。

## 安装、启动与自启动

```sh
# 普通用户，在真实 Linux 登录会话中执行
lazyclash install --core-version latest
loginctl enable-linger "$(id -un)"  # 用户或管理员明确授权
lazyclash service start
lazyclash status

# 管理员已安装到受保护路径：独立的 root system 实例
sudo /usr/local/bin/lazyclash install --core-version latest
sudo /usr/local/bin/lazyclash service start
sudo /usr/local/bin/lazyclash status
```

安装只发布内核、默认配置与 unit，保持停止，不 enable、不修改 linger。普通用户的 Start/Restart/Enable 要求 user manager 可用且 linger 已明确启用；root 通过系统 manager 操作，不依赖用户 bus 或 linger。`service enable/disable` 只改变当前 unit 的自启动，不启动或停止现有进程。

手动 Stop 保持当前服务停止，直到显式 Start 或已启用的服务在下次 manager/机器启动时运行；希望开机后也停止时同时 Disable。用户实例使用 `$XDG_DATA_HOME/lazyclash` 或 `~/.local/share/lazyclash`，root 默认 `/var/lib/lazyclash/<service-name>`。自定义实例需同时隔离数据、unit 与端口，不能通过同名或 sudo 推断其他实例归属。[loginctl](https://www.freedesktop.org/software/systemd/man/latest/loginctl.html)

unit 使用 `Restart=on-failure`、两秒重试、60 秒内最多五次启动、15 秒停止超时和私有 umask。明确 Stop 不触发自动重启；达到限速后显示 failed，修复后可对所属 manager 的 unit 执行 reset-failed 再 Start。归属检查拒绝路径、精确内容或 drop-in 与受管定义不符的 unit，不接管外部服务。[systemd.service](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html)

## 状态与管理事务

- **期望状态**：`runtime-state.json` 绑定安装 ID 与服务名，记录运行意图、自启动偏好及最近操作状态。缺少可信记录时为 unknown，不从一次观察伪造意图。
- **实际状态**：systemd 提供 ActiveState/SubState/PID、退出结果、重启次数和启用状态；认证 Mihomo API 提供运行版本、模式、TUN 和代理组。保存的配置与当前运行值分开显示，差异报告为 Drift 或 TUN 状态异常。
- **操作结果**：启动先检查归属、配置、端口和当前作用域条件，保存意图并提交 job，再等待 systemd、API 与代理监听就绪。job 被接受不代表业务成功；观察被取消不撤回已托管进程。

配置、内核与 TUN 变更在应用锁和持久 journal 下执行。普通错误或取消尽力恢复旧配置、权限与先前运行状态，失败保留记录并明确报告。进程强制终止后，下次应用调用才核对并恢复遗留 journal；没有脱离客户端的事务执行器承诺立即完成未结束的管理操作。systemd 对已启动内核的进程监督与生命周期钩子继续独立运行。

重新打开 TUI 不自动 Start 或覆盖配置。独立 `stop-intent.json` 保存紧急停止意图，即使配置或其他状态损坏也优先尝试停止经验证的自有 unit；只有通过预检查的明确 Start/Restart 才清除。恢复不得重启已明确停止的服务。

## TUN 的启动与退出

```sh
sudo /usr/local/bin/lazyclash tun on --scope host --confirm-host
sudo /usr/local/bin/lazyclash tun off
```

开启前明确确认整台主机的路由、DNS 以及其他用户和服务影响。停止时只保存下次启动策略，保持停止且不提前修改网络；运行时先停旧进程，再发布 unit 能力和配置并重启。普通系统代理无额外 capabilities；TUN 才授予专用服务账号 `CAP_NET_ADMIN CAP_NET_RAW`，停用后移除。

system unit 的 `ExecStartPre`、`ExecStartPost`、`ExecStopPost` 调用受保护 `installation/lazyclash` 的有限内部命令，分别检查/登记 TUN 管理权、等待并记录就绪、清理本次资源。直接 systemctl、自动重启和开机启动也经过这些钩子。同一 namespace 的其他管理者、已有 TUN 设备或保留路由区间阻止启动；不会自动抢占。明确停止原实例并确认清理后，再为目标实例开启；自启动也须相应明确调整。

退出钩子覆盖正常 Stop、异常退出和部分启动失败，只清理归属一致的设备、IPv4/IPv6 规则及路由。外部改动、无法确认的归属或清理失败保留记录供管理员处理。DNS 劫持发生在 TUN 内，不修改系统 resolver 或 `/etc/resolv.conf`；不承诺关闭 TUN 能撤销用户或其他 VPN 的网络变更。

## 模式、Profile 与缓存

`mode [rule|global|direct]` 在运行时 PATCH 并读回，配置代际同时保存；停止时仅保存。显式模式和 TUN 策略在 Profile 应用后保留。节点选择检查目标并读回，生成配置启用 `profile.store-selected: true`，稳定的 `installation/runtime` 承载 Mihomo 原生缓存。读回只确认当前运行值，不等于所有断电场景均已刷盘。[Mihomo cache](https://wiki.metacubex.one/en/config/general/#cache)

```sh
lazyclash mode direct
lazyclash select PROXY DIRECT
lazyclash profile add 'https://example.invalid/subscription' --name office --remember-source
lazyclash profile refresh office  # 校验并保存草稿，不 apply、不重启
lazyclash profile apply office
lazyclash service enable
lazyclash service disable
lazyclash service stop
lazyclash uninstall               # 要求停止且禁用；保留 Profile、来源和偏好
```

订阅与来源按 Profile 私密保存。Refresh 下载、构建并校验后原子提交草稿，失败保留旧内容和运行配置；明确 Apply 才加载。没有后台订阅调度。卸载仅删除经归属验证的安装与运行状态，保留用户数据；外来文件或归属变化会阻止删除，部分失败会说明手工清理范围。卸载没有崩溃自动续跑承诺。

系统代理与内核生命周期分开：`env` / `init` 只输出用户明确应用的 Shell 环境，不能修改父 Shell；Stop 不自动清理父 Shell，可执行 `eval "$(lazyclash env --unset)"`。

## 可重复验收

```sh
make check
make race
make package VERSION=dev
./scripts/verify-linux.sh YOUR_DEDICATED_VM
./scripts/e2e.sh YOUR_DEDICATED_VM --proxy-mode environment
./scripts/service-scopes-e2e.sh YOUR_DEDICATED_VM /absolute/packaged/lazyclash /absolute/vm/mihomo
```

普通用户代理、root 系统部署与 host TUN 使用各自的真实测试入口，隔离数据目录、服务名和端口，检查并清理自有资源。双作用域完整验收仍需真实 Mihomo、设备、IPv4/IPv6 路由、DNS、失败恢复与 Stop 清理，当前运行进度和已通过范围见 [验证记录](verification.md)。SSH 最后会话注销、机器重启和其他平台分别验证；PTY SIGHUP、linger 检查、单元测试或交叉编译均不能代替这些结论。
