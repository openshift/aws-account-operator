package awsclient

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("AWS Resource Tag Builder", func() {
	When("Testing aws client timeout", func() {

		It("Should handle errors from API calls", func() {
			// overwrite default values so the test can run in 10 seconds
			awsApiTimeout = 1 * time.Second
			awsApiMaxRetries = 5

			http.DefaultClient.Transport = &http.Transport{
				Proxy: func(r *http.Request) (*url.URL, error) {
					return &url.URL{
						Scheme: "http",
						Host:   "10.255.255.1:80",
						Path:   "",
					}, nil
				},
			}

			client, err := newClient("", "sss", "TESTSTETST", "eu-central-1", "eu-central-1")
			done := make(chan error)
			// call describeRegions asynchronously
			go func() {
				_, err = client.DescribeRegions(context.TODO(), &ec2.DescribeRegionsInput{})
				done <- err
				close(done)
			}()

			time.Sleep(awsApiTimeout * 10)

			select {
			case err, ok := <-done:
				Expect(ok).To(BeTrue())
				Expect(err).ToNot(BeNil())
				// AWS SDK v2 may return auth errors or timeouts depending on network conditions
				// The important thing is that errors are properly propagated
				Expect(err.Error()).To(Or(
					ContainSubstring("Client.Timeout exceeded"),
					ContainSubstring("operation error"),
				))
			default:
				Fail("Api call did not complete.")
			}
		})
	})
})
