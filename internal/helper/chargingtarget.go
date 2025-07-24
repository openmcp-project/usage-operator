package helper

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	k8s "sigs.k8s.io/controller-runtime/pkg/client"

	mcpcorev1alpha1 "github.com/openmcp-project/mcp-operator/api/core/v1alpha1"
	pwcorev1alpha1 "github.com/openmcp-project/project-workspace-operator/api/core/v1alpha1"
)

const labelChargingTarget = "openmcp.cloud.sap/charging-target"
const labelChargingTargetType = "openmcp.cloud.sap/charging-target-type"

func ResolveChargingTarget(ctx context.Context, client k8s.Client, projectName string, workspaceName string, mcpName string) (string, string, error) {
	var project pwcorev1alpha1.Project
	var workspace pwcorev1alpha1.Workspace
	var mcp mcpcorev1alpha1.ManagedControlPlane

	err := client.Get(ctx, k8s.ObjectKey{
		Name: projectName,
	}, &project)
	if errors.IsNotFound(err) {
		return "", "", fmt.Errorf("cant find project %v: %w", projectName, err)
	} else if err != nil {
		return "", "", fmt.Errorf("error when getting project %v: %w", projectName, err)
	}

	err = client.Get(ctx, k8s.ObjectKey{
		Name:      workspaceName,
		Namespace: fmt.Sprintf("project-%s", projectName),
	}, &workspace)
	if errors.IsNotFound(err) {
		return "", "", fmt.Errorf("cant find workspace %v: %w", workspaceName, err)
	} else if err != nil {
		return "", "", fmt.Errorf("error when getting workspace %v: %w", workspaceName, err)
	}

	err = client.Get(ctx, k8s.ObjectKey{
		Name:      mcpName,
		Namespace: fmt.Sprintf("project-%s--ws-%s", projectName, workspaceName),
	}, &mcp)
	if errors.IsNotFound(err) {
		return "", "", fmt.Errorf("cant find mcp %v: %w", mcpName, err)
	} else if err != nil {
		return "", "", fmt.Errorf("error when getting mcp %v: %w", mcpName, err)
	}

	foundOne := false
	chargingTarget, ok := project.GetLabels()[labelChargingTarget]
	chargingTargetType := project.GetLabels()[labelChargingTargetType]
	if ok {
		foundOne = true
	}

	wsChargingTarget, ok := workspace.GetLabels()[labelChargingTarget]
	wsChargingTargetType := workspace.GetLabels()[labelChargingTargetType]
	if ok {
		foundOne = true
		chargingTarget = wsChargingTarget
		chargingTargetType = wsChargingTargetType
	}

	mcpChargingTarget, ok := mcp.GetLabels()[labelChargingTarget]
	mcpChargingTargetType := mcp.GetLabels()[labelChargingTargetType]
	if ok {
		foundOne = true
		chargingTarget = mcpChargingTarget
		chargingTargetType = mcpChargingTargetType
	}

	if !foundOne {
		return "", "", fmt.Errorf("can't find any charging target for project(%s) workspace(%s) mcp(%s)", projectName, workspaceName, mcpName)
	}

	return chargingTarget, chargingTargetType, nil
}
