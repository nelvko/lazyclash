# 路线图

已完成首阶段：首次安装引导、官方校验下载和本地二进制安装、有效默认配置、Mihomo YAML 保存/校验/应用及回滚、用户服务生命周期、控制 API 和节点选择。模型测试、真实 Linux、真实 PTY 与 Mihomo/systemd 端到端具有独立验证入口。

已完成第二轮：响应式 Overview、统一安装向导、稳定版解析、独立下载来源/代理与连接测试、安装预检查和真实进度、运行中原子更新、偏好与引导历史、Charm v2 适配。

已完成运行监控与连接：实际速率、本次内核累计、连接数与 RSS；Connections 搜索、详情和确认关闭指定连接；可取消的自动刷新、部分不可用反馈与重连，CLI 共享采样和关闭用例。跨内核重启的流量历史暂不实现。

已完成只读规则查看：按内核顺序展示、搜索和详情，独立异步刷新、取消与重试，CLI 共享读取用例。操作见 [规则查看](rules.md)。

已完成七页导航与实时日志：Overview、Proxies、Profiles、Connections、Rules、Logs、Settings；紧凑分组侧栏、上下文帮助、可滚动帮助和独立焦点组。日志为容量有界、可取消和脱敏的当前会话实时观察，CLI/TUI 共享用例；不提供历史回放，不将原始日志落盘。默认 Mariana 主题已覆盖主要页面及终端颜色回退。

已完成运行端口设置：Settings 内审阅并修改本机代理与控制 API 端口，检查 TCP/UDP 冲突，运行中明确确认重启，停止时保持停止；取消或失败恢复旧配置和服务状态，保留原始 profile 与 API 密钥。CLI 共享用例，操作见 [运行端口设置](runtime-settings.md)。

Linux 首发服务 scope 与权限实现及 Linux/arm64 双作用域、TUN 端到端验收已完成，平台范围及限制以 [验证记录](verification.md) 为准。范围见 [服务 scope](service-scopes.md)：

- 普通用户管理自己的 user 代理；root 管理独立 system 实例，默认 `/var/lib/lazyclash/<service-name>`。`sudo lazyclash` 不复制或切换原用户数据，不提供普通用户 TUN 授权流程。
- system Mihomo 使用专用非 root 账号，普通代理无额外 capabilities；明确确认 host 范围的 TUN 后才授予 `CAP_NET_ADMIN CAP_NET_RAW`。首版不提供 UID 筛选。
- 独立 unit 直接托管内核，同一受保护 lazyclash 程序的有限钩子负责 TUN 冲突、就绪与退出清理。没有独立 helper、常驻 daemon 或脱离客户端的事务执行器；遗留 journal 在下一次调用时恢复。
- `make package VERSION=dev` 生成 Linux amd64/arm64 的发行归档与 SHA256SUMS。完整验收包括用户/root 普通代理、TUN、停止清理、失败恢复及生命周期，不能以打包、单测或交叉编译代替。

项目未发行，直接重构；不保留旧 unit、manifest、目录布局或命令行为的兼容层，不编写旧安装自动迁移。

其余后续需求：

1. 基于具体需求完善日志筛选与诊断，保持当前不落盘的敏感信息边界。
2. 必要的主题偏好。
3. 有具体需求后再评估其他内核或平台。

普通用户提权、同一实例的 user/system 切换、TUN 自动交接、其他平台服务、独立系统 DNS 接管、系统代理自动设置、自动 enable/linger 和 Shell 启动文件修改均不在首发范围，也不是当前已承诺的后续实现。没有占位后端、插件框架或历史兼容层。
