package domain

const TransportLocalSend = "localsend"

const (
	LocalSendConfigure  CommandKind = "localsend_configure"
	LocalSendRefresh    CommandKind = "localsend_refresh"
	LocalSendAddDevice  CommandKind = "localsend_add_device"
	LocalSendSend       CommandKind = "localsend_send"
	LocalSendAnswer     CommandKind = "localsend_answer"
	LocalSendLink       CommandKind = "localsend_link"
	LocalSendCloseLink  CommandKind = "localsend_close_link"
	LocalSendAnswerLink CommandKind = "localsend_answer_link"
)

type LocalSendConfig struct {
	Enabled bool
	Port    int
	HTTP    bool
	PIN     string `json:"-"`
}

type LocalSendDevice struct {
	ID, Name, Model, Type, Address string
	Plain, Changed                 bool
}

type LocalSendFile struct {
	ID, Name string
	Size     int64
}

type LocalSendTransferItem struct {
	ID, Name, Status string
	Size             int64
}

type LocalSendOffer struct {
	ID, From, Text string
	IsText         bool
	Files          []LocalSendFile
}

type LocalSendLinkState struct {
	URL                    string
	Upload                 bool
	QR                     []byte
	RequestID, RequestFrom string
}

type LocalSendStatus struct {
	ProgressRevision uint64
	Active           bool
	Unavailable      bool
	HasPIN           bool
	Port             int
	Alias, Problem   string
	Devices          []LocalSendDevice
	Offer            LocalSendOffer
	Link             LocalSendLinkState
}

type LocalSendRequest struct {
	SetPIN    bool
	Config    LocalSendConfig
	AllowHTTP bool
	Upload    bool
	HTTPS     bool
}
