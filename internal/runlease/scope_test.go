package runlease

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestConcurrentAdmissionAndCancellation(t *testing.T) {
	scope := New()
	ctx := WithScope(context.Background(), scope)
	if FromContext(context.WithoutCancel(ctx)) != scope {
		t.Fatal("detachment lost task ownership")
	}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			worker, cancel := context.WithCancel(context.Background())
			defer cancel()
			release, err := scope.Register(string(rune('a'+i)), cancel)
			if errors.Is(err, ErrClosed) {
				return
			}
			if err != nil {
				t.Error(err)
				return
			}
			<-worker.Done()
			release()
		}(i)
	}
	scope.Cancel()
	wg.Wait()
	wait, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := scope.Wait(wait); err != nil {
		t.Fatal(err)
	}
}
func TestDetachedWorkerCapacityReleasedOnCompletion(t *testing.T) {
	scope := New()
	releases := make([]func(), 0, MaxTaskWorkers)
	for i := 0; i < MaxTaskWorkers; i++ {
		release, err := scope.Register(string(rune(i)), func() {})
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	if _, err := scope.Register("overflow", func() {}); err == nil {
		t.Fatal("unbounded worker admission")
	}
	releases[0]()
	release, err := scope.Register("replacement", func() {})
	if err != nil {
		t.Fatal(err)
	}
	release()
	for _, release := range releases {
		release()
	}
	scope.Cancel()
	if err = scope.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
