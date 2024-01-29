# Makefile for Go project

# Environment variables
ENV_FILE := .env

# Load environment variables from .env file
#include $(ENV_FILE)
export

# Variables
BINARY_NAME := stream
GO_FILES := $(wildcard *.go)

# Commands
.PHONY: build run clean

build:
	@echo "Building $(BINARY_NAME)..."
	@go build -o build/$(BINARY_NAME) -ldflags="-s -w" ./src/$(GO_FILES)

build-encode:
	@echo "Building encoder..."
	@go build -o build/encoder -ldflags="-s -w" ./encoder/$(GO_FILES)


test:
	@echo "Testing $(BINARY_NAME)..."
	@go test ./src/$(GO_FILES)

lint:
	@golangci-lint run -v

run:
	@echo "Running $(BINARY_NAME)..."
	@./build/$(BINARY_NAME)

clean:
	@echo "Cleaning up..."
	@rm -f $(BINARY_NAME)

docs:
	@echo "Docs"
	@go doc

godoc:
	@echo "godoc"
	open http://localhost:6060
	godoc -http=:6060


# Target to run the application
start: build run

# Target to build and run the application
restart: clean start

# Help target
help:
	@echo "Available targets:"
	@echo "  build    : Build the Go application"
	@echo "  run      : Run the Go application"
	@echo "  clean    : Clean up generated files"
	@echo "  docs     : Generated documentation files"
	@echo "  start    : Build and run the Go application"
	@echo "  restart  : Clean, build, and run the Go application"
