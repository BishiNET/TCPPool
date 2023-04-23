package tcppool

import (
	"context"
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

const (
	EPOLLET = 0x80000000
)

var (
	ErrEpollCreate = fmt.Errorf("epoll created error")
	ErrEpoll       = fmt.Errorf("epoll error")
	ErrConnExists  = fmt.Errorf("connection existed")
)

type EpollEvent struct {
	Events uint32
	Data   [8]byte // unaligned uintptr
}

func EpollCtl(epfd, op, fd int, event *EpollEvent) (errno syscall.Errno) {
	_, _, e := syscall.Syscall6(
		syscall.SYS_EPOLL_CTL,
		uintptr(epfd),
		uintptr(op),
		uintptr(fd),
		uintptr(unsafe.Pointer(event)),
		0, 0)
	return e
}

func EpollWait(epfd int, events []EpollEvent, maxev, waitms int) (int, syscall.Errno) {
	var ev unsafe.Pointer
	var _zero uintptr
	if len(events) > 0 {
		ev = unsafe.Pointer(&events[0])
	} else {
		ev = unsafe.Pointer(&_zero)
	}
	r1, _, e := syscall.Syscall6(
		syscall.SYS_EPOLL_PWAIT,
		uintptr(epfd),
		uintptr(ev),
		uintptr(maxev),
		uintptr(waitms),
		0, 0)
	return int(r1), e
}

type epollRoutine struct {
	epollfd int
	stopped context.Context
	stop    context.CancelFunc
	fdMap   sync.Map
}

func NewEpollRoutine() (*epollRoutine, error) {
	epfd, errno := syscall.EpollCreate1(syscall.EPOLL_CLOEXEC)
	if errno != nil {
		return nil, ErrEpollCreate
	}
	e := &epollRoutine{
		epollfd: epfd,
	}
	e.stopped, e.stop = context.WithCancel(context.Background())
	go e.Run()
	return e, nil
}

func (e *epollRoutine) Open(hj *hijackConn) error {
	fd := hj.FD()
	if _, ok := e.fdMap.LoadOrStore(fd, hj); ok {
		return ErrConnExists
	}
	var ev EpollEvent
	ev.Events = syscall.EPOLLRDHUP | EPOLLET
	*(**hijackConn)(unsafe.Pointer(&ev.Data)) = hj
	if err := EpollCtl(e.epollfd, syscall.EPOLL_CTL_ADD, fd, &ev); err != 0 {
		return ErrEpoll
	}
	return nil
}
func (e *epollRoutine) Close(hj *hijackConn) {
	fd := hj.FD()
	EpollCtl(e.epollfd, syscall.EPOLL_CTL_DEL, fd, nil)
	e.fdMap.Delete(fd)
}

func (e *epollRoutine) Shutdown() {
	e.stop()
	syscall.Close(e.epollfd)
}

func (e *epollRoutine) Run() {
	var events [128]EpollEvent
retry:
	for {
		n, err := EpollWait(e.epollfd, events[:], 128, -1)
		if err != 0 {
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
				ev := events[i]
				hj := *(**hijackConn)(unsafe.Pointer(&ev.Data))
				e.Close(hj)
				// unblock all Read/Write call now and report EOF
				hj.Close()
			}
		}
	}
}
