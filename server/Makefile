.PHONY: build build-arm64 build-armv7 build-armv6 dev clean copy-static

# Build for current platform
build: copy-static
	go build -o bin/sentryusb .

# Build for Raspberry Pi 4/5 (64-bit ARM)
build-arm64: copy-static
	GOOS=linux GOARCH=arm64 go build -o bin/sentryusb-linux-arm64 .

# Build for Raspberry Pi Zero W / Zero 2 (32-bit ARM)
build-armv7: copy-static
	GOOS=linux GOARCH=arm GOARM=7 go build -o bin/sentryusb-linux-armv7 .

# Build for Raspberry Pi Zero W (original, ARMv6)
build-armv6: copy-static
	GOOS=linux GOARCH=arm GOARM=6 go build -o bin/sentryusb-linux-armv6 .

# Build all Pi targets
build-all: build-arm64 build-armv7 build-armv6

# Copy built frontend into static/ for embedding
copy-static:
	@mkdir -p static
	@if [ -d ../web/dist ]; then \
		rm -rf static/*; \
		cp -r ../web/dist/* static/; \
		echo "Copied frontend build to static/"; \
	else \
		echo "Error: ../web/dist not found. Run 'npm run build' in web/ first." >&2; \
		exit 1; \
	fi

# Run in development mode (no static embedding, proxy to Vite)
dev:
	go run . -dev -port 8788

# Clean build artifacts
clean:
	rm -rf bin/ static/*
	touch static/.gitkeep
