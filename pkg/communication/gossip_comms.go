package communication

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"encoding/json"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	compress "github.com/00pauln00/niova-pumicedb/go/pkg/utils/compressor"
	serf "github.com/00pauln00/niova-pumicedb/go/pkg/utils/serfagent"
	sd "github.com/00pauln00/niova-pumicedb/go/pkg/utils/servicediscovery"

	"github.com/sirupsen/logrus"

	"github.com/00pauln00/niova-lookout/pkg/monitor"
	"github.com/00pauln00/niova-lookout/pkg/monitor/applications"
	"github.com/00pauln00/niova-lookout/pkg/requestResponseLib"
	"github.com/00pauln00/niova-lookout/pkg/xlog"
)

var SetTagsInterval int = 10

func (h *CommHandler) SerfMembership() map[string]bool {
	//Is this still used by anything? nothing in this repo uses it
	membership := h.StorageClient.GetMembership()
	returnMap := make(map[string]bool)
	for _, member := range membership {
		if member.Status == "alive" {
			returnMap[member.Name] = true
		}
	}
	return returnMap
}

type logrusWriter struct{}

// This method intercepts the log msgs from the serf logrus api and converts
//   them to xlog by parsing the log level contained in the msg.
func (lw logrusWriter) Write(p []byte) (n int, err error) {
	// Split off the first three space-separated fields

	line := string(p)

	parts := strings.SplitN(line, " ", 4)
	if len(parts) < 4 {
		xlog.Warn("Malformed Serf log: %s", line)
		return 0, nil
	}
	msgPart := parts[3] // contains :: [LEVEL] actual message

	// Find the level
	lvlStart := strings.Index(msgPart, "[")
	lvlEnd := strings.Index(msgPart, "]")
	var lvl string
	var msg string
	if lvlStart != -1 && lvlEnd != -1 && lvlEnd > lvlStart {
		lvl = msgPart[lvlStart+1 : lvlEnd]
		msg = strings.TrimSpace(msgPart[lvlEnd+1:])
	} else {
		lvl = "INFO"
		msg = msgPart
	}

	logLevel := xlog.INFO

	// Map Serf/logrus level to xlog
	switch strings.ToUpper(lvl) {
	case "DEBUG":
		logLevel = xlog.DEBUG
	case "INFO":
		logLevel = xlog.INFO
	case "WARNING":
		logLevel = xlog.WARN
	case "ERROR":
		logLevel = xlog.ERROR
	}

	xlog.WithLevel(logLevel, "%s", msg)

	return len(p), nil
}

func (h *CommHandler) StartSerfAgent() error {
	lw := logrusWriter{}

	// Wire Serf's agent logger to use our custom writer
	// no LstdFlags because we don’t need timestamp/prefix
	agentLogger := log.New(lw, "", 0)

	// Also redirect logrus itself to our adapter
	logrus.SetOutput(lw)

	h.SerfHandler = serf.SerfAgentHandler{
		Name:              h.AgentName,
		Addr:              h.Addr,
		ServicePortRangeS: uint16(h.ServicePortRangeS),
		ServicePortRangeE: uint16(h.ServicePortRangeE),
		AgentLogger:       agentLogger,
		AddrList:          h.addrList,
	}

	// Start serf agent
	memCount, err := h.SerfHandler.SerfAgentStartup(true)

	// If no members were found, wait for a while and try again.  Give
	//   other serf agents time to start.
	if memCount == 0 {
		xlog.Warn("No serf members found, retrying...")
		for i := 0; i < 3; i++ {
			time.Sleep(5 * time.Second)
			memCount, err = h.SerfHandler.SerfAgentStartup(true)
			if memCount > 0 {
				break
			}
			xlog.Warn("No serf members found, retrying... (attempt ", i+1, ")")
		}
		if memCount == 0 {
			xlog.Error("Failed to start serf agent after 3 attempts")
			return err
		}
	}
	xlog.Info("Serf agent started with ", memCount, " members")
	return err
}

func (h *CommHandler) StartClientAPI() {
	//Init niovakv client API?
	h.StorageClient = sd.ServiceDiscoveryHandler{
		HTTPRetry: 10,
		SerfRetry: 5,
		RaftUUID:  h.RaftUUID,
	}
	stop := make(chan int)
	go func() {
		err := h.StorageClient.StartClientAPI(stop, h.GossipNodesPath)
		if err != nil {
			xlog.Error("Error while starting client API : ", err)
		}
	}()
	h.StorageClient.TillReady("", 0)
}

// StartUDPListner listens for incoming UDP messages and delegates handling to getConfigNSend.
func (h *CommHandler) StartUDPListner() {
	xlog.Trace("StartUDPListner called for port ", h.UdpPort)
	var err error
	h.UdpSocket, err = net.ListenPacket("udp", ":"+h.UdpPort)
	if err != nil {
		xlog.Error("UDP listner failed : ", err)
	}

	defer h.UdpSocket.Close()
	for {
		xlog.Debug("UDP listner waiting for message")
		buf := make([]byte, 1024)
		_, addr, err := h.UdpSocket.ReadFrom(buf)
		if err != nil {
			xlog.Error("UDP read failed : ", err)
			continue
		}
		udpInfo := UdpMessage{
			addr:    addr,
			message: buf,
		}
		xlog.Debug("UDP message received from ", addr)
		go h.getConfigNSend(udpInfo)
	}
}

// getConfigNSend processes a UDP message, fetches config from PMDB, and
//  responds to the sender.
func (h *CommHandler) getConfigNSend(udpInfo UdpMessage) {
	//Get uuid from the byte array
	data := udpInfo.message
	uuidString := string(data[:36])

	h.Epc.MarkAlive(uuidString)

	//Send config read request to PMDB server
	responseByte, _ := applications.RequestPMDB(uuidString) //h.RequestPMDB(uuidString)

	//Decode response to IPAddr and Port
	responseObj := requestResponseLib.KVResponse{}
	dec := gob.NewDecoder(bytes.NewBuffer(responseByte))
	dec.Decode(&responseObj)
	var value map[string]string
	json.Unmarshal(responseObj.Value, &value)
	ipaddr := value["IP_ADDR"]
	port, _ := strconv.Atoi(value["Port"])

	//Fill C structure
	structByteArray := applications.FillNisdCStruct(uuidString, ipaddr, port)

	//Send the data to the node
	h.UdpSocket.WriteTo(structByteArray, udpInfo.addr)
}

func (h *CommHandler) SetTags() {
	for {
		tagData := h.GetCompressedGossipData()
		err := h.SerfHandler.SetNodeTags(tagData)
		if err != nil {
			xlog.Debug("setTags: ", err)
		}
		time.Sleep(time.Duration(SetTagsInterval) * time.Second)
	}
}

func (h *CommHandler) GetTags() {
	// Create lookout map so we dont get panic: assignment to entry in nil map
	h.Lookouts = make(map[string]*LookoutInfo)
	for {
		members := h.SerfHandler.GetTags("LO-Type", "LOOKOUT")
		for _, addr := range members {
			// Extract Lookout fields
			loUUID := addr["LO-UUID"]
			loHport := addr["LO-Hport"]
			loTime := addr["LO-Time"]
			// Parse last seen time
			lastSeen, err := strconv.ParseInt(loTime, 10, 64)
			if err != nil {
				xlog.Error("Failed to parse LO-Time: ", err)
				continue
			}
			readableTime := time.Unix(lastSeen, 0).Format("2006-01-02 15:04:05")
			// Decode LO-IPAddr
			var ipAddrs []string
			if err := json.Unmarshal([]byte(addr["LO-IPAddr"]), &ipAddrs); err != nil {
				xlog.Error("Failed to parse LO-IPAddr: ", err)
				continue
			}
			// Decode LO-PortRange
			var portRange string
			if err := json.Unmarshal([]byte(addr["LO-PortRange"]), &portRange); err != nil {
				xlog.Error("Failed to parse LO-PortRange: ", err)
				continue
			}
			lookout := &LookoutInfo{
				IPAddrs:   ipAddrs,
				HTTPPort:  loHport,
				PortRange: portRange,
				Apps:      map[uuid.UUID]MonitoredApp{},
				LastSeen:  readableTime,
			}

			// Parse monitored app info from the rest of the tags
			for key, value := range addr {
				if strings.HasPrefix(key, "LO-") {
					continue // skip lookout metadata
				}

				// Decompress UUID key
				uuidStr, err := compress.DecompressUUID(key)
				if err != nil {
					xlog.Error("Failed to decompress UUID key: ",
						key, " error: ", err)
					continue
				}
				parsedUUID, err := uuid.Parse(uuidStr)
				if err != nil {
					xlog.Error("Invalid UUID: ", uuidStr)
					continue
				}

				// Decode MonitoredApp JSON value
				var gData MonitoredApp
				if err := json.Unmarshal([]byte(value), &gData); err != nil {
					xlog.Error("Failed to unmarshal MonitoredApp: ", err)
					continue
				}

				lookout.Apps[parsedUUID] = gData
			}

			// Save to CommHandler
			h.mu.Lock()
			h.Lookouts[loUUID] = lookout
			h.mu.Unlock()

			xlog.Tracef("Updated Lookout: %s, Monitoring %d apps",
				loUUID, len(lookout.Apps))
		}

		//TODO: make this a variable
		time.Sleep(10 * time.Second)
	}
}

func (h *CommHandler) GetCompressedGossipData() map[string]string {
	gossipData := make(map[string]string)
	epMap := h.Epc.GetList()
	for _, ep := range epMap {
		//nisd should say if it wants gossip monitoring
		if ep.App.IsMonitoringEnabled() {
			//Get data from map
			epUUID := ep.Uuid.String()
			//Compact the uuid
			cuuid, _ := compress.CompressUUID(epUUID)
			cstatus := "0"
			if ep.State == monitor.EPstateRunning {
				cstatus = "1"
			}
			//add type
			epType := ep.App.GetAppName()

			// Create GossipData struct
			gsd := MonitoredApp{
				Status: cstatus,
				Type:   epType,
			}

			marshaledGossipDataStruct, err := json.Marshal(gsd)
			if err != nil {
				xlog.Errorf("json.Marshal(): %v", err)
				continue
			}
			gossipData[cuuid] = string(marshaledGossipDataStruct)
		}
	}

	gossipData["LO-UUID"] = h.LookoutUUID
	gossipData["LO-Type"] = "LOOKOUT"
	gossipData["LO-Hport"] = strconv.Itoa(h.HttpPort)
	ipAddrs := make([]string, len(h.addrList))
	for i, ip := range h.addrList {
		ipAddrs[i] = ip.String()
	}
	cIpAddrs, _ := json.Marshal(ipAddrs)
	gossipData["LO-IPAddr"] = string(cIpAddrs)
	portRange := strconv.Itoa(int(h.ServicePortRangeS)) + " " + strconv.Itoa(int(h.ServicePortRangeE))
	cPortRange, _ := json.Marshal(portRange)
	gossipData["LO-PortRange"] = string(cPortRange)
	gossipData["LO-Time"] = strconv.FormatInt(time.Now().Unix(), 10)
	return gossipData
}

// LoadConfigInfo reads IP addresses and port ranges from a config file into CommHandler.
func (h *CommHandler) LoadConfigInfo() error {
	//Get addrs and Rports and store it in h
	if _, err := os.Stat(h.GossipNodesPath); os.IsNotExist(err) {
		xlog.Error("GossipNodesPath does not exist:", h.GossipNodesPath)
		return err
	}
	reader, err := os.OpenFile(h.GossipNodesPath, os.O_RDONLY, 0444)
	if err != nil {
		xlog.Error("Error while opening GossipNodesPath file")
		return err
	}

	scanner := bufio.NewScanner(reader)
	/*
		Following is the format of gossipNodes file:
		server addrs with space separated
		Start_port End_port
	*/
	scanner.Scan()
	IPAddrsTxt := strings.Split(scanner.Text(), " ")
	IPAddrs := removeDuplicateStr(IPAddrsTxt)
	for i := range IPAddrs {
		ipAddr := net.ParseIP(IPAddrs[i])
		h.addrList = append(h.addrList, ipAddr)
	}
	xlog.Debug("IPAddrs:", IPAddrs)
	h.Addr = net.ParseIP(IPAddrs[0])

	//Read Ports
	scanner.Scan()
	Ports := strings.Split(scanner.Text(), " ")
	xlog.Debug("Ports:", Ports)
	temp, _ := strconv.Atoi(Ports[0])
	h.ServicePortRangeS = uint16(temp)
	temp, _ = strconv.Atoi(Ports[1])
	h.ServicePortRangeE = uint16(temp)

	h.makeRange()
	return nil
}

func (h *CommHandler) makeRange() {
	a := make([]uint16, h.ServicePortRangeE-h.ServicePortRangeS+1)
	for i := range a {
		a[i] = uint16(h.ServicePortRangeS + uint16(i))
	}
	h.PortRange = a
}

func removeDuplicateStr(strSlice []string) []string {
	allKeys := make(map[string]bool)
	list := []string{}
	for _, item := range strSlice {
		if _, value := allKeys[item]; !value {
			allKeys[item] = true
			list = append(list, item)
		}
	}
	return list
}
