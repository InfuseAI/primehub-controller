package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PhDeploymentPhase string

const (
	DeploymentDeploying PhDeploymentPhase = "Deploying"
	DeploymentDeployed  PhDeploymentPhase = "Deployed"
	DeploymentStopped   PhDeploymentPhase = "Stopped"
	DeploymentStopping  PhDeploymentPhase = "Stopping"
	DeploymentFailed    PhDeploymentPhase = "Failed"
)

const (
	DeploymentPrivateEndpoint string = "private"
	DeploymentPublicEndpoint  string = "public"
)

// PhDeploymentSpec defines the desired state of PhDeployment
type PhDeploymentSpec struct {
	DisplayName   string                  `json:"displayName"`
	UserId        string                  `json:"userId"`
	UserName      string                  `json:"userName"`
	GroupId       string                  `json:"groupId"`
	GroupName     string                  `json:"groupName"`
	Stop          bool                    `json:"stop,omitempty"`
	Description   string                  `json:"description,omitempty"`
	UpdateMessage string                  `json:"updateMessage,omitempty"`
	Predictors    []PhDeploymentPredictor `json:"predictors"`
	Endpoint      PhDeploymentEndpoint    `json:"endpoint,omitempty"`
	Env           []corev1.EnvVar         `json:"env,omitempty"`
}

type PhDeploymentMetadata map[string]string

type PhDeploymentPredictor struct {
	Name            string               `json:"name"`
	Replicas        int                  `json:"replicas"`
	ModelImage      string               `json:"modelImage"`
	InstanceType    string               `json:"instanceType"`
	ModelURI        string               `json:"modelURI,omitempty"`
	ImagePullSecret string               `json:"imagePullSecret,omitempty"`
	Metadata        PhDeploymentMetadata `json:"metadata,omitempty"`
}

type PhDeploymentEndpoint struct {
	AccessType string                       `json:"accessType"`
	Clients    []PhDeploymentEndpointClient `json:"clients,omitempty"`
}

type PhDeploymentEndpointClient struct {
	Name  string `json:"name"`
	Token string `json:"token"`
}

// PhDeploymentStatus defines the observed state of PhDeployment
type PhDeploymentStatus struct {
	Phase             PhDeploymentPhase     `json:"phase"`
	Messsage          string                `json:"message,omitempty"`
	Replicas          int                   `json:"replicas,omitempty"`
	AvailableReplicas int                   `json:"availableReplicas,omitempty"`
	Endpoint          string                `json:"endpoint,omitempty"`
	History           []PhDeploymentHistory `json:"history,omitempty"`
}

type PhDeploymentHistory struct {
	Time metav1.Time      `json:"time"`
	Spec PhDeploymentSpec `json:"spec"`
}

// +kubebuilder:subresource:status
// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".status.replicas"
// +kubebuilder:printcolumn:name="Available",type="integer",JSONPath=".status.availableReplicas"

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
