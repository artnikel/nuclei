APP_NAME=nuclei-gui
MAIN_PKG=./cmd/main.go
BUILD_DIR=build

LD_FLAGS="-s -w"
GO_FLAGS=-trimpath

.PHONY: all clean build release

all: build

build:
	go build -ldflags=$(LD_FLAGS) $(GO_FLAGS) -o $(BUILD_DIR)/$(APP_NAME) $(MAIN_PKG)

garble:
	GARBLE_DIR=$(BUILD_DIR) garble build -ldflags=$(LD_FLAGS) -o $(BUILD_DIR)/$(APP_NAME) $(MAIN_PKG)

upx:
	upx --best --lzma $(BUILD_DIR)/$(APP_NAME)

release: garble upx

clean:
	rm -rf $(BUILD_DIR)
