package transcode

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
)

type teeWriter struct {
	w1, w2 io.Writer
}

func (tw *teeWriter) Write(p []byte) (int, error) {
	n, err := tw.w1.Write(p)
	if err != nil {
		return n, err
	}
	_, w2Err := tw.w2.Write(p)
	return n, w2Err
}

type Transcoder struct {
	ffmpegPath  string
	maxConc     int
	cacheDir    string
	activeCount atomic.Int32
	mu          sync.Mutex
	cond        *sync.Cond
}

func New(ffmpegPath, cacheDir string, maxConcurrency int) *Transcoder {
	if maxConcurrency < 1 {
		maxConcurrency = 3
	}
	t := &Transcoder{
		ffmpegPath: ffmpegPath,
		maxConc:   maxConcurrency,
		cacheDir:  cacheDir,
	}
	t.cond = sync.NewCond(&t.mu)

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		log.Printf("transcode: failed to create cache dir %s: %v", cacheDir, err)
	}

	return t
}

func (t *Transcoder) IsInstalled() bool {
	_, err := exec.LookPath(t.ffmpegPath)
	return err == nil
}

func (t *Transcoder) getCachePath(inputPath, outputFormat string, maxBitRate int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d", inputPath, outputFormat, maxBitRate)))
	key := fmt.Sprintf("%x", h)
	return filepath.Join(t.cacheDir, key+"."+outputFormat)
}

func (t *Transcoder) acquire() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for int(t.activeCount.Load()) >= t.maxConc {
		t.cond.Wait()
	}
	t.activeCount.Add(1)
}

func (t *Transcoder) release() {
	t.activeCount.Add(-1)
	t.mu.Lock()
	t.cond.Signal()
	t.mu.Unlock()
}

func (t *Transcoder) Transcode(ctx context.Context, inputPath, outputFormat string, maxBitRate int) (io.ReadCloser, error) {
	cachePath := t.getCachePath(inputPath, outputFormat, maxBitRate)

	if f, err := os.Open(cachePath); err == nil {
		log.Printf("transcode: cache hit for %s", filepath.Base(inputPath))
		return f, nil
	}

	t.acquire()

	tmpFile, err := os.CreateTemp(t.cacheDir, "transcode-*.tmp")
	if err != nil {
		t.release()
		return nil, fmt.Errorf("transcode: create temp: %w", err)
	}

	pr, pw := io.Pipe()
	tee := &teeWriter{w1: pw, w2: tmpFile}

	cmd := t.buildCmd(ctx, inputPath, outputFormat, maxBitRate)
	cmd.Stdout = tee

	if err := cmd.Start(); err != nil {
		t.release()
		pw.Close()
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return nil, fmt.Errorf("transcode: start ffmpeg: %w", err)
	}

	go func() {
		defer t.release()
		defer tmpFile.Close()

		waitErr := cmd.Wait()
		pw.Close()

		if waitErr != nil {
			log.Printf("transcode: ffmpeg error for %s: %v", filepath.Base(inputPath), waitErr)
			os.Remove(tmpFile.Name())
			return
		}

		if err := os.Rename(tmpFile.Name(), cachePath); err != nil {
			log.Printf("transcode: rename cache: %v", err)
			os.Remove(tmpFile.Name())
		}
	}()

	return pr, nil
}

func (t *Transcoder) buildCmd(ctx context.Context, inputPath, outputFormat string, maxBitRate int) *exec.Cmd {
	args := []string{
		"-i", inputPath,
		"-map", "0:a:0",
		"-v", "0",
	}

	switch outputFormat {
	case "opus":
		args = append(args, "-c:a", "libopus")
	case "aac":
		args = append(args, "-c:a", "aac")
	default:
		args = append(args, "-c:a", "libmp3lame")
		outputFormat = "mp3"
	}

	if maxBitRate > 0 {
		args = append(args, "-b:a", strconv.Itoa(maxBitRate)+"k")
	}

	args = append(args, "-f", outputFormat, "pipe:1")

	cmd := exec.CommandContext(ctx, t.ffmpegPath, args...)
	cmd.Stderr = nil
	return cmd
}
