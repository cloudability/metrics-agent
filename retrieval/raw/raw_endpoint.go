package raw

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/cloudability/metrics-agent/util"
)

//Client defines an HTTP Client
type Client struct {
	HTTPClient  *http.Client
	insecure    bool
	bearerToken string
}

//NewClient creates a new raw.Client
func NewClient(HTTPClient http.Client, insecure bool, bearerToken string) Client {
	return Client{
		HTTPClient:  &HTTPClient,
		insecure:    insecure,
		bearerToken: bearerToken,
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
func (c *Client) GetRawEndPoint(sourceName string, workDir *os.File, URL string) (rawRespFile *os.File, rerr error) {

	var empty os.File
	var fileExt string

	req, err := c.createRequest("GET", URL, nil)
	if err != nil {
		return &empty, errors.New("Unable to create raw request for " + sourceName)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return &empty, err
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
		fmt.Print(err)
		return &empty, errors.New("Error writing file: " + rawRespFile.Name())
	}

	return rawRespFile, err
}
