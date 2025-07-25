package client_test

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudability/metrics-agent/client"
	"github.com/cloudability/metrics-agent/measurement"
	"github.com/cloudability/metrics-agent/test"
)

const metricsSuffix string = "/metricsample"

// nolint: dupl
func TestClientCreation(t *testing.T) {
	t.Parallel()

	t.Run("valid baseURL is allowed", func(t *testing.T) {
		c, err := client.NewHTTPMetricClient(client.Configuration{
			Timeout:    10 * time.Second,
			Token:      test.SecureRandomAlphaString(20),
			BaseURL:    "https://cloudability.com",
			MaxRetries: 2,
			Region:     "us-west-2",
		})
		if c == nil || err != nil {
			t.Error("Expected client to successfully create")
		}
	})
}

func TestToJSONLines(t *testing.T) {
	t.Parallel()

	measurements := []measurement.Measurement{
		createRandomMeasurement(),
		createRandomMeasurement(),
		createRandomMeasurement(),
	}

	b, _ := client.ToJSONLines(measurements)
	scanner := bufio.NewScanner(bytes.NewReader(b))
	counter := 0
	for scanner.Scan() {
		_ = scanner.Text()
		counter++
	}
	if counter != 3 {
		t.Error("Expected three lines")
	}
}

// nolint:gosec
func createRandomMeasurement() measurement.Measurement {
	tags := make(map[string]string)
	tags["host"] = "macbookpro.Local.abc123"
	randKey := "zz" + test.SecureRandomAlphaString(62) //ensure ordering
	randValue := test.SecureRandomAlphaString(64)
	tags[randKey] = randValue

	name := test.SecureRandomAlphaString(64)
	value := rand.Float64()
	ts := time.Now().Unix()

	m := measurement.Measurement{
		Name:      name,
		Value:     value,
		Tags:      tags,
		Timestamp: ts,
	}

	return m
}

// nolint gocyclo
func TestSendMetricSample(t *testing.T) {
	token := test.SecureRandomAlphaString(20)
	contentTypeJson := "application/json"
	testAgentVersion := "0.0.1"
	UID := "867-53-abc-efg-09"
	hash := "0WaFZj9FYzQZLgWaPUiWGA=="

	f, err := os.Open("testdata/test-cluster-1510159016.tgz")
	if err != nil {
		t.Error("unable to open testdata: ", err)
	}

	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		switch r.RequestURI {
		case "/metricsample":

			if r.Method != http.MethodPost {
				t.Error("Expected to be a POST")
			}

			if r.Header.Get(client.ContentTypeHeader) != contentTypeJson {
				t.Error("Expected Content-Type to be set on request")
			}

			if r.Header.Get(client.APIKeyHeader) != token {
				t.Error("Expected token to be set on request")
			}
			if r.Header.Get(client.AuthHeader) != token {
				t.Error("Expected auth header to be set on request")
			}
			if r.Header.Get(client.AgentVersionHeader) != testAgentVersion {
				t.Error("Expected Agent Version to be set on request")
			}
			if r.Header.Get(client.ClusterUIDHeader) != UID {
				t.Error("Expected UID to be set on request")
			}

			if r.Header.Get(client.UploadFileHash) != hash {
				t.Error("Expected file hash sum to be set on request")
			}

			resp := client.MetricSampleResponse{Location: "http://localhost:8889"}
			jsonResp, _ := json.Marshal(resp)

			w.Header().Set("Content-Type", "application/json")
			w.Write(jsonResp)

			w.WriteHeader(200)
		case "/":

			if r.Method != http.MethodPut {
				t.Error("Expected to be a PUT")
			}

			if r.Header.Get(client.ContentTypeHeader) != "multipart/form-data" {
				t.Error("Expected file hash sum to be set on request")
			}

			if r.Header.Get(client.ContentMD5) != hash {
				t.Error("Expected file hash sum to be set on request")
			}

			reader, err := gzip.NewReader(r.Body)

			if err != nil {
				t.Errorf("Error reading metric sample: %v", err)
			}

			defer reader.Close()

			w.WriteHeader(200)

			w.Write(nil)
		}

	}))

	ts.Listener, _ = net.Listen("tcp", "127.0.0.1:8889")
	ts.Start()
	defer ts.Close()

	c, err := client.NewHTTPMetricClient(client.Configuration{
		Timeout:    10 * time.Second,
		Token:      token,
		MaxRetries: 2,
		BaseURL:    ts.URL + metricsSuffix,
		Region:     "us-west-2",
	})
	if err != nil {
		t.Error(err)
	}

	err = c.SendMetricSample(f, testAgentVersion, UID)
	if err != nil {
		t.Error(err)
	}

}

// nolint: gocyclo, errcheck
func TestSendMetricSample_ErrorState(t *testing.T) {
	testAgentVersion := "0.0.1"
	UID := "867-53-abc-efg-09"

	t.Run("404 Not Found", func(t *testing.T) {
		ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			switch r.RequestURI {
			case "/metricsample":

				resp := client.MetricSampleResponse{Location: "http://localhost:1888"}
				jsonResp, _ := json.Marshal(resp)
				w.Write(jsonResp)
			case "/":
				w.WriteHeader(404)
			}

		}))

		ts.Listener, _ = net.Listen("tcp", "localhost:1888")
		ts.Start()
		defer ts.Close()

		c, err := client.NewHTTPMetricClient(client.Configuration{
			Timeout:    10 * time.Second,
			Token:      test.SecureRandomAlphaString(20),
			BaseURL:    ts.URL,
			MaxRetries: 2,
			Region:     "us-west-2",
		})
		if err != nil {
			t.Error(err)
		}

		f, err := os.Open("testdata/test-cluster-1510159016.tgz")
		if err != nil {
			t.Error("unable to open testdata: ", err)
		}

		err = c.SendMetricSample(f, testAgentVersion, UID)
		if err == nil {
			t.Errorf("Expected to receive an error")
			return
		}
		if !strings.Contains(err.Error(), "404") {
			t.Logf("Error message: %v", err.Error())
			t.Errorf("Expected 404 status code in response")
		}
	})
	//nolint dupl
	t.Run("500 Internal Server Error", func(t *testing.T) {
		callCount := 0
		//nolint dupl
		ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			switch r.RequestURI {
			case "/metricsample":

				resp := client.MetricSampleResponse{Location: "http://localhost:2888"}
				jsonResp, _ := json.Marshal(resp)
				w.Write(jsonResp)
			case "/":
				callCount++
				w.WriteHeader(500)
			}

		}))

		ts.Listener, _ = net.Listen("tcp", "127.0.0.1:2888")
		ts.Start()
		defer ts.Close()

		c, err := client.NewHTTPMetricClient(client.Configuration{
			Timeout:    10 * time.Second,
			Token:      test.SecureRandomAlphaString(20),
			BaseURL:    ts.URL + metricsSuffix,
			MaxRetries: 2,
			Region:     "us-west-2",
		})
		if err != nil {
			t.Error(err)
		}

		f, err := os.Open("testdata/test-cluster-1510159016.tgz")
		if err != nil {
			t.Error("unable to open testdata: ", err)
		}

		err = c.SendMetricSample(f, testAgentVersion, UID)
		if err == nil {
			t.Errorf("Expected to receive an error")
			return
		}
		if !strings.Contains(err.Error(), "500") {
			t.Logf("Error message: %v", err.Error())
			t.Errorf("Expected 500 status code in response")
		}

		if callCount != 2 {
			t.Errorf("Expected 2 retries, got %d instead", callCount)
		}
	})
	//nolint dupl
	t.Run("403 Forbidden", func(t *testing.T) {
		callCount := 0

		//nolint dupl
		ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			switch r.RequestURI {
			case "/metricsample":

				resp := client.MetricSampleResponse{Location: "http://localhost:3888"}
				jsonResp, _ := json.Marshal(resp)
				w.Write(jsonResp)
			case "/":
				callCount++
				w.WriteHeader(403)
			}

		}))

		ts.Listener, _ = net.Listen("tcp", "127.0.0.1:3888")
		ts.Start()
		defer ts.Close()

		c, err := client.NewHTTPMetricClient(client.Configuration{
			Timeout:    10 * time.Second,
			Token:      test.SecureRandomAlphaString(20),
			MaxRetries: 2,
			BaseURL:    ts.URL,
			Region:     "us-west-2",
		})
		if err != nil {
			t.Error(err)
		}

		f, err := os.Open("testdata/test-cluster-1510159016.tgz")
		if err != nil {
			t.Error("unable to open testdata: ", err)
		}

		err = c.SendMetricSample(f, testAgentVersion, UID)
		if err == nil {
			t.Errorf("Expected to receive an error")
			return
		}
		if !strings.Contains(err.Error(), "403") {
			t.Logf("Error message: %v", err.Error())
			t.Errorf("Expected 403 status code in response")
		}

		if callCount != 2 {
			t.Errorf("Expected 2 retries, got %d instead", callCount)
		}
	})

	t.Run("Timeout", func(t *testing.T) {
		callCount := 0
		ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			switch r.RequestURI {
			case "/metricsample":
				resp := client.MetricSampleResponse{Location: "http://localhost:4888"}
				jsonResp, _ := json.Marshal(resp)
				w.Write(jsonResp)
			default:
				callCount++
				time.Sleep(3 * time.Second)
			}

		}))

		ts.Listener, _ = net.Listen("tcp", "127.0.0.1:4888")
		ts.Start()
		defer ts.Close()

		c, err := client.NewHTTPMetricClient(client.Configuration{
			Timeout:    2 * time.Second,
			Token:      test.SecureRandomAlphaString(20),
			MaxRetries: 2,
			BaseURL:    ts.URL,
			Region:     "us-west-2",
		})
		if err != nil {
			t.Error(err)
		}

		f, err := os.Open("testdata/test-cluster-1510159016.tgz")
		if err != nil {
			t.Error("unable to open testdata: ", err)
		}

		err = c.SendMetricSample(f, testAgentVersion, UID)
		if err == nil {
			t.Errorf("Expected to receive an error")
			return
		}
		if callCount != 2 {
			t.Errorf("Expected 2 retries, got %d instead", callCount)
		}
	})

	t.Run("400 Update Metric Agent", func(t *testing.T) {
		didPanic := false
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
			}
		}()

		callCount := 0
		ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			switch r.RequestURI {
			case "/metricsample":

				resp := client.MetricSampleResponse{Location: "http://localhost:5888"}
				jsonResp, _ := json.Marshal(resp)
				w.Write(jsonResp)
			case "/":
				callCount++
				http.Error(w, "Incompatible agent version please upgrade", 400)
			}

		}))

		ts.Listener, _ = net.Listen("tcp", "127.0.0.1:5888")
		ts.Start()
		defer ts.Close()

		c, err := client.NewHTTPMetricClient(client.Configuration{
			Timeout:    10 * time.Second,
			Token:      test.SecureRandomAlphaString(20),
			BaseURL:    ts.URL + metricsSuffix,
			MaxRetries: 2,
			Region:     "us-west-2",
		})
		if err != nil {
			t.Error(err)
		}

		f, err := os.Open("testdata/test-cluster-1510159016.tgz")
		if err != nil {
			t.Error("unable to open testdata: ", err)
		}

		err = c.SendMetricSample(f, testAgentVersion, UID)
		if err == nil {
			t.Errorf("Expected to receive an error")
			return
		}

		if callCount != 1 {
			t.Errorf("Expected 1 retries, got %d instead", callCount)
		}

		if !didPanic {
			t.Error("Expected panic.")
		}
	})
}

// nolint gocyclo
func TestGetUploadURL(t *testing.T) {
	token := test.SecureRandomAlphaString(20)
	contentTypeJson := "application/json"
	testAgentVersion := "0.0.1"
	UID := "867-53-abc-efg-09"
	// hash := "kaYrHhXA+D07q9KOx/nIhg=="
	resp := client.MetricSampleResponse{
		Location: "http://tj",
	}

	f, err := os.Open("testdata/test-cluster-1510159016.tgz")
	if err != nil {
		t.Error("unable to open testdata: ", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Error("Expected to be a POST")
		}

		if r.Header.Get(client.ContentTypeHeader) != contentTypeJson {
			t.Error("Expected Content-Type to be set on request")
		}

		if r.Header.Get(client.APIKeyHeader) != token {
			t.Error("Expected token to be set on request")
		}
		if r.Header.Get(client.AuthHeader) != token {
			t.Error("Expected token to be set on request")
		}
		if r.Header.Get(client.APIKeyHeader) != token {
			t.Error("Expected token to be set on request")
		}
		if r.Header.Get(client.AgentVersionHeader) != testAgentVersion {
			t.Error("Expected Agent Version to be set on request")
		}
		if r.Header.Get(client.ClusterUIDHeader) != UID {
			t.Error("Expected UID to be set on request")
		}

		jsonResp, _ := json.Marshal(resp)

		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonResp)

		w.WriteHeader(200)

	}))
	defer ts.Close()

	c, err := client.NewHTTPMetricClient(client.Configuration{
		Timeout:    10 * time.Second,
		Token:      token,
		MaxRetries: 2,
		BaseURL:    ts.URL,
		Region:     "us-west-2",
	})
	if err != nil {
		t.Error(err)
	}

	url, _, err := c.GetUploadURL(f, ts.URL, testAgentVersion, UID, 0)
	if err != nil {
		t.Error(err)
	}
	if url != resp.Location {
		t.Error("invalid location response", resp.Location)
	}
}

func Test_getB64MD5Hash(t *testing.T) {
	f, err := os.Open("testdata/random-test-data.txt")
	if err != nil {
		t.Error("unable to open testdata: ", err)
	}

	hash, err := client.GetB64MD5Hash(f.Name())
	if err != nil {
		t.Error("unable to get hash ", err)
	}

	if hash != "kaYrHhXA+D07q9KOx/nIhg==" {
		t.Error("hash not was not correctly calculated")
	}

}

func Test_getUploadURLByRegion(t *testing.T) {
	// us-west-2 url generation
	uploadURL := client.GetUploadURLByRegion("us-west-2")
	if uploadURL != client.DefaultBaseURL {
		t.Error("US URL was not generated correctly")
	}
	// eu-central-1 url generation
	uploadURL = client.GetUploadURLByRegion("eu-central-1")
	if uploadURL != client.EUBaseURL {
		t.Error("EU URL was not generated correctly")
	}
	uploadURL = client.GetUploadURLByRegion("ap-southeast-2")
	if uploadURL != client.AUBaseURL {
		t.Error("AU URL was not generated correctly")
	}
	uploadURL = client.GetUploadURLByRegion("me-central-1")
	if uploadURL != client.MEBaseURL {
		t.Error("ME URL was not generated correctly")
	}
	uploadURL = client.GetUploadURLByRegion("us-gov-west-1")
	if uploadURL != client.GovBaseURL {
		t.Error("Gov URL was not generated correctly")
	}
	// unsupported region should default to us-west-2 url generation
	uploadURL = client.GetUploadURLByRegion("my-unsupported-region-1")
	if uploadURL != client.DefaultBaseURL {
		t.Error("Unsupported region default to US URL was not generated correctly")
	}
	uploadURL = client.GetUploadURLByRegion("us-west-2-staging")
	if uploadURL != client.StagingBaseURL {
		t.Error("US staging URL was not generated correctly")
	}
}

func Test_BuildProxyFunc(t *testing.T) {
	proxyStr := "https://proxy.example.com"
	proxyURL, err := url.Parse(proxyStr)
	if err != nil {
		t.Error(err)
	}
	testCases := []struct {
		name       string
		cfg        client.Configuration
		requestURL string
		expected   string
	}{
		{
			name: "get upload url - no proxy",
			cfg: client.Configuration{
				UseProxyForGettingUploadURLOnly: false,
				ProxyURL:                        url.URL{},
			},
			requestURL: "https://test.cloudability.com/metricsample",
			expected:   "",
		},
		{
			name: "get upload url - proxy enabled for both requests",
			cfg: client.Configuration{
				UseProxyForGettingUploadURLOnly: false,
				ProxyURL:                        *proxyURL,
			},
			requestURL: "https://test.cloudability.com/metricsample",
			expected:   proxyStr,
		},
		{
			name: "get upload url - proxy enabled for only get upload url requests - proxy url not set",
			cfg: client.Configuration{
				UseProxyForGettingUploadURLOnly: true,
				ProxyURL:                        url.URL{},
			},
			requestURL: "https://test.cloudability.com/metricsample",
			expected:   "",
		},
		{
			name: "get upload url - proxy enabled for only get upload url requests - proxy url is set",
			cfg: client.Configuration{
				UseProxyForGettingUploadURLOnly: true,
				ProxyURL:                        *proxyURL,
			},
			requestURL: "https://test.cloudability.com/metricsample",
			expected:   proxyStr,
		},
		{
			name: "s3 upload - no proxy",
			cfg: client.Configuration{
				UseProxyForGettingUploadURLOnly: false,
				ProxyURL:                        url.URL{},
			},
			requestURL: "https://s3.amazonaws.com/test",
			expected:   "",
		},
		{
			name: "s3 upload - proxy enabled for both requests",
			cfg: client.Configuration{
				UseProxyForGettingUploadURLOnly: false,
				ProxyURL:                        *proxyURL,
			},
			requestURL: "https://s3.amazonaws.com/test",
			expected:   proxyStr,
		},
		{
			name: "s3 upload - proxy enabled for only get upload url requests - proxy url not set",
			cfg: client.Configuration{
				UseProxyForGettingUploadURLOnly: true,
				ProxyURL:                        url.URL{},
			},
			requestURL: "https://s3.amazonaws.com/test",
			expected:   "",
		},
		{
			name: "s3 upload - proxy enabled for only get upload url requests - proxy url is set",
			cfg: client.Configuration{
				UseProxyForGettingUploadURLOnly: true,
				ProxyURL:                        *proxyURL,
			},
			requestURL: "https://s3.amazonaws.com/test",
			expected:   "",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			proxyFunc := client.BuildProxyFunc(tc.cfg)
			var request *http.Request
			request, err = http.NewRequest("POST", tc.requestURL, nil)
			if err != nil {
				t.Error(err)
			}
			var actualURL *url.URL
			actualURL, err = proxyFunc(request)
			if err != nil {
				t.Error(err)
			}
			if actualURL == nil && tc.expected != "" {
				t.Errorf("Expected empty strings for actual and expected")
			}
			if actualURL != nil && actualURL.String() != tc.expected {
				t.Errorf("Actual URL: %s, expected URL: %s", actualURL.String(), tc.expected)
			}
		})
	}
}

func Test_SendMetrics_ProxyScenarios(t *testing.T) {
	uploadServer := newUploadServer()
	defer uploadServer.Close()

	urlServer := newURLServer(t, uploadServer.URL)
	defer urlServer.Close()

	mockSample, err := os.CreateTemp("", "metricsample")
	if err != nil {
		t.Error(err)
	}
	defer func(name string) {
		err = os.Remove(name)
		if err != nil {
			t.Error(err)
		}
	}(mockSample.Name())
	_, err = mockSample.WriteString("some data")
	if err != nil {
		t.Error(err)
	}

	config := client.Configuration{
		Token:      test.SecureRandomAlphaString(20),
		BaseURL:    urlServer.URL + "/metricsample",
		MaxRetries: 1,
		Region:     "us-west-2",
	}

	testCases := []struct {
		name                            string
		config                          client.Configuration
		outboundProxySet                bool
		useProxyForGettingUploadURLOnly bool
		expectedProxyHitCount           int32
	}{
		{
			name:                            "no proxy",
			config:                          config,
			outboundProxySet:                false,
			useProxyForGettingUploadURLOnly: false,
			expectedProxyHitCount:           0,
		},
		{
			name:                            "proxy enabled for both requests",
			config:                          config,
			outboundProxySet:                true,
			useProxyForGettingUploadURLOnly: false,
			expectedProxyHitCount:           2,
		},
		{
			name:                            "proxy enabled for only get upload url requests - proxy url not set",
			config:                          config,
			outboundProxySet:                false,
			useProxyForGettingUploadURLOnly: true,
			expectedProxyHitCount:           0,
		},
		{
			name:                            "proxy enabled for only get upload url requests - proxy url is set",
			config:                          config,
			outboundProxySet:                true,
			useProxyForGettingUploadURLOnly: true,
			expectedProxyHitCount:           1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err = mockSample.Seek(0, io.SeekStart)
			if err != nil {
				t.Error(err)
			}
			proxy, actualProxyHitCount := newProxySpy()
			defer proxy.Close()

			proxyURL, err := url.Parse(proxy.URL)
			if err != nil {
				t.Error(err)
			}
			tc.config.UseProxyForGettingUploadURLOnly = tc.useProxyForGettingUploadURLOnly
			if tc.outboundProxySet {
				tc.config.ProxyURL = *proxyURL
			}

			mockClient, err := client.NewHTTPMetricClient(tc.config)
			if err != nil {
				t.Error(err)
			}

			err = mockClient.SendMetricSample(mockSample, "version", "uid")
			if err != nil {
				t.Error(err)
			}

			if *actualProxyHitCount != tc.expectedProxyHitCount {
				t.Errorf("Actual proxy hits: %d, expected: %d", actualProxyHitCount, tc.expectedProxyHitCount)
			}
		})
	}
}

func newProxySpy() (*httptest.Server, *int32) {
	hitCounter := int32(0)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hitCounter, 1)
		req, err := http.NewRequest(r.Method, r.URL.String(), r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req.Header = r.Header.Clone()

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()
		_, err = io.Copy(w, resp.Body)
		w.WriteHeader(http.StatusOK)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	return proxy, &hitCounter
}

func newURLServer(t *testing.T, uploadURL string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/metricsample", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Content-type", "application/json")
		_, err := fmt.Fprintf(w, `{"Location":"%s"}`, uploadURL)
		if err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusOK)
	})
	return httptest.NewServer(mux)
}

func newUploadServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}
