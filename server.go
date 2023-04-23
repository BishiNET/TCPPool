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

func (sp *ServerPool) Get(connID uint16) (net.Conn, error) {
	if c, ok := sp.connMap.Load(connID); ok {
		if hj, ok := c.(*hijackConn); ok {
			return hj, nil
		}
	}
	return nil, ErrDestNotFound
}

func (sp *ServerPool) Put(c net.Conn, connID uint16) error {
	var err error
	hj, ok := c.(*hijackConn)
	if !ok {
		id := connID
		hj, err = newHijackConn(sp.stopped, c, func(_ *hijackConn) {
			sp.connMap.Delete(id)
		})
		if err != nil {
			return err
		}
		sp.ep.Open(hj)
	}
	sp.connMap.Store(connID, hj)
	return nil
}
