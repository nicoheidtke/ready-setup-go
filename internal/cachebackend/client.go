// Package cachebackend implements the GitHub Actions cache v2 results service.
// Control requests use the runner token; blob transfers use only signed URLs.
package cachebackend

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"setup-go-faster/internal/actionlog"
	"setup-go-faster/internal/httpretry"
)

const (
	// MaxArchiveSize is the default compressed archive transfer limit.
	MaxArchiveSize     int64 = 10 << 30
	defaultBlockSize   int64 = 32 << 20
	maxControlResponse       = 1 << 20
	servicePath              = "/twirp/github.actions.results.api.v1.CacheService/"
)

var (
	ErrConflict   = errors.New("cache entry already exists or is being written")
	ErrPermission = errors.New("cache access denied")
)

// Client uses ACTIONS_RESULTS_URL and ACTIONS_RUNTIME_TOKEN supplied by the runner.
// Zero transfer settings use 10 GiB, 32 MiB blocks, and four workers. Redirects
// are disabled even with a custom client to keep credentials on their endpoint.
type Client struct {
	BaseURL     string
	Token       string
	HTTPClient  *http.Client
	MaxBytes    int64
	BlockSize   int64
	Concurrency int
	// Download settings are independent of upload blocks/workers. One worker
	// retains the sequential GET path; zero uses four 32 MiB range workers.
	DownloadConcurrency int
	DownloadBlockSize   int64
}

// Entry's URL is a credential. Callers must not log or persist it.
type Entry struct{ Key, URL string }

// Lookup returns nil for a service-reported miss. The service enforces branch
// visibility and ordered prefix matching using the runner's token scopes.
func (c *Client) Lookup(ctx context.Context, key, version string, restoreKeys []string) (*Entry, error) {
	if err := validateKeys(key, version, restoreKeys); err != nil {
		return nil, err
	}
	var response struct {
		OK  bool   `json:"ok"`
		URL string `json:"signed_download_url"`
		Key string `json:"matched_key"`
	}
	err := c.rpc(ctx, "GetCacheEntryDownloadURL", struct {
		Key         string   `json:"key"`
		Version     string   `json:"version"`
		RestoreKeys []string `json:"restore_keys"`
	}{key, version, append([]string{}, restoreKeys...)}, &response)
	if err != nil {
		return nil, err
	}
	if !response.OK {
		return nil, nil
	}
	if response.Key == "" {
		return nil, errors.New("cache lookup returned no matched key")
	}
	if _, err := parseURL(response.URL); err != nil {
		return nil, errors.New("cache lookup returned an invalid download URL")
	}
	return &Entry{Key: response.Key, URL: response.URL}, nil
}

// Download streams at most MaxBytes to dst. Callers should publish a temporary
// file only after success; an error may leave partial bytes in dst.
func (c *Client) Download(ctx context.Context, entry Entry, dst io.Writer) (int64, error) {
	limit, _, _, err := c.settings()
	if err != nil {
		return 0, err
	}
	u, err := parseURL(entry.URL)
	if err != nil {
		return 0, errors.New("invalid cache download URL")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	block, workers, err := c.downloadSettings()
	if err != nil {
		return 0, err
	}
	if f, ok := dst.(*os.File); ok && workers > 1 {
		offset, err := f.Seek(0, io.SeekCurrent)
		if err != nil {
			return 0, errors.New("seek cache download destination")
		}
		return c.downloadRanges(ctx, u, f, offset, limit, block, workers)
	}
	return c.downloadStream(ctx, u, dst, limit)
}

func (c *Client) downloadStream(ctx context.Context, u *url.URL, dst io.Writer, limit int64) (int64, error) {
	resp, err := c.getDownload(ctx, u, "", "")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return consumeDownload(ctx, resp, dst, limit)
}

func (c *Client) getDownload(ctx context.Context, u *url.URL, byteRange, etag string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("create cache download request")
	}
	req.Header.Set("Accept-Encoding", "identity")
	if byteRange != "" {
		req.Header.Set("Range", byteRange)
	}
	if etag != "" {
		req.Header.Set("If-Match", etag)
	}
	resp, err := httpretry.Do(c.httpClient(), req)
	if err != nil {
		return nil, transferError(ctx, "download", err)
	}
	return resp, nil
}

func consumeDownload(ctx context.Context, resp *http.Response, dst io.Writer, limit int64) (int64, error) {
	if resp.StatusCode != http.StatusOK {
		return 0, statusError("download", resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return 0, errors.New("cache download exceeds archive size limit")
	}
	actionlog.TransferTotal(ctx, resp.ContentLength)
	n, err := io.Copy(dst, io.LimitReader(actionlog.TransferReader(ctx, resp.Body), limit))
	if err != nil {
		return n, transferError(ctx, "download body", err)
	}
	if resp.ContentLength >= 0 && n != resp.ContentLength {
		return n, errors.New("cache download length mismatch")
	}
	var extra [1]byte
	_, err = io.ReadFull(resp.Body, extra[:])
	if err == nil {
		return n, errors.New("cache download exceeds archive size limit")
	}
	if err != io.EOF {
		return n, transferError(ctx, "download body", err)
	}
	return n, nil
}

func (c *Client) downloadSettings() (int64, int, error) {
	block, workers := c.DownloadBlockSize, c.DownloadConcurrency
	if block == 0 {
		block = defaultBlockSize
	}
	if workers == 0 {
		workers = 4
	}
	if block < 1 || block > 4000<<20 || workers < 1 || workers > 32 {
		return 0, 0, errors.New("invalid cache download settings")
	}
	return block, workers, nil
}

// downloadRanges consumes the first block before starting bounded workers. A
// server ignoring Range returns its full 200 response through the stream path,
// without a second download. Cache blobs are immutable; a strong ETag, when
// available, additionally pins every subsequent request to the same object.
func (c *Client) downloadRanges(ctx context.Context, u *url.URL, dst *os.File, base, limit, block int64, workers int) (int64, error) {
	resp, err := c.getDownload(ctx, u, fmt.Sprintf("bytes=0-%d", block-1), "")
	if err != nil {
		return 0, err
	}
	if resp.StatusCode == http.StatusOK {
		defer resp.Body.Close()
		return consumeDownload(ctx, resp, dst, limit)
	}
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		// Empty objects may reject even the initial range. Let a full GET
		// handle that case with the same limits as a sequential download.
		resp.Body.Close()
		return c.downloadStream(ctx, u, dst, limit)
	}
	if resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return 0, statusError("download range", resp.StatusCode)
	}
	start, end, size, err := parseContentRange(resp.Header.Get("Content-Range"))
	if err != nil || start != 0 || end != min(block, size)-1 {
		resp.Body.Close()
		return 0, errors.New("invalid cache download content range")
	}
	if size > limit || base > int64(^uint64(0)>>1)-size {
		resp.Body.Close()
		return 0, errors.New("cache download exceeds archive size limit")
	}
	if 1+(size-1)/block > 50000 {
		resp.Body.Close()
		return c.downloadStream(ctx, u, dst, limit)
	}
	actionlog.TransferTotal(ctx, size)
	etag := resp.Header.Get("ETag")
	if strings.HasPrefix(etag, "W/") {
		etag = ""
	}
	n, err := consumeRange(ctx, resp, io.NewOffsetWriter(dst, base), 0, end, size, etag)
	resp.Body.Close()
	if err != nil {
		return n, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var transferred atomic.Int64
	transferred.Store(n)
	jobs := make(chan int64)
	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error
	remaining := size - end - 1
	remainingBlocks := remaining / block
	if remaining%block != 0 {
		remainingBlocks++
	}
	for range min(workers, int(remainingBlocks)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for offset := range jobs {
				if ctx.Err() != nil {
					return
				}
				last := offset + min(block, size-offset) - 1
				response, err := c.getDownload(ctx, u, fmt.Sprintf("bytes=%d-%d", offset, last), etag)
				if err == nil {
					var count int64
					count, err = consumeRange(ctx, response, io.NewOffsetWriter(dst, base+offset), offset, last, size, etag)
					transferred.Add(count)
					response.Body.Close()
				}
				if err != nil {
					once.Do(func() { firstErr = err; cancel() })
					return
				}
			}
		}()
	}
feed:
	for offset := end + 1; offset < size; offset += min(block, size-offset) {
		select {
		case jobs <- offset:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return transferred.Load(), firstErr
	}
	if ctx.Err() != nil {
		return transferred.Load(), transferError(ctx, "download", ctx.Err())
	}
	if _, err := dst.Seek(base+size, io.SeekStart); err != nil {
		return transferred.Load(), errors.New("seek cache download destination")
	}
	return transferred.Load(), nil
}

func parseContentRange(raw string) (start, end, total int64, err error) {
	bad := errors.New("invalid cache download content range")
	if !strings.HasPrefix(raw, "bytes ") {
		return 0, 0, 0, bad
	}
	rangePart, totalPart, ok := strings.Cut(strings.TrimPrefix(raw, "bytes "), "/")
	if !ok {
		return 0, 0, 0, bad
	}
	first, last, ok := strings.Cut(rangePart, "-")
	if !ok {
		return 0, 0, 0, bad
	}
	start, err = strconv.ParseInt(first, 10, 64)
	if err != nil {
		return 0, 0, 0, bad
	}
	end, err = strconv.ParseInt(last, 10, 64)
	if err != nil {
		return 0, 0, 0, bad
	}
	total, err = strconv.ParseInt(totalPart, 10, 64)
	if err != nil || start < 0 || end < start || total <= end || raw != fmt.Sprintf("bytes %d-%d/%d", start, end, total) {
		return 0, 0, 0, bad
	}
	return start, end, total, nil
}

func consumeRange(ctx context.Context, resp *http.Response, dst io.Writer, first, last, size int64, etag string) (int64, error) {
	if resp.StatusCode != http.StatusPartialContent {
		return 0, statusError("download range", resp.StatusCode)
	}
	start, end, total, err := parseContentRange(resp.Header.Get("Content-Range"))
	if err != nil || start != first || end != last || total != size {
		return 0, errors.New("invalid cache download content range")
	}
	if encoding := resp.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return 0, errors.New("cache download range has transformed content")
	}
	if got := resp.Header.Get("ETag"); etag != "" && got != "" && got != etag {
		return 0, errors.New("cache download object changed")
	}
	length := last - first + 1
	if resp.ContentLength >= 0 && resp.ContentLength != length {
		return 0, errors.New("cache download range length mismatch")
	}
	n, err := io.Copy(dst, io.LimitReader(actionlog.TransferReader(ctx, resp.Body), length))
	if err != nil {
		return n, transferError(ctx, "download range body", err)
	}
	if n != length {
		return n, errors.New("cache download range length mismatch")
	}
	var extra [1]byte
	if _, err := io.ReadFull(resp.Body, extra[:]); err == nil {
		return n, errors.New("cache download range length mismatch")
	} else if err != io.EOF {
		return n, transferError(ctx, "download range body", err)
	}
	return n, nil
}

// Upload reserves an immutable entry, transfers its blob, and finalizes it only
// after all bytes are committed. Large blobs use parallel bounded-memory block
// requests; io.ReaderAt sources (including *os.File) need no chunk-sized buffers.
func (c *Client) Upload(ctx context.Context, key, version string, src io.ReadSeeker, size int64) error {
	if err := validateKeys(key, version, nil); err != nil {
		return err
	}
	limit, blockSize, workers, err := c.settings()
	if err != nil {
		return err
	}
	if size < 0 || size > limit {
		return errors.New("cache upload size is outside archive limits")
	}
	if size > blockSize && (size+blockSize-1)/blockSize > 50000 {
		return errors.New("cache upload exceeds block count limit")
	}
	actual, err := src.Seek(0, io.SeekEnd)
	if err != nil || actual != size {
		return errors.New("cache upload source size mismatch")
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return errors.New("seek cache upload source")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	var reserved struct {
		OK      bool   `json:"ok"`
		URL     string `json:"signed_upload_url"`
		Message string `json:"message"`
	}
	identity := struct {
		Key     string `json:"key"`
		Version string `json:"version"`
	}{key, version}
	if err := c.rpc(ctx, "CreateCacheEntry", identity, &reserved); err != nil {
		return err
	}
	if !reserved.OK {
		if strings.HasPrefix(reserved.Message, "cache write denied:") {
			return ErrPermission
		}
		return errors.New("cache reservation was refused")
	}
	u, err := parseURL(reserved.URL)
	if err != nil {
		return errors.New("cache reservation returned an invalid upload URL")
	}
	reader, ok := src.(io.ReaderAt)
	if !ok {
		reader = &seekReaderAt{src: src}
	}
	if size <= blockSize {
		if err := c.put(ctx, u, io.NewSectionReader(reader, 0, size), size, "application/octet-stream", true); err != nil {
			return err
		}
	} else if err := c.uploadBlocks(ctx, u, reader, size, blockSize, workers); err != nil {
		return err
	}
	var finalized struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	if err := c.rpc(ctx, "FinalizeCacheEntryUpload", struct {
		Key     string `json:"key"`
		Version string `json:"version"`
		Size    string `json:"size_bytes"`
	}{key, version, strconv.FormatInt(size, 10)}, &finalized); err != nil {
		return err
	}
	if !finalized.OK {
		if strings.HasPrefix(finalized.Message, "cache write denied:") {
			return ErrPermission
		}
		return errors.New("cache finalization was refused")
	}
	return nil
}

func (c *Client) uploadBlocks(ctx context.Context, u *url.URL, src io.ReaderAt, size, blockSize int64, workers int) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	count := int((size + blockSize - 1) / blockSize)
	if count > 50000 {
		return errors.New("cache upload exceeds block count limit")
	}
	ids := make([]string, count)
	for i := range ids {
		ids[i] = base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%08d", i)))
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	var firstErr error
	var once sync.Once
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				start := int64(i) * blockSize
				n := min(blockSize, size-start)
				blockURL := *u
				q := blockURL.Query()
				q.Set("comp", "block")
				q.Set("blockid", ids[i])
				blockURL.RawQuery = q.Encode()
				if err := c.put(ctx, &blockURL, io.NewSectionReader(src, start, n), n, "application/octet-stream", false); err != nil {
					once.Do(func() { firstErr = err; cancel() })
					return
				}
			}
		}()
	}
feed:
	for i := range ids {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	if ctx.Err() != nil {
		return transferError(ctx, "upload", ctx.Err())
	}
	var body strings.Builder
	body.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?><BlockList>")
	for _, id := range ids {
		body.WriteString("<Latest>" + id + "</Latest>")
	}
	body.WriteString("</BlockList>")
	listURL := *u
	q := listURL.Query()
	q.Set("comp", "blocklist")
	listURL.RawQuery = q.Encode()
	return c.put(ctx, &listURL, strings.NewReader(body.String()), int64(body.Len()), "application/xml", false)
}

func (c *Client) put(ctx context.Context, u *url.URL, body io.Reader, size int64, contentType string, blob bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), body)
	if err != nil {
		return errors.New("create cache blob upload request")
	}
	req.ContentLength = size
	if size == 0 {
		req.Body = http.NoBody
	} else if source, ok := body.(io.ReaderAt); ok {
		// Each attempt gets an independent cursor over identical bytes. The
		// upload owns its reservation and uses deterministic block IDs/list.
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(io.NewSectionReader(source, 0, size)), nil
		}
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("x-ms-version", "2021-12-02")
	req.Header.Set("x-ms-date", time.Now().UTC().Format(http.TimeFormat))
	if blob {
		req.Header.Set("x-ms-blob-type", "BlockBlob")
	}
	resp, err := httpretry.Do(c.httpClient(), req)
	if err != nil {
		return transferError(ctx, "upload", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return statusError("upload", resp.StatusCode)
	}
	_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if err != nil {
		return transferError(ctx, "upload response", err)
	}
	if contentType == "application/octet-stream" {
		// Count acknowledged payload only, excluding the block-list request.
		actionlog.Transferred(ctx, size)
	}
	return nil
}

func (c *Client) rpc(ctx context.Context, method string, input, output any) error {
	u, err := parseURL(c.BaseURL)
	if err != nil || c.Token == "" {
		return errors.New("cache service credentials are unavailable or invalid")
	}
	u.Path = servicePath + method
	u.RawPath = ""
	u.RawQuery = ""
	body, err := json.Marshal(input)
	if err != nil {
		return errors.New("encode cache service request")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return errors.New("create cache service request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	var resp *http.Response
	if method == "GetCacheEntryDownloadURL" {
		// This POST is read-only. Reservation/finalization have ambiguous
		// outcomes on transport failure and deliberately remain single-shot.
		resp, err = httpretry.Do(c.httpClient(), req)
	} else {
		resp, err = c.httpClient().Do(req)
	}
	if err != nil {
		return transferError(ctx, "service request", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxControlResponse+1))
	if err != nil {
		return transferError(ctx, "service response", err)
	}
	if len(data) > maxControlResponse {
		return errors.New("cache service response exceeds size limit")
	}
	if resp.StatusCode != http.StatusOK {
		var detail struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(data, &detail)
		if detail.Code == "already_exists" {
			return ErrConflict
		}
		if detail.Code == "permission_denied" || detail.Code == "unauthenticated" {
			return ErrPermission
		}
		if detail.Code == "invalid_argument" {
			return fmt.Errorf("cache service %s returned HTTP %d (invalid_argument)", method, resp.StatusCode)
		}
		return statusError("service "+method, resp.StatusCode)
	}
	if err := json.Unmarshal(data, output); err != nil {
		return errors.New("invalid cache service JSON response")
	}
	return nil
}

func (c *Client) httpClient() *http.Client {
	client := http.Client{Timeout: 5 * time.Minute}
	if c.HTTPClient != nil {
		client = *c.HTTPClient
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}

func (c *Client) settings() (int64, int64, int, error) {
	limit, block, workers := c.MaxBytes, c.BlockSize, c.Concurrency
	if limit == 0 {
		limit = MaxArchiveSize
	}
	if block == 0 {
		block = defaultBlockSize
	}
	if workers == 0 {
		workers = 4
	}
	if limit < 1 || limit > 1<<40 || block < 1 || block > 4000<<20 || workers < 1 || workers > 32 {
		return 0, 0, 0, errors.New("invalid cache transfer settings")
	}
	return limit, block, workers, nil
}

func parseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("invalid URL")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1")) {
		return nil, errors.New("cache endpoint must use HTTPS")
	}
	return u, nil
}

func validateKeys(key, version string, restoreKeys []string) error {
	if version == "" || len(restoreKeys) > 9 {
		return errors.New("invalid cache version or restore key count")
	}
	for _, k := range append([]string{key}, restoreKeys...) {
		if k == "" || len(k) > 512 || strings.ContainsAny(k, ",\r\n") {
			return errors.New("invalid cache key")
		}
	}
	return nil
}

func statusError(operation string, status int) error {
	if status == http.StatusConflict {
		return ErrConflict
	}
	if status == http.StatusForbidden || status == http.StatusUnauthorized {
		return ErrPermission
	}
	return fmt.Errorf("cache %s returned HTTP %d", operation, status)
}

type seekReaderAt struct {
	mu  sync.Mutex
	src io.ReadSeeker
}

func (r *seekReaderAt) ReadAt(p []byte, offset int64) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.src.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}
	return io.ReadFull(r.src, p)
}
