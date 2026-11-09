# 连接页面

Connections 参考 Clash Verge、CFW 和 FlClash 的连接检查界面，使用完整正文宽度。顶部统计带展示同一次 `/connections` 观测的汇总，下方依次是搜索/暂停/排序、筛选匹配数与运行状态、连接列表。

| 接口响应字段 | 顶部展示 |
| --- | --- |
| `downloadTotal` | 累计下载 |
| `uploadTotal` | 累计上传 |
| `memory` | 内存占用 |
| `connections` | 活动连接数，取数组长度 |

字节数自动换算为 B、KiB、MiB 等易读单位。累计量直接来自 Mihomo，包含已经结束的连接，不由活动行求和，也不随搜索过滤改变。内存同样取连接接口快照，不混用独立 `/memory` 流。统计与列表暂停时一致冻结；不可用时显示 `--`，空连接数组显示 0，仍保留累计量与内存。

统计按正文实际宽度与本地化标签长度自适应为四列、两列或单列，典型终端 160 列为一行、80/60 列为两行、40 列为四行；120 列时中文标签可排一行，英文排两行，避免横向截掉上传量或内存。固定区域至少保留两行列表；高度不足时沿用整个工具栏进入正文滚动的退化方式。页头只保留“关闭全部”，避免重复活动数在暂停时与统计快照不一致。

字段定义参见 [Mihomo API 文档](https://wiki.metacubex.one/api/#connections)。

正文至少 100 列时复用 Charm Bubbles table，显示目标、协议、代理链、双向速度和时长；至少 125 列增加进程和匹配规则，至少 155 列增加单条累计下载/上传量。更窄时使用双行列表，优先显示目标、协议、速度和进程；完整字段在详情内查看。Bubbles textinput 负责搜索编辑和水平滚动；页面沿用 dashboard 的滚动、键盘焦点和鼠标命中，仅渲染可见连接。

- `/` 搜索：不区分大小写，匹配域名、IP、端口、协议、进程、路径、规则、代理链和连接 ID。`×` 清空。Enter/Esc 完成输入，Tab/Shift+Tab 移动到相邻控件。
- `s` 循环排序：最新、下载速度、上传速度、累计下载、累计上传、时长。除时长从长到短外，其余数值从大到小；同值按连接 ID 稳定排序。排序和实时更新保持选中 ID。
- `p` 暂停/继续：固定本页列表、速率、累计量和时长，后台监控仍继续。恢复时立即显示最新观测。内核停止、API 不可用、内核身份改变或累计计数器重置时，丢弃旧快照。
- ↑↓/j/k 选择，Home/End 跳到首尾，PgUp/PgDn 按可见行数翻页。Enter 或点击任一行打开详情，Esc/← 返回并保留筛选与列表位置。
- `x` 审阅关闭单条连接，默认选择取消，`y` 确认。详情和关闭校验始终使用实时状态；暂停期间已经结束的连接不能关闭其他行。

页头“关闭全部”先展示确认，默认取消。确认后一次性关闭 Mihomo 的全部活动连接，包括筛选结果之外、暂停快照之外及确认期间新建立的连接；执行时恢复实时画面并保留搜索条件。无活动连接、数据不可用或操作进行中时不可执行。连接页不再提供手动刷新按钮或 `g` / `Ctrl+R` 刷新快捷键，数据由共享实时会话持续更新。

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
