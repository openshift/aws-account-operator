package mock

import (
	"go.uber.org/mock/gomock"
	"github.com/openshift/aws-account-operator/pkg/awsclient"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Builder is an IBuilder implementation that knows how to produce a mocked AWS Client for
// testing purposes. To facilitate use of the mocks, this IBuilder's GetClient method always
// returns the same Client.
type Builder struct {
	MockController *gomock.Controller
	// cachedClient gives this singleton behavior.
	cachedClient awsclient.Client
}

// GetClient generates a mocked AWS client using the embedded MockController.
// The arguments are ignored, and the error is always nil.
// The returned client is a singleton for any given MockBuilder instance, so you can do e.g.
//    mp.GetClient(...).EXPECT()...
// and then when the code uses a client created via GetClient(), it'll be using the same client.
func (mp *Builder) GetClient(controllerName string, kubeClient client.Client, input awsclient.NewAwsClientInput) (awsclient.Client, error) {
	if mp.cachedClient == nil {
		mp.cachedClient = NewMockClient(mp.MockController)
	}
	return mp.cachedClient, nil
}

// GetMockClient is a convenience method to be called only from tests. It returns the (singleton)
// mocked AWS Client as a MockClient so it can be EXPECT()ed upon.
func GetMockClient(b awsclient.IBuilder) *MockClient {
	// Make sure this is only called from tests
	_ = b.(*Builder)
	// The arguments don't matter. This returns a Client
	c, _ := b.GetClient("", nil, awsclient.NewAwsClientInput{})
	// What we want is a MockClient
	return c.(*MockClient)
}
