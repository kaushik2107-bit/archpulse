package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"infra-sim/internal/webapi"
)

func main() {
	address := flag.String("addr", ":8080", "HTTP listen address")
	webDir := flag.String("web-dir", "web/dist", "built frontend directory")
	flag.Parse()

	api := webapi.New().Handler()
	frontend := spaHandler(*webDir)
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/api/") {
			api.ServeHTTP(writer, request)
			return
		}
		frontend.ServeHTTP(writer, request)
	})
	server := &http.Server{Addr: *address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 5 * time.Minute, IdleTimeout: 60 * time.Second}
	log.Printf("Infra-Sim web server listening on http://localhost%s", *address)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func spaHandler(directory string) http.Handler {
	abs, err := filepath.Abs(directory)
	if err != nil {
		log.Fatal(err)
	}
	files := http.FileServer(http.Dir(abs))
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		cleanPath := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(request.URL.Path, "/")))
		path := filepath.Join(abs, cleanPath)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			files.ServeHTTP(writer, request)
			return
		}
		index := filepath.Join(abs, "index.html")
		if _, err := os.Stat(index); err != nil {
			http.Error(writer, fmt.Sprintf("frontend is not built; run npm install and npm run build in %s", filepath.Join(abs, "..")), http.StatusNotFound)
			return
		}
		http.ServeFile(writer, request, index)
	})
}
