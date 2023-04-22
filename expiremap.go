package tcppool

import (
	"sync"
	"time"
)

type entry struct {
	value  any
	expire int64
}

type ExpireMap struct {
	m sync.Map
}

func NewExpireMap() *ExpireMap {
	return &ExpireMap{}
}

func (e *ExpireMap) Load(key any) (value any, exist bool) {
	if v, ok := e.m.Load(key); ok {
		if one, ok := v.(*entry); ok {
			if time.Now().Unix() < one.expire || one.expire == 0 {
				value = one.value
				exist = true
				return
			}
			// expired
			e.m.Delete(key)
		}
	}
	return
}

func (e *ExpireMap) Store(key, value any, expired ...time.Duration) {
	newEntry := &entry{
		value: value,
	}
	if len(expired) > 0 {
		newEntry.expire = time.Now().Add(expired[0]).Unix()
	}
	e.m.Store(key, newEntry)
}

func (e *ExpireMap) Reset(key, value any, expired ...time.Duration) (exist bool) {
	var newExpired int64
	if len(expired) > 0 {
		newExpired = time.Now().Add(expired[0]).Unix()
	}
	if v, ok := e.m.Load(key); ok {
		if one, ok := v.(*entry); ok {
			// remove it temporarily to avoid race
			e.m.Delete(key)
			one.expire = newExpired
			e.m.Store(key, one)
			exist = true
		}
	}
	return
}

func (e *ExpireMap) Delete(key any) {
	e.m.Delete(key)
}

func (e *ExpireMap) DeleteExpired() {
	e.m.Range(func(key, value any) bool {
		if one, ok := value.(*entry); ok {
			if time.Now().Unix() >= one.expire && one.expire != 0 {
				e.m.Delete(key)
			}
		}
		return true
	})
}
