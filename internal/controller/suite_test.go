package controller_test

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	testutils "github.com/openmcp-project/controller-utils/pkg/testing"

	"github.com/openmcp-project/usage-operator/api/install"
	usagev1alpha1 "github.com/openmcp-project/usage-operator/api/v1alpha1"
	"github.com/openmcp-project/usage-operator/internal/controller"
	"github.com/openmcp-project/usage-operator/internal/shared"
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

const (
	platform     = "platform"
	onboarding   = "onboarding"
	cfgRec       = "config"
	nsRec        = "namespace"
	providerName = "usage"
)

var (
	resourceReconcileTrigger chan event.TypedGenericEvent[*unstructured.Unstructured]

	secretGVK     = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}
	configMapGVK  = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	deploymentGVK = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
)

func defaultTestSetup(testDirPathSegments ...string) *testutils.ComplexEnvironment {
	platformDirExists := true
	_, err := os.Stat(filepath.Join(append(testDirPathSegments, platform)...))
	Expect(err).To(Or(Not(HaveOccurred()), MatchError(os.IsNotExist, "IsNotExist")))
	if err != nil {
		platformDirExists = false
	}
	onboardingDirExists := true
	_, err = os.Stat(filepath.Join(append(testDirPathSegments, onboarding)...))
	Expect(err).To(Or(Not(HaveOccurred()), MatchError(os.IsNotExist, "IsNotExist")))
	if err != nil {
		onboardingDirExists = false
	}
	envB := testutils.NewComplexEnvironmentBuilder().
		WithFakeClient(platform, install.InstallOperatorAPIsPlatform(runtime.NewScheme())).
		WithFakeClient(onboarding, install.InstallOperatorAPIsOnboarding(runtime.NewScheme())).
		WithFakeClientBuilderCall(onboarding, "WithIndex", &usagev1alpha1.ResourceUsage{}, "spec.resource.kind", func(obj client.Object) []string {
			ru := obj.(*usagev1alpha1.ResourceUsage)
			return []string{ru.Spec.Resource.Kind}
		}).
		WithFakeClientBuilderCall(onboarding, "WithIndex", &usagev1alpha1.ResourceUsage{}, "spec.resource.version", func(obj client.Object) []string {
			ru := obj.(*usagev1alpha1.ResourceUsage)
			return []string{ru.Spec.Resource.Version}
		}).
		WithFakeClientBuilderCall(onboarding, "WithIndex", &usagev1alpha1.ResourceUsage{}, "spec.resource.group", func(obj client.Object) []string {
			ru := obj.(*usagev1alpha1.ResourceUsage)
			return []string{ru.Spec.Resource.Group}
		}).
		WithFakeClientBuilderCall(onboarding, "WithIndex", &usagev1alpha1.ResourceUsage{}, "spec.resource.name", func(obj client.Object) []string {
			ru := obj.(*usagev1alpha1.ResourceUsage)
			return []string{ru.Spec.Resource.Name}
		}).
		WithFakeClientBuilderCall(onboarding, "WithIndex", &usagev1alpha1.ResourceUsage{}, "spec.resource.namespace", func(obj client.Object) []string {
			ru := obj.(*usagev1alpha1.ResourceUsage)
			return []string{ru.Spec.Resource.Namespace}
		}).
		WithFakeClientBuilderCall(onboarding, "WithIndex", &usagev1alpha1.ResourceUsage{}, "status.phase", func(obj client.Object) []string {
			ru := obj.(*usagev1alpha1.ResourceUsage)
			return []string{ru.Status.Phase}
		}).
		WithReconcilerConstructor(cfgRec, func(clients ...client.Client) reconcile.Reconciler {
			return controller.NewConfigController(clusters.NewTestClusterFromClient(platform, clients[0]), clusters.NewTestClusterFromClient(onboarding, clients[1]), providerName, nil)
		}, platform, onboarding).
		WithReconcilerConstructor(nsRec, func(clients ...client.Client) reconcile.Reconciler {
			return controller.NewNamespaceController(clusters.NewTestClusterFromClient(onboarding, clients[0]))
		}, onboarding)
	if platformDirExists {
		envB.WithInitObjectPath(platform, append(testDirPathSegments, platform)...)
	}
	if onboardingDirExists {
		envB.WithInitObjectPath(onboarding, append(testDirPathSegments, onboarding)...)
	}
	env := envB.Build()
	return env
}
