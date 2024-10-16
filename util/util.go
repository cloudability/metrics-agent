package util

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/spf13/viper"
	"k8s.io/client-go/rest"
)

// ErrEmptyDataDir error to indicate the data directory is empty
var ErrEmptyDataDir = errors.New("empty data directory")

// IsValidURL returns true if string is a valid URL
func IsValidURL(toTest string) bool {
	_, err := url.ParseRequestURI(toTest)
	return err == nil
}

// TestHTTPConnection takes
// a given client / URL(string) / bearerToken(string)/ retries count (int)
// and returns true if response code is 2xx.
func TestHTTPConnection(testClient rest.HTTPClient,
	URL, method, bearerToken string, retries uint, verbose bool) (successful bool, body *[]byte, err error) {
	IsValidURL(URL)
	attempts := retries + 1

	req, err := http.NewRequest(method, URL, nil)
	if err != nil {
		log.Fatalf("Unable to make new request: %v", err)
	}

	if bearerToken != "" {
		req.Header.Add("Authorization", "Bearer "+bearerToken)
	}
	for i := uint(0); i < attempts; i++ {
		resp, err := testClient.Do(req)
		if err != nil {
			if verbose {
				log.Warnf("Unable to connect to URL: %s retrying: %v", URL, i+1)
			}
			time.Sleep(time.Duration(int64(math.Pow(2, float64(i)))) * time.Second)
			continue
		}
		defer SafeClose(resp.Body.Close, &err)
		body, rerr := io.ReadAll(resp.Body)
		if rerr != nil {
			err = fmt.Errorf("Unable to read response from: %s", URL)
		}

		return resp.StatusCode <= 200, &body, err

	}

	return false, &[]byte{}, err

}

// CheckRequiredSettings checks for required min values / flags / environment variables
func CheckRequiredSettings(requiredArgs []string) error {

	for _, a := range requiredArgs {
		if viper.GetString(a) != "" {
			continue
		}
		return fmt.Errorf("Required flag: %v or environment variable: CLOUDABILITY_"+strings.ToUpper(
			a)+" has not been set", a)

	}

	if viper.IsSet("poll_interval") && viper.GetInt("poll_interval") < 5 {
		return fmt.Errorf(
			"Polling interval must be 5 seconds or greater")
	}

	return nil
}

// CreateMetricSample creates a metric sample from a given directory removing the source directory if cleanup is true
func CreateMetricSample(exportDirectory os.File, uid string, cleanUp bool, scratchDir string) (*os.File, error) {

	ed, err := exportDirectory.Stat()
	if err != nil || !ed.IsDir() {
		log.Errorf("Unable to stat sample directory: %v", err)
		return nil, err
	}

	if err := CheckIfDirEmpty(exportDirectory.Name()); err != nil {
		return nil, err
	}

	sampleFilename := getExportFilename(uid)
	destFile, err := os.Create(scratchDir + "/" + sampleFilename + ".tgz")

	if err != nil {
		log.Errorf("Unable to create metric sample file: %v", err)
		return nil, err
	}

	err = createTGZ(exportDirectory, destFile)

	if err != nil {
		log.Errorf("Unable to tar metric sample directory: %v", err)
		return nil, err
	}

	// cleanup directory after creating the sample
	if cleanUp {
		err = removeDirectoryContents(exportDirectory.Name() + "/")
	}

	if err != nil {
		log.Errorf("Unable to cleanup metric sample directory: %v", err)
		return nil, err
	}

	return destFile, err
}

// createTGZ takes a source and variable writers and walks 'source' writing each file
// found to the tar writer; the purpose for accepting multiple writers is to allow
// for multiple outputs
func createTGZ(src os.File, writers ...io.Writer) (rerr error) {

	// ensure the src actually exists before trying to tar it
	if _, err := os.Stat(src.Name()); err != nil {
		return fmt.Errorf("Unable to tar files - %v", err.Error())
	}

	mw := io.MultiWriter(writers...)

	//nolint gas
	gzw, _ := gzip.NewWriterLevel(mw, 9)

	defer SafeClose(gzw.Close, &rerr)

	tw := tar.NewWriter(gzw)

	defer func() {
		err := tw.Close()
		if err != nil {
			log.Fatal(err)
		}
	}()

	// walk path
	return filepath.Walk(src.Name(), func(file string, fileInfo os.FileInfo, err error) (rerr error) {

		// return on any error
		if err != nil {
			return err
		}

		// create a new dir/file header
		header, err := tar.FileInfoHeader(fileInfo, fileInfo.Name())
		if err != nil {
			return err
		}

		// return on directories since there will be no content to tar
		if fileInfo.Mode().IsDir() {
			return nil
		}

		// if not a directory update the name to correctly reflect the desired destination when untaring
		if !fileInfo.Mode().IsDir() {
			header.Name = filepath.Join(filepath.Base(src.Name()), strings.TrimPrefix(file, src.Name()))
		}
		// write the header
		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		// open files for taring
		//nolint gosec
		f, err := os.Open(file)
		if err != nil {
			return err
		}

		defer SafeClose(f.Close, &rerr)

		// copy file data into tar writer
		if _, err := io.Copy(tw, f); err != nil {
			return err
		}

		return err
	})
}

func getExportFilename(uid string) string {
	t := time.Now().UTC()
	return uid + "_" + t.Format("20060102150405")
}

// CreateMSWorkingDirectory takes a given prefix and returns a metric sample working directory
func CreateMSWorkingDirectory(uid string, scratchDir string) (*os.File, error) {
	// create metric sample directory
	td, err := os.MkdirTemp(scratchDir, "cldy-metrics")
	if err != nil {
		log.Errorf("Unable to create temporary directory: %v", err)
		return nil, err
	}

	t := time.Now().UTC()

	ed := td + "/" + uid + "_" + t.Format("20060102150405")

	err = os.MkdirAll(ed, os.ModePerm)
	if err != nil {
		log.Errorf("Error creating metric sample export directory : %v", err)
	}
	//nolint gosec
	exportDir, err := os.Open(ed)
	if err != nil {
		log.Fatalln("Unable to open metric sample export directory")
	}

	return exportDir, err
}

func removeDirectoryContents(dir string) (err error) {
	//nolint gosec
	d, err := os.Open(dir)
	if err != nil {
		return err
	}

	defer SafeClose(d.Close, &err)

	names, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, name := range names {
		err = os.RemoveAll(filepath.Join(dir, name))
		if err != nil {
			return err
		}
	}
	return nil
}

// CopyFileContents copies the contents of the file named src to the file named
// by dst. The file will be created if it does not already exist. If the
// destination file exists, all it's contents will be replaced by the contents
// of the source file.
func CopyFileContents(dst, src string) (rerr error) {
	//nolint gosec
	in, err := os.Open(src)
	if err != nil {
		return err
	}

	defer SafeClose(in.Close, &rerr)

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer SafeClose(out.Close, &rerr)

	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// SafeClose will close the given closer function, setting the err ONLY if it is currently nil. This
// allows for cleaner handling of always-closing, but retaining the original error (ie from a previous
// Write).
func SafeClose(closer func() error, err *error) {
	if closeErr := closer(); closeErr != nil && *err == nil {
		(*err) = closeErr
	}
}

// SafeCloseDeferLogger takes a file and runs the deferred closing of both. Used for custom agent uploads
func SafeCloseDeferLogger(file *os.File) {
	defer func(name string) {
		err := os.Remove(name)
		if err != nil {
			log.Warnf("Warning: Unable to cleanup after metric sample upload: %v", err)
		}
	}(file.Name())

	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			log.Warnf("Warning: Unable to close metric sample: %v", err)
		}
	}(file)
}

// MatchOneFile returns the name of one file based on a given directory and pattern
// returning an error if more or less than one match is found. The syntax of patterns is the same
// as in filepath.Glob & Match.
func MatchOneFile(directory string, pattern string) (fileName string, err error) {
	results, err := filepath.Glob(directory + pattern)
	if err != nil {
		return "", fmt.Errorf("Error encountered reading directory: %v", err)
	}

	if len(results) == 1 {
		return results[0], nil
	} else if len(results) > 1 {
		return "", fmt.Errorf("More than one file matched the pattern: %+v", results)
	}

	return "", fmt.Errorf("No matches found")
}

// SetupLogger sets configuration for the default logger
func SetupLogger() (err error) {

	var (
		ll = viper.GetString("log_level")
		lf = strings.ToLower(viper.GetString("log_format"))
	)

	// Set log level
	l, err := log.ParseLevel(ll)
	if err != nil {
		return fmt.Errorf("Invalid log level: %v", ll)
	}
	log.SetLevel(l)
	log.Debugf("Log level set to: %v", l.String())

	// Set log format
	switch lf {
	case "json":
		log.SetFormatter(&log.JSONFormatter{})
	default:
		log.SetFormatter(&log.TextFormatter{
			DisableLevelTruncation: true,
			PadLevelText:           true,
		})
	}
	return nil
}

// ValidateScratchDir validates whether or not the scratch directory exists or not
func ValidateScratchDir(scratchDir string) error {
	if _, err := os.Stat(scratchDir); os.IsNotExist(err) {
		return fmt.Errorf("There was a problem validating provided scratch directory: %v", err)
	}

	return nil
}

// CheckIfDirEmpty checks if a directory is empty, returning an ErrEmptyDataDir error if it is
func CheckIfDirEmpty(dirname string) (rerr error) {
	dir, err := os.Open(dirname)
	if err != nil {
		return err
	}

	defer SafeClose(dir.Close, &rerr)

	_, err = dir.Readdir(1)
	if err != nil {
		switch err {
		case io.EOF:
			return ErrEmptyDataDir
		default:
			return err
		}
	}

	return nil
}
