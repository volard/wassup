GO ?= go
BUILD_DIR ?= build
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)
BINARY ?= $(BUILD_DIR)/wassup

.PHONY: build clean test demo

build:
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -trimpath -o $(BINARY) .

test:
	$(GO) test ./...

demo:
	mkdir -p $(BUILD_DIR)
	$(GO) build -trimpath -o $(BUILD_DIR)/wassup-demo ./docs/demo
	docker build -q -f docs/demo/Dockerfile -t wassup-vhs docs/demo
	docker run --rm -v "$(CURDIR):/vhs" -e VHS_UID="$$(id -u)" -e VHS_GID="$$(id -g)" wassup-vhs docs/demo.tape

clean:
	rm -rf $(BUILD_DIR)
