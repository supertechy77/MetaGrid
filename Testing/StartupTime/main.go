package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Service represents a service to be tested
type Service struct {
	Name       string
	HealthURL  string
	ConsulKey  string
	DockerName string
}

var (
	serviceDirs []string
	currentDir  string
	err         error
)

func main() {
	// Define services to test

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
	}

	// Run the test 5 times
	for i := 1; i <= 5; i++ {
		fmt.Printf("\nRunning test iteration %d of 5...\n", i)

		// Output CSV file
		outputFile, err := os.Create(fmt.Sprintf("startup_times_run_%d.csv", i))
		if err != nil {
			fmt.Printf("Failed to create output file: %v\n", err)
			return
		}
		defer outputFile.Close()

		writer := bufio.NewWriter(outputFile)
		defer writer.Flush()

		// Write CSV header
		writer.WriteString("ServiceName,TotalStartupTime(s),ConsulCheckTime(s),HealthCheckTime(s),ConsulChecksPassed,ContainerStartTime(s)\n")

		for _, dir := range serviceDirs {
			serviceName := strings.ToLower(filepath.Base(dir))
			fmt.Printf("Testing %s...\n", serviceName)
			service := Service{
				Name:       serviceName,
				HealthURL:  fmt.Sprintf("http://%s.localhost/health", serviceName),
				ConsulKey:  serviceName,
				DockerName: serviceName,
			}

			startupTime, consulCheckTime, healthCheckTime, consulReady, containerStartTime, err := measureDetailedStartupTime(service, dir)
			if err != nil {
				fmt.Printf("Error testing %s: %v\n", service.Name, err)
				continue
			}

			writer.WriteString(fmt.Sprintf("%s,%.2f,%.2f,%.2f,%v,%.2f\n",
				service.Name, startupTime, consulCheckTime, healthCheckTime, consulReady, containerStartTime))

			fmt.Printf("%s started in %.2f seconds (Consul checks passed: %v)\n", service.Name, startupTime, consulReady)
		}

		// Shut down all services after each run
		shutdownAllServices(serviceDirs)

		// Wait for a short period before the next run
		if i < 5 {
			fmt.Println("Waiting 30 seconds before the next run...")
			time.Sleep(30 * time.Second)
		}
	}
}

func measureDetailedStartupTime(service Service, dir string) (float64, float64, float64, bool, float64, error) {
	if err := os.Chdir(dir); err != nil {
		return 0, 0, 0, false, 0, fmt.Errorf("failed to change directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(currentDir); err != nil {
			fmt.Printf("Warning: Failed to change back to original directory: %v\n", err)
		}
	}()

	// Stop the service if it's already running
	fmt.Printf("Stopping service %s if running...\n", service.DockerName)
	stopCmd := exec.Command("docker-compose", "down")
	if err := stopCmd.Run(); err != nil {
		fmt.Printf("Warning: Failed to stop service: %v\n", err)
	}

	// Start the service
	fmt.Printf("Starting service %s...\n", service.DockerName)
	startCmd := exec.Command("docker-compose", "up", "-d")
	startTime := time.Now()
	if err := startCmd.Run(); err != nil {
		return 0, 0, 0, false, 0, fmt.Errorf("failed to start service: %v", err)
	}

	containerStartTime := time.Since(startTime).Seconds()

	// Wait for the service to be ready
	timeout := time.After(2 * time.Minute)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var consulCheckTime, healthCheckTime float64
	consulReady, serviceHealthy := false, false

	for {
		select {
		case <-timeout:
			return 0, 0, 0, false, 0, fmt.Errorf("service did not start within the timeout period")
		case <-ticker.C:
			if !consulReady {
				consulReady = isConsulServiceHealthy(service.ConsulKey)
				if consulReady {
					consulCheckTime = time.Since(startTime).Seconds()
				}
			}
			if !serviceHealthy {
				serviceHealthy = isServiceHealthy(service.HealthURL)
				if serviceHealthy {
					healthCheckTime = time.Since(startTime).Seconds()
				}
			}

			if consulReady && serviceHealthy {
				totalStartupTime := time.Since(startTime).Seconds()
				return totalStartupTime, consulCheckTime, healthCheckTime, consulReady, containerStartTime, nil
			}

			fmt.Printf("Waiting for %s to be ready (Consul: %v, Health: %v)\n", service.Name, consulReady, serviceHealthy)
		}
	}
}

// ... (rest of the code remains unchanged)

// isConsulServiceHealthy checks if a service is healthy in Consul
func isConsulServiceHealthy(consulKey string) bool {
	url := fmt.Sprintf("http://localhost:8500/v1/health/checks/%s", consulKey)
	fmt.Println(url)

	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("Error querying Consul for %s: %v\n", consulKey, err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Unexpected response code from Consul for %s: %d\n", consulKey, resp.StatusCode)
		return false
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading Consul response for %s: %v\n", consulKey, err)
		return false
	}

	var checks []map[string]interface{}
	if err := json.Unmarshal(body, &checks); err != nil {
		fmt.Printf("Error unmarshaling Consul response for %s: %v\n", consulKey, err)
		return false
	}

	if len(checks) == 0 {
		fmt.Printf("No health checks found for %s\n", consulKey)
		return false
	}

	for _, check := range checks {
		status, ok := check["Status"].(string)
		if !ok {
			fmt.Printf("Invalid status format for %s\n", consulKey)
			return false
		}
		if status != "passing" {
			fmt.Printf("Service %s is not passing: %s\n", consulKey, status)
			return false
		}
	}

	fmt.Printf("Service %s is healthy in Consul\n", consulKey)
	return true
}

// isServiceHealthy checks if a service's health endpoint is responding
// isServiceHealthy checks if a service's health endpoint is responding
func isServiceHealthy(url string) bool {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Printf("Error creating request for %s: %v\n", url, err)
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error accessing %s: %v\n", url, err)
		return false
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading response body from %s: %v\n", url, err)
		return false
	}

	fmt.Printf("Response from %s: Status: %d, Body: %s\n", url, resp.StatusCode, string(body))

	return resp.StatusCode == http.StatusOK
}

// shutdownAllServices stops all services in the given directories
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
