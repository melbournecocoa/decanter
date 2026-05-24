// Command yt-auth performs the one-time OAuth dance to obtain a long-lived
// refresh token for the Decanter worker's YouTube uploads. Input is a
// client_secret.json downloaded from Google Cloud Console (Desktop app
// OAuth client). Output is a runtime creds JSON consumed via
// DECANTER_YOUTUBE_CREDS_FILE.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/youtube/v3"
)

type clientSecretFile struct {
	Installed struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	} `json:"installed"`
}

type runtimeCreds struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
}

func main() {
	var clientCredsPath, outPath string
	flag.StringVar(&clientCredsPath, "client-creds", "", "Path to Google Cloud client_secret.json")
	flag.StringVar(&outPath, "out", "", "Path to write the runtime creds JSON")
	flag.Parse()

	if clientCredsPath == "" || outPath == "" {
		log.Fatalf("usage: yt-auth --client-creds <path> --out <path>")
	}

	raw, err := os.ReadFile(clientCredsPath)
	if err != nil {
		log.Fatalf("read client creds: %v", err)
	}
	var cf clientSecretFile
	if err := json.Unmarshal(raw, &cf); err != nil {
		log.Fatalf("parse client creds: %v", err)
	}
	if cf.Installed.ClientID == "" || cf.Installed.ClientSecret == "" {
		log.Fatalf("client_secret.json missing installed.client_id / installed.client_secret")
	}

	// Bind to an ephemeral local port for the OAuth redirect.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("listen on loopback: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	config := &oauth2.Config{
		ClientID:     cf.Installed.ClientID,
		ClientSecret: cf.Installed.ClientSecret,
		Scopes:       []string{youtube.YoutubeForceSslScope},
		Endpoint:     google.Endpoint,
		RedirectURL:  redirectURL,
	}

	state := fmt.Sprintf("decanter-%d", time.Now().UnixNano())
	authURL := config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		gotState := r.URL.Query().Get("state")
		if gotState != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			errCh <- fmt.Errorf("state mismatch: want %s got %s", state, gotState)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			errCh <- fmt.Errorf("no code in callback")
			return
		}
		fmt.Fprintln(w, "Authorisation successful. You can close this tab.")
		codeCh <- code
	})

	srv := &http.Server{Handler: mux}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	defer srv.Close()

	fmt.Println("Open the following URL in your browser:")
	fmt.Println()
	fmt.Println(authURL)
	fmt.Println()
	fmt.Println("Waiting for callback on", redirectURL)

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		log.Fatalf("auth flow failed: %v", err)
	case <-time.After(5 * time.Minute):
		log.Fatalf("timed out waiting for OAuth callback")
	}

	token, err := config.Exchange(context.Background(), code)
	if err != nil {
		log.Fatalf("exchange code: %v", err)
	}
	if token.RefreshToken == "" {
		log.Fatalf("no refresh_token returned (did Google skip the consent screen? try revoking the app's access and re-running)")
	}

	out := runtimeCreds{
		ClientID:     cf.Installed.ClientID,
		ClientSecret: cf.Installed.ClientSecret,
		RefreshToken: token.RefreshToken,
	}
	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		log.Fatalf("marshal creds: %v", err)
	}
	if err := os.WriteFile(outPath, body, 0o600); err != nil {
		log.Fatalf("write creds: %v", err)
	}

	fmt.Println("Wrote runtime creds to", outPath)
	fmt.Println("Set DECANTER_YOUTUBE_CREDS_FILE to this path and restart the worker.")
}
