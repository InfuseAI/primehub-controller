package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:validation:Enum=inactive;daily;weekly;monthly;custom
type RecurrenceType string

const (
	RecurrenceTypeInactive RecurrenceType = "inactive"
	RecurrenceTypeDaily    RecurrenceType = "daily"   // 0 4 * * *
	RecurrenceTypeWeekly   RecurrenceType = "weekly"  // 0 4 * * 0
	RecurrenceTypeMonthly  RecurrenceType = "monthly" // 0 4 1 * *
	RecurrenceTypeCustom   RecurrenceType = "custom"
)

type Recurrence struct {
	Type RecurrenceType `json:"type"`
	Cron string         `json:"cron,omitempty"`
}

// PhScheduleSpec defines the desired state of PhSchedule
type PhScheduleSpec struct {
	Recurrence  Recurrence        `json:"recurrence"`
	JobTemplate PhJobTemplateSpec `json:"jobTemplate"`
}

// PhScheduleStatus defines the observed state of PhSchedule
type PhScheduleStatus struct {
	Invalid     bool         `json:"invalid,omitempty"`
	NextRunTime *metav1.Time `json:"nextRunTime,omitempty"`
	Message     string       `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="User",type="string",JSONPath=".spec.jobTemplate.spec.userName"
// +kubebuilder:printcolumn:name="Group",type="string",JSONPath=".spec.jobTemplate.spec.groupName"
// +kubebuilder:printcolumn:name="Invalid",type="boolean",JSONPath=".status.invalid"
// +kubebuilder:printcolumn:name="NextRun",type="string",JSONPath=".status.nextRunTime"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:subresource:status
// see: https://github.com/kubernetes-sigs/kubebuilder/issues/751

// PhSchedule is the Schema for the phschedules API
type PhSchedule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PhScheduleSpec   `json:"spec,omitempty"`
	Status PhScheduleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PhScheduleList contains a list of PhSchedule
type PhScheduleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PhSchedule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PhSchedule{}, &PhScheduleList{})
}
