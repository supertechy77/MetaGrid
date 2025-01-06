#!/bin/bash

set -e  # Exit immediately if a command exits with a non-zero status

function change_dir() {
    if [ -d "$1" ]; then
        cd "$1"
    else
        echo "Error: Directory $1 does not exist."
        echo "Current directory: $(pwd)"
        echo "Contents of current directory:"
        ls -la
        exit 1
    fi
}

function to_lowercase() {
    echo "$1" | tr '[:upper:]' '[:lower:]'
}
function add_hosts() {
    echo "Adding hosts to /etc/hosts..."
    
    # Check if entries already exist
    if grep -q "parking.localhost" /etc/hosts && \
       grep -q "traffic.localhost" /etc/hosts && \
       grep -q "weather.localhost" /etc/hosts; then
        echo "Host entries already exist in /etc/hosts. Skipping."
        return
    fi

    # Add entries to /etc/hosts
    echo "127.0.0.1 parking.localhost" | sudo tee -a /etc/hosts
    echo "127.0.0.1 traffic.localhost" | sudo tee -a /etc/hosts
    echo "127.0.0.1 weather.localhost" | sudo tee -a /etc/hosts

    echo "Hosts added successfully."
}

# Add necessary hosts to /etc/hosts
add_hosts
# Start Docker Compose for the Brain services
change_dir Brain

echo "Starting Docker Compose..."
docker compose up -d

echo "Docker Compose is up."

# Build service images
echo "Current directory before building services: $(pwd)"
echo "Contents of current directory:"
ls -la

# Navigate back to the root directory of the project
cd ..
for service in Parking Traffic Weather; do
    if [ -d "Services/$service" ]; then
        change_dir "Services/$service"
        echo "Building $service Service..."
        docker build -t "$(to_lowercase $service)-service" . || { echo "Failed to build $service service"; exit 1; }
        cd ../..  # Return to the project root directory after each build
    else
        echo "Warning: Directory for $service service not found. Skipping."
    fi
done

echo "Service images are ready."

# Navigate to Testing folder
if [ -d "Testing" ]; then
    change_dir "Testing"

    # Run tests
    for test in Latency StartupTime Failure; do
        if [ -d "$test" ]; then
            echo "Starting Phase: $test Test..."
            change_dir "$test"
            go run . || { echo "Failed to run $test test"; exit 1; }
            cd ..
        else
            echo "Warning: Test directory $test not found. Skipping."
        fi
    done

    # Navigate back to the project root
    cd ..
else
    echo "Warning: Testing directory not found. Skipping tests."
fi
# Shutdown services
change_dir "Brain"
echo "Shutting down Docker Compose..."
docker compose down

echo "All services shut down. Cleaning up Docker images..."
docker rmi parking-service traffic-light-service weather-service || true

echo "Cleanup complete."
exit 0
