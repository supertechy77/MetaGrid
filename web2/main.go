package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	serverPort  = ":2020"
	httpTimeout = 10 * time.Second
)

type TrafficLight struct {
	ID       int    `json:"id"`
	Location string `json:"location"`
	Color    string `json:"color"`
}

type WeatherEntry struct {
	ID          int       `json:"id"`
	Location    string    `json:"location"`
	Temperature float64   `json:"temperature"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type ParkingSpot struct {
	ID           int       `json:"id"`
	Location     string    `json:"location"`
	Availability bool      `json:"availability"`
	CreatedAt    time.Time `json:"created_at"`
}

type App struct {
	client    *http.Client
	templates *template.Template
}

func NewApp() *App {
	templates := template.Must(template.New("").ParseGlob("templates/*.html"))
	return &App{
		client:    &http.Client{Timeout: httpTimeout},
		templates: templates,
	}
}

func main() {
	app := NewApp()

	mux := http.NewServeMux()
	mux.HandleFunc("/", app.homeHandler)
	mux.HandleFunc("/dashboard", app.dashboardHandler)
	mux.HandleFunc("/traffic-lights", app.trafficLightsHandler)
	mux.HandleFunc("/add-traffic-light", app.addTrafficLightHandler)
	mux.HandleFunc("/update-traffic-light/", app.updateTrafficLightHandler)
	mux.HandleFunc("/delete-traffic-light/", app.deleteTrafficLightHandler)

	mux.HandleFunc("/weather-entries", app.weatherEntriesHandler)
	mux.HandleFunc("/add-weather-entry", app.addWeatherEntryHandler)
	mux.HandleFunc("/update-weather-entry/", app.updateWeatherEntryHandler)
	mux.HandleFunc("/delete-weather-entry/", app.deleteWeatherEntryHandler)

	mux.HandleFunc("/parking-spots", app.parkingSpotsHandler)
	mux.HandleFunc("/add-parking-spot", app.addParkingSpotHandler)
	mux.HandleFunc("/update-parking-spot/", app.updateParkingSpotHandler)
	mux.HandleFunc("/delete-parking-spot/", app.deleteParkingSpotHandler)

	server := &http.Server{
		Addr:         serverPort,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	log.Printf("Server running at http://localhost%s\n", serverPort)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// Home Page Handler
func (app *App) homeHandler(w http.ResponseWriter, r *http.Request) {
	if err := app.templates.ExecuteTemplate(w, "home.html", nil); err != nil {
		log.Printf("Error executing template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// Dashboard Handler
func (app *App) dashboardHandler(w http.ResponseWriter, r *http.Request) {
	if err := app.templates.ExecuteTemplate(w, "dashboard.html", nil); err != nil {
		log.Printf("Error executing template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (app *App) fetchTrafficLights() ([]TrafficLight, error) {
	resp, err := app.client.Get("http://traffic.localhost/traffic-lights")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch traffic lights: %s", resp.Status)
	}

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	// Try to unmarshal as an array of TrafficLight
	var lights []TrafficLight
	if err := json.Unmarshal(body, &lights); err == nil {
		// If successful, return the array
		return lights, nil
	}

	// If unmarshaling as an array fails, try to unmarshal as a message object
	var messageResponse struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &messageResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %v", err)
	}

	// If the message indicates no traffic lights, return an empty slice
	if messageResponse.Message == "There are no traffic lights" {
		return []TrafficLight{}, nil
	}

	// If the message is unexpected, return an error
	return nil, fmt.Errorf("unexpected response: %s", messageResponse.Message)
}

func (app *App) createTrafficLight(light TrafficLight) error {
	body, err := json.Marshal(light)
	if err != nil {
		return err
	}

	resp, err := app.client.Post("http://traffic.localhost/traffic-light", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("failed to create traffic light: %s", resp.Status)
	}
	return nil
}

func (app *App) updateTrafficLight(id int, color string) error {
	req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("http://traffic.localhost/traffic-light/%d?color=%s", id, color), nil)
	if err != nil {
		return err
	}

	resp, err := app.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to update traffic light: %s", resp.Status)
	}
	return nil
}

func (app *App) deleteTrafficLight(id int) error {
	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("http://traffic.localhost/traffic-light/%d", id), nil)
	if err != nil {
		return err
	}

	resp, err := app.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to delete traffic light: %s", resp.Status)
	}
	return nil
}

// Weather Entries Handlers
func (app *App) fetchWeatherEntries() ([]WeatherEntry, error) {
	resp, err := app.client.Get("http://weather.localhost/weather")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch weather entries: %s", resp.Status)
	}

	var entries []WeatherEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (app *App) createWeatherEntry(entry WeatherEntry) error {
	body, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	resp, err := app.client.Post("http://weather.localhost/weather", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("failed to create weather entry: %s", resp.Status)
	}
	return nil
}

func (app *App) updateWeatherEntry(id int, temperature float64, description string) error {
	input := struct {
		Temperature float64 `json:"temperature"`
		Description string  `json:"description"`
	}{temperature, description}

	body, err := json.Marshal(input)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("http://weather.localhost/weather/%d", id), bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	resp, err := app.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to update weather entry: %s", resp.Status)
	}
	return nil
}

func (app *App) deleteWeatherEntry(id int) error {
	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("http://weather.localhost/weather/%d", id), nil)
	if err != nil {
		return err
	}

	resp, err := app.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to delete weather entry: %s", resp.Status)
	}
	return nil
}

// Parking Spots Handlers
func (app *App) fetchParkingSpots() ([]ParkingSpot, error) {
	resp, err := app.client.Get("http://parking.localhost/parking")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch parking spots: %s", resp.Status)
	}

	var spots []ParkingSpot
	if err := json.NewDecoder(resp.Body).Decode(&spots); err != nil {
		return nil, err
	}
	return spots, nil
}

func (app *App) createParkingSpot(spot ParkingSpot) error {
	body, err := json.Marshal(spot)
	if err != nil {
		return err
	}

	resp, err := app.client.Post("http://parking.localhost/parking", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("failed to create parking spot: %s", resp.Status)
	}
	return nil
}

func (app *App) updateParkingSpot(id int, availability bool) error {
	req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("http://parking.localhost/parking/%d?availability=%v", id, availability), nil)
	if err != nil {
		return err
	}

	resp, err := app.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to update parking spot: %s", resp.Status)
	}
	return nil
}

func (app *App) deleteParkingSpot(id int) error {
	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("http://parking.localhost/parking/%d", id), nil)
	if err != nil {
		return err
	}

	resp, err := app.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to delete parking spot: %s", resp.Status)
	}
	return nil
}

// Traffic Lights Handlers
func (app *App) trafficLightsHandler(w http.ResponseWriter, r *http.Request) {
	lights, err := app.fetchTrafficLights()
	if err != nil {
		log.Printf("Error fetching traffic lights: %v", err)
		http.Error(w, "Failed to fetch traffic lights", http.StatusInternalServerError)
		return
	}

	tmpl := template.Must(template.New("traffic-lights").Parse(`
{{if .}}
<div class="traffic-lights">
    {{range .}}
    <div class="traffic-light">
        <strong>Location:</strong> {{.Location}}, <strong>Color:</strong> {{.Color}}
        <div style="display: inline-block; margin-left: 10px;">
            <select name="color" 
                    hx-put="/update-traffic-light/{{.ID}}"
                    hx-target="#traffic-lights"
                    hx-trigger="change"
                    hx-include="this">
                <option value="red" {{if eq .Color "red"}}selected{{end}}>Red</option>
                <option value="yellow" {{if eq .Color "yellow"}}selected{{end}}>Yellow</option>
                <option value="green" {{if eq .Color "green"}}selected{{end}}>Green</option>
            </select>
            <button hx-delete="/delete-traffic-light/{{.ID}}"
                    hx-target="#traffic-lights"
                    hx-swap="innerHTML"
                    class="btn btn-delete">Delete</button>
        </div>
    </div>
    {{end}}
</div>
{{else}}
<div class="no-lights">
    There are no traffic lights.
</div>
{{end}}`))

	if err := tmpl.Execute(w, lights); err != nil {
		log.Printf("Error executing template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (app *App) addTrafficLightHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	location := r.FormValue("location")
	color := r.FormValue("color")

	light := TrafficLight{
		Location: location,
		Color:    color,
	}

	if err := app.createTrafficLight(light); err != nil {
		log.Printf("Error creating traffic light: %v", err)
		http.Error(w, "Failed to create traffic light", http.StatusInternalServerError)
		return
	}

	app.trafficLightsHandler(w, r)
}

func (app *App) updateTrafficLightHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) != 3 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(parts[2])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	color := r.FormValue("color")
	if err := app.updateTrafficLight(id, color); err != nil {
		log.Printf("Error updating traffic light: %v", err)
		http.Error(w, "Failed to update traffic light", http.StatusInternalServerError)
		return
	}

	app.trafficLightsHandler(w, r)
}

func (app *App) deleteTrafficLightHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) != 3 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(parts[2])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	if err := app.deleteTrafficLight(id); err != nil {
		log.Printf("Error deleting traffic light: %v", err)
		http.Error(w, "Failed to delete traffic light", http.StatusInternalServerError)
		return
	}

	app.trafficLightsHandler(w, r)
}

// Weather Entries Handlers
func (app *App) weatherEntriesHandler(w http.ResponseWriter, r *http.Request) {
	entries, err := app.fetchWeatherEntries()
	if err != nil {
		log.Printf("Error fetching weather entries: %v", err)
		http.Error(w, "Failed to fetch weather entries", http.StatusInternalServerError)
		return
	}

	tmpl := template.Must(template.New("weather-entries").Parse(`
{{if .}}
<div class="weather-entries">
    {{range .}}
    <div class="weather-entry">
        <strong>Location:</strong> {{.Location}}, <strong>Temperature:</strong> {{.Temperature}}°C, <strong>Description:</strong> {{.Description}}
        <div style="display: inline-block; margin-left: 10px;">
            <button hx-delete="/delete-weather-entry/{{.ID}}"
                    hx-target="#weather-entries"
                    hx-swap="innerHTML"
                    class="btn btn-delete">Delete</button>
        </div>
    </div>
    {{end}}
</div>
{{else}}
<div class="no-entries">
    There are no weather entries.
</div>
{{end}}`))

	if err := tmpl.Execute(w, entries); err != nil {
		log.Printf("Error executing template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (app *App) addWeatherEntryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	location := r.FormValue("location")
	temperature, _ := strconv.ParseFloat(r.FormValue("temperature"), 64)
	description := r.FormValue("description")

	entry := WeatherEntry{
		Location:    location,
		Temperature: temperature,
		Description: description,
	}

	if err := app.createWeatherEntry(entry); err != nil {
		log.Printf("Error creating weather entry: %v", err)
		http.Error(w, "Failed to create weather entry", http.StatusInternalServerError)
		return
	}

	app.weatherEntriesHandler(w, r)
}

func (app *App) updateWeatherEntryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) != 3 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(parts[2])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var input struct {
		Temperature float64 `json:"temperature"`
		Description string  `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	if err := app.updateWeatherEntry(id, input.Temperature, input.Description); err != nil {
		log.Printf("Error updating weather entry: %v", err)
		http.Error(w, "Failed to update weather entry", http.StatusInternalServerError)
		return
	}

	app.weatherEntriesHandler(w, r)
}

func (app *App) deleteWeatherEntryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) != 3 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(parts[2])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	if err := app.deleteWeatherEntry(id); err != nil {
		log.Printf("Error deleting weather entry: %v", err)
		http.Error(w, "Failed to delete weather entry", http.StatusInternalServerError)
		return
	}

	app.weatherEntriesHandler(w, r)
}

// Parking Spots Handlers
func (app *App) parkingSpotsHandler(w http.ResponseWriter, r *http.Request) {
	spots, err := app.fetchParkingSpots()
	if err != nil {
		log.Printf("Error fetching parking spots: %v", err)
		http.Error(w, "Failed to fetch parking spots", http.StatusInternalServerError)
		return
	}

	tmpl := template.Must(template.New("parking-spots").Parse(`
{{if .}}
<div class="parking-spots">
    {{range .}}
    <div class="parking-spot">
        <strong>Location:</strong> {{.Location}}, <strong>Availability:</strong> {{if .Availability}}Available{{else}}Unavailable{{end}}
        <div style="display: inline-block; margin-left: 10px;">
            <select name="availability" 
                    hx-put="/update-parking-spot/{{.ID}}"
                    hx-target="#parking-spots"
                    hx-trigger="change"
                    hx-include="this">
                <option value="true" {{if .Availability}}selected{{end}}>Available</option>
                <option value="false" {{if not .Availability}}selected{{end}}>Unavailable</option>
            </select>
            <button hx-delete="/delete-parking-spot/{{.ID}}"
                    hx-target="#parking-spots"
                    hx-swap="innerHTML"
                    class="btn btn-delete">Delete</button>
        </div>
    </div>
    {{end}}
</div>
{{else}}
<div class="no-spots">
    There are no parking spots.
</div>
{{end}}`))

	if err := tmpl.Execute(w, spots); err != nil {
		log.Printf("Error executing template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (app *App) addParkingSpotHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	location := r.FormValue("location")
	availability := r.FormValue("availability") == "true"

	spot := ParkingSpot{
		Location:     location,
		Availability: availability,
	}

	if err := app.createParkingSpot(spot); err != nil {
		log.Printf("Error creating parking spot: %v", err)
		http.Error(w, "Failed to create parking spot", http.StatusInternalServerError)
		return
	}

	app.parkingSpotsHandler(w, r)
}

func (app *App) updateParkingSpotHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) != 3 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(parts[2])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	availability := r.FormValue("availability") == "true"

	if err := app.updateParkingSpot(id, availability); err != nil {
		log.Printf("Error updating parking spot: %v", err)
		http.Error(w, "Failed to update parking spot", http.StatusInternalServerError)
		return
	}

	app.parkingSpotsHandler(w, r)
}

func (app *App) deleteParkingSpotHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) != 3 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(parts[2])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	if err := app.deleteParkingSpot(id); err != nil {
		log.Printf("Error deleting parking spot: %v", err)
		http.Error(w, "Failed to delete parking spot", http.StatusInternalServerError)
		return
	}

	app.parkingSpotsHandler(w, r)
}
