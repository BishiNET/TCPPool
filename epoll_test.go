package tcppool

import (
	"context"
	"log"
	"net"
	"sync"
	"testing"
	"time"
)

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
