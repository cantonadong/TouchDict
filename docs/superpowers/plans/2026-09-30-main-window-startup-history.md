# TouchDict Main Window, Shared History, and Startup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a tray-opened desktop lookup window, typo suggestions, a shared persistent 500-result history, and default-enabled Windows login startup.

**Architecture:** Introduce a query coordinator used by both popup and main-window entry points, and promote the current Gemini cache into the single observable result repository. Keep window code presentation-only, extend Gemini's structured response to return either a definition or spelling suggestions, and let settings own synchronization of the per-user Windows startup registry entry.

**Tech Stack:** Go 1.26+, `github.com/lxn/walk`, Win32 APIs, Windows Registry, Gemini REST structured JSON.

**Spec:** `docs/superpowers/specs/2026-09-30-main-window-startup-history-design.md`

## Global Constraints

- Work directly on `main`; do not create or use another branch or worktree.
- Target Windows 11 x64 and remain compatible with Windows 10 x64.
- Keep one GUI executable at `D:\Dev\TouchDict\artifacts\TouchDict.exe`, overwriting it for every user build.
- Do not run automated or manual tests; the user explicitly permits only formatting, static compilation, and output-file inspection.
- Before overwriting the EXE, terminate every running `TouchDict.exe` process.
- Keep Gemini credentials out of source, logs, cache entries, and error messages.
- Preserve old `settings.json` and `touchdict_cache.json` compatibility.
- Use the existing cache as the only persistent result set and retain its 500-entry limit.
- Startup launches to the tray only; closing the main window hides it and only the tray Exit command terminates the app.

## Review Focus

- Old settings without `startupEnabled`: migration must yield `true` without overwriting unrelated preferences.
- EXE paths containing spaces or quotes: the HKCU Run value must contain a correctly quoted executable path.
- Duplicate terms with different selection context: retain distinct cached definitions while displaying intelligible history labels.
- Rapid queries and late network responses: an older response must not replace a newer state in the same UI entry point.
- Malformed Gemini output containing both/neither definition and suggestions: reject it without caching partial content.

---

### Task 1: Result Model, Gemini Union Response, and Observable 500-Entry Repository

**Files:**
- Modify: `internal/model/model.go`
- Modify: `internal/gemini/client.go`
- Modify: `internal/gemini/cache_windows.go`

**Interfaces:**
- Produces: `model.QueryResult` with exactly one of `Definition *Definition` or `Suggestions []string`.
- Produces: `model.HistoryEntry` containing cache key, query text, context, and definition.
- Produces: `gemini.Client.Lookup(ctx context.Context, s model.Selection) (model.QueryResult, error)`.
- Produces: `gemini.History() []model.HistoryEntry`, `gemini.Touch(key string)`, and `gemini.SubscribeHistory(func([]model.HistoryEntry)) func()`.

- [ ] **Step 1: Extend the shared model**

Add query-result and history types plus a spelling-suggestion view kind. Limit suggestions to five unique, trimmed entries of at most 80 runes.

- [ ] **Step 2: Change Gemini structured output to a discriminated result**

Require a response `type` of `definition` or `suggestions`; validate that exactly the matching payload is useful. Keep all existing definition length validation, and never cache suggestions.

- [ ] **Step 3: Promote the disk cache to the shared history repository**

Persist query text and context alongside each definition while accepting legacy entries that only contain the old key and definition. Return newest-first snapshots, move selected entries to the newest position, notify subscribers after successful mutations, and keep the limit at 500.

- [ ] **Step 4: Format and statically compile the task**

Run: `gofmt -w internal/model internal/gemini` then `go build ./...`
Expected: exit code 0. This is compilation only, not a test run.

- [ ] **Step 5: Commit**

Commit message: `feat: share query results and history`

### Task 2: Shared Query Coordinator

**Files:**
- Create: `internal/query/service.go`
- Modify: `cmd/touchdict/main_windows.go`

**Interfaces:**
- Consumes: `gemini.Client.Lookup`, `gemini.History`, `gemini.Touch`, and model query states.
- Produces: `query.New(clientFactory func() *gemini.Client, timeout time.Duration) *Service`.
- Produces: `Service.Lookup(scope string, selection model.Selection, emit func(model.ViewState))` and `Service.Cancel(scope string)`.
- Produces: `Service.History() []model.HistoryEntry`, `Service.SelectHistory(key string) (model.Definition, bool)`, and `Service.SubscribeHistory(...)`.

- [ ] **Step 1: Implement per-scope cancellation and generation ownership**

Give popup and main-window lookups independent scope names. A new request cancels the previous request in its scope and only the current generation may emit a terminal state.

- [ ] **Step 2: Centralize cache-hit, network, suggestion, and error state production**

The service emits loading first, returns cached definitions without network access, stores only valid definitions, maps suggestions to the new view state, and marks retryable errors consistently.

- [ ] **Step 3: Route the existing popup through the service**

Replace `startLookup` and direct cache checks in `cmd/touchdict/main_windows.go` while preserving popup placement, speech, retry, logging, and selection capture behavior.

- [ ] **Step 4: Format and statically compile the task**

Run: `gofmt -w internal/query cmd/touchdict` then `go build ./...`
Expected: exit code 0.

- [ ] **Step 5: Commit**

Commit message: `refactor: centralize TouchDict queries`

### Task 3: Main Window and Tray Entry Points

**Files:**
- Create: `internal/mainwindow/window_windows.go`
- Create: `internal/mainwindow/result_windows.go`
- Modify: `cmd/touchdict/main_windows.go`
- Modify: `internal/popup/window_windows.go`

**Interfaces:**
- Consumes: query-service lookup, history snapshot/selection/subscription, speech callbacks, and `model.ViewState`.
- Produces: `mainwindow.New(callbacks Callbacks) (*Window, error)`.
- Produces: `Window.Show()`, `Window.Hide()`, `Window.Update(model.ViewState)`, `Window.SetHistory([]model.HistoryEntry)`, and `Window.Close()`.
- Produces callbacks for typed lookup, history selection, suggestion selection, retry, and pronunciation.

- [ ] **Step 1: Build the resizable main-window shell**

Create a top input/button row, a scrollable left history list with a stable minimum width, and a flexible right result pane. Enter and the button submit the same trimmed input; blank input remains local.

- [ ] **Step 2: Build the shared-content result pane**

Display the same definition fields and pronunciation action as the popup, plus loading/error states and up to five clickable spelling suggestions. Selecting a suggestion replaces the input and immediately submits it.

- [ ] **Step 3: Connect shared history**

Load the newest-first snapshot when the window is created, subscribe to later popup/main query mutations, and display a selected cached result without network access while moving it to the newest position.

- [ ] **Step 4: Add tray opening behavior**

Add “打开主窗口”, attach notify-icon double-click to the same single-window `Show` action, and make the main-window close button hide rather than dispose. Keep startup tray-only and tray Exit authoritative.

- [ ] **Step 5: Update popup suggestion handling defensively**

If a popup query returns suggestions, show a concise instruction to open the main window rather than exposing unusable link-like text in the compact card.

- [ ] **Step 6: Format and statically compile the task**

Run: `gofmt -w internal/mainwindow internal/popup cmd/touchdict` then `go build ./...`
Expected: exit code 0.

- [ ] **Step 7: Commit**

Commit message: `feat: add desktop lookup window`

### Task 4: Default-Enabled Windows Login Startup

**Files:**
- Modify: `internal/settings/settings_windows.go`
- Modify: `internal/settings/window_windows.go`
- Create: `internal/settings/startup_windows.go`
- Modify: `cmd/touchdict/main_windows.go`

**Interfaces:**
- Produces: `Config.StartupEnabled bool` with missing-field migration to `true`.
- Produces: `settings.SyncStartup(exePath string, enabled bool) error` targeting `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, value name `TouchDict`.

- [ ] **Step 1: Add migration-safe startup configuration**

Use a pointer only in the disk representation so omitted legacy JSON maps to enabled while an explicit `false` remains false.

- [ ] **Step 2: Implement current-user startup registration**

Write a correctly quoted absolute EXE command when enabled and remove only the `TouchDict` value when disabled. Treat an already-missing value as successful removal.

- [ ] **Step 3: Add the settings checkbox and atomic save flow**

Show “开机自动启动” checked from config. Synchronize the registry before accepting the dialog; on failure keep the dialog open, show the error, and do not claim success.

- [ ] **Step 4: Reconcile the default on application startup**

After loading configuration, synchronize the Run entry so first launch and upgraded legacy installations receive the default-enabled behavior. Log only the error category/path-independent message on failure and continue running.

- [ ] **Step 5: Format and statically compile the task**

Run: `gofmt -w internal/settings cmd/touchdict` then `go build ./...`
Expected: exit code 0.

- [ ] **Step 6: Commit**

Commit message: `feat: enable login startup by default`

### Task 5: Documentation, Final Build, and Delivery

**Files:**
- Modify: `README.md`
- Modify: `docs/development-log.md`
- Modify: `build.ps1` only if the existing fixed-output build no longer covers new files.

**Interfaces:**
- Consumes: all preceding tasks.
- Produces: updated user documentation and `D:\Dev\TouchDict\artifacts\TouchDict.exe`.

- [ ] **Step 1: Document the finished user flows**

Describe opening the main window, manual lookup, shared 500-entry history, spelling links, tray-only startup, and the default-enabled startup setting.

- [ ] **Step 2: Record implementation decisions and user verification items**

Update the development log with migration behavior, cache sharing, coordinator ownership, build environment, known limitations, and the UI/network checks intentionally left to the user.

- [ ] **Step 3: Inspect the complete diff against the spec**

Run: `git diff --check`, `git status --porcelain=v1`, and review every changed file. Expected: no whitespace errors, no unrelated edits, no credential content, and every acceptance criterion represented.

- [ ] **Step 4: Build the fixed user-testing executable**

Run: `powershell -NoProfile -ExecutionPolicy Bypass -File .\build.ps1`
Expected: all existing `TouchDict.exe` processes are terminated first, compilation exits 0, and the same `artifacts\TouchDict.exe` path is overwritten.

- [ ] **Step 5: Inspect the artifact without launching it**

Run: `Get-Item .\artifacts\TouchDict.exe | Select-Object FullName,Length,LastWriteTime`
Expected: the fixed path exists, has non-zero length, and has the current build timestamp.

- [ ] **Step 6: Commit**

Commit message: `docs: document main window workflow`
