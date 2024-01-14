
# Define the output directory
OUTPUT_DIR := bin
APP_NAME := statexec

# Get the current branch name
current_branch=$(shell git rev-parse --abbrev-ref HEAD)

CURRENT_VERSION=$(shell git describe --tags --abbrev=0)
MAJOR=$(shell echo $(CURRENT_VERSION) | cut -d. -f1)
MINOR=$(shell echo $(CURRENT_VERSION) | cut -d. -f2)
PATCH=$(shell echo $(CURRENT_VERSION) | cut -d. -f3)

# Get the latest Git tag
ifneq ($(shell git describe --tags --exact-match 2>/dev/null),)
	bin_version="$(shell git describe --tags)"
else
	bin_version="${current_branch}:$(shell git rev-parse --short HEAD)"
endif

# Define the build command
BUILD_CMD := go build -ldflags "-w -s -X main.version=$(bin_version)"

# Define the targets
.PHONY: all clean

version: 
	@echo $(bin_version)

# Version bumping
bump-major:
	$(eval NEW_MAJOR=$(shell echo $$(( $(MAJOR) + 1 )) ))
	@echo "Bumping major version..."
	git tag -a $(NEW_MAJOR).0.0 -m "Bump major version to $(NEW_MAJOR).0.0"
	@echo To push this tag execute : 
	@echo git push origin $(NEW_MAJOR).0.0

bump-minor:
	$(eval NEW_MINOR=$(shell echo $$(( $(MINOR) + 1 )) ))
	@echo "Bumping minor version..."
	git tag -a $(MAJOR).$(NEW_MINOR).0 -m "Bump minor version to $(MAJOR).$(NEW_MINOR).0"
	@echo To push this tag execute : 
	@echo git push origin $(MAJOR).$(NEW_MINOR).0

bump-patch:
	$(eval NEW_PATCH=$(shell echo $$(( $(PATCH) + 1 )) ))
	@echo "Bumping patch version..."
	git tag -a $(MAJOR).$(MINOR).$(NEW_PATCH) -m "Bump patch version to $(MAJOR).$(MINOR).$(NEW_PATCH)"
	@echo To push this tag execute : 
	@echo git push origin $(MAJOR).$(MINOR).$(NEW_PATCH)

git-push:
	git push && git push --tags

build: 
	@$(shell [ -e $(OUTPUT_DIR)/$(APP_NAME) ] && rm $(OUTPUT_DIR)/$(APP_NAME))
	@$(BUILD_CMD) -o $(OUTPUT_DIR)/$(APP_NAME)

all: linux darwin
linux: linux_amd64 linux_arm64

linux_amd64:
	@$(shell [ -e $(OUTPUT_DIR)/$(APP_NAME) ] && rm $(OUTPUT_DIR)/$(APP_NAME)-linux-amd64)
	@GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(BUILD_CMD) -o $(OUTPUT_DIR)/$(APP_NAME)-linux-amd64

linux_arm64:
	@$(shell [ -e $(OUTPUT_DIR)/$(APP_NAME) ] && rm $(OUTPUT_DIR)/$(APP_NAME)-linux-arm64)
	@GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(BUILD_CMD) -o $(OUTPUT_DIR)/$(APP_NAME)-linux-arm64

darwin: darwin_amd64 darwin_arm64

darwin_amd64:
	@$(shell [ -e $(OUTPUT_DIR)/$(APP_NAME) ] && rm $(OUTPUT_DIR)/$(APP_NAME)-darwin-amd64)
	@GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 $(BUILD_CMD) -o $(OUTPUT_DIR)/$(APP_NAME)-darwin-amd64

darwin_arm64:
	@$(shell [ -e $(OUTPUT_DIR)/$(APP_NAME) ] && rm $(OUTPUT_DIR)/$(APP_NAME)-darwin-arm64)
	@GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 $(BUILD_CMD) -o $(OUTPUT_DIR)/$(APP_NAME)-darwin-arm64

