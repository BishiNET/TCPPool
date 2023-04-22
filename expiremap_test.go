package tcppool

import (
	"testing"
	"time"
)

func TestExpired(t *testing.T) {
	var m ExpireMap
	m.Store("xxx", 123456, 5*time.Second)
	time.Sleep(6 * time.Second)
	t.Log(m.Load("xxx"))
	m.Store("xxx", 123456)
	time.Sleep(4 * time.Second)
	t.Log(m.Load("xxx"))
}
