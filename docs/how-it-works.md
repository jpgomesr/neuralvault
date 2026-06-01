A step-by-step explanation of the full pipeline from source ingestion to AI response.

---

## Pipeline Overview

```text
User Query
↓
Embedding Generation
↓
Semantic Search
↓
Reranking
↓
Context Compression
↓
LLM
↓
Streaming Response
```

---

## 1. Source Ingestion

The user connects knowledge sources:

- Obsidian vaults
- GitHub repositories
- Local files
- PDFs
- Documentation

---

## 2. Chunking

The system splits large content into smaller chunks.

Examples:

- Markdown sections
- Code blocks
- Paragraphs

This improves retrieval quality by ensuring the vector search operates on meaningful, focused pieces of text.

---

## 3. Embeddings

Each chunk is transformed into a vector using embedding models.

Example models:

- `nomic-embed-text`
- BGE
- Jina Embeddings

The embeddings are stored in the vector database.

---

## 4. Vector Database

The project uses **Qdrant** to store and search semantic vectors.

This enables semantic search instead of keyword-only search — finding content by meaning rather than exact words.

---

## 5. Retrieval Engine

When the user asks a question, the system:

1. Embeds the question
2. Searches the vector database
3. Retrieves the most relevant chunks
4. Reranks results
5. Compresses context
6. Sends optimized context to the LLM

---

## 6. LLM Response

The optimized context is forwarded to an LLM provider.

Supported providers:

- OpenAI
- Claude (Anthropic)
- Gemini (Google)
- Ollama (local)
- Qwen
- DeepSeek

The response is streamed back to the frontend in real time.
