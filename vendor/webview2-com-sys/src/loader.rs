//! The small SDK loader is embedded; the browser remains the installed system runtime.
use std::{ffi::c_void, sync::OnceLock};

#[link(name = "kernel32")]
unsafe extern "system" {
    fn LoadLibraryExW(path: *const u16, file: isize, flags: u32) -> isize;
    fn GetProcAddress(module: isize, name: *const u8) -> *mut c_void;
    fn GetLastError() -> u32;
}
static MODULE: OnceLock<Result<isize, u32>> = OnceLock::new();
fn load() -> Result<isize, u32> {
    // Write our exact embedded bytes to a private process directory. Never load
    // a caller-supplied DLL from the working directory or beside the executable.
    let root = std::env::temp_dir();
    let mut directory = None;
    for index in 0..1000 {
        let path = root.join(format!("touchdict-loader-{}-{index}", std::process::id()));
        match std::fs::create_dir(&path) {
            Ok(()) => { directory = Some(path); break; }
            Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => continue,
            Err(e) => return Err(e.raw_os_error().unwrap_or(5) as u32),
        }
    }
    let directory = directory.ok_or(183u32)?;
    let path = directory.join("WebView2Loader.dll");
    let bytes = include_bytes!("../x64/WebView2Loader.dll");
    std::fs::write(&path, bytes).map_err(|e| e.raw_os_error().unwrap_or(5) as u32)?;
    let wide: Vec<u16> = path.as_os_str().encode_wide().chain(Some(0)).collect();
    // Only the extracted SDK DLL and Windows system DLLs participate in search.
    let module = unsafe { LoadLibraryExW(wide.as_ptr(), 0, 0x100 | 0x800) };
    if module == 0 { Err(unsafe { GetLastError() }) } else { Ok(module) }
}
use std::os::windows::ffi::OsStrExt;
pub fn resolve(name: &[u8]) -> Result<*mut c_void, u32> {
    let module = *MODULE.get_or_init(load);
    let address = unsafe { GetProcAddress(module?, name.as_ptr()) };
    if address.is_null() { Err(unsafe { GetLastError() }) } else { Ok(address) }
}
