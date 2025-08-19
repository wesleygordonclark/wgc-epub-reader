# eReading System (Go backend + TypeScript frontend)

A simple EPUB reader: upload .epub files, the server unpacks and parses them, and the web UI lets you browse the library and read chapters with a basic TOC.

## Features
- Upload EPUB files via UI (or `POST /api/upload`).
- Server unpacks EPUB, parses `container.xml` and OPF for metadata, manifest, and spine.
- Minimal TOC support by extracting links from `nav.xhtml` when present.
- Serve any resource from the book (HTML chapters, CSS, images, fonts).
- Reader UI: Light/Dark theme, adjustable font size, Prev/Next, TOC list.

## Prereqs
- Go 1.21+
- Node 18+

## Getting Started

### 1) Backend
```bash
cd backend
go mod tidy
go run .
# API at http://localhost:8080
```

### 2) Frontend
```bash
cd frontend
npm i
npm run dev
# UI at http://localhost:5173
```

The frontend expects the backend at `http://localhost:8080`. If you use a different port, set `VITE_API` when running Vite, e.g. `VITE_API=http://localhost:9090 npm run dev`.

## API Overview
- `POST /api/upload` — multipart form field `file`: the EPUB. Returns `{ id, title, author }`.
- `GET /api/books` — list books.
- `GET /api/books/:id` — single book info.
- `GET /api/books/:id/metadata` — `{ title, author }`.
- `GET /api/books/:id/spine` — reading order with resolved `href`s.
- `GET /api/books/:id/toc` — basic table of contents.
- `GET /api/books/:id/file/<relative path>` — serve any file from unpacked EPUB.

## Notes
- EPUB parsing here is intentionally minimal; it works for most well-formed EPUB3 titles but is not exhaustive. You can expand OPF parsing to support more metadata and media overlays, or switch to a dedicated EPUB library if needed.
- Security: chapter HTML is displayed in an `<iframe>` to keep book JS/CSS sandboxed from the app. Consider additional sanitization if you plan to inline HTML instead.
- Persistence: an in-memory index is used; books are unpacked to `data/books/<id>`. You can add a small DB (SQLite) to persist the catalog across restarts.
- CORS: enabled for vite dev server. Tighten for production.








