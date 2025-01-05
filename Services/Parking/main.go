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
	"strconv"
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
	r.Post("/parking", addParkingSpot)
	r.Get("/parking/{id}", getParkingSpot)
	r.Put("/parking/{id}", updateParkingSpot)
	r.Delete("/parking/{id}", deleteParkingSpot)
	r.Get("/parking", listParkingSpots)
	r.Get("/health", healthCheck)

	// Set up graceful shutdown
	serviceID = fmt.Sprintf("parking-service-%d", time.Now().UnixNano())

	// Find an available port
	listener, port, err := findAvailablePort(7050, 7100)
	if err != nil {
		log.Fatalf("Failed to find an available port: %v", err)
	}
	defer listener.Close()

	log.Printf("Parking Service running on :%d", port)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	server := &http.Server{Handler: r}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	registerWithConsul(serviceID, "parking", "parking", port)
	defer deregisterWithConsul(serviceID)

	<-stop
	log.Println("Shutting down Parking Service...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("HTTP server Shutdown: %v", err)
	}
}

func ensureTableExists() error {
	query := `
		CREATE TABLE IF NOT EXISTS parking (
			id SERIAL PRIMARY KEY,
			location TEXT NOT NULL,
			availability BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
	`
	_, err := db.Exec(query)
	return err
}

func addParkingSpot(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Location     string `json:"location"`
		Availability bool   `json:"availability"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	var id int
	err := db.QueryRow(
		`INSERT INTO parking (location, availability) VALUES ($1, $2) RETURNING id`,
		input.Location, input.Availability,
	).Scan(&id)
	if err != nil {
		http.Error(w, "Failed to add parking spot", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "Parking spot added with ID: %d", id)
}

func getParkingSpot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	row := db.QueryRow(`SELECT id, location, availability, created_at FROM parking WHERE id = $1`, id)

	var parkingSpot struct {
		ID           int       `json:"id"`
		Location     string    `json:"location"`
		Availability bool      `json:"availability"`
		CreatedAt    time.Time `json:"created_at"`
	}
	if err := row.Scan(&parkingSpot.ID, &parkingSpot.Location, &parkingSpot.Availability, &parkingSpot.CreatedAt); err != nil {
		http.Error(w, "Parking spot not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(parkingSpot)
}

func updateParkingSpot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	availabilityStr := r.URL.Query().Get("availability")

	if availabilityStr == "" {
		http.Error(w, "Availability query parameter is required", http.StatusBadRequest)
		return
	}

	availability, err := strconv.ParseBool(availabilityStr)
	if err != nil {
		http.Error(w, "Invalid availability value. Must be 'true' or 'false'", http.StatusBadRequest)
		return
	}

	result, err := db.Exec(`UPDATE parking SET availability = $1 WHERE id = $2`, availability, id)
	if err != nil {
		http.Error(w, "Failed to update parking spot", http.StatusInternalServerError)
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil || rowsAffected == 0 {
		http.Error(w, "Parking spot not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Parking spot %s updated to availability: %v", id, availability)
}

func deleteParkingSpot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, err := db.Exec(`DELETE FROM parking WHERE id = $1`, id)
	if err != nil {
		http.Error(w, "Failed to delete parking spot", http.StatusInternalServerError)
		return
	}

	w.Write([]byte("Parking spot deleted"))
}

func listParkingSpots(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, location, availability, created_at FROM parking`)
	if err != nil {
		http.Error(w, "Failed to query parking spots", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var parkingSpots []struct {
		ID           int       `json:"id"`
		Location     string    `json:"location"`
		Availability bool      `json:"availability"`
		CreatedAt    time.Time `json:"created_at"`
	}
	for rows.Next() {
		var spot struct {
			ID           int       `json:"id"`
			Location     string    `json:"location"`
			Availability bool      `json:"availability"`
			CreatedAt    time.Time `json:"created_at"`
		}
		if err := rows.Scan(&spot.ID, &spot.Location, &spot.Availability, &spot.CreatedAt); err != nil {
			http.Error(w, "Failed to read parking spots", http.StatusInternalServerError)
			return
		}
		parkingSpots = append(parkingSpots, spot)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(parkingSpots)
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
			fmt.Sprintf("traefik.http.routers.%s.rule=Host(`parking.localhost`)", formattedServiceName),
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
		log.Fatalf("Failed to connect to Consul: %v", err)
	}

	if err := consul.Agent().ServiceDeregister(serviceID); err != nil {
		log.Printf("Failed to deregister service with Consul: %v", err)
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
	return nil, 0, fmt.Errorf("no available ports between %d and %d", start, end)
}
