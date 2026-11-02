# 持久运行与当前交付范围

项目目标是 root 与普通用户拥有独立实例：root 默认使用 system backend，普通用户默认使用 user backend，经 sudo 授权后可为同一实例启用 system/TUN。普通代理可并存，同一网络命名空间内的 TUN 管理权独占，覆盖范围另行配置。实例身份、backend 切换与 TUN 显式交接见 [服务 scope 与权限模型](service-scopes.md)。本页描述现有运行时，不表示 system scope 或 TUN 已实现；重构不承诺兼容现有 unit 或数据格式。

当前可用组合是 **Linux + systemd 用户服务 + Mihomo 的本机 HTTP/SOCKS 代理**。TUI 为主要控制入口，CLI 使用相同应用用例。没有 lazyclash daemon；systemd 直接启动 Mihomo，TUI 关闭、终端挂断及 CLI 返回均不发送 Stop。系统级服务、特权 TUN、路由/DNS 接管及自动系统代理尚未实现，不能把基本代理可用解释为这些能力已经交付。权限选择与后续条件见 [权限边界](permissions.md)。

## 部署与作用域

```sh
# 在普通用户的真实 Linux 登录会话中执行
lazyclash install --core-version latest
lazyclash doctor
# 由用户或管理员明确授权；可能要求密码或被机器策略拒绝
loginctl enable-linger "$(id -un)"
lazyclash service start
lazyclash status
```

安装只发布二进制、默认配置和用户 unit，不启动、不 enable、不修改 linger。Start/Restart 和 Enable 要求确认 linger；未知或关闭时返回明确错误，不偷偷更改用户登录策略。无桌面 SSH 会话不需要 GUI agent，能否自行 enable-linger 取决于机器的 logind/polkit 策略，必要时由管理员执行。用户 manager、会话 bus 或 XDG_RUNTIME_DIR 不可用时应修复登录会话，不能用 sudo 启动整个 lazyclash。

linger 维持用户 manager；`service enable` 单独控制本 unit 随用户 manager 启动。`service disable` 不停止现有内核。手动 Stop 会一直保持停止，直到显式 Start 或下一次启用的服务随 manager 启动；若希望重启机器也保持停止，应同时 Disable。此行为遵循 systemd 的服务启用语义，不把“当前停止”混同“永久禁用”。[loginctl](https://www.freedesktop.org/software/systemd/man/latest/loginctl.html)，[systemctl](https://www.freedesktop.org/software/systemd/man/latest/systemctl.html)

unit 使用 `Restart=on-failure`、两秒重试、60 秒内最多五次启动、15 秒停止超时及私有 umask。达到限速后状态明确为 failed，修复原因后可用 `systemctl --user reset-failed lazyclash-mihomo.service` 再 Start。systemd 明确 Stop 不触发 on-failure 恢复。[systemd.service](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html)

只控制当前用户 manager 内精确匹配本安装路径的 unit，拒绝外部 drop-in、其他安装路径和未经 reload 的编辑。已知旧版 unit 在只读查询时仍可验证，显式 Start/Restart/Enable 时才升级策略。没有第二个系统级 backend，也不会自动切换作用域。`--home` 与 `--service-name` 可以隔离实例，端口仍须不同。

## 状态与失败语义

- **期望状态**：`runtime-state.json` 绑定安装 ID 与服务名，保存用户运行意图、自启动偏好及最近操作的 pending/complete/failed；旧安装缺少记录时为 unknown，不根据一次观察伪造意图。
- **实际状态**：每次向 systemd 查询 ActiveState/SubState/PID、退出状态、恢复次数和启用状态，向认证 Mihomo API 查询版本、模式及代理组。配置代际保存下次启动加载的配置；API 反映当前进程状态。状态接口分别暴露这些信息以及 Drift。
- **操作状态**：启动先验证所属关系、配置、监听端口与持久运行条件，持久化意图后提交 systemd job，再等待 active + API + 代理 TCP 监听就绪；请求被接受不代表成功。观察被取消保留 pending，重新查询实际状态，不撤回已托管服务。启动失败不偷偷停止或改变用户意图。

重新打开 TUI 只查询状态与必要事务恢复，不自动 Start、不覆写当前配置。期望/实际差异展示后由用户决定重试；systemd 负责内核进程异常恢复，应用不另建竞争性的重启循环。配置切换在锁和代际事务内预先校验，失败回滚；恢复记录异常时保留文件并报告。Stop 以验证后的 unit 归属授权，即使配置、意图文件或恢复记录损坏仍请求停止，然后报告损坏，不能让配置修复阻止紧急停止。

退出 TUI 只取消终端交互和观察任务。配置应用等事务在可完成回滚时恢复旧配置和先前运行状态；明确 Stop 保存的意图优先，恢复不得把它重新启动。独立的 stop-intent.json 在配置或运行状态文件损坏时仍保留停止意图，只有通过预检查的显式 Start/Restart 才清除；修复文件后的状态查询也不得复活已停止服务。进程在请求提交之前被强制杀死的窗口无法保证 job 已提交，pending 状态与真实 systemd 状态用于定位；没有 daemon 为未提交请求代跑。

## 运行设置与数据

`mode [rule|global|direct]` / Settings 模式按钮在已运行时使用 PATCH 并读回确认，配置代际同时保存以供重启恢复；停止时只保存。显式设置的模式跨 Profile 应用保留。节点选择检查目标并读回，生成配置强制 `profile.store-selected: true`，固定 `installation/runtime` 目录承载 Mihomo 原生选择缓存。[Mihomo cache](https://wiki.metacubex.one/en/config/general/#cache)

缓存写入由 Mihomo 管理，API 不提供磁盘提交确认；选择读回确认当前运行值，不等于应用额外保证所有断电场景下已刷盘。旧版已应用配置如显式禁用了 store-selected，升级后应明确 Apply 当前 Profile，以生成启用缓存的新配置；打开 TUI 不自动迁移生效配置。相同名字的组共享 Mihomo 的缓存命名空间。

```sh
lazyclash mode direct
lazyclash mode
lazyclash select PROXY DIRECT
lazyclash profile add 'https://example.invalid/subscription' --name office --remember-source
lazyclash profile refresh office  # 校验并保存来源更新，不 apply、不重启
lazyclash profile apply office
lazyclash service enable
lazyclash service disable
lazyclash service stop
lazyclash uninstall               # 要求停止且禁用；保留 Profile、来源和偏好
```

默认 CLI Add 是一次性导入；明确 `--remember-source` 才将 URL/绝对路径保存在该 Profile 的私有 `0600` 元数据中。TUI 的 Add subscription 继续快速自动命名，New profile 可填写名称、描述并保存 URL/绝对路径来源。Profile 直接拥有名称、描述、地址、更新时间与内容 revision，没有独立来源管理。Refresh 验证旧快照、下载新内容并结合用户覆写校验，成功后原子提交；失败保持旧记录和当前运行配置。TUI 的 URL Add、New profile、卡片 Update 和 Update all 调用相同用例，显式 Apply 才加载草稿。删除 Profile 同时删除其地址与当前内容引用，不留下下载任务；未提供后台订阅调度。

原始订阅、配置、API secret、来源和偏好均不因 Stop 丢失。卸载验证所有代际及文件归属，只删除安装目录和绑定的运行意图；用户新增/改动的非缓存安装文件会阻止删除（runtime/ 中自有缓存随内核移除）。若 unit 已删而后续文件清理失败，错误明确标注需要手工清理，不声称完整成功；当前不提供卸载事务的崩溃自动续跑。

系统代理与内核启停分离：`env`/`init` 仅输出由用户显式应用的 Shell 环境，TUI 无法修改父 Shell，Stop 不自动清除父 Shell 环境；用 `eval "$(lazyclash env --unset)"` 清除。当前配置拒绝开启 TUN，Stop 没有本产品创建的 TUN/路由/系统代理副作用需要撤销；未来特权集成必须另行实现并验收其持久配置和清理。

## 可重复验收

```sh
make check
make race
make build
python3 -m unittest discover -s scripts -p 'test_*.py'
./scripts/verify-linux.sh YOUR_DEDICATED_VM
./scripts/e2e.sh YOUR_DEDICATED_VM --proxy-mode environment
```

真实测试只在显式选定的普通用户 Linux VM 中创建随机专用 unit 和临时目录，检查端口冲突并清理自有资源。`lifecycle-e2e.py` 由完整 e2e 入口调用：真实 PTY SIGHUP 后 PID 保持、动态 mode 不重启、配置应用/显式重启恢复模式与节点、SIGKILL 后 systemd 自动恢复、Enable/Disable 不改变运行 PID、Stop 等待超过 RestartSec 不复活。其余现有场景覆盖订阅、卸载、配置、端口变更、连接/规则/日志与 TUI。

**真实 SSH 最后一次登录退出和机器重启必须单独验收**，PTY SIGHUP、Shell 退出和 linger 检查不能代替它们：在专用机器 enable-linger 后 Start，记录 PID，断开所有该用户 SSH 会话，再次登录查询 status，确认仍为相同 PID；Enable 后重启机器确认 API 就绪，再 Disable/Stop、重启并确认未启动。不要对共享开发机器执行 terminate-user 或 reboot。TUN 创建、路由/DNS 清理和其他作用域/平台未实现，因此无对应实测通过结论。
