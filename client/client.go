package client

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"crypto/md5" //nolint gosec

	"github.com/cloudability/metrics-agent/measurement"
	"github.com/cloudability/metrics-agent/util"
	"github.com/cloudability/metrics-agent/version"
)

//nolint gosec

const defaultBaseURL = "https://metrics-collector.cloudability.com"
const defaultTimeout = 1 * time.Minute
const defaultMaxRetries = 5

const authHeader = "token"
const apiKeyHeader = "x-api-key"
const clusterUIDHeader = "x-cluster-uid"
const agentVersionHeader = "x-agent-version"
const contentTypeHeader = "Content-Type"
const userAgentHeader = "User-Agent"
const uploadFileHash = "x-upload-file"
const contentMD5 = "Content-MD5"
const proxyAuthHeader = "Proxy-Authorization"

var /* const */ validToken = regexp.MustCompile(`^\w+$`)

// Configuration represents configurable values for the Cloudability Client
type Configuration struct {
	Timeout       time.Duration
	Token         string
	MaxRetries    int
	BaseURL       string
	ProxyURL      url.URL
	ProxyAuth     string
	ProxyInsecure bool
	Verbose       bool
}

// NewHTTPMetricClient will configure a new instance of a Cloudability client.
func NewHTTPMetricClient(cfg Configuration) (MetricClient, error) {

	if cfg.Timeout.Seconds() > 60 {
		return nil, errors.New("A valid timeout is required (between 1s and 60s")
	}
	if !validToken.MatchString(cfg.Token) {
		return nil, errors.New("Token format is invalid (only alphanumeric are allowed)")
	}

	// Use defaults
	if cfg.Timeout.Seconds() <= 0 {
		if cfg.Verbose {
			log.Infof("Using default timeout of %v", defaultTimeout)
		}
		cfg.Timeout = defaultTimeout
	}
	if len(strings.TrimSpace(cfg.BaseURL)) == 0 {
		if cfg.Verbose {
			log.Infof("Using default baseURL of %v", defaultBaseURL)
		}
		cfg.BaseURL = defaultBaseURL
	}
	if cfg.MaxRetries <= 0 {
		if cfg.Verbose {
			log.Infof("Using default retries %v", defaultMaxRetries)
		}
		cfg.MaxRetries = defaultMaxRetries
	}

	netTransport := &http.Transport{
		Dial:                (&net.Dialer{Timeout: cfg.Timeout}).Dial,
		TLSHandshakeTimeout: cfg.Timeout,
	}

	// configure outbound proxy
	if len(cfg.ProxyURL.Host) > 0 {
		ConnectHeader := http.Header{}

		if cfg.ProxyAuth != "" {
			basicAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(cfg.ProxyAuth))
			ConnectHeader.Add(proxyAuthHeader, basicAuth)
		}

		netTransport = &http.Transport{
			Dial:                (&net.Dialer{Timeout: cfg.Timeout}).Dial,
			Proxy:               http.ProxyURL(&cfg.ProxyURL),
			ProxyConnectHeader:  ConnectHeader,
			TLSHandshakeTimeout: cfg.Timeout,
			TLSClientConfig: &tls.Config{
				//nolint gas
				InsecureSkipVerify: cfg.ProxyInsecure,
			},
		}
	}

	httpClient := http.Client{
		Timeout:   cfg.Timeout,
		Transport: netTransport,
	}

	userAgent := fmt.Sprintf("cldy-client/%v", version.VERSION)

	return httpMetricClient{
		httpClient: httpClient,
		userAgent:  userAgent,
		baseURL:    cfg.BaseURL,
		token:      cfg.Token,
		verbose:    cfg.Verbose,
		maxRetries: cfg.MaxRetries,
	}, nil

}

// MetricClient represents a interface to send a cloudability measurement or metrics sample to an endpoint.
type MetricClient interface {
	SendMeasurement(measurements []measurement.Measurement) error
	SendMetricSample(*os.File, string, string) error
	GetUploadURL(*os.File, string, string, string) (string, string, error)
}

type httpMetricClient struct {
	httpClient http.Client
	userAgent  string
	baseURL    string
	token      string
	verbose    bool
	maxRetries int
}

// MetricSampleResponse represents the response from the uploadmetrics endpoint
type MetricSampleResponse struct {
	Location string `json:"location"`
}

func (c httpMetricClient) SendMeasurement(measurements []measurement.Measurement) error {

	measurementURL := c.baseURL + "/metrics"

	b, err := toJSONLines(measurements)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, measurementURL, bytes.NewBuffer(b))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(authHeader, c.token)
	req.Header.Set(apiKeyHeader, c.token)
	req.Header.Set(userAgentHeader, c.userAgent)

	if c.verbose {
		requestDump, err := httputil.DumpRequest(req, true)
		if err != nil {
			log.Errorln(err)
		}
		log.Infoln(string(requestDump))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Request received %v response", resp.StatusCode)
	}

	if c.verbose {
		responseDump, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Errorln(err)
		}
		log.Infoln(string(responseDump))
	}

	return nil
}

// SendMetricSample uploads a file at a given path to the metrics endpoint.
func (c httpMetricClient) SendMetricSample(metricSampleFile *os.File, agentVersion string, UID string) (rerr error) {
	metricSampleURL := c.baseURL + "/metricsample"

	resp, err := c.retryWithBackoff(metricSampleURL, metricSampleFile, agentVersion, UID)
	if err != nil {
		return err
	}
	if resp == nil {
		return err
	}

	defer util.SafeClose(resp.Body.Close, &rerr)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Request received %v response", resp.StatusCode)
	}

	if c.verbose {
		responseDump, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Errorln(err)
		}
		log.Infof("%q", responseDump)
	}

	return nil
}

func toJSONLines(measurements []measurement.Measurement) ([]byte, error) {
	output := []byte{}
	newline := "\n"
	for _, m := range measurements {
		b, err := json.Marshal(m)
		if err != nil {
			return nil, err
		}
		output = append(output, b...)
		output = append(output, newline...)
	}
	return output, nil
}

func (c httpMetricClient) retryWithBackoff(
	metricSampleURL string,
	metricFile *os.File,
	agentVersion,
	UID string,
) (resp *http.Response, err error) {

	for i := 0; i < c.maxRetries; i++ {

		var uploadURL, hash string
		uploadURL, hash, err = c.GetUploadURL(metricFile, metricSampleURL, agentVersion, UID)
		if err != nil {
			log.Debugf("Client proxy or deployment YAML may be misconfigured.  Please check your client settings.")
			log.Errorf("error encountered while retrieving upload location: %v", err)
			continue
		}
		log.Infof("Successfully retrieved upload URL with retry=%v", i)

		resp, err = c.buildAndDoRequest(metricFile, uploadURL, agentVersion, UID, hash)
		if err == nil {
			log.Infof("Successfully put sample with retry=%v", i)
		}

		if err != nil && strings.Contains(err.Error(), "Client.Timeout exceeded") {
			time.Sleep(getSleepDuration(i))
			continue
		}

		if resp == nil {
			continue
		}

		buf := new(bytes.Buffer)
		_, err = buf.ReadFrom(resp.Body)
		if err != nil {
			continue
		}

		s := buf.String()

		if strings.Contains(s, "Incompatible agent version please upgrade") {
			panic("Incompatible agent version please upgrade")
		}
		if resp.StatusCode == http.StatusInternalServerError || resp.StatusCode == http.StatusForbidden {
			time.Sleep(getSleepDuration(i))
			continue
		}

		break
	}

	return resp, err
}

func (c httpMetricClient) buildAndDoRequest(
	metricFile *os.File,
	metricSampleURL,
	agentVersion,
	UID string,
	hash string,
) (resp *http.Response, err error) {

	var (
		req *http.Request
	)

	metricFile, err = os.Open(metricFile.Name())
	if err != nil {
		log.Infof("Failed to open metric sample: %v", err)
		return nil, err
	}

	fi, err := metricFile.Stat()
	if err != nil {
		log.Infof("Failed to read metric from file: %v", err)
		return nil, err
	}

	size := fi.Size()

	req, err = http.NewRequest(http.MethodPut, metricSampleURL, metricFile)
	if err != nil {
		log.Infof("Failed to put metrics sample: %v", err)
		// dump request
		requestDump, err := httputil.DumpRequest(req, true)
		if err != nil {
			log.Error(err)
		}
		log.Infoln(string(requestDump))
		log.Infof("File info : %+v", metricFile)
		return nil, err
	}

	req.Header.Set(contentTypeHeader, "multipart/form-data")
	req.Header.Set(contentMD5, hash)
	req.ContentLength = size

	if c.verbose {
		requestDump, err := httputil.DumpRequest(req, true)
		if err != nil {
			log.Error(err)
		}
		log.Infoln(string(requestDump))
		log.Infof("File info : %+v", metricFile)
	}

	return c.httpClient.Do(req)
}

func getSleepDuration(tries int) time.Duration {
	seconds := int((0.5) * (math.Pow(2, float64(tries)) - 1))
	return time.Duration(seconds) * time.Second
}

func (c httpMetricClient) GetUploadURL(
	metricFile *os.File,
	metricSampleURL,
	agentVersion,
	UID string,
) (string, string, error) {
	var rerr error
	hash, err := GetB64MD5Hash(metricFile.Name())
	if err != nil {
		log.Errorf("error encountered generating upload check sum: %v", err)
		return "", "", err
	}

	d := MetricSampleResponse{}

	req, err := http.NewRequest(http.MethodPost, metricSampleURL, nil)
	if err != nil {
		return "", "", err
	}

	req.Header.Set(contentTypeHeader, "application/json")
	req.Header.Set(authHeader, c.token)
	req.Header.Set(apiKeyHeader, c.token)
	req.Header.Set(userAgentHeader, c.userAgent)
	req.Header.Set(agentVersionHeader, agentVersion)
	req.Header.Set(clusterUIDHeader, UID)
	req.Header.Set(uploadFileHash, hash)

	if c.verbose {
		requestDump, err := httputil.DumpRequest(req, true)
		if err != nil {
			log.Errorln(err)
		}
		log.Infoln(string(requestDump))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("Unable to retrieve upload URI: %v", err)
	}

	defer util.SafeClose(resp.Body.Close, &rerr)

	if c.verbose {
		responseDump, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Errorln(err)
		}
		log.Infoln(string(responseDump))
	}

	if resp.StatusCode != 200 {
		return "", d.Location, errors.New("Error retrieving upload URI: " + strconv.Itoa(resp.StatusCode))
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err == nil && data != nil {
		err = json.Unmarshal(data, &d)
	}

	return d.Location, hash, err
}

// GetB64MD5Hash returns base64 encoded MD5 Hash
func GetB64MD5Hash(name string) (b64Hash string, rerr error) {
	//nolint gosec
	f, err := os.Open(name)
	if err != nil {
		log.Fatal(err)
	}

	defer util.SafeClose(f.Close, &rerr)

	//nolint gas
	h := md5.New()

	if _, err := io.Copy(h, f); err != nil {
		log.Fatal(err)
	}

	return base64.StdEncoding.EncodeToString(h.Sum(nil)), err
}
