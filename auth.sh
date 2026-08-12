#!/bin/bash
go run ./cmd/yt-auth \
  --client-creds ~/.config/decanter/client_secret.json \
  --out ~/.config/decanter/youtube-creds.json
