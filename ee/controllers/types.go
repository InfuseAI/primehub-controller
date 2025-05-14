package controllers

import (
	autoscalingv2beta2 "k8s.io/api/autoscaling/v2beta1"
	v1 "k8s.io/api/core/v1"
	networkv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PhIngress struct {
	Annotations map[string]string
	Hosts       []string
	TLS         []networkv1.IngressTLS
	ClassName   string
}

type PredictiveUnitType string

const (
	UNKNOWN_TYPE       PredictiveUnitType = "UNKNOWN_TYPE"
	ROUTER             PredictiveUnitType = "ROUTER"
	COMBINER           PredictiveUnitType = "COMBINER"
	MODEL              PredictiveUnitType = "MODEL"
	TRANSFORMER        PredictiveUnitType = "TRANSFORMER"
	OUTPUT_TRANSFORMER PredictiveUnitType = "OUTPUT_TRANSFORMER"
)

type PredictiveUnitImplementation string

const (
	UNKNOWN_IMPLEMENTATION PredictiveUnitImplementation = "UNKNOWN_IMPLEMENTATION"
	SIMPLE_MODEL           PredictiveUnitImplementation = "SIMPLE_MODEL"
	SIMPLE_ROUTER          PredictiveUnitImplementation = "SIMPLE_ROUTER"
	RANDOM_ABTEST          PredictiveUnitImplementation = "RANDOM_ABTEST"
	AVERAGE_COMBINER       PredictiveUnitImplementation = "AVERAGE_COMBINER"
)

type PredictiveUnitMethod string

const (
	TRANSFORM_INPUT  PredictiveUnitMethod = "TRANSFORM_INPUT"
	TRANSFORM_OUTPUT PredictiveUnitMethod = "TRANSFORM_OUTPUT"
	ROUTE            PredictiveUnitMethod = "ROUTE"
	AGGREGATE        PredictiveUnitMethod = "AGGREGATE"
	SEND_FEEDBACK    PredictiveUnitMethod = "SEND_FEEDBACK"
)

type EndpointType string

const (
	REST EndpointType = "REST"
	GRPC EndpointType = "GRPC"
)

type Endpoint struct {
	ServiceHost string       `json:"service_host,omitempty" protobuf:"string,1,opt,name=service_host"`
	ServicePort int32        `json:"service_port,omitempty" protobuf:"int32,2,opt,name=service_port"`
	Type        EndpointType `json:"type,omitempty" protobuf:"int,3,opt,name=type"`
}
type ParmeterType string

const (
	INT    ParmeterType = "INT"
	FLOAT  ParmeterType = "FLOAT"
	DOUBLE ParmeterType = "DOUBLE"
	STRING ParmeterType = "STRING"
	BOOL   ParmeterType = "BOOL"
)

type Parameter struct {
	Name  string       `json:"name" protobuf:"string,1,opt,name=name"`
	Value string       `json:"value" protobuf:"string,2,opt,name=value"`
	Type  ParmeterType `json:"type" protobuf:"int,3,opt,name=type"`
}

type LoggerMode string

const (
	LogAll      LoggerMode = "all"
	LogRequest  LoggerMode = "request"
	LogResponse LoggerMode = "response"
)

// Logger provides optional payload logging for all endpoints
// +experimental
type Logger struct {
	// URL to send request logging CloudEvents
	// +optional
	Url *string `json:"url,omitempty"`
	// What payloads to log
	Mode LoggerMode `json:"mode,omitempty"`
}

type AlibiExplainerType string

const (
	AlibiAnchorsTabularExplainer  AlibiExplainerType = "AnchorTabular"
	AlibiAnchorsImageExplainer    AlibiExplainerType = "AnchorImages"
	AlibiAnchorsTextExplainer     AlibiExplainerType = "AnchorText"
	AlibiCounterfactualsExplainer AlibiExplainerType = "Counterfactuals"
	AlibiContrastiveExplainer     AlibiExplainerType = "Contrastive"
)

type PredictorSpec struct {
	Name            string                  `json:"name" protobuf:"string,1,opt,name=name"`
	Graph           *PredictiveUnit         `json:"graph" protobuf:"bytes,2,opt,name=predictiveUnit"`
	ComponentSpecs  []*SeldonPodSpec        `json:"componentSpecs,omitempty" protobuf:"bytes,3,opt,name=componentSpecs"`
	Replicas        int32                   `json:"replicas,omitempty" protobuf:"string,4,opt,name=replicas"`
	Annotations     map[string]string       `json:"annotations,omitempty" protobuf:"bytes,5,opt,name=annotations"`
	EngineResources v1.ResourceRequirements `json:"engineResources,omitempty" protobuf:"bytes,6,opt,name=engineResources"`
	Labels          map[string]string       `json:"labels,omitempty" protobuf:"bytes,7,opt,name=labels"`
	SvcOrchSpec     SvcOrchSpec             `json:"svcOrchSpec,omitempty" protobuf:"bytes,8,opt,name=svcOrchSpec"`
	Traffic         int32                   `json:"traffic,omitempty" protobuf:"bytes,9,opt,name=traffic"`
	Explainer       *Explainer              `json:"explainer,omitempty" protobuf:"bytes,10,opt,name=explainer"`
	Shadow          bool                    `json:"shadow,omitempty" protobuf:"bytes,11,opt,name=shadow"`
}

type PredictiveUnit struct {
	Name               string                        `json:"name" protobuf:"string,1,opt,name=name"`
	Children           []PredictiveUnit              `json:"children,omitempty" protobuf:"bytes,2,opt,name=children"`
	Type               *PredictiveUnitType           `json:"type,omitempty" protobuf:"int,3,opt,name=type"`
	Implementation     *PredictiveUnitImplementation `json:"implementation,omitempty" protobuf:"int,4,opt,name=implementation"`
	Methods            *[]PredictiveUnitMethod       `json:"methods,omitempty" protobuf:"int,5,opt,name=methods"`
	Endpoint           *Endpoint                     `json:"endpoint,omitempty" protobuf:"bytes,6,opt,name=endpoint"`
	Parameters         []Parameter                   `json:"parameters,omitempty" protobuf:"bytes,7,opt,name=parameters"`
	ModelURI           string                        `json:"modelUri,omitempty" protobuf:"bytes,8,opt,name=modelUri"`
	ServiceAccountName string                        `json:"serviceAccountName,omitempty" protobuf:"bytes,9,opt,name=serviceAccountName"`
	EnvSecretRefName   string                        `json:"envSecretRefName,omitempty" protobuf:"bytes,10,opt,name=envSecretRefName"`
	// Request/response  payload logging. v2alpha1 feature that is added to v1 for backwards compatibility while v1 is the storage version.
	Logger *Logger `json:"logger,omitempty"`
}

type SeldonPodSpec struct {
	Metadata metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Spec     v1.PodSpec        `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	HpaSpec  *SeldonHpaSpec    `json:"hpaSpec,omitempty" protobuf:"bytes,3,opt,name=hpaSpec"`
}

type SvcOrchSpec struct {
	Resources *v1.ResourceRequirements `json:"resources,omitempty" protobuf:"bytes,1,opt,name=resources"`
	Env       []*v1.EnvVar             `json:"env,omitempty" protobuf:"bytes,2,opt,name=env"`
}

type Explainer struct {
	Type               AlibiExplainerType `json:"type,omitempty" protobuf:"string,1,opt,name=type"`
	ModelUri           string             `json:"modelUri,omitempty" protobuf:"string,2,opt,name=modelUri"`
	ServiceAccountName string             `json:"serviceAccountName,omitempty" protobuf:"string,3,opt,name=serviceAccountName"`
	ContainerSpec      v1.Container       `json:"containerSpec,omitempty" protobuf:"bytes,4,opt,name=containerSpec"`
	Config             map[string]string  `json:"config,omitempty" protobuf:"bytes,5,opt,name=config"`
	Endpoint           *Endpoint          `json:"endpoint,omitempty" protobuf:"bytes,6,opt,name=endpoint"`
	EnvSecretRefName   string             `json:"envSecretRefName,omitempty" protobuf:"bytes,7,opt,name=envSecretRefName"`
}

type SeldonHpaSpec struct {
	MinReplicas *int32                          `json:"minReplicas,omitempty" protobuf:"int,1,opt,name=minReplicas"`
	MaxReplicas int32                           `json:"maxReplicas" protobuf:"int,2,opt,name=maxReplicas"`
	Metrics     []autoscalingv2beta2.MetricSpec `json:"metrics,omitempty" protobuf:"bytes,3,opt,name=metrics"`
}
