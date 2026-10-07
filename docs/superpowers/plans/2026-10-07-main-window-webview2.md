# TouchDict Main Window WebView2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans for inline execution, or superpowers:subagent-driven-development if the user selects delegation. Steps use checkbox syntax for tracking.

**Goal:** 主窗口全部功能使用 WebView2 展示和操作，交付可运行的原路径 EXE。

**Architecture:** 保留 Walk MainWindow 和现有回调，窗口客户区嵌入 WebView2。Go 管理状态和原生操作，内嵌 HTML/CSS/JavaScript 负责布局和交互；复用现有查询服务。

**Tech Stack:** Go、Walk/Win32、Microsoft Edge WebView2、纯 Go WebView2 bindings、原生 HTML/CSS/JavaScript。

**Spec:** `docs/superpowers/specs/2026-10-07-main-window-webview2-design.md`

## Global Constraints

- 直接在 main 工作，保留工作区已有修改。
- 用户未要求测试，不运行自动化测试或自动化界面测试。
- 编译完成后结束现有 TouchDict 进程并覆盖 artifacts/TouchDict.exe。
- 此次不迁移查词浮窗和设置窗口，不改变模型、缓存、取词、全局快捷键和托盘行为。
- HTML、CSS、JavaScript 随 EXE 内嵌，不加载远程页面、字体或 CDN，不启动本地 HTTP 服务。
- 字号范围 8–72 pt，Ctrl+0 恢复 30/12 pt。

## Review Focus

遵循用户不运行测试的要求，以下由代码审查和用户实际试用验证，不添加或执行自动化测试。

1. 页面尚未 ready 时收到历史或查询结果：必须在 ready 后恢复最新完整状态。
2. 词义包含引号、HTML、换行或超长文本：作为文字展示，正常换行，无脚本执行。
3. 查询过程中更换输入、点历史或切换模型：保持现有取消和过期结果机制，输入不被旧状态覆盖。
4. 连续删除历史或异步刷新：保留有效选中项和滚动位置，不误删搜索框中的文字。
5. 运行时缺失、窗口隐藏重开、多屏 DPI、最大化和退出：给出明确故障提示，避免空白和残留控件。

### Task 1: WebView2 嵌入组件

**Files:** Create `internal/edgeview/view_windows.go`; modify `go.mod`, `go.sum`.

**Interfaces:** `New(hwnd uintptr, onMessage func(string)) (*View, error)`; `(*View).SetHTML(string)`, `Eval(string)`, `Resize()`, `Focus()`, `Close()`.

- [x] 阅读并固定纯 Go WebView2 bindings 版本，确认已有 Walk 消息循环、COM 初始化及依赖版本兼容；优先使用 `github.com/jchv/go-webview2/pkg/edge`，不能另起应用消息循环。
- [x] 实现组件初始化、宿主 HWND 嵌入、独立用户数据目录、系统已安装运行时检测和中文错误；遵循用户补充要求，不下载或捆绑运行时。
- [x] 实现消息回调、脚本执行、调整边界、聚焦与释放；禁用默认右键菜单和页面缩放，阻止外部导航与新窗口。若绑定库未暴露必要能力，在组件内部补充最小 COM 接口，不让调用方依赖绑定细节。
- [x] 静态检查所有初始化失败路径和清理路径，核对 Review Focus 1、5。

### Task 2: 主窗口状态与原生动作迁移

**Files:** Modify `internal/mainwindow/window_windows.go`, `internal/mainwindow/resize_windows.go`, `internal/mainwindow/history_windows.go`; create `internal/mainwindow/bridge_windows.go`.

**Interfaces:** 保留 `New(Callbacks) (*Window, error)`、`Window.MW`、`Show()`、`Hide()`、`Close()`、`SetHistory([]model.HistoryEntry)`、`SetInput(string)`、`Update(model.ViewState)` 和现有 `Callbacks`。

- [x] 用 WebView2 替换主窗口原生客户区控件，在 Go 保存输入、ViewState、历史、选中 key、字号、固顶、ready 状态及待聚焦请求。
- [x] 定义 JSON 状态和白名单动作：ready、input、lookup、retry、suggestion、history-select、history-delete、history-clear、history-export、speak、copy-example、pin、zoom、content-height；验证参数和历史 key 是否存在。
- [x] 接入现有回调，保留重复查询控制、候选查询、朗读和绕过缓存重试；ready 时推送完整状态，禁止更新过程无条件覆盖用户输入。
- [x] 复用 `exportHistory()` 和清空确认/错误提示，将菜单动作同时作为页面入口可调用；复制例句使用 Windows 剪贴板并回传成功或错误提示。
- [x] 将删除键处理迁移到页面消息，保留删除后相邻选择和异步刷新逻辑；尺寸回调继续保存高度并同步浮窗字号。
- [x] 保留浮窗使用的 `ResultLayout`、`CardLayout`、`ResultHeight`、`MeasureRenderedLabel` 及其依赖文件；不删除共享布局或测量代码。
- [x] 核对全部现有调用点与 Review Focus 1、3、4，无遗漏功能。

### Task 3: 完整页面与交互

**Files:** Create `internal/mainwindow/web/index.html`, `internal/mainwindow/web/style.css`, `internal/mainwindow/web/app.js`, `internal/mainwindow/page_windows.go`.

**Interfaces:** Go 内嵌并组合页面资源；页面 `window.touchdict.applyState(state)`、`window.touchdict.focusSearch()`；向 Go 发送 JSON 字符串动作。

- [x] 实现顶部搜索、左侧历史、右侧结果布局和稳定操作区；浅色、蓝色重点色、系统字体、圆角和清晰焦点，历史和结果独立滚动。
- [x] 实现六种 ViewState 的展示、空字段隐藏、候选点击、查询和重试按钮状态、例句复制提示及失败信息。
- [x] 实现历史点击、键盘上下选择、Delete 连续删除、清空及导出入口；更新列表时保留选中和滚动。
- [x] 实现 Enter 查询并避开输入法组合输入，Ctrl 加减号及 Ctrl+0 自定义字号；显示固顶状态，控制按钮与原生行为一致。
- [x] 以 textContent 安全显示全部动态文字；状态更新不重建搜索框，事件不累积绑定。
- [x] 页面测量内容高度并发送请求，Go 限制到屏幕工作区，避免高度反馈循环；窄窗及长句通过换行和滚动保持可操作。
- [x] 静态核对 Review Focus 2、3、4；不启动自动化浏览器。

### Task 4: 窗口生命周期与启动兼容

**Files:** Modify `internal/mainwindow/resize_windows.go`, `internal/mainwindow/window_windows.go`; modify `cmd/touchdict/main_windows.go` only if initialization error handling requires it.

- [x] 将客户区尺寸变化、窗口移动和 DPI 变化转发给 WebView2，继续使用现有屏幕高度转换帮助函数。
- [x] 保留关闭隐藏、最小化恢复、固顶、搜索全选、托盘双击打开和 --main-preview 入口；页面 ready 前聚焦请求排队。
- [x] 明确关闭释放顺序，保证正常退出及初始化失败不留空白窗；隐藏重开复用页面。
- [x] 静态核对 Review Focus 5 和主程序已有查询/历史回调，避免改动工作区无关内容。

### Task 5: 编译和交付

**Files:** Update `README.md` and `docs/development-log.md` with WebView2 requirements and actual implemented behavior; overwrite `artifacts/TouchDict.exe`.

- [x] 仅格式化本次修改的 Go 文件，检查本次差异和依赖；不运行 `go test` 或界面测试。
- [x] 结束所有现有 TouchDict 进程，执行 `go build -trimpath -ldflags '-H=windowsgui -s -w' -o artifacts/TouchDict.exe ./cmd/touchdict`；预期退出码 0。依赖下载受沙箱阻挡时通过批准执行重试。
- [x] 检查生成文件路径、大小、更新时间，确认使用同一个 EXE；不为用户创建额外预览程序。
- [x] 按需求清单审查实现，说明编译结果及未运行测试，给出 EXE 路径和简洁的用户试用项目。不自行推送或发布。
