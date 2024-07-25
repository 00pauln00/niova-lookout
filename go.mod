module github.com/00pauln00/niova-lookout

replace controlplane/serfAgent => ../niova-core/go/serf/agent/

replace common/serviceDiscovery => ../niova-core/go/serf/serviceDiscovery/

replace common/serfClient => ../niova-core/go/serf/client

replace common/httpClient => ../niova-core/go/http/client

replace common/specificCompressionLib => ../niova-core/go/specificCompressionLib/

replace niova/go-pumicedb-lib/common => ../niova-core/go/pumiceDB/common

go 1.22.5

require (
	github.com/fsnotify/fsnotify v1.7.0
	github.com/google/uuid v1.6.0
	github.com/sirupsen/logrus v1.9.3
)

require (
	common/httpClient v0.0.0-00010101000000-000000000000 // indirect
	common/serfClient v0.0.0-00010101000000-000000000000 // indirect
	common/serviceDiscovery v0.0.0-00010101000000-000000000000 // indirect
	common/specificCompressionLib v0.0.0-00010101000000-000000000000 // indirect
	controlplane/serfAgent v0.0.0-00010101000000-000000000000 // indirect
	github.com/armon/circbuf v0.0.0-20150827004946-bbbad097214e // indirect
	github.com/armon/go-metrics v0.0.0-20180917152333-f0300d1749da // indirect
	github.com/armon/go-radix v1.0.0 // indirect
	github.com/bgentry/speakeasy v0.1.0 // indirect
	github.com/fatih/color v1.9.0 // indirect
	github.com/google/btree v0.0.0-20180813153112-4030bb1f1f0c // indirect
	github.com/hashicorp/errwrap v1.0.0 // indirect
	github.com/hashicorp/go-immutable-radix v1.0.0 // indirect
	github.com/hashicorp/go-msgpack v0.5.3 // indirect
	github.com/hashicorp/go-multierror v1.1.0 // indirect
	github.com/hashicorp/go-sockaddr v1.0.0 // indirect
	github.com/hashicorp/go-syslog v1.0.0 // indirect
	github.com/hashicorp/golang-lru v0.5.0 // indirect
	github.com/hashicorp/logutils v1.0.0 // indirect
	github.com/hashicorp/mdns v1.0.4 // indirect
	github.com/hashicorp/memberlist v0.3.0 // indirect
	github.com/hashicorp/serf v0.9.7 // indirect
	github.com/mattn/go-colorable v0.1.6 // indirect
	github.com/mattn/go-isatty v0.0.12 // indirect
	github.com/miekg/dns v1.1.41 // indirect
	github.com/mitchellh/cli v1.1.0 // indirect
	github.com/mitchellh/mapstructure v0.0.0-20160808181253-ca63d7c062ee // indirect
	github.com/posener/complete v1.2.3 // indirect
	github.com/satori/go.uuid v1.2.0 // indirect
	github.com/sean-/seed v0.0.0-20170313163322-e2103e2c3529 // indirect
	golang.org/x/net v0.0.0-20210410081132-afb366fc7cd1 // indirect
	golang.org/x/sys v0.4.0 // indirect
	niova/go-pumicedb-lib/common v0.0.0-00010101000000-000000000000 // indirect
)
