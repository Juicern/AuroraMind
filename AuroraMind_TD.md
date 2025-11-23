# AuroraMind – Technical Design (TD)

## 1. Architecture Overview

AuroraMind consists of five layers:
1. React + TypeScript Frontend
2. Golang App Service (REST + SSE)
3. Python AI Service (FastAPI + LangChain)
4. PostgreSQL for metadata & chunks
5. Pinecone as vector database

Document storage:
- Local filesystem for MVP
- Switchable to AWS S3 via config

---

## 2. Component Responsibilities

### 2.1 Frontend (React + TS)
- Chat UI with SSE streaming
- Upload documents
- View documents & KB collections
- Session management UI

### 2.2 Go App Service
Responsible for:
- JWT authentication
- All REST API for the frontend
- SSE streaming of AI responses
- Session & chat history handling
- KB collections & document metadata
- Triggering ingestion via Python AI Service
- Does NOT handle chunking or embeddings

### 2.3 Python AI Service
Responsible for:
- Document ingestion
- Text extraction & chunking
- Embedding using OpenAI
- Vector upserts to Pinecone
- RAG pipeline: query → retrieve → generate
- Streaming model responses back to Go

### 2.4 PostgreSQL
Stores:
- Users
- Chat sessions
- Chat messages
- Knowledge base collections
- KB documents
- Text chunks

### 2.5 Pinecone
- One global index
- Namespaces per KB collection
- Stores embeddings only
- Metadata stored per vector:
  - collection_id
  - doc_id
  - chunk_id
  - user_id

---

## 3. Data Flow

### 3.1 Document Ingestion Flow
1. FE → Go: Upload document
2. Go → Python: `/internal/ingest`
3. Python:
   - Load file (local/S3)
   - Extract text
   - Chunk text
   - Insert chunks → Postgres
   - Create embeddings → Pinecone
   - Mark document as `ready`
4. FE polls or refreshes list

### 3.2 RAG Query Flow
1. FE sends message via SSE
2. Go stores user message
3. Go → Python `/rag/query/stream`
4. Python:
   - Embed query
   - Retrieve from Pinecone
   - Construct prompt
   - Stream response tokens
5. Go re-streams tokens to FE
6. After completion:
   - Store assistant message
   - Store context-chunk links

---

## 4. API Design

### 4.1 Frontend → Go (REST)
- `POST /v1/auth/login`
- `POST /v1/sessions`
- `GET /v1/sessions`
- `POST /v1/sessions/{id}/messages/stream` (SSE)
- `POST /v1/kb/{id}/documents`
- `GET /v1/kb/{id}/documents`

### 4.2 Go → Python
- `POST /internal/ingest`
- `POST /internal/rag/query/stream`

---

## 5. Database Schema (Logical)

### users
```
id UUID PK  
email TEXT UNIQUE  
password_hash TEXT  
```

### chat_sessions
```
id UUID PK  
user_id UUID  
title TEXT  
default_kb_id UUID  
```

### chat_messages
```
id UUID PK  
session_id UUID  
role TEXT  
content TEXT  
token_usage JSONB  
model TEXT  
```

### kb_collections
```
id UUID PK  
user_id UUID  
name TEXT  
description TEXT  
```

### kb_documents
```
id UUID PK  
collection_id UUID  
title TEXT  
storage_uri TEXT  
status TEXT  
status_message TEXT  
```

### kb_chunks
```
id UUID PK  
document_id UUID  
chunk_index INT  
text TEXT  
token_count INT  
```

### chat_message_context_chunks
```
message_id UUID  
chunk_id UUID  
rank INT  
score FLOAT  
```

---

## 6. Streaming Design

### Python → Go
- Chunked HTTP streaming

### Go → FE
- SSE (`Content-Type: text/event-stream`)
- Event types:
  - `token` (delta text)
  - `done` (final payload)
  - `error`

---

## 7. Configuration

```
APP_ENV=local|prod  
APP_PORT=8080  
DB_DSN=...  
JWT_SECRET=...  
AI_SERVICE_URL=...  
SERVICE_TOKEN=...  

STORAGE_BACKEND=local|s3  
LOCAL_STORAGE_PATH=/data/documents  
S3_BUCKET=...  

OPENAI_API_KEY=...  
OPENAI_CHAT_MODEL=gpt-4.1-mini  
OPENAI_EMBED_MODEL=text-embedding-3-small  

PINECONE_API_KEY=...  
PINECONE_INDEX_NAME=kb-index  
```

---

## 8. Deployment

### Local (Docker Compose)
- Go App Service
- Python AI Service
- PostgreSQL
- Pinecone = remote SaaS
- MinIO optional

### AWS EC2
- One EC2 for Go
- One EC2 for Python
- RDS for PostgreSQL
- S3 for documents
- Nginx or ALB for HTTPS

---

## 9. Future Extensions
- Multi-user / multi-tenant mode
- API key access for other apps
- Integration with calendar/email
- Scheduled ingestion
- Re‑rankers / hybrid search
