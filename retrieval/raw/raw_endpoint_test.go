package raw

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

func rawEndpointTests(t testing.TB) {
	var scenarios = []func(t testing.TB){
		ensureThatFileCreatedForHeapsterData,
		ensureThatErrorsAreHandled,
		ensureNetworkErrorsAreHandled,
		ensureThatFileParsedAndCreatedForPodsData,
		ensureThatFileCreatedForPodsData,
	}
	for _, v := range scenarios {
		v(t)
	}
}

func TestRawEndpoint(t *testing.T) {
	rawEndpointTests(t)
}

func BenchmarkMetricFileCreation(b *testing.B) {
	for n := 0; n < b.N; n++ {
		ensureThatFileCreatedForHeapsterData(b)
	}
}

func BenchmarkPodsFile(b *testing.B) {
	for n := 0; n < b.N; n++ {
		ensureThatFileCreatedForPodsData(b)
	}
}

func BenchmarkParsedPodsFile(b *testing.B) {
	for n := 0; n < b.N; n++ {
		ensureThatFileParsedAndCreatedForPodsData(b)
	}
}

func ensureThatErrorsAreHandled(t testing.TB) {
	httpClient := http.DefaultClient
	client := NewClient(
		*httpClient,
		true,
		"",
		"",
		2,
		false,
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

func ensureThatFileCreatedForHeapsterData(t testing.TB) {
	ensureThatFileCreated(t, "../../testdata/heapster-metric-export.json", "heapster", true, false)
}

func ensureThatFileParsedAndCreatedForPodsData(t testing.TB) {
	ensureThatFileCreated(t, "../../testdata/pods.json", "pods", true, true)
}

func ensureThatFileCreatedForPodsData(t testing.TB) {
	ensureThatFileCreated(t, "../../testdata/pods.json", "pods", false, false)
}

func ensureThatFileCreated(t testing.TB, testData string, source string, parseData bool, checkForSecrets bool) {
	httpClient := http.DefaultClient
	client := NewClient(
		*httpClient,
		true,
		"",
		"",
		2,
		parseData,
	)

	wd, _ := ioutil.TempDir("", "raw_endpoint_test")
	workingDir, _ := os.Open(wd)

	body, _ := ioutil.ReadFile(testData)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer ts.Close()

	testFileName, err := client.GetRawEndPoint(http.MethodGet, source, workingDir, ts.URL, nil, true)
	if err != nil {
		t.Error(err)
	}
	sourceFile, _ := os.Open(testData)
	testFile, _ := os.Open(testFileName)

	defer sourceFile.Close()
	defer testFile.Close()

	sF, _ := sourceFile.Stat()
	tF, _ := testFile.Stat()

	if checkForSecrets {
		in, _ := os.ReadFile(testData)
		if !strings.Contains(string(in), "superSecret") {
			t.Error("Source file should have contained secret, but did not")
		}

		out, _ := os.ReadFile(testFileName)
		if strings.Contains(string(out), "superSecret") {
			t.Error("Dest file should not have contained secret, but did")
		}
	}

	_, fileShouldBeParsed := ParsableFileSet[source]
	if fileShouldBeParsed && parseData {
		tFs := tF.Size()
		sFs := sF.Size()
		if sFs == tFs {
			t.Error("Source file matches output, but should have been parsed")
		}
		percent := ((sFs - tFs) * 100) / sFs
		if percent > 51 || percent < 49 {
			t.Error("Output file should be roughly 50% the size of the input file but was " + strconv.Itoa(int(percent)))
		}
	} else {
		if sF.Size() != tF.Size() {
			t.Error("Source file size does not match output")
		}
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
		false,
	)

	wd, _ := ioutil.TempDir("", "raw_endpoint_test")
	workingDir, _ := os.Open(wd)

	_, err := client.GetRawEndPoint(http.MethodGet, "heapster", workingDir, "http://localhost:1234", nil, true)
	if err == nil {
		t.Error("Unable to to connect to server but function did not raise error")
	}
}
