package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	consulapi "github.com/hashicorp/consul/api"
	_ "github.com/lib/pq"
)

var (
	db        *sql.DB
	serviceID string
)

func main() {
	// Connect to DB
	var err error
	db, err = sql.Open("postgres", "postgresql://Admin:admin123@postgres:5432/MetaGrid?sslmode=disable")
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	defer db.Close()

	// Ensure table exists
	if err := ensureTableExists(); err != nil {
		log.Fatalf("Failed to ensure table exists: %v", err)
	}

	// Create router
	r := chi.NewRouter()
	r.Use(middleware.Logger)

	// Add endpoints
	r.Post("/weather", addWeatherEntry)
	r.Get("/weather/{id}", getWeatherEntry)
	r.Put("/weather/{id}", updateWeatherEntry)
	r.Delete("/weather/{id}", deleteWeatherEntry)
	r.Get("/weather", listWeatherEntries)
	r.Get("/health", healthCheck)

	// Set up graceful shutdown
	serviceID = fmt.Sprintf("weather-service-%d", time.Now().UnixNano())

	// Find an available port
	listener, port, err := findAvailablePort(6050, 6100)
	if err != nil {
		log.Fatalf("Failed to find an available port: %v", err)
	}
	defer listener.Close()

	log.Printf("Weather Service running on :%d", port)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	server := &http.Server{Handler: r}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	registerWithConsul(serviceID, "weather", "weather", port)
	defer deregisterWithConsul(serviceID)

	<-stop
	log.Println("Shutting down Weather Service...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("HTTP server Shutdown: %v", err)
	}
}

func ensureTableExists() error {
	query := `
		CREATE TABLE IF NOT EXISTS weather (
			id SERIAL PRIMARY KEY,
			location TEXT NOT NULL,
			temperature REAL NOT NULL,
			description TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
	`
	_, err := db.Exec(query)
	return err
}

func addWeatherEntry(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Location    string  `json:"location"`
		Temperature float64 `json:"temperature"`
		Description string  `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	var id int
	err := db.QueryRow(
		`INSERT INTO weather (location, temperature, description) VALUES ($1, $2, $3) RETURNING id`,
		input.Location, input.Temperature, input.Description,
	).Scan(&id)
	if err != nil {
		http.Error(w, "Failed to add weather entry", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "Weather entry added with ID: %d", id)
}

func getWeatherEntry(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	row := db.QueryRow(`SELECT id, location, temperature, description, created_at FROM weather WHERE id = $1`, id)

	var weatherEntry struct {
		ID          int       `json:"id"`
		Location    string    `json:"location"`
		Temperature float64   `json:"temperature"`
		Description string    `json:"description"`
		CreatedAt   time.Time `json:"created_at"`
	}
	if err := row.Scan(&weatherEntry.ID, &weatherEntry.Location, &weatherEntry.Temperature, &weatherEntry.Description, &weatherEntry.CreatedAt); err != nil {
		http.Error(w, "Weather entry not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(weatherEntry)
}

func updateWeatherEntry(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var input struct {
		Temperature float64 `json:"temperature"`
		Description string  `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	result, err := db.Exec(`UPDATE weather SET temperature = $1, description = $2 WHERE id = $3`, input.Temperature, input.Description, id)
	if err != nil {
		http.Error(w, "Failed to update weather entry", http.StatusInternalServerError)
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil || rowsAffected == 0 {
		http.Error(w, "Weather entry not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Weather entry %s updated", id)
}

func deleteWeatherEntry(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, err := db.Exec(`DELETE FROM weather WHERE id = $1`, id)
	if err != nil {
		http.Error(w, "Failed to delete weather entry", http.StatusInternalServerError)
		return
	}

	w.Write([]byte("Weather entry deleted"))
}

func listWeatherEntries(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, location, temperature, description, created_at FROM weather`)
	if err != nil {
		http.Error(w, "Failed to query weather entries", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var weatherEntries []struct {
		ID          int       `json:"id"`
		Location    string    `json:"location"`
		Temperature float64   `json:"temperature"`
		Description string    `json:"description"`
		CreatedAt   time.Time `json:"created_at"`
	}
	for rows.Next() {
		var entry struct {
			ID          int       `json:"id"`
			Location    string    `json:"location"`
			Temperature float64   `json:"temperature"`
			Description string    `json:"description"`
			CreatedAt   time.Time `json:"created_at"`
		}
		if err := rows.Scan(&entry.ID, &entry.Location, &entry.Temperature, &entry.Description, &entry.CreatedAt); err != nil {
			http.Error(w, "Failed to read weather entries", http.StatusInternalServerError)
			return
		}
		weatherEntries = append(weatherEntries, entry)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(weatherEntries)
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	err := db.Ping()
	if err != nil {
		http.Error(w, "Database connection failed", http.StatusInternalServerError)
		return
	}

	response := struct {
		Status string `json:"status"`
		ID     string `json:"id"`
	}{
		Status: "OK",
		ID:     serviceID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func registerWithConsul(serviceID, serviceName, serviceHost string, servicePort int) {
	consulConfig := consulapi.DefaultConfig()
	consulConfig.Address = "consul:8500"

	consul, err := consulapi.NewClient(consulConfig)
	if err != nil {
		log.Fatalf("Failed to connect to Consul: %v", err)
	}

	formattedServiceName := strings.ToLower(strings.ReplaceAll(serviceName, " ", ""))

	reg := &consulapi.AgentServiceRegistration{
		ID:      serviceID,
		Name:    serviceName,
		Address: serviceHost,
		Port:    servicePort,
		Tags: []string{
			"traefik.enable=true",
			fmt.Sprintf("traefik.http.routers.%s.rule=Host(`weather.localhost`)", formattedServiceName),
			fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port=%d", formattedServiceName, servicePort),
		},
		Check: &consulapi.AgentServiceCheck{
			HTTP:     fmt.Sprintf("http://%s:%d/health", serviceHost, servicePort),
			Interval: "10s",
			Timeout:  "5s",
		},
	}

	if err := consul.Agent().ServiceRegister(reg); err != nil {
		log.Fatalf("Failed to register service with Consul: %v", err)
	}

	log.Printf("Successfully registered service %s with Consul", serviceID)
}

func deregisterWithConsul(serviceID string) {
	consulConfig := consulapi.DefaultConfig()
	consulConfig.Address = "consul:8500"

	consul, err := consulapi.NewClient(consulConfig)
	if err != nil {
		log.Printf("Failed to connect to Consul for deregistration: %v", err)
		return
	}

	if err := consul.Agent().ServiceDeregister(serviceID); err != nil {
		log.Printf("Failed to deregister service with Consul: %v", err)
		return
	}

	log.Printf("Successfully deregistered service %s from Consul", serviceID)
}

func findAvailablePort(start, end int) (net.Listener, int, error) {
	for port := start; port <= end; port++ {
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			return listener, port, nil
		}
	}
	return nil, 0, fmt.Errorf("no available ports in range %d-%d", start, end)
}
