# lazyclash 工程质量审计 · 2026-11-26

**建议优先修三处会改变用户结果的错误。** 同一份订阅经过配置构建后，真实 Mihomo 向另一台 DNS 服务器发出了查询。两个成功的设置操作可以相互覆盖用户未指定的端口。配置恢复日志发布后的单次目录同步失败，则会让下一次管理操作因缺少候选代而中止。下文分别给出触发顺序、证据、影响和验收条件。

本轮列出 **3 项高、11 项中、5 项低**。高表示正常输入产生静默的配置或运行结果错误，或一次故障即可使安装脱离自动恢复。中表示有条件的正确性、可用性、交互性能或验证问题。低表示影响局限、有直接绕行办法，或文档与实现不一致。编号同时用于实施路线图。[关键问题的复现附录](engineering-quality-audit-2026-11-26-repro.md)

## 范围和取证方式

- 基线为北京时间 11:24:32、HEAD `584354a6a6c46dac04397afab93f06fbeab1e6f1` 上的**实际工作树**，包括 93 项已修改或未跟踪的路径，当时暂存区为空。主模块含 230 个实现 Go 文件、288 个测试 Go 文件，分别为 43,193 和 60,200 行，含注释和空行，排除第三方副本。检查范围还包括安装与打包脚本、PTY 和 Linux 验证入口、双语资源、文档及被替换的 Ultraviolet 依赖。
- 三个子代理分别检查生命周期与存储、CLI/TUI、内核和下载。我交叉检查了关键代码、Shell Integration、测试运行器、覆盖率及外部实现。实验使用独立目录、回环 fixture、编译期 Go overlay 或真实独立 Mihomo。磁盘同步故障和特定 systemd 失败由受控替身注入。性能数据来自 Apple M4 Pro、Go 1.27.1，表示相应函数的工作量；表中的 MB 是累计分配，MiB 是二进制大小。
- 审计期间其他会话继续修改工作树。11:28:48 又冻结了一份源码并复跑验证，单列已获修正的问题。以下位置按第一份快照的行号和函数名给出。两份源码的汇总 SHA-256 分别为 `465facd6b970ae80c422f51ea18cb96e9b76d44862a1caa3c97bb885cdab8fa8` 和 `08733d969fe0d5245a12213757b8c1ad2229a97487704a86c7d7ae78e890e04a`，计算范围是 `internal/`、`scripts/`、`third_party/`（排除 Python 缓存）及根目录的 `main.go`、`Makefile`、`go.mod`、`go.sum`。本地证据目录保留两份指纹清单，并提供可直接解压以复跑附录的第一份源码。[冻结源码包](../.test-artifacts/engineering-audit-2026-11-26/source-baseline.tar.gz) · [本机审计证据](../.test-artifacts/engineering-audit-2026-11-26/)

## 高优先级

### F01　配置构建改变 DNS 策略匹配顺序

**位置**　`internal/mihomo/config.go:106` 的 `decodeConfig` 将整个文档解码到 `map[string]any`。`internal/mihomo/builder.go:53` 序列化后，`config.go:95` 的监听策略和 `config.go:148` 的模式设置还会再次编码文档。

**触发与影响**　两个 DNS rule-set 同时匹配同一域名，声明顺序和键名字典序不同。原订阅把特定查询交给首选解析器，生成配置改选另一台。两个配置均通过内核校验，管理界面没有错误。导入、应用及重写现有配置都要考虑这一语义。

**实证**　在同一台机器的两个独立回环 UDP 应答器上，为 `fixture.example` 分别返回 `192.0.2.10` 和 `192.0.2.20`。先声明 `rule-set:z-first`，再声明 `rule-set:a-second`。真实 Mihomo v1.19.33 的同一个 `/dns/query` 请求，在输入原文得到前者，在 `Build` 的输出得到后者。上游按插入顺序读取策略，解析阶段返回首个匹配项。[Mihomo 的策略读取](https://github.com/MetaCubeX/mihomo/blob/v1.19.33/config/config.go#L1340-L1349) · [Mihomo 的匹配顺序](https://github.com/MetaCubeX/mihomo/blob/v1.19.33/dns/resolver.go#L261-L275)

**修复与验收**　用 `yaml.Node` 等有序表示保留有顺序语义的映射，在同一文档上完成受管字段变更后统一编码。覆盖 `nameserver-policy` 和 `proxy-server-nameserver-policy`，对构建、改模式、改通用设置前后的真实查询结果做对照。已测量一份 388,897 字节、含 10,000 条普通规则的订阅。显式使用 rule 模式并关闭 TUN 时，每次构建耗时 39.4–41.7 ms、63.26 MB、约 31.15 万次分配。以查询结果为正确性门槛，再比较编码次数和分配量。

### F02　CLI 修改一个端口会撤销另一个会话的修改

**位置**　`internal/cli/settings.go:47` 读取整份设置，`:51` 复制后只改命令行指定字段，`:58` 调用未携带原值的 `PrepareRuntimeSettings`。`internal/app/settings.go:92` 与 `:121` 只在传入原草稿时检查是否过期。TUI 的 `internal/tui/ports.go:129` 已使用带原值的接口。

**触发与影响**　A 读取端口后，B 成功更改控制端口，接着 A 只提交 `--mixed-port`。A 用旧整份配置生成一份合法的新计划，B 的更改被持久覆盖。此窗口在读取与预检之间，提交时重新检查已生成的计划无法发现它。

**实证**　在真实 Cobra 子命令和应用存储事务之间插入可控的 B 操作，服务保持停止。三次均观察到两次调用成功，且控制端口被 A 改回；一组实测为 57824 → 57825 → 57824。相同交错下，带原草稿的预检拒绝 A。

**修复与验收**　在 CLI 的小接口中使用 `PrepareRuntimeSettingsEdit(ctx, current, proposed)`。将上述双写序列加入 Cobra 测试，覆盖两种端口、dry-run、运行中重启确认以及拒绝后的磁盘状态。

### F03　恢复日志已经发布时，目录同步失败会删除日志需要的配置代

**位置**　`internal/store/store.go:125` 至 `:133` 先发布目录项，再同步父目录。`internal/app/mode.go:114、:139`、`settings.go:272、:301`、`tun.go:121、:143` 的延迟清理在日志写入返回错误时删除候选代。`internal/app/recovery.go:93` 随后校验该代。

**触发与影响**　文件链接已对当前进程可见，父目录 fsync 返回 I/O 错误。当前指针仍可指向旧代，但恢复日志引用的新代已消失。下一次 `Snapshot` 或需要先恢复的管理操作停在人工检查，影响该实例的可管理性。故障窗口很窄，后果会跨进程保留。

**实证**　仅为本次测试在目录项发布后返回一次同步错误，调用 `SetMode`，确认日志存在后关闭注入。下一次 `Snapshot` 返回 `recovery generation changed; preserving files and journal for manual inspection`。实测入口是模式切换；另两个入口由相同清理分支定位。

**修复与验收**　采用 `internal/app/profile_generation.go:64` 已有的处理方式，让可见的恢复日志继续拥有候选代。集中这段已重复的发布与清理规则。分别注入发布前失败、发布后同步失败、恢复中再次取消，核对当前代、日志和两个代文件，第二次操作可以自动收敛。

## 中优先级

### F04　删除 systemd unit 后 reload 失败，卸载无法重试

- **位置和触发**　`internal/service/systemd.go:596` 至 `:601` 删除已验证的 unit 后才执行 daemon-reload。`internal/app/removal.go:35、:56` 每次卸载都从验证 unit 文件开始。
- **影响和证据**　对已停止且禁用的隔离安装，使用真实 Systemd 适配器、临时 unit 目录和受控命令替身，仅令第一次 reload 退出 1。unit 已删除而安装树仍在。恢复命令成功行为后第二次卸载直接返回 ENOENT。保留订阅属于预期，卡住的是内核卸载事务。
- **修复与验收**　删除前持久记录经过验证的 unit、安装身份和已检查的文件树。重试时重新核对 manager、替代 unit、运行状态及文件归属，再执行 reload 和剩余清理。覆盖失败重试、进程退出重入、同名外来 unit 三种路径。

### F05　共享读锁内并发检查更新会写同一服务对象

- **位置和触发**　`internal/app/core_update.go:45、:49、:104` 在共享文件锁内调用 `load`。`internal/app/installation.go:142` 的校验进入 `internal/app/tun.go:35`，写入共享的 `Systemd.TUN`。
- **影响和证据**　同一进程共享一个已安装的 App，用四个 goroutine 各执行三次 `CheckUpdate`。使用真实 Systemd 类型和本地版本查询替身，race detector 给出两条写栈，均落在 `tun.go:35`。独立 CLI 进程的内存彼此分离；本轮没有观察到 TUN 被错误开启。
- **修复与验收**　让读取安装记录和验证配置成为纯读，把本次操作的 TUN 策略显式传给需要它的适配器操作。将该并发测试加入 `-race`，并保留 user/system 策略校验。

### F06　慢速状态观察或订阅下载阻塞同一实例的 Stop

- **位置和触发**　`internal/app/snapshot.go:60、:155、:167、:175` 在同一应用锁内依次访问三个控制 API。`internal/app/profile_import.go:47、:56、:121、:184` 在锁内下载。订阅 HTTP 超时位于 `internal/app/source.go:33`，为 45 秒。
- **影响和证据**　分别用回环 HTTP fixture 挂起状态请求和导入下载。并发执行限时 250 ms 的 `Control(stop)`，两次都超时，服务替身仍 active，且未收到 stop。取消慢请求后，Stop 随即成功。实际等待取决于慢请求、调用者截止时间和排队情况。
- **修复与验收**　锁内捕获安装身份、配置代、进程及订阅 revision，释放锁做远端观察或下载，再加锁核对身份并提交。保持真正的提交和回滚阶段互斥。停服测试须先确认 HTTP 请求确已发出，并覆盖取回数据前 Profile 被改名、更新或删除的情况。

### F07　热重载或停止前发出的查询能在变更后报告成功

- **位置和触发**　`internal/app/rules.go:28、:38、:52` 只复核安装身份和 PID，同进程热重载仍会通过。`internal/app/monitor.go:179、:190` 的 `Connections` 在 GET 后直接返回。CLI 对应 `internal/cli/connections.go:22`。
- **影响和证据**　挂起旧规则响应，通过正常用例为当前 Profile 添加一条前置规则并完成热重载，随后释放旧响应，`Rules` 仍返回 ready 和旧的 `MATCH,custom`。另一次挂起连接查询，确认 Stop 完成后再释放旧响应，调用仍无错并返回旧的 `uploadTotal=100`。错误在观察结果上，实验中的实际服务变更已生效。
- **修复与验收**　规则按安装、运行进程及配置代复核；连接读取至少核对安装身份、PID 和进程启动标识。复用 `internal/app/providers.go:26` 已有的前后观察方式。用两个阻塞响应分别断言丢弃或明确标为过期。

### F08　Shell 安装日志的读取与删除上限不同，重试持续失败

- **位置和触发**　`internal/shellintegration/installer.go:409、:460、:535` 写入包含修改前后内容的 JSON，以 48 MiB 上限读取，最后调用 `store.Remove`。`internal/store/store.go:85、:143` 删除前按通用 16 MiB 上限重新读取。
- **影响和证据**　在临时 HOME 放入 6,720,000 字节的合法注释作为 `.zshrc`。安装产生 17,931,312 字节日志，返回 `managed file exceeds size limit`。查询显示未完成事务，原样重试仍报同一错误。较大的 Bash 双文件事务也共享这条收尾路径。
- **修复与验收**　为安装元数据使用一致的发布、对比、恢复和删除上限，写前还要校验整份编码日志是否可恢复。保留对原文件内容、身份和权限的检查。覆盖超过 16 MiB 但在允许范围内的日志，以及确实超过总上限时在写入前返回错误。

### F09　Bash 更换优先登录文件后，Shell 集成仍报告已安装

- **位置和触发**　`internal/shellintegration/installer.go:273` 按 Bash 顺序选登录文件；`:205、:386` 在旧收据记录的引用仍完整时直接接受，只有引用缺失才在 `:395` 重新选路径。
- **影响和证据**　先在只有 `.profile` 的临时 HOME 安装，再新建不加载 `.profile` 或 `.bashrc` 的 `.bash_profile`。`Installation("bash")` 返回 true，重装返回 nil，新优先文件仍无托管引用。此后的 Bash 登录 Shell 不会自动加载该 hook；非登录 Shell 的 `.bashrc` 路径不受这个例子影响。
- **修复与验收**　状态检查时比较当前启动路径与收据覆盖集合。遇到切换，返回可操作的待修复状态；显式安装时检查冲突并为当前入口补引用，保留用户后续编辑。增加登录文件优先级改变后重装的回归。

### F10　即使服务停止，监控心跳仍每秒散列整个内核文件

- **位置和触发**　`internal/app/runtime_stream.go:89、:92、:163` 的会话检查进入 `monitor.go:40` 的 `load`，由 `installation.go:112、:128、:142` 读取配置、散列可执行文件并解析策略。系统实例还会验证生命周期程序。
- **影响和证据**　48 MiB 合成二进制、热文件缓存、已安装且服务停止、假服务管理器，十轮平均每次检查 21.3 ms；加入 5,000 条规则后为 34.6 ms。会话持续存在时，每次检查要从文件接口读完 48 MiB。这测量的是逻辑读取，物理磁盘流量由内核缓存决定。
- **修复与验收**　分开周期性的进程/安装身份观察与完整性验证。仅在可证明身份未变的观察路径复用已验证结果；更换文件、代、进程和执行管理动作时进行必要的完整校验。有更改时仍应检测异常。以多轮稳定心跳完整 hash 次数、进程变化检测和基准量化验收。

### F11　连接页为屏幕外连接反复排序和构造布局

- **位置和触发**　`internal/tui/connections_archive.go:48、:74` 生成筛选排序集合，`connections_toolbar.go:71` 为计数再次取集合。`connections_view.go:241、:274、:283` 在固定视口中仍按全集构造占位和命中信息。
- **影响和证据**　120×32、快照不变、搜索匹配全部活动连接，100 / 1,000 / 5,000 行的单次布局依次为 5.23 / 6.48 / 14.53 ms，累计分配 1.91 / 4.23 / 17.63 MB。此基准限于布局，未计网络和终端绘制。
- **修复与验收**　同一快照、筛选和排序只计算一次索引，计数和交互共用；只为可见窗口构造行与命中区。使用现成的 Bubbles table 绘制，沿用规则页窗口化经验。重复上述三档基准，保留选择、归档、排序、滚轮和鼠标命中回归。

### F12　新日志使整份 200 条显示文档失效

- **位置和触发**　`internal/tui/logs_view.go:75、:84、:91` 用首序号和数量作为整份文档的缓存键，`:99、:118、:140` 对所有匹配条目重新换行、着色、装载 viewport。满环时追加一条也会改变首序号。
- **影响和证据**　120×32、已有 200 条、通过模型的真实 Update 送入一条正常日志。每条 72 字节时，单次更新 5.28 ms、3.32 MB、48,986 次分配；504 字节时为 11.68 ms、6.90 MB、105,282 次分配。内存留存有界，持续的计算和分配发生在高频显示路径。
- **修复与验收**　宽度、主题和筛选条件稳定时按日志序号复用已排版行，仅追加和逐出变化项。缩放、颜色或匹配选项改变时整批失效。保留 200 条上限、暂停副本和每批消息限制，复跑长短日志基准与 PTY 的暂停/恢复、锚点和换行检查。

### F13　大代理组测速后，其他页面的普通消息仍触发平方级扫描

- **位置和触发**　`internal/tui/model.go:1151` 的公共更新路径调用代理状态同步。`internal/tui/proxies.go:895、:898` 遍历测速结果，对每条结果用 `:771` 的线性查找重新确认组成员及成员路径。
- **影响和证据**　同组 n 个结果时，成员比较为 O(n²)。保持同一快照，切回概览后单测 `syncProxies`，250 / 1,000 / 4,000 个已有结果分别为 0.075 / 0.726 / 7.95 ms。4,000 条时单次还分配约 192 KB。
- **修复与验收**　仅在新的代理数据可能改变成员或路径时清理结果，每个组只构建一次成员索引。运行中同一代的 API 更新也要触发失效。断言无关消息不扫描整组，再比较这三组规模。

### F14　Linux 验证器从错误目录运行，部分源码检查可空跑通过

- **位置和触发**　`scripts/verify-linux.sh:33、:40、:45` 从仓库根目录启动各包的测试二进制。`internal/i18n/i18n_test.go:71、:150` 用包目录相对路径 Glob；零个匹配时遍历直接结束。
- **影响和证据**　在临时源码树给合成 TUI 文件写入不存在的词条。运行相同测试二进制和 `TestLiteralMessageReferencesExist`，仓库根目录退出 0；切换到 `internal/i18n` 后退出 1 并指出具体缺键。故而 Linux 全套绿灯不能证明这两项源码扫描实际执行。宿主按包运行的 `go test` 会扫描到源码。
- **修复与验收**　用 `go list` 得到每个包目录，把所需源码或夹具映射到 VM，逐包设置工作目录。两项扫描都要断言至少解析了预期包的实现文件。用临时错误词条做负对照，宿主和 Linux 入口均应失败，移除后均应通过。

## 低优先级

| 编号 | 位置与触发 | 受影响的行为及验证 | 修复与验收 |
| --- | --- | --- | --- |
| F15　未通过语义校验的订阅永久留下磁盘快照 | `internal/app/profile_import.go:63、:83、:191` 和 `profile_document.go:158、:172` 先 Put 后完整构建；`internal/configuration/library.go:141` 按摘要落盘。 | 对一个有效 Profile 连续刷新三个引用不存在策略组的不同版本。三次均被拒，索引逐字节不变，快照从 1 变到 4，重复相同输入仍为 4；删除 Profile 后仍为 4。新增三份从未发布。成功发布过的历史版本有明确留存约定。 | 先在内存中对候选原文完成构建和适用的内核校验，再 Put 并原子发布索引。覆盖导入、编辑和刷新；检验失败候选不增长、重复内容去重、历史和运行代仍可读。 |
| F16　未启动过也会被记录为已运行 | `internal/app/state.go:147、:163` 的成功 Stop 总是调用 `preferences.go:178`。 | 全新已安装、从未启动的 fixture 执行一次 Stop，服务仅收到 `[stop]`，持久化的 HasRun 从 false 变 true。读取偏好或展示首次运行状态时得到错误历史。 | 用成功启动或认证 API 观测记录首次运行。覆盖首次操作为 Stop，以及先启动后停止两种顺序。 |
| F17　latest 不接受版本接口已经认可的无 v 版本 | `internal/mihomo/release.go:101、:107、:110` 仅给显式参数补 v；`internal/mihomo/update_test.go:23` 允许无前缀稳定版响应。 | 本地 HTTP 版本源返回 `1.19.33`，解析 latest 失败，显式指定同一版本成功选出 `v1.19.33`。影响这一种受支持的响应形态。 | 将稳定版规范化放在两个来源汇合后；对显式与 latest 各跑带/不带 v 的矩阵。 |
| F18　端口测试夹具依赖机器上的空闲端口 | `internal/app/listeners_test.go:111` 的初始密钥轮换测试使用默认端口；`internal/app/settings_test.go:24、:29` 从 OS 领取 TCP 端口后只尝试一次同号 UDP。 | 初次宿主测试和专用 VM 均在已占用的 9097 上失败。同次宿主覆盖率运行还因 UDP 55317 已用而失败。第二份工作树已为密钥轮换改用动态设置，宿主定向 race 复跑通过；单次 UDP 选择仍在，第二份快照里的用例为 listeners_test.go:113。 | 保留已完成的默认端口修正；对同时需要 TCP/UDP 的端口做有界重选，保持必要的预留或用隔离网络命名空间。占用默认端口并行重复跑夹具。 |
| F19　日志协议和 Profile 字段说明落后于实现 | `docs/logs.md:3、:7` 描述共用 app.StreamLogs 和普通 HTTP 行流；`docs/subscription-design.md:111、:116` 泛称固定回环、set 指定受管字段均会拒绝。 | `internal/tui/runtime.go:141` 经 StreamRuntime，CLI 经 `internal/cli/logs.go:27`，底层 `internal/mihomo/websocket.go:54` 使用 WS。真实 WS 集成通过。Build 接受指定 log-level 与 store-fake-ip 的覆写，但输出采用实例值，由 `internal/mihomo/general_settings.go:206、:229`、`docs/settings.md:44` 及测试确认。使用旧说明会准备错误的日志 fixture 或误判字段优先级。 | 同步日志传输、会话归属和监听/实例偏好的字段表；在文档中区分直接拒绝和接受后由实例覆盖的字段。用实际日志测试和字段用例对照校阅。 |

## 架构、日志和组件边界

| 维度 | 当前实现与审计判断 |
| --- | --- |
| 模块依赖 | `go list` 的实际导入图为 main → CLI → app，默认交互经 CLI → TUI → app。app 对 Cobra、Bubble Tea 均无依赖。配置库负责摘要和索引，Mihomo 包负责配置语义和协议，service 适配 systemd。F02 说明这种共享用例设计还需要一致的调用契约。优先修复接口使用，不必重分所有包。 |
| 状态与错误处理 | 文件替换、配置代、归属校验和恢复日志已有清晰边界。已有测试能恢复被中断的 Apply，并让显式 Stop 优先。F03/F04 揭示两种不同的部分成功状态。`internal/diagnostic/outcome.go:10、:32` 能区分取消、需人工处理和组合错误，适合继续用于展示这些结果。 |
| 并发和资源 | `internal/app/runtime_stream.go:299` 等待读取者退出；`internal/tui/runtime.go:129、:144、:216` 处理前代结束、取消和旧消息。连续 40 次 REST 请求服务端观测到打开和关闭各 40 条连接；40 次 WebSocket 完成握手后取消静默读取，服务端最终活动数为零，重复三轮。按次克隆并关闭 HTTP transport 会付出重新建连的成本，单凭连接计数不足以列作瓶颈。已确认的磁盘累积见 F15。 |
| 日志与诊断 | `internal/mihomo/logs.go:17、:38、:73` 限制帧和正文、清理终端控制符并脱敏；`internal/service/systemd.go:397` 为受管单元禁用原始标准输出和标准错误落盘。CLI 对条数和观察时间设限，能区分静默成功和未连接。TUI 保留最近 200 条，暂停副本也有界。实时日志和持久化事务日志各有明确用途。F12 是显示成本，F19 是协议说明，两者可独立修复。 |
| UI 组件复用 | 当前已经使用 Bubbles 的 textinput、textarea、spinner、table、viewport、progress、help 和 key。连接表见 `internal/tui/connections_view.go:192`，日志 viewport 见 `logs_view.go:138`，订阅进度条见 `profile_card.go:38`。多行规则和命中范围共用窗口坐标，理由见 `rules_view.go:158、:270`。保留这些组件；F11–F13 的成本来自送入组件的数据和派发频率。 |
| 可维护性 | `internal/tui/model.go` 有 1,901 行。F11–F13 的基准指明了应迁移到具体页面或数据变更处的工作。模式、监听和 TUN 三处重复清理则已造成相同错误，适合提取一个共享实现。Ultraviolet 的本地补丁有固定来源、独立回归和移除条件，仓库的 check/race 也包含其测试。[补丁说明](../third_party/ultraviolet/LAZYCLASH.md) |

## 同类开源项目源码对照

| 项目与相同场景 | 源码实际行为 | 对 lazyclash 的取舍 |
| --- | --- | --- |
| Clash Verge Rev v2.4.3，生成、校验和应用配置 | 先生成并校验草稿，reload 成功才 apply；校验错误或指定的应用错误 discard，满足条件的 reload 错误重启后再试。 | F03 可借鉴其显式提交边界。lazyclash 还需在多进程、掉电以及 systemd 状态变化后恢复，必须保留磁盘日志和代际校验。[配置应用与失败分支](https://github.com/clash-verge-rev/clash-verge-rev/blob/fa74e79f40d10411efd0f553b2207be59ad26739/src-tauri/src/core/manager/config.rs#L61-L115) |
| Clash Verge Rev，订阅更新 | 在取得远端条目后调用配置更新函数，再持久化。 | F06 可参照其下载与提交分段。lazyclash 需额外核对本机其他 CLI/TUI 进程已经发布的 revision。[下载与提交顺序](https://github.com/clash-verge-rev/clash-verge-rev/blob/1dba31c6afd41ecdc80f137f250386bb1a60868a/src-tauri/src/feat/profile.rs#L261-L315) |
| MetaCubeXD v1.277.0，监控断线与连接列表 | 客户端用 generation 和 endpoint identity 丢弃旧 WS 回调，主动断开时拆除回调并取消重连。连接页复用计算后的筛选结果，分页后才生成行。 | lazyclash 已有代际过滤、等待旧读取者以及按来源独立重试。F07 可把同样的身份约束带到一次性 HTTP 读路径；F11 可共享筛选结果。[WS 归属校验](https://github.com/MetaCubeX/metacubexd/blob/6bd158d5f5ae299ef424552dd7be81ba10b9aa23/packages/ui/composables/useWebSocket.ts#L84-L108) · [分页和计数](https://github.com/MetaCubeX/metacubexd/blob/6bd158d5f5ae299ef424552dd7be81ba10b9aa23/packages/ui/pages/connections.vue#L489-L556) |
| Zashboard，连接历史表 | 维护完整滚动尺度，只给视口附近的虚拟行生成单元格和点击事件，上下用空白占位。 | F11 可以保留终端现有键盘、滚轮及命中坐标，只按可见行构造终端渲染对象。[连接历史调用方](https://github.com/Zephyruso/zashboard/blob/b51d07099e0b18c0c5126cac22ed4e5e01291a1e/src/components/overview/ConnectionHistory.vue#L98-L105) · [虚拟表绘制](https://github.com/Zephyruso/zashboard/blob/b51d07099e0b18c0c5126cac22ed4e5e01291a1e/src/components/common/VirtualTable.vue#L56-L90) |

此外，F01 直接依据被管理内核 Mihomo v1.19.33 的解析源码和真实查询结果判定语义。前端项目提供的是可以移植的组织方式；审计没有对不同语言、不同 UI 框架的吞吐量作横向排名。

## 测试覆盖与平台验证

| 本轮执行 | 结果与边界 |
| --- | --- |
| 宿主 Go 测试、vet 和覆盖率 | macOS/arm64、Go 1.27.1。第一份快照 `go test -count=1 -coverprofile=host.cover ./...` 触发 F18 的两种端口冲突；独立 vet 通过。第二份快照 `make check` 及显式不使用测试结果缓存的全包覆盖率运行均通过。 |
| race | 第一份快照现有全包 `go test -race -count=1 -timeout=20m ./... github.com/charmbracelet/ultraviolet/...` 仅 app 包因默认端口测试失败，TUI 及第三方包通过。第二份快照的整个 `internal/app` 包使用 `-race -count=1` 通过。加入 F05 的定向测试后，同一共享读路径出现数据竞争；已有测试的绿灯没有覆盖该交错。 |
| Linux 全包 | Debian 12.15/arm64、systemd 252，实际在专用 OrbStack VM 中运行。原始 `./scripts/verify-linux.sh lazyclash-e2e` 首次被默认端口测试中断。第一份快照另按包目录补跑。第一次自定义网络隔离命令误继承了 root 的 HOME，使 app 包另报权限错误；改为普通用户 HOME 和私有 XDG 目录后，该包通过，其他包在 VM 常规环境通过。第二份快照的原始脚本完整通过；其源码扫描限制见 F14。 |
| 真实 Mihomo 与 systemd | macOS/arm64 与 Linux/arm64，独立运行实际 v1.19.33，通过配置应用/拒绝和重载、Provider 缓存、额外监听、Provider API 及 WS 共五项测试。VM 另通过真实 user manager 查询和独立瞬态服务的 PID/启动时间测试。以 root 在 VM 跑了六项独立测试，涉及系统默认路径、文件归属、生命周期身份、普通 UID/GID 执行和下载后版本探测。 |
| TUI、Shell 与脚本 | 宿主真实 PTY 跑连接页中文真彩 200×45 至 40×16 共六种尺寸，以及日志页英文无色 160×45 至 40×16 共五种尺寸。两组使用内存后端，验证鼠标、键盘、缩放、暂停和列表状态。Linux 上 Bash/Zsh/Fish 的身份 PTY 通过。Python `unittest discover` 10 项通过。 |
| 发行包 | `make package VERSION=audit` 产生 Linux amd64 和 arm64 两份归档，两份 SHA256SUMS 校验通过。另用模块声明的最低 Go 1.26.0 在 macOS/arm64 编译并运行 CLI 帮助成功。amd64 是交叉构建结果，本轮没有原生 amd64 服务、长时间生产负载或 WSL 实测。 |

第二份快照的宿主语句覆盖率为 **87.7%**，按主模块 Go coverprofile 的语句数加权。Ultraviolet 独立模块、真实 PTY 子进程及 Linux 专属构建路径单独验证，未计入这一百分比。

| 包 | 语句覆盖率 | 包 | 语句覆盖率 | 包 | 语句覆盖率 |
| --- | ---: | --- | ---: | --- | ---: |
| tui | 93.6% | app | 76.0% | mihomo | 84.9% |
| cli | 73.0% | service | 73.3% | store | 67.1% |
| configuration | 91.2% | shellintegration | 76.5% | downloadnet | 75.4% |
| diagnostic | 97.4% | i18n | 79.2% | editor | 79.2% |

入口 main 的语句覆盖率为 0%，buildinfo 包无测试文件。当前缺口更集中在**同一字段跨入口的并发修改、发布后部分失败、跨配置代的迟到结果、源码扫描的负对照**。这些场景应有行为断言。真实核心测试验证 F01 这种只有运行路径才显现的差异。

本轮没有运行需要实际切换整台 VM 路由的 host TUN 双作用域 E2E。通用 `e2e.sh` 的预检固定占用 7897/9097，本轮在 VM 上确认两端口已有监听，保留了该实例。独立核心、真实 transient service 和 TUN 的故障替身测试分别验证各自边界，完整安装到卸载的双作用域链路仍待单独验收。

## 按优先级实施

| 顺序 | 工作与依赖 | 完成判据 |
| --- | --- | --- |
| 1　先保护用户结果 | F02 的 CLI 改用已有草稿守卫，F03 统一恢复日志发布语义；F01 修改有序配置表示。三项可独立提交。 | 每项先将对应复现转为失败的正确性断言。不同字段并发提交时保留已保存值；目录同步失败后下次操作可恢复；重叠 DNS 策略在各种配置重写前后仍命中原首选。 |
| 2　补强验证入口和恢复 | F14 修测试工作目录，F18 去掉 UDP 分配偶然性；随后处理 F04 卸载重试、F05 查新数据竞争、F07 旧观测。 | 负对照在 Linux 上按预期失败，端口占用不影响无关测试。重复卸载可收敛，定向 race 通过，过期 rules/connections 有明确状态。 |
| 3　缩短锁和明确持久化时点 | F06 将下载和只读 API 移出临界区；在整理同一条 Profile 发布路径时完成 F15。Shell 安装独立修 F08/F09，运行历史 F16 可单独提交。 | 慢服务器未返回时另一进程能停服；过期候选不能提交；正常验证失败不留下新快照。大启动文件可完整提交，变更后的实际登录入口可加载，首次 Stop 不产生运行历史。 |
| 4　减少空闲与重绘成本 | 先修每秒发生的 F10，再按工作负载选择 F11 连接页、F12 日志页、F13 代理结果。沿用本文相同数据和尺寸做改前改后对照。 | 稳定心跳避免重复完整 hash；屏外行数增加不再引起等比例的布局分配；单条日志可复用旧排版；无关事件不再扫描已测速大组。复跑既有控制、搜索、暂停、缩放和取消测试。 |
| 5　完成发布前验收 | 独立修 F17 版本文本和 F19 文档；在专用空闲端口环境运行全生命周期与 host TUN 双作用域脚本，再做 Linux/amd64 原生抽验。 | 发布记录分列宿主单测、定向 race、真实内核、两种服务作用域、PTY 和实际 CPU 架构；保留脚本失败或跳过项。 |

[复现附录](engineering-quality-audit-2026-11-26-repro.md) 中的实验可在对应源码 checkout 重建。本机忽略目录另存经核对的命令输出、主要夹具、基准及首次冻结的源码；它们不随审计文档提交。[测试与构建结果](../.test-artifacts/engineering-audit-2026-11-26/validation/) · [应用生命周期证据](../.test-artifacts/engineering-audit-2026-11-26/lifecycle/) · [DNS、下载与连接资源证据](../.test-artifacts/engineering-audit-2026-11-26/core/) · [CLI/TUI 实验](../.test-artifacts/engineering-audit-2026-11-26/ui/) · [Shell 实验](../.test-artifacts/engineering-audit-2026-11-26/shell/)
