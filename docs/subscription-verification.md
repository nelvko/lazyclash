# 订阅与配置重构验证

日期：2026-10-29。架构与流程见 [设计](subscription-design.md)，源码调研见 [调研报告](subscription-research.md)。

## 本次已通过

| 层次 | 实际入口 | 结果与证据边界 |
| --- | --- | --- |
| 宿主单元/业务/模型 | `make check` | 全包 `go test ./...` 与 `go vet ./...` 通过 |
| 竞争检测 | `make race` | 全包 `go test -race ./...` 通过，宿主 Darwin/arm64 |
| Python 辅助脚本 | `python3 -m unittest discover -s scripts -p 'test_*.py'` | 4 项通过；新增配置 E2E 另外做 Python 编译/Shell 语法检查 |
| 真实 Linux 测试执行 | `./scripts/verify-linux.sh lazyclash-e2e` | 重新编译 Linux/arm64 各包测试二进制并在现有 Debian 12 专用 VM 执行，全通过；不是仅交叉编译 |
| 真实 Mihomo | 下述 opt-in Go 集成测试 | 官方 v1.19.33 / Darwin arm64；测试与 race 通过，内核进程自动清理 |

Linux 测试机是普通 uid 501、systemd 252，用户 manager running、Linger=yes；未修改 linger。首次完整 E2E 在 `lazyclash-e2e` 预检发现控制端口 9097 被既有进程占用，脚本明确退出，没有停止该进程。实际完整 E2E 改用项目历史中已有的 `proxyctl-e2e` 专用 Debian 12/arm64 VM，其默认 TCP/UDP 测试端口均空闲；结果见下节。

## 单元与业务覆盖

新增和重构测试覆盖真实用例约束，而非只验证某个 merge 函数：

- 原始快照先写、索引未提交时重新打开只观察旧 revision；外部索引编辑和过期写入不会被覆盖。
- HTTP 200 的错误 JSON/YAML 或明显其他内核格式在来源层拒绝，不进入空配置退化路径，不改变库或服务。
- 损坏快照不接受、不静默修复；关系不完整的配方不能发布；新文件权限为私有。
- source 添加不选择或启动；记源为显式操作；安全列表不包含地址、token 或模板内容。
- Full 与 Managed 对相同来源消费不同内容；Managed 有序多源、重复节点、provider-only、悬空 dialer/group/provider 和循环引用。
- 整体字段替换、前插/后插规则、重复规则保留、拒绝受管字段和 null 自定义删除。
- 含逗号的正则、大小写规则类型、`MATCH,PASS` 及含 PASS 的组、原生 null 空容器，避免误拒合法 Mihomo 输入。
- DNS resolver 保留而额外监听清空；HTTP provider URL/header/behavior 变化使用隔离缓存；file provider 路径边界。
- 更新所有依赖配方后才提交 source revision；下载、YAML、引用、引擎验证、取消或外部编辑失败时保留原索引。
- 覆写保存和来源更新保持运行 PID/代际不变；选中配置显示 pending，显式 Apply 后才提交。
- 新生成代固定 recipe/source SHA；运行时加载失败恢复旧配方/配置，新的订阅草稿仍可重试。
- 停止状态 Apply 不启动；设置调整继续使用已应用配置而不采用刷新草稿；mode、节点持久化、恢复 journal、Stop 优先和进程变化保护回归。
- 选中 Profile 删除保护、共享来源引用保护、历史快照保留；卸载内核保留新库。
- CLI source/profile 完整命令链；TUI 明确记源、隐藏输入、source 刷新、覆写文件导入、名称确认删除、pending 和窄屏命中区。

故障恢复包含确定性服务/HTTP fixture 注入：重启失败、恢复失败、操作取消、两代或 unit/binary 后续变化、中断 journal。它们不等同于在真实系统制造任意掉电或网络副作用。

## 可重复的真实内核验证

```sh
LAZYCLASH_TEST_MIHOMO=/absolute/path/to/mihomo \
  go test -race ./internal/mihomo -run 'TestMihomo.*Integration$' -count=1 -v
```

默认 `go test` 跳过这些测试；显式提供现有二进制，不自动下载、安装服务或操作主机代理。测试在临时目录/端口运行独立内核，有限时和退出清理。

本次使用官方 `mihomo-darwin-arm64-v1.19.33.gz`，下载物 SHA256 与官方 release API digest 核对：`060b64b0f2c9a310831db0fe4adb2878de67bfa606c311813c1f13966aa504b6`。这只是本次测试工具来源，产品安装仍走原有官方发布校验。

实际验证：

- Builder 生成 Full、Full+覆写、Managed 和 Default 均通过真实 `-t`。
- 校验时测试端口仍被占用但 `-t` 通过，证明解析校验不能当监听可用证明。
- 启动后认证 API 和回环 mixed listener 可达，DNS listener 未启用。
- 原生 PUT reload 可更新规则，非法引用返回 400 并保留原规则，core home 外 path 拒绝。
- 选择通过 reload、进程终止后使用相同 `-d` 重启仍保持。
- A/B 两个本地 HTTP provider 使用相同原始 path/name、不同 URL，生成缓存不同；切换后 B 被实际请求、节点为 B，未复用 A 的有效缓存。

PUT 测试用于确认内核能力与边界，产品当前的 Apply 仍使用 systemd 重启，不声称已经交付产品热加载事务。

## 真实 Linux/systemd E2E

可重复入口：

```sh
./scripts/e2e.sh proxyctl-e2e --proxy-mode environment
```

VM 参数必须明确选择已存在的专用环境。脚本用随机 unit 与 `mktemp` 数据目录，拒绝占用端口，官方 v1.19.32/v1.19.33 均由产品下载并校验，结束只清理本次所属资源。environment 明确使用此测试 VM 既有的标准代理环境，不读取其他代理工具配置。

本次最终完整执行通过，覆盖官方安装、更新/重装、Full/Managed、多来源/覆写、刷新 PID 与 pending、无效更新与删除保护、规则/日志/连接页面、CLI/TUI 端口设置及所有 PTY 模式。生命周期实际验证终端 SIGHUP 不改变核心 PID、SIGKILL 触发 systemd 恢复，mode/selector 跨配置应用与重启保持，enable/disable 不改变 PID，Stop 超过 RestartSec 仍保持停止。

清理记录 `.test-artifacts/cleanup.txt` 确认随机专用 unit 为 `not-found`，本次默认及动态 TCP/UDP 监听释放，`resources released=1`、`exit=0`；VM 内临时目录由宿主脚本删除。脱敏执行记录在忽略目录 `.test-artifacts/e2e.log`、`linux-tests.log` 与各 `pty-*.txt`。

回归过程中曾有一次端口场景在停止、重新 Apply、启动后，本地代理请求未成功；当时诊断没有保留 HTTP 状态。增加脱敏 HTTP/mode/selector 诊断后，后续两次完整重跑通过（最后一次还加入了错误 JSON/外核响应的真实配置链验证），尚无证据判定这一瞬时失败的根因或声称已修复。基本就绪检查不保证后续每个请求成功，这一观察保留为后续请求层状态诊断的限制。旧测试对 raw profile 路径的断言也已适配新库。

## 未验证与已知限制

- 未实跑 Linux/amd64、其他发行版或最低 Go 1.26；本次 Linux 运行平台为 Debian 12/arm64。
- 没有实际结束机器上最后一个 SSH 会话或重启 VM；linger 检查、PTY 退出/SIGHUP 与 systemd 独立运行不能替代这两项验收。
- TUN、路由/DNS 接管、系统服务和特权授权尚未实现。
- 未用真实机场账号、远程节点、DNS 上游或地理数据库证明端到端外网连通；不将本地 API/监听就绪表述为这些能力均成功。
- Full 的远程 provider 内容由内核自治更新，顶层 SHA 不包括全部缓存；本次验证的是缓存身份隔离，不是 provider 内容事务快照。
- 没有产品热加载、定时刷新、历史回退 UI、自动旧数据迁移和垃圾回收；Managed 仅支持 inline 节点来源，高级配方创建先用 CLI。
- 强杀控制进程或断电后若有 pending journal，下一次应用用例负责恢复；无人访问时无额外 daemon 立即恢复。
