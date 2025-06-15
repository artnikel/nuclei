// package gui implements the user interface of the project - scanner section
package gui

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
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
	ThreadsEntry       *walk.LineEdit
	TimeoutEntry       *walk.LineEdit
	StatsLabel         *walk.Label
	StartBtn           *walk.PushButton
	StopBtn            *walk.PushButton
}

var (
	scannerWidget ScannerPageWidget
	targetsFile   string
	templatesDir  string
	isRunning     = &atomic.Bool{}
	cancelScan    context.CancelFunc
	goodResultsMu sync.Mutex
)

// BuildScannerSection builds the scanner UI section and returns the page and widget structure
func BuildScannerSection(logger *logging.Logger) (TabPage, *ScannerPageWidget) {
	maxThreads := runtime.NumCPU()
	maxThreadsStr := strconv.Itoa(maxThreads)

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
				MinSize:  Size{200, 30},
			},
			Label{
				AssignTo: &scannerWidget.TargetsLabel,
				Text:     "Targets: (not selected)",
			},
			VSpacer{Size: 10},

			PushButton{
				AssignTo: &scannerWidget.SelectTemplatesBtn,
				Text:     "Select template (.yaml/.yml)",
				MinSize:  Size{200, 30},
			},
			Label{
				AssignTo: &scannerWidget.TemplatesLabel,
				Text:     "Templates: (not selected)",
			},
			VSpacer{Size: 10},

			Composite{
				Layout: Grid{Columns: 2},
				Children: []Widget{
					Label{Text: "Number of threads:"},
					LineEdit{
						AssignTo: &scannerWidget.ThreadsEntry,
						Text:     maxThreadsStr,
					},
					Label{Text: "Timeout (seconds):"},
					LineEdit{
						AssignTo: &scannerWidget.TimeoutEntry,
						Text:     "10",
					},
				},
			},
			VSpacer{Size: 10},

			Composite{
				Layout: HBox{},
				Children: []Widget{
					PushButton{
						AssignTo: &scannerWidget.StartBtn,
						Text:     "Start",
						MinSize:  Size{80, 30},
					},
					PushButton{
						AssignTo: &scannerWidget.StopBtn,
						Text:     "Stop",
						MinSize:  Size{80, 30},
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
	return "Statistics:\nTargets loaded: 0\nProcessed: 0\nSuccesses: 0\nErrors: 0\nAvg time (ms): 0"
}

// handleStartButtonClick handles a click on the scan start button
func handleStartButtonClick(parent walk.Form, widget *ScannerPageWidget, logger *logging.Logger) {
	if isRunning.Load() {
		walk.MsgBox(parent, "Scanner running", "Scanner is already running", walk.MsgBoxIconInformation)
		return
	}

	threads, _ := strconv.Atoi(widget.ThreadsEntry.Text())
	if threads <= 0 {
		walk.MsgBox(parent, "Error", "Invalid thread count", walk.MsgBoxIconError)
		return
	}

	timeoutFloat, err := strconv.ParseFloat(widget.TimeoutEntry.Text(), 64)
	if err != nil || timeoutFloat < 0 {
		walk.MsgBox(parent, "Error", "Invalid timeout", walk.MsgBoxIconError)
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

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutFloat)*time.Second)
	cancelScan = cancel

	statsUpdateCh := make(chan string, 10)
	go updateStatsLabel(widget, statsUpdateCh)

	go runScan(ctx, targetsFile, threads, template, statsUpdateCh, parent, widget, logger)
}

// updateStatsLabel listens to the update channel and updates the statistics label
func updateStatsLabel(widget *ScannerPageWidget, statsUpdateCh <-chan string) {
	for update := range statsUpdateCh {
		// Use synchronous call to update UI from goroutine
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
	parent walk.Form,
	widget *ScannerPageWidget,
	logger *logging.Logger,
) {
	defer func() {
		close(statsUpdateCh)
		widget.StartBtn.Synchronize(func() {
			isRunning.Store(false)
			widget.StartBtn.SetEnabled(true)
			widget.StopBtn.SetEnabled(false)
		})
	}()

	var totalTargets, processed, success, errors, totalDuration int64
	targetsChan := make(chan string, 1000)

	go feedTargets(ctx, targetsFile, targetsChan, &totalTargets)

	processFn := func(ctx context.Context, target string) error {
		startTime := time.Now()
		matched, err := templates.MatchTemplate(ctx, target, "", template, &templates.AdvancedSettingsChecker{}, logger)
		durationMs := time.Since(startTime).Milliseconds()

		atomic.AddInt64(&processed, 1)
		atomic.AddInt64(&totalDuration, durationMs)

		if err != nil {
			logger.Info.Printf("Error processing target %s: %v\n", target, err)
			atomic.AddInt64(&errors, 1)
			return err
		}

		if matched {
			templates.SaveGood(target, template.ID, goodResultsMu)
			atomic.AddInt64(&success, 1)
			return nil
		}

		atomic.AddInt64(&errors, 1)
		return fmt.Errorf("no match found")
	}

	resultsDone := scanner.StartWorkers(ctx, targetsChan, threads, processFn, logger)

	select {
	case <-ctx.Done():
		return
	case <-resultsDone:
	}

	statsUpdateCh <- "Scan finished.\n" + formatStats(totalTargets, processed, success, errors, totalDuration)
}

// feedTargets reads targets from the file and sends them to the channel for scanning
func feedTargets(ctx context.Context, targetsFile string, targetsChan chan<- string, totalTargets *int64) {
	defer close(targetsChan)

	file, err := os.Open(targetsFile)
	if err != nil {
		fmt.Printf("Error opening targets file %s: %v\n", targetsFile, err)
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
			targetsChan <- target
			atomic.AddInt64(totalTargets, 1)
		}
	}
}

// formatStats formats the collected statistics at the end of scanning
func formatStats(totalTargets, processed, success, errors, totalDuration int64) string {
	var avgMs int64
	if processed > 0 {
		avgMs = totalDuration / processed
	}
	return fmt.Sprintf(
		"Statistics:\nTargets loaded: %d\nProcessed: %d\nSuccesses: %d\nErrors: %d\nAvg time (ms): %d",
		totalTargets, processed, success, errors, avgMs,
	)
}
