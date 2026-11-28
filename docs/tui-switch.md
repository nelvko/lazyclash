# TUI Switch 组件

设置页的底部提示栏、开机自启、允许局域网访问及 Mihomo 布尔配置使用默认的 `Slider`。概览的内核状态、终端代理和虚拟网卡使用 `Status`。两种外观共用受控状态和输入处理。[组件源码](../internal/tui/switch.go)

## 状态与输入

| 字段或方法 | 作用 |
| --- | --- |
| `On` | 最近确认的布尔值，决定滑块位置以及下一次请求的方向 |
| `Disabled` | 禁用操作，可继续展示已知状态或不可用原因 |
| `Pending` | 阻止重复请求，在原位置显示等待标记 |
| `OnLabel`、`OffLabel` | 为 Status 提供已本地化的开、关文案 |
| `Message`、`Color` | 覆盖当前 Status 文案与主题色，例如启动中、等待 Shell 激活、读取失败 |
| `Intent()` | 点击或快捷键激活时返回目标值和是否可操作 |
| `KeyIntent(key, focused)` | 使用 Bubbles key 匹配聚焦控件上的 Enter 和空格，过滤带修饰键或标记为重复的输入 |
| `View(options)` | 绘制含焦点槽的单行。可指定主题表面、焦点、悬停、低色模式、弹窗背景弱化及总字符格宽度 |

`Message` 与操作权限独立。例如，终端代理请求已写入而当前 Shell 尚待激活时，可显示待激活文案，同时允许撤销请求。提交该操作的异步调用仍在执行时，页面设置 `Pending`。

## 页面接入

1. 从业务快照构造 `Switch`。页面提供业务文案、当前权限和等待状态，布局基于 `View` 的实际显示宽度计算命中区域。
2. 导航、弹窗和鼠标命中测试确定输入归属。激活时重新读取当前控件，只将它返回的目标值发送给业务层。
3. 提交前同步设置等待状态。任务完成后按操作编号接收结果。服务或配置写入失败时也重新读取快照，未能取得新观测时显示不可用。底部提示栏独立保存偏好，写入失败时重新读取偏好并报告错误。
4. 新观测驱动下一帧。Slider 在等待期间把标记留在原滑块位置。Status 显示带圆点的状态或业务提供的文字。中英文、组合字和 emoji 的裁切与排列均使用终端字符格宽度。

```go
control := Switch{
	Variant: Status, On: running, Pending: stopping,
	OnLabel: "运行中", OffLabel: "已停止",
}
if stopping {
	control.Message = "停止中…"
}
face := control.View(switchOptions{Surface: panel, Focused: focused})
```

## 验证

```sh
./scripts/project-go test ./internal/tui -run 'Test(Switch|SettingsSwitch|OverviewCoreToggle|KeyHints)' -count=1
python3 scripts/switch-pty.py --keep-artifacts --output .test-artifacts/switch/pty --png
```

PTY 夹具同时展示滑块与状态文字，覆盖键盘、鼠标、失败回读、重复输入、过期结果以及不同终端宽度与颜色模式。[真实终端测试](../scripts/switch-pty.py)

中文和英文的真彩色、256 色和无色 PTY 均验证了设置页在 160×45 和 80×24 下的开关操作。模拟底部提示栏保存失败时，底栏仍然可见；重试成功后才隐藏。独立组件夹具另覆盖 40×16 缩放和恢复。[设置页](../.test-artifacts/switch-variants/settings-focused/20261127-172337-dc625e/zh-Hans/truecolor/settings-switches-160x45.png) · [保存中](../.test-artifacts/switch-variants/settings-focused/20261127-172337-dc625e/zh-Hans/truecolor/preference-save-pending.png) · [40×16 组件](../.test-artifacts/switch-variants/component/20261127-172027-3e9c1d/zh-Hans/truecolor/switch-40x16.png)

专用 Linux VM 上的真实 Mihomo 和 systemd 验证了点击启动、键盘停止、端口冲突后的状态回读和外部启停同步。[概览 80×24](../.test-artifacts/switch-variants/overview/20261127-172027-2a267d/zh-Hans/truecolor/running/overview-80x24.png) · [服务验证记录](../.test-artifacts/switch-variants/linux-service/result.json)
