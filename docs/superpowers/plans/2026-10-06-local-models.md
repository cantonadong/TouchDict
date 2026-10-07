# Local GGUF Models Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans. User rules override worktree and testing defaults: work on main, do not run tests, overwrite the same EXE.

**Goal:** Query the user's Gemma/Qwen GGUF models and switch between local and Gemini models from settings and tray.

**Architecture:** A lookup interface preserves the existing query coordinator. A managed llama.cpp CPU server loads only the selected GGUF file, listens on loopback, returns constrained dictionary JSON, and is owned by the app. Common history/cache remains shared.

**Tech Stack:** Go, Windows Walk tabs/menus, official llama.cpp Windows CPU binaries.

**Requirements:** D:\Models default directory, recursive GGUF discovery, editable directory with search button and single-select dropdown, local/online settings tabs, top-level 在线模型 / 本地模型 tray submenus, persistent provider and per-provider selections. No model files copied or modified; no automatic cloud fallback from local queries.

## Review Focus

- Empty/unreadable directory and missing selected model: clear error, cannot save invalid local selection.
- Settings cancel: draft path/list changes do not alter active configuration.
- Model switch/exit: only app-owned server stops; orphan process prevention and bounded startup.
- Query cancellation/model load: no stale response and local timeout longer than online.
- Small-model JSON/think output: schema and validation protect existing result/history fields.

## Tasks

- [x] Add local model discovery and persisted provider/path/file settings.
- [x] Add managed runtime and local dictionary client; generalize lookup factory and reuse cache.
- [x] Add settings tabs and rebuild online/local tray submenus after settings changes.
- [x] Download pinned official CPU runtime beside EXE, document packaging and lifecycle.
- [x] Format, compile same EXE, static review and fix findings; no tests.

## Rulings

- User has asked to implement directly and previously rejected design approval loops. Proceed within the specified behavior without another approval gate.
- GGUF is the discovered format; use llama.cpp rather than requiring Ollama installation/import.
- Local inference loads lazily, binds 127.0.0.1 with an app-generated token, uses a Windows job object for cleanup, and never downloads a model.
- Use CPU runtime for portability; performance is user-verified. Existing shared cache remains usable across providers.
