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
	r.Post("/traffic-light", addTrafficLight)
	r.Get("/traffic-light/{id}", getTrafficLight)
	r.Put("/traffic-light/{id}", updateTrafficLight)
	r.Delete("/traffic-light/{id}", deleteTrafficLight)
	r.Get("/traffic-lights", listTrafficLights)
	r.Get("/health", healthCheck)

	// Set up graceful shutdown
	serviceID = fmt.Sprintf("traffic-light-service-%d", time.Now().UnixNano())

	// Find an available port
	listener, port, err := findAvailablePort(5050, 5100)
	if err != nil {
		log.Fatalf("Failed to find an available port: %v", err)
	}
	defer listener.Close()

	log.Printf("Traffic Light Service running on :%d", port)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	server := &http.Server{Handler: r}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	registerWithConsul(serviceID, "traffic", "traffic", port)
	defer deregisterWithConsul(serviceID)

	<-stop
	log.Println("Shutting down Traffic Light Service...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("HTTP server Shutdown: %v", err)
	}
}

// ensureTableExists creates the traffic_lights table if it does not already exist.
func ensureTableExists() error {
	query := `
		CREATE TABLE IF NOT EXISTS traffic_lights (
			id SERIAL PRIMARY KEY,
			location TEXT NOT NULL,
			color TEXT NOT NULL DEFAULT 'red'
		);
	`
	_, err := db.Exec(query)
	return err
}

func addTrafficLight(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Location string `json:"location"`
		Color    string `json:"color"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	var id int
	err := db.QueryRow(`INSERT INTO traffic_lights (location, color) VALUES ($1, $2) RETURNING id`, input.Location, input.Color).Scan(&id)
	if err != nil {
		http.Error(w, "Failed to add traffic light", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "Traffic light added with ID: %d", id)
}

func getTrafficLight(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	row := db.QueryRow(`SELECT id, location, color FROM traffic_lights WHERE id = $1`, id)

	var trafficLight struct {
		ID       int    `json:"id"`
		Location string `json:"location"`
		Color    string `json:"color"`
	}
	if err := row.Scan(&trafficLight.ID, &trafficLight.Location, &trafficLight.Color); err != nil {
		http.Error(w, "Traffic light not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(trafficLight)
}

func updateTrafficLight(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	color := r.URL.Query().Get("color")

	if color == "" {
		http.Error(w, "Color query parameter is required", http.StatusBadRequest)
		return
	}

	result, err := db.Exec(`UPDATE traffic_lights SET color = $1 WHERE id = $2`, color, id)
	if err != nil {
		http.Error(w, "Failed to update traffic light", http.StatusInternalServerError)
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "Error checking update result", http.StatusInternalServerError)
		return
	}

	if rowsAffected == 0 {
		http.Error(w, "Traffic light not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Traffic light %s updated to color %s", id, color)
}
func deleteTrafficLight(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, err := db.Exec(`DELETE FROM traffic_lights WHERE id = $1`, id)
	if err != nil {
		http.Error(w, "Failed to delete traffic light", http.StatusInternalServerError)
		return
	}

	w.Write([]byte("Traffic light deleted"))
}

func listTrafficLights(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, location, color FROM traffic_lights`)
	if err != nil {
		http.Error(w, "Failed to query traffic lights", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var trafficLights []struct {
		ID       int    `json:"id"`
		Location string `json:"location"`
		Color    string `json:"color"`
	}
	for rows.Next() {
		var trafficLight struct {
			ID       int    `json:"id"`
			Location string `json:"location"`
			Color    string `json:"color"`
		}
		if err := rows.Scan(&trafficLight.ID, &trafficLight.Location, &trafficLight.Color); err != nil {
			http.Error(w, "Failed to read traffic lights", http.StatusInternalServerError)
			return
		}
		trafficLights = append(trafficLights, trafficLight)
	}

	w.Header().Set("Content-Type", "application/json")

	if len(trafficLights) == 0 {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "There are no traffic lights"})
		return
	}
	json.NewEncoder(w).Encode(trafficLights)
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	// Check database connection
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
			fmt.Sprintf("traefik.http.routers.%s.rule=Host(`traffic.localhost`)", formattedServiceName),
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
