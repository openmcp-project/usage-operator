package helper

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	openmcpconstv1alpha1 "github.com/openmcp-project/openmcp-operator/api/constants"
	"github.com/openmcp-project/openmcp-operator/lib/clusteraccess"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/openmcp-project/usage-operator/api"
	usagev1 "github.com/openmcp-project/usage-operator/api/usage/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	"k8s.io/apimachinery/pkg/runtime"
)

func GetOnboardingCluster(ctx context.Context, log logging.Logger, client client.Client) (*clusters.Cluster, error) {
	onboardingScheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(onboardingScheme))
	utilruntime.Must(usagev1.AddToScheme(onboardingScheme))
	utilruntime.Must(clustersv1alpha1.AddToScheme(onboardingScheme))

	providerSystemNamespace := os.Getenv(openmcpconstv1alpha1.EnvVariablePlatformClusterNamespace)
	if providerSystemNamespace == "" {
		log.Error(nil, fmt.Sprintf("environment variable %s is not set", openmcpconstv1alpha1.EnvVariablePlatformClusterNamespace))
		return nil, fmt.Errorf("environment variable %s is not set", openmcpconstv1alpha1.EnvVariablePlatformClusterNamespace)
	}

	clusterAccessManager := clusteraccess.NewClusterAccessManager(client, api.UsageOperatorPlatformServiceName, providerSystemNamespace).
		WithLogger(&log).
		WithInterval(10 * time.Second).
		WithTimeout(30 * time.Minute)

	// TODO: Put the correct policies in there
	onboardingCluster, err := clusterAccessManager.CreateAndWaitForCluster(ctx, "onboarding", clustersv1alpha1.PURPOSE_ONBOARDING,
		onboardingScheme, []clustersv1alpha1.PermissionsRequest{
			{
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{"core.openmcp.cloud"},
						Resources: []string{"managedcontrolplanes", "managedcontrolplanes/status"},
						Verbs:     []string{"get", "list"},
					},
					{
						APIGroups: []string{"apiextensions.k8s.io"},
						Resources: []string{"customresourcedefinitions"},
						Verbs:     []string{"create", "delete"},
					},
					{
						APIGroups: []string{"usage.openmcp.cloud"},
						Resources: []string{"*"},
						Verbs:     []string{"*"},
					},
				},
			},
		},
	)
	if err != nil {
		log.Error(err, "error creating/updating onboarding cluster")
		return nil, fmt.Errorf("error creating/updating onboarding cluster: %w", err)
	}

	return onboardingCluster, nil
}
