// © Copyright Apptio, an IBM Corp. 2024, 2025

package client_test

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

// nolint: gocyclo
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
	// unsupported region should default to us-west-2 url generation
	uploadURL = client.GetUploadURLByRegion("my-unsupported-region-1")
	if uploadURL != client.DefaultBaseURL {
		t.Error("Unsupported region default to US URL was not generated correctly")
	}
}
