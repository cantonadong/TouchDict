pub enum Page {
    Main,
    Popup,
    Settings,
}

pub fn html(page: Page) -> String {
    let (html, css, js) = match page {
        Page::Main => (
            include_str!("../internal/mainwindow/web/index.html"),
            include_str!("../internal/mainwindow/web/style.css").to_string(),
            include_str!("../internal/mainwindow/web/app.js"),
        ),
        Page::Popup => (
            include_str!("../internal/popup/web/index.html"),
            format!(
                "{}\n{}",
                include_str!("../internal/mainwindow/web/style.css"),
                include_str!("../internal/popup/web/style.css")
            ),
            include_str!("../internal/popup/web/app.js"),
        ),
        Page::Settings => (
            include_str!("../ui/settings.html"),
            include_str!("../ui/settings.css").to_string(),
            include_str!("../ui/settings.js"),
        ),
    };
    html.replace("<!-- STYLE -->", &format!("<style>{css}</style>"))
        .replace("<!-- SCRIPT -->", &format!("<script>{js}</script>"))
}
