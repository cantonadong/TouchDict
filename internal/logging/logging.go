package logging

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

func Open() (*log.Logger, io.Closer, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return nil, nil, err
	}
	dir := filepath.Join(base, "TouchDict")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, nil, err
	}
	path := filepath.Join(dir, "touchdict.log")
	if info, e := os.Stat(path); e == nil && info.Size() > 1024*1024 {
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, nil, err
	}
	return log.New(f, "", log.Ldate|log.Ltime|log.LUTC), f, nil
}
