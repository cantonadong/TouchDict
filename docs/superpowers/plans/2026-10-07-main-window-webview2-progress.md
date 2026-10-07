# Execution ledger — docs/superpowers/plans/2026-10-07-main-window-webview2.md

- User approved spec and plan; inline execution selected.
- Main branch confirmed; pre-existing working changes retained.
- User rules override worktree and automated test workflows; build and static review only.
- Pre-flight: Window.MW and existing callbacks remain the integration boundary. Popup imports shared layout and text measurement, so these files remain.
- Task 1 in progress. Dependency downloaded. Ruling: use bindings' embedded loader with a small local COM wrapper instead of Chromium. The wrapper handles asynchronous failure without the upstream Chromium implementation's log.Fatal calls and blocking initialization loop. Public wrapper adds an error callback; only mainwindow consumes it.
- User clarified no Runtime download: confirmed system directory contains msedgewebview2.exe. Use official loader discovery of existing Evergreen installation; no Runtime installer or bundled browser.
- Tasks 1–4 implemented: asynchronous COM host, complete main-window action bridge, embedded page, resize/focus/hide lifecycle, query generation guard on history/model switching. Popup shared layout and measurement remain untouched.
- First EXE compilation succeeded (go build exit 0); no tests or app launches performed.
- Final read-only independent review found incorrect PermissionRequested and ProcessFailed vtable event indices. Verified against bindings' base ICoreWebView2 vtable and corrected to 23 and 25. Added uintptr escape annotation for native out parameters and guard against delayed ready events after failure. Final build pending.
- Tasks 1–5: complete. Final go build exit 0; artifacts/TouchDict.exe overwritten (10,912,256 bytes). node --check passed; scoped git diff --check passed. No automated tests, network trials, or app launches. No commits, pushes, branches or worktrees created; user's existing work remains on main. No deferred review findings.
- Ruling: no feature-branch integration menu or cleanup — user explicitly requires main-only work and delivery of the existing EXE; retain work in the current workspace for user testing.
