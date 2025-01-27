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

function to_lowercase() {
  echo "$1" | tr '[:upper:]' '[:lower:]'
}

# Start Docker Compose for the Brain services
change_dir Brain
echo "Starting Docker Compose for Brain..."
docker compose up -d
echo "Brain services are up."

# Navigate back to the root directory
cd ..

# Build and start each service
for service in Parking Traffic Weather; do
  if [ -d "Services/$service" ]; then
    change_dir "Services/$service"
    echo "Building $service Service..."
    docker build -t "$(to_lowercase $service)-service" . || {
      echo "Failed to build $service service"
      exit 1
    }
    echo "Starting Docker Compose for $service..."
    docker compose up -d
    echo "$service service is up."
    cd ../.. # Return to the project root directory after each service
  else
    echo "Warning: Directory for $service service not found. Skipping."
  fi
done

echo "All services are built and running."

# Run "go run ." in the web path of MetaGrid
if [ -d "web2" ]; then
  change_dir "web2"
  echo "Running MetaGrid web application..."
  go run . &
  WEB_PID=$!
  wait $WEB_PID
else
  echo "Error: 'web' directory not found."
  exit 1
fi
