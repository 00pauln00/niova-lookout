func (handler *nisdMonitor) requestPMDB(key string) ([]byte, error) {
	request := requestResponseLib.KVRequest{
		Operation: "read",
		Key:       key,
	}
	var requestByte bytes.Buffer
	enc := gob.NewEncoder(&requestByte)
	enc.Encode(request)
	responseByte, err := handler.storageClient.Request(requestByte.Bytes(), "", false)
	return responseByte, err
}