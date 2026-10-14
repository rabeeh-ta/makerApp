# espHog: one Go binary with the website, device API and firmware baked in.
#
#   make            build firmware + server for this machine -> bin/esphog
#   make run        build and run locally on :8080
#   make linux      cross-compile for a Linux server (amd64 + arm64)
#   make test       run the game check and Go tests
#   make games-check run every downloadable game on this computer (bin/gamecheck)
#   make deploy     build for Linux and upload to Sideyard (settings in .env.deploy)

FW_DIR   := firmware
SRV_DIR  := server
FW_OUT   := $(FW_DIR)/.pio/build/feather_s3_rtft
EMBED    := $(SRV_DIR)/internal/fw/bin
BOOT_APP := $(HOME)/.platformio/packages/framework-arduinoespressif32/tools/partitions/boot_app0.bin
PIO      ?= pio

.PHONY: all firmware server run linux deploy test games-check clean

all: firmware server

firmware:
	cd $(FW_DIR) && $(PIO) run
	mkdir -p $(EMBED)
	cp $(FW_OUT)/bootloader.bin $(FW_OUT)/partitions.bin $(FW_OUT)/firmware.bin $(EMBED)/
	cp $(BOOT_APP) $(EMBED)/
	cp $(FW_DIR)/version.txt $(EMBED)/

server:
	mkdir -p bin
	cd $(SRV_DIR) && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o ../bin/esphog .

run: all
	./bin/esphog

linux: firmware
	mkdir -p bin
	cd $(SRV_DIR) && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o ../bin/esphog-linux-amd64 .
	cd $(SRV_DIR) && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o ../bin/esphog-linux-arm64 .

# Sideyard: .env.deploy holds SIDEYARD_URL, SIDEYARD_PROJECT and SIDEYARD_TOKEN.
# Sideyard health-checks the new release and keeps the old one serving if it fails.
SIDEYARD_ARCH ?= amd64

deploy: firmware
	mkdir -p bin
	cd $(SRV_DIR) && CGO_ENABLED=0 GOOS=linux GOARCH=$(SIDEYARD_ARCH) go build -trimpath -ldflags "-s -w" -o ../bin/esphog-sideyard .
	@set -a; . ./.env.deploy; set +a; \
	curl --fail-with-body -sS -H "Authorization: Bearer $$SIDEYARD_TOKEN" \
		-F binary=@bin/esphog-sideyard --form-string "message=firmware $$(cat $(FW_DIR)/version.txt)" \
		"$$SIDEYARD_URL/api/projects/$$SIDEYARD_PROJECT/deploys"; echo

test: games-check
	cd $(SRV_DIR) && go test ./...

LUA_DIR := $(FW_DIR)/lib/lua/src

games-check:
	mkdir -p bin/gamecheck-obj
	cd bin/gamecheck-obj && cc -O2 -w -c $(addprefix ../../,$(wildcard $(LUA_DIR)/*.c))
	c++ -O2 -std=c++17 -I$(LUA_DIR) -I$(FW_DIR)/src -o bin/gamecheck \
		$(FW_DIR)/tools/gamecheck.cpp $(FW_DIR)/src/lua_api.cpp $(FW_DIR)/src/game_vm.cpp bin/gamecheck-obj/*.o
	./bin/gamecheck $(SRV_DIR)/internal/games/*.lua

clean:
	rm -rf bin $(FW_DIR)/.pio/build
