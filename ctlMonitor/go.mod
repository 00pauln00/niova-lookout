module controlplane/lookout

replace common/requestResponseLib => ../RequestResponseLib/

replace common/prometheus_handler => ../prometheusHandler/

go 1.16

require (
	github.com/fsnotify/fsnotify v1.5.1
	github.com/google/uuid v1.3.0
)

require (
	common/prometheus_handler v0.0.0-00010101000000-000000000000 // indirect
	common/requestResponseLib v0.0.0-00010101000000-000000000000 // indirect
	github.com/satori/go.uuid v1.2.0
	github.com/sirupsen/logrus v1.9.3 // indirect
	gopkg.in/check.v1 v0.0.0-20161208181325-20d25e280405 // indirect
)
