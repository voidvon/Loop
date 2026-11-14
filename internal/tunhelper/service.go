package tunhelper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// Bump when the installed privileged implementation must be upgraded. Ordinary
// UI releases keep this version, so they don't require another installation.
const ServiceProtocol = 2
const ServiceLabel = "local.loop.tun"
const ServiceSocket = "/var/run/local.loop.tun.sock"
const ServiceDirectory = "/Library/Application Support/LoopTUN"
const ServiceExecutable = "/Library/PrivilegedHelperTools/local.loop.tun"
const ServicePlist = "/Library/LaunchDaemons/local.loop.tun.plist"

type serviceBootstrap struct {
	Action string `json:"action"`
	Token  string `json:"token,omitempty"`
}

type ServiceStatus struct {
	Protocol int    `json:"protocol"`
	UID      int    `json:"uid"`
	CoreSHA  string `json:"core_sha"`
	Active   bool   `json:"active"`
	Error    string `json:"error,omitempty"`
}

type serviceInstallation struct {
	UID     int    `json:"uid"`
	CoreSHA string `json:"core_sha"`
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 512<<20 {
		return "", errors.New("内核必须是小于 512 MiB 的普通文件")
	}
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, 512<<20)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func serviceStatusAt(ctx context.Context, socket string, verifyPeer func(net.Conn) error) (ServiceStatus, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
	if err != nil {
		return ServiceStatus{}, err
	}
	defer conn.Close()
	if err := verifyPeer(conn); err != nil {
		return ServiceStatus{}, err
	}
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(conn).Encode(serviceBootstrap{Action: "status"}); err != nil {
		return ServiceStatus{}, err
	}
	var status ServiceStatus
	err = json.NewDecoder(io.LimitReader(conn, 4096)).Decode(&status)
	if err == nil && status.Error != "" {
		err = errors.New(status.Error)
	}
	return status, err
}

// The service may outlive Loop, but each core/DNS session belongs to exactly
// one authenticated connection. Disconnecting releases it before another starts.
func serveService(ctx context.Context, listener net.Listener, installation serviceInstallation, core string,
	peerUID func(net.Conn) (int, error), routeCheck func() error,
	setupDNS func(map[string]any) (func(), <-chan struct{}, error)) error {
	var active sync.Mutex
	var mu sync.Mutex
	connections := map[net.Conn]bool{}
	var workers sync.WaitGroup
	stop := context.AfterFunc(ctx, func() {
		listener.Close()
		mu.Lock()
		for conn := range connections {
			conn.Close()
		}
		mu.Unlock()
	})
	defer stop()
	defer func() {
		listener.Close()
		mu.Lock()
		for conn := range connections {
			conn.Close()
		}
		mu.Unlock()
		workers.Wait()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		mu.Lock()
		connections[conn] = true
		mu.Unlock()
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer conn.Close()
			defer func() { mu.Lock(); delete(connections, conn); mu.Unlock() }()
			uid, err := peerUID(conn)
			if err != nil || (uid != installation.UID && uid != 0) {
				return
			}
			conn.SetDeadline(time.Now().Add(3 * time.Second))
			var request serviceBootstrap
			if err := json.NewDecoder(io.LimitReader(conn, 4096)).Decode(&request); err != nil {
				return
			}
			if request.Action == "status" {
				running := !active.TryLock()
				if !running {
					active.Unlock()
				}
				_ = json.NewEncoder(conn).Encode(ServiceStatus{Protocol: ServiceProtocol, UID: installation.UID, CoreSHA: installation.CoreSHA, Active: running})
				return
			}
			if request.Action != "connect" || uid != installation.UID || len(request.Token) != 64 {
				return
			}
			if _, err := hex.DecodeString(request.Token); err != nil {
				return
			}
			reject := func(err error) { _ = json.NewEncoder(conn).Encode(Message{Token: request.Token, Error: err.Error()}) }
			if !active.TryLock() {
				reject(errors.New("TUN 服务已有连接，请先停止原连接"))
				return
			}
			var release sync.Once
			unlock := func() { release.Do(active.Unlock) }
			defer unlock()
			if err := routeCheck(); err != nil {
				reject(err)
				return
			}
			conn.SetDeadline(time.Time{})
			_ = serveSession(ctx, releaseConnection{Conn: conn, release: unlock}, request.Token, setupDNS, core)
		}()
	}
}

type releaseConnection struct {
	net.Conn
	release func()
}

func (c releaseConnection) Close() error { c.release(); return c.Conn.Close() }

// relayService drains the privileged side even after the local client closes.
// Its EOF therefore acknowledges that both the core and DNS have been cleaned.
func relayService(ctx context.Context, local, remote *net.UnixConn) error {
	defer local.Close()
	defer remote.Close()
	stop := context.AfterFunc(ctx, func() { local.Close(); remote.Close() })
	defer stop()
	writeDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(remote, local)
		_ = remote.CloseWrite()
		_ = remote.SetReadDeadline(time.Now().Add(10 * time.Second))
		close(writeDone)
	}()
	_, err := io.Copy(discardClosedWriter{local}, remote)
	local.Close()
	<-writeDone
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("等待 TUN 服务清理失败：%w", err)
	}
	return nil
}

type discardClosedWriter struct{ io.Writer }

func (w discardClosedWriter) Write(p []byte) (int, error) {
	_, _ = w.Writer.Write(p)
	return len(p), nil
}
