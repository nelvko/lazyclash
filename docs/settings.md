# 设置页

设置页采用分组设置行：左侧名称，右侧当前值或操作，次行按需补充说明。
现有内核、端口、语言和 TUN 操作继续通过原有应用用例执行。

- **界面偏好**：语言分段选项、快捷键提示栏开关、当前 Mariana 主题。
- **服务与启动**：内核状态、启动/停止/重启，以及自动启动与注销后运行条件。
- **网络与路由**：路由模式分段选项、本地端口与控制 API、当前作用域的 TUN 状态。
- **内核与配置**：安装版本、管理/更新入口、配置导入和卸载说明。

下载网络继续使用固定页头入口。安装、端口审阅、配置导入及主机 TUN 确认流程沿用原实现。
主题为只读信息；页面不增加尚未实现的设置。

## 布局与交互

正文填满侧栏和外侧留白之外的可用宽度，不设最大列宽。
正文宽度至少 80 列、终端高度至少 20 行时使用两列，否则按同一分组顺序堆叠。
每行优先对齐名称与值/控件；空间不足时将值和控件移到下一行，按钮按完整显示宽度换行。
分隔线区分设置项，选中项带 `✓`，焦点带 `›`，无色终端保留这些文字标记。

Tab/Shift+Tab 按可见阅读顺序遍历操作，现有滚动机制自动揭示焦点；滚轮和 PgUp/PgDn 浏览页面。
改变窗口尺寸会重新计算布局与命中区域。语言和提示栏沿用已保存的偏好，模式按钮指示保存的模式；
实际运行模式不同或无法读取时单独说明。服务错误、最近退出结果、配置漂移及 TUN/模式诊断保留在对应分组。

设置展示实现独立放在 `internal/tui/settings_view.go`，新增文案放在独立的 `*.settings.json`。
复用现有按钮、面板和页面滚动组件；[Charm Bubbles](https://github.com/charmbracelet/bubbles)
没有对应的设置表单或开关组件，端口编辑仍使用其 textinput。

设计借鉴用户提供的 Clash Verge、CFW 及工具设置截图中的分组、行对齐和当前值展示，
并参考 [Lazygit 配置](https://github.com/jesseduffield/lazygit/blob/master/docs/Config.md)
对自适应面板和键盘焦点滚动的处理。

## 验证入口

```sh
go test ./internal/tui -run 'TestSettings|TestLanguage|TestRuntimePorts|TestKeyHints|TestTun' -count=1
go test -c ./internal/tui -o bin/settings-tui.test
python3 scripts/settings-pty.py --binary bin/settings-tui.test \
  --output .test-artifacts/settings-redesign/pty \
  --profile truecolor --profile 256 --profile no-color
```

PTY 使用隔离的内存后端，不访问真实 Mihomo、订阅或系统服务。PNG 从实际 PTY 字符格与颜色重绘，
用于检查排版和命中，不代表真实代理网络或系统服务生命周期验证。

2026-11-05 验证通过：`make check build`、八项 Python 终端解析测试，以及
`./scripts/verify-linux.sh lazyclash-e2e` 的 Linux/arm64 实际测试。
独立导出的设置页改动基于已提交源码通过构建、设置/语言/端口/TUN/提示栏与文案定向测试，
确认不依赖其他会话未提交的页面实现。

真实 PTY 完成中英文 × TrueColor、ANSI256、NO_COLOR 六组，每组均验证
160×45、120×32、80×24、60×20、40×16 并恢复宽屏；覆盖语言保存时重复点击阻断、
提示栏鼠标切换和 Ctrl+T 恢复、滚轮、Tab 揭示导入与端口，以及端口窗口取消后的焦点恢复。
每组语言保存 2 次、提示栏保存 2 次、端口读取 6 次，服务、路由模式和端口应用次数均为 0。
已目视检查中文宽屏、40×16 及英文无色布局。

证据在忽略目录 `.test-artifacts/settings-redesign/`；PTY 汇总和字符格截图在
`pty/20261105-214548-3cca79/`。本次未重跑真实 Mihomo/systemd/TUN 生命周期 E2E。

2026-11-06 移除设置页沿用通用布局的 112 列上限。回归从实际终端尺寸验证正文宽度和卡片最右边框，
覆盖侧栏出现及单双列断点，避免只直接测试 `settingsContent` 而遗漏外层限宽。
`make check build`、Linux/arm64 VM 测试与独立导出的设置页定向测试均通过。
中英文 TrueColor PTY 完成 240、200、160、120、105、104、100、99、84、83、80、60、40 列及恢复宽屏；
每组端口读取 14 次，语言与提示栏各保存 2 次，服务、模式和端口应用次数均为 0。
已目视检查宽屏和单列布局。证据在 `.test-artifacts/settings-width/`，最终 PTY 为
`pty/20261106-163755-cb3db0/`。本次未重跑完整颜色组合或真实内核/服务生命周期。
