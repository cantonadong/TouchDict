//go:build windows

package mainwindow

import (
	"embed"
	"strings"
)

//go:embed web/index.html web/style.css web/app.js
var pageFiles embed.FS

func mainPage() string {
	html, _ := pageFiles.ReadFile("web/index.html")
	css, _ := pageFiles.ReadFile("web/style.css")
	js, _ := pageFiles.ReadFile("web/app.js")
	page := strings.Replace(string(html), "<!-- STYLE -->", "<style>"+string(css)+"</style>", 1)
	return strings.Replace(page, "<!-- SCRIPT -->", "<script>"+string(js)+"</script>", 1)
}

// SharedStyle keeps popup and main-window components visually consistent.
func SharedStyle() string { css, _ := pageFiles.ReadFile("web/style.css"); return string(css) }
