package scanner

import (
	"context"
	"fmt"
	"sync"
)

type ProcessTargetFunc func(ctx context.Context, target string) error

func StartWorkers(ctx context.Context, targetsCh <-chan string, workers int, processFn ProcessTargetFunc) <-chan struct{} {
	doneCh := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
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
					if err != nil {
						fmt.Printf("Error processing target %s: %v\n", target, err)
					}
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(doneCh)
	}()

	return doneCh
}
