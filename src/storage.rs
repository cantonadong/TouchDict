use crate::{
    model::{Definition, Entry, Selection},
    native,
};
use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use serde_json::json;
use sha2::{Digest, Sha256};
use std::{
    fs,
    path::{Path, PathBuf},
};

pub fn config_dir() -> PathBuf {
    PathBuf::from(std::env::var_os("APPDATA").unwrap_or_default()).join("TouchDict")
}
pub fn exe_dir() -> PathBuf {
    std::env::current_exe()
        .ok()
        .and_then(|p| p.parent().map(Path::to_owned))
        .unwrap_or_default()
}
pub fn atomic_write(path: &Path, bytes: &[u8]) -> Result<()> {
    fs::create_dir_all(path.parent().context("存储目录不可用")?)?;
    let temp = path.with_extension(format!("{}.tmp", std::process::id()));
    {
        use std::io::Write;
        let mut f = fs::File::create(&temp)?;
        f.write_all(bytes)?;
        f.sync_all()?;
    }
    native::replace_file(&temp, path).inspect_err(|_| {
        let _ = fs::remove_file(&temp);
    })
}
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct MainWindowSize {
    pub width: u32,
    pub height: u32,
}
impl MainWindowSize {
    pub fn load() -> Option<Self> {
        let value: Self = serde_json::from_slice(&fs::read(config_dir().join("main-window.json")).ok()?).ok()?;
        (value.width >= 820 && value.height >= 560).then_some(value)
    }
    pub fn save(&self) -> Result<()> {
        atomic_write(&config_dir().join("main-window.json"), &serde_json::to_vec(self)?)
    }
}
#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct Config {
    pub model: String,
    pub hotkey_enabled: bool,
    pub alt_enabled: bool,
    pub auto_speak: bool,
    pub startup_enabled: bool,
    pub term_font_size: u32,
    #[serde(rename = "resultContentFontSize")]
    pub content_font_size: u32,
}
impl Default for Config {
    fn default() -> Self {
        Self {
            model: String::new(),
            hotkey_enabled: true,
            alt_enabled: true,
            auto_speak: true,
            startup_enabled: true,
            term_font_size: 23,
            content_font_size: 15,
        }
    }
}
impl Config {
    pub fn load() -> Result<Self> {
        let path = config_dir().join("settings.json");
        if !path.exists() {
            return Ok(Self::default());
        }
        let value: serde_json::Value = serde_json::from_slice(&fs::read(path)?)?;
        let mut c: Self = serde_json::from_value(value.clone())?;
        // Apply the new typography baseline once, including existing installations.
        if value["typographyVersion"] != 1 {
            c.term_font_size = 23;
            c.content_font_size = 15;
        }
        if !c.model.starts_with("chatgpt:") {
            c.model.clear();
        }
        c.term_font_size = c.term_font_size.clamp(20, 48);
        c.content_font_size = c.content_font_size.clamp(12, 24);
        Ok(c)
    }
    pub fn save(&self) -> Result<()> {
        let path = config_dir().join("settings.json");
        let mut v = serde_json::to_value(self)?;
        v["modelProvider"] = json!("codex");
        v["typographyVersion"] = json!(1);
        atomic_write(&path, &serde_json::to_vec_pretty(&v)?)
    }
    pub fn label(&self) -> String {
        self.model
            .strip_prefix("chatgpt:")
            .map(|s| format!("Codex · {s}"))
            .unwrap_or_else(|| "Codex · 尚未连接".into())
    }
}
#[derive(Default, Deserialize, Serialize)]
pub struct History {
    pub entries: Vec<Entry>,
}
impl History {
    pub fn load() -> Result<Self> {
        let p = exe_dir().join("touchdict_cache.json");
        if !p.exists() {
            return Ok(Self::default());
        }
        serde_json::from_slice(&fs::read(p).context("无法读取已有查词历史")?)
            .context("已有查词历史格式无效，请先备份并修复历史文件")
    }
    pub fn save(&self) -> Result<()> {
        atomic_write(
            &exe_dir().join("touchdict_cache.json"),
            &serde_json::to_vec(self)?,
        )
    }
    pub fn cached(&self, s: &Selection) -> Option<Definition> {
        self.entries
            .iter()
            .rev()
            .find(|e| {
                e.prompt_version == 6
                    && e.query.eq_ignore_ascii_case(&s.text)
                    && e.context == s.context
            })
            .map(|e| e.definition.clone())
    }
    pub fn store(&mut self, s: &Selection, d: Definition) -> Result<()> {
        let old = self.entries.iter().position(|e| {
            e.query.eq_ignore_ascii_case(&s.text)
                && e.context == s.context
                && e.definition.meaning_zh == d.meaning_zh
                && e.definition.part_of_speech == d.part_of_speech
        });
        let (key, count) = if let Some(i) = old {
            let old = self.entries.remove(i);
            (old.key, old.count + 1)
        } else {
            (
                format!(
                    "rust:{:x}",
                    Sha256::digest(format!(
                        "{}\0{}\0{}\0{}",
                        s.text.to_lowercase(),
                        s.context,
                        d.meaning_zh,
                        d.part_of_speech
                    ))
                ),
                1,
            )
        };
        self.entries.push(Entry {
            key,
            query: s.text.clone(),
            context: s.context.clone(),
            definition: d,
            count,
            prompt_version: 6,
        });
        if self.entries.len() > 500 {
            self.entries.drain(..self.entries.len() - 500);
        }
        self.save()
    }
}
