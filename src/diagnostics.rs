use std::io::Write;

pub fn record(message: &str) {
    let root = std::env::var_os("LOCALAPPDATA")
        .map(std::path::PathBuf::from)
        .unwrap_or_else(std::env::temp_dir)
        .join("TouchDict");
    if std::fs::create_dir_all(&root).is_err() {
        return;
    }
    if let Ok(mut file) = std::fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(root.join("rust-ui.log"))
    {
        let time = time::OffsetDateTime::now_utc()
            .format(&time::format_description::well_known::Rfc3339)
            .unwrap_or_default();
        let _ = writeln!(file, "{time} {} {}", std::process::id(), message);
    }
}
