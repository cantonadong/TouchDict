package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

type Config struct {
	APIKey           string `json:"-"`
	Model            string `json:"model"`
	ModelProvider    string `json:"modelProvider"`
	LocalModelDir    string `json:"localModelDir"`
	LocalModel       string `json:"localModel"`
	HotkeyEnabled    bool   `json:"hotkeyEnabled"`
	MiddleEnabled    bool   `json:"middleEnabled"`
	AltEnabled       bool   `json:"altEnabled"`
	AutoSpeak        bool   `json:"autoSpeak"`
	StartupEnabled   bool   `json:"startupEnabled"`
	ResultFontScale  int    `json:"resultFontScale"`
	TermFontSize     int    `json:"termFontSize"`
	ContentFontSize  int    `json:"resultContentFontSize"`
	MainWindowHeight int    `json:"mainWindowHeightPixels"`
	configDir        string
}

type diskConfig struct {
	Model           string `json:"model"`
	ModelProvider   string `json:"modelProvider,omitempty"`
	LocalModelDir   string `json:"localModelDir,omitempty"`
	LocalModel      string `json:"localModel,omitempty"`
	HotkeyEnabled   bool   `json:"hotkeyEnabled"`
	MiddleEnabled   bool   `json:"middleEnabled"`
	AltEnabled      *bool  `json:"altEnabled,omitempty"`
	AutoSpeak       bool   `json:"autoSpeak"`
	StartupEnabled  *bool  `json:"startupEnabled,omitempty"`
	ResultFontScale *int   `json:"resultFontScale,omitempty"`
	TermFontSize    *int   `json:"termFontSize,omitempty"`
	ContentFontSize *int   `json:"resultContentFontSize,omitempty"`
	// Legacy mainWindowHeight used virtualized coordinates; start at the
	// corrected 1200-screen-pixel default until a new preference is saved.
	MainWindowHeight *int   `json:"mainWindowHeightPixels,omitempty"`
	ProtectedKey     []byte `json:"protectedKey,omitempty"`
}

type dataBlob struct {
	Size uint32
	Data *byte
}

var (
	crypt32            = syscall.NewLazyDLL("crypt32.dll")
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	cryptProtectData   = crypt32.NewProc("CryptProtectData")
	cryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	localFree          = kernel32.NewProc("LocalFree")
)

func defaults() Config {
	return Config{Model: "gemini-3.1-flash-lite", ModelProvider: "online", LocalModelDir: `D:\Models`, HotkeyEnabled: true, MiddleEnabled: true, AltEnabled: true, AutoSpeak: true, StartupEnabled: true, ResultFontScale: 100, TermFontSize: 30, ContentFontSize: 12, MainWindowHeight: 1200}
}

func Load(exeDir string) (Config, error) {
	cfg := defaults()
	base, err := os.UserConfigDir()
	if err != nil {
		return cfg, err
	}
	cfg.configDir = filepath.Join(base, "TouchDict")
	b, err := os.ReadFile(filepath.Join(cfg.configDir, "settings.json"))
	if err == nil {
		var d diskConfig
		if json.Unmarshal(b, &d) == nil {
			if d.ModelProvider == "local" {
				cfg.ModelProvider = "local"
			}
			if d.LocalModelDir != "" {
				cfg.LocalModelDir = d.LocalModelDir
			}
			cfg.LocalModel = d.LocalModel
			if d.Model != "" {
				cfg.Model = d.Model
			}
			if cfg.Model == "gemini-flash-lite-latest" {
				cfg.Model = "gemini-3.1-flash-lite"
			}
			cfg.HotkeyEnabled, cfg.MiddleEnabled, cfg.AutoSpeak = d.HotkeyEnabled, d.MiddleEnabled, d.AutoSpeak
			if d.AltEnabled != nil {
				cfg.AltEnabled = *d.AltEnabled
			}
			if d.StartupEnabled != nil {
				cfg.StartupEnabled = *d.StartupEnabled
			}
			if d.ResultFontScale != nil && *d.ResultFontScale >= 80 && *d.ResultFontScale <= 200 {
				cfg.ResultFontScale = *d.ResultFontScale
			}
			if d.TermFontSize != nil && *d.TermFontSize >= 8 && *d.TermFontSize <= 72 {
				cfg.TermFontSize = *d.TermFontSize
			}
			if d.ContentFontSize != nil && *d.ContentFontSize >= 8 && *d.ContentFontSize <= 72 {
				cfg.ContentFontSize = *d.ContentFontSize
			}
			if d.MainWindowHeight != nil && *d.MainWindowHeight >= 560 && *d.MainWindowHeight <= 32767 {
				cfg.MainWindowHeight = *d.MainWindowHeight
			}
			if len(d.ProtectedKey) > 0 {
				cfg.APIKey, _ = unprotect(d.ProtectedKey)
			}
		}
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		cfg.APIKey = strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		for _, p := range []string{filepath.Join(exeDir, "gemini_key.txt"), filepath.Join(filepath.Dir(exeDir), "gemini_key.txt")} {
			if b, e := os.ReadFile(p); e == nil {
				cfg.APIKey = strings.TrimSpace(string(b))
				break
			}
		}
	}
	return cfg, nil
}

func (c Config) Save() error {
	if c.configDir == "" {
		return errors.New("配置目录不可用")
	}
	if err := os.MkdirAll(c.configDir, 0700); err != nil {
		return err
	}
	p, err := protect(c.APIKey)
	if err != nil {
		return err
	}
	altEnabled := c.AltEnabled
	startupEnabled := c.StartupEnabled
	fontScale := c.ResultFontScale
	termSize, contentSize := c.TermFontSize, c.ContentFontSize
	height := c.MainWindowHeight
	d := diskConfig{Model: c.Model, HotkeyEnabled: c.HotkeyEnabled, MiddleEnabled: c.MiddleEnabled, AltEnabled: &altEnabled, AutoSpeak: c.AutoSpeak, StartupEnabled: &startupEnabled, ResultFontScale: &fontScale, TermFontSize: &termSize, ContentFontSize: &contentSize, MainWindowHeight: &height, ProtectedKey: p}
	d.ModelProvider, d.LocalModelDir, d.LocalModel = c.ModelProvider, c.LocalModelDir, c.LocalModel
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.configDir, "settings.json"), b, 0600)
}

func protect(s string) ([]byte, error)   { return crypt([]byte(s), true) }
func unprotect(b []byte) (string, error) { out, err := crypt(b, false); return string(out), err }

func crypt(in []byte, enc bool) ([]byte, error) {
	if len(in) == 0 {
		return nil, nil
	}
	inBlob := dataBlob{uint32(len(in)), &in[0]}
	var out dataBlob
	proc := cryptProtectData
	args := []uintptr{uintptr(unsafe.Pointer(&inBlob)), 0, 0, 0, 0, 1, uintptr(unsafe.Pointer(&out))}
	if !enc {
		proc = cryptUnprotectData
		args = []uintptr{uintptr(unsafe.Pointer(&inBlob)), 0, 0, 0, 0, 1, uintptr(unsafe.Pointer(&out))}
	}
	r, _, e := proc.Call(args...)
	if r == 0 {
		return nil, e
	}
	defer localFree.Call(uintptr(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}
