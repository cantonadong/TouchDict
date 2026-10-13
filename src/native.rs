use anyhow::{Result, anyhow};
use std::{
    ffi::c_void,
    path::Path,
    sync::{
        OnceLock,
        atomic::{AtomicBool, AtomicU64, Ordering},
    },
    time::{Duration, Instant},
};

fn wide(s: &str) -> Vec<u16> {
    s.encode_utf16().chain(Some(0)).collect()
}
#[repr(C)]
struct Blob {
    len: u32,
    data: *mut u8,
}
#[repr(C)]
#[derive(Default, Copy, Clone)]
pub struct Point {
    pub x: i32,
    pub y: i32,
}
#[repr(C)]
#[derive(Default, Clone, Copy)]
pub struct ScreenRect {
    pub left: i32,
    pub top: i32,
    pub right: i32,
    pub bottom: i32,
}
#[repr(C)]
struct MonitorInfo {
    size: u32,
    monitor: ScreenRect,
    work: ScreenRect,
    flags: u32,
}
pub fn work_area(point: Point) -> Option<ScreenRect> {
    unsafe {
        let monitor = MonitorFromPoint(point, 2);
        let mut info = MonitorInfo {
            size: std::mem::size_of::<MonitorInfo>() as u32,
            monitor: ScreenRect::default(),
            work: ScreenRect::default(),
            flags: 0,
        };
        if GetMonitorInfoW(monitor, &mut info) != 0 {
            Some(info.work)
        } else {
            None
        }
    }
}
#[repr(C)]
#[derive(Default)]
struct Message {
    hwnd: isize,
    message: u32,
    wparam: usize,
    lparam: isize,
    time: u32,
    point: Point,
    private: u32,
}
#[repr(C)]
struct Keyboard {
    vk: u32,
    scan: u32,
    flags: u32,
    time: u32,
    extra: usize,
}
#[link(name = "crypt32")]
unsafe extern "system" {
    fn CryptProtectData(
        input: *const Blob,
        description: *const u16,
        entropy: *const Blob,
        reserved: *mut c_void,
        prompt: *mut c_void,
        flags: u32,
        out: *mut Blob,
    ) -> i32;
    fn CryptUnprotectData(
        input: *const Blob,
        description: *mut *mut u16,
        entropy: *const Blob,
        reserved: *mut c_void,
        prompt: *mut c_void,
        flags: u32,
        out: *mut Blob,
    ) -> i32;
}
#[link(name = "kernel32")]
unsafe extern "system" {
    fn LocalFree(memory: *mut c_void) -> *mut c_void;
    fn MoveFileExW(from: *const u16, to: *const u16, flags: u32) -> i32;
    fn CreateMutexW(attributes: *const c_void, owned: i32, name: *const u16) -> isize;
    fn GetLastError() -> u32;
    fn CloseHandle(handle: isize) -> i32;
    fn GetCurrentThreadId() -> u32;
}
#[link(name = "user32")]
unsafe extern "system" {
    fn MonitorFromPoint(point: Point, flags: u32) -> isize;
    fn GetMonitorInfoW(monitor: isize, info: *mut MonitorInfo) -> i32;
    fn GetCursorPos(point: *mut Point) -> i32;
    fn GetAsyncKeyState(key: i32) -> i16;
    fn SetWindowsHookExW(
        kind: i32,
        callback: Option<unsafe extern "system" fn(i32, usize, isize) -> isize>,
        module: isize,
        thread: u32,
    ) -> isize;
    fn CallNextHookEx(hook: isize, code: i32, wparam: usize, lparam: isize) -> isize;
    fn UnhookWindowsHookEx(hook: isize) -> i32;
    fn GetMessageW(message: *mut Message, window: isize, min: u32, max: u32) -> i32;
    fn PostThreadMessageW(thread: u32, message: u32, wparam: usize, lparam: isize) -> i32;
    fn RegisterHotKey(window: isize, id: i32, modifiers: u32, key: u32) -> i32;
    fn UnregisterHotKey(window: isize, id: i32) -> i32;
    fn keybd_event(key: u8, scan: u8, flags: u32, extra: usize);
    fn GetForegroundWindow() -> isize;
    fn GetWindowThreadProcessId(window: isize, pid: *mut u32) -> u32;
    fn MessageBoxW(window: isize, text: *const u16, title: *const u16, flags: u32) -> i32;
    fn GetClipboardSequenceNumber() -> u32;
}
#[link(name = "shell32")]
unsafe extern "system" {
    fn ShellExecuteW(
        window: isize,
        verb: *const u16,
        file: *const u16,
        parameters: *const u16,
        directory: *const u16,
        show: i32,
    ) -> isize;
}
pub fn protect(bytes: &[u8]) -> Result<Vec<u8>> {
    crypt(bytes, true)
}
pub fn unprotect(bytes: &[u8]) -> Result<Vec<u8>> {
    crypt(bytes, false)
}
fn crypt(bytes: &[u8], encrypt: bool) -> Result<Vec<u8>> {
    if bytes.is_empty() {
        return Ok(vec![]);
    };
    unsafe {
        let input = Blob {
            len: bytes.len().try_into()?,
            data: bytes.as_ptr() as *mut _,
        };
        let mut output = Blob {
            len: 0,
            data: std::ptr::null_mut(),
        };
        let ok = if encrypt {
            CryptProtectData(
                &input,
                std::ptr::null(),
                std::ptr::null(),
                std::ptr::null_mut(),
                std::ptr::null_mut(),
                1,
                &mut output,
            )
        } else {
            CryptUnprotectData(
                &input,
                std::ptr::null_mut(),
                std::ptr::null(),
                std::ptr::null_mut(),
                std::ptr::null_mut(),
                1,
                &mut output,
            )
        };
        if ok == 0 {
            return Err(anyhow!("Windows 凭据加密/解密失败"));
        };
        let result = std::slice::from_raw_parts(output.data, output.len as usize).to_vec();
        LocalFree(output.data.cast());
        Ok(result)
    }
}
pub fn replace_file(from: &Path, to: &Path) -> Result<()> {
    unsafe {
        if MoveFileExW(
            wide(&from.to_string_lossy()).as_ptr(),
            wide(&to.to_string_lossy()).as_ptr(),
            1 | 8,
        ) == 0
        {
            return Err(std::io::Error::last_os_error().into());
        }
    }
    Ok(())
}
pub struct Instance(isize);
impl Drop for Instance {
    fn drop(&mut self) {
        unsafe {
            CloseHandle(self.0);
        }
    }
}
pub fn single_instance() -> Result<Option<Instance>> {
    unsafe {
        let h = CreateMutexW(
            std::ptr::null(),
            0,
            wide("Local\\TouchDict.SingleInstance").as_ptr(),
        );
        if h == 0 {
            return Err(std::io::Error::last_os_error().into());
        };
        if GetLastError() == 183 {
            CloseHandle(h);
            return Ok(None);
        };
        Ok(Some(Instance(h)))
    }
}
pub fn open_url(address: &str) -> Result<()> {
    let url = reqwest::Url::parse(address)?;
    anyhow::ensure!(
        url.scheme() == "https"
            && [
                "auth.openai.com",
                "chatgpt.com",
                "aistudio.google.com",
                "console.groq.com",
                "openrouter.ai"
            ]
            .contains(&url.host_str().unwrap_or("")),
        "不支持此网页地址"
    );
    unsafe {
        if ShellExecuteW(
            0,
            wide("open").as_ptr(),
            wide(address).as_ptr(),
            std::ptr::null(),
            std::ptr::null(),
            1,
        ) <= 32
        {
            return Err(anyhow!("无法打开默认浏览器"));
        }
    }
    Ok(())
}
pub fn show_error(text: &str) {
    unsafe {
        MessageBoxW(0, wide(text).as_ptr(), wide("TouchDict").as_ptr(), 0x10);
    }
}

pub fn confirm(text: &str) -> bool {
    unsafe { MessageBoxW(0, wide(text).as_ptr(), wide("TouchDict").as_ptr(), 0x134) == 6 }
}
pub fn cursor() -> Point {
    let mut p = Point::default();
    unsafe {
        GetCursorPos(&mut p);
    }
    p
}
pub fn mouse_down() -> bool {
    unsafe { [1, 2, 4].iter().any(|key| GetAsyncKeyState(*key) < 0) }
}
pub fn external_foreground() -> bool {
    unsafe {
        let mut pid = 0;
        GetWindowThreadProcessId(GetForegroundWindow(), &mut pid);
        pid != std::process::id()
    }
}
pub fn copy_selected() -> Result<String> {
    let mut clip = arboard::Clipboard::new()?;
    let old = clip.get_text().ok();
    let before = unsafe { GetClipboardSequenceNumber() };
    let until = Instant::now() + Duration::from_millis(650);
    while Instant::now() < until {
        if unsafe { GetAsyncKeyState(0x11) | GetAsyncKeyState(0x12) | GetAsyncKeyState(0x10) } >= 0
        {
            break;
        }
        std::thread::sleep(Duration::from_millis(15));
    }
    unsafe {
        keybd_event(0x11, 0, 0, REPLAY);
        keybd_event(0x43, 0, 0, REPLAY);
        keybd_event(0x43, 0, 2, REPLAY);
        keybd_event(0x11, 0, 2, REPLAY);
    }
    let until = Instant::now() + Duration::from_millis(700);
    let mut answer = None;
    while Instant::now() < until {
        if unsafe { GetClipboardSequenceNumber() } != before {
            if let Ok(text) = clip.get_text() {
                if !text.trim().is_empty() {
                    answer = Some(text);
                    break;
                }
            }
        }
        std::thread::sleep(Duration::from_millis(20));
    }
    if let Some(old) = old {
        if answer.is_some() {
            let _ = clip.set_text(old);
        }
    }
    answer.ok_or_else(|| anyhow!("未读取到选中的英文，请先选择文本或使用鼠标悬停查词"))
}
const REPLAY: usize = 0x54444354;
pub static LISTENING: AtomicBool = AtomicBool::new(true);
pub static ALT_ENABLED: AtomicBool = AtomicBool::new(true);
pub static HOTKEY_ENABLED: AtomicBool = AtomicBool::new(true);
static TRIGGER: OnceLock<Box<dyn Fn(&str) + Send + Sync>> = OnceLock::new();
static LAST: AtomicU64 = AtomicU64::new(0);
fn emit(source: &str) {
    if !LISTENING.load(Ordering::Relaxed) || !external_foreground() {
        return;
    };
    let now = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis() as u64;
    if now.saturating_sub(LAST.swap(now, Ordering::Relaxed)) < 350 {
        return;
    };
    if let Some(send) = TRIGGER.get() {
        send(source)
    }
}
struct AltState {
    pending: bool,
    forwarded: bool,
    start: Instant,
    scan: u8,
}
thread_local! {static ALT_STATE:std::cell::RefCell<AltState>=std::cell::RefCell::new(AltState{pending:false,forwarded:false,start:Instant::now(),scan:0});}
unsafe extern "system" fn key_hook(code: i32, wp: usize, lp: isize) -> isize {
    if code >= 0 {
        let k = unsafe { &*(lp as *const Keyboard) };
        if k.extra != REPLAY {
            let alt = k.vk == 0xa4 || k.vk == 0x12;
            let down = wp == 0x100 || wp == 0x104;
            let up = wp == 0x101 || wp == 0x105;
            let swallowed = ALT_STATE.with(|state| {
                let mut s = state.borrow_mut();
                if alt
                    && down
                    && (s.pending
                        || (LISTENING.load(Ordering::Relaxed)
                            && ALT_ENABLED.load(Ordering::Relaxed)
                            && !s.forwarded))
                {
                    if !s.pending {
                        if unsafe {
                            GetAsyncKeyState(0x11) | GetAsyncKeyState(0x10) | GetAsyncKeyState(0xa5)
                        } < 0
                        {
                            s.forwarded = true;
                            return false;
                        }
                        s.pending = true;
                        s.start = Instant::now();
                        s.scan = k.scan as u8;
                    }
                    return true;
                }
                if s.pending && !alt && down {
                    s.pending = false;
                    s.forwarded = true;
                    unsafe {
                        keybd_event(0xa4, s.scan, 0, REPLAY);
                        keybd_event(k.vk as u8, k.scan as u8, k.flags & 1, REPLAY);
                    }
                    if k.vk == 0x44
                        && HOTKEY_ENABLED.load(Ordering::Relaxed)
                        && unsafe { GetAsyncKeyState(0x11) } < 0
                    {
                        emit("hotkey")
                    }
                    return true;
                }
                if alt && up && s.pending {
                    s.pending = false;
                    if s.start.elapsed() < Duration::from_millis(900)
                        && ALT_ENABLED.load(Ordering::Relaxed)
                    {
                        emit("alt")
                    } else {
                        unsafe {
                            keybd_event(0xa4, s.scan, 0, REPLAY);
                            keybd_event(0xa4, s.scan, 2, REPLAY);
                        }
                    }
                    return true;
                }
                if alt && up {
                    s.forwarded = false;
                }
                false
            });
            if swallowed {
                return 1;
            }
        }
    }
    unsafe { CallNextHookEx(0, code, wp, lp) }
}
pub struct Hooks {
    thread: u32,
}
impl Drop for Hooks {
    fn drop(&mut self) {
        unsafe {
            PostThreadMessageW(self.thread, 0x12, 0, 0);
        }
    }
}
pub fn start_hooks(callback: impl Fn(&str) + Send + Sync + 'static) -> Result<Hooks> {
    let _ = TRIGGER.set(Box::new(callback));
    let (tx, rx) = std::sync::mpsc::channel();
    std::thread::spawn(move || unsafe {
        let mut msg = Message::default();
        let keyboard = SetWindowsHookExW(13, Some(key_hook), 0, 0);
        if keyboard == 0 {
            let _ = tx.send(Err("无法启动全局键盘监听".to_string()));
            return;
        };
        let hotkey = RegisterHotKey(0, 1, 0x4003, 0x44) != 0;
        let _ = tx.send(Ok(GetCurrentThreadId()));
        while GetMessageW(&mut msg, 0, 0, 0) > 0 {
            if msg.message == 0x312 && HOTKEY_ENABLED.load(Ordering::Relaxed) {
                emit("hotkey")
            }
        }
        UnhookWindowsHookEx(keyboard);
        if hotkey {
            UnregisterHotKey(0, 1);
        }
    });
    let thread = rx.recv()?.map_err(|e| anyhow!(e))?;
    Ok(Hooks { thread })
}

pub fn sync_startup(enabled: bool) -> Result<()> {
    use windows::{Win32::System::Registry::*, core::w};
    unsafe {
        let mut key = HKEY::default();
        RegCreateKeyExW(
            HKEY_CURRENT_USER,
            w!("Software\\Microsoft\\Windows\\CurrentVersion\\Run"),
            None,
            None,
            REG_OPTION_NON_VOLATILE,
            KEY_SET_VALUE,
            None,
            &mut key,
            None,
        )
        .ok()?;
        let result = if enabled {
            let value = wide(&format!(
                "\"{}\" --background",
                std::env::current_exe()?.display()
            ));
            let bytes = std::slice::from_raw_parts(value.as_ptr().cast(), value.len() * 2);
            RegSetValueExW(key, w!("TouchDict"), Some(0), REG_SZ, Some(bytes)).ok()
        } else {
            let result = RegDeleteValueW(key, w!("TouchDict"));
            if result.0 == 2 { Ok(()) } else { result.ok() }
        };
        let _ = RegCloseKey(key);
        result?;
    }
    Ok(())
}
