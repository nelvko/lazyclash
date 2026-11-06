# Mariana 默认主题

LazyClash 的稳定主题标识为 `mariana`，显示名称为 Mariana。它以 Mariana 的正文配色和 Adaptive 的界面层次为参考，适配终端字符单元；不是 Sublime Text 的像素复刻。此前没有可保存的主题选项，因此本次直接更新默认配色，不增加主题选择器或更改已有偏好文件。

## 原始资源与版本

本次直接只读检查本机 Sublime Text 4 **Build 4215** 随附资源，没有读取 Sublime 用户偏好或自定义覆盖文件：

- `/Applications/Sublime Text.app/Contents/Info.plist`：`CFBundleVersion=4215`。
- `Contents/MacOS/Packages/Color Scheme - Default.sublime-package` 内 `Mariana.sublime-color-scheme`，作者字段：Sublime HQ Pty Ltd, Dmitri Voronianski。
- `Contents/MacOS/Packages/Theme - Default.sublime-package` 内 **`Adaptive.sublime-theme`**。包名称包含 Default，但读取的界面主题是 Adaptive，不是 Default 或 Default Dark。

SHA-256：

| 对象 | SHA-256 |
| --- | --- |
| Mariana 资源内容 | `b26ba885408a51cda462d5fa890e45252b3f440f1cbf3f94f1e6888bd6d42db9` |
| Adaptive 资源内容 | `4f54692826579f1c1ca04c1c134916c6674717b9f7f4a5e087b8d0bf6f612a95` |
| Color Scheme - Default 包 | `b9ad09699425506b40562398fc6136c91996a7ae86b922d8a85403a0b673e5cf` |
| Theme - Default 包 | `26d648cdfd1781e005e89dab96244a5a9a04d72300f993c0534efa00eb3a84f5` |

原始值来自这些资源，官方文档仅用于解释规则：[配色方案](https://www.sublimetext.com/docs/color_schemes.html)、[主题与亮度属性](https://www.sublimetext.com/docs/themes.html#luminosity)、[minihtml 颜色运算](https://www.sublimetext.com/docs/minihtml.html#color-mod-function)。Sublime 文档指向的 [2016 年 CSS 草案](https://www.w3.org/TR/2016/WD-css-color-4-20160705/#blend-adjuster) 解释了 blend 权重方向。

## 换算规则

A：原资源直接提供的不透明颜色，HSL 到 RGB 仅转换表示方式。B：按原资源运算或透明度合成得到的不透明色。C：为终端用途、对比度或统一状态所作的明确适配。

HSL 使用标准 sRGB HSL 转换。中间步骤保留浮点精度，最终 0–255 通道四舍五入，恰好半值向上。例如 `hsl(210, 60%, 70%)` 绿色通道为 178.5，取 179。色值可能因此与提前取整的网络色板差一个通道单位。

`color(base blend(other p%))` 在未指定色彩空间时使用 RGB：`result = p × base + (1-p) × other`，这里 p 是保留 **base** 的比例。它与“不透明度 p 的 other 覆盖 base”含义相反。透明覆盖使用 `result = alpha × foreground + (1-alpha) × background`；若 layer 另有 opacity，则乘入 alpha。终端输出全部为不透明颜色，无真实透明遮罩。

Mariana 的背景变量 `blue3 = hsl(210, 15%, 22%)` 未取整 RGB 为 `(47.685, 56.1, 64.515)`。其 HSV V 为 0.253，因此根据 Sublime 文档属于 `file_medium_dark`；Adaptive 的 sidebar 和 quick_panel 使用 `medium_dark_bg`。

原始界面关系也核对了以下规则：

| 原始位置 | 原始表达式 | 在 Mariana 背景上换算 |
| --- | --- | --- |
| 正文背景 / 正文 | `var(blue3)` / `var(white3)` | `#303841` / `#d8dee9` |
| 注释 / 次要文字参考 | `var(blue6) = hsl(221, 12%, 69%)` | `#a6acb9` |
| 文本选区 | `var(blue2) = hsla(210, 13%, 40%, 0.7)` | 覆盖正文背景得到 `#4c5864`；未指定 selection_foreground，继承正文 |
| 选区边界 | `var(blue4) = hsl(210, 13%, 45%)` | `#647382` |
| 侧边栏 / quick_panel | `medium_dark_bg = color(var(--background) blend(black 60%))` | `#1d2227` |
| 侧边栏普通文字 / 选中文字 | `color(var(--background) blend(white 20%))` / `white` | `#d6d7d9` / `#ffffff` |
| 侧边栏 heading | `color(var(--background) blend(white 45%))` | `#a2a5a9` |
| 侧边栏悬停 | white `a(0.06)`，layer opacity 1 | 覆盖侧边栏得到 `#2a2f34` |
| 侧边栏选中 | white `a(0.18)`，layer opacity 0.6 | 有效 alpha 0.108，覆盖侧边栏得到 `#353a3e` |
| 弹出列表选中 | white，layer opacity 0.15 | 参考透明叠加关系；LazyClash 统一使用文本选区角色，避免每种列表另设颜色 |
| 输入控件 modifier | `l(- 5%) s(* 50%)` | 若作用于 Mariana 背景：HSL `(210, 7.5%, 17%)` → `#282b2f` |
| Adaptive 浅层底色 | `light_bg = color(var(--background) blend(black 90%))` | `#2b323a` |
| auto-complete 暗色表面 tint | white `a(0.05)` | 覆盖正文背景得到 `#3a424a` |

输入 modifier 在原主题中受 inactive sheet 规则约束；LazyClash 将计算结果复用于所有输入框。`light_bg` 是原资源变量名，并不表示 LazyClash 新增浅色主题。卡片、弹窗等角色的复用见下表；没有声称它们与 Sublime 每个控件完全相同。

## LazyClash 语义映射

所有值集中在 `internal/tui/theme.go`，不在业务页面定义十六进制颜色。不同语义允许共用值，但不将“成功”“焦点”“普通标题”混为同一角色。

| LazyClash Token | 最终值 | 类别与依据 |
| --- | --- | --- |
| `bg` | `#303841` | A，Mariana `background=var(blue3)` |
| `panel` | `#2b323a` | B 计算 / C 用途，Adaptive `light_bg`，用于克制的卡片底色 |
| `raised` | `#3a424a` | B 计算 / C 用途，Adaptive white alpha .05 覆盖正文背景，用于弹窗 |
| `sidebarBackground` | `#1d2227` | B，Adaptive `medium_dark_bg` |
| `inputBackground` | `#282b2f` | B 计算 / C 用途，Adaptive 输入 modifier 作用于 Mariana 背景 |
| `text` | `#d8dee9` | A，Mariana `foreground=var(white3)=hsl(219,28%,88%)` |
| `muted` | `#a9afbc` | C，`blue6` 亮度 69% → 70%；原值在 raised 上仅 4.481:1，为说明文字达到 4.5:1 而提高 1 个百分点 |
| `disabled`、`backdropText` | `#7b8896` | C，终端禁用及非交互背景弱化色；不用于正常说明、路径、版本或错误 |
| `border` | `#647382` | A，Mariana `selection_border=var(blue4)`，用作控件边界 |
| `divider` | `#444c54` | B 计算 / C 用途，Adaptive black/white tint 体系，white alpha .10 覆盖正文背景，用作装饰分隔线 |
| `selectionBackground` | `#4c5864` | B，Mariana 的半透明选区覆盖正文背景；列表及弹出菜单当前项使用该不透明结果 |
| `selectionForeground` | `#d8dee9` | A，Mariana 正文；明确配对，不继承任意终端颜色 |
| `hoverBackground` | `#3c444c` | B 计算 / C 用途，Adaptive sidebar hover white alpha .06 覆盖正文背景；用于按钮、列表及弹出菜单悬停 |
| `accent`、`info` | `#85b3e0` | C，原 `blue-vibrant=hsl(210,60%,60%)` 亮度提高到 70%，使交互提示在 raised 上可读 |
| `focus` | `#f9ae58` | A 颜色 / C 用途，Mariana caret 的 `orange=hsl(32,93%,66%)`，用于键盘焦点 |
| `primaryButtonBackground` | `#5c99d6` | A，Mariana 原始 `accent=blue-vibrant` |
| `primaryButtonForeground` | `#1d2227` | B 颜色 / C 用途，与按钮底色配对，以深色正文保证对比 |
| `success` | `#99c794` | A 颜色 / C 语义，Mariana `green=hsl(114,31%,68%)` |
| `warning` | `#f9ae58` | A 颜色 / C 语义，Mariana `orange`；通过警告文字与焦点符号区分用途 |
| `errorColor` | `#f28d92` | C，Mariana `red=hsl(357,79%,65%)` 亮度提高到 75%，保证错误原因在 raised 上可读 |

未安装、未配置、没有数据使用中性色与文字，不等同错误。成功、失败、进行中均保留文字。当前页选中和键盘焦点采用独立角色与非颜色标记。Modal 弱化只改变实际输出的前景与背景，不混合真实终端画面。

Logo 使用加粗和 `accent` 强调色，与普通菜单文字形成层级；沿用终端字符尺寸。常驻侧栏 Logo 区使用 `sidebarBackground` 深色背景，右侧标题区及窄窗口 Menu 按钮周围使用与正文一致的 `bg`，常驻侧栏保留独立深色背景。页面标题加粗，右侧状态统一使用次要文字色 `muted`，操作按钮保留自身状态样式。Logo 与标题共用高度与文字基线，上下留白对称。常驻侧栏第一项菜单直接接在 Logo 区底边；右侧标题分隔线采用贴字符格顶部的 `▔` 和低对比度 `divider`，与该底边对齐，从侧栏右边界贯穿至终端右边界（无侧栏时铺满全宽），不受正文内边距和最大宽度影响；正文紧接分隔线下一行，较原布局减少一行留白。弹出菜单保留内部滚动提示区，不覆盖顶栏。侧栏底部仅展示上传、下载速率，两行使用中性色箭头与右对齐的正文色数值；缺少实时数据时显示 `--`，真实零速率显示 `0 B/s`。速率只在菜单下方留白足够时显示，常驻侧栏跨页面保持采样，弹出菜单不展示速率。

首次进入 TUI，实际终端尺寸、色彩能力与初始状态加载就绪约 200ms 后，常驻侧栏 Logo 播放一次约 800ms 的“翻个身，接着睡”。原 emoji 的两格依次显示 `z `、` Z`、💤，保持两侧文字固定；青色从 `accent` 平滑变亮至 `#c5efff` 再恢复，不显示梦泡。初始侧栏不可见、加载失败、被遮挡或低于 256 色时跳过，不在之后扩大窗口、切页或关闭弹窗时补播。播放或等待期间，切页、缩放及遮挡会结束本次进场，点击则立即切入完整剧情。

常驻侧栏 Logo 的点击彩蛋持续约 1.6 秒：以暖色 `focus` 惊醒为 `!!`，恢复 `accent` 后依次显示 `..`、`z `、` Z`、💤，只替换原 emoji 的两格，保持字标宽度与位置。终端至少 24 行时，梦泡在原 emoji 上方留白中逐格向右移动；第一口使用 `accent`，第二口使用更淡的 `disabled`。20～23 行只播放原位变化，不占用菜单空间。动画不循环，再次点击从头播放；忙碌、弹窗或侧栏隐藏时结束，不新增键盘 Tab 停靠点。

常驻侧栏宽 20 列，通过背景色差与正文分区，不绘制竖向分隔线。菜单标签本身居中，键盘焦点在标签前独立两列显示橙色 `› `，不参与标签居中计算。当前页使用正文 `bg` 并整行延伸到右侧边缘，菜单名加粗；普通彩色模式不再叠加圆点和下划线。未选中项悬停仅提亮文字，保留侧栏原底色。侧栏方向键同步切页，焦点进入正文后菜单仍显示当前页背景。宽度不足 100 列或高度不足 20 行时，常驻侧栏自动收起；导航焦点转到顶部菜单按钮，正文焦点保持原位，不自动打开遮挡正文的抽屉。用户明确打开的抽屉在紧凑布局内缩放时保持，恢复宽屏后归入常驻侧栏。窄窗口弹出菜单保留右边框、独立选区及悬停背景。低色和无色模式在标签后显示 `•` 区分当前页，圆点同样不参与居中；焦点、悬停、当前页及颜色能力变化均不移动标签。标签裁切始终预留两侧装饰空间；极窄弹出菜单可以裁掉装饰，但不越过边框。

菜单项固定为三行并连续排列，相邻点击区域共用边界，不设置项间或分组空行；上下留白包含在菜单内部。短窗口通过独立的 Bubbles viewport 显示完整菜单项，并在视口外显示上下溢出箭头，不压缩常规菜单。鼠标滚轮每次移动一个菜单项，不切页、不滚正文，状态刷新保留浏览位置；侧栏 ↑↓ / j/k / Home / End 同步切页，重新进入导航组、打开菜单与缩放窗口时自动显示当前项。Enter / → / l 进入上次有效内容位置，只移动焦点；Esc 逐层返回，普通内容和纵向列表也可用 ← / h 执行同层返回。横向控件优先使用自身方向键，Profiles 网格内的 ← / h 保留卡片移动；输入、编辑、弹窗和菜单保留自身按键。常驻导航的 ← / h 不移动焦点，抽屉中的 ← / h 关闭抽屉。Shift+Tab 始终移到上一个控件。仅极小弹出菜单无法同时容纳三行项和提示行时降为单行。

底部快捷键提示默认显示，可通过 `Ctrl+T`、Settings → Interface 或帮助界面的按钮整体收起并记住选择。隐藏时包括退出提示在内的整行不占空间，正文、侧栏视口、溢出提示和鼠标区域同步释放一行；不改变宽度断点，不重置内容位置。可按 `?` 打开帮助，隐藏后的底栏仍可通过 `Ctrl+T` 或 Settings → Interface 恢复；模态层仍保留自身必要的保存、取消或确认说明。

配置卡片采用名称、来源与更新时间、状态三个层次，固定六行高度以保持网格对齐；辅助信息最多两行，就绪状态使用正文色。类型、覆写及内置说明放在已有详情区，完整脱敏来源可换行。页面摘要明确区分自定义数量和内置配置数量。

概览按打开页面后的判断顺序组织为五张卡片：内核与服务、代理路径、实时流量、当前配置、运行环境。正文至少 90 列且终端至少 20 行时采用约 3:2 的主辅栏：左侧为内核与代理路径，右侧为流量与配置，两栏各自按内容自然排列，底部环境通栏；其余尺寸按上述顺序单列。卡片不补齐到相同高度，上下保留一行空白，横向保留两列间隔。

内核卡集中呈现状态、安装/运行版本、PID、期望状态、漂移诊断与启动/停止/重启/管理入口。已知安装版和运行版相同时合并成一行；版本不同或运行状态不可用时分别列出。代理路径把保存/运行模式、模式切换、当前代理组和节点放在同一卡中，两个模式已知且一致时合并说明，不一致或未知时分别显示。当前节点仍代表所选代理组的选择，不宣称所有流量都经过该节点。

流量卡突出上传、下载的实时数值，连接数和内存为辅助指标，累计流量与重启归零说明放在下方；缺失数据用 `--`，实际零值才显示零。当前配置以名称为主，状态和已保存数量为辅助。环境将平台、作用域、服务管理器压成一行，保留自启动、TUN 与系统代理的能力说明；主机 TUN 的操作与确认机制保持一致。未安装、尚未应用、无运行数据均不作错误强调。卡片的语义身份与几何区域由布局直接提供，不从翻译文字或边框扫描推断；宽窄切换保留卡片焦点，Enter 先进入控件，之后显式激活才执行操作。

首次安装区左边界与页头对齐，正文上限为 96 列，页头分隔线仍铺满内容区。选择“稍后设置”后，概览只保留内核卡片内一个普通安装按钮；路由模式以一句说明代替禁用按钮，未安装时不重复展示内核信息卡片。“管理配置”直接进入配置页，内置配置和可选导入继续在该页管理。

## 按钮

实际按钮由 `internal/tui/button.go` 的共享渲染函数统一输出。Bubbles 没有对应按钮组件，因此这里只封装外观；页面继续负责操作、取消、禁用检查和鼠标命中。

| 用途 | 表现 |
| --- | --- |
| 主操作 | `primaryButtonBackground` 蓝底、`primaryButtonForeground` 深色文字、加粗 |
| 次操作 | 正文色；页面内使用 `panel`，卡片和弹窗内使用 `raised` |
| 文字操作 | 沿用所在容器背景、`accent` 文字，悬停时加下划线 |
| 危险操作入口 | `errorColor` 文字、中性表面 |
| 最终危险确认 | `errorColor` 底色、深色文字、加粗 |
| 已选择项 | `selectionBackground` 和正文色，固定预留勾号列，以 `✓` 标识选择 |

按钮左侧固定预留两列；键盘焦点在其中显示橙色 `› `，默认不加下划线。切换焦点、悬停或颜色能力时，标签、后续按钮和命中区位置不变。普通彩色按钮去掉方括号，两侧各留一列空白；ANSI16 与 `NO_COLOR` 使用同宽的 `[标签]` 保留边界。文字操作继续依靠文字及焦点标记。

运行状态与操作分开：运行中显示状态，按钮明确标为停止；路由模式等已选状态不复用主操作蓝底。执行中的按钮使用次要文字，禁用按钮使用禁用色，均不生成可操作命中区；现有进度与 spinner 继续表达任务状态，可取消任务保留取消入口，等待取消清理时禁止再次触发。

配置删除确认默认聚焦取消；输入名称后需明确确认，名称匹配校验继续保留。普通菜单仍采用列表选择语义，焦点箭头使用独立橙色，危险项文字保留红色。

## 搜索框

代理、连接、规则和日志页统一采用三行圆角输入框，文字置于中间一行，左右各留一格内边距。背景使用 `panel`，默认边框使用 `divider`；悬停使用 `accent`，键盘聚焦使用 `focus`，无效正则使用 `errorColor`。输入继续复用 Bubbles textinput 的编辑、粘贴、光标和水平滚动；圆角框与配置页输入复用同一渲染函数。

`/` 前缀保留搜索快捷键提示，整个框（包括上下边框）均可点击聚焦。筛选、清空等按钮在宽屏与输入文字居中对齐，空间不足时移至框下方；匹配计数和说明位于框外。固定栏高度随之调整，短窗口保留仍可见控件的点击区域；通知避让搜索框。

间距与焦点处理参考 [Charm 官方 textinputs 示例](https://github.com/charmbracelet/bubbletea/tree/main/examples/textinputs)和 [Huh 主题](https://github.com/charmbracelet/huh/blob/main/theme.go)，沿用项目的 Mariana 语义色。无色终端仍保留圆角轮廓、搜索前缀、按钮边界与编辑光标。

## 可读性与终端限制

按 WCAG sRGB 相对亮度公式，对真正使用的前景/背景配对计算 `(L高+0.05)/(L低+0.05)`。这是辅助设计检查，不能代替真实终端观察。

| 配对 | 对比度（约） |
| --- | --- |
| 正文 `#d8dee9` / 背景 `#303841` | 8.79:1 |
| 说明 `#a9afbc` / 卡片 `#2b323a` | 5.89:1 |
| 说明 `#a9afbc` / raised `#3a424a` | 4.64:1 |
| 选中文字 `#d8dee9` / 选区 `#4c5864` | 5.38:1 |
| 主按钮字 `#1d2227` / 按钮 `#5c99d6` | 5.33:1 |
| 错误 `#f28d92` / 输入底色 `#282b2f` | 6.09:1 |
| 提示 `#85b3e0` / raised `#3a424a` | 4.63:1 |
| 焦点 `#f9ae58` / raised `#3a424a` | 5.45:1 |
| 成功 `#99c794` / raised `#3a424a` | 5.32:1 |

低对比的装饰线、已禁用内容和 Modal 后的不可操作页面不按普通正文处理。必要控件仍有文字、焦点标记和边界。256 色和低色终端使用专门的语义回退，避免自动量化将蓝灰底色变成纯蓝。True Color/256 色之外的基本 ANSI 调色板受用户终端设置影响，不能保证不同终端像素一致。`NO_COLOR` 时仍保留导航、步骤、状态文字和焦点标记；不修改终端全局配色或 Shell 设置。

本次不引入浅色主题、系统主题跟随、主题市场或主题编辑器。

## 验证与页面预览

日常检查仍为 `make check`、`make race`、`make build`，不会创建虚拟机或安装服务。模型检查覆盖未配置/运行中首页、侧边栏选中/焦点/悬停、Core/Version/Confirm、输入错误、网络独立测试结果、下载、取消清理禁用态及成功/失败结果。尺寸覆盖 160×45、120×32、80×24、60×20、120×18 及极小窗口；每种状态检查 True Color、ANSI256、ANSI16 和无色输出，保持原断点。

只验证真实终端交互与色彩输出，可以在 macOS 或 Linux 对已构建二进制运行：

```sh
python3 scripts/theme-pty.py --binary "$(pwd)/bin/lazyclash" \
  --output .test-artifacts/mariana/pty
```

此入口使用 Python 标准库和已有 VT 当前屏幕还原器，分别启动真彩、256 色和 `NO_COLOR` 会话。每个会话使用独立临时 home 和随机服务名称；验证首页、网络草稿、焦点、缩放和返回，不查询版本、不安装内核或服务。检查输出没有 OSC10/11 颜色设置；256 色不含 RGB，无色模式不含非 reset 的 SGR。它不能证明所有终端模拟器的实际 RGB 显示一致。

导出实际 Go View 并用真实等宽字体排版为单页面 PNG：

```sh
# 使用已安装 Pillow 的 Python；默认查找 Menlo 或 DejaVu Sans Mono。
python3 scripts/theme-preview.py .test-artifacts/mariana
# 其他环境可显式指定等宽 TTF/TTC：
python3 scripts/theme-preview.py .test-artifacts/mariana --font /path/to/monospace.ttf
```

这些预览是 **模型 fixture + 实际 ANSI View + 真实字体** 的排版检查，不是运行中 Mihomo、原生终端截图或服务实测。每张 PNG 对应独立页面/状态；下载数字、运行数据属于明确的模型样本。没有使用生成式效果图。已有来源/代理选项使用左右循环选择控件，本次没有新增独立下拉组件。


## 终端色彩能力与备用配色

Bubble Tea v2 在启动时通过当前输出终端、环境变量、terminfo 及适用时的 tmux 信息检测色彩能力，并发送 `ColorProfileMsg`。LazyClash 在该消息到达前用当前环境给出初值，随后按检测结果重绘；操作步骤、焦点、输入和安装任务均不重置。没有按操作系统选择色彩模式，也没有无条件强制真彩色。

本次不主动发送 RGB/Tc 能力查询：固定版本 Bubble Tea 的 `termcap.go` 明确提示，部分终端（例如 Apple Terminal）可能错误响应该查询并破坏输出。真正支持真彩色但连接链路只声明 256 色的终端，会使用下表中的明确备用配色；用户仍可在确知终端支持时为单次进程声明 `COLORTERM=truecolor`。

下面的数字分别是标准 xterm 256 色索引和 ANSI 16 色索引，属于 C 类终端适配。256 色表使用 16 以上的固定索引，避免依赖用户自定义的前十六色；16 色表不可避免地受终端主题影响。低色模式合并部分背景层次，继续依靠边框、文字和非颜色的当前项/焦点标记区分状态。

| 语义角色 | ANSI256 | ANSI16 |
| --- | --- | --- |
| `bg` | 236 | 0（黑） |
| `panel` | 235 | 0 |
| `raised` | 237 | 0 |
| `sidebarBackground`、`primaryButtonForeground` | 234 | 0 |
| `inputBackground` | 235 | 0 |
| `text`、`selectionForeground` | 253 | 15（亮白） |
| `muted` | 249 | 7（浅灰） |
| `disabled`、`backdropText` | 245 | 8（亮黑） |
| `selectionBackground` | 240 | 4（海军蓝，仅用于小范围选中项） |
| `hoverBackground` | 238 | 0 |
| `border` | 244 | 8 |
| `divider` | 238 | 8 |
| `accent`、`info` | 110 | 14（亮青） |
| `focus`、`warning` | 215 | 11（亮黄） |
| `primaryButtonBackground` | 68 | 14（亮青） |
| `success` | 114 | 10（亮绿） |
| `errorColor` | 210 | 9（亮红） |

`NO_COLOR` 采用非空即禁用颜色的语义，迟到的真彩色能力消息也不能覆盖这个选择。无颜色模式移除颜色和装饰，保留原有文字、边框、当前步骤方括号、`›` 焦点标记与键盘操作。主题只输出应用字符单元的 SGR 样式，不写 Shell 配置或终端调色板；`tea.View` 的终端默认前景/背景字段保持为空，避免通过 OSC10/11 改变终端默认颜色。

`internal/tui/theme_profile_test.go` 检查 True Color 与 ANSI256 下正文、说明、选区、按钮、错误、焦点的实际配对均达到 4.5:1 目标，并检查无颜色、低色模式和窗口缩放期间的内容、布局、任务与焦点保留。这些自动检查不代表已在所有终端模拟器上观察通过。
