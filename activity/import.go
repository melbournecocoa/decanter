package activity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"go.temporal.io/sdk/activity"

	"github.com/melbournecocoa/decanter/model"
)

// Import ingests a local video file from <BasePath>/imports/<FileName> into
// the per-workflow workspace as source.mp4. The import is consumed (moved) on
// success — rename on the same filesystem, copy+delete fallback across
// filesystems. Used when a YouTube live stream dropped out mid-broadcast and
// we have a local recording to feed in instead.
func (a *Activities) Import(ctx context.Context, input model.ImportInput) (model.ImportOutput, error) {
	logger := activity.GetLogger(ctx)

	if err := validateImportFileName(input.FileName); err != nil {
		return model.ImportOutput{}, err
	}

	srcPath := filepath.Join(a.BasePath, "imports", input.FileName)
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return model.ImportOutput{}, fmt.Errorf("import source: %w", err)
	}
	if srcInfo.IsDir() {
		return model.ImportOutput{}, fmt.Errorf("import source is a directory: %s", srcPath)
	}

	wsDir := a.workspaceDir(ctx)
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		return model.ImportOutput{}, fmt.Errorf("create workspace dir: %w", err)
	}

	dstPath := filepath.Join(wsDir, "source.mp4")
	logger.Info("Importing local video", "src", srcPath, "dst", dstPath, "bytes", srcInfo.Size())

	renameErr := os.Rename(srcPath, dstPath)
	switch {
	case renameErr == nil:
		logger.Info("Import complete (rename)", "dst", dstPath)
	case !isCrossDevice(renameErr):
		return model.ImportOutput{}, fmt.Errorf("rename: %w", renameErr)
	default:
		// Cross-filesystem: stream copy with heartbeats, then delete source.
		if err := copyWithHeartbeats(ctx, srcPath, dstPath); err != nil {
			return model.ImportOutput{}, err
		}
		if err := os.Remove(srcPath); err != nil {
			return model.ImportOutput{}, fmt.Errorf("remove source after copy: %w", err)
		}
		logger.Info("Import complete (copy)", "dst", dstPath)
	}

	// Seed event.json at the workspace root. If the trigger supplied a
	// RecordingDate (UI / automated source), use it; otherwise leave it
	// empty for the reviewer to fill in at the review_approval gate. File
	// mtime is deliberately NOT used as a fallback — usually the transfer
	// time, not the recording time. Failures here are non-fatal: a missing
	// event.json just means Upload skips recordingDetails.
	if err := writeEvent(filepath.Join(wsDir, eventFileName), model.EventMetadata{RecordingDate: input.RecordingDate}); err != nil {
		logger.Warn("Failed to seed event.json", "error", err)
	}

	return model.ImportOutput{VideoPath: dstPath}, nil
}

// validateImportFileName rejects filenames that would escape <BasePath>/imports/
// or hit hidden/special entries. Filenames must be flat (no path separators)
// and must not start with a dot.
func validateImportFileName(name string) error {
	if name == "" {
		return errors.New("import filename: must not be empty")
	}
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, '\\') {
		return fmt.Errorf("import filename: must not contain path separators: %q", name)
	}
	if filepath.Base(name) != name {
		return fmt.Errorf("import filename: must be a plain filename: %q", name)
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("import filename: must not start with '.': %q", name)
	}
	return nil
}

func isCrossDevice(err error) bool {
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return errors.Is(linkErr.Err, syscall.EXDEV)
	}
	return errors.Is(err, syscall.EXDEV)
}

func copyWithHeartbeats(ctx context.Context, srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("create dest: %w", err)
	}
	defer dst.Close()

	// 32 MiB chunks — one heartbeat per chunk keeps Temporal happy on slow NFS
	// without thrashing the activity event history.
	const chunk = 32 * 1024 * 1024
	buf := make([]byte, 32*1024)
	var copied int64
	var lastHeartbeat int64

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			if _, writeErr := dst.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("write dest: %w", writeErr)
			}
			copied += int64(n)
			if copied-lastHeartbeat >= chunk {
				activity.RecordHeartbeat(ctx, copied)
				lastHeartbeat = copied
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read source: %w", readErr)
		}
	}

	if err := dst.Sync(); err != nil {
		return fmt.Errorf("sync dest: %w", err)
	}
	return nil
}
