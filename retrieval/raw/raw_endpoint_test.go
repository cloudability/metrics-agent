package raw

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func rawEndpointTests(t testing.TB) {
	var scenarios = []func(t testing.TB){
		ensureThatErrorsAreHandled,
		ensureNetworkErrorsAreHandled,
	}
	for _, v := range scenarios {
		v(t)
	}
}

func TestRawEndpoint(t *testing.T) {
	rawEndpointTests(t)
}

func ensureThatErrorsAreHandled(t testing.TB) {
	httpClient := http.DefaultClient
	client := NewClient(
		*httpClient,
		true,
		"",
		"",
		2,
	)

	wd, _ := ioutil.TempDir("", "raw_endpoint_test")
	workingDir, _ := os.Open(wd)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer ts.Close()

	_, err := client.GetRawEndPoint(http.MethodGet, "heapster", workingDir, ts.URL, nil, true)
	if err == nil {
		t.Error("Server returned invalid response code but function did not raise error")
	}
}

func ensureNetworkErrorsAreHandled(t testing.TB) {
	httpClient := http.DefaultClient
	client := NewClient(
		*httpClient,
		true,
		"",
		"",
		2,
	)

	wd, _ := ioutil.TempDir("", "raw_endpoint_test")
	workingDir, _ := os.Open(wd)

	_, err := client.GetRawEndPoint(http.MethodGet, "heapster", workingDir, "http://localhost:1234", nil, true)
	if err == nil {
		t.Error("Unable to to connect to server but function did not raise error")
	}
}
