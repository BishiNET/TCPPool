package tcppool

import (
	"net"
	"sync"
	"sync/atomic"
)

const (
	MAX_POOL_SIZE = 65536
)

type pool struct {
	current chan net.Conn
	pool    sync.Map
	poolLen atomic.Int32
}

func (p *pool) findOneAndRemove(hj *hijackConn) {
	p.pool.Delete(hj)
	p.poolLen.Add(-1)
}

func (p *pool) Close() {
	p.pool.Range(func(key, _ any) bool {
		if hj, ok := key.(*hijackConn); ok {
			hj.Close()
		}
		return true
	})
}

func (p *pool) Push(c *hijackConn) bool {
	if p.poolLen.Add(1) >= MAX_POOL_SIZE {
		p.poolLen.Add(-1)
		return false
	}
	p.pool.LoadOrStore(c, struct{}{})
	return true
}

func (p *pool) getConnectionFromPool() (c net.Conn, err error) {
	hasEOF := false
	for {
		select {
		case hc := <-p.current:
			hijack := hc.(*hijackConn)
			if !hijack.IsEOF() {
				c = hc
				err = nil
				return
			}
			if !hasEOF {
				hasEOF = true
			}
		default:
			if hasEOF {
				err = ErrNoConnection
			}
			return
		}
	}
}
