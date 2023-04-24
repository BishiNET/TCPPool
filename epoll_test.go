package tcppool

import (
	"context"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type PauseConn struct {
	pauseGroup sync.WaitGroup
	newConn    chan net.Conn
	isPausing  atomic.Bool
	Remote     sync.WaitGroup
	Started    atomic.Bool
}

func NewPauseConn() *PauseConn {
	return &PauseConn{
		newConn: make(chan net.Conn, 1),
	}
}

func (p *PauseConn) Pause() {
	if p.isPausing.CompareAndSwap(false, true) {
		p.pauseGroup.Add(1)
	}
}
func (p *PauseConn) Start() {
	p.Started.Store(true)
	p.Remote.Add(1)
}
func (p *PauseConn) Wake() {
	p.isPausing.Store(false)
	p.pauseGroup.Done()
}

func (p *PauseConn) Wait() {
	p.pauseGroup.Wait()
}

func (p *PauseConn) WaitAndGetConn() net.Conn {
	p.pauseGroup.Wait()
	return p.GetConn()
}

func (p *PauseConn) WaitRemote() {
	p.Remote.Wait()
}
func (p *PauseConn) IsPause() bool {
	return p.isPausing.Load()
}

func (p *PauseConn) NewConn(c net.Conn) {
	p.newConn <- c
}

func (p *PauseConn) GetConn() net.Conn {
	select {
	case c := <-p.newConn:
		return c
	default:
		return nil
	}
}

func newServer(wg *sync.WaitGroup) {
	l, err := net.Listen("tcp", "127.0.0.1:6789")
	if err != nil {
		log.Fatal(err)
	}
	wg.Done()
	for {
		conn, _ := l.Accept()
		wg.Add(1)
		go func(conn net.Conn) {
			defer wg.Done()
			defer conn.Close()
			time.Sleep(time.Second)
			log.Println("Exit")
		}(conn)
	}
}

func newServerPG(wg *sync.WaitGroup, pg *PauseConn) {
	l, err := net.Listen("tcp", "127.0.0.1:6789")
	if err != nil {
		log.Fatal(err)
	}
	wg.Done()
	for {
		conn, _ := l.Accept()
		wg.Add(1)
		go func(conn net.Conn) {
			defer wg.Done()
			defer conn.Close()
			if pg.Started.Load() {
				pg.Pause()
			}
			time.Sleep(2 * time.Second)
			log.Println("Exit")
		}(conn)
	}
}
func newConn(ep *epollRoutine, t *testing.T) {
	c, err := net.Dial("tcp", "127.0.0.1:6789")
	if err != nil {
		t.Errorf(err.Error())
		return
	}
	hj, err := newHijackConn(context.TODO(), c, func(hj *hijackConn) {
		t.Log("Disconnected: ", hj)
	})
	if err != nil {
		t.Errorf(err.Error())
		return
	}
	err = ep.Open(hj)
	if err != nil {
		t.Errorf(err.Error())
		return
	}
}

func TestEpoll(t *testing.T) {
	var wg sync.WaitGroup
	ep, _ := NewEpollRoutine()
	defer ep.Shutdown()
	wg.Add(1)
	go newServer(&wg)
	wg.Wait()
	for i := 0; i < 10; i++ {
		newConn(ep, t)
	}
	wg.Wait()
	time.Sleep(10 * time.Second)
}

func TestPause(t *testing.T) {
	var wg sync.WaitGroup
	pg := NewPauseConn()
	wg.Add(1)
	go newServerPG(&wg, pg)
	wg.Wait()
	sig := make(chan struct{}, 1)
	go func() {
		pg.Start()
		defer pg.Remote.Done()
		c, err := net.Dial("tcp", "127.0.0.1:6789")
		if err != nil {
			t.Errorf(err.Error())
			return
		}
		var b [1]byte
		_, err = c.Read(b[:])
		if err != nil {
			if pg.IsPause() {
				t.Log("Pause")
				t1 := time.Now()
				sig <- struct{}{}
				if c = pg.WaitAndGetConn(); c != nil {
					t.Logf("Wake up, elapsed time: %d", time.Since(t1).Nanoseconds())
					t.Log(c.LocalAddr().String() + " -> " + c.RemoteAddr().String())
				}
			}
		}
	}()
	<-sig
	t1 := time.Now()
	c, err := net.Dial("tcp", "127.0.0.1:6789")
	if err != nil {
		t.Errorf(err.Error())
		return
	}
	t.Log(c.LocalAddr().String() + " -> " + c.RemoteAddr().String())
	pg.NewConn(c)
	t.Log(time.Since(t1).Nanoseconds())
	pg.Wake()
	pg.Remote.Wait()
	wg.Wait()
	time.Sleep(10 * time.Second)
}
