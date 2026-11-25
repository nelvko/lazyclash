# TUI Toggle 组件

`internal/tui/toggle.go` 提供同包业务页面可复用的布尔状态控件。组件使用当前主题、ANSI 显示格宽度工具及 Bubbles key；不依赖 `internal/app` 或具体服务，也不启动命令、执行操作或产生动画。

它沿用 `renderButton` 等组件的结构，不单独实现 `tea.Model`。父级 `Model.Update` 负责键盘路由、异步任务、错误通知和实际状态查询，现有业务页面尚未接入。

## 状态与文案

| 状态常量 | 默认显示 | 颜色 | 允许切换 |
| --- | --- | --- | --- |
| `toggleOn` | `● 已开启` | `success` 绿色 | 是，请求关闭 |
| `toggleOff` | `○ 已关闭` | `muted` 次级灰色 | 是，请求开启 |
| `togglePending` | `◌ 切换中…` | `accent` 主题强调色 | 否 |
| `toggleUnavailable` | `不可用` | `disabled` 弱化灰色 | 否 |
| `toggleUnsupported` | `暂不支持` | `disabled` 弱化灰色 | 否 |
| `toggleUnknown` | `状态未知` | `disabled` 弱化灰色 | 否 |

`toggleModel` 的零值是 Unknown。无法识别的状态值同样显示为未知并拦截操作。Unavailable、Unsupported、Unknown 均不显示圆点。

`toggleLabels` 的六个字段 `On`、`Off`、`Pending`、`Unavailable`、`Unsupported`、`Unknown` 均可覆盖；未填写的字段使用默认文案。页面可传入现有本地化器产生的文字，例如运行中、已停止、启动中…、停止中…、重启中…，或英文标签。文案会去掉 ANSI 样式，并将空白规范为单个空格，保持单行布局及统一状态颜色。

## 组件 API

所有类型和函数均位于已有 `tui` 包内，与现有组件一样使用包内 API。

| API | 用途 |
| --- | --- |
| `toggleModel.setState(state toggleState, reason string)` | 同步外部状态和原因；仅 On/Off 更新最后确认状态 |
| `toggleModel.state` | 最近一次同步的状态 |
| `toggleModel.lastConfirmed` | 最后一次确认的 On/Off；尚未确认时为 Unknown |
| `toggleModel.reason` | 外部提供的最新原因，由父级按需展示 |
| `toggleModel.canToggle() bool` | 当前是否为可切换的 On/Off |
| `toggleModel.intent(msg tea.KeyPressMsg, focused bool) (toggleIntent, bool)` | 判断按键是否产生切换意图 |
| `toggleIntent.On` | 请求的目标值，不代表已生效 |
| `renderToggle(control toggleModel, options toggleOptions) string` | 返回一行 ANSI 样式文本 |

进入 Pending 不会更新 `lastConfirmed`。Unavailable、Unsupported、Unknown 同样保留历史确认信息，但这些状态均不可操作，不能把历史信息当作当前实际值。一个控件对应一个业务对象；切换业务对象时重新初始化控件。

`reason` 不拼接到开关文案，不参与宽度计算。父级可将权限、环境或查询失败原因显示在相邻说明、详情或现有通知中。

## 展示与布局

```go
var coreToggle toggleModel
coreToggle.setState(toggleOff, "")

options := toggleOptions{
	Labels: toggleLabels{
		On:      "运行中",
		Off:     "已停止",
		Pending: "启动中…",
	},
	Surface:      panel,
	Width:        16,
	RightAligned: true,
	Focused:      m.focus == "example.core",
}
value := renderToggle(coreToggle, options)
```

父级可将 `value` 直接放进既有 `dashboardBlock.lines`、卡片正文或列表行，也可单独展示。组件不生成边框、滑块、额外焦点占位或按钮面板。

`toggleOptions` 提供以下布局属性：

- `Labels`：六种状态的可覆盖文案。
- `Surface`：所在表面颜色；未指定时使用 `bg`，卡片可传 `panel`。
- `Width`：正值表示包含符号在内的精确显示格宽度，超长使用现有 `cellCrop` 截断；非正值按全部六种文案的最大显示宽度自动分配。使用同一套文案时，状态变化不会改变总宽度。
- `RightAligned`：在分配的宽度内向右对齐；默认向左对齐。
- `Focused`：使用主题 `focus` 色下划线强调文本，保留状态颜色；焦点不改变值、文案位置或总宽度，宽度补齐的空格不加下划线。
- `Dim`：复用现有弹窗背景弱化规则。

符号与文字之间固定一个空格。宽度计算、裁切和补齐均以终端显示格为单位，支持中英文、ANSI、组合重音、旗帜和 ZWJ emoji。焦点样式覆盖完整文本段，不在 Unicode 字素内部插入样式序列。

窗口变化时，父级根据新的可用区域重新传入 `Width`。同一行的多个控件可以传入一致宽度；如果 Pending 文案随操作变化且长度不同，可为它们预留统一的固定宽度。

最终完整视图继续通过既有 `terminalTheme` 适配 True Color、ANSI256、ANSI16 和 NO_COLOR。NO_COLOR 按项目规则去掉样式，父级可沿用现有行焦点轮廓提供文字焦点提示。

## 焦点与异步消息

父级使用现有 `m.focus`、导航及遮挡层路由，只给当前拥有输入权的控件调用 `intent`。组件不管理 Tab 或方向键，不监听全局消息。`Focused` 和 `intent` 的 `focused` 参数都由父级同一个焦点来源计算。

```go
focused := m.focus == "example.core"
intent, ok := coreToggle.intent(key, focused)
if ok {
	// 在父级重新检查业务能力、权限及页面忙碌状态。
	// 通过后先同步 Pending，再返回操作 Cmd。
	coreToggle.setState(togglePending, "")
	// intent.On 是交给业务用例的目标值。
}
```

Enter 和 Space 可以产生意图；其他按键、带修饰键的 Enter/Space、键盘协议标记的重复按键均被忽略。非 On/Off 状态或未聚焦时不会返回意图。`intent` 为只读操作，绝不自行进入 Pending 或翻转状态。父级应在同一次 `Update` 中同步 Pending，然后返回异步 Cmd，防止下一个按键再次启动操作。

异步接入遵循现有任务身份或 generation 机制：

1. 父级批准意图后，为当前对象生成新的操作身份并同步 Pending。
2. Cmd 调用业务用例，操作结束后查询真实状态；每条完成消息携带对象和操作身份。
3. `Update` 先核对身份。旧操作、已离开对象或已被新观测替代的消息直接丢弃，包括其错误提示。
4. 有效结果同步为确认的 On/Off；无法确认实际状态时同步 Unknown。
5. 操作错误经既有通知机制展示。错误不构成独立 Failed 状态，也不能单凭错误恢复旧值。

新的权威状态观测或对象切换同样需要让旧任务身份失效。generation 属于父级业务生命周期，不放入展示组件。

内核、系统代理、终端代理和 TUN 分别从自己的业务观测映射成六种状态，再复用相同的控件；父级继续负责各自的权限判断和确认流程。

`internal/tui/toggle_pty_test.go` 的 `toggleFixture` 给出了完整、可运行的父级示例：两个独立开关、单一焦点路由、先 Pending 后延迟结果、权威刷新使旧结果失效。

## 验证入口

```sh
go test ./internal/tui -run '^TestToggle' -count=1
go test -race ./internal/tui -run '^TestToggle' -count=1
make check build

go test -c ./internal/tui -o bin/toggle-tui.test
python3 scripts/toggle-pty.py \
  --binary bin/toggle-tui.test \
  --output .test-artifacts/toggle/pty --png
```

PTY 入口默认覆盖中文和英文 × True Color、ANSI256、NO_COLOR。每组覆盖 160×45、120×32、80×24、60×20、40×16 及恢复，检查六种状态、右对齐、焦点、双控件输入归属、Pending 重复触发拦截、不可操作状态和过期结果。

夹具只运行通用组件和内存父级，不访问 Mihomo、系统服务、代理配置或业务页面。生成的 PNG 根据真实 PTY 字符格及颜色重绘。
