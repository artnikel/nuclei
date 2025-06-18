// package gui implements the user interface of the project - scanner section
package gui

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/artnikel/nuclei/internal/logging"
	"github.com/artnikel/nuclei/internal/scanner"
	"github.com/artnikel/nuclei/internal/templates"
)

// ScannerPageWidget holds all the widgets for the scanner section
type ScannerPageWidget struct {
	TargetsLabel       *walk.Label
	TemplatesLabel     *walk.Label
	SelectTargetsBtn   *walk.PushButton
	SelectTemplatesBtn *walk.PushButton
	StatsLabel         *walk.Label
	StartBtn           *walk.PushButton
	StopBtn            *walk.PushButton
}

// ScanStats holds comprehensive scanning statistics
type ScanStats struct {
	TotalTargets   int64
	Processed      int64
	Successes      int64
	Errors         int64
	TotalDuration  int64
	StartTime      time.Time
}

var (
	scannerWidget ScannerPageWidget
	targetsFile   string
	templatesDir  string
	isRunning     = &atomic.Bool{}
	cancelScan    context.CancelFunc
	scanStats     ScanStats
)

// BuildScannerSection builds the scanner UI section and returns the page and widget structure
func BuildScannerSection(logger *logging.Logger) (TabPage, *ScannerPageWidget) {
	page := TabPage{
		Title:  "Scanner",
		Layout: VBox{},
		Children: []Widget{
			Label{
				Text: "Scan Targets Section",
				Font: Font{Bold: true, PointSize: 12},
			},
			VSpacer{Size: 10},

			PushButton{
				AssignTo: &scannerWidget.SelectTargetsBtn,
				Text:     "Select targets (.txt)",
				MinSize:  Size{Width: 200, Height: 30},
			},
			Label{
				AssignTo: &scannerWidget.TargetsLabel,
				Text:     "Targets: (not selected)",
			},
			VSpacer{Size: 10},

			PushButton{
				AssignTo: &scannerWidget.SelectTemplatesBtn,
				Text:     "Select template (.yaml/.yml)",
				MinSize:  Size{Width: 200, Height: 30},
			},
			Label{
				AssignTo: &scannerWidget.TemplatesLabel,
				Text:     "Templates: (not selected)",
			},
			VSpacer{Size: 10},

			Composite{
				Layout: HBox{},
				Children: []Widget{
					PushButton{
						AssignTo: &scannerWidget.StartBtn,
						Text:     "Start",
						MinSize:  Size{Width: 80, Height: 30},
					},
					PushButton{
						AssignTo: &scannerWidget.StopBtn,
						Text:     "Stop",
						MinSize:  Size{Width: 80, Height: 30},
						Enabled:  false,
					},
				},
			},
			VSpacer{Size: 10},

			Label{
				AssignTo: &scannerWidget.StatsLabel,
				Text:     initialStatsText(),
			},
		},
	}

	return page, &scannerWidget
}

// InitializeScannerSection initializes the scanner section widgets with their event handlers
func InitializeScannerSection(widget *ScannerPageWidget, parent walk.Form, logger *logging.Logger) {
	widget.SelectTargetsBtn.Clicked().Attach(func() {
		selectTargetsFile(parent, widget)
	})

	widget.SelectTemplatesBtn.Clicked().Attach(func() {
		selectTemplatesFile(parent, widget)
	})

	widget.StartBtn.Clicked().Attach(func() {
		handleStartButtonClick(parent, widget, logger)
	})

	widget.StopBtn.Clicked().Attach(func() {
		if cancelScan != nil {
			cancelScan()
		}
	})
}

// selectTargetsFile opens a file dialog to select targets file
func selectTargetsFile(parent walk.Form, widget *ScannerPageWidget) {
	dlg := new(walk.FileDialog)
	dlg.Filter = "Text Files (*.txt)|*.txt"
	dlg.Title = "Select targets file"

	if ok, err := dlg.ShowOpen(parent); err != nil {
		walk.MsgBox(parent, "Error", err.Error(), walk.MsgBoxIconError)
		return
	} else if !ok {
		return
	}

	targetsFile = dlg.FilePath
	widget.TargetsLabel.SetText("Targets: " + targetsFile)
}

// selectTemplatesFile opens a file dialog to select template file
func selectTemplatesFile(parent walk.Form, widget *ScannerPageWidget) {
	dlg := new(walk.FileDialog)
	dlg.Filter = "YAML Files (*.yaml;*.yml)|*.yaml;*.yml"
	dlg.Title = "Select template file"

	if ok, err := dlg.ShowOpen(parent); err != nil {
		walk.MsgBox(parent, "Error", err.Error(), walk.MsgBoxIconError)
		return
	} else if !ok {
		return
	}

	templatesDir = dlg.FilePath
	widget.TemplatesLabel.SetText("Template: " + templatesDir)
}

// initialStatsText returns a string with initial statistics values
func initialStatsText() string {
	return "Statistics:\nTargets loaded: 0\nProcessed: 0\nSuccesses: 0\nErrors: 0\n"
}

// handleStartButtonClick handles a click on the scan start button
func handleStartButtonClick(parent walk.Form, widget *ScannerPageWidget, logger *logging.Logger) {
	if isRunning.Load() {
		walk.MsgBox(parent, "Scanner running", "Scanner is already running", walk.MsgBoxIconInformation)
		return
	}

	if targetsFile == "" {
		walk.MsgBox(parent, "Error", "Targets file not selected", walk.MsgBoxIconError)
		return
	}
	if templatesDir == "" {
		walk.MsgBox(parent, "Error", "Templates file not selected", walk.MsgBoxIconError)
		return
	}

	template, err := templates.LoadTemplate(templatesDir)
	if err != nil {
		logger.Error.Printf("failed to load template: %v", err)
		walk.MsgBox(parent, "Error", fmt.Sprintf("Failed to load template: %v", err), walk.MsgBoxIconError)
	}

	isRunning.Store(true)
	widget.StartBtn.SetEnabled(false)
	widget.StopBtn.SetEnabled(true)

	ctx, cancel := context.WithTimeout(context.Background(), advanced.Timeout)
	cancelScan = cancel

	statsUpdateCh := make(chan string, 10)
	go updateStatsLabel(widget, statsUpdateCh)

	go runScan(ctx, targetsFile, advanced.Workers, template, statsUpdateCh, widget, logger)
}

func updateStatsLabel(widget *ScannerPageWidget, statsUpdateCh <-chan string) {
	for update := range statsUpdateCh {
		widget.StatsLabel.Synchronize(func() {
			widget.StatsLabel.SetText(update)
		})
	}
}

// runScan starts the scan cycle: read targets, apply templates, collect statistics
func runScan(
	ctx context.Context,
	targetsFile string,
	threads int,
	template *templates.Template,
	statsUpdateCh chan<- string,
	widget *ScannerPageWidget,
	logger *logging.Logger,
) {
	defer func() {
		widget.StartBtn.Synchronize(func() {
			isRunning.Store(false)
			widget.StartBtn.SetEnabled(true)
			widget.StopBtn.SetEnabled(false)
		})
	}()

	ResetStats()

	targetsChan := make(chan string, 1000)

	go func() {
		defer close(targetsChan)

		file, err := os.Open(targetsFile)
		if err != nil {
			logger.Error.Printf("Error opening targets file %s: %v", targetsFile, err)
			return
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
				target := strings.TrimSpace(scanner.Text())
				if target == "" {
					continue
				}
				normalized, err := NormalizeTarget(target)
				if err != nil {
					logger.Info.Printf("Skipping invalid target %s: %v", target, err)
					continue
				}
				targetsChan <- normalized
				IncrementTargets()
			}
		}
	}()

	processFn := func(ctx context.Context, target string) error {
		startTime := time.Now()
		matched, err := templates.MatchTemplate(ctx, target, "", template, advanced, logger)
		duration := time.Since(startTime)

		IncrementProcessed()
		AddDuration(duration)

		if err != nil {
			logger.Info.Printf("Error processing target %s: %v", target, err)
			IncrementErrors()
			return err
		}

		if matched {
			templates.SaveGood(target, template.ID)
			IncrementSuccesses()
			return nil
		}
		IncrementErrors()
		return fmt.Errorf("no match found")
	}
	var tickerWg sync.WaitGroup
	tickerWg.Add(1)
	resultsDone := scanner.StartWorkers(ctx, targetsChan, threads, processFn, logger)

	go func() {
		defer tickerWg.Done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				select {
				case statsUpdateCh <- FormatEnhancedStats():
				case <-ctx.Done():
					return
				}
			case <-resultsDone:
				return
			}
		}
	}()

	tickerWg.Wait()

	statsUpdateCh <- "Scan finished.\n" + FormatEnhancedStats()
	close(statsUpdateCh)
}

func NormalizeTarget(target string) (string, error) {
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return target, nil
	}

	normalized := "https://" + target
	if _, err := url.Parse(normalized); err != nil {
		return "", err
	}

	return normalized, nil
}

func FormatEnhancedStats() string {
	stats := GetCurrentStats()

	elapsed := time.Since(stats.StartTime)

	return fmt.Sprintf(`Statistics:
Targets loaded: %d
Processed: %d
Successes: %d
Errors: %d
Elapsed: %v`,
		stats.TotalTargets,
		stats.Processed,
		stats.Successes,
		stats.Errors,
		elapsed.Truncate(time.Second),
	)
}

// ResetStats resets all statistics
func ResetStats() {
	atomic.StoreInt64(&scanStats.TotalTargets, 0)
	atomic.StoreInt64(&scanStats.Processed, 0)
	atomic.StoreInt64(&scanStats.Successes, 0)
	atomic.StoreInt64(&scanStats.Errors, 0)
	atomic.StoreInt64(&scanStats.TotalDuration, 0)
	scanStats.StartTime = time.Now()
}

// IncrementTargets increments the total targets counter
func IncrementTargets() {
	atomic.AddInt64(&scanStats.TotalTargets, 1)
}

// IncrementProcessed increments the processed counter
func IncrementProcessed() {
	atomic.AddInt64(&scanStats.Processed, 1)
}

// IncrementSuccesses increments the successes counter
func IncrementSuccesses() {
	atomic.AddInt64(&scanStats.Successes, 1)
}

// IncrementErrors increments the errors counter
func IncrementErrors() {
	atomic.AddInt64(&scanStats.Errors, 1)
}

// AddDuration adds processing duration
func AddDuration(duration time.Duration) {
	atomic.AddInt64(&scanStats.TotalDuration, duration.Milliseconds())
}

// GetCurrentStats returns current statistics snapshot
func GetCurrentStats() ScanStats {
	return ScanStats{
		TotalTargets:  atomic.LoadInt64(&scanStats.TotalTargets),
		Processed:     atomic.LoadInt64(&scanStats.Processed),
		Successes:     atomic.LoadInt64(&scanStats.Successes),
		Errors:        atomic.LoadInt64(&scanStats.Errors),
		TotalDuration: atomic.LoadInt64(&scanStats.TotalDuration),
		StartTime:     scanStats.StartTime,
	}
}
