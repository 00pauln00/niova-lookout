package communication

import (
	"bufio"
	"bytes"
	"common/serviceDiscovery"
	compressionLib "common/specificCompressionLib"
	"controlplane/serfAgent"
	"encoding/gob"
	"encoding/json"
	"io/ioutil"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/00pauln00/niova-lookout/pkg/monitor/applications"
	"github.com/00pauln00/niova-lookout/pkg/requestResponseLib"
	"github.com/sirupsen/logrus"
)

var SetTagsInterval int = 10

func (handler *ComHandler) SerfMembership() map[string]bool {
	membership := handler.StorageClient.GetMembership()
	returnMap := make(map[string]bool)
	for _, member := range membership {
		if member.Status == "alive" {
			returnMap[member.Name] = true
		}
	}
	return returnMap
}

func (handler *ComHandler) StartSerfAgent() error {
	setLogOutput(handler.SerfLogger)
	//agentPort := handler.agentPort
	handler.SerfHandler = serfAgent.SerfAgentHandler{
		Name:              handler.AgentName,
		Addr:              net.ParseIP(handler.Addr),
		ServicePortRangeS: uint16(handler.ServicePortRangeS),
		ServicePortRangeE: uint16(handler.ServicePortRangeE),
		AgentLogger:       log.Default(),
	}

	//Start serf agent
	_, err := handler.SerfHandler.SerfAgentStartup(true)
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
func (handler *ComHandler) StartClientAPI() {
	//Init niovakv client API
	handler.StorageClient = serviceDiscovery.ServiceDiscoveryHandler{
		HTTPRetry: 10,
		SerfRetry: 5,
	}
	stop := make(chan int)
	go func() {
		err := handler.StorageClient.StartClientAPI(stop, handler.GossipNodesPath)
		if err != nil {
			logrus.Fatal("Error while starting client API : ", err)
		}
	}()
	handler.StorageClient.TillReady("", 0)
}

// NISD
func (handler *ComHandler) StartUDPListner() {
	var err error
	handler.UdpSocket, err = net.ListenPacket("udp", ":"+handler.UdpPort)
	if err != nil {
		logrus.Error("UDP listner failed : ", err)
	}

	defer handler.UdpSocket.Close()
	for {
		buf := make([]byte, 1024)
		_, addr, err := handler.UdpSocket.ReadFrom(buf)
		if err != nil {
			continue
		}
		udpInfo := UdpMessage{
			addr:    addr,
			message: buf,
		}
		go handler.getConfigNSend(udpInfo)
	}
}

// NISD
func (handler *ComHandler) getConfigNSend(udpInfo UdpMessage) {
	//Get uuid from the byte array
	data := udpInfo.message
	uuidString := string(data[:36])

	handler.Epc.MarkAlive(uuidString)

	//Send config read request to PMDB server
	//TODO: make this call a funtion Request which is application specific through the interface
	responseByte, _ := applications.RequestPMDB(uuidString) //handler.RequestPMDB(uuidString)

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
	handler.UdpSocket.WriteTo(structByteArray, udpInfo.addr)
}

// NISD
func (handler *ComHandler) SetTags() {
	for {
		//TODO: make this a function that is application specific through the interface
		tagData := handler.GetCompressedGossipDataNISD()
		err := handler.SerfHandler.SetNodeTags(tagData)
		if err != nil {
			logrus.Debug("setTags: ", err)
		}
		time.Sleep(time.Duration(SetTagsInterval) * time.Second)
	}
}

func (handler *ComHandler) GetCompressedGossipDataNISD() map[string]string {
	returnMap := make(map[string]string)
	epMap := handler.Epc.GetList()
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
	httpPort := handler.RecvdPort
	returnMap["Type"] = "LOOKOUT"
	returnMap["Hport"] = strconv.Itoa(httpPort)
	return returnMap
}

func (handler *ComHandler) LoadConfigInfo() error {
	//Get addrs and Rports and store it in handler
	if _, err := os.Stat(handler.GossipNodesPath); os.IsNotExist(err) {
		logrus.Error("GossipNodesPath does not exist:", handler.GossipNodesPath)
		return err
	}
	reader, err := os.OpenFile(handler.GossipNodesPath, os.O_RDONLY, 0444)
	if err != nil {
		logrus.Error("Error while opening GossipNodesPath file")
		return err
	}

	scanner := bufio.NewScanner(reader)
	//Read IPAddrs
	scanner.Scan()
	IPAddrs := strings.Split(scanner.Text(), " ")
	logrus.Debug("IPAddrs:", IPAddrs)
	handler.Addr = IPAddrs[0]

	//TODO: fix this the ip addr contains ports and the ports are not being put into ServicePortRangeS and ServicePortRangeE
	//Read Ports
	scanner.Scan()
	Ports := strings.Split(scanner.Text(), " ")
	temp, _ := strconv.Atoi(Ports[0])
	handler.ServicePortRangeS = uint16(temp)
	temp, _ = strconv.Atoi(Ports[1])
	handler.ServicePortRangeE = uint16(temp)

	handler.makeRange()
	return nil
}

func (handler *ComHandler) makeRange() {
	a := make([]uint16, handler.ServicePortRangeE-handler.ServicePortRangeS+1)
	for i := range a {
		a[i] = uint16(handler.ServicePortRangeS + uint16(i))
	}
	handler.PortRange = a
}
