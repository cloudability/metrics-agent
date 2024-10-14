//nolint:gosec
package kubernetes

import (
	"context"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/cloudability/metrics-agent/client"
	"github.com/cloudability/metrics-agent/measurement"
	k8s_stats "github.com/cloudability/metrics-agent/retrieval/k8s"
	"github.com/cloudability/metrics-agent/retrieval/raw"
	"github.com/cloudability/metrics-agent/util"
	cldyVersion "github.com/cloudability/metrics-agent/version"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

// ClusterVersion contains a concatenated version number as well as the k8s version discovery info
type ClusterVersion struct {
	version     float64
	versionInfo *version.Info
}

// KubeAgentConfig K8s agent configuration
type KubeAgentConfig struct {
	APIKey                             string
	BearerToken                        string
	BearerTokenPath                    string
	Cert                               string
	ClusterName                        string
	ClusterHostURL                     string
	clusterUID                         string
	HeapsterURL                        string
	Key                                string
	OutboundProxyAuth                  string
	OutboundProxy                      string
	provisioningID                     string
	ForceKubeProxy                     bool
	Insecure                           bool
	OutboundProxyInsecure              bool
	UseInClusterConfig                 bool
	PollInterval                       int
	ConcurrentPollers                  int
	CollectionRetryLimit               uint
	failedNodeList                     map[string]error
	AgentStartTime                     time.Time
	Clientset                          kubernetes.Interface
	ClusterVersion                     ClusterVersion
	HeapsterProxyURL                   url.URL
	OutboundProxyURL                   url.URL
	HTTPClient                         http.Client
	NodeClient                         raw.Client
	InClusterClient                    raw.Client
	msExportDirectory                  *os.File
	TLSClientConfig                    rest.TLSClientConfig
	Namespace                          string
	ScratchDir                         string
	NodeMetrics                        EndpointMask
	Informers                          map[string]*cache.SharedIndexInformer
	InformerResyncInterval             int
	ParseMetricData                    bool
	HTTPSTimeout                       int
	UploadRegion                       string
	CustomS3UploadBucket               string
	CustomS3Region                     string
	CustomAzureUploadBlobContainerName string
	CustomAzureBlobURL                 string
	CustomAzureTenantID                string
	CustomAzureClientID                string
	CustomAzureClientSecret            string
}

const uploadInterval time.Duration = 10
const retryCount uint = 10
const DefaultCollectionRetry = 1
const DefaultInformerResync = 24

// node connection methods
const proxy = "proxy"
const direct = "direct"
const unreachable = "unreachable"

const kbTroubleShootingURL string = "https://help.apptio.com/en-us/cloudability/product/k8s-metrics-agent.htm"
const kbProvisionURL string = "https://help.apptio.com/en-us/cloudability/product/k8s-cluster-provisioning.htm"
const forbiddenError string = uploadURIError + ": 403"
const uploadURIError string = "Error retrieving upload URI"

// nolint lll
const transportError string = `Network transport issues are potentially blocking the agent from contacting the metrics collection API.
	Please confirm that the metrics-agent is able to establish a connection to: %s`

// nolint lll
const apiKeyError string = `Current Cloudability API Key is expired and access needs to be re-enabled before re-provisioning the metrics-agent as detailed here: %s.
	Please contact support to re-activate the API keys.
	Note: Be sure to use the exact same cluster name as what is currently in use.
	***IMPORTANT*** If the cluster is managed by GKE - there are special instructions for provisioning.`
const rbacError string = `RBAC role in the Cloudability namespace may need to be updated.
	Re-provision the metrics-agent for this cluster as detailed here: ` + kbProvisionURL +
	`Note: Be sure to use the exact same cluster name as what is currently in use.
	***IMPORTANT*** If the cluster is managed by GKE - there are special instructions for provisioning.`

// CollectKubeMetrics Collects metrics from Kubernetes on a predetermined interval
// nolint: gocyclo
func CollectKubeMetrics(config KubeAgentConfig) {

	log.Infof("Starting Cloudability Kubernetes Metric Agent version: %v", cldyVersion.VERSION)
	log.Infof("Metric collection retry limit set to %d (default is %d)",
		config.CollectionRetryLimit, DefaultCollectionRetry)
	log.Debugf("Informer resync interval is set to %d (default is %d)",
		config.InformerResyncInterval, DefaultInformerResync)

	ctx := context.Background()

	// Create k8s agent
	kubeAgent := newKubeAgent(ctx, config)

	customS3Mode := isCustomS3UploadEnvsSet(&kubeAgent)
	customAzureMode := isCustomAzureUploadEnvsSet(&kubeAgent)

	// Log start time
	kubeAgent.AgentStartTime = time.Now()

	clientSetNodeSource := NewClientsetNodeSource(kubeAgent.Clientset)

	// run , sleep etc..
	doneChan := make(chan bool)

	sendChan := time.NewTicker(uploadInterval * time.Minute)

	pollChan := time.NewTicker(time.Duration(config.PollInterval) * time.Second)

	err := fetchDiagnostics(ctx, kubeAgent.Clientset, config.Namespace, kubeAgent.msExportDirectory)

	if err != nil {
		log.Warnf(rbacError)
		log.Warnf("Warning non-fatal error: Agent error occurred retrieving runtime diagnostics: %s ", err)
		log.Warnf("For more information see: %v", kbTroubleShootingURL)
	}

	// informer channel, closes only if metrics-agent stops executing
	// closing this will kill all informers
	informerStopCh := make(chan struct{})
	// start up informers for each of the k8s resources that metrics are being collected on
	kubeAgent.Informers, err = k8s_stats.StartUpInformers(kubeAgent.Clientset, kubeAgent.ClusterVersion.version,
		config.InformerResyncInterval, informerStopCh)
	if err != nil {
		log.Warnf("Warning: Informers failed to start up: %s", err)
	}
	defer close(informerStopCh)

	err = downloadBaselineMetricExport(ctx, kubeAgent, clientSetNodeSource)

	if err != nil {
		log.Warnf("Warning: Non-fatal error occurred retrieving baseline metrics: %s", err)
	}

	if !customS3Mode && !customAzureMode {
		err = performConnectionChecks(&kubeAgent)
		if err != nil {
			log.Warnf("WARNING: failed to retrieve S3 URL in connectivity test, agent will fail to "+
				"upload metrics to Cloudability with error: %v", err)
		}
	}

	log.Info("Cloudability Metrics Agent successfully started.")

	for {
		select {

		case <-sendChan.C:
			// Bundle raw metrics
			metricSample, err := util.CreateMetricSample(
				*kubeAgent.msExportDirectory, kubeAgent.clusterUID, true, kubeAgent.ScratchDir)
			if err != nil {
				switch err {
				case util.ErrEmptyDataDir:
					log.Warn("Got an empty data directory, skipping this send")
					continue
				default:
					log.Fatalf("Error creating metric sample: %s", err)
				}
			}
			// Send metric sample
			kubeAgent.sendMetricsBasedOnUploadMode(customS3Mode, customAzureMode, metricSample)

		case <-pollChan.C:
			err := kubeAgent.collectMetrics(ctx, kubeAgent, kubeAgent.Clientset, clientSetNodeSource)
			if err != nil {
				log.Fatalf("Error retrieving metrics %v", err)
			}

		case <-doneChan:
			ctx.Done()
			return
		}
	}

}

// isCustomS3UploadEnvsSet checks to see if the agent has a custom S3 location and S3 region to upload to
// if both these variables are not set, default upload to Apptio S3
func isCustomS3UploadEnvsSet(ka *KubeAgentConfig) bool {
	if ka.CustomS3Region == "" && ka.CustomS3UploadBucket == "" {
		if ka.APIKey == "" {
			log.Fatalf("Invalid agent configuration. CLOUDABILITY_API_KEY is required " +
				"when not using CLOUDABILITY_CUSTOM_S3_BUCKET & CLOUDABILITY_CUSTOM_S3_REGION")
		}
		return false
	}
	if ka.CustomS3UploadBucket == "" || ka.CustomS3Region == "" {
		log.Fatalf("Invalid agent configuration. Detected only one of the two required environment variables "+
			"to run in custom S3 upload mode. CLOUDABILITY_CUSTOM_S3_BUCKET is set to %s and "+
			"CLOUDABILITY_CUSTOM_S3_REGION is set to %s.", ka.CustomS3UploadBucket, ka.CustomS3Region)
	}
	log.Infof("Detected custom S3 bucket location and S3 bucket region. "+
		"Will upload collected metrics to %s in the aws region %s", ka.CustomS3UploadBucket, ka.CustomS3Region)
	return true
}

func isCustomAzureUploadEnvsSet(ka *KubeAgentConfig) bool {
	if ka.CustomAzureUploadBlobContainerName == "" && ka.CustomAzureBlobURL == "" && ka.CustomAzureClientID == "" &&
		ka.CustomAzureClientSecret == "" && ka.CustomAzureTenantID == "" {
		if ka.APIKey == "" {
			log.Fatalf("Invalid agent configuration. CLOUDABILITY_API_KEY is required " +
				"when not using CLOUDABILITY_CUSTOM_AZURE_BLOB env vars.")
		}
		return false
	}
	if ka.CustomAzureUploadBlobContainerName == "" || ka.CustomAzureBlobURL == "" || ka.CustomAzureClientID == "" ||
		ka.CustomAzureClientSecret == "" || ka.CustomAzureTenantID == "" {
		log.Fatalf("Invalid agent configuration. Detected only one of the six required environment variables "+
			"to run in custom Azure upload mode. CLOUDABILITY_CUSTOM_AZURE_BLOB_CONTAINER_NAME is set to %s. "+
			"CLOUDABILITY_CUSTOM_AZURE_BLOB_URL is set to %s CLOUDABILITY_CUSTOM_AZURE_TENANT_ID is set to %s. "+
			"CLOUDABILITY_CUSTOM_AZURE_CLIENT_ID is set to %s. CLOUDABILITY_CUSTOM_AZURE_CLIENT_SECRET is set to %s.",
			ka.CustomAzureUploadBlobContainerName, ka.CustomAzureBlobURL, ka.CustomAzureClientID, ka.CustomAzureTenantID,
			ka.CustomAzureClientSecret)
	}
	log.Infof("Detected custom Azure blob configuration, "+
		"Will upload collected metrics to %s", ka.CustomAzureUploadBlobContainerName)
	return true
}

func performConnectionChecks(ka *KubeAgentConfig) error {

	log.Info("Performing connectivity checks. Checking that the agent can retrieve S3 URL")

	cldyMetricClient, err := client.NewHTTPMetricClient(client.Configuration{
		Token:         ka.APIKey,
		Verbose:       false,
		ProxyURL:      ka.OutboundProxyURL,
		ProxyAuth:     ka.OutboundProxyAuth,
		ProxyInsecure: ka.OutboundProxyInsecure,
		Timeout:       time.Duration(ka.HTTPSTimeout) * time.Second,
		Region:        ka.UploadRegion,
	})
	if err != nil {
		return err
	}

	metricSampleURL := client.GetUploadURLByRegion(ka.UploadRegion)

	file, err := os.Create("/tmp/temp.txt")
	if err != nil {
		return errors.New("failed to create temp.txt file in connectivity test")
	}
	defer os.Remove(file.Name())

	_, err = file.WriteString("Health Check")
	if err != nil {
		return errors.New("failed to write in file temp.txt in connectivity test")
	}

	_, _, err = cldyMetricClient.GetUploadURL(file, metricSampleURL, cldyVersion.VERSION, ka.clusterUID, 0)
	if err != nil {
		return err
	}
	log.Info("Connectivity check succeeded")
	return nil
}

func newKubeAgent(ctx context.Context, config KubeAgentConfig) KubeAgentConfig {
	config, err := createClusterConfig(config)
	if err != nil {
		log.Fatalf("cloudability metric agent is unable to initialize cluster configuration: %v", err)
	}

	config, err = updateConfig(ctx, config)
	if err != nil {
		log.Fatalf("cloudability metric agent is unable update cluster configuration options: %v", err)
	}

	// launch local services if we can't connect to them
	config, err = ensureMetricServicesAvailable(ctx, config)
	if err != nil {
		log.Fatal(err)
	}

	// setup directory for writing metrics to
	err = util.ValidateScratchDir(config.ScratchDir)
	if err != nil {
		log.Fatal(err)
	}

	// Create metric sample working directory
	config.msExportDirectory, err = util.CreateMSWorkingDirectory(config.clusterUID, config.ScratchDir)
	if err != nil {
		log.Fatalf("cloudability metric agent is unable to create a temporary working directory: %v", err)
	}

	return config
}

func (ka KubeAgentConfig) collectMetrics(ctx context.Context, config KubeAgentConfig,
	clientset kubernetes.Interface, nodeSource NodeSource) (rerr error) {

	sampleStartTime := time.Now().UTC()

	// refresh client token before each collection
	token, err := getBearerToken(config.BearerTokenPath)
	if err != nil {
		log.Warnf("Warning: Unable to update service account token for cloudability-metrics-agent. If this"+
			" token is not refreshed in clusters >=1.21, the metrics-agent won't be able to collect data once"+
			" token is expired. %s", err)
	}

	config.BearerToken = token
	config.InClusterClient.BearerToken = token
	config.NodeClient.BearerToken = token

	// create metric sample directory
	msd, metricSampleDir, err := createMSD(config.msExportDirectory.Name(), sampleStartTime)
	if err != nil {
		return err
	}

	err = retrieveNodeSummaries(ctx, config, msd, metricSampleDir, nodeSource)
	if err != nil {
		log.Warnf("Warning: %s", err)
	}

	// export k8s resource metrics (ex: pods.jsonl) using informers to the metric sample directory
	err = k8s_stats.GetK8sMetricsFromInformer(config.Informers, metricSampleDir, config.ParseMetricData)
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
		if err != nil && os.IsPermission(err) {
			log.WithFields(log.Fields{
				"exportDirectory": exportDirectory,
				"filePath":        filePath,
				"error":           err,
			}).Warn("Error reading a folder or file when search for node baseline files")
		} else if err != nil {
			return err
		}
		if info.IsDir() && filePath != path.Dir(exportDirectory) {
			return filepath.SkipDir
		}
		if strings.HasPrefix(info.Name(), "baseline-summary") ||
			strings.HasPrefix(info.Name(), "baseline-container") ||
			strings.HasPrefix(info.Name(), "baseline-cadvisor") {
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
			nodeName, extension := extractNodeNameAndExtension("stats", info.Name())
			baselineNodeMetric := path.Dir(exportDirectory) + fmt.Sprintf("/baseline%s%s", nodeName, extension)

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
		Timeout:       time.Duration(ka.HTTPSTimeout) * time.Second,
		Region:        ka.UploadRegion,
	})

	if err != nil {
		log.Fatalf("error creating Cloudability Metric client: %v ", err)
	}

	err = SendData(metricSample, ka.clusterUID, cldyMetricClient)
	if err != nil {
		if warnErr := handleError(err, ka.UploadRegion); warnErr != "" {
			log.Warnf(warnErr)
		}
		log.Fatalf("error sending metrics: %v", err)
	}
}

func (ka KubeAgentConfig) sendMetricsBasedOnUploadMode(customS3Mode bool, customAzureMode bool, metricSample *os.File) {
	if customS3Mode {
		log.Infof("Uploading Metrics to Custom S3 Bucket %s", ka.CustomS3UploadBucket)
		go ka.sendMetricsToCustomS3(metricSample)
	} else if customAzureMode {
		log.Infof("Uploading Metrics to Custom Azure Blob %s", ka.CustomAzureUploadBlobContainerName)
		go ka.sendMetricsToCustomBlob(metricSample)
	} else {
		log.Info("Uploading Metrics")
		go ka.sendMetrics(metricSample)
	}
}

func (ka KubeAgentConfig) sendMetricsToCustomS3(metricSample *os.File) {
	sess, err := session.NewSession(&aws.Config{
		Region:     aws.String(ka.CustomS3Region),
		MaxRetries: aws.Int(3)},
	)
	if err != nil {
		log.Fatalf("Could not establish AWS Session, "+
			"ensure AWS environment variables are set correctly: %s", err)
	}
	svc := s3.New(sess)
	uploader := s3manager.NewUploaderWithClient(svc)

	fileReader, err := os.Open(metricSample.Name())
	if err != nil {
		log.Fatalf("Unable to open metric sample file %v", err)
	}
	defer fileReader.Close()

	key := generateSampleKey(ka.clusterUID, "aws")
	sampleToUpload := &s3manager.UploadInput{
		Bucket: aws.String(ka.CustomS3UploadBucket),
		Key:    aws.String(key),
		Body:   fileReader,
	}

	_, err = uploader.Upload(sampleToUpload)
	if err != nil {
		log.Fatalf("Failed to put Object to custom S3 with error: %s", err)
	} else {
		sn := strings.Split(metricSample.Name(), "/")
		log.Infof("Exported metric sample %s to custom S3 bucket: %s",
			strings.TrimSuffix(sn[len(sn)-1], ".tgz"), ka.CustomS3UploadBucket)
	}
	err = os.Remove(metricSample.Name())
	if err != nil {
		log.Warnf("Warning: Unable to cleanup after metric sample upload: %v", err)
	}
}

func (ka KubeAgentConfig) sendMetricsToCustomBlob(metricSample *os.File) {
	os.Setenv("AZURE_TENANT_ID", ka.CustomAzureTenantID)
	os.Setenv("AZURE_CLIENT_ID", ka.CustomAzureClientID)
	os.Setenv("AZURE_CLIENT_SECRET", ka.CustomAzureClientSecret)
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		log.Fatalf("Could not establish Azure credentials, "+
			"ensure Azure environment variables are set correctly: %s", err)
	}
	azureClient, err := azblob.NewClient(ka.CustomAzureBlobURL, cred, nil)
	if err != nil {
		log.Fatalf("Could not establish Azure Client, "+
			"ensure Azure environment variables are set correctly: %s", err)
	}
	ka.uploadBlob(azureClient, metricSample)
}

func (ka KubeAgentConfig) uploadBlob(client *azblob.Client, metricSample *os.File) {
	file, err := os.Open(metricSample.Name())
	if err != nil {
		log.Fatalf("Unable to open metric sample file %v", err)
	}
	defer file.Close()
	key := generateSampleKey(ka.clusterUID, "azure")
	_, err = client.UploadFile(context.Background(), ka.CustomAzureUploadBlobContainerName, key, file, nil)
	if err != nil {
		log.Fatalf("Failed to put Object to custom Azure blob with error: %s", err)
	} else {
		sn := strings.Split(metricSample.Name(), "/")
		log.Infof("Exported metric sample %s to custom Azure blob: %s",
			strings.TrimSuffix(sn[len(sn)-1], ".tgz"), ka.CustomAzureUploadBlobContainerName)
	}
	err = os.Remove(metricSample.Name())
	if err != nil {
		log.Warnf("Warning: Unable to cleanup after metric sample upload: %v", err)
	}
}

// generateSampleKey creates a key (location) for s3 or azure to upload the sample to. Example of s3 location format
// /production/data/metrics-agent/<YYYY>/<MM>/<DD>/<CLUSTER_UID>/<CLUSTER_UID>-<YYYYMMDD>-<HH>-<MM>.tgz
// example of azure location format
// production/data/metrics-agent/<YYYY>/<MM>/<DD>/<CLUSTER_UID>/<CLUSTER_UID>-<YYYYMMDD>-<HH>-<MM>.tgz
func generateSampleKey(clusterUID, provider string) string {
	currentTime := time.Now()
	year, month, day := currentTime.Date()
	hour := currentTime.Hour()
	minute := currentTime.Minute()
	if provider == "aws" {
		return fmt.Sprintf("/production/data/metrics-agent/%d/%02d/%02d/%s/%s-%s-%02d-%02d.tgz", year,
			int(month), day, clusterUID, clusterUID, currentTime.Format("20060102"), hour, minute)
	}
	if provider == "azure" {
		return fmt.Sprintf("production/data/metrics-agent/%d/%02d/%02d/%s/%s-%s-%02d-%02d.tgz", year,
			int(month), day, clusterUID, clusterUID, currentTime.Format("20060102"), hour, minute)
	}
	return fmt.Sprintf("/production/data/metrics-agent/%d/%02d/%02d/%s/%s-%s-%02d-%02d.tgz", year,
		int(month), day, clusterUID, clusterUID, currentTime.Format("20060102"), hour, minute)
}

func handleError(err error, region string) string {
	if err.Error() == forbiddenError {
		return fmt.Sprintf(apiKeyError, kbProvisionURL)
	} else if strings.Contains(err.Error(), uploadURIError) {
		return fmt.Sprintf(transportError, client.GetUploadURLByRegion(region))
	}
	return ""
}

// SendData takes Cloudability metric sample and sends data to Cloudability via go client
func SendData(ms *os.File, uid string, mc client.MetricClient) (err error) {
	err = mc.SendMetricSample(ms, cldyVersion.VERSION, uid)
	if err != nil {
		log.Warnf("cloudability write failed: %v", err)
	} else {
		sn := strings.Split(ms.Name(), "/")
		log.Infof("Exported metric sample %s to cloudability", strings.TrimSuffix(sn[len(sn)-1], ".tgz"))
		err = os.Remove(ms.Name())
		if err != nil {
			log.Warnf("Warning: Unable to cleanup after metric sample upload: %v", err)
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
				log.Warn(
					"The cloudability metrics agent is unable to create cluster config")
			}
			config.UseInClusterConfig = false
			config.ClusterHostURL = thisConfig.Host
			config.Cert = thisConfig.CertFile
			config.Key = thisConfig.KeyFile
			config.TLSClientConfig = thisConfig.TLSClientConfig
			config.Clientset, err = kubernetes.NewForConfig(thisConfig)
			config.BearerToken = thisConfig.BearerToken
			config.BearerTokenPath = thisConfig.BearerTokenFile
			return config, err
		}
		log.Warn(
			"Unable to create cluster config via a service account. Check for associated service account. Trying Anonymous")
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
		config.BearerTokenPath = thisConfig.BearerTokenFile
		return config, err

	}
	config.UseInClusterConfig = true
	config.ClusterHostURL = thisConfig.Host
	config.Cert = thisConfig.CertFile
	config.Key = thisConfig.KeyFile
	config.TLSClientConfig.CAFile = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	config.BearerToken = thisConfig.BearerToken
	config.BearerTokenPath = thisConfig.BearerTokenFile
	if config.Namespace == "" {
		config.Namespace = "cloudability"
	}

	config.Clientset, err = kubernetes.NewForConfig(thisConfig)
	return config, err

}

func updateConfig(ctx context.Context, config KubeAgentConfig) (KubeAgentConfig, error) {
	updatedConfig, err := updateConfigurationForServices(ctx, config)
	if err != nil {
		log.Fatalf("cloudability metric agent is unable set internal configuration options: %v", err)
	}

	updatedConfig.NodeMetrics = EndpointMask{}

	updatedConfig, err = createKubeHTTPClient(updatedConfig)
	if err != nil {
		return updatedConfig, err
	}
	updatedConfig.InClusterClient = raw.NewClient(updatedConfig.HTTPClient, config.Insecure,
		config.BearerToken, config.BearerTokenPath, config.CollectionRetryLimit, config.ParseMetricData)

	updatedConfig.clusterUID, err = getNamespaceUID(ctx, updatedConfig.Clientset, "default")
	if err != nil {
		log.Fatalf("cloudability metric agent is unable to find the default namespace: %v", err)
	}

	updatedConfig.ClusterVersion, err = getClusterVersion(updatedConfig.Clientset)
	if err != nil {
		log.Warnf("cloudability metric agent is unable to determine the cluster version: %v", err)
	}

	updatedConfig.provisioningID, err = getProvisioningID(updatedConfig.APIKey)

	return updatedConfig, err
}

func updateConfigurationForServices(ctx context.Context, config KubeAgentConfig) (
	KubeAgentConfig, error) {

	var err error
	proxyRef, err := setProxyURL(config.OutboundProxy)
	if err != nil {
		log.Fatalf("cloudability metric agent encountered an error while setting the outbound proxy: %v", err)
	}

	config.OutboundProxyURL = proxyRef

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
func getNamespaceUID(ctx context.Context, clientset kubernetes.Interface, namespace string) (
	string, error) {
	defaultNamespace, err := clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
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
		log.Warnf("cloudability metric agent is unable to determine the cluster version: %v", err)
		return cv, err
	}

	// generate a compiled version number
	reg := regexp.MustCompile("[^0-9D.*$]+")
	cv.version, err = strconv.ParseFloat(reg.ReplaceAllString(cv.versionInfo.Major+"."+cv.versionInfo.Minor, ""), 64)
	if err != nil {
		log.Warnf("Error parsing cluster version: %v", err)
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

func downloadBaselineMetricExport(ctx context.Context, config KubeAgentConfig, nodeSource NodeSource) (rerr error) {
	ed, err := os.Open(path.Dir(config.msExportDirectory.Name()))
	if err != nil {
		log.Fatalln("Unable to open metric sample export directory")
	}

	defer util.SafeClose(ed.Close, &rerr)

	// get baseline metric sample
	config.failedNodeList, err = downloadNodeData(ctx, "baseline", config, ed, nodeSource)
	if len(config.failedNodeList) > 0 {
		log.Warnf("Warning failed to retrieve metric data from %v nodes. Metric samples may be incomplete: %+v %v",
			len(config.failedNodeList), config.failedNodeList, err)
	}

	return err
}

func ensureMetricServicesAvailable(ctx context.Context, config KubeAgentConfig) (KubeAgentConfig, error) {
	config, err := ensureNodeSource(ctx, config)
	if err != nil {
		log.Warnf(handleNodeSourceError(err))
	} else {
		log.Infof("Node summaries connection method: %s", config.NodeMetrics.Options(NodeStatsSummaryEndpoint))
	}

	if err == FatalNodeError {
		//nolint lll
		log.Debugf(`Unable to retrieve data due to metrics-agent configuration.
			May be caused by cluster security mis-configurations or the RBAC role in the Cloudability namespace needs to be updated.
			Please confirm with your cluster security administrators that the RBAC role is able to work within your cluster's security configurations.`)
		return config, fmt.Errorf("unable to retrieve node summaries: %s", err)
	}

	return config, nil
}

func handleNodeSourceError(err error) string {
	var nodeError string
	if strings.Contains(err.Error(), "Please verify RBAC roles") {
		nodeError = rbacError
	}
	errStr := "Warning non-fatal error: Agent error occurred verifying node source metrics: %v\n" +
		"For more information see: %s"
	return nodeError + fmt.Sprintf(errStr, err, kbTroubleShootingURL)
}

func createKubeHTTPClient(config KubeAgentConfig) (KubeAgentConfig, error) {

	var (
		transport *http.Transport
		err       error
		cert      tls.Certificate
		tlsConfig *tls.Config
	)

	// Check for client side certificates / inClusterConfig
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

	pemData, err := os.ReadFile(config.TLSClientConfig.CAFile)
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

// CreateAgentStatusMetric creates a agent status measurement and returns a Cloudability Measurement
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
	m.Values["incluster_config"] = strconv.FormatBool(config.UseInClusterConfig)
	m.Values["insecure"] = strconv.FormatBool(config.Insecure)
	m.Values["poll_interval"] = strconv.Itoa(config.PollInterval)
	m.Values["provisioning_id"] = config.provisioningID
	m.Values["outbound_proxy_url"] = config.OutboundProxyURL.String()
	m.Values["stats_summary_retrieval_method"] = config.NodeMetrics.Options(NodeStatsSummaryEndpoint)
	m.Values["retrieve_node_summaries"] = "true"
	m.Values["informer_resync_interval"] = strconv.Itoa(config.InformerResyncInterval)
	m.Values["force_kube_proxy"] = strconv.FormatBool(config.ForceKubeProxy)
	m.Values["number_of_concurrent_node_pollers"] = strconv.Itoa(config.ConcurrentPollers)
	m.Values["parse_metric_data"] = strconv.FormatBool(config.ParseMetricData)
	m.Values["https_client_timeout"] = strconv.Itoa(config.HTTPSTimeout)
	m.Values["upload_region"] = config.UploadRegion
	m.Values["custom_s3_bucket"] = config.CustomS3UploadBucket
	m.Values["custom_s3_region"] = config.CustomS3Region
	m.Values["custom_azure_blob_container_name"] = config.CustomAzureUploadBlobContainerName
	m.Values["custom_azure_blob_url"] = config.CustomAzureBlobURL
	m.Values["custom_azure_tenant_id"] = config.CustomAzureTenantID
	m.Values["custom_azure_client_id"] = config.CustomAzureClientID
	m.Values["custom_azure_client_secret"] = config.CustomAzureClientSecret
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

	cldyMetric, err := json.Marshal(m)
	if err != nil {
		log.Errorf("An error occurred converting Cldy measure.  Error: %v", err)
	}

	err = os.WriteFile(exportFile, cldyMetric, 0644)
	if err != nil {
		log.Errorf("An error occurred creating a Cldy measure.  Error: %v", err)
	}

	return err
}

func extractNodeNameAndExtension(prefix, fileName string) (string, string) {
	extension := path.Ext(fileName)
	if strings.Contains(fileName, prefix) {
		name := fileName[len(prefix) : len(fileName)-len(extension)]
		return name, extension
	}
	return "", extension
}

func getPodLogs(ctx context.Context, clientset kubernetes.Interface,
	namespace, podName, containerName string, previous bool, dst io.Writer) (err error) {

	req := clientset.CoreV1().Pods(namespace).GetLogs(podName,
		&v1.PodLogOptions{
			Container: containerName,
			Previous:  previous,
		})

	readCloser, err := req.Stream(ctx)
	if err != nil {
		return err
	}

	defer util.SafeClose(readCloser.Close, &err)

	_, err = io.Copy(dst, readCloser)
	return err
}

func fetchDiagnostics(ctx context.Context, clientset kubernetes.Interface, namespace string,
	msExportDirectory *os.File) (err error) {

	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
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

				err = getPodLogs(ctx, clientset, namespace, pod.Name, c.Name, false, f)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil

}

// getBearerToken reads the service account token
func getBearerToken(bearerTokenPath string) (string, error) {
	token, err := os.ReadFile(bearerTokenPath)
	if err != nil {
		return "", errors.New("could not read bearer token from file")
	}
	return string(token), nil
}
