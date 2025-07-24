/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// MCPUsageSpec defines the desired state of MCPUsage.
type MCPUsageSpec struct {
	ChargingTarget    string       `json:"charging_target"`
	Project           string       `json:"project"`
	Workspace         string       `json:"workspace"`
	MCP               string       `json:"mcp"`
	Usage             []DailyUsage `json:"daily_usage,omitempty"`
	LastUsageCaptured metav1.Time  `json:"last_usage_captured,omitempty"`
	MCPCreatedAt      metav1.Time  `json:"mcp_created_at,omitempty"`
	MCPDeletedAt      metav1.Time  `json:"mcp_deleted_at,omitempty"`

	Message string `json:"message,omitempty"`
}

// MCPUsageStatus defines the observed state of MCPUsage.
type MCPUsageStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	DailyUsageReport []DailyUsageReport `json:"daily_usage_report"`
}

type DailyUsageReport struct {
	Date    metav1.Time `json:"date"`
	Status  string      `json:"status,omitempty"`
	Message string      `json:"message,omitempty"`
}

type DailyUsage struct {
	Date  metav1.Time     `json:"date"`
	Usage metav1.Duration `json:"usage"`
}

func NewDailyUsage(date time.Time, hours int) (DailyUsage, error) {
	duration, err := time.ParseDuration(fmt.Sprintf("%vh", hours))
	if err != nil {
		return DailyUsage{}, err
	}

	return DailyUsage{
		Date: metav1.NewTime(date),
		Usage: metav1.Duration{
			Duration: duration,
		},
	}, nil
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=mcpu
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=onboarding"
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.spec.project`
// +kubebuilder:printcolumn:name="Workspace",type=string,JSONPath=`.spec.workspace`
// +kubebuilder:printcolumn:name="MCP",type=string,JSONPath=`.spec.mcp`

// MCPUsage is the Schema for the mcpdailies API.
type MCPUsage struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MCPUsageSpec   `json:"spec,omitempty"`
	Status MCPUsageStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MCPUsageList contains a list of MCPUsage.
type MCPUsageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPUsage `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MCPUsage{}, &MCPUsageList{})
}
