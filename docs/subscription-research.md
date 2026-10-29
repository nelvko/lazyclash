# 订阅、Profile 与运行配置：源码调研和方案选择

调研日期：2026-10-29。本报告读取 GitHub 实际源码、官方文档及相关 Issue/PR，源码链接固定在下表提交。默认分支可能含尚未发布的改动；官方文档与 Issue 会继续更新，不能将某一版本的行为外推到所有版本。Issue 用于理解真实问题和设计动机，下面单独列出核查状态，不将报告内容当成本地复现结论。

## 取样与主要结论

| 项目 | 取样分支与提交 | 主要业务模型 | 更适合的场景与代价 |
| --- | --- | --- | --- |
| Clash Verge Rev | dev / `df70146b025e06661c8364351cf8b250267ed985` | 单一完整订阅底稿 + 全局/每订阅扩展 + 应用字段回写 | 机场完整配置开箱即用；多层优先级和脚本增加解释、恢复成本 |
| FlClash | main / `7fd417fb9a9a277c6a9339a8ac87f907df7c25f7` | 文件/远程完整 Profile 与应用维护的 Custom Profile，共享构建链 | 需要可视化维护组/规则和复用多个节点来源；应用数据、脚本与 Dart/Go 边界较复杂 |
| Clash Nyanpasu | main / `2fadf212f161417dcdf2f37d81a305a37ff918a4` | 配置与变换分离；File/Composition + scoped/global transforms | 高级组合和可追溯构建；执行图、作业、serial owner 等基础设施成本高 |
| OpenClash | master / `e553e82e01c697aefbe1555261a503f425e00ccd` | UCI 订阅 + 当前配置路径 + 路由器设置/覆写 + 独立运行文件 | 路由器网络接管；UCI、Ruby、procd、网络脚本具有平台耦合 |
| ShellCrash | master / `f37609b9fb76b1b4f375445eb71d5e4eed6c0eed` | 完整底稿与模板/provider 两条路径 + 独立用户文件 | Shell 部署与用户模板；文本拼接及失败降级需要明确语义 |
| Mihomo | Meta / `3f27ef39f8c96bbfbc8b0b9bb10f5c9ef60149d8` | 解析配置、应用子系统、运行 API；不是客户端配置数据库 | 决定配置与加载真实语义，不能从 API 成功推断全部网络组件已工作 |

五个客户端都体现了输入与运行产物分离，但没有统一的“YAML 合并”业务。完整订阅承载节点、组、规则、DNS 等相互依赖的配置；节点来源模式则让用户自己拥有组、规则和解析策略。两种语义都成立，必须由 Profile 明确选择，不能把任何订阅默认为可安全抽节点的数据包。

最值得借鉴的是：来源与启用选择分开，用户定制独立保存；应用接管字段有明确所有权；规则和引用按内核语义检查；生成、校验、文件提交、内核应用、实际观测分别记账。最不应照搬的是为早期项目引入通用脚本平台，或者在失败后静默删除用户规则、替换成无节点默认配置。

## Clash Verge Rev

固定提交：`df70146b025e06661c8364351cf8b250267ed985`（dev）。

### 来源与启用配置

- `IProfiles` 保存 `current` 和 `items`，item 包含 remote/local/merge/script/rules/proxies/groups 类型、URL、原始文件名和每订阅扩展引用。远程下载先要求成功 HTTP 响应和 YAML mapping，并要求至少有 `proxies` 或 `proxy-providers`；原文保存在 `file_data`，不是直接变成运行文件。[来源对象与下载验证](https://github.com/clash-verge-rev/clash-verge-rev/blob/df70146b025e06661c8364351cf8b250267ed985/src-tauri/src/config/prfitem.rs#L249-L425)
- 添加第一份 remote/local 会设为 current；已有 current 时只增加 item。原文单独写到 profiles 目录。[添加语义](https://github.com/clash-verge-rev/clash-verge-rev/blob/df70146b025e06661c8364351cf8b250267ed985/src-tauri/src/config/profiles.rs#L185-L220)
- 更新下载会写原始文件和 updated/extra 等元数据，再判断是不是当前 Profile。只有当前 Profile 更新才刷新内核。若内核应用失败，明确报告“订阅已保存，但未能确认已应用到内核”，而不伪装成全部回滚。[更新流程](https://github.com/clash-verge-rev/clash-verge-rev/blob/df70146b025e06661c8364351cf8b250267ed985/src-tauri/src/feat/profile.rs#L180-L240)
- 删除当前 item 会挑选下一份 remote/local；另有删除计划延后清理文件。[删除计划](https://github.com/clash-verge-rev/clash-verge-rev/blob/df70146b025e06661c8364351cf8b250267ed985/src-tauri/src/config/profiles.rs#L340-L377)

### 构建链路与优先级

实际顺序：当前原文 → 每订阅 rules/proxies/groups 的 prepend/append/delete → 应用 Clash 设置 → 内置转换/TUN/DNS 设置 → 全局 Merge → 全局 Script → 每订阅 Merge → 每订阅 Script → 应用所拥有字段强制恢复 → 分组清理/排序。[完整构建入口](https://github.com/clash-verge-rev/clash-verge-rev/blob/df70146b025e06661c8364351cf8b250267ed985/src-tauri/src/enhance/mod.rs#L875-L973)

- `CONTROL_PLANE_KEYS` 明确接管 controller、secret、各监听端口、mode、allow-lan 等；app-owned 字段扩展无权最终改变。TUN 只恢复 GUI 管理的子键；DNS 仅恢复启用覆写时接管的子键。被扩展改写而被丢弃的字段有告警。[字段所有权](https://github.com/clash-verge-rev/clash-verge-rev/blob/df70146b025e06661c8364351cf8b250267ed985/src-tauri/src/enhance/mod.rs#L307-L409)
- 普通 mapping 递归合并，sequence/scalar 整体替换；DNS 下写出的子字段整体替换（例如 nameserver-policy 不逐项合并），hosts 整体替换。[实际合并器](https://github.com/clash-verge-rev/clash-verge-rev/blob/df70146b025e06661c8364351cf8b250267ed985/src-tauri/src/enhance/merge.rs#L1-L35)
- prepend/append/delete 是另一条显式序列操作机制；删除节点会维护组内引用，添加节点还有 selector 关联逻辑，并非通用数组合并。[序列操作](https://github.com/clash-verge-rev/clash-verge-rev/blob/df70146b025e06661c8364351cf8b250267ed985/src-tauri/src/enhance/seq.rs#L5-L115)
- 官方多订阅示例使用用户维护的完整配置 + 多个 HTTP proxy-providers + 名称前缀；这与“多个完整 YAML 自动合并”不同。[官方多订阅案例](https://www.clashverge.dev/guide/config.html)

### 校验、加载与恢复

同一次序列化的 YAML 同时用于 validator 和 run file；校验成功后才应用。服务模式先让 service stage runtime，支持时 reload staged path，不支持时替换 core；普通模式 reload 失败会尝试 restart。[校验/应用](https://github.com/clash-verge-rev/clash-verge-rev/blob/df70146b025e06661c8364351cf8b250267ed985/src-tauri/src/core/manager/config.rs#L328-L467)

切换 Profile 使用 candidate profiles 构建、校验和应用后再保存选择；应用或保存失败会重新构建和应用旧 profiles。[切换事务](https://github.com/clash-verge-rev/clash-verge-rev/blob/df70146b025e06661c8364351cf8b250267ed985/src-tauri/src/core/manager/config.rs#L213-L268) 但“下载更新原文”与“切换 current”是不同事务边界；不要从切换回滚推断订阅更新也回滚原文。

优势是完整订阅开箱即用，用户定制独立，关键控制面稳定；代价是链路长，字段被多少层改写很难解释，reload 失败自动 restart 还可能把本可继续服务的旧进程打断。lazyclash 应借鉴所有权和候选校验，谨慎选择自动重启策略。

官方文档确认两级扩展、应用字段回写及 DNS 特殊合并规则。[扩展文档](https://www.clashverge.dev/guide/extend.html) Issue [#8207](https://github.com/clash-verge-rev/clash-verge-rev/issues/8207) 报告 DNS 设置保存成功但 runtime 未合并，说明 UI 设置、持久化、生成、实际加载需要分别有证据；该 Issue 是用户报告，不能据此声称当前提交仍有此 bug。

## FlClash

固定提交：`7fd417fb9a9a277c6a9339a8ac87f907df7c25f7`（main）。

### 模型、更新、定制

`Profile` 包含 file/url/custom 类型、URL、更新元数据、selectedMap、标准/脚本扩展与 ProfileOverrides；custom 由应用数据库维护节点、组、规则。[Profile 模型](https://github.com/chen08209/FlClash/blob/7fd417fb9a9a277c6a9339a8ac87f907df7c25f7/lib/models/profile.dart#L63-L116)

- 下载先写临时文件并调用 core 验证，成功后复制到 Profile 文件；失败不会进入该复制步骤。添加第一份 Profile 会选择它，后续新增保持当前选择。[下载保存](https://github.com/chen08209/FlClash/blob/7fd417fb9a9a277c6a9339a8ac87f907df7c25f7/lib/models/profile.dart#L181-L216) [首次选择](https://github.com/chen08209/FlClash/blob/7fd417fb9a9a277c6a9339a8ac87f907df7c25f7/lib/providers/actions/profiles.dart#L62-L66)
- 更新当前 Profile 会 debounce apply；更新非当前 Profile 也检查其是否被其他 custom Profile 作为 Provider 引用，再应用受影响配置。下载完成后重新读取 latest，避免旧下载覆盖期间发生的删除或元数据编辑。[更新](https://github.com/chen08209/FlClash/blob/7fd417fb9a9a277c6a9339a8ac87f907df7c25f7/lib/providers/actions/profiles.dart#L138-L184)
- custom Profile 可以通过组的 use 引用其他 Profile，生成 type:file/path 的 proxy-provider；只注入实际引用的 provider，而不是把所有来源都塞进去。[按引用注入](https://github.com/chen08209/FlClash/blob/7fd417fb9a9a277c6a9339a8ac87f907df7c25f7/lib/providers/actions/setup.dart#L568-L610)

### 构建

普通 Profile 可以执行脚本；custom 从应用节点/组/规则构造。它们都进入 makeRealProfileTask，统一应用端口/控制面/TUN/DNS/规则等。[构建入口](https://github.com/chen08209/FlClash/blob/7fd417fb9a9a277c6a9339a8ac87f907df7c25f7/lib/providers/actions/setup.dart#L476-L565)

- controller 的其他入口被清空，secret/端口/认证由应用设置写入；TUN 先继承原始，再覆写选中键和应用强制键；provider 的缓存路径归入每 Profile 的专属目录。[控制面与路径](https://github.com/chen08209/FlClash/blob/7fd417fb9a9a277c6a9339a8ac87f907df7c25f7/lib/common/task.dart#L134-L268)
- DNS 先按需补 baseline，再应用用户明确选择的 overrides；附加规则放在原规则之前；custom 的组/规则整体使用应用数据。[DNS 与规则](https://github.com/chen08209/FlClash/blob/7fd417fb9a9a277c6a9339a8ac87f907df7c25f7/lib/common/task.dart#L278-L374)
- 为避免在 Dart 来回搬运大节点列表，core 保存原始 proxies/proxy-groups/sub-rules，host 生成其余 overlay，Go 再组装：上述大段从原文取回，rules 用 overlay 前置后接原规则。这个优化不应误解为订阅与应用配置简单 deep merge。[Go 组装器](https://github.com/chen08209/FlClash/blob/7fd417fb9a9a277c6a9339a8ac87f907df7c25f7/core/profile_base.go#L20-L22) [分段合成](https://github.com/chen08209/FlClash/blob/7fd417fb9a9a277c6a9339a8ac87f907df7c25f7/core/profile_base.go#L120-L160) [规则顺序](https://github.com/chen08209/FlClash/blob/7fd417fb9a9a277c6a9339a8ac87f907df7c25f7/core/profile_base.go#L221-L239)

### 内核与失败语义

最终 config.yaml 用临时文件 rename 写入，可供 Android 服务独立启动和 Mihomo 自己 reload；说明产物必须脱离 UI 内存。[独立文件](https://github.com/chen08209/FlClash/blob/7fd417fb9a9a277c6a9339a8ac87f907df7c25f7/core/profile_base.go#L252-L265)

重要反例：`applyConfig` 解析失败会应用内置 default（无代理），然后返回原错误；不是保留旧业务配置。host 在推送前清除 applied hash，只有成功才重新记账，避免错误地宣称仍运行原 Profile。[core fallback](https://github.com/chen08209/FlClash/blob/7fd417fb9a9a277c6a9339a8ac87f907df7c25f7/core/config.go#L151-L183) [host 状态](https://github.com/chen08209/FlClash/blob/7fd417fb9a9a277c6a9339a8ac87f907df7c25f7/lib/providers/actions/setup.dart#L762-L816)

优势是 Full/Custom 共用生成链，Provider 引用自然组合来源，选定字段覆写避免全部默认值接管。局限是维护应用格式、Dart/Go 边界、脚本和 Provider 路径迁移的成本高；失败 fallback 不适合 lazyclash “订阅更新不破坏当前可用服务”的目标。

官方站点 [发布说明](https://chen08209.github.io/FlClash/) 显示 0.9.2 引入 optional general overrides，0.9.1 调整 TUN 键继承；因此不应拿旧版教程描述当前所有权。Issue [#2836](https://github.com/chen08209/FlClash/issues/2836) 报告 DNS 默认值强制覆盖引发解析失败，作为真实使用风险的证据，不作为本地已复现结论。

## Clash Nyanpasu

固定提交：`2fadf212f161417dcdf2f37d81a305a37ff918a4`（main）；当前 2.0 的结构与旧 1.x 教程差别很大。

### 模型

显式分 `ProfileDefinition::Config` 和 `Transform`。Config 分 FileConfig 与 CompositionConfig；FileConfig 有 source + transforms；CompositionConfig 有 optional base、extend_proxies_from、自己的 transforms；Transform 分 YAML Overlay / Script(JS,Lua)。[定义](https://github.com/libnyanpasu/clash-nyanpasu/blob/2fadf212f161417dcdf2f37d81a305a37ff918a4/backend/nyanpasu-config/src/profile/definition.rs#L5-L73)

这已经覆盖题目中的两种方向：有 base 时继承完整配置，无 base 时 clean seed + nodes-only contributors。组合成员先运行自己的 scoped transforms，再仅提取 proxies 按声明顺序追加；不会自动携带成员的 groups/rules/proxy-providers。若成员缺 proxies 则告警、不贡献节点。没有在这个 append 函数内做节点重名消解。[组合执行](https://github.com/libnyanpasu/clash-nyanpasu/blob/2fadf212f161417dcdf2f37d81a305a37ff918a4/backend/nyanpasu-config/src/runtime/executor/compose.rs#L22-L162)

### 生成与可观测性

RuntimeBuilder 先验证 profiles 引用结构，再执行独立 pipeline；输入把 resolved ports、TUN、core flavor 当参数，executor 不做端口探测。[builder](https://github.com/libnyanpasu/clash-nyanpasu/blob/2fadf212f161417dcdf2f37d81a305a37ff918a4/backend/nyanpasu-core/src/enhance/runtime_builder.rs#L121-L186)

共享尾部：global transforms → optional field filter → 保留 unmanaged 字段 → managed settings → 缺失默认值 → 内置 core transforms → include-all expansion。每一步保留 graph/snapshot/log；transform 失败在 executor 中 passthrough 并记 failure，RuntimeBuilder 的 validate_transforms 可拒绝发布，因此不能简单说“脚本失败照常启用”。[pipeline](https://github.com/libnyanpasu/clash-nyanpasu/blob/2fadf212f161417dcdf2f37d81a305a37ff918a4/backend/nyanpasu-config/src/runtime/executor/mod.rs#L270-L388) [拒绝变换错误](https://github.com/libnyanpasu/clash-nyanpasu/blob/2fadf212f161417dcdf2f37d81a305a37ff918a4/backend/nyanpasu-core/src/enhance/runtime_builder.rs#L123-L148)

远程导入 fetch-before-commit，不先持久化空占位；下载后由 serial owner 检查 URL/definition fingerprint 是否仍匹配，已删除/已改源的迟到下载不允许写入；语法验证要求配置为 mapping 且含代理来源。[导入/下载边界](https://github.com/libnyanpasu/clash-nyanpasu/blob/2fadf212f161417dcdf2f37d81a305a37ff918a4/backend/tauri/src/state/profiles/actor.rs#L309-L334) [防迟到覆盖](https://github.com/libnyanpasu/clash-nyanpasu/blob/2fadf212f161417dcdf2f37d81a305a37ff918a4/backend/tauri/src/state/profiles/actor.rs#L644-L684) [提交检查](https://github.com/libnyanpasu/clash-nyanpasu/blob/2fadf212f161417dcdf2f37d81a305a37ff918a4/backend/tauri/src/state/profiles/actor.rs#L785-L868)

### 运行状态与恢复

RuntimeSnapshot 只序列化一次，配置文件、下发内容、receipt、YAML 预览共用相同文本。状态区分 promoted（生成产物）、applied（确认且具有效配置观测）、pending（已确认但观测未就绪），并另有 confirmed receipt，恢复基准不从“文件存在”推断。[快照与 receipt](https://github.com/libnyanpasu/clash-nyanpasu/blob/2fadf212f161417dcdf2f37d81a305a37ff918a4/backend/tauri/src/client/runtime.rs#L65-L189)

候选文件使用独占 create_new、0600、sync_all、内容 hash；product 原子替换。[候选落盘](https://github.com/libnyanpasu/clash-nyanpasu/blob/2fadf212f161417dcdf2f37d81a305a37ff918a4/backend/tauri/src/client/runtime.rs#L359-L451)

恢复必须对照上一次 confirmed receipt 验证 source hash/core/host/run intent/新 binding。源码明确解释：恢复请求返回 RolledBack 可能意味着“这次恢复失败、仍在新配置”，不能视作恢复成功；缺观测时进入 RecoveryRequired。[恢复验证](https://github.com/libnyanpasu/clash-nyanpasu/blob/2fadf212f161417dcdf2f37d81a305a37ff918a4/backend/tauri/src/client/runtime_recovery.rs#L1-L28) [严格判定](https://github.com/libnyanpasu/clash-nyanpasu/blob/2fadf212f161417dcdf2f37d81a305a37ff918a4/backend/tauri/src/client/runtime_recovery.rs#L138-L179)

官方文档强调配置与 transformation 分离、Remote/Local/Linked source 和运行配置检查。[Profile 文档](https://nyanpasu.org/tutorial/profiles-and-subscriptions/) [Transform 文档](https://nyanpasu.org/tutorial/proxy-chain/)

Issue [#5609](https://github.com/libnyanpasu/clash-nyanpasu/issues/5609) 报告 guard 把默认 tcp-concurrent 重写成 true，说明“默认值”与“明确接管字段”不能混淆。PR [#5720](https://github.com/libnyanpasu/clash-nyanpasu/pull/5720) 记录订阅残留 port/socks-port 与应用 mixed-port 冲突，修复通过无配置时也删除应用管理字段，而非仅在非零时覆盖。Issue [#1456](https://github.com/libnyanpasu/clash-nyanpasu/issues/1456) 记录旧全局链/订阅链重复执行造成代理名称冲突，说明链式扩展需要明确定义作用域和顺序。

优势是模型明确、组合与完整配置共享执行器、输入可追溯、运行确认严谨。局限是 serial owner、资源日志、作业管理、执行图、脚本、远程 host 等整套复杂度远超过早期 Linux CLI/TUI 的需要。lazyclash 应借鉴“数据源/方案/产物/确认”的边界和失败语义，暂不复制 DAG 与通用脚本平台。

## OpenClash：完整配置底稿 + 路由器设置 + 分层覆写

**订阅条目和启用配置是分开的。** `config_subscribe` UCI 条目含 URL、UA、过滤条件、转换后端和模板。每条订阅下载到 `/tmp/<name>.yaml`，最终原始底稿保存为 `/etc/openclash/config/<name>.yaml`；启用项另由 `openclash.config.config_path` 指向。若原先没有选择配置，首次成功下载会写入选择。更新非当前条目无需触发重启；更新当前条目时才设置 restart 标记。见 [下载条目和地址构造](https://github.com/vernesong/OpenClash/blob/e553e82e01c697aefbe1555261a503f425e00ccd/luci-app-openclash/root/usr/share/openclash/openclash.sh#L347-L460)、[比较、替换及选择/重启判定](https://github.com/vernesong/OpenClash/blob/e553e82e01c697aefbe1555261a503f425e00ccd/luci-app-openclash/root/usr/share/openclash/openclash.sh#L185-L217)。

**更新是先下载临时文件、校验，再替换原始底稿。** `config_test()` 调用内核 `-t -d ... -f ...`；还有 Ruby 检查 proxies/proxy-providers，下载失败有直连重试。这里的原始底稿并不保证逐字保留服务器响应，节点过滤等处理会改变候选。见 [内核测试](https://github.com/vernesong/OpenClash/blob/e553e82e01c697aefbe1555261a503f425e00ccd/luci-app-openclash/root/usr/share/openclash/openclash.sh#L52-L89)、[更新入口](https://github.com/vernesong/OpenClash/blob/e553e82e01c697aefbe1555261a503f425e00ccd/luci-app-openclash/root/usr/share/openclash/openclash.sh#L462-L499)。

**存在独立运行配置。** 启动时从 raw 文件复制到临时文件；依次应用网络/端口/DNS/TUN 等应用设置、规则修改、覆写模块、自定义脚本，再重定位 provider 路径，最终将临时文件移动为 `/etc/openclash/<name>.yaml`，交 procd 启动。见 [底稿复制](https://github.com/vernesong/OpenClash/blob/e553e82e01c697aefbe1555261a503f425e00ccd/luci-app-openclash/root/etc/init.d/openclash#L466-L470)、[生成顺序](https://github.com/vernesong/OpenClash/blob/e553e82e01c697aefbe1555261a503f425e00ccd/luci-app-openclash/root/etc/init.d/openclash#L3824-L3887)、[发布及 procd](https://github.com/vernesong/OpenClash/blob/e553e82e01c697aefbe1555261a503f425e00ccd/luci-app-openclash/root/etc/init.d/openclash#L848-L874)。因此 Web 界面退出不会结束内核。

**覆写不是普通 Deep Merge。** YAML.rb 自定义 DSL 区分 prepend、append、delete、force overwrite、batch update；普通 map 递归，集合操作具有独立含义。见 [操作符解析](https://github.com/vernesong/OpenClash/blob/e553e82e01c697aefbe1555261a503f425e00ccd/luci-app-openclash/root/usr/share/openclash/YAML.rb#L735-L814)、[集合操作](https://github.com/vernesong/OpenClash/blob/e553e82e01c697aefbe1555261a503f425e00ccd/luci-app-openclash/root/usr/share/openclash/YAML.rb#L912-L943)。优势是表达力高，局限是额外 DSL、脚本执行、安全及调试成本显著。lazyclash 当前无需复制这套语言。

**规则必须有位置和引用语义。** 自定义规则分 top/bottom，后者寻找原有 MATCH/GEOIP 等锚点；不存在的组/节点引用会告警并跳过。见 [检查目标、按位置插入](https://github.com/vernesong/OpenClash/blob/e553e82e01c697aefbe1555261a503f425e00ccd/luci-app-openclash/root/usr/share/openclash/yml_rules_change.sh#L131-L220)。对 lazyclash 更合理的是拒绝不完整引用，避免用户以为规则已生效；规则应绑定 Profile，或采用明确定义的稳定别名。

**多订阅组合主要可走外部转换服务。** 源码把多行地址编码并以 `|` 组合，连同模板传到 subconverter；这与任意两个完整 YAML 合并不是同一业务。见上面的地址构造 L436-L454。采用第三方转换会引入隐私及可用性依赖，不适合作为 lazyclash 首版默认。

**局限与启示。** 现有启动链路在应用定制后发布运行文件并重启内核，不是一套带原子提交标识的状态事务；下载前置验证不能覆盖后续用户覆写产生的全部错误。网络、UCI、Ruby、procd 强耦合是路由器产品的自然结果，lazyclash 应借鉴输入/产物隔离和类型化语义，保留自己的应用服务层。

相关用户需求：[OpenClash #5335](https://github.com/vernesong/OpenClash/issues/5335) 描述切换完整订阅后代理组名称不同、同一自定义规则失效的问题。这支持 Profile 级自定义规则与引用校验，而非无条件全局追加。

## ShellCrash：完整订阅和本地模板 + providers 两条链路

**完整配置模式。** 在线获取/转换配置下载到临时文件，经内容检查后与当前底稿比较；变化时将旧底稿备份为 `.bak` 再替换。见 [下载及替换](https://github.com/juewuy/ShellCrash/blob/f37609b9fb76b1b4f375445eb71d5e4eed6c0eed/scripts/starts/core_config.sh#L30-L107)、[基础内容检查](https://github.com/juewuy/ShellCrash/blob/f37609b9fb76b1b4f375445eb71d5e4eed6c0eed/scripts/starts/clash_config_check.sh#L4-L43)。底稿检查是字符串/章节检查，不等于最终内核校验。

**本地生成 providers 模式。** 独立 `providers.cfg` 保存多个来源、本地文件、UA、过滤等；模板提供 proxy-groups/rules，每条来源生成有独立缓存路径的 provider，并建立引用它的组。拼成候选后执行内核 `-t`，成功才放到 `yamls/config.yaml`，随后由用户选择是否启动。见 [模板、全部来源、测试及发布](https://github.com/juewuy/ShellCrash/blob/f37609b9fb76b1b4f375445eb71d5e4eed6c0eed/scripts/menus/providers_clash.sh#L11-L81)、[provider 生成](https://github.com/juewuy/ShellCrash/blob/f37609b9fb76b1b4f375445eb71d5e4eed6c0eed/scripts/menus/providers_clash.sh#L84-L126)。这是真正的 Managed Profile 类模式；多个来源共享规则模板，provider 的定时更新由内核完成。

**运行生成和用户文件隔离。** `clash_modify.sh` 从底稿拆出 proxies/groups/providers/rules 等章节；应用生成 set/DNS/hosts，自定义 proxies/groups/rules/user/others 独立保存并插入，最后生成运行 `config.yaml`。自定义 rules 前置；部分 user 字段可覆盖 set。见 [拆分及用户节点/组](https://github.com/juewuy/ShellCrash/blob/f37609b9fb76b1b4f375445eb71d5e4eed6c0eed/scripts/starts/clash_modify.sh#L119-L182)、[规则和最终组合](https://github.com/juewuy/ShellCrash/blob/f37609b9fb76b1b4f375445eb71d5e4eed6c0eed/scripts/starts/clash_modify.sh#L185-L237)。

**错误策略是降级，未必保留用户意图。** 最终候选 `-t` 失败后保存 `error.yaml`，移除自定义内容并改用基础配置继续启动。见 [测试及降级](https://github.com/juewuy/ShellCrash/blob/f37609b9fb76b1b4f375445eb71d5e4eed6c0eed/scripts/starts/clash_modify.sh#L240-L253)。适合路由器“尽力恢复联网”，但可能丢弃用户阻断规则。lazyclash 应保留已知可用配置、返回明确失败，不静默去掉规则。

**真实问题证明集合不能靠文本拼接。** [ShellCrash #1292](https://github.com/juewuy/ShellCrash/issues/1292) 反馈 providers 模式与自定义 sections 组合产生重复键；[#839](https://github.com/juewuy/ShellCrash/issues/839) 反馈 mix 模式自动 rule-providers 与自定义 rule-providers 重复。不能据这些报告认定当前提交仍未修复，但它们准确展示了生成器必须有单一 YAML AST、字段类型策略与重复检测。

**抽节点不保证等价于完整配置。** [ShellCrash #1367](https://github.com/juewuy/ShellCrash/issues/1367) 给出完整订阅依赖 hosts/DNS 的最小案例：转 provider 后节点可加载，校验通过，但连接失败。报告限定了版本与测试边界。lazyclash 应让用户显式选择 full/nodes 模式；不声称 nodes 模式完整继承订阅行为，不自动把整个 DNS 合并到 Managed Profile。

## Mihomo：必须遵守的真实配置/加载语义

1. **`-t` 是解析校验，不能证明可运行。** main 中测试分支只调用 `executor.Parse/ParseWithBytes`，没有 `ApplyConfig`，因此不验证监听端口占用、TUN 创建权限、所有远程 provider 是否可用。见 [main.go L175-L192](https://github.com/MetaCubeX/mihomo/blob/3f27ef39f8c96bbfbc8b0b9bb10f5c9ef60149d8/main.go#L175-L192)。应在与正式运行相同 `-d` 和路径上下文执行，避免 provider 相对路径失真。
2. **PUT 解析失败时不会调用 Apply。** `PUT /configs?force=true` 接受 path/payload；先解析，失败返回 400；成功调用 ApplyConfig，然后 204。显式 path 必须绝对路径并在安全路径内。见 [route/configs.go L405-L453](https://github.com/MetaCubeX/mihomo/blob/3f27ef39f8c96bbfbc8b0b9bb10f5c9ef60149d8/hub/route/configs.go#L405-L453)、[安全路径实现](https://github.com/MetaCubeX/mihomo/blob/3f27ef39f8c96bbfbc8b0b9bb10f5c9ef60149d8/constant/path.go#L87-L104)。候选应在 core home 下，不要直接传任意 `/tmp`。
3. **Apply 本身不是事务。** ApplyConfig 无 error 返回，依次更新代理、规则、DNS、监听、TUN，再加载 providers。provider 初始化错误只写日志；许多子系统失败不会变成 PUT 错误。见 [executor.go L84-L123](https://github.com/MetaCubeX/mihomo/blob/3f27ef39f8c96bbfbc8b0b9bb10f5c9ef60149d8/hub/executor/executor.go#L84-L123)、[provider 错误只记日志](https://github.com/MetaCubeX/mihomo/blob/3f27ef39f8c96bbfbc8b0b9bb10f5c9ef60149d8/hub/executor/executor.go#L318-L343)。所以 204 代表调用已完成，不代表真实网络已满足用户意图。
4. **`force` 主要影响监听重建，TUN 无论 force 都会更新。** 见 [监听和 TUN 更新](https://github.com/MetaCubeX/mihomo/blob/3f27ef39f8c96bbfbc8b0b9bb10f5c9ef60149d8/hub/executor/executor.go#L186-L212)。普通配置切换无需重启 core，但也不能承诺所有热加载完全无中断；controller 自身的启动上下文应由 service/受控设置保持稳定。
5. **规则和组是引用图。** 重名节点/组会报错，组先 DAG 检查排序；规则目标、SUB-RULE、RULE-SET 都校验存在性。见 [节点、组、provider 解析](https://github.com/MetaCubeX/mihomo/blob/3f27ef39f8c96bbfbc8b0b9bb10f5c9ef60149d8/config/config.go#L880-L965)、[规则引用检查](https://github.com/MetaCubeX/mihomo/blob/3f27ef39f8c96bbfbc8b0b9bb10f5c9ef60149d8/config/config.go#L1098-L1134)。应保持规则顺序、显式处理最终 MATCH，不做盲目的数组去重或索引合并。
6. **GET /configs 返回 General，不含完整配置或 revision/hash。** 见 [路由 GET](https://github.com/MetaCubeX/mihomo/blob/3f27ef39f8c96bbfbc8b0b9bb10f5c9ef60149d8/hub/route/configs.go#L134-L137)、[GetGeneral](https://github.com/MetaCubeX/mihomo/blob/3f27ef39f8c96bbfbc8b0b9bb10f5c9ef60149d8/hub/executor/executor.go#L129-L184)。lazyclash 的 applied revision 是自己确认/记录的收据，外部 API 修改、core crash 或断连后应显示未知/漂移，不能伪装可从 core 读回准确 hash。
7. **HTTP provider 自治更新会改变节点而不改变顶层 YAML。** [Fetcher.Initial](https://github.com/MetaCubeX/mihomo/blob/3f27ef39f8c96bbfbc8b0b9bb10f5c9ef60149d8/component/resource/fetcher.go#L56-L68) 会优先使用已有有效 path 的缓存，并不比较该缓存原先来自哪个 URL；因此顶层 URL 变更但 path 相同也可能先载入旧来源。lazyclash 将 HTTP 缓存路径按定义隔离。 Managed Profile 若追求所有输入可回滚，应由应用下载成受控本地/内联快照；若直接交给 core 更新，则应明确 provider 状态独立，不把顶层配置哈希当所有节点的版本。

官方文档亦说明 [配置 API 与 SAFE_PATHS](https://wiki.metacubex.one/api/)、[provider 配置](https://wiki.metacubex.one/config/proxy-providers/)。[Mihomo PR #3373](https://github.com/MetaCubeX/mihomo/pull/3373) 在调研时为 open、未合并，提议独立 PUT /rules，理由正是完整 reload 会重建 DNS/代理/监听并暂停隧道；不可把该提案当已发布 API。

## Issue/PR 的状态与证据边界

以下状态于 2026-10-29 通过 GitHub API 单独核查。open 表示讨论仍开放，不能证明取样源码必然存在报告的问题；closed(completed) 也不能替代检查对应版本。PR merged 与尚未合并的提议严格区分。

| 讨论 | 核查状态 | 本报告采用的证据 |
| --- | --- | --- |
| [Verge #8207](https://github.com/clash-verge-rev/clash-verge-rev/issues/8207) | open | 设置持久化、配置生成与实际加载需要各自有证据；不是本地复现结论 |
| [FlClash #2836](https://github.com/chen08209/FlClash/issues/2836) | open | 默认 DNS 强制接管可能改变完整订阅的联网行为 |
| [Nyanpasu #5609](https://github.com/libnyanpasu/clash-nyanpasu/issues/5609) | closed / completed | 历史回归：普通默认值不应自动成为强制覆写 |
| [Nyanpasu #1456](https://github.com/libnyanpasu/clash-nyanpasu/issues/1456) | open / reopened | 旧版重复执行全局/每订阅链造成名称冲突的报告 |
| [Nyanpasu PR #5720](https://github.com/libnyanpasu/clash-nyanpasu/pull/5720) | merged，2026-10-11 | 已合并修复：关闭 HTTP/SOCKS 端口时也应删除订阅残留值 |
| [OpenClash #5335](https://github.com/vernesong/OpenClash/issues/5335) | open / reopened | 切换完整配置后组名变化，自定义规则需要作用域与引用策略 |
| [ShellCrash #1292](https://github.com/juewuy/ShellCrash/issues/1292) | open | 模板/provider 与自定义配置组合产生重复键的报告 |
| [ShellCrash #839](https://github.com/juewuy/ShellCrash/issues/839) | open / reopened | 自动与自定义 rule-providers 冲突的报告 |
| [ShellCrash #1367](https://github.com/juewuy/ShellCrash/issues/1367) | open | 抽节点丢失 hosts/DNS 依赖，即使解析通过也可能无法连接 |
| [Mihomo PR #3373](https://github.com/MetaCubeX/mihomo/pull/3373) | open，未合并 | 独立规则更新的设计提议；本实现不依赖或声称已有该 API |

## lazyclash 的可行方案比较

| 方案 | 业务与用户体验 | 实现、维护与扩展成本 | 决定 |
| --- | --- | --- | --- |
| 直接运行下载的原始配置 | 最容易导入完整订阅，但来源可决定端口、控制接口、TUN；修改原文件会在更新时丢失 | 初始代码少，运行归属、权限与恢复难以稳定；不同内核仍需适配 | 不采用。保留原文作为输入，不作为服务直接启动的文件 |
| 通用 YAML Deep Merge | 表面支持任意定制，但列表、DNS 映射、规则位置、重名和字段删除含义不明确 | 合并器简单，业务边界与错误诊断反而复杂；不能解决跨内核语义差异 | 不采用为核心契约。必须先定义字段所有权与显式操作 |
| Full Profile + 显式 override | 保留一份完整订阅的组、规则和 resolver；用户定制独立，应用最终决定受管网络字段 | 当前可完整落地；保留未知 Mihomo 字段并交内核验证，避免维护整个配置 schema | 首要路径，适合大多数直接导入机场完整配置的用户 |
| Managed Profile + 节点快照 | 有序组合多来源 inline 节点，用户 template 自己维护组、规则、DNS；来源更新不会替换这些定制 | 与 Full 共用下载/存储/校验/代际应用；需处理节点重名、dialer 引用和来源依赖 | 作为受限的第二种模式实现；当前只提取 inline proxies，不做完整配置自动合并 |

明确采用后两者共存，Full 为默认导入路径。Managed 不是 Full 的通用替代：只追加各来源 `proxies`，保留用户模板的其他字段；不继承来源的组、规则、DNS、hosts 或 provider。来源顺序由 Profile 保存；重复来源、同名节点/组、悬空引用和循环引用应拒绝。provider-only 来源不能在此模式下提取节点，明确报错，不暗中联网展开或切换模式。节点依赖的 hosts/DNS 或 dialer 需要用户在 template 中明确提供；不能声称抽节点与原始完整订阅等价。

两个任意 Full Profile 不合并。需要多订阅时，用户可建立 Managed Profile，或在自己拥有的完整模板中明确维护 provider 与策略组。未来支持由应用下载的 provider 快照，需要另行设计资源引用、缓存及回滚边界，不能只把远程 URL 放进最终文件便宣称全部输入可复现。

## 推荐方案如何落实在当前代码

此节按共享工作区实际代码核对；完整操作流程和验证记录由专门设计文档说明。文件和运行状态仍是不同事实，以下实现不会把文件存在当作“内核正在使用”的证明。

| 责任 | 当前代码与边界 |
| --- | --- |
| 来源和配方持久化 | [configuration/library.go](../internal/configuration/library.go) 保存独立 Subscription 与 Profile；原文是按 SHA-256 寻址的 snapshot，先写 blob，再用原子 `library.json` 提交来源 revision 与配方关系。中断可留下未引用 blob，不发布半份索引 |
| 用户定制 | Profile 保存 `core`、`mode`、来源引用、用户 template 与 overrides。override 只接受 `set`、`prepend-rules`、`append-rules`；`set` 是所写顶层字段整体替换，包含 mapping，不是递归合并；禁止覆写受管网络字段 |
| 内核配置语义 | [mihomo/builder.go](../internal/mihomo/builder.go) 组合 Full/Managed 输入、检查容器和常见引用；复杂逻辑规则等由安装的 Mihomo `-t` 作最终语法/字段验证。未安装内核时仅结构验证，明确区分 validated 标记 |
| 字段所有权 | [mihomo/config.go](../internal/mihomo/config.go) 在用户定制后应用 controller、secret、监听端口、loopback 与用户服务权限策略。DNS 保留 resolver/hosts 相关配置，清空 `dns.listen`；不把整段 DNS 无条件关闭或改成固定公共 DNS。节点选择通过稳定 core home 的 `profile.store-selected` 持久化 |
| Provider 缓存 | [mihomo/providers.go](../internal/mihomo/providers.go) 按类别、名称及完整定义摘要隔离 HTTP 缓存，避免不同 URL 共用旧 path 后先加载旧节点；file provider 限定 runtime 内相对资产。此隔离不表示远程内容已纳入来源快照 |
| 业务和提交 | [app/subscriptions.go](../internal/app/subscriptions.go) 与 [app/configuration.go](../internal/app/configuration.go) 共用下载、依赖检查和构建。添加不激活；更新/编辑验证成功后只保存，显式 apply 才影响运行。刷新会验证所有依赖配方，失败保留旧来源 revision |
| 运行产物和恢复 | [app/profiles.go](../internal/app/profiles.go) 构建与校验候选，generation 记录配方和来源 SHA；当前 pointer 与恢复 journal 保护发布。正在运行时切换 generation 后请求 systemd restart，检查就绪，失败走已有恢复；停止时 apply 更新下次启动产物而不启动内核 |
| 服务与界面生命周期 | [service/systemd.go](../internal/service/systemd.go) 管理独立 systemd 用户服务；CLI/TUI 调同一个 app 用例，界面退出不 stop。当前没有系统级服务或特权 helper，不能保证未配置 linger 时用户会话结束后仍继续运行 |

当前显式 apply 使用重启，没有采用 PUT 热加载。理由不是 Mihomo 不支持热加载，而是其 `-t` 与 PUT 的成功条件不足以证明监听、DNS、TUN、provider 等所有子系统都按新配置工作。既有 generation + systemd 恢复流程更适合当前阶段；代价是显式应用会中断现有连接。以后若增加 reload，仍应复用候选校验、提交记录、结果不确定状态和恢复协议，而不是把 204 当作完成事务。

当前 readiness 只检查 systemd active、认证控制 API 可达和 mixed TCP listener 可连接，不能证明远程节点、DNS 上游、rule/provider 资源全部可达。Full Profile 中由内核自治更新的 HTTP provider 仍有自己的可变缓存；顶层来源 SHA 和 generation 不等于这些缓存也已快照化。发生外部控制 API 修改时，也不能通过 `GET /configs` 还原精确配置 hash。后续健康检查与运行漂移检测必须分别设计。

当前 TUN 明确不可用：仅有无特权 systemd 用户服务，没有独立授权的系统服务。最终构建中启用 `tun.enable` 或包含 TUN listener 会被拒绝，CLI/TUI 能力模型应显示具体原因；仅保存来源不表示该来源已经通过运行配置校验。未来系统级服务需要验证权限、所有权及 SSH/注销后的生命周期，不能只增加 YAML 开关。

未来接入 sing-box 时，Subscription 的来源、下载、revision 和原子关系管理可以复用，Profile 通过 `core` 选择对应 builder/validator；Mihomo YAML 与 sing-box JSON 的组、规则、DNS 语义留在各自内核适配器中。当前无需通用代理配置中间语言、执行 DAG 或空平台实现。
