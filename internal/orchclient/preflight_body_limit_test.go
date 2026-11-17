package orchclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/NetworkTheoryAppliedResearchInstitute/soholink/internal/orchestrator"
)

// Decoding only the first JSON value must not hide bytes beyond the budget.
func TestPreflightResponseBodyLimit(t *testing.T) {
	const limit = 65536
	for _, chunked := range []bool{false, true} {
		for _, size := range []int{limit - 1, limit, limit + 1} {
			t.Run(fmt.Sprintf("chunked=%t/bytes=%d", chunked, size), func(t *testing.T) {
				payload := `{"Ready":true}`
				payload += strings.Repeat(" ", size-len(payload))
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					if chunked {
						w.(http.Flusher).Flush()
					} else {
						w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
					}
					_, _ = io.WriteString(w, payload)
				}))
				defer srv.Close()
				got, err := New(srv.URL).PreflightJob(context.Background(), orchestrator.SubmitJobRequest{})
				if size <= limit {
					if err != nil || !got.Ready {
						t.Fatalf("within budget: got=%+v err=%v", got, err)
					}
					return
				}
				if err == nil || !strings.Contains(err.Error(), "status 200: response body exceeds 65536 bytes") {
					t.Fatalf("overflow not reported with status and budget: got=%+v err=%v", got, err)
				}
				if !reflect.DeepEqual(got, orchestrator.JobPreflight{}) {
					t.Fatalf("overflow returned a partial preflight: %+v", got)
				}
			})
		}
	}
}

// Removing the reader bound must consume too much, even with capped diagnostics.
func TestPreflightStopsReadingAndClosesBody(t *testing.T) {
	body := &countedBody{Reader: strings.NewReader(`{"Ready":true}` + strings.Repeat(" ", 131072) + "PRIVATE_RESPONSE")}
	c := New("http://fixture.invalid")
	c.httpClient = &http.Client{Transport: responseTransport{body: body, status: http.StatusOK}}
	got, err := c.PreflightJob(context.Background(), orchestrator.SubmitJobRequest{})
	if err == nil || !strings.Contains(err.Error(), "exceeds 65536 bytes") {
		t.Fatalf("overflow not reported: got=%+v err=%v", got, err)
	}
	if strings.Contains(err.Error(), "PRIVATE_RESPONSE") || len(err.Error()) > 200 {
		t.Fatalf("unbounded overflow diagnostic: %v", err)
	}
	if !reflect.DeepEqual(got, orchestrator.JobPreflight{}) {
		t.Errorf("overflow returned a partial preflight: %+v", got)
	}
	if body.read != 65537 {
		t.Errorf("read %d bytes; want budget plus one detection byte", body.read)
	}
	if !body.closed {
		t.Error("response body not closed")
	}
}

// A complete JSON prefix must not hide a subsequent transport read failure.
func TestPreflightRejectsReadFailureAfterJSON(t *testing.T) {
	body := &countedBody{Reader: io.MultiReader(strings.NewReader(`{"Ready":true}`), failedResponseReader{})}
	c := New("http://fixture.invalid")
	c.httpClient = &http.Client{Transport: responseTransport{body: body, status: http.StatusOK}}
	got, err := c.PreflightJob(context.Background(), orchestrator.SubmitJobRequest{})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("read failure lost: %v", err)
	}
	if !reflect.DeepEqual(got, orchestrator.JobPreflight{}) {
		t.Errorf("read failure returned a partial preflight: %+v", got)
	}
	if !body.closed {
		t.Error("response body not closed")
	}
}
