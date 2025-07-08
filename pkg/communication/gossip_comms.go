package communication

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"encoding/json"
	"io/ioutil"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	compressionLib "github.com/00pauln00/niova-pumicedb/go/pkg/utils/compressor"

	serfAgent "github.com/00pauln00/niova-pumicedb/go/pkg/utils/serfagent"

	serviceDiscovery "github.com/00pauln00/niova-pumicedb/go/pkg/utils/servicediscovery"

	"github.com/00pauln00/niova-lookout/pkg/monitor/applications"
	"github.com/00pauln00/niova-lookout/pkg/requestResponseLib"
	"github.com/sirupsen/logrus"
)

var SetTagsInterval int = 10

func (h *CommHandler) SerfMembership() map[string]bool {
	membership := h.StorageClient.GetMembership()
	returnMap := make(map[string]bool)
	for _, member := range membership {
		if member.Status == "alive" {
			returnMap[member.Name] = true
		}
	}
	return returnMap
}

func (h *CommHandler) StartSerfAgent() error {
	setLogOutput(h.SerfLogger)
	//agentPort := h.agentPort
	h.SerfHandler = serfAgent.SerfAgentHandler{
		Name:              h.AgentName,
		Addr:              net.ParseIP(h.Addr),
		ServicePortRangeS: uint16(h.ServicePortRangeS),
		ServicePortRangeE: uint16(h.ServicePortRangeE),
		AgentLogger:       log.Default(),
	}

	//Start serf agent
	_, err := h.SerfHandler.SerfAgentStartup(true)
	return err
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
func (h *CommHandler) StartClientAPI() {
	//Init niovakv client API
	h.StorageClient = serviceDiscovery.ServiceDiscoveryHandler{
		HTTPRetry: 10,
		SerfRetry: 5,
	}
	stop := make(chan int)
	go func() {
		err := h.StorageClient.StartClientAPI(stop, h.GossipNodesPath)
		if err != nil {
			logrus.Fatal("Error while starting client API : ", err)
		}
	}()
	h.StorageClient.TillReady("", 0)
}

// NISD
func (h *CommHandler) StartUDPListner() {
	var err error
	h.UdpSocket, err = net.ListenPacket("udp", ":"+h.UdpPort)
	if err != nil {
		logrus.Error("UDP listner failed : ", err)
	}

	defer h.UdpSocket.Close()
	for {
		buf := make([]byte, 1024)
		_, addr, err := h.UdpSocket.ReadFrom(buf)
		if err != nil {
			continue
		}
		udpInfo := UdpMessage{
			addr:    addr,
			message: buf,
		}
		go h.getConfigNSend(udpInfo)
	}
}

// NISD
func (h *CommHandler) getConfigNSend(udpInfo UdpMessage) {
	//Get uuid from the byte array
	data := udpInfo.message
	uuidString := string(data[:36])

	h.Epc.MarkAlive(uuidString)

	//Send config read request to PMDB server
	//TODO: make this call a funtion Request which is application specific through the interface
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
	//TODO: make this call a funtion FillCStruct which is application specific through the interface
	structByteArray := applications.FillNisdCStruct(uuidString, ipaddr, port)

	//Send the data to the node
	h.UdpSocket.WriteTo(structByteArray, udpInfo.addr)
}

// NISD
func (h *CommHandler) SetTags() {
	for {
		//TODO: make this a function that is application specific through the interface
		tagData := h.GetCompressedGossipDataNISD()
		err := h.SerfHandler.SetNodeTags(tagData)
		if err != nil {
			logrus.Debug("setTags: ", err)
		}
		time.Sleep(time.Duration(SetTagsInterval) * time.Second)
	}
}

func (h *CommHandler) GetCompressedGossipDataNISD() map[string]string {
	returnMap := make(map[string]string)
	epMap := h.Epc.GetList()
	for _, ep := range epMap {
		//Get data from map
		uuid := ep.Uuid.String()
		status := ep.Alive
		//Compact the data
		cuuid, _ := compressionLib.CompressUUID(uuid)
		cstatus := "0"
		if status {
			cstatus = "1"
		}

		//Fill map; will add extra info in future
		returnMap[cuuid] = cstatus
	}
	httpPort := h.RecvdPort
	returnMap["Type"] = "LOOKOUT"
	returnMap["Hport"] = strconv.Itoa(httpPort)
	return returnMap
}

func (h *CommHandler) LoadConfigInfo() error {
	//Get addrs and Rports and store it in h
	if _, err := os.Stat(h.GossipNodesPath); os.IsNotExist(err) {
		logrus.Error("GossipNodesPath does not exist:", h.GossipNodesPath)
		return err
	}
	reader, err := os.OpenFile(h.GossipNodesPath, os.O_RDONLY, 0444)
	if err != nil {
		logrus.Error("Error while opening GossipNodesPath file")
		return err
	}

	scanner := bufio.NewScanner(reader)
	//Read IPAddrs
	scanner.Scan()
	IPAddrs := strings.Split(scanner.Text(), " ")
	logrus.Debug("IPAddrs:", IPAddrs)
	h.Addr = IPAddrs[0]

	//TODO: fix this the ip addr contains ports and the ports are not being put into ServicePortRangeS and ServicePortRangeE
	//Read Ports
	scanner.Scan()
	Ports := strings.Split(scanner.Text(), " ")
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
