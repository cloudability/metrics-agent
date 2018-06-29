package raw

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestGetRawEndPoint(t *testing.T) {

	t.Run("ensure that a file is created from a raw endpoint", func(t *testing.T) {
		t.Parallel()

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

		testFile, err := client.GetRawEndPoint("heapster", workingDir, ts.URL)
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
}
