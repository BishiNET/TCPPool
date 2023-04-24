package tcppool

import (
	"context"
	"net"
	"sync"
)

// Does server really need the pool ?
type ServerPool struct {
	connMap sync.Map
	ep      *epollRoutine
	stopped context.Context
	stop    context.CancelFunc
}

type ContextConn struct {
	c     net.Conn
	value any
}

func (c *ContextConn) Conn() net.Conn {
	return c.c
}
func (c *ContextConn) Value() any {
	return c.value
}

func NewServerPool() (*ServerPool, error) {
	ep, err := NewEpollRoutine()
	if err != nil {
		return nil, err
	}
	cp := &ServerPool{
		ep: ep,
	}
	cp.stopped, cp.stop = context.WithCancel(context.Background())
	return cp, nil
}

func (sp *ServerPool) Close() {
	select {
	case <-sp.stopped.Done():
		return
	default:
	}
	sp.stop()
	sp.connMap.Range(func(key, _ any) bool {
		if hj, ok := key.(*hijackConn); ok {
			hj.Close()
		}
		return true
	})
}

func (sp *ServerPool) Get(connID uint32) (*ContextConn, error) {
	if c, ok := sp.connMap.Load(connID); ok {
		if hj, ok := c.(*ContextConn); ok {
			return hj, nil
		}
	}
	return nil, ErrDestNotFound
}

func (sp *ServerPool) Register(c net.Conn, connID uint32, on func(), userctx ...any) net.Conn {
	hj, ok := c.(*hijackConn)
	if ok {
		return hj
	}
	id := connID
	doEOF := on
	hj, err := newHijackConn(sp.stopped, c, func(_ *hijackConn) {
		sp.ep.Close(hj)
		sp.connMap.Delete(id)
		doEOF()
	})

	if err != nil {
		return err
	}
	sp.ep.Open(hj)
	if len(userctx) > 0 {
		ctx.value = userctx[0]
	}

	ctx := &ContextConn{
		c: c,
	}
	if len(userctx) > 0 {
		ctx.value = userctx[0]
	}

	sp.connMap.Store(connID, ctx)

}

func (sp *ServerPool) Put(c net.Conn, connID uint32) error {
	var err error
	hj, ok := c.(*hijackConn)
	if !ok {
		return ErrUnknownConn
	}
	if sp.connMap.Load(connID)
	return nil
}

func (sp *ServerPool) Remove(connID uint32) {
	if c, ok := sp.connMap.LoadAndDelete(connID); ok {
		if hj, ok := c.(net.Conn); ok {
			hj.Close()
		}
	}
}
