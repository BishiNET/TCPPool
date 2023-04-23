package tcppool

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
)

var (
	ErrPoolFull     = fmt.Errorf("too many connections")
	ErrDestNotFound = fmt.Errorf("destination not found")
	ErrNoConnection = fmt.Errorf("no connection")
	ErrConnClosed   = fmt.Errorf("put a closed connection")
)

type ClientPool struct {
	poolSize    int
	poolMap     sync.Map
	dialContext *net.Dialer
	dialMu      sync.RWMutex
	ep          *epollRoutine
	stopped     context.Context
	stop        context.CancelFunc
}

func NewClientPool(maxSize ...int) (*ClientPool, error) {
	SIZE := MAX_POOL_SIZE
	if len(maxSize) > 0 {
		SIZE = maxSize[0]
	}
	ep, err := NewEpollRoutine()
	if err != nil {
		return nil, err
	}
	cp := &ClientPool{
		poolSize:    SIZE,
		dialContext: &net.Dialer{},
		ep:          ep,
	}
	cp.stopped, cp.stop = context.WithCancel(context.Background())
	return cp, nil
}

func (cp *ClientPool) WithDialer(d *net.Dialer) {
	cp.dialMu.Lock()
	cp.dialContext = d
	cp.dialMu.Unlock()
}

func (cp *ClientPool) getPool(key string) *pool {
	var conn *pool

	newConnPool := &pool{
		current: make(chan net.Conn, cp.poolSize),
	}
	if already, ok := cp.poolMap.LoadOrStore(key, newConnPool); ok {
		conn = already.(*pool)
		newConnPool = nil
	} else {
		conn = newConnPool
	}

	return conn
}

func (cp *ClientPool) getDialer() *net.Dialer {
	cp.dialMu.RLock()
	defer cp.dialMu.RUnlock()
	return cp.dialContext
}

func (cp *ClientPool) pushConn(conn *pool, ret net.Conn) (net.Conn, error) {
	// do a copy to avoid some problem
	thePool := conn
	hj, err := newHijackConn(cp.stopped, ret, func(hj *hijackConn) {
		thePool.findOneAndRemove(hj)
	})
	if err != nil {
		return nil, err
	}
	if !conn.Push(hj) {
		ret.Close()
		return nil, ErrPoolFull
	}
	cp.ep.Open(hj)
	return hj, nil
}

func (cp *ClientPool) Dial(network, address string) (c net.Conn, err error) {
	key := strings.ToLower(network) + address
	conn := cp.getPool(key)
	if len(conn.current) > 0 {
		if c, err = conn.getConnectionFromPool(); err != nil {
			return
		}
	}
	ret, err := cp.getDialer().Dial(network, address)
	if err == nil {
		c, err = cp.pushConn(conn, ret)
	}
	return
}

func (cp *ClientPool) DialContext(ctx context.Context, network, address string) (c net.Conn, err error) {
	key := strings.ToLower(network) + address
	conn := cp.getPool(key)
	if len(conn.current) > 0 {
		if c, err = conn.getConnectionFromPool(); err != nil {
			return
		}
	}
	ret, err := cp.getDialer().DialContext(ctx, network, address)
	if err == nil {
		c, err = cp.pushConn(conn, ret)
	}
	return
}

func (cp *ClientPool) Put(c net.Conn) (err error) {
	key := strings.ToLower(c.RemoteAddr().Network()) + c.RemoteAddr().String()
	if connFromPool, ok := cp.poolMap.Load(key); ok {
		conn := connFromPool.(*pool)
		hj, ok := c.(*hijackConn)
		if !ok {
			thePool := conn
			hj, err = newHijackConn(cp.stopped, c, func(hj *hijackConn) {
				thePool.findOneAndRemove(hj)
			})
			if err != nil {
				return
			}
			if !conn.Push(hj) {
				err = ErrPoolFull
				return
			}
			cp.ep.Open(hj)
		} else {
			if hj.IsEOF() {
				return ErrConnClosed
			}
		}
		select {
		case conn.current <- hj:
		default:
			err = ErrPoolFull
		}
	} else {
		err = ErrDestNotFound
	}
	return
}

func (cp *ClientPool) Close() {
	select {
	case <-cp.stopped.Done():
		return
	default:
	}
	cp.stop()
	cp.ep.Shutdown()
	cp.poolMap.Range(func(_, value any) bool {
		conn := value.(*pool)
		conn.Close()
		return true
	})
}
