package tcppool

import (
	"context"
	"fmt"
	"sync"
	"syscall"
)

const (
	EPOLLET = 0x80000000
)

var (
	ErrEpollCreate = fmt.Errorf("epoll created error")
	ErrEpoll       = fmt.Errorf("epoll error")
	ErrConnExists  = fmt.Errorf("connection existed")
)

type epollRoutine struct {
	epollfd int
	stopped context.Context
	stop    context.CancelFunc
	fdMap   sync.Map
}

func NewEpollRoutine() (*epollRoutine, error) {
	epfd, errno := syscall.EpollCreate1(syscall.EPOLL_CLOEXEC)
	if errno != 0 {
		return nil, ErrEpollCreate
	}
	e := &epollRoutine{
		epollfd: epfd,
	}
	go e.Run()
	return e, nil
}

func (e *epollRoutine) Open(hj *hijackConn) error {
	fd := hj.FD()
	if _, ok := e.fdMap.LoadOrStore(fd, hj); ok {
		return ErrConnExists
	}
	var ev syscall.EpollEvent
	ev.Events = syscall.EPOLLRDHUP | EPOLLET
	ev.Fd = fd
	if err := syscall.EpollCtl(e.epollfd, syscall.EPOLL_CTL_ADD, fd, &ev); err != nil {
		return nil, ErrEpoll
	}
	return nil
}
func (e *epollRoutine) Close(hj *hijackConn) {
	fd := hj.FD()
	syscall.EpollCtl(e.epollfd, syscall.EPOLL_CTL_DEL, fd, nil)
	e.fdMap.Delete(fd)
}

func (e *epollRoutine) Shutdown() {
	e.stop()
	syscall.Close(e.epollfd)
}

func (e *epollRoutine) Run() {
	var events [128]syscall.EpollEvent
retry:
	for {
		n, err := syscall.EpollWait(e.epollfd, events[:], 128, -1)
		if err != nil {
			if err == syscall.EINTR {
				continue retry
			}
			select {
			case <-e.stopped.Done():
				return
			default:
				continue retry
			}
		}
		for i := 0; i < n; i++ {
			if events[i].Events&(syscall.EPOLLERR|syscall.EPOLLRDHUP|syscall.EPOLLHUP) != 0 {
				if c, ok := e.fdMap.Load(events[i].Fd); ok {
					hj := c.(*hijackConn)
					e.Close(hj)
					// unblock all Read/Write call now and report EOF
					hj.Close()
				}
			}
		}
	}
}
