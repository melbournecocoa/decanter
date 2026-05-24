package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/melbournecocoa/decanter/activity"
	wf "github.com/melbournecocoa/decanter/workflow"
)

const taskQueue = "decanter-pipeline"

func main() {
	// Optional .env file in the working directory. Does not override variables
	// already set in the environment — explicit exports always win.
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("Loading .env: %v", err)
	}

	// Temporal server address from env, default to localhost:7233
	addr := os.Getenv("TEMPORAL_ADDRESS")
	if addr == "" {
		addr = "localhost:7233"
	}

	c, err := client.Dial(client.Options{HostPort: addr})
	if err != nil {
		log.Fatalf("Unable to create Temporal client: %v", err)
	}
	defer c.Close()

	w := worker.New(c, taskQueue, worker.Options{})

	// Register workflows
	w.RegisterWorkflow(wf.PipelineWorkflow)
	w.RegisterWorkflow(wf.SegmentWorkflow)
	w.RegisterWorkflow(wf.UploadOnlyWorkflow)

	// Read workspace config from environment
	basePath := os.Getenv("DECANTER_WORKSPACE_PATH")
	if basePath == "" {
		basePath = "/tmp/decanter"
	}
	basePath, err = filepath.Abs(basePath)
	if err != nil {
		log.Fatalf("Invalid workspace path: %v", err)
	}

	importsDir := filepath.Join(basePath, "imports")
	if err := os.MkdirAll(importsDir, 0o755); err != nil {
		log.Fatalf("Create imports directory: %v", err)
	}

	bumperRef := os.Getenv("DECANTER_BUMPER_REF_IMAGE")
	if bumperRef == "" {
		bumperRef = "assets/bumper_reference.png"
	}
	bumperRef, err = filepath.Abs(bumperRef)
	if err != nil {
		log.Fatalf("Invalid bumper reference path: %v", err)
	}

	scriptDir := os.Getenv("DECANTER_SCRIPT_DIR")
	if scriptDir == "" {
		scriptDir = "scripts"
	}
	scriptDir, err = filepath.Abs(scriptDir)
	if err != nil {
		log.Fatalf("Invalid script dir path: %v", err)
	}

	if os.Getenv("GROQ_API_KEY") == "" {
		log.Fatalf("GROQ_API_KEY must be set; Transcribe activity calls the Groq API")
	}

	meetupGroup := os.Getenv("DECANTER_MEETUP_GROUP_URLNAME")
	if meetupGroup == "" {
		meetupGroup = "Melbourne-CocoaHeads"
	}

	ytCreds := os.Getenv("DECANTER_YOUTUBE_CREDS_FILE")
	if ytCreds != "" {
		ytCreds, err = filepath.Abs(ytCreds)
		if err != nil {
			log.Fatalf("Invalid YouTube creds path: %v", err)
		}
		if _, err := os.Stat(ytCreds); err != nil {
			log.Printf("WARNING: DECANTER_YOUTUBE_CREDS_FILE set but not readable (%v); Upload will fail at runtime", err)
		}
	} else {
		log.Printf("WARNING: DECANTER_YOUTUBE_CREDS_FILE not set; Upload activity will fail at runtime")
	}

	intro := os.Getenv("DECANTER_INTRO_VIDEO")
	if intro == "" {
		intro = "assets/intro.m4v"
	}
	intro, err = filepath.Abs(intro)
	if err != nil {
		log.Fatalf("Invalid intro video path: %v", err)
	}
	if _, err := os.Stat(intro); err != nil {
		log.Fatalf("Intro video not found at %s: %v", intro, err)
	}

	outro := os.Getenv("DECANTER_OUTRO_VIDEO")
	if outro == "" {
		outro = "assets/outro.m4v"
	}
	outro, err = filepath.Abs(outro)
	if err != nil {
		log.Fatalf("Invalid outro video path: %v", err)
	}
	if _, err := os.Stat(outro); err != nil {
		log.Fatalf("Outro video not found at %s: %v", outro, err)
	}

	// Register activities
	activities := activity.New(basePath, bumperRef, scriptDir, ytCreds, intro, outro, meetupGroup)
	w.RegisterActivity(activities)

	log.Printf("Starting Decanter worker on task queue %q (Temporal: %s)", taskQueue, addr)
	log.Printf("Drop local files into %s/ to use the LocalFileName workflow input", importsDir)
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("Worker failed: %v", err)
	}
}
