use std::{
    sync::{Arc, atomic::{AtomicU64, Ordering}, mpsc::{self, Sender}},
    thread,
    time::{Duration, Instant},
};
use windows::{
    Win32::{Media::Speech::*, System::Com::*},
    core::{PCWSTR, w},
};

pub struct Speech {
    sender: Sender<(String, u64, bool)>,
    generation: Arc<AtomicU64>,
}
#[link(name = "winmm")]
unsafe extern "system" {
    fn mciSendStringW(command: *const u16, result: *mut u16, length: u32, callback: isize) -> u32;
}
fn mci(command: &str) -> bool {
    let wide: Vec<u16> = command.encode_utf16().chain(Some(0)).collect();
    unsafe { mciSendStringW(wide.as_ptr(), std::ptr::null_mut(), 0, 0) == 0 }
}
fn online(text: &str, id: u64, current: &AtomicU64) -> bool {
    let result = (|| -> anyhow::Result<bool> {
        let client = reqwest::blocking::Client::builder().timeout(Duration::from_secs(8)).build()?;
        let bytes = client.get("https://translate.google.com/translate_tts")
            .query(&[("ie", "UTF-8"), ("client", "tw-ob"), ("tl", "en-US"), ("q", text)])
            .header("User-Agent", "Mozilla/5.0").send()?.error_for_status()?.bytes()?;
        if current.load(Ordering::Relaxed) != id || bytes.is_empty() || bytes.len() > 4 * 1024 * 1024 { return Ok(false); }
        let path = std::env::temp_dir().join(format!("touchdict-speech-{}-{id}.mp3", std::process::id()));
        std::fs::write(&path, bytes)?;
        mci("close touchdictspeech");
        let played = mci(&format!("open \"{}\" type mpegvideo alias touchdictspeech", path.display())) && mci("play touchdictspeech");
        if played {
            loop {
                if current.load(Ordering::Relaxed) != id { break; }
                let mut status = [0u16; 32];
                let command: Vec<u16> = "status touchdictspeech mode".encode_utf16().chain(Some(0)).collect();
                let ok = unsafe { mciSendStringW(command.as_ptr(), status.as_mut_ptr(), 32, 0) == 0 };
                if !ok || !String::from_utf16_lossy(&status).starts_with("playing") { break; }
                thread::sleep(Duration::from_millis(20));
            }
        }
        mci("close touchdictspeech");
        let _ = std::fs::remove_file(path);
        Ok(played)
    })();
    result.unwrap_or(false)
}
impl Speech {
    pub fn new() -> Self {
        let (tx, rx) = mpsc::channel::<(String, u64, bool)>();
        let generation = Arc::new(AtomicU64::new(0));
        let current = generation.clone();
        thread::spawn(move || unsafe {
            let initialized = CoInitializeEx(None, COINIT_APARTMENTTHREADED).is_ok();
            let voice: windows::core::Result<ISpVoice> =
                CoCreateInstance(&SpVoice, None, CLSCTX_INPROC_SERVER);
            if let Ok(ref voice) = voice {
                let category: windows::core::Result<ISpObjectTokenCategory> =
                    CoCreateInstance(&SpObjectTokenCategory, None, CLSCTX_INPROC_SERVER);
                if let Ok(category) = category {
                    if category
                        .SetId(
                            w!("HKEY_LOCAL_MACHINE\\SOFTWARE\\Microsoft\\Speech\\Voices"),
                            false,
                        )
                        .is_ok()
                    {
                        if let Ok(tokens) = category.EnumTokens(w!("Language=409"), None) {
                            let mut token = None;
                            if tokens.Next(1, &mut token, None).is_ok() {
                                if let Some(token) = token {
                                    let _ = voice.SetVoice(&token);
                                }
                            }
                        }
                    }
                }
            }
            let mut started = Instant::now();
            for (text, id, again) in rx {
                if current.load(Ordering::Relaxed) != id { continue; }
                if text.is_empty() {
                    mci("close touchdictspeech");
                    if let Ok(ref voice) = voice { let _ = voice.Speak(w!(""), 3, None); }
                    continue;
                }
                if again {
                    while started.elapsed() < Duration::from_millis(1500) && current.load(Ordering::Relaxed) == id {
                        thread::sleep(Duration::from_millis(20));
                    }
                }
                if current.load(Ordering::Relaxed) != id { continue; }
                started = Instant::now();
                if !online(&text, id, &current) && current.load(Ordering::Relaxed) == id {
                    if let Ok(ref voice) = voice {
                        let wide: Vec<u16> = text.encode_utf16().chain(Some(0)).collect();
                        if voice.Speak(PCWSTR(wide.as_ptr()), 3, None).is_ok() {
                            while current.load(Ordering::Relaxed) == id {
                                let mut status = SPVOICESTATUS::default();
                                if voice.GetStatus(&mut status, std::ptr::null_mut()).is_err() || status.dwRunningState == SPRS_DONE.0 as u32 { break; }
                                thread::sleep(Duration::from_millis(20));
                            }
                            if current.load(Ordering::Relaxed) != id { let _ = voice.Speak(w!(""), 3, None); }
                        }
                    } else { crate::diagnostics::record("speech: online unavailable and SAPI initialization failed"); }
                }
            }
            drop(voice);
            if initialized { CoUninitialize(); }
        });
        Self { sender: tx, generation }
    }
    pub fn speak(&self, text: &str) {
        let id = self.generation.fetch_add(1, Ordering::Relaxed) + 1;
        mci("stop touchdictspeech");
        let _ = self.sender.send((text.chars().take(2000).collect(), id, false));
    }
    pub fn speak_again(&self, text: &str) {
        let id = self.generation.load(Ordering::Relaxed);
        let _ = self.sender.send((text.chars().take(2000).collect(), id, true));
    }
    pub fn stop(&self) {
        let id = self.generation.fetch_add(1, Ordering::Relaxed) + 1;
        mci("stop touchdictspeech");
        let _ = self.sender.send((String::new(), id, false));
    }
}
