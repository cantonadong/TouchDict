//go:build windows

package localmodel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type serverProcess struct {
	command               *exec.Cmd
	done                  chan struct{}
	job                   windows.Handle
	url, token, modelPath string
}

type Runtime struct {
	mu        sync.Mutex
	gate      chan struct{}
	server    *serverProcess
	directory string
	http      *http.Client
}

func NewRuntime(exeDir string) *Runtime {
	return &Runtime{directory: filepath.Join(exeDir, "local-runtime"), gate: make(chan struct{}, 1), http: &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 90 * time.Second}}
}

func (r *Runtime) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked()
}

func (r *Runtime) stopLocked() {
	if r.server != nil {
		windows.CloseHandle(r.server.job)
		r.server = nil
	}
}

func (r *Runtime) Close() {
	r.Stop()
	r.http.CloseIdleConnections()
}

func (r *Runtime) start(ctx context.Context, modelPath string) (*serverProcess, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.server != nil && r.server.modelPath == modelPath {
		select {
		case <-r.server.done:
			r.stopLocked()
		default:
			return r.server, nil
		}
	}
	r.stopLocked()
	if info, err := os.Stat(modelPath); err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("本地模型文件不存在或无法读取，请在设置中重新检索")
	}
	exe := filepath.Join(r.directory, "llama-server.exe")
	if _, err := os.Stat(exe); err != nil {
		return nil, fmt.Errorf("缺少本地运行时，请保留 TouchDict.exe 旁的 local-runtime 文件夹")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("无法分配本地模型端口：%w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	var secret [24]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(secret[:])
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	command := exec.Command(exe, "--model", modelPath, "--host", "127.0.0.1", "--port", strconv.Itoa(port), "--api-key", token,
		"--ctx-size", "4096", "--parallel", "1", "--n-gpu-layers", "0", "--jinja", "--reasoning", "off", "--no-webui")
	command.Dir = r.directory
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	logDir, err := os.UserCacheDir()
	if err == nil {
		logDir = filepath.Join(logDir, "TouchDict")
		_ = os.MkdirAll(logDir, 0700)
	} else {
		logDir = os.TempDir()
	}
	logFile, logErr := os.OpenFile(filepath.Join(logDir, "local-model.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if logErr == nil {
		command.Stdout, command.Stderr = logFile, logFile
	}
	if err := command.Start(); err != nil {
		windows.CloseHandle(job)
		if logFile != nil {
			logFile.Close()
		}
		return nil, fmt.Errorf("无法启动本地模型：%w", err)
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(command.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
		windows.CloseHandle(process)
	}
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		windows.CloseHandle(job)
		if logFile != nil {
			logFile.Close()
		}
		return nil, fmt.Errorf("无法管理本地模型进程：%w", err)
	}
	server := &serverProcess{command: command, done: make(chan struct{}), job: job, url: "http://127.0.0.1:" + strconv.Itoa(port), token: token, modelPath: modelPath}
	r.server = server
	go func() {
		_ = command.Wait()
		if logFile != nil {
			logFile.Close()
		}
		close(server.done)
	}()
	return server, nil
}

func (r *Runtime) ready(ctx context.Context, server *serverProcess) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-server.done:
			return errors.New("本地模型启动失败，请查看日志 local-model.log")
		default:
		}
		probeCtx, cancel := context.WithTimeout(ctx, time.Second)
		request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, server.url+"/health", nil)
		if err != nil {
			cancel()
			return err
		}
		request.Header.Set("Authorization", "Bearer "+server.token)
		response, err := r.http.Do(request)
		ready := false
		if err == nil {
			ready = response.StatusCode == http.StatusOK
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			response.Body.Close()
		}
		cancel()
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-server.done:
			return errors.New("本地模型启动失败，请查看日志 local-model.log")
		case <-ticker.C:
		}
	}
}
