package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	Reset      = "\033[0m"
	LightGreen = "\033[92m"
	Red        = "\033[91m"
	Yellow     = "\033[93m"
)

// sql error pattern
var sqlErrorPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)mysql_fetch`),
	regexp.MustCompile(`(?i)sql syntax`),
	regexp.MustCompile(`(?i)mysql error`),
	regexp.MustCompile(`(?i)unclosed quotation`),
	regexp.MustCompile(`(?i)unknown column`),
	regexp.MustCompile(`(?i)sql server`),
	regexp.MustCompile(`(?i)sqlite3`),
	regexp.MustCompile(`(?i)postgres`),
}

type RequestResult struct {
	Success      bool
	URL          string
	ResponseTime float64
	ErrorMsg     string
	Body         string
	BodySize     int
	IsSQLi       bool
	SQLiType     string // "time-based", "error-based", "none"
	BaselineTime float64
	BaselineSize int // For response size comparison
}

func clearScreen() {
	cmd := exec.Command("clear") // Linux
	cmd.Stdout = os.Stdout
	cmd.Run()
}

func performRequest(url, payload, cookie string, timeout time.Duration) RequestResult {
	urlWithPayload := url + payload
	startTime := time.Now()

	client := &http.Client{
		Timeout: timeout,
	}

	req, err := http.NewRequest("GET", urlWithPayload, nil)
	if err != nil {
		return RequestResult{
			Success:      false,
			URL:          urlWithPayload,
			ResponseTime: time.Since(startTime).Seconds(),
			ErrorMsg:     err.Error(),
		}
	}

	if cookie != "" {
		req.Header.Add("Cookie", cookie)
	}

	resp, err := client.Do(req)
	responseTime := time.Since(startTime).Seconds()

	if err != nil {
		return RequestResult{
			Success:      false,
			URL:          urlWithPayload,
			ResponseTime: responseTime,
			ErrorMsg:     err.Error(),
		}
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return RequestResult{
			Success:      false,
			URL:          urlWithPayload,
			ResponseTime: responseTime,
			ErrorMsg:     err.Error(),
		}
	}

	body := string(bodyBytes)
	return RequestResult{
		Success:      true,
		URL:          urlWithPayload,
		ResponseTime: responseTime,
		Body:         body,
		BodySize:     len(body),
	}
}

func getBaseline(url, cookie string, timeout time.Duration) (float64, int, error) {
	result := performRequest(url, "", cookie, timeout)
	if !result.Success {
		return 0, 0, fmt.Errorf("baseline request failed: %s", result.ErrorMsg)
	}
	return result.ResponseTime, result.BodySize, nil
}

func analyzeSQLi(result RequestResult, baselineTime float64, baselineSize int) RequestResult {
	timeThreshold := baselineTime * 3
	if result.ResponseTime >= timeThreshold && result.ResponseTime >= 5 { // Time-based detection
		result.IsSQLi = true
		result.SQLiType = "time-based"
		return result
	}

	// Error-based detection
	for _, pattern := range sqlErrorPatterns {
		if pattern.MatchString(result.Body) {
			result.IsSQLi = true
			result.SQLiType = "error-based"
			return result
		}
	}

	if baselineSize > 0 && (result.BodySize < baselineSize/2 || result.BodySize > baselineSize*2) {
		result.IsSQLi = true
		result.SQLiType = "anomaly-based"
		return result
	}

	result.SQLiType = "none"
	return result
}

func printBanner() {
	banner := []string{
		"       ░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓███████▓▒░ ░▒▓██████▓▒░░▒▓█▓▒░      ░▒▓███████▓▒░░▒▓████████▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓███████▓▒░░▒▓█▓▒░░▒▓█▓▒░ ",
		"       ░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░             ░▒▓█▓▒░▒▓█▓▒░      ░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░ ",
		"       ░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░      ░▒▓█▓▒░             ░▒▓█▓▒░▒▓█▓▒░      ░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░ ",
		"       ░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒▒▓███▓▒░▒▓█▓▒░      ░▒▓███████▓▒░░▒▓██████▓▒░ ░▒▓█▓▒░░▒▓█▓▒░▒▓███████▓▒░ ░▒▓██████▓▒░  ",
		"░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░             ░▒▓█▓▒░▒▓█▓▒░      ░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░  ░▒▓█▓▒░     ",
		"░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░             ░▒▓█▓▒░▒▓█▓▒░      ░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░  ░▒▓█▓▒░     ",
		" ░▒▓██████▓▒░ ░▒▓██████▓▒░░▒▓█▓▒░░▒▓█▓▒░░▒▓██████▓▒░░▒▓████████▓▒░▒▓███████▓▒░░▒▓█▓▒░       ░▒▓██████▓▒░░▒▓█▓▒░░▒▓█▓▒░  ░▒▓█▓▒░     ",
	}

	clearScreen()
	for _, line := range banner {
		fmt.Printf("%s%s%s\n", LightGreen, line, Reset)
	}
	fmt.Printf("%s                                                                 By Whoamikiddie v0.2%s\n", LightGreen, Reset)
}

func main() {
	url := flag.String("u", "", "Single URL to scan")
	urlList := flag.String("l", "", "Text file containing a list of URLs to scan")
	payloadsFile := flag.String("p", "", "Text file containing the payloads (required)")
	cookie := flag.String("c", "", "Cookie to include in the GET request")
	threads := flag.Int("t", 5, "Number of concurrent threads (1-20)")
	logFile := flag.String("log", "sqli_scan.log", "Log file to store results")

	flag.Parse()

	if *payloadsFile == "" || (*url == "" && *urlList == "") {
		flag.Usage()
		os.Exit(1)
	}

	logFileHandle, err := os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("%s✗ Error opening log file: %s%s\n", Red, err, Reset)
		os.Exit(1)
	}
	defer logFileHandle.Close()
	logger := log.New(logFileHandle, "SQLiScanner: ", log.LstdFlags)

	var urls []string
	if *url != "" {
		urls = append(urls, *url)
	} else {
		file, err := os.Open(*urlList)
		if err != nil {
			fmt.Printf("%s✗ Error opening URL list: %s%s\n", Red, err, Reset)
			os.Exit(1)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			urls = append(urls, strings.TrimSpace(scanner.Text()))
		}
	}

	payloadsFileHandle, err := os.Open(*payloadsFile)
	if err != nil {
		fmt.Printf("%s✗ Error opening payloads file: %s%s\n", Red, err, Reset)
		os.Exit(1)
	}
	defer payloadsFileHandle.Close()

	var payloads []string
	scanner := bufio.NewScanner(payloadsFileHandle)
	for scanner.Scan() {
		payloads = append(payloads, strings.TrimSpace(scanner.Text()))
	}

	printBanner()

	var wg sync.WaitGroup
	results := make(chan RequestResult, len(urls)*len(payloads))
	semaphore := make(chan struct{}, *threads)
	if *threads < 1 || *threads > 20 {
		*threads = 5 // Default to 5 if out of range
	}

	for _, url := range urls {
		baselineTime, baselineSize, err := getBaseline(url, *cookie, 15*time.Second)
		if err != nil {
			fmt.Printf("%s✗ Failed to get baseline for %s: %s%s\n", Red, url, err, Reset)
			logger.Printf("Baseline failure for %s: %s", url, err)
			continue
		}

		for _, payload := range payloads {
			wg.Add(1)
			go func(u, p string, bt float64, bs int) {
				defer wg.Done()
				semaphore <- struct{}{}
				result := performRequest(u, p, *cookie, 15*time.Second)
				result.BaselineTime = bt
				result.BaselineSize = bs
				result = analyzeSQLi(result, bt, bs)
				results <- result
				<-semaphore
			}(url, payload, baselineTime, baselineSize)
		}
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for result := range results {
		switch result.SQLiType {
		case "time-based":
			fmt.Printf("%s✓ Time-Based SQLi Found! URL: %s - Response Time: %.2f s (Baseline: %.2f s)%s\n",
				LightGreen, result.URL, result.ResponseTime, result.BaselineTime, Reset)
			logger.Printf("Time-Based SQLi: %s - Time: %.2f s", result.URL, result.ResponseTime)
		case "error-based":
			fmt.Printf("%s✓ Error-Based SQLi Found! URL: %s - Response Time: %.2f s%s\n",
				Yellow, result.URL, result.ResponseTime, Reset)
			logger.Printf("Error-Based SQLi: %s - Time: %.2f s", result.URL, result.ResponseTime)
		case "anomaly-based":
			fmt.Printf("%s✓ Anomaly-Based SQLi Detected! URL: %s - Size: %d (Baseline: %d)%s\n",
				Yellow, result.URL, result.BodySize, result.BaselineSize, Reset)
			logger.Printf("Anomaly-Based SQLi: %s - Size: %d", result.URL, result.BodySize)
		case "none":
			fmt.Printf("%s✗ Not Vulnerable. URL: %s - Response Time: %.2f s%s\n",
				Red, result.URL, result.ResponseTime, Reset)
		}
		if result.ErrorMsg != "" {
			fmt.Printf("%s✗ Error: %s%s\n", Red, result.ErrorMsg, Reset)
			logger.Printf("Error: %s - %s", result.URL, result.ErrorMsg)
		}
	}
}
