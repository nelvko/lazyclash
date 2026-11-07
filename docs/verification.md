# 验证入口

## 统一圆角输入框（2026-11-08）

匹配选项间距收紧为一格，`Aa ab .*` 三项共九列；ANSI16 / NO_COLOR 的 `*`（选中）、`›`（焦点）与 `+`（选中且聚焦）共用固定的一格状态槽。定向输入/搜索/窄屏回归、`go vet ./internal/tui` 与 `make build` 通过；真实 controlling PTY 完成中英文 × TrueColor / NO_COLOR 四组、160×45 / 80×24 / 60×20 / 40×16 共十六次尺寸检查，并验证清除、悬停和匹配切换。记录与实际字符格重绘 PNG 位于 `.test-artifacts/search-option-spacing/`；使用隔离内存后端，未执行真实服务操作。

全部输入框改为共享 `input_field.go`，继续复用 Charm Bubbles textinput 的编辑、验证、粘贴、隐藏与补全能力。普通输入和五处搜索最大 60 列，配置来源导入最多 72 列；三行圆角细描边、与所在页面/卡片/弹窗相同内底、统一外部间距，短窗口压缩留白。匹配控件在框内右侧紧凑排列，顺序为文字、`×`、分隔线、`Aa ab .*`；清除是图标组第一项，没有尾部选项时距右边框只留一格，空间不足时选项在下一行居右，清除仍留在框内右侧。选中只用高亮文字及粗体，匹配按键与 `×` 悬停只提亮文字，`ab` 固定下划线；NO_COLOR 保留独立选中和焦点标记。清空只修改当前文本并返回编辑，沿用各表单的校验、补全及计划失效逻辑，不提交或更改匹配选项。配置创建、端口和安装窗口的正文滚动及固定操作区同步适配三行输入，删除确认仍支持 24×7。

所有输入使用真实闪烁竖线光标，编辑区宽度由共享组件随命中区域返回，完整帧才允许硬件光标；中英文水平滚动、居中弹窗、尺寸变化及裁切复用同一坐标。修复 Bubbles 未着色的光标字符继承边框强调色的问题。原本只展开一层命令批次的配置测试辅助函数补充递归展开，防止把未执行操作误判为稳定 ID 回归；端口及配置导航断言覆盖新增清空停靠点。

为保留共享工作区其他会话的概览与流量改动，从 `5eea958` 导出隔离源码，只叠加本次输入框相关文件或代码块后验证。最终隔离源码 `make check build`、输入/搜索/配置创建及端口定向 race、已有专用 `lazyclash-e2e` Linux/arm64 VM 实际全包测试通过；匹配与清除图标的悬停样式和紧凑尾部布局另补宿主与 Linux 搜索控件定向验证及构建。Python 八项回归通过。原始失败日志保留：沙箱拒绝临时本地监听，以及首次完整检查中旧导航断言和极小删除窗口布局的问题均在最终验证前修复。

真实 controlling PTY 覆盖 20 类输入，中英文 × TrueColor / NO_COLOR 共 80 个用例、320 次尺寸检查，尺寸为 160×45、80×24、60×20、40×16。包含中文、长查询、中间编辑、水平滚动、缩放恢复、真实光标协议、密码遮蔽、框内背景、清空和焦点、匹配选项保留；所有后端修改计数为零。光标字符颜色修复后另完成 12 个代表性用例、48 次尺寸检查，最终清除在前的布局另完成全部二十类输入的英文真彩，以及中英文规则/配置来源的真彩与无色代表性复验，共 26 个用例、104 次尺寸检查，所有活动输入完整可见且后端修改次数为零。已目视检查宽屏规则、小窗口配置创建和光标字符颜色。PNG 根据真实终端字符格与样式重绘；PTY 使用隔离内存后端，没有访问真实 Mihomo 或执行安装、systemd、TUN 生命周期操作。

证据位于 `.test-artifacts/input-unification/`：`final-clear-first-check-build.log`、`final-clear-first-race.log`、`final-isolated-linux.log`、`clear-first-tests.log`、`final-clear-first-linux.log`、`python-tests.log`，PTY 为 `final-pty/`、`final-forms-pty/`、`final-glyph-pty/`、`final-hover-pty/`、`final-hover-clean-pty/`、`final-clear-hover-pty/`、`final-clear-first-pty/`。隔离源码及文件归属清单位于 `isolated-source/` 与 `task-files.json`。复验入口：

```sh
go test -c ./internal/tui -o bin/input-fields-tui.test
python3 scripts/input-fields-pty.py --binary "$PWD/bin/input-fields-tui.test" \
  --output .test-artifacts/input-unification/pty --png
```

## 填色搜索区与真实竖线光标（2026-11-08）

代理、连接、规则、日志及规则集合弹窗统一采用三行填色留白搜索区；默认深底、聚焦稍亮，彩色模式去掉外轮廓，低色与无色终端保留同尺寸边界。移除框内 `/`，页脚优先展示搜索快捷键；清空移至输入尾部，仅非空时参与点击与 Tab，激活后继续编辑。规则匹配选项缩为紧凑组，计数不再被提示覆盖。实现细节见 `docs/theme-mariana.md`。

宿主 `make check build` 通过，包含全包 Go 测试（TUI 68.910 秒）、vet 和构建；搜索、固定栏与日志交互定向 race 通过（24.129 秒）。最后的页脚文案顺序调整通过中英文 × 160×45、80×24、60×20、40×16 的四页回归，以及 i18n、搜索和导航定向测试，并再次完成 vet 和构建。`./scripts/verify-linux.sh lazyclash-e2e` 在已有 Debian/arm64 专用 VM 内实际执行全包测试通过；最终搜索/导航及页脚窄屏用例另在同一 VM 复验通过，本轮临时目录清理成功。八项 Python 回归通过。

真实 controlling PTY 中，规则、连接、日志各完成中英文 × True Color、ANSI256、NO_COLOR 六组，共十八组，覆盖 160×45 至 40×16 及恢复，连接另含 200×45。代理页中文 True Color 主流程及独立组/测速夹具、规则集合弹窗中文 True Color 流程通过。实际终端竖线光标另完成五个搜索输入 × 真彩/无色十组，共一百项位置、可见性、中文跨宽字移动、长查询缩放和内嵌清空校验；二十组主流程原始输出均含竖线光标形状指令。模型测试另覆盖中间编辑、遮挡/裁剪时隐藏、极小尺寸恢复、提示文字完整与无色选中标记。已目视中文宽屏、无色窄屏和含搜索提示的规则页。

证据位于忽略目录 `.test-artifacts/search-filled/`：`resume-check-build.log`、`linux.log`、`resume-linux-final-targeted.log`、`final-shortcut-check.log`、`final-shortcut-linux.log`、`race.log`、`python-tests.log`；页面 PTY 在 `pty/`，真实光标探针在 `cursor-probe/`，汇总为 `cursor-probe-final.log` 和 `bar-sequences.json`。PNG 按实际 PTY 字符格重绘，硬件光标的位置与形状由原始协议验证。全部流程使用隔离内存后端，未访问实际 Mihomo、节点或操作系统服务，未重跑其安装/systemd/TUN 生命周期 E2E。

## 概览常态无边框（2026-11-07）

五张普通概览卡片改为常态无外边框，保留独立底色、内边距、卡间空白及代理路径的内部分隔线。始终预留原来的周边字符格；键盘浏览聚焦某一卡片时显式绘制黄色轮廓，进入按钮或返回导航后移除，内容与命中区不移动。无色终端仍显示焦点轮廓。首次安装引导与其他页面沿用原来的边框。焦点轮廓继续避开通知覆盖的字符格。

共享工作区同时存在另一会话未完成的搜索输入改动。本轮从 `4a8b74f` 导出隔离源码，只叠加本次三个产品文件、布局回归与概览 PTY 脚本后验证；这些文件逐字节与本次工作内容一致，SHA256 清单记录于 `.test-artifacts/overview-borderless/source-manifest.json`。隔离快照通过完整 `make check build`（TUI 65.894 秒），包含全包 Go 测试、vet 和构建；`./scripts/verify-linux.sh lazyclash-e2e` 在现有专用 Linux/arm64 VM 实际执行全部可运行测试包通过，脚本完成临时目录清理。八项 Python 回归及概览 PTY 脚本语法检查通过。没有覆盖、回退或纳入另一会话的改动。

真实 controlling PTY 完成中英文 × True Color、ANSI256、NO_COLOR 六组，覆盖 160×45、120×32、80×24、60×20、40×16 及恢复；断言导航时无卡片外框、卡片焦点只有一个完整轮廓且彩色时为黄色、进入按钮后无轮廓残留、内部分隔线保留，并验证 Tab 揭示全部操作及配置/代理入口。各组服务、模式、节点修改均为零。已目视复核中文宽屏常态与 40×16 焦点画面。模型回归另外覆盖部分滚动后的轮廓裁切、卡片/按钮几何不变和通知内容不被焦点轮廓覆盖。

日志为 `.test-artifacts/overview-borderless/{check-build,linux,python}.log`；最终 PTY 证据为 `.test-artifacts/overview-borderless/pty/20261107-210854-df56d4/`，包含 PNG/ANSI/TXT、原始 PTY 输出与操作计数。PNG 从实际终端字符格和颜色重绘，后端为隔离内存 fixture；没有操作真实 Mihomo/systemd/TUN，也未运行其完整生命周期 E2E 或 race。复验命令：

```sh
go test -c ./internal/tui -o bin/overview-borderless-tui.test
python3 scripts/overview-pty.py --binary bin/overview-borderless-tui.test \
  --output .test-artifacts/overview-borderless/pty --png
```

## 共享按钮组件与状态配色（2026-11-07）

所有命令按钮与互斥选择继续由 `internal/tui/button.go` 统一渲染：固定两格 ` ›` 焦点槽，相邻按钮之间不叠加空白，箭头距前一个色块一格、距当前色块零格。新增正文最小字符格宽度，路由与语言组等宽居中，移除勾号占位；ANSI16/NO_COLOR 使用同宽的 `[标签]` 与 `<标签>` 区分选择。按钮专用的普通、主操作、已选和危险悬停色不修改列表的原有颜色；处理中或禁用时保留当前选择。页头、配置表单、引导、端口、TUN、通知及页内按钮同步调整绘制、换行和命中区域，忙碌的规则/日志按钮不再生成可点击目标。代理页的路由选择统一表示保存的目标模式，运行中模式不同或未知时单独说明。

最终宿主 `make check build` 通过（TUI 63.856 秒），含全部 Go 测试、vet 与产品构建；组件、调用、选择组、窄屏规则、终端配色与相关引导定向 race 通过（11.161 秒），八项 Python 回归与 PTY 脚本语法检查通过。回归覆盖十一类真实按钮调用、中英文、颜色能力、等宽与焦点组合、命中区域、不可操作守卫，以及正常/悬停/选中/危险各文字配对在真彩与 256 色下达到 4.5:1。新增 34/40 列规则筛选回归，先复现统一最小宽度挤掉清空按钮，再将六格最小宽度移至路由/语言等调用方，短筛选开关使用自然宽度。首次测试发现的旧勾号断言与状态不完整夹具已修正，失败日志独立保留。

`./scripts/verify-linux.sh lazyclash-e2e` 在已有专用 Linux/arm64 VM 实际执行全部可运行包测试通过。窄屏修复后的最终源码另编译 TUI 测试二进制，在同一 VM 的独立临时目录实际执行共享按钮、选择组、设置及规则回归通过，清理目录成功；没有将交叉编译当作运行验证。

真实 controlling PTY 的专用按钮夹具、概览、设置和代理页各完成中英文 × True Color、ANSI256、NO_COLOR 六组。专用夹具验证全部按钮角色、悬停、焦点与选择分离、处理中/禁用防误触，以及 160×45、120×32、80×24、60×20、40×16 与恢复；概览验证安全进入、Tab 揭示和页面入口，设置覆盖十四种尺寸及语言保存、滚轮与端口弹窗返回。代理另有多组与节点延迟两组，六组主流程各进行三次虚拟节点选择、三次虚拟模式切换和十次虚拟测速。窄屏修复后重建二进制并复验专用按钮六组，以及规则中文 NO_COLOR 常规流程；另在测试进程内将既有规则脚本尺寸改为 34×16 → 40×16 → 160×45，确认四个筛选控件单行可见，滚轮后坐标稳定（脚本固定摘要仍写五尺寸，此补充实测为上述三种）。配置页使用隔离的生产二进制完成英文 NO_COLOR 全流程，覆盖创建、刷新、路径补全、繁忙防重复、重开持久化与删除确认；同步修正了脚本里过期的 Rules 页头名称断言。已目视复核中文真彩概览、选中悬停及无色窄屏等 PNG。

证据位于忽略目录 `.test-artifacts/buttons/`：宿主 `check-build-complete.log`、`race-complete.log`，Linux 全包 `linux-verified.log` 与最终窄屏 `linux-narrow-complete.log`；最终专用按钮 `pty-narrow-final/20261107-000539-668665/`，概览 `overview-pty/20261106-235248-10af52/`，设置 `settings-pty/20261106-235248-67ddef/`，代理 `proxies-pty/`，配置 `profiles-pty/20261106-buttons-final/`，规则 `rules-narrow-final/20261107-000539-b8ff1d/` 与 `rules-34-final/20261107-000714-6eba11/`。PNG 根据实际 PTY 字符格与样式重绘，页面后端为隔离内存 fixture；配置页仅访问 loopback HTTP 并写入临时数据目录。本轮未操作真实 Mihomo/systemd/TUN 服务，也未重跑其完整生命周期 E2E。

```sh
make check build
go test -race ./internal/tui -run 'Test(Button|SelectableButton|ActionButtonGroups|BusyToolbarButtons|NarrowRulesFilters|SelectionGroups|ProxyModeSelection|BusyModeControls|MarianaTrueColor|MarianaTerminal|WizardFooter|WizardCancelling|HeaderButton)' -count=1
python3 -m unittest discover -s scripts -p 'test_*.py'
go test -c ./internal/tui -o bin/buttons-tui.test
# --png requires a Python environment with Pillow and the existing fonts.
python3 scripts/buttons-pty.py --binary bin/buttons-tui.test \
  --output .test-artifacts/buttons/pty --png
```

## 概览信息层次与非对称卡片（2026-11-06）

将原来的七张概览卡片重新组织为五张：内核与服务合并运行控制和版本/PID/期望状态/漂移诊断，代理路径合并模式切换和所选代理组，实时流量突出上传/下载与连接/内存，当前配置突出名称，运行环境集中平台、作用域、自启动、TUN 和系统代理说明。已知版本或模式一致时合并文字，不一致、未知和诊断仍分别保留；缺失指标不展示为零。正文至少 90 列且终端至少 20 行时采用约 3:2 的独立双栏及底部环境通栏，其余尺寸单列；不再补齐卡片高度。卡片语义区域直接由布局返回，替代扫描边框后按顺序分配身份的做法，缩放保留焦点。

最终 `make check build` 通过（TUI 63.834 秒），包含全部 Go 测试、vet 和产品构建；八项 Python 回归及四个变更脚本的语法检查通过。`./scripts/verify-linux.sh lazyclash-e2e` 在现有专用 Linux/arm64 VM 实际运行全部可运行包测试通过，测试临时目录按脚本清理。另构建 `bin/linux-arm64/lazyclash`。模型回归覆盖中英文的五卡片身份、非对称自然尺寸、操作归属与命中区、长名称/诊断、模式和版本的确认/未知语义，以及安全进入卡片与显式管理操作。完整检查发现的旧滚动与通知夹具已改为依据实际布局验证可见操作坐标及焦点边框遮挡，没有放宽原有断言。

真实 controlling PTY 完成中英文 × True Color、ANSI256、NO_COLOR 六组，覆盖 160×45、120×32、80×24、60×20、40×16 及恢复，验证卡片进入不触发操作、Tab 自动揭示全部控件、配置/代理入口鼠标点击及底栏。另在中文 True Color 验证停止、状态不可用、版本与模式不一致三种场景。所有组的服务、模式、节点修改次数均为零，已目视复核最终宽屏与 40×16 无色 PNG。PTY 使用隔离内存后端，PNG 从实际终端字符格与颜色重绘；没有操作真实 Mihomo/systemd/TUN，也未重跑其完整生命周期 E2E。既有引导、主题与服务 PTY 脚本仅同步受影响的概览标题/状态断言，未重跑其完整流程；本轮未运行 race。

最终证据为 `.test-artifacts/overview-check-build-verified.log`、`overview-linux-verified.log`、`overview-python-verified.log`，以及 `.test-artifacts/overview-redesign/pty-final/20261106-222456-8b3868/` 与 `pty-edges-final/20261106-222456-760304/` 中的 PNG/ANSI/TXT、原始 PTY 输出和操作计数。首次沙箱端口限制与旧夹具失败日志保留，不作为最终通过依据。复验入口：

```sh
go test -c ./internal/tui -o bin/overview-tui.test
python3 scripts/overview-pty.py --binary bin/overview-tui.test \
  --output .test-artifacts/overview-redesign/pty --png
python3 scripts/overview-pty.py --binary bin/overview-tui.test \
  --output .test-artifacts/overview-redesign/pty-edges --png \
  --lang zh-Hans --profile truecolor \
  --scenario stopped --scenario unavailable --scenario drift
```

## 日志会话级持续采集与暂停显示（2026-11-06）

保留已验证的 HTTP `/logs?level=info` NDJSON 长连接，将采集生命周期从日志页提升到整个 TUI 会话：初始观测到已安装且运行的内核时自动开始，首次打开日志页也可触发不可用诊断。切页、帮助、表单、普通任务及其取消均不停止采集。一个会话任务拥有读流与 1/2/4/8/16/30 秒退避重试，收到日志后重置退避，安静的长连接不会定时重连；每次重试沿用应用层的安装归属、身份和进程校验。退出或父上下文取消会释放读流、等待命令与计时器；手动重连使旧代事件和错误失效。

`f` / Space 与页头按钮改为暂停/恢复显示，暂停期间单独保留最多 200 条显示快照，实时缓存独立接收并保持最近 200 条，避免淘汰时覆盖被冻结的内容。End 恢复到缓存最新内容，不重连。移除合成 GAP 记录及其筛选例外，断流/恢复只在状态区显示，不计入条数；HTTP 流没有独立握手通知，恢复状态在首条重连事件到达后确认。前台任务期间继续缓存日志，但不从后台日志结果启动竞争性的完整状态读取；对应回归修改前失败，修复后通过。

最终 `make check build` 通过（TUI 63.125 秒），包括全包 Go 测试、vet 与产品构建；日志会话、前后台隔离、安静流身份变化、管理锁与超时等定向 race 通过（TUI 18.340 秒、app 2.505 秒、mihomo 1.260 秒）。`./scripts/verify-linux.sh lazyclash-e2e` 在专用 Debian/arm64 VM 内执行全部可运行包测试通过并正常清理临时目录，最终前后台隔离补丁另在同一 VM 完成日志专项复验。八项 Python 解析回归及修改脚本语法检查通过。原有页面测试夹具补齐初始会话所有权，有限页面请求的同步测试不再执行常驻采集任务。

真实 controlling PTY 完成中英文 × True Color、ANSI256、NO_COLOR 六组，覆盖 160×45、120×32、80×24、60×20、40×16 及恢复。每组验证 Overview 初始采集、跨页/帮助/表单/只读刷新不断流、筛选与长中文布局、暂停显示期间突发 230 条后视图保持、恢复最近 200 条、清空不中断、一次手动重连与一次模拟断流后的自动恢复，以及搜索 GAP 无合成记录。每组 `streams=3`、`canceled=2`、`failures=1`、`burst_events=230`，服务/模式/节点变更均为 `0`。已目视检查中文暂停与自动恢复、40×16 窄屏及英文无色画面。

证据位于忽略目录 `.test-artifacts/logs-session/`，包含 `check-build-status-final.log`、`race-status-final.log`、`linux-final.log`、`linux-log-session-final.log`、前后台隔离修复前后的定向日志，以及 `pty/20261106-171835-e7a06f/verification.json` 和 PNG/ANSI/TXT。初次旧夹具阻塞与旧断言失败记录保留在 `first-pass/`；不作为最终通过依据。PNG 按真实 PTY 字符格与颜色重绘，PTY 使用内存实时事件；本轮没有操作真实 Mihomo 或 systemd 服务，也未重跑其完整生命周期 E2E。既有真实内核日志 PTY 脚本已按持续采集、暂停显示和自动恢复语义更新。复验入口：

```sh
go test -c ./internal/tui -o bin/logs-tui.test
python3 scripts/logs-pty.py --binary bin/logs-tui.test \
  --output .test-artifacts/logs-session/pty \
  --profile truecolor --profile 256 --profile no-color
```

## 缩窄窗口自动收起侧栏（2026-11-06）

修复常驻侧栏折叠后，因为保留 `nav.*` 焦点而自动打开抽屉、遮挡正文的问题。布局沿用共享的 100 列 / 20 行断点：进入紧凑布局时导航焦点转到顶部菜单，正文焦点保持；用户明确打开的抽屉在紧凑尺寸内缩放时保留，恢复宽屏后关闭抽屉并恢复常驻导航焦点。

修改前新增缩放回归在 99×32、80×24、120×19 和 20×7 均复现失败；修改后全部通过，另覆盖重复尺寸消息、普通重绘、正文焦点、明确打开的抽屉和宽屏恢复。宿主 `make check build` 通过（TUI 63.892 秒），侧栏、导航、方向键、缩放和帮助相关定向 race 通过（42.408 秒）。记录位于 `.test-artifacts/sidebar-resize/`。

真实 controlling PTY 完成中英文 × True Color、ANSI256、NO_COLOR 六组，覆盖 160×45 → 99×32 → 80×24 → 60×20、手动打开及关闭抽屉、恢复宽屏，以及 120×20 → 120×19。每组规则查询一次，服务、模式和节点修改均为零。已目视检查中文 80×24 自动收起后的无遮挡规则页及手动打开的菜单；PNG 按实际 PTY 字符格重绘，使用隔离内存后端，没有操作实际 Mihomo 或系统服务。证据及复验说明在 `.test-artifacts/sidebar-resize/pty/`。

最终从 HEAD 与本次修复导出独立源码快照，确认其代码与暂存区一致，再运行 `./scripts/verify-linux.sh lazyclash-e2e`：专用 Linux/arm64 VM 实际执行全部可运行包测试通过，临时目录已清理，日志为 `.test-artifacts/sidebar-resize/linux-isolated.log`。首轮共享树验证混入另一会话尚未完成的日志文案改动，因缺少翻译键失败，原记录保留在 `linux.log`；该会话改动未纳入本次提交。本轮未重跑真实 Mihomo/systemd/TUN 生命周期 E2E。

## 规则集合弹窗（2026-11-06）

参考 Clash Verge 的规则集合窗口，将提供器管理从规则子页面改为覆盖主列表的弹窗。终端至少 110×30 时居中展示，最大 96×34；窄屏使用全屏覆盖，极小尺寸保留关闭入口。标题与更新全部、搜索/状态、底部关闭固定，正文复用 Bubbles viewport 独立滚动。关闭恢复主规则页的筛选、展开与滚动位置；弹窗期间键盘、点击和滚轮不穿透，关闭取消集合请求及更新队列并拒绝迟到响应。

最终 `make check build` 通过（TUI 66.975 秒），八项 Python 脚本回归通过。模型回归覆盖固定头尾、1×1 至 160×45 的画布/命中区裁切、Tab 与更新对象一致、正文限定滚轮、搜索结束后焦点可见、宽窄缩放后的焦点定位，以及主规则页状态恢复。搜索中 q 和数字作为输入，其他状态 Esc/q 关闭。

真实 controlling PTY 完成中英文 × True Color、ANSI256、NO_COLOR 六组，覆盖 160×45、120×32、80×24、60×20、40×16。每组完成单项更新、更新全部的部分失败及独立重试、长列表滚动、背景输入隔离、关闭后恢复主列表与重新打开；服务、模式、节点修改均为零。证据为 `.test-artifacts/provider-modal-pty-final/20261106-163842-600439/`；焦点修复后的最终中文 True Color 流程与 PNG 位于 `.test-artifacts/provider-modal-pty-focus-final/20261106-164044-311f00/`，已目视复核宽屏布局。PNG 按实际 PTY 字符格与颜色重绘，后端使用隔离内存 fixture，没有操作实际 Mihomo 或系统服务。本轮未重跑 race 或真实内核/systemd/TUN 生命周期 E2E。

宿主与脚本日志为 `.test-artifacts/rule-providers-modal-{check-build-final,python}.log`、`.test-artifacts/provider-modal-pty-final.log` 和 `.test-artifacts/provider-modal-pty-focus-final.log`。

最终 `./scripts/verify-linux.sh lazyclash-e2e` 在专用 Debian/arm64 VM 实际执行全部可运行包测试通过，测试临时目录清理检查通过；日志为 `.test-artifacts/rule-providers-modal-linux-final.log`。

## 移除 TUI 自定义规则编辑（2026-11-06）

按当前界面取舍移除自定义规则编辑入口、视图、草稿与保存/应用路由，以及对应 TUI 回归和文案；保留应用层独立覆盖能力、已保存配置和后端诊断。规则集合的 PTY fixture 与脚本改为只验证提供器管理。

Rules、RuleProviders、PageHeaders、KeyHints 和 i18n 定向回归通过（TUI 5.959 秒，i18n 0.174 秒），Python 脚本语法检查通过。真实 controlling PTY 在英文 NO_COLOR 下完成单项更新、更新全部的部分失败与独立重试、筛选，以及 120×32、80×24、60×20、40×16、160×45 五尺寸验证；服务、模式和节点修改均为零。证据为 `.test-artifacts/remove-custom-ui-pty/20261106-162459-0790e2/`，日志为 `.test-artifacts/remove-custom-ui-tests.log` 与 `.test-artifacts/remove-custom-ui-pty.log`。PTY 使用隔离内存后端，没有操作真实 Mihomo 或系统服务。

## Rules 只显示浏览焦点（2026-11-06）

生效规则列表移除常驻蓝色选中竖条及失焦后的选中背景，保留鼠标悬浮与键盘当前焦点的黄色箭头/背景；切页后仍记住浏览位置，进入内容可恢复焦点。中英文操作提示由「选择」改为「浏览」。展开内容移除启用状态及命中/未命中统计，保留完整条件、类型、目标、适用的集合条目数与实际禁用提示。展开与收起共用类型列宽，避免箭头和目标组名向左跳；长类型仍可完整展开。

宿主 `make check build` 通过（TUI 71.025 秒），八项 Python 解析测试通过。调整既有回归，验证切页返回、焦点恢复、展开后失焦、hover、半行裁切和无色显示；规则计数为零仍如实显示，统计变化不关闭当前展开。新增 20/34/60/160 列展开前后列位置回归，以及 12/20/34/60/160 列长类型、中文目标、禁用提示与条目数的换行行高一致性回归。

真实 controlling PTY 完成中英文 × True Color、ANSI256、NO_COLOR 六组，覆盖 160×45、120×32、80×24、60×20、40×16 及恢复。另断言 Enter 展开前后目标组名坐标不变。每组 `queries=4`，服务、模式和节点修改均为零。已目视检查宽屏失焦无标记、40×16 无色布局及长规则展开无统计。证据位于 `.test-artifacts/rules-focus-only/pty-final/20261106-111849-deefa7/`，PTY 日志为同目录上级的 `pty-final.log`；宿主及 Python 日志为 `.test-artifacts/rules-focus-only-{check-build,python}-final.log`。最终中文 True Color PNG 复核证据为 `.test-artifacts/rules-focus-only/pty-preview-final/20261106-131542-a0d24d/`，已目视展开后的列对齐。PNG 根据实际 PTY 字符格和颜色重绘，使用隔离内存后端；本次只调整呈现，未重跑 race 或真实 Mihomo/systemd/TUN 生命周期 E2E。

`./scripts/verify-linux.sh lazyclash-e2e` 在专用 Debian/arm64 VM 内执行全部可运行包测试通过；临时目录清理检查通过。最终日志为 `.test-artifacts/rules-focus-only-linux-final.log`。

## 概览卡片间距恢复（2026-11-06）

根据视觉反馈，恢复概览各组上下卡片之间的一行空白，包括窄屏单列时内核信息与环境卡片之间的间隔。`overviewContent` 与最初 `387f39f` 的实现逐行一致；共享分隔线和页头到正文的间距、状态精简、规则摘要均保持不变。顶部背景已在独立提交 `c7640b4` 恢复。

宿主概览、页头、画布边界及导航相关现有测试通过（4.098 秒），`make build` 通过。已目视检查 160×45 的概览模型字体预览；预览位于 `.test-artifacts/tui-overview-spacing/preview/`，宿主测试、构建与预览日志使用 `.test-artifacts/tui-overview-spacing-{tests,build,preview}.log`。模型预览不代表真实内核或系统服务运行。

`./scripts/verify-linux.sh lazyclash-e2e` 在专用 Debian/arm64 VM 中执行全部可运行测试包通过，临时目录清理检查通过，日志为 `.test-artifacts/tui-overview-spacing-linux.log`。本次未运行真实 Mihomo/systemd/TUN 生命周期 E2E。

## 顶栏恢复正文底色（2026-11-06）

根据视觉反馈，右侧标题区、摘要和操作按钮周围恢复原来的正文 `bg`（`#303841`），窄窗口的 Menu 周围与 Logo 背景同步恢复；常驻侧栏保持独立深色背景。全宽分隔线、正文及卡片间距、概览状态精简和规则摘要均保持上一轮结果。同步移除仅为深色页头加入的按钮底色特例。

宿主页头、按钮、Mariana、概览、安装引导和侧栏相关测试通过（12.802 秒），`make build` 通过。与上一轮 48 份模型视图逐一比较，去除颜色编码后的文本和字符格布局完全一致；已目视检查 160×45 和 80×24 概览的模型字体预览。预览与日志分别位于 `.test-artifacts/tui-header-background/preview/` 和 `.test-artifacts/tui-header-background-{tests,build,preview}.log`。模型预览不代表真实内核或系统服务运行。

`./scripts/verify-linux.sh lazyclash-e2e` 在专用 Debian/arm64 VM 中执行全部可运行测试包通过，临时目录清理检查通过，日志为 `.test-artifacts/tui-header-background-linux.log`。本次未运行真实 Mihomo/systemd/TUN 生命周期 E2E。

## TUI 顶栏、概览与规则摘要（2026-11-06）

共享标题栏统一使用侧栏 `sidebarBackground`，标题文字和操作按钮保留原位置及状态样式。分隔线从侧栏右边界铺至终端右边界，无侧栏时铺满全宽；正文紧接横线下一行，统一释放原来的一行留白。概览上下卡片之间各移除一行额外空白，卡片内部和横向间距不变。实际内核状态保留在操作卡，内核信息不再重复状态，版本、PID、期望状态与漂移诊断保持完整；没有操作卡的安装引导仍保留页头状态。

规则数量只在搜索区显示：默认「670 条规则 · 按匹配顺序」，输入搜索或开启匹配选项后显示「显示 32 / 670 条 · 按匹配顺序」。无效正则、加载失败、原有选项提示及所有操作入口保留，没有修改业务逻辑、快捷键或动作守卫。

真实 controlling PTY 的 Rules 中英文 × True Color、ANSI256、NO_COLOR 六组通过，覆盖 160×45、120×32、80×24、60×20、40×16 及恢复；每组 `queries=4`，服务、模式和节点修改均为零。另用中文 True Color、670 条规则检查七页 × 五种尺寸共 35 组，逐格核对标题底色与横线边界，默认数量和搜索后 `1 / 670` 均仅一处；该探针 `queries=2`，其他修改计数为零。概览运行、停止、未安装状态均已检查；后两种状态复用隔离安装内存 fixture，在 160×45、80×24 完成成功/取消四组，`starts=0`。PNG 由实际 PTY 字符格和颜色重绘，已目视检查宽窄屏概览、默认规则及搜索结果；未操作真实 Mihomo 或系统服务。

首轮全包检查暴露三处测试假设：代理网格 fixture 伪造就绪后残留 Loading 消息，被既有通知同步机制转换为浮层；短日志窗口的固定栏因新增一行而提前可用；侧栏样式断言依赖了多余的结尾 ANSI reset。分别清理 fixture 消息、将 fallback 尺寸从 12 行调整到 11 行、移除无关的 reset 匹配，保留完整卡片可见、输入与选项可点击及前景/背景/粗体断言。没有为这些问题修改产品逻辑。

最终源码通过 `make check build`（TUI 74.224 秒），包含全部 Go 测试、vet 与产品构建；八项 Python 终端解析测试通过。`./scripts/verify-linux.sh lazyclash-e2e` 在已有专用 Debian/arm64 VM 执行全部可运行测试包通过，临时目录清理检查通过；未在 Linux/amd64 运行。最终宿主与 Linux 日志分别为 `.test-artifacts/tui-layout-check-build-final.log`、`.test-artifacts/tui-layout-linux-final.log`。此次只调整绘制和摘要，未重跑 race 或真实 Mihomo/systemd/TUN 生命周期 E2E。

证据位于忽略目录 `.test-artifacts/tui-layout/`：`layout-probe/` 保存七页边界与 670 条规则的截图、ANSI、文字和 `checks.json`；`pty/20261106-100008-c264bc/` 保存六组 Rules 流程；`onboarding/20261106-100132-2d997d/` 保存停止及未安装状态；`before/`、`shared/`、`final/` 是字体渲染的模型预览。首轮宿主和 Linux 失败记录分别保留在 `.test-artifacts/tui-layout-check-build.log`、`.test-artifacts/tui-layout-linux-initial.log`。

实现涉及 `internal/tui/{dashboard,page_header,button,overview_controls,rules_view}.go` 和 `internal/i18n/locales/{en,zh-Hans}.pages.json`。同步调整已有测试 `internal/tui/{dashboard,page_header,button,inline_install,notification_view,rules,proxy_hover,search_field,theme}_test.go`，以及 `docs/{theme-mariana,rules,verification}.md`。

复验入口：

```sh
make check build
python3 -m unittest discover -s scripts -p 'test_*.py'
./scripts/verify-linux.sh lazyclash-e2e
go test -c ./internal/tui -o bin/tui-layout-pty.test
python3 scripts/rules-pty.py --binary bin/tui-layout-pty.test \
  --output .test-artifacts/tui-layout/recheck --png
```

## Rules 集合管理与独立自定义规则（2026-11-06）

主规则列表取消独立详情页，Enter 原位展开完整条件与可选统计；取消常驻刷新按钮，保留 Ctrl+R / g 和失败重试。页头新增规则集合与自定义规则入口。集合支持搜索、元数据、单项更新、最多三个并发的更新全部，以及逐项失败重试；Inline 不参与更新。当前 Profile 的自定义规则独立保存于 `prepend-rules`，支持增改删排序；保存不应用，应用单独确认，运行中应用会重启服务并中断连接。

模型及应用层回归覆盖：提供器重复调度、部分失败、离页取消、迟到响应、旧计时器及重入身份；自定义规则保存与应用分离、订阅更新保留、完整配方冲突、Profile 绑定、校验失败和回滚、未提交输入丢弃确认、跨页草稿、退出时等待保存/应用与其他持久化完成。生效列表仍只绘制当前可见行，保留滚动响应与全高选择标记。

实际 controlling PTY 使用隔离内存后端完成中英文 × True Color、ANSI256、NO_COLOR 六组，验证增改删排序、长中文、跨页、保存与应用、默认取消、集合单更、批量部分失败及独立重试，以及 160×45、120×32、80×24、60×20、40×16。每组 `loads=4`、`saves=1`、`applies=1`，集合更新尝试四次，成功三次、一次模拟 File 来源失败；无 Inline 更新，服务、模式和节点修改计数均为零。已目视检查宽屏部分失败和窄屏输入/确认。PNG 按实际 PTY 字符格和颜色重绘，不是原生终端截图。

最终源码通过 `make check build`（TUI 73.453 秒）、上述 Rules 管理生命周期定向 race（28.374 秒）和八项 Python 解析测试。管理页六组终端证据在 `.test-artifacts/rule-management/final-pty/20261106-093542-c44e15/`；补充退出边界后，最终源码中文 True Color 复验也通过，位于 `final-source-pty/20261106-094254-972d88/`。宿主、race、Python 和 Linux 日志使用 `.test-artifacts/rules-management-*.log`。PTY 只验证终端交互，持久化及事务由应用层文件/HTTP fixture 验证；本轮未向用户实际提供器发起更新，也未重跑真实 Mihomo/systemd/TUN 生命周期 E2E。

Linux 首次并行运行及随后单独运行均触及原脚本的三分钟整包总时限，仍在执行既有布局测试，无断言失败；原始日志分别保留在 `rules-management-linux-initial.log` 和 `rules-management-linux-3m-serial.log`。验证脚本的整包时限调整为五分钟，测试断言及各项操作超时保持不变。最终 `./scripts/verify-linux.sh lazyclash-e2e` 在已有 Linux/arm64 VM 中执行全部可运行测试包通过，临时目录清理检查通过；未在 Linux/amd64 运行。

复验入口：

```sh
make check build
go test -race ./internal/tui -run 'TestCustomRules|TestRulesProviders|TestRulesInline|TestRulesSelection' -count=1
python3 -m unittest discover -s scripts -p 'test_*.py'
./scripts/verify-linux.sh lazyclash-e2e
go test -c ./internal/tui -o bin/rules-management-tui.test
python3 scripts/rules-management-pty.py --binary bin/rules-management-tui.test \
  --output .test-artifacts/rule-management/recheck \
  --profile truecolor --profile 256 --profile no-color
```

## Rules 切页后选中标记（2026-11-05）

修复规则在切页返回、焦点仍属于侧栏时只在第一行显示蓝条的问题。选中标记现在覆盖规则的两行内容；恢复规则焦点后只在首行显示黄色箭头，次行留白。Hover 背景与选择标记独立，分隔线不带竖条，视口边缘按实际可见行裁切，无色终端仍能识别选择。

最终 `make check build` 通过（TUI 34.172 秒），包含全包测试、vet 和产品构建；`./scripts/verify-linux.sh lazyclash-e2e` 在已有 Debian/arm64 VM 内执行全部可运行包测试通过。Rules 定向测试通过，新增回归覆盖实际切页返回及焦点恢复、选中/悬浮叠加、160×45、80×24、60×20、40×9 边缘裁切、ASCII 与显式 NO_COLOR。此次只调整绘制，未重跑 race。

真实 controlling PTY 在中英文 × True Color、ANSI256、NO_COLOR 六组全部通过，增加鼠标从 Rules 切至 Connections 再返回时的双行标记与 Enter 恢复焦点检查，并保留筛选、详情及五种尺寸的完整流程。每组 `queries=4`，服务、模式和节点变更均为零。首次中文探针的宽泛文字匹配误点了连接页搜索提示，现将导航点击限制在侧栏区域；原失败记录保留在 `pty/`，最终证据为 `.test-artifacts/rules-selection/pty-final/20261105-233619-ca6139/`，日志为同目录上级的 `check-build.log`、`linux.log`、`pty-final.log`。已目视检查中文切页返回后的完整竖条。PNG 由实际 PTY 字符格与颜色重绘，使用内存后端，未操作实际 Mihomo 或系统服务。

## 四页统一搜索框（2026-11-05）

代理、连接、规则和日志页复用 Bubbles textinput 与三行圆角框；文字左右留白，筛选按钮与文字居中对齐，窄屏移至框下。上下边框均可点击聚焦，悬停、聚焦和正则错误使用不同语义色。固定栏高度、长输入水平滚动和通知避让同步调整，日志极短窗口裁剪后仍保留可见控件命中区。设计参考见 `docs/theme-mariana.md`。

最终 `make check build` 通过（TUI 34.119 秒），含全包测试、vet 和产品构建；`./scripts/verify-linux.sh lazyclash-e2e` 在已有 Debian/arm64 专用 VM 内实际执行全部可运行 Go 测试通过。八项 Python 终端解析回归通过。新增模型回归覆盖四页边框点击、中英文长 Unicode 输入从 160×45 缩至 5×2 再恢复、固定栏滚动和宽窄屏按钮对齐，以及 40×12、60×12 日志搜索框和选项点击。

真实 controlling PTY 使用隔离内存后端：连接、规则和日志各完成中英文 × True Color、ANSI256、NO_COLOR 六组，共十八组；覆盖 160×45、120×32、80×24、60×20、40×16 及恢复，连接另含 200×45。各页原有搜索、清空、筛选、详情返回、暂停和滚动流程通过；代理页中文 True Color 完整 PTY 流程及独立组/延迟 fixture 通过。已目视中文宽屏搜索框及无色窄屏，PNG 按实际 PTY 字符格与颜色重绘。脚本同步移除“搜索文字与正文同一 X 坐标”的旧布局假设，详情返回后等待完整重绘再检查。

证据位于忽略目录 `.test-artifacts/search-redesign/`：`final-check-build.log`、`final-linux.log`、`final-connections-pty/20261105-221216-e7924c/`、`final-rules-pty/20261105-221124-cc69f1/`、`final-logs-pty/20261105-221124-0d9636/` 和 `proxies-pty/`。本轮为布局改动，未重跑 race 或真实 Mihomo/systemd/TUN 生命周期 E2E；PTY 未访问实际内核、节点或操作系统服务。

## Connections 表格与实时检查（2026-11-05）

连接页参考 Clash Verge、CFW 和 FlClash，使用完整正文宽度，固定统计、搜索、暂停及排序栏。正文至少 100 列时复用 Bubbles table，较宽时增加进程、规则及累计流量列；窄屏回退到双行列表。支持六种稳定排序、实时双向速率、时长、代理链、详情，以及默认取消的单条关闭确认。暂停只冻结页面观测，实时监控继续运行；API 失效、内核身份改变或计数器回退会丢弃旧快照。详见 `docs/connections.md`。

最终工作区 `make check build` 通过（TUI 34.310 秒），连接页、运行监控和详情返回相关 race 通过（29.346 秒），八项 Python 终端解析回归通过；独立导出的连接页源码也已验证，未依赖其他会话的未完成草稿。`./scripts/verify-linux.sh lazyclash-e2e` 在已有 Debian/arm64 VM 中实际运行全部可运行测试包通过；普通用户 uid 501、systemd 用户 manager running、Linger=yes。新增回归覆盖速率来源时间戳、首次/空闲采样、计数器与内核重置、快照隔离、关闭目标消失、排序身份、搜索 Tab、Home/End/翻页，以及详情期间插入连接后返回时自动揭示原选中项。

真实 controlling PTY 使用隔离内存后端完成中英文 × True Color、ANSI256、NO_COLOR 六组，每组验证 200×45、160×45、120×32、80×24、60×20、40×16 并回到宽屏。覆盖目标/进程/代理链筛选、六种实际排序、暂停期间至少两次后台采样、固定工具栏滚动、第二行点击、IPv6 详情、无匹配恢复和准确关闭指定 ID。每组仅关闭 `pty-connection-00` 一次，剩余 47 条；服务、模式及节点修改均为 0。已目视检查英文 200×45、中文宽屏与 40×16、英文无色 80×24，最终列宽让常见 RuleSet 载荷及 Duration 表头完整显示。初次详情返回回归使用了仅两条连接却设置八行滚动的无效 fixture，现改为真实可滚动列表并保留原位置断言；另有回归覆盖数据插入后自动揭示原选中 ID。

证据在忽略目录 `.test-artifacts/connections-redesign/`：`final-check.log`、`final-race.log`、`final-linux.log` 和 `final-pty/20261105-214757-7e226f/`。PNG 根据真实 PTY 字符格与颜色重绘；本轮未访问实际 Mihomo 或操作系统服务，也未重跑 Mihomo/systemd/TUN 生命周期 E2E。复验命令见 `docs/connections.md`。


## Logs 固定工具栏与连续采集阅读（2026-11-05）

日志页参考 CFW、Clash Verge、lnav 和 K9s，采用固定搜索、全部/Info/Warn/Error 本地筛选与简洁/详细切换。默认详细布局将时间和级别置于首行，正文换行并用细线分隔；简洁布局每条一行。页头提供清空、暂停/恢复和重连，底部显示采集/阅读状态、期间收到的新事件数与回到最新入口。复用 Bubbles textinput 与 viewport，工具栏随窄屏折行。

滚动只停止跟随，搜索和级别筛选继续使用同一采集请求；按日志身份与条目内行号锚定阅读位置，窗口缩放、视图切换或新事件不跳到其他记录，锚点被 200 条上限淘汰后显示最早保留记录。显式暂停才断开请求，恢复产生的采集间隙不被筛选隐藏。清空只清内存记录，不重连，也不修改筛选设置或内核日志级别。健康空闲、无匹配、暂停与连接失败分别显示。

本次独立暂存源码（排除其他会话正在实现的页面改动）通过 `make check build`，含全部 Go 测试、vet 与产品构建（TUI 33.034 秒）；日志生命周期、采集间隙与页面离开等定向 race 通过（15.327 秒）。`./scripts/verify-linux.sh lazyclash-e2e` 在 Debian/arm64 VM 内执行全部可运行包测试通过，脚本正常清理本轮临时目录。八项 Python 终端解析回归通过；既有真实内核日志 PTY 流程同步移除已移至固定状态栏的重复标题断言。

`scripts/logs-pty.py` 使用真实 controlling PTY 与内存实时事件完成中英文 × True Color、ANSI256、NO_COLOR 六组，每组覆盖 160×45、120×32、80×24、60×20、40×16 及恢复。验证长中文完整换行、简洁截断、级别筛选、大小写搜索、数字与 q 输入隔离、固定栏滚轮、持续流中的锚定、暂停/End 恢复与 Error 筛选下的间隙提示，以及清空不中断采集、主动重连。每组 `streams=3`、`canceled=3`，服务/模式/节点变更均为 `0`。模型测试另覆盖记录淘汰、5×2 极小尺寸恢复、详细锚点重排、Tab 顺序及迟到结果隔离。已目视检查中文宽窄屏和英文无色布局。

证据位于忽略目录 `.test-artifacts/logs-redesign/`，包含 `staged-check-build.log`、`staged-race.log`、`staged-linux.log`、`python-tests.log` 及 `pty/20261105-213219-6ca703/`。PNG 按实际 PTY 字符格与颜色重绘；本轮没有连接真实 Mihomo、操作系统服务或重跑 Mihomo/systemd/TUN 生命周期 E2E。复验入口：

```sh
go test -c ./internal/tui -o bin/logs-tui.test
python3 scripts/logs-pty.py --binary bin/logs-tui.test \
  --output .test-artifacts/logs-redesign/pty \
  --profile truecolor --profile 256 --profile no-color
```

## Rules 鼠标响应与列表点击（2026-11-05）

规则列表改为只样式化当前可见的行，逻辑总行数与命中区域仍覆盖完整列表；焦点揭示按行位置计算。普通鼠标移动与滚轮跳过数据协调和焦点修复，同一命中区域移动及滚动边界不重绘；滚轮后按事件中的指针坐标重新命中。保留侧栏、菜单、通知和弹窗的事件所有权，通知避让使用逻辑总高度并保护固定搜索框。没有加入应用侧滚动动画、惯性或丢弃输入。

单击规则的主行或次行只选中并定位规则，留在列表；Enter 可主动查看完整长条件与可选统计，返回后恢复筛选、选择和滚动位置。

Apple M4 Pro 本机无通知正常页面的 `Update + View` 量级诊断：160×45、670 条规则的 hover/滚轮从约 63–64ms 降到约 3.7ms，2000 条从约 170–172ms 降到约 3.7ms；200×60 优化后约 5.1ms，同一命中区域约 0.6–0.9ms。基线每项三次、优化后每项二十次，测量不包含 SSH 或真实触摸板。真实 controlling PTY 的 25 条 motion 事件批次，670/2000 条规则由 831/2827ms 降到约 73/73ms；12 条下滚由 403/1253ms 降到 72/86ms，上滚由 400/1448ms 降到 75/78ms。最后 hover、滚动后重新命中、完整可见序号范围以及停止输入后 1.2 秒稳定均通过；终端只提供离散滚轮消息，不能据此声称控制了终端自身的触摸板惯性。

最终 Rules 独立源码快照 `make check build` 通过（TUI 32.362 秒），包含全部 Go 测试、vet 和产品构建；Rules、PageHeader、PageReads、Notification 定向 race 通过（53.285 秒），八项 Python 终端解析回归通过。`./scripts/verify-linux.sh lazyclash-e2e` 在已有 Debian/arm64 VM 内执行全包测试，通过。新增模型测试覆盖七种状态、八种终端尺寸及七个偏移的窗口/完整渲染逐屏等价、半行裁切、筛选与稀疏索引、670/2000 条 hover/连续轮滚/边界、弹层所有权、通知输入避让及点击保留筛选与视口。

最终主流程真实 PTY 在中英文 × True Color、ANSI256、NO_COLOR 六组全部通过，验证 160×45、120×32、80×24、60×20、40×16 再返回宽屏，包含主行/次行单击留在列表、Enter 详情、筛选和返回；每组 `queries=3`，服务、模式与节点变更均为零。证据位于 `.test-artifacts/rules-scroll/`，其中 `before/` 与 `after/` 保留性能对照，`final-main/` 与 `final-scroll/` 保存最终交互，`final-check.log`、`final-linux.log`、`final-race.log` 为最终验证记录；基准与 CPU 诊断位于 `.test-artifacts/rules-performance-review/`。

验证快照只包含本轮 Rules 改动，不包含同时进行的连接、日志和设置页重构；后者的未提交内容及暂存状态保留原状。PTY 使用内存后端，PNG 为实际字符格与颜色重绘，本轮未操作实际 Mihomo/systemd/TUN 服务。滚动探针复验：

```sh
go test -c ./internal/tui -o bin/rules-tui.test
python3 scripts/rules-pty.py --binary bin/rules-tui.test \
  --output .test-artifacts/rules-scroll/recheck --scenario scroll \
  --count 670 --count 2000 --require-wheel-hover
```

## Rules 双行列表与固定筛选栏（2026-11-05）

规则列表参考 Clash Verge，改为独立对齐的序号、匹配条件主行、类型与目标次行及细分隔线，使用完整可用正文宽度。固定筛选栏复用 Bubbles textinput；窄窗口将选项拆至下一行，极短窗口回退到正文内。支持大小写、Unicode 整词、正则组合筛选及清空，摘要显示匹配数；原始匹配顺序、只读详情、空 MATCH 数据、禁用观察、异步取消和迟到结果隔离保留。

最终源码 `make check build` 通过（TUI 34.037 秒），包含全部 Go 测试、vet 和产品构建；`go test -race ./internal/tui -run 'TestRules|TestPageHeader|TestPageReads' -count=1` 通过（66.853 秒），八项 Python 终端解析回归通过。`./scripts/verify-linux.sh lazyclash-e2e` 在已有 Debian/arm64 VM 内执行全部可运行包测试，通过并确认暂存目录已清理。新增模型回归覆盖 Unicode 边界、整词正则分支 `a|ab`、无效正则恢复、字段独立匹配、固定栏 Tab 顺序与滚动保持、双行点击、空 MATCH 与禁用状态，以及从 160×45 到 5×2 再恢复时的搜索、选择和详情返回。

`scripts/rules-pty.py` 使用真实 controlling PTY 完成中英文 × True Color、ANSI256、NO_COLOR 六组，每组均验证 160×45、120×32、80×24、60×20、40×16 并返回宽屏；覆盖第二行点击、各匹配选项、正则错误恢复、无匹配与清空、详情返回、长禁用规则详情和固定栏滚轮。每组 `queries=3`，服务、模式与节点变更均为 `0`。已目视检查中文宽屏、40×16 及英文无色布局。

证据位于忽略目录 `.test-artifacts/rules-redesign/`，Linux 运行日志为 `linux-tests.log`，最终 PTY 记录为 `pty/20261105-203324-c22e55/`。PNG 按实际 PTY 字符格与颜色重绘；PTY 使用隔离内存后端，未访问实际 Mihomo 或操作系统服务，本轮未重跑 Mihomo/systemd/TUN 生命周期 E2E。复验入口：

```sh
go test -c ./internal/tui -o bin/rules-tui.test
python3 scripts/rules-pty.py --binary bin/rules-tui.test \
  --output .test-artifacts/rules-redesign/pty \
  --profile truecolor --profile 256 --profile no-color
```

## Proxies 延迟交互去下划线（2026-11-05）

延迟数值与状态统一移除下划线，悬浮或聚焦时只改变固定延迟区域的背景。复用较深的 `inputBackground`，保留成功、失败等原有文字颜色；分隔空白、卡片背景、选中竖条和外侧 padding 不变。测试中与等待中不显示局部背景强调，重复点击继续复用请求。

最终工作区 `make check build` 通过（TUI 29.306 秒），Linux/arm64 VM 内全部可运行包测试通过并确认临时目录已清理。独立导出的本次暂存源码通过延迟重测、交互样式与 Hover 定向测试（1.608 秒），确认不依赖其他会话尚未提交的网格与间距改动。样式回归覆盖中英文、六种延迟状态、普通/选中卡片及悬浮/聚焦，验证状态色、居中、命中区域和留白不变。

中文 True Color、160×45 实际 PTY 探针通过：逐格检查数值与失败/超时状态无下划线、局部背景范围、Testing/Queued 无局部强调、选中引用卡片和 DIRECT/REJECT 的行为，最终 `tests=11`、`canceled=0`、`max_tests=4`、`selections=0`、`modes=0`。已目视检查数值悬浮与测试中画面。证据在 `.test-artifacts/proxies-delay-style/`；PNG 按实际 PTY 字符格和颜色重绘，使用内存后端，未访问远端节点或操作系统服务。本轮为样式修改，未重跑 race 或全部语言/颜色组合。

## Proxies 卡片 A/B 对比与 Timeout 样式（2026-11-05）

正式布局继续为三行，P1 + P2 与自适应列数不变。两行、四行实验只通过 `.test-artifacts/proxies-card-ab/diagnostic/` 内的独立 Go overlay 替换节点垂直行数组和动作行坐标，没有加入产品设置或修改默认卡片高度。三种方案均用相同四组、Auto URLTest 展开、47 节点、Mariana、英文界面及中英文节点名、相同 Selected/Focus/Hover、scroll=0，在 200×40、160×40、120×32、80×24 生成最终 Model.View 的完整 ANSI/TXT 与字符格 PNG。80 列沿用两级成员页，节点为两列。

三行／两行／四行的 47 节点网格实测高度：200 列为 47/35/59 行，160 列为 63/47/79 行，120 与 80 列为 95/71/119 行。200×40 的完整可见节点分别为 24/32/20（包含局部卡片为 28/36/20），完整显示最后节点需滚动 22/10/34 行；同一尺寸实际正文视口为 33 行。全部测量见 `metrics.csv`，明确区分完整／局部可见及最后节点／页面末端滚动。网格高度与 `ceil(47/列数)×(卡片高度+1)−1` 逐项核对一致。

三套独立渲染与交互测试均通过：逐格唯一命中、引用／延迟动作独立、底行及底部 padding 点击、自动组详情与返回、上下左右、PgDn/PgUp、双向滚轮、四尺寸反复 Resize 的节点身份及完整揭示。ANSI 逐格检查 Hover/Selected 背景与选中竖条全高、黄色焦点及状态叠加；组头输出、网格起点和列宽在三种方案间一致。PNG 来自真实页面 ANSI 重绘，不是原生终端截图，未为实验运行远端节点或系统服务。

唯一正式样式修改为 Timeout 在延迟 Hover/Focus 时不加下划线。原因为 `proxyNode` 对所有可测试延迟统一调用 `Underline(true)`，并非超时判定或终端超链接。中英文、正常／Hover／Focus、窄裁切的回归通过，Timeout 保留原文字和红色，成功／Failed 及其他按钮原有交互修饰继续保留。最终 `make check build` 通过（TUI 27.938 秒），Linux/arm64 VM 全部可运行包测试通过；本轮未重跑 race。独立样式补丁、12 张渲染、尺寸切换画廊、测量报告和复现入口均在 `.test-artifacts/proxies-card-ab/`。早期测试和 CJK 字符偏移解析适配记录保留于 `first-pass/`。

## Proxies 节点网格自适应（2026-11-05）

节点网格从 dashboard 已扣除侧栏和外侧留白的正文宽度，再减去原有网格缩进（宽屏 2 列、窄屏 0 列），按最小卡片宽度 36 列、列间隔 1 列计算一至四列。取消窄屏成员页强制单列：96×40、80×24 为两列，60×20、40×16 为单列，160 列三列，200/240 列四列。卡片宽度等分，尾排沿用同一槽位；组头、卡片高度/Padding 和上一阶段 P1 + P2 均保持。没有新增配置字段或依赖。

最终 `make check build` 通过（TUI 27.902 秒），`./scripts/verify-linux.sh lazyclash-e2e` 在已有 Debian/arm64 VM 中执行全部可运行包测试通过，暂存目录清理确认通过。八项 Python 终端解析回归通过。模型测试覆盖列数临界宽度、整除余量、少量及过滤成员的槽位、中英文/全角/Emoji/超长名称、不同列数下的命中区域；48 节点在九个尺寸之间反复发送 WindowSizeMsg，验证节点身份、当前选择、焦点完整可见、滚动边界以及缩放后的上下左右导航。原有默认排序返回测试补充成员以维持实际需要滚动的前提，保留原有焦点与滚动断言。

中文 True Color 实际 PTY 主流程、多组及单节点测速场景全部通过。缩放序列新增 96×40，逐帧检查 96/80 列两列与 60/40 列单列，并保留 160 列三列、200/240 列四列、末排/短组宽度、选中/延迟、引用入口和连续滚动。已目视检查 80×24 两列效果。主流程请求仍为 `selections=3`、`modes=3`、`tests=10`、`canceled=4`、`max_tests=4`。本轮没有改动异步状态，未重跑 race 或完整六种语言/颜色组合。PTY 使用内存后端，PNG 按实际字符格和颜色重绘，不代表真实远端测速。

证据位于忽略目录 `.test-artifacts/proxies-adaptive-grid/`。初次测试中旧四成员排序 fixture 在两列后已不再需要滚动的失败记录保留于 `first-pass/`；修正 fixture 后最终全包、Linux 与 PTY 均通过。

## Proxies 组间距 P1 + P2（2026-11-05）

终端至少 100 列、24 行时，移除相邻代理组之间及展开组头到节点网格之间的额外空行。组头仍为上下各一行留白的三行容器；窄屏或低于 24 行时保留两处原有的一行间隔。节点卡片、网格排间隔及其他操作不变。

以前次审计保存的最终 View 为基线，对相同的 46 组尺寸、语言、展开和底栏条件重新渲染。200×40、四组按 Others / Auto / Domestic / Proxies 排列、Proxies 含八个成员时：全部收起的组列表由 15 行降为 12 行，含一行工具栏及其后一行间隔的正文由 17 行降为 14 行；Proxies 展开时正文由 25 行降为 21 行。每个独立组头、节点网格、节点卡片及其 Hover/Selected 的 ANSI 输出和局部命中区域均与基线一致；所有窄屏、低高度正文及命中区域也与基线一致。对照 PNG 来自最终 View 的字符格和颜色重绘，不是原生终端截图。

`make check build` 通过（TUI 26.205 秒），相关间距、标题 padding 点击、Hover 和滚轮定向测试通过。`./scripts/verify-linux.sh lazyclash-e2e` 在已有 Debian/arm64 VM 内的全部可运行包测试通过，测试暂存目录已清理。中文 True Color 实际 PTY 主流程、多组和单节点重测场景全部通过，覆盖完整组头高亮、点击/键盘选择、展开折叠、连续滚动、引用入口及各尺寸缩放。主流程仍为 `selections=3`、`modes=3`、`tests=10`、`canceled=4`、`max_tests=4`。本轮为纯布局调整，未重跑 race 或六种语言/颜色组合，PTY 使用内存后端。

证据位于忽略目录 `.test-artifacts/proxies-group-spacing-p1p2/`，包含渲染前后 JSON/PNG/逐行文本、逐场景比较、宿主与 Linux 测试和实际 PTY 日志。临时诊断源码及 overlay 已删除。

## Proxies 单节点重测与延迟居中（2026-11-05）

点击节点右侧延迟数值或状态，只为该节点重新测速并更新结果，不改变选择、展开组或排序。延迟使用固定宽度区域居中，悬浮或聚焦时显示下划线；DIRECT、REJECT 等不适用区域不触发测速或选择。重复点击复用等待中或进行中的请求，也可复用本组测速工作；单节点队列最多同时运行四项，与一组测速并行。开始组测速只取消该组的单节点工作。来源、模式、成员或引用路径变化会使旧请求失效，旧结果与旧路径的等待/测试状态均不能遮盖新结果。

本轮实际通过：

- 最终源码宿主 macOS/arm64 `make check build`，含全包 Go 测试、vet 和产品构建（TUI 25.355 秒）；`TestProxies`、`TestProxy` 与 `TestPageNavigation` 定向 race 通过（53.153 秒）。整个 `^TestProxies` 模型回归通过，覆盖独立点击、卡片完整字符格命中、固定区域的中英文居中、禁用行为、焦点身份恢复、去重、队列并发、组测速协作与取消、失效后补队列，以及旧引用路径的结果和标签隔离。
- `./scripts/verify-linux.sh lazyclash-e2e` 对最终源码构建 11 个包，在现有 Debian/arm64 VM 内实际运行九个可运行测试包，通过；普通用户 uid 501，systemd 252，user manager 为 running、Linger=yes，测试前后 `/tmp/lazyclash-linux.*` 均为空。
- 八项 Python 终端解析测试通过。中文 True Color 实际 PTY 主流程及独立多组场景通过，保留各终端尺寸、四列上限、选择、引用、排序与滚动检查；本轮没有重跑六种语言/颜色组合。主流程 `selections=3`、`modes=3`、`tests=10`、`canceled=4`、`max_tests=4`；多组场景 `tests=14`、`canceled=4`、`max_tests=8`，其中取消后旧回调故意晚 80 ms 返回，单轮仍最多四项。
- 新增独立单节点 PTY 场景精确发起 HK 01 两次、US 01 一次、AUTO 两次请求，记录 `tests=5`、`canceled=0`、`max_tests=1`、`selections=0`、`modes=0`。HK 01 从已有的 42 ms 更新至 45 ms，再更新至 46 ms；验证未测试、失败状态可点击重测，引用不误进入，DIRECT/REJECT 不发起请求。已目视检查数值与状态居中、选中竖条和卡片留白。最后增加的旧引用路径标签保护由最终模型、race 和 Linux 测试验证。

证据位于忽略目录 `.test-artifacts/proxies-node-delay/`，包含宿主检查、race、模型、Python、Linux 环境/运行/清理日志，以及 `pty/` 下的逐场景记录和 PNG。PNG 按实际 PTY 字符格与颜色重绘，使用内存测试后端；本轮未测试真实远端节点、下载内核或操作 Mihomo/systemd/TUN 服务。

## Proxies 紧凑节点行距（2026-11-05）

节点卡片由四行缩为三行：顶部留白、名称、元数据，移除底部额外留白。保留一行卡片外部分隔，相邻节点名称行距由五行降为四行。选中竖条与点击区域同步缩为三行；顶部及元数据右侧留白继续选择节点，引用按钮保持独立。

最终 `make check build` 通过（TUI 24.628 秒），并通过 `./scripts/verify-linux.sh lazyclash-e2e` 在现有 Debian/arm64 VM 内的全部可运行包测试，临时目录清理确认通过。中文 True Color 实际 PTY 与独立多组场景通过，逐格检查三行选中竖条、顶部及元信息右侧留白点击、四列网格名称行距四行、少量成员槽位、第四列导航和引用入口；已目视检查 200×40 的紧凑布局。本轮未重跑完整六种语言/颜色组合或 race。

证据位于忽略目录 `.test-artifacts/proxies-compact-rows/`；最初遗漏一处旧四行高度断言的失败记录另存 `before-height-assertion-update/`，修正后完整检查与 Linux 运行重新通过。PTY 使用内存测试后端，PNG 由实际字符格与颜色重绘；不涉及真实节点、内核下载或服务操作。

## Proxies 最多四列（2026-11-05）

节点列数上限由三列调整为四列，继续按可用宽度自适应：160 列终端保持三列，200/240 列展示四列，更宽窗口也最多四列。少量成员和搜索后的单个成员继续保留对应列宽。同步调整既有列数、上下左右导航和部分尾排边界断言。

最终源码通过 `make check build`（TUI 23.629 秒），并通过 `./scripts/verify-linux.sh lazyclash-e2e` 在现有 Debian/arm64 VM 中的全部可运行包测试，临时目录清理确认通过。针对本轮布局变更运行中文 True Color 实际 PTY 场景及独立多组场景，覆盖既有各终端尺寸，明确检查 160 列三列、200/240 列四列、第四列左右键导航、短组/筛选列宽、padding 和独立引用入口，均通过。已目视检查 200×40 的四列画面；本轮未重跑完整六种语言/颜色组合或 race。

日志与实际 PTY 字符格/颜色重绘图片位于忽略目录 `.test-artifacts/proxies-four-columns/`。PTY 使用内存测试后端，不涉及真实节点、内核下载或服务操作；历史三列截图与基准保留其原有验证语境。

## Proxies 组头、多组展开与滚轮（2026-11-05）

组头使用完整正文宽度，包含名称、摘要和操作间的空白都连续绘制悬浮背景，数字徽标保留独立面板底色。名称前只保留键盘焦点，折叠箭头放到右侧数量之后；正常宽窗口的组头上下各留一行，短窗口和窄窗口保持紧凑。数量在不同位数下保持数字末端对齐。

宽屏支持多个组独立展开，并提供「全部收起」；各组分别保留排序，测速和排序按钮显式绑定所属组。引用与详情返回恢复原展开集合、搜索、焦点和滚动，宽窄切换保留展开集合。不同组测速先取消上一轮并隔离迟到结果，折叠不取消本轮测速。后台运行来源或模式变化使旧排序菜单失效；组头操作的上下键从本组继续导航。

普通、无通知的代理页滚轮只改变视口，跳过无关数据协调和焦点修复，最多重绘一次；到达边界且没有其他变化时不重绘。空通知集合不再触发无用的页面构建。侧栏、菜单、弹窗和通知继续保留各自的滚轮处理，没有增加分页、缓存或另一层 viewport。

本轮实际通过：

- 最终源码宿主 macOS/arm64 `make check build`，包含全包 Go 测试、vet 和产品构建，TUI 测试用时 24.309 秒。最终 `TestProxies`、`TestProxy` 和 `TestPageNavigation` 系列定向 race 用时 50.651 秒。此前完整 TUI race 通过（226.231 秒），该记录早于排序菜单失效及组头操作键盘导航两处最终修复，单独保存在 `before-final-state-fixes/`，不视为最终源码的完整 race 重跑。
- `./scripts/verify-linux.sh lazyclash-e2e` 对最终源码构建 11 个包，在已有 Debian 12/arm64、systemd 252 VM 内实际执行九个可运行测试包，通过。普通用户 uid 501，user manager 为 running、Linger=yes；前后 `/tmp/lazyclash-linux.*` 为空。
- 模型回归覆盖独立展开/折叠、全部收起、各组动作归属、分组排序、跨组测速取消和迟到结果、引用/详情展开快照、Global 多级引用、宽窄恢复、来源/模式变化、组删除/重排及按钮身份恢复。滚轮回归覆盖 48/300 节点、宽/窄窗口、离屏焦点、双向滚动、边界、完整布局等价，以及侧栏、导航弹层、通知和排序菜单的事件归属。
- Apple M4 Pro、200×45、每项 20 次、包含 `Update + View` 的滚轮基准：48 个节点 7.740 ms、4.16 MB/次；300 个节点 27.770 ms、12.44 MB/次。上轮同一事件协议的诊断分别为 20.714 ms、9.76 MB 和 98.602 ms、42.71 MB，约减少 63%/72% 耗时。此数据衡量本机事件处理，不包含 SSH 或终端显示延迟；仍会绘制完整正文，不能据此宣称超大列表的开销恒定。
- 八项 Python 终端解析回归通过。
- 最终 `scripts/proxies-pty.py` 使用真实 controlling PTY 完成中英文 × True Color、ANSI256、NO_COLOR 六组，每组 `selections=3`、`modes=3`、`tests=10`、`canceled=4`、`max_tests=4`，最终 `PROXY=HK 01`。覆盖 160×45、200×40、240×40、120×32、80×24、60×20、40×16、120×18；原有卡片 padding、三列与少量成员列宽、选择、引用、搜索、排序、完成提示与滚动范围继续通过。True Color/ANSI256 逐格检查组头三行的连续背景，包括普通空隙、padding 和按钮区域，数字徽标保留面板色；NO_COLOR 验证结构和动作。
- 独立 200×50 多组 PTY 场景验证同时展开、各组排序、跨组测速取消/迟到隔离和全部收起后继续完成测速；记录 `selections=0`、`modes=0`、`tests=14`、`canceled=4`、`max_tests=8`，选择未改变，没有旧代故意返回的 `777 ms`。该场景的内存后端在取消后故意延迟 80 ms 才结束旧回调，故旧代与新代每轮最多四项可短暂重叠，聚合并发上限不是单轮调度上限。
- 独立真实 PTY 滚轮探针使用内存后端、200×45，连续下滚 12 次、再上滚 12 次：48 节点约 127/114 ms 完成，300 节点约 365/349 ms 完成，两者均回到 `1–21` 的起始可见范围，无停滞或焦点拉回。48 节点首次可见变化约 44/31 ms，300 节点约 98/53 ms；终端刷新节奏也影响该值。上轮同尺寸、同事件协议诊断的整批下/上滚分别为 269/284 ms、1469/1398 ms；原始基线保存在 `.test-artifacts/proxies-scroll-diagnosis/`，本轮放在 `wheel/`，没有覆盖基线。
- 已目视检查中文整头高亮、多组独立展开和排序、80×24 窄屏及 120×18 短窗口的实际 PTY 画面。PNG 按实际 PTY 字符格和颜色重绘，用于布局检查，不是原生终端截图；PTY 明确使用内存后端，不代表真实 Mihomo API 或远端节点测速。

证据位于忽略目录 `.test-artifacts/proxies-groups/`，包含 `host-check-build.log`、`host-final-proxies-race.log`、`model-regressions.log`、`scroll-targeted.log`、`scroll-targeted-race.log`、`scroll-benchmark.log`、`python-tests.log`、`linux-tests.log`、`linux-environment.txt`、`linux-cleanup.json`、`pty.log`、`pty/` 和 `wheel/`。脚本适配过程的失败画面与记录单独保存在 `adaptation/`、`adaptation-notices/`，最终六组和多组场景全部通过。本轮未访问真实节点、重跑 Mihomo/systemd/TUN E2E 或尝试下载内核；Linux 实际运行平台为 arm64，未在 amd64 运行。

## Proxies 三列卡片与悬浮响应（2026-11-04）

节点网格按可用宽度分为一至三列，少量成员及搜索后剩余一、两个成员时继续保留列宽，不拉伸到整行。组头与首排卡片空一行；卡片的两行内容上下各加一行，右侧留两格，选中竖条贯穿四行。留白属于卡片的点击区域，引用入口仍独立；方向键按节点身份去重，避免卡片拆分的命中区域被误当成相邻节点。

普通代理页鼠标移动只更新悬浮：同一命中区不重复布局，切换命中区跳过无关的数据协调与焦点修复。弹窗和菜单保留原有事件流程。没有新增缓存或悬浮延时。

本轮实际通过：

- 最终源码宿主 macOS/arm64 `make check build`（全包 Go 测试、vet、产品构建），TUI 测试用时 22.586 秒；整个 `TestProxies` 与 `TestProxyHover` 系列的定向 race 通过，用时 18.789 秒。本轮未重跑全包 race。
- `./scripts/verify-linux.sh lazyclash-e2e` 对最终源码构建 11 个包，在 Debian 12/arm64、systemd 252 VM 内实际执行九个可运行测试包，通过。普通用户 uid 501，user manager 为 running，Linger=yes；`/tmp/lazyclash-linux.*` 前后均为空，没有私有测试目录残留。
- 模型回归覆盖最多三列、少量和筛选后单个成员不拉伸、四行选中竖条与左右内容对齐、卡片全部字符格的独立命中、上下留白的真实鼠标选择、标题间隔不捕获操作，以及部分可见 padding 仍计入成员范围。原有多列导航、引用往返、选择确认与取消、测速和过期结果保护继续通过。
- 悬浮回归确认普通节点、引用入口、侧栏与空白处的屏幕及命中区与完整布局一致，不改变焦点、滚动或选择，不启动后端命令；排序菜单继续响应移动。可复现本地基准包含 `Update` 与 `View`，每项 20 次，Apple M4 Pro：48 个节点由原完整流程的 16.17 ms 降为跨卡 7.23 ms、同卡 1.24 ms；300 个节点分别为 76.39、27.46、2.24 ms。对照保持相同布局与鼠标事件，仅比较事件处理路径，不代表 SSH 或终端显示延迟。
- 八项 Python 终端解析回归通过。
- 最终 `scripts/proxies-pty.py` 在真实 controlling PTY 完成中英文 × True Color、ANSI256、NO_COLOR 六组，各组记录 `selections=3`、`modes=3`、`tests=12`、`canceled=2`、`max_tests=4`，最终 `PROXY=HK 01`。覆盖 160×45、200×40、240×40、120×32、80×24、60×20、40×16、120×18，200 与 240 列均保持三列；验证标题间隔、上下 padding 点击选择与四行竖条、两成员和搜索后单成员保留列宽、引用入口、排序稳定、测速完成提示收起及连续滚动。已目视检查中文 200×40、240×40 的单卡及双卡、80×24 效果。PTY 使用内存后端，PNG 按实际 PTY 字符格与颜色重绘，不是原生终端截图，也不代表真实 Mihomo API 或远端节点测速。

证据保存在忽略目录 `.test-artifacts/proxies-card-spacing/`：`host-check-build.log`、`host-proxies-hover-race.log`、`hover-benchmark.log`、`navigation-fix.log`、`python-tests.log`、`linux-tests.log`、`linux-environment.txt`、`linux-cleanup.json`、`pty.log` 与 `pty/`。修复方向键回归前的完整测试及 Linux 失败记录独立保存在 `first-pass/`，早期 PTY 记录及脚本断言修正记录分别保存在 `pre-horizontal-fix/` 和 `script-*/`，最终源码与脚本已重新通过上述验证。本轮未访问真实节点或重跑 Mihomo/systemd/TUN E2E，也未尝试下载内核；Linux 实际运行平台为 arm64，未在 amd64 运行。

## Proxies 留白分组与展开信息精简（2026-11-04）

策略组之间留一行空白并去掉横向分隔线；组头显示最终节点、必要的「自动」标记和右对齐数量。展开后直接展示两行卡片，不重复当前选择与默认排序；非默认排序留在组头，引用卡片统一显示「策略组 · 最终节点」。测速进度只在实际有测试项时出现，完成或用户取消后保留两秒提示再收起，离页取消不延续提示。节点选择、独立引用入口、宽窄浏览和连续滚动范围沿用已有交互。

本轮实际通过：

- 最终源码的宿主 macOS/arm64 `make check build`（全部 Go 测试、vet 和产品构建），以及八项 Python 终端解析回归通过。最终 TUI 普通测试用时 22.053 秒。
- 本轮全包 `make race` 通过，TUI 用时 208.406 秒。最后一处排序焦点修复后，对最终源码另运行整个 `TestProxies` 系列的定向 race，通过，用时 15.353 秒；全包 race 与最后修复后的定向验证分别保留日志。
- `./scripts/verify-linux.sh lazyclash-e2e` 对最终源码构建 11 个包，在已有专用 Debian 12/arm64、systemd 252 VM 内实际执行全部九个可运行测试包，通过。普通用户 uid 501，user manager 为 running，Linger=yes；运行前后核对 `/tmp/lazyclash-linux.*`，没有本轮私有目录残留。
- 模型回归覆盖 160、80、40 列的组间留白不捕获选择、非默认排序与测速命中区、摘要精简但详情保留完整引用路径，以及完成和取消提示的到期、旧到期消息不影响新测速。最后补充鼠标事件回归：从非首组的排序入口恢复默认后，焦点与滚动位置仍返回该组，不跳到首组。既有多列导航、宽窄引用往返、自动组只读、选择确认、取消与迟到结果回归继续通过。
- 最终 `scripts/proxies-pty.py` 在真实 controlling PTY 完成中英文 × True Color、ANSI256、NO_COLOR 六组，每组记录 `selections=1`、`modes=3`、`tests=10`、`canceled=4`、`max_tests=4`，最终 `PROXY=HK 01`。覆盖 160×45、200×40、240×40、120×32、80×24、60×20、40×16、120×18，其中 200、240 列分别展示四列、五列卡片；继续验证连续滚动范围。新增检查确认测速完成提示自动收起后，卡片延迟结果与当前排序仍保留；已目视检查 200、160、80 列效果。PTY 使用明确的内存后端，PNG 按实际 PTY 字符格与颜色重绘，不是原生终端截图，也不表示真实 Mihomo API 或远端节点测速通过。
- `scripts/pty-smoke.py` 的显示断言同步为选中竖条与内置项 `—`，保留真实 API 回读和刷新 PID 断言。脚本语法、上述六组已捕获 PTY 字符屏幕及 51 个选择正反例的离线检查通过；这一步没有启动子 PTY、网络、Mihomo 或服务，不是重跑真实 E2E。修改脚本后八项 Python 回归再次通过。

本轮证据保存在忽略目录 `.test-artifacts/proxies-whitespace/`：`host-check-build.log`、`host-race.log`、`host-proxies-race-final.log`、`python-tests.log`、`linux-tests.log`、`linux-environment.txt`、`linux-cleanup.json`、`pty.log`、`pty-smoke-display-check.log` 和 `pty/`。最终焦点修复前的 check/build、Linux 与 PTY 记录另保存在 `first-pass/`。本轮未重跑真实 Mihomo/systemd 或 system scope/TUN 专项，也没有重新尝试官方内核下载；之前的真实运行与下载网络失败仍是独立历史记录，不能视为本轮源码的重新验收。Linux 实际运行平台仍为 arm64，未在 amd64 运行。

## Proxies 全宽布局与引用组就地浏览（2026-11-04）

代理页使用全部可用正文宽度，按实际空间和成员数量分配节点列数；卡片之间保留紧凑间隔，选中竖条与键盘焦点独立。宽屏模式与搜索同行，数量徽标右对齐，测速移入展开组标题；引用卡片提供独立入口。多列方向键先访问同排相邻成员，宽屏引用跳转保留组列表并将目标组标题定位到视口上部，返回恢复原搜索、焦点和滚动位置；窄屏继续采用两级浏览。成员连续滚动，可见范围保留在按键提示上方，不使用分页。

本轮实际通过：

- 最终源码的宿主 macOS/arm64 `make check build`（全部 Go 测试、vet 和产品构建）、全包 `make race`，以及八项 Python 终端解析回归。最终 TUI race 用时 209.219 秒，通过。
- `./scripts/verify-linux.sh lazyclash-e2e` 在已有专用 Debian 12/arm64、systemd 252 VM 内实际执行全部九个可运行测试包，通过。随后增加引用跳转锚点与切换等待期间的交互保护，最终源码另在同一 VM 重新执行整个 TUI 测试包，通过。两批日志分别保留，不将修复前的全包运行描述为最终源码的全包重跑。VM 普通用户 uid 501，user manager 为 running，Linger=yes；私有测试暂存目录已清理。
- 最终源码的模型回归覆盖三列、四列与不完整末行导航、少量成员铺满可用行、左右相邻成员优先、引用入口与选择互不混触、远处目标组定位和返回恢复、实时成员重排、模式切换、busy 期间禁用入口、右侧徽标与独立测速命中区，以及裁切到部分卡片时的可见范围和固定底部布局。
- 最终 `scripts/proxies-pty.py` 在真实 controlling PTY 完成中英文 × True Color、ANSI256、NO_COLOR 六组。每组实际记录 `selections=1`、`modes=3`、`tests=10`、`canceled=4`、`max_tests=4`，最终 `PROXY=HK 01`。覆盖独立引用入口与键盘往返、选择确认、自动组只读、排序稳定、取消与迟到结果、空组和负载均衡，以及 160×45、200×40、240×40、120×32、80×24、60×20、40×16、120×18 缩放；80×24 另以 PgDn 验证连续滚动后显示 `4–8 / 8`。该入口明确使用内存后端；PNG 按实际 PTY 字符格和颜色重绘，用于布局检查，不是原生终端截图，也不代表真实 Mihomo API 或远端节点网络测试通过。

真实网络验收未完成，以下三次尝试均保留失败状态：

1. 首次 `./scripts/e2e.sh lazyclash-e2e` 使用 direct 路线，通过 empty PTY 后，真实 download-network PTY 的官方版本查询报 `network connection`，未进入安装阶段。
2. 对同一官方版本地址进行独立 direct HEAD 查询返回 HTTP 200 后，沿同一路线进行一次完整 E2E 重试。此次通过 empty 与 download-network PTY、未安装的只读版本查询和 v1.19.32 pinned dry-run，但实际 release 文件下载在 `0/20809592` 字节时报 `network connection`，尚未部署内核或进入 Proxies。
3. 单独使用生产安装下载器，以明确的 environment 路线尝试获取官方 v1.19.33，供后续隔离的真实 Proxies 专项使用；release 文件连接探测同样报 `network connection`，未完成获取。既有 CONNECT observer 记录 `github=1`、`connected=1`，`rejected_api=0`、`other=0`、`upstream_failures=0`；这只证明经过明确选择的代理隧道，不能证明隧道后的 TLS 和下载已完成。全程保留 TLS 证书校验与生产下载大小限制，没有降低检查或在产品中增加路线回退。此次专项没有运行到真实 Proxies API/PTY。

两次完整尝试和单独获取尝试的清理均为 `resources released=1`、`exit=1`；没有遗留本轮专用服务，默认与 audit TCP/UDP 端口可重新绑定，VM 私有暂存目录已清理。本轮没有真实 Proxies API/PTY 运行通过的记录，也未重跑 system scope/TUN 专项；下方 2026-11-03 的完整 E2E 成功属于前一轮的独立历史证据。实际 Linux 运行平台仍为 arm64，未在 amd64 运行。

本轮脱敏证据位于忽略目录 `.test-artifacts/proxies-adaptive/`：`host-check-build.log`、`host-race.log`、`python-tests.log`、`linux-tests-pre-anchor.log`、`linux-tui-final.log`、`network-diagnostic.txt`，以及 `pty/`。两次完整尝试分别保存在 `first-attempt/`、`second-attempt/`，各自包含 E2E 日志、实际到达阶段的 PTY 记录和清理结果；`targeted-runtime/` 保存单独 environment 获取的生产下载日志、来源类别计数与清理记录。

## Proxies 分组与节点浏览重设计（2026-11-03）

代理页改为折叠组摘要与双列成员；窄屏使用组列表、成员列表两级浏览。选择确认与键盘焦点分别显示，自动策略组只供查看；搜索、排序、引用组往返、延迟测试与取消均保留稳定焦点和过期结果保护。引用组不复用核心的历史延迟，只显示绑定当前解析路径的本轮测试结果；叶子节点仍可使用核心历史。

本轮实际通过：

- 宿主 macOS/arm64 的 `make check`、`make build`、全包 `make race` 与完整 TUI/i18n race。最终引用组历史保护后重新通过 `make check build` 与 Proxies 定向 race；八项 Python 终端解析回归及变更脚本语法检查通过。
- `./scripts/verify-linux.sh lazyclash-e2e` 在已有专用 Debian 12/arm64、systemd 252 VM 内实际执行全部九个可运行测试包。最终产品提交另在 VM 内重跑整个 TUI 测试包，通过，包含引用组路径变化后旧历史失效的回归；私有暂存目录已清理。
- `scripts/proxies-pty.py` 在真实 controlling PTY 完成中英文 × True Color/ANSI256/NO_COLOR 六组，覆盖协议/UDP、双列键盘与鼠标选择、自动策略只读、组引用与搜索往返、排序、测试完成/取消/迟到结果、空组、负载均衡、模式切换，以及 160×45、120×32、80×24、60×20、40×16、120×18 缩放。该入口明确使用内存后端，调用报告与 ANSI/文本/PNG 位于 `.test-artifacts/proxies-redesign/pty/`；PNG 按实际 PTY 字符格与颜色重绘，不是原生终端截图，也不代表远端节点网络测试通过。
- `./scripts/e2e.sh lazyclash-e2e` 以 direct 下载方式完整通过真实 Mihomo/user systemd/PTY 验收。Proxies 使用真实内核的 DIRECT/REJECT，键盘和鼠标切换后均由 API 回读确认，`r` 刷新保持 PID；内置项显示 N/A。整轮还完成官方 v1.19.32 安装与 v1.19.33 升级、配置事务与回滚、规则、日志暂停/恢复、实时连接、端口修改及 SIGHUP/SIGKILL 恢复等既有场景。验证脚本同步新代理页交互与当前页面导航，日志停启状态改由实际服务 JSON 核对，同时保留捕获不可用、重连和新事件断言。

最终脱敏记录位于忽略目录 `.test-artifacts/proxies-redesign/`：`linux-tests.log`、`linux-tui-final.log`、`e2e.log`、`e2e-cleanup.txt` 及相关 `pty-*.txt`。完整 E2E 退出码为 0；随机专用 unit 已移除，默认与动态 TCP/UDP 监听均释放，清理记录为 `resources released=1`、`exit=0`。实际 Linux 运行平台为 arm64，未在 amd64 运行；本轮未重跑 system scope/TUN 专项，也不把内存后端延迟结果当作真实远端节点测速证据。

## systemd 属性查询与作用域诊断（2026-11-03）

管理器预检、服务名冲突检查和 linger 查询统一读取 `属性名=值`，移除旧版 systemctl/loginctl 不支持的 `--value`。可读的 `SystemState=degraded` 与 `running` 均表示管理器可访问；不要求清理全机失败服务。只有明确的 `LoadState=not-found` 才允许发布新 unit，查询失败、缺失属性、已有 vendor unit 或 masked unit 仍阻止安装。用户管理器失败保留会话与 `XDG_RUNTIME_DIR` 提示，系统管理器失败使用独立的中英文诊断。

核对了上游 systemd **v219** 的 [systemctl 属性选项与输出实现](https://github.com/systemd/systemd/blob/v219/src/systemctl/systemctl.c)、[Manager 的 SystemState 属性](https://github.com/systemd/systemd/blob/v219/src/core/dbus-manager.c) 和 [logind 用户的 Linger 属性](https://github.com/systemd/systemd/blob/v219/src/login/logind-user-dbus.c)。该版本支持所需属性及带名称的输出；systemctl 的多属性逗号列表也已存在。此核对不等同于在 v219 系统运行产品，也不代表当前生成的 unit、安全沙箱、capabilities 和完整 Mihomo 生命周期已适配 v219。

本轮实际通过：

- 宿主 `make check build`（全部 Go 测试、vet 和产品构建）、`go test -race ./internal/service ./internal/app ./internal/i18n`。
- 回归 fixture 在修改前复现 `--value` 导致的预检失败及 root 误报用户管理器；修改后覆盖 user/system 查询、running/degraded/starting 状态、带名属性解析、空/错属性、服务冲突、失败时不发布 unit、linger 的明确 yes/no、取消与错误链，以及中英文诊断。
- `./scripts/verify-linux.sh lazyclash-e2e` 在已有专用 Debian 12/arm64、systemd 252 VM 内实际执行全部可运行的 Linux Go 测试，通过。
- 显式启用 `LAZYCLASH_TEST_SYSTEMD=1`，分别以该 VM 的普通用户和 root 执行 `TestSystemdManagerIntegration`。真实 user/system 管理器预检、随机未占用服务名、未安装状态回读和 scope/linger 处理均通过；user 实际为 Linger=yes，system 不查询 linger。没有安装 unit、启停服务或改变 linger，测试二进制的私有暂存目录已清理。

证据保存在忽略目录 `.test-artifacts/systemd-properties/`：`before-fix.log`、`service-i18n.log`、`check-build.log`、`race.log`、`linux-tests.log`、`live-managers.log` 和最终 user/root 定向复验的 `linux-properties-final.log`；`upstream-v219/` 为只读获取的上述上游资源。普通测试默认跳过真实管理器检查；可在专用 Linux 测试机器上显式运行：

```sh
LAZYCLASH_TEST_SYSTEMD=1 go test ./internal/service -run '^TestSystemdManagerIntegration$' -count=1 -v
```

本轮未访问用户的服务器，没有真实 systemd 219 环境，也未重跑 Mihomo 安装/启动或 TUN 完整端到端测试。修复范围是已复现的命令选项、属性解析和作用域诊断，不能据此宣称整体兼容 systemd 219。

## master 合并验证（2026-11-03）

合并 `codex/service-scopes` 的四个提交，同时保留 master 的 API-free 下载实现及下载审计。修正两者的执行边界：安装用例把专用服务身份传入下载阶段，解压后的系统内核保持 root 所有、服务组只读和可执行，再以该非 root 身份运行版本检查；普通用户下载仍为 0700。

合并结果通过 `make check`、`make build`、`go test -race ./internal/app ./internal/mihomo`、八项 Python 回归及 `./scripts/verify-linux.sh lazyclash-e2e`。另在 Linux 以 root 实际执行八项身份、路径和文件权限测试，其中新增下载回归检查真实子进程 UID/GID、空附加组、最小环境、无法改写自身、最终文件归属及临时文件清理。`make package VERSION=dev` 重新生成两种 Linux 架构的归档并校验 SHA256SUMS。

用于重跑真实内核端到端的旧临时 Mihomo 文件已不存在；本轮获取官方 v1.19.33 测试文件时遇到 GitHub HTTPS TLS 连接错误，因此未重跑真实 Mihomo/systemd/TUN 完整端到端。下方两批实现各自的真实运行结果及限制分别保留，不能作为合并后重跑成功的记录。本轮 Linux 实测仍为 arm64，amd64 仅构建和归档验证。

## 双作用域、root TUN 与发行打包（2026-11-03）

本节记录服务 scope 分支的运行时验收结果；后续章节分别保留各批实现及验证范围。

本轮按首发范围实现运行时代码：root 使用独立 system service，普通用户各自使用 user service；`sudo lazyclash` 忽略原用户继承的实例环境。system 内核由专用非 root 账号运行，普通代理无网络 capabilities，明确授权 host TUN 后才授予 `CAP_NET_ADMIN CAP_NET_RAW`。不增加 daemon、提权 helper、普通用户 TUN 或旧安装兼容层。

实际通过的验证：

- macOS/arm64、Go 1.27.1：`make check`、完整 `make race`、`make build`、四项 Python 终端解析回归、Shell/Python 脚本语法及差异空白检查。
- `./scripts/verify-linux.sh lazyclash-e2e`：在已有专用 Debian 12/arm64、systemd 252 VM 内实际执行最终源码的全部可运行 Go 测试。另以 root 实际执行七项独立 scope 默认值、目录/文件归属、sandbox 路径、稳定清理身份和 Mihomo 降权回归；验证子进程 UID/GID 与补充组确实已下降。
- 第一批 scope 提交独立导出暂存源码，完成 `make check`、Linux 全包运行及真实 root/system、普通用户/user 的 proxy-only 验收；第二批 TUN 提交也从独立暂存树通过 check，避免依赖后续工作区文件。
- `make package VERSION=dev` 生成 Linux/amd64、Linux/arm64 归档与 SHA256SUMS，显式使用 amd64 v1 / arm64 v8.0。校验归档摘要和 ELF 架构，解包后的 arm64 产品用于下述完整实测；没有用 `go run` 或内存后端代替。
- root/system 与普通用户/user 实际安装、并发普通代理、TCP/UDP 端口冲突拒绝、独立数据/服务、root 核心非零 UID、普通代理 CapEff=0、`sudo` 继承用户环境不接管用户实例、安装后未自动启动及正常卸载。
- CLI 缺少 host 范围确认时拒绝启用 TUN；普通用户没有提权流程。已有外来 TUN 接口时拒绝启用；另一个 root 实例占用时错误包含占用实例，原服务不被停止。
- TUN 通过受控本地 HTTP CONNECT fixture 实际处理 `203.0.113.77:80` 流量，并将发往 `203.0.113.53:53` 的 DNS 查询劫持至受控 DNS fixture，验证响应与代理请求计数。TUN 期间专用内核只有 NET_ADMIN/NET_RAW，两种规则族、路由表和接口均核对。
- TUN 开启时重新应用 profile 保持策略；SIGKILL 内核后退出钩子清理，systemd 自动重启并再次达到真实 TUN 就绪；当前配置故意损坏时 Stop 报检查错误，但内核、接口、双栈规则/路由及 claim 仍可靠释放。关闭 TUN 后再次启动，网络 capabilities 归零；显式停止后由另一个 root 实例取得管理权。
- root 产品实际 controlling PTY 在 120×36、80×24 显示完整整机流量与 DNS 影响确认；默认 Enter 取消且网络不变。120×36 另实际确认开启 TUN，再经 CLI 关闭并核对资源释放。模型补充中英文 160×45、80×24、60×20、120×18、20×7，极小窗口无法完整阅读时禁止确认。

实测发现并修复了 iproute2 自定义路由表的字符串 JSON、设备消失后的 `iif_detached` 标记，以及 Mihomo 已创建网络设备但 TUN 栈尚未初始化完成的窗口。unit 就绪钩子与 app 就绪均需 API 确认 TUN 实际开启；单凭接口或 `systemd active` 不判定成功。清理使用独立受保护 instance-id，不依赖损坏的 current 配置；不删除无法确认归属的规则、路由或接口。

此 OrbStack VM 为所有 system service 注入会弱化 sandbox 的全局 drop-in，且 LXC generator 会在 daemon-reload 时重建它。测试 fixture 在专用 VM 临时保存这两份文件，结束后按原始 SHA256 恢复并 reload，没有重启其他服务。产品仍拒绝额外 drop-in，没有为测试环境放宽 unit 所有权或权限校验。各轮创建的专用 unit、账号、安装目录、网络 fixture 和 TUN 管理记录已清理。

本轮也显式执行既有 `./scripts/e2e.sh lazyclash-e2e`；默认 9097/7897 端口被该 VM 其他已有 Mihomo 实例占用，脚本按预期在资源创建前退出，没有停止已有进程。完整运行验收由新入口使用独立动态端口完成；本轮不将旧入口报告为通过。Mihomo 使用明确选择的本地 v1.19.33 可执行文件副本，不读取旧安装的订阅或凭据，也不把这次本地安装验收描述为官方下载安装链路验证。

入口：

```sh
make package VERSION=dev
# 解包对应 VM 架构的归档后；第三个参数是 VM 内明确选择的 Mihomo 可执行文件
./scripts/service-scopes-e2e.sh VM /absolute/packaged/lazyclash /absolute/vm/mihomo
./scripts/service-scopes-e2e.sh VM /absolute/packaged/lazyclash /absolute/vm/mihomo --proxy-only
./scripts/service-scopes-e2e.sh VM /absolute/packaged/lazyclash /absolute/vm/mihomo --ui-tun-only
```

脱敏证据位于忽略目录 `.test-artifacts/`：`scopes-check.log`、`scopes-race.log`、`scopes-linux.log`、`scopes-root-tests.log`、`package-verify.log`、`scopes-index-check.log`、`scopes-index-linux.log`、`tun-index-check.log`、`service-scopes-e2e.log`、`service-scopes-proxy-e2e.log`、`service-scopes-ui-e2e.log` 与 `pty-root-tun-{120x36,80x24}.txt`。

实际运行平台为 Linux/arm64；amd64 完成打包构建与归档验证，尚未在 amd64 机器运行。本轮没有重启 VM、注销最后一个真实 SSH 会话、同时运行 Alice/Bob 两个普通 UID，也未完成 IPv6 端到端流量测试；双栈规则/路由核对不等同于这些场景通过。自动交接、UID 流量筛选、普通用户提权与系统 resolver 改写不在首发范围。


普通构建/单元测试不会调用 OrbStack、安装服务或修改系统配置。以下三类运行验证均为显式入口，不由 `go test` 自动触发。

## 移除 GitHub Release API 依赖（2026-11-03）

安装、重装、指定版本、升级和下载方式连接测试共用 API-free 资源解析：最新版本来自既有 `version.txt` 检查，稳定标签与 CPU 能力规则生成固定的官方 Release 文件地址。最多读取 4 KiB 探测候选，只有 HTTP 404/410 会尝试兼容构建；所有请求遵循用户所选的 direct/environment/accelerated 路线。没有访问 `api.github.com` 的生产回退。

当前校验模型为响应状态与长度、64 MiB 压缩包上限、128 MiB 解压文件上限、完整 gzip/CRC、0700 权限与同步、执行 `-v` 精确版本，以及应用层配置验证、事务和回滚。HTTPS 证书验证保持开启；HTTP 加速前缀没有该段 TLS 保护。不存在上游预期摘要或独立签名验证，本地安装记录摘要用于后续变化和恢复身份检测。下方 2026-11-02 及之前的摘要/API 记录描述当时实现，不能视为当前校验承诺。

本轮已实际验证：

- 宿主 macOS/arm64：最终源码的 `make check`（全包测试及 vet）、全包 `make race`、`make build` 通过。独立导出的实现暂存树另通过 `go test -mod=readonly ./...`，不依赖未暂存的验证脚本。
- HTTP fixture 覆盖稳定版本文本与固定标签资源链、amd64 兼容候选、仅 404/410 降级、有限 Range/gzip 头/响应长度、未知总量、完整 gzip 损坏与截断、精确执行版本、取消和失败清理。CLI 实际使用生产 Resolver/下载器，以本地 HTTP 和 gzip 可执行文件 fixture 验证 latest dry-run 不完整下载，指定版本只安装一次且服务保持停止；应用层保留配置、事务和回滚回归。
- `scripts/verify-linux.sh lazyclash-e2e` 在已有专用 Debian 12/arm64 VM 内实际执行全部可运行的 Linux Go 测试，通过；不是仅交叉编译。普通 uid 501，systemd 用户 manager running，Linger=yes。
- `scripts/onboarding-pty.py --test-binary` 在真实 controlling PTY 完成中英文 × 120×32、60×20 × 更多选项/成功共八组，核对新连接测试名称、诚实的校验说明及成功后 `starts=0`。使用明确启用的内存后端；PNG 是实际 PTY 字符格与颜色重绘，不是 Ghostty 原生截图，也不替代真实下载安装验证。
- Python 八项回归、全部 Python 脚本语法、E2E Shell 语法及差异空白检查通过。新增 CONNECT observer 的本地协议测试验证不改写隧道负载、上游认证、API 拒绝、失败无回退、错误脱敏及关闭后的端口释放。

真实网络验收使用同一显式 environment 上游，由仅记录类别的 CONNECT observer 串联，保留端到端 TLS。首次完整 E2E 尝试已通过实际产品 empty/TUI 连接测试、未安装只读检查、指定版本 dry-run、官方 v1.19.32 下载与安装、运行中手动换为 v1.19.33、同版本复用与重装、旧版运行时只读检查；手动换版保持旧 PID、实际 API 版本和配置。所有已执行远程阶段的审计均为 GitHub API 尝试零。

`./scripts/e2e.sh lazyclash-e2e --download-method environment` 三次均未整轮通过：分别在确认升级的版本文本查询、指定版本 dry-run 的文件探测、TUI 连接测试的文件探测遇到网络/TLS 中断。无 observer 的同一路线只读对照也复现 `SSLZeroReturnError`，不能将其归因为 GitHub API 限流或据此声称整套生命周期 E2E 已通过。没有自动重试、切换路线或忽略失败。首轮清理曾因 observer 的 TIME_WAIT 被误判为监听残留而报失败，修正其监听复用设置后，本地协议测试和后两轮清理均通过；首轮已另行核验自有 unit、进程和端口全部释放。

随后沿同一路线定向执行真实安装、启动、只读检查与确认升级。四阶段均首次成功，原始退出码保存在 `targeted-upgrade/attempts.jsonl`。检查保持旧 PID；确认升级由官方 v1.19.32 升至 v1.19.33，实际 PID 改变，安装版本与实际 API 运行版本一致，配置摘要不变，`core-pending.json` 已清除。运行会话中断后恢复检查现有实例并完成清理，没有重新下载或重新执行升级。审计共 12 个官方 CONNECT 隧道，API/其他目标/上游 CONNECT 失败均为零。补验及清理记录明确区分于原始四阶段执行：专用 unit 为 not-found、自有进程归零、默认端口和 observer 端口均可绑定，`resources released=1`、`exit=0`。

证据位于忽略目录 `.test-artifacts/api-free/`：`check.log`、`race.log`、`build.log`、`staged-code-test.log`、`staged-script-test.log`、`linux-tests.log` 和 `pty/20261103-110314-254fab/`；失败整轮保留在 `first-attempt/`、`second-attempt/` 与 `e2e-run.log`，路线对照为 `network-diagnostics.jsonl`，成功定向升级及恢复清理证据在 `targeted-upgrade/`。实测平台仍为 macOS/arm64 与专用 Debian/arm64；没有扩大 Linux/amd64、其他发行版或真实掉电恢复的运行验证范围。

## 轻量卡片与侧栏布局（2026-11-02）

概览保留轻量卡片：内核、路由模式、配置、代理组形成主区域，流量统计继续通栏展示，环境及已安装内核的信息仍为独立卡片。TUN 和系统代理说明收进环境，跳过安装后仅有一个普通安装入口；管理配置直接进入配置页。首次安装正文上限为 96 列并与完整页头左边界对齐。配置卡片减少重复元数据，摘要明确区分自定义数量与内置数量。侧栏宽 20 列，最终按反馈恢复标签本身居中，焦点箭头及低色当前页圆点独立占位，不挤偏文字。

本轮实际验证：

- 宿主 macOS/arm64：概览基线的 `make check`、全包 `make race`、`make build` 通过；删除未引用翻译键后另通过 i18n 测试。最终居中侧栏重新通过 `make check`、`make build` 及 Sidebar/MarianaSidebar/DashboardSidebar 定向 race。Python 四项 VT 解析回归、变更脚本语法及差异空白检查通过。
- 模型覆盖中英文、单列/双列、首次安装/跳过/已安装、按钮命中区与滚动裁切、稳定卡片 ID、环境焦点保持及显式进入/激活内核管理。安装区覆盖 216×55 至 20×7，断言正文限宽不缩短页头分隔线。
- 实际产品安装/概览 PTY 完成中英文 × 216×55、160×45、80×24 六组。首次安装区实际宽度分别为 96、96、76 列；验证稍后设置、唯一普通安装按钮、管理配置直达配置页且无导入框、返回时保留会话跳过，以及窄屏滚动到流量和环境卡片。
- 产品 `profiles-pty.py` 的 loopback HTTP fixture 完整通过创建、刷新、菜单、缩放、删除与退出重开持久化，HTTP 请求计数为 14。`i18n-pty.py` 通过语言切换、重开持久化、中文鼠标命中、帮助、网络草稿及 120×40 ↔ 80×24 缩放。
- `scripts/verify-linux.sh lazyclash-e2e` 在已有专用 Debian/arm64 VM 内实际运行全部可运行的 Go 测试包，通过；最终居中侧栏也重新通过同一入口。Linux 产品 `theme-pty.py` 在 True Color、ANSI256、NO_COLOR 下均通过 160×45、120×32、80×24、60×20、120×18，覆盖焦点、通知、网络草稿及缩放；私有暂存目录已清理。
- 侧栏缩至 20 列后，Logo 点击与进场 PTY 分别通过 160×45、120×24、100×20，保持字标坐标及动画时序；另验证 80×24 启动后扩大不补播。
- 最终居中侧栏的产品 PTY 通过中英文 × True Color/ANSI256/NO_COLOR 六组，每组包含 160×45、100×20、80×24 弹出菜单，逐格核对标签中心及上下切页后的坐标。最终中文 160×45、80×24 另复验安装/跳过/管理配置/返回链路并检查 PNG；模型补充 ANSI16 与窄至 4 列时的装饰裁切及边框保持。

记录在忽略目录 `.test-artifacts/light-cards/`、`.test-artifacts/layout-profiles-pty/`、`.test-artifacts/sidebar-layout/` 及最终居中的 `.test-artifacts/sidebar-centered/`。PNG 为实际 PTY 字符格重绘或 Go View 模型预览，不是原生 Ghostty 截图。PTY 使用隔离的未安装目录；本轮未重跑真实 Mihomo/systemd 生命周期 E2E，需明确核心路径的集成测试仍按条件跳过。

## 紧凑色块按钮（2026-11-02）

普通页面、页头、概览、安装区、向导、配置卡片与表单、帮助及通知共用按钮渲染。主操作、当前选中、焦点、危险确认、禁用和执行中使用独立表现；固定两列焦点位置，颜色能力切换不改变按钮宽度。配置删除默认取消，仍需名称匹配与明确确认。页头的全部更新在最后一个可更新配置消失后会修复焦点，旧动作也不能启动任务。

本轮实际验证：

- 宿主 macOS/arm64：最终 `make check`、`make build` 通过；全包 `make race` 通过，随后页头禁用保护的最终改动另通过 Button/HeaderButton/ProfilesHeader 的定向 race。Python 四项 VT 解析回归、全部 Python 脚本语法与差异空白检查通过。
- 模型回归覆盖中英文、True Color/ANSI256/ANSI16/无色、焦点与悬停的标签和命中区不移动、通知控件不重叠、忙碌防重复与取消清理禁用、配置删除默认取消及名称校验。删除按钮尺寸覆盖 120×32 至 24×7，其他控件保留既有极小窗口回归。
- 安装引导实际 controlling PTY 完成中英文 × True Color/ANSI256/NO_COLOR × 160×45、80×24、60×20、120×18 × 取消、成功、查询重试、更多选项共 96 组；内核更新另完成中英文 × 三种颜色能力共 6 组，每组包含 160×45、120×32、80×24、60×20、120×18、40×16 缩放。明确启用内存后端，核对调用计数、取消清理和成功后未启动，不实际安装内核或服务。
- 另在实际 PTY 完成 24 组中英文按钮外观检查，逐格验证主按钮 `#5c99d6` 底色、`#1d2227` 文字、加粗、橙色 `#f9ae58` 焦点，以及 Tab/Shift+Tab、悬停、256 色与无色边界的固定位置。PNG 按 PTY 字符格重绘，不是 Ghostty 原生截图。
- 产品 `profiles-pty.py` 的 loopback HTTP fixture 完整通过创建、刷新、菜单、持久化、缩放和四种删除确认场景；请求计数为 14。产品 `theme-pty.py` 在宿主和 Linux/arm64 各完成三种颜色能力 × 五种尺寸，覆盖通知、焦点、网络草稿和缩放。
- 产品 `i18n-pty.py` 通过中英文切换、重开后语言持久化、中文鼠标命中、帮助、网络表单及缩放；脚本先显式取消首次只读版本查询，再操作语言按钮，避免在忙碌期间点击禁用按钮。
- `scripts/verify-linux.sh lazyclash-e2e` 在已有专用 Debian/arm64 VM 内实际执行全部可运行的 Linux Go 测试，通过。预检为普通 uid 501、systemd 用户 manager running、Linger=yes；私有 PTY 暂存目录已清理，没有修改 VM 配置。

记录位于忽略目录 `.test-artifacts/buttons/`：宿主检查及 race 日志、`linux-tests.log`、`theme-pty/`、`linux-pty/`、`profiles-pty/` 和 `wizard/`。本轮没有重跑真实 Mihomo/systemd 生命周期 E2E；需显式实际核心路径的集成测试仍按条件跳过，安装进度和成功场景来自内存后端。

## 概览内安装与统一下载方式（2026-11-02）

本节记录当时实现；下载与校验行为已由上方 2026-11-03 的 API-free 实现替代。下方同日的首次安装和单页安装记录保留其当时行为与验证结果。概览直接显示安装区，默认只读检查稳定版本；明确点击下载并安装后才解析资产、预检及安装，不再打开初装弹窗或要求第二次确认。下载方式统一为直连、使用环境代理、GitHub 加速，当时版本文本、发布 API、安装包探测和下载遵循同一设置。该轮没有移除安装阶段对 GitHub API 预期 SHA256 的依赖。

本轮已实际验证：

- 宿主 macOS/arm64 的全包 `make check`、`make race` 与构建通过；包含查询取消与迟到结果、单次安装链、失败重试、预检阻断、取消等待清理、明确启动、输入焦点和极小窗口回归。
- `scripts/verify-linux.sh lazyclash-e2e` 在已有专用 Debian/arm64 VM 中实际执行全部 Linux 测试包，通过。网络 fixture 验证 version.txt → 发布元数据 → Range 探测 → 完整 SHA256 下载的统一加速地址，没有静默直连回退。旧偏好迁移和拒绝不支持配置时保留原文件也有回归。
- `scripts/onboarding-pty.py --test-binary` 在宿主真实 controlling PTY 完成中英文 × 三种颜色能力 × 120×32、80×24、60×20 的 18 组取消场景；成功、查询失败重试、更多选项各完成中英文 × 三种尺寸，共 18 组。每组核对检查、解析、预检、安装调用次数，成功页明确验证 `starts=0`。
- Linux/arm64 实际 controlling PTY 完成取消、成功、更多选项 × 中英文 × 三种尺寸，共 18 组；临时目录已清理。`core-update-pty.py` 另完成中英文 × 三种颜色能力的六组更新场景，包括 120×32、80×24、60×20、40×16 缩放。
- 实际产品 `theme-pty.py` 完成三种颜色能力各五种尺寸；`i18n-pty.py` 完成语言切换、偏好持久化、中文鼠标命中、帮助、下载方式表单及缩放。Python 四项 VT 解析回归、全部 Python 脚本语法、E2E Shell 语法与差异空白检查通过。
- 收尾复核修正失败页展开更多选项的重复焦点，以及后台查询失败后从导航返回正文时恢复到禁用安装按钮的问题；实际 `Update` 按键回归验证可达选项及显式重试。最终源码重新完成全包 check/race、Linux 全包测试与构建，独立导出的实现暂存树通过 `go test -mod=readonly ./...`。日志分别为 `.test-artifacts/inline-check-final.log`、`inline-race-final.log`、`inline-install-linux-final.log` 与 `inline-staged-implementation-test.log`。

最终实际执行 `./scripts/e2e.sh lazyclash-e2e --download-method environment`：冲突预检及真实产品 `empty` PTY 通过，包括安装前导入、下载方式草稿、24×8 缩放、本地长路径输入、Esc/SIGTERM 取消和清理。随后官方固定版本 Release API 返回 HTTP 403，尚未下载内核，本轮真实安装、启动及后续 systemd 生命周期未完成。清理确认 `resources released=1`、`exit=1`。记录为 `.test-artifacts/inline-e2e-after-pty-sync.log` 和 `.test-artifacts/cleanup.txt`。

完整前置操作后的长路径输入暴露了验证脚本读取旧增量帧的问题：脚本连续发 Tab 时仍在消费输入期间积压的画面。`pty-smoke.py` 现在先等完整路径，再等同步渲染帧完成后判断每次焦点，保留准确的安装按钮断言。独立 Linux 实际产品 `empty` 也完整通过，记录为 `.test-artifacts/inline-install-pty/empty-real-final.txt`。

主要证据位于忽略的 `.test-artifacts/inline-install-pty/`：`final-matrix/20261102-170431-f9df1e`、`final-scenarios/20261102-170632-6e2b08`、`linux/evidence/20261102-171358-437da5`、`core-update/`、`theme-final/` 与 `i18n-final/`。稳定展示图为 `.test-artifacts/inline-install-preview-{initial,download,success,network,compact}.png`，已按 Linux 实际 PTY 字符格和颜色重绘并人工查看。

安装与升级 PTY 使用明确启用的内存后端 fixture；它们验证产品状态机和终端交互，不下载或执行真实 Mihomo，也不创建 systemd 服务。PNG 不是原生 Ghostty 截图，不以其代替真实下载安装或服务生命周期验证。

## Mihomo 版本检查与升级（2026-11-02）

本轮实现与边界见 [内核版本管理](core-updates.md)。已实际运行：

- 宿主 macOS/arm64：`make check`（全部 Go 测试与 vet）、`make race`、`make build`、四项 Python 终端解析测试及 `git diff --check` 通过。最终界面紧凑布局及诊断调整后再次运行全包 check/build，并对 app、CLI、TUI、service、store 的更新/恢复/锁相关用例运行 race，通过。
- `./scripts/verify-linux.sh lazyclash-e2e` 在现有专用 Debian 12/arm64 VM 内实际执行全部包测试，两次均通过，最终记录位于忽略的 `.test-artifacts/core-update-linux-run.log`。包括正式版比较、Alpha/未知格式、未安装/不可执行、HTTP/超时/无效响应、损坏下载、配置与版本验证失败、取消、重启/API就绪失败、回滚失败保留材料、中断边界、Stop 优先、systemd start-limit 清理、外部状态变化及并发升级。
- `scripts/core-update-pty.py` 使用 opt-in TUI 测试二进制，在真实 controlling PTY 内验证中英文 Manage 打开不查询、一次手动检查、版本审阅、一次明确确认升级、Esc 等待模拟恢复以及缩放；各自调用报告为 `checks=1, upgrades=1`。测试尺寸 120×32、80×24、60×20、40×16；40×16 初始屏同时可见本地/目标版本、状态和重启提示。记录位于 `.test-artifacts/core-update-20261102/pty-final/`。此 PTY 使用 fake backend，无真实网络、内核文件或系统服务，不能当作真实升级证据。
- 同目录 `previews/` 保存 24 组中英文检查/进度/结果的 ANSI 与 PNG（四种尺寸）；PNG 是模型终端渲染预览，已人工查看，非原生终端截图。
- 构建后的实际 CLI 经显式 environment 代理成功查询 stable `v1.19.33`、Alpha `alpha-69c1631`。两次以不存在的数据目录调用 `install check`，查询后均确认目录不存在；没有创建锁、下载内核或启动服务。

真实完整 E2E **本轮未通过**：显式执行 `./scripts/e2e.sh lazyclash-e2e --proxy-mode environment` 与无代理版本，均在第一个固定版本安装的官方 Releases API 查询返回 HTTP 403（permission denied or rate limit），尚未下载内核。两次均完成冲突预检和 empty PTY；清理确认 `resources released=1`，无专用 unit、进程或监听残留。产品没有绕过摘要验证；同一网络的 `version.txt` 查询可以成功。

E2E 脚本已新增旧版实际运行时只读检查保持 PID、确认升级改变 PID/实际版本且配置不变的断言，但因上述限流尚未执行到该场景。真实新内核启动失败、start-limit 后旧版本恢复尚无本轮 systemd 实测证据，仅有业务及服务 fixture 测试。未验证真实掉电持久性、Linux/amd64 CPU 或其他发行版；不能由 Linux/arm64 单元执行和文件同步顺序推断这些环境已通过。

## 新建配置表单（2026-11-02）

本轮第二个按钮使用 New profile / 新建配置，统一创建远程 URL 或本地文件配置，并接受可选显示名称、描述。已验证：

- 宿主 macOS/arm64：全包 `go test ./...`、`go vet ./...`、构建，以及新增 HTTPS 创建/取消/失败原子性、描述清空业务测试通过。TLS 使用本地 fixture 的受信客户端，不关闭产品证书校验。
- `./scripts/verify-linux.sh lazyclash-e2e` 在现有专用 Debian 12/arm64 VM 内实际运行全部测试二进制通过，包含新增配置创建业务测试和表单模型测试。
- `scripts/profiles-pty.py` 通过真实 controlling PTY 验证本地路径匹配/Tab 补全、已完成路径到创建按钮的焦点切换、远程名称与描述、忙碌期间防重复提交、一次下载、保留来源更新、退出重开元数据持久化，以及现有卡片菜单和命名回归。
- 表单模型覆盖 120×30、80×24、60×20、40×16、40×14，操作按钮命中区与错误保持可见。中英文实际 PTY 屏幕另检查 120×32、80×24、60×20、40×16、40×14，保存屏幕文字及按 VT 单元格重绘的 PNG；PNG 为终端状态预览，不是原生终端应用截图。
- Python 终端屏幕解析测试通过。脱敏结果位于忽略的 `.test-artifacts/profile-create-20261102-final/`，表单预览在 `.test-artifacts/profile-create-20261102/previews/`。

本轮 PTY 使用临时未安装目录和 loopback HTTP fixture，不安装或启动内核；没有重跑真实 Mihomo/systemd 的完整 E2E，也不把测试中的假服务视为真实服务验证。

分批提交复核：每个代码批次均单独导出 Git 暂存树，以 `go test -mod=readonly ./...` 验证其不依赖后续未提交文件；完整代码树另通过 vet 与构建。主题 PTY 已按当前首次引导、导航层级和通知行为更新，并在 Truecolor、256 色、NO_COLOR 的 160×45、120×32、80×24、60×20、120×18 下通过，结果位于 `.test-artifacts/commit-theme-20261102/`。

`scripts/i18n-pty.py` 同时通过实际语言切换、退出重开后的偏好持久化、中文鼠标命中、帮助、网络表单和缩放检查，记录在 `.test-artifacts/commit-i18n-20261102/`；Python 屏幕解析单元测试、全部 Python 脚本语法及 E2E Shell 语法检查通过。

## 1. 单元、HTTP fixture、模型与静态检查（macOS 或 Linux）

```sh
make check
make race
make build
python3 -m unittest discover -s scripts -p 'test_*.py'
```

覆盖原子写和锁取消、下载校验及失败清理、控制 API HTTP fixture、配置生成、服务归属和命令失败、安装与应用回滚、恢复日志和用户修改保护。TUI 模型测试独立验证异步操作、取消、过期结果与显示状态；模型测试不等于真实终端验证。

## 2. Linux VM 内的测试

先检查已安装的 OrbStack，不创建或启动未知环境：

```sh
orb list
VM=你的专用测试虚拟机
orb -m "$VM" sh -c 'id; uname -m; ps -p 1 -o comm=; systemctl --user is-system-running; loginctl show-user "$(id -un)" -p Linger -p RuntimePath'
./scripts/verify-linux.sh "$VM"
```

要求普通用户、systemd 用户会话、Python 3。脚本根据 VM 实际架构，在宿主编译 Linux 测试二进制，复制至 Linux 的 `mktemp` 目录后**逐包执行**，不依赖 VM 内 Go 的版本。它并非仅交叉编译检查。使用 `CGO_ENABLED=0`，Linux 此入口不声称运行 race；race 单独在宿主运行。

## 3. 真实终端 PTY

`scripts/pty-smoke.py` 使用 Python 标准库 `pty.fork` 创建有 controlling terminal 的进程，发送按键、改变窗口大小并检查渲染和退出。它不调用模型的 `Update` 方法来模拟终端。

```sh
python3 scripts/pty-smoke.py --help
```

`empty` 模式仅操作专用的未安装数据目录，检查概览内安装区、下载方式草稿的编辑与放弃、安装前导入、宽窄布局、安装阶段取消和 SIGTERM。`download-network` 模式按明确选择的 direct/environment 方式，在未安装实例中真实查询版本和探测安装包，再放弃草稿，断言没有创建安装或服务。`install-local` 模式通过 TUI 内联安装明确的本地 Mihomo 二进制并确认未启动服务；`installed` 模式使用已启动的专用服务，以鼠标和键盘切换 PROXY 节点，并通过实际 API 回读确认。`runtime` 模式使用专用流量连接，验证实时卡片、按 UUID 搜索/详情/确认关闭、缩放和停止/启动后的自动恢复。`runtime-settings` 模式验证端口输入、取消草稿、占用后的纠正、确认重启、宽窄切换及各控制 API 页面的重新连接。完整的目录、服务、前置检查和清理由下一入口安排，建议通过它执行全部模式。

## 4. 真实 Mihomo + systemd 端到端

```sh
./scripts/e2e.sh "$VM"
# 若明确使用该 VM 已有的标准代理环境变量：
./scripts/e2e.sh "$VM" --download-method environment
```

此脚本显式修改所选 Linux VM 中的**临时资源**：

- 动态检测 CPU 架构、普通用户与用户 systemd，不硬编码用户名或工程目录。
- `mktemp` 数据目录、随机 `lazyclash-e2e-*` unit；运行前检查同名 unit 与 `9097`/`7897` 端口，冲突即失败，不停止已有资源。
- 显式 environment 模式将已有 HTTP(S) `HTTPS_PROXY` 作为上游，为本轮进程设置临时本地 CONNECT observer；不解密 TLS，只记录目标类别与隧道计数，并拒绝 `api.github.com`。每个远程阶段断言经过该路线且 API 尝试为零。此验证工具不支持 SOCKS 上游；产品本身的环境代理支持范围不变。direct 模式不产生同等的 observer 审计证据。
- 由产品按明确标签与设备规则下载官方 Mihomo **v1.19.32** 和 **v1.19.33**，验证 HTTP 响应、大小限制、gzip 完整性与精确可执行版本；不查询 GitHub Release API 或校验上游预期摘要。另建独立 unit，经 TUI 本地安装和 CLI `--bin` 验证。
- 未安装时真实执行 TUI 连接测试、`install check` 和指定版本 dry-run，检查无安装或 unit；旧版运行时的只读检查保持 PID，确认升级后核对新 PID、已安装与运行版本、配置不变及 journal 清理。
- 验证真实默认配置、API 认证和组、PTY 显示与节点切换、本地 HTTP YAML 保存/校验/应用，以及 start/stop/restart 生命周期；运行旧版时安装新版、同版本复用和显式重装均检查 PID、旧运行版本和配置字节保持不变。
- `trap` 仅清理本次确认所属的 unit、进程和文件，失败也执行；不读取旧代理配置，不修改 macOS 代理、DNS 或路由。

日志保存在忽略的 `.test-artifacts/`，只包含脱敏场景输出、PTY 文本、测试结果与清理状态，不保存 secret、原始配置或未过滤 journal。失败时先查看这些诊断，不直接贴出原始配置。

## 已执行环境

2026-10-28：

- 宿主 macOS/arm64，Go 1.27.1：构建、全项目 `go test`、`go vet`、`go test -race` 通过。
- 复用现有专用 OrbStack Debian 12/arm64 测试 VM（本次选择 `proxyctl-e2e`），普通用户、systemd 252、用户 manager 可用且 Linger=yes。脚本仍要求调用者显式选择 VM，不依赖此名称。
- `verify-linux.sh` 将各包测试二进制放入 VM 后实际执行，包括 TUI 模型与 HTTP fixture。
- `e2e.sh` 实际运行官方下载/校验与本地安装、有效默认配置、HTTP 订阅、API、PTY 和服务生命周期；SIGTERM/按键取消等待清理。
- 清理检查确认专用 unit `LoadState=not-found`，7897/9097 释放，临时文件及测试进程清除；脱敏记录在 `.test-artifacts/`。

该日期使用当时的依赖版本；当前版本验证见下文。未实际验证 Linux/amd64、其他发行版或真实注销/重启后的服务持续运行。没有把交叉编译视为这些环境的运行证明；macOS 服务管理明确不支持。


## 响应式首页、网络设置与向导（2026-10-29）

当前依赖已统一为稳定 Charm v2：Bubble Tea 2.1.1、Bubbles 2.2.1、Lip Gloss 2.0.6、Fang 2.0.1，最低构建声明为 Go 1.26。

测试分类与可重复入口保持独立：

- `make check`：全项目测试与 vet。网络 fixture 包含 official/prefix/mirror、None/环境/自定义代理、来源与代理组合、HTTP/HTTPS CONNECT、认证 SOCKS5/SOCKS5H、API 限流和 HTML、TLS/DNS/超时分类、文件 Range 限量和摘要不匹配。全部使用本地 fixture，不测试开发者真实代理。
- `make race`：在宿主 macOS/arm64、Go 1.27.1 运行全项目 race。
- TUI 模型：默认进入首页、单一 Core/Version/Confirm/Installing/Result 状态机、网络草稿和保存、测试身份、重试、迟到消息、取消清理、键鼠/Paste/KeyRelease、focus及背景隔离。布局覆盖 160×45、120×32、100×28、80×24、60×20、120×18 和极小窗口；长中文/路径不会越界。
- `verify-linux.sh VM`：按 VM 的实际架构构建并在 Debian 12/arm64 内执行所有测试二进制，包含新网络和 TUI 测试。不是只做交叉编译。
- `e2e.sh VM`：真实 controlling PTY 和真实 Mihomo/systemd 生命周期。PTY runner 使用纯 Python 标准库还原当前 VT 屏幕，以适配 v2 按单元格更新的 renderer；不能把删除 ANSI 后拼接的历史输出误当当前屏幕。

模型 View 的 ANSI 渲染另做颜色/宽度图像检查，诊断图保存在忽略的 `.test-artifacts/`。这些是模型渲染检查，与真实 PTY 运行证据分开。

实测平台仍为宿主 macOS/arm64 和专用 OrbStack Debian 12/arm64。尚未在 Linux/amd64、其他发行版或最低 Go 1.26 工具链实跑；没有 macOS 内核安装或服务管理后端。真实网络可能遇到官方 API 限流或文件传输超时，此时记录失败阶段并清理，不切换未知镜像或跳过摘要。


最终本轮结果：以上宿主 check/race/build 与 Linux 全包测试均通过。v2 完整端到端以 `e2e.sh VM --proxy-mode environment` 通过，仍访问官方 API 和官方资产；没有 cache 或镜像 fallback。VM 的 No proxy 直连在文件下载出现超时，产品正确报告并清理；显式环境模式使用测试 VM 已有的标准代理变量，不读取 proxyctl 配置，不修改代理、DNS 或路由。日志只记录所选模式，不输出原始环境地址或凭据。

真实 PTY 包含默认 Overview、网络隐藏输入/草稿放弃、预安装导入、宽窄切换、Core/Version/Confirm/Installing/Result、本地安装、安装阶段 Esc 和 SIGTERM 清理，以及鼠标导航和 PROXY→REJECT 后实际 API 回读。真实更新从已运行 v1.19.32 安装 v1.19.33，断言 PID、API 报告的旧运行版本及配置字节不变；同版本复用与显式重装也保持原进程。最终 `cleanup.txt` 显示 resources released=1、exit=0，unit 为 not-found，7897/9097 释放，专用进程和临时目录不存在。

## 入口与业务边界整理（2026-10-29）

入口已移动到根目录 `main.go`，构建与 E2E 脚本统一使用模块根目录。CLI 在 `internal/cli` 内按命令职责拆文件；应用层在 `internal/app` 内按安装、安装记录、恢复、配置、运行及快照拆文件。首次安装的暂存、发布和服务注册分别执行，继续由同一锁和恢复日志保护。

本轮实际通过：

- macOS/arm64、Go 1.27.1：`make check`（全包测试与 vet）、`go test -race ./...`、`make build` 和根入口 `--help`。
- 新增安装边界测试：暂存不依赖服务，文件和配置验证后才注册最终路径，注册期间 journal 仍存在；暂存/注册失败或取消会清理，已有配置保持不变。
- `scripts/verify-linux.sh proxyctl-e2e`：Debian 12/arm64 内实际执行所有包测试，包含新增测试。
- `scripts/e2e.sh proxyctl-e2e --proxy-mode environment`：真实 PTY 三种模式、官方 Mihomo v1.19.32/v1.19.33、用户 systemd 生命周期、配置应用、节点切换、运行中更新/重装及取消恢复通过。
- 清理返回 `resources released=1`、`exit=0`，专用 unit 为 `not-found`；额外检查确认临时目录、测试进程已清除，7897/9097 可重新绑定。

服务范围仍为 Linux/systemd 用户服务。未新增 unisvc 依赖或其他平台后端，未扩大已实测平台范围。

## 运行监控与 Connections（2026-10-29）

本轮实际通过：

- macOS/arm64、Go 1.27.1：`make check`、`make race`、`make build`。业务测试使用临时目录和本地 HTTP fixture，验证真实累计来源、指标独立失败、取消关闭请求、采样不阻塞停止、停止/PID 变化/归属变化后丢弃旧样本及精确 ID 关闭。TUI 模型覆盖单任务刷新、迟到消息、搜索输入、列表重排和焦点、关闭确认、故障恢复以及既有尺寸矩阵。
- `python3 -m unittest discover -s scripts -p 'test_*.py'`：4 个 VT 协议回归测试，不启动子进程、服务或 VM。
- `scripts/verify-linux.sh lazyclash-e2e`：在本次显式选择的 Debian 12/arm64 专用 VM 内实际执行所有 Go 测试。普通用户、用户 systemd 可用，Linger=yes。
- `scripts/e2e.sh lazyclash-e2e --proxy-mode environment`：官方 Mihomo v1.19.32/v1.19.33 的下载/校验/安装/更新，以及 empty、install-local、installed、runtime 四类真实 PTY 场景通过。仍使用显式所选 VM 的既有标准代理变量，不读取旧代理配置或修改网络设置。
- 专用本地 HTTP fixture 接收经 Mihomo mixed 端口的两条独立连接，每条上传 256 KiB 并持续下载；断言实际正速率、累计增长和 RSS。CLI 关闭第一条时第二条仍存活，TUI 按完整 UUID 搜索、查看和确认关闭第二条，再从真实 API 回读；关闭后累计保留，内核停止后指标不可用，重新启动后累计为零且 TUI 自动恢复。
- 最终 `cleanup.txt` 为 `resources released=1`、`exit=0`，专用 unit 为 `not-found`，测试进程和目录清除，监听端口释放。诊断保存在忽略的 `.test-artifacts/`。

初次回归发现两处测试工具问题并已修复：端口探测使用与 Go 监听一致的 `SO_REUSEADDR`，容许已关闭连接的 TIME_WAIT，另行验证仍拒绝现存 listener；VT 屏幕解析补齐渲染器使用的滚动区域、插入/删除行、滚动和反向制表指令，保留当前屏幕断言。修复后完整端到端复跑通过，没有改用历史输出匹配掩盖显示问题。

本轮未实测 Linux/amd64、其他发行版、真实注销/重启后的持久运行或最低 Go 1.26 工具链。仅支持 Linux/systemd 用户服务，累计流量不跨内核重启持久化。

## 只读规则查看（2026-10-29）

已运行并通过：

- 宿主 macOS/arm64、Go 1.27.1：`make check`、`make race`、`make build`，以及 4 个 Python VT 协议测试和脚本语法检查。
- API fixture：真实规则 schema、原始顺序与不同 index 的重复规则、空 MATCH 条件、`size=-1/0`、可选统计、有效空数组、HTML/无效字段/HTTP 错误/超限/取消和脱敏。
- 应用用例：服务不可用时不访问 API，查询不持锁阻塞停止，跨停止/进程/安装身份/归属变化的旧规则被拒绝；CLI 输出顺序一致，查询不改配置或服务。
- TUI 模型：独立查询/五秒复查、重试及迟到消息、搜索文本、规则详情身份、索引复用、键鼠和窗口尺寸矩阵；额外检查 100×20 时第六导航不会被 Core 摘要覆盖。
- `scripts/verify-linux.sh lazyclash-e2e`：在显式选定的专用 Debian 12/arm64 VM 内实际执行全部 Go 测试。
- `scripts/e2e.sh lazyclash-e2e --proxy-mode environment`：真实官方 Mihomo v1.19.32/v1.19.33、systemd 和全部 PTY 模式通过。新增 rules 模式实际验证规则先后顺序、原 index、空 MATCH、搜索/详情/60×20 缩放/刷新、停止后隐藏旧列表与重启恢复；配置 hash 前后相同，无订阅默认配置的单条 Match 也可读取。

新增脱敏终端记录为 `.test-artifacts/pty-rules.txt`。本轮完整端到端首次执行通过，`cleanup.txt` 为 `resources released=1`、`exit=0`，专用 unit 为 `not-found`；额外检查确认临时目录、测试进程和监听端口已清理。

未扩大平台支持和实测范围。规则编辑、启用/禁用、提供器展开与配置覆盖仍未实现；列表显示内核已加载的规则，匹配顺序仅在 Rule 模式适用。


## Mariana + Adaptive 默认主题（2026-10-29）

来源、计算和颜色回退见 [主题映射](theme-mariana.md)。使用本机 Sublime Text 4 Build 4215 随附的 Mariana 与 Adaptive 资源；没有读取 Sublime 用户偏好。

本轮实际通过：

- macOS/arm64、Go 1.27.1：`make check`、`make race`、`make build`，Python VT 四项协议回归及新增预览/PTY 脚本语法检查。
- 模型：全部向导阶段、两种首页、网络结果、输入错误、禁用/选中/悬停/焦点；True Color、ANSI256、ANSI16、无色，指定尺寸及极小窗口。真实颜色配对检查正文、说明、选区、主按钮、错误和焦点的对比度。
- `scripts/theme-pty.py`：macOS/arm64 与显式选择的 OrbStack `lazyclash-e2e`（Debian 12/arm64，普通用户）的真实 controlling PTY。真彩、256 色、NO_COLOR 三种模式均完成五种尺寸、网络隐藏草稿、Tab 焦点、缩放保持、Esc 放弃与返回。输出检查无 OSC10/11 设置、256 色无 RGB、无色无实际 SGR 样式。
- `scripts/verify-linux.sh lazyclash-e2e`：Linux 全包测试二进制实际执行通过，暂存目录清理完成。
- `scripts/theme-preview.py`：导出真实模型 ANSI View，以 Menlo 实际字体生成独立 PNG，逐页检查首页、宽窄向导、网络结果、输入错误、下载、取消、成功/失败及256色回退。它们是模型排版预览，不能作为真实内核运行或服务验证证据。

证据位于忽略目录 `.test-artifacts/mariana/`；PTY 使用独立临时 home 和随机服务名，没有查询版本、安装内核或创建服务，临时进程/文件已清理。本轮主题修改未重跑真实 Mihomo/systemd 安装生命周期 E2E；此前记录单独保留。未逐一实测 Termius、iTerm2 等原生终端前端，16色仍受终端基础调色板影响。


## 七页导航、紧凑布局与实时日志（2026-10-29）

导航与实际单键快捷键统一为 `1 Overview`、`2 Proxies`、`3 Profiles`、`4 Connections`、`5 Rules`、`6 Logs`、`7 Settings`。侧栏数字为右侧提示，不属于页面名称；输入、搜索和帮助层中的数字按当前控件语义处理。当前页与键盘焦点保持独立，侧栏/列表各自作为焦点组。

本轮实际运行通过：

- `scripts/verify-linux.sh lazyclash-e2e`：在已有专用 OrbStack Debian 12/arm64 VM 中实际执行全部 Linux 测试二进制，普通 uid 501，systemd 252 用户 manager 正常；包含新日志协议、脱敏、取消、身份变化及布局/焦点测试。
- `scripts/e2e.sh lazyclash-e2e --proxy-mode environment`：完整 Mihomo/systemd E2E 通过（包含服务停止及恢复后的顶栏状态断言），仍由产品下载并验证官方 v1.19.32/v1.19.33；保留安装、更新、重装、配置、规则、连接及六种真实 PTY 场景。
- 新 Logs 实测：私有 fixture 配置显式启用 info 事件，经自有 loopback HTTP 流量产生真实 Mihomo TCP 日志；CLI 在条数/时间边界内输出日志，TUI 验证暂停、数字与 q 搜索、帮助层隔离、缩放、重连，以及停止/启动后收集包含新 source port 的真实事件。读取前后配置 SHA256 不变；没有启用系统 journal 或修改产品默认日志级别。
- `scripts/theme-pty.py`：macOS/arm64 和所选 Debian 12/arm64 的真实 controlling PTY，True Color、ANSI256、NO_COLOR 各覆盖 160×45、120×32、80×24、60×20、120×18，确认新布局不破坏网络草稿、焦点和返回。
- 现有 Python VT 协议四项测试、Python 语法检查、Bash 语法检查和差异空白检查通过。

证据单独保存到忽略目录 `.test-artifacts/ui-layout/`，包括 `linux-tests.log`、`e2e.log`、`pty-logs.txt`、`cleanup.txt` 和主机/Linux 颜色 PTY 记录。E2E 清理确认 dedicated unit 为 not-found、两个监听端口释放、`resources released=1`、exit=0；主机/Linux 测试与 PTY 暂存文件、进程已清理。没有读取 proxyctl 运行配置，没有修改 macOS 代理、DNS、路由或 Shell 文件。

日志行为与边界见 [实时日志查看](logs.md)。这些日志仅为当前会话实时观察，暂停或离页期间没有历史补采。未实际验证 Linux/amd64、其他发行版或其他 init 系统；没有把 macOS PTY 或交叉编译当作 macOS 服务支持证明。

## 运行端口设置（2026-10-29）

共享读取、预检查与事务应用用例，以及 TUI/CLI 操作见 [运行端口设置](runtime-settings.md)。普通测试使用临时目录、替代服务和本地 HTTP fixture；真实服务验证仍是显式入口。

本轮实际运行：

- 宿主 macOS/arm64、Go 1.27.1：最终源代码的 `make check`（全包测试与 vet）、`make race`、`make build` 通过。
- `scripts/verify-linux.sh lazyclash-e2e` 在最终源代码上重新构建并实际执行全部 Linux/arm64 测试，通过；包含普通 Start 的 UDP-only 冲突、占用默认端口安装提示、冲突后纠正和迟到消息不能提前退出清理等新增回归。
- `scripts/e2e.sh lazyclash-e2e --proxy-mode environment` 完整通过。在专用 OrbStack Debian 12/arm64、普通 uid 501、systemd 252 用户 manager 中，使用官方下载并校验的 Mihomo v1.19.32/v1.19.33；未读取已有代理配置，也未修改网络环境。
- 安装向导在自有 TCP/UDP fixture 占用默认端口时仍可安装，服务保持停止；明确 Start 因冲突失败，原 fixture 仍存活。
- CLI 分别验证 mixed TCP/UDP 和 controller TCP 冲突不修改文件或停止进程、dry-run、运行态重启确认、无变更不重启、运行态移动与自有端口互换、停止态保持停止。端口动态分配并记录供失败清理，不停止任何外部占用者。
- 端口变更以当前已应用 YAML 为基础：刻意修改已保存但未重新应用的 profile，证明配置其余字节、原始 profile、安装身份、API key 和 unit 保持不变；之后明确 Apply profile 仍保留新端口。实际请求验证新代理端口、新认证 API、`env` 输出和旧端口释放。
- `runtime-settings` 真实 controlling PTY 验证取消草稿、数字/q 输入、端口冲突后纠正、审阅不提前提交、明确 Apply & restart、120×32 / 80×24 / 60×20 / 120×18 / 120×40 缩放保留草稿，并在 Proxies、Rules、Logs、Overview 重新连接新端口；实际 HTTP 流量生成日志，未使用假数据。
- Python 四项 VT 回归、Python/Bash 语法检查通过。首次端口 PTY 运行暴露测试脚本的错误焦点假设：预检查失败已返回首个输入框，脚本仍发送 Shift+Tab；修复后增加新输入可见及错误清除的当前屏幕断言，完整端到端复跑通过。

证据在忽略的 `.test-artifacts/`：`e2e.log`、`pty-runtime-settings.txt`、`cleanup.txt`。最终清理为 `resources released=1`、`exit=0`，专用 unit 为 not-found，默认和动态 TCP/UDP 测试端口释放；额外检查专用测试目录及进程已消失。应用失败、取消、恢复失败和用户修改保护由可控模型/应用测试验证，未声称真实 VM 中注入过全部故障。实测平台未扩大到 Linux/amd64、其他发行版或其他 init。

完整 E2E 的二进制构建早于最后两项小修：将普通 Start 的端口检查统一扩展到 UDP、清除已结束预检查的底栏消息。两项都包含在随后最终源代码的 Linux 全包测试中；未因此重复声称又跑了一轮完整 E2E。

## 持久生命周期与权限边界（2026-10-29）

本轮实现范围为 Linux/systemd 用户服务/Mihomo 的普通本机代理，不包含特权 TUN 或系统级服务。设计和可重复步骤见 [持久运行](runtime-lifecycle.md) 与 [权限方案](permissions.md)。

最终源代码验证：

- 宿主 macOS/arm64：`make check`（全包测试及 vet）、`make race`、`make build` 全部通过；Python 四项 VT 回归、Python/Bash 语法检查与 `git diff --check` 通过。
- 复用已运行的专用 `lazyclash-e2e` Debian 12/arm64 VM：uid 501 普通用户，systemd 252，用户 manager running、Linger=yes。`verify-linux.sh lazyclash-e2e` 重新构建并实际执行全部 Linux 测试二进制，通过。
- `e2e.sh lazyclash-e2e --proxy-mode environment` 完整通过。使用官方 SHA256 校验的 Mihomo v1.19.32/v1.19.33；保留安装、更新、重装、订阅、配置、节点、连接、规则、日志、端口和真实 PTY 场景，增加记住来源后的 Refresh、不改变活动配置的刷新、停止态卸载和保留用户数据。
- 新 `lifecycle-e2e.py` 实测：真实 controlling PTY 发送 SIGHUP 后内核 PID 不变；模式通过动态 API 应用不重启；配置应用和显式重启恢复模式/节点；对已验证自有 PID 发送 SIGKILL 后 systemd 自动恢复，新 PID、恢复计数、模式与节点均实际读回；Enable/Disable 不改变当前 PID；明确 Stop 超过 RestartSec 后仍为 PID=0 和 inactive。
- Linux 文件 capabilities 用例因普通测试用户没有设置 `security.capability` 的权限明确 skip。setuid/setgid 拒绝路径实测通过，不能声称已对带真实 file capabilities 的二进制完成本轮运行验证。

真实 E2E 首轮暴露“API 可达但代理监听尚未准备好”的启动窗口，已在应用就绪检查增加 mixed TCP 监听探测并增加回归，最终端口互换与全套 E2E 通过。Mihomo 恢复时节点缓存加载可能晚于 API 首次可读，恢复验收在 15 秒内等待实际期望节点出现，未将首次快照中的默认选择直接视作持久化成功或缓存丢失。

记录保存在忽略目录 `.test-artifacts/`：`e2e.log`、`linux-lifecycle-tests.log`、各 `pty-*.txt` 和 `cleanup.txt`。最终清理确认专用 unit 为 not-found、默认及动态 TCP/UDP 监听均释放、`resources released=1`、`exit=0`。未修改主机系统代理、DNS、路由、Shell 配置或 VM 的 linger 策略。

未实测最后一个真实 SSH 登录会话退出、机器重启和 Linux/amd64 运行；单元测试、PTY SIGHUP、linger 检查和交叉编译都不能替代这些验收。文档提供专用机器上的 SSH/logout/boot 步骤。TUN、路由/DNS 接管、系统级部署及其停止清理尚未实现，不存在“模拟测试通过所以真实 TUN 已通过”的结论。

## 顶部通知条（2026-10-31）

- 宿主 macOS/arm64：`make check`、`make race`、`make build` 和四项 Python VT 回归通过。模型测试覆盖新旧通知计时隔离、同文案重复反馈、任务取消、弹窗去重、表单校验取消、Esc 优先级与关闭后的焦点恢复。
- `scripts/verify-linux.sh proxyctl-e2e`：先检查已有专用 Debian 12/arm64 VM 的普通用户、systemd 用户 manager 和 Linger 状态，再在 VM 内实际执行全部包测试，通过。
- `scripts/theme-pty.py`：在隔离数据目录和随机服务名下，使用真实 controlling PTY 验证错误超过五秒仍保留、鼠标/Esc 关闭、相同错误再次出现、输入取消提示自动收起，以及既有向导和隐藏输入。真彩、256 色和 NO_COLOR 三种模式均覆盖 160×45、120×32、80×24、60×20、120×18；当前屏幕记录保存在忽略目录 `.test-artifacts/notifications-pty/`。
- 通知模型另覆盖中文长文案、三行裁切及 20×7、10×4、1×1 极小窗口；`scripts/theme-preview.py` 导出的宽窄通知 PNG 用于检查字体排版，属于模型预览。

本轮未重跑真实 Mihomo 安装及 systemd 生命周期 E2E；PTY 使用未安装实例，不创建服务、不查询发布版本或修改系统网络。

## Toast 浮层（2026-10-31）

全局通知改为右上角 Toast，不再挤占正文高度。宿主 `make check`、`make build`、四项 Python VT 回归，以及 `scripts/verify-linux.sh proxyctl-e2e` 的 Linux 全包运行测试通过。新增回归验证正文几何、总行数和滚动位置稳定，覆盖区域不接收底层点击，订阅 URL 和搜索输入的避让，以及输入框部分滚出视口、短页面越界滚动裁切后的反馈。

更新后的 `scripts/theme-pty.py` 在真彩、256 色和 NO_COLOR 三种模式下通过 160×45、120×32、80×24、60×20、120×18 的真实终端验证，覆盖持久错误、键鼠关闭、重复反馈和自动收起；记录保存在 `.test-artifacts/toast-pty/`。宽窄 PNG 仍为模型预览。本次未重跑真实 Mihomo/systemd 生命周期 E2E。

## 多条 Toast 与底部空间回收（2026-11-01）

通知固定从页头右侧展开，宽屏最多两条、紧凑窗口最多一条；溢出进入有边界的列表，详情使用 Charm Bubbles viewport。正文回收原底部状态栏预留的一行，按键提示固定在终端最后一行，侧栏、紧凑导航遮挡区域和滚动边界同步。

本轮最终源代码验证：

- 宿主 macOS/arm64：`make check`（全部 Go 测试与 vet）、`make race`、`make build` 通过；Python 四项 VT 协议回归、脚本语法与 `git diff --check` 通过。
- 模型覆盖独立操作身份、同操作进度/结果/重试、相同错误不合并、按条目关闭、旧定时器隔离、成功 3 秒与信息 4 秒、隐藏/溢出/弹窗暂停计时、错误跨页保留、输入校验留在局部，以及通知列表、完整详情、键盘焦点和鼠标遮挡。受控故障测试覆盖恢复失败、最终状态回读失败时显示未知、弹窗关闭后保留严重错误，以及端口已提交但回读失败时仍保留已应用结果。
- 新底部布局回归覆盖 160×45、120×32、100×20、80×24、60×20、120×18、30×12、20×7，断言正文/滚动指示/鼠标命中到达倒数第二行、末行按键提示独立、侧栏填满正文高度、紧凑导航末行不能点击穿透；另检查 120×25 的侧栏边界。
- `scripts/verify-linux.sh proxyctl-e2e`：复用已有专用 Debian 12/arm64 VM，普通 uid 501、systemd 用户 manager running、Linger=yes；实际执行全部 Linux Go 测试通过，包含最终两项底部布局回归的全部尺寸。最终记录为 `.test-artifacts/notification-stack/footer-linux-tests.log`，此前通知集合验证为同目录 `linux-tests.log`。
- `scripts/theme-pty.py`：真彩、ANSI256、NO_COLOR × 160×45、120×32、80×24、60×20、120×18 的真实 controlling PTY 通过，覆盖独立同文案操作、顺序与溢出、Ctrl+N/鼠标展开、指定关闭、跨页保留、局部校验修改与离页、弹窗后计时及既有焦点和草稿行为。最终布局复跑逐一检查 15 份当前屏幕，确认 `q Quit` 固定末行，滚动标记位于倒数第二行。证据分别为 `.test-artifacts/notification-stack/pty/` 与 `footer-pty/`，隔离暂存资源已清理。
- 以最终实际 `Model.View()` 导出 ANSI，使用 Menlo 重新渲染并检查 120×32 的多条 Toast、局部输入校验和 60×20 的紧凑 Toast PNG。它们是模型排版预览，不作为真实服务运行证据。

本轮未重跑真实 Mihomo/systemd 生命周期 E2E；PTY 使用隔离的未安装实例，不安装内核或创建服务。恢复和状态回读故障来自受控模型测试，没有声称在 VM 内注入所有真实故障。实测平台范围未扩大。

## 侧栏 Logo 首版呼吸与点击彩蛋（2026-11-02）

初始状态、实际终端尺寸与色彩能力就绪约 200ms 后，常驻侧栏 Logo 以青色轻呼吸一次，约 800ms 内渐亮再恢复，不显示梦泡。每次启动仅一次，不因切页、缩放或关闭弹窗而重播；初始紧凑布局、加载失败、遮挡、无色及 16 色模式跳过。点击仍播放约 1.6 秒的原位变化与上方梦泡，字标宽度及菜单位置固定，20～23 行省略梦泡。没有适合此类品牌帧动画的 Bubbles 组件，使用封装的 Bubble Tea 定时消息；播放结束或被打断后不再安排动画消息。

本轮实际通过：

- 宿主 macOS/arm64：`make check`（全包测试与 vet）、`make build`、4 项 Python VT 协议回归及 Logo 验证脚本语法检查。
- 模型回归：首次尺寸、颜色和加载结果的六种到达顺序，200ms 等待与 800ms 呼吸、颜色升降、自然结束及各类跳过/打断后的不补播，等待和呼吸中的点击抢占、过期帧丢弃；另保留点击剧情的固定字标与梦泡坐标、总时长、重复点击重播、宽窄缩放，以及忙碌、退出、帮助、表单、通知列表和操作菜单打断。Logo 可点击但不增加 Tab 停靠点。
- `scripts/verify-linux.sh lazyclash-e2e`：在已有专用 Debian 12/arm64 VM 内实际执行全部可运行的 Go 测试，未启用需显式环境变量的真实 Mihomo 集成测试。
- `scripts/logo-demo.py`：macOS/arm64 与所选 Linux/arm64 的真实 controlling PTY 均验证 160×45、120×24、100×20，分别捕获 14、14、7 帧；检查 `la` 与 `yClash` 的字符格位置不变、没有尾部追加文字、梦泡逐格移动及最终恢复静态。
- `scripts/logo-intro-pty.py`：最终实现分别在 macOS/arm64 与所选 Linux/arm64 真实 controlling PTY 通过 160×45、120×24、100×20；实际采样 SGR 前景色，观察到基色与 20 帧变化，约 213～231ms 首次变亮、约 979～1021ms 恢复基色，峰值为 `#b7e5f7`，字符始终不变且没有进场梦泡。切页、缩放、关闭帮助后不重播，随后点击剧情完整通过；另验证 80×24 启动的静态标题 Logo 扩大到 160×45 后不补播。

可重复的宿主 PTY 入口：

```sh
make build
python3 scripts/logo-demo.py --binary bin/lazyclash \
  --output .test-artifacts/logo-wake-dream-pty \
  --size 160x45 --size 120x24 --size 100x20
python3 scripts/logo-intro-pty.py --binary bin/lazyclash \
  --output .test-artifacts/logo-intro/macos
```

最终进场验证的主机检查与 Linux 全包测试日志保存在 `.test-artifacts/logo-intro/`，其中 `macos/` 与 `linux/` 保存各尺寸的颜色观察、当前屏幕和原始 ANSI；每次脚本运行生成独立目录。此前点击剧情记录保留在 `.test-artifacts/logo-wake-dream/` 与 `.test-artifacts/logo-wake-dream-pty/`。PTY 使用隔离的未安装数据目录，VM 临时目录已清理；本轮未重跑 Mihomo/systemd 安装生命周期 E2E。

## Logo 原位翻身进场（2026-11-02）

进场在界面就绪约 200ms 后播放一次约 800ms 的“翻个身，接着睡”：原 emoji 的两格依次显示 `z `、` Z`、💤，两侧文字固定，同时青色渐亮至 `#c5efff` 再恢复。使用 40ms 帧间隔，总时长保持 800ms；进场不冒梦泡，点击仍播放完整的 1.6 秒惊醒与梦泡剧情。首次显示条件、取消与只播放一次的规则沿用首版。

本轮实际通过：

- 宿主 `make check`、`make build`、4 项 Python VT 协议回归及脚本语法检查。
- 模型验证完整的进场字形序列、中心两格与字标宽度固定、无进场梦泡、青色升降及恢复；保留初始化乱序、首次跳过、取消、点击抢占、旧消息隔离和点击剧情回归。
- `scripts/verify-linux.sh lazyclash-e2e`：在已有专用 Debian/arm64 VM 内实际执行全部可运行的 Go 测试，包含更新后的进场与生命周期回归。
- 更新后的 `scripts/logo-intro-pty.py` 分别在 macOS/arm64 和 Linux/arm64 的真实 controlling PTY 验证 160×45、120×24、100×20。各尺寸均捕获 `💤 → z → Z → 💤`，实际青色峰值为 `#c5efff`，随后恢复 `#85b3e0`；没有进场梦泡或字标偏移。切页、缩放与关闭帮助后不重播，原点击剧情完整通过；80×24 启动后扩大到 160×45 不补播。

记录位于 `.test-artifacts/logo-sleepy-entry/`：主机检查和 Linux 全包测试日志，以及 `macos/`、`linux-pty/` 下逐尺寸的中心字形、颜色、当前屏幕和原始 ANSI。仍可通过上一节的 `scripts/logo-intro-pty.py` 命令复验，使用新的输出目录即可。PTY 使用隔离的未安装实例，VM 暂存目录已清理；本轮未重跑真实 Mihomo/systemd 安装生命周期 E2E。

## 首次安装入口（2026-11-02）

未安装时，首页只突出安装 Mihomo，默认聚焦主按钮，按一次 Enter 打开现有安装向导；启动 TUI 不自动下载或安装。“稍后设置”只跳过当前会话的准备页，不写入永久关闭引导偏好。安装后的内置默认配置属于正常可用状态，可由用户明确启动；订阅和本地 YAML 均为可选能力，不构成必经步骤。

本轮实际验证：

- 宿主 macOS/arm64：最终源代码的 `make check`（全部 Go 测试及 vet）通过；本轮较早版本的 `make race` 通过。新增模型测试覆盖初始化消息乱序、一次 Enter 仅打开向导、迟到结果不抢焦点、会话跳过、无订阅默认配置就绪与明确启动，以及安装完成后保持停止。
- `scripts/verify-linux.sh lazyclash-e2e`：在已有专用 Debian/arm64 VM 内实际执行最终源代码的 Linux 全包测试，通过；不是仅做交叉编译。
- `scripts/onboarding-pty.py`：宿主中英文 × True Color、ANSI256、NO_COLOR × 160×45、120×32、80×24、60×20、120×18 的真实 controlling PTY 通过；Linux 中英文 True Color 也通过上述五种尺寸。最终界面另在中英文 True Color 下复验 160×45、120×32、80×24、60×20，检查首屏、安装向导入口、临时跳过和配置页。PNG 根据实际 PTY 字符格和颜色重绘，用于排版检查，不是原生 Ghostty 截图。
- `scripts/pty-smoke.py` 已同步现有页脚、单来源导入、默认配置名称和导航交互，并修正等待初始快照的竞态；E2E 的预导入检查按配置内部 ID 验证，不再把自动生成的显示名当作 ID。独立 Linux `empty` 模式通过，保留凭据隐藏、预安装导入、缩放、Esc 取消和 SIGTERM 清理场景。

最新完整 `e2e.sh lazyclash-e2e --proxy-mode environment` 尝试通过了 `empty` PTY，但在官方 Mihomo 发布元数据查询阶段因网络连接失败中止；后续只读直连 `curl` 诊断返回 HTTP 403。本轮没有完成该版本的真实安装、启动及后续 systemd 生命周期验证，不把模型测试和入口 PTY 视为这些场景的通过证据。清理记录为 `resources released=1`、`exit=1`，保留了本次 E2E 失败状态。

记录保存在忽略目录 `.test-artifacts/onboarding/`，包括最终宿主与 Linux 检查日志、较早的 race 日志、中英文各颜色/尺寸的当前屏幕与 ANSI、重绘 PNG，以及 E2E 输出和清理记录。验证使用隔离数据目录和随机服务名，临时资源已清理；实测平台范围未扩大。

## 单页安装与设备构建选择（2026-11-02）

首次打开安装窗口时直接显示版本与下载设置，复用 `CheckUpdate` 查询稳定版，不再要求确认唯一的 Mihomo 内核选项。用户明确点击安装后，使用已查到的固定版本依次解析官方资产、预检并安装；取消或修改选项会清除这次安装意图，迟到结果不能触发安装。已有实例通过手动入口重装、降级或更换本地文件时仍保留计划审阅，预检阻止项不能绕过。

设备构建选择继续限定为已有 Linux/arm64 与 Linux/amd64。arm64 使用原生构建；amd64 根据每个可见 CPU 的指令集交集及操作系统 AVX 状态支持，选择可用的最高安全 v3、v2 或 v1，信息不完整时保守选择 v1。旧版无标签 amd64 只在已确认 v3 能力时使用，`compatible` 作为 v1 回退；初装、指定版本和升级共用该解析器。参考 [Mihomo FAQ](https://github.com/MetaCubeX/mihomo/wiki/FAQ) 与 [Go 最低要求](https://go.dev/wiki/MinimumRequirements#amd64)，没有凭 CPU 型号推测能力，也没有扩大其他架构或旧 Linux 内核的支持范围。

本轮实际验证：

- 宿主 macOS/arm64：最终源代码的 `make check`（全部 Go 测试及 vet）通过，记录为 `.test-artifacts/install-flow/check-final.log`。
- `scripts/verify-linux.sh lazyclash-e2e`：在已有专用 Debian/arm64 VM 内实际执行最终源代码的 Linux 全包测试，通过；记录为 `.test-artifacts/install-flow/linux-final.log`。
- Mihomo 及 TUI 的 Install、CoreUpdate、Wizard、Resolve 相关 race 测试通过，记录为 `.test-artifacts/install-flow/race-final.log`；不是本轮全包 race 的结论。设备选择测试覆盖全部 v2/v3 必需位缺失、系统未启用 YMM 状态、多核能力不一致、CPU 信息缺失，以及资产缺少时降级和禁止向上误选；AMD64 测试二进制另通过交叉构建，未声称已在真实 AMD64 设备运行。
- Python 四项 VT 协议回归通过。真实产品二进制使用 environment 连接模式查询官方稳定版本，得到 `v1.19.33`，查询后隔离数据目录仍不存在，未创建安装或服务。
- 最终 `make build`、Python 脚本语法和 Shell 语法检查及差异空白检查通过。
- 更新后的 `scripts/onboarding-pty.py --test-binary` 在宿主完成中英文 × True Color、ANSI256、NO_COLOR × 160×45、120×32、80×24、60×20、120×18 共 30 场景；在专用 Linux/arm64 VM 实际完成中英文 × True Color × 120×32、80×24、60×20 共 6 场景。每场明确记录 `checks=1, resolves=1, prepares=1, installs=1`，验证一次安装操作串联、取消等待，以及返回后的会话跳过与浏览。后端是明确启用的内存 fixture，没有真实下载、执行 Mihomo 或创建服务。主机记录在 `.test-artifacts/install-flow-pty/final/20261102-140846-33c0eb/`，Linux 记录在 `.test-artifacts/install-flow-pty/linux/evidence/20261102-141332-16465f/`；已目视检查 Linux 120×32、60×20 安装页 PNG。VM 私有暂存目录已清理并复查不存在。

最新完整 `e2e.sh lazyclash-e2e --proxy-mode environment` 尝试通过 `empty` PTY 后，在官方固定版本 release API 查询阶段收到 HTTP 403；清理记录为 `resources released=1`、`exit=1`。本轮未完成真实内核下载安装、启动及后续 systemd 生命周期验证，不以版本查询、模型测试或界面预览代替这些证据。

证据保存在忽略目录 `.test-artifacts/install-flow/`。终端 PNG 按实际 PTY 字符格与颜色重绘，安装进度和结果的内存替代后端预览只用于界面检查；两者均不能作为实际下载或服务运行证据。测试使用隔离数据目录和专用资源，实测平台范围未扩大。
