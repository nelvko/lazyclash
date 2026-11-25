# 设置页

设置页集中管理界面偏好、持久服务设置和维护操作。卡片左侧列出设置名称，右侧显示开关和下拉框。

- **界面偏好**包含语言、快捷键提示栏、主题、开关样式及外部编辑器设置。
- **Mihomo 设置**包含当前实例的 IPv6 开关、统一延迟开关和日志等级下拉框。
- **服务与端口**包含自动启动、注销后运行条件和本地端口编辑。端口摘要显示已保存的 HTTP/SOCKS5 mixed 端口与脱敏控制 API 地址。
- **内核管理**包含 Mihomo 安装版本、安装或管理和更新入口、重启与卸载说明。
- **故障排查**包含客户端版本、日志页入口和终端命令 `lazyclash doctor`。服务错误、异常退出结果和配置漂移仅在出现时展示。

下载设置位于固定页头。日常启停和 TUN 在概览，路由模式在概览和代理页，配置导入在订阅页。
点概览「终端代理」下的「配置」可在弹窗中修改混合端口。控制 API 端口在设置页的「服务与端口」中修改。两处的端口变更都需审阅后应用。

## Mihomo 通用设置

安装内核后即可修改。点击开关，或用 Tab 聚焦后按 Space / Enter，即可切换。日志等级从下拉框选择。

| 设置项 | 新实例默认值 | 用途 |
| --- | --- | --- |
| IPv6 | 开 | 控制 Mihomo 的 IPv6 支持。启用主机 TUN 时内核临时开启 IPv6，关闭 TUN 后恢复保存的选择。 |
| 统一延迟 | 关 | 调整节点延迟测试的计算方式，减少不同代理协议握手时间对结果的影响。 |
| 日志等级 | `info` | 控制内核输出的详细程度，可选 `debug`、`info`、`warning`、`error`、`silent`。 |

这三项保存在当前实例，优先于订阅原文和该订阅的覆写。刷新或切换订阅、重新打开 TUI 和重启内核都会保留选择。已有实例保留明确设置过的值。原先选择「跟随配置」的项取当前已部署配置中的值，缺省时取上表默认值。

保存时会校验生成的配置。运行中的内核按需热重载，停止的内核在下次启动时读取新配置。订阅源文件保持原样。[Mihomo 通用配置文档](https://wiki.metacubex.one/config/general/)

## 布局与交互

正文填满侧栏和外侧留白之外的可用宽度，不设最大列宽。
正文宽度至少 80 列时使用两列，否则按同一分组顺序堆叠；矮终端保留宽度足够的双列布局。
卡片边框内左右各留两个字符格。开关和下拉框按右边缘对齐。放不下名称、两个字符格的间隔和完整控件时，控件移到名称下一行，从左侧开始。
版本号、端口地址和说明靠左显示。「编辑」「查看」「重启」等操作跟在名称或版本号后面，空间不足时换行。
分隔线区分设置项。下拉菜单用 `✓` 标记当前值。

「界面偏好 → 开关样式」中的每个选项都并排预览关闭和开启效果。默认斜切式显示 `●▱▱` 和 `▰▰●`。滑块式用连续的背景色画出六格宽的长条。亮色滑块占两格，关闭时在左端，开启时在右端。文本式显示 `OFF` 和 ` ON`，线条式显示 `●──` 和 `━━●`。无色终端中的滑块式，以及显示宽度超过三列的字符样式，会在当前终端显示为文本式。

确认选择后，界面立即使用该样式，并把标识 `slanted`、`slider`、`text` 或 `line` 保存在当前实例的 `preferences.json` 的 `SwitchStyle` 字段。读取时缺少字段或值无效会使用默认样式。若保存失败，界面恢复先前选择并显示原因。

Tab/Shift+Tab 按可见阅读顺序遍历操作，现有滚动机制自动揭示焦点；滚轮和 PgUp/PgDn 浏览页面。
底栏按聚焦控件显示实际操作，例如选择语言、编辑端口或查看日志。通过 Tab 聚焦选项后按 Enter 执行，← / Esc 返回导航。
改变窗口尺寸会重新计算布局与命中区域。语言、提示栏和主题选择会保存到下次启动。

语言和主题使用相同的下拉框。点击控件，或用 Tab 聚焦后按 Enter 打开。语言可选 English 和简体中文，主题按 Light、Dark、Auto 排列。`✓` 标记当前偏好。用 ↑/↓ 移动后按 Enter，或直接点击选项即可保存。Esc 取消本次选择。

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

2026-11-25 初版曾提供「跟随配置」选项。在专用 Linux/arm64 虚拟机中通过设置页修改三项通用设置，并从 Mihomo v1.19.33 的控制 API 读回实际值。运行时修改保留原进程，服务停止时保存保留停止状态。[初版真实内核验证结果](../.test-artifacts/mihomo-general-settings/linux-real-core.json)

中英文 PTY 在真彩、256 色和无色模式下覆盖 240 至 40 列、键鼠选择、取消和恢复跟随配置。[160×45 中文设置页](../.test-artifacts/mihomo-general-settings/pty/20261125-163818-26756d/zh-Hans/truecolor/mihomo-overrides-wide.png) · [40×16 菜单](../.test-artifacts/mihomo-general-settings/pty/20261125-163818-26756d/zh-Hans/truecolor/mihomo-menu-compact.png)

设置行的布局回归按终端字符格检查中英文七个开关和下拉框，包括刚好能同行和少一格必须换行两种情况。真实 PTY 另验证了换行后点击语言下拉框以及用 Esc 收起菜单。[160×45 双列布局](../.test-artifacts/settings-row-alignment/pty/20261125-165055-37cbbc/zh-Hans/truecolor/settings-wide.png) · [28×24 换行布局](../.test-artifacts/settings-row-alignment/pty/20261125-165055-37cbbc/zh-Hans/truecolor/settings-wrapped-28x24.png)

实例设置固定后，Linux/arm64 实测验证了新安装的默认值和首次导入订阅后的值。通过设置页修改三项后，切换订阅、刷新、重启服务并重开 TUI，均与保存值一致。运行中的 Mihomo v1.19.33 热重载时保留原进程。[真实内核读回结果](../.test-artifacts/settings-fixed-switches/linux-real-core.json)

统一样式前的六格滑轨留有宽屏和窄屏预览。[历史双列设置页](../.test-artifacts/settings-fixed-switches/rail-only/20261125-170352-e741fa/zh-Hans/truecolor/settings-wide.png) · [历史换行布局](../.test-artifacts/settings-fixed-switches/rail-only/20261125-170352-e741fa/zh-Hans/truecolor/switch-wrapped-28x24.png)

统一开关的设置页在中文真彩、英文 256 色和英文无色 PTY 上均完成四种样式的切换。每轮检查菜单中的关、开预览，以及页面开关切换样式前后的命中坐标。彩色终端逐格检查滑块和轨道的背景色，无色终端验证滑块式回退至 OFF / ON。每轮记录四次样式保存，服务、路由模式和端口写入均为零。[160×45 样式菜单](../.test-artifacts/unified-switch/settings/20261125-172336-2dfe93/zh-Hans/truecolor/switch-style-menu.png) · [24×24 聚焦开关](../.test-artifacts/unified-switch/settings/20261125-172336-2dfe93/zh-Hans/truecolor/switch-wrapped-24x24.png)
