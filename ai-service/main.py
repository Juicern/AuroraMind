import asyncio
import json
import logging
import os
import time
from dataclasses import dataclass, field
from functools import lru_cache
from pathlib import Path
from typing import AsyncIterator, Dict, List, Optional

import httpx
from fastapi import BackgroundTasks, FastAPI, HTTPException, Request
from fastapi.responses import StreamingResponse
from langchain.text_splitter import RecursiveCharacterTextSplitter
from langchain_core.output_parsers import StrOutputParser
from langchain_core.prompts import ChatPromptTemplate
from langchain_openai import ChatOpenAI, OpenAIEmbeddings
from pinecone import Pinecone
from pypdf import PdfReader

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")


def get_env(key: str, default: str) -> str:
    return os.getenv(key, default)


SERVICE_TOKEN = get_env("SERVICE_TOKEN", "")
OPENAI_MODEL = get_env("OPENAI_CHAT_MODEL", "gpt-4.1-mini")
APP_PORT = int(get_env("APP_PORT", "9000"))
VECTOR_INDEX = get_env("PINECONE_INDEX_NAME", "kb-index")
OPENAI_API_KEY = os.getenv("OPENAI_API_KEY", "")
OPENAI_EMBED_MODEL = get_env("OPENAI_EMBED_MODEL", "text-embedding-3-small")


app = FastAPI(title="AuroraMind AI Service", version="0.1.0")


@dataclass
class DocumentRecord:
    document_id: str
    collection_id: str
    storage_uri: str
    title: str
    status: str = "queued"
    note: Optional[str] = None
    created_at: float = field(default_factory=time.time)


DOCUMENTS: Dict[str, DocumentRecord] = {}
pc_client: Optional[Pinecone] = None


def get_pinecone() -> Optional[Pinecone]:
    global pc_client
    if pc_client:
        return pc_client
    api_key = os.getenv("PINECONE_API_KEY")
    if not api_key:
        return None
    pc_client = Pinecone(api_key=api_key)
    return pc_client


@app.middleware("http")
async def verify_service_token(request: Request, call_next):
    if request.url.path.startswith("/internal/") and SERVICE_TOKEN:
        token = request.headers.get("x-service-token")
        if token != SERVICE_TOKEN:
            raise HTTPException(status_code=401, detail="unauthorized")
    return await call_next(request)


@app.get("/health")
async def health() -> Dict[str, str]:
    return {"status": "ok", "vector_index": VECTOR_INDEX, "model": OPENAI_MODEL}


@app.post("/internal/ingest")
async def ingest(request: Request, tasks: BackgroundTasks) -> Dict[str, str]:
    payload = await request.json()
    required = ["document_id", "collection_id", "storage_uri"]
    if any(field not in payload for field in required):
        raise HTTPException(status_code=400, detail="missing required fields")

    record = DocumentRecord(
        document_id=payload["document_id"],
        collection_id=payload["collection_id"],
        storage_uri=payload["storage_uri"],
        title=payload.get("title") or "Untitled",
        status="processing",
    )
    DOCUMENTS[record.document_id] = record
    logging.info("queued ingest: %s", record)

    tasks.add_task(process_ingestion, record.document_id)
    return {"status": "accepted", "document_id": record.document_id}


async def process_ingestion(document_id: str) -> None:
    record = DOCUMENTS.get(document_id)
    if not record:
        return
    try:
        text = extract_text(record.storage_uri)
        splitter = RecursiveCharacterTextSplitter(chunk_size=800, chunk_overlap=120)
        chunks = splitter.split_text(text)
        if not chunks:
            raise RuntimeError("no text extracted")

        embeddings = OpenAIEmbeddings(model=OPENAI_EMBED_MODEL, api_key=OPENAI_API_KEY) if OPENAI_API_KEY else None
        pc = get_pinecone()
        if not embeddings or not pc:
            raise RuntimeError("missing OPENAI_API_KEY or PINECONE_API_KEY for ingestion")

        upsert_to_pinecone(pc, chunks, record.collection_id, record.document_id, embeddings)
        record.status = "ready"
        record.note = f"Upserted {len(chunks)} chunks to Pinecone."
        logging.info("completed ingest: %s chunks=%d", record.document_id, len(chunks))
    except Exception as exc:  # pragma: no cover
        record.status = "error"
        record.note = str(exc)
        logging.error("ingestion failed for %s: %s", document_id, exc)


@app.post("/internal/rag/query/stream")
async def rag_stream(request: Request) -> StreamingResponse:
    payload = await request.json()
    prompt = payload.get("prompt")
    session_id = payload.get("session_id", "")
    kb_id = payload.get("kb_id", "")
    if not prompt:
        raise HTTPException(status_code=400, detail="prompt is required")

    logging.info(
        "streaming response for session=%s kb=%s prompt=%s",
        session_id,
        kb_id,
        prompt[:64],
    )

    async def token_stream() -> AsyncIterator[bytes]:
        try:
            context, _sources = await retrieve_context(prompt, kb_id or "kb-default")
            chain = get_langchain_chain()
            async for chunk in chain.astream({"question": prompt, "context": context}):
                yield chunk.encode("utf-8") + b"\n"
        except Exception as exc:  # pragma: no cover
            logging.warning("RAG streaming failed, falling back: %s", exc)
            synthetic = [
                "Synthesizing an AuroraMind reply. ",
                "This stubbed AI service will echo your prompt back. ",
                f'Prompt: "{prompt}". ',
                "Connect Pinecone and OpenAI to replace this path.",
            ]
            for piece in synthetic:
                await asyncio.sleep(0.12)
                yield piece.encode("utf-8") + b"\n"

    return StreamingResponse(token_stream(), media_type="text/plain")


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=APP_PORT)


@lru_cache
def get_langchain_chain():
    """Build a simple LangChain streaming chain."""
    prompt = ChatPromptTemplate.from_template(
        "You are AuroraMind's AI assistant. Use the provided context to answer concisely.\n\nContext: {context}\n\nQuestion: {question}"
    )
    llm = ChatOpenAI(model=OPENAI_MODEL, temperature=0.3, streaming=True)
    return prompt | llm | StrOutputParser()


def extract_text(path: str) -> str:
    p = Path(path)
    if not p.exists():
        raise FileNotFoundError(f"file not found: {path}")
    if p.suffix.lower() == ".pdf":
        reader = PdfReader(p)
        return "\n".join(page.extract_text() or "" for page in reader.pages)
    with open(p, "r", encoding="utf-8", errors="ignore") as f:
        return f.read()


def upsert_to_pinecone(pc: Pinecone, chunks: List[str], collection_id: str, doc_id: str, embeddings: OpenAIEmbeddings) -> None:
    index = pc.Index(VECTOR_INDEX)
    vectors = []
    for i, chunk in enumerate(chunks):
        embedding = embeddings.embed_query(chunk)
        vectors.append(
            {
                "id": f"{doc_id}-chunk-{i}",
                "values": embedding,
                "metadata": {
                    "collection_id": collection_id,
                    "document_id": doc_id,
                    "chunk_id": f"chunk-{i}",
                    "text": chunk,
                },
            }
        )
    index.upsert(vectors=vectors, namespace=collection_id)


async def retrieve_context(query: str, collection_id: str) -> (str, List[Dict[str, str]]):
    pc = get_pinecone()
    if not pc or not OPENAI_API_KEY:
        raise RuntimeError("vector or embedding client missing")
    index = pc.Index(VECTOR_INDEX)
    embedder = OpenAIEmbeddings(model=OPENAI_EMBED_MODEL, api_key=OPENAI_API_KEY)
    q_emb = embedder.embed_query(query)
    res = index.query(vector=q_emb, top_k=5, namespace=collection_id, include_metadata=True)
    matches = res.matches if hasattr(res, "matches") else res["matches"]
    contexts = []
    sources = []
    for m in matches:
        md = m.metadata or {}
        text = md.get("text", "")
        contexts.append(text)
        sources.append(
            {
                "collection_id": md.get("collection_id", collection_id),
                "document_id": md.get("document_id", ""),
                "chunk_id": md.get("chunk_id", ""),
                "score": float(m.score) if hasattr(m, "score") else md.get("score", 0),
            }
        )
    return "\n\n".join(contexts), sources
