package fetcher

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithUserAgent_SetsDefaultHeader(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := WithUserAgent(&http.Client{}, "TestAgent/9.9")
	_, err := client.Get(srv.URL)
	assert.NoError(t, err)
	assert.Equal(t, "TestAgent/9.9", gotUA)
}

func TestWithUserAgent_DoesNotOverwriteCallerHeader(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := WithUserAgent(&http.Client{}, "TestAgent/9.9")
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("User-Agent", "Custom/1.0")
	_, err := client.Do(req)
	assert.NoError(t, err)
	assert.Equal(t, "Custom/1.0", gotUA)
}

func TestWithUserAgent_NilClientReturnsConfiguredDefault(t *testing.T) {
	client := WithUserAgent(nil, "TestAgent/9.9")
	assert.NotNil(t, client)
	_, ok := client.Transport.(*userAgentTransport)
	assert.True(t, ok)
}
