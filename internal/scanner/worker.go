package scanner

import (
	"context"
	"sync"
)

type ProcessTargetFunc func(ctx context.Context, target string) error

func StartWorkers(ctx context.Context, targetsCh <-chan string, workers int, processFn ProcessTargetFunc) <-chan int {
	resultsCh := make(chan int)

	var wg sync.WaitGroup
	wg.Add(workers)

	var successCount int
	var mu sync.Mutex

	for i := 0; i < workers; i++ {
		go func(workerID int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case target, ok := <-targetsCh:
					if !ok {
						return
					}
					err := processFn(ctx, target)
					if err == nil {
						mu.Lock()
						successCount++
						mu.Unlock()
					}
				}
			}
		}(i)
	}

	go func() {
		wg.Wait()
		resultsCh <- successCount
		close(resultsCh)
	}()

	return resultsCh
}
