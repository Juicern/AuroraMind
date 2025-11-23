# AuroraMind Scaffold

Three service folders aligned to the PRD/TD:
- `frontend/` – React + TypeScript SPA with chat streaming UI, KB switcher, and document upload.
- `app-service/` – Go REST + SSE gateway (auth, sessions, docs, streaming to AI service).
- `ai-service/` – FastAPI stub handling `/internal/ingest` and `/internal/rag/query/stream`.

## Quickstart
1) Python AI Service  
```
cd ai-service
python -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt
APP_PORT=9000 uvicorn main:app --host 0.0.0.0 --port 9000 --reload
```

2) Go App Service  
```
cd app-service
APP_PORT=8080 \
AI_SERVICE_URL=http://localhost:9000 \
LOCAL_STORAGE_PATH=./storage \
SERVICE_TOKEN=local-demo \
go run main.go
```

3) Frontend  
```
cd frontend
npm install
VITE_APP_API_BASE=http://localhost:8080 npm run dev -- --host --port 5173
```

Open http://localhost:5173, log in with any email/password, create a session, upload a doc, and send a chat. Go re-streams tokens from the Python stream as SSE.

### Docker Compose (local stack)
```
docker compose up --build
```

- Frontend: http://localhost:4173 (calls Go on :8080)
- Go App Service: http://localhost:8080
- Python AI Service: http://localhost:9000
- Postgres: localhost:5432 (user/pass/db = aurora/aurora/auroramind)

.env.example includes the main knobs (copy to .env and adjust as needed).

### Make targets (quickstart)
- `make ai-setup` – create venv + install deps for `ai-service`
- `make ai-run` – start FastAPI (needs `OPENAI_API_KEY` env if you want LangChain path)
- `make go-run` – run Go app service pointing at local AI service
- `make frontend-install` / `make frontend-dev` – install deps then run Vite dev server
- `make compose-up` / `make compose-down` – full stack via Docker

## Notable APIs
- Frontend → Go  
  - `POST /v1/auth/login`  
  - `POST /v1/sessions` and `GET /v1/sessions`  
  - `POST /v1/sessions/{id}/messages/stream` (SSE)  
  - `POST /v1/kb/{id}/documents` (multipart upload)  
  - `GET /v1/kb/{id}/documents`
- Go → Python  
  - `POST /internal/ingest` after uploads  
  - `POST /internal/rag/query/stream` for streaming completions

## Config Reference
- Go: `APP_PORT`, `AI_SERVICE_URL`, `SERVICE_TOKEN`, `LOCAL_STORAGE_PATH`
- Python: `APP_PORT`, `SERVICE_TOKEN`, `OPENAI_CHAT_MODEL`, `PINECONE_INDEX_NAME`
- Frontend: `VITE_APP_API_BASE`

## Next Steps
- Wire Postgres + migrations for sessions/messages/docs per TD schema.
- Swap FastAPI stub with real OpenAI + Pinecone and connect to S3/local storage switch.
- Harden auth with real JWT signing/verification.
- Add ingestion status polling and chunk provenance display in the UI.
