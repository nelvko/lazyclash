# 验证入口

普通构建/单元测试不会调用 OrbStack、安装服务或修改系统配置。以下三类运行验证均为显式入口，不由 `go test` 自动触发。

## 1. 单元、HTTP fixture、模型与静态检查（macOS 或 Linux）

```sh
make check
make race
make build
```

覆盖原子写和锁取消、下载校验及失败清理、控制 API HTTP fixture、配置生成、服务归属和命令失败、安装与应用回滚、恢复日志和用户修改保护。TUI 模型测试独立验证异步操作、取消、过期结果与显示状态；模型测试不等于真实终端验证。

## 2. Linux VM 内的测试

先检查已安装的 OrbStack，不创建或启动未知环境：

```sh
orb list
VM=你的专用测试虚拟机
orb -m "$VM" sh -c 'id; uname -m; ps -p 1 -o comm=; systemctl --user is-system-running; loginctl show-user "$(id -un)" -p Linger -p RuntimePath'
./scripts/verify-linux.sh "$VM"
```

要求普通用户、systemd 用户会话、Python 3。脚本根据 VM 实际架构，在宿主编译 Linux 测试二进制，复制至 Linux 的 `mktemp` 目录后**逐包执行**，不依赖 VM 内 Go 的版本。它并非仅交叉编译检查。使用 `CGO_ENABLED=0`，Linux 此入口不声称运行 race；race 单独在宿主运行。

## 3. 真实终端 PTY

`scripts/pty-smoke.py` 使用 Python 标准库 `pty.fork` 创建有 controlling terminal 的进程，发送按键、改变窗口大小并检查渲染和退出。它不调用模型的 `Update` 方法来模拟终端。

```sh
python3 scripts/pty-smoke.py --help
```

`empty` 模式仅操作专用的未安装数据目录，检查首次提示、输入取消、窄屏及异步安装取消。`installed` 模式需要已启动的专用 Mihomo 服务，用真实 API 检查节点查看和选择。完整的目录、服务、前置检查和清理由下一入口安排，建议通过它执行两种模式。

## 4. 真实 Mihomo + systemd 端到端

```sh
./scripts/e2e.sh "$VM"
```

此脚本显式修改所选 Linux VM 中的**临时资源**：

- 动态检测 CPU 架构、普通用户与用户 systemd，不硬编码用户名或工程目录。
- `mktemp` 数据目录、随机 `lazyclash-e2e-*` unit；运行前检查同名 unit 与 `9097`/`7897` 端口，冲突即失败，不停止已有资源。
- 由产品下载官方 Mihomo **v1.19.33** 并校验固定 SHA256；随后另建独立 unit 验证 `--bin`。
- 验证真实默认配置、API 认证和组、PTY 显示与节点切换、本地 HTTP YAML 保存/校验/应用，以及 start/stop/restart 生命周期。
- `trap` 仅清理本次确认所属的 unit、进程和文件，失败也执行；不读取旧代理配置，不修改 macOS 代理、DNS 或路由。

日志保存在忽略的 `.test-artifacts/`，只包含脱敏场景输出、PTY 文本、测试结果与清理状态，不保存 secret、原始配置或未过滤 journal。失败时先查看这些诊断，不直接贴出原始配置。

## 已执行环境

2026-10-28：

- 宿主 macOS/arm64，Go 1.27.1：构建、全项目 `go test`、`go vet`、`go test -race` 通过。
- 复用现有专用 OrbStack Debian 12/arm64 测试 VM（本次选择 `proxyctl-e2e`），普通用户、systemd 252、用户 manager 可用且 Linger=yes。脚本仍要求调用者显式选择 VM，不依赖此名称。
- `verify-linux.sh` 将各包测试二进制放入 VM 后实际执行，包括 TUI 模型与 HTTP fixture。
- `e2e.sh` 实际运行官方下载/校验与本地安装、有效默认配置、HTTP 订阅、API、PTY 和服务生命周期；SIGTERM/按键取消等待清理。
- 清理检查确认专用 unit `LoadState=not-found`，7897/9097 释放，临时文件及测试进程清除；脱敏记录在 `.test-artifacts/`。

未实际验证：Linux/amd64、其他发行版、真实注销/重启后的服务持续运行、最低 Go 1.25 工具链。没有把交叉编译视为这些环境的运行证明；macOS 服务管理明确不支持。
