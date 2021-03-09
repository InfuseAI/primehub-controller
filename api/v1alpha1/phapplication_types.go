package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// PhApplication Phase
const (
	ApplicationStarting string = "Starting"
	ApplicationReady    string = "Ready"
	ApplicationUpdating string = "Updating"
	ApplicationStopping string = "Stopping"
	ApplicationStopped  string = "Stopped"
	ApplicationError    string = "Error"
)

// PhApplication Scope
const (
	ApplicationPublicScope   string = "public"
	ApplicationPrimehubScope string = "primehub"
	ApplicationGroupScope    string = "group"
)

// PhApplicationSpec defines the desired state of PhApplication
type PhApplicationSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	DisplayName  string             `json:"displayName"`
	GroupName    string             `json:"groupName"`
	InstanceType string             `json:"instanceType"`
	Scope        string             `json:"scope"`
	Stop         bool               `json:"stop,omitempty"`
	PodTemplate  corev1.PodSpec     `json:"podTemplate"`
	SvcTemplate  corev1.ServiceSpec `json:"svcTemplate"`
	HTTPPort     *int32             `json:"httpPort,omitempty"`
}

// PhApplicationStatus defines the observed state of PhApplication
type PhApplicationStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Phase       string `json:"phase"`
	Message     string `json:"message,omitempty"`
	ServiceName string `json:"serviceName,omitempty"`
}

// +kubebuilder:object:root=true

// PhApplication is the Schema for the phapplications API
type PhApplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PhApplicationSpec   `json:"spec"`
	Status PhApplicationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PhApplicationList contains a list of PhApplication
type PhApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PhApplication `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PhApplication{}, &PhApplicationList{})
}
