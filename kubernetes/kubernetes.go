package kubernetes

import (
	//nolint gosec
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cloudability/metrics-agent/client"
	"github.com/cloudability/metrics-agent/measurement"
	k8s_stats "github.com/cloudability/metrics-agent/retrieval/k8s"
	"github.com/cloudability/metrics-agent/retrieval/raw"
	"github.com/cloudability/metrics-agent/util"
	cldyVersion "github.com/cloudability/metrics-agent/version"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

//ClusterVersion contains a concatenated version number as well as the k8s version discovery info
type ClusterVersion struct {
	version     float64
	versionInfo *version.Info
}

//KubeAgentConfig K8s agent configuration
type KubeAgentConfig struct {
	APIKey                string
	BearerToken           string
	Cert                  string
	ClusterName           string
	ClusterHostURL        string
	clusterUID            string
	HeapsterOverrideURL   string
	HeapsterURL           string
	Key                   string
	nodeRetrievalMethod   string
	OutboundProxyAuth     string
	OutboundProxy         string
	provisioningID        string
	RetrieveNodeSummaries bool
	Insecure              bool
	OutboundProxyInsecure bool
	UseInClusterConfig    bool
	CollectHeapsterExport bool
	PollInterval          int
	failedNodeList        map[string]error
	AgentStartTime        time.Time
	Clientset             kubernetes.Interface
	ClusterVersion        ClusterVersion
	HeapsterProxyURL      url.URL
	OutboundProxyURL      url.URL
	HTTPClient            http.Client
	NodeClient            raw.Client
	InClusterClient       raw.Client
	msExportDirectory     *os.File
	TLSClientConfig       rest.TLSClientConfig
	Namespace             string
}

const uploadInterval time.Duration = 10
const retryCount uint = 10

//nolint llll
const kbURL string = "https://support.cloudability.com/hc/en-us/articles/360008368193-Kubernetes-Metrics-Agent-Error-Messages"

//CollectKubeMetrics Collects metrics from Kubernetes on a predetermined interval
func CollectKubeMetrics(config KubeAgentConfig) {

	log.Printf("Starting Cloudability Kubernetes Metric Agent")

	validateMetricCollectionConfig(config.RetrieveNodeSummaries, config.CollectHeapsterExport)

	// Create k8s agent
	kubeAgent := newKubeAgent(config)

	// Log start time
	kubeAgent.AgentStartTime = time.Now()

	clientSetNodeSource := NewClientsetNodeSource(kubeAgent.Clientset)

	// run , sleep etc..
	doneChan := make(chan bool)

	sendChan := time.NewTicker(uploadInterval * time.Minute)

	pollChan := time.NewTicker(time.Duration(config.PollInterval) * time.Second)

	err := fetchDiagnostics(kubeAgent.Clientset, config.Namespace, kubeAgent.msExportDirectory)

	if err != nil {
		log.Printf("Warning non-fatal error: Agent error occurred retrieving runtime diagnostics: %s ", err)
		log.Printf("For more information see: %v", kbURL)
	}

	err = downloadBaselineMetricExport(kubeAgent, clientSetNodeSource)

	if err != nil {
		log.Printf("Warning: Non-fatal error occurred retrieving baseline metrics: %s\n", err)
	}

	log.Printf("Cloudability Metrics Agent successfully started.")

	for {
		select {

		case <-sendChan.C:

			//Bundle raw metrics
			metricSample, err := util.CreateMetricSample(*kubeAgent.msExportDirectory, kubeAgent.clusterUID, true)
			if err != nil {
				log.Fatalf("Error creating metric sample: %s\n", err)
			}
			//Send metric sample
			log.Println("Uploading Metrics")
			go kubeAgent.sendMetrics(metricSample)

		case <-pollChan.C:
			err := kubeAgent.collectMetrics(kubeAgent, kubeAgent.Clientset, clientSetNodeSource)
			if err != nil {
				log.Fatalf("Error retrieving metrics %v", err)
			}

		case <-doneChan:
			return
		}
	}

}

func validateMetricCollectionConfig(retrieveNodeSummaries bool, collectHeapsterExport bool) {
	if !retrieveNodeSummaries && !collectHeapsterExport {
		log.Fatalf("Invalid agent configuration.")
	}
	if retrieveNodeSummaries {
		log.Printf("Primary metrics collected directly from each node.")
	}
	if retrieveNodeSummaries && collectHeapsterExport {
		log.Printf("Collecting Heapster exports if found in cluster.")
	} else if collectHeapsterExport {
		log.Printf("Primary metrics collected from Heapster exports.")
	}
}

func newKubeAgent(config KubeAgentConfig) KubeAgentConfig {

	config, err := createClusterConfig(config)
	if err != nil {
		log.Fatalf("cloudability metric agent is unable to initialize cluster configuration: %v", err)
	}

	config, err = updateConfig(config)
	if err != nil {
		log.Fatalf("cloudability metric agent is unable update cluster configuration options: %v", err)
	}

	// launch local services if we can't connect to them
	config = ensureMetricServicesAvailable(config)

	//Create metric sample working directory
	config.msExportDirectory, err = util.CreateMSWorkingDirectory(config.clusterUID)
	if err != nil {
		log.Fatalf("cloudability metric agent is unable to create a temporary working directory: %v", err)
	}

	return config

}

func (ka KubeAgentConfig) collectMetrics(
	config KubeAgentConfig, clientset kubernetes.Interface, nodeSource NodeSource) (rerr error) {

	sampleStartTime := time.Now().UTC()

	//create metric sample directory
	msd, metricSampleDir, err := createMSD(config.msExportDirectory.Name(), sampleStartTime)
	if err != nil {
		return err
	}

	if config.RetrieveNodeSummaries {

		err = retrieveNodeSummaries(config, msd, metricSampleDir, nodeSource)
		if err != nil {
			log.Printf("Warning: %s", err)
		}
	} else {
		// get raw Heapster metric sample
		hme, err := config.InClusterClient.GetRawEndPoint(
			http.MethodGet, "heapster-metrics-export", metricSampleDir, config.HeapsterURL, nil, true)
		if err != nil {
			return fmt.Errorf("unable to retrieve raw heapster metrics: %s", err)
		}

		defer util.SafeClose(hme.Close, &rerr)

		baselineMetricSample, err := util.MatchOneFile(
			path.Dir(config.msExportDirectory.Name()), "/baseline-metrics-export*")
		if err == nil || err.Error() == "No matches found" {
			if err = handleBaselineHeapsterMetrics(
				config.msExportDirectory.Name(), msd, baselineMetricSample, hme.Name()); err != nil {
				log.Printf("Warning: updating Heapster Baseline failed: %v", err)
			}
		}
	}

	// export additional metrics from the k8s api to the metric sample directory
	err = k8s_stats.GetK8sMetrics(
		config.ClusterHostURL, config.ClusterVersion.version, metricSampleDir, config.InClusterClient)
	if err != nil {
		return fmt.Errorf("unable to export k8s metrics: %s", err)
	}

	// create agent measurement and add it to measurements
	err = createAgentStatusMetric(metricSampleDir, config, sampleStartTime)
	if err != nil {
		return fmt.Errorf("unable to create cldy measurement: %s", err)
	}

	return err
}

func createMSD(exportDir string, sampleStartTime time.Time) (string, *os.File, error) {
	msd := exportDir + "/" + sampleStartTime.Format(
		"20060102150405") + "/" + strconv.FormatInt(sampleStartTime.Unix(), 10)
	err := os.MkdirAll(msd, os.ModePerm)
	if err != nil {
		return msd, nil, fmt.Errorf("error creating metric sample directory : %v", err)
	}
	//nolint gosec
	metricSampleDir, err := os.Open(msd)
	if err != nil {
		return msd, metricSampleDir, fmt.Errorf("unable to open metric sample export directory")
	}
	return msd, metricSampleDir, nil
}

func fetchNodeBaselines(msd, exportDirectory string) error {
	// get baseline metrics for each node
	err := filepath.Walk(path.Dir(exportDirectory), func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && filePath != path.Dir(exportDirectory) {
			return filepath.SkipDir
		}
		if strings.HasPrefix(info.Name(), "baseline-summary") || strings.HasPrefix(info.Name(), "baseline-container") {
			err = os.Rename(filePath, filepath.Join(msd, info.Name()))
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("error updating baseline metrics: %s", err)
	}
	return nil
}

func updateNodeBaselines(msd, exportDirectory string) error {
	err := filepath.Walk(msd, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(info.Name(), "stats-") {
			if err != nil {
				return err
			}
			nodeName := getNodeName("stats", info.Name())
			baselineNodeMetric := path.Dir(exportDirectory) + fmt.Sprintf("/baseline%s.json", nodeName)

			// update baseline metric for this node with most recent sample from this collection
			err = util.CopyFileContents(baselineNodeMetric, filePath)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("error updating baseline metrics: %s", err)
	}
	return nil
}

func (ka KubeAgentConfig) sendMetrics(metricSample *os.File) {
	cldyMetricClient, err := client.NewHTTPMetricClient(client.Configuration{
		Token:         ka.APIKey,
		Verbose:       false,
		ProxyURL:      ka.OutboundProxyURL,
		ProxyAuth:     ka.OutboundProxyAuth,
		ProxyInsecure: ka.OutboundProxyInsecure,
	})

	if err != nil {
		log.Fatalf("error creating Cloudability Metric client: %v ", err)
	}

	err = SendData(metricSample, ka.clusterUID, cldyMetricClient)
	if err != nil {
		log.Fatalf("error sending metrics: %v", err)
	}
}

// SendData takes Cloudability metric sample and sends data to Cloudability via go client
func SendData(ms *os.File, uid string, mc client.MetricClient) (err error) {
	err = mc.SendMetricSample(ms, cldyVersion.VERSION, uid)
	if err != nil {
		log.Printf("cloudability write failed: %v", err)
	} else {
		log.Printf("Exported metric sample %s to cloudability", ms.Name())
		err = os.Remove(ms.Name())
		if err != nil {
			log.Printf("Warning: Unable to cleanup after metric sample upload: %v", err)
		}

	}
	return err
}

func createClusterConfig(config KubeAgentConfig) (KubeAgentConfig, error) {
	// try and connect to the cluster using in-cluster-config
	thisConfig, err := rest.InClusterConfig()

	// If creating an in-cluster-config fails
	// read in KUBERNETES_MASTER & KUBECONFIG environment variables
	// fall back to an anonymous clientconfig
	if err != nil {
		km := os.Getenv("KUBERNETES_MASTER")
		kc := os.Getenv("KUBECONFIG")
		if km != "" && kc != "" {
			thisConfig, err := clientcmd.BuildConfigFromFlags(km, kc)
			if err != nil {
				log.Print(
					"The cloudability metrics agent is unable to create cluster config")
			}
			config.UseInClusterConfig = false
			config.ClusterHostURL = thisConfig.Host
			config.Cert = thisConfig.CertFile
			config.Key = thisConfig.KeyFile
			config.TLSClientConfig = thisConfig.TLSClientConfig
			config.Clientset, err = kubernetes.NewForConfig(thisConfig)
			return config, err
		}
		log.Print(
			"The cloudability metrics agent is unable to create cluster config via a service account. \n",
			"Does this deployment have an associated service account? Trying Anonymous")
		// create anonymous / Insecure client config
		thisConfig, err = clientcmd.DefaultClientConfig.ClientConfig()
		if err != nil {
			log.Fatalf("cloudability metric agent is unable to create a default anonymous cluster configuration: %v", err)
		}
		thisConfig.Insecure = true
		config.UseInClusterConfig = false
		config.ClusterHostURL = thisConfig.Host
		config.Cert = thisConfig.CertFile
		config.Key = thisConfig.KeyFile
		config.TLSClientConfig = thisConfig.TLSClientConfig
		config.Clientset, err = kubernetes.NewForConfig(thisConfig)
		return config, err

	}
	config.UseInClusterConfig = true
	config.ClusterHostURL = thisConfig.Host
	config.Cert = thisConfig.CertFile
	config.Key = thisConfig.KeyFile
	config.TLSClientConfig.CAFile = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	config.BearerToken = thisConfig.BearerToken
	if config.Namespace == "" {
		config.Namespace = "cloudability"
	}

	config.Clientset, err = kubernetes.NewForConfig(thisConfig)
	return config, err

}

func updateConfig(config KubeAgentConfig) (KubeAgentConfig, error) {
	updatedConfig, err := updateConfigurationForServices(config)
	if err != nil {
		log.Fatalf("cloudability metric agent is unable set internal configuration options: %v", err)
	}

	updatedConfig = updateConfigWithOverrideURLs(updatedConfig)
	updatedConfig, err = createKubeHTTPClient(updatedConfig)
	if err != nil {
		return updatedConfig, err
	}
	updatedConfig.InClusterClient = raw.NewClient(updatedConfig.HTTPClient, config.Insecure, config.BearerToken, 3)

	updatedConfig.clusterUID, err = getNamespaceUID(updatedConfig.Clientset, "default")
	if err != nil {
		log.Fatalf("cloudability metric agent is unable to find the default namespace: %v", err)
	}

	updatedConfig.ClusterVersion, err = getClusterVersion(updatedConfig.Clientset)
	if err != nil {
		log.Printf("cloudability metric agent is unable to determine the cluster version: %v", err)
	}

	updatedConfig.provisioningID, err = getProvisioningID(updatedConfig.APIKey)

	return updatedConfig, err
}

func updateConfigurationForServices(config KubeAgentConfig) (
	KubeAgentConfig, error) {

	var err error
	proxyRef, err := setProxyURL(config.OutboundProxy)
	if err != nil {
		log.Fatalf("cloudability metric agent encountered an error while setting the outbound proxy: %v", err)
	}

	config.OutboundProxyURL = proxyRef

	if config.CollectHeapsterExport {
		config.HeapsterProxyURL, err = getHeapsterURL(config.Clientset, config.ClusterHostURL)
		if err != nil {
			log.Printf("cloudability metric agent encountered an error while looking for heapster: %v", err)
		}
	}

	return config, err
}

func setProxyURL(op string) (u url.URL, err error) {
	if op != "" {
		u, err := url.ParseRequestURI(op)
		if !strings.Contains(u.Scheme, "http") && !strings.Contains(u.Scheme, "https") {
			return *u, errors.New("Proxy URL must use http:// or https:// scheme")
		}
		return *u, err
	}

	return u, err
}

// returns the UID of a given Namespace
func getNamespaceUID(clientset kubernetes.Interface, namespace string) (
	string, error) {
	defaultNamespace, err := clientset.CoreV1().Namespaces().Get(namespace, metav1.GetOptions{})
	if err != nil {
		log.Fatalf("cloudability metric agent is unable to get the cluster UID: %v", err)
	}
	clusterUID := defaultNamespace.UID
	return string(clusterUID), err
}

// returns the discovered cluster version information
func getClusterVersion(clientset kubernetes.Interface) (cv ClusterVersion, err error) {

	vi, err := clientset.Discovery().ServerVersion()
	cv.versionInfo = vi

	if err != nil {
		log.Printf("cloudability metric agent is unable to determine the cluster version: %v", err)
		return cv, err
	}

	// generate a compiled version number
	reg := regexp.MustCompile("[^0-9D.*$]+")
	cv.version, err = strconv.ParseFloat(reg.ReplaceAllString(cv.versionInfo.Major+"."+cv.versionInfo.Minor, ""), 64)
	if err != nil {
		log.Printf("Error parsing cluster version: %v", err)
	}

	return cv, err
}

// returns the provisioningID (SHA1 value) generated from a given string
func getProvisioningID(s string) (string, error) {
	//nolint gosec
	h := sha1.New()
	_, err := h.Write([]byte(s))
	sha1Hash := hex.EncodeToString(h.Sum(nil))

	return sha1Hash, err
}

func downloadBaselineMetricExport(config KubeAgentConfig, nodeSource NodeSource) (rerr error) {
	ed, err := os.Open(path.Dir(config.msExportDirectory.Name()))
	if err != nil {
		log.Fatalln("Unable to open metric sample export directory")
	}

	defer util.SafeClose(ed.Close, &rerr)

	// get baseline metric sample
	if config.RetrieveNodeSummaries {
		config.failedNodeList, err = downloadNodeData("baseline", config, ed, nodeSource)
		if len(config.failedNodeList) > 0 {
			log.Printf("Warning: Failed to retrive metric data from %v nodes. Metric samples may be incomplete: %+v %v",
				len(config.failedNodeList), config.failedNodeList, err)
		}
	}

	if config.CollectHeapsterExport {
		// get baseline metric sample
		_, err = config.InClusterClient.GetRawEndPoint(
			http.MethodGet, "baseline-metrics-export", ed, config.HeapsterURL, nil, false)
		if err != nil && !config.RetrieveNodeSummaries {
			return fmt.Errorf("Heapster metrics: %s", err)
		}
		return nil
	}
	return err
}

func updateConfigWithOverrideURLs(config KubeAgentConfig) KubeAgentConfig {

	// determine heapster URL to use
	if config.HeapsterOverrideURL != "" && util.IsValidURL(config.HeapsterOverrideURL) {
		config.HeapsterURL = config.HeapsterOverrideURL
	} else {
		config.HeapsterURL = config.HeapsterProxyURL.Host + config.HeapsterProxyURL.Path
	}
	return config
}

func ensureMetricServicesAvailable(config KubeAgentConfig) KubeAgentConfig {
	var err error

	if config.RetrieveNodeSummaries {
		config, err = ensureNodeSource(config)
		if err != nil {
			log.Printf("Warning non-fatal error: Agent error occurred retrieving node source metrics: %s ", err)
			log.Printf("For more information see: %v", kbURL)
		}
	}
	if config.CollectHeapsterExport {
		config, err = ensureValidHeapster(config)
		if err != nil {
			log.Printf("Unable to validate heapster connectivity: %v exiting", err)
			os.Exit(1)
		}
	}
	return config
}

func createKubeHTTPClient(config KubeAgentConfig) (KubeAgentConfig, error) {

	var (
		transport *http.Transport
		err       error
		cert      tls.Certificate
		tlsConfig *tls.Config
	)

	//Check for client side certificates / inClusterConfig
	if config.Insecure {
		transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				// nolint gas
				InsecureSkipVerify: true,
			},
		}
		config.HTTPClient = http.Client{Transport: transport}

		return config, err
	}

	pemData, err := ioutil.ReadFile(config.TLSClientConfig.CAFile)
	if err != nil {
		log.Fatalf("Could not load CA certificate: %v", err)
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(pemData)

	if config.Cert != "" || config.Key != "" {
		cert, err = tls.LoadX509KeyPair(config.Cert, config.Key)

		if err != nil {
			log.Fatalf("Unable to load cert: %s key: %s error: %v", config.Cert, config.Key, err)
		}

		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      caCertPool,
		}
		tlsConfig.BuildNameToCertificate()
		transport = &http.Transport{TLSClientConfig: tlsConfig}

		config.HTTPClient = http.Client{Transport: transport}

		return config, err
	}

	tlsConfig = &tls.Config{
		RootCAs: caCertPool,
	}
	transport = &http.Transport{TLSClientConfig: tlsConfig}

	config.HTTPClient = http.Client{Transport: transport}

	return config, err

}

//CreateAgentStatusMetric creates a agent status measurement and returns a Cloudability Measurement
func createAgentStatusMetric(workDir *os.File, config KubeAgentConfig, sampleStartTime time.Time) error {
	var err error

	m := measurement.Measurement{
		Name:      "cldy_agent_status",
		Tags:      make(map[string]string),
		Metrics:   make(map[string]uint64),
		Values:    make(map[string]string),
		Errors:    make([]measurement.ErrorDetail, 0),
		Timestamp: sampleStartTime.Unix(),
	}

	now := time.Now()

	exportFile := workDir.Name() + "/agent-measurement.json"

	m.Tags["cluster_uid"] = config.clusterUID
	m.Values["agent_version"] = cldyVersion.VERSION
	m.Values["cluster_name"] = config.ClusterName
	m.Values["cluster_version_git"] = config.ClusterVersion.versionInfo.GitVersion
	m.Values["cluster_version_major"] = config.ClusterVersion.versionInfo.Major
	m.Values["cluster_version_minor"] = config.ClusterVersion.versionInfo.Minor
	m.Values["heapster_url"] = config.HeapsterURL
	m.Values["heapster_override_url"] = config.HeapsterOverrideURL
	m.Values["incluster_config"] = strconv.FormatBool(config.UseInClusterConfig)
	m.Values["insecure"] = strconv.FormatBool(config.Insecure)
	m.Values["poll_interval"] = strconv.Itoa(config.PollInterval)
	m.Values["provisioning_id"] = config.provisioningID
	m.Values["outbound_proxy_url"] = config.OutboundProxyURL.String()
	m.Values["node_retrieval_method"] = config.nodeRetrievalMethod
	m.Values["retrieve_node_summaries"] = strconv.FormatBool(config.RetrieveNodeSummaries)
	if len(config.OutboundProxyAuth) > 0 {
		m.Values["outbound_proxy_auth"] = "true"
	} else {
		m.Values["outbound_proxy_auth"] = "false"
	}
	m.Metrics["uptime"] = uint64(now.Sub(config.AgentStartTime).Seconds())
	if len(config.failedNodeList) > 0 {

		for k, v := range config.failedNodeList {
			m.Errors = append(m.Errors, measurement.ErrorDetail{
				Name:    k,
				Message: v.Error(),
				Type:    "node_error",
			})
		}
	}

	if err != nil {
		log.Printf("Error creating Cloudability Agent status m : %v", err)
	}

	cldyMetric, err := json.Marshal(m)
	if err != nil {
		log.Printf("An error occurred converting Cldy m.  Error: %v\n", err)
	}

	err = ioutil.WriteFile(exportFile, cldyMetric, 0644)
	if err != nil {
		log.Printf("An error occurred creating a Cldy m.  Error: %v\n", err)
	}

	return err
}

func getNodeName(prefix, fileName string) string {
	if strings.Contains(fileName, prefix) {
		name := fileName[len(prefix) : len(fileName)-5]
		return name
	}
	return ""
}

func getPodLogs(clientset kubernetes.Interface,
	namespace, podName, containerName string, previous bool, dst io.Writer) (err error) {

	req := clientset.CoreV1().Pods(namespace).GetLogs(podName,
		&v1.PodLogOptions{
			Container: containerName,
			Previous:  previous,
		})

	readCloser, err := req.Stream()
	if err != nil {
		return err
	}

	defer util.SafeClose(readCloser.Close, &err)

	_, err = io.Copy(dst, readCloser)
	return err
}

func fetchDiagnostics(clientset kubernetes.Interface, namespace string, msExportDirectory *os.File) (err error) {

	pods, err := clientset.CoreV1().Pods(namespace).List(metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("Unable to retrieve Pod list: %v", err)
	}

	for _, pod := range pods.Items {
		if strings.Contains(pod.Name, "metrics-agent") && time.Since(pod.Status.StartTime.Time) > (time.Minute*3) {
			for _, c := range pod.Status.ContainerStatuses {

				f, err := os.Create(msExportDirectory.Name() + "/agent.diag")
				if err != nil {
					return err
				}

				defer util.SafeClose(f.Close, &err)

				_, err = f.WriteString(
					fmt.Sprintf(
						"Agent Diagnostics for Pod: %v container: %v restarted %v times \n state: %+v \n Previous runtime log: \n",
						pod.Name, c.Name, c.RestartCount, c.LastTerminationState))
				if err != nil {
					return err
				}

				err = getPodLogs(clientset, namespace, pod.Name, c.Name, false, f)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil

}
