module primehub-controller/ee

go 1.12

require (
	github.com/go-logr/logr v0.1.0
	github.com/onsi/ginkgo v1.6.0
	github.com/onsi/gomega v1.4.2
	github.com/spf13/viper v1.4.0
	go.uber.org/zap v1.10.0
	k8s.io/api v0.0.0-20190918195907-bd6ac527cfd2
	k8s.io/apimachinery v0.0.0-20190817020851-f2f3a405f61d
	k8s.io/client-go v0.0.0-20190918200256-06eb1244587a
	primehub-controller v0.0.0
	sigs.k8s.io/controller-runtime v0.3.0
)

replace primehub-controller v0.0.0 => ../
