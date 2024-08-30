package main

import (
	"flag"
	"net"
	"os"

	"github.com/00pauln00/niova-lookout/pkg/communication"
	"github.com/00pauln00/niova-lookout/pkg/monitor"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

var RecvdPort int

type handler struct {
	udpPort      string
	udpSocket    net.PacketConn
	lookout      monitor.LookoutHandler
	epc          monitor.EPContainer
	endpointRoot *string
	httpPort     int
	ctlPath      *string
	promPath     string
	standalone   bool
	PortRangeStr string
	coms         communication.ComHandler
}

func (handler *handler) parseCMDArgs() {
	var (
		showHelp      *bool
		showHelpShort *bool
		logLevel      *string
	)

	handler.ctlPath = flag.String("dir", "/tmp/.niova", "endpoint directory root")
	showHelpShort = flag.Bool("h", false, "")
	showHelp = flag.Bool("help", false, "print help")
	logLevel = flag.String("log", "info", "set log level (panic, fatal, error, warn, info, debug, trace)")

	flag.BoolVar(&handler.standalone, "std", true, "Set flag to true to run lookout standalone for NISD") // set to gossip and false default
	flag.StringVar(&handler.udpPort, "u", "1054", "UDP port for NISD communication")
	flag.IntVar(&handler.httpPort, "hp", 6666, "HTTP port for communication")
	flag.StringVar(&handler.PortRangeStr, "p", "", "Port range for the lookout to export data endpoints to, should be space seperated")
	flag.StringVar(&handler.coms.AgentName, "n", uuid.New().String(), "Agent name")
	flag.StringVar(&handler.coms.Addr, "a", "127.0.0.1", "Agent addr")
	flag.StringVar(&handler.coms.GossipNodesPath, "c", "", "PMDB server gossip info")
	flag.StringVar(&handler.promPath, "pr", "./targets.json", "Prometheus targets info")
	flag.StringVar(&handler.coms.SerfLogger, "s", "serf.log", "Serf logs")
	flag.StringVar(&handler.coms.RaftUUID, "r", "", "Raft UUID")
	flag.Parse()

	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		logrus.Fatalf("Invalid log level: %v", err)
	}
	logrus.SetLevel(level)

	nonParsed := flag.Args()
	logrus.Debug("nonParsed: ", nonParsed)
	if len(nonParsed) > 0 {
		logrus.Debugf("Unexpected argument found: %s", nonParsed[1])
		usage(1)
	}

	if *showHelpShort == true || *showHelp == true {
		usage(0)
	}
}

func usage(rc int) {
	logrus.Infof("Usage: [OPTIONS] %s\n", os.Args[0])
	flag.PrintDefaults()
	os.Exit(rc)
}

func main() {
	var handler handler
	var portAddr *int
	var err error

	//Initialize communication handler
	handler.coms = communication.ComHandler{}

	//Get cmd line args
	handler.parseCMDArgs()

	if handler.coms.GossipNodesPath != "" {
		err = handler.coms.LoadConfigInfo()
		if err != nil {
			logrus.Fatal("Error while loading config info - ", err)
		}
	} else {
		handler.coms.PortRange = make([]uint16, 1)
	}
	//Start pmdb service client discovery api
	if !handler.standalone {
		handler.coms.StartClientAPI()

		//Start serf agent
		err = handler.coms.StartSerfAgent()
		handler.coms.ServicePortRangeS = handler.coms.PortRange[0]
		handler.coms.ServicePortRangeE = handler.coms.PortRange[len(handler.coms.PortRange)-1]
		if err != nil {
			logrus.Fatal("Error while starting serf agent : ", err)
		}

		//Start udp listener
		go handler.coms.StartUDPListner()

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
	//Start lookout
	er := handler.lookout.Start()
	if er != nil {
		logrus.Fatal("Error while starting Lookout : ", er)
	}
	//Question: We never get here because this is set to happen after run has been set to false. is this correct?
	if !handler.standalone {
		//QUESTION: these are both infinite loops. should htey be ran as go functions?
		//Wait till http lookout http is up and running
		handler.coms.CheckHTTPLiveness()
		//Set serf tags
		handler.coms.SetTags()
	}
}
