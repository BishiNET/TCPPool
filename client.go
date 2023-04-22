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
	ep          *epollRoutine
	stopped     context.Context
	stop        context.CancelFunc
}

func (p *pool) find(hj []*hijackConn) []*list.Element {
	allE := []*list.Element{}
	p.poolMu.RLock()
	defer p.poolMu.RUnlock()
	for e := p.pool.Front(); e != nil; e = e.Next() {
		expect := e.Value.(*hijackConn)
		for _, h := range hj {
			if expect == h {
				allE = append(allE, e)
			}
		}
	}
	return allE
}
func (p *pool) poolSweep(hj []*hijackConn) {
	if e := p.find(hj); len(e) > 0 {
		p.poolMu.Lock()
		defer p.poolMu.Unlock()
		for _, eachE := range e {
			p.pool.Remove(eachE)
		}
	}
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
	p.poolMu.Lock()
	defer p.poolMu.Unlock()
	if p.pool.Len() >= MAX_POOL_SIZE {
		return false
	}
	p.pool.PushBack(c)
	return true
}

func (p *pool) getConnectionFromPool() (c net.Conn, err error) {
	toSweep := []*hijackConn{}
	defer func() {
		if len(toSweep) > 0 {
			p.poolSweep(toSweep)
		}
	}()
	for {
		select {
		case hc := <-p.current:
			hijack := hc.(*hijackConn)
			if !hijack.IsEOF() {
				c = net.Conn(hijack)
				err = nil
				return
			}
			toSweep = append(toSweep, hijack)
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

func (cp *ClientPool) getDialer() *net.Dialer {
	cp.dialMu.RLock()
	defer cp.dialMu.RUnlock()
	return cp.dialContext
}

func (cp *ClientPool) pushConn(conn *pool, ret net.Conn) (net.Conn, error) {
	hj, err := newHijackConn(cp.stopped, ret)
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
			hj, err = newHijackConn(cp.stopped, c)
			if err != nil {
				return
			}
			if !conn.Push(hj) {
				return nil, ErrPoolFull
			}
			cp.ep.Open(hj)
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
