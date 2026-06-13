package main

import (
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
	"go.temporal.io/sdk/client"

	"github.com/melbournecocoa/decanter/ui"
)

func main() {
	_ = godotenv.Load()

	addr := flag.String("addr", "127.0.0.1:8780", "UI listen address")
	flag.Parse()

	base := os.Getenv("DECANTER_WORKSPACE_PATH")
	if base == "" {
		base = "./workspace"
	}
	base, err := filepath.Abs(base)
	if err != nil {
		log.Fatalf("workspace path: %v", err)
	}

	temporalAddr := os.Getenv("TEMPORAL_ADDRESS")
	if temporalAddr == "" {
		temporalAddr = "localhost:7233"
	}

	srv := &ui.Server{
		Base:       base,
		Addr:       temporalAddr,
		Control:    ui.NewCLIController("temporal", temporalAddr),
		FFmpegPath: "ffmpeg",
	}

	// Temporal is best-effort: the disk previews work without it.
	if c, err := client.Dial(client.Options{HostPort: temporalAddr}); err != nil {
		log.Printf("WARNING: Temporal unavailable (%v); run list/state/approve/reset disabled", err)
	} else {
		defer c.Close()
		srv.Temporal = ui.NewSDKReader(c)
	}

	log.Printf("Decanter Review Console on http://%s (workspace %s, temporal %s)", *addr, base, temporalAddr)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
