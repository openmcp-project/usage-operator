package helper

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	openmcpconstv1alpha1 "github.com/openmcp-project/openmcp-operator/api/constants"
	"github.com/openmcp-project/openmcp-operator/lib/clusteraccess"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/openmcp-project/usage-operator/api/install"
	usagev1alpha1 "github.com/openmcp-project/usage-operator/api/v1alpha1"
)

func GetOnboardingCluster(ctx context.Context, log logging.Logger, client client.Client, providerName string) (*clusters.Cluster, error) {
	onboardingScheme := install.InstallOperatorAPIsOnboarding(install.InstallCRDAPIs(runtime.NewScheme()))

	providerSystemNamespace := os.Getenv(openmcpconstv1alpha1.EnvVariablePodNamespace)
	if providerSystemNamespace == "" {
		log.Error(nil, fmt.Sprintf("environment variable %s is not set", openmcpconstv1alpha1.EnvVariablePodNamespace))
		return nil, fmt.Errorf("environment variable %s is not set", openmcpconstv1alpha1.EnvVariablePodNamespace)
	}

	clusterAccessManager := clusteraccess.NewClusterAccessManager(client, providerName, providerSystemNamespace).
		WithLogger(&log).
		WithInterval(10 * time.Second).
		WithTimeout(30 * time.Minute)

	onboardingCluster, err := clusterAccessManager.CreateAndWaitForCluster(ctx, "onboarding", clustersv1alpha1.PURPOSE_ONBOARDING,
		onboardingScheme, []clustersv1alpha1.PermissionsRequest{
			{
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{"*"},
						Resources: []string{"*"},
						Verbs:     []string{"get", "list", "watch"},
					},
					{
						APIGroups: []string{""},
						Resources: []string{"namespaces"},
						Verbs:     []string{"get", "list", "watch"},
					},
					{
						APIGroups:     []string{"apiextensions.k8s.io"},
						Resources:     []string{"customresourcedefinitions"},
						Verbs:         []string{"get", "patch", "update", "delete"},
						ResourceNames: []string{"resourceusages." + usagev1alpha1.GroupVersion.Group},
					},
					{
						APIGroups: []string{"apiextensions.k8s.io"},
						Resources: []string{"customresourcedefinitions"},
						Verbs:     []string{"create"},
					},
					{
						APIGroups: []string{usagev1alpha1.GroupVersion.Group},
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
