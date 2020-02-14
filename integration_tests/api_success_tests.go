package integration_tests

import (
	"context"
	"log"

	. "github.com/onsi/ginkgo"
)

var _ = Describe("LONG TEST: Deploying API ServiceBridge", func() {
	logger := log.New(GinkgoWriter, "INFO: ", log.Lshortfile)

	ctx := context.Background()

	_ = logger
	_ = ctx

	Context("With API Service deployed", func() {
		It("TODO...", func() {

		})
	})

})
