func (handler *nisdMonitor) getPortRange() error {
	var response requestResponseLib.PMDBKVResponse

	responseBytes, err := handler.requestPMDB(handler.raftUUID + "_Port_Range")
	if err != nil {
		logrus.Error("Request PMDB - ", err)
		return err
	}

	dec := gob.NewDecoder(bytes.NewBuffer(responseBytes))
	dec.Decode(&response)

	err = handler.getConfigData(string(response.ResultMap[handler.raftUUID+"_Port_Range"]))
	if err != nil {
		logrus.Error("getConfigData - ", err)
	}

	return nil
}

func (handler *nisdMonitor) findFreePort() int {
	for i := 0; i < len(handler.PortRange); i++ {
		handler.httpPort = int(handler.PortRange[i])
		logrus.Debug("Trying to bind with - ", int(handler.httpPort))
		check, err := net.Listen("tcp", handler.addr+":"+strconv.Itoa(int(handler.httpPort)))
		if err != nil {
			if strings.Contains(err.Error(), "bind") {
				continue
			} else {
				logrus.Error("Error while finding port : ", err)
				return 0
			}
		} else {
			check.Close()
			break
		}
	}
	logrus.Debug("Returning free port - ", handler.httpPort)
	return handler.httpPort
}

func makeRange(min, max uint16) []uint16 {
	a := make([]uint16, max-min+1)
	for i := range a {
		a[i] = uint16(min + uint16(i))
	}
	return a
}

func (handler *nisdMonitor) checkHTTPLiveness() {
	var emptyByteArray []byte
	for {
		_, err := httpClient.HTTP_Request(emptyByteArray, "127.0.0.1:"+strconv.Itoa(int(RecvdPort))+"/check", false)
		if err != nil {
			logrus.Error("HTTP Liveness - ", err)
		} else {
			logrus.Debug("HTTP Liveness - HTTP Server is alive")
			break
		}
		time.Sleep(1 * time.Second)
	}
}

// maybe put this in the http server file
func (handler *nisdMonitor) getAddrList() []string {
	var addrs []string
	for i := 0; i < len(handler.PortRange); i++ {
		addrs = append(addrs, handler.addr+":"+strconv.Itoa(int(handler.PortRange[i])))
	}
	return addrs
}