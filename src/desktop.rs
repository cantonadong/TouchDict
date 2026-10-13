use crate::{
    auth::Auth,
    model::{Answer, Definition, Selection},
    native,
    pages::{self, Page},
    query::Backend,
    selection,
    speech::Speech,
    storage::{Config, History, MainWindowSize},
};
use anyhow::Result;
use serde::Serialize;
use serde_json::{Value, json};
use std::{
    sync::{
        Arc, Mutex,
        atomic::{AtomicBool, Ordering},
    },
    thread,
};
use tao::{
    dpi::{PhysicalPosition, PhysicalSize},
    event::{Event, WindowEvent},
    event_loop::{ControlFlow, EventLoopBuilder, EventLoopProxy},
    window::{Window, WindowBuilder},
};
use tray_icon::{
    MouseButton, TrayIcon, TrayIconBuilder, TrayIconEvent,
    menu::{CheckMenuItem, Menu, MenuEvent, MenuItem, PredefinedMenuItem, Submenu},
};
use wry::{NewWindowResponse, WebView, WebViewBuilder};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Scope {
    Main,
    Popup,
    Settings,
}
enum AppEvent {
    Ipc(Scope, String),
    Menu(String),
    Open,
    Trigger(String),
    Captured(u64, native::Point, Result<Selection, String>),
    Query(Scope, u64, Selection, Option<u64>, Result<Answer, String>),
    Auth(Value, Result<(), String>, bool),
    Models(Value, Result<(), String>),
    Export(Result<(), String>),
}
#[derive(Clone, Serialize)]
struct State {
    kind: String,
    selection: String,
    context: String,
    definition: Definition,
    message: String,
    suggestions: Vec<String>,
}
impl Default for State {
    fn default() -> Self {
        Self {
            kind: "empty".into(),
            selection: String::new(),
            context: String::new(),
            definition: Definition::default(),
            message: String::new(),
            suggestions: vec![],
        }
    }
}
struct View {
    window: Window,
    web: WebView,
    ready: bool,
    state: State,
    pinned: bool,
    request: u64,
    input: String,
    input_revision: u64,
}
struct Tray {
    _icon: TrayIcon,
    pause: CheckMenuItem,
    models: Submenu,
}
struct App {
    main: View,
    popup: View,
    settings: View,
    tray: Tray,
    config: Config,
    history: History,
    backend: Arc<Backend>,
    proxy: EventLoopProxy<AppEvent>,
    speech: Speech,
    auth_status: Value,
    auth_busy: bool,
    models_busy: bool,
    models_message: String,
    auth_message: String,
    auth_cancel: Option<Arc<AtomicBool>>,
    paused: bool,
    selected_key: String,
    capture_request: u64,
    popup_visible: bool,
    popup_mouse_was_down: bool,
    popup_trigger: Option<native::Point>,
    pending_main_size: Option<(std::time::Instant, PhysicalSize<u32>)>,
    _hooks: Option<native::Hooks>,
    _instance: native::Instance,
}

pub fn run(instance: native::Instance) -> Result<()> {
    let config = Config::load()?;
    let history = History::load()?;
    let auth = Auth::load().unwrap_or_else(|e| {
        native::show_error(&format!("已有订阅连接无法读取，请重新授权：{e}"));
        Auth::empty()
    });
    let mut config = config;
    if !auth
        .saved
        .models
        .iter()
        .any(|m| config.model == format!("chatgpt:{}", m.slug))
    {
        if let Some(model) = auth.saved.models.first() {
            config.model = format!("chatgpt:{}", model.slug);
        }
    }
    let auth_status = auth.status();
    let auth = Arc::new(Mutex::new(auth));
    let backend = Arc::new(Backend::new(auth)?);
    let event_loop = EventLoopBuilder::<AppEvent>::with_user_event().build();
    let proxy = event_loop.create_proxy();
    let main = make_view(&event_loop, &proxy, Scope::Main)?;
    let popup = make_view(&event_loop, &proxy, Scope::Popup)?;
    let settings = make_view(&event_loop, &proxy, Scope::Settings)?;
    let tray = make_tray(&proxy)?;
    let p = proxy.clone();
    let hooks = native::start_hooks(move |source| {
        let _ = p.send_event(AppEvent::Trigger(source.into()));
    });
    let hooks = match hooks {
        Ok(h) => Some(h),
        Err(e) => {
            native::show_error(&format!("划词监听启动失败：{e}"));
            None
        }
    };
    let mut app = App {
        main,
        popup,
        settings,
        tray,
        config,
        history,
        backend,
        proxy,
        speech: Speech::new(),
        auth_status,
        auth_busy: false,
        models_busy: false,
        models_message: String::new(),
        auth_message: String::new(),
        auth_cancel: None,
        paused: false,
        selected_key: String::new(),
        capture_request: 0,
        popup_visible: false,
        popup_mouse_was_down: false,
        popup_trigger: None,
        pending_main_size: None,
        _hooks: hooks,
        _instance: instance,
    };
    app.apply_preferences();
    app.refresh_tray();
    let _ = native::sync_startup(app.config.startup_enabled);
    if !std::env::args().any(|a| a == "--background") {
        app.main.window.set_visible(true);
        app.main.window.set_focus();
    }
    event_loop.run(move |event, _, flow| {
        *flow = if app.popup_visible {
            ControlFlow::WaitUntil(std::time::Instant::now() + std::time::Duration::from_millis(30))
        } else if let Some((deadline, _)) = app.pending_main_size {
            ControlFlow::WaitUntil(deadline)
        } else {
            ControlFlow::Wait
        };
        match event {
            Event::UserEvent(AppEvent::Ipc(scope, message)) => {
                if let Err(e) = app.ipc(scope, &message, flow) {
                    app.notice(scope, &e.to_string())
                }
            }
            Event::UserEvent(AppEvent::Menu(id)) => app.menu(&id, flow),
            Event::UserEvent(AppEvent::Open) => app.show_main(false),
            Event::UserEvent(AppEvent::Trigger(source)) => app.capture(&source),
            Event::UserEvent(AppEvent::Captured(id, point, result)) => {
                if id == app.capture_request {
                    match result {
                        Ok(s) => {
                            app.show_popup(point);
                            app.lookup(Scope::Popup, s, false, true)
                        }
                        Err(e) => {
                            app.show_popup(point);
                            app.popup.state = State {
                                kind: "error".into(),
                                message: e,
                                ..Default::default()
                            };
                            app.render();
                        }
                    }
                }
            }
            Event::UserEvent(AppEvent::Query(scope, id, s, mirror, result)) => {
                if id == app.view(scope).request {
                    app.finish_query(scope, s, mirror, result)
                } else if scope == Scope::Popup && mirror == Some(app.main.request) {
                    // A dismissed popup must not leave its mirrored main result loading.
                    app.finish_query(Scope::Main, s, None, result)
                }
            }
            Event::UserEvent(AppEvent::Models(models, result)) => {
                app.models_busy = false;
                app.models_message = match result {
                    Ok(()) => {
                        if app.auth_status["connected"].as_bool().unwrap_or(false) {
                            app.auth_status["models"] = models;
                        }
                        "模型列表已刷新".into()
                    }
                    Err(error) => error,
                };
                app.render();
            }
            Event::UserEvent(AppEvent::Auth(status, result, activate)) => {
                app.auth_busy = false;
                app.auth_cancel = None;
                app.auth_status = status;
                let previous_model = app.config.model.clone();
                let connected = app.auth_status["connected"].as_bool().unwrap_or(false);
                if !connected {
                    app.config.model.clear();
                }
                app.auth_message = match result {
                    Ok(()) => {
                        if activate {
                            if let Some(model) = app.auth_status["models"]
                                .as_array()
                                .and_then(|v| {
                                    v.iter()
                                        .find(|m| m["id"] == app.config.model)
                                        .or_else(|| v.first())
                                })
                                .and_then(|v| v["id"].as_str())
                            {
                                app.config.model = model.into();
                                if let Err(e) = app.config.save() {
                                    app.notice(
                                        Scope::Main,
                                        &format!("已连接，但模型保存失败：{e}"),
                                    );
                                }
                            }
                        }
                        if connected {
                            "已连接，可开始查词。".into()
                        } else {
                            "已断开连接。".into()
                        }
                    }
                    Err(e) => e,
                };
                if previous_model != app.config.model {
                    app.main.request += 1;
                    app.popup.request += 1;
                    app.main.state = State::default();
                    app.popup.state = State::default();
                    if let Err(e) = app.config.save() {
                        app.notice(Scope::Main, &e.to_string());
                    }
                }
                app.refresh_tray();
                app.render();
            }
            Event::UserEvent(AppEvent::Export(result)) => app.notice(
                Scope::Main,
                &result
                    .map(|_| "查词记录已导出".into())
                    .unwrap_or_else(|e| e),
            ),
            Event::WindowEvent {
                window_id,
                event: WindowEvent::CloseRequested,
                ..
            } => {
                if window_id == app.main.window.id() {
                    app.main.window.set_visible(false);
                } else if window_id == app.popup.window.id() {
                    app.hide_popup();
                } else if window_id == app.settings.window.id() {
                    app.settings.window.set_visible(false);
                }
            }
            Event::MainEventsCleared => {
                if app.pending_main_size.is_some_and(|(deadline, _)| std::time::Instant::now() >= deadline) {
                    app.save_main_size();
                }
                if app.popup_visible {
                    let down = native::mouse_down();
                    let clicked = down && !app.popup_mouse_was_down;
                    app.popup_mouse_was_down = down;
                    if clicked && !app.popup.pinned {
                        if let Ok(position) = app.popup.window.outer_position() {
                            let size = app.popup.window.outer_size();
                            let point = native::cursor();
                            if point.x < position.x
                                || point.y < position.y
                                || point.x >= position.x + size.width as i32
                                || point.y >= position.y + size.height as i32
                            {
                                app.hide_popup();
                            }
                        }
                    }
                }
            }
            Event::WindowEvent {
                window_id,
                event: WindowEvent::ScaleFactorChanged { scale_factor, .. },
                ..
            } => {
                let view = if window_id == app.main.window.id() {
                    Some(&app.main)
                } else if window_id == app.popup.window.id() {
                    Some(&app.popup)
                } else {
                    Some(&app.settings)
                };
                if let Some(view) = view {
                    let _ = view.web.zoom(1.0 / scale_factor);
                }
            }
            Event::WindowEvent {
                window_id,
                event: WindowEvent::Resized(size),
                ..
            } => {
                let view = if window_id == app.main.window.id() {
                    &app.main
                } else if window_id == app.settings.window.id() {
                    &app.settings
                } else {
                    &app.popup
                };
                let _ = view.web.set_bounds(wry::Rect {
                    position: wry::dpi::PhysicalPosition::new(0, 0).into(),
                    size: wry::dpi::PhysicalSize::new(size.width, size.height).into(),
                });
                if window_id == app.popup.window.id() && app.popup_visible {
                    app.fit_popup_to_screen();
                }
                if window_id == app.main.window.id() && !app.main.window.is_maximized() && size.width >= 820 && size.height >= 560 {
                    let deadline = std::time::Instant::now() + std::time::Duration::from_millis(350);
                    app.pending_main_size = Some((deadline, size));
                    if !app.popup_visible { *flow = ControlFlow::WaitUntil(deadline); }
                }
            }
            Event::LoopDestroyed => {
                app.save_main_size();
                if let Some(cancel) = &app.auth_cancel {
                    cancel.store(true, Ordering::Relaxed);
                }
                app.speech.stop();
            }
            _ => {}
        }
    });
}
fn make_view(
    loop_: &tao::event_loop::EventLoop<AppEvent>,
    proxy: &EventLoopProxy<AppEvent>,
    scope: Scope,
) -> Result<View> {
    let popup = scope == Scope::Popup;
    let size = match scope {
        Scope::Main => MainWindowSize::load().map(|s| PhysicalSize::new(s.width, s.height)).unwrap_or(PhysicalSize::new(1000, 720)),
        Scope::Popup => PhysicalSize::new(600, 180),
        Scope::Settings => PhysicalSize::new(640, 620),
    };
    let mut builder = WindowBuilder::new()
        .with_title(if scope == Scope::Settings {
            "TouchDict 设置"
        } else {
            "TouchDict"
        })
        .with_visible(false)
        .with_inner_size(size)
        .with_min_inner_size(match scope {
            Scope::Main => PhysicalSize::new(820, 560),
            Scope::Popup => PhysicalSize::new(400, 180),
            Scope::Settings => PhysicalSize::new(500, 600),
        })
        .with_window_icon(Some(window_icon()?));
    if popup {
        use tao::platform::windows::WindowBuilderExtWindows;
        builder = builder.with_skip_taskbar(true);
        builder = builder.with_focusable(false);
        builder = builder.with_minimizable(false).with_maximizable(false).with_resizable(false);
    }
    let window = builder.build(loop_)?;
    let p = proxy.clone();
    let html = pages::html(match scope {
        Scope::Main => Page::Main,
        Scope::Popup => Page::Popup,
        Scope::Settings => Page::Settings,
    });
    let data_dir = std::path::PathBuf::from(std::env::var_os("LOCALAPPDATA").unwrap_or_default()).join("TouchDict").join("WebView2");
    std::fs::create_dir_all(&data_dir)?;
    let mut web_context = wry::WebContext::new(Some(data_dir));
    let web = WebViewBuilder::new_with_web_context(&mut web_context)
        .with_html(html)
        .with_initialization_script("window.addEventListener('error',e=>window.ipc.postMessage(JSON.stringify({action:'diagnostic',text:'JS error: '+e.message})));document.addEventListener('DOMContentLoaded',()=>window.ipc.postMessage(JSON.stringify({action:'diagnostic',text:'DOM ready: '+document.body.children.length})));" )
        .with_ipc_handler(move |request| {
            let body = request.body();
            if body.len() < 1_048_576 {
                let _ = p.send_event(AppEvent::Ipc(scope, body.clone()));
            }
        })
        .with_navigation_handler(move|url| { let allow=url=="about:blank"||url.starts_with("about:blank#")||url.starts_with("data:text/html;charset=utf-8;base64,");crate::diagnostics::record(&format!("{scope:?} navigation allowed={allow}"));allow })
        .with_on_page_load_handler(move|event,url|{let phase=match event{wry::PageLoadEvent::Started=>"started",wry::PageLoadEvent::Finished=>"finished"};crate::diagnostics::record(&format!("{scope:?} page load {phase} uri={url:?}"))})
        .with_new_window_req_handler(|_, _| NewWindowResponse::Deny)
        .build(&window)?;
    // Preserve the original screen-pixel layout on high-DPI monitors.
    // WebView2 otherwise multiplies CSS pixels by the Windows scale factor.
    web.zoom(1.0 / window.scale_factor())?;
    Ok(View {
        window,
        web,
        ready: false,
        state: State::default(),
        pinned: false,
        request: 0,
        input: String::new(),
        input_revision: 0,
    })
}
fn icon_pixels() -> Result<(Vec<u8>, u32, u32)> {
    let image = image::load_from_memory_with_format(
        include_bytes!("../assets/touchdict.ico"),
        image::ImageFormat::Ico,
    )?
    .into_rgba8();
    let (width, height) = image.dimensions();
    Ok((image.into_raw(), width, height))
}
fn window_icon() -> Result<tao::window::Icon> {
    use tao::platform::windows::IconExtWindows;
    Ok(tao::window::Icon::from_resource(2, None)?)
}
fn make_tray(proxy: &EventLoopProxy<AppEvent>) -> Result<Tray> {
    let menu = Menu::new();
    let open = MenuItem::with_id("open", "打开 TouchDict", true, None);
    let models = Submenu::new("Codex 模型", true);
    let settings = MenuItem::with_id("settings", "账号与设置…", true, None);
    let pause = CheckMenuItem::with_id("pause", "监听取词", true, true, None);
    let quit = MenuItem::with_id("quit", "退出", true, None);
    menu.append_items(&[
        &open,
        &pause,
        &PredefinedMenuItem::separator(),
        &models,
        &settings,
        &PredefinedMenuItem::separator(),
        &quit,
    ])?;
    let p = proxy.clone();
    MenuEvent::set_event_handler(Some(move |event: MenuEvent| {
        let _ = p.send_event(AppEvent::Menu(event.id.0));
    }));
    let p = proxy.clone();
    TrayIconEvent::set_event_handler(Some(move |event: TrayIconEvent| {
        if let TrayIconEvent::DoubleClick {
            button: MouseButton::Left,
            ..
        } = event
        {
            let _ = p.send_event(AppEvent::Open);
        }
    }));
    let (pixels, width, height) = icon_pixels()?;
    let icon = tray_icon::Icon::from_rgba(pixels, width, height)?;
    let tray = TrayIconBuilder::new()
        .with_tooltip("TouchDict · Codex 划词词典")
        .with_icon(icon)
        .with_menu(Box::new(menu))
        .build()?;
    Ok(Tray {
        _icon: tray,
        pause,
        models,
    })
}
impl App {
    fn save_main_size(&mut self) {
        if let Some((_, size)) = self.pending_main_size.take() {
            if let Err(error) = (MainWindowSize { width: size.width, height: size.height }).save() {
                crate::diagnostics::record(&format!("main window size save failed: {error}"));
            }
        }
    }
    fn view(&self, scope: Scope) -> &View {
        match scope {
            Scope::Main => &self.main,
            Scope::Popup => &self.popup,
            Scope::Settings => &self.settings,
        }
    }
    fn view_mut(&mut self, scope: Scope) -> &mut View {
        match scope {
            Scope::Main => &mut self.main,
            Scope::Popup => &mut self.popup,
            Scope::Settings => &mut self.settings,
        }
    }
    fn render(&self) {
        for (scope, view) in [
            (Scope::Main, &self.main),
            (Scope::Popup, &self.popup),
            (Scope::Settings, &self.settings),
        ] {
            if view.ready {
                let snapshot = if scope == Scope::Settings {
                    json!({"config":self.config,"auth":self.auth_status,"authBusy":self.auth_busy,"authMessage":self.auth_message,"modelsBusy":self.models_busy,"modelsMessage":self.models_message})
                } else {
                    let s = &view.state;
                    let kind = match s.kind.as_str() {
                        "loading" => 1,
                        "success" => 2,
                        "error" => 4,
                        "suggestions" => 5,
                        _ => 3,
                    };
                    let history: Vec<Value> = self.history.entries.iter().rev().map(|e| json!({"Key":e.key,"Query":e.query,"Context":e.context,"Definition":e.definition,"Count":e.count})).collect();
                    json!({"state":{"Kind":kind,"Selection":s.selection,"Context":selection::clean_text(&s.context),"Definition":s.definition,"Message":s.message,"CanRetry":true,"Suggestions":s.suggestions},"history":history,"input":view.input,"inputRevision":view.input_revision,"currentQuery":s.selection,"selectedKey":self.selected_key,"termSize":self.config.term_font_size,"contentSize":self.config.content_font_size,"pinned":view.pinned})
                };
                let _ = view
                    .web
                    .evaluate_script(&format!("window.touchdict.applyState({snapshot})"));
            }
        }
    }
    fn notice(&self, scope: Scope, message: &str) {
        let _ = self
            .view(scope)
            .web
            .evaluate_script(&format!("window.touchdict.notice({})", json!(message)));
    }
    fn show_main(&mut self, settings: bool) {
        if settings {
            self.settings.window.set_visible(true);
            self.settings.window.set_focus();
            if self.settings.ready {
                let _ = self
                    .settings
                    .web
                    .evaluate_script("window.touchdict.openSettings()");
            }
            return;
        }
        self.main.window.set_visible(true);
        self.main.window.set_focus();
        let _ = self
            .main
            .web
            .evaluate_script("window.touchdict.focusSearch()");
    }
    fn show_popup(&mut self, point: native::Point) {
        self.popup_trigger = Some(point);
        self.popup.window.set_inner_size(PhysicalSize::new(
            600,
            self.popup.window.inner_size().height.max(180),
        ));
        self.popup.window.set_outer_position(PhysicalPosition::new(
            point.x.saturating_add(18), point.y.saturating_add(24),
        ));
        self.fit_popup_to_screen();
        self.popup.window.set_visible(true);
        self.popup.window.set_always_on_top(true);
        self.popup_visible = true;
        self.popup_mouse_was_down = native::mouse_down();
    }
    fn fit_popup_to_screen(&self) {
        let Some(area) = self.popup_trigger.and_then(native::work_area) else { return };
        let window = &self.popup.window;
        let outer = window.outer_size();
        let inner = window.inner_size();
        let border_w = outer.width.saturating_sub(inner.width);
        let border_h = outer.height.saturating_sub(inner.height);
        let available_w = (area.right - area.left).max(1) as u32;
        let available_h = (area.bottom - area.top).max(1) as u32;
        let width = inner.width.min(available_w.saturating_sub(border_w).max(1));
        let height = inner.height.min(available_h.saturating_sub(border_h).max(1));
        if width != inner.width || height != inner.height {
            window.set_inner_size(PhysicalSize::new(width, height));
        }
        if let Ok(position) = window.outer_position() {
            let x = position.x.clamp(area.left, (area.right - (width + border_w) as i32).max(area.left));
            let y = position.y.clamp(area.top, (area.bottom - (height + border_h) as i32).max(area.top));
            if x != position.x || y != position.y {
                window.set_outer_position(PhysicalPosition::new(x, y));
            }
        }
    }
    fn hide_popup(&mut self) {
        self.popup_visible = false;
        self.popup.window.set_focusable(false);
        self.popup.request += 1;
        self.popup.window.set_visible(false);
        self.speech.stop();
    }
    fn apply_preferences(&self) {
        native::ALT_ENABLED.store(self.config.alt_enabled, Ordering::Relaxed);
        native::HOTKEY_ENABLED.store(self.config.hotkey_enabled, Ordering::Relaxed);
        native::LISTENING.store(!self.paused, Ordering::Relaxed);
    }
    fn refresh_tray(&self) {
        self.tray.pause.set_checked(!self.paused);
        while self.tray.models.remove_at(0).is_some() {}
        if let Some(models) = self.auth_status["models"].as_array() {
            for m in models {
                if let (Some(id), Some(label)) = (m["id"].as_str(), m["label"].as_str()) {
                    let item = CheckMenuItem::with_id(
                        format!("model:{id}"),
                        label,
                        true,
                        self.config.model == id,
                        None,
                    );
                    let _ = self.tray.models.append(&item);
                }
            }
        }
        if self.tray.models.items().is_empty() {
            let _ = self.tray.models.append(&MenuItem::with_id(
                "settings",
                "请先连接 Codex",
                true,
                None,
            ));
        }
    }
    fn capture(&mut self, source: &str) {
        if self.paused {
            return;
        };
        self.capture_request += 1;
        let id = self.capture_request;
        let p = self.proxy.clone();
        let source = source.to_string();
        let point = native::cursor();
        thread::spawn(move || {
            let result = selection::capture(&source, point).map_err(|e| e.to_string());
            let _ = p.send_event(AppEvent::Captured(id, point, result));
        });
    }
    fn lookup(&mut self, scope: Scope, mut s: Selection, bypass: bool, mirror: bool) {
        s.text = s
            .text
            .split_whitespace()
            .collect::<Vec<_>>()
            .join(" ")
            .chars()
            .take(300)
            .collect();
        if s.text.is_empty() {
            return;
        };
        self.speech.stop();
        self.selected_key.clear();
        s.context = selection::clean_text(&s.context);
        if scope == Scope::Popup && self.config.auto_speak && s.text.split_whitespace().count() <= 3 {
            self.speech.speak(&s.text);
        }
        let view = self.view_mut(scope);
        view.input = s.text.clone();
        view.input_revision += 1;
        view.request += 1;
        let id = view.request;
        view.state = State {
            kind: "loading".into(),
            selection: s.text.clone(),
            context: s.context.clone(),
            ..Default::default()
        };
        let mirror_request = if mirror {
            self.main.request += 1;
            self.main.state = self.popup.state.clone();
            self.main.input = s.text.clone();
            self.main.input_revision += 1;
            Some(self.main.request)
        } else {
            None
        };
        self.render();
        if !bypass {
            if let Some(d) = self.history.cached(&s) {
                self.finish_query(scope, s, mirror_request, Ok(Answer::Definition(d)));
                return;
            }
        }
        let backend = self.backend.clone();
        let c = self.config.clone();
        let p = self.proxy.clone();
        thread::spawn(move || {
            let result = backend.lookup(&c, &s).map_err(|e| e.to_string());
            let _ = p.send_event(AppEvent::Query(scope, id, s, mirror_request, result));
        });
    }
    fn finish_query(
        &mut self,
        scope: Scope,
        s: Selection,
        mirror: Option<u64>,
        result: Result<Answer, String>,
    ) {
        let state = match result {
            Ok(Answer::Definition(d)) => {
                if let Err(e) = self.history.store(&s, d.clone()) {
                    self.notice(scope, &format!("结果已返回，但历史保存失败：{e}"));
                }
                if self.config.auto_speak && s.text.split_whitespace().count() <= 3 {
                    if scope == Scope::Popup {
                        self.speech.speak_again(&d.term);
                    } else {
                        self.speech.speak(&d.term);
                    }
                }
                State {
                    kind: "success".into(),
                    selection: s.text.clone(),
                    context: s.context.clone(),
                    definition: d,
                    ..Default::default()
                }
            }
            Ok(Answer::Suggestions(suggestions)) => State {
                kind: "suggestions".into(),
                selection: s.text.clone(),
                context: s.context.clone(),
                suggestions,
                ..Default::default()
            },
            Err(message) => State {
                kind: "error".into(),
                selection: s.text.clone(),
                context: s.context.clone(),
                message,
                ..Default::default()
            },
        };
        self.view_mut(scope).state = state.clone();
        if mirror == Some(self.main.request) {
            self.main.state = state;
        }
        self.render();
    }
    fn menu(&mut self, id: &str, flow: &mut ControlFlow) {
        match id {
            "open" => self.show_main(false),
            "settings" => self.show_main(true),
            "pause" => {
                self.paused = !self.paused;
                if self.paused {
                    self.capture_request += 1;
                }
                self.apply_preferences();
                self.refresh_tray();
                self.render()
            }
            "quit" => {
                if let Some(cancel) = &self.auth_cancel {
                    cancel.store(true, Ordering::Relaxed)
                }
                self.speech.stop();
                *flow = ControlFlow::Exit
            }
            _ => {
                if let Some(model) = id.strip_prefix("model:") {
                    if self.auth_status["models"]
                        .as_array()
                        .is_some_and(|m| m.iter().any(|v| v["id"] == model))
                    {
                        let old = self.config.model.clone();
                        self.config.model = model.into();
                        if let Err(e) = self.config.save() {
                            self.config.model = old;
                            self.notice(Scope::Main, &e.to_string());
                        } else {
                            self.main.request += 1;
                            self.popup.request += 1;
                            self.main.state = State::default();
                            self.popup.state = State::default();
                        }
                        self.refresh_tray();
                        self.render();
                    }
                }
            }
        }
    }
    fn ipc(&mut self, scope: Scope, body: &str, flow: &mut ControlFlow) -> Result<()> {
        let v: Value = serde_json::from_str(body)?;
        let action = v["action"].as_str().unwrap_or_default();
        match action {
            "ready" => {
                crate::diagnostics::record(&format!("{scope:?} frontend ready"));
                self.view_mut(scope).ready = true;
                self.render();
            }
            "diagnostic" => {
                crate::diagnostics::record(&format!(
                    "{scope:?} {}",
                    v["text"].as_str().unwrap_or_default()
                ));
            }
            "lookup" | "retry" => {
                let text = v["text"]
                    .as_str()
                    .unwrap_or(&self.view(scope).state.selection)
                    .to_string();
                let context = if action == "retry" {
                    self.view(scope).state.context.clone()
                } else {
                    String::new()
                };
                self.lookup(
                    scope,
                    Selection {
                        text,
                        context,
                        ..Default::default()
                    },
                    action == "retry",
                    false,
                );
            }
            "settings" => {
                self.hide_popup();
                self.show_main(true);
            }
            "input" => {
                self.view_mut(scope).input = v["text"].as_str().unwrap_or_default().to_string();
            }
            "suggestion" => {
                let text = v["text"].as_str().unwrap_or_default();
                if self
                    .view(scope)
                    .state
                    .suggestions
                    .iter()
                    .any(|candidate| candidate == text)
                {
                    self.lookup(
                        scope,
                        Selection {
                            text: text.into(),
                            ..Default::default()
                        },
                        false,
                        false,
                    );
                }
            }
            "edit" => {
                self.popup.window.set_focusable(true);
                self.popup.window.set_focus();
                let _ = self
                    .popup
                    .web
                    .evaluate_script("window.touchdict.beginEdit()");
            }
            "edit-cancel" => {
                let _ = self.popup.web.evaluate_script("window.touchdict.endEdit()");
            }
            "settings-height" => {
                if scope == Scope::Settings {
                    let height = v["height"].as_u64().unwrap_or(0).min(10000) as u32;
                    if height > self.settings.window.inner_size().height {
                        let width = self.settings.window.inner_size().width;
                        self.settings.window.set_inner_size(PhysicalSize::new(width, height));
                    }
                }
            }
            "content-height" => {
                if scope == Scope::Popup {
                    let height = v["height"].as_u64().unwrap_or(0).min(10000) as u32;
                    if height > 0 {
                        let max = self
                            .popup
                            .window
                            .current_monitor()
                            .map(|m| m.size().height.saturating_sub(80))
                            .unwrap_or(900);
                        let height = height.clamp(180, max.max(180));
                        let width = self.popup.window.inner_size().width;
                        self.popup
                            .window
                            .set_inner_size(PhysicalSize::new(width, height));
                        self.fit_popup_to_screen();
                    }
                }
            }
            "close" => {
                if scope == Scope::Popup {
                    self.hide_popup()
                } else {
                    self.view(scope).window.set_visible(false)
                }
            }
            "pin" => {
                let view = self.view_mut(scope);
                view.pinned = !view.pinned;
                view.window.set_always_on_top(view.pinned);
                self.render();
            }
            "speak" => {
                let state = &self.view(scope).state;
                if state.kind == "success" {
                    self.speech.speak(&state.definition.term)
                }
            }
            "copy-example" => {
                let example = &self.view(scope).state.definition.example_en;
                if !example.is_empty() {
                    arboard::Clipboard::new()?.set_text(example)?;
                    self.notice(scope, "例句已复制");
                }
            }
            "history-select" => {
                if let Some(e) = self
                    .history
                    .entries
                    .iter()
                    .find(|e| Some(e.key.as_str()) == v["key"].as_str())
                {
                    self.main.request += 1;
                    self.selected_key = e.key.clone();
                    self.main.input = e.query.clone();
                    self.main.input_revision += 1;
                    self.main.state = State {
                        kind: "success".into(),
                        selection: e.query.clone(),
                        context: e.context.clone(),
                        definition: e.definition.clone(),
                        ..Default::default()
                    };
                    self.render();
                }
            }
            "history-delete" => {
                let backup = self.history.entries.clone();
                self.history
                    .entries
                    .retain(|e| Some(e.key.as_str()) != v["key"].as_str());
                if let Err(e) = self.history.save() {
                    self.history.entries = backup;
                    return Err(e);
                }
                self.selected_key.clear();
                self.render();
            }
            "history-clear" => {
                if !native::confirm("确定清空所有查词历史和查询次数吗？此操作无法撤销。")
                {
                    return Ok(());
                }
                let backup = std::mem::take(&mut self.history.entries);
                if let Err(e) = self.history.save() {
                    self.history.entries = backup;
                    return Err(e);
                }
                self.selected_key.clear();
                self.render();
                self.notice(scope, "查词记录已清空");
            }
            "history-export" => self.export(),
            "authorize" => self.authorize()?,
            "cancel-auth" => {
                if let Some(cancel) = &self.auth_cancel {
                    cancel.store(true, Ordering::Relaxed)
                }
            }
            "refresh-models" | "disconnect" => self.account_action(action),
            "save-settings" => {
                let mut c: Config = serde_json::from_value(v["config"].clone())?;
                c.term_font_size = c.term_font_size.clamp(20, 48);
                c.content_font_size = c.content_font_size.clamp(12, 24);
                if !c.model.is_empty() {
                    anyhow::ensure!(
                        self.auth_status["models"]
                            .as_array()
                            .is_some_and(|models| models.iter().any(|m| m["id"] == c.model)),
                        "请选择账号返回的 Codex 模型"
                    )
                };
                native::sync_startup(c.startup_enabled)?;
                if let Err(e) = c.save() {
                    let _ = native::sync_startup(self.config.startup_enabled);
                    return Err(e);
                }
                if c.model != self.config.model {
                    self.main.request += 1;
                    self.popup.request += 1;
                }
                self.config = c;
                self.apply_preferences();
                self.refresh_tray();
                self.render();
                let _ = self
                    .settings
                    .web
                    .evaluate_script("window.touchdict.settingsSaved()");
                self.notice(scope, "设置已保存");
            }
            "zoom" => {
                let reset = v["reset"].as_bool().unwrap_or(false);
                let delta = v["delta"].as_i64().unwrap_or(0).clamp(-1, 1) as i32;
                let old = self.config.clone();
                if reset {
                    self.config.term_font_size = 23;
                    self.config.content_font_size = 15
                } else {
                    self.config.term_font_size =
                        (self.config.term_font_size as i32 + delta).clamp(20, 48) as u32;
                    self.config.content_font_size =
                        (self.config.content_font_size as i32 + delta).clamp(12, 24) as u32
                }
                if let Err(e) = self.config.save() {
                    self.config = old;
                    return Err(e);
                }
                self.render();
            }
            "quit" => *flow = ControlFlow::Exit,
            _ => {}
        }
        Ok(())
    }
    fn authorize(&mut self) -> Result<()> {
        if self.auth_busy {
            return Ok(());
        };
        let attempt = self
            .backend
            .auth
            .try_lock()
            .map_err(|_| anyhow::anyhow!("账号正在更新，请稍后重试"))?
            .begin()?;
        native::open_url(&attempt.url)?;
        self.auth_busy = true;
        self.auth_message = "等待浏览器授权…".into();
        let cancel = Arc::new(AtomicBool::new(false));
        self.auth_cancel = Some(cancel.clone());
        self.render();
        let auth = self.backend.auth.clone();
        let p = self.proxy.clone();
        thread::spawn(move || {
            let mut auth = auth.lock().unwrap();
            let result = auth
                .finish(attempt, &cancel)
                .and_then(|_| auth.refresh_models())
                .map_err(|e| e.to_string());
            let status = auth.status();
            let _ = p.send_event(AppEvent::Auth(status, result, true));
        });
        Ok(())
    }
    fn account_action(&mut self, action: &str) {
        if action == "refresh-models" {
            if self.models_busy || self.auth_busy { return; }
            self.models_busy = true;
            self.models_message.clear();
            self.render();
            let auth = self.backend.auth.clone();
            let proxy = self.proxy.clone();
            thread::spawn(move || {
                let mut auth = auth.lock().unwrap();
                let result = auth.refresh_models().map_err(|error| error.to_string());
                let models = auth.status()["models"].clone();
                let _ = proxy.send_event(AppEvent::Models(models, result));
            });
            return;
        }
        if self.auth_busy {
            return;
        };
        self.auth_busy = true;
        self.auth_message = if action == "disconnect" {
            "正在断开连接…"
        } else {
            "正在刷新模型列表…"
        }
        .into();
        self.render();
        let auth = self.backend.auth.clone();
        let p = self.proxy.clone();
        let disconnect = action == "disconnect";
        thread::spawn(move || {
            let mut auth = auth.lock().unwrap();
            let result = if disconnect {
                auth.disconnect()
            } else {
                auth.refresh_models()
            }
            .map_err(|e| e.to_string());
            let status = auth.status();
            let _ = p.send_event(AppEvent::Auth(status, result, !disconnect));
        });
    }
    fn export(&self) {
        let entries = self.history.entries.clone();
        let p = self.proxy.clone();
        thread::spawn(move || {
            let Some(path) = rfd::FileDialog::new()
                .set_title("导出查词记录")
                .set_file_name("TouchDict.csv")
                .add_filter("CSV", &["csv"])
                .save_file()
            else {
                return;
            };
            let quote = |s: &str| {
                let value = if s.starts_with(['=', '+', '-', '@', '\t', '\r']) {
                    format!("'{s}")
                } else {
                    s.into()
                };
                format!("\"{}\"", value.replace('"', "\"\""))
            };
            let mut csv =
                "\u{feff}查询,词性,音标,释义,理解与用法,英文例句,中文例句,原句,查询次数\r\n"
                    .to_string();
            for e in entries.iter().rev() {
                let d = &e.definition;
                csv.push_str(
                    &[
                        &e.query,
                        &d.part_of_speech,
                        &d.phonetic,
                        &d.meaning_zh,
                        &d.learning_zh,
                        &d.example_en,
                        &d.example_zh,
                        &e.context,
                        &e.count.to_string(),
                    ]
                    .iter()
                    .map(|s| quote(s))
                    .collect::<Vec<_>>()
                    .join(","),
                );
                csv.push_str("\r\n");
            }
            let result = std::fs::write(path, csv).map_err(|e| format!("导出失败：{e}"));
            let _ = p.send_event(AppEvent::Export(result));
        });
    }
}
