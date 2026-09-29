# TouchDict Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a lightweight Windows desktop dictionary that captures selected English text on a three-finger/middle-click trigger, queries Gemini for contextual definitions, and presents the result with American-English speech in a native popup.

**Architecture:** A single Go Win32 GUI executable owns the tray lifecycle, trigger hooks, selection capture, Gemini client, SAPI speech service, and custom-drawn popup. Modules communicate through typed interfaces and immutable result/state values; the popup never performs network or selection work directly. A preview mode drives the same popup with mock states before that component is connected to the application controller.

**Tech Stack:** Go 1.26+, `golang.org/x/sys/windows`, Windows Win32/UI Automation/Clipboard/SAPI APIs, Gemini REST API, standard-library HTTP/JSON.

**Spec:** `docs/superpowers/specs/2026-09-29-touchdict-design.md`

## Global Constraints

- Target Windows 11 x64 and remain compatible with Windows 10 x64.
- Produce one GUI executable at `D:\Dev\TouchDict\artifacts\TouchDict.exe` and overwrite that same file for every user build.
- Use only the `main` branch if Git becomes available; the supplied directory currently has no `.git` metadata or remote.
- Do not run automated or manual tests; the user explicitly requested the EXE without assistant-side testing.
- Verification is limited to formatting, static compilation, and checking that the requested output file exists.
- Never embed or log the value from `gemini_key.txt`; copy it next to the EXE only as a local runtime credential.
- Keep idle work event-driven; do not poll continuously.
- Preview must cover normal, loading, empty, error, and edge-case data using the same popup component as production.
- Before overwriting the final EXE, terminate all running `TouchDict.exe` processes.

## Review Focus

- Clipboard formats owned by another process: preserve supported original content and never clear it on capture failure.
- UI Automation unavailable, hung, or returning a stale selection: fall back with a bounded timeout and keep the UI responsive.
- Multiple triggers while a request or speech operation is active: cancel/replace the older operation without showing stale results.
- Mixed-language, blank, or very long selections: reject or truncate deterministically before sending data externally.
- Gemini timeout, HTTP error, blocked network, invalid JSON, or missing fields: produce a retryable typed error without exposing the key or response body.

---

### Task 1: Project foundation, contracts, and safe local configuration

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `cmd/touchdict/main_windows.go`
- Create: `internal/model/model.go`
- Create: `internal/settings/settings_windows.go`
- Create: `internal/logging/logging.go`
- Create: `docs/development-log.md`
- Create: `build.ps1`

**Interfaces:**
- Produces: `model.Selection`, `model.Definition`, `model.ViewState`, and typed application errors.
- Produces: `settings.Load(exeDir string) (Config, error)` and `Config.Save() error` with DPAPI-protected stored keys.
- Produces: `logging.Open() (*log.Logger, io.Closer, error)` with redacted, bounded diagnostics.

- [ ] Create the Go module and Windows-only application entry point with an event-driven controller skeleton.
- [ ] Define shared immutable values and error categories used by later modules.
- [ ] Implement configuration precedence: DPAPI-protected saved key, `GEMINI_API_KEY`, then EXE-adjacent `gemini_key.txt`.
- [ ] Implement local application-data paths and a size-bounded diagnostic logger that accepts categories rather than request bodies.
- [ ] Add `.gitignore` entries for credentials, artifacts, logs, and transient Go output while retaining the three user-supplied instruction files.
- [ ] Add a repeatable PowerShell build script that formats source, builds a Windows GUI x64 EXE, and never runs tests.
- [ ] Record architecture, environment, credential rules, and current milestone in the development log.
- [ ] Run `gofmt` and `go build` only; expected result is a compiling controller skeleton.

### Task 2: Native popup and preview state driver

**Files:**
- Create: `internal/popup/window_windows.go`
- Create: `internal/popup/layout.go`
- Create: `internal/popup/theme.go`
- Create: `internal/preview/preview.go`
- Modify: `cmd/touchdict/main_windows.go`
- Modify: `docs/development-log.md`

**Interfaces:**
- Consumes: `model.ViewState` and `model.Definition`.
- Produces: `popup.New(callbacks Callbacks) (*Window, error)`, `Window.ShowAt(point Point, state model.ViewState)`, `Window.Update(state model.ViewState)`, `Window.Hide()`, and `Window.Close()`.
- Produces: preview state fixtures and keyboard/menu actions for switching normal, loading, empty, error, and edge-case states.

- [ ] Register a per-monitor-DPI-aware, borderless, topmost Win32 popup class with activation, Escape, click-outside, and monitor-boundary behavior.
- [ ] Implement one custom layout and drawing path for all states, including scalable typography, rounded card, shadow, blue accent, and disabled/error treatments.
- [ ] Add hit targets for pronunciation, retry, settings, and close actions with keyboard-accessible equivalents.
- [ ] Add bounded fade/loading animation that stops when hidden and honors reduced-animation settings.
- [ ] Implement `--preview` using mock data and direct state switching, without network, selection, credential, or speech dependencies.
- [ ] Connect the production entry point to the same popup component without adding business behavior.
- [ ] Update the development log with the UI contract and preview controls.
- [ ] Run `gofmt` and `go build` only; expected result is a compiling preview-capable executable.

### Task 3: Tray lifecycle, single instance, trigger hooks, and settings UI

**Files:**
- Create: `internal/app/controller_windows.go`
- Create: `internal/tray/tray_windows.go`
- Create: `internal/trigger/trigger_windows.go`
- Create: `internal/settings/window_windows.go`
- Modify: `cmd/touchdict/main_windows.go`
- Modify: `docs/development-log.md`

**Interfaces:**
- Produces: `trigger.Listener.Start(events chan<- Event) error`, `Listener.SetEnabled(bool)`, and `Listener.Close()`.
- Produces: `tray.New(Callbacks)`, pause/resume, preview, settings, help, and exit actions.
- Consumes: popup methods and settings configuration from prior tasks.

- [ ] Add a named-mutex single-instance guard and bring the running instance forward when a second launch occurs.
- [ ] Add a hidden message window and tray icon with Pause, Preview, Settings, Setup Help, and Exit commands.
- [ ] Register default global hotkey `Ctrl+Alt+D` and a non-blocking low-level middle-button hook; preserve ordinary middle clicks.
- [ ] Debounce duplicate trigger events and marshal all callbacks onto the owning window thread.
- [ ] Implement a compact settings window for Gemini key, trigger enablement, shortcut, auto-pronunciation, and startup preference.
- [ ] Show explicit Windows instructions for mapping three-finger tap to middle mouse.
- [ ] Save secrets via DPAPI and non-secret preferences in local app data.
- [ ] Update the development log and run `gofmt` plus `go build` only.

### Task 4: Selection capture with UI Automation and clipboard fallback

**Files:**
- Create: `internal/selection/selection_windows.go`
- Create: `internal/selection/uia_windows.go`
- Create: `internal/selection/clipboard_windows.go`
- Create: `internal/selection/text.go`
- Modify: `internal/app/controller_windows.go`
- Modify: `docs/development-log.md`

**Interfaces:**
- Produces: `selection.Reader.Read(ctx context.Context, foreground windows.Handle) (model.Selection, error)`.
- Produces: normalized selected text, bounded context, source application identity, and selection method.
- Consumes: trigger events and publishes loading/empty/error states to the popup.

- [ ] Initialize UI Automation on a dedicated COM STA and read TextPattern/TextPattern2 selections plus a bounded surrounding range.
- [ ] Add timeouts and release every COM interface deterministically.
- [ ] Implement clipboard snapshot/restore for text and safely restorable common formats, simulated `Ctrl+C`, change-sequence waiting, and bounded retries.
- [ ] Normalize Unicode whitespace, reject blank/non-Latin input, cap selection and context lengths, and detect multiword input.
- [ ] Keep foreground focus stable and position the loading popup near the cursor after a valid selection is obtained.
- [ ] Ensure a new trigger cancels the prior selection attempt and prevents stale completion.
- [ ] Update the development log and run `gofmt` plus `go build` only.

### Task 5: Gemini contextual dictionary client

**Files:**
- Create: `internal/gemini/client.go`
- Create: `internal/gemini/prompt.go`
- Create: `internal/gemini/response.go`
- Modify: `internal/app/controller_windows.go`
- Modify: `internal/settings/window_windows.go`
- Modify: `docs/development-log.md`

**Interfaces:**
- Produces: `gemini.Client.Lookup(ctx context.Context, selection model.Selection) (model.Definition, error)`.
- Consumes: API key, selected text, bounded context, and cancellable request context.
- Produces: validated fields `Term`, `PartOfSpeech`, `MeaningZH`, `ExampleEN`, and `ExampleZH`, or a typed retryable/non-retryable error.

- [ ] Implement an HTTP client with connection/request timeouts, cancellation, proxy support from Windows environment, and no secret-bearing logs.
- [ ] Request Gemini structured JSON output using the currently documented stable/free-tier-compatible model and response schema.
- [ ] Build an injection-resistant prompt that treats selection/context as data and requests only the five dictionary fields.
- [ ] Decode the documented response envelope, validate all fields and length limits, and perform one conservative fenced-JSON cleanup only.
- [ ] Map missing key, authentication, quota/rate limit, timeout, connectivity, server error, safety block, and malformed response into UI error categories.
- [ ] Add a small concurrency-safe, size-bounded in-memory cache keyed by normalized selection plus context.
- [ ] Wire loading, success, retry, cancellation, and stale-result suppression into the controller.
- [ ] Update the development log and run `gofmt` plus `go build` only.

### Task 6: American-English speech and full interaction wiring

**Files:**
- Create: `internal/speech/speech_windows.go`
- Modify: `internal/app/controller_windows.go`
- Modify: `internal/popup/window_windows.go`
- Modify: `docs/development-log.md`

**Interfaces:**
- Produces: `speech.Service.Speak(text string) error`, `Service.Stop()`, `Service.Available() bool`, and `Service.Close()`.
- Consumes: successful definitions and pronunciation-button callbacks.

- [ ] Initialize SAPI on its own COM apartment and select an installed `en-US` voice, preferring an American English voice token.
- [ ] Queue at most one utterance, stop older speech on new lookup/hide/exit, and keep SAPI callbacks off the UI thread.
- [ ] Auto-pronounce once after a new successful lookup when enabled; never auto-repeat cached view updates.
- [ ] Connect pronunciation, retry, settings, close, Escape, click-outside, tray pause/resume, and exit behavior end to end.
- [ ] Disable the pronunciation control with a concise explanation when no suitable voice is installed.
- [ ] Update the development log and run `gofmt` plus `go build` only.

### Task 7: Packaging and user-test executable

**Files:**
- Create: `assets/app.manifest`
- Create: `assets/touchdict.ico` or generate equivalent resource data using repository code/tools already available.
- Create: `internal/resource/resource_windows.syso` if a resource compiler path is available; otherwise use a Go-native resource embedding path.
- Modify: `build.ps1`
- Modify: `README.md`
- Modify: `docs/development-log.md`

**Interfaces:**
- Produces: `D:\Dev\TouchDict\artifacts\TouchDict.exe`.
- Produces: adjacent local-only `gemini_key.txt` for this user's test build without embedding its contents in the binary.

- [ ] Add application identity, DPI-awareness, Windows compatibility manifest, version metadata, and icon without introducing a runtime dependency.
- [ ] Document startup, three-finger-to-middle-click setup, fallback shortcut, preview invocation, credential precedence, tray controls, and known application-specific selection limitations.
- [ ] Make `build.ps1` resolve its own project root, stop all running `TouchDict.exe` instances, create the fixed artifacts directory, format, and build with `-trimpath` and Windows GUI linker flags.
- [ ] Ensure the build script copies the existing credential file to the artifact directory for local testing without printing it.
- [ ] Run the build script without launching the application and without running tests.
- [ ] Confirm only that `artifacts\TouchDict.exe` exists, is non-empty, and has a current timestamp.
- [ ] Update the development log with delivered path, build environment, skipped-test decision, known risks, and exact items requiring user verification.
