package raw

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestGetRawEndPoint(t *testing.T) {
	t.Parallel()

	t.Run("ensure that a file is created from a raw endpoint", func(t *testing.T) {

		httpClient := http.DefaultClient

		testData := "../../testdata/heapster-metric-export.json"

		client := NewClient(
			*httpClient,
			true,
			"",
		)

		wd, _ := ioutil.TempDir("", "raw_endpoint_test")
		workingDir, _ := os.Open(wd)

		body, _ := ioutil.ReadFile(testData)

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(body)
		}))
		defer ts.Close()

		testFile, err := client.GetRawEndPoint("heapster", workingDir, ts.URL, 2)
		if err != nil {
			t.Error(err)
		}
		sourceFile, _ := os.Open(testData)

		defer sourceFile.Close()
		sF, _ := sourceFile.Stat()
		tF, _ := testFile.Stat()

		if sF.Size() != tF.Size() {
			t.Error("Source file does not match source")
		}

	})

	t.Run("ensure retry when non http 200-299 returned", func(t *testing.T) {

		httpClient := http.DefaultClient

		client := NewClient(
			*httpClient,
			true,
			"",
		)

		wd, _ := ioutil.TempDir("", "raw_endpoint_test")
		workingDir, _ := os.Open(wd)

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(404)
		}))
		defer ts.Close()

		_, err := client.GetRawEndPoint("heapster", workingDir, ts.URL, 2)
		if err != nil {
			t.Error(err)
		}

	})

	t.Run("ensure retry when error returned", func(t *testing.T) {

		httpClient := http.DefaultClient

		client := NewClient(
			*httpClient,
			true,
			"",
		)

		wd, _ := ioutil.TempDir("", "raw_endpoint_test")
		workingDir, _ := os.Open(wd)

		_, err := client.GetRawEndPoint("heapster", workingDir, "http://localhost:1234", 2)
		if err != nil {
			t.Error(err)
		}

	})
}
