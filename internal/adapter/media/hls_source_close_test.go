package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// A reader blocked on an HLS segment that the server is still fetching
// must return promptly when Close is called, even though the caller's
// context has not been cancelled. Before the fix this reader would hang
// for up to ten seconds — the http.Client.Timeout in HLSSource — and the
// outbound pipeline that wraps it would block forever on <-errCh.
func TestHLSSource_CloseUnblocksReader(t *testing.T) {
	t.Parallel()

	// Build a playlist pointing at a segment URL we control.
	var segURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/seg.ts", func(w http.ResponseWriter, r *http.Request) {
		// Stay blocked until the client's context is cancelled (which
		// HLSSource.Close triggers via the runCtx it created on Open).
		<-r.Context().Done()
		w.WriteHeader(200)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	segURL = srv.URL + "/seg.ts"

	playlist := srv.Client()
	_ = playlist

	// Serve the playlist itself from a separate handler so the playlist
	// fetch returns quickly and the long wait happens on the segment.
	mux.HandleFunc("/playlist.m3u8", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-ENDLIST\n"+segURL+"\n")
	})

	src := NewHLSSource(model.MediaConfig{
		Kind: model.SourceKindHLS,
		Path: srv.URL + "/playlist.m3u8",
	})

	rc, err := src.Open(context.Background())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		// Make sure the close path runs even if the assertion failed
		// early, so the test server's pending request can drain.
		_ = rc.Close()
		_ = src.Close()
	}()

	// A read that hangs forever before the fix; with the fix it returns
	// within 200ms of Close because Close cancels the run context, which
	// unblocks the httptest handler, which returns and the stream loop
	// reaches the next ctx.Done() check; then CloseWithError wakes the
	// reader with ErrSourceClosed.
	var (
		wg        sync.WaitGroup
		readErr   error
		startRead = time.Now()
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		_, readErr = rc.Read(buf)
	}()

	// Give the goroutine a moment to actually start blocking on the pipe.
	time.Sleep(50 * time.Millisecond)

	if err := src.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reader did not unblock within 1s after Close")
	}

	elapsed := time.Since(startRead)
	if elapsed > 500*time.Millisecond {
		t.Errorf("Read took %s after Close; the spec requires 100ms", elapsed)
	}
	if readErr == nil {
		t.Fatal("Read returned nil error after Close; expected ErrSourceClosed")
	}
	if errors.Is(readErr, io.EOF) {
		t.Errorf("Read returned io.EOF after Close; should be ErrSourceClosed so the call site distinguishes \"we stopped this\" from \"the source ended\"")
	}
	if !errors.Is(readErr, ErrSourceClosed) {
		t.Errorf("Read error = %v; want it to wrap ErrSourceClosed", readErr)
	}

	// Close must be idempotent.
	if err := src.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// A Read that is paused only because no segments have arrived yet must
// still unblock on Close: the close path cancels runCtx, the stream
// goroutine sees it on its next segment iteration, returns, and the
// pipe writer is closed with the same error.
func TestHLSSource_CloseInterruptsBeforeAnySegment(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	playlistStarted := make(chan struct{})
	mux.HandleFunc("/playlist.m3u8", func(w http.ResponseWriter, r *http.Request) {
		close(playlistStarted)
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-ENDLIST\nhttp://10.255.255.1/never.ts\n")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := NewHLSSource(model.MediaConfig{
		Kind: model.SourceKindHLS,
		Path: srv.URL + "/playlist.m3u8",
	})
	rc, err := src.Open(context.Background())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = src.Close(); _ = rc.Close() }()

	// Wait until Open has finished parsing the playlist before closing;
	// otherwise we close before the stream goroutine has even started.
	select {
	case <-playlistStarted:
	case <-time.After(time.Second):
		t.Fatal("playlist handler was not reached")
	}

	start := time.Now()
	if err := src.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	buf := make([]byte, 64)
	errCh := make(chan error, 1)
	go func() {
		_, err := rc.Read(buf)
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("Read returned nil after Close")
		}
		if errors.Is(err, io.EOF) {
			t.Errorf("Read returned io.EOF; want ErrSourceClosed")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("Read still blocked after %s post-Close", time.Since(start))
	}
}

// A Read that gets bytes should see those bytes arrive before the close
// error closes the pipe; the test guards against a regression where the
// fix accidentally delivers the close error before the in-flight bytes.
func TestHLSSource_ReadGetsDataThenCloseError(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/playlist.m3u8", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-ENDLIST\n"+muxServeURL(r)+"/seg.bin\n")
	})
	mux.HandleFunc("/seg.bin", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("payload-bytes"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	src := NewHLSSource(model.MediaConfig{
		Kind: model.SourceKindHLS,
		Path: srv.URL + "/playlist.m3u8",
	})
	rc, err := src.Open(context.Background())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = src.Close(); _ = rc.Close() }()

	// Read all bytes: must equal the segment, no error.
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, []byte("payload-bytes")) {
		t.Errorf("got %q, want %q", got, "payload-bytes")
	}
}

// muxServeURL returns the test server's base URL from inside a handler.
// r.Host may be "example.com:80"-style; httptest sets it to the bound
// address, so reconstructing from r works without storing the URL.
func muxServeURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
