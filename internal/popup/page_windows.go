//go:build windows

package popup

import (
	"embed"
	"strings"
	"touchdict/internal/mainwindow"
)

//go:embed web/index.html web/style.css web/app.js
var pageFiles embed.FS

func popupPage() string {
	html, _ := pageFiles.ReadFile("web/index.html")
	css, _ := pageFiles.ReadFile("web/style.css")
	js, _ := pageFiles.ReadFile("web/app.js")
	page := strings.Replace(string(html), "<!-- STYLE -->", "<style>"+mainwindow.SharedStyle()+string(css)+"</style>", 1)
	return strings.Replace(page, "<!-- SCRIPT -->", "<script>"+string(js)+"</script>", 1)
}
