package tcppool

import (
	"net"
	"sync"
	"time"
)

type hijackConn struct {
	conn  net.Conn
	err   error
	errMu sync.RWMutex
}

func newHijackConn(c net.Conn) *hijackConn {
	hj := &hijackConn{
		conn: c,
	}
	return hj
}

func (hj *hijackConn) Error() error {
	// fast-path , don't use read lock.
	// In the most common case, reading is safe.
	if hj.err != nil {
		return hj.err
	}
	hj.errMu.RLock()
	err := hj.err
	hj.errMu.RUnlock()
	return err
}

// Read reads data from the connection.
// Read can be made to time out and return an error after a fixed
// time limit; see SetDeadline and SetReadDeadline.
func (hj *hijackConn) Read(b []byte) (n int, err error) {
	if n, err = hj.conn.Read(b); err != nil {
		if err = hj.Error(); err != nil {
			return
		}
		hj.errMu.Lock()
		hj.err = err
		hj.errMu.Unlock()
	}
	return
}

// Write writes data to the connection.
// Write can be made to time out and return an error after a fixed
// time limit; see SetDeadline and SetWriteDeadline.
func (hj *hijackConn) Write(b []byte) (n int, err error) {
	if n, err = hj.conn.Write(b); err != nil {
		if err = hj.Error(); err != nil {
			return
		}
		hj.errMu.Lock()
		hj.err = err
		hj.errMu.Unlock()
	}
	return
}

// Close closes the connection.
// Any blocked Read or Write operations will be unblocked and return errors.
func (hj *hijackConn) Close() error {
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
