package controller_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/openmcp-project/usage-operator/internal/shared"
)

var (
	resourceReconcileTrigger chan event.TypedGenericEvent[*unstructured.Unstructured]
)

func TestComponentUtils(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Test Suite")
}

func resetSharedInformation() {
	shared.SharedInformation().Reset()

	shared.SharedInformation().SetInitialized()
	resourceReconcileTrigger = make(chan event.TypedGenericEvent[*unstructured.Unstructured], 1024)
	shared.SharedInformation().SetReconcileTrigger(resourceReconcileTrigger)
	shared.SharedInformation().SetStartInformerFunc(func(_ schema.GroupVersionKind) error { return nil })
}
