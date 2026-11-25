# TUI 开关

在「设置 → 界面偏好 → 开关样式」选择样式。每个选项都展示开、关两个实际渲染效果。选择立即应用到所有页面，并保存到当前实例的 `preferences.json` 的 `SwitchStyle` 字段。缺少字段、类型错误或标识未知时使用默认的 `slanted`。

| 样式 | 标识 | 关闭 | 开启 |
| --- | --- | --- | --- |
| 斜切式 | `slanted` | `●▱▱` | `▰▰●` |
| 滑块式 | `slider` | 亮色滑块位于左端 | 亮色滑块位于右端 |
| 文本式 | `text` | `OFF` | ` ON` |
| 线条式 | `line` | `●──` | `━━●` |

## 终端表现

正常开关占三列一行。滑块式使用三个连续的空格，一个用较亮背景表示滑块，另外两个用同色系的较暗背景绘制轨道。线条式沿用所在行的背景。颜色统一来自主题。键盘焦点和鼠标悬浮使用页面既有的导航与高亮方式，焦点标记的空间由页面预留。

无色模式下，滑块式回退为可读的文本式。斜切式、线条式逐帧通过项目的 `ansi.StringWidth` 检查显示宽度；其中一帧偏离三列时，整种样式回退为文本式。终端把 East Asian Ambiguous 字符显示成两格时，可以用 `RUNEWIDTH_EASTASIAN=1 lazyclash` 使项目的列宽策略与终端一致。终端实际字形宽度及字体回退由本地终端决定，TUI 无法自动读取每种字体的度量值。能力回退仅作用于当前渲染，持久化的样式偏好保持不变。

## 页面接入

- 概览的内核、终端代理、TUN 使用开关。内核卡继续展示观察到的服务状态，网络卡保留需要激活或运行状态不一致等诊断信息。
- 设置的快捷键提示、IPv6、统一延迟、自启等二元项，以及帮助中的快捷键提示，共用同一组件。TUN 配置窗的开关编辑当前草稿，保存和权限确认沿用该窗口的流程。
- `internal/tui/toggle.go` 提供 `toggleModel`、`renderToggle` 和 `switchAnimations`。操作权属于当前页面。Bubbles 的键绑定匹配 Space 和 Enter，页面负责鼠标命中、键盘导航和业务确认。
- `internal/tui/page_switches.go` 将全局样式和页面实际状态传给渲染器。预览也调用 `renderToggle`。一次性操作（例如重启、更新和下载）继续使用普通动作按钮。

## 状态同步

`toggleOn`、`toggleOff` 是可操作的确认状态。`togglePending` 以三列 `...` 表示异步等待；`toggleUnavailable`、`toggleUnsupported`、`toggleUnknown` 显示本地化的弱化说明。操作进行中及状态不可用时，页面拦截重复输入。后台诊断和操作失败走既有通知流程。

每个已挂载开关使用稳定的 ID。首次观测只确定位置；收到启停请求后，父级先进入等待状态，收到业务确认后才由 `switchAnimations.observe` 比较新旧值。斜切式、滑块式与线条式分别在约 0、60、120 ms 展示起点、中点和终点。文本式直接展示确认值。每条 Bubble Tea 定时消息携带开关 ID 和本次动画的序号。移除控件、切换样式或开始另一轮动画后，过期帧失效。

## 验证

```sh
go test ./internal/tui -run '^Test(Switch|Toggle)' -count=1
go test -c ./internal/tui -o bin/switch-tui.test
python3 scripts/toggle-pty.py --binary bin/switch-tui.test --output .test-artifacts/switch-component
python3 scripts/toggle-pty.py --binary bin/switch-tui.test --output .test-artifacts/switch-wide \
  --lang en --profile truecolor --wide-ambiguous
```

PTY 夹具使用两个独立的内存控件和延迟确认。默认运行中文和英文的真彩、256 色、无色组合，在 160×45、120×32、80×24、60×20、40×16 及恢复尺寸下检查字符格。验收还读取滑块三个单元实际的背景色，检查中间帧、键盘焦点、等待期间的重复输入和过期业务结果。加 `--png` 可使用 Pillow 按 PTY 字符格与颜色重绘截图。
