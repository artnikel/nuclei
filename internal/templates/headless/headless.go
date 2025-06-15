// Package headless provides utilities for running headless Chrome browser tasks
package headless

import (
	"context"
	"fmt"
	"sync"
	"time"

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
			chromedp.Flag("headless", true),
			chromedp.Flag("ignore-certificate-errors", true),
			chromedp.Flag("disable-gpu", true),
		)

		var cancel context.CancelFunc
		allocCtx, cancel = chromedp.NewExecAllocator(context.Background(), opts...)

		browserCtx, _ = chromedp.NewContext(allocCtx)

		initErr = chromedp.Run(browserCtx)

		if initErr != nil {
			cancel()
			browserCtx = nil
		}
	})

	return initErr
}

// DoHeadlessRequest opens a new tab, navigates to fullURL, waits for body, and returns the page HTML
func DoHeadlessRequest(ctx context.Context, fullURL string, tabs int, timeout time.Duration) (string, error) {
	if err := InitHeadless(); err != nil {
		return "", fmt.Errorf("failed to init headless: %w", err)
	}

	if browserCtx == nil {
		return "", fmt.Errorf("internal error: headless browser context is nil")
	}

	headlessSem := make(chan struct{}, tabs) // semaphore limiting concurrent headless tabs
	headlessSem <- struct{}{}
	defer func() { <-headlessSem }()

	tabCtx, cancel := chromedp.NewContext(browserCtx)
	defer cancel()

	tabCtx, timeoutCancel := context.WithTimeout(tabCtx, timeout)
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
