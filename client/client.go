package client

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

const DefaultBaseURL string = "https://metrics-collector.cloudability.com/metricsample"
const EUBaseURL string = "https://metrics-collector-eu.cloudability.com/metricsample"
const AUBaseURL string = "https://metrics-collector-au.cloudability.com/metricsample"
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
	Region        string
}

// NewHTTPMetricClient will configure a new instance of a Cloudability client.
func NewHTTPMetricClient(cfg Configuration) (MetricClient, error) {

	if !validToken.MatchString(cfg.Token) {
		return nil, errors.New("token format is invalid (only alphanumeric are allowed). Please check you " +
			"are using your Containers Insights API Key (not Frontdoor). This can be found in the YAML after " +
			"provisioning in the 'Insights -> Containers' UI under the CLOUDABILITY_API_KEY environment variable")
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
			log.Infof("Using default baseURL of %v", DefaultBaseURL)
		}
		cfg.BaseURL = GetUploadURLByRegion(cfg.Region)
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
	SendMetricSample(*os.File, string, string) error
	GetUploadURL(*os.File, string, string, string, int) (string, string, error)
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

// SendMetricSample uploads a file at a given path to the metrics endpoint.
func (c httpMetricClient) SendMetricSample(metricSampleFile *os.File, agentVersion string, UID string) (rerr error) {
	metricSampleURL := c.baseURL

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

// nolint gocyclo
func (c httpMetricClient) retryWithBackoff(
	metricSampleURL string,
	metricFile *os.File,
	agentVersion,
	UID string,
) (resp *http.Response, err error) {

	for i := 0; i < c.maxRetries; i++ {

		var uploadURL, hash string
		uploadURL, hash, err = c.GetUploadURL(metricFile, metricSampleURL, agentVersion, UID, i)
		if err != nil {
			log.Debugf("Client proxy or deployment YAML may be misconfigured.  Please check your client settings.")
			log.Errorf("error encountered while retrieving upload location: %v", err)
			continue
		}

		var awsRequestID, statusMessage string
		var responseDump, requestDump []byte
		var dumpErr error
		resp, requestDump, err = c.buildAndDoRequest(metricFile, uploadURL, agentVersion, UID, hash)
		if resp != nil {
			awsRequestID = resp.Header.Get("X-Amz-Request-Id")
			statusMessage = resp.Status
			responseDump, dumpErr = httputil.DumpResponse(resp, true)
			if dumpErr != nil {
				log.Errorln(dumpErr)
			}
		}

		if err != nil && strings.Contains(err.Error(), "Client.Timeout exceeded") {
			time.Sleep(getSleepDuration(i))
			log.Errorf("Put S3 Retry %d: Failed to put data to S3 due to request timeout, "+
				"Status: %s X-Amzn-Requestid: %s", i, statusMessage, awsRequestID)
			log.Debugln(string(requestDump))
			if resp != nil {
				log.Debugln(string(responseDump))
			}
			continue
		}

		if resp == nil {
			log.Errorf("Put S3 Retry %d: Failed to put data to S3. Response is empty", i)
			log.Debugln(string(requestDump))
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
			log.Errorf("Put S3 Retry %d: Failed to put data to S3, Status: %s X-Amzn-Requestid: %s", i,
				statusMessage, awsRequestID)
			log.Debugln(string(requestDump))
			log.Debugln(string(responseDump))
			continue
		}
		log.Infof("Put S3 Retry %d: Successfully put data to S3, X-Amzn-Requestid: %s", i, awsRequestID)
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
) (resp *http.Response, requestDump []byte, err error) {

	var (
		req     *http.Request
		dumpErr error
	)

	metricFile, err = os.Open(metricFile.Name())
	if err != nil {
		log.Fatalf("Failed to open metric sample: %v", err)
		return nil, nil, err
	}

	fi, err := metricFile.Stat()
	if err != nil {
		return nil, nil, err
	}

	size := fi.Size()

	req, err = http.NewRequest(http.MethodPut, metricSampleURL, metricFile)
	if err != nil {
		return nil, nil, err
	}

	req.Header.Set(contentTypeHeader, "multipart/form-data")
	req.Header.Set(contentMD5, hash)
	req.ContentLength = size

	requestDump, dumpErr = httputil.DumpRequest(req, false)
	if dumpErr != nil {
		log.Errorln(dumpErr)
	}

	resp, respErr := c.httpClient.Do(req)

	return resp, requestDump, respErr
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
	attempt int,
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
		requestDump, requestErr := httputil.DumpRequest(req, true)
		if requestErr != nil {
			log.Errorln(requestErr)
		}
		log.Infoln(string(requestDump))
	}

	var awsRequestID, statusMessage string
	var responseDump []byte
	var dumpErr error

	resp, err := c.httpClient.Do(req)
	if resp != nil {
		awsRequestID = resp.Header.Get("X-Amzn-Requestid")
		statusMessage = resp.Status
		responseDump, dumpErr = httputil.DumpResponse(resp, true)
		if dumpErr != nil {
			log.Errorln(dumpErr)
		}
	}
	if err != nil {
		log.Errorf("GetURL Retry %d: Failed to acquire s3 url, Status: %s X-Amzn-Requestid: %s", attempt,
			statusMessage, awsRequestID)
		if resp != nil {
			log.Debugln(string(responseDump))
		}
		return "", "", fmt.Errorf("Unable to retrieve upload URI: %v", err)
	}

	defer util.SafeClose(resp.Body.Close, &rerr)

	if resp.StatusCode != 200 {
		log.Errorf("GetURL Retry %d: Failed to acquire s3 url, Status: %s X-Amzn-Requestid: %s", attempt,
			statusMessage, awsRequestID)
		log.Debugln(string(responseDump))
		return "", d.Location, errors.New("Error retrieving upload URI: " + strconv.Itoa(resp.StatusCode))
	}

	data, err := io.ReadAll(resp.Body)
	if err == nil && data != nil {
		err = json.Unmarshal(data, &d)
	}

	log.Infof("GetURL Retry %d: Successfully acquired s3 url, X-Amzn-Requestid: %s", attempt, awsRequestID)
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

// GetUploadURLByRegion returns the correct base url depending on the env variable CLOUDABILITY_UPLOAD_REGION.
// If value is not supported, default to us-west-2 (original) URL
func GetUploadURLByRegion(region string) string {
	switch region {
	case "eu-central-1":
		return EUBaseURL
	case "ap-southeast-2":
		return AUBaseURL
	case "us-west-2":
		return DefaultBaseURL
	default:
		log.Warnf("Region %s is not supported. Defaulting to us-west-2 region.", region)
		return DefaultBaseURL
	}
}
