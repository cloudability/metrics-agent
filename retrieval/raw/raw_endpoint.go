package raw

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/cloudability/metrics-agent/util"
	log "github.com/sirupsen/logrus"
)

const (
	KubernetesLastAppliedConfig = "kubectl.kubernetes.io/last-applied-configuration"
)

//Client defines an HTTP Client
type Client struct {
	HTTPClient      *http.Client
	insecure        bool
	BearerToken     string
	BearerTokenPath string
	retries         uint
	parseMetricData bool
}

//NewClient creates a new raw.Client
func NewClient(HTTPClient http.Client, insecure bool, bearerToken, bearerTokenPath string, retries uint,
	parseMetricData bool) Client {
	return Client{
		HTTPClient:      &HTTPClient,
		insecure:        insecure,
		BearerToken:     bearerToken,
		BearerTokenPath: bearerTokenPath,
		retries:         retries,
		parseMetricData: parseMetricData,
	}
}

//createRequest creates a HTTP request using a given client
func (c *Client) createRequest(method, url string, body io.Reader) (*http.Request, error) {

	request, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	if c.BearerToken != "" {
		request.Header.Add("Authorization", "bearer "+c.BearerToken)
	}

	return request, err
}

//GetRawEndPoint retrives the body of HTTP response from a given method ,
// sourcename, working directory, URL, and request body
func (c *Client) GetRawEndPoint(method, sourceName string,
	workDir *os.File, URL string, body []byte, verbose bool) (filename string, err error) {

	attempts := c.retries + 1
	b := bytes.NewBuffer(body)

	for i := uint(0); i < attempts; i++ {
		if i > 0 {
			time.Sleep(time.Duration(int64(math.Pow(2, float64(i)))) * time.Second)
		}
		filename, err = downloadToFile(c, method, sourceName, workDir, URL, b)
		if err == nil {
			return filename, nil
		}
		if verbose {
			log.Warnf("%v URL: %s -- retrying: %v", err, URL, i+1)
		}
	}
	return filename, err
}

func downloadToFile(c *Client, method, sourceName string, workDir *os.File, URL string,
	body io.Reader) (filename string, rerr error) {

	var fileExt string

	req, err := c.createRequest(method, URL, body)
	if err != nil {
		return filename, errors.New("Unable to create raw request for " + sourceName)
	}

	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return filename, errors.New("Unable to connect")
	}

	defer util.SafeClose(resp.Body.Close, &rerr)

	if !(resp.StatusCode >= 200 && resp.StatusCode <= 299) {
		return filename, fmt.Errorf("Invalid response %s", strconv.Itoa(resp.StatusCode))
	}

	ct := resp.Header.Get("Content-Type")

	if strings.Contains(ct, "application/json") {
		fileExt = ".json"
	} else if strings.Contains(ct, "text/plain") {
		fileExt = ".txt"
	} else {
		fileExt = ""
	}

	rawRespFile, err := os.Create(workDir.Name() + "/" + sourceName + fileExt)
	if err != nil {
		return filename, errors.New("Unable to create raw metric file")
	}
	defer util.SafeClose(rawRespFile.Close, &rerr)
	filename = rawRespFile.Name()

	if _, ok := ParsableFileSet[sourceName]; c.parseMetricData && ok {
		err = parseAndWriteData(sourceName, resp.Body, rawRespFile)
		return filename, err
	}

	_, err = io.Copy(rawRespFile, resp.Body)
	if err != nil {
		return filename, errors.New("Error writing file: " + rawRespFile.Name())
	}

	return filename, rerr
}

// TODO: investigate streamed json reading / writing
func parseAndWriteData(filename string, reader io.Reader, writer io.Writer) error {
	var to = getType(filename)
	out := reflect.New(reflect.TypeOf(to))
	err := json.NewDecoder(reader).Decode(out.Interface())

	if err != nil {
		return err
	}
	to = sanitizeData(out.Elem().Interface())

	data, err := json.Marshal(to)
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, bytes.NewReader(data))
	return err
}

func getType(filename string) interface{} {
	var to interface{}
	switch filename {
	case Nodes:
		to = NodeList{}
	case Namespaces:
		to = NamespaceList{}
	case Pods:
		to = PodList{}
	case PersistentVolumes:
		to = PersistentVolumeList{}
	case PersistentVolumeClaims:
		to = PersistentVolumeClaimList{}
	case AgentMeasurement:
		to = CldyAgent{}
	case Services, ReplicationControllers:
		to = LabelMapMatchedResourceList{}
	case Deployments, ReplicaSets, Jobs, DaemonSets:
		to = LabelSelectorMatchedResourceList{}
	}
	return to
}

func sanitizeData(to interface{}) interface{} {
	switch to.(type) {
	case LabelSelectorMatchedResourceList:
		return sanitizeSelectorMatchedResourceList(to)
	case PodList:
		return sanitizePodList(to)
	case LabelMapMatchedResourceList:
		return sanitizeMapMatchedResourceList(to)
	}
	return to
}

func sanitizeSelectorMatchedResourceList(to interface{}) interface{} {
	cast := to.(LabelSelectorMatchedResourceList)
	for i, item := range cast.Items {

		// stripping env var and related data from the object
		item.ObjectMeta.ManagedFields = nil
		if _, ok := item.ObjectMeta.Annotations[KubernetesLastAppliedConfig]; ok {
			delete(item.ObjectMeta.Annotations, KubernetesLastAppliedConfig)
		}
		cast.Items[i] = item
	}
	return cast
}

func sanitizePodList(to interface{}) interface{} {
	cast := to.(PodList)
	for i, item := range cast.Items {

		// stripping env var and related data from the object
		item.ObjectMeta.ManagedFields = nil
		if _, ok := item.ObjectMeta.Annotations[KubernetesLastAppliedConfig]; ok {
			delete(item.ObjectMeta.Annotations, KubernetesLastAppliedConfig)
		}
		cast.Items[i] = item
	}
	return cast
}

func sanitizeMapMatchedResourceList(to interface{}) interface{} {
	cast := to.(LabelMapMatchedResourceList)
	for i, item := range cast.Items {

		// stripping env var and related data from the object
		item.ObjectMeta.ManagedFields = nil
		if _, ok := item.ObjectMeta.Annotations[KubernetesLastAppliedConfig]; ok {
			delete(item.ObjectMeta.Annotations, KubernetesLastAppliedConfig)
		}
		cast.Items[i] = item
	}
	return cast
}

// Within the items[].metadata object there are two fields that can commonly contain environment variables.
// We delete these entries as they are unneeded for processing and could potentially contain sensitive data
//func sanitizeResources(to interface{}) interface{} {
//	cast := to.(map[string]interface{})
//	if items, ok := cast["items"]; ok {
//		resources := items.([]interface{})
//		for i, resource := range resources {
//			item := resource.(map[string]interface{})
//			if meta, ok := item["metadata"]; ok {
//				metadata := meta.(map[string]interface{})
//				delete(metadata, "managedFields")
//				delete(metadata, "annotations")
//				item["metadata"] = metadata
//			}
//			resources[i] = item
//		}
//		cast["items"] = resources
//	}
//	return cast
//}
