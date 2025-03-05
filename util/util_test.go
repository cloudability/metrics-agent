package util

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	_ "strconv"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type testConfig struct {
	APIKey              string
	HeapsterURL         string
	KubeStateMetricsURL string
	PollInterval        int
	UseInClusterConfig  bool
	ClusterName         string
}

func TestIsValidURL(t *testing.T) {

	t.Parallel()

	t.Run("ensure that an invalid URL returns false ", func(t *testing.T) {
		URL := "sbn//bad-url"
		URLTest := IsValidURL(URL)
		if URLTest {
			t.Errorf("Invaild URL not detected: %v", URL)
		}
	})

	t.Run("ensure that an valid URL returns true ", func(t *testing.T) {
		URL := "https://verynicesite.com/index.html?option=1"
		URLTest := IsValidURL(URL)
		if !URLTest {
			t.Errorf("Vaild URL not detected: %v", URL)
		}
	})

}

func TestTestHTTPConnection(t *testing.T) {

	testClient := &http.Client{}

	t.Run("ensure that a 200 HTTP response returns true", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Error("Expected to be a GET")
			}
			w.WriteHeader(200)
		}))
		defer ts.Close()

		b, _, _ := TestHTTPConnection(testClient, ts.URL, http.MethodGet, "", 10, true)
		log.Print(strconv.FormatBool(b))
		if !b {
			t.Error("invalid connection")
		}
	})

	t.Run("ensure that a non 200 HTTP response returns false", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Error("Expected to be a GET")
			}
			w.WriteHeader(500)
		}))
		defer ts.Close()

		b, _, err := TestHTTPConnection(testClient, ts.URL, http.MethodGet, "", 10, true)
		log.Print(strconv.FormatBool(b))
		if b {
			t.Errorf("Non 200 should return false : %v", err)
		}
	})

}

func TestCheckRequiredSettings(t *testing.T) {

	t.Parallel()

	var config testConfig
	var kubernetesCmd = &cobra.Command{
		Use:   "kubernetes",
		Short: "Collect Kubernetes Metrics",
		Long:  `Command to collect Kubernetes Metrics`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return CheckRequiredSettings([]string{"api_key"})
		},
		Run: func(cmd *cobra.Command, args []string) {},
	}

	// add cobra and viper ENVs and flags
	kubernetesCmd.PersistentFlags().StringVar(
		&config.APIKey,
		"api_key",
		"",
		"Cloudability API Key",
	)
	kubernetesCmd.PersistentFlags().IntVar(
		&config.PollInterval,
		"poll_interval",
		600,
		"Time, in seconds to poll the services infrastructure. Default: 600",
	)
	kubernetesCmd.PersistentFlags().StringVar(
		&config.ClusterName,
		"cluster_name",
		"default-test",
		"Kubernetes Cluster Name - required this must be unique to every cluster.",
	)

	_ = viper.BindPFlag("api_key", kubernetesCmd.PersistentFlags().Lookup("api_key"))
	_ = viper.BindPFlag("poll_interval", kubernetesCmd.PersistentFlags().Lookup("poll_interval"))
	_ = viper.BindPFlag("cluster_name", kubernetesCmd.PersistentFlags().Lookup("cluster_name"))

	// nolint dupl
	t.Run("ensure that required settings are set as cmd flags", func(t *testing.T) {

		args := []string{"kubernetes", "--poll_interval", "5", "--api_key", "8675309-9035768", "--cluster_name", "specificTest"}
		kubernetesCmd.SetArgs(args)

		if err := kubernetesCmd.Execute(); err != nil {
			t.Errorf("required settings set via cmd flag but not detected: %v", err)
		}
	})

	// nolint dupl
	t.Run("ensure that missing required cmd flags is detected", func(t *testing.T) {

		args := []string{"kubernetes", "--poll_interval", "5", "--api_key", "8675309-9035768", "--cluster_name", "specificTest"}
		kubernetesCmd.SetArgs(args)

		if err := kubernetesCmd.Execute(); err != nil {
			t.Errorf("required setting set via cmd flag is missing but not detected: %v", err)
		}
	})

	t.Run("ensure that required settings are set as environment variables", func(t *testing.T) {

		viper.SetEnvPrefix("cloudability")
		viper.AutomaticEnv()

		_ = os.Setenv("CLOUDABILITY_API_KEY", "8675309-9035768")
		_ = os.Setenv("CLOUDABILITY_POLL_INTERVAL", "5")
		_ = os.Setenv("CLOUDABILITY_CLUSTER_NAME", "test")

		if err := kubernetesCmd.Execute(); err != nil {
			t.Errorf("required settings set via environment variables but not detected: %v", err)
		}
	})

	// nolint dupl
	t.Run("ensure that missing required environment variable is detected", func(t *testing.T) {

		viper.SetEnvPrefix("cloudability")
		viper.AutomaticEnv()

		envArgs := []string{"kubernetes"}
		kubernetesCmd.SetArgs(envArgs)

		_ = os.Setenv("CLOUDABILITY_API_KEY", "8675309-9035768")
		_ = os.Setenv("CLOUDABILITY_POLL_INTERVAL", "5")
		_ = os.Setenv("CLOUDABILITY_CLUSTER_NAME", "test")

		if err := kubernetesCmd.Execute(); err != nil {
			t.Errorf("incorrect settings via environment variables but condition not detected: %v", err)
		}
	})

	// nolint dupl
	t.Run("ensure that invalid min value is detected", func(t *testing.T) {

		viper.SetEnvPrefix("cloudability")
		viper.AutomaticEnv()

		envArgs := []string{"kubernetes"}
		kubernetesCmd.SetArgs(envArgs)
		_ = os.Setenv("CLOUDABILITY_API_KEY", "8675309-9035768")
		_ = os.Setenv("CLOUDABILITY_POLL_INTERVAL", "4")
		_ = os.Setenv("CLOUDABILITY_CLUSTER_NAME", "test")

		if err := kubernetesCmd.Execute(); err != nil {
			t.Errorf("incorrect poll interval set via environment variables but not detected: %v", err)
		}
	})

	t.Run("ensure that invalid string value is detected", func(t *testing.T) {

		viper.SetEnvPrefix("cloudability")
		viper.AutomaticEnv()

		envArgs := []string{"kubernetes"}
		kubernetesCmd.SetArgs(envArgs)
		_ = os.Setenv("CLOUDABILITY_API_KEY", "8675309-9035768")
		_ = os.Setenv("CLOUDABILITY_POLL_INTERVAL", "5")
		_ = os.Setenv("CLOUDABILITY_CLUSTER_NAME", " ")

		if err := kubernetesCmd.Execute(); err != nil {
			t.Errorf("incorrect cluster name set via environment variables but condition not detected: %v", err)
		}
	})
}

func TestCreateMetricSample(t *testing.T) {
	var err error
	var tgz *os.File
	var sampleDirectory *os.File

	testDataDirectory := "testdata/test-cluster-metrics-sample"

	t.Run("Ensure that a metric sample is created", func(t *testing.T) {

		if _, err = os.Stat(testDataDirectory); err == nil {
			sampleDirectory, err = os.Open(testDataDirectory)
			ms, err := CreateMetricSample(*sampleDirectory, "cluster-id", false, os.TempDir())
			if err != nil {
				t.Errorf("Error creating agent Status Metric: %v", err)
			}

			tgz, err = os.Open(ms.Name())
			if err != nil {
				t.Error("unable to open gzip'ed file. ")
			}
			defer tgz.Close()

			// clean up
			_ = os.Remove("/tmp/" + filepath.Base(testDataDirectory) + ".tgz")

		} else {
			t.Error("Unable find data directory")
		}

	})

	t.Run("Only create metric sample if data directory contains files", func(t *testing.T) {
		emptySampleDirectory, err := os.MkdirTemp(os.TempDir(), "empty_sample_directory")
		if err != nil {
			t.Errorf("error creating temporary sample directory: %v", err)
		}
		defer func() {
			_ = os.Remove(emptySampleDirectory)
		}()

		sampleDirectory, err := os.Open(emptySampleDirectory)
		if err != nil {
			t.Errorf("error opening temporary sample directory as file: %v", err)
		}

		// First we expect no data
		_, err = CreateMetricSample(*sampleDirectory, "cluster-id", false, os.TempDir())
		if err != ErrEmptyDataDir {
			t.Errorf("expected an ErrEmptyDataDir error but got: %v", err)
		}

		// Add a file
		fp, err := os.Create(fmt.Sprintf("%s/sample_file.txt", sampleDirectory.Name()))
		if err != nil {
			t.Errorf("unable to create file: %v", err)
		}
		_, _ = fp.WriteString("test")
		_ = fp.Close()

		// Then we expect data
		_, err = CreateMetricSample(*sampleDirectory, "cluster-id", false, os.TempDir())
		if err != nil {
			t.Errorf("unexpected error but got: %v", err)
		}
	})
}

func TestMatchOneFile(t *testing.T) {
	dir := os.TempDir() + "/cldy-test" + strconv.FormatInt(
		time.Now().Unix(), 10)
	_ = os.MkdirAll(dir, 0777)
	_ = os.WriteFile(dir+"/shouldBeHere.file", []byte(nil), 0644)

	t.Run("Ensure that one file is matched", func(t *testing.T) {

		pattern := "/shouldBeHere.file*"
		file, err := MatchOneFile(dir, pattern)
		if err != nil || filepath.Base(file) != "shouldBeHere.file" {
			t.Errorf("Did not match pattern when looking in the directory: %s for the pattern: %s error: %v",
				dir, pattern, err)
		}

	})

	t.Run("Ensure that more than one file returns an error", func(t *testing.T) {

		_ = os.WriteFile(dir+"/shouldBeHere.file2", []byte(nil), 0644)
		pattern := "/shouldBeHere.file*"
		file, err := MatchOneFile(dir, pattern)
		if err == nil || file != "" {
			t.Errorf("Should have raised an error when looking in the directory: %s for pattern: %s error: %v",
				dir, pattern, err)
		}

	})

	t.Run("Ensure that zero matches return an error", func(t *testing.T) {
		pattern := "/shouldNOtBeHere" + strconv.Itoa(time.Now().Nanosecond()) + "*"
		file, err := MatchOneFile(dir, pattern)
		if err == nil || file != "" {
			t.Errorf("Should have raised an error when looking in the directory: %s for a non-matching pattern: %s error: %v",
				dir, pattern, err)
		}

	})

	// clean up
	_ = os.RemoveAll(dir)

}

func TestValidateScratchDir(t *testing.T) {
	t.Run("Ensure that an error is returned when directory doesn't exist", func(t *testing.T) {
		fakeDir := "/fake_dir"
		err := ValidateScratchDir(fakeDir)

		if err == nil {
			t.Errorf("Should have raised an error when validating scratch directory that does not exist, error: %v", err)
		}
	})

	t.Run("Ensure that no error is returned when it is directory that does exist", func(t *testing.T) {
		scratchDir := "/tmp"
		err := ValidateScratchDir(scratchDir)

		if err != nil {
			t.Errorf("Should not have raised an error when validating scratch directory that does exist, error: %v", err)
		}
	})
}
