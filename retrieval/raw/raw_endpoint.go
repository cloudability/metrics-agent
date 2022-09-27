package raw

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	v1 "k8s.io/api/core/v1"
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

// Client defines an HTTP Client
type Client struct {
	HTTPClient      *http.Client
	insecure        bool
	BearerToken     string
	BearerTokenPath string
	retries         uint
	parseMetricData bool
}

// NewClient creates a new raw.Client
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

// createRequest creates a HTTP request using a given client
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

// GetRawEndPoint retrives the body of HTTP response from a given method ,
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
		return filename, fmt.Errorf("unable to create raw request for %s", sourceName)
	}

	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return filename, errors.New("unable to connect")
	}

	defer util.SafeClose(resp.Body.Close, &rerr)

	if !(resp.StatusCode >= 200 && resp.StatusCode <= 299) {
		return filename, fmt.Errorf("invalid response %s", strconv.Itoa(resp.StatusCode))
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
		return filename, errors.New("unable to create raw metric file")
	}
	defer util.SafeClose(rawRespFile.Close, &rerr)
	filename = rawRespFile.Name()

	if _, ok := ParsableFileSet[sourceName]; c.parseMetricData && ok {
		err = parseAndWriteData(sourceName, resp.Body, rawRespFile)
		return filename, err
	}

	_, err = io.Copy(rawRespFile, resp.Body)
	if err != nil {
		return filename, fmt.Errorf("error writing file: %s", rawRespFile.Name())
	}

	return filename, rerr
}

// TODO: investigate streamed json reading / writing
func parseAndWriteData(filename string, reader io.Reader, writer io.Writer) error {
	var to = getType(filename)
	out := reflect.New(reflect.TypeOf(to))
	err := json.NewDecoder(reader).Decode(out.Interface())

	if err != nil {
		return fmt.Errorf("unable to decode data for file: %s", filename)
	}
	to = sanitizeData(out.Elem().Interface())

	data, err := json.Marshal(to)
	if err != nil {
		return fmt.Errorf("unable to marshal data for file: %s", filename)
	}
	_, err = io.Copy(writer, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("error writing file: %s", filename)
	}
	return nil
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
	case NamespaceList:
		return sanitizeNamespaceData(to)
	}
	return to
}

func sanitizeNamespaceData(to interface{}) interface{} {
	cast := to.(NamespaceList)
	for i := range cast.Items {
		cast.Items[i].ObjectMeta.ManagedFields = nil
	}
	return cast
}

func sanitizeSelectorMatchedResourceList(to interface{}) interface{} {
	cast := to.(LabelSelectorMatchedResourceList)
	for i := range cast.Items {

		// stripping env var and related data from the object
		cast.Items[i].ObjectMeta.ManagedFields = nil
		if _, ok := cast.Items[i].ObjectMeta.Annotations[KubernetesLastAppliedConfig]; ok {
			delete(cast.Items[i].ObjectMeta.Annotations, KubernetesLastAppliedConfig)
		}
	}
	return cast
}

func sanitizePodList(to interface{}) interface{} {
	cast := to.(PodList)
	for i := range cast.Items {

		// stripping env var and related data from the object
		cast.Items[i].ObjectMeta.ManagedFields = nil
		if _, ok := cast.Items[i].ObjectMeta.Annotations[KubernetesLastAppliedConfig]; ok {
			delete(cast.Items[i].ObjectMeta.Annotations, KubernetesLastAppliedConfig)
		}
		for j, container := range cast.Items[i].Spec.Containers {
			cast.Items[i].Spec.Containers[j] = sanitizeContainer(container)
		}
		for j, container := range cast.Items[i].Spec.InitContainers {
			cast.Items[i].Spec.InitContainers[j] = sanitizeContainer(container)
		}
	}
	return cast
}

func sanitizeContainer(container v1.Container) v1.Container {
	container.Env = nil
	container.Command = nil
	container.Args = nil
	container.ImagePullPolicy = ""
	container.LivenessProbe = nil
	container.StartupProbe = nil
	container.ReadinessProbe = nil
	container.TerminationMessagePath = ""
	container.TerminationMessagePolicy = ""
	container.SecurityContext = nil
	return container
}

func sanitizeMapMatchedResourceList(to interface{}) interface{} {
	cast := to.(LabelMapMatchedResourceList)
	for i := range cast.Items {

		// stripping env var and related data from the object
		cast.Items[i].ObjectMeta.ManagedFields = nil
		if _, ok := cast.Items[i].ObjectMeta.Annotations[KubernetesLastAppliedConfig]; ok {
			delete(cast.Items[i].ObjectMeta.Annotations, KubernetesLastAppliedConfig)
		}
		cast.Items[i].Finalizers = nil
	}
	return cast
}
