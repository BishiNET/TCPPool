package tcppool

import (
	"context"
	"fmt"
	"net"
	"syscall"
	"time"
)

var (
	ErrUnknownConn = fmt.Errorf("unknown net.conn")
)

type hijackConn struct {
	conn  net.Conn
	fd    int
	eof   context.Context
	doEOF context.CancelFunc
}

// use syscall wouldn't dup the file descriptor
func getFD(c net.Conn) int {
	var fd int
	var fs syscall.RawConn
	switch t := c.(type) {
	case *net.TCPConn:
		fs, _ = t.SyscallConn()
	case *net.UDPConn:
		fs, _ = t.SyscallConn()
	case *net.IPConn:
		fs, _ = t.SyscallConn()
	case *net.UnixConn:
	default:
	}
	if fs != nil {
		fs.Control(func(_fd uintptr) {
			fd = int(_fd)
		})
	}
	return fd

}

func newHijackConn(ctx context.Context, c net.Conn) (*hijackConn, error) {
	fd := getFD(c)
	if fd == 0 {
		return nil, ErrUnknownConn
	}
	hj := &hijackConn{
		conn: c,
		fd:   fd,
	}
	hj.eof, hj.doEOF = context.WithCancel(ctx)
	return hj, nil
}

func (hj *hijackConn) IsEOF() bool {
	select {
	case <-hj.eof.Done():
		return true
	default:
		return false
	}
}
func (hj *hijackConn) FD() int {
	return hj.fd
}

// Read reads data from the connection.
// Read can be made to time out and return an error after a fixed
// time limit; see SetDeadline and SetReadDeadline.
func (hj *hijackConn) Read(b []byte) (n int, err error) {
	n, err = hj.conn.Read(b)
	return
}

// Write writes data to the connection.
// Write can be made to time out and return an error after a fixed
// time limit; see SetDeadline and SetWriteDeadline.
func (hj *hijackConn) Write(b []byte) (n int, err error) {
	n, err = hj.conn.Write(b)
	return
}

// Close closes the connection.
// Any blocked Read or Write operations will be unblocked and return errors.
func (hj *hijackConn) Close() error {
	hj.doEOF()
	return hj.conn.Close()
}

// LocalAddr returns the local network address, if known.
func (hj *hijackConn) LocalAddr() net.Addr {
	return hj.conn.LocalAddr()
}

// RemoteAddr returns the remote network address, if known.
func (hj *hijackConn) RemoteAddr() net.Addr {
	return hj.conn.RemoteAddr()
}

// SetDeadline sets the read and write deadlines associated
// with the connection. It is equivalent to calling both
// SetReadDeadline and SetWriteDeadline.
//
// A deadline is an absolute time after which I/O operations
// fail instead of blocking. The deadline applies to all future
// and pending I/O, not just the immediately following call to
// Read or Write. After a deadline has been exceeded, the
// connection can be refreshed by setting a deadline in the future.
//
// If the deadline is exceeded a call to Read or Write or to other
// I/O methods will return an error that wraps os.ErrDeadlineExceeded.
// This can be tested using errors.Is(err, os.ErrDeadlineExceeded).
// The error's Timeout method will return true, but note that there
// are other possible errors for which the Timeout method will
// return true even if the deadline has not been exceeded.
//
// An idle timeout can be implemented by repeatedly extending
// the deadline after successful Read or Write calls.
//
// A zero value for t means I/O operations will not time out.
func (hj *hijackConn) SetDeadline(t time.Time) error {
	return hj.conn.SetDeadline(t)
}

// SetReadDeadline sets the deadline for future Read calls
// and any currently-blocked Read call.
// A zero value for t means Read will not time out.
func (hj *hijackConn) SetReadDeadline(t time.Time) error {
	return hj.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the deadline for future Write calls
// and any currently-blocked Write call.
// Even if write times out, it may return n > 0, indicating that
// some of the data was successfully written.
// A zero value for t means Write will not time out.
func (hj *hijackConn) SetWriteDeadline(t time.Time) error {
	return hj.conn.SetWriteDeadline(t)
}
