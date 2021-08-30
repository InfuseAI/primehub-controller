module primehub-controller/ee

go 1.15

require (
	github.com/fatih/structtag v1.2.0
	github.com/go-logr/logr v0.1.0
	github.com/karlseguin/ccache v2.0.3+incompatible
	github.com/onsi/ginkgo v1.8.0
	github.com/onsi/gomega v1.5.0
	github.com/prometheus/common v0.4.1
	github.com/robfig/cron/v3 v3.0.0
	github.com/spf13/viper v1.4.0
	github.com/stretchr/testify v1.3.0
	go.uber.org/zap v1.10.0
	gopkg.in/yaml.v2 v2.2.4
	k8s.io/api v0.0.0-20190918195907-bd6ac527cfd2
	k8s.io/apimachinery v0.0.0-20190817020851-f2f3a405f61d
	k8s.io/client-go v0.0.0-20190918200256-06eb1244587a
	primehub-controller v0.0.0
	sigs.k8s.io/controller-runtime v0.3.0
)

replace primehub-controller v0.0.0 => ../
