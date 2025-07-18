BINARY_NAME = niova-lookout
BUILD_DIR = cmd/lookout
OUTPUT_DIR = .
OUTPUT_PATH = $(OUTPUT_DIR)/$(BINARY_NAME)

.PHONY: all build clean

all: build

build:
	cd $(BUILD_DIR) && go build -o ../../$(OUTPUT_PATH)
	@echo "Built $(BINARY_NAME) at $(OUTPUT_PATH)"

clean:
	go clean
