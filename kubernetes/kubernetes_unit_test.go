package kubernetes

import (
	"fmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"testing"
)

func TestKubernetes(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Kubernetes Unit Tests")
}

var _ = Describe("Kubernetes", func() {

	Describe("error validation", func() {
		It("should return an error if metrics-agent receives a 500 error getting upload URI", func() {
			errorStr := handleError(fmt.Errorf("Error retrieving upload URI: 500"))
			Expect(errorStr).To(Equal(fmt.Sprintf(transportError, apiEndpoint)))
		})

		It("should return an error if metrics-agent receives a 403 error getting upload URI", func() {
			errorStr := handleError(fmt.Errorf(forbiddenError))
			Expect(errorStr).To(Equal(fmt.Sprintf(apiKeyError, kbProvisionURL)))
		})

		It("should not return an error if the metrics-agent receives any other error", func() {
			errorStr := handleError(fmt.Errorf("test error"))
			Expect(errorStr).To(Equal(""))
		})

		It("Node source error handler should return an error if we need to verify RBAC roles", func() {
			errorStr := handleNodeSourceError(fmt.Errorf("Please verify RBAC roles"))
			Expect(errorStr).To(ContainSubstring("RBAC role in the Cloudability namespace may need to be updated."))
		})
	})
})
