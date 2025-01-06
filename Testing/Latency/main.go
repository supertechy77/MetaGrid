package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
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

func main() {
	// Define services to test
	services := []Service{
		{"Weather Service", "http://weather.localhost/health"},
		{"Parking Service", "http://parking.localhost/health"},
		{"Traffic Light Service", "http://traffic.localhost/health"},
	}

	servicesRoot := "../../Services"

	consulURL := "http://localhost:8500/v1/status/leader"
	resp, err := http.Get(consulURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Fatalf("Failed to reach Consul: %v", err)
	}
	resp.Body.Close()
	fmt.Println("Successfully connected to Consul")

	// Get all subdirectories in the Services folder
	serviceDirs, err := getSubdirectories(servicesRoot)
	if err != nil {
		fmt.Printf("Error fetching subdirectories: %v\n", err)
		return
	}

	// Iterate over each subdirectory and run `docker-compose up`
	for _, dir := range serviceDirs {
		fmt.Printf("Starting service in directory: %s\n", dir)
		if err := runDockerComposeUp(dir); err != nil {
			fmt.Printf("Failed to start service in %s: %v\n", dir, err)
		} else {
			fmt.Printf("Successfully started service in %s\n", dir)
		}
	}

	// Wait for services to be ready
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

func getSubdirectories(root string) ([]string, error) {
	var subdirectories []string
	files, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if file.IsDir() {
			subdirectories = append(subdirectories, filepath.Join(root, file.Name()))
		}
	}
	return subdirectories, nil
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
