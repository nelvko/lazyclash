# 连接页面

Connections 参考 Clash Verge、CFW 和 FlClash 的连接检查界面，使用完整正文宽度。固定区域显示筛选后的连接数、全部活动连接数、内核累计下载/上传量、搜索、暂停与排序。累计量直接来自 Mihomo，包含已经结束的连接，不由活动行求和。

正文至少 100 列时复用 Charm Bubbles table，显示目标、协议、代理链、双向速度和时长；至少 125 列增加进程和匹配规则，至少 155 列增加单条累计下载/上传量。更窄时使用双行列表，优先显示目标、协议、速度和进程；完整字段在详情内查看。Bubbles textinput 负责搜索编辑和水平滚动；页面沿用 dashboard 的滚动、键盘焦点和鼠标命中，仅渲染可见连接。

- `/` 搜索：不区分大小写，匹配域名、IP、端口、协议、进程、路径、规则、代理链和连接 ID。`×` 清空。Enter/Esc 完成输入，Tab/Shift+Tab 移动到相邻控件。
- `s` 循环排序：最新、下载速度、上传速度、累计下载、累计上传、时长。除时长从长到短外，其余数值从大到小；同值按连接 ID 稳定排序。排序和刷新保持选中 ID。
- `p` 暂停/继续：固定本页列表、速率、累计量和时长，后台监控仍继续。恢复时立即显示最新观测。内核停止、API 不可用、内核身份改变或累计计数器重置时，丢弃旧快照。
- ↑↓/j/k 选择，Home/End 跳到首尾，PgUp/PgDn 按可见行数翻页。Enter 或点击任一行打开详情，Esc/← 返回并保留筛选与列表位置。
- `x` 审阅关闭单条连接，默认选择取消，`y` 确认。详情和关闭校验始终使用实时状态；暂停期间已经结束的连接不能关闭其他行。没有批量关闭操作。

速率用相同连接连续两次成功观测的累计字节差除以实际观测间隔计算。首次采样、计数器回退、内核切换、连接 ID 重新使用或观测中断时显示 `--`；已知空闲连接显示 `0 B/s`。速率仅供展示，不参与内核累计量计算。

验证入口：

```sh
go test ./internal/tui -run 'TestConnection|TestLiveConnections' -count=1
go test -race ./internal/tui -run 'TestConnection|TestLiveConnections|TestRuntime' -count=1
go test -c ./internal/tui -o bin/connections-tui.test
python3 scripts/connections-pty.py --binary bin/connections-tui.test \
  --output .test-artifacts/connections-redesign/pty \
  --profile truecolor --profile 256 --profile no-color --png
```

PTY 使用显式启用的隔离内存后端，不访问真实 Mihomo 或操作系统服务。PNG 根据真实 PTY 字符格与颜色重绘。
