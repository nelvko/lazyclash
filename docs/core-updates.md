# Mihomo 版本检查与升级

当前支持 Linux/systemd 用户服务中的 Mihomo，下载平台为 arm64 与 amd64。安装、只读检查、确认升级分别是独立用例；CLI 与 TUI 都直接调用 `internal/app`，不互相启动命令。

## 使用

```sh
lazyclash install check
lazyclash install check --channel alpha
lazyclash install check --proxy-mode environment
lazyclash install upgrade --yes --proxy-mode environment
```

`install check` 不创建数据目录或锁文件，不执行恢复，不下载压缩包，不修改配置或控制服务。读取现有安装的真实 `mihomo -v`，再查询远程版本；安装记录、文件摘要或版本有问题时返回 `unavailable` 和原因，而不是冒充未安装。恢复日志存在时保留现场，提示先检查状态。远程网络失败仍返回错误，不显示“已经最新”。

`install upgrade` 要求 `--yes`，主动重新检查并升级到这次检查得到的确定版本。原服务正在运行时会重启并验证，停止时保持停止。独立检查输出用于自动化查看；升级不接受外部 JSON 作为可执行计划。TUI 中已安装实例打开 Settings → Manage / 管理内核，手动选择检查更新，审阅版本、渠道、时间和状态，再确认升级。进入管理页不联网。升级进度复用 spinner、下载计数与日志；取消后等待回滚完成。

未安装实例直接进入单页安装界面，不再选择唯一支持的 Mihomo。显式打开安装页后复用同一 `CheckUpdate(stable)` 查询；点击“下载并安装”才固定检查版本并串联资产解析、预检与既有安装管线。首次安装没有额外确认页；预检阻断仍显示原因。已有实例的手动指定版本/本地文件替换保留最终计划审阅。修改网络或输入、退出与取消均清除未完成的安装意图，迟到查询和预检不能触发安装。

原 `install --core-version ...`、本地安装和显式重装仍只发布文件，保留当前进程；它们可用于用户明确选择的版本更换。该行为与“升级并验证运行状态”的 `install upgrade` 不同。

## 查询与比较

检查直接请求以下官方文本地址，复用现有 None / Environment / Custom（HTTP、HTTPS、SOCKS5、SOCKS5H）代理设置。文件 prefix / mirror 不改写版本查询，也不成为摘要权威。

- 正式版：<https://github.com/MetaCubeX/mihomo/releases/latest/download/version.txt>
- Alpha：<https://github.com/MetaCubeX/mihomo/releases/download/Prerelease-Alpha/version.txt>

请求最多 30 秒，响应最多 256 字节，版本 token 最多 64 个安全 ASCII 字符。拒绝非 2xx、空内容、HTML/JSON、内部空白/控制符、路径及渠道错配；安全但未知的版本格式保留为无法比较。错误沿用 `downloadnet` 的脱敏分类，不输出响应正文或代理凭据。

结果包含 `installedVersion`、`latestVersion`、`channel`、`status`、`checkedAt`；必要时包含 `installedError`。

| status | 含义 |
| --- | --- |
| `current` | 版本相同，或正式版语义版本相等 |
| `available` | 正式版远程版本严格高于本地 |
| `ahead` | 正式版本地版本高于远程，不执行降级 |
| `incomparable` | 不同 Alpha hash、不同渠道或未知版本格式 |
| `not-installed` | 安装目录不存在 |
| `unavailable` | 已有安装无法安全读取真实版本，或等待恢复 |

正式版规范化可选的 `v` 前缀，按 SemVer 数值主体和 prerelease 规则比较，忽略 build metadata；数字比较不依赖固定整数宽度。Alpha 真实形式为 `alpha-<commit hash>`，同 hash 为相同版本，不同 hash 没有先后关系。本期只提供 Alpha 识别与检查，不提供 Alpha 一键升级，也不从 Alpha 自动切换到 stable。未知本地渠道的自动检查不猜测远端渠道，可显式选择 stable/alpha 观察远端，但仍不可直接升级。

不持久化远程查询缓存。TUI 保留本次结果和检查时间，用户可手动刷新；下一次检查失败会显示失败。升级不再请求变化中的 latest，而是解析已确认的正式标签。

## 发布、重启与恢复

1. 在现有 namespace 文件锁内重新核验安装 ID、代际、文件摘要和实际版本，拒绝修改过或过期的检查结果。同目录的安装、配置和生命周期操作共用该锁；两个并发升级只会有一个发布成功。当前运行状态与保存的停止意图冲突时先要求用户通过 Start / Stop 消除差异；暂存后再次检查运行状态，检测到外部启停则在发布前拒绝。
2. 按确定标签调用现有 Releases API 取得平台资产、大小和官方 SHA256 digest。缺失摘要或 API 限流时失败关闭，不跳过完整性校验。
3. 复用现有下载管线：5 分钟网络超时、64 MiB 压缩包与 128 MiB 二进制上限、大小及 SHA256 校验、gzip 完整解压、0700 权限、执行 `-v` 精确核对目标。临时目录位于数据目录中，与发布路径同文件系统。
4. 使用新内核验证现有配置；hard link 保留旧 inode，写入包含双方摘要、旧/新代际和原运行状态的 `core-pending.json` 并同步目录；随后 rename 原子发布二进制、切换配置代际。旧进程的二进制从不被原地写入。
5. 原先运行时，通过验证所属关系的 systemd unit 执行 restart，等待服务 active、认证 API 与 mixed 监听就绪，并核对实际运行版本。原先停止时只安排下次启动使用新版本。
6. 上述步骤成功后才持久化 committed，再清理备份与 journal。失败或取消使用独立的有界清理上下文恢复旧文件、代际和原运行状态。恢复旧服务前对已确认所属的 unit 执行 `reset-failed`，避免新版本反复崩溃触发的 start-limit 阻止旧版本启动；恢复后也核对实际版本。恢复启动失败会组合报告原错误和恢复错误，并保留 journal/备份供下一次恢复，避免把失败当成功。

进程被强行终止时，未提交事务在下一次会触发恢复的应用调用中回滚。只读检查本身不触发恢复。显式 Stop 的持久标记优先，恢复不会越过用户停止意图重启。恢复前核对文件/代际摘要和 unit 所属关系，发现用户修改则保留现场并报错。没有常驻恢复进程；机器断电或客户端被 SIGKILL 后无法承诺立即恢复服务。

## 上游依据与取舍（2026-11-02）

[Mihomo build.yml](https://github.com/MetaCubeX/mihomo/blob/Meta/.github/workflows/build.yml) 定义资产命名和构建矩阵；本次实取 stable 为 `v1.19.33`，Alpha 为 `alpha-69c1631`。

- arm64 选择 `mihomo-linux-arm64-<version>.gz`。
- amd64 根据 Linux `/proc/cpuinfo` 中每个可见 CPU 的指令集交集及 `x/sys/cpu` 对 OSXSAVE/XGETBV 的检测，选最高兼容的 v3/v2/v1；虚拟机按 guest 暴露的能力判断。检测不完整时保守选 v1。优先所选等级，有缺失则降级；v3 能力也可使用旧普通 `amd64` 资产，旧 `amd64-compatible` 是 v1 的末级回退。低等级 CPU 不会选择旧普通 v3 资产。资产名校验不依赖测试宿主 CPU，实际选择集中在共享 Resolver，初装、指定版本和升级共用。
- [官方 FAQ](https://github.com/MetaCubeX/mihomo/wiki/FAQ) 的 v1/v2/v3 表示 CPU 指令集等级，具体要求见 [Go AMD64 最低要求](https://go.dev/wiki/MinimumRequirements#amd64)。旧 Linux 2.6.32～3.1 的 `go123` 构建未加入：lazyclash 自身 Go 1.26 已要求 Linux 3.2 以上。ARM32、MIPS、其他 Linux 架构仍未扩大支持范围。
- 正式版 `v1.19.33/checksums.txt` 实取 404，官方资产 API 提供 SHA256，所以下载继续依赖 API digest。检查版本时无需 API；本次 API 遭遇 403 限流时，两个 `version.txt` 仍正常，无需新增查询 API fallback。
- Alpha 发布移动 `Prerelease-Alpha` tag 并删除旧资产，文件名带 hash，当前另有 `checksums.txt`。未来支持 Alpha 升级必须固定版本文件名及可信摘要，并处理旧资产已删除的竞态；不能悄悄改用新 latest。无需为了比较 hash 引入 commit 历史 API。

参考 [Clash Verge Rev core_upgrade.rs](https://github.com/clash-verge-rev/clash-verge-rev/blob/dev/src-tauri/src/feat/core_upgrade.rs) 的暂存、执行验证、hard link 与 rename 发布思路。其“版本不同即升级”、检查与升级绑定以及缺少官方摘要的策略未沿用。本项目继续复用已有文件锁、下载器、安装记录和恢复日志。

未来扩展记录在 [TODO](../TODO.md)，没有为未支持的平台或其他内核增加空接口。
