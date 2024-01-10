# statexec

`statexec` is a versatile command execution tool written in Go that gathers system metrics during the execution of a specified command. It can operate in various modes, including standalone execution or synchronized start and stop with a server. The tool is perfect for performance monitoring and debugging in different environments.

## Features

- **Multiple Execution Modes:** Supports standalone execution, and client-server start/stop synchronization.
- **Metrics Gathering:** Collects and records detailed system metrics, including CPU, memory, and network usage. 
- **Standard format for metrics:** Metrics are written in a file in [OpenMetrics](https://openmetrics.io/) format (Prometheus compatible).
- **Flexible Configuration:** Customizable through environment variables for tailored usage in different scenarios.

## Usage

The general syntax for using `statexec` is:

```bash
statexec <mode> <command>
```

Where `<mode>` can be one of the following:
- `exec`: Execute the command without synchronization.
- `waitStart`: Start an HTTP server and wait for a /start request before executing the command.
- `waitStartAndStop`: Similar to waitStart but also waits for a /stop request to stop the command.
- `syncStart <url>`: Send /start request to the server `<url>` before to start the command.
- `syncStartAndStop <url>`:  Send /start and /stop request to the server `<url>` respectively before the start and after the stop of the command.
  
For more detailed usage instructions, use:

```bash
statexec help
```

## Environment Variables

`statexec` can be configured using the following environment variables:

- `SE_METRICS_FILE`: Path to the file where metrics will be written. (default is `/tmp/statexec_metrics.txt`)
- `SE_INSTANCE`: Override metrics label instance. (default is machine hostname)
- `SE_TIME_RELATIVE`: Defines a custom reference timestamp in milliseconds since the epoch (Unix timestamp). This timestamp is used as a baseline for generating relative timestamps in metrics. If set to `-1` (minus one), the current system time is used as the reference. This feature is particularly useful for synchronizing metrics across multiple instances or aligning them with external events. (Default is `-1`)
- `SE_WAIT_TIME_BEFORE_COMMAND`: Optionnal time in seconds time to wait while wollecting metrics before to start the command. (Default is `0`)
- `SE_WAIT_TIME_AFTER_COMMAND`: Optionnal time in seconds to wait while wollecting metrics after the command has finished. (Default is `0`)
- `SE_LABEL_<key>`: Extra labels to add to all metrics.

## Examples

### Standalone mode

Simple ping command :

```bash
statexec exec ping -c 10 google.com
```

Add a label to all metrics :

```bash
SE_LABEL_env=prod statexec exec grep "ERROR" /var/log/syslog
```

### Synchronized mode

This example demonstrates a synchronized network performance test using `iperf3`, a tool for active measurements of the maximum achievable bandwidth on IP networks. We use `statexec` to start an iperf3 server and client in a synchronized manner, with the server waiting for a start signal and the client synchronized to send this signal. Both sides collect system metrics during the test.

#### Setting up the iperf3 Server

First, we set up the iperf3 server:

```bash
export SE_INSTANCE=localhost
export SE_TIME_RELATIVE=1704067200000 # 2024-01-01 00:00:00 UTC
export SE_LABEL_BENCHMARK=sample 
export SE_WAIT_TIME_AFTER_COMMAND=5 # Monitored Cooldown
go run main.go waitStartAndStop iperf3 -s
```

- `SE_INSTANCE`: Identifies the server instance as 'localhost'.
- `SE_TIME_RELATIVE`: Sets a fixed reference time for metrics, so result will not depend on real start time.
- `SE_LABEL_BENCHMARK`: Adds a custom label to categorize these metrics under label `benchmark=sample`.
- `SE_WAIT_TIME_AFTER_COMMAND`: Configures a cooldown period where metrics continue to be collected for 5 seconds after iperf3 completes its execution.

The waitStartAndStop mode starts an HTTP server on port 8080 and waits for a /start request to initiate the iperf3 server. After the test, it waits for a /stop request.

#### Setting up the iperf3 Client

Next, we configure the iperf3 client:

```bash
export SE_INSTANCE=localhost
export SE_TIME_RELATIVE=1704067200000 # 2024-01-01 00:00:00 UTC
export SE_LABEL_BENCHMARK=sample 
export SE_WAIT_TIME_AFTER_COMMAND=5 # Monitored Cooldown
export SE_WAIT_TIME_BEFORE_COMMAND=2 # Wait for server to start before to start client
go run main.go syncStartAndStop http://localhost:8080 iperf3 -c 127.0.0.1
```

- `SE_WAIT_TIME_BEFORE_COMMAND`: Introduces a delay of 2 seconds while collecting metrics before starting the client, ensuring the server is ready to accept connections.

The syncStartAndStop mode instructs the client to send a /start request to the server (at http://localhost:8080) and initiate the `iperf3` client command. Once the test is over, it sends a /stop request to the server.

This setup ensures both server and client start their respective `iperf3` commands in a coordinated manner, and system metrics are gathered on both sides with synchronized timestamps, allowing for accurate analysis of network performance and system behavior during the test.


### Results

After running `statexec`, the metrics collected during the execution of your command are stored in a designated file. This file provides a detailed record of various system metrics captured during the command's runtime, formatted for compatibility with monitoring and analysis tools.

To give you a better idea of what this output looks like and the type of data statexec gathers, we have included an example metrics file. You can find this file at [example/statexec_metrics.txt](example/statexec_metrics.txt). This example file showcases the format and types of metrics statexec records, such as CPU usage, memory consumption, and network activity, among others. It is a result of the "Synchronized mode" example.

## Exporting and Importing Metrics

### Viewing Collected Metrics

`statexec` stores the collected metrics in a specified file, which by default is `/tmp/statexec_metrics.txt`. To view the contents of this file and the collected metrics, use the following command:

```bash 
cat /tmp/statexec_metrics.txt
```

This command displays the metrics in Prometheus exposition format, which can be easily imported into various monitoring systems.

### Importing Metrics into Victoria Metrics VMsingle

To import the collected metrics into a Victoria Metrics VMsingle instance, use the following curl command. This command sends a POST request to the Victoria Metrics import API, uploading the metrics file:

```bash
curl -v -X POST http://vmsingle:8428/api/v1/import/prometheus -T /tmp/statexec_metrics.txt
```

- `-v`: Verbose mode. Provides additional details about the request and response for debugging purposes.
- `-X POST`: Specifies that the request is a POST request.
http://vmsingle:8428/api/v1/import/prometheus: The URL of the Victoria Metrics import API endpoint.
- `-T`: Indicates that the specified file will be uploaded.

This command will transmit the metrics data to Victoria Metrics, allowing it to be stored, queried, and visualized in the VMsingle system. Ensure that the URL (http://vmsingle:8428) matches the address of your Victoria Metrics VMsingle instance.

## Note on Standard Streams and Interrupt Signal Handling

### Direct Piping of Standard Streams

`statexec` is designed to directly pipe the standard streams (stdin, stdout, and stderr) to the executed command. This means that any input you provide to `statexec` will be directly passed to the command it's running, and any output or error from the command will be displayed as if it were executed directly in the shell. This feature allows for interactive commands or scripts to be run using `statexec`, including starting a shell (e.g., bash, sh).

For example, you can start a shell with `statexec`:

```bash
go run main.go exec bash
```

In this mode, `statexec` will behave as if you're directly interacting with the bash shell, with the added benefit of metric collection in the background.

### Forwarding the Interrupt Signal

Additionally, `statexec` handles the interrupt signal (SIGINT, commonly triggered by `Ctrl+C`) by forwarding it to the command being executed. This means that if you send an interrupt signal to `statexec`, it will gracefully pass this signal to the child process (the command it is running). This is particularly useful for stopping long-running processes or scripts gracefully.

For instance, if `statexec` is used to run a long-running server or a continuous process, hitting `Ctrl+C` will send the interrupt signal to that process, allowing it to terminate cleanly. This ensures that `statexec` does not interfere with the standard way of stopping commands and allows for a seamless integration into existing workflows.



## About BlackSwift

Based in France, [BlackSwift](https://blackswift.fr) is a company dedicated to simplifying cloud infrastructure management. Our primary offering is Kubernetes Namespaces as a Service, which includes essential features like monitoring, logs, and backups to streamline cloud operations for our clients.

The development of `statexec` emerged from our own needs to conduct effective benchmarks and debugging within our services. As a tool designed for executing commands and collecting system metrics, `statexec` has become integral to our process of optimizing and maintaining the quality of our Kubernetes environments.

True to our ethos of community and collaboration, we're pleased to share statexec with others. We believe in the power of shared knowledge and tools, especially in the ever-evolving world of cloud computing.


## License
This project is licensed under the MIT License - see the [LICENSE.md](LICENSE.md) file for details.
