/*

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PhDeploymentPhase string

const (
	DeploymentDeploying PhDeploymentPhase = "Deploying"
	DeploymentDeployed  PhDeploymentPhase = "Deployed"
	DeploymentStopped   PhDeploymentPhase = "Stopped"
	DeploymentFailed    PhDeploymentPhase = "Failed"
)

// PhDeploymentSpec defines the desired state of PhDeployment
type PhDeploymentSpec struct {
	DisplayName string `json:"displayName"`
	UserId      string `json:"userId"`
	UserName    string `json:"userName,omitempty"`
	GroupId     string `json:"groupId"`
	GroupName   string `json:"groupName"`
	Stop        bool   `json:"stop,omitempty"`
	Description string `json:"description,omitempty"`
}

type PhDeploymentMetadata map[string]string

type PhDeploymentPredictor struct {
	Name            string               `json:"name"`
	Replicas        int                  `json:"replicas"`
	ModelImage      string               `json:"modelImage"`
	InstanceType    string               `json:"instanceType"`
	ImagePullSecret string               `json:"imagePullSecret"`
	Metadata        PhDeploymentMetadata `json:"metadata"`
}

// PhDeploymentStatus defines the observed state of PhDeployment
type PhDeploymentStatus struct {
	Phase             PhDeploymentPhase     `json:"phase"`
	Messsage          PhDeploymentPhase     `json:"message"`
	Replicas          int                   `json:"replicas"`
	AvailableReplicas int                   `json:"availableReplicas"`
	Endpoint          string                `json:"endpoint"`
	History           []PhDeploymentHistory `json:"history"`
}

type PhDeploymentHistory struct {
	Time metav1.Time      `json:"time"`
	Spec PhDeploymentSpec `json:"spec"`
}

// +kubebuilder:object:root=true

// PhDeployment is the Schema for the phdeployments API
type PhDeployment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PhDeploymentSpec   `json:"spec,omitempty"`
	Status PhDeploymentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PhDeploymentList contains a list of PhDeployment
type PhDeploymentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PhDeployment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PhDeployment{}, &PhDeploymentList{})
}
