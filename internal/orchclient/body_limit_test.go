package orchclient

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
)

func TestSubmitJobBodyLimit(t *testing.T) {
	for _, status := range []int{200, 500} {
		for _, chunked := range []bool{false, true} {
			name := map[int]string{200: "success", 500: "error"}[status] + "/" + map[bool]string{false: "declared", true: "chunked"}[chunked]
			t.Run(name, func(t *testing.T) {
				limit := 1 << 20
				if status == 500 {
					limit = 4096
				}
				payload := `{"JobID":"` + strings.Repeat("x", limit) + `TAIL_SECRET"}`
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if !chunked {
						w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
					}
					w.WriteHeader(status)
					if chunked {
						w.(http.Flusher).Flush()
					}
					_, _ = io.WriteString(w, payload)
				}))
				defer srv.Close()
				got, err := New(srv.URL).SubmitJob(context.Background(), orchestrator.SubmitJobRequest{})
				if err == nil {
					t.Fatalf("oversized body accepted: JobID length=%d", len(got.JobID))
				}
				if strings.Contains(err.Error(), "TAIL_SECRET") || len(err.Error()) > limit+200 {
					t.Errorf("unbounded diagnostic: length=%d", len(err.Error()))
				}
				if got.JobID != "" {
					t.Error("oversized response returned a partial job")
				}
			})
		}
	}
}

type countedBody struct {
	io.Reader
	read   int
	closed bool
}

func (b *countedBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.read += n
	return n, err
}
func (b *countedBody) Close() error { b.closed = true; return nil }

type responseTransport struct {
	body   *countedBody
	status int
}

func (rt responseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: rt.status, Header: make(http.Header), Body: rt.body, ContentLength: -1}, nil
}

// Removing the bounded reader must consume too many bytes, even if the error text is capped later.
func TestSubmitJobStopsReadingAndClosesBody(t *testing.T) {
	for _, status := range []int{200, 500} {
		limit := 1 << 20
		if status == 500 {
			limit = 4096
		}
		body := &countedBody{Reader: strings.NewReader(strings.Repeat("x", 2*limit))}
		c := New("http://fixture.invalid")
		c.httpClient = &http.Client{Transport: responseTransport{body: body, status: status}}
		_, err := c.SubmitJob(context.Background(), orchestrator.SubmitJobRequest{})
		if err == nil {
			t.Error("expected error")
		}
		if body.read > limit+1 {
			t.Errorf("read %d bytes; bound=%d", body.read, limit+1)
		}
		if !body.closed {
			t.Error("response body not closed")
		}
	}
}

func TestSubmitJobConfiguredLimits(t *testing.T) {
	for _, status := range []int{200, 500} {
		limit := 64
		if status == 200 {
			limit = 128
		}
		for _, size := range []int{limit, limit + 1} {
			payload := `{"JobID":"job-ok","NodeID":"node-ok"}`
			payload += strings.Repeat(" ", size-len(payload))
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status); _, _ = io.WriteString(w, payload) }))
			c := New(srv.URL, BodyLimits{SuccessBytes: 128, ErrorBytes: 64})
			got, err := c.SubmitJob(context.Background(), orchestrator.SubmitJobRequest{})
			srv.Close()
			if size > limit {
				if err == nil || !strings.Contains(err.Error(), "exceeds") {
					t.Errorf("status=%d: overflow err=%v", status, err)
				}
			} else if status == 200 {
				if err != nil || got.JobID != "job-ok" {
					t.Errorf("exact limit: got=%+v err=%v", got, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), payload) {
				t.Errorf("bounded error body lost: %v", err)
			}
		}
	}
}

type failedResponseReader struct{}

func (failedResponseReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestSubmitJobClosesBodyOnAllOutcomes(t *testing.T) {
	for _, tt := range []struct {
		name      string
		reader    io.Reader
		status    int
		wantError bool
	}{
		{"valid", strings.NewReader(`{"JobID":"job-ok"}`), 200, false},
		{"upstream error", strings.NewReader("temporary error"), 503, true},
		{"invalid json", strings.NewReader("not json"), 200, true},
		{"read failure", failedResponseReader{}, 200, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := &countedBody{Reader: tt.reader}
			c := New("http://fixture.invalid")
			c.httpClient = &http.Client{Transport: responseTransport{body: body, status: tt.status}}
			got, err := c.SubmitJob(context.Background(), orchestrator.SubmitJobRequest{})
			if (err != nil) != tt.wantError {
				t.Errorf("got=%+v err=%v", got, err)
			}
			if !body.closed {
				t.Error("response body not closed")
			}
		})
	}
}

func TestSubmitJobRejectsInvalidLimits(t *testing.T) {
	for _, limits := range []BodyLimits{{SuccessBytes: -1}, {ErrorBytes: -1}, {SuccessBytes: math.MaxInt64}, {ErrorBytes: math.MaxInt64}} {
		c := New("http://fixture.invalid", limits)
		_, err := c.SubmitJob(context.Background(), orchestrator.SubmitJobRequest{})
		if err == nil || !strings.Contains(err.Error(), "body limits") {
			t.Errorf("limits=%+v err=%v", limits, err)
		}
	}
}
