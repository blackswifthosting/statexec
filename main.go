package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/blackswifthosting/statexec/collectors"
)

var (
	version                     = "dev"
	extraLabels                 map[string]string
	metricsFile                 string
	metricsStartTime            int64
	metricsStartTimeOverride    int64 = -1
	secondesSinceGatheringStart int
	waitTimeBeforeCommand       int64
	waitTimeAfterCommand        int64
	jobName                     string = "statexec"
	instanceOverride            string = ""
	instance                    string
	role                        string
	commandState                int = 0
)

const (
	EnvVarPrefix         = "SE_"
	MetricPrefix         = "statexec_"
	CommandStatusPending = 0
	CommandStatusRunning = 1
	CommandStatusDone    = 2
)

func main() {
	// Check if a command is provided
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	// Parse environment variables
	parseEnvVars()

	// Delete metrics file if it exists
	_ = os.Remove(metricsFile)

	switch os.Args[1] {
	case "waitStart":
		role = "server"
		// Start the HTTP server
		waitForHttpSyncToStartCommand(exec.Command(os.Args[2], os.Args[3:]...), false)
	case "waitStartAndStop":
		role = "server"
		// Start the HTTP server and wait for a /start request before starting the command
		waitForHttpSyncToStartCommand(exec.Command(os.Args[2], os.Args[3:]...), true)
	case "exec":
		role = "standalone"
		// Start the command
		startCommand(exec.Command(os.Args[2], os.Args[3:]...))
	case "syncStart":
		role = "client"
		syncServerUrl := os.Args[2]
		syncStartCommand(exec.Command(os.Args[3], os.Args[4:]...), syncServerUrl, false)
	case "syncStartAndStop":
		role = "client"
		syncServerUrl := os.Args[2]
		syncStartCommand(exec.Command(os.Args[3], os.Args[4:]...), syncServerUrl, true)
	case "help":
		usage()
		os.Exit(0)
	case "--help":
		usage()
		os.Exit(0)
	case "-h":
		usage()
		os.Exit(0)
	case "version":
		fmt.Println(version)
		os.Exit(0)
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("Usage: " + os.Args[0] + " <mode> <command>")
	fmt.Println("Version:", version)
	fmt.Println("Description: Start a command and gather metrics about the system while the command is running")
	fmt.Println("Modes:")
	fmt.Println("  exec <command>")
	fmt.Println("    Start the command")
	fmt.Println("  waitStart <command>")
	fmt.Println("    Start an HTTP server on port 8080 and wait for a /start request before starting the command")
	fmt.Println("  waitStartAndStop <command>")
	fmt.Println("    Start an HTTP server on port 8080 and wait for a /start request before starting the command, and a /stop request to stop the command")
	fmt.Println("  syncStart <syncServerUrl> <command>")
	fmt.Println("    Send a /start request to the sync server before starting the command")
	fmt.Println("  syncStartAndStop <syncServerUrl> <command>")
	fmt.Println("    Send a /start request to the sync server before starting the command, and a /stop request to stop the command")
	fmt.Println("Environment variables:")
	fmt.Println("  " + EnvVarPrefix + "METRICS_FILE: (string) path to the file where metrics will be written (default: /tmp/exomonitor_metrics.txt)")
	fmt.Println("  " + EnvVarPrefix + "INSTANCE: (string) instance name (default is first argument of the command)")
	fmt.Println("  " + EnvVarPrefix + "TIME_RELATIVE: (int64) timestamp in ms since epoch, used to generate metrics timestamps. Set to -1 to keep current time (default: -1)")
	fmt.Println("  " + EnvVarPrefix + "WAIT_TIME_BEFORE_COMMAND: (int) time in seconds to wait before starting the command (default: 0)")
	fmt.Println("  " + EnvVarPrefix + "WAIT_TIME_AFTER_COMMAND: (int) time in seconds to wait after the command has finished (default: 0)")
	fmt.Println("  " + EnvVarPrefix + "LABEL_<key>: (string) extra label to add to all metrics (example: " + EnvVarPrefix + "LABEL_env=prod)")
	fmt.Println("Examples:")
	fmt.Println("  " + os.Args[0] + " exec ping -c 10 google.fr")
	fmt.Println("  " + EnvVarPrefix + "LABEL_env=prod " + os.Args[0] + " ./mycommand")
}

func appendToResultFile(text string) {
	// Open metrics file in append mode
	resultFile, err := os.OpenFile(metricsFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Error opening metrics file:", err)
		os.Exit(1)
	}
	defer resultFile.Close()
	if _, err := resultFile.WriteString(text); err != nil {
		fmt.Println("Error writing to metrics file:", err)
		os.Exit(1)
	}

}

func parseEnvVars() {

	// Read metrics file from environment variable, or use default
	metricsFile = os.Getenv(EnvVarPrefix + "METRICS_FILE")
	if metricsFile == "" {
		metricsFile = jobName + "_metrics.prom"
	}

	// Read instance from environment variable, or use default
	instanceOverride = os.Getenv(EnvVarPrefix + "INSTANCE")

	// Read time relative from environment variable, or use default
	timeRelativeStr := os.Getenv(EnvVarPrefix + "TIME_RELATIVE")
	if timeRelativeStr != "" {
		var err error
		metricsStartTimeOverride, err = strconv.ParseInt(timeRelativeStr, 10, 64)
		if err != nil {
			panic(fmt.Sprintln("Error parsing "+EnvVarPrefix+"TIME_RELATIVE env var, must be an int64 (timestamp in ms since epoch), found : ", timeRelativeStr))
		}
	}

	// Read wait time before command from environment variable, or use default
	waitTimeBeforeCommandStr := os.Getenv(EnvVarPrefix + "WAIT_TIME_BEFORE_COMMAND")
	if waitTimeBeforeCommandStr != "" {
		var err error
		waitTimeBeforeCommand, err = strconv.ParseInt(waitTimeBeforeCommandStr, 10, 64)
		if err != nil {
			panic(fmt.Sprintln("Error parsing "+EnvVarPrefix+"WAIT_TIME_BEFORE_COMMAND env var, must be an int64 (time in ms), found : ", waitTimeBeforeCommandStr))
		}
	} else {
		waitTimeBeforeCommand = 0
	}

	// Read wait time after command from environment variable, or use default
	waitTimeAfterCommandStr := os.Getenv(EnvVarPrefix + "WAIT_TIME_AFTER_COMMAND")
	if waitTimeAfterCommandStr != "" {
		var err error
		waitTimeAfterCommand, err = strconv.ParseInt(waitTimeAfterCommandStr, 10, 64)
		if err != nil {
			panic(fmt.Sprintln("Error parsing "+EnvVarPrefix+"WAIT_TIME_AFTER_COMMAND env var, must be an int64 (time in ms), found : ", waitTimeAfterCommandStr))
		}
	} else {
		waitTimeAfterCommand = 0
	}

	// Get extra labels from environment variables
	extraLabels = parseExtraLabelsFromEnv()
}

func parseExtraLabelsFromEnv() map[string]string {
	// List of forbidden label names
	forbiddenKeys := []string{"instance", "job", "cpu", "mode", "interface"}

	extraLabels := make(map[string]string)
	for _, env := range os.Environ() {

		if strings.HasPrefix(env, EnvVarPrefix+"LABEL_") {

			parts := strings.SplitN(env, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimPrefix(parts[0], EnvVarPrefix+"LABEL_")
				value := parts[1]

				// Replace non-alphanumeric characters with underscores
				safeKey := regexp.MustCompile(`[^a-zA-Z0-9]`).ReplaceAllString(key, "_")

				// Check if key is not forbidden
				for _, forbiddenKey := range forbiddenKeys {
					if safeKey == forbiddenKey {
						panic(fmt.Sprintln("Error parsing " + EnvVarPrefix + "LABEL_" + key + " env var, label " + safeKey + " is forbidden"))
					}
				}

				// Add label
				extraLabels[strings.ToLower(safeKey)] = value
			}
		}
	}
	return extraLabels
}

func syncStartCommand(cmd *exec.Cmd, syncServerUrl string, syncStop bool) {

	fmt.Println("Sending start sync at " + syncServerUrl + "/start")
	_, err := http.Post(syncServerUrl+"/start", "text/plain", nil)
	if err != nil {
		fmt.Println("Error sending start sync request:", err)
		os.Exit(1)
	}
	fmt.Println("Start sync done")

	startCommand(cmd)

	if syncStop {
		fmt.Println("Sending stop sync at " + syncServerUrl + "/stop")
		_, err := http.Post(syncServerUrl+"/stop", "text/plain", nil)
		if err != nil {
			fmt.Println("Error sending stop sync request:", err)
			os.Exit(1)
		}
		fmt.Println("Command finished sync ")
	}
}

func waitForHttpSyncToStartCommand(cmd *exec.Cmd, waitForStop bool) {
	// Create mutex
	var mutex = &sync.Mutex{}
	var cmdStarted = false
	var cmdFinished = false

	server := &http.Server{
		Addr: ":8080",
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<html><body><a href="/start">/start</a> : Start the command</body></html>`)
	})

	http.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()

		if cmdStarted {
			w.WriteHeader(http.StatusConflict)
			fmt.Fprintf(w, "KO")
		} else {

			// Start the command in a goroutine
			go func() {
				cmdStarted = true
				startCommand(cmd)
				cmdFinished = true

				if !waitForStop {
					os.Exit(0)
				}
			}()

			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, "OK")
		}
	})

	http.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		if cmdStarted {
			if cmdFinished {
				w.WriteHeader(http.StatusNoContent)
				fmt.Fprintf(w, "Command already finished")
			} else {
				w.WriteHeader(http.StatusAccepted)
				cmd.Process.Signal(os.Interrupt)
				fmt.Fprintf(w, "Command stopped")
			}

			go func() {
				// Create a context with a timeout
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				// Shutdown the server gracefully
				if err := server.Shutdown(ctx); err != nil {
					panic(err)
				}
			}()

		} else {
			w.WriteHeader(http.StatusPreconditionFailed)
			fmt.Fprintf(w, "Command not started yet")
		}
	})
	err := server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		fmt.Println("Error starting the server:", err)
	}
}

type GrafanaAnnotation struct {
	Time    int64    `json:"time"`
	TimeEnd int64    `json:"timeEnd"`
	Text    string   `json:"text"`
	Tags    []string `json:"tags"`
}

func writeAnnotation(annotation GrafanaAnnotation) {
	annotationBuffer, err := json.Marshal(annotation)
	if err != nil {
		fmt.Println("Error marshalling annotation:", err)
		os.Exit(1)
	}
	appendToResultFile("#grafana-annotation " + string(annotationBuffer) + "\n")
}

func startCommand(cmd *exec.Cmd) {
	var err error
	var wg sync.WaitGroup

	realStartTime := time.Now()

	if metricsStartTimeOverride != -1 {
		metricsStartTime = metricsStartTimeOverride
	} else {
		metricsStartTime = realStartTime.UnixMilli()
	}

	// Get instance name from environment variable, or use default (first argument of the command)
	if instanceOverride != "" {
		instance = instanceOverride
	} else {
		instance = cmd.Args[0]
	}

	// Connect the command's standard input/output/error to those of the program
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Channel to signal when to stop gathering metrics
	quit := make(chan struct{})
	defer close(quit)

	// Start gathering metrics in a goroutine we will wait for
	wg.Add(1)
	go func() {
		defer wg.Done()
		startGathering(quit)
	}()

	// Wait before starting the command
	if waitTimeBeforeCommand > 0 {
		time.Sleep(time.Duration(waitTimeBeforeCommand) * time.Second)
	}

	// Catch interrupt signal and forward it to the child process
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT)

	go func() {
		sig := <-sigs
		// Transmettre le signal SIGINT au processus enfant
		if err := cmd.Process.Signal(sig); err != nil {
			panic(err)
		}
	}()

	// Start the command
	err = cmd.Start()
	if err != nil {
		fmt.Println("Error starting command:", err)
		os.Exit(1)
	}

	commandState = CommandStatusRunning

	// Write annotation
	annotationTime := metricsStartTime + time.Now().UnixMilli() - realStartTime.UnixMilli()
	writeAnnotation(GrafanaAnnotation{
		Time:    annotationTime,
		TimeEnd: annotationTime,
		Text:    "Command started",
		Tags: []string{
			"statexec",
			"start",
			"instance=" + instance,
			"job=" + jobName,
			"role=" + role,
		},
	})

	// Wait for the command to finish
	_ = cmd.Wait()

	commandState = CommandStatusDone

	// Write annotation
	annotationTime = metricsStartTime + time.Now().UnixMilli() - realStartTime.UnixMilli()
	writeAnnotation(GrafanaAnnotation{
		Time:    annotationTime,
		TimeEnd: annotationTime,
		Text:    "Command done",
		Tags: []string{
			"statexec",
			"done",
			"instance=" + instance,
			"job=" + jobName,
			"role=" + role,
		},
	})

	// Wait after the command
	if waitTimeAfterCommand > 0 {
		time.Sleep(time.Duration(waitTimeAfterCommand) * time.Second)
	}

	// Signal to stop gathering metrics
	stopGatheringMetrics(quit)

	// Wait for the metrics goroutine to finish
	wg.Wait()
}

// Start gathering metrics with a 1 second interval
func startGathering(quit chan struct{}) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	secondesSinceGatheringStart = 0

	gatherMetrics(secondesSinceGatheringStart)

	stopGatheringNextIteration := false
	for {
		select {
		case <-ticker.C:
			secondesSinceGatheringStart++
			gatherMetrics(secondesSinceGatheringStart)
			if stopGatheringNextIteration {
				return
			}
		case <-quit:
			stopGatheringNextIteration = true
		}
	}
}

func stopGatheringMetrics(quit chan struct{}) {
	quit <- struct{}{}
}

// Generate a string to render labels in prometheus format
func generateLabelRender(metricsLabels map[string]string) string {
	var labelRender []string

	// Static labels
	labelRender = append(labelRender, fmt.Sprintf("instance=\"%s\"", instance))
	labelRender = append(labelRender, fmt.Sprintf("job=\"%s\"", jobName))
	labelRender = append(labelRender, fmt.Sprintf("role=\"%s\"", role))

	// Metrics labels
	for key, value := range metricsLabels {
		labelRender = append(labelRender, fmt.Sprintf("%s=\"%s\"", key, value))
	}

	// Extra labels
	for key, value := range extraLabels {
		labelRender = append(labelRender, fmt.Sprintf("%s=\"%s\"", key, value))
	}
	return strings.Join(labelRender, ",")
}

// Gather metrics
func gatherMetrics(secondesSinceStart int) error {
	timeBeforeGathering := time.Now()
	metricsBuffer := ""
	defaultLabels := generateLabelRender(nil)
	currentTimestamp := metricsStartTime + int64(secondesSinceGatheringStart)*1000

	// Command status

	metricsBuffer += fmt.Sprintf(MetricPrefix+"command_status{%s} %d %d\n", defaultLabels, commandState, currentTimestamp)

	// CPU usage

	cpuMetrics := collectors.CollectCpuMetrics()
	for _, cpuMetric := range cpuMetrics {
		for mode, cpuTime := range cpuMetric.CpuTimePerMode {
			metricLabels := map[string]string{
				"cpu":  cpuMetric.Cpu,
				"mode": mode,
			}
			metricsBuffer += fmt.Sprintf(MetricPrefix+"cpu_seconds_total{%s} %f %d\n", generateLabelRender(metricLabels), cpuTime, currentTimestamp)
		}
	}

	// Memory usage

	memoryMetrics := collectors.CollectMemoryMetrics()
	metricsBuffer += fmt.Sprintf(MetricPrefix+"memory_total_bytes{%s} %d %d\n", defaultLabels, memoryMetrics.Total, currentTimestamp)
	metricsBuffer += fmt.Sprintf(MetricPrefix+"memory_available_bytes{%s} %d %d\n", defaultLabels, memoryMetrics.Available, currentTimestamp)
	metricsBuffer += fmt.Sprintf(MetricPrefix+"memory_used_bytes{%s} %d %d\n", defaultLabels, memoryMetrics.Used, currentTimestamp)
	metricsBuffer += fmt.Sprintf(MetricPrefix+"memory_free_bytes{%s} %d %d\n", defaultLabels, memoryMetrics.Free, currentTimestamp)
	metricsBuffer += fmt.Sprintf(MetricPrefix+"memory_buffers_bytes{%s} %d %d\n", defaultLabels, memoryMetrics.Buffers, currentTimestamp)
	metricsBuffer += fmt.Sprintf(MetricPrefix+"memory_cached_bytes{%s} %d %d\n", defaultLabels, memoryMetrics.Cached, currentTimestamp)
	metricsBuffer += fmt.Sprintf(MetricPrefix+"memory_used_percent{%s} %f %d\n", defaultLabels, memoryMetrics.UsedPercent, currentTimestamp)

	// Network counters

	networkMetrics := collectors.CollectNetworkMetrics()
	for _, networkMetric := range networkMetrics {
		metricLabels := map[string]string{
			"interface": networkMetric.Interface,
		}
		metricsBuffer += fmt.Sprintf(MetricPrefix+"network_sent_bytes_total{%s} %d %d\n", generateLabelRender(metricLabels), networkMetric.SentTotalBytes, currentTimestamp)
		metricsBuffer += fmt.Sprintf(MetricPrefix+"network_received_bytes_total{%s} %d %d\n", generateLabelRender(metricLabels), networkMetric.RecvTotalBytes, currentTimestamp)
	}

	// Disk monitoring

	diskMetrics := collectors.CollectDiskMetrics()
	for _, diskMetric := range diskMetrics {
		metricLabels := map[string]string{
			"disk": diskMetric.Device,
		}
		renderedLabels := generateLabelRender(metricLabels)
		metricsBuffer += fmt.Sprintf(MetricPrefix+"disk_read_bytes_total{%s} %d %d\n", renderedLabels, diskMetric.ReadBytesTotal, currentTimestamp)
		metricsBuffer += fmt.Sprintf(MetricPrefix+"disk_write_bytes_total{%s} %d %d\n", renderedLabels, diskMetric.WriteBytesTotal, currentTimestamp)
	}

	// Self monitoring
	metricsBuffer += fmt.Sprintf(MetricPrefix+"seconds_since_start{%s} %d %d\n", defaultLabels, secondesSinceStart, currentTimestamp)
	metricsBuffer += fmt.Sprintf(MetricPrefix+"metric_generation_duration_ms{%s} %d %d\n", defaultLabels, time.Since(timeBeforeGathering).Abs().Milliseconds(), currentTimestamp)

	// Write metrics to file
	appendToResultFile(metricsBuffer)

	return nil
}
