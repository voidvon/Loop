package tunhelper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func socketUID(conn net.Conn) (int, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return -1, errors.New("需要 Unix Socket")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return -1, err
	}
	var uid int
	var credentialErr error
	err = raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		credentialErr = err
		if err == nil {
			uid = int(cred.Uid)
		}
	})
	return uid, errors.Join(err, credentialErr)
}

func verifyRootPeer(conn net.Conn) error {
	uid, err := socketUID(conn)
	if err != nil {
		return err
	}
	if uid != 0 {
		return errors.New("TUN 服务身份校验失败")
	}
	return nil
}

func StatusService(ctx context.Context) (ServiceStatus, error) {
	return serviceStatusAt(ctx, ServiceSocket, verifyRootPeer)
}

// DialService authenticates the privileged peer and opens an owned session.
// The caller must half-close writes, then read EOF before releasing the session.
func DialService(ctx context.Context, token string) (*net.UnixConn, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", ServiceSocket)
	if err != nil {
		return nil, err
	}
	if err := verifyRootPeer(conn); err != nil {
		conn.Close()
		return nil, err
	}
	conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(conn).Encode(serviceBootstrap{Action: "connect", Token: token}); err != nil {
		conn.Close()
		return nil, err
	}
	conn.SetWriteDeadline(time.Time{})
	return conn.(*net.UnixConn), nil
}

func EnsureService(ctx context.Context, helper, core string) error {
	coreSHA, err := hashFile(core)
	if err != nil {
		return err
	}
	status, statusErr := StatusService(ctx)
	if statusErr == nil {
		if status.UID != os.Getuid() {
			return errors.New("TUN 服务已授权给其他用户")
		}
		if status.Protocol == ServiceProtocol && status.CoreSHA == coreSHA {
			return nil
		}
		if status.Active {
			return errors.New("请先停止 TUN 连接，再更新授权服务")
		}
	} else if errors.Is(statusErr, os.ErrPermission) {
		return errors.New("当前用户无权访问已安装的 TUN 服务")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := authorize(ctx, helper, "Loop 需要安装或更新 TUN 授权服务。安装后，重开应用和修改配置无需再次输入密码。", "-install-service-root", "-core", core, "-uid", strconv.Itoa(os.Getuid())); err != nil {
		return err
	}
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, err = StatusService(ctx)
		if err == nil && status.Protocol == ServiceProtocol && status.UID == os.Getuid() && status.CoreSHA == coreSHA {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("TUN 授权服务未就绪，请检查系统后台项目是否允许 Loop TUN")
		case <-ticker.C:
		}
	}
}

func authorize(ctx context.Context, helper, prompt string, arguments ...string) error {
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	command := quote(helper)
	for _, argument := range arguments {
		command += " " + quote(argument)
	}
	script := "with timeout of 2147483647 seconds\ndo shell script " + strconv.Quote(command) + " with administrator privileges with prompt " + strconv.Quote(prompt) + "\nend timeout"
	output, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("TUN 服务安装/授权未完成：%w\n%s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func RunServiceClient(ctx context.Context, helper, core, socket, token string) error {
	// A cancelled start removes the private listening socket. Cancel a pending
	// installation dialog as well; a late installation cannot start a tunnel.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	installationDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-installationDone:
				return
			case <-ticker.C:
				if _, err := os.Lstat(socket); err != nil {
					select {
					case <-installationDone:
						return
					default:
						cancel()
						return
					}
				}
			}
		}
	}()
	if err := EnsureService(ctx, helper, core); err != nil {
		return err
	}
	close(installationDone)
	local, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
	if err != nil {
		return err
	}
	defer local.Close()
	remote, err := (&net.Dialer{}).DialContext(ctx, "unix", ServiceSocket)
	if err != nil {
		return err
	}
	defer remote.Close()
	if err := verifyRootPeer(remote); err != nil {
		return err
	}
	if err := json.NewEncoder(remote).Encode(serviceBootstrap{Action: "connect", Token: token}); err != nil {
		return err
	}
	return relayService(ctx, local.(*net.UnixConn), remote.(*net.UnixConn))
}

func verifyRootPath(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || info.Mode()&0022 != 0 || info.Mode()&os.ModeSymlink != 0 || info.IsDir() != directory || (!directory && !info.Mode().IsRegular()) {
		return fmt.Errorf("TUN 服务路径权限异常：%s", path)
	}
	return nil
}

func rootWrite(path string, reader io.Reader, mode os.FileMode) error {
	if err := verifyRootPath(filepath.Dir(path), true); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".loop-tun-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	written, err := io.Copy(f, io.LimitReader(reader, (512<<20)+1))
	if err != nil {
		return err
	}
	if written > 512<<20 {
		return errors.New("安装文件过大")
	}
	if err := f.Chmod(mode); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func installCopy(source, destination string) error {
	f, err := os.Open(source)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return errors.New("安装源必须是可执行普通文件")
	}
	return rootWrite(destination, f, 0700)
}

func stopInstalledService() error {
	err := exec.Command("/bin/launchctl", "bootout", "system/"+ServiceLabel).Run()
	if err != nil && exec.Command("/bin/launchctl", "print", "system/"+ServiceLabel).Run() == nil {
		return fmt.Errorf("无法停止原 TUN 服务：%w", err)
	}
	return nil
}

func InstallService(core string, uid int) error {
	if os.Geteuid() != 0 || uid <= 0 || !filepath.IsAbs(core) {
		return errors.New("安装 TUN 服务需要管理员授权与普通用户 UID")
	}
	if !serviceSocketActivationAvailable {
		return errors.New("TUN 按需服务需要启用 CGO 构建辅助程序")
	}
	for _, parent := range []string{"/Library", "/Library/Application Support"} {
		if err := verifyRootPath(parent, true); err != nil {
			return err
		}
	}
	for _, path := range []string{"/Library/PrivilegedHelperTools", "/Library/LaunchDaemons", ServiceDirectory} {
		if err := os.MkdirAll(path, 0755); err != nil {
			return err
		}
		if err := verifyRootPath(path, true); err != nil {
			return err
		}
	}
	if err := os.Chmod(ServiceDirectory, 0700); err != nil {
		return err
	}
	if status, err := StatusService(context.Background()); err == nil && status.Active {
		return errors.New("请先停止 TUN，再安装授权服务")
	}
	helper, err := os.Executable()
	if err != nil {
		return err
	}
	// Stage complete, private copies before stopping an existing installation.
	staging, err := os.MkdirTemp(ServiceDirectory, "install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := installCopy(helper, filepath.Join(staging, "loop-tun")); err != nil {
		return err
	}
	if err := installCopy(core, filepath.Join(staging, "sing-box")); err != nil {
		return err
	}
	coreSHA, err := hashFile(filepath.Join(staging, "sing-box"))
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(serviceInstallation{UID: uid, CoreSHA: coreSHA})
	if err != nil {
		return err
	}
	plist := serviceLaunchPlist(uid)
	if err := stopInstalledService(); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(staging, "loop-tun"), ServiceExecutable); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(staging, "sing-box"), filepath.Join(ServiceDirectory, "sing-box")); err != nil {
		return err
	}
	if err := rootWrite(filepath.Join(ServiceDirectory, "installation.json"), strings.NewReader(string(metadata)), 0600); err != nil {
		return err
	}
	if err := rootWrite(ServicePlist, strings.NewReader(plist), 0644); err != nil {
		return err
	}
	if output, err := exec.Command("/bin/launchctl", "bootstrap", "system", ServicePlist).CombinedOutput(); err != nil {
		return fmt.Errorf("注册 TUN 服务失败：%w\n%s", err, output)
	}
	return nil
}

func serviceLaunchPlist(uid int) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>local.loop.tun</string>
<key>ProgramArguments</key><array><string>/Library/PrivilegedHelperTools/local.loop.tun</string><string>-service</string></array>
<key>Sockets</key><dict><key>Control</key><dict>
<key>SockPathName</key><string>/var/run/local.loop.tun.sock</string>
<key>SockPathOwner</key><integer>%d</integer>
<key>SockPathMode</key><integer>384</integer>
<key>SockType</key><string>stream</string>
</dict></dict>
<key>ThrottleInterval</key><integer>1</integer>
<key>ExitTimeOut</key><integer>12</integer>
<key>Umask</key><integer>63</integer>
<key>StandardErrorPath</key><string>/Library/Application Support/LoopTUN/service.log</string>
</dict></plist>
`, uid)
}

func RunInstalledService(ctx context.Context) error {
	if os.Geteuid() != 0 {
		return errors.New("TUN 服务只能由系统启动")
	}
	for _, parent := range []string{"/Library", "/Library/Application Support", "/Library/PrivilegedHelperTools"} {
		if err := verifyRootPath(parent, true); err != nil {
			return err
		}
	}
	for _, path := range []string{ServiceExecutable, filepath.Join(ServiceDirectory, "sing-box"), filepath.Join(ServiceDirectory, "installation.json")} {
		if err := verifyRootPath(path, false); err != nil {
			return err
		}
	}
	if err := verifyRootPath(ServiceDirectory, true); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(ServiceDirectory, "installation.json"))
	if err != nil {
		return err
	}
	var installation serviceInstallation
	if err := json.Unmarshal(data, &installation); err != nil {
		return err
	}
	if installation.UID <= 0 {
		return errors.New("无效的 TUN 服务用户")
	}
	listener, err := activatedServiceListener()
	if err != nil {
		return err
	}
	defer listener.Close()
	return serveService(ctx, listener, installation, filepath.Join(ServiceDirectory, "sing-box"), socketUID, CheckRouteConflict, startDNS, 5*time.Second)
}

func UninstallService(ctx context.Context, helper string) error {
	return authorize(ctx, helper, "Loop 将移除 TUN 授权服务。之后再次启用 TUN 时需要重新安装。", "-uninstall-service-root")
}

func UninstallServiceRoot() error {
	if os.Geteuid() != 0 {
		return errors.New("移除 TUN 服务需要管理员授权")
	}
	if err := stopInstalledService(); err != nil {
		return err
	}
	for _, path := range []string{ServicePlist, ServiceExecutable, ServiceSocket} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := verifyRootPath(ServiceDirectory, true); err == nil {
		return os.RemoveAll(ServiceDirectory)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}
