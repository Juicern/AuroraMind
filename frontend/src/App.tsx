import { useEffect, useMemo, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import './App.css'

type ChatMessage = { role: 'user' | 'assistant'; content: string }
type Session = { id: string; title: string; default_kb_id?: string }
type DocumentItem = { id: string; title: string; storage_uri: string; status: string }

const API_BASE = (import.meta.env.VITE_APP_API_BASE as string) || 'http://localhost:8080'
const DEFAULT_KB = 'kb-default'

function App() {
  const [email, setEmail] = useState('demo@auroramind.ai')
  const [password, setPassword] = useState('password')
  const [token, setToken] = useState('')
  const [sessions, setSessions] = useState<Session[]>([])
  const [activeSession, setActiveSession] = useState<string>('')
  const [chatInput, setChatInput] = useState('')
  const [messages, setMessages] = useState<Record<string, ChatMessage[]>>({})
  const [isStreaming, setIsStreaming] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [selectedFile, setSelectedFile] = useState<File | null>(null)
  const [kbId, setKbId] = useState(DEFAULT_KB)
  const [documents, setDocuments] = useState<DocumentItem[]>([])
  const [status, setStatus] = useState('')
  const streamAbort = useRef<AbortController | null>(null)

  const headers = useMemo(
    () => ({
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
    }),
    [token],
  )

  useEffect(() => {
    if (!token) return
    fetchSessions()
    fetchDocuments()
  }, [token, kbId])

  const fetchSessions = async () => {
    const res = await fetch(`${API_BASE}/v1/sessions`, { headers })
    if (!res.ok) return
    const data: Session[] = await res.json()
    setSessions(data)
    if (data.length && !activeSession) {
      setActiveSession(data[0].id)
    }
  }

  const fetchDocuments = async () => {
    if (!token || !kbId) return
    const res = await fetch(`${API_BASE}/v1/kb/${kbId}/documents`, { headers })
    if (!res.ok) return
    setDocuments(await res.json())
  }

  const login = async (evt: FormEvent) => {
    evt.preventDefault()
    const res = await fetch(`${API_BASE}/v1/auth/login`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email, password }),
    })
    if (!res.ok) {
      setStatus('Login failed')
      return
    }
    const data = await res.json()
    setToken(data.token)
    setStatus('Authenticated. Ready to chat.')
  }

  const createSession = async () => {
    const res = await fetch(`${API_BASE}/v1/sessions`, {
      method: 'POST',
      headers,
      body: JSON.stringify({ title: `Chat ${sessions.length + 1}`, default_kb_id: kbId }),
    })
    if (!res.ok) return
    const session = await res.json()
    setSessions((prev) => [...prev, session])
    setActiveSession(session.id)
  }

  const handleStream = async () => {
    if (!chatInput || !activeSession || !token) return
    setIsStreaming(true)
    setStatus('Streaming from Go → Python → FE...')
    streamAbort.current = new AbortController()

    setMessages((prev) => ({
      ...prev,
      [activeSession]: [...(prev[activeSession] || []), { role: 'user', content: chatInput }, { role: 'assistant', content: '' }],
    }))

    try {
      const res = await fetch(`${API_BASE}/v1/sessions/${activeSession}/messages/stream`, {
        method: 'POST',
        headers: { ...headers, Accept: 'text/event-stream' },
        body: JSON.stringify({ content: chatInput, kb_id: kbId }),
        signal: streamAbort.current.signal,
      })

      if (!res.ok || !res.body) {
        setStatus('Stream failed to start')
        setIsStreaming(false)
        return
      }

      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''

      const applyToken = (text: string) => {
        setMessages((prev) => {
          const current = prev[activeSession] || []
          const updated = [...current]
          const last = updated[updated.length - 1]
          if (last && last.role === 'assistant') {
            updated[updated.length - 1] = { ...last, content: (last.content || '') + text }
          }
          return { ...prev, [activeSession]: updated }
        })
      }

      const processChunk = (chunk: string) => {
        const events = chunk.split('\n\n')
        events.forEach((evt) => {
          const lines = evt.split('\n')
          let event = 'message'
          let data = ''
          lines.forEach((line) => {
            if (line.startsWith('event:')) event = line.replace('event:', '').trim()
            if (line.startsWith('data:')) data += line.replace(/^data:\s?/, '') + '\n'
          })

          const payload = data.endsWith('\n') ? data.slice(0, -1) : data

          if (event === 'token') {
            applyToken(payload)
          } else if (event === 'done') {
            setStatus(`Completed stream ${payload}`)
          }
        })
      }

      while (true) {
        const { value, done } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const parts = buffer.split('\n\n')
        buffer = parts.pop() || ''
        parts.forEach(processChunk)
      }
    } catch (err) {
      setStatus('Stream cancelled or failed.')
    } finally {
      setChatInput('')
      setIsStreaming(false)
    }
  }

  const upload = async (evt: FormEvent) => {
    evt.preventDefault()
    if (!selectedFile) return
    setUploading(true)
    const form = new FormData()
    form.append('file', selectedFile)
    const res = await fetch(`${API_BASE}/v1/kb/${kbId}/documents`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
      body: form,
    })
    if (res.ok) {
      setStatus('Document uploaded, ingestion triggered.')
      setSelectedFile(null)
      fetchDocuments()
    } else {
      setStatus('Upload failed')
    }
    setUploading(false)
  }

  const stopStream = () => {
    streamAbort.current?.abort()
    setIsStreaming(false)
    setStatus('Stream cancelled')
  }

  const activeMessages = messages[activeSession] || []

  return (
    <div className="page">
      <header className="hero">
        <div>
          <p className="eyebrow">AuroraMind</p>
          <h1>Personal knowledge-base AI</h1>
          <p className="lede">
            Go App Service streams from the Python AI Service with SSE. Upload docs, build knowledge collections, and chat with your files.
          </p>
        </div>
        <div className="badge">{API_BASE}</div>
      </header>

      <section className="panel-grid">
        <div className="panel">
          <h2>Auth</h2>
          <form className="form" onSubmit={login}>
            <label>
              Email
              <input value={email} onChange={(e) => setEmail(e.target.value)} />
            </label>
            <label>
              Password
              <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
            </label>
            <button type="submit">Login</button>
          </form>
          <div className="status">{status || 'Awaiting login...'}</div>
        </div>

        <div className="panel">
          <h2>Knowledge Bases</h2>
          <label>
            Active KB
            <input value={kbId} onChange={(e) => setKbId(e.target.value)} />
          </label>
          <div className="docs">
            {documents.map((doc) => (
              <div key={doc.id} className="doc">
                <div className="doc-title">{doc.title}</div>
                <div className="doc-meta">{doc.status}</div>
                <div className="doc-path">{doc.storage_uri}</div>
              </div>
            ))}
            {!documents.length && <p className="muted">No documents yet.</p>}
          </div>
          <form className="upload" onSubmit={upload}>
            <input type="file" onChange={(e) => setSelectedFile(e.target.files?.[0] || null)} />
            <button disabled={uploading || !selectedFile}>{uploading ? 'Uploading...' : 'Upload & Ingest'}</button>
          </form>
        </div>
      </section>

      <section className="panel wide">
        <div className="panel-header">
          <div>
            <h2>Chat</h2>
            <p className="muted">Sessions live in the Go App Service; responses stream token-by-token.</p>
          </div>
          <div className="actions">
            <button onClick={createSession}>New Session</button>
            <select value={activeSession} onChange={(e) => setActiveSession(e.target.value)}>
              {!sessions.length && <option value="">No sessions</option>}
              {sessions.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.title}
                </option>
              ))}
            </select>
          </div>
        </div>

        <div className="chat-window">
          {activeMessages.map((msg, idx) => (
            <div key={idx} className={`bubble ${msg.role}`}>
              <div className="role">{msg.role}</div>
              <p>{msg.content}</p>
            </div>
          ))}
          {!activeMessages.length && <p className="muted">Ask something to start streaming.</p>}
        </div>

        <div className="chat-input">
          <textarea
            placeholder="Ask about your uploaded knowledge..."
            value={chatInput}
            onChange={(e) => setChatInput(e.target.value)}
            disabled={!token || isStreaming}
          />
          <div className="chat-actions">
            <button onClick={handleStream} disabled={isStreaming || !chatInput || !activeSession || !token}>
              {isStreaming ? 'Streaming...' : 'Send & Stream'}
            </button>
            {isStreaming && (
              <button onClick={stopStream} className="ghost">
                Stop
              </button>
            )}
          </div>
        </div>
      </section>
    </div>
  )
}

export default App
