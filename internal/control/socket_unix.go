//go:build unix

package control

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func SocketDirectory(stateDir string) (string, error) {
	absolute, err := filepath.Abs(stateDir)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(absolute))
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("loop-%d-%s", os.Getuid(), hex.EncodeToString(sum[:8])))
	if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		return "", err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm() != 0700 || !ok || owner.Uid != uint32(os.Getuid()) {
		return "", errors.New("本地通信目录的权限或所有者不正确")
	}
	return dir, nil
}

func lockFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("该配置已有后台或界面进程运行")
	}
	return file, nil
}

func unlockFile(file *os.File) { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }

// WithOfflineMutation prevents legacy file-management commands racing either
// a running daemon or a concurrently starting one.
func WithOfflineMutation(stateDir string, work func() error) error {
	dir, err := SocketDirectory(stateDir)
	if err != nil {
		return err
	}
	lock, err := lockFile(filepath.Join(dir, "daemon.lock"))
	if err != nil {
		return errors.New("后台运行期间请使用 policy select 或节点管理界面；旧管理参数仅支持离线修改")
	}
	defer unlockFile(lock)
	return work()
}

func detachProcess() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }

func readPrivateJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}
