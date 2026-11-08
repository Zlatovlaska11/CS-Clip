package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"zlatovlas/cs-clip/config"
	"zlatovlas/cs-clip/highlights"
	"zlatovlas/cs-clip/parser"
	"zlatovlas/cs-clip/web"
)

func main() {
	cfgPath := flag.String("config", "config.json", "path to config.json")
	demoPath := flag.String("demo", "", "parse a single .dem file and print events (dev mode)")
	addr := flag.String("addr", ":8080", "address to listen on")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if err := os.MkdirAll(cfg.ClipsFolder, 0755); err != nil {
		log.Fatalf("clips folder: %v", err)
	}
	if err := os.MkdirAll(cfg.DemosFolder, 0755); err != nil {
		log.Fatalf("demos folder: %v", err)
	}

	if *demoPath != "" {
		runParseMode(*demoPath)
		return
	}

	srv := web.NewServer(cfg)
	log.Printf("cs-clip running at http://localhost%s", *addr)
	if err := http.ListenAndServe(*addr, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}

func runParseMode(path string) {
	fmt.Printf("Parsing %s ...\n\n", path)
	info, err := parser.ParseDemo(path)
	if err != nil {
		log.Fatalf("parse: %v", err)
	}
	parser.PrintEvents(info)
	hl := highlights.Detect(info)
	fmt.Printf("\n=== Detected %d highlight(s) ===\n", len(hl))
	for _, h := range hl {
		fmt.Printf("  [%d] %s\n", h.ID, h)
	}
}
