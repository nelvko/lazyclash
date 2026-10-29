# 运行端口设置

在 TUI 中按 `7` 打开 Settings，再选择 **Runtime settings / Ports**。已安装 Mihomo 后，可编辑本机 HTTP/SOCKS 代理端口和控制 API 端口，先 Review changes，再确认应用。两个端口必须为不同的整数，范围均为 1024–65535。

监听仍限定为本机，控制地址原有的 loopback 主机部分及 API 密钥保持不变。此入口不开放 LAN、TUN、DNS 或系统代理设置，也不会修改 systemd unit、自启动或 Shell 启动文件。

## 应用与服务状态

- **服务已停止**：配置提交后仍保持停止，下一次明确启动使用新端口。
- **服务正在运行**：确认页明确显示旧值、新值和连接中断影响；选择 **Apply & restart** 后，停止并重新启动当前受管理服务，等待新控制 API 通过认证就绪检查。
- **端口占用或校验失败**：保留输入并显示原因；预检查不会停止占用端口的进程，也不会替换当前配置。
- **应用失败或取消**：等待安全边界与恢复完成，再展示结果；恢复旧的配置代际、端口和原有服务运行状态。若恢复无法完成，保留恢复记录与所需文件并报告错误，供后续诊断。

预检查只是当时的观察，提交时会再次验证安装、配置、服务状态和端口。其他进程在检查后抢占端口时，应用会失败并恢复，不会停止那个进程。过期的确认计划必须重新 Review。

首次安装不会启动服务，因此默认端口已被占用时仍可完成安装，并看到提示。随后通过此页选择其他端口，再明确启动。普通启动仍会检查冲突。

## 配置与界面刷新

设置以当前真正应用的配置为基础，只更新受管理的监听字段。已保存但尚未应用的订阅或本地 YAML 文件不会被重新读取来覆盖当前配置，原始 profile 文件和 API 密钥不因端口修改而变化。以后应用 profile 时，继续使用已保存的新端口。

提交后刷新 Dashboard、代理组及控制 API 地址。监控、Rules 和 Logs 在恢复可见时连接新地址，旧异步任务的迟到结果不会覆盖新状态。

TUI 不能修改父 Shell 中已经导出的代理变量。改端口后，`lazyclash env` 会输出新地址；需要父 Shell 同步时，明确执行 `eval "$(lazyclash env)"`，或重新调用已配置的 Shell helper。LazyClash 不会自动编辑 Shell 配置。

## CLI

CLI 与 TUI 直接调用同一组读取、预检查和事务应用用例：

```sh
lazyclash settings
lazyclash settings show
lazyclash settings ports --mixed-port 17897 --controller-port 19097 --dry-run
lazyclash settings ports --mixed-port 17897 --controller-port 19097 --restart
```

任一端口参数可以省略并保留当前值，至少提供一个参数。`--dry-run` 只输出脱敏的审阅计划。服务正在运行且配置确有变化时，需要 `--restart` 明确接受连接中断；停止状态不需要该标志，提交也不会隐式启动。完全相同的设置作为无变更操作处理。

## 验证入口

- `make check`、`make race`、`make build`：普通测试和构建，不创建 VM、不安装服务。
- `scripts/verify-linux.sh VM`：在显式选择的 Linux VM 中执行测试二进制。
- `scripts/e2e.sh VM --proxy-mode environment`：使用独立临时目录和随机服务，验证真实 Mihomo 与 systemd，包含 runtime-settings PTY 场景；所有监听先检查冲突，结束后清理。

新增端口 E2E 会动态寻找并跟踪测试端口，分别验证 TCP/UDP 冲突、运行与停止状态、原始 profile 和控制密钥保留、profile 应用、Shell 导出及 TUI 各主要控制 API 页面的重新连接。该入口不读取开发者现有代理配置。
