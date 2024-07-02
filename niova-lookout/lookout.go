


func (handler *nisdMonitor) getConfigData(config string) error {
	portRangeStart, err := strconv.Atoi(strings.Split(config, "-")[0])
	portRangeEnd, err := strconv.Atoi(strings.Split(config, "-")[1])
	if err != nil {
		return err
	}

	for i := portRangeStart; i <= portRangeEnd; i++ {
		handler.PortRange = append(handler.PortRange, uint16(i))
	}

	if len(handler.PortRange) < 3 {
		return errors.New("Not enough ports available in the specified range to start services")
	}

	return err
}

func (handler *nisdMonitor) loadConfigInfo() error {
	//Get addrs and Rports and store it in handler

	if _, err := os.Stat(handler.gossipNodesPath); os.IsNotExist(err) {
		return err
	}
	reader, err := os.OpenFile(handler.gossipNodesPath, os.O_RDONLY, 0444)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(reader)
	//Read IPAddrs
	scanner.Scan()
	IPAddrs := strings.Split(scanner.Text(), " ")
	logrus.Debug("IPAddrs:", IPAddrs)
	handler.addr = IPAddrs[0]

	//Read Ports
	scanner.Scan()
	Ports := strings.Split(scanner.Text(), " ")
	temp, _ := strconv.Atoi(Ports[0])
	handler.ServicePortRangeS = uint16(temp)
	temp, _ = strconv.Atoi(Ports[1])
	handler.ServicePortRangeE = uint16(temp)

	handler.PortRange = makeRange(handler.ServicePortRangeS, handler.ServicePortRangeE)
	return nil
}

func main() {
	var nisd nisdMonitor
	var portAddr *int

	//Get cmd line args
	nisd.parseCMDArgs()

	err := nisd.loadConfigInfo()
	if err != nil {
		logrus.Fatal("Error while loading config info - ", err)
	}
	//Start pmdb service client discovery api
	if !nisd.standalone {
		nisd.startClientAPI()

		//Start serf agent
		err = nisd.startSerfAgent()
		nisd.ServicePortRangeS = nisd.PortRange[0]
		nisd.ServicePortRangeE = nisd.PortRange[len(nisd.PortRange)-1]
		if err != nil {
			logrus.Fatal("Error while starting serf agent : ", err)
		}

		//Start udp listener
		go nisd.startUDPListner()

	}

	portAddr = &RecvdPort
	//Start lookout monitoring
	logrus.Debug("Port Range: ", nisd.PortRange)
	nisd.lookout = lookout.EPContainer{
		MonitorUUID: "*",
		AppType:     "NISD",
		HttpPort:    6666,
		PortRange:   nisd.PortRange,
		CTLPath:     *nisd.ctlPath,
		PromPath:    nisd.promPath,
		//SerfMembershipCB: nisd.SerfMembership,
		EnableHttp: true,
		RetPort:    portAddr,
	}
	errs := nisd.lookout.Start()
	if errs != nil {
		logrus.Fatal("Error while starting Lookout : ", errs)
	}
	logrus.Info("Lookout started successfully")

	if !nisd.standalone {
		//Wait till http lookout http is up and running
		nisd.checkHTTPLiveness()
		//Set serf tags
		nisd.setTags()
	}
}

func setLogOutput(logPath string) {
	switch logPath {
	case "ignore":
		logrus.SetOutput(ioutil.Discard)
	default:
		f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			logrus.SetOutput(os.Stderr)
		} else {
			logrus.SetOutput(f)
		}
	}
}

func (handler *nisdMonitor) parseCMDArgs() {
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
	flag.StringVar(&handler.PortRangeStr, "p", "", "Port range for the lookout to export data endpoints to, should be space seperated")
	flag.StringVar(&handler.agentName, "n", uuid.New().String(), "Agent name")
	flag.StringVar(&handler.addr, "a", "127.0.0.1", "Agent addr")
	flag.StringVar(&handler.gossipNodesPath, "c", "./gossipNodes", "PMDB server gossip info")
	flag.StringVar(&handler.promPath, "pr", "./targets.json", "Prometheus targets info")
	flag.StringVar(&handler.serfLogger, "s", "serf.log", "Serf logs")
	flag.StringVar(&handler.raftUUID, "r", "", "Raft UUID")
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