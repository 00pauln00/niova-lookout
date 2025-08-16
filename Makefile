BINARY_NAME = niova-lookout
BUILD_DIR = cmd/lookout
OUTPUT_DIR = .
OUTPUT_PATH = $(OUTPUT_DIR)/$(BINARY_NAME)

.PHONY: all build clean debug

all: build

build:
	cd $(BUILD_DIR) && go build -o ../../$(OUTPUT_PATH)
	@echo "Built $(BINARY_NAME) at $(OUTPUT_PATH)"

debug:
	cd $(BUILD_DIR) && go build -gcflags=all="-N -l" -o ../../$(OUTPUT_PATH)
	@echo "Built debug $(BINARY_NAME) at $(OUTPUT_PATH)"

clean:
	go clean

dist-clean:
	go clean -cache -testcache -modcache
