# AuroraMind – Product Requirements Document (PRD)

## 1. Overview
AuroraMind is a personal knowledge‑base AI platform designed for self‑use. It allows the user to upload documents, organize them into collections, and interact with an AI chatbot that retrieves relevant information through Retrieval‑Augmented Generation (RAG).

The system architecture includes:
- React + TypeScript Frontend  
- Golang App Service (REST + SSE)  
- Python AI Service (FastAPI + LangChain)  
- PostgreSQL relational DB  
- Pinecone vector DB  
- Local document storage (MVP), configurable to Amazon S3 later

---

## 2. Goals

### 2.1 Functional Goals
- Provide a knowledge‑aware chatbot that uses the user’s documents to answer questions.
- Allow users to upload and manage documents.
- Organize documents into Knowledge Base collections.
- Stream chat responses in real‑time using SSE.
- Maintain chat sessions and history.
- Provide explainability: list which chunks were used to answer.

### 2.2 Non‑Functional Goals
- Latency: 3–5 seconds P95 for RAG queries.
- Scalable service separation (Go ↔ Python).
- Flexible storage backends (local → S3).
- Secure authentication (JWT).
- Clean extensibility for future features.

---

## 3. Core User Stories

### 3.1 Knowledge Base
- As a user, I can create knowledge base collections.
- As a user, I can upload documents into collections.
- As a user, I can check document ingestion status.
- As a user, I can view the list of documents.

### 3.2 Chat
- As a user, I can create chat sessions.
- As a user, I can send messages and receive streaming responses.
- As a user, I can see which KB documents/chunks contributed to the answer.

### 3.3 Storage & Pipelines
- As a user, I want to ingest documents via chunking and embedding.
- As a user, I want to store documents locally first, and optionally in S3 later.

---

## 4. Requirements

### 4.1 Functional Requirements
- JWT-authenticated REST API.
- SSE streaming from Go → FE.
- Python AI Service must handle all chunking, embeddings, and Pinecone writes.
- PostgreSQL must store:
  - Users
  - Sessions
  - Messages
  - KB collections
  - Documents
  - Text chunks (without embeddings)
- Pinecone stores embeddings only.

### 4.2 Non‑Functional Requirements
- Services are stateless.
- Easy Docker deployment (local + EC2).
- Robust logging & error handling.
- Configurable environment scheme.

---

## 5. Constraints & Out of Scope
- No multi‑tenant enterprise features.
- No collaborative document editing.
- No billing or paywall.
- No self‑hosted LLM model (initial release).

---

## 6. Success Metrics
- <5s average query time.
- Reliable ingestion for PDFs and text documents.
- Stable real‑time streaming with no disconnections.
- Minimal operational overhead.
