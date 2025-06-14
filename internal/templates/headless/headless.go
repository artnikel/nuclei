// Package headless provides utilities for running headless Chrome browser tasks
package headless

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/artnikel/nuclei/internal/constants"
	"github.com/chromedp/chromedp"
)

var (
	once       sync.Once       // ensures headless browser initializes only once
	allocCtx   context.Context // Chrome exec allocator context
	browserCtx context.Context // browser context for tabs
	initErr    error           // error during initialization
)

// InitHeadless initializes the shared headless Chrome browser context once
func InitHeadless() error {
	once.Do(func() {
		opts := append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("ignore-certificate-errors", true),
			chromedp.Headless,
			chromedp.DisableGPU,
		)

		var cancel context.CancelFunc
		allocCtx, cancel = chromedp.NewExecAllocator(context.Background(), opts...)

		browserCtx, _ = chromedp.NewContext(allocCtx,
			chromedp.WithLogf(func(format string, args ...interface{}) {
				msg := fmt.Sprintf(format, args...)
				if strings.Contains(msg, "could not unmarshal event") {
					return
				}
			}),
		)

		initErr = chromedp.Run(browserCtx)
		if initErr != nil {
			cancel()
		}
	})

	return initErr
}

// DoHeadlessRequest opens a new tab, navigates to fullURL, waits for body, and returns the page HTML
func DoHeadlessRequest(ctx context.Context, fullURL string, tabs int) (string, error) {
	if err := InitHeadless(); err != nil {
		return "", fmt.Errorf("failed to init headless: %w", err)
	}
	headlessSem := make(chan struct{}, tabs) // semaphore limiting concurrent headless tabs
	headlessSem <- struct{}{}
	defer func() { <-headlessSem }()

	tabCtx, cancel := chromedp.NewContext(browserCtx)
	defer cancel()

	tabCtx, timeoutCancel := context.WithTimeout(tabCtx, constants.OneMinTimeout)
	defer timeoutCancel()

	var htmlContent string

	err := chromedp.Run(tabCtx,
		chromedp.Navigate(fullURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.OuterHTML("html", &htmlContent, chromedp.ByQuery),
	)
	if err != nil {
		return "", fmt.Errorf("chromedp run failed: %w", err)
	}

	return htmlContent, nil
}
