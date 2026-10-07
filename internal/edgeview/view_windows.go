//go:build windows && amd64

// Package edgeview embeds WebView2 in an existing UI-thread HWND and message loop.
package edgeview

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"github.com/jchv/go-webview2/webviewloader"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

type View struct {
	hwnd                             uintptr
	environment, controller, webview *object
	events                           []registration
	html                             string
	initialized, closed, failed      bool
	loaded                           bool
	loadingDocument                  bool
	message                          func(string)
	onError                          func(error)
	diagnostic                       func(string)
}

// New starts asynchronous initialization. All callbacks run on the HWND's UI
// thread; clients must defer modal dialogs until outside a COM event callback.
func New(hwnd uintptr, onMessage func(string), onError func(error), diagnostic func(string)) (*View, error) {
	version, err := webviewloader.GetInstalledVersion()
	// Prefer the installed version's system directory, with the user's
	// confirmed installation as a fallback if registry discovery is unavailable.
	var browserFolder string
	for _, folder := range []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "EdgeWebView", "Application", version),
		`C:\Program Files (x86)\Microsoft\EdgeWebView\Application\153.0.4234.32`,
	} {
		if info, statErr := os.Stat(filepath.Join(folder, "msedgewebview2.exe")); statErr == nil && !info.IsDir() {
			browserFolder = folder
			break
		}
	}
	if browserFolder == "" && err != nil {
		return nil, fmt.Errorf("无法检测 Edge WebView2 Runtime：%w", err)
	}
	if browserFolder == "" && version == "" {
		return nil, fmt.Errorf("无法找到系统 Edge WebView2 Runtime，请检查 Microsoft\\EdgeWebView\\Application 系统目录")
	}
	var browserPath *uint16
	if browserFolder != "" {
		browserPath, err = windows.UTF16PtrFromString(browserFolder)
		if err != nil {
			return nil, err
		}
	}
	root := os.Getenv("LOCALAPPDATA")
	if root == "" {
		return nil, fmt.Errorf("无法找到 WebView2 用户数据目录")
	}
	dataDir := filepath.Join(root, "TouchDict", "WebView2")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("无法创建 WebView2 用户数据目录：%w", err)
	}
	path, err := windows.UTF16PtrFromString(dataDir)
	if err != nil {
		return nil, err
	}
	v := &View{hwnd: hwnd, message: onMessage, onError: onError, diagnostic: diagnostic}
	v.trace("runtime version=" + version)
	v.trace("runtime directory=" + browserFolder)
	h := newHandler("{4E8A3389-C9D8-4BD2-B6B5-124FEE6CC14D}", v.environmentCompleted)
	hr, err := webviewloader.CreateCoreWebView2EnvironmentWithOptions(browserPath, path, 0, h.address())
	runtime.KeepAlive(browserPath)
	runtime.KeepAlive(path)
	// The asynchronous API owns its reference after a successful call.
	h.release()
	if err != nil {
		return nil, fmt.Errorf("初始化 WebView2 失败：%w", err)
	}
	if err := result(hr, "初始化 WebView2"); err != nil {
		return nil, err
	}
	return v, nil
}

func (v *View) trace(message string) {
	if v.diagnostic != nil {
		v.diagnostic("webview2: " + message)
	}
}

func (v *View) fail(err error) {
	if err == nil || v.closed || v.failed {
		return
	}
	v.failed = true
	v.trace("failure: " + err.Error())
	if v.onError != nil {
		v.onError(err)
	}
}

func (v *View) environmentCompleted(hr, pointer uintptr) uintptr {
	if v.closed {
		return 0
	}
	if err := result(hr, "创建 WebView2 环境"); err != nil {
		v.fail(err)
		return 0
	}
	if pointer == 0 {
		v.fail(fmt.Errorf("WebView2 未返回可用环境"))
		return 0
	}
	v.environment = asObject(pointer)
	v.environment.call(1)
	h := newHandler("{6C4819F3-C9B7-4260-8127-C9F5BDE7F68C}", v.controllerCompleted)
	err := result(v.environment.call(3, v.hwnd, h.address()), "创建 WebView2 控件")
	h.release()
	v.fail(err)
	return 0
}

func (v *View) controllerCompleted(hr, pointer uintptr) uintptr {
	if v.closed {
		if pointer != 0 {
			asObject(pointer).call(24)
		}
		return 0
	}
	if err := result(hr, "创建 WebView2 控件"); err != nil {
		v.fail(err)
		return 0
	}
	if pointer == 0 {
		v.fail(fmt.Errorf("WebView2 未返回可用控件"))
		return 0
	}
	v.controller = asObject(pointer)
	v.controller.call(1)
	if err := result(v.controller.call(25, uintptr(unsafe.Pointer(&v.webview))), "获取 WebView2 页面"); err != nil {
		v.fail(err)
		return 0
	}
	if v.webview == nil {
		v.fail(fmt.Errorf("WebView2 页面不可用"))
		return 0
	}
	if err := v.configure(); err != nil {
		v.fail(err)
		return 0
	}
	v.initialized = true
	v.Resize()
	v.Show()
	v.navigate()
	return 0
}

func (v *View) configure() error {
	var settings *object
	if err := result(v.webview.call(3, uintptr(unsafe.Pointer(&settings))), "获取 WebView2 设置"); err != nil {
		return err
	}
	if settings == nil {
		return fmt.Errorf("WebView2 设置不可用")
	}
	defer settings.release()
	// Stable ICoreWebView2Settings only. Newer interfaces must be queried
	// explicitly rather than indexing past this base interface's vtable.
	for _, slot := range []int{8, 10, 12, 14, 16, 18, 20} {
		if err := result(settings.call(slot, 0), "配置 WebView2 设置"); err != nil {
			return err
		}
	}
	if err := v.listen(v.webview, 34, "{57213F19-00E6-49FA-8E07-898EA01ECBD2}", func(_, args uintptr) uintptr {
		if v.closed || v.failed {
			return 0
		}
		a := asObject(args)
		source, err := a.string(3)
		if err != nil || source != "about:blank" {
			return 0
		}
		message, err := a.string(5)
		if err == nil && v.message != nil {
			v.loaded = true
			v.message(message)
		}
		return 0
	}); err != nil {
		return err
	}
	if err := v.listen(v.webview, 7, "{9ADBE429-F36D-432B-9DDC-F8881FBD76E3}", func(_, args uintptr) uintptr {
		a := asObject(args)
		uri, err := a.string(3)
		// Authorize the initial document by the host-issued NavigateToString,
		// not its NavigationStarting URI. Runtime implementations can expose
		// an internal URI here while the resulting document is about:blank.
		// The embedded bootstrap has no links, external resources or navigation;
		// all subsequent navigation is blocked once it sends ready.
		allowed := err == nil && v.loadingDocument && !v.loaded
		scheme, _, _ := strings.Cut(uri, ":")
		v.trace(fmt.Sprintf("navigation starting: scheme=%q uriLength=%d allowed=%t", scheme, len(uri), allowed))
		if !allowed {
			a.call(8, 1)
		} // put_Cancel
		return 0
	}); err != nil {
		return err
	}
	if err := v.listen(v.webview, 44, "{D4C185FE-C81C-4989-97AF-2D3FA7AB5651}", func(_, args uintptr) uintptr {
		asObject(args).call(7, 1) // put_Handled: never launch another window
		return 0
	}); err != nil {
		return err
	}
	if err := v.listen(v.webview, 23, "{15E1C6A3-C72A-4DF3-91D7-D097FBEC6BFD}", func(_, args uintptr) uintptr {
		asObject(args).call(7, 2) // put_State = DENY
		return 0
	}); err != nil {
		return err
	}
	if err := v.listen(v.webview, 15, "{D33A35BF-1C49-4F98-93AB-006E0533FE1C}", func(_, args uintptr) uintptr {
		var ok int32
		a := asObject(args)
		var status int32
		a.call(4, uintptr(unsafe.Pointer(&status)))
		if err := result(a.call(3, uintptr(unsafe.Pointer(&ok))), "读取页面加载状态"); err != nil {
			v.fail(err)
		} else {
			v.trace(fmt.Sprintf("navigation completed: success=%t status=%d ready=%t", ok != 0, status, v.loaded))
			// Superseded initial navigation and deliberately blocked navigation
			// report OPERATION_CANCELED (14), not a broken local document.
			if ok == 0 && status != 14 && !v.loaded {
				v.fail(fmt.Errorf("WebView2 主窗口页面加载失败（错误码 %d），请重新打开程序", status))
			}
		}
		return 0
	}); err != nil {
		return err
	}
	return v.listen(v.webview, 25, "{79E0AEA4-990B-42D9-AA1D-0FCC2E5BC7F1}", func(_, _ uintptr) uintptr {
		v.fail(fmt.Errorf("WebView2 页面进程异常退出，请重新打开 TouchDict"))
		return 0
	})
}

func (v *View) SetHTML(html string) {
	v.html = html
	if v.initialized {
		v.navigate()
	}
}

func (v *View) navigate() {
	if v.html == "" || v.closed || v.failed {
		return
	}
	text, err := windows.UTF16PtrFromString(v.html)
	if err != nil {
		v.fail(err)
		return
	}
	v.trace("loading embedded main window")
	v.loadingDocument = true
	v.fail(result(v.webview.call(6, uintptr(unsafe.Pointer(text))), "加载主窗口页面"))
	runtime.KeepAlive(text)
}

func (v *View) Eval(script string) {
	if !v.initialized || v.closed || v.failed {
		return
	}
	text, err := windows.UTF16PtrFromString(script)
	if err != nil {
		v.fail(err)
		return
	}
	v.fail(result(v.webview.call(29, uintptr(unsafe.Pointer(text)), 0), "更新主窗口页面"))
	runtime.KeepAlive(text)
}

func (v *View) Resize() {
	if !v.initialized || v.closed {
		return
	}
	var bounds win.RECT
	if win.GetClientRect(win.HWND(v.hwnd), &bounds) {
		// RECT is a 16-byte by-value struct, passed indirectly on Windows x64.
		v.fail(result(v.controller.call(6, uintptr(unsafe.Pointer(&bounds))), "调整主窗口页面大小"))
	}
}

func (v *View) ParentMoved() {
	if v.initialized && !v.closed {
		v.controller.call(23)
	}
}

func (v *View) Focus() {
	if v.initialized && !v.closed {
		v.controller.call(12, 0)
	}
}

func (v *View) Show() {
	if v.initialized && !v.closed {
		v.controller.call(4, 1)
	}
}
func (v *View) Hide() {
	if v.initialized && !v.closed {
		v.controller.call(4, 0)
	}
}

func (v *View) Close() {
	if v.closed {
		return
	}
	v.closed = true
	for _, event := range v.events {
		event.object.call(event.remove, uintptr(event.token))
		event.handler.release()
	}
	v.events = nil
	if v.controller != nil {
		v.controller.call(24)
	}
	v.webview.release()
	v.controller.release()
	v.environment.release()
	v.webview, v.controller, v.environment = nil, nil, nil
}
