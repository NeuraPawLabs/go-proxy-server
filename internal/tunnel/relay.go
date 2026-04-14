package tunnel

import (
	"net"
	"sync"
	"time"

	"github.com/apeming/go-proxy-server/internal/constants"
)

const defaultTunnelIdleTimeout = 10 * time.Minute

type relayDirection int

const (
	relayDirectionLeftToRight relayDirection = iota
	relayDirectionRightToLeft
)

type relayObserver struct {
	OnBytes func(direction relayDirection, n int)
	OnClose func()
}

var relayBufferPool = sync.Pool{
	New: func() any {
		return make([]byte, constants.BufferSizeLarge)
	},
}

func relayConnections(left, right net.Conn) {
	relayConnectionsWithObserver(left, right, defaultTunnelIdleTimeout, relayObserver{})
}

func relayConnectionsWithIdleTimeout(left, right net.Conn, idleTimeout time.Duration) {
	relayConnectionsWithObserver(left, right, idleTimeout, relayObserver{})
}

func relayConnectionsWithObserver(left, right net.Conn, idleTimeout time.Duration, observer relayObserver) {
	defer left.Close()
	defer right.Close()
	if observer.OnClose != nil {
		defer observer.OnClose()
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		copyStream(left, right, idleTimeout, relayDirectionRightToLeft, observer)
		closeWrite(left)
		closeRead(right)
	}()

	go func() {
		defer wg.Done()
		copyStream(right, left, idleTimeout, relayDirectionLeftToRight, observer)
		closeWrite(right)
		closeRead(left)
	}()

	wg.Wait()
}

func copyStream(dst net.Conn, src net.Conn, idleTimeout time.Duration, direction relayDirection, observer relayObserver) {
	buf := relayBufferPool.Get().([]byte)
	defer relayBufferPool.Put(buf)

	for {
		if idleTimeout > 0 {
			_ = src.SetReadDeadline(time.Now().Add(idleTimeout))
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			if idleTimeout > 0 {
				_ = dst.SetWriteDeadline(time.Now().Add(idleTimeout))
			}
			if err := writeAll(dst, buf[:n]); err != nil {
				return
			}
			if observer.OnBytes != nil {
				observer.OnBytes(direction, n)
			}
		}
		if readErr != nil {
			return
		}
	}
}

func writeAll(dst net.Conn, data []byte) error {
	for len(data) > 0 {
		n, err := dst.Write(data)
		if err != nil {
			return err
		}
		data = data[n:]
	}
	return nil
}

func closeWrite(conn net.Conn) {
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.CloseWrite()
	}
}

func closeRead(conn net.Conn) {
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.CloseRead()
	}
}
