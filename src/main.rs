#![windows_subsystem = "windows"]
mod auth;
mod desktop;
mod diagnostics;
mod model;
mod native;
mod pages;
mod query;
mod selection;
mod speech;
mod storage;

fn main() {
    // Configure the installed runtime before any worker threads exist.
    let runtime = std::path::Path::new(r"C:\Program Files (x86)\Microsoft\EdgeWebView\Application\153.0.4234.32");
    if runtime.join("msedgewebview2.exe").is_file() {
        unsafe { std::env::set_var("WEBVIEW2_BROWSER_EXECUTABLE_FOLDER", runtime); }
        diagnostics::record(&format!("system WebView2 runtime={}", runtime.display()));
    }
    diagnostics::record("process entry");
    let args: Vec<String> = std::env::args().collect();
    if args.get(1).is_some_and(|a| a == "--capture") {
        let source = args.get(2).map(String::as_str).unwrap_or("alt");
        let point = native::Point {
            x: args.get(3).and_then(|v| v.parse().ok()).unwrap_or(0),
            y: args.get(4).and_then(|v| v.parse().ok()).unwrap_or(0),
        };
        let value = match selection::capture_in_process(source, point) {
            Ok(s) => serde_json::to_value(s).unwrap_or_default(),
            Err(e) => serde_json::json!({"error":e.to_string()}),
        };
        println!("{value}");
        return;
    }
    match native::single_instance() {
        Ok(Some(instance)) => {
            if let Err(e) = desktop::run(instance) {
                native::show_error(&format!("TouchDict 启动失败：{e:#}"));
            }
        }
        Ok(None) => native::show_error("TouchDict 已在运行，请从系统托盘打开。"),
        Err(e) => native::show_error(&format!("无法检查运行实例：{e}")),
    }
}
