package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	networkv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"primehub-controller/pkg/escapism"
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

type PhApplicationPodTemplate struct {
	Spec corev1.PodSpec `json:"spec"`
}

type PhApplicationSvcTemplate struct {
	Spec corev1.ServiceSpec `json:"spec"`
}

// PhApplicationSpec defines the desired state of PhApplication
type PhApplicationSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	DisplayName  string                   `json:"displayName"`
	GroupName    string                   `json:"groupName"`
	InstanceType string                   `json:"instanceType"`
	Scope        string                   `json:"scope"`
	Stop         bool                     `json:"stop,omitempty"`
	PodTemplate  PhApplicationPodTemplate `json:"podTemplate"`
	SvcTemplate  PhApplicationSvcTemplate `json:"svcTemplate"`
	HTTPPort     *int32                   `json:"httpPort,omitempty"`
}

// PhApplicationStatus defines the observed state of PhApplication
type PhApplicationStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Phase       string `json:"phase"`
	Message     string `json:"message,omitempty"`
	ServiceName string `json:"serviceName,omitempty"`
}

// +kubebuilder:subresource:status
// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Group",type="string",JSONPath=".spec.groupName"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase",description="Status of the deployment"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message",description="Message of the deployment"

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

func (in *PhApplication) GroupNetworkPolicyIngressRule() []networkv1.NetworkPolicyIngressRule {
	var ports []networkv1.NetworkPolicyPort
	groupName := escapism.EscapeToPrimehubLabel(in.Spec.GroupName)
	for _, p := range in.Spec.SvcTemplate.Spec.Ports {
		ports = append(ports, networkv1.NetworkPolicyPort{
			Protocol: &p.Protocol,
			Port:     &p.TargetPort,
		})
	}
	return []networkv1.NetworkPolicyIngressRule{
		{
			Ports: ports,
			From: []networkv1.NetworkPolicyPeer{
				{
					PodSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"primehub.io/group": groupName},
					},
				},
			},
		},
	}
}

func (in *PhApplication) ProxyNetworkPolicyIngressRule() []networkv1.NetworkPolicyIngressRule {
	var ports []networkv1.NetworkPolicyPort
	for _, p := range in.Spec.SvcTemplate.Spec.Ports {
		ports = append(ports, networkv1.NetworkPolicyPort{
			Protocol: &p.Protocol,
			Port:     &p.TargetPort,
		})
	}
	return []networkv1.NetworkPolicyIngressRule{
		{
			Ports: ports,
			From: []networkv1.NetworkPolicyPeer{
				{
					PodSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"primehub.io/phapplication": in.AppName()},
					},
				},
			},
		},
	}
}

func (in *PhApplication) App() string {
	return "primehub-app"
}

func (in *PhApplication) AppID() string {
	return in.ObjectMeta.Name
}

func (in *PhApplication) AppName() string {
	return "app-" + in.ObjectMeta.Name
}

func (in *PhApplication) GroupName() string {
	return escapism.EscapeToPrimehubLabel(in.Spec.GroupName)
}
