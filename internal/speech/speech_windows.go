//go:build windows

package speech

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

const mciAlias = "touchdictspeech"

var mciSendString = syscall.NewLazyDLL("winmm.dll").NewProc("mciSendStringW")

type Service struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	cmd    *exec.Cmd
}

func New() *Service                { return &Service{} }
func (s *Service) Available() bool { return s != nil }

// Speak uses an online en-US voice first, so systems with SAPI/voice packages
// removed can still speak. SAPI remains a best-effort offline fallback.
func (s *Service) Speak(text string) error {
	if s == nil {
		return errors.New("发音服务不可用")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	s.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	go func() {
		if err := s.speakOnline(ctx, text); err != nil && ctx.Err() == nil {
			_ = s.speakSAPI(ctx, text)
		}
	}()
	return nil
}

func (s *Service) speakOnline(ctx context.Context, text string) error {
	q := url.Values{"ie": {"UTF-8"}, "client": {"tw-ob"}, "tl": {"en-US"}, "q": {text}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://translate.google.com/translate_tts?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("online speech HTTP %d", resp.StatusCode)
	}
	f, err := os.CreateTemp("", "touchdict-speech-*.mp3")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	_, copyErr := io.Copy(f, io.LimitReader(resp.Body, 4<<20))
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	_ = mci("close " + mciAlias)
	if err := mci(`open "` + name + `" type mpegvideo alias ` + mciAlias); err != nil {
		return err
	}
	defer mci("close " + mciAlias)
	return mci("play " + mciAlias + " wait")
}

func (s *Service) speakSAPI(ctx context.Context, text string) error {
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		return err
	}
	data := base64.StdEncoding.EncodeToString([]byte(text))
	script := "$b=[Convert]::FromBase64String('" + data + "');$t=[Text.Encoding]::UTF8.GetString($b);$v=New-Object -ComObject SAPI.SpVoice;$null=$v.Speak($t)"
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-EncodedCommand", encodeCommand(script))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	s.mu.Lock()
	s.cmd = cmd
	s.mu.Unlock()
	err := cmd.Run()
	s.mu.Lock()
	if s.cmd == cmd {
		s.cmd = nil
	}
	s.mu.Unlock()
	return err
}

func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel, cmd := s.cancel, s.cmd
	s.cancel, s.cmd = nil, nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	_ = mci("stop " + mciAlias)
	_ = mci("close " + mciAlias)
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func (s *Service) Close() { s.Stop() }

func mci(command string) error {
	p, err := syscall.UTF16PtrFromString(command)
	if err != nil {
		return err
	}
	r, _, _ := mciSendString.Call(uintptr(unsafe.Pointer(p)), 0, 0, 0)
	if r != 0 {
		return fmt.Errorf("MCI error %d", r)
	}
	return nil
}

func encodeCommand(v string) string {
	r := utf16.Encode([]rune(v))
	b := make([]byte, len(r)*2)
	for i, x := range r {
		binary.LittleEndian.PutUint16(b[i*2:], x)
	}
	return base64.StdEncoding.EncodeToString(b)
}
