# makerApp: one Go binary with the website, the device API and the makerDevice
# firmware baked in. It deploys to Sideyard; AGENTS.md has the contract.
#
# Local development:   make api   (Go on :3000)   and   make web-dev   (Vite on :5173)
# Deploy from laptop:  put SIDEYARD_URL/PROJECT/TOKEN in .env.deploy, then make deploy
#
#   make firmware     build the firmware (PlatformIO) into internal/fw/bin; commit the result
#   make test         run the game check and the Go tests
#   make games-check  run every downloadable game on this computer (bin/gamecheck)

GOARCH ?= amd64

FW_DIR   := firmware
FW_OUT   := $(FW_DIR)/.pio/build/feather_s3_rtft
EMBED    := internal/fw/bin
BOOT_APP := $(HOME)/.platformio/packages/framework-arduinoespressif32/tools/partitions/boot_app0.bin
PIO      ?= pio
LUA_DIR  := $(FW_DIR)/lib/lua/src

.PHONY: api web-dev web build deploy firmware test games-check clean

api:
	go run .

web-dev:
	cd web && npm run dev

web:
	cd web && npm ci && npm run build

# A Linux binary for the server, with the frontend and firmware embedded.
build: web
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -trimpath -ldflags="-s -w" -o app .

deploy: build
	@set -a; [ -f .env.deploy ] && . ./.env.deploy; set +a; ./scripts/deploy.sh app

firmware:
	cd $(FW_DIR) && $(PIO) run
	mkdir -p $(EMBED)
	cp $(FW_OUT)/bootloader.bin $(FW_OUT)/partitions.bin $(FW_OUT)/firmware.bin $(EMBED)/
	cp $(BOOT_APP) $(EMBED)/
	cp $(FW_DIR)/version.txt $(EMBED)/

test: games-check
	go test ./...

games-check:
	mkdir -p bin/gamecheck-obj
	cd bin/gamecheck-obj && cc -O2 -w -c $(addprefix ../../,$(wildcard $(LUA_DIR)/*.c))
	c++ -O2 -std=c++17 -I$(LUA_DIR) -I$(FW_DIR)/src -o bin/gamecheck \
		$(FW_DIR)/tools/gamecheck.cpp $(FW_DIR)/src/lua_api.cpp $(FW_DIR)/src/game_vm.cpp bin/gamecheck-obj/*.o
	./bin/gamecheck internal/games/*.lua

clean:
	rm -rf bin app $(FW_DIR)/.pio/build
