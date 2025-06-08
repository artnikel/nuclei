package gui

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"github.com/artnikel/nuclei/internal/scanner"
	"github.com/artnikel/nuclei/internal/templates"
)

func BuildScannerSection(a fyne.App, w fyne.Window) (fyne.CanvasObject, *atomic.Bool, *context.CancelFunc) {
	var targetsFile string
	var templatesDir string

	isRunning := &atomic.Bool{}
	var cancelScan context.CancelFunc

	targetsLabel := widget.NewLabel("Targets: (not selected)")
	templatesLabel := widget.NewLabel("Templates: (not selected)")

	selectTargetsBtn := widget.NewButton("Select targets.txt", func() {
		fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil || reader == nil {
				return
			}
			targetsFile = reader.URI().Path()
			targetsLabel.SetText("Targets: " + targetsFile)
		}, w)
		fd.SetFilter(storage.NewExtensionFileFilter([]string{".txt"}))
		fd.Show()
	})

	selectTemplatesBtn := widget.NewButton("Select templates folder", func() {
		fd := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil || uri == nil {
				return
			}
			templatesDir = uri.Path()
			templatesLabel.SetText("Templates: " + templatesDir)
		}, w)
		fd.Show()
	})

	maxThreads := runtime.NumCPU()
	options := []string{}
	for i := 1; i <= maxThreads; i++ {
		options = append(options, strconv.Itoa(i))
	}
	threadsSelect := widget.NewSelect(options, nil)
	threadsSelect.SetSelected(strconv.Itoa(maxThreads))

	timeoutEntry := widget.NewEntry()
	timeoutEntry.SetText("5")

	statsBinding := binding.NewString()
	_ = statsBinding.Set("Statistics:\nTargets loaded: 0\nProcessed: 0\nSuccesses: 0\nErrors: 0\nAvg time (ms): 0")
	statsLabel := widget.NewLabelWithData(statsBinding)

	startBtn := widget.NewButton("Start", nil)
	stopBtn := widget.NewButton("Stop", nil)
	stopBtn.Disable()

	startBtn.OnTapped = func() {
		if isRunning.Load() {
			dialog.ShowInformation("Scanner running", "Scanner is already running", w)
			return
		}

		threads, err1 := strconv.Atoi(threadsSelect.Selected)
		timeoutSec, err2 := strconv.Atoi(timeoutEntry.Text)

		if err1 != nil || threads <= 0 {
			dialog.ShowError(fmt.Errorf("invalid thread count"), w)
			return
		}
		if err2 != nil || timeoutSec <= 0 {
			dialog.ShowError(fmt.Errorf("invalid timeout"), w)
			return
		}
		if targetsFile == "" {
			dialog.ShowError(fmt.Errorf("targets file not selected"), w)
			return
		}
		if templatesDir == "" {
			dialog.ShowError(fmt.Errorf("templates folder not selected"), w)
			return
		}
		file, err := os.Open(targetsFile)
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to open targets file: %w", err), w)
			return
		}
		defer file.Close()

		bufScanner := bufio.NewScanner(file)
		var firstTarget string
		if bufScanner.Scan() {
			firstTarget = bufScanner.Text()
		}
		if firstTarget == "" {
			dialog.ShowError(fmt.Errorf("no targets found in file"), w)
			return
		}
		if !strings.HasPrefix(firstTarget, "http://") && !strings.HasPrefix(firstTarget, "https://") {
			firstTarget = "http://" + firstTarget
		}

		loadedTemplate, err := templates.FindFirstTemplate(firstTarget, templatesDir)

		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to load template: %w", err), w)
			return
		}

		isRunning.Store(true)
		startBtn.Disable()
		stopBtn.Enable()

		ctx, cancel := context.WithCancel(context.Background())
		cancelScan = cancel

		statsUpdateCh := make(chan string, 10)

		go func() {
			for update := range statsUpdateCh {
				_ = statsBinding.Set(update)
			}
		}()

		go func() {
			defer func() {
				close(statsUpdateCh)
				a.Driver().DoFromGoroutine(func() {
					isRunning.Store(false)
					startBtn.Enable()
					stopBtn.Disable()
				}, true)
			}()

			var totalTargets int64 = 0
			var processed int64 = 0
			var success int64 = 0
			var errors int64 = 0
			var totalDuration int64 = 0

			targetsChan := make(chan string, 1000)

			go func() {
				file, err := os.Open(targetsFile)
				if err != nil {
					log.Printf("Error opening targets file %s: %v\n", targetsFile, err)
					close(targetsChan)
					return
				}

				defer file.Close()

				bufScanner := bufio.NewScanner(file)
				for bufScanner.Scan() {
					select {
					case <-ctx.Done():
						close(targetsChan)
						return
					default:
						target := bufScanner.Text()
						atomic.AddInt64(&totalTargets, 1)
						targetsChan <- target
					}
				}
				if err := bufScanner.Err(); err != nil {
					log.Println("Error reading targets:", err)
				}
				close(targetsChan)
			}()

			wrappedProcessFn := func(ctx context.Context, target string) error {
				startTime := time.Now()
				err := scanner.ProcessTarget(ctx, target, loadedTemplate, timeoutSec)
				durationMs := time.Since(startTime).Milliseconds()

				atomic.AddInt64(&processed, 1)
				atomic.AddInt64(&totalDuration, durationMs)

				if err == nil {
					atomic.AddInt64(&success, 1)
				} else {
					log.Printf("Error processing target %s: %v\n", target, err)
					atomic.AddInt64(&errors, 1)
				}

				return err
			}

			resultsDone := scanner.StartWorkers(ctx, targetsChan, threads, wrappedProcessFn)

			ticker := time.NewTicker(300 * time.Millisecond)
			defer ticker.Stop()

		loop:
			for {
				select {
				case <-ctx.Done():
					break loop
				case <-resultsDone:
					break loop
				case <-ticker.C:
					processedCount := atomic.LoadInt64(&processed)
					totalDurationMs := atomic.LoadInt64(&totalDuration)
					avgDuration := float64(0)
					if processedCount > 0 {
						avgDuration = float64(totalDurationMs) / float64(processedCount)
					}
					errorsCount := atomic.LoadInt64(&errors)
					successCount := atomic.LoadInt64(&success)
					totalCount := atomic.LoadInt64(&totalTargets)

					statsUpdateCh <- fmt.Sprintf(
						"Statistics:\nTargets loaded: %d\nProcessed: %d\nSuccesses: %d\nErrors: %d\nAvg time (ms): %.2f",
						totalCount,
						processedCount,
						successCount,
						errorsCount,
						avgDuration,
					)
				}
			}

			processedCount := atomic.LoadInt64(&processed)
			totalDurationMs := atomic.LoadInt64(&totalDuration)
			avgDuration := float64(0)
			if processedCount > 0 {
				avgDuration = float64(totalDurationMs) / float64(processedCount)
			}
			errorsCount := atomic.LoadInt64(&errors)
			successCount := atomic.LoadInt64(&success)
			totalCount := atomic.LoadInt64(&totalTargets)

			statsUpdateCh <- fmt.Sprintf(
				"Scan finished.\nTargets loaded: %d\nProcessed: %d\nSuccesses: %d\nErrors: %d\nAvg time (ms): %.2f",
				totalCount,
				processedCount,
				successCount,
				errorsCount,
				avgDuration,
			)
		}()
	}

	stopBtn.OnTapped = func() {
		if cancelScan != nil {
			cancelScan()
		}
	}

	section := container.NewVBox(
		widget.NewLabel("Scan Targets Section"),
		selectTargetsBtn, targetsLabel,
		selectTemplatesBtn, templatesLabel,
		widget.NewForm(
			widget.NewFormItem("Number of threads", threadsSelect),
			widget.NewFormItem("Timeout (seconds)", timeoutEntry),
		),
		container.NewHBox(startBtn, stopBtn),
		statsLabel,
	)

	return section, isRunning, &cancelScan
}
