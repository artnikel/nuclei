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
	cancelFunc context.CancelFunc // cancel browser func
	initErr    error           // error during initialization
)

// InitHeadless initializes the shared headless Chrome browser context once
func InitHeadless() error {
	once.Do(func() {
		opts := append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", true),
			chromedp.Flag("ignore-certificate-errors", true),
			chromedp.Flag("disable-gpu", true),
			chromedp.Flag("no-sandbox", true),
			chromedp.Flag("disable-dev-shm-usage", true), 
			chromedp.Flag("disable-background-timer-throttling", true),
			chromedp.Flag("disable-backgrounding-occluded-windows", true),
			chromedp.Flag("disable-renderer-backgrounding", true),
			chromedp.Flag("memory-pressure-off", true),
			chromedp.Flag("max_old_space_size", "256"), 
		)

		allocCtx, cancelFunc = chromedp.NewExecAllocator(context.Background(), opts...)
		browserCtx, _ = chromedp.NewContext(allocCtx)
		initErr = chromedp.Run(browserCtx, chromedp.Tasks{})

		if initErr != nil {
			if cancelFunc != nil {
				cancelFunc()
			}
			browserCtx = nil
		}
	})

	return initErr
}

func CloseHeadless() {
	if cancelFunc != nil {
		cancelFunc()
	}
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
		chromedp.ActionFunc(func(ctx context.Context) error {
			return chromedp.Run(ctx,
				chromedp.Evaluate(`
					document.addEventListener('DOMContentLoaded', function() {
						var images = document.querySelectorAll('img');
						for (var i = 0; i < images.length; i++) {
							images[i].src = '';
						}
					});
				`, nil),
			)
		}),
		
		chromedp.Navigate(fullURL),
		
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.OuterHTML("html", &htmlContent, chromedp.ByQuery),
		chromedp.Navigate("about:blank"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return chromedp.Run(ctx, chromedp.Evaluate("if (window.gc) window.gc();", nil))
		}),
	)

	if err != nil {
		return "", fmt.Errorf("chromedp run failed: %w", err)
	}
	
	const maxHTMLSize = 5 * 1024 * 1024 
	if len(htmlContent) > maxHTMLSize {
		htmlContent = htmlContent[:maxHTMLSize]
	}
	
	return htmlContent, nil
}

func ForceReinitHeadless() {
	if cancelFunc != nil {
		cancelFunc()
	}
	once = sync.Once{}
	allocCtx = nil
	browserCtx = nil
	cancelFunc = nil
	initErr = nil
}
