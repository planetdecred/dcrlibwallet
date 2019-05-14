package dcrlibwallet

import (
	"encoding/json"

	"github.com/raedahgroup/dcrlibwallet/syncprogressestimator"
)

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

func (wrapper *EstimatedSyncProgressListenerJsonWrapper) OnGeneralSyncProgress(report syncprogressestimator.GeneralSyncProgressReport) {
	reportJson, err := json.Marshal(report)
	if err != nil {
		log.Error(err)
		wrapper.jsonListener.OnError(err)
		return
	}

	wrapper.jsonListener.OnGeneralSyncProgress(string(reportJson))
}

func (wrapper *EstimatedSyncProgressListenerJsonWrapper) OnHeadersFetchProgress(report syncprogressestimator.HeadersFetchProgressReport, generalProgress syncprogressestimator.GeneralSyncProgressReport) {
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

func (wrapper *EstimatedSyncProgressListenerJsonWrapper) OnAddressDiscoveryProgress(report syncprogressestimator.AddressDiscoveryProgressReport, generalProgress syncprogressestimator.GeneralSyncProgressReport) {
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

func (wrapper *EstimatedSyncProgressListenerJsonWrapper) OnHeadersRescanProgress(report syncprogressestimator.HeadersRescanProgressReport, generalProgress syncprogressestimator.GeneralSyncProgressReport) {
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
