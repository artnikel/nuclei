package headless

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
)

var (
	once        sync.Once
	allocCtx    context.Context
	browserCtx  context.Context
	initErr     error
	headlessSem = make(chan struct{}, runtime.NumCPU())
)

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

func DoHeadlessRequest(ctx context.Context, fullURL string) (string, error) {
	if err := InitHeadless(); err != nil {
		return "", fmt.Errorf("failed to init headless: %w", err)
	}

	headlessSem <- struct{}{}
	defer func() { <-headlessSem }()

	tabCtx, cancel := chromedp.NewContext(browserCtx)
	defer cancel()

	tabCtx, timeoutCancel := context.WithTimeout(tabCtx, 60*time.Second)
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
