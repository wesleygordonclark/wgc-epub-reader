package main

import (
	"archive/zip"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

// ---------- Public data models returned by the API ----------

type BookInfo struct {
	ID       string      `json:"id"`
	Title    string      `json:"title"`
	Author   string      `json:"author"`
	RootFile string      `json:"rootFile"`
	RootFS   string      `json:"-"` // absolute path on disk (not exposed)
	OPF      *OPFPackage `json:"-"`
	TOC      *NavDoc     `json:"-"`
}

type SpineItem struct {
	IDRef string `json:"idref"`
	Href  string `json:"href"`
	Type  string `json:"mediaType"`
	Title string `json:"title"`
}

// ---------- Minimal EPUB parsing (container.xml + OPF + nav.xhtml) ----------

type containerXML struct {
	XMLName   xml.Name `xml:"container"`
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

type OPFPackage struct {
	XMLName  xml.Name     `xml:"package"`
	Meta     OPFMetadata  `xml:"metadata"`
	Manifest []OPFItem    `xml:"manifest>item"`
	Spine    []OPFItemref `xml:"spine>itemref"`
}

type OPFMetadata struct {
	Title   string `xml:"http://purl.org/dc/elements/1.1/ title"`
	Creator string `xml:"http://purl.org/dc/elements/1.1/ creator"`
}

type OPFItem struct {
	ID        string `xml:"id,attr"`
	Href      string `xml:"href,attr"`
	MediaType string `xml:"media-type,attr"`
}

type OPFItemref struct {
	IDRef string `xml:"idref,attr"`
}

type NavDoc struct {
	Items []NavItem `json:"items"`
}

type NavItem struct {
	Href string `json:"href"`
	Text string `json:"text"`
}

// ---------- Store and utilities ----------

type Store struct {
	rootDir string
	mu      sync.RWMutex
	books   map[string]*BookInfo
}

func NewStore(root string) *Store {
	return &Store{rootDir: root, books: map[string]*BookInfo{}}
}

func (s *Store) UploadEPUB(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 500<<20) // 500MB
	if err := r.ParseMultipartForm(100 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	id, info, err := s.ingest(file, header)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(fmt.Sprintf(`{"id":"%s","title":"%s","author":"%s"}`, id, jsonEscape(info.Title), jsonEscape(info.Author))))
}

func (s *Store) ListBooks(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]BookInfo, 0, len(s.books))
	for _, b := range s.books {
		list = append(list, BookInfo{ID: b.ID, Title: b.Title, Author: b.Author, RootFile: b.RootFile})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

func (s *Store) GetBook(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	b, ok := s.GetBookByID(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(BookInfo{ID: b.ID, Title: b.Title, Author: b.Author, RootFile: b.RootFile})
}

func (s *Store) GetMetadata(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	b, ok := s.GetBookByID(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"title":  b.Title,
		"author": b.Author,
	})
}

func (s *Store) GetSpine(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	b, ok := s.GetBookByID(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	// build spine list with hrefs + labels
	itemsByID := map[string]OPFItem{}
	for _, it := range b.OPF.Manifest {
		itemsByID[it.ID] = it
	}
	var out []SpineItem
	for _, sp := range b.OPF.Spine {
		it := itemsByID[sp.IDRef]
		out = append(out, SpineItem{IDRef: sp.IDRef, Href: normJoin(path.Dir(b.RootFile), it.Href), Type: it.MediaType, Title: ""})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Store) GetTOC(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	b, ok := s.GetBookByID(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(b.TOC)
}

func (s *Store) GetBookByID(id string) (*BookInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.books[id]
	return b, ok
}

// ingest unpacks and indexes an EPUB from uploaded multipart file.
func (s *Store) ingest(file multipart.File, header *multipart.FileHeader) (string, *BookInfo, error) {
	// derive ID from sha1(filename + size + time)
	h := sha1.New()
	_, _ = io.WriteString(h, header.Filename)
	_, _ = io.WriteString(h, fmt.Sprintf("-%d-%d", header.Size, time.Now().UnixNano()))
	id := hex.EncodeToString(h.Sum(nil))[:12]

	bookDir := filepath.Join(s.rootDir, id)
	if err := os.MkdirAll(bookDir, 0o755); err != nil {
		return "", nil, err
	}

	// write uploaded epub to disk
	epubPath := filepath.Join(bookDir, "book.epub")
	out, err := os.Create(epubPath)
	if err != nil {
		return "", nil, err
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		return "", nil, err
	}
	out.Close()

	// unzip into bookDir/unpacked
	root := filepath.Join(bookDir, "unpacked")
	if err := unzipFile(epubPath, root); err != nil {
		return "", nil, err
	}

	// parse container.xml -> rootfile (OPF)
	rootfile, err := findRootfile(root)
	if err != nil {
		return "", nil, err
	}
	opf, err := parseOPF(filepath.Join(root, filepath.FromSlash(rootfile)))
	if err != nil {
		return "", nil, err
	}

	// attempt to parse nav document for TOC (if any)
	toc := &NavDoc{Items: []NavItem{}}
	if nav := findNavItem(opf); nav != "" {
		items, _ := extractNav(filepath.Join(root, filepath.FromSlash(normJoin(path.Dir(rootfile), nav))))
		toc.Items = items
	}

	info := &BookInfo{
		ID:       id,
		Title:    strings.TrimSpace(opf.Meta.Title),
		Author:   strings.TrimSpace(opf.Meta.Creator),
		RootFile: rootfile,
		RootFS:   root,
		OPF:      opf,
		TOC:      toc,
	}

	s.mu.Lock()
	s.books[id] = info
	s.mu.Unlock()
	return id, info, nil
}

// ---------- EPUB helpers ----------

func unzipFile(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		p := filepath.Join(dest, filepath.FromSlash(f.Name))
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(p, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func findRootfile(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "META-INF", "container.xml"))
	if err != nil {
		return "", err
	}
	var c containerXML
	if err := xml.Unmarshal(data, &c); err != nil {
		return "", err
	}
	if len(c.Rootfiles) == 0 {
		return "", errors.New("no rootfile in container.xml")
	}
	return c.Rootfiles[0].FullPath, nil
}

func parseOPF(path string) (*OPFPackage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p OPFPackage
	if err := xml.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func findNavItem(p *OPFPackage) string {
	// EPUB3 nav is usually media-type="application/xhtml+xml" and properties="nav".
	// We only have the minimal manifest here; look for href named like nav.* or toc.* as fallback.
	candidates := []string{}
	for _, it := range p.Manifest {
		if strings.Contains(it.Href, "nav") || strings.Contains(it.Href, "toc") {
			candidates = append(candidates, it.Href)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	// prefer shortest path (often nav.xhtml)
	sort.Slice(candidates, func(i, j int) bool { return len(candidates[i]) < len(candidates[j]) })
	return candidates[0]
}

func extractNav(navPath string) ([]NavItem, error) {
	b, err := os.ReadFile(navPath)
	if err != nil {
		return nil, err
	}
	// very light-weight nav extractor: pull out <a href> text
	t := string(b)
	items := []NavItem{}
	for _, line := range strings.Split(t, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "<a ") {
			continue
		}
		// crude href/text scraping good enough for demo
		href := between(line, "href=\"", "\"")
		text := stripTags(line)
		if href != "" && text != "" {
			items = append(items, NavItem{Href: href, Text: text})
		}
	}
	return items, nil
}

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	s = s[i+len(a):]
	j := strings.Index(s, b)
	if j < 0 {
		return ""
	}
	return s[:j]
}

func stripTags(s string) string {
	out := []rune{}
	in := false
	for _, r := range s {
		if r == '<' {
			in = true
			continue
		}
		if r == '>' {
			in = false
			continue
		}
		if !in {
			out = append(out, r)
		}
	}
	return strings.TrimSpace(strings.ReplaceAll(string(out), "\u00a0", " "))
}

func normJoin(base, rel string) string {
	if rel == "" {
		return base
	}
	p := path.Clean(path.Join(base, rel))
	return p
}

func jsonEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}
