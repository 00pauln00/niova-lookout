func (handler *nisdMonitor) SerfMembership() map[string]bool {
	membership := handler.storageClient.GetMembership()
	returnMap := make(map[string]bool)
	for _, member := range membership {
		if member.Status == "alive" {
			returnMap[member.Name] = true
		}
	}
	return returnMap
}

// NISD- should this be in the nisd file? should it pertain to any of the other files? gossip can give info on other services
func (handler *nisdMonitor) setTags() {
	for {
		tagData := handler.getCompressedGossipDataNISD()
		err := handler.serfHandler.SetNodeTags(tagData)
		if err != nil {
			logrus.Debug("setTags: ", err)
		}
		time.Sleep(time.Duration(SetTagsInterval) * time.Second)
	}
}

func (handler *nisdMonitor) startSerfAgent() error {
	setLogOutput(handler.serfLogger)
	//agentPort := handler.agentPort
	handler.serfHandler = serfAgent.SerfAgentHandler{
		Name:              handler.agentName,
		Addr:              net.ParseIP(handler.addr),
		ServicePortRangeS: uint16(handler.ServicePortRangeS),
		ServicePortRangeE: uint16(handler.ServicePortRangeE),
		AgentLogger:       log.Default(),
	}

	//Start serf agent
	_, err := handler.serfHandler.SerfAgentStartup(true)
	return err
}

// maybe belong to nisd file
func (handler *nisdMonitor) getCompressedGossipDataNISD() map[string]string {
	returnMap := make(map[string]string)
	nisdMap := handler.lookout.GetList()
	for _, nisd := range nisdMap {
		//Get data from map
		uuid := nisd.Uuid.String()
		status := nisd.Alive
		//Compact the data
		cuuid, _ := compressionLib.CompressUUID(uuid)
		cstatus := "0"
		if status {
			cstatus = "1"
		}

		//Fill map; will add extra info in future
		returnMap[cuuid] = cstatus
	}
	httpPort := RecvdPort
	returnMap["Type"] = "LOOKOUT"
	returnMap["Hport"] = strconv.Itoa(httpPort)
	return returnMap
}