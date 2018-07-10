package raw

import (
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cloudability/metrics-agent/util"
)

//Client defines an HTTP Client
type Client struct {
	HTTPClient  *http.Client
	insecure    bool
	bearerToken string
	retries     uint
}

//NewClient creates a new raw.Client
func NewClient(HTTPClient http.Client, insecure bool, bearerToken string) Client {
	return Client{
		HTTPClient:  &HTTPClient,
		insecure:    insecure,
		bearerToken: bearerToken,
		retries:     uint(3),
	}
}

//createRequest creates a HTTP request using a given client
func (c *Client) createRequest(method, url string, body io.Reader) (*http.Request, error) {

	request, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	if c.bearerToken != "" {
		request.Header.Add("Authorization", "Bearer "+c.bearerToken)
	}

	return request, err
}

//GetRawEndPoint retrives the body of HTTP response from a given sourcename, working directory, and URL
func (c *Client) GetRawEndPoint(sourceName string, workDir *os.File, URL string, retries uint) (rawRespFile *os.File, err error) {

	attempts := retries + 1

	for i := uint(0); i < attempts; i++ {
		rawRespFile, err := downloadToFile(c, sourceName, workDir, URL, i)
		if err != nil {
			log.Printf("%v URL: %s retrying: %v", err, URL, i)
			time.Sleep(time.Duration(int64(math.Pow(2, float64(i)))) * time.Second)
			continue
		}
		return rawRespFile, err
	}
	return &os.File{}, err
}

func downloadToFile(c *Client, sourceName string, workDir *os.File, URL string, retryCount uint) (rawRespFile *os.File, rerr error) {

	var empty os.File
	var fileExt string

	req, err := c.createRequest("GET", URL, nil)
	if err != nil {
		return &empty, errors.New("Unable to create raw request for " + sourceName)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return &empty, errors.New("Unable to connect to")
	} else if !(resp.StatusCode >= 200 && resp.StatusCode <= 299) {
		return &empty, errors.New("Invalid response " + strconv.Itoa(resp.StatusCode))
	}

	defer util.SafeClose(resp.Body.Close, &rerr)

	ct := resp.Header.Get("Content-Type")

	if strings.Contains(ct, "application/json") {
		fileExt = ".json"
	} else if strings.Contains(ct, "text/plain") {
		fileExt = ".txt"
	} else {
		fileExt = ""
	}

	rawRespFile, err = os.Create(workDir.Name() + "/" + sourceName + fileExt)
	if err != nil {
		return &empty, errors.New("Unable to create raw metric file")
	}

	_, err = io.Copy(rawRespFile, resp.Body)
	if err != nil {
		return &empty, errors.New("Error writing file: " + rawRespFile.Name())
	}

	return rawRespFile, err
}
