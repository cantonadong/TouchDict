use crate::{model::Selection, native::Point};
use anyhow::{Context, Result, bail};
use std::{
    ffi::c_void,
    os::windows::process::CommandExt,
    process::{Command, Stdio},
    time::{Duration, Instant},
};
use windows::{
    Win32::{Foundation::POINT, System::Com::*, UI::Accessibility::*},
    core::HSTRING,
};

pub fn capture(source: &str, point: Point) -> Result<Selection> {
    let mut child = Command::new(std::env::current_exe()?)
        .args([
            "--capture",
            source,
            &point.x.to_string(),
            &point.y.to_string(),
        ])
        .creation_flags(0x08000000)
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()?;
    let deadline = Instant::now() + Duration::from_secs(6);
    loop {
        if child.try_wait()?.is_some() {
            let out = child.wait_with_output()?;
            let v: serde_json::Value =
                serde_json::from_slice(&out.stdout).context("无法读取英文，请手动输入后查询")?;
            if let Some(error) = v["error"].as_str() {
                bail!("{error}")
            };
            return Ok(serde_json::from_value(v)?);
        }
        if Instant::now() > deadline {
            let _ = child.kill();
            let _ = child.wait();
            bail!("划词读取超时，请手动输入后查询")
        };
        std::thread::sleep(Duration::from_millis(30));
    }
}
pub fn clean_text(text: &str) -> String {
    text.chars()
        .filter(|c| !matches!(c, '\u{fffc}' | '\u{fffd}' | '\u{200b}' | '\u{feff}') && (!c.is_control() || c.is_whitespace()))
        .collect::<String>()
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ")
}
pub fn capture_in_process(source: &str, point: Point) -> Result<Selection> {
    unsafe {
        windows::Win32::System::WinRT::RoInitialize(
            windows::Win32::System::WinRT::RO_INIT_MULTITHREADED,
        )?;
    }
    struct Com;
    impl Drop for Com {
        fn drop(&mut self) {
            unsafe {
                windows::Win32::System::WinRT::RoUninitialize();
            }
        }
    }
    let _com = Com;
    // Freeze pixels before accessibility calls, so video subtitles stay stable.
    let screenshot = if source == "alt" {
        screenshot(point).ok()
    } else {
        None
    };
    let passive = read_uia(point, source == "alt");
    let mut selected = match passive {
        Ok(s) => s,
        Err(_) => {
            if source == "alt" {
                let image = screenshot.context("未读取到英文，请输入单词后查询")?;
                ocr(point, image)?
            } else {
                Selection {
                    text: crate::native::copy_selected()?,
                    source: source.into(),
                    ..Default::default()
                }
            }
        }
    };
    selected.context = clean_text(&selected.context);
    selected.text = selected
        .text
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ")
        .chars()
        .take(300)
        .collect();
    selected.source = source.into();
    anyhow::ensure!(
        selected.text.chars().any(|c| c.is_ascii_alphabetic()),
        "请选择英文单词或短语"
    );
    Ok(selected)
}
fn read_uia(point: Point, hover: bool) -> Result<Selection> {
    unsafe {
        let automation: IUIAutomation =
            CoCreateInstance(&CUIAutomation, None, CLSCTX_INPROC_SERVER)?;
        let walker = automation.ControlViewWalker()?;
        let mut element = if hover {
            automation.ElementFromPoint(POINT {
                x: point.x,
                y: point.y,
            })?
        } else {
            automation.GetFocusedElement().or_else(|_| {
                automation.ElementFromPoint(POINT {
                    x: point.x,
                    y: point.y,
                })
            })?
        };
        for _ in 0..10 {
            if let Ok(pattern) =
                element.GetCurrentPatternAs::<IUIAutomationTextPattern>(UIA_TextPatternId)
            {
                let selected = pattern
                    .GetSelection()
                    .ok()
                    .and_then(|ranges| {
                        if ranges.Length().ok()? > 0 {
                            ranges.GetElement(0).ok()
                        } else {
                            None
                        }
                    })
                    .filter(|range| {
                        range
                            .GetText(300)
                            .is_ok_and(|text| !text.to_string().trim().is_empty())
                    });
                let range = if let Some(selected) = selected {
                    selected
                } else if hover {
                    let range = pattern.RangeFromPoint(POINT {
                        x: point.x,
                        y: point.y,
                    })?;
                    range.ExpandToEnclosingUnit(TextUnit_Word)?;
                    range
                } else {
                    element = walker.GetParentElement(&element)?;
                    continue;
                };
                let text = range.GetText(301)?.to_string();
                if !text.chars().any(|c| c.is_ascii_alphabetic()) {
                    element = walker.GetParentElement(&element)?;
                    continue;
                };
                let bounds = range_bounds(&range);
                if hover {
                    if let Some(b) = bounds {
                        if point.x < b[0] - 8
                            || point.x > b[2] + 8
                            || point.y < b[1] - 8
                            || point.y > b[3] + 8
                        {
                            element = walker.GetParentElement(&element)?;
                            continue;
                        }
                    }
                }
                let paragraph = range.Clone()?;
                let context = if paragraph.ExpandToEnclosingUnit(TextUnit_Paragraph).is_ok() {
                    paragraph
                        .GetText(1400)
                        .map(|t| t.to_string())
                        .unwrap_or_default()
                } else {
                    String::new()
                };
                return Ok(Selection {
                    text: clean_text(&text),
                    context: clean_text(&context).chars().take(1200).collect(),
                    bounds,
                    ..Default::default()
                });
            }
            element = walker.GetParentElement(&element)?;
        }
        bail!("当前应用没有可读取的文本")
    }
}
#[link(name = "oleaut32")]
unsafe extern "system" {
    fn SafeArrayAccessData(array: *mut c_void, data: *mut *mut c_void) -> i32;
    fn SafeArrayUnaccessData(array: *mut c_void) -> i32;
    fn SafeArrayGetUBound(array: *mut c_void, dimension: u32, upper: *mut i32) -> i32;
    fn SafeArrayGetLBound(array: *mut c_void, dimension: u32, lower: *mut i32) -> i32;
    fn SafeArrayDestroy(array: *mut c_void) -> i32;
}
fn range_bounds(range: &IUIAutomationTextRange) -> Option<[i32; 4]> {
    unsafe {
        let array = range.GetBoundingRectangles().ok()? as *mut c_void;
        if array.is_null() {
            return None;
        };
        let mut lower = 0;
        let mut upper = 0;
        let mut data = std::ptr::null_mut();
        let mut bounds = None;
        if SafeArrayGetLBound(array, 1, &mut lower) >= 0
            && SafeArrayGetUBound(array, 1, &mut upper) >= 0
            && upper - lower >= 3
            && SafeArrayAccessData(array, &mut data) >= 0
        {
            if !data.is_null() {
                let b = std::slice::from_raw_parts(data as *const f64, 4);
                if b.iter().all(|x| x.is_finite()) && b[2] > 0.0 && b[3] > 0.0 {
                    bounds = Some([
                        b[0] as i32,
                        b[1] as i32,
                        (b[0] + b[2]) as i32,
                        (b[1] + b[3]) as i32,
                    ]);
                }
            }
            SafeArrayUnaccessData(array);
        }
        SafeArrayDestroy(array);
        bounds
    }
}

#[repr(C)]
struct BitmapHeader {
    size: u32,
    width: i32,
    height: i32,
    planes: u16,
    bits: u16,
    compression: u32,
    image_size: u32,
    xppm: i32,
    yppm: i32,
    used: u32,
    important: u32,
}
#[repr(C)]
struct BitmapInfo {
    header: BitmapHeader,
    colors: [u32; 3],
}
#[link(name = "user32")]
unsafe extern "system" {
    fn GetDC(window: isize) -> isize;
    fn ReleaseDC(window: isize, dc: isize) -> i32;
    fn GetSystemMetrics(index: i32) -> i32;
}
#[link(name = "gdi32")]
unsafe extern "system" {
    fn CreateCompatibleDC(dc: isize) -> isize;
    fn CreateCompatibleBitmap(dc: isize, width: i32, height: i32) -> isize;
    fn SelectObject(dc: isize, object: isize) -> isize;
    fn DeleteObject(object: isize) -> i32;
    fn DeleteDC(dc: isize) -> i32;
    fn BitBlt(
        dest: isize,
        x: i32,
        y: i32,
        width: i32,
        height: i32,
        source: isize,
        sx: i32,
        sy: i32,
        operation: u32,
    ) -> i32;
    fn GetDIBits(
        dc: isize,
        bitmap: isize,
        start: u32,
        lines: u32,
        bits: *mut c_void,
        info: *mut BitmapInfo,
        usage: u32,
    ) -> i32;
}
struct Image {
    pixels: Vec<u8>,
    width: i32,
    height: i32,
    left: i32,
    top: i32,
}
fn screenshot(point: Point) -> Result<Image> {
    unsafe {
        let sx = GetSystemMetrics(76);
        let sy = GetSystemMetrics(77);
        let sw = GetSystemMetrics(78);
        let sh = GetSystemMetrics(79);
        let width = 1000.min(sw);
        let height = 260.min(sh);
        anyhow::ensure!(width > 0 && height > 0, "屏幕区域不可用");
        let left = (point.x - width / 2).clamp(sx, sx + sw - width);
        let top = (point.y - height / 2).clamp(sy, sy + sh - height);
        let screen = GetDC(0);
        let dc = CreateCompatibleDC(screen);
        let bitmap = CreateCompatibleBitmap(screen, width, height);
        if dc == 0 || bitmap == 0 {
            if dc != 0 {
                DeleteDC(dc);
            }
            if bitmap != 0 {
                DeleteObject(bitmap);
            }
            ReleaseDC(0, screen);
            bail!("无法读取屏幕")
        };
        let old = SelectObject(dc, bitmap);
        let copied = BitBlt(dc, 0, 0, width, height, screen, left, top, 0x00cc0020) != 0;
        SelectObject(dc, old);
        let mut pixels = vec![0; (width * height * 4) as usize];
        let mut info = BitmapInfo {
            header: BitmapHeader {
                size: 40,
                width,
                height: -height,
                planes: 1,
                bits: 32,
                compression: 0,
                image_size: 0,
                xppm: 0,
                yppm: 0,
                used: 0,
                important: 0,
            },
            colors: [0; 3],
        };
        let lines = if copied {
            GetDIBits(
                dc,
                bitmap,
                0,
                height as u32,
                pixels.as_mut_ptr().cast(),
                &mut info,
                0,
            )
        } else {
            0
        };
        DeleteObject(bitmap);
        DeleteDC(dc);
        ReleaseDC(0, screen);
        anyhow::ensure!(lines == height, "读取屏幕失败");
        for pixel in pixels.chunks_exact_mut(4) {
            pixel[3] = 255
        }
        Ok(Image {
            pixels,
            width,
            height,
            left,
            top,
        })
    }
}
fn ocr(point: Point, image: Image) -> Result<Selection> {
    use windows::{
        Globalization::Language,
        Graphics::Imaging::{BitmapPixelFormat, SoftwareBitmap},
        Media::Ocr::OcrEngine,
        Storage::Streams::DataWriter,
    };
    let writer = DataWriter::new()?;
    writer.WriteBytes(&image.pixels)?;
    let bitmap = SoftwareBitmap::CreateCopyFromBuffer(
        &writer.DetachBuffer()?,
        BitmapPixelFormat::Bgra8,
        image.width,
        image.height,
    )?;
    let engine =
        OcrEngine::TryCreateFromLanguage(&Language::CreateLanguage(&HSTRING::from("en-US"))?)
            .context("Windows 英文 OCR 语言包不可用")?;
    let result = engine.RecognizeAsync(&bitmap)?.get()?;
    let x = (point.x - image.left) as f32;
    let y = (point.y - image.top) as f32;
    let mut chosen = None;
    let mut distance = f32::MAX;
    for line in result.Lines()? {
        for word in line.Words()? {
            let rect = word.BoundingRect()?;
            let dx = if x < rect.X {
                rect.X - x
            } else if x > rect.X + rect.Width {
                x - rect.X - rect.Width
            } else {
                0.0
            };
            let dy = if y < rect.Y {
                rect.Y - y
            } else if y > rect.Y + rect.Height {
                y - rect.Y - rect.Height
            } else {
                0.0
            };
            let d = dx * dx + dy * dy;
            if d < distance && dx <= 12.0 && dy <= 8.0 {
                let text = word
                    .Text()?
                    .to_string()
                    .trim_matches(|c: char| !c.is_ascii_alphabetic() && c != '\'' && c != '-')
                    .to_string();
                if text.chars().any(|c| c.is_ascii_alphabetic()) {
                    distance = d;
                    chosen = Some(Selection {
                        text,
                        context: line.Text()?.to_string(),
                        bounds: Some([
                            image.left + rect.X as i32,
                            image.top + rect.Y as i32,
                            image.left + (rect.X + rect.Width) as i32,
                            image.top + (rect.Y + rect.Height) as i32,
                        ]),
                        ..Default::default()
                    });
                }
            }
        }
    }
    chosen.context("鼠标下未读到英文，请手动输入查询")
}
