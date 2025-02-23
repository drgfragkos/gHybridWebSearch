package main

import (
    "bufio"
    "context"
    "flag"
    "fmt"
    "log"
    "net/http"
    "os"
    "os/signal"
    "strings"
    "sync"
    "syscall"
    "time"
)

/*
---------------------------------------------------------------------------------------
gHybridWebSearch+ (Go version) - with Graceful Shutdown
(c) Based on original gHybridWebSearch by @drgfragkos, updated by <YourName/YourHandle>.

PURPOSE:
  - Quickly probe a target host for various files/paths (using a custom dictionary file).
  - Identify old, backup, or unreferenced files that might be interesting or sensitive.
  - Logs all requests, highlights 200 OK, and excludes 404 from one of the logs.

KEY FEATURES:
  - HEAD or GET requests: Decide whether to retrieve the response body or just headers.
  - Concurrency: Multiple requests in parallel for speed.
  - Timeout: Prevents hanging on slow/unresponsive servers.
  - Graceful Shutdown: Responds to CTRL+C (SIGINT) by terminating ongoing scans gracefully.

FILES PRODUCED:
  1. .log.dat         - Raw log of each request with status line or error.
  2. output-200.txt   - Only lines with "200 OK".
  3. output-ex404.txt - All lines NOT containing "404 Not Found" (e.g., 200, 403, 500, etc.).

PERFORMANCE CONSIDERATIONS:
  - Concurrency (`-c`): Higher concurrency increases speed but can overload your system or the remote host.
  - Timeouts: The default 5-second timeout can be raised or lowered depending on network conditions.
  - Dictionary Size: Large dictionaries might produce many requests. Ensure you have enough concurrency
    (but not too high) and that your system resources can handle it.

USAGE EXAMPLES:
  # 1) Basic usage: HEAD requests, port 80, concurrency=10, default dictionary
  go run gHybridWebSearchPlus.go -h www.example.com

  # 2) Custom port (8080), concurrency=20, using GET
  go run gHybridWebSearchPlus.go -h www.example.com -p 8080 -c 20 -t GET

  # 3) Use a specific dictionary file
  go run gHybridWebSearchPlus.go -h mysite.com -d customDictionary.txt

FLAGS:
  -h, --host          (required)   The target hostname or IP address
  -p, --port          (80)         Port to connect to
  -d, --dict          (hybridWebSearch.dic)  Dictionary file
  -c, --concurrency   (10)         Number of concurrent workers
  -t, --type          (HEAD)       HTTP method: HEAD or GET
---------------------------------------------------------------------------------------
*/

func main() {
    // Create a context that cancels on SIGINT (CTRL+C) or SIGTERM
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Listen for OS signals (CTRL+C, SIGTERM)
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

    // If we get an interrupt, cancel the context to trigger a graceful shutdown
    go func() {
        <-sigChan
        fmt.Println("\n[!] Received interrupt signal. Attempting graceful shutdown...")
        cancel()
    }()

    // Parse command-line flags
    hostFlag := flag.String("host", "", "Target host (required)")
    flag.StringVar(hostFlag, "h", "", "Target host (short)")

    portFlag := flag.Int("port", 80, "Port to connect to")
    flag.IntVar(portFlag, "p", 80, "Port to connect to (short)")

    dictFlag := flag.String("dict", "hybridWebSearch.dic", "Dictionary file")
    flag.StringVar(dictFlag, "d", "hybridWebSearch.dic", "Dictionary file (short)")

    concurrencyFlag := flag.Int("concurrency", 10, "Number of concurrent workers")
    flag.IntVar(concurrencyFlag, "c", 10, "Number of concurrent workers (short)")

    methodFlag := flag.String("type", "HEAD", "HTTP method: HEAD or GET")
    flag.StringVar(methodFlag, "t", "HEAD", "HTTP method (short)")

    flag.Parse()

    // Validate required host
    if *hostFlag == "" {
        log.Fatal("[Error] Host (-h or --host) is required. Example: -h www.example.com")
    }

    // Attempt to open the dictionary file
    dictFile, err := os.Open(*dictFlag)
    if err != nil {
        log.Fatalf("[Error] Cannot open dictionary file %q: %v", *dictFlag, err)
    }
    defer dictFile.Close()

    // Prepare output files
    logFile, err := os.Create(".log.dat")
    if err != nil {
        log.Fatalf("[Error] Cannot create .log.dat: %v", err)
    }
    defer logFile.Close()

    out200, err := os.Create("output-200.txt")
    if err != nil {
        log.Fatalf("[Error] Cannot create output-200.txt: %v", err)
    }
    defer out200.Close()

    outEx404, err := os.Create("output-ex404.txt")
    if err != nil {
        log.Fatalf("[Error] Cannot create output-ex404.txt: %v", err)
    }
    defer outEx404.Close()

    // Setup an HTTP client with a timeout
    client := &http.Client{
        Timeout: 5 * time.Second, // Adjust if needed for slower networks
    }

    // Channels for work distribution
    linesChan := make(chan string, 100)
    resultsChan := make(chan string, 100)

    // Worker pool wait group
    var wg sync.WaitGroup

    // Start workers
    for i := 0; i < *concurrencyFlag; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            // Each worker pulls paths from linesChan until it's closed or context is canceled
            for {
                select {
                case <-ctx.Done():
                    // Context canceled (CTRL+C or other signal) => exit worker
                    return
                case path, ok := <-linesChan:
                    if !ok {
                        // linesChan is closed => no more work
                        return
                    }
                    processPath(ctx, path, *hostFlag, *portFlag, *methodFlag, client, resultsChan)
                }
            }
        }()
    }

    // Collector goroutine (handles results, logs, etc.)
    var collectorWg sync.WaitGroup
    collectorWg.Add(1)
    go func() {
        defer collectorWg.Done()
        for res := range resultsChan {
            // Write every result to .log.dat
            _, _ = logFile.WriteString(res + "\n")

            // 200's go to output-200.txt
            if strings.Contains(strings.ToLower(res), "200 ok") {
                _, _ = out200.WriteString(res + "\n")
            }

            // Everything except "404 Not Found" goes to output-ex404.txt
            if !strings.Contains(strings.ToLower(res), "404 not found") {
                _, _ = outEx404.WriteString(res + "\n")
            }
        }
    }()

    // Dictionary-reading goroutine:
    // Reads lines from the dictionary file and sends them to linesChan.
    // If context is canceled, it stops reading and closes linesChan.
    go func() {
        scanner := bufio.NewScanner(dictFile)
        for scanner.Scan() {
            line := strings.TrimSpace(scanner.Text())
            if line == "" {
                continue // skip empty lines
            }
            select {
            case <-ctx.Done():
                // Gracefully stop if interrupted
                break
            case linesChan <- line:
                // normal case
            }
            // If context is canceled while blocked on linesChan, we break out
            if ctx.Err() != nil {
                break
            }
        }

        // Close linesChan so workers know there are no more lines
        close(linesChan)
    }()

    // Wait for the workers to finish
    wg.Wait()

    // Once all workers are done, no more results will come, so close resultsChan
    close(resultsChan)

    // Wait for the collector to finish
    collectorWg.Wait()

    fmt.Println("[*] Scan completed (or interrupted).")
    fmt.Println("[*] Results:")
    fmt.Println("    .log.dat         (Raw log of all requests)")
    fmt.Println("    output-200.txt   (Endpoints returning 200 OK)")
    fmt.Println("    output-ex404.txt (Endpoints not returning 404)")
}

func processPath(
    ctx context.Context,
    path string,
    host string,
    port int,
    method string,
    client *http.Client,
    resultsChan chan<- string,
) {
    url := fmt.Sprintf("http://%s:%d/%s", host, port, path)

    var (
        resp       *http.Response
        err        error
        statusLine string
    )

    // Decide GET vs HEAD (default HEAD)
    switch strings.ToUpper(method) {
    case "GET":
        resp, err = client.Get(url)
    default:
        resp, err = client.Head(url)
    }

    if err != nil {
        statusLine = fmt.Sprintf("%s\t---ERROR: %v", path, err)
        select {
        case <-ctx.Done():
            return
        case resultsChan <- statusLine:
        }
        return
    }
    defer resp.Body.Close()

    // Construct "status line" for logging
    // e.g. "somePath  HTTP/1.1 200 OK"
    statusLine = fmt.Sprintf("%s\tHTTP/%d.%d %d %s",
        path,
        resp.ProtoMajor,
        resp.ProtoMinor,
        resp.StatusCode,
        http.StatusText(resp.StatusCode),
    )

    // Send the result
    select {
    case <-ctx.Done():
        return
    case resultsChan <- statusLine:
    }
}
