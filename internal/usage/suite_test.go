package usage

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	ctx    context.Context
	cancel context.CancelFunc
)

func TestUsageModule(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Usage Module")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

})

var _ = AfterSuite(func() {
	cancel()
})
