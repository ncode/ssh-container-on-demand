BINARY ?= ssh-container-on-demand
GOOS ?= $(shell go env GOOS)

.PHONY: build guest-bootstrap
ifeq ($(GOOS),darwin)
build: guest-bootstrap
	SDKROOT="$$(xcrun --sdk macosx --show-sdk-path)" && \
	SDKROOT="$$SDKROOT" CGO_ENABLED=1 go build -o "$(BINARY)" .
	codesign --force --sign - --entitlements virtualization.entitlements "$(BINARY)"
else
build:
	go build -o "$(BINARY)" .
endif

guest-bootstrap:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o internal/microvm/assets/guest-bootstrap ./internal/guestbootstrap
