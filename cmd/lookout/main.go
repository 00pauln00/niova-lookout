package main

import (
	"flag"
	"fmt"
	"net"
	"os"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/00pauln00/niova-lookout/pkg/communication"
	"github.com/00pauln00/niova-lookout/pkg/monitor"
	"github.com/00pauln00/niova-lookout/pkg/xlog"
)

var RecvdPort int

type handler struct {
	lookout      monitor.LookoutHandler
	epc          monitor.EPContainer
	endpointRoot *string
	httpPort     int
	ctlPath      *string
	promPath     string
	standalone   bool
	pmdb         bool
	PortRangeStr string
	coms         communication.CommHandler
}

func (handler *handler) parseCMDArgs() {
	var (
		showHelp      *bool
		showHelpShort *bool
		logLevel      *string
		agentAddr     string
		lookoutLogger string
	)

	handler.ctlPath = flag.String("dir",
		"/tmp/.niova", "endpoint directory root")

	showHelpShort = flag.Bool("h", false, "")
	showHelp = flag.Bool("help", false, "print help")

	logLevel = flag.String("log", "info",
		"set log level (panic, fatal, error, warn, info, debug, trace)")

	flag.BoolVar(&handler.standalone, "std", false,
		"Set flag to true to run lookout standalone for NISD")

	flag.BoolVar(&handler.pmdb, "pmdb", false,
		"Set flag to true to run lookout with pmdb")

	flag.StringVar(&handler.coms.UdpPort, "u",
		"1054", "UDP port for NISD communication")

	flag.IntVar(&handler.httpPort, "hp", 6666,
		"HTTP port for communication")

	flag.StringVar(&handler.PortRangeStr, "p", "",
		"Port range for the lookout to export data endpoints to, should be space seperated")

	flag.StringVar(&handler.coms.AgentName, "n", uuid.New().String(),
		"Agent name")

	flag.StringVar(&agentAddr, "a", "127.0.0.1", "Agent addr")
	flag.StringVar(&handler.coms.GossipNodesPath, "c",
		"./gossipNodes", "Gossip Node File Path")

	flag.StringVar(&handler.promPath, "pr", "./targets.json",
		"Prometheus targets info")

	flag.StringVar(&handler.coms.SerfLogger, "s", "serf.log", "Serf logs")
	flag.StringVar(&lookoutLogger, "l", "", "Lookout logs")
	flag.StringVar(&handler.coms.RaftUUID, "r", "", "Raft UUID")
	flag.StringVar(&handler.coms.LookoutUUID, "lu", uuid.NewString(),
		"Lookout UUID")
	flag.Parse()

	xlog.InitXlog(lookoutLogger, logLevel)

	// Convert agentAddr string to net.IP after parsing
	handler.coms.Addr = net.ParseIP(agentAddr)

	nonParsed := flag.Args()
	if len(nonParsed) > 0 {
		logrus.Debugf("Unexpected argument found: %s", nonParsed[0])
		usage(1)
	}

	if *showHelpShort == true || *showHelp == true {
		usage(0)
	}
}

func usage(rc int) {
	x := os.Stderr

	if rc == 0 {
		x = os.Stdout
	}

	fmt.Fprintf(x, "Usage: [OPTIONS] %s\n", os.Args[0])

	flag.CommandLine.SetOutput(x)
	flag.PrintDefaults()

	os.Exit(rc)
}

func main() {
	var handler handler
	var portAddr *int
	var err error

	//Get cmd line args
	handler.parseCMDArgs()

	//Initialize communication handler
	handler.coms = communication.CommHandler{}

	if handler.coms.GossipNodesPath != "" {
		err = handler.coms.LoadConfigInfo()
		if err != nil {
			logrus.Fatal("Error while loading config info - ", err)
		}
	} else {
		handler.coms.PortRange = make([]uint16, 1)
	}

	if !handler.standalone {
		logrus.Trace("Starting Serf")

		//Start serf agent
		err = handler.coms.StartSerfAgent()
		if err != nil {
			logrus.Fatal("Error while starting serf agent: ", err)
		}

		if handler.pmdb {
			logrus.Trace("Starting Client API for PMDB")
			handler.coms.StartClientAPI()
			go handler.coms.StartUDPListner()
		}
	}

	portAddr = &RecvdPort
	//Start lookout monitoring
	logrus.Debug("Port Range: ", handler.coms.PortRange)
	handler.epc = monitor.EPContainer{
		MonitorUUID: "*",
	}

	handler.lookout = monitor.LookoutHandler{
		Epc:      &handler.epc,
		PromPath: handler.promPath,
		CTLPath:  *handler.ctlPath,
		HttpPort: handler.httpPort,
	}

	errs := make(chan error, 1)
	//Start http service

	handler.coms.RetPort = portAddr
	handler.coms.Epc = &handler.epc
	handler.coms.HttpPort = handler.httpPort

	go func() {
		logrus.Info("Starting http server")
		err_r := handler.coms.ServeHttp()
		errs <- err_r
		if <-errs != nil {
			return
		}
	}()
	if err := <-errs; err != nil {
		*handler.coms.RetPort = -1
		logrus.Fatal("Error while starting http server : ", err)
	}

	if !handler.standalone {
		//Wait till http lookout http is up and running
		handler.coms.CheckHTTPLiveness()
		go handler.coms.SetTags()
		go handler.coms.GetTags()
	}

	//Start lookout
	logrus.Info("Starting lookout")
	er := handler.lookout.Start()
	if er != nil {
		logrus.Fatal("Error while starting Lookout : ", er)
	}
}
