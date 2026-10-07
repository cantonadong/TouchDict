# TouchDict 开发日志

## 2026-10-07 主窗口 WebView2

- 用户指出主窗口整体比例不对，要求沿用其指定字号。以此前确认的词语 30 pt、正文 12 pt 为基准统一比例：输入、历史行、主要按钮和卡片标题为 12 pt；搜索框缩为 54px，历史行 42px，操作按钮 44px；同步收紧侧栏、卡片内边距和圆角。保留 Figma 结构及配色，移除窗口宽度断点中的字号变化，只调整空间。字号快捷键、默认值及业务逻辑未改；未运行测试，覆盖同一路径 EXE。

- 按用户指定的 pic/TouchDict_Figma_Assets 调整主窗口：顶部通栏蓝框圆角搜索及框内查询按钮、302px 设计基准历史栏和浅蓝选中行、白色结果面板、浅蓝释义卡、紫色例句标题及翻译分隔线；底部左侧发音/固顶，右侧重试。按钮 SVG 图标使用对应独立切片的路径；字号控制放在右上角，保留已有 30/12 pt 默认和全部动作。小窗口下逐级收紧间距、侧栏和按钮尺寸，长内容继续滚动。同步内容高度测量计入面板上下边距与留白。未运行测试或启动程序，交付同一路径 EXE 供用户查看。

- 用户反馈白屏。最新日志显示 NavigationCompleted success=true 和 main page ready，排除页面未加载。对用户正在运行的进程进行只读 HWND 层级/尺寸检查，发现 Walk 客户区 Composite 位于 Chrome_WidgetWin_0 之前，二者覆盖相同区域且为同级窗口；Walk 客户区遮挡页面。将 WebView2 宿主改为 MainWindow.AsContainerBase().Handle()，使其成为客户区内部子窗口，并监听客户区 SizeChanged 更新边界。未启动应用或运行测试，重新编译交给用户验证。

- 后续用户日志明确显示指定系统 Runtime 加载成功，但 NavigationStarting 的 URI 未通过 about:blank 判定，程序主动取消主页面（WebErrorStatus=14），随后 30 秒超时。改为依据宿主发起的 NavigateToString 授权首次内嵌页面加载，不再将导航事件 URI 与文档最终 Source 混用；ready 后继续禁止跳转。未运行测试或启动程序，重新编译覆盖同一个 EXE。

- 用户后续反馈“WebView2 主窗口页面加载失败”。修正仅允许一次 about:blank 导航的初始化竞争；初始化完成前允许本地空白页导航，读取 NavigationCompleted 的具体 WebErrorStatus，区分取消导航（14）与实际加载错误。新增运行时目录、页面导航状态及 ready 日志；用户提供的 msedgewebview2.exe 所在目录明确传给加载器，优先采用系统已安装的新版本目录。重新编译覆盖原 EXE；未运行测试，待用户验证。

- 用户反馈双击托盘打开主窗口后闪退。静态追踪显示 MainWindow.Show 触发布局，而迁移后未给 Walk 客户区 Composite 设置 Layout；FormBase.startLayout 调用 CreateLayoutItemsForContainer，最终 ContainerBase.CreateLayoutItem 解引用空 layout。补上外壳 VBoxLayout，网页仍由 WebView2 渲染。GUI EXE 的运行时崩溃输出同时写入 touchdict.log，后续 panic 不再丢失堆栈。按用户要求不启动程序或运行测试，重新编译覆盖同一个 EXE，待用户复验。

- 主窗口客户区改用系统已安装的 Edge WebView2 Runtime，保留 Go 查询服务和 Walk/Win32 窗口外壳。HTML/CSS/JavaScript 内嵌 EXE，页面不依赖网络或 CDN，不下载或捆绑另一份浏览器运行时。
- 已确认用户给出的系统目录包含 msedgewebview2.exe；使用官方 loader 自动发现已安装版本，避免写死 153.0.4234.32 后在系统更新时失效。
- 保留输入/Enter 查询、拼写候选、词性释义例句翻译、朗读、重试、例句复制、固顶、历史选择/删除/清空/CSV 导出、字号快捷键和浮窗字号同步、窗口高度记忆、关闭隐藏与托盘重新打开。
- 界面采用顶部搜索、左侧历史、右侧结果卡片和独立滚动区；加载、错误、空状态和复制反馈单独展示。原生 CSV 保存对话框和清空确认仍然保留。
- 异步初始化期间保存最新状态，页面 ready 后统一恢复。查询文本作为普通文字渲染，白名单消息桥接原生操作；切换历史和模型时防止旧查询结果覆盖当前内容。
- 使用小型本地 COM 封装和依赖的嵌入加载器管理初始化/事件/关闭，避免上游 Chromium 初始化实现的阻塞消息循环及 log.Fatal。独立只读代码审查发现两处事件 vtable 索引错误，按接口定义修正。
- 已通过 Go EXE 编译（退出码 0）、JavaScript 语法检查和差异空白检查。覆盖 D:\Dev\TouchDict\artifacts\TouchDict.exe；未运行测试、未启动应用，完整交互及实际外观待用户试用。

## 2026-09-29

- 已确认完整需求、架构和实施计划。
- 环境：Windows amd64，Go 1.26.3。
- 决策：采用 Go + 原生 Windows 控件；三指轻点由 Windows 映射为鼠标中键，另提供 `Ctrl+Alt+D`。
- 安全：Gemini 密钥不写入源码或日志；设置保存时使用当前 Windows 用户的 DPAPI。
- 用户明确要求不运行测试；仅执行格式化和编译。
- 当前目录不是 Git 仓库，因此不能提交或推送。
- 已完成：DPAPI 配置、托盘生命周期、中键/快捷键触发、剪贴板取词与恢复、Gemini 结构化查询、美式 SAPI 朗读、状态浮窗、设置页和独立预览模式。
- 编译审查修复：适配 Walk/Go-OLE 实际 API；使用剪贴板序号识别“选区文本恰好等于旧剪贴板”的情况；按触发来源应用启用设置；把当前选区状态更新封送回 UI 线程。
- 已知限制：当前版本通过复制选区跨应用取词。未实现 UI Automation 上下文扩展，因此无法可靠读取未选中的整句；Gemini 会根据用户实际选中的单词或短语解释含义。
- 用户启动截图显示 `TTM_ADDTOOL failed`。根因是 Walk 依赖 Common Controls v6，但首版 EXE 未嵌入 Windows manifest；已添加应用清单资源，声明 Common Controls v6 与 Per-Monitor DPI。
- 用户反馈 hover 单词后三指轻点无反应。根因是旧流程只复制已有选区；修复为先尝试现有选区，失败后自动双击鼠标下单词再复制，并在启用三指模式时消费中键事件以避免浏览器自动滚动。
- 快捷键取词会等待 Ctrl/Alt 释放再复制，避免 `Ctrl+Alt+C` 取代预期的 `Ctrl+C`。
- 使用 imagegen 生成蓝色词典触控图标，源文件为 `assets/touchdict.png`，并生成 `assets/touchdict.ico` 嵌入 EXE/托盘。
- 用户的 Windows 三指轻点映射为左 Alt：新增对系统注入的独立左 Alt 短按监听，组合键及物理 Alt 不触发，防止干扰正常菜单快捷键。
- Gemini 诊断：该密钥的 models 列表包含固定 2.5 模型，但实际 generateContent 返回 404；官方别名 `gemini-flash-lite-latest` 实测成功。已迁移默认/旧配置并在请求前回退。
- 浮窗位移：结果状态改变会触发布局更新；现锁定 440×300 尺寸及首次显示锚点，取消强制抢焦点。
- 托盘图标改为从嵌入资源组 ID 2 显式加载，不再使用可能受缓存影响的 `IconApplication()`。
- Chrome hover 取词仍提示无单词：左 Alt 路径改为直接进入 hover 模式，使用 `SendInput` 原子发送双击、等待 Chromium 完成选区后再复制；其他触发仍优先保留已有多词选区。
- 用户设备的触控板左 Alt 事件未带 injected 标志，故放宽为任意独立短按左 Alt；组合键仍不触发。
- 浮窗边界改为 `MonitorFromPoint + GetMonitorInfo` 获取鼠标所在显示器工作区，使用像素坐标在单词附近放置并保持 12px 边距，支持副屏和负坐标。
- 发音无反馈根因是 go-ole 将 COM 的正常 `S_FALSE` 返回包装为 error；现接受该状态并平衡释放，SAPI 失败时在浮窗状态栏显示具体提示。
- 连续反馈显示 Walk 隐藏主窗口同时承担弹窗在双屏/DPI 下不可靠：显示改为 Win32 `SetWindowPos(HWND_TOPMOST)`，使用 `WS_EX_TOOLWINDOW|WS_EX_NOACTIVATE`，结果更新不再重设坐标。
- `Ctrl+Alt+D` 新增独立 `RegisterHotKey` 消息线程，保留键盘钩子作为注册失败回退。
- 发音后端替换为隐藏 Windows PowerShell 进程调用系统 `SAPI.SpVoice`；文本经 Base64 传递，避免命令注入，每次朗读会停止上一进程。
- 按用户要求完全移除鼠标中键钩子与取词入口，避免和浏览器自动滚动等常用功能冲突；三指映射左 Alt 为主触发，`Ctrl+Alt+D` 仅作兜底。

## 2026-09-30

- 新增常驻但默认隐藏的桌面主窗口：顶部输入查询，左侧显示共享历史，右侧展示与划词卡片相同的结果字段。
- 托盘菜单新增“打开主窗口”，双击托盘图标也会显示同一个窗口实例；关闭主窗口仅隐藏，程序仍驻留托盘。
- 主窗口和划词卡片通过共享查询协调层使用同一 Gemini 客户端行为，并按入口独立取消过期请求。
- Gemini 结构化响应扩展为释义或拼写候选两种状态；主窗口候选词可点击后立即查询，候选本身不写入历史。
- 现有 `touchdict_cache.json` 原地扩展为共享结果集，兼容旧条目，继续限制为最近 500 条；点击历史不重新联网。
- 设置新增默认选中的“开机自动启动”，使用当前用户 `HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run`，无需管理员权限；旧设置缺失字段时迁移为启用。
- 遵守用户要求未运行自动测试或手动 UI/网络测试；仅执行格式化、静态编译和固定路径 EXE 构建。
- 待用户验证：主窗口布局与 Enter 查询、拼写候选链接、历史实时刷新、托盘双击、登录后仅驻留托盘，以及设置开关的注册表效果。
- 根据主窗口截图定位历史栏位移根因：历史项拼接中文释义后触发 ListBox 横向滚动，选中不同长度项目会改变横向滚动位置。现改为固定 210 宽度、仅显示英文查询并将超过 25 个字符的内容省略，避免横向滚动。
- 主窗口右侧结果默认字号提高到 120%；支持 `Ctrl++`、`Ctrl+=`、`Ctrl+-` 在 80%–200% 间按 10% 调整，并把比例保存到现有用户设置供下次启动恢复。
- 按设计稿重排主窗口：增加图标与品牌标题、整行搜索区、可拖动左右分栏、结果区右上角字号按钮，以及底部居中的朗读/固顶操作；固顶按钮可切换主窗口置顶状态。
- 点击历史记录现只显示已保存的结果，不再更新最近使用顺序或改变左侧列表位置；新查询和划词结果仍按原规则进入历史。
- 用户截图确认 Walk `HSplitter` 会接管子控件宽度并忽略历史列表的最大宽度，导致左栏被扩展到约 590 像素。已移除可伸缩 Splitter，历史栏改为固定 210 像素，中间使用不可拖动分隔线，右侧结果区独占全部剩余宽度。

## 2026-10-06 Gemini 503 排查

- 日志确认连续 503 和 18 秒超时。使用现有密钥直接调用 models 列表成功，排除该诊断环境下的密钥失效和完全断网。
- 相同结构化查词请求对比：gemini-flash-lite-latest 在 25 秒后超时；gemini-3.1-flash-lite 在约 4.6 秒返回 HTTP 200 和完整 appeal 释义。定位为当前别名模型的生成服务异常，无法确定 Google 内部具体故障原因。
- 默认模型、旧配置迁移、请求前的旧模型映射及设置列表统一改为已实测可用的 gemini-3.1-flash-lite。无需用户手动改设置，不增加重试功能。

## 2026-10-06 免费模型说明与快速切换

- 核对 Gemini 官方价格、限额、计费与模型资料，新增 docs/Gemini免费模型与额度说明.md，并复制到 artifacts 供用户阅读。项目实际 RPM/TPM/RPD 需登录 AI Studio 查看，不用历史公共数字冒充当前额度。
- 设置与托盘共享 10 个兼容词典 JSON 的免费层级文本模型。音频、向量、专用机器人模型在文档单列；2.5 访问限制明确说明。
- 托盘新增模型快速切换子菜单，父项显示当前模型，选中项带勾。先持久化再修改当前配置，失败时恢复勾选；两处设置入口保存后同步菜单。
- 移除对主动选择的 2.5 模型的强制替换；保留原不稳定 flash-lite-latest 别名迁移。快速切换保存时保留字体字段。
- 查询在 UI 线程创建客户端快照再异步请求，避免模型切换改变正在启动的请求。历史与缓存仍共享，切换不删除记录。
- 按用户要求不运行测试；仅静态审查、格式化及同一路径 EXE 构建。

## 2026-10-06 主窗口高度记忆

- 主窗口默认高度改为 1200 像素。用户拖动边框调整结束后，将正常窗口高度保存到现有 settings.json 的 mainWindowHeight；重启加载恢复，旧配置缺失字段使用新默认值。
- 最大化、最小化以及内容自动撑高不写入用户高度偏好。设置页面保存、模型快速切换继续保留这个字段。
- 按用户要求未运行测试，仅格式化、静态审查并覆盖同一个 artifacts/TouchDict.exe。

## 2026-10-06 Windows 兼容缩放下的实际高度

- 只读诊断：当前主窗口物理尺寸 1800×840，旧保存高度 560；当前用户的 TouchDict EXE 兼容性标记为 ~ DPIUNAWARE，导致原生坐标被 Windows 按 150% 位图缩放。截图中的 1800 是宽度。
- 保留用户兼容性设置与现有宽度/字体，通过 LogicalToPhysicalPointForPerMonitorDPI 映射客户端内部两点，换算程序坐标与屏幕实际高度。
- 默认高度及用户高度偏好改为屏幕实际像素；初始化和内容适配将偏好转回原生坐标，手动调整保存实际高度。
- 新字段 mainWindowHeightPixels 区分单位；不沿用旧版含混的 mainWindowHeight，首次使用修复版恢复默认实际 1200，之后正常记忆。
- 未运行测试；静态审查并重新构建覆盖同一个 EXE。


### Context capture and main window sizing

- Capture the surrounding sentence using a cloned UI Automation text range, without changing the selection. Carry Context through lookup, history and retries.
- Show the source sentence above the definition and highlight the selected query using text nodes. Manual main-window searches clear the context and hide this card.
- Default main window width is 1800 screen pixels with the existing 1200-pixel height preference and compatibility scaling conversion. Results scroll inside the right panel instead of growing the window to fit content.
- Built the same artifacts/TouchDict.exe; no tests or UI launches performed. Sentence capture depends on the source app exposing a UI Automation text pattern.


### WebView2 lookup popup

- Replaced native popup content with embedded HTML/CSS/JS using the existing system-runtime Edge wrapper and the main window shared stylesheet. Both windows show the part of speech as a pale-blue rounded badge at body size.
- Kept native placement, pinning, outside-click dismissal, no-activation display and double-click term editing. Routed speech, copy, retry and edits through queued host messages. Sentence context and selected-word highlighting are shown in the popup.
- DOM measures content height; native sizing is capped to the monitor work area, with overflowing results scrolling above the action footer. Initialization errors retain a native fallback label and diagnostics.
- Compiled the same artifacts/TouchDict.exe. No tests or GUI launch performed, per user preference.


### History sense labels

- History rows show the query, a part-of-speech badge and full Chinese meaning, making contextual senses distinguishable. DOM signatures include the displayed definition fields.
- Saved results use separate IDs for different parts of speech or meanings within the same query/context. Identical senses reuse their existing ID and count, including legacy records. Cache lookup selects the most recent sense for the exact query/context. Existing overwritten senses cannot be recovered.
- No tests run; compiled the existing artifacts/TouchDict.exe for user verification.


### History alignment and contextual capture corrections

- Keep query, POS badge and meaning on one history row; truncate overflow. Share main-panel footer margins, divider position and 44px button height.
- Capture the trigger pointer before potentially slow UI Automation work. Cap popup height to the available space below the selected text, using internal scrolling instead of pushing the top edge far above the word.
- Read UI Automation from both the pointer and focused-element ancestor chains, validate selected ranges, and fall back to source text-node names. Use UTF-8 output, preserve bounds while trying another context source, and record capture path/failure and context length. Missing context is explicitly shown in the result.
- Both model prompts explicitly use sentence evidence to decide POS/meaning. No tests or GUI runs; built the same executable for user verification.


### Context reader startup failure

- User runtime log showed context reader exit status 1 and context_chars=0. Assembly inspection confirmed loading UIAutomationClient/UIAutomationTypes alone does not resolve System.Windows.Point. Explicitly load WindowsBase before constructing the pointer.
- Preserve subprocess stderr in diagnostics, and log context character count at both main and popup query submission (including retry). This distinguishes capture failure from loss between capture and request.
- Verified WindowsBase resolves System.Windows.Point by assembly inspection; no tests or source-app UI runs performed. Rebuilt the same EXE.

## 2026-10-07 — Release 1.4

- 发布 WebView2 主窗口与查词卡片、本地 GGUF 模型支持及模型切换界面。
- 原句读取改用独立的原生 UI Automation 进程，修复 Windows 日志确认的 .NET GetText 访问冲突。
- 统一界面边缘留白，调整屏幕边角定位及卡片高度限制，去除重复留白造成的滚动条。
- 删除固定词义示例，增加词条和例句关联检查，按提示词版本停用旧查询缓存并保留历史。
- Windows x64 EXE 编译通过；用户要求不运行测试，释义准确率由用户试用确认。