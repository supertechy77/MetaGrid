package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// Service represents a service endpoint to be tested
type Service struct {
	Name string
	URL  string
}

// TestResult holds the results of testing a service
type TestResult struct {
	ServiceName     string
	RequestTime     time.Time
	Latency         time.Duration
	Success         bool
	ConcurrentGroup int
}

var (
	serviceDirs []string
	currentDir  string
	err         error
)

func main() {
	// Define services to test
	services := []Service{
		{"Weather Service", "http://weather.localhost/health"},
		{"Parking Service", "http://parking.localhost/health"},
		{"Traffic Light Service", "http://traffic.localhost/health"},
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start a goroutine to handle the shutdown signal
	go func() {
		<-sigChan
		fmt.Println("\nReceived interrupt signal. Shutting down services...")
		shutdownAllServices(serviceDirs)
		os.Exit(0)
	}()

	// Print current working directory and full path of servicesRoot
	currentDir, err = os.Getwd()
	if err != nil {
		fmt.Printf("Error getting current working directory: %v\n", err)
	}

	servicesRoot := filepath.Join(currentDir, "..", "..", "services")

	fmt.Printf("Current working directory: %s\n", currentDir)
	fmt.Printf("Services root directory: %s\n", servicesRoot)

	// Query Consul
	consulURL := "http://localhost:8500/v1/status/leader"
	resp, err := http.Get(consulURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Fatalf("Failed to query Consul or received non-200 status code: %v", err)
	}
	resp.Body.Close()
	fmt.Println("Successfully connected to Consul")

	// Get all subdirectories in the Services folder
	if err = getSubdirectories(servicesRoot); err != nil {
		fmt.Printf("Error fetching subdirectories: %v\n", err)
		return
	}

	// Print found service directories
	fmt.Println("Found service directories:")
	for _, dir := range serviceDirs {
		fmt.Println(dir)
		runDockerComposeUp(dir)
	} // Wait for services to be ready
	fmt.Println("Waiting for services to be ready...")
	time.Sleep(10 * time.Second)

	// Output CSV file
	outputFile, err := os.Create("test_results.csv")
	if err != nil {
		fmt.Printf("Failed to create output file: %v\n", err)
		return
	}
	defer outputFile.Close()
	csvWriter := csv.NewWriter(outputFile)
	defer csvWriter.Flush()

	// Write CSV header
	csvWriter.Write([]string{"ServiceName", "RequestTime", "Latency(ms)", "Success", "ConcurrentGroup"})

	// Run tests with increasing stress levels
	stressLevels := []int{10, 50, 100, 200, 500} // Number of concurrent requests
	for _, level := range stressLevels {
		fmt.Printf("Testing with %d concurrent requests...\n", level)
		testServices(services, level, csvWriter)
	}

	for _, dir := range serviceDirs {
		fmt.Printf("Stopping service in directory: %s\n", dir)
		if err := runDockerComposeDown(dir); err != nil {
			fmt.Printf("Failed to stop service in %s: %v\n", dir, err)
		} else {
			fmt.Printf("Successfully stopped service in %s\n", dir)
		}
	}

	fmt.Println("Testing complete. Results saved to test_results.csv")
}

func testServices(services []Service, concurrency int, csvWriter *csv.Writer) {
	var wg sync.WaitGroup
	results := make(chan TestResult, concurrency*len(services))

	for _, service := range services {
		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(s Service, group int) {
				defer wg.Done()
				start := time.Now()

				client := &http.Client{
					Timeout: 10 * time.Second,
				}

				resp, err := client.Get(s.URL)
				latency := time.Since(start)

				success := false
				statusCode := 0
				if err != nil {
					fmt.Printf("Error requesting %s: %v\n", s.URL, err)
				} else {
					statusCode = resp.StatusCode
					if resp.StatusCode == http.StatusOK {
						success = true
					} else {
						fmt.Printf("Unexpected status code for %s: %d\n", s.URL, resp.StatusCode)
					}
					resp.Body.Close()
				}

				result := TestResult{
					ServiceName:     s.Name,
					RequestTime:     start,
					Latency:         latency,
					Success:         success,
					ConcurrentGroup: group,
				}

				results <- result

				fmt.Printf("Request to %s completed. Success: %v, Latency: %v, Status Code: %d\n",
					s.Name, success, latency, statusCode)
			}(service, concurrency)
		}
	}

	wg.Wait()
	close(results)

	// Write results to CSV
	for result := range results {
		csvWriter.Write([]string{
			result.ServiceName,
			result.RequestTime.Format(time.RFC3339),
			strconv.FormatFloat(result.Latency.Seconds()*1000, 'f', 2, 64),
			strconv.FormatBool(result.Success),
			strconv.Itoa(result.ConcurrentGroup),
		})
	}
	csvWriter.Flush()
}

func shutdownAllServices(serviceDirs []string) {
	for _, dir := range serviceDirs {
		fmt.Printf("Stopping service in directory: %s\n", dir)
		cmd := exec.Command("docker-compose", "down")
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			fmt.Printf("Failed to stop service in %s: %v\n", dir, err)
		}
	}
}

// getSubdirectories returns a list of subdirectories in the given root directory
func getSubdirectories(root string) error {
	serviceDirs = []string{} // Clear the existing slice
	fmt.Printf("Searching for subdirectories in: %s\n", root)

	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && path != root {
			serviceDirs = append(serviceDirs, path)
			fmt.Printf("Found directory: %s\n", path)
		}
		return nil
	})
}

// runDockerComposeUp runs `docker-compose up -d` in the given directory
func runDockerComposeUp(dir string) error {
	cmd := exec.Command("docker-compose", "up", "-d")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runDockerComposeDown(dir string) error {
	cmd := exec.Command("docker-compose", "down")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
