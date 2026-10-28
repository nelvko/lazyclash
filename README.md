# lazyclash

以 TUI 为主要交互的 Mihomo 终端管理工具。Go module：`github.com/nelvko/lazyclash`。

首阶段已实现：安装 → 校验和保存 YAML 订阅 → 应用 → 服务启动 → 查看代理组并切换节点。没有订阅时，默认的 `PROXY` 选择组包含 `DIRECT` 和 `REJECT`，可直接启动；默认不会提供远程代理节点。

## 构建与使用

需要 Go 1.25+。服务管理需要 Linux、systemd 和可用的普通用户会话；macOS 可构建、运行模型测试和 TUI，但没有 macOS 服务管理后端。

```sh
make build
./bin/lazyclash
```

TUI：`i` 下载官方 Mihomo，`b` 安装本地二进制，`s/x/r` 启动/停止/重启，`a` 添加订阅，`2` 查看和应用订阅，`3` 查看组和节点。`Enter` 应用或选择，`g` 刷新，`Esc` 取消，`q` 退出。执行任务时取消或退出会等待清理与回滚。支持窄窗口和列表滚动。

CLI 为自动化、Shell 集成和诊断提供薄入口，与 TUI 调用同一份业务用例：

```sh
lazyclash install                        # 固定官方版本并核验 SHA256
lazyclash install --bin /absolute/mihomo # 离线安装，仍校验 -v 和默认配置
lazyclash profile add work https://example.org/subscription.yaml
lazyclash profile add local /absolute/subscription.yaml
lazyclash profile list
lazyclash profile apply work
lazyclash profile apply default
lazyclash service start
lazyclash service stop
lazyclash service restart
lazyclash status                         # 不含密钥的 JSON
lazyclash nodes
lazyclash select PROXY DIRECT
lazyclash doctor
```

添加只校验并保存，不隐式应用；同名文件拒绝覆盖。停止状态应用只切换配置，不隐式启动。运行状态应用会重启并检查控制 API；失败恢复旧配置与原服务。自动选择组可查看，只有 `Selector` 组可手动切换。

订阅应为完整的 Mihomo YAML，而非 Base64 节点列表或其他内核格式。TUI 中订阅来源隐藏输入，不把 URL 保存到普通日志。CLI URL 参数可能进入用户自己的 Shell 历史，需要保密的来源优先通过 TUI 输入。

## 服务与控制 API

首版只实现用户级 `lazyclash-mihomo.service`。当前只需要本机代理与 API，无需 root；安装、操作和清理均归当前用户所有。安装不自动启动、不 enable，也不修改 linger。需要退出登录后继续运行时，先检查 `lazyclash doctor`，再由用户决定是否执行：

```sh
loginctl enable-linger "$(id -un)"
```

默认 mixed 代理端口为 `127.0.0.1:7897`，控制 API 为 `127.0.0.1:9097`。安装与启动前检查端口，冲突时失败，不停止已有服务腾位置。首版不提供自定义端口界面。

安装生成随机 API secret。每次生成有效配置都注入相同的本机 controller 和 secret，TUI/CLI 自动认证，安装后可立即查看默认组。密钥、原始订阅和有效配置使用私有目录及 `0600` 文件权限；状态输出不包含密钥。内核校验输出不直接显示，服务标准输出/错误暂时丢弃，避免 provider URL 等凭据进入普通日志。服务状态、退出状态及 API 可达性用于首阶段诊断。

受管配置字段：API 地址/凭据、mixed 端口、loopback 绑定、认证和 CORS；其他 controller、代理端口、listeners/tunnels、TUN、DNS、TUIC server、iptables、NTP 与外部 UI 下载入口关闭。保留订阅的出站节点、组和路由。首阶段不管理宿主代理、DNS、路由或特权透明代理。

数据默认位于 `$XDG_DATA_HOME/lazyclash`，未设置 XDG 时为 `~/.local/share/lazyclash`。包含安装二进制、完整配置快照、运行数据、订阅与恢复日志。unit 位于用户的 `$XDG_CONFIG_HOME/systemd/user`（默认 `~/.config/systemd/user`）。

`--home ABS` / `LAZYCLASH_HOME` 可隔离数据；`--service-name lazyclash-NAME` / `LAZYCLASH_SERVICE` 可隔离服务。不同实例仍需避免默认监听端口冲突。程序核验 unit 内容及 manager 实际加载路径，拒绝修改另一实例、手改 unit 或带 drop-in 的 unit。

## 父 Shell 集成

子进程和 TUI 无法修改父 Shell。显式求值以下输出：

```sh
eval "$(lazyclash env)"           # 只设置当前 Shell 的代理环境变量
eval "$(lazyclash env --unset)"   # 即使未安装或配置损坏也可清除

eval "$(lazyclash init --shell zsh)"  # bash 同样支持；只输出函数，不改启动文件
lazyclash_on                     # 启动成功后设置当前 Shell 环境
lazyclash_off                    # 清除环境，不停止后台服务
```

TUI 启停服务不会改变这些环境变量。`env --unset` 清除标准大小写代理变量和 no_proxy；如原先有自定义值，请在 Shell 中自行保存和恢复。

## 架构与数据保护

- `internal/tui`：Bubble Tea 异步模型、输入和显示；不启动自身 CLI。
- `internal/cli`：Cobra/Fang 入口与 Shell 输出。
- `internal/app`：唯一的安装、订阅、应用、服务和节点用例。
- `internal/store`：持久进程锁、取消等待、私有文件和原子替换。
- `internal/mihomo`：固定版本下载、SHA256、YAML、内核校验和控制 API。
- `internal/service`：实际 systemd 用户服务操作和所属关系验证。

固定 Bubble Tea 1.3.10、Bubbles 0.21.0、Lip Gloss 1.1.0、Fang 1.0.0、Cobra 1.10.2。Fang 的 Lip Gloss v2 依赖固定到稳定的 2.0.6，公共 cellbuf 固定为兼容的 0.0.15；完整依赖图记录在 go.mod/go.sum。

安装在私有 staging 中完成并校验，再发布二进制与默认快照；失败回滚 unit 和文件。应用以完整快照和原子指针切换提交，并保留旧快照用于失败恢复。操作先获得进程锁再读取状态，锁文件不删除。进程中断留下的安装/应用日志会在下一次操作时恢复；如果用户修改了相关文件，程序保留现场并报告冲突，不默默覆盖或删除。

参考源码 `/Users/nelvko/GolandProjects/proxyctl` 只读用于研究失败边界。新仓库历史独立，没有旧名称别名、运行配置读取或迁移逻辑；根目录下的 `proxyctl/` 也明确忽略。

## 验证

```sh
make check  # go test ./... 与 go vet ./...
make race   # go test -race ./...
```

普通构建和单元测试使用临时目录、HTTP fixture 与替换服务依赖，不创建 VM、不安装服务，也不操作现有代理配置。Linux 运行、真实 PTY 和 Mihomo/systemd 端到端的独立入口见 [验证说明](docs/verification.md)。

[路线图](docs/roadmap.md) 记录后续能力。当前不预建其他内核、系统级或其他平台服务后端。

上游依据：[Mihomo 官方 release](https://github.com/MetaCubeX/mihomo/releases/tag/v1.19.33)、[控制 API](https://wiki.metacubex.one/en/api/)、[控制器配置](https://wiki.metacubex.one/en/config/general/)。
