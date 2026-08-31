GO ?= go
BUILD_DIR ?= build
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)
BINARY ?= $(BUILD_DIR)/wassup

.PHONY: build clean test

build:
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -trimpath -o $(BINARY) .

test:
	$(GO) test ./...

clean:
	rm -rf $(BUILD_DIR)
