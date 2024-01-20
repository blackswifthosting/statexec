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
	version        = "dev"
	jobName string = "statexec"

	metricsFile              string = ""
	metricsStartTimeOverride int64  = -1 // in milliseconds
	delayBeforeCommand       int64  = 0
	delayAfterCommand        int64  = 0
	instanceOverride         string = ""

	role            string = "standalone"
	serverIp        string = ""
	syncPort        string = "8080"
	syncWaitForStop bool   = true

	extraLabels map[string]string

	metricsStartTime int64 // in milliseconds
	instance         string
	commandState     int = 0

	metricStore     []InstantMetric
	annotationStore []GrafanaAnnotation
)

const (
	EnvVarPrefix string = "SE_"
	MetricPrefix string = "statexec_"

	CommandStatusPending int = 0
	CommandStatusRunning int = 1
	CommandStatusDone    int = 2

	ModeStandalone int = 0
	ModeLeader     int = 1
	ModeFollower   int = 2
)

type GrafanaAnnotation struct {
	Time    int64    `json:"time"`
	TimeEnd int64    `json:"timeEnd"`
	Text    string   `json:"text"`
	Tags    []string `json:"tags"`
}

type InstantMetric struct {
	cmdStatus       int
	cpu             []collectors.CpuMetrics
	memory          collectors.MemoryMetrics
	network         []collectors.NetworkMetrics
	disk            []collectors.DiskMetrics
	msSinceStart    int64
	collectDuration int64
	timestamp       int64
}

func main() {
	// Default values
	metricsFile = jobName + "_metrics.prom"

	// Initialize extra labels to an empty map
	extraLabels = make(map[string]string)

	// Parse environment variables
	parseEnvVars()

	// Parse command line arguments
	cmd := parseArgs()

	// Override instance name if set, else use command name
	if instanceOverride != "" {
		instance = instanceOverride
	} else {
		instance = cmd[0]
	}

	// Create command to execute
	execCmd := exec.Command(cmd[0], cmd[1:]...)

	// Start statexec in the right mode
	switch role {
	case "standalone":
		startCommand(execCmd)
	case "client":
		syncStartCommand(execCmd, fmt.Sprintf("http://%s:%s", serverIp, syncPort), syncWaitForStop)
	case "server":
		waitForHttpSyncToStartCommand(execCmd, syncWaitForStop)
	}
}

func usage() {
	binself := os.Args[0]
	fmt.Printf("Usage: %s [OPTIONS] <command> [command args]\n", binself)
	fmt.Printf("Version: %s\n", version)
	fmt.Println("")
	fmt.Printf("Common options:\n")
	fmt.Printf("  --file, -f <file>                       %sFILE                 Metrics file (default: statexec_metrics.prom)\n", EnvVarPrefix)
	fmt.Printf("  --instance, -i <instance>               %sINSTANCE             Instance name (default: <command>)\n", EnvVarPrefix)
	fmt.Printf("  --metrics-start-time, -mst <timestamp>  %sMETRICS_START_TIME   Metrics start time in milliseconds (default: now)\n", EnvVarPrefix)
	fmt.Printf("  --delay, -d <seconds>                   %sDELAY                Delay in seconds before and after the command (default: 0)\n", EnvVarPrefix)
	fmt.Printf("  --delay-before-command, -dbc <seconds>  %sDELAY_BEFORE_COMMAND Delay in seconds  before the command (default: 0)\n", EnvVarPrefix)
	fmt.Printf("  --delay-after-command, -dac <seconds>   %sDELAY_AFTER_COMMAND  Delay in seconds  after the command (default: 0)\n", EnvVarPrefix)
	fmt.Printf("  --label, -l <key>=<value>               %sLABEL_<key>          Extra label to add to all metrics (no default)\n", EnvVarPrefix)
	fmt.Printf("Synchronization options:\n")
	fmt.Printf("  --server, -s               %s                   Start server mode (no default)\n", strings.Repeat(" ", len(EnvVarPrefix)))
	fmt.Printf("  --connect, -c <ip>         %sCONNECT            Connect to server on <ip> (no default)\n", EnvVarPrefix)
	fmt.Printf("  --sync-port, -sp <port>    %sSYNC_PORT          Sync port (default: 8080)\n", EnvVarPrefix)
	fmt.Printf("  --sync-start-only, -sso    %sSYNC_START_ONLY    Sync start only (default: false)\n", EnvVarPrefix)
	fmt.Println("Other options:")
	fmt.Printf("  --version, -v        Print version and exit\n")
	fmt.Printf("  --help, -help, -h    Print help and exit\n")
	fmt.Printf("  --                   Stop parsing arguments\n")
	fmt.Println("")
	fmt.Println("Standalone examples:")
	fmt.Printf("  %s ping 8.8.8.8 -c 4\n", binself)
	fmt.Printf("  %sFILE=data.prom %sLABEL_type=sample %s -d 3 -l env=dev -- ./mycommand.sh arg1 arg2\n", EnvVarPrefix, EnvVarPrefix, binself)
	fmt.Println("")
	fmt.Println("Sync mode examples:")
	fmt.Println("  # Wait for a client sync to start the command")
	fmt.Printf("  %s -s -- date\n", binself)
	fmt.Println("  # Connect to server on <localhost> to start and stop the command")
	fmt.Printf("  %s -c localhost -- echo start date now\n", binself)
}

func parseArgs() []string {
	var err error
	cmd := []string{}

	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "-f", "--file":
			metricsFile = os.Args[i+1]
			i++

		case "-i", "--instance":
			instanceOverride = os.Args[i+1]
			i++

		case "-mst", "--metrics-start-time":
			metricsStartTimeOverride, err = strconv.ParseInt(os.Args[i+1], 10, 64)
			if err != nil {
				fmt.Println("Error parsing metrics time override:", err)
				os.Exit(1)
			}
			i++

		case "-c", "--connect":
			if role == "server" {
				fmt.Println("Error: server and client modes are mutually exclusive")
				os.Exit(1)
			}
			role = "client"
			serverIp = os.Args[i+1]
			i++
		case "-s", "--server":
			if role == "client" {
				fmt.Println("Error: server and client modes are mutually exclusive")
				os.Exit(1)
			}
			role = "server"

		case "-sp", "--sync-port":
			syncPort = os.Args[i+1]
			i++
		case "-sso", "--sync-start-only":
			syncWaitForStop = false

		// Delay in seconds
		case "-d", "--delay":
			timeToWaitInScd, err := strconv.ParseInt(os.Args[i+1], 10, 64)
			if err != nil {
				fmt.Println("Error parsing wait time:", err)
				os.Exit(1)
			}
			delayBeforeCommand = timeToWaitInScd
			delayAfterCommand = timeToWaitInScd
			i++
		case "-dbc", "--delay-before-command":
			timeToWaitInMs, err := strconv.ParseInt(os.Args[i+1], 10, 64)
			if err != nil {
				fmt.Println("Error parsing wait time:", err)
				os.Exit(1)
			}
			delayBeforeCommand = timeToWaitInMs
			i++
		case "-dac", "--delay-after-command":
			timeToWaitInMs, err := strconv.ParseInt(os.Args[i+1], 10, 64)
			if err != nil {
				fmt.Println("Error parsing wait time:", err)
				os.Exit(1)
			}
			delayAfterCommand = timeToWaitInMs
			i++

		// Extra labels
		case "-l", "--label":
			parts := strings.SplitN(os.Args[i+1], "=", 2)
			if len(parts) == 2 {
				addLabel(parts[0], parts[1])
			} else {
				fmt.Println("Error parsing label:", os.Args[i+1])
				os.Exit(1)
			}
			i++

		case "-v", "--version":
			fmt.Println(version)
			os.Exit(0)
		case "-h", "-help", "--help":
			usage()
			os.Exit(0)
		case "--":
			cmd = os.Args[i+1:]
			i = len(os.Args)
		default:
			cmd = os.Args[i:]
			i = len(os.Args)
		}
	}
	return cmd
}

func parseEnvVars() {
	var err error
	// Metrics file (-f, --file)
	if value := os.Getenv(EnvVarPrefix + "FILE"); value != "" {
		metricsFile = value
	}

	// Instance name (-i, --instance)
	if value := os.Getenv(EnvVarPrefix + "INSTANCE"); value != "" {
		instanceOverride = value
	}

	// Metrics start time (-mst, --metrics-start-time)
	if value := os.Getenv(EnvVarPrefix + "METRICS_START_TIME"); value != "" {
		metricsStartTimeOverride, err = strconv.ParseInt(value, 10, 64)
		if err != nil {
			fmt.Println("Error parsing "+EnvVarPrefix+"METRICS_START_TIME env var, must be an int64 (timestamp in ms since epoch), found : ", value)
			os.Exit(1)
		}
	}

	// Connect to server (-c, --connect)
	if value := os.Getenv(EnvVarPrefix + "CONNECT"); value != "" {
		if role == "server" {
			fmt.Println("Error: server and client modes are mutually exclusive")
			os.Exit(1)
		}
		role = "client"
		serverIp = value
	}

	// Start server (-s, --server)
	if value := os.Getenv(EnvVarPrefix + "SERVER"); value != "" {
		if role == "client" {
			fmt.Println("Error: server and client modes are mutually exclusive")
			os.Exit(1)
		}
		role = "server"
	}

	// Sync port (-sp, --sync-port)
	if value := os.Getenv(EnvVarPrefix + "SYNC_PORT"); value != "" {
		syncPort = value
	}

	// Sync start only (-sso, --sync-start-only)
	if value := os.Getenv(EnvVarPrefix + "SYNC_START_ONLY"); value != "" {
		// If value of syncStartOnly is "true", set syncWaitForStop to false
		if value == "true" {
			syncWaitForStop = false
		}
	}

	// Delay in seconds (-d, --delay)
	if value := os.Getenv(EnvVarPrefix + "DELAY"); value != "" {
		timeToWaitInScd, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			fmt.Println("Error parsing "+EnvVarPrefix+"DELAY env var, must be an int64 (time in ms), found : ", value)
			os.Exit(1)
		}
		delayBeforeCommand = timeToWaitInScd
		delayAfterCommand = timeToWaitInScd
	}

	// Delay before command in seconds (-dbc, --delay-before-command)
	if value := os.Getenv(EnvVarPrefix + "DELAY_BEFORE_COMMAND"); value != "" {
		timeToWaitInScd, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			fmt.Println("Error parsing "+EnvVarPrefix+"DELAY_BEFORE_COMMAND env var, must be an int64 (time in ms), found : ", value)
			os.Exit(1)
		}
		delayBeforeCommand = timeToWaitInScd
	}

	// Delay after command in seconds (-dac, --delay-after-command)
	if value := os.Getenv(EnvVarPrefix + "DELAY_AFTER_COMMAND"); value != "" {
		timeToWaitInScd, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			fmt.Println("Error parsing "+EnvVarPrefix+"DELAY_AFTER_COMMAND env var, must be an int64 (time in ms), found : ", value)
			os.Exit(1)
		}
		delayAfterCommand = timeToWaitInScd
	}

	// Get extra labels from environment variables (-l, --label)
	parseExtraLabelsFromEnv()
}

func addLabel(key string, value string) {
	// List of forbidden label names
	forbiddenKeys := []string{"instance", "job", "cpu", "mode", "interface"}

	// Replace non-alphanumeric characters with underscores
	safeKey := regexp.MustCompile(`[^a-zA-Z0-9]`).ReplaceAllString(key, "_")

	// Check if key is not forbidden
	for _, forbiddenKey := range forbiddenKeys {
		if safeKey == forbiddenKey {
			fmt.Printf("Override label %s is forbidden", key)
			os.Exit(1)
		}
	}

	extraLabels[strings.ToLower(safeKey)] = value
}

func parseExtraLabelsFromEnv() map[string]string {
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, EnvVarPrefix+"LABEL_") {
			parts := strings.Split(env, "=")
			if len(parts) == 2 {
				key := strings.TrimPrefix(parts[0], EnvVarPrefix+"LABEL_")
				value := parts[1]
				addLabel(key, value)
			} else {
				fmt.Println("Error parsing label of ENV :", env)
				os.Exit(1)
			}
		}
	}
	return extraLabels
}

func syncStartCommand(cmd *exec.Cmd, syncServerUrl string, syncStop bool) {

	// Sending start sync at server
	_, err := http.Post(syncServerUrl+"/start", "text/plain", nil)
	if err != nil {
		fmt.Println("Error sending start sync request:", err)
		os.Exit(1)
	}

	// Start the command
	startCommand(cmd)

	// Check if we need to sync the stop to the server
	if syncStop {
		// Sending stop sync at server
		_, err := http.Post(syncServerUrl+"/stop", "text/plain", nil)
		if err != nil {
			fmt.Println("Error sending stop sync request:", err)
			os.Exit(1)
		}
	}
}

func waitForHttpSyncToStartCommand(cmd *exec.Cmd, waitForStop bool) {
	// Create mutex
	var mutex = &sync.Mutex{}
	var wg sync.WaitGroup
	var cmdStarted = false
	var cmdFinished = false

	server := &http.Server{
		Addr: ":" + syncPort,
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
			wg.Add(1)
			// Start the command in a goroutine
			go func() {
				cmdStarted = true
				startCommand(cmd)
				cmdFinished = true
				wg.Done()

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

				wg.Wait()

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
		os.Exit(1)
	}
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
		startMetricCollectLoop(quit)
	}()

	// Wait before starting the command
	if delayBeforeCommand > 0 {
		time.Sleep(time.Duration(delayBeforeCommand) * time.Second)
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
	commandStartedAtTime := time.Now().UnixMilli() - realStartTime.UnixMilli()
	collectInstantMetrics(commandStartedAtTime)

	// Annotate the command start
	annotationStore = append(annotationStore, GrafanaAnnotation{
		Time:    commandStartedAtTime,
		TimeEnd: commandStartedAtTime,
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
	commandFinishedAtTime := time.Now().UnixMilli() - realStartTime.UnixMilli()
	collectInstantMetrics(commandFinishedAtTime)

	// Annotate the command end
	annotationStore = append(annotationStore, GrafanaAnnotation{
		Time:    commandFinishedAtTime,
		TimeEnd: commandFinishedAtTime,
		Text:    "Command done with status " + strconv.Itoa(cmd.ProcessState.ExitCode()),
		Tags: []string{
			"statexec",
			"done",
			"instance=" + instance,
			"job=" + jobName,
			"role=" + role,
		},
	})

	// Wait after the command
	if delayAfterCommand > 0 {
		time.Sleep(time.Duration(delayAfterCommand) * time.Second)
	}

	// Signal to stop gathering metrics
	stopCollectingMetrics(quit)

	// Wait for the metrics goroutine to finish
	wg.Wait()
}

// Start gathering metrics with a 1 second interval
func startMetricCollectLoop(quit chan struct{}) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var msSinceStart int64 = 0

	collectInstantMetrics(msSinceStart)

	stopGatheringNextIteration := false
	for {
		select {
		case <-ticker.C:
			msSinceStart += 1000
			collectInstantMetrics(msSinceStart)
			if stopGatheringNextIteration {
				writeResultToFile()
				return
			}
		case <-quit:
			stopGatheringNextIteration = true
		}
	}
}

func stopCollectingMetrics(quit chan struct{}) {
	quit <- struct{}{}
}

// Generate a string to render labels in prometheus format
func renderLabels(metricsLabels map[string]string) string {
	var result []string

	// Static labels
	result = append(result, fmt.Sprintf("instance=\"%s\"", instance))
	result = append(result, fmt.Sprintf("job=\"%s\"", jobName))
	result = append(result, fmt.Sprintf("role=\"%s\"", role))

	// Metrics labels
	for key, value := range metricsLabels {
		result = append(result, fmt.Sprintf("%s=\"%s\"", key, value))
	}

	// Extra labels
	for key, value := range extraLabels {
		result = append(result, fmt.Sprintf("%s=\"%s\"", key, value))
	}
	return strings.Join(result, ",")
}

// Gather metrics
func collectInstantMetrics(msSinceStart int64) {
	timeBeforeGathering := time.Now()
	currentTimestamp := metricsStartTime + msSinceStart

	instantMetric := InstantMetric{
		cmdStatus:    commandState,
		cpu:          collectors.CollectCpuMetrics(),
		memory:       collectors.CollectMemoryMetrics(),
		network:      collectors.CollectNetworkMetrics(),
		disk:         collectors.CollectDiskMetrics(),
		msSinceStart: msSinceStart,
		timestamp:    currentTimestamp,
	}
	instantMetric.collectDuration = time.Since(timeBeforeGathering).Milliseconds()

	// Add metric to store
	metricStore = append(metricStore, instantMetric)
}

func writeResultToFile() error {
	defaultLabels := renderLabels(nil)

	// Delete metrics file
	_ = os.Remove(metricsFile)

	// Open metrics file in append mode
	resultFile, err := os.OpenFile(metricsFile, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Error opening metrics file:", err)
		os.Exit(1)
	}
	defer resultFile.Close()

	urlSuffix := ""
	if version != "dev" {
		urlSuffix = "tree/" + version
	}
	commentBlock := `
# Collector: blackswift/statexec
# Version: ` + version + `
# Url: https://github.com/blackswifthosting/statexec/` + urlSuffix + `

# HELP statexec_command_status Status of the command (0: pending, 1: running, 2: done)
# TYPE statexec_command_status gauge
# HELP statexec_cpu_seconds_total CPU time spent in seconds
# TYPE statexec_cpu_seconds_total counter
# HELP statexec_memory_total_bytes Total memory in bytes
# TYPE statexec_memory_total_bytes gauge
# HELP statexec_memory_available_bytes Available memory in bytes
# TYPE statexec_memory_available_bytes gauge
# HELP statexec_memory_used_bytes Used memory in bytes
# TYPE statexec_memory_used_bytes gauge
# HELP statexec_memory_free_bytes Free memory in bytes
# TYPE statexec_memory_free_bytes gauge
# HELP statexec_memory_buffers_bytes Memory buffers in bytes
# TYPE statexec_memory_buffers_bytes gauge
# HELP statexec_memory_cached_bytes Memory cached in bytes
# TYPE statexec_memory_cached_bytes gauge
# HELP statexec_memory_used_percent Used memory in percent
# TYPE statexec_memory_used_percent gauge
# HELP statexec_network_sent_bytes_total Total sent bytes
# TYPE statexec_network_sent_bytes_total counter
# HELP statexec_network_received_bytes_total Total received bytes
# TYPE statexec_network_received_bytes_total counter
# HELP statexec_disk_read_bytes_total Total read bytes
# TYPE statexec_disk_read_bytes_total counter
# HELP statexec_disk_write_bytes_total Total written bytes
# TYPE statexec_disk_write_bytes_total counter
# HELP statexec_time_since_start_ms Milliseconds since monitoring start
# TYPE statexec_time_since_start_ms gauge
# HELP statexec_metric_collect_duration_ms Duration of the metric collection in milliseconds
# TYPE statexec_metric_collect_duration_ms gauge

`
	if _, err := resultFile.WriteString(commentBlock); err != nil {
		fmt.Println("Error writing to metrics file:", err)
		os.Exit(1)
	}

	// ====== Write annotation to file ======
	annotationsBuffer := ""
	for _, annotation := range annotationStore {

		annotationJson, err := json.Marshal(annotation)
		if err != nil {
			fmt.Println("Error marshalling annotation:", err)
			os.Exit(1)
		}

		annotationsBuffer += "#grafana-annotation " + string(annotationJson) + "\n"
	}
	annotationsBuffer += "\n"
	if _, err := resultFile.WriteString(annotationsBuffer); err != nil {
		fmt.Println("Error writing to metrics file:", err)
		os.Exit(1)
	}

	// ====== Write metrics to file ======
	for _, metric := range metricStore {
		metricsBuffer := ""

		// Command status
		metricsBuffer += fmt.Sprintf(MetricPrefix+"command_status{%s} %d %d\n", defaultLabels, metric.cmdStatus, metric.timestamp)

		// CPU usage
		for _, cpuMetric := range metric.cpu {
			for mode, cpuTime := range cpuMetric.CpuTimePerMode {
				metricLabels := map[string]string{
					"cpu":  cpuMetric.Cpu,
					"mode": mode,
				}
				metricsBuffer += fmt.Sprintf(MetricPrefix+"cpu_seconds_total{%s} %f %d\n", renderLabels(metricLabels), cpuTime, metric.timestamp)
			}
		}

		// Memory usage
		metricsBuffer += fmt.Sprintf(MetricPrefix+"memory_total_bytes{%s} %d %d\n", defaultLabels, metric.memory.Total, metric.timestamp)
		metricsBuffer += fmt.Sprintf(MetricPrefix+"memory_available_bytes{%s} %d %d\n", defaultLabels, metric.memory.Available, metric.timestamp)
		metricsBuffer += fmt.Sprintf(MetricPrefix+"memory_used_bytes{%s} %d %d\n", defaultLabels, metric.memory.Used, metric.timestamp)
		metricsBuffer += fmt.Sprintf(MetricPrefix+"memory_free_bytes{%s} %d %d\n", defaultLabels, metric.memory.Free, metric.timestamp)
		metricsBuffer += fmt.Sprintf(MetricPrefix+"memory_buffers_bytes{%s} %d %d\n", defaultLabels, metric.memory.Buffers, metric.timestamp)
		metricsBuffer += fmt.Sprintf(MetricPrefix+"memory_cached_bytes{%s} %d %d\n", defaultLabels, metric.memory.Cached, metric.timestamp)
		metricsBuffer += fmt.Sprintf(MetricPrefix+"memory_used_percent{%s} %f %d\n", defaultLabels, metric.memory.UsedPercent, metric.timestamp)

		// Network counters
		for _, networkMetric := range metric.network {
			metricLabels := map[string]string{
				"interface": networkMetric.Interface,
			}
			metricsBuffer += fmt.Sprintf(MetricPrefix+"network_sent_bytes_total{%s} %d %d\n", renderLabels(metricLabels), networkMetric.SentTotalBytes, metric.timestamp)
			metricsBuffer += fmt.Sprintf(MetricPrefix+"network_received_bytes_total{%s} %d %d\n", renderLabels(metricLabels), networkMetric.RecvTotalBytes, metric.timestamp)
		}

		// Disk monitoring
		for _, diskMetric := range metric.disk {
			metricLabels := map[string]string{
				"disk": diskMetric.Device,
			}
			renderedLabels := renderLabels(metricLabels)
			metricsBuffer += fmt.Sprintf(MetricPrefix+"disk_read_bytes_total{%s} %d %d\n", renderedLabels, diskMetric.ReadBytesTotal, metric.timestamp)
			metricsBuffer += fmt.Sprintf(MetricPrefix+"disk_write_bytes_total{%s} %d %d\n", renderedLabels, diskMetric.WriteBytesTotal, metric.timestamp)
		}

		// Self monitoring
		metricsBuffer += fmt.Sprintf(MetricPrefix+"statexec_time_since_start_ms{%s} %d %d\n", defaultLabels, metric.msSinceStart, metric.timestamp)
		metricsBuffer += fmt.Sprintf(MetricPrefix+"metric_collect_duration_ms{%s} %d %d\n", defaultLabels, metric.collectDuration, metric.timestamp)

		// Write metrics to file
		if _, err := resultFile.WriteString(metricsBuffer); err != nil {
			fmt.Println("Error writing to metrics file:", err)
			os.Exit(1)
		}
	}

	return nil
}
