package tcppool

import (
	"container/list"
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
)

const (
	MAX_POOL_SIZE = 65536
)

type pool struct {
	current chan net.Conn
	pool    *list.List
	poolMu  sync.RWMutex
}

type ClientPool struct {
	poolSize    int
	poolMap     sync.Map
	dialContext *net.Dialer
	dialMu      sync.RWMutex
}

func (p *pool) find(hj *hijackConn) *list.Element {
	p.poolMu.RLock()
	defer p.poolMu.RUnlock()
	for e := p.pool.Front(); e != nil; e = e.Next() {
		expect := e.Value.(*hijackConn)
		if expect == hj {
			return e
		}
	}
	return nil
}
func (p *pool) poolSweep(hj *hijackConn) {
	if e := p.find(hj); e != nil {
		p.poolMu.Lock()
		p.pool.Remove(e)
		p.poolMu.Unlock()
	}
	hj.Close()
}

func (p *pool) Close() {
	p.poolMu.Lock()
	defer p.poolMu.Unlock()
	for e := p.pool.Front(); e != nil; e = e.Next() {
		hj := e.Value.(*hijackConn)
		hj.Close()
	}
}

func (p *pool) Push(c *hijackConn) bool {
	select {
	case p.current <- c:
		p.poolMu.Lock()
		p.pool.PushBack(c)
		p.poolMu.Unlock()
	default:
		return false
	}
	return true
}

func (p *pool) getConnectionFromPool() (c net.Conn, err error) {
	for {
		select {
		case hc := <-p.current:
			hijack := hc.(*hijackConn)
			ehj := hijack.Error()
			if ehj == nil {
				c = net.Conn(hijack)
				err = nil
				return
			}
			p.poolSweep(hijack)
		default:
			err = ErrNoConnection
			return
		}
	}
}

func NewClientPool(maxSize ...int) (*ClientPool, error) {
	SIZE := MAX_POOL_SIZE
	if len(maxSize) > 0 {
		SIZE = maxSize[0]
	}
	return &ClientPool{
		poolSize:    SIZE,
		dialContext: &net.Dialer{},
	}, nil
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
		pool:    list.New(),
	}
	if already, ok := cp.poolMap.LoadOrStore(key, newConnPool); ok {
		conn = already.(*pool)
		newConnPool = nil
	} else {
		conn = newConnPool
	}

	return conn
}

func (cp *ClientPool) Dial(network, address string) (c net.Conn, err error) {
	key := strings.ToLower(network) + address
	conn := cp.getPool(key)
	if len(conn.current) > 0 {
		if c, err = conn.getConnectionFromPool(); err != nil {
			return
		}
	}

	cp.dialMu.RLock()
	defer cp.dialMu.RUnlock()
	ret, err := cp.dialContext.Dial(network, address)

	if err != nil {
		hj := newHijackConn(ret)
		if !conn.Push(hj) {
			ret.Close()
			return nil, ErrPoolFull
		}
		c = net.Conn(hj)
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

	cp.dialMu.RLock()
	defer cp.dialMu.RUnlock()
	ret, err := cp.dialContext.DialContext(ctx, network, address)

	if err != nil {
		hj := newHijackConn(ret)
		if !conn.Push(hj) {
			ret.Close()
			return nil, ErrPoolFull
		}
		c = net.Conn(hj)
	}
	return
}

func (cp *ClientPool) Put(c net.Conn) (err error) {
	key := strings.ToLower(c.RemoteAddr().Network()) + c.RemoteAddr().String()
	if connFromPool, ok := cp.poolMap.Load(key); ok {
		conn := connFromPool.(*pool)
		hj, ok := c.(*hijackConn)
		if !ok {
			hj = newHijackConn(c)
		}
		if !conn.Push(hj) {
			err = ErrPoolFull
		}
	} else {
		err = ErrDestNotFound
	}
	return
}

func (cp *ClientPool) Close() {
	cp.poolMap.Range(func(_, value any) bool {
		conn := value.(*pool)
		conn.Close()
		return true
	})
}
