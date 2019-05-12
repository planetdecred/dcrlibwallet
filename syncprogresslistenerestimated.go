package dcrlibwallet

import "encoding/json"

type EstimatedSyncProgressListener interface {
	OnGeneralSyncProgress(report GeneralSyncProgressReport)
	OnHeadersFetchProgress(report HeadersFetchProgressReport, generalProgress GeneralSyncProgressReport)
	OnAddressDiscoveryProgress(report AddressDiscoveryProgressReport, generalProgress GeneralSyncProgressReport)
	OnHeadersRescanProgress(report HeadersRescanProgressReport, generalProgress GeneralSyncProgressReport)
}

type EstimatedSyncProgressJsonListener interface {
	OnGeneralSyncProgress(report string)
	OnHeadersFetchProgress(report string, generalProgress string)
	OnAddressDiscoveryProgress(report string, generalProgress string)
	OnHeadersRescanProgress(report string, generalProgress string)
	OnError(err error)
}

type EstimatedSyncProgressListenerJsonWrapper struct {
	jsonListener EstimatedSyncProgressJsonListener
}

func (wrapper *EstimatedSyncProgressListenerJsonWrapper) OnGeneralSyncProgress(report GeneralSyncProgressReport) {
	reportJson, err := json.Marshal(report)
	if err != nil {
		log.Error(err)
		wrapper.jsonListener.OnError(err)
		return
	}

	wrapper.jsonListener.OnGeneralSyncProgress(string(reportJson))
}

func (wrapper *EstimatedSyncProgressListenerJsonWrapper) OnHeadersFetchProgress(report HeadersFetchProgressReport, generalProgress GeneralSyncProgressReport) {
	reportJson, err := json.Marshal(report)
	if err != nil {
		log.Error(err)
		wrapper.jsonListener.OnError(err)
		return
	}

	generalProgressJson, err := json.Marshal(generalProgress)
	if err != nil {
		log.Error(err)
		wrapper.jsonListener.OnError(err)
		return
	}

	wrapper.jsonListener.OnHeadersFetchProgress(string(reportJson), string(generalProgressJson))
}

func (wrapper *EstimatedSyncProgressListenerJsonWrapper) OnAddressDiscoveryProgress(report AddressDiscoveryProgressReport, generalProgress GeneralSyncProgressReport) {
	reportJson, err := json.Marshal(report)
	if err != nil {
		log.Error(err)
		wrapper.jsonListener.OnError(err)
		return
	}

	generalProgressJson, err := json.Marshal(generalProgress)
	if err != nil {
		log.Error(err)
		wrapper.jsonListener.OnError(err)
		return
	}

	wrapper.jsonListener.OnAddressDiscoveryProgress(string(reportJson), string(generalProgressJson))
}

func (wrapper *EstimatedSyncProgressListenerJsonWrapper) OnHeadersRescanProgress(report HeadersRescanProgressReport, generalProgress GeneralSyncProgressReport) {
	reportJson, err := json.Marshal(report)
	if err != nil {
		log.Error(err)
		wrapper.jsonListener.OnError(err)
		return
	}

	generalProgressJson, err := json.Marshal(generalProgress)
	if err != nil {
		log.Error(err)
		wrapper.jsonListener.OnError(err)
		return
	}

	wrapper.jsonListener.OnHeadersRescanProgress(string(reportJson), string(generalProgressJson))
}

type GeneralSyncProgressReport struct {
	Status         string `json:"status"`
	ConnectedPeers int32  `json:"connectedPeers"`
	Error          string `json:"error"`
	Done           bool   `json:"done"`

	TotalSyncProgress  int32    `json:"totalSyncProgress"`
	TotalTimeRemaining string   `json:"totalTimeRemaining"`
}

type HeadersFetchProgressReport struct {
	TotalHeadersToFetch  int32  `json:"totalHeadersToFetch"`
	DaysBehind           string `json:"daysBehind"`
	FetchedHeadersCount  int32  `json:"fetchedHeadersCount"`
	HeadersFetchProgress int32  `json:"headersFetchProgress"`
}

type AddressDiscoveryProgressReport struct {
	AddressDiscoveryProgress int32 `json:"addressDiscoveryProgress"`
}

type HeadersRescanProgressReport struct {
	TotalHeadersToScan  int32  `json:"totalHeadersToScan"`
	RescanProgress      int32 `json:"rescanProgress"`
	CurrentRescanHeight int32 `json:"currentRescanHeight"`
}
