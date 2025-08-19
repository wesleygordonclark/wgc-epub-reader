package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
	"github.com/rs/cors"
)

func main() {
	// Create data dirs
	if err := os.MkdirAll("data/books", 0o755); err != nil {
		log.Fatal(err)
	}

	store := NewStore("data/books")

	r := mux.NewRouter()

	// API routes
	r.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}).Methods("GET")

	r.HandleFunc("/api/upload", store.UploadEPUB).Methods("POST")
	r.HandleFunc("/api/books", store.ListBooks).Methods("GET")
	r.HandleFunc("/api/books/{id}", store.GetBook).Methods("GET")
	r.HandleFunc("/api/books/{id}/metadata", store.GetMetadata).Methods("GET")
	r.HandleFunc("/api/books/{id}/spine", store.GetSpine).Methods("GET")
	r.HandleFunc("/api/books/{id}/toc", store.GetTOC).Methods("GET")
	// Serve any resource from the unpacked book root (html, css, images, fonts)
	r.PathPrefix("/api/books/{id}/file/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		id := vars["id"]
		book, ok := store.GetBookByID(id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		prefix := "/api/books/" + id + "/file/"
		rel := r.URL.Path[len(prefix):]
		p := filepath.Join(book.RootFS, filepath.FromSlash(rel))
		http.ServeFile(w, r, p)
	})

	// CORS for vite dev server and general use
	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173", "http://127.0.0.1:5173", "*"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Println("🚀 API listening on :" + port)
	log.Fatal(http.ListenAndServe(":"+port, c.Handler(r)))
}
