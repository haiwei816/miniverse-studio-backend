package service

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestLimiterBoundAndExpiration(t *testing.T) {
	s := New(nil, true)
	now := time.Now()
	for i := 0; i < 20000; i++ {
		s.allowRequest(fmt.Sprint(i), now)
	}
	if len(s.limits) != maxLimitEntries {
		t.Fatal(len(s.limits))
	}
	if s.allowRequest("new-source", now) {
		t.Fatal("capacity bypass")
	}
	if !s.allowRequest("0", now) {
		t.Fatal("existing source blocked")
	}
	if !s.allowRequest("new-source", now.Add(time.Minute)) {
		t.Fatal("expired entries not reclaimed")
	}
	if len(s.limits) != 1 {
		t.Fatal(len(s.limits))
	}
	for i := 1; i < 600; i++ {
		if !s.allowRequest("new-source", now.Add(time.Minute)) {
			t.Fatal(i)
		}
	}
	if s.allowRequest("new-source", now.Add(time.Minute)) {
		t.Fatal("rate limit bypass")
	}
}
func TestLimiterConcurrentSources(t *testing.T) {
	s := New(nil, true)
	now := time.Now()
	var wg sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				s.allowRequest(fmt.Sprintf("%d-%d", worker, i), now)
			}
		}(worker)
	}
	wg.Wait()
	if len(s.limits) > maxLimitEntries {
		t.Fatal("unbounded concurrent growth")
	}
}
