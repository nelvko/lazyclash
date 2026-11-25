# 设置页

设置页集中管理界面偏好、持久服务设置和维护操作。左侧列出设置名称，当前值和操作与标签对齐。

- **界面偏好**包含语言分段选项、快捷键提示栏开关和主题下拉框。
- **服务与端口**包含自动启动、注销后运行条件和本地端口编辑。端口摘要显示已保存的 HTTP/SOCKS5 mixed 端口与脱敏控制 API 地址。
- **内核管理**包含 Mihomo 安装版本、安装或管理和更新入口、重启与卸载说明。
- **故障排查**包含客户端版本、日志页入口和终端命令 `lazyclash doctor`。服务错误、异常退出结果和配置漂移仅在出现时展示。

下载设置位于固定页头。日常启停和 TUN 在概览，路由模式在概览和代理页，配置导入在订阅页。
概览的端口详情入口会跳到设置页并打开同一个端口编辑器。端口修改保留审阅步骤。

## 布局与交互

正文填满侧栏和外侧留白之外的可用宽度，不设最大列宽。
正文宽度至少 80 列时使用两列，否则按同一分组顺序堆叠；矮终端保留宽度足够的双列布局。
每组按最长名称建立标签列，最多占 28 个字符格，值和控件在其后两格开始对齐，不随卡片变宽远移。
名称过长或剩余空间不足时将值和控件移到下一行，按钮按完整显示宽度换行；说明仍使用卡片的可用宽度。
分隔线区分设置项，选中项带 `✓`，焦点带 `›`，无色终端保留这些文字标记。

Tab/Shift+Tab 按可见阅读顺序遍历操作，现有滚动机制自动揭示焦点；滚轮和 PgUp/PgDn 浏览页面。
底栏按聚焦控件显示实际操作，例如应用语言、编辑端口或查看日志。通过 Tab 聚焦选项后按 Enter 执行，← / Esc 返回导航。
改变窗口尺寸会重新计算布局与命中区域。语言、提示栏和主题选择会保存到下次启动。

点击主题下拉框，或用 Tab 聚焦后按 Enter 打开。菜单按 Light、Dark、Auto 排列，`✓` 标记当前偏好。用 ↑/↓ 移动后按 Enter，或直接点击选项即可保存。Esc 取消本次选择。

默认的 Dark 沿用 Mariana 配色。Light 是可保存的浅色主题占位，目前与 Dark 外观相同。Auto 根据终端报告的背景明暗选择主题，终端未报告背景色时使用 Dark。

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

初版真实 PTY 完成中英文 × TrueColor、ANSI256、NO_COLOR 六组，每组均验证
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

2026-11-21 将设置页重组为界面偏好、服务与端口、内核管理和故障排查。模型回归覆盖入口去重、
端口摘要脱敏、重启可用性、条件诊断和缩放后的焦点。中英文 TrueColor PTY 各完成 14 次尺寸切换，
验证 240 列双列至 40 列单列、语言和提示栏保存、端口窗口往返，以及从故障排查打开日志后返回设置。
每组读取 14 次端口与 1 次隔离日志流。截图和汇总位于
`.test-artifacts/settings-dedup/pty/20261121-120313-7ce5cf/`。
