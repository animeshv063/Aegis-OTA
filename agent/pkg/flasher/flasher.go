package flasher

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	// DefaultChunkSize is the streaming buffer size (1MB) to ensure minimal RAM footprint.
	DefaultChunkSize = 1024 * 1024
)

// ProgressReporter is an optional callback function to track flash percentage and speed.
type ProgressReporter func(writtenBytes int64, totalBytes int64, percent float64)

// StreamDirectOptions defines parameters for flashing an image to a raw block device or file.
type StreamDirectOptions struct {
	ImageURL         string
	TargetPath       string
	ExpectedSHA256   string
	ExpectedSizeBytes int64
	ChunkSize        int
	OnProgress       ProgressReporter
}

// StreamToBlockDevice downloads an OS image stream and writes directly to targetPath
// while computing the SHA-256 on the fly without buffering the image in memory.
func StreamToBlockDevice(opts StreamDirectOptions) (string, error) {
	if opts.ImageURL == "" || opts.TargetPath == "" {
		return "", errors.New("image URL and target path are required")
	}

	chunkSize := opts.ChunkSize
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}

	// Open raw block device or file for direct write
	targetFile, err := os.OpenFile(opts.TargetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0660)
	if err != nil {
		return "", fmt.Errorf("failed to open target block device %s: %w", opts.TargetPath, err)
	}
	defer targetFile.Close()

	// Initiate HTTP stream request
	client := &http.Client{
		Timeout: 30 * time.Minute,
	}

	resp, err := client.Get(opts.ImageURL)
	if err != nil {
		return "", fmt.Errorf("failed to initiate image download stream from %s: %w", opts.ImageURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("unexpected HTTP response status: %s", resp.Status)
	}

	totalSize := resp.ContentLength
	if totalSize <= 0 && opts.ExpectedSizeBytes > 0 {
		totalSize = opts.ExpectedSizeBytes
	}

	hasher := sha256.New()
	buf := make([]byte, chunkSize)
	var writtenTotal int64

	for {
		nr, readErr := resp.Body.Read(buf)
		if nr > 0 {
			// Write chunk to target block device
			nw, writeErr := targetFile.Write(buf[0:nr])
			if writeErr != nil {
				return "", fmt.Errorf("block device write failure at offset %d: %w", writtenTotal, writeErr)
			}
			if nr != nw {
				return "", fmt.Errorf("short write error: read %d, wrote %d", nr, nw)
			}

			// Feed data to SHA-256 hasher
			hasher.Write(buf[0:nr])
			writtenTotal += int64(nw)

			if opts.OnProgress != nil && totalSize > 0 {
				percent := float64(writtenTotal) / float64(totalSize) * 100.0
				opts.OnProgress(writtenTotal, totalSize, percent)
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return "", fmt.Errorf("network stream interrupted during read: %w", readErr)
		}
	}

	// Force kernel buffers to flush to physical non-volatile storage
	if err := targetFile.Sync(); err != nil {
		return "", fmt.Errorf("failed to sync block device storage: %w", err)
	}

	computedHash := hex.EncodeToString(hasher.Sum(nil))

	// Verify against expected SHA-256
	if opts.ExpectedSHA256 != "" && computedHash != opts.ExpectedSHA256 {
		return computedHash, fmt.Errorf("integrity violation: computed SHA-256 (%s) does not match expected (%s)", computedHash, opts.ExpectedSHA256)
	}

	if opts.ExpectedSizeBytes > 0 && writtenTotal != opts.ExpectedSizeBytes {
		return computedHash, fmt.Errorf("size mismatch: wrote %d bytes, expected %d", writtenTotal, opts.ExpectedSizeBytes)
	}

	return computedHash, nil
}
