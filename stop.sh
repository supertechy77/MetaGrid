#!/bin/bash
set -e # Exit immediately if a command exits with a non-zero status

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

# Shutdown Brain services
echo "Shutting down Brain services..."
if [ -d "Brain" ]; then
  change_dir Brain
  docker compose down
  echo "Brain services are down."
  cd ..
else
  echo "Warning: Brain directory not found. Skipping."
fi

# Shutdown each service
for service in Parking Traffic Weather; do
  if [ -d "Services/$service" ]; then
    echo "Shutting down $service service..."
    change_dir "Services/$service"
    docker compose down
    echo "$service service is down."
    cd ../.. # Return to the project root directory
  else
    echo "Warning: Directory for $service service not found. Skipping."
  fi
done

# Find and kill the MetaGrid web application process
echo "Stopping MetaGrid web application..."
pkill -f "go run ." || {
  echo "No running MetaGrid web process found."
}

echo "All services have been shut down."
