package controllers

import "k8s.io/api/extensions/v1beta1"

type PhIngress struct {
	Annotations map[string]string
	Hosts       []string
	TLS         []v1beta1.IngressTLS
}
