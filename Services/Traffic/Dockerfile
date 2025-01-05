FROM golang:1.23.3

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o main .

EXPOSE 5050

CMD ["./main"]