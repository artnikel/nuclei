// pacakage scanner implementing workers that process targets
package scanner

import (
	"context"
	"sync"

	"github.com/artnikel/nuclei/internal/logging"
)

// ProcessTargetFunc defines a function for processing one target (target)
type ProcessTargetFunc func(ctx context.Context, target string) error

// StartWorkers starts the specified number of Workers that process targets from the targetsCh channel in parallel.
// Returns the channel that will be closed after all Workers are finished
func StartWorkers(ctx context.Context, targetsCh <-chan string, workers int, processFn ProcessTargetFunc, logger *logging.Logger) <-chan struct{} {
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
						//logger.Info.Printf("Error processing target %s: %v\n", target, err)
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
