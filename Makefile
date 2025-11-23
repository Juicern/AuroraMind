SHELL := /bin/bash

.PHONY: help ai-setup ai-run ai-stop go-run go-build frontend-install frontend-dev frontend-build frontend-local compose-up compose-down

help:
	@echo "Common targets:"
	@echo "  make ai-setup        # create venv and install Python deps for ai-service"
	@echo "  make ai-run          # run FastAPI dev server with uvicorn (ai-service)"
	@echo "  make go-run          # run Go app-service locally"
	@echo "  make go-build        # build Go binary"
	@echo "  make frontend-install# npm install for frontend"
	@echo "  make frontend-dev    # start Vite dev server"
	@echo "  make frontend-build  # build production assets"
	@echo "  make compose-up      # docker compose up --build"
	@echo "  make compose-down    # docker compose down"

AI_ENV?=.venv
AI_PORT?=9000
GO_PORT?=8080
FE_PORT?=5173

ai-setup:
	cd ai-service && test -d $(AI_ENV) || python3 -m venv $(AI_ENV)
	cd ai-service && source $(AI_ENV)/bin/activate && pip install -r requirements.txt

ai-run:
	cd ai-service && if [ ! -d $(AI_ENV) ]; then python3 -m venv $(AI_ENV) && source $(AI_ENV)/bin/activate && pip install -r requirements.txt; fi
	cd ai-service && \
		( [ -f ../.env ] && set -a && source ../.env && set +a || true ) && \
		source $(AI_ENV)/bin/activate && APP_PORT=$(AI_PORT) uvicorn main:app --host 0.0.0.0 --port $(AI_PORT) --reload

ai-stop:
	- lsof -ti tcp:$(AI_PORT) | xargs kill -15

go-run:
	cd app-service && APP_PORT=$(GO_PORT) AI_SERVICE_URL=http://localhost:$(AI_PORT) LOCAL_STORAGE_PATH=./storage SERVICE_TOKEN=local-demo go run main.go

go-build:
	cd app-service && go build -o auroramind-app main.go

frontend-install:
	cd frontend && npm install

frontend-dev:
	cd frontend && VITE_APP_API_BASE=http://localhost:$(GO_PORT) npm run dev -- --host --port $(FE_PORT)

frontend-build:
	cd frontend && npm run build

frontend-local:
	cd frontend && VITE_APP_API_BASE=http://localhost:$(GO_PORT) npm run dev -- --host --port $(FE_PORT)

compose-up:
	docker compose up --build

compose-down:
	docker compose down
