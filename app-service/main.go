package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type Config struct {
	Port             string
	AIServiceURL     string
	ServiceToken     string
	LocalStoragePath string
	DBDSN            string
}

type ChatSession struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	DefaultKBID  string    `json:"default_kb_id"`
	CreatedAt    time.Time `json:"created_at"`
	LastActivity time.Time `json:"last_activity"`
}

type ChatMessage struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type Document struct {
	ID            string    `json:"id"`
	CollectionID  string    `json:"collection_id"`
	Title         string    `json:"title"`
	StorageURI    string    `json:"storage_uri"`
	Status        string    `json:"status"`
	StatusMessage string    `json:"status_message"`
	UploadedAt    time.Time `json:"uploaded_at"`
}

type Server struct {
	cfg      Config
	sessions map[string]*ChatSession
	messages map[string][]ChatMessage
	documents map[string][]Document
	mu       sync.Mutex
	client   *http.Client
	db       *sql.DB
}

func main() {
	cfg := loadConfig()
	server := &Server{
		cfg:       cfg,
		sessions:  map[string]*ChatSession{},
		messages:  map[string][]ChatMessage{},
		documents: map[string][]Document{},
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	if cfg.DBDSN != "" {
		db, err := sql.Open("postgres", cfg.DBDSN)
		if err != nil {
			log.Fatalf("failed to open db: %v", err)
		}
		if err := db.Ping(); err != nil {
			log.Fatalf("failed to ping db: %v", err)
		}
		server.db = db
		if err := server.ensureDocumentsTable(); err != nil {
			log.Fatalf("failed to ensure documents table: %v", err)
		}
	}

	router := chi.NewRouter()
	router.Use(middleware.Logger)
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS", "DELETE"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Service-Token"},
		AllowCredentials: false,
	}))

	router.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	router.Post("/v1/auth/login", server.handleLogin)
	router.Route("/v1", func(r chi.Router) {
		r.Use(server.authGuard)

		r.Post("/sessions", server.createSession)
		r.Get("/sessions", server.listSessions)
		r.Post("/sessions/{id}/messages/stream", server.streamMessage)

		r.Post("/kb/{id}/documents", server.uploadDocument)
		r.Get("/kb/{id}/documents", server.listDocuments)
		r.Delete("/kb/{id}/documents/{docId}", server.deleteDocument)
	})

	addr := ":" + cfg.Port
	log.Printf("Go App Service listening on %s", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

func loadConfig() Config {
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}

	storage := os.Getenv("LOCAL_STORAGE_PATH")
	if storage == "" {
		storage = "./storage"
	}

	return Config{
		Port:             port,
		AIServiceURL:     strings.TrimSuffix(os.Getenv("AI_SERVICE_URL"), "/"),
		ServiceToken:     os.Getenv("SERVICE_TOKEN"),
		LocalStoragePath: storage,
		DBDSN:            os.Getenv("DB_DSN"),
	}
}

func (s *Server) authGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			http.Error(w, "missing Authorization header", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	type loginRequest struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	type loginResponse struct {
		Token   string `json:"token"`
		UserID  string `json:"user_id"`
		Expires int    `json:"expires_in_seconds"`
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	userID := uuid.NewString()
	resp := loginResponse{
		Token:   "demo-" + userID,
		UserID:  userID,
		Expires: 24 * 60 * 60,
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	type sessionRequest struct {
		Title       string `json:"title"`
		DefaultKBID string `json:"default_kb_id"`
	}
	var req sessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	session := &ChatSession{
		ID:           uuid.NewString(),
		Title:        firstNonEmpty(req.Title, "New Chat"),
		DefaultKBID:  req.DefaultKBID,
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
	}

	s.mu.Lock()
	s.sessions[session.ID] = session
	s.mu.Unlock()

	writeJSON(w, http.StatusCreated, session)
}

func (s *Server) listSessions(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sessions := make([]*ChatSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) streamMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	s.mu.Lock()
	session, ok := s.sessions[sessionID]
	s.mu.Unlock()
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	type messageRequest struct {
		Content string `json:"content"`
		KBID    string `json:"kb_id"`
	}
	var req messageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		http.Error(w, "content required", http.StatusBadRequest)
		return
	}

	userMessage := ChatMessage{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      "user",
		Content:   req.Content,
		CreatedAt: time.Now(),
	}

	s.mu.Lock()
	s.messages[sessionID] = append(s.messages[sessionID], userMessage)
	sessionsLast := s.sessions[sessionID]
	sessionsLast.LastActivity = time.Now()
	s.mu.Unlock()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ctx := r.Context()
	tokenCh := make(chan string)
	go s.forwardToAI(ctx, tokenCh, req.Content, sessionID, req.KBID)

	var assistantContent strings.Builder

	for token := range tokenCh {
		if token == "" {
			continue
		}
		assistantContent.WriteString(token)
		fmt.Fprintf(w, "event: token\n")
		fmt.Fprintf(w, "data: %s\n\n", escapeData(token))
		flusher.Flush()
	}

	assistantMessage := ChatMessage{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      "assistant",
		Content:   assistantContent.String(),
		CreatedAt: time.Now(),
	}

	s.mu.Lock()
	s.messages[sessionID] = append(s.messages[sessionID], assistantMessage)
	session.LastActivity = time.Now()
	s.mu.Unlock()

	donePayload := map[string]any{
		"message_id": assistantMessage.ID,
		"session_id": sessionID,
		"kb_id":      firstNonEmpty(req.KBID, session.DefaultKBID),
		"tokens":     len(assistantContent.String()) / 4, // rough placeholder
	}
	fmt.Fprintf(w, "event: done\n")
	writeSSEJSON(w, donePayload)
	flusher.Flush()
}

func (s *Server) uploadDocument(w http.ResponseWriter, r *http.Request) {
	collectionID := chi.URLParam(r, "id")
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	if err := os.MkdirAll(filepath.Join(s.cfg.LocalStoragePath, collectionID), 0o755); err != nil {
		http.Error(w, "cannot create storage path", http.StatusInternalServerError)
		return
	}

	documentID := uuid.NewString()
	dstPath := filepath.Join(s.cfg.LocalStoragePath, collectionID, documentID+"_"+sanitizeFileName(header.Filename))
	dst, err := os.Create(dstPath)
	if err != nil {
		http.Error(w, "cannot store file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		http.Error(w, "failed to save file", http.StatusInternalServerError)
		return
	}

	absPath, _ := filepath.Abs(dstPath)

	doc := Document{
		ID:           documentID,
		CollectionID: collectionID,
		Title:        header.Filename,
		StorageURI:   absPath,
		Status:       "uploaded",
		UploadedAt:   time.Now(),
	}

	if s.db != nil {
		if err := s.insertDocument(doc); err != nil {
			http.Error(w, "failed to save document metadata", http.StatusInternalServerError)
			return
		}
	} else {
		s.mu.Lock()
		s.documents[collectionID] = append(s.documents[collectionID], doc)
		s.mu.Unlock()
	}

	go s.notifyIngest(doc)
	writeJSON(w, http.StatusCreated, doc)
}

func (s *Server) listDocuments(w http.ResponseWriter, r *http.Request) {
	collectionID := chi.URLParam(r, "id")
	var docs []Document
	if s.db != nil {
		var err error
		docs, err = s.fetchDocuments(collectionID)
		if err != nil {
			http.Error(w, "failed to load documents", http.StatusInternalServerError)
			return
		}
	} else {
		s.mu.Lock()
		docs = append([]Document{}, s.documents[collectionID]...)
		s.mu.Unlock()
	}
	writeJSON(w, http.StatusOK, docs)
}

func (s *Server) deleteDocument(w http.ResponseWriter, r *http.Request) {
	collectionID := chi.URLParam(r, "id")
	documentID := chi.URLParam(r, "docId")

	var doc Document
	if s.db != nil {
		existing, err := s.fetchDocument(collectionID, documentID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "document not found", http.StatusNotFound)
				return
			}
			http.Error(w, "failed to load document", http.StatusInternalServerError)
			return
		}
		doc = existing
		if err := s.deleteDocumentDB(collectionID, documentID); err != nil {
			http.Error(w, "failed to delete document", http.StatusInternalServerError)
			return
		}
	} else {
		s.mu.Lock()
		docs := s.documents[collectionID]
		index := -1
		for i, d := range docs {
			if d.ID == documentID {
				index = i
				doc = d
				break
			}
		}
		if index == -1 {
			s.mu.Unlock()
			http.Error(w, "document not found", http.StatusNotFound)
			return
		}
		s.documents[collectionID] = append(docs[:index], docs[index+1:]...)
		s.mu.Unlock()
	}

	if doc.StorageURI != "" {
		_ = os.Remove(doc.StorageURI)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "document_id": documentID})
}

func (s *Server) forwardToAI(ctx context.Context, tokenCh chan<- string, content, sessionID, kbID string) {
	defer close(tokenCh)

	body := map[string]string{
		"prompt":     content,
		"session_id": sessionID,
		"kb_id":      kbID,
	}
	if s.cfg.AIServiceURL == "" {
		s.streamFallback(content, tokenCh)
		return
	}

	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.AIServiceURL+"/internal/rag/query/stream", bytes.NewReader(payload))
	if err != nil {
		s.streamFallback(content, tokenCh)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if s.cfg.ServiceToken != "" {
		req.Header.Set("X-Service-Token", s.cfg.ServiceToken)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("ai service request failed: %v", err)
		s.streamFallback(content, tokenCh)
		return
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		case tokenCh <- scanner.Text():
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("ai stream error: %v", err)
	}
}

func (s *Server) notifyIngest(doc Document) {
	if s.cfg.AIServiceURL == "" {
		return
	}
	payload := map[string]string{
		"document_id":  doc.ID,
		"collection_id": doc.CollectionID,
		"storage_uri":  doc.StorageURI,
		"title":        doc.Title,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, s.cfg.AIServiceURL+"/internal/ingest", bytes.NewReader(body))
	if err != nil {
		log.Printf("cannot build ingest request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if s.cfg.ServiceToken != "" {
		req.Header.Set("X-Service-Token", s.cfg.ServiceToken)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("ingest notify failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("ingest notify returned %d", resp.StatusCode)
	}
}

func (s *Server) streamFallback(content string, tokenCh chan<- string) {
	tokens := []string{
		"AI service unavailable. ",
		"This is a placeholder stream for: ",
		content,
	}
	for _, t := range tokens {
		tokenCh <- t
		time.Sleep(150 * time.Millisecond)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeSSEJSON(w http.ResponseWriter, payload any) {
	b, _ := json.Marshal(payload)
	fmt.Fprintf(w, "data: %s\n\n", b)
}

func escapeData(data string) string {
	data = strings.ReplaceAll(data, "\n", " ")
	return data
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func sanitizeFileName(name string) string {
	name = strings.ReplaceAll(name, " ", "_")
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.', r == '_', r == '-':
			return r
		default:
			return '-'
		}
	}, name)
}

func (s *Server) ensureDocumentsTable() error {
	stmt := `
CREATE TABLE IF NOT EXISTS documents (
  id TEXT PRIMARY KEY,
  collection_id TEXT NOT NULL,
  title TEXT NOT NULL,
  storage_uri TEXT NOT NULL,
  status TEXT,
  status_message TEXT,
  uploaded_at TIMESTAMP WITH TIME ZONE
);`
	_, err := s.db.Exec(stmt)
	return err
}

func (s *Server) insertDocument(doc Document) error {
	_, err := s.db.Exec(
		`INSERT INTO documents (id, collection_id, title, storage_uri, status, status_message, uploaded_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		doc.ID, doc.CollectionID, doc.Title, doc.StorageURI, doc.Status, doc.StatusMessage, doc.UploadedAt,
	)
	return err
}

func (s *Server) fetchDocuments(collectionID string) ([]Document, error) {
	rows, err := s.db.Query(
		`SELECT id, collection_id, title, storage_uri, status, status_message, uploaded_at
		 FROM documents WHERE collection_id = $1 ORDER BY uploaded_at DESC`, collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []Document
	for rows.Next() {
		var d Document
		if err := rows.Scan(&d.ID, &d.CollectionID, &d.Title, &d.StorageURI, &d.Status, &d.StatusMessage, &d.UploadedAt); err != nil {
			return nil, err
		}
		docs = append(docs, d)
	}
	return docs, rows.Err()
}

func (s *Server) fetchDocument(collectionID, docID string) (Document, error) {
	var d Document
	err := s.db.QueryRow(
		`SELECT id, collection_id, title, storage_uri, status, status_message, uploaded_at
		 FROM documents WHERE collection_id=$1 AND id=$2`, collectionID, docID,
	).Scan(&d.ID, &d.CollectionID, &d.Title, &d.StorageURI, &d.Status, &d.StatusMessage, &d.UploadedAt)
	return d, err
}

func (s *Server) deleteDocumentDB(collectionID, docID string) error {
	_, err := s.db.Exec(`DELETE FROM documents WHERE collection_id=$1 AND id=$2`, collectionID, docID)
	return err
}
